package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
)

type projectBranch struct {
	raw        jsontext.Value
	decoded    project.Config
	value      any
	needsValue bool
	services   bool
	banked     bool
	err        error
}

type engine struct {
	branches map[string]projectBranch
	config   project.Config
	err      error
	ready    bool
	checks   *preparedChecks
	profiles any
	size     int
	timeline *historyTimeline
}

// NewCall returns a private editor-worker handler with owned project state.
// Call it serially. Synchronization replaces changed top-level branches.
// Explicit project requests remain stateless and do not replace this state.
func NewCall() func(string) string {
	model := new(engine)
	return func(input string) string {
		result, err := model.handle(input)
		return encodeResponse(result, err)
	}
}

func (e *engine) handle(input string) (response, error) {
	command, err := decodeRequest(input)
	if err != nil {
		return response{helper: helperOperation(command.Op)}, err
	}
	if helperOperation(command.Op) {
		result, helperErr := executeHelper(command, input)
		result.helper = true
		return result, helperErr
	}
	if command.Op == "history" {
		if len(command.Project) != 0 || len(command.ParkRide) != 0 || len(command.View) != 0 || len(command.Edit) != 0 || command.Keys != nil || len(command.Patch) != 0 {
			return response{}, errors.New("history cannot include other operation parameters")
		}
		return e.historyOperation(command.History)
	}
	if len(command.History) != 0 {
		return response{}, errors.New("the editor operation cannot include history parameters")
	}
	if command.Op == "sync" {
		if len(command.Project) != 0 || len(command.ParkRide) != 0 || len(command.View) != 0 || len(command.Edit) != 0 {
			return response{}, errors.New("synchronization cannot include operation parameters")
		}
		return e.sync(command)
	}
	if len(command.Project) != 0 {
		return execute(input)
	}
	if command.Keys != nil || len(command.Patch) != 0 {
		return response{}, errors.New("an operation cannot include synchronization fields")
	}
	if !e.ready {
		return response{}, errors.New("the editor project is not synchronized")
	}
	if command.Op == "edit" {
		if len(command.ParkRide) != 0 || len(command.View) != 0 {
			return response{}, errors.New("editor operation has unrelated parameters")
		}
		return e.edit(command.Edit)
	}
	if command.Op == "checks" {
		if len(command.ParkRide) != 0 || len(command.View) != 0 || len(command.Edit) != 0 {
			return response{}, errors.New("checks do not accept operation parameters")
		}
		checks, err := e.draftChecks()
		if err != nil {
			return response{}, err
		}
		return response{Checks: &checks}, nil
	}
	if e.err != nil {
		return response{}, e.err
	}
	return operate(e.config, command)
}

// sync replaces the changed branches of the owned project. It decodes the
// patch, takes the next branches, decodes the changed branches, and checks
// the metadata branches together. A request, encode, or draft decode error
// leaves the engine unchanged and gives an empty response. A project error
// synchronizes the project and marks it not valid.
func (e *engine) sync(command request) (response, error) {
	patch, err := decodeSyncPatch(command)
	if err != nil {
		return response{}, err
	}
	next, size, err := e.patchBranches(command.Keys, patch)
	if err != nil {
		return response{}, err
	}
	err = checkDeclaredKeys(patch, next)
	if err != nil {
		return response{}, err
	}
	config, firstError, err := e.combineBranches(command.Keys, next)
	if err != nil {
		return response{}, err
	}
	if firstError == nil && metadataCheckNeeded(next, config) {
		if firstError, err = checkMetadataBranches(next); err != nil {
			return response{}, err
		}
	}
	e.invalidateCaches(next)
	e.branches, e.config, e.err, e.ready, e.size = next, config, firstError, true, size
	if firstError != nil {
		return response{Synced: true}, firstError
	}
	return response{Valid: true, Synced: true}, nil
}

// decodeSyncPatch checks that command has keys and a patch object, and
// decodes the patch.
func decodeSyncPatch(command request) (map[string]jsontext.Value, error) {
	if command.Keys == nil || command.Patch.Kind() != '{' {
		return nil, errors.New("synchronization needs project keys and a patch object")
	}
	var patch map[string]jsontext.Value
	if err := json.Unmarshal(command.Patch, &patch); err != nil {
		return nil, fmt.Errorf("decode project patch: %w", err)
	}
	return patch, nil
}

