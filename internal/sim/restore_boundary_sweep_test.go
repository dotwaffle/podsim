package sim

import (
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"testing"
)

// sweepOffsets are the distances in meters from a boundary at which
// TestRestoreBoundarySweep puts a pod, in addition to 0 to 4 float steps
// to each side.
var sweepOffsets = []float64{1e-12, 1e-10, 1e-9, 2e-9, 1e-7}

// sweepBoundary is a block end or a release distance of a route, with
// the kinds of boundary at that distance: "block end", "node",
// "track", "junction", or "berth".
type sweepBoundary struct {
	at    float64
	kinds []string
}

// sweepBoundaries returns the block ends and the release distances of the
// resources of the blocks of v past the origin tail of v, in order. With
// more than limit of them, it takes limit at equal steps through the
// route, and also the first boundary of each kind.
func sweepBoundaries(v *vehicle, limit int) []sweepBoundary {
	kinds := make(map[float64][]string)
	add := func(at float64, kind string) {
		if at > v.originTail() && at < v.blocks.end(v.blocks.len()-1) && !slices.Contains(kinds[at], kind) {
			kinds[at] = append(kinds[at], kind)
		}
	}
	names := map[resourceKind]string{berthResource: "berth", nodeResource: "node", trackResource: "track", junctionResource: "junction"}
	for _, b := range v.blocks.peekSpan(0, v.blocks.len()) {
		add(b.end, "block end")
		for _, claimed := range b.resources {
			add(resourceReleaseDistance(b, claimed), names[claimed.kind])
		}
	}
	all := slices.Sorted(maps.Keys(kinds))
	keep := make(map[float64]bool)
	firsts := make(map[string]bool)
	for index, at := range all {
		if len(all) <= limit || index*limit/len(all) != (index+1)*limit/len(all) {
			keep[at] = true
		}
		for _, kind := range kinds[at] {
			if !firsts[kind] {
				firsts[kind], keep[at] = true, true
			}
		}
	}
	var boundaries []sweepBoundary
	for _, at := range all {
		if keep[at] {
			boundaries = append(boundaries, sweepBoundary{at: at, kinds: kinds[at]})
		}
	}
	return boundaries
}

// sweepDistances returns the distances near boundary: 0 to 4 float steps
// to each side, and each of sweepOffsets to each side.
func sweepDistances(boundary float64) []float64 {
	distances := []float64{boundary}
	for _, direction := range []float64{math.Inf(-1), math.Inf(1)} {
		at := boundary
		for range 4 {
			at = math.Nextafter(at, direction)
			distances = append(distances, at)
		}
	}
	for _, offset := range sweepOffsets {
		distances = append(distances, boundary-offset, boundary+offset)
	}
	return distances
}

// sweepFixture is a fleet of TestRestoreBoundarySweep: a roundTripCase
// whose simulation is at a save of the pod at index with its whole route.
type sweepFixture struct {
	c     roundTripCase
	index int
	// boundaries limits the boundaries of the route, and kinds are the
	// kinds of boundary that the sweep must check.
	boundaries int
	kinds      []string
	// descending tells that the sweep must check a pod at the lower speed
	// limit of the next lane.
	descending bool
}

// sweepCounts counts the placements of a sweep: the placements whose
// restore before the step kept the pod in place, the placements that the
// restore refused or demoted before the step, the checks, the checks with
// a later route start, at rest, and at a descending speed limit, the two
// kinds of residual (see classifyResidual), and the second round trips.
// moved is the largest move of a residual (a).
type sweepCounts struct {
	placed, skipped, checked, trimmed, resting, descending int
	snapped, held, second                                  int
	moved                                                  float64
}

