package sim

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"testing"
)

// platoonMonitor checks the platoon rules after each step. It keeps the
// owners of the step before, so that it can check each change of owner.
type platoonMonitor struct {
	s      *Simulation
	owners map[resource]string
	// coupled holds each pod that was in a platoon, and largest is the
	// largest platoon.
	coupled map[string]bool
	largest int
	// ownerTicks is the number of ticks between two checks of the owners
	// against the full retention scan. The scan is the slowest check, so
	// the long corridor runs make it less often. Each other check runs at
	// each tick.
	ownerTicks int64
	// speeds holds the speed of each traveling pod after the step before,
	// and wasCoupled holds each pod that was coupled then.
	speeds     map[string]float64
	wasCoupled map[string]bool
	// largestTurn is the largest turn of a link.
	largestTurn float64
}

func newPlatoonMonitor(s *Simulation) *platoonMonitor {
	return &platoonMonitor{s: s, owners: maps.Clone(s.owners), coupled: make(map[string]bool), ownerTicks: 1, speeds: make(map[string]float64)}
}

// maxSpeedStep is the largest change of speed in one tick of a coupled pod.
// A pod brakes at most at acceleration.
const maxSpeedStep = acceleration/TicksPerSecond + 1e-9

// check verifies the owners against the retention rules, the links, the
// control rule of each link, and each change of owner. A track or junction
// resource can go from one pod to another in one step only when the second
// pod is behind the first in its platoon.
func (m *platoonMonitor) check(t *testing.T) {
	t.Helper()
	s := m.s
	if s.tick%m.ownerTicks == 0 {
		checkIncrementalOwners(t, s)
	}
	links := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.follower != 0 && s.vehicles[v.follower-1].link.leader != i+1 {
			t.Fatalf("tick %d: pod %s names a follower that does not name it", s.tick, v.Pod.ID)
		}
		if v.link.leader == 0 {
			if v.follower != 0 {
				m.largest = max(m.largest, platoonLength(s, i))
			}
			continue
		}
		links++
		leader := &s.vehicles[v.link.leader-1]
		m.coupled[v.Pod.ID], m.coupled[leader.Pod.ID] = true, true
		if leader.follower != i+1 {
			t.Fatalf("tick %d: pod %s names a predecessor that does not name it", s.tick, v.Pod.ID)
		}
		for index := v.link.lane; index < v.link.lane+v.link.lanes; index++ {
			if role := v.Route[index].StationRole; role == StationEntryRole || role == StationBerthAccessRole {
				t.Fatalf("tick %d: the link of pod %s holds station lane %s", s.tick, v.Pod.ID, v.Route[index].ID)
			}
		}
		if v.Pod.Activity != Traveling || leader.Pod.Activity != Traveling {
			if s.holdsPending(v) {
				t.Fatalf("tick %d: pod %s holds a resource of a pod that stopped traveling", s.tick, v.Pod.ID)
			}
			continue
		}
		if s.holdsPending(v) && v.reservedThrough >= v.blocks.laneFirst(v.link.lane) &&
			(v.reservedThrough > v.link.end || leaderBlock(v, leader, v.link, v.reservedThrough) > leader.reservedThrough) {
			t.Fatalf("tick %d: pod %s holds a cell of a pod ahead and reserved block %d past its predecessor %s",
				s.tick, v.Pod.ID, v.reservedThrough, leader.Pod.ID)
		}
		m.checkCertificate(t, v, leader)
		position := leaderPosition(v, leader, v.link)
		if gap := position - v.distance; gap < v.link.clearance-1e-6 {
			t.Fatalf("tick %d: pod %s is %.6f m behind pod %s, want at least %.6f m", s.tick, v.Pod.ID, gap, leader.Pod.ID, v.link.clearance)
		}
		if excess := v.distance + stoppingDistance(v.Pod.Speed) + v.link.clearance - position - stoppingDistance(leader.Pod.Speed); excess > 1e-6 {
			t.Fatalf("tick %d: the stop point of pod %s is %.6f m past its control limit", s.tick, v.Pod.ID, excess)
		}
	}
	checkNoOvertake(t, s)
	m.checkSpeeds(t)
	if links != s.platoonLinks {
		t.Fatalf("tick %d: %d links, platoonLinks is %d", s.tick, links, s.platoonLinks)
	}
	for r, owner := range s.owners {
		before := m.owners[r]
		if before == "" || before == owner || r.kind != trackResource && r.kind != junctionResource {
			continue
		}
		if v := s.findVehicle(owner); v == nil || !s.aheadInPlatoon(v, before) {
			t.Fatalf("tick %d: %v went from pod %s to pod %s", s.tick, r, before, owner)
		}
	}
	m.owners = maps.Clone(s.owners)
}

