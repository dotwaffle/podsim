package session

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// newOrdinaryRun runs the example project with no demand and closes the
// session. It returns the final saved state.
func newOrdinaryRun(t *testing.T) storedRun {
	t.Helper()
	config := project.Default()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	advanceTicks(s, sim.TicksPerSecond)
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	writes := store.writeList()
	data := writes[len(writes)-1]
	return storedRun{session: s, data: data, file: decodeTestState(t, data)}
}

// setPod gives pod index of file the holds and the purpose, and returns the
// edit.
func setPod(index int, hold, purpose, owner uint8) func(*stateFile) {
	return func(file *stateFile) {
		file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
		pod := &file.Simulation.Pods[index]
		pod.Withdrawn, pod.Purpose, pod.Owner = hold, purpose, owner
	}
}

// TestRestorePolicyMovesAside checks that a save with a hold that has no
// owner, and a demo save with a hold, a purpose, a record, or a counter, is
// invalid_state in both tiers, moves aside, and starts a new session. The
// text and the order of the refusals are part of the format.
func TestRestorePolicyMovesAside(t *testing.T) {
	t.Parallel()
	const (
		refuge   = "saved pod 01 has a refuge"
		recovery = "saved pod 01 has an empty recovery with the emergency owner"
		demoPod  = "saved demo pod 01 has a service hold or an operational purpose"
		demoRest = "saved demo fleet has fault or emergency state"
	)
	faults := &sim.SavedFaults{Counters: sim.FaultCounters{Started: 1}}
	emergencies := &sim.SavedEmergencies{Counters: sim.EmergencyCounters{Started: 1}}
	record := &sim.SavedFaults{Records: []sim.SavedFault{{Generation: 1, Serial: 1, Pod: 0}}}
	emergencyRecord := &sim.SavedEmergencies{Records: []sim.SavedEmergency{{Generation: 1, Serial: 1, Pod: 0, Order: 1}}}
	ordinary, demo := newOrdinaryRun(t), newDemoRun(t)
	ended := func(edit func(*stateFile)) func(*stateFile) {
		return func(file *stateFile) {
			file.Simulation.Demo = nil
			if edit != nil {
				edit(file)
			}
		}
	}
	both := func(edits ...func(*stateFile)) func(*stateFile) {
		return func(file *stateFile) {
			for _, edit := range edits {
				edit(file)
			}
		}
	}
	setFaults := func(file *stateFile) { file.Simulation.Faults = faults }
	setEmergencies := func(file *stateFile) { file.Simulation.Emergencies = emergencies }
	setRecord := func(file *stateFile) { file.Simulation.Faults = record; file.Simulation.IncidentSerial = 1 }
	setEmergencyRecord := func(file *stateFile) {
		file.Simulation.Emergencies = emergencyRecord
		file.Simulation.IncidentSerial = 1
	}
	for _, test := range []struct {
		name string
		run  storedRun
		edit func(*stateFile)
		want string
	}{
		{"refuge, fault owner", ordinary, setPod(0, 1, 2, 1), refuge},
		{"refuge, emergency owner", ordinary, setPod(0, 2, 2, 2), refuge},
		{"recovery, emergency owner", ordinary, setPod(0, 2, 3, 2), recovery},
		{"recovery, emergency owner, both holds", ordinary, setPod(0, 3, 3, 2), recovery},
		{"recovery before a later refuge", ordinary, both(setPod(0, 2, 3, 2), setPod(1, 1, 2, 1)), recovery},
		{"refuge before E2", ordinary, both(setPod(1, 2, 0, 0), setPod(0, 1, 2, 1)), refuge},
		{"demo hold", demo, setPod(0, 1, 0, 0), demoPod},
		{"demo emergency hold", demo, setPod(0, 2, 0, 0), demoPod},
		{"demo purpose", demo, setPod(0, 1, 3, 1), demoPod},
		{"demo refuge before the demo rules", demo, both(setPod(0, 1, 2, 1), setFaults), refuge},
		{"demo hold before a demo counter", demo, both(setPod(0, 1, 0, 0), setFaults), demoPod},
		{"demo fault record", demo, setRecord, demoRest},
		{"demo emergency record", demo, setEmergencyRecord, demoRest},
		{"demo fault counter", demo, setFaults, demoRest},
		{"demo emergency counter", demo, setEmergencies, demoRest},
		{"ended demo hold", demo, ended(setPod(0, 1, 0, 0)), demoPod},
		{"ended demo fault counter", demo, ended(setFaults), demoRest},
		{"ended demo emergency counter", demo, ended(setEmergencies), demoRest},
	} {
		for _, logical := range []bool{false, true} {
			name := test.name + " physical"
			if logical {
				name = test.name + " logical"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				data := test.run.edited(t, func(file *stateFile) {
					test.edit(file)
					if logical {
						file.RestoreAttempts = restoreLoopAttempts - 1
					}
				})
				_, err := newTestSession(t).loadState(loadInput{data: data, steps: realRestoreSteps()})
				if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("load: %v, want %q", err, test.want)
				}
				store := &fakeStore{data: data}
				s, err := NewFromStore(t.Context(), StoreInput{Store: store})
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if s.restore.Tier != restoreEmpty || s.restore.Reason != reasonInvalidState ||
					!slices.Equal(store.callList(), []string{"read", "reject", "write"}) {
					t.Fatalf("restore %+v, store calls %v", s.restore, store.callList())
				}
			})
		}
	}
}

