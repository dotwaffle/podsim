package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// dwellHarness moves one pod of a tall bay network along a route with
// every block reserved, so only the dwell latch stops it.
type dwellHarness struct {
	s *Simulation
	v *vehicle
	// points holds the route distance at the start of each lane.
	points []float64
}

func newDwellHarness(t *testing.T, ids ...string) dwellHarness {
	t.Helper()
	network := tallBayNetwork(2, 7)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "stop", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	var route []Lane
	for _, id := range ids {
		route = append(route, s.network.Lanes[networkLaneIndex(s.network, id)])
	}
	v := &s.vehicles[0]
	v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
	s.setVehicleRoute(v, route)
	v.reservedThrough, v.blockIndex, v.distance = v.blocks.len()-1, 0, 0
	h := dwellHarness{s: s, v: v}
	for k := range route {
		h.points = append(h.points, v.blocks.lanes[k].start)
	}
	return h
}

// run moves the pod for ticks, or until it stops at the end of its route,
// and returns the number of ticks at rest at the start of each lane, by
// route index. A pod at rest at the end of a tick counts once.
func (h dwellHarness) run(ticks int) map[int]int {
	rest := make(map[int]int)
	for range ticks {
		h.s.tick++
		h.s.move(h.v)
		if h.v.Pod.Speed != 0 {
			continue
		}
		if h.v.distance >= h.v.blocks.end(h.v.blocks.len()-1) {
			break
		}
		if k := slices.Index(h.points, h.v.distance); k >= 0 {
			rest[k]++
		}
	}
	return rest
}

// TestReversalDwell checks the dwell at each change of the reverse flag: a
// departure from a bay stops for 1 s where it enters the aisle, a
// forward-only route does not stop, and a turn at the inner node of a bay
// stops at each change.
func TestReversalDwell(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		route []string
		want  map[int]int
	}{
		{name: "departure", route: []string{"bay-1-out-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out", "leave"}, want: map[int]int{2: reversalDwellTicks}},
		{name: "forward only", route: []string{"approach", "aisle-1", "aisle-2", "aisle-out", "leave"}, want: map[int]int{}},
		{name: "inner turn", route: []string{"aisle-1", "bay-1-in-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out"},
			want: map[int]int{2: reversalDwellTicks, 3: reversalDwellTicks}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			h := newDwellHarness(t, test.route...)
			if rest := h.run(100 * TicksPerSecond); !maps.Equal(rest, test.want) {
				t.Fatalf("ticks at rest by lane %v, want %v", rest, test.want)
			}
		})
	}
}

// TestReversalDwellResets checks that a route replacement and a fault
// start the dwell again, and that an arrival empties the latch.
func TestReversalDwellResets(t *testing.T) {
	t.Parallel()
	route := []string{"bay-1-out-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out", "leave"}
	untilDwell := func(h dwellHarness, ticks int) {
		for range 100 * TicksPerSecond {
			if h.v.reversal.ticks >= ticks {
				return
			}
			h.s.tick++
			h.s.move(h.v)
		}
		t.Fatal("the pod did not dwell")
	}
	t.Run("route", func(t *testing.T) {
		t.Parallel()
		h := newDwellHarness(t, route...)
		untilDwell(h, reversalDwellTicks/2)
		h.v.replaceRoute(h.v.Route)
		if h.v.reversal != (reversal{}) {
			t.Fatalf("latch %+v after a route replacement", h.v.reversal)
		}
		// The pod is at rest at the start of the aisle before the run.
		if rest := h.run(100 * TicksPerSecond); rest[2]+1 != reversalDwellTicks {
			t.Fatalf("ticks at rest after the replacement %d, want %d", rest[2]+1, reversalDwellTicks)
		}
	})
	t.Run("fault", func(t *testing.T) {
		t.Parallel()
		h := newDwellHarness(t, route...)
		untilDwell(h, reversalDwellTicks/2)
		h.s.faultsOn = true
		seconds := int64(2)
		if _, err := h.s.Fault(FaultRequest{PodID: "01", DurationSeconds: &seconds}); err != nil {
			t.Fatal(err)
		}
		if h.v.reversal.ticks != 0 || h.v.reversal.lane != 2 {
			t.Fatalf("latch %+v at fault onset", h.v.reversal)
		}
		// The faulted pod does not count, and after the clear the full
		// dwell runs.
		for range 2 * TicksPerSecond {
			h.s.tick++
			h.s.move(h.v)
		}
		if h.v.reversal.ticks != 0 {
			t.Fatalf("a faulted pod counted %d ticks", h.v.reversal.ticks)
		}
		h.v.faulted = false
		if rest := h.run(100 * TicksPerSecond); rest[2]+1 != reversalDwellTicks {
			t.Fatalf("ticks at rest after the clear %d, want %d", rest[2]+1, reversalDwellTicks)
		}
	})
	t.Run("held at the boundary", func(t *testing.T) {
		t.Parallel()
		// The reservation ends at the start of the aisle for longer than
		// the dwell. When it grows, the pod goes on at once.
		h := newDwellHarness(t, route...)
		h.v.reservedThrough = h.v.blocks.laneFirst(2) - 1
		h.run(20 * TicksPerSecond)
		if h.v.distance != h.points[2] || h.v.reversal.lane != -2 {
			t.Fatalf("distance %g, latch %+v", h.v.distance, h.v.reversal)
		}
		h.v.reservedThrough = h.v.blocks.len() - 1
		h.s.tick++
		h.s.move(h.v)
		if h.v.distance == h.points[2] {
			t.Fatal("the pod dwelled again at the same boundary")
		}
	})
	t.Run("arrival", func(t *testing.T) {
		t.Parallel()
		h := newDwellHarness(t, route...)
		untilDwell(h, reversalDwellTicks/2)
		h.v.destination, h.v.destinationStation = h.s.network.Stations[1].Berths[0], "stop"
		h.s.arrive(h.v)
		if h.v.reversal != (reversal{}) {
			t.Fatalf("latch %+v after an arrival", h.v.reversal)
		}
	})
}

