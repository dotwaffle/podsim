package editormodel

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestRepairOnePairInEditorRefused pins that the editor cannot repair a
// project with two refused pairs one pair at a time. The project has two
// independent pairs of small lanes 3.0 m apart that share no node. Lane b
// of a pair is 200 m long, and lane a is the middle 100 m beside it. A
// geometry edit that moves the "to" node of lane b of the first pair 100 m
// to the side clears that pair. The edit fails the server network check on
// the second pair, and the editor discards its patch.
func TestRepairOnePairInEditorRefused(t *testing.T) {
	t.Parallel()
	withPairs := func(pairs int) project.Config {
		config := project.Clone(project.Default())
		for index := range pairs {
			name, x := fmt.Sprintf("pair-%d", index+1), 9000+1000*float64(index)
			for _, row := range []struct {
				lane        string
				from, to, y float64
			}{{"a", 0, 100, 0}, {"b", -50, 150, 3}} {
				from, to := name+"-"+row.lane+"-from", name+"-"+row.lane+"-to"
				config.Network.Nodes = append(config.Network.Nodes, sim.Node{ID: from, Position: sim.Point{X: x + row.from, Y: row.y}}, sim.Node{ID: to, Position: sim.Point{X: x + row.to, Y: row.y}})
				config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: name + "-" + row.lane, From: from, To: to, SpeedLimit: 14})
			}
		}
		return config
	}
	const edit = `{"field":"geometry","value":{"action":"moveNode","id":"pair-1-b-to","point":{"x":9150,"y":103}}}`
	// With the first pair alone, the edit repairs the project.
	if _, err := editProject(configDraft(t, withPairs(1)), jsontext.Value(edit)); err != nil {
		t.Fatalf("the edit does not repair the first pair: %v", err)
	}
	config := withPairs(2)
	const first = `invalid project scenario: nonincident paths "pair-1-b" and "pair-1-a" are less than 12 meters apart`
	if err := project.Validate(config); err == nil || err.Error() != first {
		t.Fatalf("project refusal = %v, want %s", err, first)
	}
	const want = `validate shape clearance: nonincident paths "pair-2-b" and "pair-2-a" are less than 12 meters apart`
	draft := configDraft(t, config)
	before := cloneEditValue(draft)
	change, err := editProject(draft, jsontext.Value(edit))
	if err == nil || err.Error() != want || len(change.Patch) != 0 {
		t.Fatalf("edit error = %v with %d patch branches, want %s", err, len(change.Patch), want)
	}
	if !reflect.DeepEqual(draft, before) {
		t.Fatal("the refused edit changed the draft")
	}
	model := new(engine)
	synchronize(t, model, config)
	raw := model.branches["network"].raw.Clone()
	if _, err := model.handle(`{"op":"edit","edit":` + edit + `}`); err == nil || err.Error() != want {
		t.Fatalf("engine edit error = %v, want %s", err, want)
	}
	if !reflect.DeepEqual(model.branches["network"].raw, raw) {
		t.Fatal("the refused edit changed the synchronized network")
	}
}
