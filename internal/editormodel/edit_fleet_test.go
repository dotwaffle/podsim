package editormodel

import (
	"encoding/json/jsontext"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func fleetDraft() map[string]any {
	return map[string]any{
		"network": map[string]any{"Stations": []any{
			map[string]any{"ID": "alpha", "Berths": []any{map[string]any{"ID": "a1"}, map[string]any{"ID": "a2"}, map[string]any{"ID": "a3"}}},
			map[string]any{"ID": "beta", "Berths": []any{map[string]any{"ID": "b1"}}},
		}},
		"fleet": []any{
			map[string]any{"ID": "01", "StationID": "alpha", "BerthID": "a2"},
			map[string]any{"ID": "02", "StationID": "beta", "BerthID": "b1"},
			map[string]any{"ID": "07", "StationID": "alpha", "BerthID": "missing"},
		},
	}
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
				if member(pod, "ID") != row.ids[i] || member(pod, "BerthID") != row.berth[i] {
					t.Fatalf("pod %d = %#v", i, pod)
				}
			}
			object(fleet[0])["ID"] = "mutated"
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
		`{"field":"name","target":"alpha","value":"Name"}`,
		`{"field":"name","target":"","value":"Name"}`, `{"field":"name","target":null,"value":"Name"}`,
		`{"field":"fleetCount","target":null,"value":1}`, `{"field":"fleetCount","target":1,"value":1}`,
	} {
		if _, err := editProject(draft, jsontext.Value(raw)); err == nil {
			t.Errorf("invalid target accepted: %s", raw)
		}
	}
	var full []any
	for i := range project.MaxPods {
		full = append(full, map[string]any{"ID": fmt.Sprintf("other-%d", i), "StationID": "beta", "BerthID": fmt.Sprintf("berth-%d", i)})
	}
	draft["fleet"] = full
	if _, err := editProject(draft, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":1}`)); err == nil {
		t.Fatal("proposed fleet exceeds the existing transfer count limit")
	}
	change, err := editProject(draft, jsontext.Value(`{"field":"fleetCount","target":"alpha","value":0}`))
	if err != nil || len(change.Patch) != 0 {
		t.Fatal("unchanged full fleet should remain editable")
	}
	delete(object(draft["network"]), "Stations")
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
