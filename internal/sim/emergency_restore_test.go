package sim

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// emergencyRestoreInput returns the input that restores state, a save of
// s, with the incident and emergency markers, and with the fault marker
// and the fault settings of s when s has faults on.
func emergencyRestoreInput(s *Simulation, state SavedState) RestoreStateInput {
	input := RestoreStateInput{
		Network: s.network, Fleet: s.initial, State: state,
		IncidentContract: IncidentV1Contract, EmergencyContract: EmergencyV1Contract,
	}
	if s.faultsOn {
		input.FaultContract = FaultV1Contract
		input.Faults = FaultSettings{EvacuationSeconds: int(s.faultSettings.evacuationSeconds)}
	}
	return input
}

// emergencyRestorePhases puts pod 01 of emergencyFleet in each phase of a
// record with order 1 as the party: deferred in its arrival chain to s1,
// bound to s1, unloading at s0, and deferred with a fault in its arrival
// chain.
var emergencyRestorePhases = []struct {
	name    string
	prepare func(t *testing.T, s *Simulation, v *vehicle)
}{
	{"deferred", func(t *testing.T, s *Simulation, v *vehicle) {
		t.Helper()
		travelToArrivalChain(t, s, v, "s1")
		startEmergency(t, s, v, 1)
	}},
	{"bound", func(t *testing.T, s *Simulation, v *vehicle) {
		t.Helper()
		travelOn(t, s, v, "s0-link")
		boundEmergency(t, s, v)
	}},
	{"unloading", func(t *testing.T, s *Simulation, v *vehicle) {
		t.Helper()
		startEmergency(t, s, v, 1)
		s.Step()
	}},
	{"faulted", func(t *testing.T, s *Simulation, v *vehicle) {
		t.Helper()
		s.faultsOn = true
		s.faultSettings.evacuationSeconds = 300
		travelToArrivalChain(t, s, v, "s1")
		startEmergency(t, s, v, 1)
		startFault(t, s, v, 0)
		s.Step()
	}},
}

// emergencyPhaseSave puts pod 01 of emergencyFleet in the phase, and
// returns the simulation, the pod, and the save.
func emergencyPhaseSave(t *testing.T, phase int) (*Simulation, *vehicle, SavedState) {
	t.Helper()
	s, v := emergencyFleet(t)
	checkEmergenciesEachTick(t, s)
	emergencyRestorePhases[phase].prepare(t, s, v)
	state := s.ExportState()
	return s, v, state
}

// TestEmergencyPhysicalRestore checks the physical restore of each phase
// (section 10.7 of the incident emergency contract). The save holds the
// record and the counters. The restore keeps the records, the counters,
// the purposes, and the holds, and the restored state saves the same
// state. The restored simulation continues the emergency: the pod
// unloads, the party is interrupted, and the record ends with the hold.
func TestEmergencyPhysicalRestore(t *testing.T) {
	t.Parallel()
	for phase, test := range emergencyRestorePhases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v, state := emergencyPhaseSave(t, phase)
			record := s.emergencies[0]
			want := []SavedEmergency{{Generation: record.generation, Serial: record.serial, Start: record.start, Pod: s.vehicleIndex(v), Order: 1}}
			if state.Emergencies == nil || !slices.Equal(state.Emergencies.Records, want) || state.Emergencies.Counters != s.emergencyCounters.exported() {
				t.Fatalf("saved emergencies %+v, want the records %+v", state.Emergencies, want)
			}
			restored, result, err := RestoreState(emergencyRestoreInput(s, state))
			if err != nil || !cleanRestore(result) {
				t.Fatalf("restore %v, %+v", err, result)
			}
			if !restored.emergenciesOn || !slices.Equal(restored.emergencies, s.emergencies) || restored.emergencyCounters != s.emergencyCounters ||
				!reflect.DeepEqual(restored.ExportState(), state) {
				t.Fatalf("the restore changed the emergencies: %+v, counters %+v", restored.emergencies, restored.emergencyCounters)
			}
			checkEmergenciesEachTick(t, restored)
			if len(restored.faults) != 0 {
				if err := restored.clearFault(restored.faults[0].id()); err != nil {
					t.Fatal(err)
				}
			}
			stepUntil(t, restored, "the end of the emergency", func() bool { return len(restored.emergencies) == 0 })
			if pod := restored.findVehicle(v.Pod.ID); !slices.Contains(restored.undelivered, 1) || pod.withdrawn != 0 {
				t.Fatalf("interrupted %v, holds %#x", restored.undelivered, pod.withdrawn)
			}
		})
	}
}

