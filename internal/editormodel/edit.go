package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type editCommand struct {
	Field  string         `json:"field"`
	Value  jsontext.Value `json:"value"`
	Target jsontext.Value `json:"target,omitempty"`
	Editor jsontext.Value `json:"editor,omitzero"`
}

type projectChange struct {
	Patch map[string]any `json:"patch"`
	// Flag names a valid boolean edit that preserves the existing checks.
	Flag       string            `json:"flag,omitempty"`
	Background *backgroundChange `json:"background,omitempty"`
	Note       string            `json:"note,omitempty"`
}

// editProject proposes owned replacement branches. The caller commits them
// only after its draft and gesture guards pass. This operation does not
// change the synchronized worker state or the existing undo history.
func editProject(draft any, raw jsontext.Value) (projectChange, error) {
	if raw.Kind() != '{' {
		return projectChange{}, errors.New("a project edit needs a command object")
	}
	var command editCommand
	if err := json.Unmarshal(raw, &command, json.RejectUnknownMembers(true)); err != nil {
		return projectChange{}, fmt.Errorf("decode editor change: %w", err)
	}
	if len(command.Editor) != 0 && command.Editor.Kind() != 't' && command.Editor.Kind() != 'f' {
		return projectChange{}, errors.New("the editor defaults flag must be boolean")
	}
	change, err := proposeProjectEdit(draft, command)
	if err == nil && command.Editor.Kind() == 't' {
		repairDemandDestination(draft, &change)
	}
	return change, err
}

func proposeProjectEdit(draft any, command editCommand) (projectChange, error) {
	switch command.Field {
	case "geometry":
		return proposeDocumentEdit(draft, command, "a geometry edit does not accept a target", editGeometry)
	case "map":
		return proposeDocumentEdit(draft, command, "a map edit does not accept a target", editMap)
	case "background":
		return proposeDocumentEdit(draft, command, "a background edit does not accept a target", editBackground)
	case "railArrival":
		return proposeDocumentEdit(draft, command, "a rail edit does not accept a target", editRailArrival)
	case "railDeparture":
		return proposeDocumentEdit(draft, command, "a rail edit does not accept a target", editRailDeparture)
	}
	value, err := scalarEditValue(command)
	if err != nil {
		return projectChange{}, err
	}
	return proposeScalarEdit(draft, command, value)
}

// proposeDocumentEdit proposes an edit whose value is a JSON document.
// Such an edit does not accept a target, and refuses one with refusal.
func proposeDocumentEdit(draft any, command editCommand, refusal string, edit func(any, jsontext.Value) (projectChange, error)) (projectChange, error) {
	if len(command.Target) != 0 {
		return projectChange{}, errors.New(refusal)
	}
	return edit(draft, command.Value)
}

func editRailArrival(draft any, raw jsontext.Value) (projectChange, error) {
	return editRail(draft, false, raw)
}

func editRailDeparture(draft any, raw jsontext.Value) (projectChange, error) {
	return editRail(draft, true, raw)
}

// scalarEditValue checks that command sets a field to a scalar value, and
// decodes the value. Only a fleet edit accepts a target.
func scalarEditValue(command editCommand) (any, error) {
	if command.Field == "" || len(command.Value) == 0 || command.Value.Kind() == 'n' || command.Value.Kind() == '{' || command.Value.Kind() == '[' {
		return nil, errors.New("a project edit needs a field and a scalar value")
	}
	if len(command.Target) != 0 && command.Field != "fleetCount" {
		return nil, errors.New("the editor field does not accept a target")
	}
	var value any
	if err := json.Unmarshal(command.Value, &value); err != nil {
		return nil, fmt.Errorf("decode editor value: %w", err)
	}
	return value, nil
}

// proposeScalarEdit proposes the edit of a field with a scalar value.
// Normalization and the conversion to trains propose their own change.
func proposeScalarEdit(draft any, command editCommand, value any) (projectChange, error) {
	change := projectChange{Patch: make(map[string]any)}
	var err error
	switch command.Field {
	case "normalize":
		if value != true {
			return projectChange{}, errors.New("normalization requires a true value")
		}
		return normalizeProject(draft)
	case "fleetCount":
		err = change.fleetCountAt(draft, command.Target, value)
	case "dailyStartTime":
		err = change.dailyStartTime(draft, value)
	case "name":
		err = change.name(draft, value)
	case "demandEnabled", "redistribution", "pickupReassignment":
		err = change.operatingFlag(draft, command.Field, value)
	case "demandRate", "demandSeed":
		err = change.demandNumber(draft, command.Field, value)
	case "demandDestination", "demandBand", "demandProfile", "demandPattern":
		err = change.demandSelection(draft, command.Field, value)
	case "sharedRidePartyLimit", "sharedRideMaxStops":
		change.sharingLimit(draft, command.Field, value)
	case "sharedRideMode", "sharedRideJoin":
		err = change.sharingPolicy(draft, command.Field, value)
	case "platoonLimit":
		change.platoonLimit(draft, command.Field, value)
	default:
		return projectChange{}, errors.New("unknown editor field")
	}
	if err != nil {
		return projectChange{}, err
	}
	return change, nil
}

