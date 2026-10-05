package sim

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// The exit blocker leaves its berth at this tick. It enters rear-road after
// junction c and holds the rear-road cells that only the exit closure claims
// when the pair is first ready. Measured: departures from 4680 to 4900 refuse
// 18 to 238 ticks and then form before the 300-tick partner deadline; from
// 4920 the hold outlasts the deadline, and up to 4660 the blocker clears first.
const couplingSplitExitDeparture = 4800

// Add a one-berth Compact station. The extra lane either feeds the next node
// from the station exit or reaches the station entry from the previous node.
func couplingSplitExitAddStation(network *Network, id string, entry, berth, exit Point, extra Lane) {
	compact := classBit(string(CompactClass))
	network.Nodes = append(network.Nodes, Node{ID: id + "-entry", Position: entry}, Node{ID: id + "-berth", Position: berth}, Node{ID: id + "-exit", Position: exit})
	for _, lane := range []Lane{{ID: id + "-in", From: id + "-entry", To: id + "-berth"}, {ID: id + "-out", From: id + "-berth", To: id + "-exit"},
		{ID: id + "-through", From: id + "-entry", To: id + "-exit"}, extra} {
		lane.SpeedLimit, lane.VehicleClasses = 14, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: id + "-berth", Node: id + "-berth", VehicleClasses: compact}}})
}

// The single-pair journey network with a feeder into junction c and a far
// goal behind the rear goal. The exit blocker travels exit-origin, c,
// rear-road, rear-through, and far-road, so it crosses the rear member's
// exit lane after the split.
func couplingSplitExitScenario(t *testing.T) couplingMultiScenario {
	t.Helper()
	network := couplingMultiBase(t)
	couplingSplitExitAddStation(&network, "exit-origin", Point{X: 1000, Y: -260}, Point{X: 1030, Y: -240}, Point{X: 1055, Y: -220},
		Lane{ID: "exit-origin-feed", From: "exit-origin-exit", To: "c"})
	couplingSplitExitAddStation(&network, "far-goal", Point{X: 1600, Y: -200}, Point{X: 1650, Y: -225}, Point{X: 1710, Y: -225},
		Lane{ID: "far-road", From: "rear-exit", To: "far-goal-entry"})
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	return couplingMultiScenario{prepared: p,
		contracts: FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
			CouplingSites: couplingMultiSites(t, p, ""), CouplingCorridors: []CouplingCorridor{couplingMultiCorridor("")}},
		trips: []couplingMultiTrip{{"blocker", "origin", "block-goal", 1, 0}, {"front", "front-origin", "front-goal", 1, 0},
			{"rear", "rear-origin", "rear-goal", 2, 0}, {"exit-blocker", "exit-origin", "far-goal", 1, couplingSplitExitDeparture}}}
}

// Report whether the only candidate passes every ordinary readiness and
// reservation check at this boundary, and which resources of its static exit
// closure the exit blocker owns. When it owns any, preparing the motion
// context, which writes no live grants, must refuse at the closure. A wrong
// answer is reported, and the test continues so that Step shows the result.
func couplingSplitExitReady(t *testing.T, s *Simulation) (bool, []resource) {
	t.Helper()
	if len(s.couplingApproaches) != 1 {
		return false, nil
	}
	a := s.couplingApproaches[0]
	front, rear := s.findVehicle(a.context.members[0].id), s.findVehicle(a.context.members[1].id)
	if a.state.Phase != couplingApproachWaiting || front.follower != 0 || rear.link.leader != 0 || rear.Pod.Speed != 0 || rear.distance != a.context.rearTarget {
		return false, nil
	}
	input := a.context.formationInput(s, front, rear)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		return false, nil
	}
	static := &couplingMotionContext{reservation: reservation}
	if err = static.bindExits(); err != nil {
		t.Fatalf("ready reservation at tick %d has no static exit closure: %v", s.tick, err)
	}
	var blocked []resource
	for _, claim := range static.claims {
		if s.owners[claim.Resource].isPod("exit-blocker") {
			blocked = append(blocked, claim.Resource)
		}
	}
	if len(blocked) == 0 {
		return true, nil
	}
	var foreign []string
	for _, v := range s.vehicles {
		if v.Pod.ID != front.Pod.ID && v.Pod.ID != rear.Pod.ID {
			foreign = append(foreign, v.Pod.ID)
		}
	}
	_, err = prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "probe", ForeignIDs: foreign})
	if err == nil || !strings.Contains(err.Error(), "complete closure has a foreign typed owner") {
		t.Errorf("held exit %v at tick %d was not refused by the exit closure: %v", blocked, s.tick, err)
	}
	return true, blocked
}

