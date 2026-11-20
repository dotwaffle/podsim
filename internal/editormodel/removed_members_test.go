package editormodel

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/jsonerr"
	"github.com/dotwaffle/podsim/internal/project"
)

// removedMemberDraft is a draft whose network has removed members:
// inserted is the text after the opening brace of the network object, and
// first is the member that a refusal names.
type removedMemberDraft struct {
	inserted, first string
}

// removedMemberDrafts returns each removed network member with the values
// true, false, and null, and both members in each order.
func removedMemberDrafts() []removedMemberDraft {
	var drafts []removedMemberDraft
	for _, member := range []string{"lateralMerges", "shapeClearance"} {
		for _, value := range []string{"true", "false", "null"} {
			drafts = append(drafts, removedMemberDraft{inserted: `"` + member + `":` + value + `,`, first: member})
		}
	}
	return append(drafts,
		removedMemberDraft{inserted: `"lateralMerges":true,"shapeClearance":true,`, first: "lateralMerges"},
		removedMemberDraft{inserted: `"shapeClearance":false,"lateralMerges":null,`, first: "shapeClearance"})
}

// TestEditorRefusesRemovedMembers pins the text of each editor operation on
// a draft whose network has a removed member, for an explicit project and
// for the synchronized project of the engine. Validation and each edit,
// also an edit of a field that is not in the network, refuse the draft.
// The checks report the error of the network decode. With both members,
// each operation names the first member in document order. The test
// compares the text of the editor reply (jsonerr.Text).
func TestEditorRefusesRemovedMembers(t *testing.T) {
	t.Parallel()
	config := project.Default()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	const network = `"network":{`
	if strings.Count(string(data), network) != 1 {
		t.Fatal("the project does not have one network object")
	}
	var branches map[string]any
	if err = json.Unmarshal(data, &branches); err != nil {
		t.Fatal(err)
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for key := range branches {
			if !yield(key) {
				return
			}
		}
	})
	encodedKeys, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	node := config.Network.Nodes[0]
	edits := map[string]string{
		"name":     `{"field":"name","value":"Renamed"}`,
		"geometry": fmt.Sprintf(`{"field":"geometry","value":{"action":"moveNode","id":%q,"point":{"x":%g,"y":%g}}}`, node.ID, node.Position.X+0.5, node.Position.Y),
	}
	run := func(t *testing.T, draft string) (results map[string]error, checks []string) {
		t.Helper()
		results = make(map[string]error)
		_, results["call validate"] = execute(`{"op":"validate","project":` + draft + `}`)
		for name, edit := range edits {
			_, results["call edit "+name] = execute(`{"op":"edit","project":` + draft + `,"edit":` + edit + `}`)
		}
		reply, err := execute(`{"op":"checks","project":` + draft + `}`)
		if err != nil || reply.Checks == nil {
			t.Fatalf("call checks: %v", err)
		}
		for _, check := range reply.Checks.Errors {
			checks = append(checks, "call "+check.Text)
		}
		model := new(engine)
		_, results["engine sync"] = model.handle(`{"op":"sync","keys":` + string(encodedKeys) + `,"patch":` + draft + `}`)
		_, results["engine validate"] = model.handle(`{"op":"validate"}`)
		for name, edit := range edits {
			_, results["engine edit "+name] = model.handle(`{"op":"edit","edit":` + edit + `}`)
		}
		reply, err = model.handle(`{"op":"checks"}`)
		if err != nil || reply.Checks == nil {
			t.Fatalf("engine checks: %v", err)
		}
		for _, check := range reply.Checks.Errors {
			checks = append(checks, "engine "+check.Text)
		}
		return results, checks
	}
	// The control: the same operations without the members.
	results, checks := run(t, string(data))
	for name, err := range results {
		if err != nil {
			t.Errorf("control %s: %v", name, err)
		}
	}
	if len(checks) != 0 {
		t.Errorf("control checks %q", checks)
	}
	for _, draft := range removedMemberDrafts() {
		t.Run(draft.inserted, func(t *testing.T) {
			t.Parallel()
			member := `unknown object member name "` + draft.first + `"`
			branch := "decode project branch network: json: invalid JSON object at /network/" + draft.first + ": " + member
			want := map[string]string{
				"call validate":      "decode editor project: json: invalid JSON object at /network/" + draft.first + ": " + member,
				"call edit name":     branch,
				"call edit geometry": branch,
				"engine sync":        branch,
				"engine validate":    branch,
				"engine edit name":   branch, "engine edit geometry": branch,
			}
			results, checks := run(t, strings.Replace(string(data), network, network+draft.inserted, 1))
			for name, text := range want {
				if err := results[name]; err == nil || jsonerr.Text(err) != text {
					t.Errorf("%s: error %v, want %s", name, err, text)
				}
			}
			geometry := "decode bank geometry: json: invalid JSON object at /" + draft.first + ": " + member
			if want := []string{"call " + geometry, "engine " + geometry}; !slices.Equal(checks, want) {
				t.Errorf("checks %q, want %q", checks, want)
			}
		})
	}
	// A removed member after an earlier decode error: each operation
	// refuses the draft, also an edit of a field that is not in the
	// network, and names the earlier error, as the server does.
	for _, member := range []string{"lateralMerges", "shapeClearance"} {
		t.Run("after a type error "+member, func(t *testing.T) {
			t.Parallel()
			results, _ := run(t, strings.Replace(string(data), network, network+`"depots":false,"`+member+`":true,`, 1))
			for name, err := range results {
				if err == nil || !strings.Contains(jsonerr.Text(err), "/network/depots") || strings.Contains(err.Error(), member) {
					t.Errorf("%s: error %v, want the decode error of depots", name, err)
				}
			}
		})
	}
}