// fleetCountAt sets the fleet count of the station that target names.
func (c *projectChange) fleetCountAt(draft any, target jsontext.Value, value any) error {
	if target.Kind() != '"' {
		return errors.New("a fleet edit needs a station ID string")
	}
	var stationID string
	if err := json.Unmarshal(target, &stationID); err != nil {
		return fmt.Errorf("decode fleet station ID: %w", err)
	}
	return c.fleetCount(draft, stationID, value)
}

func (c *projectChange) dailyStartTime(draft, value any) error {
	minute, err := editClock(value)
	if err != nil {
		return err
	}
	return c.demand(draft, "dailyStartMinute", minute)
}

func (c *projectChange) name(draft, value any) error {
	name, ok := value.(string)
	if !ok {
		return errors.New("the project name must be text")
	}
	c.set(draft, "name", trimEditorSpace(name))
	return nil
}

// operatingFlag sets a Boolean operating flag. The change names the flag
// when the draft already has a Boolean value for it.
func (c *projectChange) operatingFlag(draft any, field string, value any) error {
	flag, ok := value.(bool)
	if !ok {
		return errors.New("an operating flag must be true or false")
	}
	previous := member(draft, field)
	if field == "demandEnabled" {
		if err := c.demand(draft, "enabled", flag); err != nil {
			return err
		}
		previous = member(member(draft, "demand"), "enabled")
	} else {
		c.set(draft, field, flag)
	}
	if _, valid := previous.(bool); valid {
		c.Flag = field
	}
	return nil
}

// demandNumber sets the demand rate or the demand seed to a whole number.
// The seed is not negative.
func (c *projectChange) demandNumber(draft any, field string, value any) error {
	x, err := editNumber(value)
	if err != nil {
		return err
	}
	key := "perMinute"
	x = math.Floor(x)
	if field == "demandSeed" {
		key, x = "seed", max(0, x)
	}
	return c.demand(draft, key, x)
}

// sharingLimit sets the party limit or the stop limit, clamped to its
// range. A number that is not valid, or zero, sets the default.
func (c *projectChange) sharingLimit(draft any, field string, value any) {
	limit, fallback := float64(sim.MaxSharedRideParties), 1.0
	if field == "sharedRideMaxStops" {
		limit, fallback = sim.MaxSharedRideStops, 3
	}
	x, err := editNumber(value)
	if err != nil || x == 0 {
		x = fallback
	}
	c.set(draft, field, max(1, min(limit, math.Floor(x))))
}

// sharingPolicy sets the sharing mode or the join policy. A name that is
// not known sets the default.
func (c *projectChange) sharingPolicy(draft any, field string, value any) error {
	accepted, fallback := []string{"drop-offs", "destination"}, "drop-offs"
	if field == "sharedRideJoin" {
		accepted, fallback = []string{"unassigned", "reassign-existing"}, "unassigned"
	}
	setting, ok := value.(string)
	if !ok {
		return errors.New("a sharing policy must be text")
	}
	if !slices.Contains(accepted, setting) {
		setting = fallback
	}
	c.set(draft, field, setting)
	return nil
}

// platoonLimit sets the platoon limit. A limit that is not valid sets
// zero.
func (c *projectChange) platoonLimit(draft any, field string, value any) {
	x, err := editNumber(value)
	if err != nil || !draftPlatoonLimit(x) {
		x = 0
	}
	c.set(draft, field, x)
}

