package session

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCouplingProtectedLateMarkers(t *testing.T) {
	prefix := `{"padding":[` + strings.Repeat(`null,`, 65_536) + `null],`
	for _, test := range []struct {
		name, suffix string
		protected    bool
	}{
		{"late version", `"format":"podsim-session","version":8}`, true},
		{"late marker", `"format":"podsim-session","version":2,"couplingContract":null}`, true},
		{"late native marker", `"format":"podsim-session","version":2,"simulation":{"couplingGroups":null}}`, true},
		{"folded native marker", `"format":"podsim-session","version":2,"simulation":{"COUPLINGGROUPS":null}}`, false},
		{"unmarked legacy", `"format":"podsim-session","version":2}`, false},
		{"unsupported ancestor", `"format":"podsim-session","version":2,"other":{"couplingContract":null}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{data: compressTestJSON(t, []byte(prefix+test.suffix))}
			before := bytes.Clone(store.data)
			_, decodeErr := decodeStateFile(before)
			if !errors.Is(decodeErr, errJSONArrayTooLong) {
				t.Fatal("changed first validation error", decodeErr)
			}
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			if test.protected {
				assertCouplingPreserved(t, s, err, store, before)
				return
			}
			if err != nil || s == nil {
				t.Fatal("ordinary unmarked rejection changed", err)
			}
			t.Cleanup(s.Close)
			if !slices.Equal(store.callList(), []string{"read", "reject", "write"}) {
				t.Fatal("ordinary unmarked rejection changed store calls", store.callList())
			}
		})
	}
	for _, test := range []struct{ name, raw string }{
		{"opaque depth", `{"padding":` + strings.Repeat(`[`, 65) + `null` + strings.Repeat(`]`, 65) + `,"format":"podsim-session","version":8}`},
		{"unknown version", `{"couplingContract":"compact-pair-v1","format":"podsim-session","version":9}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{data: compressTestJSON(t, []byte(test.raw))}
			before := bytes.Clone(store.data)
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			assertCouplingPreserved(t, s, err, store, before)
		})
	}
}

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
			if !slices.Equal(store.callList(), []string{"read", "write"}) || store.lastWrite(t).Version != couplingStateVersion {
				t.Fatal("startup did not preserve save8", store.callList())
			}
			// A second startup without a periodic/final save cannot choose logical recovery.
			before := bytes.Clone(store.data)
			second, err := NewFromStore(t.Context(), StoreInput{Store: store})
			if err == nil || second != nil || !bytes.Equal(before, store.data) || !slices.Equal(store.callList(), []string{"read", "write", "read"}) {
				t.Fatal("committed state entered logical fallback or startup replacement", err)
			}
		})
	}
}

func TestCouplingProtectedStoreFailures(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	base := couplingPhaseFile(t, input)
	for _, test := range []struct {
		name         string
		edit         func(*stateFile)
		panicRestore bool
	}{
		{"attempt one", func(f *stateFile) { f.RestoreAttempts = 1 }, false},
		{"attempt limit", func(f *stateFile) { f.RestoreAttempts = restoreLoopAttempts }, false},
		{"invalid speed", func(f *stateFile) { f.Speed = 3 }, false},
		{"native invalid phase", func(f *stateFile) { f.Simulation.CouplingGroups[0].Phase = "unknown" }, false},
		{"native panic", func(*stateFile) {}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := base
			file.Simulation.CouplingGroups = slices.Clone(file.Simulation.CouplingGroups)
			test.edit(&file)
			store := &fakeStore{data: encodeTestState(t, file)}
			before := bytes.Clone(store.data)
			steps := realRestoreSteps()
			if test.panicRestore {
				steps.restoreSimulation = func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
					panic("protected restore test")
				}
			}
			s, err := newFromStore(t.Context(), StoreInput{Store: store}, steps)
			assertCouplingPreserved(t, s, err, store, before)
		})
	}
	for _, name := range []string{"gzip truncated", "gzip checksum"} {
		t.Run(name, func(t *testing.T) {
			encoded := bytes.Clone(encodeTestState(t, base))
			if name == "gzip truncated" {
				encoded = encoded[:len(encoded)-4]
			} else {
				encoded[len(encoded)-1] ^= 1
			}
			store := &fakeStore{data: encoded}
			before := bytes.Clone(store.data)
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			assertCouplingPreserved(t, s, err, store, before)
		})
	}

	for _, raw := range []string{
		`{"format":"podsim-session","version":8`,
		`{"couplingContract":"compact-pair-v1","format":"podsim-session","version":`,
		`{"format":"podsim-session","version":8,"simulation":{"couplingGroups":null}}`,
		`{"couplingContract":"compact-pair-v1","format":"podsim-session","version":null}`,
		`{"couplingContract":"compact-pair-v1","format":"podsim-session","version":"8"}`,
		`{"couplingContract":"compact-pair-v1","format":"podsim-session","version":{}}`,
		`{"couplingContract":"compact-pair-v1","format":"podsim-session","version":[]}`,
		`{"couplingContract":"compact-pair-v1","format":"podsim-session","version":2}`,
		`{"format":"podsim-session","version":2,"simulation":{"couplingGroups":[{}]}}`,
	} {
		t.Run("incomplete recognition="+raw, func(t *testing.T) {
			store := &fakeStore{data: compressTestJSON(t, []byte(raw))}
			before := bytes.Clone(store.data)
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			assertCouplingPreserved(t, s, err, store, before)
		})
	}
}