// checkCertificate checks the run of the link of the traveling follower v.
// The link has the turn of its platoon and the clearance of that turn. The
// total turn from the lane of v to the end of the run is within the turn.
// Each pod that owns a resource that v holds is ahead of v in its platoon,
// and it is on the run of v before the end of the run.
func (m *platoonMonitor) checkCertificate(t *testing.T, v, leader *vehicle) {
	t.Helper()
	s, link := m.s, &v.link
	m.largestTurn = max(m.largestTurn, link.turn)
	if leader.link.leader != 0 && leader.link.turn != link.turn || link.clearance != linkClearance(link.turn) || link.turn > platoonMaxTurn {
		t.Fatalf("tick %d: pod %s has turn %v and clearance %v, its predecessor turn %v", s.tick, v.Pod.ID, link.turn, link.clearance, leader.link.turn)
	}
	current, last := v.blocks.routeLane(v.blockIndex), link.lane+link.lanes-1
	if current <= last {
		if turn := s.runTurn(v.Route, current, last); turn > link.turn+platoonTurnSlack {
			t.Fatalf("tick %d: the run of pod %s turns %v from its lane, more than %v", s.tick, v.Pod.ID, turn, link.turn)
		}
	}
	geometry, _ := linkEnds(&v.blocks, *link)
	for r := range v.routeReleases {
		owner := s.owners[r]
		if owner == v.Pod.ID {
			continue
		}
		if !s.aheadInPlatoon(v, owner) {
			t.Fatalf("tick %d: pod %s holds %v of pod %s, which is not ahead of it in its platoon", s.tick, v.Pod.ID, r, owner)
		}
		o := s.findVehicle(owner)
		lane := o.blocks.routeLane(o.blockIndex)
		position := math.Inf(1)
		for index := max(current, link.lane); index <= last; index++ {
			if v.Route[index].ID == o.Route[lane].ID {
				position = v.blocks.lanes[index].start + o.distance - o.blocks.lanes[lane].start
				break
			}
		}
		if position < v.distance || position >= geometry {
			t.Fatalf("tick %d: pod %s holds %v of pod %s, which is at %v on its run, outside %v to %v", s.tick, v.Pod.ID, r, owner, position, v.distance, geometry)
		}
	}
}

// checkSpeeds checks that no traveling pod that is coupled, or was coupled
// at the step before, changes its speed by more than maxSpeedStep in one
// tick.
func (m *platoonMonitor) checkSpeeds(t *testing.T) {
	t.Helper()
	speeds := make(map[string]float64, len(m.speeds))
	wasCoupled := m.wasCoupled
	m.wasCoupled = make(map[string]bool, len(wasCoupled))
	for i := range m.s.vehicles {
		v := &m.s.vehicles[i]
		if v.Pod.Activity != Traveling {
			continue
		}
		before, ok := m.speeds[v.Pod.ID]
		if ok && (v.coupled() || wasCoupled[v.Pod.ID]) && math.Abs(v.Pod.Speed-before) > maxSpeedStep {
			t.Fatalf("tick %d: coupled pod %s went from %v m/s to %v m/s", m.s.tick, v.Pod.ID, before, v.Pod.Speed)
		}
		speeds[v.Pod.ID] = v.Pod.Speed
		if v.coupled() {
			m.wasCoupled[v.Pod.ID] = true
		}
	}
	m.speeds = speeds
}

