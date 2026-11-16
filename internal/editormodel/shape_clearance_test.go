package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// shapeDraft returns the two small lanes of largeDraftPaths, gap meters
// apart, with shapeClearance set as given. The lanes share no node. At
// 5 m, the server certifies them as a shape pair at 3.1 m. At 3 m, their
// bodies overlap, so they keep 12 m, and the server refuses them.
func shapeDraft(gap float64, field bool) map[string]any {
	network := largeDraftPaths(gap, false)
	if field {
		network["shapeClearance"] = true
	}
	return network
}

// TestShapeDraftMatchesServer checks that the editor and the server agree
// on the lane gaps of a draft network that sets shapeClearance: the draft
// checks, the lane conflict of a layout edit, and the server checks of an
// edit. With the field, a shape pair is accepted by both, and a small pair
// within 12 m that is not a shape pair is refused by both with the text of
// the server. Without the field, the server does not audit small pairs,
// and the editor results do not change: the draft checks accept both
// pairs, and the lane conflict of a layout edit refuses both at 12 m.
func TestShapeDraftMatchesServer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		gap     float64
		field   bool
		refused bool
	}{
		{gap: 5, field: true}, {gap: 3, field: true, refused: true},
		{gap: 5}, {gap: 3},
	} {
		t.Run(fmt.Sprintf("gap %g field %v", test.gap, test.field), func(t *testing.T) {
			t.Parallel()
			network := shapeDraft(test.gap, test.field)
			typed, err := decodeDraftNetwork(network)
			if err != nil {
				t.Fatal(err)
			}
			if typed.ShapeClearance != test.field {
				t.Fatalf("decoded field %v", typed.ShapeClearance)
			}
			serverErr := sim.ValidateNetwork(typed)
			if (serverErr != nil) != test.refused {
				t.Fatalf("server: %v", serverErr)
			}
			const wantText = `nonincident paths "first" and "second" are less than 12 meters apart`
			want := ""
			if test.refused {
				if serverErr.Error() != wantText {
					t.Fatalf("server error %q, want %q", serverErr, wantText)
				}
				want = "validate shape clearance: " + wantText
			}

			var report checkList
			checkNetwork(network, &report)
			var texts []string
			for _, item := range report.items {
				texts = append(texts, item.Text)
			}
			if want == "" && len(texts) != 0 || want != "" && !reflect.DeepEqual(texts, []string{want}) {
				t.Fatalf("draft checks %q, want %q", texts, want)
			}

			conflict := geometryDraft{network: network}.laneConflict([]string{"second"}, true)
			if test.field && conflict != nil {
				t.Fatalf("lane conflict %+v with the field, want none", conflict)
			}
			if !test.field && (conflict == nil || conflict.other != "first" || conflict.minimum != sim.Clearance) {
				t.Fatalf("lane conflict %+v without the field, want first at %g m", conflict, sim.Clearance)
			}

			draft := map[string]any{"network": network}
			before := cloneEditValue(draft)
			change, err := editGeometry(draft, jsontext.Value(`{"action":"laneSpeed","id":"second","value":15}`))
			if want == "" && err != nil || want != "" && (err == nil || err.Error() != want) {
				t.Fatalf("edit error %v, want %q", err, want)
			}
			if err != nil && len(change.Patch) != 0 || !reflect.DeepEqual(before, draft) {
				t.Fatal("the edit changed its source or published a refused patch")
			}
		})
	}
}

// TestShapeDraftEditRefusal moves a node of a draft with shapeClearance.
// When the lanes come within the side-by-side distance, the server checks
// of the edit refuse it with the text of the server. When a lane becomes
// too short, the lane checks of the edit refuse it first, as for a draft
// with large geometry. Without the field, both edits are accepted, as
// before.
func TestShapeDraftEditRefusal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ edit, want string }{
		{`{"action":"moveNode","id":"d","point":{"x":100,"y":2}}`, `validate shape clearance: nonincident paths "first" and "second" are less than 12 meters apart`},
		{`{"action":"moveNode","id":"d","point":{"x":10,"y":5}}`, "lane second would be shorter than 24 meters"},
	} {
		for _, field := range []bool{true, false} {
			draft := map[string]any{"network": shapeDraft(5, field)}
			_, err := editGeometry(draft, jsontext.Value(test.edit))
			if field && (err == nil || err.Error() != test.want) || !field && err != nil {
				t.Fatalf("%s, field %v: edit error %v, want %q", test.edit, field, err, test.want)
			}
		}
	}
}

