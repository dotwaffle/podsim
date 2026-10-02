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
)

type editCommand struct {
	Field  string         `json:"field"`
	Value  jsontext.Value `json:"value"`
	Target jsontext.Value `json:"target,omitempty"`
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
	if command.Field == "geometry" {
		if len(command.Target) != 0 {
			return projectChange{}, errors.New("a geometry edit does not accept a target")
		}
		return editGeometry(draft, command.Value)
	}
	if command.Field == "map" {
		if len(command.Target) != 0 {
			return projectChange{}, errors.New("a map edit does not accept a target")
		}
		return editMap(draft, command.Value)
	}
	if command.Field == "background" {
		if len(command.Target) != 0 {
			return projectChange{}, errors.New("a background edit does not accept a target")
		}
		return editBackground(draft, command.Value)
	}
	if command.Field == "railArrival" || command.Field == "railDeparture" {
		if len(command.Target) != 0 {
			return projectChange{}, errors.New("a rail edit does not accept a target")
		}
		return editRail(draft, command.Field == "railDeparture", command.Value)
	}
	if command.Field == "" || len(command.Value) == 0 || command.Value.Kind() == 'n' || command.Value.Kind() == '{' || command.Value.Kind() == '[' {
		return projectChange{}, errors.New("a project edit needs a field and a scalar value")
	}
	if len(command.Target) != 0 && command.Field != "fleetCount" {
		return projectChange{}, errors.New("the editor field does not accept a target")
	}
	var value any
	if err := json.Unmarshal(command.Value, &value); err != nil {
		return projectChange{}, fmt.Errorf("decode editor value: %w", err)
	}
	change := projectChange{Patch: make(map[string]any)}
	switch command.Field {
	case "normalize":
		if value != true {
			return projectChange{}, errors.New("normalization requires a true value")
		}
		return normalizeProject(draft)
	case "fleetCount":
		if command.Target.Kind() != '"' {
			return projectChange{}, errors.New("a fleet edit needs a station ID string")
		}
		var stationID string
		if err := json.Unmarshal(command.Target, &stationID); err != nil {
			return projectChange{}, fmt.Errorf("decode fleet station ID: %w", err)
		}
		if err := change.fleetCount(draft, stationID, value); err != nil {
			return projectChange{}, err
		}
	case "dailyStartTime":
		minute, err := editClock(value)
		if err != nil {
			return projectChange{}, err
		}
		if err := change.demand(draft, "dailyStartMinute", minute); err != nil {
			return projectChange{}, err
		}
	case "name":
		name, ok := value.(string)
		if !ok {
			return projectChange{}, errors.New("the project name must be text")
		}
		change.set(draft, "name", trimEditorSpace(name))
	case "demandEnabled", "redistribution", "stationBuffers", "pickupReassignment":
		flag, ok := value.(bool)
		if !ok {
			return projectChange{}, errors.New("an operating flag must be true or false")
		}
		if command.Field == "demandEnabled" {
			if err := change.demand(draft, "enabled", flag); err != nil {
				return projectChange{}, err
			}
		} else {
			change.set(draft, command.Field, flag)
		}
		previous := member(draft, command.Field)
		if command.Field == "demandEnabled" {
			previous = member(member(draft, "demand"), "enabled")
		}
		if _, valid := previous.(bool); valid {
			change.Flag = command.Field
		}
	case "demandRate", "demandSeed":
		x, err := editNumber(value)
		if err != nil {
			return projectChange{}, err
		}
		key := "perMinute"
		x = math.Floor(x)
		if command.Field == "demandSeed" {
			key, x = "seed", max(0, x)
		}
		if err := change.demand(draft, key, x); err != nil {
			return projectChange{}, err
		}
	case "demandDestination", "demandBand", "demandProfile", "demandPattern":
		if err := change.demandSelection(draft, command.Field, value); err != nil {
			return projectChange{}, err
		}
	case "sharedRidePartyLimit", "sharedRideMaxStops":
		limit, fallback := 8.0, 1.0
		if command.Field == "sharedRideMaxStops" {
			limit, fallback = 7, 3
		}
		x, err := editNumber(value)
		if err != nil || x == 0 {
			x = fallback
		}
		change.set(draft, command.Field, max(1, min(limit, math.Floor(x))))
	case "sharedRideMode", "sharedRideJoin":
		accepted, fallback := []string{"drop-offs", "destination"}, "drop-offs"
		if command.Field == "sharedRideJoin" {
			accepted, fallback = []string{"unassigned", "reassign-existing"}, "unassigned"
		}
		setting, ok := value.(string)
		if !ok {
			return projectChange{}, errors.New("a sharing policy must be text")
		}
		if !slices.Contains(accepted, setting) {
			setting = fallback
		}
		change.set(draft, command.Field, setting)
	case "platoonLimit":
		x, err := editNumber(value)
		if err != nil || !slices.Contains([]float64{0, 2, 3, 4}, x) {
			x = 0
		}
		change.set(draft, command.Field, x)
	default:
		return projectChange{}, errors.New("unknown editor field")
	}
	return change, nil
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
