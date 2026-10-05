package session

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestFaultDigestTrailers checks the digest extensions of the incident
// suspension contract, N 2 to N 9. A command with one fault field set
// hashes the input
// of the same command without the field, and then a trailer of one field:
// its N and its value. Faults goes in the trailer as one value, with each
// of its members. A command without a fault field writes no trailer, so
// the digest baseline does not change.
func TestFaultDigestTrailers(t *testing.T) {
	t.Parallel()
	faults := &project.FaultConfig{EvacuationSeconds: new(0), DebrisShare: new(0.5),
		Duration: &project.FaultDuration{Kind: "fixed", Seconds: new(600)}}
	for _, test := range []struct {
		n     uint64
		set   func(*Command) any
		field string
	}{
		{2, func(c *Command) any {
			c.Project.FaultContract = project.FaultV1Contract
			return c.Project.FaultContract
		}, "Project.FaultContract"},
		{3, func(c *Command) any { c.Project.Faults = faults; return c.Project.Faults }, "Project.Faults"},
		{4, func(c *Command) any { c.PodID = "01"; return c.PodID }, "PodID"},
		{5, func(c *Command) any { c.LaneID = "bypass-in"; return c.LaneID }, "LaneID"},
		// A start at 0 is set: the pointer keeps it apart from no start.
		{6, func(c *Command) any { c.FromMeters = new(0.0); return c.FromMeters }, "FromMeters"},
		{7, func(c *Command) any { c.ToMeters = new(2.5); return c.ToMeters }, "ToMeters"},
		{8, func(c *Command) any { c.DurationSeconds = new(int64(0)); return c.DurationSeconds }, "DurationSeconds"},
		{9, func(c *Command) any { c.FaultID = "i1.1"; return c.FaultID }, "FaultID"},
	} {
		t.Run(test.field, func(t *testing.T) {
			t.Parallel()
			if !slices.Contains(digestExtensions, digestExtension{n: test.n, path: test.field}) {
				t.Fatalf("no registry entry %d for %s", test.n, test.field)
			}
			plain := project.Default()
			base := Command{Action: "project", Client: "digest", Sequence: 1, Epoch: testStateEpoch, ProjectRevision: 1, Project: &plain}
			marked := base
			markedProject := project.Clone(plain)
			marked.Project = &markedProject
			value := test.set(&marked)
			var encoded recordingHash
			writer := digestWriter{hash: &encoded}
			writer.value(reflect.ValueOf(value))
			trailer := slices.Concat(binary.AppendUvarint(nil, 1), binary.AppendUvarint(nil, test.n), encoded.Bytes())
			if got, want := digestInput(marked), slices.Concat(digestInput(base), trailer); !bytes.Equal(got, want) {
				t.Fatal("the input is not the input without the field and the trailer of the field")
			}
			if digestCommand(base).matches(digestCommand(marked)) {
				t.Fatal("the field did not change the digest")
			}
		})
	}
}