// TestEmergencyLogicalRestore checks the logical restore of each phase
// (section 10.7 of the incident emergency contract). Every record ends,
// DroppedEmergencies counts it, the counters stay, and no pod keeps the
// emergency hold. The stage 1 rules handle the purposes: the party of an
// emergency unload is interrupted, and a deferred party is requeued.
func TestEmergencyLogicalRestore(t *testing.T) {
	t.Parallel()
	for phase, test := range emergencyRestorePhases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v, state := emergencyPhaseSave(t, phase)
			input := emergencyRestoreInput(s, state)
			input.LogicalOnly = true
			restored, result, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			var interrupted []int
			if v.op.purpose == opEmergencyUnload {
				interrupted = []int{1}
			}
			if result.Tier != RestoreLogical || result.DroppedEmergencies != 1 || !slices.Equal(result.Interrupted, interrupted) ||
				len(restored.emergencies) != 0 || restored.emergencyCounters != s.emergencyCounters || !restored.emergenciesOn {
				t.Fatalf("result %+v, records %v, counters %+v", result, emergencyIDs(restored), restored.emergencyCounters)
			}
			for index := range restored.vehicles {
				if pod := &restored.vehicles[index]; pod.withdrawn != 0 {
					t.Fatalf("pod %s keeps the holds %#x", pod.Pod.ID, pod.withdrawn)
				}
			}
			checkEmergenciesEachTick(t, restored)
			for range 60 {
				restored.Step()
			}
		})
	}
}

// TestEmergencyRestoreDemotion checks that a physical restore that would
// demote a record pod fails the physical tier, and that the restore then
// uses the logical tier, as stage 1 allows (section 10.7 of the incident
// emergency contract). No single record ends to keep the physical
// tier.
func TestEmergencyRestoreDemotion(t *testing.T) {
	t.Parallel()
	for _, phase := range []int{0, 1} {
		s, v, state := emergencyPhaseSave(t, phase)
		state.Pods = slices.Clone(state.Pods)
		state.Pods[s.vehicleIndex(v)].Route = nil
		restored, result, err := RestoreState(emergencyRestoreInput(s, state))
		if err != nil {
			t.Fatal(err)
		}
		if result.Tier != RestoreLogical || result.PhysicalError == nil || !strings.Contains(result.PhysicalError.Error(), "would demote pod 01") ||
			result.DroppedEmergencies != 1 || len(restored.emergencies) != 0 {
			t.Fatalf("%s: result %+v, records %v", emergencyRestorePhases[phase].name, result, emergencyIDs(restored))
		}
	}
}

// emergencyE8Save returns a save in which the record pod has a refuge
// with active riders, which breaks invariant E8, and the simulation.
func emergencyE8Save(t *testing.T) (*Simulation, SavedState) {
	t.Helper()
	s := incidentLegFleet(t)
	s.emergenciesOn = true
	v := boardParties(t, s, "s2", "s2")
	travelOn(t, s, v, "s0-link")
	if err := s.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opRefuge, owner: faultHold, station: "s1", berth: "s1-2"}); err != nil {
		t.Fatal(err)
	}
	s.incidentSerial++
	s.emergencies = append(s.emergencies, emergencyRecord{serial: s.incidentSerial, start: s.tick, pod: s.vehicleIndex(v), order: v.Riders[0].ID})
	v.withdrawn |= emergencyHold
	if err := s.CheckContract(); err == nil || !strings.HasPrefix(err.Error(), "E8:") {
		t.Fatalf("the fixture does not break E8: %v", err)
	}
	state := s.ExportState()
	return s, state
}

