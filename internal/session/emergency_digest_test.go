package session

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestEmergencyDigestTrailers checks the digest extensions of the incident
// emergency contract (section 11.2). A command with one emergency field
// set hashes the input of the same command without the field, and then a
// trailer of one field: its N and its value. Emergencies goes in the
// trailer as one value, with its member. A command without an emergency
// field writes no trailer, so the digest baseline does not change.
func TestEmergencyDigestTrailers(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		n     uint64
		set   func(*Command) any
		field string
	}{
		{10, func(c *Command) any {
			c.Project.EmergencyContract = project.EmergencyV1Contract
			return c.Project.EmergencyContract
		}, "Project.EmergencyContract"},
		// An empty object is set: the pointer keeps it apart from no
		// settings.
		{11, func(c *Command) any { c.Project.Emergencies = &project.EmergencyConfig{}; return c.Project.Emergencies }, "Project.Emergencies"},
		{11, func(c *Command) any {
			c.Project.Emergencies = &project.EmergencyConfig{PerHour: new(0.0)}
			return c.Project.Emergencies
		}, "Project.Emergencies"},
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
