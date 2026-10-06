package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

type couplingMemberMotion struct {
	distance, speed float64
}

// couplingMemberMotions records the pose of each committed member before a tick.
func couplingMemberMotions(s *Simulation) map[string]couplingMemberMotion {
	result := make(map[string]couplingMemberMotion)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.couplingID != "" {
			result[v.Pod.ID] = couplingMemberMotion{distance: v.distance, speed: v.Pod.Speed}
		}
	}
	return result
}

// checkCouplingMemberMotion is independent of the planner and of
// checkTickMotion. For each member that was committed before the tick,
// including a member that retires during the tick, it checks Euler distance, bounded acceleration, the speed limit of the
// current lane and of every crossed lane, braking room before each lower lane
// limit, and continuous stopping room inside the member's owned grant.
func checkCouplingMemberMotion(s *Simulation, before map[string]couplingMemberMotion) error {
	profile, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	const slack = 1e-9
	for i := range s.vehicles {
		v := &s.vehicles[i]
		old, ok := before[v.Pod.ID]
		if !ok {
			continue
		}
		distance, speed := v.distance, v.Pod.Speed
		if !finite(distance) || !finite(speed) || speed < 0 {
			return fmt.Errorf("member %s has a nonfinite or negative motion", v.Pod.ID)
		}
		if math.Abs(distance-(old.distance+speed/TicksPerSecond)) > slack {
			return fmt.Errorf("member %s moved %.9f m at %.9f m/s", v.Pod.ID, distance-old.distance, speed)
		}
		if math.Abs(speed-old.speed) > max(profile.Acceleration, profile.Braking)/TicksPerSecond+slack {
			return fmt.Errorf("member %s changed speed from %.9f to %.9f m/s in one tick", v.Pod.ID, old.speed, speed)
		}
		reach := distance + speed*speed/(2*profile.Braking)
		for lane := range v.Route {
			start := v.blocks.lanes[lane].start
			end := start + v.blocks.lanes[lane].length
			limit := v.Route[lane].SpeedLimit
			if end >= old.distance && start <= distance && speed > limit+slack {
				return fmt.Errorf("member %s at %.9f m/s exceeds the %.9f m/s limit of lane %s", v.Pod.ID, speed, limit, v.Route[lane].ID)
			}
			if start > distance && start <= reach && limit < speed && distance+(speed*speed-limit*limit)/(2*profile.Braking) > start+slack {
				return fmt.Errorf("member %s lacks braking room before lane %s", v.Pod.ID, v.Route[lane].ID)
			}
		}
		if v.reservedThrough < 0 || v.reservedThrough >= v.blocks.len() || reach > v.blocks.end(v.reservedThrough)+slack {
			return fmt.Errorf("member %s stops at %.9f m beyond its owned grant", v.Pod.ID, reach)
		}
	}
	return nil
}

