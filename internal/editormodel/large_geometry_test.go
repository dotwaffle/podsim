package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestLargeDraftBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		classes any
		want    float64
	}{
		{"omitted", nil, 24}, {"compact", []any{"compact"}, 24},
		{"group", []any{"group"}, 40}, {"mixed", []any{"legacy", "group"}, 40},
		{"express geometry", []any{"express"}, 40},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lane := map[string]any{}
			if test.classes != nil {
				lane["vehicleClasses"] = test.classes
			}
			if got := draftLaneMinimum(lane); got != test.want {
				t.Fatalf("minimum = %g, want %g", got, test.want)
			}
			if got := draftPairClearance(lane, map[string]any{}); got != test.want/2 {
				t.Fatalf("pair bound = %g", got)
			}
		})
	}
}

func largeDraftPaths(gap float64, large bool) map[string]any {
	network := map[string]any{"nodes": []any{}, "lanes": []any{}, "stations": []any{}}
	for i, id := range []string{"a", "b", "c", "d"} {
		network["nodes"] = append(items(network["nodes"]), map[string]any{"id": id, "position": map[string]any{"x": float64(i%2) * 100, "y": float64(i/2) * gap}})
	}
	for i, id := range []string{"first", "second"} {
		lane := map[string]any{"id": id, "from": []string{"a", "c"}[i], "to": []string{"b", "d"}[i], "speedLimit": float64(14)}
		if large && i == 0 {
			lane["vehicleClasses"] = []any{"group"}
		}
		network["lanes"] = append(items(network["lanes"]), lane)
	}
	return network
}

func TestLargeDraftPairClearance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		gap             float64
		large, conflict bool
	}{
		{"ordinary twelve", 12, false, false}, {"ordinary eleven", 11, false, true},
		{"mixed nineteen", 19, true, true}, {"mixed twenty", 20, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			geometry := geometryDraft{network: largeDraftPaths(test.gap, test.large)}
			conflict := geometry.laneConflict([]string{"second"}, true)
			if (conflict != nil) != test.conflict {
				t.Fatalf("conflict = %+v", conflict)
			}
			if conflict != nil && conflict.minimum != draftPairClearance(items(geometry.network["lanes"])[0], items(geometry.network["lanes"])[1]) {
				t.Fatal("wrong reported minimum")
			}
		})
	}
}

func TestLargeDraftNoBankAudit(t *testing.T) {
	t.Parallel()
	for _, gap := range []float64{19, 20} {
		network := largeDraftPaths(gap, true)
		var report checkList
		checkNetwork(network, &report)
		if (len(report.items) != 0) != (gap < 20) {
			t.Fatalf("gap %g checks: %+v", gap, report.items)
		}
		draft := map[string]any{"network": network}
		before := cloneEditValue(draft)
		change, err := editGeometry(draft, jsontext.Value(`{"action":"laneSpeed","id":"second","value":15}`))
		if (err != nil) != (gap < 20) {
			t.Fatalf("gap %g edit error: %v", gap, err)
		}
		if err != nil && len(change.Patch) != 0 {
			t.Fatal("rejected audit published a patch")
		}
		if !reflect.DeepEqual(draft, before) {
			t.Fatal("audit changed its source")
		}
	}
}

func TestLargeDraftMinimumEditsAtomic(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":30}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","spacing":60}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			draft := bankEditorFixture(2)
			draft["version"] = float64(3)
			network := object(draft["network"])
			for _, lane := range items(network["lanes"]) {
				object(lane)["vehicleClasses"] = []any{"legacy", "group"}
			}
			before := cloneEditValue(draft)
			change, err := editGeometry(draft, jsontext.Value(raw))
			if err == nil || !strings.Contains(err.Error(), "40") {
				t.Fatalf("short large lane accepted: %v", err)
			}
			if len(change.Patch) != 0 || !reflect.DeepEqual(draft, before) {
				t.Fatal("rejected edit changed its source or published a patch")
			}
		})
	}
}

func TestLargeDraftMaskCacheInvalidation(t *testing.T) {
	t.Parallel()
	config := project.Default()
	model := new(engine)
	keys := synchronize(t, model, config)
	for _, large := range []bool{true, false, true} {
		network := config.Network
		network.Lanes = append([]sim.Lane(nil), config.Network.Lanes...)
		if large {
			classes, err := sim.NewClassSet("legacy", "group")
			if err != nil {
				t.Fatal(err)
			}
			network.Lanes[0].VehicleClasses = classes
			for i := range network.Nodes {
				if network.Nodes[i].ID == network.Lanes[0].To {
					network.Nodes = append([]sim.Node(nil), network.Nodes...)
					for j := range network.Nodes {
						if network.Nodes[j].ID == network.Lanes[0].From {
							network.Nodes[i].Position = sim.Point{X: network.Nodes[j].Position.X + 30, Y: network.Nodes[j].Position.Y}
							break
						}
					}
					break
				}
			}
		}
		raw, err := json.Marshal(map[string]any{"network": network})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.sync(request{Keys: keys, Patch: raw}); err != nil {
			t.Fatal(err)
		}
		compareOnboardChecks(t, model, large)
	}
}

