package editormodel

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestFaultSettingsEditorVerdicts checks that the editor engine, the editor
// call and the server decoder agree on faults. Each accepts absent settings,
// an empty object, and explicit values, and each refuses null at any level,
// a rate above 0 with no duration, a rate above 60, and a removed marker with
// any value. The engine keeps the settings of an accepted draft.
func TestFaultSettingsEditorVerdicts(t *testing.T) {
	t.Parallel()
	base, err := jsonv2.Marshal(project.Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	const faults = `,"faults":{"evacuationSeconds":0,"duration":{"kind":"fixed","seconds":600}}`
	for _, test := range []struct {
		extra string
		ok    bool
	}{
		{"", true},
		{faults, true},
		{`,"faults":{}`, true},
		{`,"faults":{"evacuationSeconds":0}`, true},
		{`,"faults":null`, false},
		{`,"faults":{"perHour":null}`, false},
		{`,"faults":{"evacuationSeconds":null}`, false},
		{`,"faults":{"perHour":1}`, false},
		{`,"faults":{"perHour":1,"duration":{"kind":"fixed","seconds":600}}`, true},
		{`,"faults":{"perHour":61,"duration":{"kind":"fixed","seconds":600}}`, false},
		{`,"faultContract":"fault-v1"`, false},
		{`,"faultContract":"fault-v1"` + faults, false},
		{`,"faultContract":null`, false},
		{`,"faultContract":""`, false},
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
			if !reflect.DeepEqual(model.config.Faults, native.Faults) {
				t.Fatalf("engine faults %+v, want the native members", model.config.Faults)
			}
		})
	}
}