// The oracle accepts actual committed motion and rejects each fabricated
// violation. The retirement case checks the tick in which both members leave
// the group.
func TestCouplingMemberMotionOracle(t *testing.T) {
	t.Parallel()
	input := nativeCouplingSavedFixture(t, false, couplingConnected, 1)
	s, _, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPaused(false)
	for range 120 {
		before := couplingMemberMotions(s)
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatal(err)
		}
		if err := checkCouplingMemberMotion(s, before); err != nil {
			t.Fatal("oracle rejected actual committed motion", err)
		}
	}
	before := couplingMemberMotions(s)
	if len(before) != 2 {
		t.Fatal("fixture lost its committed members")
	}
	s.Step()
	for _, test := range []struct {
		name   string
		change func(*vehicle, *couplingMemberMotion)
	}{
		{"euler", func(v *vehicle, _ *couplingMemberMotion) { v.distance += 0.01 }},
		{"acceleration", func(v *vehicle, old *couplingMemberMotion) {
			old.speed = v.Pod.Speed - 1
			old.distance = v.distance - v.Pod.Speed/TicksPerSecond
		}},
		{"lane_limit", func(v *vehicle, old *couplingMemberMotion) {
			v.Pod.Speed = v.Route[v.blocks.routeLane(v.blockIndex)].SpeedLimit + 0.01
			old.speed = v.Pod.Speed
			v.distance = old.distance + v.Pod.Speed/TicksPerSecond
		}},
		{"owned_grant", func(v *vehicle, _ *couplingMemberMotion) { v.reservedThrough = v.blockIndex - 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clone := s.Clone()
			old := maps.Clone(before)
			v := clone.findVehicle("front")
			motion := old["front"]
			test.change(v, &motion)
			old["front"] = motion
			if err := checkCouplingMemberMotion(clone, old); err == nil {
				t.Fatal("oracle accepted a fabricated violation")
			}
		})
	}
	t.Run("retirement", func(t *testing.T) {
		t.Parallel()
		retired, before := couplingRetirementTick(t)
		if err := checkCouplingMemberMotion(retired, before); err != nil {
			t.Fatal("oracle rejected the actual retirement tick", err)
		}
		retired.findVehicle("rear").distance += 0.01
		if err := checkCouplingMemberMotion(retired, before); err == nil {
			t.Fatal("oracle accepted a fabricated retirement tick")
		}
	})
}

// couplingRetirementTick steps the last drain leg until the group retires.
// It returns the simulation after the retirement tick and the member motion
// from before that tick. Both members have no coupling ID after the tick.
func couplingRetirementTick(t *testing.T) (*Simulation, map[string]couplingMemberMotion) {
	t.Helper()
	s, _, err := RestoreState(nativeCouplingSavedFixture(t, false, couplingDraining, 4))
	if err != nil {
		t.Fatal(err)
	}
	s.SetPaused(false)
	for range 6000 {
		before := couplingMemberMotions(s)
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatal(err)
		}
		if len(s.couplingGroups) == 0 {
			if len(before) != 2 || s.findVehicle("front").couplingID != "" || s.findVehicle("rear").couplingID != "" {
				t.Fatal("retirement tick did not clear both members", before)
			}
			return s, before
		}
	}
	t.Fatal("drain fixture did not retire within its cap")
	return nil, nil
}

// couplingTwinView holds the observables that two twin fleets must share:
// the exported state, the live pods and route distances, the motion frame,
// the complete owner ledger, and the request and completion counters.
type couplingTwinView struct {
	State       SavedState
	Pods        []Pod
	Distances   []float64
	Frame       MotionFrame
	FrameOK     bool
	Owners      map[resource]resourceOwner
	Completions []StepCompletion
	Completed   int
	Boarded     int
}

func couplingTwinObservables(s *Simulation) couplingTwinView {
	view := couplingTwinView{State: s.ExportState(), Owners: maps.Clone(s.owners), Completions: slices.Clone(s.StepCompletions()), Completed: s.completed, Boarded: s.boarded}
	view.Frame, view.FrameOK = s.MotionFrame()
	for i := range s.vehicles {
		view.Pods = append(view.Pods, s.vehicles[i].Pod)
		view.Distances = append(view.Distances, s.vehicles[i].distance)
	}
	return view
}

// couplingTwinMoved reports whether a live pod position or speed differs.
func couplingTwinMoved(a, b couplingTwinView) bool {
	for i := range a.Pods {
		if a.Pods[i].Position != b.Pods[i].Position || a.Pods[i].Speed != b.Pods[i].Speed {
			return true
		}
	}
	return false
}

