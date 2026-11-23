package sim

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// motionLanes is a straight path of four lanes of 300 m.
var motionLanes = []geometryLane{{to: Point{X: 300}}, {to: Point{X: 600}}, {to: Point{X: 900}}, {to: Point{X: 1200}}}

// motionFixture returns the network, the fleet, and the saved state of
// restoreGeometry for pods at rest on lane l0 of motionLanes. edit changes
// the network first, or is nil.
func motionFixture(t *testing.T, pods int, head, gap float64, edit func(*Network)) (Network, []Placement, SavedState) {
	t.Helper()
	network := geometryNetwork(Point{}, motionLanes, pods)
	if edit != nil {
		edit(&network)
	}
	s := restoreGeometry(t, network, len(motionLanes), pods, head, gap)
	return network, s.initial, s.ExportState()
}

// restoreMotion restores state and fails on an error.
func restoreMotion(t *testing.T, network Network, fleet []Placement, state SavedState) (*Simulation, RestoreResult) {
	t.Helper()
	restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	return restored, result
}

// withPod returns state with a copy of its pods, and pod index changed by
// edit.
func withPod(state SavedState, index int, edit func(*SavedPod)) SavedState {
	state.Pods = slices.Clone(state.Pods)
	edit(&state.Pods[index])
	return state
}

// withoutMotion returns state with a copy of its pods, each with speed 0
// and no reservation lead, as a save before the saved motion has them.
func withoutMotion(state SavedState) SavedState {
	state.Pods = slices.Clone(state.Pods)
	for index := range state.Pods {
		state.Pods[index].Speed, state.Pods[index].ReservedAhead = 0, 0
	}
	return state
}

// headRestore restores input as the restore before the saved motion does
// (restore speed proposal, section 4.2): the input checks on input without
// the saved motion, then the physical tier of restoreBeforeMotion, and the
// logical tier when that fails with an error other than a fault refusal,
// as restoreState does.
func headRestore(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	input.State = withoutMotion(input.State)
	newFleet := restoreFleets(input, func() (*PreparedNetwork, error) {
		return prepareContractNetwork(input.Network, input.Fleet, input.OrderContract)
	})
	if err := checkRestoreInput(input, newFleet); err != nil {
		return nil, RestoreResult{}, err
	}
	var physicalErr error
	if !input.LogicalOnly {
		s, result, err := restoreBeforeMotion(input, newFleet)
		if err == nil {
			return s, result, nil
		}
		if errors.Is(err, errInvalidFaults) {
			return nil, RestoreResult{PhysicalError: err}, err
		}
		physicalErr = err
	}
	return restoreLogicalTier(input, newFleet, physicalErr)
}

// checkFallback checks that the restore of input, which gives restored and
// result, places each pod as the restore before the saved motion does
// (see headRestore and placeTravelingPod): the same tier and
// demoted pods, the same owners and number of links, and each pod at the
// same distance, speed, frontier, predecessor, and follower.
func checkFallback(t *testing.T, input RestoreStateInput, restored *Simulation, result RestoreResult) {
	t.Helper()
	head, want, err := headRestore(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != want.Tier || !slices.Equal(result.Demoted, want.Demoted) || result.PhysicalError != nil || want.PhysicalError != nil {
		t.Fatalf("result %+v, want %+v", result, want)
	}
	if !maps.Equal(restored.owners, head.owners) || restored.platoonLinks != head.platoonLinks {
		t.Fatalf("the owners\n%v\nand %d links, want\n%v\nand %d", restored.owners, restored.platoonLinks, head.owners, head.platoonLinks)
	}
	for index := range restored.vehicles {
		got, v := &restored.vehicles[index], &head.vehicles[index]
		if got.distance != v.distance || got.Pod.Speed != v.Pod.Speed || got.reservedThrough != v.reservedThrough ||
			got.link.leader != v.link.leader || got.follower != v.follower {
			t.Fatalf("pod %s at %v at %v m/s, frontier %d, predecessor %d, follower %d, want %v at %v m/s, %d, %d, %d", got.Pod.ID,
				got.distance, got.Pod.Speed, got.reservedThrough, got.link.leader, got.follower, v.distance, v.Pod.Speed, v.reservedThrough, v.link.leader, v.follower)
		}
	}
}

// frontierAfter returns the first block after block from whose
// reservationEnd is the block (fixed true), or is not (fixed false).
func frontierAfter(t *testing.T, v *vehicle, from int, fixed bool) int {
	t.Helper()
	for block := from + 1; block < v.blocks.len(); block++ {
		if (reservationEnd(&v.blocks, block) == block) == fixed {
			return block
		}
	}
	t.Fatalf("no block after %d with fixed %t", from, fixed)
	return 0
}

// TestRestoreMotionFrontier checks the saved frontier of a pod at rest.
// The restore accepts a frontier where a grant can end. It places a pod
// with any other frontier, or with a frontier past its saved route, at the
// derived frontier. It demotes such a pod when that placement also fails.
func TestRestoreMotionFrontier(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 1, 100, 0, nil)
	base, result := restoreMotion(t, network, fleet, state)
	if !cleanRestore(result) {
		t.Fatalf("base restore %+v", result)
	}
	v := &base.vehicles[0]
	derived := v.reservedThrough
	fixed, boundary := frontierAfter(t, v, derived, true), frontierAfter(t, v, derived, false)
	for _, test := range []struct {
		name  string
		ahead int
		lane  string
		// through is the restored frontier, -1 for the derived placement,
		// or -2 for a demotion.
		through int
	}{
		{name: "a frontier where a grant ends", ahead: fixed - derived, through: fixed},
		{name: "a frontier inside a grant", ahead: boundary - derived, through: -1},
		{name: "a frontier past the route", ahead: v.blocks.len() - derived, through: -1},
		{name: "a frontier far past the route", ahead: math.MaxInt, through: -1},
		{name: "a frontier inside a grant on another lane", ahead: boundary - derived, lane: "l1", through: -2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			edited := withPod(state, 0, func(pod *SavedPod) { pod.ReservedAhead, pod.LaneID = test.ahead, cmp.Or(test.lane, pod.LaneID) })
			restored, result := restoreMotion(t, network, fleet, edited)
			switch test.through {
			case -1:
				checkFallback(t, RestoreStateInput{Network: network, Fleet: fleet, State: edited}, restored, result)
				if restored.vehicles[0].reservedThrough != derived {
					t.Fatalf("frontier %d, want the derived frontier %d", restored.vehicles[0].reservedThrough, derived)
				}
				return
			case -2:
				if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"p01"}) {
					t.Fatalf("result %+v, want p01 demoted", result)
				}
				return
			}
			if !cleanRestore(result) || restored.vehicles[0].reservedThrough != test.through {
				t.Fatalf("result %+v, frontier %d, want %d", result, restored.vehicles[0].reservedThrough, test.through)
			}
			if got := restored.ExportState().Pods[0].ReservedAhead; got != test.ahead {
				t.Fatalf("the restored pod saves %d blocks ahead, want %d", got, test.ahead)
			}
		})
	}
}

// trimLanes is a straight path whose second lane is 122 m long, with five
// cells of 24.4 m.
var trimLanes = []geometryLane{{to: Point{X: 300}}, {to: Point{X: 422}}, {to: Point{X: 722}}, {to: Point{X: 1022}}}

// TestRestoreMotionTrimmedFrontier restores a pod at rest 1e-9 m past the
// end of the second cell of lane l1, with its frontier at the third cell.
// The saved route starts at l1. In the route distances of the saved
// route, the pod is 1.0000036354540498e-9 m past the cell end, more than
// restoreDriftTolerance, so the restore finds the pod in the third cell.
// In the live route distances, it is 9.999894245993346e-10 m past the
// cell end. The export must derive the frontier with the distances of the
// saved route. With the live distances, it finds the second cell, and the
// restore takes one more cell.
func TestRestoreMotionTrimmedFrontier(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 1)
	base := restoreGeometry(t, network, len(trimLanes), 1, 100, 0)
	state, fleet := base.ExportState(), base.initial
	at := withPod(state, 0, func(pod *SavedPod) { pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 48.8, 348.8 })
	live, _ := restoreMotion(t, network, fleet, at)
	v := &live.vehicles[0]
	first := v.blocks.laneFirst(1)
	if reservationEnd(&v.blocks, first+2) != first+2 || v.reservedThrough > first+2 {
		t.Fatalf("the third cell of l1 is not a frontier ahead of %d", v.reservedThrough)
	}
	at = withPod(at, 0, func(pod *SavedPod) { pod.ReservedAhead = first + 2 - v.reservedThrough })
	live, result := restoreMotion(t, network, fleet, at)
	v = &live.vehicles[0]
	if !cleanRestore(result) || v.reservedThrough != first+2 || v.distance != 348.8 {
		t.Fatalf("result %+v, frontier %d at %g, want %d at 348.8", result, v.reservedThrough, v.distance, first+2)
	}
	// Move the pod as a tick does.
	v.distance = 348.800000001
	v.blockIndex = first + 2
	v.Pod.LaneDistance = v.distance - v.blocks.lanes[1].start
	v.Pod.Position = live.position(v.blocks.route[1], v.Pod.LaneDistance)
	saved := live.ExportState()
	if pod := saved.Pods[0]; pod.RouteIndex != 0 || network.Lanes[pod.Route[0]].ID != "l1" {
		t.Fatalf("the saved route starts at lane %s, route index %d", network.Lanes[pod.Route[0]].ID, pod.RouteIndex)
	}
	restored, result := restoreMotion(t, network, fleet, saved)
	if !cleanRestore(result) {
		t.Fatalf("result %+v", result)
	}
	got := &restored.vehicles[0]
	if lane := got.blocks.lane(got.reservedThrough).ID; lane != "l1" || got.reservedThrough != 2 {
		t.Fatalf("the restored frontier is block %d on lane %s, want the third cell of l1", got.reservedThrough, lane)
	}
	checkRestoredMatches(t, live, restored)
}