// TestEmergencyRestorePreTier checks the checks of section 11.5 of the
// incident emergency contract that run before either restore tier. Each
// save is invalid as a whole, with no logical fallback, also when the
// physical tier fails and the logical tier would remove the evidence.
func TestEmergencyRestorePreTier(t *testing.T) {
	t.Parallel()
	bound, v, boundState := emergencyPhaseSave(t, 1)
	pod := bound.vehicleIndex(v)
	faulted, _, faultedState := emergencyPhaseSave(t, 3)
	e8, e8State := emergencyE8Save(t)
	tests := []struct {
		name   string
		s      *Simulation
		state  SavedState
		change func(state *SavedState, input *RestoreStateInput)
		want   string
	}{
		{"wrong party, demoted pod", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Order = 2
			state.Pods[pod].Route = nil
		}, "not only the party 2"},
		{"wrong interrupt bit", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Pods[pod].Interrupt = 2
		}, "E3: pod 01 interrupts"},
		{"serial 0", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Serial = 0
		}, "a serial outside"},
		{"serial above the incident serial", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Serial = state.IncidentSerial + 1
		}, "a serial outside"},
		{"fault serial", faulted, faultedState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Serial = state.Faults.Records[0].Serial
		}, "the serial of a fault record"},
		{"serial order", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records = append([]SavedEmergency{state.Emergencies.Records[0]}, state.Emergencies.Records...)
			state.Emergencies.Records[1].Pod = 1 - pod
		}, "is not after the record"},
		{"two records for one pod", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.IncidentSerial++
			second := state.Emergencies.Records[0]
			second.Serial = state.IncidentSerial
			state.Emergencies.Records = append(state.Emergencies.Records, second)
		}, "with another record"},
		{"pod out of range", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Pod = len(state.Pods)
		}, "out of range"},
		{"too many records", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			for range MaxEmergencies {
				state.Emergencies.Records = append(state.Emergencies.Records, state.Emergencies.Records[0])
			}
		}, "more than 4"},
		{"negative start", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Start = -1
		}, "starts at tick -1"},
		{"start after the tick", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Start = state.Tick + 1
		}, "starts at tick"},
		{"order 0", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Order = 0
		}, "not positive"},
		{"negative counter", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Counters.EmergencyTicks = -1
		}, "counter is negative"},
		{"negative started", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Counters.Started = -1
		}, "counter is negative"},
		{"negative ended", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Counters.Ended = -1
		}, "counter is negative"},
		{"hold and no record", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records = nil
		}, "E2: pod 01"},
		{"emergency unload of another owner", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Pods[pod].Withdrawn, state.Pods[pod].Owner = uint8(faultHold|emergencyHold), uint8(faultHold)
		}, "E4: the emergency unload of pod 01"},
		{"emergency unload and no record", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Emergencies.Records[0].Pod = 1 - pod
			state.Pods[pod].Withdrawn = 0
		}, "E4: pod 01"},
		{"emergency unload and no passenger", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			for index := range state.Pods[pod].Riders {
				state.Pods[pod].Riders[index].Completed = true
			}
		}, "E5: pod 01"},
		{"refuge with riders", e8, e8State, func(*SavedState, *RestoreStateInput) {}, "E8: pod 01"},
		{"recovery with riders", bound, boundState, func(state *SavedState, _ *RestoreStateInput) {
			state.Pods[pod].Withdrawn, state.Pods[pod].Owner = uint8(faultHold|emergencyHold), uint8(faultHold)
			state.Pods[pod].Purpose, state.Pods[pod].Interrupt = uint8(opEmptyRecovery), 0
		}, "E8: pod 01"},
	}
	for _, test := range tests {
		state := test.state
		state.Pods = slices.Clone(state.Pods)
		for index := range state.Pods {
			state.Pods[index].Riders = slices.Clone(state.Pods[index].Riders)
		}
		emergencies := *state.Emergencies
		emergencies.Records = slices.Clone(emergencies.Records)
		state.Emergencies = &emergencies
		input := emergencyRestoreInput(test.s, state)
		test.change(&input.State, &input)
		restored, result, err := RestoreState(input)
		if !errors.Is(err, errInvalidEmergencies) || !strings.Contains(err.Error(), test.want) || restored != nil {
			t.Errorf("%s: error %v, result %+v, want %q", test.name, err, result, test.want)
		}
	}
	// Records need the emergency marker.
	input := emergencyRestoreInput(bound, boundState)
	input.EmergencyContract = ""
	if _, _, err := RestoreState(input); err == nil || !strings.Contains(err.Error(), "need the emergency contract") {
		t.Fatalf("a save with a record and no marker: %v", err)
	}
	// A save without the record restores with the logical tier only when
	// the pre-tier checks pass: the control has a demoted pod and a valid
	// party.
	control := boundState
	control.Pods = slices.Clone(control.Pods)
	control.Pods[pod].Route = nil
	if _, result, err := RestoreState(emergencyRestoreInput(bound, control)); err != nil || result.Tier != RestoreLogical {
		t.Fatalf("control: %v, %+v", err, result)
	}
}

