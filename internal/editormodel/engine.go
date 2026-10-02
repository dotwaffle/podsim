package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

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

func (e *engine) sync(command request) (response, error) {
	if command.Keys == nil || command.Patch.Kind() != '{' {
		return response{}, errors.New("synchronization needs project keys and a patch object")
	}
	var patch map[string]jsontext.Value
	if err := json.Unmarshal(command.Patch, &patch); err != nil {
		return response{}, fmt.Errorf("decode project patch: %w", err)
	}
	next := make(map[string]projectBranch, len(command.Keys))
	size := 2
	for index, key := range command.Keys {
		if _, exists := next[key]; exists {
			return response{}, errors.New("project synchronization repeats a key")
		}
		branch, exists := e.branches[key]
		if raw, changed := patch[key]; changed {
			if !exists || !bytes.Equal(raw, branch.raw) {
				branch = projectBranch{raw: raw}
			}
		} else if !exists {
			return response{}, fmt.Errorf("project synchronization needs %s", key)
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return response{}, fmt.Errorf("encode project key: %w", err)
		}
		size += len(encodedKey) + 1 + len(branch.raw)
		if index > 0 {
			size++
		}
		if size > project.MaxFileBytes {
			return response{}, errors.New("editor project is too large")
		}
		next[key] = branch
	}
	for key := range patch {
		if _, exists := next[key]; !exists {
			return response{}, errors.New("project patch contains an undeclared key")
		}
	}
	var config project.Config
	var firstError error
	for _, key := range command.Keys {
		branch := next[key]
		previous, exists := e.branches[key]
		if !exists || !bytes.Equal(previous.raw, branch.raw) {
			if key != "demandProfiles" {
				if err := json.Unmarshal(branch.raw, &branch.value); err != nil {
					return response{}, fmt.Errorf("decode editor draft branch: %w", err)
				}
				branch.services = hasServiceMetadata(map[string]any{key: branch.value})
				branch.banked = key == "network" && hasBanks(branch.value)
			}
			encoded, err := json.Marshal(map[string]jsontext.Value{key: branch.raw})
			if err != nil {
				return response{}, fmt.Errorf("encode project branch: %w", err)
			}
			// Check version and field presence after combining the branches.
			type branchConfig project.Config
			var decoded branchConfig
			branch.err = json.Unmarshal(encoded, &decoded, json.RejectUnknownMembers(true))
			branch.decoded = project.Config(decoded)
			next[key] = branch
		}
		if branch.err != nil {
			if firstError == nil {
				firstError = fmt.Errorf("decode project branch %s: %w", key, branch.err)
			}
			continue
		}
		if !copyBranch(&config, key, branch.decoded) && firstError == nil {
			firstError = fmt.Errorf("unsupported editor project field %s", key)
		}
	}
	servicePresent := next["network"].services || next["fleet"].services || next["expressServices"].services
	if firstError == nil && (next["network"].banked || config.Version == 2 || config.Version == 3 || servicePresent) {
		fields := make(map[string]jsontext.Value, 4)
		for _, key := range []string{"version", "network", "fleet", "expressServices"} {
			if branch, present := next[key]; present {
				fields[key] = branch.raw
			}
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			return response{}, fmt.Errorf("encode editor metadata branches: %w", err)
		}
		var checked project.Config
		firstError = json.Unmarshal(raw, &checked, json.RejectUnknownMembers(true))
	}
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
	if e.checks != nil {
		for _, key := range []string{"version", "fleet", "expressServices"} {
			if !bytes.Equal(e.branches[key].raw, next[key].raw) {
				e.checks.servicesReady = false
				break
			}
		}
	}
	e.branches, e.config, e.err, e.ready, e.size = next, config, firstError, true, size
	if firstError != nil {
		return response{Synced: true}, firstError
	}
	return response{Valid: true, Synced: true}, nil
}

func copyBranch(dst *project.Config, key string, src project.Config) bool {
	switch key {
	case "version":
		dst.Version = src.Version
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
	case "stationBuffers":
		dst.StationBuffers = src.StationBuffers
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