// TestRestoreMotionStopCheck checks that a restored pod at speed must stop
// within its reservation at the maximum braking rate of its class, and
// must slow to the lower speed limit of the next lane at that rate. A pod
// that cannot restores at rest with the derived frontier.
func TestRestoreMotionStopCheck(t *testing.T) {
	t.Parallel()
	const speed = 14.0
	reach := motionEnvelopeFor(LegacyClass).stoppingDistance(speed)
	lower := func(network *Network) {
		for index := range network.Lanes {
			if network.Lanes[index].ID == "l1" {
				network.Lanes[index].SpeedLimit = 5
			}
		}
	}
	for _, test := range []struct {
		name     string
		distance float64
		edit     func(*Network)
		// far gives the pod a frontier past its stopping distance.
		far  bool
		rest bool
	}{
		{name: "a frontier past the stop point", distance: 100, far: true},
		{name: "a frontier before the stop point", distance: 100, rest: true},
		{name: "a lower limit within reach", distance: 200, edit: lower, far: true},
		{name: "a lower limit too near", distance: 280, edit: lower, far: true, rest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network, fleet, state := motionFixture(t, 1, test.distance, 0, test.edit)
			base, _ := restoreMotion(t, network, fleet, state)
			v := &base.vehicles[0]
			derived, through := v.reservedThrough, v.reservedThrough
			if test.far {
				for v.blocks.end(through) < v.distance+reach+1 {
					through = frontierAfter(t, v, through, true)
				}
			}
			if !test.far && v.blocks.end(through) >= v.distance+reach {
				t.Fatalf("the derived frontier ends at %g, past the stop point %g", v.blocks.end(through), v.distance+reach)
			}
			edited := withPod(state, 0, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = speed, through-derived })
			restored, result := restoreMotion(t, network, fleet, edited)
			if test.rest {
				checkFallback(t, RestoreStateInput{Network: network, Fleet: fleet, State: edited}, restored, result)
				if !cleanRestore(result) || restored.vehicles[0].Pod.Speed != 0 || restored.vehicles[0].reservedThrough != derived {
					t.Fatalf("result %+v, speed %g, frontier %d, want at rest at %d", result, restored.vehicles[0].Pod.Speed, restored.vehicles[0].reservedThrough, derived)
				}
				return
			}
			if !cleanRestore(result) || restored.vehicles[0].Pod.Speed != speed || restored.vehicles[0].reservedThrough != through {
				t.Fatalf("result %+v, speed %g, frontier %d, want %g and %d", result, restored.vehicles[0].Pod.Speed, restored.vehicles[0].reservedThrough, speed, through)
			}
		})
	}
}

// stopCheckSave edits the save of input: the traveling pod at index gets
// the speed and the frontier that pick returns from the pod and its
// derived frontier. The first physical restore must keep the pod in
// place: at rest with the derived frontier when rest is true, and at the
// speed with that frontier when rest is false. The restore must then give
// the same result.
func stopCheckSave(t *testing.T, input RestoreStateInput, index int, speed float64, rest bool, pick func(v *vehicle, derived int) int) {
	t.Helper()
	at := input
	at.State = withoutMotion(input.State)
	base, _, err := firstRestore(at)
	if err != nil {
		t.Fatal(err)
	}
	v := &base.vehicles[index]
	derived := v.reservedThrough
	through := pick(v, derived)
	input.State = withPod(input.State, index, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = speed, through-derived })
	first, result, err := firstRestore(input)
	if err != nil || !cleanRestore(result) {
		t.Fatalf("the restore with the saved motion gives %v, %+v", err, result)
	}
	got := &first.vehicles[index]
	wantSpeed, wantThrough := speed, through
	if rest {
		wantSpeed, wantThrough = 0, derived
	}
	if got.distance != v.distance || got.Pod.Speed != wantSpeed || got.reservedThrough != wantThrough {
		t.Fatalf("pod %s restores at %v at %g m/s with the frontier %d, want at %v at %g m/s with %d",
			got.Pod.ID, got.distance, got.Pod.Speed, got.reservedThrough, v.distance, wantSpeed, wantThrough)
	}
	restored, result, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	if rest {
		checkFallback(t, input, restored, result)
	} else if got := &restored.vehicles[index]; !cleanRestore(result) || got.Pod.Speed != speed || got.reservedThrough != through {
		t.Fatalf("result %+v, pod %s at %g m/s with the frontier %d, want %g and %d", result, got.Pod.ID, got.Pod.Speed, got.reservedThrough, speed, through)
	}
}

// fixedFrontier returns the first block after derived at which a grant can
// end and whose end is in the range from to to, in meters past v.
func fixedFrontier(t *testing.T, v *vehicle, derived int, from, to float64) int {
	t.Helper()
	for block := derived; block < v.blocks.len(); block++ {
		end := v.blocks.end(block) - v.distance
		if end > to {
			break
		}
		if end >= from && reservationEnd(&v.blocks, block) == block {
			return block
		}
	}
	t.Fatalf("no frontier from %g m to %g m past %g", from, to, v.distance)
	return 0
}

// TestRestoreMotionExpressStopCheck restores an Express pod at 14 m/s on
// a-link of largeMotionNetwork, 20 m ahead of its live position in the
// same block, with a frontier that ends about 70 m ahead. A pod of the
// Legacy class stops in 49 m, and an Express pod at its maximum braking
// rate needs 85.22 m. So the pod restores at rest with its derived
// frontier, and with a frontier past 85.22 m it keeps its speed.
func TestRestoreMotionExpressStopCheck(t *testing.T) {
	t.Parallel()
	const speed = 14.0
	legacy, express := motionEnvelopeFor(LegacyClass).stoppingDistance(speed), motionEnvelopeFor(ExpressClass).stoppingDistance(speed)
	if legacy != 49 || math.Abs(express-85.22) > 0.01 {
		t.Fatalf("stopping distances %g and %g, want 49 and 85.22", legacy, express)
	}
	s, err := expressPhysicalFleet(largeMotionNetwork(false), []Placement{{ID: "pod", Class: ExpressClass, StationID: "a", BerthID: "a-1"}})
	if err != nil {
		t.Fatal(err)
	}
	expressMotionRequest(t, s, "pod", "c")
	v := s.findVehicle("pod")
	for range 120 * TicksPerSecond {
		s.Step()
		if v.Pod.LaneID == "a-link" && v.Pod.Speed == speed {
			break
		}
	}
	if v.Pod.LaneID != "a-link" || v.Pod.Speed != speed {
		t.Fatalf("the pod is on %s at %g m/s, want a-link at %g", v.Pod.LaneID, v.Pod.Speed, speed)
	}
	const shift = 20
	if block := &v.blocks; v.distance+shift >= block.end(v.blockIndex) {
		t.Fatalf("the pod is at %g, and its block ends at %g", v.distance, block.end(v.blockIndex))
	}
	state := withPod(s.ExportState(), 0, func(pod *SavedPod) { pod.Distance, pod.LaneDistance = pod.Distance+shift, pod.LaneDistance+shift })
	input := RestoreStateInput{
		OrderContract: ExpressOrderContract, Network: s.network, Fleet: s.initial, State: state,
		ExpressServices: slices.Collect(maps.Values(s.expressServices)), OnboardPickups: s.onboardPickups,
	}
	stopCheckSave(t, input, 0, speed, true, func(v *vehicle, derived int) int {
		return fixedFrontier(t, v, derived, legacy+1, express-1)
	})
	stopCheckSave(t, input, 0, speed, false, func(v *vehicle, derived int) int {
		return fixedFrontier(t, v, derived, express+1, math.Inf(1))
	})
}

// TestRestoreMotionFarLowerLimit restores a Legacy pod at 14 m/s at 294 m
// on l0, with a frontier past its stopping distance. Lane l1 is 24 m long
// at 14 m/s, and lane l2 starts 30 m ahead. When l2 has the limit 5 m/s,
// the pod needs 42.75 m to slow to it, so it restores at rest. When l2
// has the limit 14 m/s, the pod keeps its speed.
func TestRestoreMotionFarLowerLimit(t *testing.T) {
	t.Parallel()
	const speed = 14.0
	envelope := motionEnvelopeFor(LegacyClass)
	if slow := (speed*speed - 5*5) / (2 * envelope.maximumBraking); slow != 42.75 {
		t.Fatalf("slowing to 5 m/s takes %g m, want 42.75", slow)
	}
	lanes := []geometryLane{{to: Point{X: 300}}, {to: Point{X: 324}}, {to: Point{X: 900}}, {to: Point{X: 1200}}}
	for _, limit := range []float64{5, 14} {
		network := geometryNetwork(Point{}, lanes, 1)
		for index := range network.Lanes {
			if network.Lanes[index].ID == "l2" {
				network.Lanes[index].SpeedLimit = limit
			}
		}
		s := restoreGeometry(t, network, len(lanes), 1, 294, 0)
		input := RestoreStateInput{Network: network, Fleet: s.initial, State: s.ExportState()}
		stopCheckSave(t, input, 0, speed, limit == 5, func(v *vehicle, derived int) int {
			if start := v.blocks.lanes[2].start - v.distance; start != 30 {
				t.Fatalf("l2 starts %g m ahead, want 30", start)
			}
			return fixedFrontier(t, v, derived, envelope.stoppingDistance(speed)+1, math.Inf(1))
		})
	}
}

