package editormodel

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestRemovedMarkersEditorVerdicts checks that the editor engine, the editor
// call and the server decoder refuse each removed marker as an unknown member,
// with each value, in a whole draft.
func TestRemovedMarkersEditorVerdicts(t *testing.T) {
	t.Parallel()
	base, err := jsonv2.Marshal(project.Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"incidentContract", "faultContract", "emergencyContract"} {
		for _, value := range []string{`"incident-v1"`, `"fault-v1"`, `"emergency-v1"`, `null`, `""`, `false`} {
			t.Run(name+" "+value, func(t *testing.T) {
				t.Parallel()
				raw := append(bytes.TrimSuffix(slices.Clone(base), []byte("}")), []byte(`,"`+name+`":`+value+`}`)...)
				for index, verdict := range []error{nativeVerdict(t, raw), engineVerdict(raw), callVerdict(raw)} {
					if verdict == nil {
						t.Errorf("verdict %d accepted the member", index)
					}
				}
			})
		}
	}
}