// dwellSaves returns the tall bay network, its fleet, and clones of a pod
// that departs from a bay: before the aisle, at the start of the aisle at
// rest, in its dwell, after the dwell, and at the first tick with a
// trimmed route that starts at lane 1.
func dwellSaves(t *testing.T) (Network, []Placement, []*Simulation) {
	t.Helper()
	network := tallBayNetwork(2, 7)
	fleet := []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "stop"); err != nil {
		t.Fatal(err)
	}
	atBranch := func(s *Simulation) bool {
		pod := s.vehicles[0].Pod
		return pod.Activity == Traveling && pod.Position == (Point{}) && pod.Speed == 0
	}
	var lives []*Simulation
	for tick := 0; tick < 120*TicksPerSecond && len(lives) < 5; tick++ {
		before := s.vehicles[0].Pod
		s.Step()
		switch {
		case len(lives) == 0 && before.LaneID == "bay-1-out-branch-1" && s.vehicles[0].Pod.Position.Y < 1 && !atBranch(s):
			lives = append(lives, s.Clone())
		case len(lives) == 1 && atBranch(s):
			lives = append(lives, s.Clone())
		case len(lives) == 2 && s.vehicles[0].reversal.ticks == reversalDwellTicks/2:
			lives = append(lives, s.Clone())
		case len(lives) == 3 && s.vehicles[0].reversal.lane < 0:
			lives = append(lives, s.Clone())
		case len(lives) == 4 && trimmedStart(s) == 1:
			lives = append(lives, s.Clone())
		}
	}
	if len(lives) != 5 {
		t.Fatalf("%d saves", len(lives))
	}
	return network, fleet, lives
}

// restAtLaneStarts steps s until the first pod arrives, for at most 120 s,
// and returns the ticks at rest at the start of each lane of its route
// after each step, by route index. A pod at rest at the start of the run
// also counts.
func restAtLaneStarts(s *Simulation) map[int]int {
	rest := make(map[int]int)
	count := func() {
		v := &s.vehicles[0]
		if v.Pod.Activity != Traveling || v.Pod.Speed != 0 {
			return
		}
		for k := 1; k < len(v.Route); k++ {
			if v.distance == v.blocks.lanes[k].start {
				rest[k]++
			}
		}
	}
	count()
	for tick := 0; tick < 120*TicksPerSecond && s.vehicles[0].Pod.Activity == Traveling; tick++ {
		s.Step()
		count()
	}
	return rest
}

// restorableHarness returns a dwell harness whose route goes on to the
// stop station, as an empty move from bay 1 to that station, so that its
// save restores.
func restorableHarness(t *testing.T, ids ...string) dwellHarness {
	t.Helper()
	h := newDwellHarness(t, append(ids, "loop-down", "loop-in")...)
	h.v.RelocatingTo, h.v.destinationStation = "stop", "stop"
	h.v.origin = h.s.network.Stations[0].Berths[0]
	return h
}

