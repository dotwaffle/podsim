package sim

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// faultPose is the state of a pod that the transition rules of section 10
// of the incident suspension contract compare.
type faultPose struct {
	faulted, traveling     bool
	faultCap, distance     float64
	speed                  float64
	lane, berth            string
	laneDistance           float64
	position               Point
	routeVersion           uint64
	reservedThrough        int
	riders, completedRider int
}

// faultPoses returns the pose of each pod.
func faultPoses(s *Simulation) []faultPose {
	poses := make([]faultPose, len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		completed := 0
		for _, rider := range v.Riders {
			if rider.Completed {
				completed++
			}
		}
		poses[index] = faultPose{
			faulted: v.faulted, traveling: v.Pod.Activity == Traveling, faultCap: v.faultCap, distance: v.distance, speed: v.Pod.Speed,
			lane: v.Pod.LaneID, berth: v.Pod.BerthID, laneDistance: v.Pod.LaneDistance, position: v.Pod.Position,
			routeVersion: v.routeVersion, reservedThrough: v.reservedThrough, riders: len(v.Riders), completedRider: completed,
		}
	}
	return poses
}

// faultTransition checks the transition rules of a pod that is faulted
// before and after one tick or command (F3 and F4). The speed never rises,
// and the cap does not change. The grants do not grow. While the pod
// brakes, its distance does not fall and stays at most the cap. A pod at
// rest keeps its physical pose and speed 0, and its distance while its
// route stays. No rider completes.
func faultTransition(before, after faultPose) error {
	switch {
	case after.speed > before.speed:
		return errors.New("the speed rises")
	case after.faultCap != before.faultCap:
		return errors.New("the cap changes")
	case after.reservedThrough > before.reservedThrough:
		return errors.New("the grants grow")
	case after.completedRider > before.completedRider:
		return errors.New("a rider completes")
	}
	sameRoute := after.routeVersion == before.routeVersion
	if before.speed > 0 {
		if sameRoute && (after.distance < before.distance || after.distance > after.faultCap) {
			return errors.New("the braking pod leaves its distance range")
		}
		return nil
	}
	if after.speed != 0 || after.lane != before.lane || after.laneDistance != before.laneDistance || after.berth != before.berth || after.position != before.position {
		return errors.New("the pod at rest moves")
	}
	if sameRoute && after.distance != before.distance {
		return errors.New("the distance of the pod at rest changes")
	}
	return nil
}

// checkFaultsEachTick makes s check, after each tick and each public
// command, the state contract (with the blocked set), the order balance,
// and the transition rules of each faulted pod.
func checkFaultsEachTick(t *testing.T, s *Simulation) {
	t.Helper()
	previous := faultPoses(s)
	s.monitor = func(s *Simulation) {
		if err := s.CheckContract(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		if err := checkOrderBalance(s); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		next := faultPoses(s)
		for index := range next {
			if previous[index].faulted && next[index].faulted {
				if err := faultTransition(previous[index], next[index]); err != nil {
					t.Fatalf("tick %d: pod %s: %v: %+v, then %+v", s.tick, s.vehicles[index].Pod.ID, err, previous[index], next[index])
				}
			}
		}
		previous = next
	}
}

// faultLegFleet is incidentLegFleet with faults on and an evacuation delay
// of 300 seconds.
func faultLegFleet(t *testing.T) *Simulation {
	t.Helper()
	s := incidentLegFleet(t)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	return s
}

// cruiseOn steps s until pod v travels on the lane at the speed limit of
// the lane.
func cruiseOn(t *testing.T, s *Simulation, v *vehicle, lane string) {
	t.Helper()
	stepUntil(t, s, "pod "+v.Pod.ID+" cruises on "+lane, func() bool {
		return v.Pod.Activity == Traveling && v.Pod.LaneID == lane && v.Pod.Speed == v.blocks.currentLane(v.blockIndex).SpeedLimit
	})
}

// laneBlocked reports whether the blocked set has the lane with the ID.
func laneBlocked(s *Simulation, id string) bool {
	index, ok := s.graph.lanes[id]
	return ok && index < len(s.blocked.lanes) && s.blocked.lanes[index]
}

// TestFaultMoveStep checks the braking step. With a far cap the commanded
// speed never rises above the speed, where the ordinary step accelerates.
// With the cap at the stopping distance, the speed falls by at most
// acceleration × dt in each step, and the pod comes to rest at the cap.
func TestFaultMoveStep(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{400}, []float64{14})
	blocks := &v.blocks
	lane := 0
	far := blocks.end(blocks.len() - 1)
	for _, speed := range []float64{0, 0.5, 7, 13.9} {
		ordinary := ordinaryMoveStep(blocks, lane, 10, speed, far)
		braking := faultMoveStep(blocks, lane, 10, speed, far, far)
		if ordinary.speed <= speed || braking.speed > speed || braking.commandedSpeed > speed {
			t.Fatalf("speed %g: ordinary %+v, braking %+v", speed, ordinary, braking)
		}
	}
	// The cap lowers a far grant end, and a near grant end lowers the cap.
	if a, b := faultMoveStep(blocks, lane, 10, 14, far, 20), faultMoveStep(blocks, lane, 10, 14, 20, far); a != b {
		t.Fatalf("the cap and the grant end differ: %+v and %+v", a, b)
	}
	distance, speed := 50.0, 14.0
	stop := distance + stoppingDistance(speed)
	dt := 1.0 / TicksPerSecond
	for tick := 0; speed > 0; tick++ {
		if tick > 10*TicksPerSecond {
			t.Fatal("the pod does not come to rest")
		}
		next := faultMoveStep(blocks, lane, distance, speed, far, stop)
		if next.speed > speed || speed-next.speed > acceleration*dt+1e-9 || next.distance < distance || next.distance > stop {
			t.Fatalf("tick %d: %g m at %g m/s, then %+v", tick, distance, speed, next)
		}
		distance, speed = next.distance, next.speed
	}
	if distance != stop {
		t.Fatalf("the pod rests at %g m, want the cap %g m", distance, stop)
	}
}

