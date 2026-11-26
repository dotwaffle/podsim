package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// presetParityPath holds the digests of TestPresetBehaviorParity. Run the
// test with -update to write it again.
const presetParityPath = "testdata/preset_behavior_parity.json"

// presetParityInterval is the time between two checkpoints of a London
// preset run, and presetParityCheckpoints is the number of checkpoints.
const (
	presetParityInterval    = 10 * 60 * sim.TicksPerSecond
	presetParityCheckpoints = 6
)

// presetParityCheckpoint holds the SHA-256 of the saved-state JSON and of
// the full stream envelope at one state of a preset run.
type presetParityCheckpoint struct {
	State      string `json:"state"`
	Tick       int    `json:"tick"`
	SavedState string `json:"savedState"`
	Full       string `json:"full"`
}

// presetParityRecord holds the SHA-256 of the topology JSON and the
// digests at each checkpoint of a preset run.
type presetParityRecord struct {
	Topology    string                   `json:"topology"`
	Checkpoints []presetParityCheckpoint `json:"checkpoints"`
}

// removedNetworkMembers are the network members that the shape clearance
// always-on proposal removes from the format. The parity digests do not
// include them, so that the golden file holds when they leave the JSON.
var removedNetworkMembers = []string{"lateralMerges", "shapeClearance"}

// stripRemovedNetworkMembers returns data with each member of
// removedNetworkMembers removed from each object that is the value of a
// member "network". It encodes the other tokens again with the options of
// jsontext, so the digests of the golden file are of this encoding.
func stripRemovedNetworkMembers(data []byte) ([]byte, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true))
	var out bytes.Buffer
	enc := jsontext.NewEncoder(&out, jsontext.AllowDuplicateNames(true))
	for {
		kind, length := dec.StackIndex(dec.StackDepth())
		if kind == '{' && length%2 == 0 && dec.PeekKind() == '"' {
			name, err := dec.ReadToken()
			if err != nil {
				return nil, err
			}
			// After a name, the pointer refers to the value of the member.
			if slices.Contains(removedNetworkMembers, name.String()) && strings.HasSuffix(string(dec.StackPointer()), "/network/"+name.String()) {
				if _, err = dec.ReadValue(); err != nil {
					return nil, err
				}
				continue
			}
			if err := enc.WriteToken(name); err != nil {
				return nil, err
			}
			continue
		}
		token, err := dec.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := enc.WriteToken(token); err != nil {
			return nil, err
		}
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// TestStripRemovedNetworkMembers checks that the parity digests drop the
// removed members of a network and only those.
func TestStripRemovedNetworkMembers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{`{"network":{"nodes":[],"lateralMerges":true,"shapeClearance":false,"lanes":[]}}`, `{"network":{"nodes":[],"lanes":[]}}`},
		{`{"project":{"network":{"shapeClearance":null}},"x":1.50}`, `{"project":{"network":{}},"x":1.50}`},
		{`{"shapeClearance":true,"lanes":[{"lateralMerges":true}]}`, `{"shapeClearance":true,"lanes":[{"lateralMerges":true}]}`},
		{`{"network":{"other":{"shapeClearance":true}}}`, `{"network":{"other":{"shapeClearance":true}}}`},
	} {
		got, err := stripRemovedNetworkMembers([]byte(c.in))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("strip %s: got %s, want %s", c.in, got, c.want)
		}
	}
}

