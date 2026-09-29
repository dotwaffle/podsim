package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"strings"
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
	routes := make(map[string][]string, len(pods))
	for index, pod := range pods {
		routes[fmt.Sprintf("p%02d", index+1)] = pod.route
	}
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
		if pod.leader != "" {
			state.Pods[index].Platoon = savedRunLink(network, pod.route, routes[pod.leader], pod.leader, pod.lanes)
		}
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

// corridorPod is a pod at rest for restoreCorridor. leader is the ID of
// its saved platoon predecessor, or empty. lanes is the number of lanes in
// the run of the saved link, or 0 for each lane that both routes share.
type corridorPod struct {
	route    []string
	station  string
	distance float64
	leader   string
	lanes    int
}

// savedRunLink returns the saved link of a pod with route follower to the
// pod leader with route ahead, when both pods are on the first lane of
// their routes. The run holds lanes lanes, or each lane that both routes
// share and continue after when lanes is 0. The turn is the total turn of
// the run.
func savedRunLink(network Network, follower, ahead []string, leader string, lanes int) *SavedPlatoonLink {
	if lanes == 0 {
		for lanes+1 < min(len(follower), len(ahead)) && follower[lanes] == ahead[lanes] {
			lanes++
		}
	}
	var sum turnSum
	for _, id := range follower[:lanes] {
		for _, lane := range network.Lanes {
			if lane.ID == id {
				sum = sum.add(newLaneShape(network.Polyline(lane)))
			}
		}
	}
	return &SavedPlatoonLink{Leader: leader, Lanes: lanes, Turn: sum.turn}
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

// TestPlatoonRestore saves the corridor queues while they run in platoons
// and restores them two times. The restore couples the pods again with the
// saved links, so each pod keeps its place and each link keeps its
// certificate. Without the links, the followers that hold cells of their
// predecessors lose their places.
func TestPlatoonRestore(t *testing.T) {
	t.Parallel()
	network := mergeCorridor(true, 30)
	s := restoreCorridor(t, network, corridorQueues(corridorFeedLength-100))
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	for range 40 * TicksPerSecond {
		s.Step()
	}
	fleet := make([]Placement, len(s.initial))
	copy(fleet, s.initial)
	restored := roundTrip(t, network, s)
	if restored.platoonLinks != s.platoonLinks || s.platoonLinks == 0 {
		t.Fatalf("%d links after the restore, want %d", restored.platoonLinks, s.platoonLinks)
	}
	restored = roundTrip(t, network, restored)
	if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	monitor := newPlatoonMonitor(restored)
	for range 60 * TicksPerSecond {
		restored.Step()
		checkTraffic(t, restored.Snapshot())
		monitor.check(t)
	}
	unlinked := s.ExportState()
	for index := range unlinked.Pods {
		unlinked.Pods[index].Platoon = nil
	}
	_, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: unlinked})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Demoted) == 0 {
		t.Fatal("no pod lost its place without the links")
	}
}

// TestPlatoonSnapshotMembers checks PlatoonID and PlatoonIndex at each
// tick on the merge corridor. The first pod of a platoon has index 1 and
// its own ID. Each follower has the ID of its predecessor's platoon and the
// next index. A pod that is not coupled has neither member in its JSON
// form.
func TestPlatoonSnapshotMembers(t *testing.T) {
	t.Parallel()
	s := restoreCorridor(t, mergeCorridor(true, 30), corridorQueues(corridorFeedLength-150))
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	largest := 0
	for range 40 * TicksPerSecond {
		s.Step()
		snapshot := s.Snapshot()
		for index, got := range snapshot.Vehicles {
			v := &s.vehicles[index]
			wantID, wantIndex := "", 0
			switch {
			case v.link.leader != 0:
				ahead := snapshot.Vehicles[v.link.leader-1]
				wantID, wantIndex = ahead.PlatoonID, ahead.PlatoonIndex+1
			case v.follower != 0:
				wantID, wantIndex = got.Pod.ID, 1
			}
			if got.PlatoonID != wantID || got.PlatoonIndex != wantIndex {
				t.Fatalf("tick %d: pod %s has platoon %q at %d, want %q at %d", s.tick, got.Pod.ID, got.PlatoonID, got.PlatoonIndex, wantID, wantIndex)
			}
			if wantID == "" {
				data, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("Platoon")) {
					t.Fatalf("tick %d: pod %s is not coupled, but its JSON form is %s", s.tick, got.Pod.ID, data)
				}
			}
			largest = max(largest, got.PlatoonIndex)
		}
	}
	if largest != MaxPlatoonLimit {
		t.Fatalf("the largest platoon index is %d, want %d", largest, MaxPlatoonLimit)
	}
}

