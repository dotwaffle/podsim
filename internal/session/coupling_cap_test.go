package session

import "testing"

// TestCouplingEncodeStateRejectsInvalidView checks that the HTTP state
// encoder of the server refuses a coupling frame that the assembler
// refuses, and gives no bytes. The server makes its frame from a checked
// snapshot, so only this test reaches the check.
func TestCouplingEncodeStateRejectsInvalidView(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, topology, frame := couplingStreamFixture(t, data.Frames[0], "")
	if _, err := EncodeStateJSON(topology, frame); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*StreamFrame){
		"cabin binding": func(f *StreamFrame) { f.State.Simulation.Vehicles[0].CouplingID = "other" },
		"membership":    func(f *StreamFrame) { f.State.Simulation.CouplingGroups[0].Members[0] = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bad := ownStreamBoardings(frame)
			edit(&bad)
			if raw, err := EncodeStateJSON(topology, bad); err == nil || raw != nil {
				t.Fatalf("encoder gave %d bytes and %v for an invalid coupling view", len(raw), err)
			}
		})
	}
}
