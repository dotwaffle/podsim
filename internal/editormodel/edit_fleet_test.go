package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func fleetDraft() map[string]any {
	return map[string]any{
		"network": map[string]any{"stations": []any{
			map[string]any{"id": "alpha", "berths": []any{map[string]any{"id": "a1"}, map[string]any{"id": "a2"}, map[string]any{"id": "a3"}}},
			map[string]any{"id": "beta", "berths": []any{map[string]any{"id": "b1"}}},
		}},
		"fleet": []any{
			map[string]any{"id": "01", "stationID": "alpha", "berthID": "a2"},
			map[string]any{"id": "02", "stationID": "beta", "berthID": "b1"},
			map[string]any{"id": "07", "stationID": "alpha", "berthID": "missing"},
		},
	}
}

// stationsDraft is fleetDraft with a network that the server accepts:
// stations alpha and beta with the berths of fleetDraft, joined in a ring.
// The server checks of a geometry edit refuse a draft whose stations have
// no entry, exit, or berth nodes.
func stationsDraft(t *testing.T) map[string]any {
	t.Helper()
	var network sim.Network
	station := func(id string, x float64, berths ...string) {
		entry, exit := id+"-entry", id+"-exit"
		network.Nodes = append(network.Nodes, sim.Node{ID: entry, Position: sim.Point{X: x}}, sim.Node{ID: exit, Position: sim.Point{X: x + 200}})
		network.Lanes = append(network.Lanes, sim.Lane{ID: id + "-through", From: entry, To: exit, SpeedLimit: 14, StationID: id, StationRole: sim.StationThroughRole})
		stop := sim.Station{ID: id, Name: id, Entry: entry, Exit: exit}
		for index, berth := range berths {
			node := id + "-berth-" + berth
			network.Nodes = append(network.Nodes, sim.Node{ID: node, Position: sim.Point{X: x + 100, Y: 40 * float64(index+1)}})
			network.Lanes = append(network.Lanes,
				sim.Lane{ID: id + "-in-" + berth, From: entry, To: node, SpeedLimit: 14, StationID: id, StationRole: sim.StationBerthAccessRole},
				sim.Lane{ID: id + "-out-" + berth, From: node, To: exit, SpeedLimit: 14, StationID: id, StationRole: sim.StationDepartureRole})
			stop.Berths = append(stop.Berths, sim.Berth{ID: berth, Node: node})
		}
		network.Stations = append(network.Stations, stop)
	}
	station("alpha", 0, "a1", "a2", "a3")
	station("beta", 1000, "b1")
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: "outbound", From: "alpha-exit", To: "beta-entry", SpeedLimit: 14},
		sim.Lane{ID: "inbound", From: "beta-exit", To: "alpha-entry", SpeedLimit: 14, Control: &sim.Point{X: 600, Y: -800}})
	raw, err := json.Marshal(network)
	if err != nil {
		t.Fatal(err)
	}
	draft := fleetDraft()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := sim.ValidateNetwork(network); err != nil {
		t.Fatal(err)
	}
	draft["network"] = value
	return draft
}

func TestFleetEditPreservesPodsAndAllocatesFreeIDs(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		value string
		ids   []string
		berth []string
	}{
		{`"0"`, []string{"02"}, []string{"b1"}},
		{`"1"`, []string{"02", "01"}, []string{"b1", "a2"}},
		{`"2.9"`, []string{"02", "01", "03"}, []string{"b1", "a2", "a1"}},
		{`"100"`, []string{"02", "01", "03", "04"}, []string{"b1", "a2", "a1", "a3"}},
		{`"-2"`, []string{"02"}, []string{"b1"}},
		{`""`, []string{"02"}, []string{"b1"}},
		{`"invalid"`, []string{"02"}, []string{"b1"}},
	} {
		t.Run(row.value, func(t *testing.T) {
			t.Parallel()
			draft := fleetDraft()
			before := cloneEditValue(draft)
			change, err := editProject(draft, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":`+row.value+`}`))
			if err != nil {
				t.Fatal(err)
			}
			fleet := items(change.Patch["fleet"])
			if len(fleet) != len(row.ids) || !reflect.DeepEqual(draft, before) {
				t.Fatal("fleet count or input ownership differs")
			}
			for i, pod := range fleet {
				if member(pod, "id") != row.ids[i] || member(pod, "berthID") != row.berth[i] {
					t.Fatalf("pod %d = %#v", i, pod)
				}
			}
			object(fleet[0])["id"] = "mutated"
			if !reflect.DeepEqual(draft, before) {
				t.Fatal("returned fleet shares input maps")
			}
		})
	}
}

func TestFleetEditLimitsAndTargets(t *testing.T) {
	t.Parallel()
	draft := fleetDraft()
	for _, raw := range []string{
		`{"field":"fleetCount","value":1}`, `{"field":"fleetCount","target":"missing","value":1}`,
		`{"field":"name","target":"alpha","value":"name"}`,
		`{"field":"name","target":"","value":"name"}`, `{"field":"name","target":null,"value":"name"}`,
		`{"field":"fleetCount","target":null,"value":1}`, `{"field":"fleetCount","target":1,"value":1}`,
	} {
		if _, err := editProject(draft, jsontext.Value(raw)); err == nil {
			t.Errorf("invalid target accepted: %s", raw)
		}
	}
	var full []any
	for i := range project.MaxPods {
		full = append(full, map[string]any{"id": fmt.Sprintf("other-%d", i), "stationID": "beta", "berthID": fmt.Sprintf("berth-%d", i)})
	}
	draft["fleet"] = full
	if _, err := editProject(draft, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":1}`)); err == nil {
		t.Fatal("proposed fleet exceeds the existing transfer count limit")
	}
	change, err := editProject(draft, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":0}`))
	if err != nil || len(change.Patch) != 0 {
		t.Fatal("unchanged full fleet should remain editable")
	}
	delete(object(draft["network"]), "stations")
	if _, err := editProject(draft, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":1}`)); err == nil {
		t.Fatal("malformed network accepted a fleet edit")
	}
}

func TestDailyClockEdit(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		value string
		want  float64
	}{
		{`"00:00"`, 0}, {`"23:59"`, 1439}, {`"07:30"`, 450},
	} {
		change, err := editProject(editFixture(), jsontext.Value(`{"field":"dailyStartTime","value":`+row.value+`}`))
		if err != nil || member(change.Patch["demand"], "dailyStartMinute") != row.want {
			t.Fatalf("clock %s = %+v, %v", row.value, change, err)
		}
	}
	for _, value := range []string{`""`, `"7:30"`, `"24:00"`, `"23:60"`, `"-1:00"`, `"ab:cd"`, `420`, `true`} {
		if _, err := editProject(editFixture(), jsontext.Value(`{"field":"dailyStartTime","value":`+value+`}`)); err == nil {
			t.Errorf("invalid clock accepted: %s", value)
		}
	}
}
