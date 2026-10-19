package editormodel

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
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

// queueStation returns the depot station of a draft network.
func queueStation(t *testing.T, network any) map[string]any {
	t.Helper()
	for _, station := range items(member(network, "stations")) {
		if text(member(station, "id")) == "parking" {
			return object(station)
		}
	}
	t.Fatal("no depot station")
	return nil
}

// queueLane returns a lane of a draft network.
func queueLane(t *testing.T, network any, id string) map[string]any {
	t.Helper()
	for _, lane := range items(member(network, "lanes")) {
		if text(member(lane, "id")) == id {
			return object(lane)
		}
	}
	t.Fatalf("no lane %s", id)
	return nil
}

// invalidQueueDrafts returns depot drafts that the depot validation of the
// server refuses, by name.
func invalidQueueDrafts(t *testing.T) map[string]map[string]any {
	t.Helper()
	drafts := make(map[string]map[string]any)
	add := func(name string, change func(network map[string]any)) {
		draft := queueDraft(t)
		change(object(draft["network"]))
		drafts[name] = draft
	}
	add("empty banks", func(network map[string]any) { queueStation(t, network)["banks"] = []any{} })
	add("malformed banks", func(network map[string]any) {
		queueStation(t, network)["banks"] = []any{map[string]any{"id": "a"}}
	})
	add("berth separation group", func(network map[string]any) {
		object(items(queueStation(t, network)["berths"])[0])["separationGroup"] = "upper"
	})
	add("lane separation group", func(network map[string]any) { queueLane(t, network, "s05-in-1")["separationGroup"] = "upper" })
	add("partial depot", func(network map[string]any) {
		lane := object(items(member(items(network["depots"])[0], "lanes"))[0])
		lane["berthIDs"] = items(lane["berthIDs"])[:2]
	})
	add("moved entry", func(network map[string]any) { queueStation(t, network)["entry"] = "s05-lead-1" })
	add("not parking", func(network map[string]any) { queueStation(t, network)["parkingOnly"] = false })
	return drafts
}

// TestDraftClearanceDepotFields checks that the clearance index of a draft
// copies each member that the depot validation and the lateral index of
// the server read: the projection equals the server decode except for the
// members that neither reads. Each draft also gets the same depot result.
func TestDraftClearanceDepotFields(t *testing.T) {
	t.Parallel()
	drafts := invalidQueueDrafts(t)
	drafts["preset"] = queueDraft(t)
	// Banks with members, and a null banks member.
	withBanks := queueDraft(t)
	queueStation(t, withBanks["network"])["banks"] = []any{map[string]any{"id": "a", "entry": "s05-entry", "exit": "s05-exit", "berthIDs": []any{"parking-1-01"}}}
	drafts["banks"] = withBanks
	nullBanks := queueDraft(t)
	queueStation(t, nullBanks["network"])["banks"] = nil
	drafts["null banks"] = nullBanks
	unread := func(network sim.Network) sim.Network {
		network.ManeuverGroups = nil
		for i := range network.Stations {
			network.Stations[i].Name = ""
		}
		for i := range network.Lanes {
			network.Lanes[i].StationID, network.Lanes[i].StationRole = "", ""
		}
		return network
	}
	for name, draft := range drafts {
		typed, err := decodeDraftNetwork(draft["network"])
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		projected, ok := draftClearanceNetwork(draft["network"])
		if !ok {
			t.Fatalf("%s: no projection", name)
		}
		if got, want := unread(projected), unread(typed); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the projection drops a member", name)
		}
		if got, want := fmt.Sprint(projected.ValidateDepots()), fmt.Sprint(typed.ValidateDepots()); got != want {
			t.Errorf("%s: projection %s, server %s", name, got, want)
		}
	}
}

// TestDepotDraftServerErrorFirst checks that both editor paths name the
// depot error of the server for a draft whose depot the server refuses:
// the geometry edit check before its lane minimums, here with a chain lane
// shorter than its minimum, and the full validation before its bank
// checks.
func TestDepotDraftServerErrorFirst(t *testing.T) {
	t.Parallel()
	shorten := func(draft map[string]any) {
		// Slot 2 of lane 1 moves 0.6 m toward slot 3, so the chain lane
		// from slot 3 is shorter than its minimum.
		slot2, slot1 := queueNode(t, draft, "s05-slot-1-02"), queueNode(t, draft, "s05-slot-1-01")
		for _, axis := range []string{"x", "y"} {
			slot2[axis] = number(slot2[axis]) - 0.1*(number(slot1[axis])-number(slot2[axis]))
		}
	}
	valid := queueDraft(t)
	shorten(valid)
	g := geometryDraft{network: object(valid["network"])}
	if err := g.validateBankChanges(object(queueDraft(t)["network"])); err == nil || strings.HasPrefix(err.Error(), "validate depots: ") {
		t.Fatalf("the short chain lane alone: %v", err)
	}
	for name, draft := range invalidQueueDrafts(t) {
		typed, err := decodeDraftNetwork(draft["network"])
		if err != nil {
			t.Fatal(err)
		}
		_, serverErr := sim.PrepareNetwork(typed)
		if serverErr == nil || !strings.HasPrefix(serverErr.Error(), "depot station ") {
			t.Fatalf("%s: server %v", name, serverErr)
		}
		want := "validate depots: " + serverErr.Error()
		if err := validateBankDraft(draft["network"]); err == nil || err.Error() != want {
			t.Errorf("%s: full validation %v, want %s", name, err, want)
		}
		shorten(draft)
		g := geometryDraft{network: object(draft["network"])}
		if err := g.validateBankChanges(object(queueDraft(t)["network"])); err == nil || err.Error() != want {
			t.Errorf("%s: edit check %v, want %s", name, err, want)
		}
	}
}