// TestEmergencyPlatoonRestore restores a draining link with an emergency
// at its leader. The restored link keeps draining on each tick and does
// not grow, as in the simulation that made the save, until it ends.
func TestEmergencyPlatoonRestore(t *testing.T) {
	t.Parallel()
	s, leader, follower := emergencyPlatoon(t)
	for range 30 * TicksPerSecond {
		s.Step()
	}
	startEmergency(t, s, leader, 0)
	s.Step()
	if !follower.link.draining {
		t.Fatalf("the link does not drain: %+v", follower.link)
	}
	restored, result, err := RestoreState(RestoreStateInput{
		Network: s.network, Fleet: s.initial, State: s.ExportState(),
		IncidentContract: IncidentV1Contract, EmergencyContract: EmergencyV1Contract,
	})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore %v, %+v", err, result)
	}
	if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	checkEmergenciesEachTick(t, restored)
	link := &restored.vehicles[1].link
	if link.leader == 0 || !link.draining {
		t.Fatalf("the restored link does not drain: %+v", *link)
	}
	lanes := link.lanes
	for link.leader != 0 {
		restored.Step()
		if link.leader != 0 && (!link.draining || link.lanes != lanes) {
			t.Fatalf("tick %d: the restored link does not drain: %+v", restored.tick, *link)
		}
		if restored.tick > s.tick+600 {
			t.Fatal("the restored link did not end")
		}
	}
}

// TestEmergencyRestoreMemo checks that a restored simulation searches on
// the next cadence tick of a record whose source had a no-candidate memo
// entry. The memo is not saved, and the blocked set of the source came
// from no fault record, so the restored pod binds.
func TestEmergencyRestoreMemo(t *testing.T) {
	t.Parallel()
	s, v := missFleet(t)
	startEmergency(t, s, v, 0)
	if len(s.emergencyMisses) != 1 {
		t.Fatalf("memo %+v", s.emergencyMisses)
	}
	restored, result, err := RestoreState(emergencyRestoreInput(s, s.ExportState()))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore %v, %+v", err, result)
	}
	if restored.emergencyMisses != nil || len(restored.emergencies) != 1 {
		t.Fatalf("memo %+v, records %v", restored.emergencyMisses, emergencyIDs(restored))
	}
	before := restored.searchCounters
	stepToCadence(restored)
	if after := restored.searchCounters; after.choices != before.choices+1 || after.memoHits != before.memoHits {
		t.Fatalf("the restored simulation did not search: %+v, then %+v", before, after)
	}
	if pod := restored.findVehicle(v.Pod.ID); pod.op.purpose != opEmergencyUnload {
		t.Fatalf("the restored pod did not bind: %+v", pod.op)
	}
}

