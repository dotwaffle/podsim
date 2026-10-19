package editormodel

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
)

// compactDraft returns the compact station preset as an editor draft.
func compactDraft(t *testing.T) map[string]any {
	t.Helper()
	config, err := scenarios.CompactStation()
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

// TestManeuverGroupDraftChecks checks that the editor accepts the short
// lanes of a maneuver group, refuses the same lane outside the group, and
// runs the full validation of the server on a draft with a group.
func TestManeuverGroupDraftChecks(t *testing.T) {
	t.Parallel()
	draft := compactDraft(t)
	if report := draftChecks(draft); len(report.Errors) != 0 || len(report.Warnings) != 0 {
		t.Fatalf("preset checks: %+v", report)
	}
	outside := compactDraft(t)
	group := object(items(member(member(outside, "network"), "maneuverGroups"))[0])
	group["laneIDs"] = slices.DeleteFunc(items(group["laneIDs"]), func(id any) bool { return id == "aisle-2" })
	if report := draftChecks(outside); len(report.Errors) != 1 || report.Errors[0].Text != "Lane aisle-2 is shorter than 24 m." {
		t.Fatalf("lane outside the group: %+v", report.Errors)
	}
	// Bay 3 one meter closer to bay 2 passes the editor checks, and only
	// the certification of the server refuses it.
	closer := compactDraft(t)
	for _, node := range items(member(member(closer, "network"), "nodes")) {
		if text(member(node, "id")) == "berth-3" {
			position := object(member(node, "position"))
			position["x"] = number(position["x"]) - 1
		}
	}
	report := draftChecks(closer)
	if len(report.Errors) != 1 || !strings.HasPrefix(report.Errors[0].Text, `validate maneuver groups: maneuver group "compact-bays"`) {
		t.Fatalf("bay 3 moved: %+v", report.Errors)
	}
}

// TestManeuverGroupGeometryEdits checks that a geometry edit of a draft
// with a group runs the full validation, and that the lanes of one group
// are not a lane conflict.
func TestManeuverGroupGeometryEdits(t *testing.T) {
	t.Parallel()
	draft := compactDraft(t)
	position := func(id string) map[string]any {
		for _, node := range items(member(member(draft, "network"), "nodes")) {
			if text(member(node, "id")) == id {
				return object(member(node, "position"))
			}
		}
		t.Fatalf("no node %s", id)
		return nil
	}
	berth := position("berth-3")
	move := func(id string, x, y float64) error {
		raw, err := jsonv2.Marshal(map[string]any{"action": "moveNode", "id": id, "point": map[string]any{"x": x, "y": y}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = editGeometry(draft, jsontext.Value(raw))
		return err
	}
	if err := move("berth-3", number(berth["x"])-1, number(berth["y"])); err == nil || !strings.Contains(err.Error(), `validate maneuver groups: maneuver group "compact-bays"`) {
		t.Fatalf("bay 3 moved: %v", err)
	}
	town := position("town-berth-2")
	if err := move("town-berth-2", number(town["x"])+1, number(town["y"])); err != nil {
		t.Fatalf("town berth moved: %v", err)
	}
	geometry := geometryDraft{network: object(member(draft, "network"))}
	var lanes []string
	for _, id := range items(member(items(member(member(draft, "network"), "maneuverGroups"))[0], "laneIDs")) {
		lanes = append(lanes, text(id))
	}
	if conflict := geometry.laneConflict(lanes, true); conflict != nil {
		t.Fatalf("group lanes conflict: %+v", conflict)
	}
}
