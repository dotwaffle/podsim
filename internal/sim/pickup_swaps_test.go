package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// crossedPickupFixture has two valid assignments on a directed loop. Pod 01
// approaches S3 from S0, while pod 02 approaches S1 from S2. Swapping avoids
// a long loop for each pickup. Ordinary greedy dispatch would not create
// these assignments at this position, but they remain valid during travel.
func crossedPickupFixture(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(lineNetwork(lineStations(0, 2, 2, 2, 2)), place("s0-1", "s2-1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pickup := range []struct {
		pod, from, to string
	}{{"01", "s3", "s0"}, {"02", "s1", "s2"}} {
		v := s.findVehicle(pickup.pod)
		if pickupErr := s.sendPickup(v, pickup.from); pickupErr != nil {
			t.Fatal(pickupErr)
		}
		s.requestID++
		s.waiting = append(s.waiting, waitingTrip{request: Request{
			SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID, PodID: pickup.pod, From: pickup.from, To: pickup.to,
			PartySize: 1, RequestedTick: 0,
		}})
	}
	s.SetExperimentRecords(true)
	advance(s, 5*TicksPerSecond)
	for _, v := range s.vehicles {
		if v.Pod.Activity != Traveling || v.Pod.Speed <= 0 || v.reservedThrough < 0 {
			t.Fatal("fixture did not reach moving pickup frontiers")
		}
	}
	return s
}

func TestPickupSwapCrossedAssignments(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	control := s.Clone()
	before := s.ExportState()
	owners := maps.Clone(s.owners)
	var oldBlocks [2][]block
	for index := range s.vehicles {
		v := &s.vehicles[index]
		oldBlocks[index] = slices.Clone(v.blocks.all()[:v.reservedThrough+1])
	}
	s.SetPickupSwaps(true)
	s.swapPickups()
	if s.PickupSwapStats().Swaps != 1 || s.waiting[0].request.PodID != "02" || s.waiting[1].request.PodID != "01" {
		t.Fatalf("crossed requests were not swapped: %+v %+v", s.PickupSwapStats(), s.waiting)
	}
	if !maps.Equal(owners, s.owners) {
		t.Fatal("swap changed existing resource owners")
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		saved := before.Pods[index]
		if v.distance != saved.Distance || v.Pod.Speed != control.vehicles[index].Pod.Speed ||
			v.Pod.LaneID != control.vehicles[index].Pod.LaneID || v.Pod.Position != control.vehicles[index].Pod.Position ||
			!reflect.DeepEqual(oldBlocks[index], v.blocks.all()[:v.reservedThrough+1]) {
			t.Fatal("swap changed motion or reserved track")
		}
		trip := s.waiting[index]
		if trip.request.ID != before.Waiting[index].Request.ID || trip.request.RequestedTick != before.Waiting[index].Request.RequestedTick {
			t.Fatal("swap changed request identity or age")
		}
	}
	for _, simulation := range []*Simulation{control, s} {
		for range 600 * TicksPerSecond {
			simulation.Step()
			checkTraffic(t, simulation.Snapshot())
			if _, safetyErr := simulation.SafetyObservation().Check(); safetyErr != nil {
				t.Fatal(safetyErr)
			}
			if simulation.completed == 2 {
				break
			}
		}
		if simulation.completed != 2 || simulation.requestID != 2 {
			t.Fatal("swap comparison did not conserve and complete both requests")
		}
	}
	oldWaits := make(map[int]int64)
	for _, timing := range control.RequestTimings() {
		oldWaits[timing.RequestID] = timing.BoardedTick - timing.RequestedTick
	}
	for _, timing := range s.RequestTimings() {
		wait := timing.BoardedTick - timing.RequestedTick
		if wait >= oldWaits[timing.RequestID] {
			t.Fatalf("request %d pickup did not improve: old%d new%d", timing.RequestID, oldWaits[timing.RequestID], wait)
		}
	}
}

func TestPickupSwapImprovementUsesRequestCosts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                   string
		oldA, oldB, newA, newB float64
		want                   bool
	}{
		{name: "both requests improve", oldA: 100, oldB: 200, newA: 150, newB: 50, want: true},
		{name: "each pod improves but first request worsens", oldA: 100, oldB: 200, newA: 50, newB: 150},
		{name: "one tie and useful combined gain", oldA: 100, oldB: 200, newA: 200, newB: 90},
		{name: "gain too small", oldA: 100, oldB: 200, newA: 195, newB: 96},
		{name: "nonfinite estimate", oldA: math.Inf(1), oldB: 200, newA: 150, newB: 50},
		{name: "negative estimate", oldA: 100, oldB: 200, newA: -1, newB: 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := pickupSwapImproves(test.oldA, test.oldB, test.newA, test.newB); got != test.want {
				t.Fatalf("improvement = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPickupSwapRejectsIncompatibleAssignments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(*Simulation)
	}{
		{name: "occupied", edit: func(s *Simulation) { s.vehicles[0].Pod.Occupied = true }},
		{name: "active rider", edit: func(s *Simulation) { s.vehicles[0].Riders = []Request{{PartySize: 1}} }},
		{name: "released", edit: func(s *Simulation) { s.vehicles[0].released = true }},
		{name: "rebalancing", edit: func(s *Simulation) { s.vehicles[0].Rebalancing = true }},
		{name: "buffer member", edit: func(s *Simulation) { s.vehicles[0].buffered = true }},
		{name: "platoon leader", edit: func(s *Simulation) { s.vehicles[0].follower = 2 }},
		{name: "platoon follower", edit: func(s *Simulation) { s.vehicles[1].link.leader = 1 }},
		{name: "committed inlet", edit: func(s *Simulation) { s.vehicles[0].reservedThrough = s.vehicles[0].blocks.len() - 1 }},
		{name: "wrong activity", edit: func(s *Simulation) { s.vehicles[0].Pod.Activity = Idle }},
		{name: "wrong target", edit: func(s *Simulation) { s.vehicles[0].destinationStation = "s1" }},
		{name: "wrong relocation", edit: func(s *Simulation) { s.vehicles[0].RelocatingTo = "s1" }},
		{name: "completed request", edit: func(s *Simulation) { s.waiting[0].request.Completed = true }},
		{name: "duplicate binding", edit: func(s *Simulation) { s.waiting = append(s.waiting, s.waiting[0], s.waiting[0]) }},
		{name: "congestion policy", edit: func(s *Simulation) { s.routingPolicy = CongestionRouting }},
		{name: "queue policy", edit: func(s *Simulation) { s.routingPolicy = QueueRouting }},
		{name: "cooldown", edit: func(s *Simulation) { s.pickupSwaps.cooldown["01"] = s.tick + 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := crossedPickupFixture(t)
			s.SetPickupSwaps(true)
			test.edit(s)
			before := s.Clone()
			s.swapPickups()
			if !reflect.DeepEqual(stripCaches(s).vehicles, stripCaches(before).vehicles) || !reflect.DeepEqual(s.waiting, before.waiting) || !maps.Equal(s.owners, before.owners) || s.PickupSwapStats().Swaps != 0 {
				t.Fatalf("rejected pair changed physical state: %+v", s.PickupSwapStats())
			}
		})
	}
}

func TestPickupSwapFailedSecondRouteChangesNothing(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	s.SetPickupSwaps(true)
	_, from, ok := s.divertStart(&s.vehicles[1])
	if !ok {
		t.Fatal("second pod cannot divert")
	}
	station, _ := s.station(s.waiting[0].request.From)
	// Model an unavailable route result through the existing route cache.
	// The first replacement remains reachable and must not commit alone.
	for _, berth := range station.Berths {
		s.cacheRoute(routeKey{from: from, to: berth.Node}, routeResult{err: ErrUnreachable})
		s.cacheRoute(routeKey{from: from, to: berth.Node, station: true}, routeResult{err: ErrUnreachable})
	}
	before := s.Clone()
	s.swapPickups()
	if s.PickupSwapStats().RouteFailures != 1 || s.PickupSwapStats().Swaps != 0 ||
		!reflect.DeepEqual(stripCaches(s).vehicles, stripCaches(before).vehicles) || !reflect.DeepEqual(s.waiting, before.waiting) || !maps.Equal(s.owners, before.owners) {
		t.Fatalf("failed second route changed physical state: %+v", s.PickupSwapStats())
	}
}

func TestPickupSwapImmediateRestoreKeepsAssignments(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	s.SetPickupSwaps(true)
	s.swapPickups()
	if s.PickupSwapStats().Swaps != 1 {
		t.Fatal("fixture did not swap before save")
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
		t.Fatalf("immediate swapped restore: %+v %v", result, err)
	}
	if restored.pickupSwaps != nil || len(restored.waiting) != 2 {
		t.Fatal("restore changed pending count or enabled swaps")
	}
	for index, trip := range restored.waiting {
		before := s.waiting[index].request
		if trip.request.ID != before.ID || trip.request.PodID != before.PodID || trip.request.RequestedTick != before.RequestedTick {
			t.Fatal("restore lost a swapped binding, identity, or request age")
		}
	}
	for _, v := range restored.vehicles {
		old := s.findVehicle(v.Pod.ID)
		if v.Pod.Activity != Traveling || v.Pod.Position != old.Pod.Position || v.Pod.LaneID != old.Pod.LaneID || v.Pod.LaneDistance != old.Pod.LaneDistance {
			t.Fatal("restore changed a moving pickup's physical position")
		}
	}
	for range 600 * TicksPerSecond {
		restored.Step()
		checkTraffic(t, restored.Snapshot())
		if _, safetyErr := restored.SafetyObservation().Check(); safetyErr != nil {
			t.Fatal(safetyErr)
		}
		if restored.completed == 2 {
			return
		}
	}
	t.Fatal("disabled restored controller failed to serve the swapped requests")
}

func TestPickupSwapCommittedInletKeepsAssignment(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	v := s.findVehicle("01")
	stepUntil(t, s, "first committed pickup inlet cell", func() bool {
		if v.Pod.Activity != Traveling || v.reservedThrough < 0 {
			return false
		}
		last := v.blocks.at(v.reservedThrough)
		return last.lane.To == v.destination.Node && v.Pod.BerthID == ""
	})
	if v.distance >= v.blocks.end(v.blocks.len()-1) {
		t.Fatal("fixture already arrived")
	}
	s.SetPickupSwaps(true)
	before := s.Clone()
	s.swapPickups()
	if s.PickupSwapStats().Swaps != 0 || !reflect.DeepEqual(stripCaches(s).vehicles, stripCaches(before).vehicles) || !reflect.DeepEqual(s.waiting, before.waiting) {
		t.Fatal("reserved pickup inlet allowed reassignment")
	}
}

func TestPickupSwapRedirectPreservesOnlySamePendingAge(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-group", true: "changed-suffix"}[changed], func(t *testing.T) {
			t.Parallel()
			s := crossedPickupFixture(t)
			if !changed {
				advance(s, admissionAgeTicks)
			}
			v := s.findVehicle("01")
			to := redirection{route: slices.Clone(v.Route), berth: v.destination, station: v.destinationStation}
			if changed {
				// The pending group crosses the S1 entry. Its through-lane
				// suffix changes to a berth inlet for the replacement pickup.
				s.owners[resource{kind: nodeResource, id: "s1-entry"}] = "external"
				stepUntil(t, s, "pickup stopped before the entry fork", func() bool {
					return v.Pod.Speed == 0 && v.Pod.BlockedBy == "external"
				})
				route, berth, ok := s.candidateRoute(v, "s1", nil)
				if !ok {
					t.Fatal("replacement cannot reach its pickup")
				}
				to = redirection{route: route, berth: berth, station: "s1"}
			}
			v.pending, v.waitSince = v.reservedThrough+1, s.tick-admissionAgeTicks
			first, age := v.pending, v.waitSince
			s.redirectPickupSwap(v, to)
			if !changed {
				if v.pending != first || v.waitSince != age || compareAdmission(intent{since: v.waitSince, priority: 1}, intent{since: s.tick, priority: 0}, s.tick) >= 0 {
					t.Fatal("unchanged pending group lost its aging precedence")
				}
				return
			}
			if v.pending != -1 {
				t.Fatal("changed suffix inherited the old pending resource")
			}
			// Deny the replacement's next block, making admission record
			// the new group's age instead of the unrelated old wait.
			s.owners[v.blocks.at(v.reservedThrough + 1).resources[0]] = "external"
			s.admit()
			if v.pending != first || v.waitSince != s.tick {
				t.Fatalf("changed group's wait did not restart: pending%d since%d tick%d", v.pending, v.waitSince, s.tick)
			}
		})
	}
}