// lateralQueueDraft returns the preset draft with lane 2 of the depot
// moved across, so the lanes are width apart, and with the run
// lanes and the throats at speed.
func lateralQueueDraft(t *testing.T, width, speed float64) map[string]any {
	t.Helper()
	draft := queueDraft(t)
	inner, outer := queueNode(t, draft, "s05-slot-1-01"), queueNode(t, draft, "s05-slot-2-01")
	dx, dy := number(inner["x"])-number(outer["x"]), number(inner["y"])-number(outer["y"])
	shift := 1 - width/math.Hypot(dx, dy)
	dx, dy = shift*dx, shift*dy
	for _, node := range items(member(draft["network"], "nodes")) {
		if id := text(member(node, "id")); strings.Contains(id, "-2-") || strings.HasSuffix(id, "-2") {
			position := object(member(node, "position"))
			position["x"], position["y"] = number(position["x"])+dx, number(position["y"])+dy
		}
	}
	for _, item := range items(member(draft["network"], "lanes")) {
		lane := object(item)
		id := text(lane["id"])
		if strings.HasSuffix(id, "-2") && lane["control"] != nil {
			control := object(lane["control"])
			control["x"], control["y"] = number(control["x"])+dx, number(control["y"])+dy
		}
		if strings.HasPrefix(id, "s05-run-") || strings.HasPrefix(id, "s05-throat-") {
			lane["speedLimit"] = speed
		}
	}
	return draft
}

// TestDepotLateralDraft checks that the editor and the server agree on a
// depot with lanes 6 m apart. The server accepts the draft, and the
// checks of the editor report no error. The pair pieces of the editor
// equal those of the server index for each pair of depot lanes, and the
// lead-ins are one piece at the lateral clearance. Lanes 4 m apart are
// refused by both.
func TestDepotLateralDraft(t *testing.T) {
	t.Parallel()
	draft := lateralQueueDraft(t, 6, 2)
	typed, err := decodeDraftNetwork(draft["network"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sim.PrepareNetwork(typed); err != nil {
		t.Fatalf("server: %v", err)
	}
	if report := draftChecks(draft); len(report.Errors) != 0 {
		t.Fatalf("editor checks: %+v", report.Errors)
	}
	editor, server := newDraftClearance(draft["network"]), sim.NewLaneClearance(typed)
	typedLane := func(id string) sim.Lane {
		for _, lane := range typed.Lanes {
			if lane.ID == id {
				return lane
			}
		}
		t.Fatalf("no lane %s", id)
		return sim.Lane{}
	}
	lanes := make(map[string]any)
	var ids []string
	for _, item := range items(member(draft["network"], "lanes")) {
		if id := text(member(item, "id")); strings.HasPrefix(id, "s05-") && !strings.HasSuffix(id, "through") {
			lanes[id] = item
			ids = append(ids, id)
		}
	}
	for i, a := range ids {
		for _, b := range ids[i+1:] {
			got, want := editor.pairPieces(lanes[a], lanes[b], 10, 20), server.PairPieces(typedLane(a), typedLane(b), 10, 20)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s and %s: editor %+v, server %+v", a, b, got, want)
			}
		}
	}
	if got := server.PairPieces(typedLane("s05-in-1"), typedLane("s05-in-2"), 10, 20); len(got) != 1 || got[0].Clearance >= sim.Clearance {
		t.Fatalf("lead-in pieces %+v", got)
	}
	tight := lateralQueueDraft(t, 4, 2)
	typed, err = decodeDraftNetwork(tight["network"])
	if err != nil {
		t.Fatal(err)
	}
	_, serverErr := sim.PrepareNetwork(typed)
	if serverErr == nil || !strings.Contains(serverErr.Error(), "less than 12 meters apart") {
		t.Fatalf("server at 4 m: %v", serverErr)
	}
	if report := draftChecks(tight); len(report.Errors) == 0 {
		t.Fatal("the editor accepts lanes 4 m apart")
	}
}
