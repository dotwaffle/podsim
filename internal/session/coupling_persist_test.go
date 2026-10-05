package session

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCouplingStorePhaseRestores(t *testing.T) {
	data := couplingPhaseFixtures(t)
	for _, frame := range data.Frames {
		t.Run(frame.Name, func(t *testing.T) {
			input := couplingPhaseInput(t, data, frame)
			store := &fakeStore{data: encodeTestState(t, couplingPhaseFile(t, input))}
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			if err != nil || s == nil {
				t.Fatal("physical saved-phase restore failed", err)
			}
			t.Cleanup(s.Close)
			if s.restore.Tier != "physical" || !reflect.DeepEqual(s.simulation.ExportState(), input.State) {
				t.Fatal("startup lost group, cabin, route, or phase state")
			}
			if !slices.Equal(store.callList(), []string{"read", "write"}) || store.lastWrite(t).Version != stateVersion {
				t.Fatal("startup did not write version 9", store.callList())
			}
			// A second startup without a periodic or final save cannot use
			// logical recovery for committed groups. The file moves aside.
			second := &fakeStore{data: store.data}
			restarted, err := NewFromStore(t.Context(), StoreInput{Store: second})
			assertMovedAside(t, restarted, err, second, reasonInvalidState)
		})
	}
}

// assertMovedAside checks that a restart moved the saved state aside for
// reason and started a new session that saves.
func assertMovedAside(t *testing.T, s *Session, err error, store *fakeStore, reason string) {
	t.Helper()
	if err != nil || s == nil {
		t.Fatal("rejected state stopped the start", err)
	}
	t.Cleanup(s.Close)
	if !slices.Equal(store.callList(), []string{"read", "reject", "write"}) || s.restore.Reason != reason {
		t.Fatalf("rejection calls %v, reason %q, want %q", store.callList(), s.restore.Reason, reason)
	}
}

// assertPreserved checks that a restart kept the saved state, turned
// saving off and failed.
func assertPreserved(t *testing.T, s *Session, err error, store *fakeStore, before []byte) {
	t.Helper()
	if err == nil || s != nil {
		t.Fatal("preserved state returned a session", err)
	}
	if _, preserved := errors.AsType[*preservedStateError](err); !preserved {
		t.Fatal("failure lost preservation classification", err)
	}
	if !bytes.Equal(before, store.data) || !slices.Equal(store.callList(), []string{"read"}) {
		t.Fatal("preserved state was archived or overwritten", store.callList())
	}
}

// TestCouplingRestoreResultMovedAside checks that a restore of committed
// groups that loses or changes state moves the file aside.
func TestCouplingRestoreResultMovedAside(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	file := couplingPhaseFile(t, input)
	for _, test := range []struct {
		name string
		edit func(*sim.RestoreResult)
	}{
		{"logical", func(r *sim.RestoreResult) { r.Tier = sim.RestoreLogical }},
		{"physical error", func(r *sim.RestoreResult) { r.PhysicalError = errors.New("partial restore") }},
		{"demoted", func(r *sim.RestoreResult) { r.Demoted = []string{"front"} }},
		{"requeued", func(r *sim.RestoreResult) { r.Requeued = []int{1} }},
		{"dropped", func(r *sim.RestoreResult) { r.Dropped = []int{1} }},
		{"logical completion", func(r *sim.RestoreResult) { r.LogicalCompleted = []int{1} }},
		{"lost party", func(r *sim.RestoreResult) { r.DroppedParties = 1 }},
		{"unaccounted", func(r *sim.RestoreResult) { r.Unaccounted = 1 }},
		{"over cap", func(r *sim.RestoreResult) { r.OverCap = 1 }},
		{"over budget", func(r *sim.RestoreResult) { r.OverBudget = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{data: encodeTestState(t, file)}
			steps := realRestoreSteps()
			steps.restoreSimulation = func(i sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
				s, result, err := sim.RestoreState(i)
				if err != nil {
					return s, result, err
				}
				test.edit(&result)
				return s, result, nil
			}
			s, err := newFromStore(t.Context(), StoreInput{Store: store}, steps)
			assertMovedAside(t, s, err, store, reasonInvalidState)
		})
	}
}

func TestCouplingStoreOpaqueReadTooLarge(t *testing.T) {
	t.Parallel()
	store := &fakeStore{data: []byte("opaque original bytes"), readErr: fmt.Errorf("bounded read: %w", ErrStateTooLarge)}
	before := bytes.Clone(store.data)
	s, err := NewFromStore(t.Context(), StoreInput{Store: store})
	assertPreserved(t, s, err, store, before)
	if !errors.Is(err, ErrStateTooLarge) {
		t.Fatal("lost original read-size error", err)
	}
}

