package sim

import (
	"maps"
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
// departure from a bay stops for 60 ticks where it enters the aisle, a
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
		untilDwell(h, 30)
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
		untilDwell(h, 30)
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
		untilDwell(h, 30)
		h.v.destination, h.v.destinationStation = h.s.network.Stations[1].Berths[0], "stop"
		h.s.arrive(h.v)
		if h.v.reversal != (reversal{}) {
			t.Fatalf("latch %+v after an arrival", h.v.reversal)
		}
	})
}

// TestReversalDwellRestore saves a pod that departs from a bay, before
// the aisle, at the start of the aisle at rest, and in its dwell. After
// each restore, the pod stays at rest at the start of the aisle for the
// full dwell, and the pod completes its journey.
func TestReversalDwellRestore(t *testing.T) {
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
	branch := Point{0, 0}
	atBranch := func(s *Simulation) bool {
		pod := s.vehicles[0].Pod
		return pod.Activity == Traveling && pod.Position == branch && pod.Speed == 0
	}
	var saves []SavedState
	for tick := 0; tick < 120*TicksPerSecond && len(saves) < 3; tick++ {
		before := s.vehicles[0].Pod
		s.Step()
		switch {
		case len(saves) == 0 && before.LaneID == "bay-1-out-branch-1" && s.vehicles[0].Pod.Position.Y < 1 && !atBranch(s):
			saves = append(saves, s.ExportState())
		case len(saves) == 1 && atBranch(s):
			saves = append(saves, s.ExportState())
		case len(saves) == 2 && s.vehicles[0].reversal.ticks == 30:
			saves = append(saves, s.ExportState())
		}
	}
	if len(saves) != 3 {
		t.Fatalf("%d saves", len(saves))
	}
	for index, state := range saves {
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
		if err != nil || result.Tier != RestorePhysical {
			t.Fatalf("save %d: tier %s, error %v", index, result.Tier, err)
		}
		if restored.vehicles[0].reversal != (reversal{}) {
			t.Fatalf("save %d: restored latch %+v", index, restored.vehicles[0].reversal)
		}
		rest := 0
		if atBranch(restored) {
			rest++
		}
		for tick := 0; tick < 120*TicksPerSecond && restored.completed == 0; tick++ {
			restored.Step()
			if atBranch(restored) {
				rest++
			}
		}
		if restored.completed != 1 || rest != reversalDwellTicks {
			t.Fatalf("save %d: completed %d, %d ticks at rest at the aisle", index, restored.completed, rest)
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
	with := s.routeSeconds(route, motionEstimate{})
	detachIndexes(s)
	s.maneuvers = nil
	if without := s.routeSeconds(route, motionEstimate{}); with != without {
		t.Fatalf("estimate %g with groups, %g without", with, without)
	}
}