// TestRestorePolicyRunsFirst checks that the policy step runs before the
// simulation restore and that a refused save never reaches it.
func TestRestorePolicyRunsFirst(t *testing.T) {
	t.Parallel()
	data := newOrdinaryRun(t).edited(t, setPod(0, 1, 2, 1))
	steps := realRestoreSteps()
	steps.restoreSimulation = func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
		t.Error("the restore ran after a refusal of the policy")
		return nil, sim.RestoreResult{}, nil
	}
	if _, err := newTestSession(t).loadState(loadInput{data: data, steps: steps}); err == nil {
		t.Fatal("the load accepted the save")
	}
}

// TestRestoreSwitchesFromSession checks the input of the simulation restore:
// the switches are on for each save, also of a demo fleet, and the settings
// are the effective settings of the project. The session turns the switches
// off for a demo fleet after the restore.
func TestRestoreSwitchesFromSession(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]storedRun{"ordinary": newOrdinaryRun(t), "demo": newDemoRun(t)} {
		steps := realRestoreSteps()
		var input sim.RestoreStateInput
		steps.restoreSimulation = func(got sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
			input = got
			return sim.RestoreState(got)
		}
		loaded, err := newTestSession(t).loadState(loadInput{data: run.data, steps: steps})
		if err != nil {
			t.Fatal(name, err)
		}
		if !input.FaultsOn || !input.EmergenciesOn || input.Faults != project.EffectiveFaultSettings(loaded.config) {
			t.Errorf("%s: faults on %t, emergencies on %t, settings %+v", name, input.FaultsOn, input.EmergenciesOn, input.Faults)
		}
		if off := loaded.simulation.Snapshot().OperationsOff; off != (name == "demo") {
			t.Errorf("%s: operationsOff %t after the restore", name, off)
		}
	}
}

// TestRestoredOrphanFaultHoldReleases restores an ordinary save with a fault
// hold and no fault. The physical tier keeps the hold, and the next tick
// releases it. The logical tier releases each such hold in the restore.
func TestRestoredOrphanFaultHoldReleases(t *testing.T) {
	t.Parallel()
	run := newOrdinaryRun(t)
	for _, test := range []struct {
		name string
		edit func(*stateFile)
		tier string
		hold uint8
	}{
		{"physical", setPod(0, 1, 0, 0), "physical", 1},
		{"logical", func(file *stateFile) {
			setPod(0, 1, 0, 0)(file)
			logicalOnly(file)
		}, "logical", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := startFromStore(t, StoreInput{Store: &fakeStore{data: run.edited(t, test.edit)}})
			t.Cleanup(s.Close)
			if s.restore.Tier != test.tier {
				t.Fatalf("restore %+v", s.restore)
			}
			if got := s.simulation.ExportState().Pods[0].Withdrawn; got != test.hold {
				t.Fatalf("restored holds %d, want %d", got, test.hold)
			}
			s.step()
			if got := s.simulation.ExportState().Pods[0].Withdrawn; got != 0 {
				t.Fatalf("holds %d after a tick", got)
			}
		})
	}
}

// TestSavedDecodeRefusalOrder pins the order of the save decode with inputs
// that have more than one error. A removed marker reaches the strict decode,
// so a scan before it wins and each member check after it loses.
func TestSavedDecodeRefusalOrder(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	raw := decompressTestJSON(t, encodeTestState(t, file))
	const (
		at      = `"simulation":{`
		marker  = `"faultContract":"fault-v1",`
		express = `"orderContract":"express-v1",`
	)
	badPacked := `"pending":[{"from":"Z2FyZGV"}],`
	packedRefusal := scanPackedOrders(bytes.Replace(raw, []byte(at), []byte(at+badPacked), 1))
	if packedRefusal == nil {
		t.Fatal("the packed scan accepts the origin")
	}
	for _, test := range []struct{ name, root, insert, want string }{
		{"packed scan before the marker", "", marker + badPacked, packedRefusal.Error()},
		{"marker before a null incident member", "", marker + `"interrupted":null,`, `unknown object member name "faultContract"`},
		{"marker before a null fault member", "", marker + `"faults":null,`, `unknown object member name "faultContract"`},
		{"incident null before fault null", "", `"faults":null,"interrupted":null,`, "saved incident member /simulation/interrupted is null"},
		{"fault null before emergency null", "", `"emergencies":null,"faults":null,`, "saved member /simulation/faults is null"},
		{"emergency null before the order contract", express, `"emergencies":null,`, "saved member /simulation/emergencies is null"},
		{"order contract before empty faults", express, `"faults":{},`, "saved order contract markers disagree"},
		{"empty faults before empty emergencies", "", `"emergencies":{},"faults":{},`, "saved faults member is empty"},
		{"empty emergencies", "", `"emergencies":{},`, "saved emergencies member is empty"},
	} {
		edited := bytes.Replace(raw, []byte(at), []byte(at+test.insert), 1)
		edited = append([]byte("{"+test.root), edited[1:]...)
		_, err := decodeStateFile(compressTestJSON(t, edited))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want %q", test.name, err, test.want)
		}
	}
}
