package editormodel

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// shapeDraft returns the two small lanes of largeDraftPaths, gap meters
// apart. The lanes share no node. At 5 m, the server certifies them as a
// shape pair at 3.1 m. At 3 m, their bodies overlap, so they keep 12 m,
// and the server refuses them.
func shapeDraft(gap float64) map[string]any {
	return largeDraftPaths(gap, false)
}

// TestShapeDraftMatchesServer checks that the editor and the server agree
// on the lane gaps of a draft network: the draft checks and the server
// checks of an edit. A shape pair is accepted by both, and a small pair
// within 12 m that is not a shape pair is refused by both with the text of
// the server.
func TestShapeDraftMatchesServer(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		gap     float64
		refused bool
	}{
		{gap: 5}, {gap: 3, refused: true},
	} {
		t.Run(fmt.Sprintf("gap %g", test.gap), func(t *testing.T) {
			t.Parallel()
			network := shapeDraft(test.gap)
			typed, err := decodeDraftNetwork(network)
			if err != nil {
				t.Fatal(err)
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

// TestShapeDraftEditRefusal moves a node of a draft. When the lanes come
// within the side-by-side distance, the server checks of the edit refuse
// it with the text of the server. When a lane becomes too short, the lane
// checks of the edit refuse it first, as for a draft with large geometry.
func TestShapeDraftEditRefusal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ edit, want string }{
		{`{"action":"moveNode","id":"d","point":{"x":100,"y":2}}`, `validate shape clearance: nonincident paths "first" and "second" are less than 12 meters apart`},
		{`{"action":"moveNode","id":"d","point":{"x":10,"y":5}}`, "lane second would be shorter than 24 meters"},
	} {
		draft := map[string]any{"network": shapeDraft(5)}
		if _, err := editGeometry(draft, jsontext.Value(test.edit)); err == nil || err.Error() != test.want {
			t.Fatalf("%s: edit error %v, want %q", test.edit, err, test.want)
		}
	}
}

// TestShapeDraftLargeLanes checks two cases of large lanes that the server
// accepts, and the editor with it. Two lanes that admit Compact and
// Express, 5 m apart, form a shape pair at 4.0 m. On reachLayoutDraft, the
// envelope reach by position separates "link-in" and "over", which the
// whole-lane reach does not.
func TestShapeDraftLargeLanes(t *testing.T) {
	t.Parallel()
	const layout = `{"action":"stationLayout","id":"station","value":{"spacing":260}}`
	paths := largeDraftPaths(5, true)
	for _, lane := range items(paths["lanes"]) {
		object(lane)["vehicleClasses"] = []any{"compact", "express"}
	}
	reach := reachLayoutDraft()
	typed, err := decodeDraftNetwork(paths)
	if err != nil {
		t.Fatal(err)
	}
	serverErr := sim.ValidateNetwork(typed)
	var report checkList
	checkNetwork(paths, &report)
	if serverErr != nil || len(report.items) != 0 {
		t.Fatalf("shape pair: server %v, draft checks %+v", serverErr, report.items)
	}

	draft := map[string]any{"network": reach}
	change, err := editGeometry(draft, jsontext.Value(layout))
	if err != nil {
		t.Fatalf("layout edit: %v", err)
	}
	edited, err := decodeDraftNetwork(change.Patch["network"])
	if err != nil {
		t.Fatal(err)
	}
	if err := sim.ValidateNetwork(edited); err != nil {
		t.Fatalf("the server refuses the edited layout: %v", err)
	}
}

// TestShapeDraftBanks checks a draft with banks, and two more small lanes 3 m
// apart that share no node. The editor keeps the bank audit of a draft with
// banks, and its refusal keeps the prefix of the bank audit, with the text of
// the server. Without the far lanes, the editor and the server accept the
// draft.
func TestShapeDraftBanks(t *testing.T) {
	t.Parallel()
	for _, pair := range []bool{false, true} {
		network := object(bankEditorFixture(1)["network"])
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
