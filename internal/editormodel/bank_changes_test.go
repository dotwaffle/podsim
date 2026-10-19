package editormodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// londonCompactStations are the Compact stations of the London drafts.
// LondonCentral gets the first seven.
var londonCompactStations = []string{
	"940GZZLURGP", "940GZZLULBN", "940GZZLULGT", "940GZZLUERB", "940GZZLUBWT", "940GZZLUHPC", "940GZZLUBOR", "940GZZDLTWG",
}

// londonDraft returns a London preset with two depot lanes of six slots
// at each Parking facility and the Compact stations, as an editor draft.
func londonDraft(t *testing.T, full bool) map[string]any {
	t.Helper()
	options, build, compact := scenarios.DefaultLondonCentralOptions(), scenarios.LondonCentralWith, londonCompactStations[:7]
	if full {
		options, build, compact = scenarios.DefaultLondonFullOptions(), scenarios.LondonFullWith, londonCompactStations
	}
	options.ParkingLanes, options.ParkingBerths, options.CompactStations = 2, 6, compact
	config, err := build(options)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := jsonv2.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	return draft
}

// equivalenceDraft is a draft of the equivalence tests. A large draft
// gets only the cheap cases.
type equivalenceDraft struct {
	name  string
	draft map[string]any
	large bool
}

// equivalenceDrafts returns the drafts of the equivalence tests: the bank
// example, the depot ring, the compact station, and, without -short, the
// two London drafts.
func equivalenceDrafts(t *testing.T) []equivalenceDraft {
	t.Helper()
	drafts := []equivalenceDraft{
		{name: "banks", draft: configDraft(t, bankEditorConfig())}, {name: "depot", draft: queueDraft(t)}, {name: "compact", draft: compactDraft(t)},
	}
	if !testing.Short() {
		drafts = append(drafts, equivalenceDraft{name: "london-central", draft: londonDraft(t, false)}, equivalenceDraft{name: "london-full", draft: londonDraft(t, true), large: true})
	}
	return drafts
}

// referenceBankChanges is validateBankChanges before the geometry index:
// it finds each lane and each lane end with a scan of the network.
func referenceBankChanges(g geometryDraft, original map[string]any) error {
	old := geometryDraft{network: original}
	oldCounts, _ := old.nodeLaneCounts()
	counts, order := g.nodeLaneCounts()
	for _, id := range order {
		if counts[id] > project.MaxNodeLanes && counts[id] > oldCounts[id] {
			return fmt.Errorf("node %s would exceed the %d-lane limit", id, project.MaxNodeLanes)
		}
	}
	for _, key := range []string{"nodes", "lanes", "stations"} {
		for _, item := range items(g.network[key]) {
			if !validID(member(item, "id")) {
				return fmt.Errorf("a generated item ID must contain 1 to %d bytes", project.MaxIDLength)
			}
			if key == "nodes" {
				position := member(item, "position")
				if !finiteRange(number(member(position, "x")), -project.MaxCoordinate, project.MaxCoordinate) || !finiteRange(number(member(position, "y")), -project.MaxCoordinate, project.MaxCoordinate) {
					return errors.New("the geometry edit exceeds the coordinate limit")
				}
			}
		}
	}
	for _, station := range items(g.network["stations"]) {
		for _, berth := range items(member(station, "berths")) {
			if !validID(member(berth, "id")) {
				return fmt.Errorf("a berth ID must contain 1 to %d bytes", project.MaxIDLength)
			}
		}
	}
	if err := validateDraftDepots(g.network); err != nil {
		return err
	}
	clearance, oldClearance := newDraftClearance(g.network), newDraftClearance(original)
	for _, lane := range items(g.network["lanes"]) {
		before, _ := old.find("lanes", text(member(lane, "id")))
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		a, err := g.point(from)
		if err != nil {
			return err
		}
		b, err := g.point(to)
		if err != nil {
			return err
		}
		oldA, _ := old.point(from)
		oldB, _ := old.point(to)
		minimum := clearance.laneMinimum(lane)
		if before != nil && reflect.DeepEqual(before, lane) && a == oldA && b == oldB && minimum <= oldClearance.laneMinimum(before) {
			continue
		}
		var control *geometryPoint
		if value := member(lane, "control"); value != nil {
			if !finite(member(value, "x")) || !finite(member(value, "y")) {
				return errors.New("the lane needs a finite control point")
			}
			control = &geometryPoint{number(member(value, "x")), number(member(value, "y"))}
		}
		points := geometryPolyline(a, b, control)
		length := 0.0
		for index := 1; index < len(points); index++ {
			length += math.Hypot(points[index].X-points[index-1].X, points[index].Y-points[index-1].Y)
		}
		if length < minimum {
			return fmt.Errorf("lane %s would be shorter than %g meters", text(member(lane, "id")), minimum)
		}
	}
	return nil
}