// TestReversalDwellRestoreDrops checks the saved latch against the
// restored position and speed of the pod. The physical tier drops a latch
// that would skip a dwell, shorten a dwell, or move the pod back, and lists
// the pod in DroppedLatches. The pod then restores as before the saved
// latch: it stops for a full dwell at the next change of the reverse flag
// that it has not passed. Each case changes a valid save of
// TestReversalDwellRestore, or of a harness pod with two dwells. A
// trimmed route with a drift within its snap keeps its latch, and a drift
// past the snap drops it.
func TestReversalDwellRestoreDrops(t *testing.T) {
	t.Parallel()
	network, fleet, lives := dwellSaves(t)
	saves := make([]SavedState, len(lives))
	for index, live := range lives {
		saves[index] = live.ExportState()
	}
	pod := saves[0].Pods[0]
	if pod.RouteIndex != 1 || pod.ReversalLane != 2 || pod.ReversalTicks != 0 {
		t.Fatalf("the first save has the pod at lane %d with the latch %d, %d", pod.RouteIndex, pod.ReversalLane, pod.ReversalTicks)
	}
	// past moves the pod of a save a distance into the aisle, at rest.
	past := func(meters float64) func(*SavedPod) {
		return func(pod *SavedPod) {
			start := lives[2].vehicles[0].blocks.lanes[2].start
			pod.RouteIndex, pod.LaneID, pod.LaneDistance, pod.Distance, pod.Speed = 2, "aisle-2", meters, start+meters, 0
		}
	}
	// short moves the pod of a save a distance before the aisle, at rest.
	short := func(meters float64) func(*SavedPod) {
		return func(pod *SavedPod) {
			v := &lives[2].vehicles[0]
			lane := v.blocks.route[1]
			pod.RouteIndex, pod.LaneID, pod.LaneDistance = 1, lane.ID, lives[2].laneLength(lane)-meters
			pod.Distance, pod.Speed, pod.ReservedAhead = v.blocks.lanes[2].start-meters, 0, 0
		}
	}
	// drift is the difference of a pod at a lane start after a discarded
	// prefix of 1,500,000 m and twenty lanes of 20.1 m, between the live
	// frame less the prefix and the trimmed frame (Astra review, round 2).
	drift := func() float64 {
		live, trimmed := 1_500_000.0, 0.0
		for range 20 {
			live, trimmed = live+20.1, trimmed+20.1
		}
		return live - 1_500_000 - trimmed
	}()
	if drift <= restoreDriftTolerance {
		t.Fatalf("the drift %g is within the snap", drift)
	}
	full := map[int]int{2: reversalDwellTicks}
	for _, test := range []struct {
		name string
		save int
		edit func(*SavedPod)
		// rest is the ticks at rest at each lane start after the restore.
		rest map[int]int
	}{
		{"a done lane ahead of the pod", 0, func(pod *SavedPod) {
			pod.LaneDistance, pod.Distance, pod.ReversalLane = pod.LaneDistance-5, pod.Distance-5, -2
		}, full},
		{"a done lane just ahead of the pod", 0, func(pod *SavedPod) { pod.ReversalLane = -2 }, full},
		{"a done lane just ahead of a stopped pod", 1, func(pod *SavedPod) { short(5e-10)(pod); pod.ReversalLane, pod.ReversalTicks = -2, 0 }, full},
		{"a latched lane behind a stopped pod", 2, past(3), map[int]int{}},
		{"a new latch behind a stopped pod", 1, past(3), map[int]int{}},
		{"a latched lane just behind the pod", 2, past(5e-10), map[int]int{}},
		{"dwell ticks short of the lane", 0, func(pod *SavedPod) { pod.ReversalTicks, pod.Speed, pod.ReservedAhead = 1, 0, 0 }, full},
		{"dwell ticks just short of the lane", 1, func(pod *SavedPod) { short(5e-10)(pod); pod.ReversalLane, pod.ReversalTicks = 2, 5 }, full},
		{"dwell ticks at speed", 2, func(pod *SavedPod) { pod.Speed = 1e-3 }, full},
		{"a trimmed route past the snap", 2, func(pod *SavedPod) { past(drift)(pod); pod.RouteTrimmed = true }, map[int]int{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := RestoreStateInput{Network: network, Fleet: fleet, State: withPod(saves[test.save], 0, test.edit)}
			restored, result, err := RestoreState(input)
			if err != nil || !cleanRestore(result) || !slices.Equal(result.DroppedLatches, []string{"01"}) {
				t.Fatalf("restore %+v, %v, want the pod in place with its latch dropped", result, err)
			}
			if got := restored.vehicles[0].reversal; got != (reversal{}) {
				t.Fatalf("the restored latch is %+v, want none", got)
			}
			if rest := restAtLaneStarts(restored); !maps.Equal(rest, test.rest) || restored.vehicles[0].Pod.Activity == Traveling {
				t.Fatalf("ticks at rest by lane %v, %+v, want %v and an arrival", rest, restored.vehicles[0].Pod, test.rest)
			}
		})
	}
	// A trimmed route restores in its own frame. A pod in its dwell 5e-10 m
	// past the start of the lane in that frame snaps to that start, so the
	// save restores with the latch.
	keeps := func(t *testing.T, input RestoreStateInput, want reversal) *Simulation {
		t.Helper()
		restored, result, err := RestoreState(input)
		if err != nil || !cleanRestore(result) || len(result.DroppedLatches) != 0 {
			t.Fatalf("restore %+v, %v, want the pod in place with its latch", result, err)
		}
		if got := restored.vehicles[0].reversal; got != want {
			t.Fatalf("the restored latch is %+v, want %+v", got, want)
		}
		return restored
	}
	drifted := withPod(saves[2], 0, func(pod *SavedPod) { past(5e-10)(pod); pod.RouteTrimmed = true })
	restored := keeps(t, RestoreStateInput{Network: network, Fleet: fleet, State: drifted}, lives[2].vehicles[0].reversal)
	if v := &restored.vehicles[0]; v.distance != v.blocks.lanes[2].start {
		t.Fatalf("the save with drift restores at %v, want the lane start %v", v.distance, v.blocks.lanes[2].start)
	}
	// A pod held at the start of the aisle after its dwell is in the last
	// block of the lane before, with the done lane one lane ahead. It keeps
	// its latch, also as a trimmed route with a drift short of that start
	// within the snap, and goes on with no second dwell.
	held := restorableHarness(t, "bay-1-out-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out", "leave")
	held.v.reservedThrough = held.v.blocks.laneFirst(2) - 1
	held.run(20 * TicksPerSecond)
	state := held.s.ExportState()
	if pod := state.Pods[0]; pod.RouteIndex != 1 || pod.ReversalLane != -2 || held.v.distance != held.points[2] {
		t.Fatalf("the held pod is at lane %d with the latch %d", pod.RouteIndex, pod.ReversalLane)
	}
	shortBy := func(pod *SavedPod) {
		pod.LaneDistance, pod.Distance, pod.RouteTrimmed = pod.LaneDistance-5e-10, pod.Distance-5e-10, true
	}
	for _, saved := range []SavedState{state, withPod(state, 0, shortBy)} {
		again := keeps(t, RestoreStateInput{Network: held.s.network, Fleet: held.s.initial, State: saved}, reversal{lane: -2})
		if rest := restAtLaneStarts(again); rest[2] > 1 {
			t.Fatalf("the held pod rests %d ticks at the aisle after the restore", rest[2])
		}
	}
	// A pod with two dwells, short of both, latches the first one. A latch
	// at the second one skips the first dwell, so the restore drops it, and
	// the pod stops at both.
	h := restorableHarness(t, "aisle-1", "bay-1-in-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out", "leave")
	for range 10 * TicksPerSecond {
		h.s.tick++
		h.s.move(h.v)
	}
	state = h.s.ExportState()
	if pod := state.Pods[0]; pod.RouteIndex != 0 || pod.ReversalLane != 2 {
		t.Fatalf("the pod is at lane %d with the latch %d", pod.RouteIndex, pod.ReversalLane)
	}
	input := RestoreStateInput{Network: h.s.network, Fleet: h.s.initial, State: withPod(state, 0, func(pod *SavedPod) { pod.ReversalLane = 3 })}
	restored, result, err := RestoreState(input)
	if err != nil || !cleanRestore(result) || !slices.Equal(result.DroppedLatches, []string{"01"}) || restored.vehicles[0].reversal != (reversal{}) {
		t.Fatalf("restore %+v, %v, want the pod in place with its latch dropped", result, err)
	}
	if rest := restAtLaneStarts(restored); rest[2] != reversalDwellTicks || rest[3] != reversalDwellTicks {
		t.Fatalf("ticks at rest by lane %v, want a dwell at lanes 2 and 3", rest)
	}
	input.State = state
	keeps(t, input, reversal{lane: 2})
}