// TestFaultCapAtGrantEnd faults a pod whose stopping distance passes its
// grant end, as the discrete ordinary step allows. The grant end is the
// cap, and the pod comes to rest there.
func TestFaultCapAtGrantEnd(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "garden", BerthID: "garden-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.faultsOn = true
	v := s.findVehicle("01")
	garden, _ := s.network.Station("garden")
	market, _ := s.network.Station("market")
	route, err := s.route(garden.Berths[0].Node, market.Berths[0].Node)
	if err != nil {
		t.Fatal(err)
	}
	s.setVehicleRoute(v, route)
	v.origin = garden.Berths[0]
	v.Pod.Activity = DepartingEmpty
	s.grant(intent{index: s.vehicleIndexes[v.Pod.ID], block: 0, through: 0})
	if v.reservedThrough < 0 || v.reservedThrough >= v.blocks.len()-1 {
		t.Fatalf("the first grant ends at block %d of %d", v.reservedThrough, v.blocks.len())
	}
	v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
	// Only move runs, so admission does not extend the grant.
	frontier := v.blocks.end(v.reservedThrough)
	for v.distance+stoppingDistance(v.Pod.Speed) <= frontier {
		if v.Pod.Speed == 0 && v.distance > 0 {
			t.Fatal("the pod stopped with its stopping distance inside its grants")
		}
		s.tick++
		s.move(v)
	}
	startFault(t, s, v, 0)
	if v.faultCap != frontier {
		t.Fatalf("cap %g, want the grant end %g", v.faultCap, frontier)
	}
	for v.Pod.Speed > 0 {
		s.tick++
		s.move(v)
	}
	if v.distance != frontier || v.Pod.Activity != Traveling {
		t.Fatalf("the pod rests at %g m as %s, want %g m", v.distance, v.Pod.Activity, frontier)
	}
}