// checkNoOvertake checks that no pod owns a track cell of its lane after
// the cell of the next pod ahead of it on that lane.
func checkNoOvertake(t *testing.T, s *Simulation) {
	t.Helper()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Traveling {
			continue
		}
		var ahead *vehicle
		for j := range s.vehicles {
			q := &s.vehicles[j]
			if q.Pod.Activity == Traveling && q.Pod.LaneID == v.Pod.LaneID && q.Pod.LaneDistance > v.Pod.LaneDistance &&
				(ahead == nil || q.Pod.LaneDistance < ahead.Pod.LaneDistance) {
				ahead = q
			}
		}
		if ahead == nil {
			continue
		}
		cell := ahead.blocks.at(ahead.blockIndex).cell
		for r := range v.routeReleases {
			if r.kind == trackResource && r.id == v.Pod.LaneID && r.cell > cell && s.owners[r] == v.Pod.ID {
				t.Fatalf("tick %d: pod %s owns %v after the cell %d of pod %s ahead of it", s.tick, v.Pod.ID, r, cell, ahead.Pod.ID)
			}
		}
	}
}

// platoonLength returns the number of pods in the platoon that starts at the
// pod at index head.
func platoonLength(s *Simulation, head int) int {
	length := 1
	for index := s.vehicles[head].follower; index != 0; index = s.vehicles[index-1].follower {
		length++
		if length > MaxPlatoonLimit+1 {
			break
		}
	}
	return length
}

func TestPlatoonSettings(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if s.Platooning() != PlatooningOff || s.PlatoonLimit() != MaxPlatoonLimit {
		t.Fatalf("new simulation: mode %d, limit %d", s.Platooning(), s.PlatoonLimit())
	}
	for _, mode := range []Platooning{PlatooningOff - 1, PlatooningVirtual + 1} {
		if err := s.SetPlatooning(mode); err == nil {
			t.Errorf("mode %d is accepted", mode)
		}
	}
	for _, limit := range []int{MinPlatoonLimit - 1, MaxPlatoonLimit + 1} {
		if err := s.SetPlatoonLimit(limit); err == nil {
			t.Errorf("limit %d is accepted", limit)
		}
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(MinPlatoonLimit); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	if s.Platooning() != PlatooningVirtual || s.PlatoonLimit() != MinPlatoonLimit {
		t.Fatalf("after reset: mode %d, limit %d", s.Platooning(), s.PlatoonLimit())
	}
}

// TestPlatoonCorridorLinks runs a stopped queue on each feed lane of the
// merge corridor with virtual platoons. The monitor checks the rules at each
// tick. Each queue must couple into platoons of the limit.
func TestPlatoonCorridorLinks(t *testing.T) {
	t.Parallel()
	feedHead := corridorFeedLength - 100
	for _, limit := range []int{0, 2, 4} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			t.Parallel()
			got := runMergeCorridor(t, corridorCase{
				side: true, sideDegrees: 30, streams: []string{"main", "side"}, queueHead: feedHead, platoonLimit: limit,
			})
			t.Logf("headway %.3f s, coupled %d, largest %d", got.headway, got.coupled, got.largest)
			wantCoupled := 2 * corridorStreamPods
			if limit == 0 {
				wantCoupled = 0
			}
			if got.coupled != wantCoupled || got.largest != limit {
				t.Fatalf("coupled %d pods in platoons of up to %d, want %d in platoons of %d", got.coupled, got.largest, wantCoupled, limit)
			}
		})
	}
}

// restoreCorridor restores pods at rest on the merge corridor. Each pod has
// a route of three lanes from its feed lane to the destination station.
func restoreCorridor(t *testing.T, network Network, pods []corridorPod) *Simulation {
	t.Helper()
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index, pod := range pods {
		id := fmt.Sprintf("p%02d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "dest", BerthID: fmt.Sprintf("dest-%02d", index+1)})
		state.RequestID++
		state.Boarded++
		route := make([]int, len(pod.route))
		for position, lane := range pod.route {
			route[position] = laneIndex[lane]
		}
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Activity: "traveling", Occupied: true, Origin: "origin-1", DestinationStation: pod.station,
			Riders: []SavedRequest{{ID: state.RequestID, From: "origin", To: pod.station, PartySize: 1, PodID: id}},
			Stops:  []string{pod.station},
			Route:  route, LaneID: pod.route[0], LaneDistance: pod.distance, Distance: pod.distance,
		})
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the pods did not restore in place: %+v", result)
	}
	return s
}

// corridorPod is a pod at rest for restoreCorridor.
type corridorPod struct {
	route    []string
	station  string
	distance float64
}