// TestShapeDraftRoundTrip checks that a project with shapeClearance keeps
// the field through normalization and a geometry edit, and that the
// project decoder reads it again.
func TestShapeDraftRoundTrip(t *testing.T) {
	t.Parallel()
	draft := map[string]any{"version": float64(project.CurrentVersion), "name": "Shape", "network": shapeDraft(5, true)}
	normalized, err := normalizeProject(draft)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(draft, normalized.Patch)
	change, err := editGeometry(draft, jsontext.Value(`{"action":"moveNode","id":"d","point":{"x":100,"y":6}}`))
	if err != nil {
		t.Fatal(err)
	}
	network, found := change.Patch["network"]
	if !found {
		t.Fatal("the edit published no network")
	}
	if member(network, "shapeClearance") != true {
		t.Fatalf("network after the edit %v", network)
	}
	draft["network"] = network
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if !config.Network.ShapeClearance {
		t.Fatal("the exported project does not set shapeClearance")
	}
}

// TestShapeDraftLargeLanes checks two cases where the draft rules of the
// editor refuse large lanes that the server accepts when the network sets
// shapeClearance. Two lanes that admit Compact and Express, 5 m apart,
// form a shape pair at 4.0 m. On reachLayoutDraft, the envelope reach by
// position separates "link-in" and "over", which the whole-lane reach does
// not. Without the field, the editor and the server refuse both.
func TestShapeDraftLargeLanes(t *testing.T) {
	t.Parallel()
	const layout = `{"action":"stationLayout","id":"station","value":{"spacing":260}}`
	for _, field := range []bool{true, false} {
		paths := largeDraftPaths(5, true)
		for _, lane := range items(paths["lanes"]) {
			object(lane)["vehicleClasses"] = []any{"compact", "express"}
		}
		reach := reachLayoutDraft()
		if field {
			paths["shapeClearance"], reach["shapeClearance"] = true, true
		}
		typed, err := decodeDraftNetwork(paths)
		if err != nil {
			t.Fatal(err)
		}
		serverErr := sim.ValidateNetwork(typed)
		var report checkList
		checkNetwork(paths, &report)
		conflict := geometryDraft{network: paths}.laneConflict([]string{"second"}, true)
		if field && (serverErr != nil || len(report.items) != 0 || conflict != nil) {
			t.Fatalf("shape pair with the field: server %v, draft checks %+v, lane conflict %+v", serverErr, report.items, conflict)
		}
		if !field && (serverErr == nil || len(report.items) != 1 || report.items[0].Text != "validate large geometry: "+serverErr.Error() || conflict == nil || conflict.minimum != 20) {
			t.Fatalf("shape pair without the field: server %v, draft checks %+v, lane conflict %+v", serverErr, report.items, conflict)
		}

		draft := map[string]any{"network": reach}
		change, err := editGeometry(draft, jsontext.Value(layout))
		if field && err != nil || !field && (err == nil || err.Error() != "lane link-in would come within 20 meters of lane over") {
			t.Fatalf("layout edit with the field %v: %v", field, err)
		}
		if !field {
			continue
		}
		edited, err := decodeDraftNetwork(change.Patch["network"])
		if err != nil {
			t.Fatal(err)
		}
		if err := sim.ValidateNetwork(edited); err != nil {
			t.Fatalf("the server refuses the edited layout: %v", err)
		}
	}
}

// TestShapeDraftBanks checks a draft with banks and shapeClearance, and
// two more small lanes 3 m apart that share no node. The editor keeps the
// bank audit of a draft with banks, and its refusal keeps the prefix of
// the bank audit, with the text of the server. Without the far lanes, the
// editor and the server accept the draft.
func TestShapeDraftBanks(t *testing.T) {
	t.Parallel()
	for _, pair := range []bool{false, true} {
		network := object(bankEditorFixture(1)["network"])
		network["shapeClearance"] = true
		if pair {
			paths := largeDraftPaths(3, false)
			for _, node := range items(paths["nodes"]) {
				position := object(member(node, "position"))
				position["x"], position["y"] = number(position["x"])+5000, number(position["y"])+5000
				network["nodes"] = append(items(network["nodes"]), node)
			}
			network["lanes"] = append(items(network["lanes"]), items(paths["lanes"])...)
		}
		typed, err := decodeDraftNetwork(network)
		if err != nil {
			t.Fatal(err)
		}
		serverErr := sim.ValidateNetwork(typed)
		editorErr := validateBankDraft(network)
		if !pair {
			if serverErr != nil || editorErr != nil {
				t.Fatalf("server %v, editor %v, want both to accept", serverErr, editorErr)
			}
			continue
		}
		const want = `nonincident paths "first" and "second" are less than 12 meters apart`
		if serverErr == nil || serverErr.Error() != want {
			t.Fatalf("server %v, want %q", serverErr, want)
		}
		if editorErr == nil || editorErr.Error() != "validate bank geometry: "+want {
			t.Fatalf("editor %v, want %q", editorErr, "validate bank geometry: "+want)
		}
	}
}
