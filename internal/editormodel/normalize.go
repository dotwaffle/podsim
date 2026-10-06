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

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func normalizeProject(draft any) (projectChange, error) {
	if object(draft) == nil {
		return projectChange{}, errors.New("normalization needs a project object")
	}
	out := maps.Clone(object(draft))
	if problem := draftContractError(draft); problem != "" {
		return projectChange{}, errors.New(problem)
	}
	if problem := onboardSettingError(draft); problem != "" {
		return projectChange{}, errors.New(problem)
	}
	// The earlier versions 2 to 5 do not migrate. Another value becomes the
	// current version.
	if _, found := earlierVersion(out); found {
		return projectChange{}, errors.New(draftVersionError(out))
	}
	out["version"] = float64(project.CurrentVersion)
	if _, ok := out["name"].(string); !ok {
		out["name"] = "Untitled scenario"
	}
	network, err := normalizeObject(out["network"])
	if err != nil {
		return projectChange{}, err
	}
	for _, key := range []string{"nodes", "lanes", "stations"} {
		if _, ok := network[key].([]any); !ok {
			network[key] = []any{}
		}
	}
	out["network"] = network
	fleet := normalizeArray(out["fleet"])
	for _, pod := range fleet {
		if editorTruthy(pod) && object(pod) == nil {
			return projectChange{}, errors.New("a pod must be an object before normalization")
		}
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
	out["sharedRidePartyLimit"] = max(1, min(sim.MaxSharedRideParties, math.Floor(editorNumberDefault(out["sharedRidePartyLimit"], 1))))
	out["sharedRideMaxStops"] = max(1, min(sim.MaxSharedRideStops, math.Floor(editorNumberDefault(out["sharedRideMaxStops"], 3))))
	if !slices.Contains([]string{"drop-offs", "destination"}, text(out["sharedRideMode"])) {
		out["sharedRideMode"] = "drop-offs"
	}
	if !slices.Contains([]string{"unassigned", "reassign-existing"}, text(out["sharedRideJoin"])) {
		out["sharedRideJoin"] = "unassigned"
	}
	if !draftPlatoonLimit(out["platoonLimit"]) {
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
			for _, station := range items(member(draft["network"], "stations")) {
				if object(station) == nil || editorTruthy(member(station, "parkingOnly")) {
					continue
				}
				demand["destination"] = member(station, "id")
				if member(station, "id") == "market" {
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
		return object(profile) != nil && sameOptionalID(profile, demand, "profile")
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
			return object(profile) != nil && sameOptionalID(profile, demand, "profile")
		})
	}
	if selected >= 0 {
		if bands, ok := member(profiles[selected], "bands").([]any); ok && !slices.ContainsFunc(bands, func(band any) bool {
			return object(band) != nil && sameOptionalID(band, demand, "band")
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

// sameOptionalID reports whether the id of left and the key member of right
// are both absent, or both present with the same value.
func sameOptionalID(left, right any, key string) bool {
	return has(left, "id") == has(right, key) && reflect.DeepEqual(member(left, "id"), member(right, key))
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