// An ordinary pod holds the rear member's exit lane past the split when the
// natural pair is first ready. Formation waits, the pair keeps its partner
// hold, and the train forms only after the exit clears, then completes.
func TestCouplingSplitExitNaturalBlocker(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "occupied"}[occupied], func(t *testing.T) {
			t.Parallel()
			sc := couplingSplitExitScenario(t)
			s := sc.start(t, true)
			requests := make(map[string]int, len(sc.trips))
			sc.request(t, s, occupied, requests)
			completed := make(map[int]bool)
			phases := make(map[couplingReservationPhase]bool)
			blockedBy := make(map[resource]bool)
			var formation, retirement, firstRefusal, refusals int64 = -1, -1, -1, 0
			var waitTick, deadline int64 = -1, -1
			for s.tick < 36000 {
				ready, held := couplingSplitExitReady(t, s)
				before := maps.Clone(s.owners)
				var hold couplingApproachState
				if ready {
					hold = s.couplingApproaches[0].state
				}
				s.Step()
				checkCouplingApproachNativeBoundary(t, s)
				for _, completion := range s.StepCompletions() {
					if completed[completion.RequestID] {
						t.Fatal("completion replayed", completion)
					}
					completed[completion.RequestID] = true
				}
				if ready && len(held) > 0 {
					// The candidate stays held at its rest targets with its original wait.
					if len(s.couplingGroups) != 0 {
						t.Fatalf("pair formed at tick %d while the exit blocker owned %v", s.tick, held)
					}
					if len(s.couplingApproaches) != 1 {
						t.Fatalf("blocked candidate disappeared at tick %d", s.tick)
					}
					a := s.couplingApproaches[0]
					front := s.findVehicle(a.context.members[0].id)
					if a.state.Phase != couplingApproachWaiting || a.state.WaitTick != hold.WaitTick || a.state.DeadlineTick != hold.DeadlineTick ||
						front.distance != a.context.target || front.Pod.Speed != 0 {
						t.Fatalf("blocked candidate lost its partner hold at tick %d: %+v", s.tick, a.state)
					}
					refusals++
					if firstRefusal < 0 {
						firstRefusal, waitTick, deadline = s.tick, hold.WaitTick, hold.DeadlineTick
					}
					for _, r := range held {
						blockedBy[r] = true
					}
				}
				if formation < 0 && len(s.couplingGroups) != 0 {
					formation = s.tick
					c := s.couplingGroups[0].context
					members := [2]string{c.reservation.members[0].Vehicle.Pod.ID, c.reservation.members[1].Vehicle.Pod.ID}
					if members != [2]string{"front", "rear"} || !ready || len(held) != 0 {
						t.Fatalf("formation at tick %d did not follow a clear ready boundary: members=%v held=%v", s.tick, members, held)
					}
					claimed := make(map[resource]bool, len(c.claims))
					for _, claim := range c.claims {
						claimed[claim.Resource] = true
						if owner := before[claim.Resource]; !owner.isZero() && !owner.isPod("front") && !owner.isPod("rear") {
							t.Fatalf("pair formed at tick %d over %+v owned by %s", s.tick, claim.Resource, owner.id)
						}
					}
					// Every refused resource is a closure claim of the train that forms.
					for r := range blockedBy {
						if !claimed[r] {
							t.Fatalf("refused resource %+v is not in the formed exit closure", r)
						}
					}
				}
				if len(s.couplingGroups) != 0 {
					phases[s.couplingGroups[0].state.Phase] = true
				} else if formation >= 0 && retirement < 0 {
					retirement = s.tick
				}
				sc.request(t, s, occupied, requests)
				idle := len(requests) == len(sc.trips) && len(s.couplingGroups) == 0 && len(s.couplingApproaches) == 0
				for _, v := range s.vehicles {
					idle = idle && v.Pod.Activity == Idle
				}
				if !idle {
					continue
				}
				if refusals == 0 || formation <= firstRefusal || formation > deadline || retirement < 0 {
					t.Fatalf("fixture lost its blocked split exit: refusals=%d first=%d formation=%d deadline=%d retirement=%d", refusals, firstRefusal, formation, deadline, retirement)
				}
				for _, phase := range couplingMultiPhases {
					if !phases[phase] {
						t.Fatalf("train skipped mechanical phase %s", savedCouplingPhase(phase))
					}
				}
				for _, trip := range sc.trips {
					if v := s.findVehicle(trip.id); v.Pod.StationID != trip.goal || occupied && !completed[requests[trip.id]] {
						t.Fatalf("journey %s did not complete at %s: %+v", trip.id, trip.goal, v.Pod)
					}
				}
				t.Logf("wait=%d deadline=%d refused=%d..%d (%d ticks) blockedBy=%v formation=%d retirement=%d idle=%d",
					waitTick, deadline, firstRefusal, firstRefusal+refusals-1, refusals, slices.Collect(maps.Keys(blockedBy)), formation, retirement, s.tick)
				return
			}
			t.Fatalf("fleet missed its idle bound: refusals=%d formation=%d retirement=%d", refusals, formation, retirement)
		})
	}
}
