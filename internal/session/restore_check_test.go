package session

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCheckedRestoreMatchesRestore checks that a start from a checked
// project, whose restore takes the network preparation of the checks, saves
// the state of a start from the same project with no checks, before and
// after 30 simulated seconds. The cases are a physical restore, a logical
// restore after the physical tier failed, and a saved traffic demo that the
// session rejects after its restore. The epoch of the rejected case is
// random, and the save time is the clock, so the comparison leaves them out.
func TestCheckedRestoreMatchesRestore(t *testing.T) {
	t.Parallel()
	stored, demo := newStoredRun(t), newDemoRun(t)
	withDemand := project.Default()
	withDemand.Demand.Enabled = true
	changed := project.Default()
	changed.Demand.PerMinute++
	for _, test := range []struct {
		name        string
		data        []byte
		config      project.Config
		wantRestore RestoreInfo
	}{
		{name: "physical", data: stored.data, config: withDemand, wantRestore: RestoreInfo{Tier: "physical"}},
		{
			name: "physical fallback", data: stored.edited(t, sharedBerth), config: withDemand,
			wantRestore: RestoreInfo{Tier: "logical", Reason: reasonPhysicalFailed},
		},
		{name: "rejected demo", data: demo.data, config: changed, wantRestore: RestoreInfo{Tier: restoreEmpty, Reason: reasonProjectChanged}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checked, err := CheckProject(test.config)
			if err != nil {
				t.Fatal(err)
			}
			var saves [2][][]byte
			for index, input := range []StoreInput{{Project: &test.config}, {Checked: &checked}} {
				store := &fakeStore{data: test.data}
				input.Store, input.Options = store, []Option{WithBuildID(testBuildID)}
				s := startFromStore(t, input)
				if restore := s.State().Restore; restore.Tier != test.wantRestore.Tier || restore.Reason != test.wantRestore.Reason {
					t.Fatalf("restore %+v, want tier and reason of %+v", restore, test.wantRestore)
				}
				advanceTicks(s, 30*sim.TicksPerSecond)
				s.Close()
				if err := s.SaveState(t.Context(), SaveFinal); err != nil {
					t.Fatal(err)
				}
				saves[index] = store.writeList()
			}
			if len(saves[0]) != len(saves[1]) {
				t.Fatalf("%d and %d saves", len(saves[0]), len(saves[1]))
			}
			for index := range saves[0] {
				if !bytes.Equal(comparableSave(t, saves[0][index]), comparableSave(t, saves[1][index])) {
					t.Fatalf("save %d differs", index)
				}
			}
		})
	}
}

// comparableSave returns data with no save time and no epoch.
func comparableSave(t *testing.T, data []byte) []byte {
	t.Helper()
	file := decodeTestState(t, data)
	file.SavedAt, file.Epoch = time.Time{}, ""
	return encodeTestState(t, file)
}

// TestSavedProjectRestoreTakesNoCheck pins that a restore of the saved
// project, with no project of the caller, does not take the checks of the
// caller: they hold another network. It does not run in parallel, because
// the count is for the process.
func TestSavedProjectRestoreTakesNoCheck(t *testing.T) {
	run := newOrdinaryRun(t)
	other := sim.NewNetworkCheck(scenarios.Small().Network, sim.LaneClearance{})
	if err := other.Err(); err != nil {
		t.Fatal(err)
	}
	plain, checked := newTestSession(t), newTestSession(t)
	before := sim.PreparationsForTest()
	want, err := plain.loadState(loadInput{data: run.data, steps: realRestoreSteps()})
	if err != nil {
		t.Fatal(err)
	}
	wantRuns := sim.PreparationsForTest() - before
	before = sim.PreparationsForTest()
	got, err := checked.loadState(loadInput{data: run.data, check: other, steps: realRestoreSteps()})
	if err != nil {
		t.Fatal(err)
	}
	if runs := sim.PreparationsForTest() - before; runs != wantRuns {
		t.Errorf("the restore prepares the network %d times, want %d", runs, wantRuns)
	}
	if !reflect.DeepEqual(got.simulation.ExportState(), want.simulation.ExportState()) {
		t.Error("the restored state differs from the restore with no checks")
	}
}
