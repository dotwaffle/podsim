package session

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestIncidentMarkerDigestTrailer checks that the incident marker is the
// digest extension N 1. A command with a marked project hashes the input
// of the same command without the marker, and then the trailer of the
// marker. This holds also for a command of another action that carries a
// marked project, because Apply computes the digest before it drops the
// project. A command without the marker writes no trailer, so the digest
// baseline does not change.
func TestIncidentMarkerDigestTrailer(t *testing.T) {
	t.Parallel()
	marker := string(sim.IncidentV1Contract)
	trailer := slices.Concat(binary.AppendUvarint(nil, 1), binary.AppendUvarint(nil, 1),
		binary.AppendVarint(nil, int64(len(marker))), []byte(marker))
	for _, action := range []string{"project", "pause"} {
		plain, marked := project.Default(), project.Default()
		marked.IncidentContract = sim.IncidentV1Contract
		base := Command{Action: action, Client: "digest", Sequence: 1, Epoch: testStateEpoch, ProjectRevision: 1, Project: &plain}
		withMarker := base
		withMarker.Project = &marked
		plainInput, markedInput := digestInput(base), digestInput(withMarker)
		if !bytes.Equal(markedInput, slices.Concat(plainInput, trailer)) {
			t.Fatalf("%s: marked input is not the plain input and the marker trailer", action)
		}
		if digestCommand(base).sum == digestCommand(withMarker).sum {
			t.Fatalf("%s: the marker did not change the digest", action)
		}
	}
}