// forkNetwork returns the merge corridor with a second destination station.
// A branch lane leaves the end of the exit lane at 30 degrees and ends at
// the entry of that station. branchLimit is the speed limit of the branch.
func forkNetwork(branchLimit float64) Network {
	network := mergeCorridor(false, 0)
	radians := math.Pi / 6
	entry := Point{X: corridorExitLength + 2000*math.Cos(radians), Y: -2000 * math.Sin(radians)}
	network.Nodes = append(network.Nodes,
		Node{ID: "fork-entry", Position: entry},
		Node{ID: "fork-berth", Position: Point{X: entry.X + 100, Y: entry.Y - 60}},
		Node{ID: "fork-exit", Position: Point{X: entry.X + 200, Y: entry.Y}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "branch", From: "exit-end", To: "fork-entry", SpeedLimit: branchLimit},
		Lane{ID: "fork-through", From: "fork-entry", To: "fork-exit", SpeedLimit: branchLimit},
		Lane{ID: "fork-in", From: "fork-entry", To: "fork-berth", SpeedLimit: branchLimit},
		Lane{ID: "fork-out", From: "fork-berth", To: "fork-exit", SpeedLimit: branchLimit})
	network.Stations = append(network.Stations, Station{
		ID: "fork", Name: "Fork", Entry: "fork-entry", Exit: "fork-exit", Berths: []Berth{{ID: "fork-1", Node: "fork-berth"}},
	})
	return network
}

// TestPlatoonForkRules checks where links form and where they end. Two pods
// wait on the main lane. The follower couples when both routes share the
// next lane and have one speed limit. The link ends before the fork where
// the routes leave each other.
func TestPlatoonForkRules(t *testing.T) {
	t.Parallel()
	head := corridorFeedLength - 100
	toDest := []string{"main", "exit", "approach"}
	toFork := []string{"main", "exit", "branch"}
	tests := []struct {
		name        string
		branchLimit float64
		follower    []string
		station     string
		mode        Platooning
		wantLink    bool
	}{
		{name: "same route", branchLimit: 14, follower: toDest, station: "dest", mode: PlatooningVirtual, wantLink: true},
		{name: "routes fork after the shared lanes", branchLimit: 14, follower: toFork, station: "fork", mode: PlatooningVirtual, wantLink: true},
		{name: "a second speed limit", branchLimit: 10, follower: toFork, station: "fork", mode: PlatooningVirtual},
		{name: "platooning off", branchLimit: 14, follower: toDest, station: "dest", mode: PlatooningOff},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := restoreCorridor(t, forkNetwork(test.branchLimit), []corridorPod{
				{route: toDest, station: "dest", distance: head},
				{route: test.follower, station: test.station, distance: head - corridorQueueGap},
			})
			if err := s.SetPlatooning(test.mode); err != nil {
				t.Fatal(err)
			}
			monitor := newPlatoonMonitor(s)
			linked, released := false, false
			for range 900 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				monitor.check(t)
				follower := &s.vehicles[1]
				if follower.link.leader != 0 {
					linked = true
					if follower.Pod.LaneID == "branch" && s.holdsPending(follower) {
						t.Fatalf("tick %d: the follower holds a cell of its predecessor on the branch", s.tick)
					}
				} else if linked {
					released = true
				}
				if s.completed == 2 {
					break
				}
			}
			if s.completed != 2 {
				t.Fatalf("%d of 2 journeys completed", s.completed)
			}
			if linked != test.wantLink || linked && !released {
				t.Fatalf("linked %v, released %v, want link %v", linked, released, test.wantLink)
			}
		})
	}
}