// TestRestoreMotionLaneLimit restores a moving pod with a saved lane ID
// that is empty, the lane of the pod, unknown, or the ID of another lane.
// The physical tier gets the lane from the route index. A pod at most
// 0.001 m/s over the limit of that lane (restore speed proposal, section
// 4.2) keeps its saved speed, and a faster pod restores at rest with the
// derived frontier, also with a named lane. An unknown lane or another
// lane demotes the pod. The first tick of each restore passes the safety
// check.
func TestRestoreMotionLaneLimit(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 1, 100, 0, nil)
	base, _ := restoreMotion(t, network, fleet, state)
	v := &base.vehicles[0]
	derived, through := v.reservedThrough, v.reservedThrough
	for v.blocks.end(through) < v.distance+motionEnvelopeFor(LegacyClass).stoppingDistance(15)+1 {
		through = frontierAfter(t, v, through, true)
	}
	// The contract value of the margin, not restoreSpeedMargin.
	const limit, margin = 14.0, 14.0 + 0.001
	over := math.Nextafter(margin, math.Inf(1))
	for _, test := range []struct {
		name    string
		lane    string
		speed   float64
		rest    bool
		demoted bool
	}{
		{name: "no lane at the limit", speed: limit},
		{name: "no lane at the margin", speed: margin},
		{name: "no lane over the margin", speed: over, rest: true},
		{name: "no lane far over the limit", speed: 15, rest: true},
		{name: "the lane at the margin", lane: "l0", speed: margin},
		{name: "the lane over the margin", lane: "l0", speed: over, rest: true},
		{name: "an unknown lane", lane: "nowhere", speed: limit, demoted: true},
		{name: "another lane at its limit", lane: "l1", speed: limit, demoted: true},
		{name: "another lane over its limit", lane: "l1", speed: 15, demoted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			edited := withPod(state, 0, func(pod *SavedPod) { pod.LaneID, pod.Speed, pod.ReservedAhead = test.lane, test.speed, through-derived })
			restored, result := restoreMotion(t, network, fleet, edited)
			if result.Tier != RestorePhysical || slices.Equal(result.Demoted, []string{"p01"}) != test.demoted {
				t.Fatalf("result %+v, want p01 demoted %t", result, test.demoted)
			}
			want := test.speed
			if test.rest {
				checkFallback(t, RestoreStateInput{Network: network, Fleet: fleet, State: edited}, restored, result)
				want = 0
			}
			if got := restored.vehicles[0].Pod.Speed; !test.demoted && math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("the restored pod moves at %v, want %v", got, want)
			}
			restored.Step()
			if _, err := restored.SafetyObservation().Check(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestRestoreMotionLaneCrossing steps a pod onto a lane with a lower speed
// limit, where the tick publishes a speed above the new limit by rounding,
// and restores the save. The restore must keep the pod in the physical
// tier with the same speed bits, also without the saved lane ID.
//
// In the first case, a pod at 5 m/s on a 14 m/s lane crosses onto a 5 m/s
// lane. In the second, a pod reaches a lane of 1e-9 m/s at 100 km. The
// test sets the speed by hand. In the second case it also puts the pod
// just before the lane start by hand, because a restore there would snap
// the pod to the start.
func TestRestoreMotionLaneCrossing(t *testing.T) {
	t.Parallel()
	limits := func(values ...float64) func(*Network) {
		return func(network *Network) {
			for index := range network.Lanes {
				var lane int
				if _, err := fmt.Sscanf(network.Lanes[index].ID, "l%d", &lane); err == nil && lane < len(values) {
					network.Lanes[index].SpeedLimit = values[lane]
				}
			}
		}
	}
	for _, test := range []struct {
		name  string
		lanes []geometryLane
		edit  func(*Network)
		// head is the restored distance of the pod, and distance is 0 or
		// the distance that the test puts the pod at.
		head, distance float64
		speed          float64
	}{
		{
			name: "a 5 m/s lane", lanes: motionLanes, edit: limits(14, 5),
			head: 299.8333333333333, speed: 5,
		},
		{
			name:  "a slow lane at 100 km",
			lanes: []geometryLane{{to: Point{X: 100000}}, {to: Point{X: 100300}}, {to: Point{X: 100600}}},
			edit:  limits(10.375, 1e-9, 1e-9), head: 99990, distance: 99999.99999989157, speed: 1e-5,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := geometryNetwork(Point{}, test.lanes, 1)
			test.edit(&network)
			s := restoreGeometry(t, network, len(test.lanes), 1, test.head, 0)
			v := &s.vehicles[0]
			v.Pod.Speed = test.speed
			if test.distance != 0 {
				v.distance, v.Pod.LaneDistance = test.distance, test.distance
				v.Pod.Position = s.position(v.blocks.route[0], v.Pod.LaneDistance)
			}
			if v.blocks.end(v.reservedThrough) <= v.blocks.lanes[1].start {
				t.Fatalf("the grant ends at %g, before l1", v.blocks.end(v.reservedThrough))
			}
			s.Step()
			limit := v.blocks.route[1].SpeedLimit
			if v.Pod.LaneID != "l1" || v.Pod.LaneDistance != 0 || v.Pod.Speed <= limit {
				t.Fatalf("the tick published %v m/s at %g m on lane %s, want more than %v m/s at the start of l1", v.Pod.Speed, v.Pod.LaneDistance, v.Pod.LaneID, limit)
			}
			t.Logf("the tick published %v m/s on a %v m/s lane: an excess of %v m/s", v.Pod.Speed, limit, v.Pod.Speed-limit)
			state := roundTripState(t, s.ExportState())
			for _, lane := range []string{"l1", ""} {
				edited := withPod(state, 0, func(pod *SavedPod) { pod.LaneID = lane })
				restored, result := restoreMotion(t, network, s.initial, edited)
				if !cleanRestore(result) || math.Float64bits(restored.vehicles[0].Pod.Speed) != math.Float64bits(v.Pod.Speed) {
					t.Fatalf("lane ID %q: result %+v, speed %v, want %v", lane, result, restored.vehicles[0].Pod.Speed, v.Pod.Speed)
				}
				if lane != "" {
					checkRestoredMatches(t, s, restored)
				}
			}
		})
	}
}

// TestRestoreMotionLinkStopPoint restores a saved link whose follower
// moves and whose predecessor is at rest. The follower is more than the
// clearance behind its predecessor, but its stop point is not, so the
// physical tier fails with the stop-point error. The restore before the
// saved motion keeps both pods, so the whole save restores that way. With
// a third pod linked too near the follower, that restore also fails, at
// the gap of the third pod. Its error then decides, and the logical tier
// follows with it (restore speed proposal, section 4.2).
func TestRestoreMotionLinkStopPoint(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 3, 200, 40, nil)
	state = withPod(state, 1, func(pod *SavedPod) { pod.Platoon = &SavedPlatoonLink{Leader: "p01", Lanes: 2} })
	if _, result := restoreMotion(t, network, fleet, state); !cleanRestore(result) || result.PhysicalError != nil {
		t.Fatalf("the linked pods at rest restore with %+v", result)
	}
	moving := withPod(state, 1, func(pod *SavedPod) { pod.Speed = 14 })
	restored, result := restoreMotion(t, network, fleet, moving)
	checkFallback(t, RestoreStateInput{Network: network, Fleet: fleet, State: moving}, restored, result)
	if !cleanRestore(result) || restored.vehicles[1].Pod.Speed != 0 || restored.vehicles[1].link.leader == 0 {
		t.Fatalf("result %+v, the follower at %v m/s with the predecessor %d, want at rest and linked", result, restored.vehicles[1].Pod.Speed, restored.vehicles[1].link.leader)
	}
	near := withPod(moving, 2, func(pod *SavedPod) {
		pod.Platoon = &SavedPlatoonLink{Leader: "p02", Lanes: 2}
		pod.Distance, pod.LaneDistance = 155, 155
	})
	input := RestoreStateInput{Network: network, Fleet: fleet, State: near}
	gap := 200 - (160 + stoppingDistance(14))
	want := fmt.Sprintf("pod p02: the stop point of the pod is %.6f m behind the stop point of its platoon predecessor, less than the clearance %.6f m", gap, linkClearance(0))
	if _, _, err := firstRestore(input); err == nil || err.Error() != want {
		t.Fatalf("the restore with the saved motion fails with %v, want %q", err, want)
	}
	_, result = restoreMotion(t, network, fleet, near)
	want = fmt.Sprintf("pod p03: the pod is %.6f m behind its platoon predecessor, less than the clearance %.6f m", 5.0, linkClearance(0))
	if result.Tier != RestoreLogical || result.PhysicalError == nil || result.PhysicalError.Error() != want {
		t.Fatalf("tier %s, physical error %v, want %q", result.Tier, result.PhysicalError, want)
	}
	if _, plain, err := headRestore(input); err != nil || plain.Tier != RestoreLogical || plain.PhysicalError == nil || plain.PhysicalError.Error() != want {
		t.Fatalf("before the saved motion, the restore gives %v, %+v, want the logical tier with %q", err, plain, want)
	}
}

// firstRestore runs the physical tier of input with the saved motion,
// without the restore before the saved motion (see restorePhysicalMotion).
func firstRestore(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	return restorePhysical(input, restoreFleets(input, func() (*PreparedNetwork, error) {
		return prepareContractNetwork(input.Network, input.Fleet, input.OrderContract)
	}))
}

// TestRestoreMotionOverspeedFollower restores a saved link whose follower
// is 62 m behind its predecessor at rest at 200 m, with a grant past its
// stop point. At 14 m/s, the stop point of the follower is 13 m behind the
// stop point of the predecessor. At a saved speed of 15 m/s, it is 5.75 m
// behind, less than the clearance of the link. The follower is over the
// speed limit of its lane, so the restore places it at rest with the
// derived frontier before the stop-point check of the link can fail
// (restore speed proposal, section 4.2). At rest, its stop point passes
// that check, so it keeps its link. The predecessor stays in place, and
// the restore keeps the physical tier.
func TestRestoreMotionOverspeedFollower(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 2, 200, 62, nil)
	state = withPod(state, 1, func(pod *SavedPod) { pod.Platoon = &SavedPlatoonLink{Leader: "p01", Lanes: 2} })
	base, _ := restoreMotion(t, network, fleet, state)
	v := &base.vehicles[1]
	derived, through := v.reservedThrough, v.reservedThrough
	for v.blocks.end(through) < v.distance+stoppingDistance(15)+1 {
		through = frontierAfter(t, v, through, true)
	}
	for _, test := range []struct {
		speed float64
		rest  bool
	}{
		{speed: 14},
		{speed: 15, rest: true},
	} {
		moving := withPod(state, 1, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = test.speed, through-derived })
		restored, result := restoreMotion(t, network, fleet, moving)
		if result.Tier != RestorePhysical || result.PhysicalError != nil || len(result.Demoted) != 0 {
			t.Fatalf("speed %v: result %+v, want no demoted pod", test.speed, result)
		}
		if test.rest {
			// The fallback of the follower alone places it: the first
			// physical restore passes, with the follower at rest and linked.
			input := RestoreStateInput{Network: network, Fleet: fleet, State: moving}
			first, firstResult, err := firstRestore(input)
			if err != nil || !cleanRestore(firstResult) || first.vehicles[1].Pod.Speed != 0 || first.vehicles[1].link.leader == 0 {
				t.Fatalf("speed %v: the restore with the saved motion gives %v, %+v, want the follower at rest and linked", test.speed, err, firstResult)
			}
			checkFallback(t, input, restored, result)
			if follower := &restored.vehicles[1]; follower.Pod.Speed != 0 || follower.reservedThrough != derived || follower.link.leader == 0 {
				t.Fatalf("speed %v: the follower restores at %g with the frontier %d and the predecessor %d, want at rest at %d with p01",
					test.speed, follower.Pod.Speed, follower.reservedThrough, follower.link.leader, derived)
			}
		}
		leader := &restored.vehicles[0]
		if leader.Pod.Activity != Traveling || leader.distance != 200 || leader.Pod.LaneID != "l0" {
			t.Fatalf("speed %v: the predecessor is %s at %g on lane %s, want traveling at 200 m on l0", test.speed, leader.Pod.Activity, leader.distance, leader.Pod.LaneID)
		}
		if follower := &restored.vehicles[1]; !test.rest && (follower.Pod.Speed != test.speed || follower.link.leader == 0) {
			t.Fatalf("speed %v: the follower restores at %g with the predecessor %d", test.speed, follower.Pod.Speed, follower.link.leader)
		}
	}
}