// placeSweepPod returns a restore of state with the pod at index at the
// route distance distance and the speed, with the shortest frontier past
// its stop point at which a grant can end. live is the pod in the
// simulation of state, whose saved route is its whole route. It returns
// nil when the restore does not keep the pod in place at the speed. It
// fails t when the restore at the speed is worse than the restore of the
// same save before the saved motion (see worseThanPlain).
func placeSweepPod(t *testing.T, c roundTripCase, state SavedState, live *vehicle, index int, distance, speed float64) *Simulation {
	t.Helper()
	lane := sweepLane(live, distance)
	edit := func(saved *SavedPod) {
		saved.RouteIndex, saved.LaneID = lane, live.blocks.route[lane].ID
		saved.LaneDistance, saved.Distance = distance-live.blocks.lanes[lane].start, distance
		saved.Speed, saved.ReservedAhead, saved.Waiting = 0, 0, false
	}
	s, result, err := RestoreState(c.input(withPod(state, index, edit)))
	if err != nil || !cleanRestore(result) || speed == 0 {
		if err != nil || !cleanRestore(result) {
			return nil
		}
		return s
	}
	v := &s.vehicles[index]
	derived, through := v.reservedThrough, v.reservedThrough
	for v.blocks.end(through) < v.distance+motionEnvelopeFor(v.Pod.Class).stoppingDistance(speed)+1 {
		next := through + 1
		for next < v.blocks.len() && reservationEnd(&v.blocks, next) != next {
			next++
		}
		if next >= v.blocks.len() {
			return nil
		}
		through = next
	}
	movingState := withPod(state, index, func(saved *SavedPod) {
		edit(saved)
		saved.Speed, saved.ReservedAhead = speed, through-derived
	})
	moving, result, err := RestoreState(c.input(movingState))
	_, plain, plainErr := headRestore(c.input(movingState))
	if failure := worseThanPlain(result, err, plain, plainErr); failure != "" {
		t.Errorf("%s: the pod at %v at %v m/s: %s", c.name, distance, speed, failure)
	}
	if err != nil || !cleanRestore(result) || moving.vehicles[index].Pod.Speed != speed {
		return nil
	}
	return moving
}

// sweepLane returns the route index of the lane of v at distance.
func sweepLane(v *vehicle, distance float64) int {
	lane := 0
	for lane+1 < len(v.blocks.route) && v.blocks.lanes[lane+1].start <= distance {
		lane++
	}
	return lane
}

// sweepSpeeds returns the speeds of a pod at distance on the route of v: at
// rest, 0.5 m/s, the speed limit of its lane, and the speed limit of the
// next lane when it is lower.
func sweepSpeeds(v *vehicle, distance float64) []float64 {
	lane := sweepLane(v, distance)
	speeds := []float64{0, 0.5, v.blocks.route[lane].SpeedLimit}
	if lane+1 < len(v.blocks.route) && v.blocks.route[lane+1].SpeedLimit < v.blocks.route[lane].SpeedLimit {
		speeds = append(speeds, v.blocks.route[lane+1].SpeedLimit)
	}
	slices.Sort(speeds)
	return slices.Compact(speeds)
}