// TestReversalLatchBoundary saves a pod at rest in its dwell at the first
// of two changes of the reverse flag. Live motion latches the second lane
// only after the first dwell, and the pod then moves on from the start of
// the first lane in the same tick. So a latch at the second lane, with the
// pod at the start of the first, skips the first dwell. The restore drops
// it, with the pod at the lane start and at the end of the lane before,
// and the pod stops at both lanes. A live save after the first dwell keeps
// its latch. The latch check finds a latched lane only within the limit
// of the frontier.
func TestReversalLatchBoundary(t *testing.T) {
	t.Parallel()
	h := restorableHarness(t, "aisle-1", "bay-1-in-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out", "leave")
	grant := reservationEnd(&h.v.blocks, h.v.blocks.laneFirst(2))
	h.v.reservedThrough = h.v.blocks.laneFirst(2) - 1
	if end := h.v.blocks.end(h.v.reservedThrough); end != h.points[2] {
		t.Fatalf("the frontier ends at %v, want the start %v of lane 2", end, h.points[2])
	}
	step := func(ticks int, until func() bool) {
		for tick := 0; tick < ticks && !until(); tick++ {
			h.s.tick++
			h.s.move(h.v)
		}
	}
	step(60*TicksPerSecond, func() bool { return h.v.reversal.lane == 2 && h.v.reversal.ticks > 0 })
	if h.v.distance != h.points[2] || h.v.Pod.Speed != 0 || h.v.reversal.lane != 2 || h.v.reversal.ticks == 0 {
		t.Fatalf("the pod is at %v at %v m/s with the latch %+v, want a dwell at %v", h.v.distance, h.v.Pod.Speed, h.v.reversal, h.points[2])
	}
	state := h.s.ExportState()
	restore := func(t *testing.T, state SavedState) (*Simulation, RestoreResult) {
		t.Helper()
		restored, result, err := RestoreState(RestoreStateInput{Network: h.s.network, Fleet: h.s.initial, State: roundTripState(t, state)})
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 {
			t.Fatalf("restore %+v, %v, want the pod in place", result, err)
		}
		return restored, result
	}
	before := h.v.blocks.route[1]
	for name, edit := range map[string]func(*SavedPod){
		"at the lane start": func(pod *SavedPod) {
			pod.RouteIndex, pod.LaneID, pod.LaneDistance = 2, h.v.blocks.route[2].ID, 0
		},
		"at the end of the lane before": func(pod *SavedPod) {
			pod.RouteIndex, pod.LaneID, pod.LaneDistance = 1, before.ID, h.s.laneLength(before)
		},
	} {
		restored, result := restore(t, withPod(state, 0, func(pod *SavedPod) {
			edit(pod)
			pod.Distance, pod.ReversalLane, pod.ReversalTicks = h.points[2], 3, 0
		}))
		if !slices.Equal(result.DroppedLatches, []string{"01"}) || restored.vehicles[0].reversal != (reversal{}) {
			t.Fatalf("%s: restore %+v with the latch %+v, want the latch dropped", name, result, restored.vehicles[0].reversal)
		}
		if rest := restAtLaneStarts(restored); rest[2] < reversalDwellTicks || rest[3] < reversalDwellTicks {
			t.Fatalf("%s: ticks at rest by lane %v, want a dwell at lanes 2 and 3", name, rest)
		}
	}
	// The frontier holds the pod at the start after its dwell. A grant of
	// the next reservation reaches past lane 3, and the pod latches lane 3
	// and moves in that tick.
	step(2*reversalDwellTicks, func() bool { return h.v.reversal.lane < 0 })
	if h.v.distance != h.points[2] || h.v.reversal != (reversal{lane: -2}) {
		t.Fatalf("after the first dwell, the pod is at %v with the latch %+v", h.v.distance, h.v.reversal)
	}
	h.v.reservedThrough = grant
	step(1, func() bool { return false })
	if h.v.distance <= h.points[2] || h.v.reversal != (reversal{lane: 3}) {
		t.Fatalf("the pod latches %+v at %v, want lane 3 past %v", h.v.reversal, h.v.distance, h.points[2])
	}
	restored, result := restore(t, h.s.ExportState())
	if len(result.DroppedLatches) != 0 || restored.vehicles[0].reversal != (reversal{lane: 3}) {
		t.Fatalf("the save after the first dwell restores %+v with the latch %+v, want the latch kept", result, restored.vehicles[0].reversal)
	}
	// No save of this route has a frontier that ends between the two lane
	// starts, because the blocks of lanes 1 to 3 share their reservation
	// end, so the limit is checked on the blocks.
	blocks := &restored.vehicles[0].blocks
	at := restored.vehicles[0].distance
	for _, test := range []struct {
		limit float64
		want  bool
	}{{h.points[3], true}, {math.Nextafter(h.points[3], 0), false}} {
		if got := restored.latchFits(blocks, 2, at, 0, test.limit, reversal{lane: 3}); got != test.want {
			t.Fatalf("with the limit %v, the latch at lane 3 fits %t, want %t", test.limit, got, test.want)
		}
	}
}

// TestReversalLatchPhysicalRouteBuilds restores a save with a latch over
// the pod route limit, and over the block budget. The physical restores
// build no route of the latched pod before the route checks, so each
// restore has the outcome of the save with no latch. The count covers the
// physical restores only. The save has no debris, so the scratch fleet of
// checkSavedFaultFootprints, which RestoreStateInput.routeBuilt does not
// count, builds no route.
func TestReversalLatchPhysicalRouteBuilds(t *testing.T) {
	t.Parallel()
	network, fleet, lives := dwellSaves(t)
	state := lives[1].ExportState()
	if pod := state.Pods[0]; pod.ReversalLane != 2 {
		t.Fatalf("the pod has the latch %d", pod.ReversalLane)
	}
	long := withPod(state, 0, func(pod *SavedPod) {
		for len(pod.Route) <= newRouteLimits(lives[1].network).pod {
			pod.Route = append(pod.Route, pod.Route[len(pod.Route)-1])
		}
	})
	for name, input := range map[string]RestoreStateInput{
		"over the route limit":  {Network: network, Fleet: fleet, State: long},
		"over the block budget": {Network: network, Fleet: fleet, State: state, blockBudget: 1},
	} {
		unlatched := input
		unlatched.State = withPod(input.State, 0, func(pod *SavedPod) { pod.ReversalLane, pod.ReversalTicks = 0, 0 })
		built := countRouteBuilds(&input)
		s, result, err := RestoreState(input)
		if built[0] != 0 {
			t.Fatalf("%s: the physical restores build the route of the latched pod %d times", name, built[0])
		}
		want, wantResult, wantErr := RestoreState(unlatched)
		if fmt.Sprint(err) != fmt.Sprint(wantErr) || fmt.Sprintf("%+v", result) != fmt.Sprintf("%+v", wantResult) || result.OverCap+result.OverBudget == 0 {
			t.Fatalf("%s: restore %+v, %v, want %+v, %v", name, result, err, wantResult, wantErr)
		}
		if err == nil && !reflect.DeepEqual(s.ExportState(), want.ExportState()) {
			t.Fatalf("%s: the restores differ", name)
		}
	}
}