// roundTrip saves s and restores it in the physical tier with no demoted
// pod. The restored state must save the same links, and each restored link
// must have the certificate of the live link. It returns the restored
// simulation.
func roundTrip(t *testing.T, network Network, s *Simulation) *Simulation {
	t.Helper()
	return roundTripUsing(t, s, func(saved SavedState) (*Simulation, RestoreResult, error) {
		return RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: saved})
	})
}

func roundTripUsing(t *testing.T, s *Simulation, restore func(SavedState) (*Simulation, RestoreResult, error)) *Simulation {
	t.Helper()
	saved := s.ExportState()
	restored, result, err := restore(saved)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("tick %d: the platoons did not restore in place: %+v", s.tick, result)
	}
	again := restored.ExportState()
	for index := range saved.Pods {
		link := saved.Pods[index].Platoon
		if !reflect.DeepEqual(again.Pods[index].Platoon, link) {
			t.Fatalf("tick %d: pod %s saves link %+v after the restore, want %+v", s.tick, saved.Pods[index].ID, again.Pods[index].Platoon, link)
		}
		if link == nil {
			if restored.vehicles[index].link.leader != 0 {
				t.Fatalf("tick %d: pod %s has a link after the restore", s.tick, saved.Pods[index].ID)
			}
			continue
		}
		live := &s.vehicles[index]
		start, _, _ := s.savedStart(live)
		want, got := linkCertificate(live, start+link.Lane), linkCertificate(&restored.vehicles[index], link.Lane)
		if got != want {
			t.Fatalf("tick %d: pod %s has certificate %s after the restore, want %s", s.tick, saved.Pods[index].ID, got, want)
		}
		// The restore reserves blocks from the position of each pod, so a
		// pod can hold fewer resources than before. Each resource of
		// another pod that it holds must have the same owner as before.
		after, before := pendingOwners(restored, &restored.vehicles[index]), pendingOwners(s, live)
		for r, owner := range after {
			if before[r] != owner {
				t.Fatalf("tick %d: pod %s holds resources of %v after the restore, want a part of %v", s.tick, saved.Pods[index].ID, after, before)
			}
		}
	}
	return restored
}

// linkCertificate returns the certificate of the link of v with the run
// from route index first: the predecessor, the lanes of the run, the lane
// and cell of the end block, the turn, and the clearance. The end block is
// none when it is before the run.
func linkCertificate(v *vehicle, first int) string {
	var lanes []string
	for _, lane := range v.Route[first : v.link.lane+v.link.lanes] {
		lanes = append(lanes, lane.ID)
	}
	end := "none"
	if v.link.end >= v.blocks.laneFirst(first) {
		lane, cell := v.blocks.cell(v.link.end)
		end = fmt.Sprintf("%s/%d", v.Route[lane].ID, cell)
	}
	return fmt.Sprintf("leader %d, run %v, end %s, turn %v, clearance %v, draining %v",
		v.link.leader, lanes, end, v.link.turn, v.link.clearance, v.link.draining || v.reservedThrough >= v.link.end)
}

// pendingOwners returns the owner of each resource that v holds and that
// another pod owns.
func pendingOwners(s *Simulation, v *vehicle) map[resource]string {
	owners := make(map[resource]string)
	for r := range v.routeReleases {
		if owner := s.owners[r]; owner != v.Pod.ID {
			owners[r] = owner
		}
	}
	return owners
}