// TestRestoreBoundarySweep puts a pod near each block end and each release
// distance of its route, at rest and moving, steps it once, and checks the
// save of the tick as roundTripCheck does. A pod is near a boundary before
// the step, and also after it: the second placement subtracts the move of
// one tick from the boundary. A pod placed at rest is also checked before
// the step. The restore can differ from the live owners in the two ways of
// sweepResidual only (restore speed proposal, section 4.1), and the test
// reports the count of each. Any other difference fails the test. The test
// runs only in mise run qualify (PODSIM_QUALIFY=1).
func TestRestoreBoundarySweep(t *testing.T) {
	t.Parallel()
	if os.Getenv("PODSIM_QUALIFY") != "1" {
		t.Skip("the boundary sweep runs only in mise run qualify (PODSIM_QUALIFY=1)")
	}
	fixtures := map[string]func(t *testing.T) sweepFixture{
		"boundary": func(t *testing.T) sweepFixture {
			t.Helper()
			c := boundaryCase(t, 3)
			return sweepFixture{c: c, boundaries: 30, kinds: []string{"block end", "track", "node"}, descending: true}
		},
		"release": func(t *testing.T) sweepFixture {
			t.Helper()
			network := geometryNetwork(Point{}, releaseLanes, 1)
			s := restoreGeometry(t, network, len(releaseLanes), 1, 100, 0)
			return sweepFixture{boundaries: 30, kinds: []string{"block end", "track", "node"}, c: roundTripCase{
				name: "release", s: s,
				input: func(state SavedState) RestoreStateInput {
					return RestoreStateInput{Network: network, Fleet: s.initial, State: state}
				},
			}}
		},
		"depot": func(t *testing.T) sweepFixture {
			t.Helper()
			network := shortDepotNetwork(2, 3)
			s := newDepotFleet(t, network, depotPlacements(network, 3))
			v := sendToDepot(t, s, "harbor-pod")
			index := slices.IndexFunc(s.vehicles, func(other vehicle) bool { return other.Pod.ID == v.Pod.ID })
			stepUntil(t, s, "the depot pod traveling", func() bool { return v.Pod.Activity == Traveling })
			return sweepFixture{index: index, boundaries: 30, kinds: []string{"block end", "track", "node", "junction"}, c: roundTripCase{
				name: "depot", s: s,
				input: func(state SavedState) RestoreStateInput {
					return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: state}
				},
			}}
		},
		"maneuver group": func(t *testing.T) sweepFixture {
			t.Helper()
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
			stepUntil(t, s, "pod 01 traveling", func() bool { return v.Pod.Activity == Traveling })
			return sweepFixture{boundaries: 30, kinds: []string{"block end", "track", "node"}, c: roundTripCase{
				name: "maneuver group", s: s,
				input: func(state SavedState) RestoreStateInput {
					return RestoreStateInput{Network: network, Fleet: s.initial, State: state}
				},
			}}
		},
		"express": func(t *testing.T) sweepFixture {
			t.Helper()
			network := largeRestoreNetwork()
			fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
			s, err := expressPhysicalFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[0]
			stepUntil(t, s, "pod 01 traveling", func() bool { return v.Pod.Activity == Traveling })
			return sweepFixture{boundaries: 30, kinds: []string{"block end", "track", "node", "junction"}, c: roundTripCase{
				name: "express", s: s,
				input: func(state SavedState) RestoreStateInput {
					services := slices.Collect(maps.Values(s.expressServices))
					return RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: state, ExpressServices: services, OnboardPickups: s.onboardPickups}
				},
			}}
		},
	}
	for _, name := range slices.Sorted(maps.Keys(fixtures)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := fixtures[name](t)
			sweepFleet(t, fixture)
		})
	}
}

