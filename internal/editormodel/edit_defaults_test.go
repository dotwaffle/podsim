package editormodel

import (
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"testing"
)

func TestEditorDefaultsSelectAnExistingPassengerDestination(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, destination, action, expected string
		parking, editor                     bool
	}{
		{"missing", "", `"field":"name","value":"Changed"`, "alpha", false, true},
		{"retain", "beta", `"field":"name","value":"Changed"`, "beta", false, true},
		{"parking", "alpha", `"field":"name","value":"Changed"`, "beta", true, true},
		{"delete", "alpha", `"field":"geometry","value":{"action":"deleteStation","id":"alpha"}`, "beta", false, true},
		{"mark-parking", "alpha", `"field":"geometry","value":{"action":"stationParking","id":"alpha","value":true}`, "beta", false, true},
		{"legacy", "", `"field":"name","value":"Changed"`, "", false, false},
		{"flag-checks", "", `"field":"redistribution","value":true`, "alpha", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := fleetDraft()
			maps.Copy(draft, editFixture())
			demand := object(draft["demand"])
			demand["destination"] = test.destination
			demand["extra"] = map[string]any{"nested": "source"}
			object(items(member(draft["network"], "Stations"))[0])["ParkingOnly"] = test.parking
			before := cloneEditValue(draft)
			flag := "false"
			if test.editor {
				flag = "true"
			}
			change, err := editProject(draft, jsontext.Value(`{`+test.action+`,"editor":`+flag+`}`))
			if err != nil {
				t.Fatal(err)
			}
			result := demand
			if replacement, exists := change.Patch["demand"]; exists {
				result = object(replacement)
			}
			if result["destination"] != test.expected || !reflect.DeepEqual(draft, before) {
				t.Fatal("incorrect destination or source mutation", result)
			}
			if test.name == "flag-checks" && change.Flag != "" {
				t.Fatal("destination repair preserved the boolean-only check marker")
			}
			if replacement := object(change.Patch["demand"]); replacement != nil {
				object(replacement["extra"])["nested"] = "caller"
				if object(demand["extra"])["nested"] != "source" {
					t.Fatal("the demand proposal shares mutable source data")
				}
			}
		})
	}
}

func TestEditorDefaultsRejectMalformedFlag(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"null", "1", `"true"`, "{}"} {
		if _, err := editProject(fleetDraft(), jsontext.Value(`{"field":"name","value":"Changed","editor":`+value+`}`)); err == nil {
			t.Fatal("accepted malformed editor flag", value)
		}
	}
}
