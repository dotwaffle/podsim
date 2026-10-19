package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// measurementCoordinate is the absolute coordinate limit of a project.
	measurementCoordinate = 100000
	// measurementItems is the number of measurements that the map shows.
	measurementItems = 200
	// measurementMaxScale is MAX_ZOOM of the editor map.
	measurementMaxScale = 5
	measurementScope    = "Draft geometry in meters; not surveyed distances or safety clearances."
)

type measurementPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// measurementItem is one ruler of the map. Label is where the map draws
// Text: the middle of the chord, or the point at t = 0.5 of a quadratic
// curve, above the line by 8 screen pixels.
type measurementItem struct {
	Kind    string            `json:"kind"`
	Name    string            `json:"label"`
	From    measurementPoint  `json:"from"`
	To      measurementPoint  `json:"to"`
	Control *measurementPoint `json:"control,omitzero"`
	Meters  float64           `json:"meters"`
	Method  string            `json:"method"`
	Text    string            `json:"text"`
	Label   measurementPoint  `json:"labelAt"`
}

// measurementReport is the ruler overlay of one selection. Items holds at
// most measurementItems rulers. Summary is the text of all measurements.
type measurementReport struct {
	Items    []measurementItem `json:"items"`
	FontSize float64           `json:"fontSize"`
	Radius   float64           `json:"radius"`
	Summary  string            `json:"summary"`
}

// draftMeasurements holds all measurements of a selection before the map
// limit.
type draftMeasurements struct {
	items       []measurementItem
	unavailable []string
}

// measurePosition gives value as a point when it is an object with finite
// x and y within the coordinate limit.
func measurePosition(value any) (measurementPoint, bool) {
	point := object(value)
	x, xOK := point["x"].(float64)
	y, yOK := point["y"].(float64)
	if point == nil || !xOK || !yOK || math.Abs(x) > measurementCoordinate || math.Abs(y) > measurementCoordinate {
		return measurementPoint{}, false
	}
	return measurementPoint{X: x, Y: y}, true
}

// indexItems gives the items of a draft list by ID, in list order. It
// gives false when an item is not an object or has an invalid or repeated
// ID.
func indexItems(value any) (map[string]map[string]any, []map[string]any, bool) {
	list, ok := reportList(value)
	if !ok {
		return nil, nil, false
	}
	index := make(map[string]map[string]any, len(list))
	ordered := make([]map[string]any, 0, len(list))
	for _, value := range list {
		item, isObject := value.(map[string]any)
		id, idOK := reportID(item["id"])
		if _, repeated := index[id]; !isObject || !idOK || repeated {
			return nil, nil, false
		}
		index[id] = item
		ordered = append(ordered, item)
	}
	return index, ordered, true
}