// sweepFleet runs the sweep of TestRestoreBoundarySweep on one fixture.
func sweepFleet(t *testing.T, fixture sweepFixture) {
	t.Helper()
	state := fixture.c.s.ExportState()
	if pod := state.Pods[fixture.index]; pod.Activity != "traveling" {
		t.Fatalf("%s: pod %s is %s", fixture.c.name, pod.ID, pod.Activity)
	}
	live := &fixture.c.s.vehicles[fixture.index]
	if start, _, _ := fixture.c.s.savedStart(live); start != 0 {
		t.Fatalf("%s: the saved route starts at lane %d", fixture.c.name, start)
	}
	boundaries := sweepBoundaries(live, fixture.boundaries)
	var counts sweepCounts
	checkedKinds := make(map[string]bool)
	for _, boundary := range boundaries {
		before := counts.checked
		for _, speed := range sweepSpeeds(live, boundary.at) {
			move := 0.0
			if trial := placeSweepPod(t, fixture.c, state, live, fixture.index, boundary.at, speed); trial != nil {
				from := trial.vehicles[fixture.index].distance
				trial.Step()
				move = trial.vehicles[fixture.index].distance - from
			}
			lane := sweepLane(live, boundary.at)
			descending := lane+1 < len(live.blocks.route) && speed == live.blocks.route[lane+1].SpeedLimit && speed < live.blocks.route[lane].SpeedLimit
			for _, landing := range []bool{false, true} {
				if landing && move == 0 {
					continue
				}
				for _, near := range sweepDistances(boundary.at) {
					distance := near
					if landing {
						distance -= move
					}
					checked := counts.checked
					sweepCheck(t, fixture, state, distance, speed, &counts, fmt.Sprintf("boundary %v %v, %v m from it (after the step %t)", boundary.at, boundary.kinds, near-boundary.at, landing))
					if descending && counts.checked > checked {
						counts.descending++
					}
				}
			}
		}
		if counts.checked > before {
			for _, kind := range boundary.kinds {
				checkedKinds[kind] = true
			}
		}
	}
	for _, kind := range fixture.kinds {
		if !checkedKinds[kind] {
			t.Errorf("%s: no check at a boundary of kind %s, only %v", fixture.c.name, kind, slices.Sorted(maps.Keys(checkedKinds)))
		}
	}
	if counts.trimmed == 0 || fixture.descending && counts.descending == 0 {
		t.Errorf("%s: %d checks with a later route start and %d at a descending limit", fixture.c.name, counts.trimmed, counts.descending)
	}
	// The baseline skips at most 5 percent of the placements, which the
	// restore refuses or demotes before the step.
	if counts.checked == 0 || counts.skipped*10 > counts.placed {
		t.Fatalf("%s: %d placements, %d skipped, more than 10 percent", fixture.c.name, counts.placed, counts.skipped)
	}
	if counts.second == 0 {
		t.Fatalf("%s: no second round trip", fixture.c.name)
	}
	t.Logf("%s: %d boundaries of the kinds %v, %d placements, %d skipped, %d checks, %d with a later route start, %d at rest, %d at a descending limit; residual (a) %d (largest move %v m), residual (b) %d; %d second round trips",
		fixture.c.name, len(boundaries), slices.Sorted(maps.Keys(checkedKinds)), counts.placed, counts.skipped, counts.checked, counts.trimmed, counts.resting, counts.descending, counts.snapped, counts.moved, counts.held, counts.second)
}

// sweepCheck places the pod of fixture at distance and speed, steps it
// once, and checks the save of the tick (see sweepResidual).
func sweepCheck(t *testing.T, fixture sweepFixture, state SavedState, distance, speed float64, counts *sweepCounts, where string) {
	t.Helper()
	s := placeSweepPod(t, fixture.c, state, &fixture.c.s.vehicles[fixture.index], fixture.index, distance, speed)
	if s == nil {
		counts.skipped++
		return
	}
	counts.placed++
	c := fixture.c
	c.s, c.noJSON = s, true
	check := func(when string) {
		v := &s.vehicles[fixture.index]
		stats := newRoundTripStats()
		got := roundTripCheck(t, c, &stats)
		counts.checked++
		if v.Pod.Speed == 0 {
			counts.resting++
		}
		if start, _, _ := s.savedStart(v); start > 0 {
			counts.trimmed++
		}
		reason := sweepResidual(s, fixture.index, got, counts)
		if reason == "" && got.restored != nil {
			reason = secondRoundTrip(t, c, got.restored)
			counts.second++
		}
		if reason != "" {
			t.Errorf("%s: %s, placed at %v at %v m/s, %s at %v at %v m/s: %s", c.name, where, distance, speed, when, v.distance, v.Pod.Speed, reason)
		}
	}
	if speed == 0 {
		// The pod at rest before the step, as a save of the restored
		// simulation finds it.
		check("before the step")
	}
	s.Step()
	if s.vehicles[fixture.index].Pod.Activity == Traveling {
		check("after the step")
	}
}

