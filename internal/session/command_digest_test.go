package session

import (
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCommandDigestBaseline pins the digest of one command per action. The
// project commands carry the plain and the Express example projects.
//
// A receipt keeps only the digest of a command, and a retry matches its
// receipt by that digest. digestCommand hashes every field of Command and
// of the nested project.Config by reflection, including zero fields. Thus a
// new field, a removed field or a changed field order changes every digest.
// The exception is an extension field of the digest extension registry: a
// command that does not set it keeps its digest.
// Work on the saved-state and stream formats must not change a command
// type, so each digest must stay the same. When a change to a command type
// or to an example project is intentional, run the test with -update and
// give the reason in the commit message.
func TestCommandDigestBaseline(t *testing.T) {
	t.Parallel()
	const path = "testdata/command_digests.txt"
	plain := project.Default()
	express := expressConsumerProject(t)
	commands := []struct {
		name    string
		command Command
	}{
		{"trip", Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 2,
			SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}},
		{"trip-express", Command{Action: "trip", OrderContract: sim.ExpressOrderContract, Origin: "harbor", Destination: "market",
			PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"}},
		{"pause", Command{Action: "pause", Paused: true}},
		{"speed", Command{Action: "speed", Speed: 15}},
		{"reset", Command{Action: "reset"}},
		{"demo", Command{Action: "demo"}},
		{"demand", Command{Action: "demand", Demand: DemandConfig{Enabled: true, PerMinute: 6, Pattern: "market", Seed: 3}}},
		{"project-plain", Command{Action: "project", Project: &plain, ProjectRevision: 1, ServerStart: testStateEpoch}},
		{"project-express", Command{Action: "project", Project: &express, ProjectRevision: 2, ServerStart: testStateEpoch}},
		{"checkpoint", Command{Action: "checkpoint"}},
		{"rewind", Command{Action: "rewind", Checkpoint: 4}},
	}
	var lines []string
	for index, item := range commands {
		item.command.Client, item.command.Sequence, item.command.Epoch = "digest-baseline", uint64(index+1), testStateEpoch
		digest := digestCommand(item.command)
		if digest.unmatched {
			t.Fatalf("%s: the command has a NaN value", item.name)
		}
		lines = append(lines, fmt.Sprintf("%s %s", item.name, hex.EncodeToString(digest.sum[:])))
	}
	got := strings.Join(lines, "\n") + "\n"
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for _, line := range slices.Concat(lines, want) {
		if !slices.Contains(lines, line) || !slices.Contains(want, line) {
			t.Errorf("command digest differs from %s: %s", path, line)
		}
	}
	if len(lines) != len(want) {
		t.Errorf("%s has %d digests, want %d", path, len(want), len(lines))
	}
}