// TestReversalLatchRetryKeeps saves a pod in its dwell and gives it a speed
// above the lane limit. The first plan drops the latch, because the pod
// counts dwell ticks only at rest, and fails, because the pod cannot keep
// that speed. The plan at rest keeps the latch. The restore reports the
// latch of the committed placement, so it drops no latch, and the pod
// ends its dwell.
func TestReversalLatchRetryKeeps(t *testing.T) {
	t.Parallel()
	network, fleet, lives := dwellSaves(t)
	live := &lives[2].vehicles[0]
	state := withPod(lives[2].ExportState(), 0, func(pod *SavedPod) { pod.Speed = live.blocks.route[pod.RouteIndex].SpeedLimit + 1 })
	restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil || !cleanRestore(result) || !slices.Equal(result.AtRest, []string{"01"}) || len(result.DroppedLatches) != 0 {
		t.Fatalf("restore %+v, %v, want the pod at rest with its latch", result, err)
	}
	if got := restored.vehicles[0].reversal; got != live.reversal || got.ticks == 0 {
		t.Fatalf("the restored latch is %+v, want %+v", got, live.reversal)
	}
	if rest := restAtLaneStarts(restored); rest[2] != reversalDwellTicks-live.reversal.ticks {
		t.Fatalf("the pod rests %d ticks at the aisle, want %d", rest[2], reversalDwellTicks-live.reversal.ticks)
	}
}

// TestReversalDwellRestore saves a pod that departs from a bay, before
// the aisle, at the start of the aisle at rest, and in its dwell. Each
// restore keeps the dwell latch of the live pod (exact restore coordinates
// proposal, EC-D8). Then the restored pod stays at rest at the start of
// the aisle for the ticks that the live pod stays there, and the pod
// completes its journey. The restore before the saved motion leaves the
// latch empty.
func TestReversalDwellRestore(t *testing.T) {
	t.Parallel()
	network, fleet, lives := dwellSaves(t)
	atBranch := func(s *Simulation) bool {
		pod := s.vehicles[0].Pod
		return pod.Activity == Traveling && pod.Position == (Point{}) && pod.Speed == 0
	}
	wantLatch := []reversal{{lane: 2}, {lane: 2}, {lane: 2, ticks: reversalDwellTicks / 2}, {lane: -2}, {lane: -2}}
	wantRest := []int{reversalDwellTicks, reversalDwellTicks, reversalDwellTicks / 2, 0, 0}
	for index, live := range lives {
		state := live.ExportState()
		if index == len(lives)-1 {
			// The trimmed save starts at lane 1, so it saves the done
			// dwell at lane 2 as lane 1 of its route. The pod does not
			// stop there again.
			state, wantLatch[index] = live.exportTrimmed(), reversal{lane: -1}
		}
		input := RestoreStateInput{Network: network, Fleet: fleet, State: roundTripState(t, state)}
		restored, result, err := RestoreState(input)
		if err != nil || !cleanRestore(result) || len(result.AtRest)+len(result.DroppedLatches) != 0 {
			t.Fatalf("save %d: %+v, error %v", index, result, err)
		}
		if got := restored.vehicles[0].reversal; got != wantLatch[index] {
			t.Fatalf("save %d: restored latch %+v, live latch %+v, want %+v", index, got, live.vehicles[0].reversal, wantLatch[index])
		}
		if earlier, _, err := beforeMotion(input, restoreFleets(input, func() (*PreparedNetwork, error) { return PrepareNetwork(network) })); err != nil || earlier.vehicles[0].reversal != (reversal{}) {
			t.Fatalf("save %d: the restore before the saved motion has the latch %+v, error %v", index, earlier.vehicles[0].reversal, err)
		}
		rest := 0
		if atBranch(restored) {
			rest++
		}
		for tick := 0; tick < 120*TicksPerSecond && restored.completed == 0; tick++ {
			restored.Step()
			live.Step()
			// A trimmed route restores in its own frame, with float
			// differences in the distance, so only the whole saves
			// compare bit for bit.
			if index < len(lives)-1 && restored.vehicles[0].Pod != live.vehicles[0].Pod {
				t.Fatalf("save %d: tick %d: pod %+v, live %+v", index, restored.tick, restored.vehicles[0].Pod, live.vehicles[0].Pod)
			}
			if atBranch(restored) {
				rest++
			}
		}
		if restored.completed != 1 || rest != wantRest[index] {
			t.Fatalf("save %d: completed %d, %d ticks at rest at the aisle, want %d", index, restored.completed, rest, wantRest[index])
		}
	}
}

// trimmedStart returns the start of the trimmed saved route of the first
// pod of s.
func trimmedStart(s *Simulation) int {
	start := s.savedStart(&s.vehicles[0])
	return start
}

// TestSavedReversal checks the saved latch of a pod whose saved route
// starts at route index start. A latched lane moves with the start. A
// lane with its dwell done moves with the start while the saved route
// holds it, and the save drops it after that. The live reversal limit
// does not read a done lane at or before the current lane.
func TestSavedReversal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		latch       reversal
		start       int
		lane, ticks int
	}{
		{reversal{}, 0, 0, 0},
		{reversal{lane: 5, ticks: 7}, 0, 5, 7},
		{reversal{lane: 5, ticks: 7}, 3, 2, 7},
		{reversal{lane: 5}, 4, 1, 0},
		{reversal{lane: -5}, 0, -5, 0},
		{reversal{lane: -5}, 3, -2, 0},
		{reversal{lane: -5}, 4, -1, 0},
		{reversal{lane: -5}, 5, 0, 0},
		{reversal{lane: -5}, 6, 0, 0},
	} {
		if lane, ticks := savedReversal(&vehicle{reversal: test.latch}, test.start); lane != test.lane || ticks != test.ticks {
			t.Errorf("latch %+v from %d saves %d and %d, want %d and %d", test.latch, test.start, lane, ticks, test.lane, test.ticks)
		}
	}
}