// TestPlatoonRestoreRoundTrip saves and restores a platoon of two pods at
// every fifth tick, while the follower shares cells, while it drains before
// the fork where the routes leave each other, and after its link ends.
// Each restore must keep the certificate, and a second restore too.
func TestPlatoonRestoreRoundTrip(t *testing.T) {
	t.Parallel()
	network := forkNetwork(14)
	toFork, toDest := []string{"exit", "branch"}, []string{"exit", "approach"}
	s := restoreCorridor(t, network, []corridorPod{
		{route: toFork, station: "fork", distance: corridorExitLength - 300, leader: "p02"},
		{route: toDest, station: "dest", distance: corridorExitLength - 280},
	})
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	restore := func(saved SavedState) (*Simulation, RestoreResult, error) {
		return prepared.RestoreState(PreparedRestoreInput{Fleet: s.initial, State: saved})
	}
	follower := &s.vehicles[0]
	phases := make(map[string]bool)
	for tick := range 120 * TicksPerSecond {
		s.Step()
		phase := "after"
		if follower.link.leader != 0 {
			phase = "sharing"
			if follower.reservedThrough >= follower.link.end && s.holdsPending(follower) {
				phase = "draining"
			}
		}
		if tick%5 == 0 && follower.Pod.Activity == Traveling {
			phases[phase] = true
			roundTripUsing(t, roundTripUsing(t, s, restore), restore)
		}
	}
	if len(phases) != 3 {
		t.Fatalf("round trips in phases %v, want sharing, draining and after", phases)
	}
}

// TestPlatoonRestoreMaxTurn forms a link before a bend of about 120
// degrees, saves it and restores it two times. The restore must keep the
// pods in place and the certificate. In the first case, the bend is a
// little less than platoonMaxTurn, and the run holds it. In the second
// case, the bend is more than platoonMaxTurn by less than
// platoonTurnSlack, and the run ends before it.
func TestPlatoonRestoreMaxTurn(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		bend  Point
		lanes int
	}{
		{name: "within the maximum", bend: Point{X: 100 * math.Cos(platoonMaxTurn-1e-12), Y: 100 * math.Sin(platoonMaxTurn-1e-12)}, lanes: 2},
		{name: "within the slack", bend: Point{X: -50.0000000433, Y: 86.6025403534}, lanes: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lanes := []geometryLane{{to: Point{}}, {to: test.bend}, {to: Point{X: 2 * test.bend.X, Y: 2 * test.bend.Y}}}
			network := geometryNetwork(Point{X: -500}, lanes, 2)
			s := restoreGeometry(t, network, len(lanes), 2, 440, 60)
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			follower := &s.vehicles[1]
			linked := 0
			for tick := range 60 * TicksPerSecond {
				s.Step()
				if follower.link.leader == 0 {
					continue
				}
				if linked == 0 && follower.link.lanes != test.lanes || follower.link.turn > platoonMaxTurn {
					t.Fatalf("tick %d: link of %d lanes with turn %v, want %d lanes and a turn within %v",
						tick, follower.link.lanes, follower.link.turn, test.lanes, platoonMaxTurn)
				}
				linked++
				if tick%5 == 0 {
					roundTrip(t, network, roundTrip(t, network, s))
				}
			}
			if linked == 0 {
				t.Fatal("the pods did not link")
			}
		})
	}
}