// TestCouplingProjectEnabledRestoreOverride restores a coupling file with
// a project file that differs from the saved project. Only couplingEnabled
// can differ. A project file with other coupling geometry moves the saved
// state aside as project_changed. A project file that is not valid stops
// the start before the saved state moves.
func TestCouplingProjectEnabledRestoreOverride(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	file := couplingPhaseFile(t, input)
	for _, change := range []struct {
		name   string
		edit   func(*project.Config)
		reason string
	}{
		{"enabled", func(c *project.Config) { c.CouplingEnabled = !c.CouplingEnabled }, ""},
		{"marker", func(c *project.Config) { c.CouplingContract = "unknown" }, "invalid project"},
		{"site", func(c *project.Config) { c.CouplingSites[0].StartMeters++ }, reasonProjectChanged},
		{"path", func(c *project.Config) { c.CouplingCorridors[0].LaneIDs[0] = "unknown" }, "invalid project"},
	} {
		t.Run(change.name, func(t *testing.T) {
			config := project.Clone(file.Project)
			change.edit(&config)
			store := &fakeStore{data: encodeTestState(t, file)}
			before := bytes.Clone(store.data)
			s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
			switch change.reason {
			case "":
				if err != nil || s == nil || s.project.CouplingEnabled != config.CouplingEnabled || s.simulation.CouplingEnabled() != config.CouplingEnabled || len(s.simulation.ExportState().CouplingGroups) != 1 {
					t.Fatal("enabled-only change lost group", err)
				}
				t.Cleanup(s.Close)
			case "invalid project":
				if err == nil || s != nil || !bytes.Equal(before, store.data) || !slices.Equal(store.callList(), []string{"read"}) {
					t.Fatal("invalid project file did not stop the start before the state moved", err, store.callList())
				}
			default:
				assertMovedAside(t, s, err, store, change.reason)
			}
		})
	}
}

func TestCouplingStoreGroupFreeRecovery(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.CouplingContract = sim.CompactPairV1CouplingContract
	store := &fakeStore{}
	s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	file := store.lastWrite(t)
	file.RestoreAttempts = 1
	store = &fakeStore{data: encodeTestState(t, file)}
	restored, err := NewFromStore(t.Context(), StoreInput{Store: store})
	if err != nil || restored == nil || restored.restore.Tier != "logical" {
		t.Fatal("proven group-free save lost ordinary recovery", err)
	}
	t.Cleanup(restored.Close)
	if store.lastWrite(t).Version != stateVersion {
		t.Fatal("group-free save changed version")
	}
}

func TestCouplingStoreGroupFreePanic(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.CouplingContract = sim.CompactPairV1CouplingContract
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	file := sessionStateFile(t, s)
	file.Version, file.CouplingContract, file.RestoreAttempts = stateVersion, config.CouplingContract, 0
	store := &fakeStore{data: encodeTestState(t, file)}
	decoded, err := decodeStateFile(store.data)
	if err != nil || decoded.Version != stateVersion || len(decoded.Simulation.CouplingGroups) != 0 {
		t.Fatal("control did not prove complete group-free decode", err)
	}
	steps := realRestoreSteps()
	steps.restoreSimulation = func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
		panic("proven group-free restore")
	}
	recovered, err := newFromStore(t.Context(), StoreInput{Store: store}, steps)
	if err != nil || recovered == nil {
		t.Fatal("proven group-free panic lost ordinary recovery", err)
	}
	t.Cleanup(recovered.Close)
	if !slices.Equal(store.callList(), []string{"read", "reject", "write"}) {
		t.Fatal("proven group-free panic changed store recovery", store.callList())
	}
}

func TestCouplingStoreMalformedGroupsMovedAside(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	file := couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))
	raw := mutateCouplingGroup(t, decompressTestJSON(t, encodeTestState(t, file)), func(g map[string]jsontext.Value) { delete(g, "formationTick") })
	store := &fakeStore{data: compressTestJSON(t, raw)}
	s, err := NewFromStore(context.Background(), StoreInput{Store: store})
	assertMovedAside(t, s, err, store, reasonInvalidState)
}

func TestCouplingProjectApplyAtomic(t *testing.T) {
	t.Parallel()
	saves := 0
	s, err := NewWithProject(project.Default(), WithProjectSaver(func(c project.Config) error {
		saves++
		if c.Version != project.CurrentVersion || c.CouplingContract != sim.CompactPairV1CouplingContract {
			return errors.New("project contract changed")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	client := newTestClient(s, "coupling-apply")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	config := project.Default()
	config.CouplingContract, config.CouplingEnabled = sim.CompactPairV1CouplingContract, true
	client.mustApply(t, Command{Action: "project", Project: &config, ProjectRevision: s.projectRevision})
	if saves != 1 || s.simulation.CouplingContract() != config.CouplingContract || !s.simulation.CouplingEnabled() {
		t.Fatal("project apply lost native coupling contract")
	}
	before := s.State()
	bad := project.Clone(config)
	bad.CouplingSites = []sim.CouplingSite{{ID: "unpaired", LaneID: "unknown"}}
	reply := s.Apply(client.next(Command{Action: "project", Project: &bad, ProjectRevision: s.projectRevision}))
	if reply.Error == "" || saves != 1 || !reflect.DeepEqual(before, s.State()) {
		t.Fatal("invalid geometry saved or installed a partial project")
	}
}