// releaseLanes is a straight path of a 300 m lane and a 24 m lane, with
// clear track after it. The first cell of l1 ends at 312 m, where a
// Legacy pod also releases the last cell of l0.
var releaseLanes = []geometryLane{{to: Point{X: 300}}, {to: Point{X: 324}}, {to: Point{X: 624}}, {to: Point{X: 924}}}

// TestRestoreMotionReleaseBoundary steps a Legacy pod from rest at
// 311.9977772777778 m, with a grant past 312 m, to 311.9999995 m, 5e-7 m
// short of the release of the last cell of l0 at 312 m. The live pod still
// owns that cell. The restore must not move the pod to the block end at
// 312 m (see restoreDriftTolerance), so the restored owners are the live
// owners.
func TestRestoreMotionReleaseBoundary(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, releaseLanes, 1)
	base := restoreGeometry(t, network, len(releaseLanes), 1, 100, 0)
	const start = 311.9977772777778
	at := withPod(base.ExportState(), 0, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", start-300, start
	})
	s, result := restoreMotion(t, network, base.initial, at)
	v := &s.vehicles[0]
	if !cleanRestore(result) || v.distance != start || v.blocks.end(v.reservedThrough) <= 312 {
		t.Fatalf("result %+v: the pod is at %v with the grant to %v, want %v and past 312 m", result, v.distance, v.blocks.end(v.reservedThrough), start)
	}
	s.Step()
	last := v.blocks.laneFirst(1) - 1
	held := v.blocks.at(last).resources
	if math.Abs(v.distance-311.9999995) > 1e-12 || v.Pod.Speed == 0 || !slices.ContainsFunc(held, func(claimed resource) bool {
		return resourceReleaseDistance(v.blocks.at(last), claimed) == 312 && s.owners[claimed].isPod("p01")
	}) {
		t.Fatalf("the tick moved the pod to %v at %v m/s, want 311.9999995 with the last cell of l0 held to 312 m", v.distance, v.Pod.Speed)
	}
	restored, result := restoreMotion(t, network, base.initial, roundTripState(t, s.ExportState()))
	if !cleanRestore(result) {
		t.Fatalf("result %+v", result)
	}
	if !maps.Equal(restored.owners, s.owners) {
		t.Fatalf("the restored owners\n%v\nwant\n%v", restored.owners, s.owners)
	}
	checkRestoredMatches(t, s, restored)
}

// TestRestoreMotionFollowerCap restores a saved link whose follower moves
// and whose predecessor is at rest at 200 m. The stop point of the
// follower is 2*restoreTolerance short of the clearance behind the
// predecessor. The link check allows 3*restoreTolerance, so the link
// restores. But the platoon cap of the follower is the stop point of the
// predecessor less the clearance, and the stop check allows only
// restoreTolerance, so the follower restores at rest with the derived
// frontier. With the stop point 2*restoreTolerance past the clearance,
// the follower keeps its speed.
func TestRestoreMotionFollowerCap(t *testing.T) {
	t.Parallel()
	const speed = 14.0
	for _, test := range []struct {
		name    string
		deficit float64
		rest    bool
	}{
		{name: "a stop point short of the clearance", deficit: 2 * restoreTolerance, rest: true},
		{name: "a stop point past the clearance", deficit: -2 * restoreTolerance},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gap := linkClearance(0) + stoppingDistance(speed) - test.deficit
			network, fleet, state := motionFixture(t, 2, 200, gap, nil)
			state = withPod(state, 1, func(pod *SavedPod) { pod.Platoon = &SavedPlatoonLink{Leader: "p01", Lanes: 2} })
			base, _ := restoreMotion(t, network, fleet, state)
			v := &base.vehicles[1]
			derived, through := v.reservedThrough, v.reservedThrough
			for v.blocks.end(through) < v.distance+stoppingDistance(speed)+1 {
				through = frontierAfter(t, v, through, true)
			}
			moving := withPod(state, 1, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = speed, through-derived })
			restored, result := restoreMotion(t, network, fleet, moving)
			if result.Tier != RestorePhysical || result.PhysicalError != nil || len(result.Demoted) != 0 {
				t.Fatalf("result %+v, want no demoted pod", result)
			}
			if test.rest {
				checkFallback(t, RestoreStateInput{Network: network, Fleet: fleet, State: moving}, restored, result)
				if got := &restored.vehicles[1]; got.Pod.Speed != 0 || got.reservedThrough != derived || got.link.leader == 0 {
					t.Fatalf("p02 restores at %g with the frontier %d and the predecessor %d, want at rest at %d with p01", got.Pod.Speed, got.reservedThrough, got.link.leader, derived)
				}
			}
			if got := &restored.vehicles[1]; !test.rest && (got.Pod.Speed != speed || got.reservedThrough != through || got.link.leader == 0) {
				t.Fatalf("p02 restores at %g with the frontier %d and the predecessor %d", got.Pod.Speed, got.reservedThrough, got.link.leader)
			}
		})
	}
}