// TestFaultBrakingOnLane faults a pod that cruises on a lane with riders.
// The pod brakes to rest at its cap with the transition rules of section
// 10. Its cap is the stopping distance or the grant end. At rest it keeps
// its pose, its grants, its owners and its riders, and admission does not
// extend its grants. It is withdrawn, and it reports its fault.
func TestFaultBrakingOnLane(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	checkFaultsEachTick(t, s)
	riders := slices.Clone(v.Riders)
	wantCap := math.Min(v.distance+stoppingDistance(v.Pod.Speed), v.blocks.end(v.reservedThrough))
	id := startFault(t, s, v, 0)
	if wantCap != v.distance+stoppingDistance(v.Pod.Speed) {
		t.Fatalf("the grant end %g is nearer than the stop at %g", v.blocks.end(v.reservedThrough), v.distance+stoppingDistance(v.Pod.Speed))
	}
	if v.faultCap != wantCap || v.withdrawn != faultHold || v.pending != -1 {
		t.Fatalf("cap %g, want %g; withdrawn %d, pending %d", v.faultCap, wantCap, v.withdrawn, v.pending)
	}
	if !laneBlocked(s, "s0-link") || !s.rerouteDue {
		t.Fatal("the lane of the faulted pod is not blocked")
	}
	if err := s.checkBlocked(); err != nil {
		t.Fatal(err)
	}
	through := v.reservedThrough
	for v.Pod.Speed > 0 {
		// Admission writes the report before the move, as each wait
		// report.
		want, speed := FaultBraking, v.Pod.Speed
		s.Step()
		if speed-v.Pod.Speed > acceleration/TicksPerSecond+1e-9 {
			t.Fatalf("tick %d: the speed falls from %g to %g m/s", s.tick, speed, v.Pod.Speed)
		}
		if v.Pod.WaitReason != want || v.Pod.BlockedBy != id {
			t.Fatalf("tick %d: report %q by %q, want %q by %s", s.tick, v.Pod.WaitReason, v.Pod.BlockedBy, want, id)
		}
	}
	if v.distance != v.faultCap || v.Pod.Activity != Traveling {
		t.Fatalf("the pod rests at %g m as %s, want the cap %g m", v.distance, v.Pod.Activity, v.faultCap)
	}
	owned := maps.Clone(s.owners)
	releases := maps.Clone(v.routeReleases)
	for range 60 * TicksPerSecond {
		s.Step()
	}
	if v.reservedThrough != through || v.Pod.Speed != 0 || !maps.Equal(s.owners, owned) || !maps.Equal(v.routeReleases, releases) {
		t.Fatalf("the pod at rest changed its grants: through %d, want %d", v.reservedThrough, through)
	}
	if !reflect.DeepEqual(v.Riders, riders) || v.Pod.WaitReason != FaultStopped || v.Pod.BlockedBy != id {
		t.Fatalf("riders %+v, report %q by %q", v.Riders, v.Pod.WaitReason, v.Pod.BlockedBy)
	}
}

// TestFaultClearDuringBraking clears a fault while the pod brakes. The
// pod has no cap after the clear, is in service again, and continues
// inside its grants with the ordinary step: it does not snap to rest, and
// it passes the old cap and delivers its riders.
func TestFaultClearDuringBraking(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	checkFaultsEachTick(t, s)
	id := startFault(t, s, v, 0)
	oldCap := v.faultCap
	for range 10 {
		s.Step()
	}
	if v.Pod.Speed == 0 {
		t.Fatal("the pod is at rest before the clear")
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	if v.faulted || v.faultCap != 0 || v.withdrawn != 0 || s.blockedActive() {
		t.Fatalf("faulted %t, cap %g, withdrawn %d, blocked %t", v.faulted, v.faultCap, v.withdrawn, s.blockedActive())
	}
	speed, distance := v.Pod.Speed, v.distance
	s.Step()
	if v.Pod.Speed == 0 || v.distance <= distance || v.Pod.Speed < speed-acceleration/TicksPerSecond {
		t.Fatalf("after the clear: %g m at %g m/s, from %g m at %g m/s", v.distance, v.Pod.Speed, distance, speed)
	}
	stepUntil(t, s, "pod 01 passes the old cap", func() bool { return v.Pod.Activity != Traveling || v.distance > oldCap })
	completed := s.completed
	stepUntil(t, s, "riders complete", func() bool { return s.completed == completed+2 })
}

// TestFaultAtBerth faults a pod at a berth in each accepted activity. The
// phase timer stops, no rider alights, the pod does not depart, and the
// berth and the lanes into it are blocked. After the clear, the timer runs
// again.
func TestFaultAtBerth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, s *Simulation) *vehicle
	}{
		{"idle", func(_ *testing.T, s *Simulation) *vehicle { return s.findVehicle("01") }},
		{"boarding", func(t *testing.T, s *Simulation) *vehicle {
			t.Helper()
			v := boardParties(t, s, "s1", "s2")
			if v.Pod.Activity != Boarding || v.phaseTicks == 0 {
				t.Fatalf("pod 01 is %s with phase %d", v.Pod.Activity, v.phaseTicks)
			}
			return v
		}},
		{"unloading", func(t *testing.T, s *Simulation) *vehicle {
			t.Helper()
			v := boardParties(t, s, "s1", "s2")
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks > 1 })
			return v
		}},
		{"continuing", func(t *testing.T, s *Simulation) *vehicle {
			t.Helper()
			v := boardParties(t, s, "s1", "s2")
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
			s.alight(v)
			s.continueJourney(v)
			if v.Pod.Activity != Continuing {
				t.Fatal("the pod does not continue")
			}
			return v
		}},
		{"departing empty", func(t *testing.T, s *Simulation) *vehicle {
			t.Helper()
			v := s.findVehicle("02")
			if err := s.RequestJourney("01", "s2"); err != nil {
				t.Fatal(err)
			}
			if err := s.RequestTrip("s1", "s2"); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, s, "pod 02 departs empty", func() bool { return v.Pod.Activity == DepartingEmpty })
			return v
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := faultLegFleet(t)
			v := test.prepare(t, s)
			checkFaultsEachTick(t, s)
			pod, phase, riders := v.Pod, v.phaseTicks, slices.Clone(v.Riders)
			id := startFault(t, s, v, 0)
			berth := Berth{ID: v.Pod.BerthID, Node: v.Pod.BerthID}
			if !s.berthBlocked(berth) || !laneBlocked(s, berth.ID+"-in") {
				t.Fatalf("berth %s is not blocked", berth.ID)
			}
			for range 30 * TicksPerSecond {
				s.Step()
			}
			pod.WaitReason, pod.BlockedBy = FaultStopped, id
			if v.Pod != pod || v.phaseTicks != phase || !reflect.DeepEqual(v.Riders, riders) {
				t.Fatalf("the faulted pod changed: %+v, phase %d, want %+v, phase %d", v.Pod, v.phaseTicks, pod, phase)
			}
			if err := s.clearFault(id); err != nil {
				t.Fatal(err)
			}
			if s.berthBlocked(berth) || v.withdrawn != 0 {
				t.Fatalf("berth blocked %t, withdrawn %d after the clear", s.berthBlocked(berth), v.withdrawn)
			}
			if phase > 0 {
				s.Step()
				if v.phaseTicks != phase-1 && v.Pod.Activity == pod.Activity {
					t.Fatalf("the phase timer is %d after the clear, want %d", v.phaseTicks, phase-1)
				}
			}
		})
	}
}