// Turning the policy off at a committed phase stops new recruitment but does
// not change the train. A twin that keeps the policy on has the same state at
// every tick until both members finish their journeys.
func TestCouplingNativeDisableDrainsEveryPhase(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4}} {
			t.Run(fmt.Sprintf("occupied=%t/phase=%d/leg=%d", occupied, phase.phase, phase.leg), func(t *testing.T) {
				t.Parallel()
				input := nativeCouplingSavedFixture(t, occupied, phase.phase, phase.leg)
				input.CouplingEnabled = true
				var twins [2]*Simulation
				for i := range twins {
					s, _, err := RestoreState(input)
					if err != nil {
						t.Fatal(err)
					}
					s.SetPaused(false)
					s.SetMotionRecording(true)
					twins[i] = s
				}
				on, off := twins[0], twins[1]
				for tick := 0; ; tick++ {
					if tick == 5 {
						group := off.couplingGroups[0]
						if err := off.SetCouplingEnabled(false); err != nil || off.CouplingEnabled() {
							t.Fatal("policy off failed", err)
						}
						if len(off.couplingGroups) != 1 || off.couplingGroups[0].state != group.state || off.couplingGroups[0].formationTick != group.formationTick {
							t.Fatal("policy off changed the committed group")
						}
					}
					on.Step()
					off.Step()
					checkCouplingApproachNativeBoundary(t, off)
					if !reflect.DeepEqual(couplingTwinObservables(on), couplingTwinObservables(off)) {
						t.Fatalf("policy off changed committed motion, ownership, or completions at tick %d", off.tick)
					}
					if tick >= 5 && (len(off.couplingApproaches) != 0 || len(off.couplingAttempts) != 0) {
						t.Fatalf("policy off started a new approach at tick %d", off.tick)
					}
					idle := len(off.couplingGroups) == 0
					for i := range off.vehicles {
						idle = idle && off.vehicles[i].Pod.Activity == Idle
					}
					if idle {
						break
					}
					if tick > 30000 {
						t.Fatal("policy off did not drain the train and finish both journeys")
					}
				}
				if occupied && off.completed != 2 {
					t.Fatal("occupied cabins did not complete both orders", off.completed)
				}
			})
		}
	}
}

// With no train formed, a fleet whose coupling policy is off has the same
// trajectory as the same fleet without a coupling contract: the same live
// pods, motion frames, owner ledger, and completions at every tick. The
// policy-on twin forms trains, so its pods move differently. Every fleet
// must issue all requests, bring each pod to its goal, and conserve its
// completions before the horizon.
func TestCouplingDisabledPolicyTrajectoryParity(t *testing.T) {
	t.Parallel()
	skipLong(t)
	sc := couplingMultiShared(t, 12000)
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "occupied"}[occupied], func(t *testing.T) {
			t.Parallel()
			plain := couplingMultiScenario{prepared: sc.prepared, trips: sc.trips}
			fleets := [3]*Simulation{plain.start(t, false), sc.start(t, false), sc.start(t, true)}
			for _, s := range fleets {
				s.SetMotionRecording(true)
			}
			requests := [3]map[string]int{{}, {}, {}}
			completions := [3]map[int]bool{{}, {}, {}}
			formed, moved := false, false
			for range 49000 {
				var views [3]couplingTwinView
				for i, s := range fleets {
					sc.request(t, s, occupied, requests[i])
					s.Step()
					if err := s.CouplingError(); err != nil {
						t.Fatal(err)
					}
					views[i] = couplingTwinObservables(s)
					for _, c := range views[i].Completions {
						if completions[i][c.RequestID] {
							t.Fatal("completion replayed", c)
						}
						completions[i][c.RequestID] = true
					}
				}
				if views[1].State.CouplingContract != CompactPairV1CouplingContract || len(views[1].State.CouplingGroups) != 0 || len(fleets[1].couplingApproaches) != 0 {
					t.Fatalf("disabled twin lost its contract or recruited at tick %d", fleets[1].tick)
				}
				views[1].State.CouplingContract = ""
				if !reflect.DeepEqual(views[0], views[1]) {
					t.Fatalf("disabled policy changed the trajectory at tick %d", fleets[1].tick)
				}
				formed = formed || len(fleets[2].couplingGroups) != 0
				moved = moved || couplingTwinMoved(views[0], views[2])
				if couplingTwinFleetsDone(sc, fleets, requests, completions, occupied) {
					if !formed || !moved {
						t.Fatalf("enabled twin formed=%t moved=%t, so the comparison cannot detect a change", formed, moved)
					}
					return
				}
			}
			t.Fatal("fleets did not finish every journey before the horizon")
		})
	}
}

