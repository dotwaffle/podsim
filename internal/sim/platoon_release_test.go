package sim

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

// largeNodeLanes returns the path of largeNodeNetwork. The first lane is
// 1000 m long and ends at the origin. The second lane, l1, is straight and
// 36 m long, or the curve from the origin with control (0, 100) to
// (120, 100). The third lane starts in line with l1 and curves by 20
// degrees, so a run of l0 and l1 with the turn of those lanes cannot grow
// onto it, and no junction zone covers the node between them. The fourth
// lane goes on in line to the station entry.
func largeNodeLanes(curve bool) (start Point, lanes []geometryLane) {
	heading := 20 * math.Pi / 180
	sign, end := 1.0, Point{X: 36}
	start = Point{X: -1000}
	if curve {
		sign, end, start = -1, Point{X: 120, Y: 100}, Point{Y: -1000}
	}
	control := Point{X: end.X + 50, Y: end.Y}
	third := Point{X: control.X + 50*math.Cos(heading), Y: control.Y + sign*50*math.Sin(heading)}
	first := geometryLane{to: end}
	if curve {
		first.control = &Point{Y: 100}
	}
	return start, []geometryLane{
		{to: Point{}}, first, {to: third, control: &control},
		{to: Point{X: third.X + 36*math.Cos(heading), Y: third.Y + sign*36*math.Sin(heading)}},
	}
}

// largeNodeNetwork returns the path of largeNodeLanes as a
// geometryNetwork. The third lane also admits the Express class, so its
// nodes admit large pods, and each lane at those nodes has a tail of at
// least largeClearance (see indexGeometryTails). l1 then holds the node at
// its end in each cell that ends within its tail of the end of the lane.
func largeNodeNetwork(t *testing.T, curve bool, fleet int) Network {
	t.Helper()
	start, lanes := largeNodeLanes(curve)
	network := geometryNetwork(start, lanes, fleet)
	for index := range network.Lanes {
		if network.Lanes[index].ID == "l2" {
			network.Lanes[index].VehicleClasses = largeGeometryClasses(t, "legacy", "express")
		}
	}
	return network
}

// TestPlatoonReleaseBoundedEnd compares the end block of a run of l0 and
// l1 of largeNodeNetwork with the end block of the old rule. The old rule
// shares a cell of l1 that holds the node at the end of l1, and that node
// releases past the end of the run. The new rule ends the run before that
// cell.
func TestPlatoonReleaseBoundedEnd(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		curve bool
		// tail is the tail of l1, cell the cell of l1 that the old rule
		// shares, and end the end block of the new rule as a cell of l0
		// (lane 0) or l1 (lane 1).
		tail            float64
		cell, lane, end int
	}{
		{name: "straight", tail: largeClearance, cell: 0, lane: 0, end: 33},
		{name: "curved", curve: true, tail: 30.89, cell: 4, lane: 1, end: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := restoreGeometry(t, largeNodeNetwork(t, test.curve, 2), 4, 2, 450, corridorQueueGap)
			v := &s.vehicles[1]
			cells := v.blocks.lanes[1].cells
			if math.Abs(cells.tail-test.tail) > 0.01 || cells.fromTail != 0 {
				t.Fatalf("l1 has tail %v and from tail %v, want %v and 0", cells.tail, cells.fromTail, test.tail)
			}
			link := platoonLink{lane: 0, lanes: 2}
			geometry, end := s.linkEnds(v, link)
			legacyGeometry, legacy := legacyLinkEnds(&v.blocks, link)
			if geometry != legacyGeometry || legacy != v.blocks.laneFirst(1)+test.cell || end != v.blocks.laneFirst(test.lane)+test.end {
				t.Fatalf("end block %d of %v m, old rule %d of %v m, want %d and %d", end, geometry, legacy, legacyGeometry,
					v.blocks.laneFirst(test.lane)+test.end, v.blocks.laneFirst(1)+test.cell)
			}
			// The cell that the old rule shares holds the node at the end of
			// l1, and the node releases past the end of the run.
			b := v.blocks.at(legacy)
			node := resource{kind: nodeResource, id: b.lane.To}
			if !slices.Contains(b.resources, node) || resourceReleaseDistance(b, node) <= geometry {
				t.Fatalf("cell %d of l1 has %v, and %v releases at %v, want past %v", b.cell, b.resources, node, resourceReleaseDistance(b, node), geometry)
			}
		})
	}
}

