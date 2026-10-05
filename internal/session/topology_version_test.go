package session

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// A plain topology has no contract marker, so only the version check
// refuses a missing or earlier project version.
func TestPlainTopologyProjectVersion(t *testing.T) {
	t.Parallel()
	base := TopologySnapshot{ProjectVersion: project.CurrentVersion, Network: sim.BankExample(),
		ServerStart: "source", Epoch: "epoch", ProjectRevision: 1}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	member := []byte(`"projectVersion":1`)
	if !bytes.Contains(raw, member) {
		t.Fatalf("topology JSON has no %s", member)
	}
	var control TopologySnapshot
	if err := json.Unmarshal(raw, &control); err != nil {
		t.Fatal("refused version 1:", err)
	}
	for name, replacement := range map[string]string{
		"missing":          `"projectRevision":1`,
		"version 0":        `"projectVersion":0`,
		"version 2":        `"projectVersion":2`,
		"version 3":        `"projectVersion":3`,
		"version 4":        `"projectVersion":4`,
		"version 5":        `"projectVersion":5`,
		"folded version 2": `"projectVerſion":2`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := bytes.Replace(raw, member, []byte(replacement), 1)
			if name == "missing" {
				edited = bytes.Replace(raw, append(member, ','), nil, 1)
			}
			var topology TopologySnapshot
			err := json.Unmarshal(edited, &topology)
			want := "project version"
			if name == "folded version 2" {
				want = "unknown field"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("decoded %s: %v", edited[:min(len(edited), 80)], err)
			}
		})
	}
}

func TestAssemblyRefusesOtherProjectVersions(t *testing.T) {
	t.Parallel()
	for _, version := range []int{0, 2, 3, 4, 5} {
		topology := TopologySnapshot{ProjectVersion: version, Network: sim.BankExample()}
		if _, err := NewStreamAssembler(topology); err == nil {
			t.Fatalf("assembler accepted project version %d", version)
		}
		if _, err := FrameState(topology, StateFrame{Speed: 1}); err == nil || !strings.Contains(err.Error(), "unsupported project version") {
			t.Fatalf("frame state accepted project version %d: %v", version, err)
		}
	}
	topology := TopologySnapshot{ProjectVersion: project.CurrentVersion, Network: sim.BankExample()}
	if _, err := NewStreamAssembler(topology); err != nil {
		t.Fatal("assembler refused the current version:", err)
	}
}