// couplingTwinFleetsDone reports whether each fleet issued every request,
// parked each pod idle at its goal, and, with passengers, completed and
// boarded each request one time.
func couplingTwinFleetsDone(sc couplingMultiScenario, fleets [3]*Simulation, requests [3]map[string]int, completions [3]map[int]bool, occupied bool) bool {
	for i, s := range fleets {
		if len(requests[i]) != len(sc.trips) {
			return false
		}
		for _, trip := range sc.trips {
			if v := s.findVehicle(trip.id); v.Pod.Activity != Idle || v.Pod.StationID != trip.goal {
				return false
			}
		}
		if occupied && (s.completed != len(sc.trips) || s.boarded != len(sc.trips) || len(completions[i]) != len(sc.trips)) {
			return false
		}
	}
	return true
}

// checkCouplingApproachHandoff checks each waiting or aborting approach whose
// rear member owns every resource that it retains. The tick that completed
// that handoff must also end the virtual link in both directions, and the
// exported state must show no link for the rear member.
func checkCouplingApproachHandoff(s *Simulation) error {
	var exported []SavedPod
	for _, a := range s.couplingApproaches {
		if a.state.Phase != couplingApproachWaiting && a.state.Phase != couplingApproachAborting {
			continue
		}
		front, rear := s.findVehicle(a.context.members[0].id), s.findVehicle(a.context.members[1].id)
		if s.holdsPending(rear) {
			continue
		}
		if rear.link.leader != 0 || front.follower != 0 {
			return fmt.Errorf("rear %s owns its retained resources but keeps its virtual link", rear.Pod.ID)
		}
		if exported == nil {
			exported = s.ExportState().Pods
		}
		for _, pod := range exported {
			if pod.ID == rear.Pod.ID && pod.Platoon != nil {
				return fmt.Errorf("exported rear %s keeps a platoon link", rear.Pod.ID)
			}
		}
	}
	return nil
}

// Turning the policy off while the rear member still borrows resources from
// the front aborts the approach with a draining link. The tick that hands
// the last borrowed resource to the rear must also end the link in both
// directions and in the exported state. A copy of that tick with the link
// put back fails checkCouplingApproachHandoff.
func TestCouplingApproachAbortUnlinksOnHandoffTick(t *testing.T) {
	t.Parallel()
	sc := couplingMultiShared(t, 12000)
	s := sc.start(t, true)
	requests := map[string]int{}
	for s.tick < 12000 {
		if s.tick == 5000 {
			if err := s.SetCouplingEnabled(false); err != nil {
				t.Fatal(err)
			}
		}
		sc.request(t, s, false, requests)
		front, rear := s.findVehicle("front"), s.findVehicle("rear")
		pending := false
		for _, a := range s.couplingApproaches {
			pending = pending || a.context.members[1].id == "rear" && a.state.Phase == couplingApproachAborting && s.holdsPending(rear)
		}
		s.Step()
		checkCouplingApproachNativeBoundary(t, s)
		if !pending || s.holdsPending(rear) {
			continue
		}
		if rear.link.leader != 0 || front.follower != 0 {
			t.Fatalf("handoff tick %d kept the virtual link", s.tick)
		}
		bad := s.Clone()
		badFront, badRear := bad.findVehicle("front"), bad.findVehicle("rear")
		index := func(id string) int {
			return slices.IndexFunc(bad.vehicles, func(v vehicle) bool { return v.Pod.ID == id })
		}
		badRear.link.leader, badFront.follower = index("front")+1, index("rear")+1
		if checkCouplingApproachHandoff(bad) == nil {
			t.Fatal("handoff check accepted a link kept after the handoff")
		}
		return
	}
	t.Fatal("policy off did not abort an approach with borrowed resources and hand them off")
}