// TestReversalDwellEstimates checks that the route estimates of a route
// with a dwell do not read the groups.
func TestReversalDwellEstimates(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(tallBayNetwork(2, 7), []Placement{{ID: "01", StationID: "stop", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	route, err := s.network.RouteForClass("berth-1", "berth-2", CompactClass)
	if err != nil {
		t.Fatal(err)
	}
	with := s.routeSeconds(seatEnvelope, route, motionEstimate{})
	detachIndexes(s)
	s.maneuvers = nil
	if without := s.routeSeconds(seatEnvelope, route, motionEstimate{}); with != without {
		t.Fatalf("estimate %g with groups, %g without", with, without)
	}
}

// driftBayNetwork returns a station with one maneuver group, as
// tallBayNetwork, with bays bays and pieces of about length meters. A lane
// "far-in" of about prefix meters leads to the entry, and the lane
// "approach" from the entry to the aisle is 30 m long. A pod that
// enters the group from "far-in" saves a trimmed route that starts at
// "approach", so the lane starts of the live route and of the trimmed
// route differ by the sums of about prefix meters and of 0.
func driftBayNetwork(bays int, length, prefix float64) Network {
	last := length * float64(bays-1)
	network := Network{
		Nodes: []Node{
			{ID: "far", Position: Point{-length - 30 - prefix, 0}}, {ID: "entry", Position: Point{-length - 30, 0}},
			{ID: "hold", Position: Point{-length, 0}}, {ID: "receive", Position: Point{last + 30, 0}}, {ID: "exit", Position: Point{last + 60, 0}},
			{ID: "loop-east", Position: Point{last + 60, -300}}, {ID: "stop-entry", Position: Point{30, -300}},
			{ID: "stop-berth", Position: Point{0, -340}}, {ID: "stop-exit", Position: Point{-30, -300}},
			{ID: "loop-west", Position: Point{-60, -300}},
		},
		Lanes: []Lane{
			{ID: "far-in", From: "far", To: "entry", SpeedLimit: 14},
			{ID: "approach", From: "entry", To: "hold", SpeedLimit: 14},
			{ID: "through", From: "entry", To: "exit", SpeedLimit: 14, Control: &Point{X: last / 2, Y: -400}},
			{ID: "leave", From: "receive", To: "exit", SpeedLimit: 14},
			{ID: "loop-down", From: "exit", To: "loop-east", SpeedLimit: 14},
			{ID: "loop-in", From: "loop-east", To: "stop-entry", SpeedLimit: 14},
			{ID: "stop-through", From: "stop-entry", To: "stop-exit", SpeedLimit: 14},
			{ID: "stop-in", From: "stop-entry", To: "stop-berth", SpeedLimit: 14},
			{ID: "stop-out", From: "stop-berth", To: "stop-exit", SpeedLimit: 14},
			{ID: "loop-out", From: "stop-exit", To: "loop-west", SpeedLimit: 14},
			{ID: "loop-up", From: "loop-west", To: "far", SpeedLimit: 14},
		},
		Stations: []Station{
			{ID: "bays", Name: "Bays", Entry: "entry", Exit: "exit"},
			{ID: "stop", Name: "Stop", Entry: "stop-entry", Exit: "stop-exit", Berths: []Berth{{ID: "stop-1", Node: "stop-berth"}}},
		},
	}
	group := ManeuverGroup{ID: "bays-group", StationID: "bays"}
	aisle := func(id, from, to string, reverse bool) {
		network.Lanes = append(network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 2, VehicleClasses: compactClassSet})
		group.LaneIDs = append(group.LaneIDs, id)
		if reverse {
			group.ReverseLaneIDs = append(group.ReverseLaneIDs, id)
		}
	}
	previous := "hold"
	for bay := 1; bay <= bays; bay++ {
		x := length * float64(bay-1)
		branch, inner, berth := fmt.Sprint("branch-", bay), fmt.Sprint("inner-", bay), fmt.Sprint("berth-", bay)
		network.Nodes = append(network.Nodes, Node{ID: branch, Position: Point{x, 0}}, Node{ID: inner, Position: Point{x, length}}, Node{ID: berth, Position: Point{x, length + 10}})
		network.Stations[0].Berths = append(network.Stations[0].Berths, Berth{ID: fmt.Sprint("bay-", bay), Node: berth, VehicleClasses: compactClassSet})
		group.BerthIDs = append(group.BerthIDs, fmt.Sprint("bay-", bay))
		aisle(fmt.Sprint("aisle-", bay), previous, branch, false)
		previous = branch
		aisle(fmt.Sprintf("bay-%d-in-inner-%d", bay, bay), branch, inner, false)
		aisle(fmt.Sprintf("bay-%d-in-berth-%d", bay, bay), inner, berth, false)
		aisle(fmt.Sprintf("bay-%d-out-inner-%d", bay, bay), berth, inner, true)
		aisle(fmt.Sprintf("bay-%d-out-branch-%d", bay, bay), inner, branch, true)
	}
	aisle("aisle-out", previous, "receive", false)
	network.ManeuverGroups = []ManeuverGroup{group}
	return network
}

// turnBackNetwork returns the network of driftBayNetwork with one bay, with
// no aisle before the bay. The bay lane "bay-1-in-inner-1" goes from the
// end of "approach" to the inner node of the bay, length meters away, and
// the reverse lane "bay-1-out-branch-1" goes back to that end.
func turnBackNetwork(length, prefix float64) Network {
	network := driftBayNetwork(1, length, prefix)
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane Lane) bool { return lane.ID == "aisle-1" })
	network.Nodes = slices.DeleteFunc(network.Nodes, func(node Node) bool { return node.ID == "branch-1" })
	for index := range network.Nodes {
		switch node := &network.Nodes[index]; node.ID {
		case "inner-1":
			node.Position = Point{-length, length}
		case "berth-1":
			node.Position = Point{-length, length + 10}
		}
	}
	for index := range network.Lanes {
		if lane := &network.Lanes[index]; lane.From == "branch-1" {
			lane.From = "hold"
		} else if lane.To == "branch-1" {
			lane.To = "hold"
		}
	}
	group := &network.ManeuverGroups[0]
	group.LaneIDs = slices.DeleteFunc(group.LaneIDs, func(id string) bool { return id == "aisle-1" })
	return network
}

