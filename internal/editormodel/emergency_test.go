package editormodel

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestEmergencyMarkerEditorVerdicts checks that the editor engine, the
// editor call and the server decoder agree on the emergency marker and on
// emergencies. Each accepts the marker with the incident marker and
// emergencies, and each refuses the marker without the incident marker or
// emergencies, emergencies without the marker, a null value, and a rate
// above 0. The engine keeps both members of an accepted draft.
func TestEmergencyMarkerEditorVerdicts(t *testing.T) {
	t.Parallel()
	base, err := json.Marshal(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	const incident = `,"incidentContract":"incident-v1"`
	const marker = `,"emergencyContract":"emergency-v1"`
	for _, test := range []struct {
		extra string
		ok    bool
	}{
		{incident + marker + `,"emergencies":{"perHour":0}`, true},
		{incident + marker + `,"emergencies":{}`, true},
		{incident + marker + `,"faultContract":"fault-v1","faults":{},"emergencies":{}`, true},
		{marker + `,"emergencies":{}`, false},
		{incident + marker, false},
		{incident + `,"emergencies":{}`, false},
		{incident + `,"emergencies":null`, false},
		{incident + `,"emergencyContract":null,"emergencies":{}`, false},
		{incident + `,"emergencyContract":"","emergencies":{}`, false},
		{incident + marker + `,"emergencies":null`, false},
		{`,"emergencyContract":null`, false},
		{`,"emergencyContract":""`, false},
		{`,"emergencies":null`, false},
		{incident + marker + `,"emergencies":{"perHour":null}`, false},
		{incident + marker + `,"emergencies":{"perHour":1}`, false},
	} {
		t.Run(test.extra, func(t *testing.T) {
			t.Parallel()
			raw := append(bytes.TrimSuffix(slices.Clone(base), []byte("}")), []byte(test.extra+`}`)...)
			verdicts := []error{nativeVerdict(t, raw), engineVerdict(raw), callVerdict(raw)}
			for index, verdict := range verdicts {
				if accepted := verdict == nil; accepted != test.ok {
					t.Errorf("verdict %d: %v", index, verdict)
				}
			}
			if !test.ok {
				return
			}
			model := new(engine)
			if err := syncDraft(model, raw); err != nil {
				t.Fatal(err)
			}
			native, err := serverDecode(t, raw)
			if err != nil {
				t.Fatal(err)
			}
			if model.config.EmergencyContract != project.EmergencyV1Contract || !reflect.DeepEqual(model.config.Emergencies, native.Emergencies) {
				t.Fatalf("engine emergency members %q and %+v, want the native members", model.config.EmergencyContract, model.config.Emergencies)
			}
		})
	}
}
