package session

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCouplingHTTPTopologyScanCap puts the topology member of a coupling
// HTTP state at the topology scan cap and one byte over it. The typed
// decode of the topology has the same cap with another text, so the test
// asserts the text of the scan.
func TestCouplingHTTPTopologyScanCap(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, topology, frame := couplingStreamFixture(t, data.Frames[0], sim.ExpressOrderContract)
	raw, err := EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DecodeStateJSON(padTopologyMember(t, raw, MaxTopologyJSON))
	if err != nil || len(state.Simulation.CouplingGroups) != 1 {
		t.Fatal("refused the topology at the cap", err)
	}
	_, err = DecodeStateJSON(padTopologyMember(t, raw, MaxTopologyJSON+1))
	wantError(t, err, "HTTP topology exceeds supported limit")
}

// TestCouplingSavedHalfFleetAtDecoder sends a coupled save with as many
// groups as half its pods, and a save with one group more, to the save
// decoder of the startup restore. The later route check has the same bound
// with another text, so the test asserts the text of the scan.
func TestCouplingSavedHalfFleetAtDecoder(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	file := couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))
	if len(file.Simulation.Pods) != 2 || len(file.Simulation.CouplingGroups) != 1 {
		t.Fatalf("the fixture has %d pods and %d groups, want 2 and 1", len(file.Simulation.Pods), len(file.Simulation.CouplingGroups))
	}
	saved := encodeTestState(t, file)
	decoded, err := decodeStateFile(saved)
	if err != nil || len(decoded.Simulation.CouplingGroups) != 1 {
		t.Fatal("refused the save at the half-fleet bound", err)
	}
	var root, simulation map[string]jsontext.Value
	if err = jsonv2.Unmarshal(decompressTestJSON(t, saved), &root); err != nil {
		t.Fatal(err)
	}
	if err = jsonv2.Unmarshal(root["simulation"], &simulation); err != nil {
		t.Fatal(err)
	}
	var groups []jsontext.Value
	if err = jsonv2.Unmarshal(simulation["couplingGroups"], &groups); err != nil {
		t.Fatal(err)
	}
	simulation["couplingGroups"] = mustCouplingJSON(t, append(groups, groups[0]))
	root["simulation"] = mustCouplingJSON(t, simulation)
	_, err = decodeStateFile(compressTestJSON(t, mustCouplingJSON(t, root)))
	wantError(t, err, "invalid_state: saved coupling groups exceed half the fleet")
}
