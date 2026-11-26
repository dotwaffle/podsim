package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"os"
	"testing"
)

// londonExpressParityPath holds the digests of
// TestLondonExpressBehaviorParity. Run the test with -update to write it
// again.
const londonExpressParityPath = "testdata/london_express_parity.json"

// parityDeltas is the number of delta publications that the parity test
// records after each checkpoint, one each replayCaptureTicks ticks.
const parityDeltas = 3

// parityCheckpoint holds the SHA-256 of the JSON of the saved state, of the
// full stream envelope, and of the delta envelopes after it, at one state
// of the London Express script.
type parityCheckpoint struct {
	State      string   `json:"state"`
	Tick       int      `json:"tick"`
	SavedState string   `json:"savedState"`
	Full       string   `json:"full"`
	Deltas     []string `json:"deltas"`
}

// parityRecord holds the SHA-256 of the JSON of the topology and the
// digests at each checkpoint.
type parityRecord struct {
	Topology    string             `json:"topology"`
	Checkpoints []parityCheckpoint `json:"checkpoints"`
}

func parityDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestLondonExpressBehaviorParity pins the behavior of london-full:dedicated
// (project D) for the shape clearance change: with the network field built
// and unset, D must give the same saved-state and stream bytes at each of
// its checkpoints (docs/design/shape-clearance.md, section 5.1). The test
// runs the script of TestLondonExpressPhysicalCheckpoints. At each state it
// records the saved-state JSON, as the encoder writes it before compression,
// and the full stream envelope. It then records the delta envelopes of the
// next parityDeltas captures, at the cadence of the publisher. The stream
// ID, the server start, the epoch, and the build ID have fixed values.
// Run the test with -update to write the golden file again.
func TestLondonExpressBehaviorParity(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the London Express parity test runs without -short or -race")
	}
	f := newLondonExpressFixture(t, WithBuildID(testBuildID))
	// These are random in a server. The client reads the epoch.
	f.shared.serverStart, f.shared.epoch = "parity-start", testStateEpoch
	f.client = newTestClient(f.shared, "london-express")
	topology, err := jsonv2.Marshal(f.shared.Topology(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	got := parityRecord{Topology: parityDigest(topology)}
	encode := func(e StreamEnvelope) string {
		t.Helper()
		data, encodeErr := EncodeStreamJSON(e)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return parityDigest(data)
	}
	var previous StreamFrame
	var sequence uint64
	pending, next := 0, 0
	f.drive(func(tick int, state string) {
		saved := decompressTestJSON(t, encodeTestState(t, sessionStateFile(t, f.shared)))
		frame, frameErr := f.shared.presentationFrame()
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		sequence = 1
		full := encode(StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "parity", Sequence: sequence,
			Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
		got.Checkpoints = append(got.Checkpoints, parityCheckpoint{State: state, Tick: tick, SavedState: parityDigest(saved), Full: full})
		previous, pending, next = frame, parityDeltas, tick+replayCaptureTicks
	}, func(tick int) bool {
		if pending == 0 || tick != next {
			return pending > 0
		}
		frame, frameErr := f.shared.presentationFrame()
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		delta, deltaErr := makeDelta(previous, frame)
		if deltaErr != nil {
			t.Fatal(deltaErr)
		}
		sequence++
		last := &got.Checkpoints[len(got.Checkpoints)-1]
		last.Deltas = append(last.Deltas, encode(StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "delta", Stream: "parity", Sequence: sequence, Base: sequence - 1,
			Source: sourceOf(frame), Build: frame.State.Build, Delta: &delta}))
		previous, next = frame, tick+replayCaptureTicks
		pending--
		return pending > 0
	})
	for _, checkpoint := range got.Checkpoints {
		if len(checkpoint.Deltas) != parityDeltas {
			t.Fatalf("checkpoint %s has %d deltas, want %d", checkpoint.State, len(checkpoint.Deltas), parityDeltas)
		}
	}
	data, err := jsonv2.Marshal(got, jsontext.WithIndent("\t"))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *update {
		if err = os.WriteFile(londonExpressParityPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(londonExpressParityPath)
	if err != nil {
		t.Fatal(err)
	}
	var want parityRecord
	if err = jsonv2.Unmarshal(stored, &want); err != nil {
		t.Fatal(err)
	}
	if got.Topology != want.Topology {
		t.Errorf("topology JSON digest %s, golden %s", got.Topology, want.Topology)
	}
	if len(got.Checkpoints) != len(want.Checkpoints) {
		t.Errorf("%d checkpoints, golden %d", len(got.Checkpoints), len(want.Checkpoints))
	}
	for i := range min(len(got.Checkpoints), len(want.Checkpoints)) {
		g, w := got.Checkpoints[i], want.Checkpoints[i]
		if g.State != w.State || g.Tick != w.Tick {
			t.Errorf("checkpoint %d: state %s at tick %d, golden %s at tick %d", i, g.State, g.Tick, w.State, w.Tick)
		}
		if g.SavedState != w.SavedState {
			t.Errorf("checkpoint %s: saved-state JSON digest %s, golden %s", g.State, g.SavedState, w.SavedState)
		}
		if g.Full != w.Full {
			t.Errorf("checkpoint %s: full stream envelope digest %s, golden %s", g.State, g.Full, w.Full)
		}
		for j := range max(len(g.Deltas), len(w.Deltas)) {
			if j >= len(g.Deltas) || j >= len(w.Deltas) || g.Deltas[j] != w.Deltas[j] {
				t.Errorf("checkpoint %s: delta envelope %d differs from the golden file (%d deltas, golden %d)", g.State, j+1, len(g.Deltas), len(w.Deltas))
			}
		}
	}
	if !bytes.Equal(stored, data) && !t.Failed() {
		t.Errorf("%s differs from the digests; run with -update", londonExpressParityPath)
	}
}