// sweepResidual returns "" when the restore of the save of s, which got
// gives, keeps the owners, or differs from them in one of the two ways of
// classifyResidual only, and it counts each way. Otherwise it returns the
// difference.
func sweepResidual(s *Simulation, index int, got roundTripResult, counts *sweepCounts) string {
	if got.failure != "" {
		return got.failure
	}
	if len(got.dropped)+len(got.extra) == 0 {
		return ""
	}
	v, copied := &s.vehicles[index], &got.restored.vehicles[index]
	start, _, _ := s.savedStart(v)
	pod := residualPod{
		moving: v.Pod.Speed > 0, trimmed: start > 0, distance: v.distance,
		end: math.Inf(1), restoredDistance: copied.distance, restoredEnd: math.NaN(),
	}
	for block, b := range v.blocks.peekSpan(0, v.blocks.len()) {
		if b.end > v.distance {
			pod.end = b.end
			if restored := block - v.blocks.laneFirst(start); restored >= 0 && restored < copied.blocks.len() {
				pod.restoredEnd = copied.blocks.end(restored)
			}
			break
		}
	}
	release := func(held resource, owners map[resource]resourceOwner) residualOwner {
		owner := residualOwner{held: held, own: owners[held] == podResourceOwner(v.Pod.ID)}
		for _, b := range v.blocks.peekSpan(0, v.reservedThrough+1) {
			if slices.Contains(b.resources, held) {
				owner.release, owner.found = max(owner.release, resourceReleaseDistance(b, held)), true
			}
		}
		return owner
	}
	for _, held := range got.dropped {
		pod.dropped = append(pod.dropped, release(held, s.owners))
	}
	for _, held := range got.extra {
		pod.extra = append(pod.extra, release(held, got.restored.owners))
	}
	switch kind, reason := classifyResidual(pod); kind {
	case 'a':
		counts.snapped++
		counts.moved = max(counts.moved, pod.restoredDistance-got.state.Pods[index].Distance)
	case 'b':
		counts.held++
	default:
		return reason
	}
	return ""
}

// residualPod is the input of classifyResidual. distance is the live route
// distance of the pod, and end the first live block end past it.
// restoredDistance is the restored route distance, and restoredEnd the end
// of the restored block that matches the live block of end. dropped and
// extra are the owners that the restore drops and adds.
type residualPod struct {
	moving, trimmed               bool
	distance, end                 float64
	restoredDistance, restoredEnd float64
	dropped, extra                []residualOwner
}

// residualOwner is a resource of a residualPod: whether the pod owns it
// (live for a dropped owner, restored for an extra owner), and its live
// release distance, the largest of the reserved blocks that hold it.
// found is false when no reserved block holds it.
type residualOwner struct {
	held       resource
	own, found bool
	release    float64
}

// classifyResidual returns the kind of a difference of the owners of a
// restore of pod, 'a' or 'b', or 0 and the reason (restore speed
// proposal, section 4.1).
//
// (a) The pod moves, and it is short of the live block end by at most
// restoreDriftTolerance. The restore puts it exactly on the matching
// restored block end and drops the owners that the live pod releases
// there: each is a resource of the pod with its live release distance in
// (distance, end].
//
// (b) The saved route of the pod starts at a later lane, and the live
// distance is a release distance. The restore keeps the live owners and
// holds more resources of the pod, each with its live release distance
// equal to the live distance.
func classifyResidual(pod residualPod) (byte, string) {
	switch {
	case len(pod.extra) == 0 && pod.moving:
		if pod.end-pod.distance > restoreDriftTolerance || pod.restoredDistance != pod.restoredEnd {
			return 0, fmt.Sprintf("the restore drops %v, the pod is %v m short of the block end %v, and it restores at %v, not at the restored end %v",
				pod.dropped, pod.end-pod.distance, pod.end, pod.restoredDistance, pod.restoredEnd)
		}
		for _, owner := range pod.dropped {
			if !owner.own || !owner.found || owner.release <= pod.distance || owner.release > pod.end {
				return 0, fmt.Sprintf("the restore drops %+v, released at %v, not in (%v, %v]", owner, owner.release, pod.distance, pod.end)
			}
		}
		return 'a', ""
	case len(pod.dropped) == 0 && pod.trimmed:
		for _, owner := range pod.extra {
			if !owner.own || !owner.found || owner.release != pod.distance {
				return 0, fmt.Sprintf("the restore adds %+v, released at %v, %d float steps from the distance %v", owner, owner.release, floatSteps(owner.release, pod.distance), pod.distance)
			}
		}
		return 'b', ""
	}
	return 0, fmt.Sprintf("the restore drops %v and adds %v (moving %t, trimmed %t)", pod.dropped, pod.extra, pod.moving, pod.trimmed)
}