// TestPlatoonDrainExtends runs a link whose run grows after it drains. The
// first lane curves by 60 degrees, and the next lane is straight and 40 m
// long. The lane after it turns by about 52 degrees, and it starts past
// the horizon when the link forms. So the run holds the first two lanes,
// with the turn of the curve. The follower reserves the end block before it
// leaves the curve, and the link drains. The third lane cannot join yet,
// because the turn from the curve to it is more than the turn of the link.
// On the straight lane, the turn to the third lane is within the turn of
// the link, so the run grows, the end block moves, and the link shares
// again. A cell that a pod outside the fleet holds for 20 s stops the
// predecessor, so the follower closes up and holds its cells while the
// link drains. A save and a restore while the link drains must keep the
// phase and the certificate, and the restored link must grow to the same
// certificate. The restore starts each pod from a stop, so the restored
// link grows at a later tick. The save is at a tick when the follower is
// in the last block of the curve. There the restore reserves the same
// blocks as the live pod, so the follower keeps the resources of the
// predecessor that it holds.
func TestPlatoonDrainExtends(t *testing.T) {
	t.Parallel()
	const bend = math.Pi/3 + 0.9
	corner := Point{X: 250}
	curveEnd := Point{X: 250 + 250*math.Cos(math.Pi/3), Y: 250 * math.Sin(math.Pi/3)}
	straightEnd := Point{X: curveEnd.X + 40*math.Cos(math.Pi/3), Y: curveEnd.Y + 40*math.Sin(math.Pi/3)}
	turnEnd := Point{X: straightEnd.X + 200*math.Cos(bend), Y: straightEnd.Y + 200*math.Sin(bend)}
	lastEnd := Point{X: turnEnd.X + 100*math.Cos(bend), Y: turnEnd.Y + 100*math.Sin(bend)}
	lanes := []geometryLane{{to: curveEnd, control: &corner}, {to: straightEnd}, {to: turnEnd}, {to: lastEnd}}
	network := geometryNetwork(Point{}, lanes, 2)
	s := restoreGeometry(t, network, len(lanes), 2, 200, 60)
	leader, follower := &s.vehicles[0], &s.vehicles[1]
	var stop []resource
	for _, b := range leader.blocks.span(0, leader.blocks.len()) {
		if b.end > 300 {
			for _, r := range b.resources {
				if r.kind == trackResource {
					stop = append(stop, r)
					s.owners[r] = "blocker"
				}
			}
			break
		}
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	var restored *Simulation
	var grown string
	for range 120 * TicksPerSecond {
		if s.tick == 20*TicksPerSecond {
			for _, r := range stop {
				delete(s.owners, r)
			}
		}
		s.Step()
		if follower.link.leader == 0 {
			continue
		}
		draining := follower.link.draining || follower.reservedThrough >= follower.link.end
		if restored == nil && draining && s.holdsPending(follower) && follower.blockIndex+1 == follower.blocks.laneFirst(1) {
			if follower.link.lanes != 2 {
				t.Fatalf("tick %d: the link drains with %d lanes, want 2", s.tick, follower.link.lanes)
			}
			restored = roundTrip(t, network, roundTrip(t, network, s))
		}
		if restored != nil && follower.link.lanes > 2 && !draining {
			grown = linkCertificate(follower, follower.link.lane)
			break
		}
	}
	if grown == "" {
		t.Fatalf("the link did not drain and grow, restored %v", restored != nil)
	}
	// The saved state does not keep the mode.
	if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	again := &restored.vehicles[1]
	for range 120 * TicksPerSecond {
		restored.Step()
		if again.link.leader == 0 {
			t.Fatalf("tick %d: the restored link ended", restored.tick)
		}
		if again.link.lanes > 2 {
			if got := linkCertificate(again, again.link.lane); got != grown {
				t.Fatalf("tick %d: the restored link grew to %s, want %s", restored.tick, got, grown)
			}
			return
		}
	}
	t.Fatal("the restored link did not grow")
}

// TestPlatoonFollowerStaysBehind restores a coupled pair that shares a
// cell of the exit lane. The follower has the lower pod ID, so its
// admission request comes first. It must not reserve a free cell before
// its predecessor does, because then each pod waits for the other. In the
// first case, the next cell is the last cell before the fork where the
// routes separate. In the second case, a third pod holds the next cell and
// then leaves.
func TestPlatoonFollowerStaysBehind(t *testing.T) {
	t.Parallel()
	toFork, toDest := []string{"exit", "branch"}, []string{"exit", "approach"}
	tests := []struct {
		name string
		pods []corridorPod
	}{
		{name: "at the fork", pods: []corridorPod{
			{route: toFork, station: "fork", distance: corridorExitLength - 53, leader: "p02"},
			{route: toDest, station: "dest", distance: corridorExitLength - 40},
		}},
		{name: "behind a pod that leaves", pods: []corridorPod{
			{route: toDest, station: "dest", distance: 3015, leader: "p02"},
			{route: toDest, station: "dest", distance: 3028},
			{route: toDest, station: "dest", distance: 3050},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := restoreCorridor(t, forkNetwork(14), test.pods)
			if s.platoonLinks != 1 {
				t.Fatalf("%d links after the restore, want 1", s.platoonLinks)
			}
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			monitor := newPlatoonMonitor(s)
			for range 900 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				monitor.check(t)
				if s.completed == len(test.pods) {
					return
				}
			}
			t.Fatalf("%d of %d journeys completed", s.completed, len(test.pods))
		})
	}
}

