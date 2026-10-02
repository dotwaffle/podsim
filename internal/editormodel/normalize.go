package editormodel

import (
	"errors"
	"maps"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func normalizeProject(draft any) (projectChange, error) {
	if object(draft) == nil {
		return projectChange{}, errors.New("normalization needs a project object")
	}
	out := maps.Clone(object(draft))
	out["version"] = float64(1)
	if hasBanks(member(out, "network")) {
		out["version"] = float64(2)
	}
	if _, ok := out["name"].(string); !ok {
		out["name"] = "Untitled scenario"
	}
	network, err := normalizeObject(out["network"])
	if err != nil {
		return projectChange{}, err
	}
	for _, key := range []string{"Nodes", "Lanes", "Stations"} {
		if _, ok := network[key].([]any); !ok {
			network[key] = []any{}
		}
	}
	out["network"] = network
	fleet := normalizeArray(out["fleet"])
	for _, pod := range fleet {
		if !editorTruthy(pod) || editorTruthy(member(pod, "BerthID")) {
			continue
		}
		if object(pod) == nil {
			return projectChange{}, errors.New("a pod must be an object before normalization")
		}
		berthID := any("")
		for _, station := range items(network["Stations"]) {
			if object(station) != nil && sameOptionalMember(station, "ID", pod, "StationID") {
				if berths := items(member(station, "Berths")); len(berths) != 0 && editorTruthy(member(berths[0], "ID")) {
					berthID = member(berths[0], "ID")
				}
				break
			}
		}
		object(pod)["BerthID"] = berthID
	}
	out["fleet"] = fleet
	if _, ok := out["demandProfiles"].([]any); !ok {
		out["demandProfiles"] = []any{}
	}
	if m := object(out["map"]); m != nil && !has(m, "opacity") {
		owned := object(cloneEditValue(m))
		owned["opacity"] = float64(0)
		out["map"] = owned
	}
	demand, err := normalizeObject(out["demand"])
	if err != nil {
		return projectChange{}, err
	}
	out["demand"] = demand
	if err := normalizeDemand(demand, out); err != nil {
		return projectChange{}, err
	}
	out["sharedRidePartyLimit"] = max(1, min(8, math.Floor(editorNumberDefault(out["sharedRidePartyLimit"], 1))))
	out["sharedRideMaxStops"] = max(1, min(7, math.Floor(editorNumberDefault(out["sharedRideMaxStops"], 3))))
	if !slices.Contains([]string{"drop-offs", "destination"}, text(out["sharedRideMode"])) {
		out["sharedRideMode"] = "drop-offs"
	}
	if !slices.Contains([]string{"unassigned", "reassign-existing"}, text(out["sharedRideJoin"])) {
		out["sharedRideJoin"] = "unassigned"
	}
	if !slices.Contains([]float64{0, 2, 3, 4}, number(out["platoonLimit"])) {
		out["platoonLimit"] = float64(0)
	}
	out["stationBuffers"] = out["stationBuffers"] == true
	out["pickupReassignment"] = out["pickupReassignment"] == true
	out["redistribution"] = editorTruthy(out["redistribution"])
	inferEditorStationLanes(network)
	change := projectChange{Patch: make(map[string]any)}
	for key, value := range out {
		if !reflect.DeepEqual(member(draft, key), value) || !has(draft, key) {
			change.Patch[key] = value
		}
	}
	return change, nil
}

func normalizeObject(value any) (map[string]any, error) {
	if !editorTruthy(value) {
		return make(map[string]any), nil
	}
	if object(value) == nil {
		return nil, errors.New("a project branch must be an object before normalization")
	}
	return object(cloneEditValue(value)), nil
}

func normalizeArray(value any) []any {
	if array, ok := value.([]any); ok {
		return items(cloneEditValue(array))
	}
	return []any{}
}

func normalizeDemand(demand, draft map[string]any) error {
	demand["enabled"] = editorTruthy(demand["enabled"])
	demand["perMinute"] = editorNumberDefault(demand["perMinute"], 2)
	if demand["pattern"] == "market" {
		demand["pattern"] = "destination"
		if !editorTruthy(demand["destination"]) {
			demand["destination"] = ""
			for _, station := range items(member(draft["network"], "Stations")) {
				if object(station) == nil || editorTruthy(member(station, "ParkingOnly")) {
					continue
				}
				demand["destination"] = member(station, "ID")
				if member(station, "ID") == "market" {
					break
				}
			}
		}
	}
	if !slices.Contains([]string{"destination", "profile", "profile-daily", "rail-arrivals", "rail-services"}, text(demand["pattern"])) {
		demand["pattern"] = "balanced"
	}
	for _, key := range []string{"destination", "profile", "band"} {
		if _, ok := demand[key].(string); !ok {
			demand[key] = ""
		}
	}
	profiles := items(draft["demandProfiles"])
	selected := slices.IndexFunc(profiles, func(profile any) bool {
		return object(profile) != nil && sameOptionalMember(profile, "id", demand, "profile")
	})
	if len(profiles) != 0 && selected < 0 {
		if object(profiles[0]) == nil {
			return errors.New("the first demand profile must be an object before normalization")
		}
		if id, found := object(profiles[0])["id"]; found {
			demand["profile"] = cloneEditValue(id)
		} else {
			delete(demand, "profile")
		}
		selected = slices.IndexFunc(profiles, func(profile any) bool {
			return object(profile) != nil && sameOptionalMember(profile, "id", demand, "profile")
		})
	}
	if selected >= 0 {
		if bands, ok := member(profiles[selected], "bands").([]any); ok && !slices.ContainsFunc(bands, func(band any) bool {
			return object(band) != nil && sameOptionalMember(band, "id", demand, "band")
		}) {
			demand["band"] = ""
			if len(bands) != 0 && editorTruthy(member(bands[0], "id")) {
				demand["band"] = cloneEditValue(member(bands[0], "id"))
			}
		}
	}
	demand["seed"] = max(0, math.Floor(editorNumberDefault(demand["seed"], 0)))
	return nil
}

func sameOptionalMember(left any, leftKey string, right any, rightKey string) bool {
	return has(left, leftKey) == has(right, rightKey) && reflect.DeepEqual(member(left, leftKey), member(right, rightKey))
}

func editorTruthy(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return value != 0 && !math.IsNaN(value)
	default:
		return true
	}
}