// patchBranches returns the branch of each key after the patch, and the
// size of the encoded project. It checks the keys in order.
func (e *engine) patchBranches(keys []string, patch map[string]jsontext.Value) (map[string]projectBranch, int, error) {
	next := make(map[string]projectBranch, len(keys))
	size := 2
	for index, key := range keys {
		if _, exists := next[key]; exists {
			return nil, 0, errors.New("project synchronization repeats a key")
		}
		branch, err := e.patchedBranch(key, patch)
		if err != nil {
			return nil, 0, err
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, 0, fmt.Errorf("encode project key: %w", err)
		}
		size += len(encodedKey) + 1 + len(branch.raw)
		if index > 0 {
			size++
		}
		if size > project.MaxFileBytes {
			return nil, 0, errors.New("editor project is too large")
		}
		next[key] = branch
	}
	return next, size, nil
}

// patchedBranch returns the branch of key after the patch. A changed value
// starts a new branch. A key that the patch does not change must have a
// branch.
func (e *engine) patchedBranch(key string, patch map[string]jsontext.Value) (projectBranch, error) {
	branch, exists := e.branches[key]
	raw, changed := patch[key]
	if !changed {
		if !exists {
			return projectBranch{}, fmt.Errorf("project synchronization needs %s", key)
		}
		return branch, nil
	}
	if !exists || !bytes.Equal(raw, branch.raw) {
		branch = projectBranch{raw: raw}
	}
	return branch, nil
}

// checkDeclaredKeys checks that each key of the patch is in next.
func checkDeclaredKeys(patch map[string]jsontext.Value, next map[string]projectBranch) error {
	for key := range patch {
		if _, exists := next[key]; !exists {
			return errors.New("project patch contains an undeclared key")
		}
	}
	return nil
}

// combineBranches decodes each changed branch into next, in key order, and
// copies each branch into one project. The first branch error is the
// project error. An encode or draft decode error stops the synchronization,
// and is the last result.
func (e *engine) combineBranches(keys []string, next map[string]projectBranch) (config project.Config, firstError, err error) {
	for _, key := range keys {
		branch := next[key]
		if previous, exists := e.branches[key]; !exists || !bytes.Equal(previous.raw, branch.raw) {
			if branch, err = decodeBranch(key, branch); err != nil {
				return project.Config{}, nil, err
			}
			next[key] = branch
		}
		if branchErr := copyDecodedBranch(&config, key, branch); firstError == nil {
			firstError = branchErr
		}
	}
	return config, firstError, nil
}

// decodeBranch decodes the raw value of a changed branch. It keeps the
// project decode error in the branch.
func decodeBranch(key string, branch projectBranch) (projectBranch, error) {
	if key != "demandProfiles" {
		if err := json.Unmarshal(branch.raw, &branch.value); err != nil {
			return projectBranch{}, fmt.Errorf("decode editor draft branch: %w", err)
		}
		branch.services = hasServiceMetadata(map[string]any{key: branch.value})
		branch.banked = key == "network" && hasBanks(branch.value)
	}
	encoded, err := json.Marshal(map[string]jsontext.Value{key: branch.raw})
	if err != nil {
		return projectBranch{}, fmt.Errorf("encode project branch: %w", err)
	}
	// Check version and field presence after combining the branches.
	type branchConfig project.Config
	var decoded branchConfig
	branch.err = json.Unmarshal(encoded, &decoded, json.RejectUnknownMembers(true))
	branch.decoded = project.Config(decoded)
	return branch, nil
}

// copyDecodedBranch copies a decoded branch into config. It returns the
// decode error of the branch, or an error for a key that the editor does
// not support.
func copyDecodedBranch(config *project.Config, key string, branch projectBranch) error {
	if branch.err != nil {
		return fmt.Errorf("decode project branch %s: %w", key, branch.err)
	}
	if !copyBranch(config, key, branch.decoded) {
		return fmt.Errorf("unsupported editor project field %s", key)
	}
	return nil
}

// serviceMetadataKeys are the branches that can hold service metadata.
var serviceMetadataKeys = []string{"orderContract", "network", "fleet", "expressServices", "onboardPickups"}

// featureKeys are the incident, fault, and emergency branches. Branch
// decoding accepts an empty or null incident, fault, or emergency marker, a
// null faults or emergencies value, and settings without their marker.
var featureKeys = []string{"incidentContract", "faultContract", "faults", "emergencyContract", "emergencies"}