// TestPresetBehaviorParity pins the behavior of the presets that keep it
// when the shape clearance rule applies to each network (slice 0 of
// docs/contracts/shape-clearance-always-on-proposal.md): LondonCentral,
// LondonFull, and the shared-track preset. For each preset, it records
// the SHA-256 of the topology JSON, and at each checkpoint of a run the
// SHA-256 of the saved-state JSON, as the encoder writes it before
// compression, and of the full stream envelope. The digests do not include
// the network members lateralMerges and shapeClearance, so the golden file
// must not move when the rule applies to each network or when the members
// leave the format. The London presets run with their demand for 60
// minutes, with a checkpoint each 10 minutes. The shared-track preset runs
// the script of its checkpoints (driveShared). The stream ID, the server
// start, the epoch, and the build ID have fixed values. Run the test with
// -update to write the golden file again.
func TestPresetBehaviorParity(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the preset parity test runs without -short or -race")
	}
	presets := []struct {
		name   string
		config func() (project.Config, error)
	}{
		{"london-central", func() (project.Config, error) { return scenarios.LondonCentral(), nil }},
		{"london-full", func() (project.Config, error) { return scenarios.LondonFull(), nil }},
		{"london-full:shared", scenarios.LondonFullShared.Generate},
	}
	var want map[string]presetParityRecord
	if !*update {
		stored, err := os.ReadFile(presetParityPath)
		if err != nil {
			t.Fatal(err)
		}
		if err = jsonv2.Unmarshal(stored, &want); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	got := map[string]presetParityRecord{}
	// The parallel subtests end before the cleanup of the test runs.
	t.Cleanup(func() {
		if !*update || t.Failed() {
			return
		}
		data, err := jsonv2.Marshal(got, jsontext.WithIndent("\t"), jsonv2.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(presetParityPath, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	for _, preset := range presets {
		t.Run(preset.name, func(t *testing.T) {
			t.Parallel()
			config, err := preset.config()
			if err != nil {
				t.Fatal(err)
			}
			config.Demand.Enabled = true
			shared, err := NewWithProject(config, WithBuildID(testBuildID))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(shared.Close)
			// These are random in a server. The client reads the epoch.
			shared.serverStart, shared.epoch = "parity-start", testStateEpoch
			g := presetParityRun(t, preset.name, config, shared)
			mu.Lock()
			got[preset.name] = g
			mu.Unlock()
			if *update {
				return
			}
			w, found := want[preset.name]
			if !found {
				t.Fatalf("%s has no record of %s; run with -update", presetParityPath, preset.name)
			}
			if g.Topology != w.Topology {
				t.Errorf("topology JSON digest %s, golden %s", g.Topology, w.Topology)
			}
			if len(g.Checkpoints) != len(w.Checkpoints) {
				t.Errorf("%d checkpoints, golden %d", len(g.Checkpoints), len(w.Checkpoints))
			}
			for i := range min(len(g.Checkpoints), len(w.Checkpoints)) {
				gc, wc := g.Checkpoints[i], w.Checkpoints[i]
				if gc.State != wc.State || gc.Tick != wc.Tick {
					t.Errorf("checkpoint %d: state %s at tick %d, golden %s at tick %d", i, gc.State, gc.Tick, wc.State, wc.Tick)
				}
				if gc.SavedState != wc.SavedState {
					t.Errorf("checkpoint %d (%s): saved-state JSON digest %s, golden %s", i, gc.State, gc.SavedState, wc.SavedState)
				}
				if gc.Full != wc.Full {
					t.Errorf("checkpoint %d (%s): full stream envelope digest %s, golden %s", i, gc.State, gc.Full, wc.Full)
				}
			}
		})
	}
}

// presetParityRun runs the preset of shared and returns its digests.
func presetParityRun(t *testing.T, name string, config project.Config, shared *Session) presetParityRecord {
	t.Helper()
	digest := func(data []byte) string {
		t.Helper()
		stripped, err := stripRemovedNetworkMembers(data)
		if err != nil {
			t.Fatal(err)
		}
		return parityDigest(stripped)
	}
	topology, err := jsonv2.Marshal(shared.Topology(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	record := presetParityRecord{Topology: digest(topology)}
	checkpoint := func(tick int, state string) {
		t.Helper()
		saved := decompressTestJSON(t, encodeTestState(t, sessionStateFile(t, shared)))
		frame, err := shared.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		full, err := EncodeStreamJSON(StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "parity", Sequence: 1,
			Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
		if err != nil {
			t.Fatal(err)
		}
		record.Checkpoints = append(record.Checkpoints, presetParityCheckpoint{State: state, Tick: tick, SavedState: digest(saved), Full: digest(full)})
	}
	if config.OrderContract == sim.ExpressOrderContract {
		sharedExpressFixtureFor(t, config, shared).driveShared(checkpoint)
		return record
	}
	for tick := 1; tick <= presetParityCheckpoints*presetParityInterval; tick++ {
		shared.advance()
		if tick%presetParityInterval == 0 {
			checkpoint(tick, "demand")
		}
	}
	if completed := shared.simulation.Snapshot().Completed; completed == 0 {
		t.Fatalf("%s: no trip completed", name)
	} else {
		t.Logf("%s: %d trips completed", name, completed)
	}
	return record
}
