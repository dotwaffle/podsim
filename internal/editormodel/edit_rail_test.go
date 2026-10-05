package editormodel

import (
	"encoding/json/jsontext"
	"reflect"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func railDraft() map[string]any {
	return map[string]any{"network": map[string]any{"stations": []any{
		map[string]any{"id": "parking", "parkingOnly": true},
		map[string]any{"id": "hub"}, map[string]any{"id": "first"}, map[string]any{"id": "second"},
	}}}
}

func TestRailEditLifecycleAndOwnership(t *testing.T) {
	t.Parallel()
	for _, departure := range []bool{false, true} {
		t.Run(strconv.FormatBool(departure), func(t *testing.T) {
			t.Parallel()
			key, choices, prefix, at, walking := "railArrivals", "destinations", "train-", 0.0, 0.0
			if departure {
				key, choices, prefix, at, walking = "railDepartures", "origins", "departure-", 600, 60
			}
			draft := railDraft()
			before := cloneEditValue(draft)
			change, err := editRail(draft, departure, jsontext.Value(`{"action":"add"}`))
			if err != nil || len(change.Patch) != 1 || !reflect.DeepEqual(draft, before) {
				t.Fatal("constructor changed its source", err)
			}
			event := object(items(change.Patch[key])[0])
			if event["id"] != prefix+"1" || event["station"] != "hub" || event["atSeconds"] != at || event["walkingSeconds"] != walking || event["passengers"] != float64(120) {
				t.Fatal("constructor defaults differ", event)
			}
			if departure && (event["requestFromSeconds"] != float64(0) || event["requestUntilSeconds"] != float64(300)) {
				t.Fatal("outbound window defaults differ")
			}
			id := prefix + "1"
			draft[key] = change.Patch[key]
			apply := func(raw string) {
				t.Helper()
				prior := cloneEditValue(draft)
				result, editErr := editRail(draft, departure, jsontext.Value(raw))
				if editErr != nil || !reflect.DeepEqual(draft, prior) {
					t.Fatal("edit failed or changed its source", raw, editErr)
				}
				if updated, exists := result.Patch[key]; exists {
					draft[key] = updated
				}
			}
			apply(`{"action":"addChoice","id":"` + id + `"}`)
			rows := items(member(items(draft[key])[0], choices))
			if len(rows) != 2 || member(rows[1], "station") != "second" {
				t.Fatal("choice constructor did not select an unused passenger station")
			}
			apply(`{"action":"set","id":"` + id + `","index":1,"choiceCount":2,"field":"weight","value":"7"}`)
			if member(items(member(items(draft[key])[0], choices))[1], "weight") != float64(7) {
				t.Fatal("choice weight edit missed its target")
			}
			apply(`{"action":"set","id":"` + id + `","field":"atSeconds","value":"900"}`)
			apply(`{"action":"add"}`)
			if member(items(draft[key])[1], "id") != prefix+"2" || member(items(draft[key])[1], "atSeconds") != float64(1500) {
				t.Fatal("second event ID or time differs")
			}
			apply(`{"action":"removeChoice","id":"` + id + `","index":0,"choiceCount":2}`)
			if member(items(member(items(draft[key])[0], choices))[0], "station") != "second" {
				t.Fatal("choice removal missed its target")
			}
			apply(`{"action":"remove","id":"` + id + `"}`)
			apply(`{"action":"add"}`)
			if member(items(draft[key])[1], "id") != prefix+"1" {
				t.Fatal("removed ID was not reused")
			}
			prior := cloneEditValue(draft)
			result, editErr := editRail(draft, departure, jsontext.Value(`{"action":"set","id":"`+id+`","field":"passengers","value":42}`))
			if editErr != nil {
				t.Fatal(editErr)
			}
			object(items(member(items(result.Patch[key])[0], choices))[0])["station"] = "mutated"
			if !reflect.DeepEqual(draft, prior) {
				t.Fatal("returned plan shares nested maps with the source")
			}
		})
	}
}

func TestRailEditsRejectStaleChoicesAndInvalidCommands(t *testing.T) {
	t.Parallel()
	draft := railDraft()
	change, err := editRail(draft, false, jsontext.Value(`{"action":"add"}`))
	if err != nil {
		t.Fatal(err)
	}
	draft["railArrivals"] = change.Patch["railArrivals"]
	for _, raw := range []string{
		`{}`, `{"action":"add","id":"old"}`, `{"action":"add","id":""}`, `{"action":"add","field":""}`,
		`{"action":"add","index":null}`, `{"action":"remove","id":"missing"}`,
		`{"action":"set","id":"train-1","field":"id","value":"new"}`,
		`{"action":"set","id":"train-1","field":"requestFromSeconds","value":0}`,
		`{"action":"set","id":"train-1","field":"station","value":3}`,
		`{"action":"set","id":"train-1","field":"passengers","value":""}`,
		`{"action":"set","id":"train-1","field":"passengers","value":"NaN"}`,
		`{"action":"set","id":"train-1","field":"passengers","value":null}`,
		`{"action":"set","id":"train-1","field":"passengers","value":false}`,
		`{"action":"set","id":"train-1","index":0,"field":"weight","value":2}`,
		`{"action":"set","id":"train-1","index":0,"choiceCount":2,"field":"weight","value":2}`,
		`{"action":"removeChoice","id":"train-1","index":1,"choiceCount":1}`,
		`{"action":"removeChoice","id":"train-1","index":0,"choiceCount":1}`,
		`{"action":"addChoice","id":"train-1","choiceCount":1}`,
	} {
		before := cloneEditValue(draft)
		if _, editErr := editRail(draft, false, jsontext.Value(raw)); editErr == nil || !reflect.DeepEqual(draft, before) {
			t.Errorf("invalid command accepted or changed the draft: %s", raw)
		}
	}
}

func TestRailConstructorsRetainExistingLimits(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name       string
		key        string
		count      int
		passengers float64
	}{
		{"combined events", "railArrivals", project.MaxRailArrivals, 1},
		{"outbound volume", "railDepartures", 15, project.MaxRailRelease},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			draft := railDraft()
			plan := make([]any, row.count)
			for i := range plan {
				plan[i] = map[string]any{"id": strconv.Itoa(i), "passengers": row.passengers}
			}
			draft[row.key] = plan
			if _, err := editRail(draft, true, jsontext.Value(`{"action":"add"}`)); err == nil {
				t.Fatal("rail constructor exceeded an existing bound")
			}
		})
	}
}