// laneStartArrival returns a simulation of network with one pod on the
// route of the lanes with ids, to the stop station. The pod starts at rest
// at the start of the lane before lane k, a reverse lane, with the grant
// of reservationEnd. It moves until it stops at the start of lane k, with
// lane k latched and no dwell ticks.
func laneStartArrival(t *testing.T, network Network, ids []string, k int) *Simulation {
	t.Helper()
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "stop", Class: CompactClass}})
	if err != nil {
		t.Fatal(err)
	}
	var route []Lane
	for _, id := range ids {
		route = append(route, s.network.Lanes[networkLaneIndex(s.network, id)])
	}
	v := &s.vehicles[0]
	v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
	s.setVehicleRoute(v, route)
	v.RelocatingTo, v.destinationStation = "stop", "stop"
	v.origin = s.network.Stations[0].Berths[0]
	v.blockIndex = v.blocks.laneFirst(k - 1)
	v.distance = v.blocks.lanes[k-1].start
	v.reservedThrough = reservationEnd(&v.blocks, v.blockIndex)
	if v.blocks.end(v.reservedThrough) <= v.blocks.lanes[k].start {
		t.Fatalf("the frontier ends at %v, short of the reverse lane at %v", v.blocks.end(v.reservedThrough), v.blocks.lanes[k].start)
	}
	for tick := 0; tick < 60*TicksPerSecond && (v.distance != v.blocks.lanes[k].start || v.Pod.Speed != 0); tick++ {
		s.tick++
		s.move(v)
	}
	if v.distance != v.blocks.lanes[k].start || v.Pod.Speed != 0 || v.reversal != (reversal{lane: k}) {
		t.Fatalf("the pod is at %v at %v m/s with the latch %+v, want an arrival at %v", v.distance, v.Pod.Speed, v.reversal, v.blocks.lanes[k].start)
	}
	return s
}

// driftBayRoute returns the IDs of the lanes of a route of
// driftBayNetwork with 20 bays, through twenty lanes of the group to the
// reverse lane at route index 22, and on to the stop station.
func driftBayRoute() []string {
	ids := []string{"far-in", "approach"}
	for bay := 1; bay <= 19; bay++ {
		ids = append(ids, fmt.Sprint("aisle-", bay))
	}
	return append(ids, "bay-19-in-inner-19", "bay-19-out-branch-19", "aisle-20", "aisle-out", "leave", "loop-down", "loop-in")
}

// departureTicks steps s until its first pod moves, and returns the
// number of steps.
func departureTicks(s *Simulation) int {
	v := &s.vehicles[0]
	at := v.distance
	for tick := range 120 * TicksPerSecond {
		if v.distance != at {
			return tick
		}
		s.Step()
	}
	return -1
}

// TestTrimmedLaneStartLatch saves a pod that arrives at the start of a
// reverse lane, with the lane latched and no dwell ticks, as a trimmed
// route after a discarded prefix of 1,500,000 m, with a 30 m lane and
// twenty lanes of 20.4 m or 20.1 m before the reverse lane. The saved
// distance is the live route distance less the start of the saved route,
// as version 13 saves it. It is more than the snap of the trimmed frame
// from the lane start of the trimmed route, short of it with 20.4 m lanes
// and past it with 20.1 m lanes, so the restore keeps the saved distance.
// TestTrimmedRoundingFollowUp records what the restore then does with the
// latch.
func TestTrimmedLaneStartLatch(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		length float64
		// saved is the saved distance.
		saved float64
	}{{"20.4 m lanes", 20.4, 437.99999999813735}, {"20.1 m lanes", 20.1, 432.00000000186265}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const k = 22
			network := driftBayNetwork(20, test.length, 1_500_000)
			s := laneStartArrival(t, network, driftBayRoute(), k)
			v := &s.vehicles[0]
			state := roundTripState(t, s.exportState(-1))
			pod := state.Pods[0]
			if !pod.RouteTrimmed || len(pod.Route) != len(v.Route)-1 || pod.ReversalLane != k-1 || pod.ReversalTicks != 0 {
				t.Fatalf("the save has the marker %t, %d lanes, and the latch %d, %d", pod.RouteTrimmed, len(pod.Route), pod.ReversalLane, pod.ReversalTicks)
			}
			if pod.Distance != v.distance-v.blocks.lanes[1].start || pod.Distance != test.saved {
				t.Fatalf("the save has the distance %v, want %v less %v, %v", pod.Distance, v.distance, v.blocks.lanes[1].start, test.saved)
			}
			trimmed, _ := s.routeBlocks(v.Route[1:])
			if start := trimmed.lanes[k-1].start; math.Abs(pod.Distance-start) <= restoreDriftTolerance || (pod.Distance < start) != (test.length == 20.4) {
				t.Fatalf("the saved distance is %g m from the lane start %v, want beyond the snap, short with 20.4 m lanes", pod.Distance-start, start)
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
			if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 {
				t.Fatalf("restore %+v, %v, want the pod in place", result, err)
			}
			// The pod is in the first block of the reverse lane.
			if got := &restored.vehicles[0]; got.distance != test.saved || got.blockIndex != got.blocks.laneFirst(k-1) {
				t.Fatalf("the restored pod is at %v in block %d, want %v in block %d", got.distance, got.blockIndex, test.saved, got.blocks.laneFirst(k-1))
			}
		})
	}
}