// measureDraft measures the selected lane, or the lanes and consecutive
// berth centers of the selected station. A station has the lanes with its
// stationID. A berth spacing is the straight chord between two berth nodes
// in the order of the berths.
func measureDraft(draft, selection any) draftMeasurements {
	var result draftMeasurements
	network := object(member(draft, "network"))
	nodes, _, nodesOK := indexItems(network["nodes"])
	lanes, laneList, lanesOK := indexItems(network["lanes"])
	stations, _, stationsOK := indexItems(network["stations"])
	if !nodesOK || !lanesOK || !stationsOK {
		result.unavailable = append(result.unavailable, "Draft geometry unavailable.")
		return result
	}
	at := func(id any) (measurementPoint, bool) {
		key, ok := id.(string)
		if !ok || nodes[key] == nil {
			return measurementPoint{}, false
		}
		return measurePosition(nodes[key]["position"])
	}
	measureLane := func(lane map[string]any) {
		from, fromOK := at(lane["from"])
		to, toOK := at(lane["to"])
		control, curved := lane["control"]
		curved = curved && control != nil
		point, controlOK := measurePosition(control)
		if !fromOK || !toOK || curved && !controlOK {
			result.unavailable = append(result.unavailable, fmt.Sprintf("Lane %s: invalid or missing geometry.", text(lane["id"])))
			return
		}
		item := measurementItem{Kind: "lane", Name: "Lane " + text(lane["id"]), From: from, To: to, Method: "straight centerline"}
		if curved {
			item.Control, item.Method = &point, "curve approximation, 16 segments"
		}
		if !curved {
			control = nil
		}
		item.Meters = draftLaneLength(nodes[text(lane["from"])]["position"], nodes[text(lane["to"])]["position"], control)
		result.items = append(result.items, item)
	}
	kind, id := text(member(selection, "type")), member(selection, "id")
	key, _ := id.(string)
	switch {
	case kind == "lane" && lanes[key] != nil:
		measureLane(lanes[key])
	case kind == "station" && stations[key] != nil:
		station := stations[key]
		for _, lane := range laneList {
			if lane["stationID"] == station["id"] {
				measureLane(lane)
			}
		}
		berths, ok := reportList(station["berths"])
		if !ok {
			result.unavailable = append(result.unavailable, "Berth center spacing unavailable.")
			return result
		}
		for i := 1; i < len(berths); i++ {
			a, b := object(berths[i-1]), object(berths[i])
			from, fromOK := at(a["node"])
			to, toOK := at(b["node"])
			aID, aOK := reportID(a["id"])
			bID, bOK := reportID(b["id"])
			if !aOK || !bOK || !fromOK || !toOK {
				result.unavailable = append(result.unavailable, fmt.Sprintf("Berth pair %d/%d: center spacing unavailable.", i, i+1))
				continue
			}
			result.items = append(result.items, measurementItem{Kind: "spacing", Name: "Berths " + aID + " / " + bID, From: from, To: to,
				Meters: math.Hypot(to.X-from.X, to.Y-from.Y), Method: "straight center chord, authored berth order"})
		}
	default:
		result.unavailable = append(result.unavailable, "Select a station or guideway to measure.")
	}
	return result
}

// fixed2 formats a nonnegative meters value with two decimals, as the
// browser does. A value halfway between two results rounds up, not to the
// even result.
func fixed2(x float64) string {
	if scaled := x * 8; scaled == math.Trunc(scaled) && math.Mod(scaled, 2) == 1 {
		return strconv.FormatFloat(math.Ceil(x*100)/100, 'f', 2, 64)
	}
	return strconv.FormatFloat(x, 'f', 2, 64)
}

// layoutMeasurements sets the map geometry of the measurements at a map
// scale. An invalid scale, or one above the editor zoom limit, is 1. The
// scale can be below the zoom limit, because a large network fits below
// it.
func layoutMeasurements(measured draftMeasurements, scale any) measurementReport {
	safe, ok := scale.(float64)
	if !ok || safe < 1e-6 || safe > measurementMaxScale {
		safe = 1
	}
	shown := measured.items[:min(len(measured.items), measurementItems)]
	report := measurementReport{Items: make([]measurementItem, 0, len(shown)), FontSize: 11 / safe, Radius: 3 / safe}
	descriptions := []string{measurementScope}
	for _, item := range shown {
		from, to := item.From, item.To
		if control := item.Control; control != nil {
			item.Label = measurementPoint{X: (from.X + 2*control.X + to.X) / 4, Y: (from.Y+2*control.Y+to.Y)/4 - 8/safe}
		} else {
			item.Label = measurementPoint{X: (from.X + to.X) / 2, Y: (from.Y+to.Y)/2 - 8/safe}
		}
		item.Text = item.Name + ": " + fixed2(item.Meters) + " m (" + item.Method + ")"
		descriptions = append(descriptions, item.Text)
		report.Items = append(report.Items, item)
	}
	if len(measured.items) > len(shown) {
		descriptions = append(descriptions, fmt.Sprintf("Only the first %d of %d measurements are shown.", len(shown), len(measured.items)))
	}
	report.Summary = strings.Join(append(descriptions, measured.unavailable...), " ")
	return report
}

// inspectMeasurements gives the ruler overlay of a draft for a selection
// and map scale.
func inspectMeasurements(draft any, raw jsontext.Value) (measurementReport, error) {
	var measure struct {
		Selection any `json:"selection"`
		Scale     any `json:"scale"`
	}
	if raw.Kind() != '{' {
		return measurementReport{}, errors.New("measurements need a selection object")
	}
	if err := json.Unmarshal(raw, &measure, json.RejectUnknownMembers(true)); err != nil {
		return measurementReport{}, fmt.Errorf("decode measurement selection: %w", err)
	}
	return layoutMeasurements(measureDraft(draft, measure.Selection), measure.Scale), nil
}