// TestPlatoonReleaseBoundedQueue runs a queue on largeNodeNetwork with
// virtual platoons. Links form on l0, and they form again in the queue
// before the station on l2 and l3, whose cells have tails. The monitor
// checks that no follower holds a resource of a pod ahead whose release
// distance is past the end of its run, and the safety observation must
// pass at each tick.
func TestPlatoonReleaseBoundedQueue(t *testing.T) {
	t.Parallel()
	for _, curve := range []bool{false, true} {
		t.Run(fmt.Sprintf("curve %v", curve), func(t *testing.T) {
			t.Parallel()
			const pods = 8
			s := restoreGeometry(t, largeNodeNetwork(t, curve, pods), 4, pods, 450, corridorQueueGap)
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			monitor := newPlatoonMonitor(s)
			tailed := 0
			for range 240 * TicksPerSecond {
				s.Step()
				monitor.check(t)
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				for i := range s.vehicles {
					if v := &s.vehicles[i]; v.link.leader != 0 && runHasTails(&v.blocks, v.link) {
						tailed++
					}
				}
			}
			if len(monitor.linked) < 4 || tailed == 0 {
				t.Fatalf("linked %d pods, %d ticks of links with tails", len(monitor.linked), tailed)
			}
		})
	}
}

// TestPlatoonRestoreOldRuleLink restores a link that the old rule of
// linkEnds accepted. On the curved l1 of largeNodeNetwork, cell 4 holds
// the node at the end of l1 and no junction. The follower is at rest in
// cell 4, so its reservation is cell 4, and its predecessor in cell 5
// holds cell 4 until 1.09 m past the end of the run. The old end block is
// cell 4, but the new end block is cell 3. linkClaims then refuses the
// claim, and the restore demotes the follower. The predecessor stays in
// place, and no migration changes the saved link.
func TestPlatoonRestoreOldRuleLink(t *testing.T) {
	t.Parallel()
	network := largeNodeNetwork(t, true, 2)
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	ids := []string{"l0", "l1", "l2", "l3"}
	route := make([]int, len(ids))
	for index, id := range ids {
		route[index] = laneIndex[id]
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index, distance := range []float64{165, 140} {
		id := fmt.Sprintf("p%02d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "origin", BerthID: fmt.Sprintf("origin-%02d", index+1)})
		state.RequestID++
		state.Boarded++
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Activity: "traveling", Occupied: true, Origin: fleet[index].BerthID, DestinationStation: "dest",
			Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}},
			Stops:  []string{"dest"},
			Route:  route, RouteIndex: 1, LaneID: "l1", LaneDistance: distance, Distance: 1000 + distance,
		})
	}
	state.Pods[1].Platoon = savedRunLink(network, ids, ids, "p01", 2)
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"p02"}) || result.PhysicalError != nil {
		t.Fatalf("restore %+v, want the physical tier with p02 demoted", result)
	}
	leader := &s.vehicles[0]
	if leader.Pod.Activity != Traveling || leader.Pod.LaneID != "l1" || s.platoonLinks != 0 {
		t.Fatalf("leader %+v, %d links", leader.Pod, s.platoonLinks)
	}
	// The old rule accepted the claim: the reservation of the follower is
	// the old end block, which the leader holds, and the new end block is
	// before it. The demoted follower has no route, so the check uses the
	// route of the leader, which is the same.
	blocks, _ := s.routeBlocks(leader.Route)
	link := platoonLink{lane: 0, lanes: 2}
	cell4 := blocks.laneFirst(1) + 4
	_, legacy := legacyLinkEnds(&blocks, link)
	_, end := s.linkEnds(leader, link)
	if reservationEnd(&blocks, cell4) != cell4 || legacy != cell4 || end != cell4-1 {
		t.Fatalf("reservation %d, old end block %d, new end block %d, want %d, %d and %d", reservationEnd(&blocks, cell4), legacy, end, cell4, cell4, cell4-1)
	}
	if _, held := leader.routeReleases[resource{kind: trackResource, id: "l1", cell: 4}]; !held {
		t.Fatal("the leader does not hold cell 4 of l1")
	}
}

// BenchmarkPlatoonLongLaneStep steps a platoon of five pods whose runs
// start on a lane of 200 km, for 300 ticks from rest. maintainLink reads
// only the end of each run, so the time of a step does not grow with the
// length of the first lane.
func BenchmarkPlatoonLongLaneStep(b *testing.B) {
	network := geometryNetwork(Point{X: -200000}, []geometryLane{{to: Point{}}, {to: Point{X: 36}}}, 5)
	for b.Loop() {
		b.StopTimer()
		s := restoreGeometry(b, network, 2, 5, 199900, corridorQueueGap)
		if err := s.SetPlatooning(PlatooningVirtual); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		linked := 0
		for range 300 {
			s.Step()
			linked += s.platoonLinks
		}
		if linked == 0 {
			b.Fatal("no link formed")
		}
	}
}