// errorText returns the text of an error, or "" for nil.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// networkMutation changes a copy of a draft network. lane is a lane of
// the network.
type networkMutation struct {
	name   string
	mutate func(network map[string]any, lane map[string]any)
}

// nodeOf returns the first node of a network with an ID.
func nodeOf(network map[string]any, id string) map[string]any {
	for _, node := range items(network["nodes"]) {
		if text(member(node, "id")) == id {
			return object(node)
		}
	}
	return nil
}

// networkMutations are the changes of the equivalence tests. Each one
// reaches a different result or branch of the lane checks.
var networkMutations = []networkMutation{
	{"none", func(map[string]any, map[string]any) {}},
	{"small move", func(network, lane map[string]any) {
		position := object(nodeOf(network, text(lane["to"]))["position"])
		position["x"] = number(position["x"]) + 0.5
	}},
	{"short lane", func(network, lane map[string]any) {
		from := object(nodeOf(network, text(lane["from"]))["position"])
		to := object(nodeOf(network, text(lane["to"]))["position"])
		to["x"], to["y"] = number(from["x"])+0.25, number(from["y"])
	}},
	{"empty end", func(_, lane map[string]any) { lane["from"] = "" }},
	{"missing end", func(_, lane map[string]any) { lane["to"] = "nowhere" }},
	{"later duplicate", func(network, lane map[string]any) {
		from := object(nodeOf(network, text(lane["from"]))["position"])
		network["nodes"] = append(items(network["nodes"]), map[string]any{"id": lane["to"], "position": map[string]any{"x": from["x"], "y": from["y"]}})
	}},
	{"earlier duplicate", func(network, lane map[string]any) {
		from := object(nodeOf(network, text(lane["from"]))["position"])
		network["nodes"] = append([]any{map[string]any{"id": lane["to"], "position": map[string]any{"x": from["x"], "y": from["y"]}}}, items(network["nodes"])...)
	}},
	{"new lane", func(network, lane map[string]any) {
		network["lanes"] = append(items(network["lanes"]), map[string]any{"id": "new-lane", "from": lane["to"], "to": lane["from"], "speedLimit": lane["speedLimit"]})
	}},
	{"renamed lane", func(_, lane map[string]any) { lane["id"] = "renamed-lane" }},
	{"faster lane", func(_, lane map[string]any) { lane["speedLimit"] = number(lane["speedLimit"]) * 4 }},
	{"bad control", func(_, lane map[string]any) { lane["control"] = map[string]any{"x": "bad", "y": 0.0} }},
}

// mutationLanes returns the lanes that the mutations change: the first
// lane, the first lane of a maneuver group and the first chain lane of a
// depot.
func mutationLanes(network map[string]any) []string {
	lanes := []string{text(member(items(network["lanes"])[0], "id"))}
	if groups := items(network["maneuverGroups"]); len(groups) != 0 {
		lanes = append(lanes, text(items(member(groups[0], "laneIDs"))[0]))
	}
	depots := items(network["depots"])
	if len(depots) == 0 {
		return lanes
	}
	nodes := make(map[string]string)
	for _, station := range items(network["stations"]) {
		for _, berth := range items(member(station, "berths")) {
			nodes[text(member(berth, "id"))] = text(member(berth, "node"))
		}
	}
	slots := items(member(items(member(depots[0], "lanes"))[0], "berthIDs"))
	from, to := nodes[text(slots[1])], nodes[text(slots[0])]
	for _, lane := range items(network["lanes"]) {
		if member(lane, "from") == from && member(lane, "to") == to {
			return append(lanes, text(member(lane, "id")))
		}
	}
	return lanes
}

// TestBankChangesMatchScan checks that the lane checks of a geometry edit
// with the geometry index give the error of the scan of each item, on the
// drafts of the equivalence tests.
func TestBankChangesMatchScan(t *testing.T) {
	t.Parallel()
	for _, fixture := range equivalenceDrafts(t) {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			original := object(member(fixture.draft, "network"))
			lanes, mutations := mutationLanes(original), networkMutations
			if fixture.large {
				lanes, mutations = lanes[:1], mutations[:2]
			}
			for _, laneID := range lanes {
				for _, mutation := range mutations {
					network := object(cloneEditValue(original))
					lane, err := geometryDraft{network: network}.find("lanes", laneID)
					if err != nil {
						t.Fatal(err)
					}
					mutation.mutate(network, lane)
					g := geometryDraft{network: network}
					got, want := errorText(g.validateBankChanges(original)), errorText(referenceBankChanges(g, original))
					if got != want {
						t.Errorf("lane %s %s: got %q, want %q", laneID, mutation.name, got, want)
					}
				}
			}
		})
	}
}
