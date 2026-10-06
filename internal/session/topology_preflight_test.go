package session

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestTopologyPreflightAtCallers sends a valid plain project whose
// topology, as the preflight measures it, is at the topology cap, and one
// whose topology is one byte over it, to each point that installs a
// project: session creation, project replace and the startup restore.
// project.Validate measures the project without HTML escapes, but the
// topology escapes each "<" as 6 bytes, so only the topology preflight
// refuses the second project.
func TestTopologyPreflightAtCallers(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the cap projects run without -short and without the race detector")
	}
	const refusal = "topology exceeds supported limit"
	atCap := escapedTopologyProject(t, project.MaxFileBytes+4096)
	over := escapedTopologyProject(t, project.MaxFileBytes+4096+1)

	t.Run("session creation", func(t *testing.T) {
		// A demand change increases the project revision without a
		// preflight. The preflight measured the largest revision, so the
		// session serves its HTTP state at each revision.
		s, err := NewWithProject(atCap)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		client := newTestClient(s, "demand")
		for {
			if _, decodeErr := DecodeStateJSON(stateHTTPReply(t, s, atCap, http.StatusOK)); decodeErr != nil {
				t.Fatal("the client refused the HTTP state at the topology cap", decodeErr)
			}
			if s.Topology().ProjectRevision == 10 {
				break
			}
			client.mustApply(t, Command{Action: "demand", Demand: atCap.Demand})
		}
		s, err = NewWithProject(over)
		if s != nil {
			s.Close()
		}
		wantError(t, err, refusal)
	})

	t.Run("project replace", func(t *testing.T) {
		s, err := NewWithProject(project.Default())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		client := newTestClient(s, "editor")
		client.mustApply(t, Command{Action: "pause", Paused: true})
		reply := s.Apply(client.next(Command{Action: "project", Project: &over, ProjectRevision: 1}))
		if reply.ErrorCode != CommandRejected || reply.Error != refusal {
			t.Fatalf("project replace got %q %q, want %q %q", reply.ErrorCode, reply.Error, CommandRejected, refusal)
		}
		if s.project.Name != project.Default().Name || s.Topology().ProjectRevision != 1 {
			t.Fatalf("the refused replace changed the session to %q at revision %d", s.project.Name, s.Topology().ProjectRevision)
		}
		client.mustApply(t, Command{Action: "project", Project: &atCap, ProjectRevision: 1})
		if _, err := DecodeStateJSON(stateHTTPReply(t, s, atCap, http.StatusOK)); err != nil {
			t.Fatal("the client refused the HTTP state at the topology cap", err)
		}
	})

	t.Run("restore", func(t *testing.T) {
		// The stored run has an epoch of 26 characters, so the saved
		// project has a topology of one byte over the cap.
		run := newStoredRun(t)
		if len(run.file.Epoch) != 26 {
			t.Fatalf("the stored run has epoch %q", run.file.Epoch)
		}
		data := run.edited(t, func(file *stateFile) { file.Project = project.Clone(over) })

		// Without a project file, the server moves the saved state aside
		// and starts the example project.
		store := &fakeStore{data: data}
		s, err := newFromStore(t.Context(), StoreInput{Store: store}, realRestoreSteps())
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		restore := s.State().Restore
		if restore.Tier != restoreEmpty || restore.Reason != reasonInvalidState {
			t.Fatalf("restore = %+v, want tier %q and reason %q", restore, restoreEmpty, reasonInvalidState)
		}
		if same, sameErr := sameProject(s.project, project.Default()); sameErr != nil || !same {
			t.Fatalf("the session has project %q, want the example project", s.project.Name)
		}
		if calls := store.callList(); !slices.Equal(calls, []string{"read", "reject", "write"}) {
			t.Fatalf("store calls %v, want read, reject and write", calls)
		}
		_, err = newSession(nil, nil).loadState(loadInput{data: data, steps: realRestoreSteps()})
		wantError(t, err, reasonInvalidState+": "+refusal)

		// With the same project as the project file, the server refuses
		// the project file as session creation does, and it keeps the
		// saved state.
		store = &fakeStore{data: data}
		s, err = newFromStore(t.Context(), StoreInput{Store: store, Project: new(project.Clone(over))}, realRestoreSteps())
		if s != nil {
			s.Close()
		}
		wantError(t, err, refusal)
		if calls := store.callList(); !slices.Equal(calls, []string{"read"}) {
			t.Fatalf("store calls %v, want only the read", calls)
		}
	})

	t.Run("epoch width", func(t *testing.T) {
		// A restore keeps the saved epoch or makes a new epoch of 26
		// characters, so the preflight measures the wider of the two.
		server := strings.Repeat("0", 16)
		if err := preflightTopology(atCap, server, "E"); err != nil {
			t.Fatal("refused a short epoch at the cap", err)
		}
		wantError(t, preflightTopology(over, server, strings.Repeat("E", 25)), refusal)
		wantError(t, preflightTopology(atCap, server, strings.Repeat("E", 27)), refusal)
		wantError(t, preflightTopology(atCap, server, strings.Repeat("<", 5)), refusal)
	})

	t.Run("Express", func(t *testing.T) {
		// The marker adds bytes, so this topology is also over the cap.
		marked := project.Config{Version: project.CurrentVersion, OrderContract: sim.ExpressOrderContract, Network: over.Network}
		wantError(t, preflightTopology(marked, strings.Repeat("0", 16), strings.Repeat("0", 26)), refusal)
	})
}