// TestPlatoonRestoreBeforeATurn restores a platoon that stopped close
// together near the end of a straight lane. The next lane turns by 90
// degrees. The run of the saved link holds only the straight lane, so its
// clearance is 12.01 m. The restore must keep that link, and not plan a
// run with the turn, because the pods are 13 m apart, less than the
// clearance of 16.98 m for the turn. A second restore must give the same
// links and positions.
func TestPlatoonRestoreBeforeATurn(t *testing.T) {
	t.Parallel()
	network := mergeCorridor(false, 0)
	network.Nodes = append(network.Nodes, Node{ID: "corner", Position: Point{X: corridorExitLength, Y: 1000}})
	for index := range network.Lanes {
		if network.Lanes[index].ID == "approach" {
			network.Lanes[index].From = "corner"
		}
	}
	network.Lanes = append(network.Lanes, Lane{ID: "rise", From: "exit-end", To: "corner", SpeedLimit: 14})
	route := []string{"exit", "rise", "approach"}
	s := restoreCorridor(t, network, []corridorPod{
		{route: route, station: "dest", distance: corridorExitLength - 113, leader: "p02", lanes: 1},
		{route: route, station: "dest", distance: corridorExitLength - 100},
	})
	follower := &s.vehicles[0]
	if follower.link.leader != 2 || follower.link.clearance > 13 {
		t.Fatalf("link %+v after the restore, want predecessor p02 and a clearance of less than 13 m", follower.link)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	monitor := newPlatoonMonitor(s)
	for range TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		monitor.check(t)
	}
	if follower.link.leader != 2 || follower.Pod.LaneID != "exit" {
		t.Fatalf("pod p01 has predecessor %d on lane %s before the save, want p02 on exit", follower.link.leader, follower.Pod.LaneID)
	}
	saved := s.ExportState()
	restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: saved})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the platoon did not restore in place: %+v", result)
	}
	for index := range s.vehicles {
		before, after := &s.vehicles[index], &restored.vehicles[index]
		if after.link.leader != before.link.leader || after.Pod.LaneID != before.Pod.LaneID || after.Pod.LaneDistance != before.Pod.LaneDistance {
			t.Fatalf("pod %s: predecessor %d at %s %.3f m after the restore, want %d at %s %.3f m", before.Pod.ID,
				after.link.leader, after.Pod.LaneID, after.Pod.LaneDistance, before.link.leader, before.Pod.LaneID, before.Pod.LaneDistance)
		}
	}
}

// TestPlatoonRestoreRejectsBadLinks checks that the physical restore fails
// for platoon links that are not valid.
func TestPlatoonRestoreRejectsBadLinks(t *testing.T) {
	t.Parallel()
	network := mergeCorridor(true, 30)
	route, side := []string{"main", "exit", "approach"}, []string{"side", "exit", "approach"}
	var pods []corridorPod
	for position := range 6 {
		pods = append(pods, corridorPod{route: route, station: "dest", distance: 2900 - corridorQueueGap*float64(position)})
	}
	pods = append(pods, corridorPod{route: side, station: "dest", distance: 2900}, corridorPod{route: side, station: "dest", distance: 2900 - corridorQueueGap})
	s := restoreCorridor(t, network, pods)
	base := s.ExportState()
	straight := func(leader string) *SavedPlatoonLink { return &SavedPlatoonLink{Leader: leader, Lanes: 2} }
	tests := []struct {
		name  string
		links map[int]*SavedPlatoonLink
		// distance moves a pod on its lane.
		distance map[int]float64
	}{
		{name: "an unknown pod", links: map[int]*SavedPlatoonLink{1: straight("p99")}},
		{name: "the pod itself", links: map[int]*SavedPlatoonLink{1: straight("p02")}},
		{name: "two followers", links: map[int]*SavedPlatoonLink{1: straight("p01"), 2: straight("p01")}},
		{name: "a loop", links: map[int]*SavedPlatoonLink{0: straight("p02"), 1: straight("p01")}},
		{name: "a platoon of five", links: map[int]*SavedPlatoonLink{1: straight("p01"), 2: straight("p02"), 3: straight("p03"), 4: straight("p04")}},
		{name: "a run with the last lane", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lanes: 3}}},
		{name: "a run past the route", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lane: 1, Lanes: 2}}},
		{name: "no run", links: map[int]*SavedPlatoonLink{1: {Leader: "p01"}}},
		{name: "a run on other lanes", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", LeaderLane: 1, Lanes: 1}}},
		{name: "a turn past the limit", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lanes: 2, Turn: 2.1}}},
		{name: "a negative turn", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lanes: 2, Turn: -0.1}}},
		{name: "a run that turns more", links: map[int]*SavedPlatoonLink{7: straight("p07")}},
		{name: "two turns", links: map[int]*SavedPlatoonLink{1: straight("p01"), 2: {Leader: "p02", Lanes: 2, Turn: 0.5}}},
		{name: "pods closer than the clearance", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lanes: 2, Turn: math.Pi / 2}}, distance: map[int]float64{1: 2887}},
		{name: "a lane index past the route", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lane: math.MaxInt, Lanes: 1}}},
		{name: "a predecessor lane index past the route", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", LeaderLane: math.MaxInt, Lanes: 1}}},
		{name: "a run of too many lanes", links: map[int]*SavedPlatoonLink{1: {Leader: "p01", Lanes: math.MaxInt}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := base
			state.Pods = make([]SavedPod, len(base.Pods))
			copy(state.Pods, base.Pods)
			for index, link := range test.links {
				state.Pods[index].Platoon = link
			}
			for index, distance := range test.distance {
				state.Pods[index].LaneDistance, state.Pods[index].Distance = distance, distance
			}
			_, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != RestoreLogical || result.PhysicalError == nil {
				t.Fatalf("tier %s, physical error %v", result.Tier, result.PhysicalError)
			}
			t.Log(result.PhysicalError)
		})
	}
}

