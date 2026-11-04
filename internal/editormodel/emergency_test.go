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

// TestEmergencySettingsEditorVerdicts checks that the editor engine, the
// editor call and the server decoder agree on emergencies. Each accepts
// absent settings, an empty object, and a rate of 0, and each refuses null
// at any level, a rate above 0, and a removed marker with any value. The
// engine keeps the settings of an accepted draft.
func TestEmergencySettingsEditorVerdicts(t *testing.T) {
	t.Parallel()
	base, err := jsonv2.Marshal(project.Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		extra string
		ok    bool
	}{
		{"", true},
		{`,"emergencies":{"perHour":0}`, true},
		{`,"emergencies":{}`, true},
		{`,"faults":{},"emergencies":{}`, true},
		{`,"emergencies":null`, false},
		{`,"emergencies":{"perHour":null}`, false},
		{`,"emergencies":{"perHour":1}`, false},
		{`,"emergencyContract":"emergency-v1"`, false},
		{`,"emergencyContract":"emergency-v1","emergencies":{}`, false},
		{`,"emergencyContract":null`, false},
		{`,"emergencyContract":""`, false},
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
			if !reflect.DeepEqual(model.config.Emergencies, native.Emergencies) {
				t.Fatalf("engine emergencies %+v, want the native members", model.config.Emergencies)
			}
		})
	}
}