func assertCouplingPreserved(t *testing.T, s *Session, err error, store *fakeStore, before []byte) {
	t.Helper()
	if err == nil || s != nil {
		t.Fatal("protected failure returned a session", err)
	}
	if _, protected := errors.AsType[*preservedStateError](err); !protected {
		t.Fatal("failure lost preservation classification", err)
	}
	if !bytes.Equal(before, store.data) || !slices.Equal(store.callList(), []string{"read"}) {
		t.Fatal("protected failure archived or overwrote the input", store.callList())
	}
}

func TestCouplingProtectedRestoreResult(t *testing.T) {
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
			before := bytes.Clone(store.data)
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
			assertCouplingPreserved(t, s, err, store, before)
		})
	}
}

func TestCouplingStoreOpaqueReadTooLarge(t *testing.T) {
	store := &fakeStore{data: []byte("opaque original bytes"), readErr: fmt.Errorf("bounded read: %w", ErrStateTooLarge)}
	before := bytes.Clone(store.data)
	s, err := NewFromStore(t.Context(), StoreInput{Store: store})
	assertCouplingPreserved(t, s, err, store, before)
	if !errors.Is(err, ErrStateTooLarge) {
		t.Fatal("lost original read-size error", err)
	}
}

func TestCouplingProjectEnabledRestoreOverride(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	file := couplingPhaseFile(t, input)
	for _, change := range []struct {
		name  string
		edit  func(*project.Config)
		valid bool
	}{
		{"enabled", func(c *project.Config) { c.CouplingEnabled = !c.CouplingEnabled }, true},
		{"marker", func(c *project.Config) { c.CouplingContract = "unknown" }, false},
		{"site", func(c *project.Config) { c.CouplingSites[0].StartMeters++ }, false},
		{"path", func(c *project.Config) { c.CouplingCorridors[0].LaneIDs[0] = "unknown" }, false},
	} {
		t.Run(change.name, func(t *testing.T) {
			config := project.Clone(file.Project)
			change.edit(&config)
			store := &fakeStore{data: encodeTestState(t, file)}
			before := bytes.Clone(store.data)
			s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
			if change.valid {
				if err != nil || s == nil || s.project.CouplingEnabled != config.CouplingEnabled || s.simulation.CouplingEnabled() != config.CouplingEnabled || len(s.simulation.ExportState().CouplingGroups) != 1 {
					t.Fatal("enabled-only change lost group", err)
				}
				t.Cleanup(s.Close)
			} else {
				assertCouplingPreserved(t, s, err, store, before)
			}
		})
	}
}

func TestCouplingStoreGroupFreeRecovery(t *testing.T) {
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
		t.Fatal("proven group-free save8 lost ordinary recovery", err)
	}
	t.Cleanup(restored.Close)
	if store.lastWrite(t).Version != couplingStateVersion {
		t.Fatal("group-free save8 downgraded")
	}
}

func TestCouplingStoreGroupFreePanic(t *testing.T) {
	config := project.Default()
	config.CouplingContract = sim.CompactPairV1CouplingContract
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	file := sessionStateFile(t, s)
	file.Version, file.CouplingContract, file.RestoreAttempts = couplingStateVersion, config.CouplingContract, 0
	store := &fakeStore{data: encodeTestState(t, file)}
	decoded, err := decodeStateFile(store.data)
	if err != nil || decoded.Version != couplingStateVersion || decoded.protected || len(decoded.Simulation.CouplingGroups) != 0 {
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

func TestCouplingStoreMalformedGroupsPreserved(t *testing.T) {
	data := couplingPhaseFixtures(t)
	file := couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))
	raw := mutateCouplingGroup(t, decompressTestJSON(t, encodeTestState(t, file)), func(g map[string]jsontext.Value) { delete(g, "formationTick") })
	store := &fakeStore{data: compressTestJSON(t, raw)}
	before := bytes.Clone(store.data)
	s, err := NewFromStore(context.Background(), StoreInput{Store: store})
	assertCouplingPreserved(t, s, err, store, before)
}

func TestCouplingProjectApplyAtomic(t *testing.T) {
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