func editorNumberDefault(value any, fallback float64) float64 {
	x := editorNumber(value)
	if x == 0 || math.IsNaN(x) {
		return fallback
	}
	return x
}

func editorNumber(value any) float64 {
	switch value := value.(type) {
	case nil:
		return 0
	case float64:
		return value
	case bool:
		if value {
			return 1
		}
		return 0
	case string:
		value = trimEditorSpace(value)
		if value == "" {
			return 0
		}
		if len(value) > 2 && value[0] == '0' && strings.ContainsRune("xXoObB", rune(value[1])) {
			base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[value[1]]
			if integer, ok := new(big.Int).SetString(value[2:], base); ok && integer.Sign() >= 0 && !strings.ContainsAny(value[2:], "+-_ ") {
				result, _ := integer.Float64()
				return result
			}
			return math.NaN()
		}
		if strings.ContainsAny(value, "_") || slices.Contains([]string{"Inf", "+Inf", "-Inf", "NaN"}, value) {
			return math.NaN()
		}
		result, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return math.NaN()
		}
		return result
	case []any:
		if len(value) == 0 || len(value) == 1 && value[0] == nil {
			return 0
		}
		if len(value) == 1 {
			switch item := value[0].(type) {
			case float64, string, []any:
				return editorNumber(item)
			}
		}
	}
	return math.NaN()
}