// TestPlatoonCruisingPodsDoNotCouple turns platooning on while two pods run
// at the lane speed limit. No link forms, because neither pod is slow.
func TestPlatoonCruisingPodsDoNotCouple(t *testing.T) {
	t.Parallel()
	route := []string{"main", "exit", "approach"}
	s := restoreCorridor(t, mergeCorridor(false, 0), []corridorPod{
		{route: route, station: "dest", distance: 1000},
		{route: route, station: "dest", distance: 800},
	})
	for range 20 * TicksPerSecond {
		s.Step()
	}
	if s.vehicles[0].Pod.Speed < 14 || s.vehicles[1].Pod.Speed < 14 {
		t.Fatalf("pods did not reach the lane speed limit: %v, %v", s.vehicles[0].Pod.Speed, s.vehicles[1].Pod.Speed)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	for range 60 * TicksPerSecond {
		s.Step()
		if s.platoonLinks != 0 {
			t.Fatalf("tick %d: cruising pods coupled", s.tick)
		}
	}
}

// TestPlatoonCoupledPodsCannotDivert checks that divertStart refuses each
// pod in a platoon, the predecessor too. The third pod stays out of the
// platoon, because the platoon limit is 2.
func TestPlatoonCoupledPodsCannotDivert(t *testing.T) {
	t.Parallel()
	route := []string{"main", "exit", "approach"}
	s := restoreCorridor(t, mergeCorridor(false, 0), []corridorPod{
		{route: route, station: "dest", distance: 2000},
		{route: route, station: "dest", distance: 2000 - corridorQueueGap},
		{route: route, station: "dest", distance: 2000 - 2*corridorQueueGap},
	})
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if s.vehicles[1].link.leader != 1 {
		t.Fatal("the second pod did not couple to the first")
	}
	for index, want := range []bool{false, false, true} {
		if _, _, ok := s.divertStart(&s.vehicles[index]); ok != want {
			t.Errorf("pod %s: divertStart reports %v, want %v", s.vehicles[index].Pod.ID, ok, want)
		}
	}
}

// TestPlatoonOffEndsLinks turns platooning off while a queue runs in
// platoons. No new link forms, and the links end without a fault while the
// queue moves.
func TestPlatoonOffEndsLinks(t *testing.T) {
	t.Parallel()
	s := restoreCorridor(t, mergeCorridor(false, 0), corridorQueues(corridorFeedLength - 100)[:corridorStreamPods])
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	monitor := newPlatoonMonitor(s)
	links := 0
	for tick := range 90 * TicksPerSecond {
		if tick == 10*TicksPerSecond {
			if s.platoonLinks == 0 {
				t.Fatal("no links formed")
			}
			if err := s.SetPlatooning(PlatooningOff); err != nil {
				t.Fatal(err)
			}
			links = s.platoonLinks
		}
		s.Step()
		checkTraffic(t, s.Snapshot())
		monitor.check(t)
		if tick >= 10*TicksPerSecond {
			if s.platoonLinks > links {
				t.Fatalf("tick %d: a link formed after platooning stopped", s.tick)
			}
			links = s.platoonLinks
		}
	}
	if s.platoonLinks != 0 {
		t.Fatalf("%d links 80 s after platooning stopped", s.platoonLinks)
	}
}

// corridorQueues returns a stopped queue of corridorStreamPods pods on each
// feed lane of the merge corridor, with the first pod of each queue at
// head.
func corridorQueues(head float64) []corridorPod {
	var pods []corridorPod
	for _, feed := range []string{"main", "side"} {
		for position := range corridorStreamPods {
			pods = append(pods, corridorPod{
				route: []string{feed, "exit", "approach"}, station: "dest", distance: head - corridorQueueGap*float64(position),
			})
		}
	}
	return pods
}

// platoonState returns the snapshot of s as JSON with the links of its pods.
func platoonState(t *testing.T, s *Simulation) string {
	t.Helper()
	snapshot, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	links := make([]string, len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		links[index] = fmt.Sprintf("%+v %d %v", v.link, v.follower, v.platoonCap)
	}
	return fmt.Sprintf("%s %v %v", snapshot, links, s.owners)
}

// TestPlatoonCloneAndDeterminism checks that a clone of a simulation with
// platoons evolves like its source, and that two runs give the same result.
func TestPlatoonCloneAndDeterminism(t *testing.T) {
	t.Parallel()
	run := func() *Simulation {
		s := restoreCorridor(t, mergeCorridor(true, 30), corridorQueues(corridorFeedLength-100))
		if err := s.SetPlatooning(PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
		return s
	}
	first, second := run(), run()
	for range 20 * TicksPerSecond {
		first.Step()
		second.Step()
	}
	clone := first.Clone()
	if !reflect.DeepEqual(clone.vehicles[1].link, first.vehicles[1].link) || clone.platoonLinks != first.platoonLinks {
		t.Fatal("the clone has other links")
	}
	for range 120 * TicksPerSecond {
		first.Step()
		second.Step()
		clone.Step()
	}
	want := platoonState(t, first)
	if platoonState(t, second) != want {
		t.Fatal("two runs differ")
	}
	if platoonState(t, clone) != want {
		t.Fatal("the clone differs from its source")
	}
}

// TestPlatoonBraking couples a stopped follower to a predecessor that
// moves at 6 m/s. Then the predecessor runs at the speed limit and stops
// at a cell that a pod outside the fleet holds, so it brakes as hard as it
// can. The monitor checks at each tick that no coupled pod changes its
// speed by more than one braking step, and that the follower keeps the
// clearance to the predecessor and to its stop point.
func TestPlatoonBraking(t *testing.T) {
	t.Parallel()
	route := []string{"main", "exit", "approach"}
	s := restoreCorridor(t, mergeCorridor(false, 0), []corridorPod{
		{route: route, station: "dest", distance: 380},
		{route: route, station: "dest", distance: 328},
	})
	leader, follower := &s.vehicles[0], &s.vehicles[1]
	// The main lane has cells of 30 m. Cell 11 holds the follower until
	// the link forms, and cell 40 stops the predecessor at 1,200 m.
	wait, stop := resource{kind: trackResource, id: "main", cell: 11}, resource{kind: trackResource, id: "main", cell: 40}
	s.owners[wait], s.owners[stop] = "blocker", "blocker"
	monitor := newPlatoonMonitor(s)
	// The pod outside the fleet is not in the retention rules.
	monitor.ownerTicks = math.MaxInt64
	braking := 0.0
	for tick := range 90 * TicksPerSecond {
		if tick == 3*TicksPerSecond {
			if follower.Pod.Speed != 0 || leader.Pod.Speed < 5 {
				t.Fatalf("speeds %v and %v before the link, want 0 and at least 5 m/s", follower.Pod.Speed, leader.Pod.Speed)
			}
			delete(s.owners, wait)
			delete(monitor.owners, wait)
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
		}
		speed := leader.Pod.Speed
		s.Step()
		braking = max(braking, speed-leader.Pod.Speed)
		checkTraffic(t, s.Snapshot())
		monitor.check(t)
		if tick == 3*TicksPerSecond && follower.link.leader != 1 {
			t.Fatal("the follower did not couple")
		}
	}
	if follower.link.leader != 1 || leader.Pod.Speed != 0 || follower.Pod.Speed != 0 || braking < 0.9*acceleration/TicksPerSecond {
		t.Fatalf("link %d, speeds %v and %v, largest braking step %v", follower.link.leader, leader.Pod.Speed, follower.Pod.Speed, braking)
	}
	if gap := leader.distance - follower.distance; gap > follower.link.clearance+1 {
		t.Fatalf("the follower stopped %.3f m behind, want close to the clearance %.3f m", gap, follower.link.clearance)
	}
}

// TestPlatoonLongJunctionRun runs a queue on each feed of the 15 degree
// merge. The junction resource of the merge covers several cells of each
// feed, so a reservation goes through all of them at once. A follower
// shares only a full reservation before the end block of its link. The
// monitor checks the owners at each tick.
func TestPlatoonLongJunctionRun(t *testing.T) {
	t.Parallel()
	var pods []corridorPod
	for _, feed := range []string{"main", "side"} {
		for position := range 6 {
			pods = append(pods, corridorPod{
				route: []string{feed, "exit", "approach"}, station: "dest", distance: corridorFeedLength - 100 - corridorQueueGap*float64(position),
			})
		}
	}
	s := restoreCorridor(t, mergeCorridor(true, 15), pods)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	monitor := newPlatoonMonitor(s)
	for range 120 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		monitor.check(t)
		entered := 0
		for i := range s.vehicles {
			if s.vehicles[i].Pod.LaneID == "exit" || s.vehicles[i].Pod.LaneID == "approach" {
				entered++
			}
		}
		if entered == len(pods) {
			if len(monitor.coupled) == 0 {
				t.Fatal("no pod coupled")
			}
			return
		}
	}
	t.Fatal("the queues did not pass the merge")
}