// TestFaultArrivalDuringBraking faults a pod whose cap is the end of its
// route. The pod arrives through arrive, and the fault stays. The berth is
// blocked in place of the lane, no rider alights, and the arrival keeps
// the fault report.
func TestFaultArrivalDuringBraking(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s1")
	stepUntil(t, s, "pod 01 near its berth", func() bool {
		return v.Pod.Activity == Traveling && v.Pod.Speed > 0 && v.reservedThrough == v.blocks.len()-1 &&
			v.distance+stoppingDistance(v.Pod.Speed) >= v.blocks.end(v.reservedThrough)
	})
	checkFaultsEachTick(t, s)
	id := startFault(t, s, v, 0)
	if v.faultCap != v.blocks.end(v.blocks.len()-1) {
		t.Fatalf("cap %g, want the route end %g", v.faultCap, v.blocks.end(v.blocks.len()-1))
	}
	stepUntil(t, s, "arrival", func() bool { return v.Pod.Activity != Traveling })
	berth := v.destination
	if v.Pod.Activity != Unloading || !v.faulted || len(s.faults) != 1 || !s.berthBlocked(berth) {
		t.Fatalf("pod %+v, faulted %t, records %v, berth blocked %t", v.Pod, v.faulted, faultIDs(s), s.berthBlocked(berth))
	}
	if v.Pod.WaitReason != FaultStopped || v.Pod.BlockedBy != id {
		t.Fatalf("report %q by %q after the arrival", v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	phase, aboard, completed := v.phaseTicks, v.RidersAboard(), s.completed
	for range 30 * TicksPerSecond {
		s.Step()
	}
	if v.phaseTicks != phase || v.RidersAboard() != aboard || s.completed != completed || v.Pod.BlockedBy != id {
		t.Fatalf("phase %d, aboard %d, completed %d, blocked by %q", v.phaseTicks, v.RidersAboard(), s.completed, v.Pod.BlockedBy)
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "the rider alights", func() bool { return s.completed == completed+1 })
}

// TestFaultReportAtEntryEnd faults a pod with no berth whose cap is the
// end of its entry route. A faulted pod chooses no berth, so it comes to
// rest there. Each publication at the entry end reports an occupied berth
// for a pod in service, and the faulted pod keeps its fault report.
func TestFaultReportAtEntryEnd(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	if v.destination.ID != "" {
		t.Fatalf("the pod has the berth %s", v.destination.ID)
	}
	// With grants to the route end, the pod chooses no berth before it
	// stops.
	end := v.blocks.end(v.blocks.len() - 1)
	s.grant(intent{index: s.vehicleIndexes[v.Pod.ID], block: v.reservedThrough + 1, through: v.blocks.len() - 1})
	stepUntil(t, s, "pod 01 near its entry end", func() bool { return v.distance+stoppingDistance(v.Pod.Speed) >= end })
	checkFaultsEachTick(t, s)
	id := startFault(t, s, v, 0)
	stepUntil(t, s, "rest", func() bool { return v.Pod.Speed == 0 })
	if v.destination.ID != "" || v.distance != end {
		t.Fatalf("the pod rests at %g m with the destination %q, want the entry end %g m", v.distance, v.destination.ID, end)
	}
	for range 10 * TicksPerSecond {
		s.Step()
		if v.Pod.WaitReason != FaultStopped || v.Pod.BlockedBy != id {
			t.Fatalf("tick %d: report %q by %q", s.tick, v.Pod.WaitReason, v.Pod.BlockedBy)
		}
	}
}

// bufferQueue returns a simulation with a station buffer at Market, where
// pod 02 is the head at the frontier and pod 01 waits behind it on the
// entry lane. An external owner holds the only berth until release.
func bufferQueue(t *testing.T) (s *Simulation, release func()) {
	t.Helper()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	s.SetStationBuffers(true)
	for _, id := range []string{"01", "02"} {
		if err := s.RequestJourney(id, "market"); err != nil {
			t.Fatal(err)
		}
	}
	barrier := resource{kind: berthResource, id: "market-1"}
	s.owners[barrier] = podResourceOwner("external")
	head, behind := s.findVehicle("02"), s.findVehicle("01")
	stepUntil(t, s, "a queue of two at the frontier", func() bool {
		return head.buffered && behind.buffered && head.Pod.Speed == 0 && behind.Pod.Speed == 0 && behind.Pod.BlockedBy == "02"
	})
	return s, func() { delete(s.owners, barrier) }
}

// TestFaultInStationEntryQueue faults the head of a station buffer. The
// head makes no berth-grant attempt when the berth is free, and it keeps
// its buffer membership, its route, and its fault report. The pod behind
// it keeps its route and does not pass it. Without the fault, the head
// takes the berth.
func TestFaultInStationEntryQueue(t *testing.T) {
	t.Parallel()
	for _, faulted := range []bool{false, true} {
		s, release := bufferQueue(t)
		s.faultsOn = true
		head, behind := s.findVehicle("02"), s.findVehicle("01")
		route, behindRoute, bufferBerth := head.Route, behind.Route, head.bufferBerth
		var id string
		if faulted {
			if head.pending < 0 {
				t.Fatal("the head requests no grant")
			}
			id = startFault(t, s, head, 0)
			if head.pending != -1 {
				t.Fatalf("the faulted head keeps the request of block %d", head.pending)
			}
			checkFaultsEachTick(t, s)
		}
		release()
		for range 30 * TicksPerSecond {
			s.Step()
			// The head rests at the end of its entry route, where the
			// publication reports an occupied berth for a pod in service.
			if faulted && (head.Pod.WaitReason != FaultStopped || head.Pod.BlockedBy != id) {
				t.Fatalf("tick %d: the faulted head reports %q by %q", s.tick, head.Pod.WaitReason, head.Pod.BlockedBy)
			}
		}
		if !faulted {
			if head.destination.ID != "market-1" {
				t.Fatalf("control: the head has the berth %q", head.destination.ID)
			}
			continue
		}
		if head.destination.ID != "" || head.bufferBerth != bufferBerth || !head.buffered || head.Pod.Speed != 0 || !sameRouteSlice(head.Route, route) {
			t.Fatalf("the faulted head moved to a berth: destination %q, buffer berth %q", head.destination.ID, head.bufferBerth)
		}
		if !sameRouteSlice(behind.Route, behindRoute) || behind.Pod.LaneID != head.Pod.LaneID || behind.Pod.LaneDistance >= head.Pod.LaneDistance {
			t.Fatalf("the pod behind changed its route or passed the head: %+v", behind.Pod)
		}
	}
}

// TestFaultServiceClaim checks the claim surrender at fault start (section
// 1.6 of the incident suspension contract). An empty relocating pod gives
// up its unused claim on a remote berth, and keeps its route, destination
// and grants. After the clear, admission takes the berth again. A pod
// whose destination is in its stopping grant keeps the claim, and so does
// an occupied pod.
func TestFaultServiceClaim(t *testing.T) {
	t.Parallel()
	market := berthResources(Berth{ID: "market-1", Node: "market-berth"})
	t.Run("remote claim", func(t *testing.T) {
		t.Parallel()
		s, relocating, _ := faultFixture(t)
		checkFaultsEachTick(t, s)
		route, destination, through := relocating.Route, relocating.destination, relocating.reservedThrough
		owned := maps.Clone(s.owners)
		id := startFault(t, s, relocating, 0)
		for _, r := range market {
			if !owned[r].isPod("01") || !s.owners[r].isZero() {
				t.Fatalf("%v: owner %v before, %v after the fault", r, owned[r], s.owners[r])
			}
			delete(owned, r)
		}
		if !maps.Equal(owned, s.owners) || !sameRouteSlice(relocating.Route, route) || relocating.destination != destination ||
			relocating.reservedThrough != through || relocating.RelocatingTo != "market" {
			t.Fatal("the fault changed more than the service claim")
		}
		if s.ExportState().Pods[s.vehicleIndex(relocating)].ClaimsDestination {
			t.Fatal("the saved pod claims its destination")
		}
		if err := s.clearFault(id); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "arrival at Market", func() bool { return relocating.Pod.Activity == Idle })
		if relocating.Pod.BerthID != "market-1" {
			t.Fatalf("the pod is idle at %q", relocating.Pod.BerthID)
		}
	})
	t.Run("stopping grant", func(t *testing.T) {
		t.Parallel()
		s, relocating, _ := faultFixture(t)
		stepUntil(t, s, "admitted destination", func() bool { return s.relocationDestinationAdmitted(relocating) })
		if relocating.Pod.Activity != Traveling {
			t.Fatal("the pod arrived")
		}
		startFault(t, s, relocating, 0)
		for _, r := range market {
			if !s.owners[r].isPod("01") {
				t.Fatalf("the pod gave up %v of its stopping grant", r)
			}
		}
		checkNow(t, s)
	})
	t.Run("occupied", func(t *testing.T) {
		t.Parallel()
		s, relocating, _ := faultFixture(t)
		relocating.Pod.Occupied = true
		startFault(t, s, relocating, 0)
		for _, r := range market {
			if !s.owners[r].isPod("01") {
				t.Fatalf("the occupied pod gave up %v", r)
			}
		}
	})
}

// TestFaultPickupRelease checks the pickup release at fault start: a bound
// pickup, an active hold and a stale deferral each get the result of the
// first service hold of stage 1. The exclusion holds until the trip
// boards: the faulted pod, also in service again after the clear, never
// gets the trip, and another pod carries it.
func TestFaultPickupRelease(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		build func(t *testing.T) (*Simulation, *vehicle)
	}{
		{"bound pickup", func(t *testing.T) (*Simulation, *vehicle) {
			t.Helper()
			s := newPickupFleet(t, "garden", "parking")
			trip := newTrip(s, "harbor", "market")
			trip.request.PodID = "01"
			s.waiting = []waitingTrip{trip}
			v := s.findVehicle("01")
			return s, v
		}},
		{"active hold", func(t *testing.T) (*Simulation, *vehicle) {
			t.Helper()
			s := newPickupFleet(t, "harbor", "market")
			advance(s, TicksPerSecond)
			v := s.findVehicle("02")
			unloadFor(v, TicksPerSecond)
			trip := newTrip(s, "market", "harbor")
			trip.deferUntil, trip.deferCheck, trip.deferPodID = s.tick+maxDispatchDeferral, s.tick+TicksPerSecond, "02"
			trip.request.DispatchReason = "Waiting for pod 02 to finish"
			s.waiting = []waitingTrip{trip}
			return s, v
		}},
		{"stale deferral", func(t *testing.T) (*Simulation, *vehicle) {
			t.Helper()
			s := newPickupFleet(t, "harbor", "market", "garden")
			advance(s, TicksPerSecond)
			trip := newTrip(s, "market", "garden")
			trip.request.DispatchReason = "Waiting for an available pod"
			trip.deferUntil, trip.deferCheck, trip.deferPodID = s.tick, s.tick, "02"
			s.waiting = []waitingTrip{trip}
			v := s.findVehicle("02")
			return s, v
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := test.build(t)
			s.faultsOn = true
			withdrawn := s.Clone()
			if err := withdrawn.withdrawService(withdrawn.findVehicle(v.Pod.ID), faultHold); err != nil {
				t.Fatal(err)
			}
			id := startFault(t, s, v, 0)
			if !reflect.DeepEqual(s.waiting, withdrawn.waiting) {
				t.Fatalf("queue after the fault %+v, want %+v", s.waiting, withdrawn.waiting)
			}
			excluded := s.waiting[0].excludedPod == v.Pod.ID
			if excluded == (test.name == "stale deferral") {
				t.Fatalf("exclusion %q", s.waiting[0].excludedPod)
			}
			if !excluded {
				return
			}
			trip := s.waiting[0].request.ID
			s.monitor = func(s *Simulation) {
				if err := checkExclusions(s); err != nil {
					t.Fatal(err)
				}
				for _, waiting := range s.waiting {
					if waiting.request.ID == trip && (waiting.excludedPod != v.Pod.ID || waiting.request.PodID == v.Pod.ID) {
						t.Fatalf("tick %d: the trip %+v", s.tick, waiting)
					}
				}
			}
			for range 5 * TicksPerSecond {
				s.Step()
			}
			if err := s.clearFault(id); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, s, "the trip boards", func() bool {
				return !slices.ContainsFunc(s.waiting, func(waiting waitingTrip) bool { return waiting.request.ID == trip })
			})
			if slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID == trip }) {
				t.Fatalf("the faulted pod %s carries the trip", v.Pod.ID)
			}
		})
	}
}

