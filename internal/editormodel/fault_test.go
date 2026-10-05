package editormodel

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestFaultMarkerEditorVerdicts checks that the editor engine, the editor
// call and the server decoder agree on the fault marker and on faults.
// Each accepts the marker with the incident marker and faults, and each
// refuses the marker without the incident marker or faults, faults
// without the marker, and a null value. The engine keeps both members of
// an accepted draft.
func TestFaultMarkerEditorVerdicts(t *testing.T) {
	t.Parallel()
	base, err := json.Marshal(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	const incident = `,"incidentContract":"incident-v1"`
	const faults = `,"faults":{"evacuationSeconds":0,"duration":{"kind":"fixed","seconds":600}}`
	for _, test := range []struct {
		extra string
		ok    bool
	}{
		{incident + `,"faultContract":"fault-v1"` + faults, true},
		{incident + `,"faultContract":"fault-v1","faults":{}`, true},
		{`,"faultContract":"fault-v1"` + faults, false},
		{incident + `,"faultContract":"fault-v1"`, false},
		{incident + faults, false},
		{incident + `,"faults":{}`, false},
		{incident + `,"faults":null`, false},
		{incident + `,"faultContract":null` + faults, false},
		{incident + `,"faultContract":""` + faults, false},
		{incident + `,"faultContract":"fault-v1","faults":null`, false},
		{`,"faultContract":null`, false},
		{`,"faultContract":""`, false},
		{`,"faults":null`, false},
		{incident + `,"faultContract":"fault-v1","faults":{"perHour":null}`, false},
		{incident + `,"faultContract":"fault-v1","faults":{"perHour":1}`, false},
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
			if model.config.FaultContract != project.FaultV1Contract || !reflect.DeepEqual(model.config.Faults, native.Faults) {
				t.Fatalf("engine fault members %q and %+v, want the native members", model.config.FaultContract, model.config.Faults)
			}
		})
	}
}
