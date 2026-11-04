package sim

import (
	"fmt"
	"slices"
	"testing"
)

// holdPose is a real saved state and the pod of the state that a holdCase
// changes.
type holdPose struct {
	name, pod string
	state     SavedState
}

// holdPoses returns the states of a pod that hold cases start from. Each pose
// comes from a run on the same network and fleet as s.
func holdPoses(t *testing.T) (poses []holdPose, s *Simulation) {
	t.Helper()
	s = incidentLegFleet(t)
	poses = append(poses, holdPose{"idle at a berth", "01", s.ExportState()})
	v := boardParties(t, s, "s1", "s1", "s2")
	travelOn(t, s, v, "s0-link")
	state := s.ExportState()
	poses = append(poses, holdPose{"traveling with riders", "01", state}, holdPose{"idle at parking", "02", state})
	_, unloading, _ := emergencyStates(t)
	poses = append(poses, holdPose{"unloading", "01", unloading})
	evacuated := faultLegFleet(t)
	if err := evacuated.SetFaults(true, FaultSettings{EvacuationSeconds: 0}); err != nil {
		t.Fatal(err)
	}
	v = boardParties(t, evacuated, "s2", "s2")
	cruiseOn(t, evacuated, v, "s0-link")
	startFault(t, evacuated, v, 0)
	stepUntil(t, evacuated, "the empty recovery", func() bool { return v.op.purpose == opEmptyRecovery })
	return append(poses, holdPose{"empty recovery", "01", evacuated.ExportState()}), s
}

// holdCase is one saved state of a pod: its holds, its purpose and owner,
// and the records that name it.
type holdCase struct {
	hold, owner            serviceHold
	purpose                opPurpose
	faultRecord, emergency bool
}

func (c holdCase) String() string {
	return fmt.Sprintf("hold %d, purpose %d, owner %d, fault record %t, emergency record %t", c.hold, c.purpose, c.owner, c.faultRecord, c.emergency)
}

// holdCases lists each state that the decoder of a save passes: a purpose
// has an owner, and the owner is one of the holds of the pod.
func holdCases() []holdCase {
	var cases []holdCase
	for _, hold := range []serviceHold{0, faultHold, emergencyHold, faultHold | emergencyHold} {
		for _, purpose := range []opPurpose{opService, opEmergencyUnload, opRefuge, opEmptyRecovery} {
			for _, owner := range []serviceHold{0, faultHold, emergencyHold} {
				if (purpose == opService) != (owner == 0) || owner != 0 && hold&owner == 0 {
					continue
				}
				for _, records := range []int{0, 1, 2, 3} {
					cases = append(cases, holdCase{hold, owner, purpose, records&1 != 0, records&2 != 0})
				}
			}
		}
	}
	return cases
}

// state returns the state of pose with the case applied to the pod of pose.
// The only records are the records of the case.
func (c holdCase) state(pose holdPose) SavedState {
	state := pose.state
	state.Pods = slices.Clone(state.Pods)
	index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == pose.pod })
	pod := &state.Pods[index]
	pod.Withdrawn, pod.Purpose, pod.Owner, pod.Interrupt = uint8(c.hold), uint8(c.purpose), uint8(c.owner), 0
	party := 1
	if active := slices.IndexFunc(pod.Riders, func(rider SavedRequest) bool { return !rider.Completed }); active >= 0 {
		party = pod.Riders[active].ID
		if c.purpose == opEmergencyUnload {
			pod.Interrupt = 1 << active
		}
	}
	state.IncidentSerial = 2
	state.Faults, state.Emergencies = nil, nil
	if c.faultRecord {
		state.Faults = &SavedFaults{Records: []SavedFault{{Generation: 1, Serial: 1, Start: state.Tick, Pod: index}}}
	}
	if c.emergency {
		state.Emergencies = &SavedEmergencies{Records: []SavedEmergency{{Generation: 1, Serial: 2, Start: state.Tick, Pod: index, Order: party}}}
	}
	return state
}

// holdReleaseLimit is the number of ticks in which a restored pod must lose
// each hold and each record.
const holdReleaseLimit = 30 * 60 * TicksPerSecond

// released steps s until pod has no hold, no purpose, and no record. A fault
// record ends with a clear, as a command ends it. It checks the contract of
// s at each tick.
func released(t *testing.T, s *Simulation, pod string) bool {
	t.Helper()
	v := s.findVehicle(pod)
	for range holdReleaseLimit {
		if v.withdrawn == 0 && v.op.purpose == opService && len(s.faults) == 0 && len(s.emergencies) == 0 {
			return true
		}
		for _, fault := range slices.Clone(s.faults) {
			if err := s.clearFault(fault.id()); err != nil {
				t.Fatalf("clear: %v", err)
			}
		}
		s.Step()
		if err := s.CheckContract(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
	}
	return false
}

// TestHoldCombinationsRelease restores each state that the decoder of a save
// passes, from each pose, with the switches on, as a session does, in both
// tiers. A state that the policy or the restore does not refuse must lose
// each hold, each purpose, and each record, so no such state holds a pod
// forever. The test pins the number of surviving states, so that a change of
// a refusal shows here, and it names the states that the contract requires
// to survive.
func TestHoldCombinationsRelease(t *testing.T) {
	t.Parallel()
	poses, s := holdPoses(t)
	survivors := map[RestoreTier]int{}
	seen := map[string]bool{}
	for _, pose := range poses {
		for _, c := range holdCases() {
			state := c.state(pose)
			for _, logical := range []bool{false, true} {
				input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, LogicalOnly: logical,
					FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}, EmergenciesOn: true}
				if err := CheckIncidentPolicy(input); err != nil {
					continue
				}
				restored, result, err := RestoreState(input)
				if err != nil {
					continue
				}
				survivors[result.Tier]++
				seen[fmt.Sprintf("%s: %s", pose.name, c)] = true
				if !released(t, restored, pose.pod) {
					pod := restored.findVehicle(pose.pod)
					t.Errorf("%s, %s, logical %t: after %d ticks, holds %d, purpose %d, activity %s, %d faults, %d emergencies",
						pose.name, c, logical, holdReleaseLimit, pod.withdrawn, pod.op.purpose, pod.Pod.Activity, len(restored.faults), len(restored.emergencies))
				}
			}
		}
	}
	// Five poses give 57 surviving states, and each state restores once in
	// each tier.
	const wantSurvivors = 57
	if survivors[RestorePhysical] != wantSurvivors || survivors[RestoreLogical] != wantSurvivors {
		t.Errorf("survivors %v, want %d in each tier", survivors, wantSurvivors)
	}
	for _, want := range []string{
		"idle at parking: hold 1, purpose 0, owner 0, fault record false, emergency record false",
		"traveling with riders: hold 1, purpose 0, owner 0, fault record true, emergency record false",
		"traveling with riders: hold 2, purpose 1, owner 2, fault record false, emergency record true",
		"unloading: hold 2, purpose 1, owner 2, fault record false, emergency record true",
		"traveling with riders: hold 3, purpose 1, owner 2, fault record true, emergency record true",
		"empty recovery: hold 1, purpose 3, owner 1, fault record false, emergency record false",
		"empty recovery: hold 1, purpose 3, owner 1, fault record true, emergency record false",
		"empty recovery: hold 3, purpose 3, owner 1, fault record true, emergency record true",
	} {
		if !seen[want] {
			t.Errorf("no restore of %s", want)
		}
	}
}