// TestFaultReusesRecoveryHold faults a pod that already has the fault hold
// of its fault recovery. The hold stays, and no second pickup release
// changes the queue.
func TestFaultReusesRecoveryHold(t *testing.T) {
	t.Parallel()
	s := newPickupFleet(t, "garden", "parking")
	s.faultsOn = true
	trip := newTrip(s, "harbor", "market")
	trip.request.PodID = "01"
	s.waiting = []waitingTrip{trip}
	v := s.findVehicle("01")
	if err := s.withdrawService(v, faultHold); err != nil {
		t.Fatal(err)
	}
	// A second release would clear this stale deferral metadata.
	s.waiting[0].deferUntil, s.waiting[0].deferCheck, s.waiting[0].deferPodID = s.tick, s.tick, "01"
	queue := slices.Clone(s.waiting)
	released := v.released
	startFault(t, s, v, 0)
	if v.withdrawn != faultHold || v.released != released || !reflect.DeepEqual(s.waiting, queue) {
		t.Fatalf("withdrawn %d, queue %+v, want %+v", v.withdrawn, s.waiting, queue)
	}
}

// TestFaultLinkGates checks the gates that keep a faulted pod out of each
// group, each with a control that forms the group without the fault.
// Platoon links refuse a faulted leader or follower and a run with a
// blocked lane. A faulted pod makes no buffer head grant.
func TestFaultLinkGates(t *testing.T) {
	t.Parallel()
	t.Run("platoon link", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			name  string
			cause func(s *Simulation, leader, follower *vehicle)
		}{
			{"control", func(*Simulation, *vehicle, *vehicle) {}},
			{"faulted leader", func(_ *Simulation, leader, _ *vehicle) { leader.faulted = true }},
			{"faulted follower", func(_ *Simulation, _, follower *vehicle) { follower.faulted = true }},
			{"blocked run", func(s *Simulation, _, follower *vehicle) {
				s.setBlocked([]faultFootprint{{id: "i0.1", resources: []resource{{kind: trackResource, id: follower.Pod.LaneID}}}})
			}},
		} {
			route := []string{"main", "exit", "approach"}
			s := restoreCorridor(t, mergeCorridor(false, 0), []corridorPod{
				{route: route, station: "dest", distance: 2000},
				{route: route, station: "dest", distance: 2000 - corridorQueueGap},
			})
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			leader, follower := &s.vehicles[0], &s.vehicles[1]
			test.cause(s, leader, follower)
			s.formPlatoons()
			if linked := follower.link.leader != 0; linked != (test.name == "control") {
				t.Fatalf("%s: linked %t", test.name, linked)
			}
			if test.name == "faulted follower" && s.canLink(follower) {
				t.Fatal("a faulted pod can link")
			}
		}
	})
	t.Run("buffer head", func(t *testing.T) {
		t.Parallel()
		s, release := bufferQueue(t)
		release()
		head := s.findVehicle("02")
		plan, ok := s.bufferPlan(head)
		if !ok {
			t.Fatal("the head has no buffer plan")
		}
		in := intent{index: s.vehicleIndex(head), block: head.reservedThrough + 1, id: head.Pod.ID}
		faulted := s.Clone()
		faulted.vehicles[in.index].faulted = true
		before := faulted.Clone()
		faulted.grantBufferedHead(in, plan)
		if !sameState(before, faulted) {
			t.Fatal("a faulted head made a berth-grant attempt")
		}
		s.grantBufferedHead(in, plan)
		if head.destination.ID != "market-1" {
			t.Fatalf("control: the head has the berth %q", head.destination.ID)
		}
	})
}