func TestLargeDraftBerthAudit(t *testing.T) {
	t.Parallel()
	for _, gap := range []float64{19, 40} {
		network := largeDraftPaths(100, false)
		network["nodes"] = append(items(network["nodes"]), map[string]any{"id": "berth-a", "position": map[string]any{"x": float64(50), "y": float64(200)}}, map[string]any{"id": "berth-b", "position": map[string]any{"x": float64(50) + gap, "y": float64(200)}})
		network["stations"] = []any{map[string]any{"id": "station", "entry": "a", "exit": "b", "vehicleClasses": []any{"legacy", "group"}, "berths": []any{map[string]any{"id": "first-berth", "node": "berth-a", "vehicleClasses": []any{"group"}}, map[string]any{"id": "second-berth", "node": "berth-b"}}}}
		for _, berth := range []string{"berth-a", "berth-b"} {
			network["lanes"] = append(items(network["lanes"]), map[string]any{"id": berth + "-in", "from": "a", "to": berth, "speedLimit": float64(14)}, map[string]any{"id": berth + "-out", "from": berth, "to": "b", "speedLimit": float64(14)})
		}
		if !hasLargeGeometry(network) {
			t.Fatal("effective group berth admission did not request audit")
		}
		if err := validateBankDraft(network); (err != nil) != (gap < 20) {
			t.Fatalf("berth gap %g: %v", gap, err)
		}
		draft := map[string]any{"network": network}
		before := cloneEditValue(draft)
		change, err := editGeometry(draft, jsontext.Value(`{"action":"laneSpeed","id":"second","value":15}`))
		if (err != nil) != (gap < 20) {
			t.Fatalf("berth edit gap %g: %v", gap, err)
		}
		if err != nil && len(change.Patch) != 0 || !reflect.DeepEqual(before, draft) {
			t.Fatal("berth audit changed its source or published rejected patch")
		}
	}
}

func TestLargeDraftMaskHistoryChecks(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../project/testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if decodeErr := json.Unmarshal(raw, &config); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	config.Fleet[0].Class = sim.LegacyClass
	config.Network.Nodes = append(config.Network.Nodes, sim.Node{ID: "a", Position: sim.Point{X: 2000, Y: 2000}}, sim.Node{ID: "b", Position: sim.Point{X: 2100, Y: 2000}}, sim.Node{ID: "c", Position: sim.Point{X: 2000, Y: 2019}}, sim.Node{ID: "d", Position: sim.Point{X: 2100, Y: 2019}})
	config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: "first", From: "a", To: "b", SpeedLimit: 14}, sim.Lane{ID: "second", From: "c", To: "d", SpeedLimit: 14})
	model := new(engine)
	keys := synchronize(t, model, config)
	compareOnboardChecks(t, model, false)
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: jsontext.Value(`null`)})
	classes, err := sim.NewClassSet("legacy", "group")
	if err != nil {
		t.Fatal(err)
	}
	config.Network.Lanes[len(config.Network.Lanes)-2].VehicleClasses = classes
	patch, err := json.Marshal(map[string]any{"network": config.Network})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	compareOnboardChecks(t, model, true)
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: jsontext.Value(`null`)})
	for _, test := range []struct {
		kind    string
		invalid bool
	}{{"undo", false}, {"redo", true}} {
		acceptedHistory(t, model, historyCommand{Kind: test.kind})
		if model.checks != nil && model.checks.network != nil {
			t.Fatal("history retained prepared network checks")
		}
		compareOnboardChecks(t, model, test.invalid)
	}
}

func TestLargeDraftCurveAdmissionAtomic(t *testing.T) {
	t.Parallel()
	for _, large := range []bool{false, true} {
		network := largeDraftPaths(400, large)
		draft := map[string]any{"network": network}
		before := cloneEditValue(draft)
		change, err := editGeometry(draft, jsontext.Value(`{"action":"moveControl","id":"first","point":{"x":200,"y":0}}`))
		if (err != nil) != large {
			t.Fatalf("large=%v path-shape error: %v", large, err)
		}
		if err != nil && len(change.Patch) != 0 || !reflect.DeepEqual(draft, before) {
			t.Fatal("curve edit changed its source or published rejected patch")
		}
	}
}

func TestLargeDraftBankChainMaskPreservation(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(2)
	draft["version"] = float64(3)
	network := object(draft["network"])
	mask := []any{"legacy", "group"}
	for _, lane := range items(network["lanes"]) {
		object(lane)["vehicleClasses"] = cloneEditValue(mask)
	}
	station := object(items(network["stations"])[0])
	station["vehicleClasses"] = cloneEditValue(mask)
	for _, berth := range items(station["berths"]) {
		object(berth)["vehicleClasses"] = cloneEditValue(mask)
	}
	grown := applyBankEdit(t, draft, `{"action":"addBankBerth","id":"station","value":"b"}`)
	for _, lane := range items(member(grown["network"], "lanes")) {
		if !reflect.DeepEqual(member(lane, "vehicleClasses"), mask) {
			t.Fatal("chain edit lost lane mask", lane)
		}
	}
	for _, berth := range items(member(items(member(grown["network"], "stations"))[0], "berths")) {
		if !reflect.DeepEqual(member(berth, "vehicleClasses"), mask) {
			t.Fatal("chain edit lost berth mask", berth)
		}
	}
}