// TestTrimmedRoundingFollowUp records three restores of trimmed routes
// that do not agree with the live pod (exact restore coordinates, review
// round 6). Version 13 restores them in the same way. The sums of the lane
// lengths of a trimmed route start at 0, and the live sums start at the
// discarded prefix, so a lane start or a cell end can round differently in
// the two. Each case checks the current behavior and then skips. A change
// that fixes a case makes it fail, and that change replaces the case with
// a test of the fix.
//
//   - The two arrivals of TestTrimmedLaneStartLatch. The restore drops the
//     latch, and the pod leaves after 1 tick, not after a full dwell.
//   - An arrival at a reverse lane after a discarded prefix of 1,000 m, a
//     30 m approach, and a lane of 60.05 m with three cells. The saved
//     distance is 90.04999999999995 m. In the trimmed route, the reverse
//     lane starts at 90.05 m, and the last cell before it ends at
//     90.04999999999998 m. The snap takes the cell end, so the restore
//     drops the latch, and the pod leaves after 1 tick.
//   - TestRestoreTrimmedCellEnd with a lane l1 of 30 m. The saved distance
//     is 103.20000000298023 m, and the third cell of l2 ends at 103.2 m in
//     the trimmed route. The saved distance is past the snap, so the
//     restore grants the fourth cell of l2, which the live pod does not
//     hold.
func TestTrimmedRoundingFollowUp(t *testing.T) {
	t.Parallel()
	dwell := func(t *testing.T, network Network, ids []string, k int, saved float64) {
		t.Helper()
		s := laneStartArrival(t, network, ids, k)
		state := roundTripState(t, s.exportState(-1))
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if pod := state.Pods[0]; err != nil || !cleanRestore(result) || pod.Distance != saved || !slices.Equal(result.DroppedLatches, []string{"01"}) {
			t.Fatalf("the save at %v restores %+v, %v, want the save at %v with the latch dropped", pod.Distance, result, err, saved)
		}
		if live, got := departureTicks(s), departureTicks(restored); live != reversalDwellTicks || got != 1 {
			t.Fatalf("the live pod leaves after %d ticks and the restored pod after %d, want %d and 1", live, got, reversalDwellTicks)
		}
		t.Skipf("follow-up: the restore at %v m drops the latch and skips the reversal dwell", saved)
	}
	t.Run("20.4 m lanes", func(t *testing.T) {
		t.Parallel()
		dwell(t, driftBayNetwork(20, 20.4, 1_500_000), driftBayRoute(), 22, 437.99999999813735)
	})
	t.Run("20.1 m lanes", func(t *testing.T) {
		t.Parallel()
		dwell(t, driftBayNetwork(20, 20.1, 1_500_000), driftBayRoute(), 22, 432.00000000186265)
	})
	t.Run("three cells of 60.05 m", func(t *testing.T) {
		t.Parallel()
		ids := []string{"far-in", "approach", "bay-1-in-inner-1", "bay-1-out-branch-1", "aisle-out", "leave", "loop-down", "loop-in"}
		dwell(t, turnBackNetwork(60.05, 1000), ids, 3, 90.04999999999995)
	})
	t.Run("a 30 m lane before the cell end", func(t *testing.T) {
		t.Parallel()
		network, fleet, live := trimmedCellEndLive(t, 30)
		saved := live.ExportState()
		restored, result := restoreMotion(t, network, fleet, saved)
		got := &restored.vehicles[0]
		third := got.blocks.lanes[1].first + 2
		if pod := saved.Pods[0]; pod.Distance != 103.20000000298023 || got.blocks.cellEnd(1, 2) != 103.2 || !cleanRestore(result) || got.reservedThrough != third+1 {
			t.Fatalf("the save at %v restores %+v with the frontier %d, want the save at 103.20000000298023 in place with %d", pod.Distance, result, got.reservedThrough, third+1)
		}
		t.Skip("follow-up: the restore at 103.20000000298023 m grants the fourth cell of l2, past the live grant")
	})
}

// TestFaultedLatchFollowUp saves a faulted pod at rest at the start of the
// first of two changes of the reverse flag, with its dwell done, and with a
// grant through the start of the second (Astra review, round 14). In the
// next tick, the search of the live pod latches the second lane, and the
// fault holds the pod at the start of the first. The restore drops that
// latch (see latchAgrees), so the export saves the trimmed route with the
// marker, as version 13 saves it. After the fault clears, the live pod
// goes on, and the restored pod dwells at the first lane again.
func TestFaultedLatchFollowUp(t *testing.T) {
	t.Parallel()
	h := restorableHarness(t, "aisle-1", "bay-1-in-inner-1", "bay-1-out-branch-1", "aisle-2", "aisle-out", "leave")
	h.v.reservedThrough = h.v.blocks.laneFirst(2) - 1
	h.run(60 * TicksPerSecond)
	if h.v.distance != h.points[2] || h.v.reversal != (reversal{lane: -2}) {
		t.Fatalf("the pod is at %v with the latch %+v, want %v with the dwell done", h.v.distance, h.v.reversal, h.points[2])
	}
	h.s.faultsOn = true
	id, err := h.s.Fault(FaultRequest{PodID: "01"})
	if err != nil {
		t.Fatal(err)
	}
	if h.v.reservedThrough = reservationEnd(&h.v.blocks, h.v.blockIndex); h.v.reservedThrough < h.v.blocks.laneFirst(3) {
		t.Fatalf("the grant ends at block %d, short of lane 3", h.v.reservedThrough)
	}
	h.s.tick++
	h.s.move(h.v)
	if h.v.distance != h.points[2] || h.v.reversal != (reversal{lane: 3}) || !h.v.faulted {
		t.Fatalf("the pod is at %v with the latch %+v, want %v with the latch of lane 3", h.v.distance, h.v.reversal, h.points[2])
	}
	state := h.s.ExportState()
	if pod := state.Pods[0]; !pod.RouteTrimmed || pod.ReversalLane != 3 {
		t.Fatalf("the save has the marker %t and the latch %d, want a trimmed route with the latch 3", pod.RouteTrimmed, pod.ReversalLane)
	}
	restored, result, err := RestoreState(RestoreStateInput{FaultsOn: true, Network: h.s.network, Fleet: h.s.initial, State: state})
	if err != nil || !cleanRestore(result) || !slices.Equal(result.DroppedLatches, []string{"01"}) {
		t.Fatalf("restore %+v, %v, want the pod in place with its latch dropped", result, err)
	}
	for _, s := range []*Simulation{h.s, restored} {
		if err := s.ClearFault(id); err != nil {
			t.Fatal(err)
		}
	}
	if live, got := restAtLaneStarts(h.s), restAtLaneStarts(restored); live[2] != 1 || got[2] != reversalDwellTicks || live[3] != reversalDwellTicks || got[3] != reversalDwellTicks {
		t.Fatalf("ticks at rest by lane: live %v, restored %v, want 1 and %d at lane 2, and %[3]d at lane 3", live, got, reversalDwellTicks)
	}
	t.Skip("follow-up: the restore drops the latch of a faulted pod held at a done lane, and the pod dwells there again")
}