// faultRestoreInput returns the input that restores the saved state of s
// with the fault marker and the fault settings of s.
func faultRestoreInput(s *Simulation, state SavedState) RestoreStateInput {
	return RestoreStateInput{
		Network: s.network, Fleet: s.initial, State: state, IncidentContract: s.incidentContract,
		FaultContract: FaultV1Contract, Faults: FaultSettings{EvacuationSeconds: int(s.faultSettings.evacuationSeconds)},
	}
}

// physicalSave saves s, restores the save in the physical tier, and checks
// the state contract on both sides (section 16.5 of the incident
// suspension contract). The restore keeps the records, the counters and
// the fault settings, and turns the fault operations on. Each faulted
// pod is faulted again, at rest, with its cap at its distance, and the
// blocked set is built again from the records, which the state contract
// checks (F10). The reroute pass is due. The restored state saves the
// same state. The restored simulation returns the restored pods with the
// fault hold and no record to service in its next fault stage (F12).
func physicalSave(t *testing.T, s *Simulation, at string) *Simulation {
	t.Helper()
	if err := s.CheckContract(); err != nil {
		t.Fatalf("%s: %v", at, err)
	}
	state := s.ExportState()
	restored, result, err := RestoreState(faultRestoreInput(s, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("%s: restore %v, %+v", at, err, result)
	}
	if err := restored.CheckContract(); err != nil {
		t.Fatalf("%s: restored: %v", at, err)
	}
	if !reflect.DeepEqual(restored.ExportState(), state) {
		t.Fatalf("%s: the restored state saves another state", at)
	}
	if !restored.faultsOn || restored.faultSettings != s.faultSettings || !slices.Equal(restored.faults, s.faults) ||
		restored.faultCounters != s.faultCounters || restored.faultContract != FaultV1Contract {
		t.Fatalf("%s: the restore lost the records, the counters or the settings", at)
	}
	if len(s.faults) > 0 && !restored.rerouteDue {
		t.Fatalf("%s: no reroute pass is due", at)
	}
	for index := range restored.vehicles {
		v, live := &restored.vehicles[index], &s.vehicles[index]
		if v.faulted != live.faulted || v.faulted && (v.Pod.Speed != 0 || v.Pod.Activity == Traveling && v.faultCap != v.distance) {
			t.Fatalf("%s: pod %s restores faulted %t at speed %g with the cap %g at %g", at, v.Pod.ID, v.faulted, v.Pod.Speed, v.faultCap, v.distance)
		}
	}
	held := restored.Clone()
	held.Step()
	for index := range held.vehicles {
		if v := &held.vehicles[index]; v.withdrawn&faultHold != 0 && !v.faulted && v.op.owner != faultHold {
			t.Fatalf("%s: pod %s keeps the fault hold after the fault stage", at, v.Pod.ID)
		}
	}
	return restored
}

// TestFaultPhysicalSaves saves at the command boundaries and tick ends of
// section 16.5 of the incident suspension contract: after a fault on a
// moving pod and on a pod at a berth, at the end of the tick in which the
// faulted pod reaches rest, and after a clear during braking. See
// physicalSave. A braking fault restores as stopped. After the clear of a
// restored fault on the moving pod, the pod continues its route.
func TestFaultPhysicalSaves(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	id := startFault(t, s, v, 0)
	braking := physicalSave(t, s, "fault on a moving pod")
	if err := braking.clearFault(id); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, braking, "the restored pod at s2", func() bool { return braking.findVehicle("01").Pod.Activity == Unloading })
	for range 10 {
		s.Step()
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	physicalSave(t, s, "clear during braking")
	cruiseOn(t, s, v, "s1-link")
	startFault(t, s, v, 0)
	for v.Pod.Speed > 0 {
		s.Step()
	}
	physicalSave(t, s, "end of the tick of rest")
	parked := s.findVehicle("02")
	startFault(t, s, parked, 0)
	physicalSave(t, s, "fault at a berth")
}

// TestFaultCheckpointBraking replays 600 ticks from a checkpoint taken
// while a faulted pod brakes, through its rest and a clear.
func TestFaultCheckpointBraking(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2", "s2")
	cruiseOn(t, s, v, "s0-link")
	id := startFault(t, s, v, 0)
	checkpoint := s.Clone()
	run := func(s *Simulation) {
		for tick := range 600 {
			if tick == 300 {
				if err := s.clearFault(id); err != nil {
					t.Fatal(err)
				}
			}
			s.Step()
		}
	}
	run(s)
	run(checkpoint)
	if !sameState(checkpoint, s) {
		t.Fatal("the replay differs from the source")
	}
}

// TestFaultDepartureGate faults a boarding pod after admission granted its
// departure, as a fault between admission and motion would. The pod does
// not depart in the motion stage. A control without the fault departs.
func TestFaultDepartureGate(t *testing.T) {
	t.Parallel()
	for _, faulted := range []bool{false, true} {
		s := faultLegFleet(t)
		v := boardParties(t, s, "s1")
		v.phaseTicks = 0
		s.admit()
		if v.reservedThrough < 0 {
			t.Fatal("the boarding pod has no grant")
		}
		if faulted {
			startFault(t, s, v, 0)
		}
		s.Step()
		if departed := v.Pod.Activity == Traveling; departed == faulted {
			t.Fatalf("faulted %t: activity %s", faulted, v.Pod.Activity)
		}
	}
}