func editClock(value any) (float64, error) {
	clock, ok := value.(string)
	if !ok || len(clock) != 5 || clock[2] != ':' {
		return 0, errors.New("the daily clock must use HH:MM")
	}
	for _, index := range []int{0, 1, 3, 4} {
		if clock[index] < '0' || clock[index] > '9' {
			return 0, errors.New("the daily clock must use HH:MM")
		}
	}
	hour := int(clock[0]-'0')*10 + int(clock[1]-'0')
	minute := int(clock[3]-'0')*10 + int(clock[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, errors.New("the daily clock is outside the day")
	}
	return float64(hour*60 + minute), nil
}

func trimEditorSpace(value string) string {
	return strings.TrimFunc(value, func(r rune) bool {
		return r >= '\t' && r <= '\r' || r >= '\u2000' && r <= '\u200a' ||
			slices.Contains([]rune{' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff'}, r)
	})
}

func editNumber(value any) (float64, error) {
	if input, ok := value.(string); ok {
		input = trimEditorSpace(input)
		if input == "" {
			return 0, nil
		}
		parsed, err := strconv.ParseFloat(input, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, errors.New("an editor number must be finite")
		}
		return parsed, nil
	}
	if !finite(value) {
		return 0, errors.New("an editor number must be finite")
	}
	return number(value), nil
}

func (c *projectChange) set(draft any, key string, value any) {
	if member(draft, key) != value {
		c.Patch[key] = value
	}
}

func (c *projectChange) demand(draft any, key string, value any) error {
	original := object(member(draft, "demand"))
	if original == nil {
		return errors.New("the draft needs a demand settings object")
	}
	if original[key] == value {
		return nil
	}
	updated := object(cloneEditValue(original))
	updated[key] = value
	c.Patch["demand"] = updated
	return nil
}

func (c *projectChange) demandSelection(draft any, field string, value any) error {
	selected, ok := value.(string)
	if !ok {
		return errors.New("a demand selection must be text")
	}
	original := object(member(draft, "demand"))
	if original == nil {
		return errors.New("the draft needs a demand settings object")
	}
	updated := maps.Clone(original)
	switch field {
	case "demandDestination":
		updated["destination"] = selected
	case "demandBand":
		updated["band"] = selected
	case "demandProfile":
		updated["profile"], updated["band"] = selected, ""
		for _, profile := range items(member(draft, "demandProfiles")) {
			if text(member(profile, "id")) == selected {
				updated["band"] = firstBand(profile)
				break
			}
		}
	case "demandPattern":
		if !slices.Contains([]string{"balanced", "destination", "market", "profile", "profile-daily", "rail-arrivals", "rail-services"}, selected) {
			return errors.New("the demand pattern is invalid")
		}
		updated["pattern"] = selected
		profiles := items(member(draft, "demandProfiles"))
		if selected == "profile" && len(profiles) != 0 {
			updated["profile"], updated["band"] = member(profiles[0], "id"), firstBand(profiles[0])
		}
		if selected == "profile-daily" {
			if len(profiles) != 0 {
				updated["profile"] = member(profiles[0], "id")
			}
			updated["band"] = ""
			if updated["dailyStartMinute"] == nil {
				updated["dailyStartMinute"] = float64(0)
			}
		} else {
			delete(updated, "dailyStartMinute")
		}
	}
	if !reflect.DeepEqual(original, updated) {
		c.Patch["demand"] = cloneEditValue(updated)
	}
	return nil
}

// cloneEditValue owns nested values in malformed drafts as well as valid ones.
func cloneEditValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		owned := make(map[string]any, len(value))
		for key, item := range value {
			owned[key] = cloneEditValue(item)
		}
		return owned
	case []any:
		owned := make([]any, len(value))
		for i, item := range value {
			owned[i] = cloneEditValue(item)
		}
		return owned
	default:
		return value
	}
}

func (e *engine) edit(raw jsontext.Value) (response, error) {
	if err := e.ensureNetworkValue(); err != nil {
		return response{}, err
	}
	draft := make(map[string]any, len(e.branches))
	for key, branch := range e.branches {
		draft[key] = branch.value
	}
	needsFlows := editNeedsFullProfiles(raw)
	// Keep raw selection IDs unchanged, including missing IDs in drafts.
	// Full flows are temporary and only supplied to edits that change them.
	if (e.profiles == nil || needsFlows) && len(e.branches["demandProfiles"].raw) != 0 {
		var value any
		if err := json.Unmarshal(e.branches["demandProfiles"].raw, &value); err != nil {
			return response{}, fmt.Errorf("decode editor profiles: %w", err)
		}
		if e.profiles == nil {
			e.profiles = profileSelections(value)
		}
		if needsFlows {
			draft["demandProfiles"] = value
		}
	}
	if !needsFlows {
		draft["demandProfiles"] = e.profiles
	}
	change, err := editProject(draft, raw)
	if err == nil {
		branches := make(map[string]jsontext.Value, len(e.branches))
		for key, branch := range e.branches {
			branches[key] = branch.raw
		}
		err = checkEditSize(e.size, branches, change.Patch)
	}
	return response{Change: &change}, err
}

func editNeedsFullProfiles(raw jsontext.Value) bool {
	var command editCommand
	if json.Unmarshal(raw, &command) != nil {
		return false
	}
	if command.Field == "normalize" {
		return true
	}
	if command.Field != "geometry" {
		return false
	}
	geometry, err := decodeGeometryEdit(command.Value)
	return err == nil && geometry.Action == "deleteStation"
}

func checkEditSize(size int, branches map[string]jsontext.Value, patch map[string]any) error {
	for key, value := range patch {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode editor change: %w", err)
		}
		size += len(encoded) - len(branches[key])
		if _, exists := branches[key]; !exists {
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return fmt.Errorf("encode editor change key: %w", err)
			}
			size += len(encodedKey) + 1
			if len(branches) != 0 {
				size++
			}
		}
	}
	if size > project.MaxFileBytes {
		return errors.New("the edit makes the project too large")
	}
	return nil
}

func firstBand(profile any) string {
	if bands := items(member(profile, "bands")); len(bands) != 0 {
		return text(member(bands[0], "id"))
	}
	return ""
}
