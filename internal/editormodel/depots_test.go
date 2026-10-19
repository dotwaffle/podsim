package editormodel

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
)

// queueDraft returns the small ring with a depot of two lanes of three
// slots as an editor draft.
func queueDraft(t *testing.T) map[string]any {
	t.Helper()
	config, err := scenarios.PresetWith("small", func(parameters *scenarios.Parameters) {
		parameters.ParkingLanes, parameters.ParkingBerths, parameters.InitialParkingPods = 2, 3, 4
	})
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

// queueNode returns the position of a node of a draft.
func queueNode(t *testing.T, draft map[string]any, id string) map[string]any {
	t.Helper()
	for _, node := range items(member(member(draft, "network"), "nodes")) {
		if text(member(node, "id")) == id {
			return object(member(node, "position"))
		}
	}
	t.Fatalf("no node %s", id)
	return nil
}

// TestDepotDraftChecks checks that the editor accepts the short chain
// lanes and the slot routes of a depot, refuses a chain lane shorter than
// its retention, and runs the full validation of the server on a draft
// with a depot.
func TestDepotDraftChecks(t *testing.T) {
	t.Parallel()
	if report := draftChecks(queueDraft(t)); len(report.Errors) != 0 || len(report.Warnings) != 0 {
		t.Fatalf("preset checks: %+v", report)
	}
	// Slot 2 of lane 1 moves 0.6 m toward slot 3, so the chain lane from
	// slot 3 is 5.4 m long.
	short := queueDraft(t)
	slot2, slot1 := queueNode(t, short, "s05-slot-1-02"), queueNode(t, short, "s05-slot-1-01")
	for _, axis := range []string{"x", "y"} {
		slot2[axis] = number(slot2[axis]) - 0.1*(number(slot1[axis])-number(slot2[axis]))
	}
	if report := draftChecks(short); len(report.Errors) != 1 || !strings.HasPrefix(report.Errors[0].Text, "Lane s05-chain-1-02 is shorter than 5.49") {
		t.Fatalf("short chain lane: %+v", report.Errors)
	}
	// The end of the lead-out of lane 1 off the line of the lane passes the
	// editor checks, and only the depot rules of the server refuse it.
	bent := queueDraft(t)
	end := queueNode(t, bent, "s05-clear-1")
	end["x"] = number(end["x"]) - 0.5
	report := draftChecks(bent)
	if len(report.Errors) != 1 || report.Errors[0].Text != `validate depots: depot station "parking" lane 0 is not straight and ordered` {
		t.Fatalf("bent lane: %+v", report.Errors)
	}
	// A lead-in at 2 m/s passes the editor checks, and the bank audit of the
	// server refuses it.
	fast := queueDraft(t)
	for _, lane := range items(member(member(fast, "network"), "lanes")) {
		if text(member(lane, "id")) == "s05-in-1" {
			object(lane)["speedLimit"] = 2.0
		}
	}
	report = draftChecks(fast)
	if len(report.Errors) != 1 || !strings.HasPrefix(report.Errors[0].Text, "validate depots: nonincident paths") || !strings.HasSuffix(report.Errors[0].Text, "are less than 7 meters apart") {
		t.Fatalf("fast lead-in: %+v", report.Errors)
	}
}

// TestDepotGeometryEdits checks that a depot station refuses the actions
// that change its geometry or its berths, that a node edit of a depot runs
// the full validation, and that another edit keeps the depots.
func TestDepotGeometryEdits(t *testing.T) {
	t.Parallel()
	draft := queueDraft(t)
	edit := func(command map[string]any) (projectChange, error) {
		raw, err := jsonv2.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		return editGeometry(draft, jsontext.Value(raw))
	}
	const fixed = "station parking is a depot, so its geometry and berths are fixed"
	for _, command := range []map[string]any{
		{"action": "moveStation", "id": "parking", "delta": map[string]any{"x": 1, "y": 0}},
		{"action": "stationBearing", "id": "parking", "value": 90},
		{"action": "stationParking", "id": "parking", "value": false},
		{"action": "deleteStation", "id": "parking"},
		{"action": "addBerth", "id": "parking"},
		{"action": "removeBerth", "id": "parking", "value": "parking-1-03"},
		{"action": "stationLayout", "id": "parking", "value": map[string]any{}},
		{"action": "stationBanks", "id": "parking", "value": 2},
		{"action": "stationLegacy", "id": "parking"},
		{"action": "bankLayout", "id": "parking", "value": map[string]any{}},
		{"action": "addBankBerth", "id": "parking", "value": "bank-a"},
	} {
		if _, err := edit(command); err == nil || err.Error() != fixed {
			t.Errorf("%s: %v", command["action"], err)
		}
	}
	if _, err := edit(map[string]any{"action": "stationName", "id": "parking", "value": "Depot"}); err != nil {
		t.Errorf("rename: %v", err)
	}
	tail := queueNode(t, draft, "s05-tail-1")
	if _, err := edit(map[string]any{"action": "moveNode", "id": "s05-tail-1", "point": map[string]any{"x": number(tail["x"]) + 0.5, "y": tail["y"]}}); err == nil || !strings.HasPrefix(err.Error(), "validate depots: ") {
		t.Errorf("bent lane: %v", err)
	}
	// Slot 2 of lane 1 0.6 m nearer to slot 3 gets the length check of the
	// editor before the server validation.
	slot2, slot1 := queueNode(t, draft, "s05-slot-1-02"), queueNode(t, draft, "s05-slot-1-01")
	x, y := number(slot2["x"])-0.1*(number(slot1["x"])-number(slot2["x"])), number(slot2["y"])-0.1*(number(slot1["y"])-number(slot2["y"]))
	if _, err := edit(map[string]any{"action": "moveNode", "id": "s05-slot-1-02", "point": map[string]any{"x": x, "y": y}}); err == nil || !strings.HasPrefix(err.Error(), "lane s05-chain-1-02 would be shorter than 5.49") {
		t.Errorf("short chain lane: %v", err)
	}
	berth := queueNode(t, draft, "s01-berth-01")
	change, err := edit(map[string]any{"action": "moveNode", "id": "s01-berth-01", "point": map[string]any{"x": number(berth["x"]) + 1, "y": berth["y"]}})
	if err != nil {
		t.Fatal(err)
	}
	if got := member(change.Patch["network"], "depots"); !reflect.DeepEqual(got, member(member(draft, "network"), "depots")) {
		t.Fatalf("depots after an edit: %v", got)
	}
}

// TestDepotNormalize checks that normalization keeps the depots and gives
// no station role to a depot lane.
func TestDepotNormalize(t *testing.T) {
	t.Parallel()
	draft := queueDraft(t)
	change, err := normalizeProject(draft)
	if err != nil {
		t.Fatal(err)
	}
	if network, changed := change.Patch["network"]; changed && !reflect.DeepEqual(network, draft["network"]) {
		for _, lane := range items(member(network, "lanes")) {
			if strings.HasPrefix(text(member(lane, "id")), "s05-") && text(member(lane, "id")) != "s05-through" && has(lane, "stationRole") {
				t.Errorf("depot lane %v", lane)
			}
		}
		if !reflect.DeepEqual(member(network, "depots"), member(draft["network"], "depots")) {
			t.Errorf("depots %v", member(network, "depots"))
		}
	}
}
