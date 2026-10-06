package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

// TestCouplingHTTPMarkersRefuseAlone checks each half of the double
// refusal of a coupling HTTP state with an unmarked topology. Through
// DecodeStateJSON, the root markers of the envelope must be the markers of
// the topology, and that check refuses first. Through FrameState and the
// assembler, which do not read the root markers, the frame binding refuses
// the same topology and frame.
func TestCouplingHTTPMarkersRefuseAlone(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, topology, frame := couplingStreamFixture(t, data.Frames[0], "")
	unmarked := topology
	unmarked.CouplingContract, unmarked.CouplingEnabled, unmarked.CouplingSites, unmarked.CouplingCorridors = "", false, nil, nil

	t.Run("envelope", func(t *testing.T) {
		t.Parallel()
		envelope := StateEnvelope{CouplingContract: topology.CouplingContract, OrderContract: topology.OrderContract, Topology: unmarked, Frame: frame}
		raw, err := jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
		if err != nil {
			t.Fatal(err)
		}
		_, err = DecodeStateJSON(raw)
		wantError(t, err, "HTTP coupling or order contracts disagree")
	})

	t.Run("frame binding", func(t *testing.T) {
		t.Parallel()
		const want = "topology coupling contract does not match state"
		_, err := FrameState(unmarked, frame.State)
		wantError(t, err, want)
		assembler, err := NewStreamAssembler(unmarked)
		if err != nil {
			t.Fatal(err)
		}
		_, err = assembler.State(frame)
		wantError(t, err, want)
	})
}