// metadataKeys are the branches that checkMetadataBranches decodes
// together.
var metadataKeys = []string{"version", "orderContract", "network", "fleet", "expressServices", "onboardPickups", "incidentContract", "faultContract", "faults", "emergencyContract", "emergencies"}

// metadataCheckNeeded reports whether the metadata branches need the native
// project decoder. This is so for banks, a version other than the current
// version, service metadata, and any feature branch.
func metadataCheckNeeded(next map[string]projectBranch, config project.Config) bool {
	servicePresent := slices.ContainsFunc(serviceMetadataKeys, func(key string) bool { return next[key].services })
	featurePresent := slices.ContainsFunc(featureKeys, func(key string) bool { _, present := next[key]; return present })
	return next["network"].banked || config.Version != project.CurrentVersion || servicePresent || featurePresent
}

// checkMetadataBranches decodes the metadata branches together with the
// native project decoder. It returns the decode error first. An encode
// error is the last result.
func checkMetadataBranches(next map[string]projectBranch) (checkErr, err error) {
	fields := make(map[string]jsontext.Value, 4)
	for _, key := range metadataKeys {
		if branch, present := next[key]; present {
			fields[key] = branch.raw
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode editor metadata branches: %w", err)
	}
	var checked project.Config
	return json.Unmarshal(raw, &checked, json.RejectUnknownMembers(true)), nil
}

// invalidateCaches clears the cached profiles and checks that read a
// changed branch: the profiles, then the network checks, and then the
// service checks.
func (e *engine) invalidateCaches(next map[string]projectBranch) {
	if !bytes.Equal(e.branches["demandProfiles"].raw, next["demandProfiles"].raw) {
		e.profiles = nil
		if e.checks != nil {
			e.checks.profiles = nil
			e.checks.profilesReady = false
		}
	}
	if !bytes.Equal(e.branches["network"].raw, next["network"].raw) {
		e.checks = nil
	}
	if e.checks != nil && slices.ContainsFunc(serviceCheckKeys, func(key string) bool { return !bytes.Equal(e.branches[key].raw, next[key].raw) }) {
		e.checks.servicesReady = false
	}
}

// serviceCheckKeys are the branches that the cached service and contract checks read.
var serviceCheckKeys = []string{"version", "orderContract", "fleet", "expressServices", "onboardPickups", "sharedRidePartyLimit", "sharedRideMode"}

func copyBranch(dst *project.Config, key string, src project.Config) bool {
	switch key {
	case "version":
		dst.Version = src.Version
	case "orderContract":
		dst.OrderContract = src.OrderContract
	case "incidentContract":
		dst.IncidentContract = src.IncidentContract
	case "faultContract":
		dst.FaultContract = src.FaultContract
	case "faults":
		dst.Faults = src.Faults
	case "emergencyContract":
		dst.EmergencyContract = src.EmergencyContract
	case "emergencies":
		dst.Emergencies = src.Emergencies
	case "name":
		dst.Name = src.Name
	case "network":
		dst.Network = src.Network
	case "fleet":
		dst.Fleet = src.Fleet
	case "expressServices":
		dst.ExpressServices = src.ExpressServices
	case "demand":
		dst.Demand = src.Demand
	case "demandProfiles":
		dst.DemandProfiles = src.DemandProfiles
	case "railArrivals":
		dst.RailArrivals = src.RailArrivals
	case "railDepartures":
		dst.RailDepartures = src.RailDepartures
	case "sharedRidePartyLimit":
		dst.SharedRidePartyLimit = src.SharedRidePartyLimit
	case "sharedRideMode":
		dst.SharedRideMode = src.SharedRideMode
	case "sharedRideMaxStops":
		dst.SharedRideMaxStops = src.SharedRideMaxStops
	case "sharedRideJoin":
		dst.SharedRideJoin = src.SharedRideJoin
	case "onboardPickups":
		dst.OnboardPickups = src.OnboardPickups
	case "pickupReassignment":
		dst.PickupReassignment = src.PickupReassignment
	case "platoonLimit":
		dst.PlatoonLimit = src.PlatoonLimit
	case "redistribution":
		dst.Redistribution = src.Redistribution
	case "geo":
		dst.Geo = src.Geo
	case "map":
		dst.Map = src.Map
	default:
		return false
	}
	return true
}