// TestCheckIncidentPolicy checks the production policy validator (section
// 8 of the incident emergency contract). With the incident marker and
// without the emergency marker, it refuses the emergency hold and the
// emergency unload of the stage 1 fixtures, which RestoreState still
// restores. With the emergency marker, or without the incident marker,
// it accepts them: the pre-tier checks and the stage 1 checks of
// RestoreState refuse them there.
func TestCheckIncidentPolicy(t *testing.T) {
	t.Parallel()
	traveling, unloading, s := emergencyStates(t)
	holdOnly := traveling
	holdOnly.Pods = slices.Clone(holdOnly.Pods)
	holdOnly.Pods[0].Purpose, holdOnly.Pods[0].Owner, holdOnly.Pods[0].Interrupt = 0, 0, 0
	plain := incidentLegFleet(t).ExportState()
	for _, test := range []struct {
		name  string
		state SavedState
		want  string
	}{
		{"traveling", traveling, "has the emergency hold"},
		{"unloading", unloading, "has the emergency hold"},
		{"hold only", holdOnly, "has the emergency hold"},
		{"plain", plain, ""},
	} {
		input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: test.state, IncidentContract: IncidentV1Contract}
		if err := CheckIncidentPolicy(input); test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Errorf("%s: %v, want %q", test.name, err, test.want)
		}
		if _, _, err := RestoreState(input); err != nil {
			t.Errorf("%s: RestoreState: %v", test.name, err)
		}
		input.EmergencyContract = EmergencyV1Contract
		if err := CheckIncidentPolicy(input); err != nil {
			t.Errorf("%s with the emergency marker: %v", test.name, err)
		}
		input.IncidentContract, input.EmergencyContract = "", ""
		if err := CheckIncidentPolicy(input); err != nil {
			t.Errorf("%s without the incident marker: %v", test.name, err)
		}
	}
	// A purpose 1 pod without the hold is refused by its purpose. The
	// stage 1 checks of RestoreState also refuse it.
	purpose := traveling
	purpose.Pods = slices.Clone(purpose.Pods)
	purpose.Pods[0].Withdrawn, purpose.Pods[0].Owner = uint8(faultHold), uint8(faultHold)
	err := CheckIncidentPolicy(RestoreStateInput{State: purpose, IncidentContract: IncidentV1Contract})
	if err == nil || !strings.Contains(err.Error(), "has an emergency unload") {
		t.Fatalf("purpose 1 with the fault hold: %v", err)
	}
	// A pod whose purpose the emergency hold owns is refused by its owner,
	// also when it has only the fault hold and another purpose.
	owner := traveling
	owner.Pods = slices.Clone(owner.Pods)
	owner.Pods[0].Withdrawn, owner.Pods[0].Owner, owner.Pods[0].Purpose = uint8(faultHold), uint8(emergencyHold), uint8(opEmptyRecovery)
	err = CheckIncidentPolicy(RestoreStateInput{State: owner, IncidentContract: IncidentV1Contract})
	if err == nil || !strings.Contains(err.Error(), "has the emergency hold") {
		t.Fatalf("an emergency owner with the fault hold: %v", err)
	}
}

// TestEmergencyRestoreCounters restores distinct counters through both
// tiers, with an active record and with none. Each tier keeps each
// counter as saved.
func TestEmergencyRestoreCounters(t *testing.T) {
	t.Parallel()
	want := emergencyCounters{started: 5, ended: 3, emergencyTicks: 77}
	saved := EmergencyCounters{Started: 5, Ended: 3, EmergencyTicks: 77}
	bound, _, boundState := emergencyPhaseSave(t, 1)
	idle, _ := emergencyFleet(t)
	idleState := idle.ExportState()
	for _, test := range []struct {
		name  string
		s     *Simulation
		state SavedState
	}{{"record", bound, boundState}, {"no record", idle, idleState}} {
		for _, logical := range []bool{false, true} {
			state := test.state
			emergencies := SavedEmergencies{Counters: saved}
			if state.Emergencies != nil {
				emergencies.Records = slices.Clone(state.Emergencies.Records)
			}
			state.Emergencies = &emergencies
			input := emergencyRestoreInput(test.s, state)
			input.LogicalOnly = logical
			restored, _, err := RestoreState(input)
			if err != nil {
				t.Fatalf("%s, logical %t: %v", test.name, logical, err)
			}
			if restored.emergencyCounters != want {
				t.Fatalf("%s, logical %t: counters %+v, want %+v", test.name, logical, restored.emergencyCounters, want)
			}
		}
	}
}

// TestEmergencyRewind checks a rewind after a start (section 10.7 of the
// incident emergency contract). A clone at the start keeps the record and
// the counters while the source runs on, and the new generation of the
// rewound simulation gives a new record a new ID prefix.
func TestEmergencyRewind(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	startEmergency(t, s, v, 1)
	checkpoint := s.Clone()
	records, counters := slices.Clone(s.emergencies), s.emergencyCounters
	stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
	rewound := checkpoint.Clone()
	rewound.SetIncidentGeneration(1)
	if !slices.Equal(rewound.emergencies, records) || rewound.emergencyCounters != counters || emergencyIDs(rewound)[0] != "i0.1" {
		t.Fatalf("records %v, counters %+v", emergencyIDs(rewound), rewound.emergencyCounters)
	}
	if id := rewound.nextIncidentID(); id != "i1.2" {
		t.Fatalf("the next ID is %s, want i1.2", id)
	}
}
