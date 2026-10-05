package editormodel

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestIncidentMarkerEditorVerdicts checks that the editor engine, the
// editor call and the server decoder agree on the incident marker. Each
// accepts incident-v1 and refuses null, empty text and other values.
func TestIncidentMarkerEditorVerdicts(t *testing.T) {
	t.Parallel()
	base, err := json.Marshal(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"incident-v1"`, `null`, `""`, `"incident-v2"`, `false`} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			raw := append(bytes.TrimSuffix(slices.Clone(base), []byte("}")), []byte(`,"incidentContract":`+value+`}`)...)
			verdicts := []error{nativeVerdict(t, raw), engineVerdict(raw), callVerdict(raw)}
			for index, verdict := range verdicts {
				if accepted := verdict == nil; accepted != (value == `"incident-v1"`) {
					t.Errorf("verdict %d for %s: %v", index, value, verdict)
				}
			}
			if value != `"incident-v1"` {
				return
			}
			model := new(engine)
			if err := syncDraft(model, raw); err != nil {
				t.Fatal(err)
			}
			if got := model.config.IncidentContract; got != "incident-v1" {
				t.Fatalf("engine marker %q, want incident-v1", got)
			}
		})
	}
}