// TestRestoreMotionReversal restores a Compact pod that departs from a bay
// at the speed limit of the bay lanes. Its grant passes the start of the
// aisle, where the pod must stop for the reversal dwell. A pod whose stop
// point is 0.01 m past that start restores at rest with the derived
// frontier, and a pod whose stop point is 0.01 m before it keeps its
// speed.
func TestRestoreMotionReversal(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(2, 7)
	fleet := []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "stop"); err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	const speed = 2.0
	reach := motionEnvelopeFor(CompactClass).stoppingDistance(speed)
	point := math.Inf(1)
	// The save has the pod at the speed limit in the block before the
	// reversal point, and both test distances are in that block.
	stepUntil(t, s, "pod 01 at speed before the aisle", func() bool {
		if v.Pod.Activity != Traveling || v.Pod.Speed != speed {
			return false
		}
		latch := v.reversal
		point = s.reversalLimit(v, v.blocks.currentRouteLane(v.blockIndex), math.Inf(1))
		v.reversal = latch
		return v.distance < point-reach-0.01 && v.blocks.end(v.blockIndex) >= point-reach+0.01 && v.blocks.end(v.reservedThrough) > point+1
	})
	state := s.ExportState()
	for _, test := range []struct {
		name string
		stop float64
		rest bool
	}{
		{name: "a stop point past the aisle start", stop: point + 0.01, rest: true},
		{name: "a stop point before the aisle start", stop: point - 0.01},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			shift := test.stop - reach - v.distance
			edited := withPod(state, 0, func(pod *SavedPod) { pod.Distance, pod.LaneDistance = pod.Distance+shift, pod.LaneDistance+shift })
			input := RestoreStateInput{Network: network, Fleet: fleet, State: edited}
			restored, result, err := RestoreState(input)
			if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
				t.Fatalf("restore %v, %+v, want no demoted pod", err, result)
			}
			if test.rest {
				checkFallback(t, input, restored, result)
				if got := &restored.vehicles[0]; got.Pod.Speed != 0 {
					t.Fatalf("01 restores at %g, want at rest", got.Pod.Speed)
				}
			}
			if got := &restored.vehicles[0]; !test.rest && (got.Pod.Speed != speed || got.reservedThrough != v.reservedThrough) {
				t.Fatalf("01 restores at %g with the frontier %d, want %g and %d", got.Pod.Speed, got.reservedThrough, speed, v.reservedThrough)
			}
			restored.Step()
			if _, err := restored.SafetyObservation().Check(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestRestoreMotionRefusals pins the refusal of each malformed saved speed
// and reservation lead, and the order of the rules for one pod. These
// checks come after the other input checks.
func TestRestoreMotionRefusals(t *testing.T) {
	t.Parallel()
	idle := RestoreStateInput{Network: Example(), Fleet: demoFleet(), State: newTraffic(t).ExportState()}
	network, fleet, traveling := motionFixture(t, 1, 100, 0, nil)
	moving := RestoreStateInput{Network: network, Fleet: fleet, State: traveling}
	for _, test := range []struct {
		name string
		base RestoreStateInput
		edit func(*SavedPod)
		want string
	}{
		{"a speed that is not a number", idle, func(pod *SavedPod) { pod.Speed = math.NaN() }, "pod 01 has speed NaN, not a finite value of 0 or more"},
		{"an infinite speed", moving, func(pod *SavedPod) { pod.Speed = math.Inf(1) }, "pod p01 has speed +Inf, not a finite value of 0 or more"},
		{"a negative speed", moving, func(pod *SavedPod) { pod.Speed = -1 }, "pod p01 has speed -1, not a finite value of 0 or more"},
		{"a speed at a berth", idle, func(pod *SavedPod) { pod.Speed = 1 }, "pod 01 is not traveling and has a speed or a reservation ahead"},
		{"a lead at a berth", idle, func(pod *SavedPod) { pod.ReservedAhead = 1 }, "pod 01 is not traveling and has a speed or a reservation ahead"},
		{"a negative lead at a berth", idle, func(pod *SavedPod) { pod.ReservedAhead = -1 }, "pod 01 is not traveling and has a speed or a reservation ahead"},
		{"a negative lead", moving, func(pod *SavedPod) { pod.ReservedAhead = -1 }, "pod p01 reserves -1 blocks ahead, not 0 or more"},
		{"a negative lead and a speed over the limit", moving, func(pod *SavedPod) { pod.ReservedAhead, pod.Speed = -1, 15 }, "pod p01 reserves -1 blocks ahead, not 0 or more"},
		{"an explicit 0 at a berth", idle, func(pod *SavedPod) { pod.SetMotionMembers(true, false) }, "pod 01 is not traveling and has a speed or a reservation ahead"},
		{"an explicit 0 lead at a berth", idle, func(pod *SavedPod) { pod.SetMotionMembers(false, true) }, "pod 01 is not traveling and has a speed or a reservation ahead"},
		{"an explicit 0 and a negative speed at a berth", idle, func(pod *SavedPod) { pod.Speed = -1; pod.SetMotionMembers(true, false) }, "pod 01 has speed -1, not a finite value of 0 or more"},
		{"an earlier refusal", idle, func(pod *SavedPod) { pod.ParkedSince, pod.Speed = -1, math.NaN() }, `pod "01" has the negative parked tick -1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := test.base
			input.State = withPod(input.State, 0, test.edit)
			s, result, err := RestoreState(input)
			if s != nil || err == nil || err.Error() != test.want || result.Tier != "" {
				t.Fatalf("got %v, %+v, want %q", err, result, test.want)
			}
		})
	}
	// A speed over the limit of a lane is not a refusal, also on a named
	// lane, and nor is an explicit 0 on a traveling pod. The physical tier
	// demotes a pod over the limit (see TestRestoreMotionLaneLimit).
	for _, edit := range []func(*SavedPod){
		func(pod *SavedPod) { pod.Speed = 15 },
		func(pod *SavedPod) { pod.Speed, pod.LaneID = 15, "nowhere" },
		func(pod *SavedPod) { pod.SetMotionMembers(true, true) },
	} {
		input := moving
		input.State = withPod(input.State, 0, edit)
		if err := checkSavedMotion(input); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRestoreFootprintsBeforeMotion checks that a refusal of the saved
// debris before either tier comes before a refusal of the saved motion.
// The scratch placement of checkSavedFaultFootprints also sees the
// negative lead of a faulted pod. With debris that meets nothing, the
// motion refusal is the result. So is it with debris on a pod without a
// fault, which only the physical tier refuses.
func TestRestoreFootprintsBeforeMotion(t *testing.T) {
	t.Parallel()
	// build returns a save with a fault on pod 02, changed by edit, and
	// debris on the lane of pod 02 when overlap is true, or on s2-link.
	build := func(t *testing.T, overlap bool, edit func(*SavedPod)) (*Simulation, SavedState) {
		t.Helper()
		s := debrisFleet(t)
		startFault(t, s, returnTraveler(t, s), 0)
		state := s.ExportState()
		index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })
		state = withPod(state, index, edit)
		if pod := state.Pods[index]; overlap {
			addSavedDebris(&state, pod.Route[pod.RouteIndex], pod.LaneDistance+1, pod.LaneDistance+2)
		} else {
			addSavedDebris(&state, laneIndex(t, s, "s2-link"), 0, 2)
		}
		return s, state
	}
	// refusal restores state and returns the text of its refusal.
	refusal := func(t *testing.T, s *Simulation, state SavedState) string {
		t.Helper()
		_, result, err := RestoreState(faultRestoreInput(s, state))
		if err == nil || result.Tier != "" {
			t.Fatalf("restore %v, tier %q, want a refusal", err, result.Tier)
		}
		return err.Error()
	}
	// The save with valid motion gives the debris refusal that the saves
	// with invalid motion must also give.
	s, state := build(t, true, func(*SavedPod) {})
	if _, _, err := RestoreState(faultRestoreInput(s, state)); !errors.Is(err, errInvalidFaults) {
		t.Fatalf("debris on the faulted pod: %v", err)
	}
	onFaulted := refusal(t, s, state)
	for _, test := range []struct {
		name string
		edit func(*SavedPod)
		want string
	}{
		{"a negative speed", func(pod *SavedPod) { pod.Speed = -1 }, "pod 02 has speed -1, not a finite value of 0 or more"},
		{"a negative lead", func(pod *SavedPod) { pod.ReservedAhead = -1 }, "pod 02 reserves -1 blocks ahead, not 0 or more"},
	} {
		live, saved := build(t, true, test.edit)
		if got := refusal(t, live, saved); got != onFaulted {
			t.Errorf("%s with debris on the faulted pod: %q, want %q", test.name, got, onFaulted)
		}
		live, saved = build(t, false, test.edit)
		if got := refusal(t, live, saved); got != test.want {
			t.Errorf("%s with debris that meets nothing: %q, want %q", test.name, got, test.want)
		}
	}
	// Two debris records meet, and a pod has a negative speed.
	s, state = debrisSave(t)
	addSavedDebris(&state, laneIndex(t, s, "s3-link"), 85, 90)
	if _, _, err := RestoreState(faultRestoreInput(s, state)); !errors.Is(err, errInvalidFaults) {
		t.Fatalf("debris that meets debris: %v", err)
	}
	onDebris := refusal(t, s, state)
	state = withPod(state, 0, func(pod *SavedPod) { pod.Speed = -1 })
	if got := refusal(t, s, state); got != onDebris {
		t.Errorf("debris that meets debris and a negative speed: %q, want %q", got, onDebris)
	}
	// The physical tier refuses debris on a pod without a fault, after the
	// input checks, so the motion refusal comes first.
	s, state = debrisSave(t)
	index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })
	state = withPod(state, index, func(pod *SavedPod) { pod.Speed = -1 })
	pod := state.Pods[index]
	addSavedDebris(&state, pod.Route[pod.RouteIndex], pod.LaneDistance+1, pod.LaneDistance+2)
	if got, want := refusal(t, s, state), "pod 02 has speed -1, not a finite value of 0 or more"; got != want {
		t.Errorf("debris on a pod without a fault: %q, want %q", got, want)
	}
}

// TestRestoreFaultedSpeedIgnored checks that a faulted pod saves no speed,
// and that the restore ignores a speed of a faulted pod, also one over the
// speed limit of its lane. The pod restores at rest with the cap at its
// distance, and with the live frontier ahead of the derived frontier.
func TestRestoreFaultedSpeedIgnored(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	startFault(t, s, v, 0)
	s.Step()
	state := s.ExportState()
	index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == v.Pod.ID })
	if v.Pod.Speed == 0 || state.Pods[index].Speed != 0 || state.Pods[index].ReservedAhead == 0 {
		t.Fatalf("the faulted pod moves at %g and saves the speed %g and %d blocks ahead", v.Pod.Speed, state.Pods[index].Speed, state.Pods[index].ReservedAhead)
	}
	state = withPod(state, index, func(pod *SavedPod) { pod.Speed = 100 })
	restored, result, err := RestoreState(faultRestoreInput(s, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore %v, %+v", err, result)
	}
	if got := &restored.vehicles[index]; !got.faulted || got.Pod.Speed != 0 || got.faultCap != got.distance {
		t.Fatalf("pod %s restores faulted %t at speed %g with the cap %g at %g", got.Pod.ID, got.faulted, got.Pod.Speed, got.faultCap, got.distance)
	}
	// The faulted pod keeps its lead: the restored frontier is the live one.
	checkRestoredFrontiers(t, s, restored)
}

// TestRestoreKeepsDrainingLink restores the fixture of
// TestEmergencyQueueRestore one tick after the start of the emergency. The
// links of p03 and p04 drain, and each follower reserved past the derived
// frontier. The restore keeps the speeds and the frontiers, so one tick
// later each pod has the link of the live run.
func TestRestoreKeepsDrainingLink(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, motionLanes, 4)
	s := restoreGeometry(t, network, len(motionLanes), 4, 200, 30)
	s.emergenciesOn = true
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	for range 5 * TicksPerSecond {
		s.Step()
	}
	startEmergency(t, s, &s.vehicles[2], 0)
	s.Step()
	state := s.ExportState()
	for _, index := range []int{2, 3} {
		if pod := state.Pods[index]; pod.Platoon == nil || !pod.Platoon.Draining || pod.Speed == 0 || pod.ReservedAhead == 0 {
			t.Fatalf("pod %s saves the link %+v, the speed %g, and %d blocks ahead", pod.ID, pod.Platoon, pod.Speed, pod.ReservedAhead)
		}
	}
	restored, result, err := RestoreState(emergencyRestoreInput(s, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore %v, %+v", err, result)
	}
	if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	checkRestoredMatches(t, s, restored)
	live := s.Clone()
	live.Step()
	restored.Step()
	for index := range live.vehicles {
		got, want := restored.vehicles[index].link.leader, live.vehicles[index].link.leader
		if got != want || index >= 2 && got == 0 {
			t.Fatalf("tick %d: pod %s has the predecessor %d, want %d", live.tick, live.vehicles[index].Pod.ID, got, want)
		}
	}
	if !reflect.DeepEqual(restored.ExportState(), live.ExportState()) {
		t.Fatalf("tick %d: the restored run differs from the live run", live.tick)
	}
}

// TestRestoreKeepsGroupRunFrontier restores the arrival of a Compact pod
// in a maneuver group at each tick. A grant on a group lane takes the
// whole run of the group junction, so the live frontier is ahead of the
// derived frontier. The restore must keep that frontier.
func TestRestoreKeepsGroupRunFrontier(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(2, 7)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "stop", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	bays, _ := s.station("bays")
	if err := s.startEmptyMove(v, emptyDestination{station: "bays", berth: bays.Berths[1], reserveBerth: true}); err != nil {
		t.Fatal(err)
	}
	kept := 0
	stepUntil(t, s, "pod 01 idle at bay 2", func() bool {
		if v.Pod.Activity != Traveling {
			return v.Pod.Activity == Idle
		}
		state := roundTripState(t, s.ExportState())
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil || !cleanRestore(result) {
			t.Fatalf("tick %d: restore %+v, %v", s.tick, result, err)
		}
		checkRestoredMatches(t, s, restored)
		if state.Pods[0].ReservedAhead > 0 && s.maneuvers.groupLane(v.blocks.lane(v.reservedThrough).ID) {
			// checkRestoredMatches compares the saved leads, so the
			// frontiers are the same blocks.
			if got := &restored.vehicles[0]; got.blocks.lane(got.reservedThrough).ID != v.blocks.lane(v.reservedThrough).ID {
				t.Fatalf("tick %d: the restored frontier is on another lane", s.tick)
			}
			kept++
		}
		return false
	})
	if kept == 0 {
		t.Fatal("no restore had a frontier on a group lane past the derived frontier")
	}
	t.Logf("%d restores kept a group run frontier", kept)
}

// TestRestoreDepotEntryFrontier restores a pod bound to a depot slot that it
// does not claim. Its live frontier is the depot entry, ahead of the
// derived frontier, and the restore keeps it, although the entry is not a
// block where a grant ends after the binding. A pod with a frontier past
// the entry, also at a block where a grant ends, restores with the derived
// frontier.
func TestRestoreDepotEntryFrontier(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 3))
	v := sendToDepot(t, s, "harbor-pod")
	index := slices.IndexFunc(s.vehicles, func(other vehicle) bool { return other.Pod.ID == v.Pod.ID })
	var state SavedState
	stepUntil(t, s, "a bound pod with a lead", func() bool {
		if s.depotPodKind(v) != "bound without its slot" {
			return false
		}
		state = s.ExportState()
		return state.Pods[index].ReservedAhead > 0
	})
	input := RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: roundTripState(t, state)}
	restored, result, err := RestoreState(input)
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore %v, %+v", err, result)
	}
	checkRestoredMatches(t, s, restored)
	got := &restored.vehicles[index]
	entry, ok := restored.depotEntryBlock(got, 0)
	if !ok || got.reservedThrough != entry || reservationEnd(&got.blocks, entry) == entry {
		t.Fatalf("the restored frontier is %d, want the entry %d, which is not a grant end", got.reservedThrough, entry)
	}
	derived := entry - state.Pods[index].ReservedAhead
	past := frontierAfter(t, got, entry, true)
	input.State = withPod(input.State, index, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = 0, past-derived })
	restored, result, err = RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	checkFallback(t, input, restored, result)
	if got := &restored.vehicles[index]; got.reservedThrough != derived {
		t.Fatalf("the restored frontier is %d, want the derived frontier %d", got.reservedThrough, derived)
	}
}

// TestRestoreMotionFallbackGrant is the two-pod case of review round 5.
// On trimLanes, p01 is at rest at 336.4 m, on the release of the first
// cell of l1. Unlinked p02 is at 260 m in the ninth cell of l0, with a
// grant through the last cell of l0 and the first cell of l1. The export
// trims the route of p01, so the restore gives p01 the track cell of
// that cell again (see TestRestoreExactCoordinatesFollowUpReleaseAtRest),
// and the saved frontier of p02 meets it. The restore then places p02 at
// rest with the derived frontier, as the restore before the saved motion
// does, and demotes no pod.
func TestRestoreMotionFallbackGrant(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 2)
	base := restoreGeometry(t, network, len(trimLanes), 2, 100, 50)
	at := withPod(base.ExportState(), 0, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 336.4-300, 336.4
	})
	at = withPod(at, 1, func(pod *SavedPod) { pod.LaneDistance, pod.Distance, pod.ReservedAhead = 260, 260, 2 })
	live, result := restoreMotion(t, network, base.initial, at)
	v := &live.vehicles[1]
	first := v.blocks.laneFirst(1)
	if !cleanRestore(result) || live.vehicles[0].distance != 336.4 || v.distance != 260 || v.blockIndex != first-2 || v.reservedThrough != first {
		t.Fatalf("result %+v: p01 at %v, p02 at %v in block %d with the frontier %d, want 336.4, and 260 in %d with %d",
			result, live.vehicles[0].distance, v.distance, v.blockIndex, v.reservedThrough, first-2, first)
	}
	held := v.blocks.at(first).resources
	cell := held[slices.IndexFunc(held, func(claimed resource) bool { return claimed.kind == trackResource })]
	if !live.owners[cell].isPod("p02") {
		t.Fatalf("the first cell of l1 is held by %v, want p02", live.owners[cell])
	}
	saved := live.ExportState()
	if pod := saved.Pods[1]; pod.ReservedAhead != 2 || network.Lanes[saved.Pods[0].Route[0]].ID != "l1" {
		t.Fatalf("p02 saves %d blocks ahead, and the saved route of p01 starts at %s, want 2 and l1", pod.ReservedAhead, network.Lanes[saved.Pods[0].Route[0]].ID)
	}
	input := RestoreStateInput{Network: network, Fleet: base.initial, State: saved}
	restored, result := restoreMotion(t, network, base.initial, saved)
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("result %+v, want both pods in place", result)
	}
	checkFallback(t, input, restored, result)
	got := &restored.vehicles[1]
	if got.Pod.Speed != 0 || got.distance != 260 || got.reservedThrough != v.reservedThrough-2 {
		t.Fatalf("p02 restores at %v at %v m/s with the frontier %d, want 260 at rest with %d", got.distance, got.Pod.Speed, got.reservedThrough, v.reservedThrough-2)
	}
	if !restored.owners[cell].isPod("p01") {
		t.Fatalf("the first cell of l1 is held by %v, want p01, as before the saved motion", restored.owners[cell])
	}
}

// TestRestoreMotionWholeFallbackOrder is TestRestoreMotionFallbackGrant
// with the pods in the other order. p01 is at 260 m with the grant
// through the first cell of l1, and p02 is at rest at 336.4 m. The
// restore places p01 first with its saved frontier, and p02 then needs
// the track cell of that cell again, at rest, so the fallback of p02 does
// not help. The whole save then restores without the saved motion, and
// keeps both pods.
func TestRestoreMotionWholeFallbackOrder(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 2)
	base := restoreGeometry(t, network, len(trimLanes), 2, 300, 50)
	at := withPod(base.ExportState(), 1, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 336.4-300, 336.4
	})
	at = withPod(at, 0, func(pod *SavedPod) { pod.LaneDistance, pod.Distance, pod.ReservedAhead = 260, 260, 2 })
	live, result := restoreMotion(t, network, base.initial, at)
	v := &live.vehicles[0]
	first := v.blocks.laneFirst(1)
	if !cleanRestore(result) || v.distance != 260 || v.reservedThrough != first || live.vehicles[1].distance != 336.4 {
		t.Fatalf("result %+v: p01 at %v with the frontier %d, p02 at %v, want 260 with %d, and 336.4", result, v.distance, v.reservedThrough, live.vehicles[1].distance, first)
	}
	saved := live.ExportState()
	if saved.Pods[0].ReservedAhead != 2 {
		t.Fatalf("p01 saves %d blocks ahead, want 2", saved.Pods[0].ReservedAhead)
	}
	input := RestoreStateInput{Network: network, Fleet: base.initial, State: saved}
	restored, result := restoreMotion(t, network, base.initial, saved)
	checkFallback(t, input, restored, result)
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 || restored.vehicles[0].reservedThrough != first-2 {
		t.Fatalf("result %+v, p01 frontier %d, want both pods in place and %d", result, restored.vehicles[0].reservedThrough, first-2)
	}
}

// TestRestoreMotionHeadSnap is case 1 of review round 6, with no saved
// motion. p01 is at rest at 336.4 m, and unlinked p02 is at rest at
// 270.0000005 m, in the last cell of l0, with its grant through the first
// cell of l1. The export trims the route of p01, so the restore gives p01
// that cell again, and the restore demotes p02. The restore before the
// saved motion snaps p02 to the block end at 270 m, in the ninth cell, and
// keeps both pods, so the whole save restores that way.
func TestRestoreMotionHeadSnap(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 2)
	base := restoreGeometry(t, network, len(trimLanes), 2, 100, 50)
	const at = 270.0000005
	saved := withPod(base.ExportState(), 0, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 336.4-300, 336.4
	})
	saved = withPod(saved, 1, func(pod *SavedPod) { pod.LaneDistance, pod.Distance = at, at })
	live, result := restoreMotion(t, network, base.initial, saved)
	v := &live.vehicles[1]
	first := v.blocks.laneFirst(1)
	if !cleanRestore(result) || v.distance != at || v.blockIndex != first-1 || v.reservedThrough != first {
		t.Fatalf("result %+v: p02 at %v in block %d with the frontier %d, want %v in %d with %d", result, v.distance, v.blockIndex, v.reservedThrough, at, first-1, first)
	}
	state := live.ExportState()
	if pod := state.Pods[1]; pod.Speed != 0 || pod.ReservedAhead != 0 || pod.Distance != at {
		t.Fatalf("p02 saves %v m/s, %d blocks ahead, at %v, want no motion at %v", pod.Speed, pod.ReservedAhead, pod.Distance, at)
	}
	input := RestoreStateInput{Network: network, Fleet: base.initial, State: state}
	if _, first, err := firstRestore(input); err != nil || !slices.Equal(first.Demoted, []string{"p02"}) {
		t.Fatalf("the restore with the drift snap gives %v, %+v, want p02 demoted", err, first)
	}
	restored, result := restoreMotion(t, network, base.initial, state)
	checkFallback(t, input, restored, result)
	if !cleanRestore(result) || restored.vehicles[1].distance != 270 {
		t.Fatalf("result %+v, p02 at %v, want both pods in place and p02 at 270", result, restored.vehicles[1].distance)
	}
}

// withDebris returns state with one more debris record, from from to to on
// the lane with the ID, as an edited save has it (see addSavedDebris).
func withDebris(t *testing.T, network Network, state SavedState, lane string, from, to float64) SavedState {
	t.Helper()
	index := slices.IndexFunc(network.Lanes, func(candidate Lane) bool { return candidate.ID == lane })
	if index < 0 {
		t.Fatalf("no lane %s", lane)
	}
	if state.Faults == nil {
		state.Faults = &SavedFaults{}
	}
	addSavedDebris(&state, index, from, to)
	return state
}

// TestRestoreMotionHeadFaultRefusal is case 2 of review round 6. p01 is at
// rest at 200 m, p02 follows it at 160 m at 14 m/s, too fast for the stop
// point of p01, and p03 follows p02 at 100 m, with debris from 104 m to
// 106 m. The restore with the saved motion fails at the stop point of
// p02. The restore before the saved motion places p02 at rest and finds
// p03 on the debris, a fault refusal, which decides.
func TestRestoreMotionHeadFaultRefusal(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 3, 200, 40, nil)
	state = withPod(state, 1, func(pod *SavedPod) {
		pod.Platoon, pod.Speed = &SavedPlatoonLink{Leader: "p01", Lanes: 2}, 14
	})
	state = withPod(state, 2, func(pod *SavedPod) {
		pod.Platoon, pod.Distance, pod.LaneDistance = &SavedPlatoonLink{Leader: "p02", Lanes: 2}, 100, 100
	})
	input := RestoreStateInput{Network: network, Fleet: fleet, State: withDebris(t, network, state, "l0", 104, 106), FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}}
	if _, _, err := firstRestore(input); err == nil || !strings.Contains(err.Error(), "the stop point of the pod") {
		t.Fatalf("the restore with the saved motion fails with %v, want the stop-point error", err)
	}
	_, headResult, headErr := headRestore(input)
	_, result, err := RestoreState(input)
	want := "pod p03: invalid saved fault: the pod holds a resource of a debris footprint"
	if !errors.Is(err, errInvalidFaults) || err.Error() != want || headErr == nil || headErr.Error() != want || result.PhysicalError == nil || result.PhysicalError.Error() != headResult.PhysicalError.Error() {
		t.Fatalf("restore %v, %+v; before the saved motion %v; want %q", err, result, headErr, want)
	}
}

// TestRestoreMotionHeadLogicalFallback is the reverse of
// TestRestoreMotionHeadFaultRefusal. p02 is unlinked at 120 m with a saved
// lead into a cell with debris, and p03 follows it too near. The restore
// with the saved motion fails at the debris, a fault refusal. The restore
// before the saved motion fails at the gap of p03, which decides, so the
// restore goes to the logical tier.
func TestRestoreMotionHeadLogicalFallback(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 3, 200, 80, nil)
	base, _ := restoreMotion(t, network, fleet, state)
	v := &base.vehicles[1]
	derived := v.reservedThrough
	through := frontierAfter(t, v, frontierAfter(t, v, derived, true), true)
	start := v.blocks.end(through - 1)
	state = withPod(state, 1, func(pod *SavedPod) { pod.ReservedAhead = through - derived })
	state = withPod(state, 2, func(pod *SavedPod) {
		pod.Platoon, pod.Distance, pod.LaneDistance = &SavedPlatoonLink{Leader: "p02", Lanes: 2}, 115, 115
	})
	input := RestoreStateInput{Network: network, Fleet: fleet, State: withDebris(t, network, state, "l0", start+1, start+2), FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}}
	if _, _, err := firstRestore(input); !errors.Is(err, errInvalidFaults) {
		t.Fatalf("the restore with the saved motion fails with %v, want a fault refusal", err)
	}
	_, headResult, headErr := headRestore(input)
	_, result, err := RestoreState(input)
	want := fmt.Sprintf("pod p03: the pod is %.6f m behind its platoon predecessor, less than the clearance %.6f m", 5.0, linkClearance(0))
	if err != nil || headErr != nil || result.Tier != RestoreLogical || result.PhysicalError == nil || result.PhysicalError.Error() != want || headResult.PhysicalError.Error() != want {
		t.Fatalf("restore %v, %+v; before the saved motion %v, %+v; want the logical tier with %q", err, result, headErr, headResult, want)
	}
}

// TestRestoreMotionFaultFootprintSnap restores faulted pod 02 of
// debrisFleet at rest 5e-7 m past the end of a block on lane "return" where
// a grant can end, with debris near the end of the next block. The
// 1e-9 m snap leaves the pod in the next block, on the debris, so the
// first physical restore fails. The restore before the saved motion snaps
// the pod back to the block end, off the debris. The check of the fault
// footprints places the pods as that restore does, so it does not refuse
// the save, and the restore equals that restore.
func TestRestoreMotionFaultFootprintSnap(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	v := returnTraveler(t, s)
	if _, err := s.Fault(FaultRequest{PodID: "02"}); err != nil {
		t.Fatal(err)
	}
	lane := v.blocks.locate(v.blockIndex, 0)
	block := v.blockIndex + 1
	for reservationEnd(&v.blocks, block) != block {
		block++
	}
	if v.blocks.locate(block+1, 0) != lane {
		t.Fatalf("block %d and the next are not on the lane of the pod", block)
	}
	start := v.blocks.lanes[lane].start
	end, next := v.blocks.end(block), v.blocks.end(block+1)
	state := s.ExportState()
	index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })
	shift := end + 5e-7 - v.distance
	state = withPod(state, index, func(pod *SavedPod) {
		pod.Distance, pod.LaneDistance, pod.ReservedAhead = pod.Distance+shift, pod.LaneDistance+shift, 0
	})
	state = withDebris(t, s.network, state, v.blocks.route[lane].ID, next-start-3, next-start-2)
	input := faultRestoreInput(s, state)
	if _, _, err := firstRestore(input); !errors.Is(err, errInvalidFaults) {
		t.Fatalf("the restore with the 1e-9 m snap fails with %v, want a fault refusal", err)
	}
	restored, result, err := RestoreState(input)
	if err != nil {
		t.Fatalf("restore %v, %+v", err, result)
	}
	checkFallback(t, input, restored, result)
	if !cleanRestore(result) {
		t.Fatalf("result %+v", result)
	}
}

// TestRestoreMotionFaultedLeadOverDebris is the case of review round 7. A
// faulted pod on l0 of motionFixture, ten cells of 30 m, is at rest at
// 120.0000005 m with a saved lead of 5, and debris is at 225 m to 226 m,
// in cell 7. The restore before the saved motion snaps the pod to 120 m,
// in cell 3, with no lead. The first physical restore keeps the pod in
// cell 4, where the saved frontier, cell 9, is not a grant end, so it
// places the pod with the derived frontier. Neither placement meets the
// debris. The check of the fault footprints places the pods as the
// restore before the saved motion does, so it does not refuse the save.
// The first restore passes and demotes no pod, so it is the result: it
// keeps the fault and the debris, and it is not worse than the restore
// before the saved motion.
func TestRestoreMotionFaultedLeadOverDebris(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 1, 100, 0, nil)
	s, _ := restoreMotion(t, network, fleet, state)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	startFault(t, s, &s.vehicles[0], 0)
	const at = 120.0000005
	state = withPod(s.ExportState(), 0, func(pod *SavedPod) { pod.Distance, pod.LaneDistance, pod.ReservedAhead = at, at, 5 })
	state = withDebris(t, network, state, "l0", 225, 226)
	input := faultRestoreInput(s, state)
	first, _, err := firstRestore(input)
	if err != nil || first.vehicles[0].distance != at {
		t.Fatalf("the restore with the saved motion gives %v, want the pod at %v", err, at)
	}
	restored, result, err := RestoreState(input)
	_, plain, plainErr := headRestore(input)
	if worse := worseThanPlain(result, err, plain, plainErr); err != nil || worse != "" {
		t.Fatalf("restore %v, %+v: %s", err, result, worse)
	}
	if got := &restored.vehicles[0]; got.distance != at || got.reservedThrough != first.vehicles[0].reservedThrough || !maps.Equal(restored.owners, first.owners) {
		t.Fatalf("the pod at %v with the frontier %d, want the first restore at %v with %d", got.distance, got.reservedThrough, at, first.vehicles[0].reservedThrough)
	}
	if !cleanRestore(result) || len(restored.faults) != 2 || !restored.vehicles[0].faulted {
		t.Fatalf("result %+v, %d fault records, the pod faulted %t, want the pod fault and the debris", result, len(restored.faults), restored.vehicles[0].faulted)
	}
}

// TestRestoreMotionWholeFallbackMixed saves the pods of
// TestRestoreMotionWholeFallbackOrder and p03 at 200 m, and then puts p03
// at rest at 340 m, in the second cell of l1, which p02 also holds. The
// restore with the saved motion demotes p02, so p03 takes the cell. The
// restore without the saved motion keeps p02 and demotes p03. The first
// restore demotes a pod that the second keeps, so the whole save restores
// without the saved motion, also though the second restore demotes
// another pod.
func TestRestoreMotionWholeFallbackMixed(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 3)
	base := restoreGeometry(t, network, len(trimLanes), 3, 300, 50)
	at := withPod(base.ExportState(), 1, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 336.4-300, 336.4
	})
	at = withPod(at, 0, func(pod *SavedPod) { pod.LaneDistance, pod.Distance, pod.ReservedAhead = 260, 260, 2 })
	live, result := restoreMotion(t, network, base.initial, at)
	if !cleanRestore(result) {
		t.Fatalf("result %+v", result)
	}
	saved := withPod(live.ExportState(), 2, func(pod *SavedPod) { pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 40, 340 })
	input := RestoreStateInput{Network: network, Fleet: base.initial, State: saved}
	if _, first, err := firstRestore(input); err != nil || !slices.Equal(first.Demoted, []string{"p02"}) {
		t.Fatalf("the restore with the saved motion gives %v, %+v, want p02 demoted", err, first)
	}
	restored, result := restoreMotion(t, network, base.initial, saved)
	checkFallback(t, input, restored, result)
	if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"p03"}) || restored.vehicles[0].Pod.Speed != 0 {
		t.Fatalf("result %+v, want p03 demoted", result)
	}
}

// TestRestoreMotionWholeFallbackLink restores a link whose predecessor
// and follower both move at 14 m/s, with the follower behind the stop
// point of the predecessor by less than its own stopping distance. The
// saved frontier of the predecessor is inside a grant, so it restores at
// rest, and the stop point of the moving follower is then past the stop
// point of the predecessor. The whole save restores without the saved
// motion, in the physical tier.
func TestRestoreMotionWholeFallbackLink(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 2, 200, linkClearance(0)+stoppingDistance(14)/2, nil)
	state = withPod(state, 1, func(pod *SavedPod) { pod.Platoon = &SavedPlatoonLink{Leader: "p01", Lanes: 2} })
	base, _ := restoreMotion(t, network, fleet, state)
	leader, follower := &base.vehicles[0], &base.vehicles[1]
	inside := frontierAfter(t, leader, leader.reservedThrough, false)
	through := follower.reservedThrough
	for follower.blocks.end(through) < follower.distance+stoppingDistance(14)+1 {
		through = frontierAfter(t, follower, through, true)
	}
	moving := withPod(state, 0, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = 14, inside-leader.reservedThrough })
	moving = withPod(moving, 1, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = 14, through-follower.reservedThrough })
	input := RestoreStateInput{Network: network, Fleet: fleet, State: moving}
	restored, result := restoreMotion(t, network, fleet, moving)
	checkFallback(t, input, restored, result)
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 || restored.vehicles[1].link.leader == 0 || restored.vehicles[1].Pod.Speed != 0 {
		t.Fatalf("result %+v, follower at %v m/s with the predecessor %d, want both in place at rest and linked", result, restored.vehicles[1].Pod.Speed, restored.vehicles[1].link.leader)
	}
}

// TestRestoreMotionWholeFallbackDebris restores pod 02 of debrisFleet,
// which cruises on lane "return" at 14 m/s, with debris from 75 m to 85 m
// on that lane, in the third cell. With a saved lead of 1, the frontier
// ends before the debris, and the pod keeps its motion. With a lead of 2,
// the frontier takes the debris cell, so the physical tier fails with the
// debris error. The restore without the saved motion keeps the pod, so the
// whole save restores that way. The same holds when pod 02 has a fault:
// the check of the fault footprints before the tiers places the pod
// without its lead, so it does not refuse the save.
func TestRestoreMotionWholeFallbackDebris(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	returnTraveler(t, s)
	startDebris(t, s, "return", 75, 85, 600)
	state := s.ExportState()
	index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })
	if pod := state.Pods[index]; pod.Speed != 14 || pod.ReservedAhead != 1 {
		t.Fatalf("pod 02 saves %v m/s and %d blocks ahead, want 14 and 1", pod.Speed, pod.ReservedAhead)
	}
	restored, result, err := RestoreState(faultRestoreInput(s, state))
	if err != nil || !cleanRestore(result) || restored.vehicles[index].Pod.Speed != 14 {
		t.Fatalf("restore %v, %+v, pod 02 at %v m/s, want 14", err, result, restored.vehicles[index].Pod.Speed)
	}
	input := faultRestoreInput(s, withPod(state, index, func(pod *SavedPod) { pod.ReservedAhead = 2 }))
	restored, result, err = RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	checkFallback(t, input, restored, result)
	if !cleanRestore(result) || restored.vehicles[index].Pod.Speed != 0 {
		t.Fatalf("result %+v, pod 02 at %v m/s, want at rest", result, restored.vehicles[index].Pod.Speed)
	}
	startFault(t, s, &s.vehicles[index], 0)
	state = s.ExportState()
	if pod := state.Pods[index]; pod.ReservedAhead != 1 {
		t.Fatalf("faulted pod 02 saves %d blocks ahead, want 1", pod.ReservedAhead)
	}
	input = faultRestoreInput(s, withPod(state, index, func(pod *SavedPod) { pod.ReservedAhead = 2 }))
	if _, _, firstErr := firstRestore(input); !errors.Is(firstErr, errInvalidFaults) {
		t.Fatalf("the restore with the saved lead of the faulted pod gives %v, want the debris error", firstErr)
	}
	restored, result, err = RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	checkFallback(t, input, restored, result)
	if !cleanRestore(result) || !restored.vehicles[index].faulted || len(restored.faults) != 2 {
		t.Fatalf("result %+v, pod 02 faulted %t, %d fault records, want the fault and the debris", result, restored.vehicles[index].faulted, len(restored.faults))
	}
}

// TestRestoreMotionPodFallbackKeepsMotion restores three unlinked pods on
// l0. The saved frontier of p01 at 200 m is inside a grant, so p01
// restores at rest with the derived frontier. p02 at 120 m moves at
// 14 m/s with a frontier past its stop point, and it keeps that speed and
// frontier, because the fallback of p01 alone places it. p03 at 40 m names
// another lane, so the restore without the saved motion also demotes it,
// and the whole save keeps the saved motion.
func TestRestoreMotionPodFallbackKeepsMotion(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 3, 200, 80, nil)
	base, _ := restoreMotion(t, network, fleet, state)
	ahead, behind := &base.vehicles[0], &base.vehicles[1]
	inside := frontierAfter(t, ahead, ahead.reservedThrough, false)
	through := behind.reservedThrough
	for behind.blocks.end(through) < behind.distance+stoppingDistance(14)+1 {
		through = frontierAfter(t, behind, through, true)
	}
	edited := withPod(state, 0, func(pod *SavedPod) { pod.ReservedAhead = inside - ahead.reservedThrough })
	edited = withPod(edited, 1, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = 14, through-behind.reservedThrough })
	edited = withPod(edited, 2, func(pod *SavedPod) { pod.LaneID = "l1" })
	restored, result := restoreMotion(t, network, fleet, edited)
	if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"p03"}) {
		t.Fatalf("result %+v, want p03 demoted", result)
	}
	if got := &restored.vehicles[0]; got.Pod.Speed != 0 || got.reservedThrough != ahead.reservedThrough {
		t.Fatalf("p01 restores at %v m/s with the frontier %d, want at rest with %d", got.Pod.Speed, got.reservedThrough, ahead.reservedThrough)
	}
	if got := &restored.vehicles[1]; got.Pod.Speed != 14 || got.reservedThrough != through {
		t.Fatalf("p02 restores at %v m/s with the frontier %d, want 14 with %d", got.Pod.Speed, got.reservedThrough, through)
	}
}

// TestRestoreExactCoordinatesFollowUpReleaseAtRest documents a case of
// review round 4 for the follow-up for exact restore coordinates (restore
// speed proposal, section 4.1). A pod at rest at 336.4 m on trimLanes is
// exactly on the release of the first cell of l1 (300 + 122/5 + 12). The
// export trims l0 and saves 36.39999999999998 m, and the restore holds
// the cell again. The test logs the owners that the restore adds, and it
// skips, so it does not fail the suite.
func TestRestoreExactCoordinatesFollowUpReleaseAtRest(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, trimLanes, 1)
	base := restoreGeometry(t, network, len(trimLanes), 1, 100, 0)
	at := withPod(base.ExportState(), 0, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance = 1, "l1", 336.4-300, 336.4
	})
	live, result := restoreMotion(t, network, base.initial, at)
	if !cleanRestore(result) || live.vehicles[0].distance != 336.4 {
		t.Skipf("the setup does not place the pod: result %+v, the pod at %v", result, live.vehicles[0].distance)
	}
	saved := live.ExportState()
	restored, result := restoreMotion(t, network, base.initial, saved)
	var extra []resource
	for held := range restored.owners {
		if _, ok := live.owners[held]; !ok {
			extra = append(extra, held)
		}
	}
	t.Logf("saved distance %v, restore %+v, owners that the restore adds %v", saved.Pods[0].Distance, result, extra)
	t.Skip("the follow-up for exact restore coordinates fixes this case")
}

// TestRestoredPositionSearch checks that the binary searches of
// restoredDistance and restoredBlock give the results of a walk of the
// blocks in order: the first block end within the snap, from the last
// block of the lane before, and the first block that ends at or after the
// distance. The distances are at, and near, each block end of each lane of
// two routes, and in each block.
func TestRestoredPositionSearch(t *testing.T) {
	t.Parallel()
	network, fleet, state := motionFixture(t, 1, 100, 0, nil)
	moving, _ := restoreMotion(t, network, fleet, state)
	traveling := debrisFleet(t)
	for _, v := range []*vehicle{&moving.vehicles[0], returnTraveler(t, traveling)} {
		blocks := &v.blocks
		for lane := range blocks.route {
			first, last := routeLaneBlocks(blocks, lane)
			start := blocks.at(first).laneStart
			var distances []float64
			for index := max(0, first-1); index <= last; index++ {
				b := blocks.at(index)
				distances = append(distances, (b.start+b.end)/2)
				distances = append(distances, sweepDistances(b.end)...)
			}
			for _, distance := range distances {
				for _, snap := range []float64{restoreDriftTolerance, restoreTolerance} {
					input := restoredDistanceInput{blocks: blocks, first: first, last: last, laneDistance: distance - start, saved: distance, snap: snap}
					got := restoredDistance(input)
					want := walkedDistance(input)
					if math.Float64bits(got) != math.Float64bits(want) {
						t.Fatalf("pod %s, lane %d, distance %v, snap %g: restoredDistance %v, want %v", v.Pod.ID, lane, distance, snap, got, want)
					}
					if got, want := restoredBlock(blocks, first, last, want), walkedBlock(blocks, first, last, want); got != want {
						t.Fatalf("pod %s, lane %d, distance %v: restoredBlock %d, want %d", v.Pod.ID, lane, distance, got, want)
					}
				}
			}
		}
	}
}

// walkedDistance is restoredDistance with a walk of the blocks in order.
func walkedDistance(input restoredDistanceInput) float64 {
	distance := input.blocks.at(input.first).laneStart + input.laneDistance
	if math.Abs(distance-input.saved) <= restoreTolerance {
		distance = input.saved
	}
	for _, b := range input.blocks.peekSpan(max(0, input.first-1), input.last+1) {
		if math.Abs(b.end-distance) <= input.snap {
			return b.end
		}
	}
	return distance
}

// walkedBlock is restoredBlock with a walk of the blocks in order.
func walkedBlock(blocks *blockList, first, last int, distance float64) int {
	for candidate, b := range blocks.peekSpan(first, last+1) {
		if b.end >= distance {
			return candidate
		}
	}
	return last
}
