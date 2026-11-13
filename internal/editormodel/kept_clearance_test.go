package editormodel

import "testing"

// TestEngineEditReadsOriginalClearance pins that an edit of the engine reads
// the lane minimums of the network before the edit from the kept clearance
// index of that network. A faster lane raises the minimum of the unchanged
// lane after it, and the edit names that lane.
func TestEngineEditReadsOriginalClearance(t *testing.T) {
	t.Parallel()
	config := bankEditorConfig()
	config.Network, _ = depotDraft(t, 0)
	k := newKeptEngine(t, config)
	const refusal = `{"valid":false,"error":"lane depot-c1 would be shorter than 12 meters"}`
	for _, validated := range []bool{false, true} {
		if validated {
			k.compare("start", "validate", "checks")
		}
		edit := `{"field":"geometry","value":{"action":"laneSpeed","id":"depot-c0","value":7.2}}`
		got, want := ask(k.model, `{"op":"edit","edit":`+edit+`}`), Call(`{"op":"edit","project":`+k.project()+`,"edit":`+edit+`}`)
		if got != want || got != refusal {
			t.Fatalf("validated=%t: edit reply = %s, stateless reply = %s, want %s", validated, got, want, refusal)
		}
	}
}