// TestPlatoonRestoreRejectsMixedSpeeds restores a link on a straight lane
// before a lane with a lower speed limit. A pod slows down at once to a
// lower limit, so its follower could not stop within its cap. A new link
// does not form on such a route, and a restore must reject the saved
// link.
func TestPlatoonRestoreRejectsMixedSpeeds(t *testing.T) {
	t.Parallel()
	network := mergeCorridor(false, 0)
	for index := range network.Lanes {
		if network.Lanes[index].ID == "approach" {
			network.Lanes[index].SpeedLimit = 1
		}
	}
	route := []string{"main", "exit", "approach"}
	s := restoreCorridor(t, network, []corridorPod{
		{route: route, station: "dest", distance: 100},
		{route: route, station: "dest", distance: 100 - corridorQueueGap},
	})
	state := s.ExportState()
	state.Pods[1].Platoon = &SavedPlatoonLink{Leader: "p01", Lanes: 1}
	_, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestoreLogical || result.PhysicalError == nil || !strings.Contains(result.PhysicalError.Error(), "speed limit") {
		t.Fatalf("tier %s, physical error %v", result.Tier, result.PhysicalError)
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
// monitor checks the owners at each tick. A save and a restore at each
// tenth tick must keep each pod in place. A predecessor that passed the
// end of the feed can still hold the junction section of the merge, which
// its follower on the feed shares.
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
	network := mergeCorridor(true, 15)
	s := restoreCorridor(t, network, pods)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	restore := func(saved SavedState) (*Simulation, RestoreResult, error) {
		return prepared.RestoreState(PreparedRestoreInput{Fleet: s.initial, State: saved})
	}
	monitor := newPlatoonMonitor(s)
	for tick := range 120 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		monitor.check(t)
		if tick%10 == 0 {
			roundTripUsing(t, roundTripUsing(t, s, restore), restore)
		}
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

// TestPlatoonReturnWaits checks the grant rule for a route that passes a
// node again. The predecessor asks for a block with a resource that it
// kept from an earlier block and that its follower also holds. The grant
// must wait, because the predecessor would keep the resource past the run
// of the follower. It goes on when the follower no longer holds it.
func TestPlatoonReturnWaits(t *testing.T) {
	t.Parallel()
	route := []string{"main", "exit", "approach"}
	s := restoreCorridor(t, mergeCorridor(false, 0), []corridorPod{
		{route: route, station: "dest", distance: 1000, leader: "p02"},
		{route: route, station: "dest", distance: 1020},
	})
	follower, leader := &s.vehicles[0], &s.vehicles[1]
	if follower.link.leader != 2 {
		t.Fatal("the pods did not restore coupled")
	}
	next := leader.reservedThrough + 1
	r := leader.blocks.at(next).resources[0]
	leader.routeReleases[r], follower.routeReleases[r] = leader.distance+1, follower.distance+1
	s.grant(intent{index: 1, block: next})
	if leader.reservedThrough >= next || leader.Pod.BlockedBy != follower.Pod.ID {
		t.Fatalf("the predecessor reserved through %d, blocked by %q", leader.reservedThrough, leader.Pod.BlockedBy)
	}
	delete(follower.routeReleases, r)
	s.grant(intent{index: 1, block: next})
	if leader.reservedThrough < next {
		t.Fatal("the predecessor did not reserve the block")
	}
}