// floatSteps returns the number of float64 values from a to b, for values
// of 0 or more.
func floatSteps(a, b float64) int64 {
	steps := int64(math.Float64bits(a)) - int64(math.Float64bits(b))
	return max(steps, -steps)
}

// TestClassifyResidual checks that classifyResidual accepts each kind of
// residual exactly, and rejects a near miss of each.
func TestClassifyResidual(t *testing.T) {
	t.Parallel()
	cell := resource{kind: trackResource, id: "l0", cell: 3}
	snapped := residualPod{
		moving: true, distance: 100 - 5e-10, end: 100, restoredDistance: 40, restoredEnd: 40,
		dropped: []residualOwner{{held: cell, own: true, found: true, release: 100}},
	}
	held := residualPod{
		trimmed: true, distance: 336.4, restoredDistance: 36.39999999999998,
		extra: []residualOwner{{held: cell, own: true, found: true, release: 336.4}},
	}
	// beyond is the largest distance short of 100 by more than the
	// tolerance.
	beyond := 100 - restoreDriftTolerance
	for 100-beyond <= restoreDriftTolerance {
		beyond = math.Nextafter(beyond, 0)
	}
	edit := func(pod residualPod, change func(*residualPod)) residualPod {
		pod.dropped, pod.extra = slices.Clone(pod.dropped), slices.Clone(pod.extra)
		change(&pod)
		return pod
	}
	for _, test := range []struct {
		name string
		pod  residualPod
		want byte
	}{
		{name: "(a)", pod: snapped, want: 'a'},
		{name: "(b)", pod: held, want: 'b'},
		{name: "(a) with a partial move", pod: edit(snapped, func(pod *residualPod) { pod.restoredDistance = math.Nextafter(40, 0) })},
		{name: "(a) short of the end by one float step more than the tolerance", pod: edit(snapped, func(pod *residualPod) { pod.distance = beyond })},
		{name: "(a) with a release one float step past the end", pod: edit(snapped, func(pod *residualPod) { pod.dropped[0].release = math.Nextafter(100, 200) })},
		{name: "(a) with a release at the distance", pod: edit(snapped, func(pod *residualPod) { pod.dropped[0].release = pod.distance })},
		{name: "(a) at rest", pod: edit(snapped, func(pod *residualPod) { pod.moving = false })},
		{name: "(b) with an extra owner one float step from the distance", pod: edit(held, func(pod *residualPod) { pod.extra[0].release = math.Nextafter(336.4, 400) })},
		{name: "(b) with a dropped owner", pod: edit(held, func(pod *residualPod) {
			pod.dropped = []residualOwner{{held: resource{kind: trackResource, id: "l1"}, own: true, found: true, release: 336.4}}
		})},
		{name: "(b) on a route that is not trimmed", pod: edit(held, func(pod *residualPod) { pod.trimmed = false })},
		{name: "(b) with the owner of another pod", pod: edit(held, func(pod *residualPod) { pod.extra[0].own = false })},
	} {
		if kind, reason := classifyResidual(test.pod); kind != test.want || (kind == 0) != (reason != "") {
			t.Errorf("%s: kind %q, reason %q, want kind %q", test.name, kind, reason, test.want)
		}
	}
}