func TestPickupSwapPendingAgeTracksCompleteGroup(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*vehicle)
		want   bool
	}{
		{name: "same complete reservation", want: true},
		{name: "same number different lane", change: func(v *vehicle) { v.blocks.route[1].ID = "other" }},
		{name: "same lane different resource", change: func(v *vehicle) { v.blocks.lanes[1].cells.resources[0].id = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			junction := resource{kind: junctionResource, id: "merge"}
			blocks := []block{
				{lane: Lane{ID: "prefix"}, end: 30},
				{lane: Lane{ID: "next"}, start: 30, end: 60, resources: []resource{junction}},
				{lane: Lane{ID: "tail"}, start: 60, end: 90, resources: []resource{junction}},
			}
			a := vehicle{pending: 1, blocks: blockListOf(blocks)}
			b := vehicle{blocks: blockListOf(blocks)}
			if test.change != nil {
				test.change(&b)
			}
			if got := samePendingReservation(&a, &b); got != test.want {
				t.Fatalf("same pending reservation = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPickupSwapWorkLimitsAndCursor(t *testing.T) {
	t.Parallel()
	t.Run("cheap scans rotate", func(t *testing.T) {
		t.Parallel()
		// No eligible bindings: all work stops before routing or physics.
		s := &Simulation{vehicles: make([]vehicle, 30), waiting: make([]waitingTrip, 2)}
		for index := range s.vehicles {
			s.vehicles[index].Pod.ID = strconv.Itoa(index)
		}
		s.SetPickupSwaps(true)
		s.swapPickups()
		if s.PickupSwapStats().ScannedPairs != pickupSwapScanLimit || s.pickupSwaps.left != 10 || s.pickupSwaps.right != 22 {
			t.Fatalf("first scan lost its frontier: %+v", s.pickupSwaps)
		}
		s.swapPickups()
		if s.PickupSwapStats().ScannedPairs != pickupSwapScanLimit {
			t.Fatal("a second call in the same second exceeded the scan limit")
		}
		s.tick += TicksPerSecond
		s.swapPickups()
		if s.PickupSwapStats().ScannedPairs != 2*pickupSwapScanLimit || s.pickupSwaps.left != 2 || s.pickupSwaps.right != 23 {
			t.Fatalf("second scan did not continue and wrap: %+v", s.pickupSwaps)
		}
	})
	t.Run("route pairs stop at budget", func(t *testing.T) {
		t.Parallel()
		var stations []lineStation
		var fleet []Placement
		for index := range 30 {
			id := fmt.Sprintf("s%d", index)
			stations = append(stations, lineStation{id: id, berths: 1})
			fleet = append(fleet, Placement{ID: fmt.Sprintf("%02d", index), StationID: id})
		}
		s, err := NewFleet(lineNetwork(stations), fleet)
		if err != nil {
			t.Fatal(err)
		}
		for index := range s.vehicles {
			v := &s.vehicles[index]
			from := fmt.Sprintf("s%d", (index+1)%30)
			if pickupErr := s.sendPickup(v, from); pickupErr != nil {
				t.Fatal(pickupErr)
			}
			s.waiting = append(s.waiting, waitingTrip{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: index + 1, PodID: v.Pod.ID, From: from}})
		}
		s.SetPickupSwaps(true)
		s.swapPickups()
		stats := s.PickupSwapStats()
		if stats.RoutePairs != pickupSwapRouteLimit || stats.Swaps != 0 || stats.ScannedPairs > pickupSwapScanLimit {
			t.Fatalf("route budget not exercised: %+v", stats)
		}
		if s.pickupSwaps.left != 0 || s.pickupSwaps.right != 9 {
			t.Fatal("route budget exhaustion lost the next pair")
		}
	})
}

func TestPickupSwapDisabledCloneResetAndRestore(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	before := s.ExportState()
	s.swapPickups()
	if !reflect.DeepEqual(before, s.ExportState()) || s.PickupSwapStats() != (PickupSwapStats{}) {
		t.Fatal("default policy changed state")
	}
	s.SetPickupSwaps(true)
	s.swapPickups()
	clone := s.Clone()
	clone.pickupSwaps.cooldown["01"]++
	if clone.pickupSwaps.cooldown["01"] == s.pickupSwaps.cooldown["01"] {
		t.Fatal("clone shares cooldown storage")
	}
	clone = s.Clone()
	for range 60 * TicksPerSecond {
		s.Step()
		clone.Step()
		if !reflect.DeepEqual(s.Snapshot(), clone.Snapshot()) || s.PickupSwapStats() != clone.PickupSwapStats() {
			t.Fatal("clone changed experimental replay")
		}
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("swapped state did not restore physically: %+v %v", result, err)
	}
	if restored.pickupSwaps != nil || restored.PickupSwapStats() != (PickupSwapStats{}) {
		t.Fatal("restore enabled an unsupported experimental policy")
	}
	s.Reset()
	if !s.pickupSwaps.enabled || s.pickupSwaps.nextTick != 0 || len(s.pickupSwaps.cooldown) != 0 || s.PickupSwapStats() != (PickupSwapStats{}) {
		t.Fatal("reset retained swap history or lost enablement")
	}
	if clone.PickupSwapStats().Swaps != 1 {
		t.Fatal("reset changed the independent clone")
	}
}
