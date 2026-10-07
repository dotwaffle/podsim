package sim

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

// entryShape selects the shape of entryNetwork. join is the angle in
// degrees at which the approach "dest-road-in-02" joins the line of the
// entry lane. approach is the length of each approach lane. crossing adds
// a lane through the node "p2" at the start of "dest-road-in-02", from the
// node "x1" to the node "x2", and a lane from "x2" back to the origin.
// third adds a third approach "dest-road-in-03" that joins at -join.
// explicit gives "dest-01-arrival-link" the through role. alt adds the
// lane "dest-alt" from the station entry to "dest-02-arrival", longer than
// the chain of arrival links, with no station role. large spaces the
// berth lanes of "dest" 50 m apart in place of 30 m, so that they are
// long enough for a large class, and puts the origin berths in a column,
// so that their lanes keep 20 m apart.
type entryShape struct {
	join, approach                        float64
	crossing, third, explicit, alt, large bool
}

// entryNetwork returns a station in the shape of a London station. Two
// approach lanes meet at the node "dest-diverge": "dest-road-in-01" is in
// line with the entry lane, and "dest-road-in-02" joins it at shape.join.
// The entry lane "dest-access-in" is 139 m long, as in LondonFull, so it
// has five cells. After the station entry, a through lane and a chain of
// three berth branches start. Each approach has a long upstream lane from
// the origin station, and the station exit leads back to the origin.
func entryNetwork(shape entryShape, fleet int) Network {
	join := shape.join * math.Pi / 180
	// comb is the spacing in meters of the berth lanes.
	comb := 30.0
	if shape.large {
		comb = 50
	}
	cosine, sine := math.Cos(join), math.Sin(join)
	p2 := Point{X: -shape.approach * cosine, Y: -shape.approach * sine}
	network := Network{
		Nodes: []Node{
			{ID: "origin-entry", Position: Point{X: -5000, Y: 5000}},
			{ID: "origin-exit", Position: Point{X: -4000, Y: 5000}},
			{ID: "g1", Position: Point{X: -shape.approach - 1350}},
			{ID: "g2", Position: Point{X: p2.X - 1300*cosine, Y: p2.Y - 1300*sine}},
			{ID: "p1", Position: Point{X: -shape.approach}},
			{ID: "p2", Position: p2},
			{ID: "dest-diverge", Position: Point{}},
			{ID: "dest-entry", Position: Point{X: 139}},
			{ID: "dest-exit", Position: Point{X: 139, Y: 4 * comb}},
			{ID: "dest-merge", Position: Point{X: 100, Y: 5 * comb}},
			{ID: "g3", Position: Point{X: p2.X - 1300*cosine, Y: -p2.Y + 1300*sine}},
			{ID: "p3", Position: Point{X: p2.X, Y: -p2.Y}},
		},
		Lanes: []Lane{
			{ID: "origin-through", From: "origin-entry", To: "origin-exit", SpeedLimit: 14},
			{ID: "feed-1", From: "origin-exit", To: "g1", SpeedLimit: 14},
			{ID: "feed-2", From: "origin-exit", To: "g2", SpeedLimit: 14},
			{ID: "u1", From: "g1", To: "p1", SpeedLimit: 14},
			{ID: "u2", From: "g2", To: "p2", SpeedLimit: 14},
			{ID: "dest-road-in-01", From: "p1", To: "dest-diverge", SpeedLimit: 14, StationID: "dest", StationRole: StationApproachRole},
			{ID: "dest-road-in-02", From: "p2", To: "dest-diverge", SpeedLimit: 14, StationID: "dest", StationRole: StationApproachRole},
			{ID: "dest-access-in", From: "dest-diverge", To: "dest-entry", SpeedLimit: 14, StationID: "dest", StationRole: StationEntryRole},
			{ID: "dest-through", From: "dest-entry", To: "dest-exit", SpeedLimit: 14, StationID: "dest", StationRole: StationThroughRole},
			{ID: "dest-access-out", From: "dest-exit", To: "dest-merge", SpeedLimit: 14, StationID: "dest", StationRole: StationExitRole},
			{ID: "return", From: "dest-merge", To: "origin-entry", SpeedLimit: 14},
			{ID: "feed-3", From: "origin-exit", To: "g3", SpeedLimit: 14},
			{ID: "u3", From: "g3", To: "p3", SpeedLimit: 14},
			{ID: "dest-road-in-03", From: "p3", To: "dest-diverge", SpeedLimit: 14, StationID: "dest", StationRole: StationApproachRole},
		},
		Stations: []Station{
			{ID: "origin", Name: "Origin", Entry: "origin-entry", Exit: "origin-exit"},
			{ID: "dest", Name: "Destination", Entry: "dest-entry", Exit: "dest-exit"},
		},
	}
	if !shape.third {
		network.Nodes = network.Nodes[:len(network.Nodes)-2]
		network.Lanes = network.Lanes[:len(network.Lanes)-3]
	}
	if shape.crossing {
		// The crossing lane passes p2 at a right angle to u2.
		normal := Point{X: -sine, Y: cosine}
		network.Nodes = append(network.Nodes,
			Node{ID: "x1", Position: Point{X: p2.X - 1500*normal.X, Y: p2.Y - 1500*normal.Y}},
			Node{ID: "x2", Position: Point{X: p2.X + 400*normal.X, Y: p2.Y + 400*normal.Y}})
		network.Lanes = append(network.Lanes,
			Lane{ID: "feed-x", From: "origin-exit", To: "x1", SpeedLimit: 14},
			Lane{ID: "x-in", From: "x1", To: "p2", SpeedLimit: 14},
			Lane{ID: "x-out", From: "p2", To: "x2", SpeedLimit: 14},
			Lane{ID: "x-return", From: "x2", To: "origin-entry", SpeedLimit: 14})
	}
	arrival, departure := "dest-entry", "dest-exit"
	for k := 1; k <= 3; k++ {
		x := 139 + comb*float64(k)
		id := fmt.Sprintf("dest-%02d", k)
		network.Nodes = append(network.Nodes,
			Node{ID: id + "-arrival", Position: Point{X: x, Y: comb}},
			Node{ID: id + "-node", Position: Point{X: x, Y: 2 * comb}},
			Node{ID: id + "-departure", Position: Point{X: x, Y: 3 * comb}},
		)
		network.Lanes = append(network.Lanes,
			Lane{ID: id + "-arrival-link", From: arrival, To: id + "-arrival", SpeedLimit: 14, StationID: "dest", StationRole: StationBerthAccessRole},
			Lane{ID: id + "-in", From: id + "-arrival", To: id + "-node", SpeedLimit: 14, StationID: "dest", StationRole: StationBerthAccessRole},
			Lane{ID: id + "-out", From: id + "-node", To: id + "-departure", SpeedLimit: 14, StationID: "dest", StationRole: StationDepartureRole},
			Lane{ID: id + "-departure-link", From: id + "-departure", To: departure, SpeedLimit: 14, StationID: "dest", StationRole: StationDepartureRole},
		)
		network.Stations[1].Berths = append(network.Stations[1].Berths, Berth{ID: id, Node: id + "-node"})
		arrival, departure = id+"-arrival", id+"-departure"
	}
	for index := range network.Lanes {
		if lane := &network.Lanes[index]; lane.ID == "dest-01-arrival-link" && shape.explicit {
			lane.StationRole = StationThroughRole
		}
	}
	if shape.alt {
		network.Lanes = append(network.Lanes, Lane{ID: "dest-alt", From: "dest-entry", To: "dest-02-arrival", SpeedLimit: 14, Control: &Point{X: 209}})
	}
	for index := range fleet {
		node := fmt.Sprintf("origin-berth-%02d", index+1)
		position := Point{X: -4960 + 25*float64(index), Y: 5060}
		if shape.large {
			position = Point{X: -4500, Y: 5000 + 200*float64(index+1)}
		}
		network.Nodes = append(network.Nodes, Node{ID: node, Position: position})
		network.Lanes = append(network.Lanes,
			Lane{ID: node + "-in", From: "origin-entry", To: node, SpeedLimit: 14},
			Lane{ID: node + "-out", From: node, To: "origin-exit", SpeedLimit: 14})
		network.Stations[0].Berths = append(network.Stations[0].Berths, Berth{ID: fmt.Sprintf("origin-%02d", index+1), Node: node})
	}
	return network
}

// londonEntry is the shape of most tests: approaches of 150 m that meet
// at 30 degrees.
var londonEntry = entryShape{join: 30, approach: 150}

// entryPod is a pod at rest for restoreEntry. approach is 1 to 3 for a
// pod on the upstream lane of an approach, at distance on that lane, with
// a route to the station entry. With lane "road", the pod is on the
// approach lane instead, and with lane "feed", on the lane from the origin
// to the upstream lane. approach 0 is a pod on the crossing lane "x-in"
// with a route back to the origin. class is the class of the pod, link is
// its saved platoon link, and since is its saved WaitSince.
//
// A pod on an approach carries a party to the station "dest". berth k > 0
// gives it the berth "dest-0k" with a route through the chain of arrival
// links, or through the lanes of path after the entry lane. With berth 0,
// it has no berth yet, and path can add lanes back to the entry lane.
// through makes the pod pass the station "dest": it relocates empty to its
// origin berth through the station.
type entryPod struct {
	approach, berth int
	distance        float64
	class           VehicleClass
	link            *SavedPlatoonLink
	lane            string
	path            []string
	since           int64
	through         bool
}

// entryLaneIndexes returns the index of each lane of network by lane ID.
func entryLaneIndexes(network Network) map[string]int {
	indexes := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		indexes[lane.ID] = index
	}
	return indexes
}

// savedEntry returns the saved state of pods on entryNetwork at tick, and
// the fleet. The pod with index i is at the origin berth i+1 of the fleet.
func savedEntry(network Network, tick int64, pods []entryPod) (SavedState, []Placement) {
	lanes := entryLaneIndexes(network)
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1, Tick: tick}
	for index, pod := range pods {
		id := fmt.Sprintf("p%02d", index+1)
		berth := fmt.Sprintf("origin-%02d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "origin", BerthID: berth, Class: pod.class})
		saved := SavedPod{ID: id, Class: pod.class, Activity: "traveling", Origin: berth, LaneDistance: pod.distance, Distance: pod.distance, Platoon: pod.link, WaitSince: pod.since}
		road := fmt.Sprintf("dest-road-in-%02d", pod.approach)
		var path []string
		switch {
		case pod.approach == 0:
			saved.DestinationStation, saved.Destination, saved.RelocatingTo = "origin", berth, "origin"
			path = []string{"x-in", "x-out", "x-return"}
		case pod.through:
			saved.DestinationStation, saved.Destination, saved.RelocatingTo = "origin", berth, "origin"
			path = []string{fmt.Sprintf("u%d", pod.approach), road, "dest-access-in", "dest-through", "dest-access-out", "return"}

		default:
			state.RequestID++
			state.Boarded++
			saved.Occupied, saved.DestinationStation, saved.Stops = true, "dest", []string{"dest"}
			saved.Riders = []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}}
			path = append([]string{fmt.Sprintf("u%d", pod.approach), road, "dest-access-in"}, pod.path...)
			for k := 1; pod.path == nil && k <= pod.berth; k++ {
				path = append(path, fmt.Sprintf("dest-%02d-arrival-link", k))
			}
			if pod.berth > 0 {
				saved.Destination = fmt.Sprintf("dest-%02d", pod.berth)
			}
		}
		if saved.Destination != "" {
			path = append(path, saved.Destination+"-in")
		}
		if saved.DestinationStation == "origin" {
			path[len(path)-1] = fmt.Sprintf("origin-berth-%02d-in", index+1)
		}
		switch pod.lane {
		case "road":
			path = path[1:]
		case "feed":
			path = append([]string{fmt.Sprintf("feed-%d", pod.approach)}, path...)
		}
		saved.LaneID = path[0]
		for _, lane := range path {
			saved.Route = append(saved.Route, lanes[lane])
		}
		state.Pods = append(state.Pods, saved)
	}
	return state, fleet
}

// restoreEntry restores pods at rest on entryNetwork (see entryPod), with
// virtual platoons.
func restoreEntry(t *testing.T, network Network, pods []entryPod) *Simulation {
	t.Helper()
	return restoreEntryAt(t, network, 0, pods)
}

// restoreEntryAt is restoreEntry with the saved tick tick.
func restoreEntryAt(t *testing.T, network Network, tick int64, pods []entryPod) *Simulation {
	t.Helper()
	state, fleet := savedEntry(network, tick, pods)
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the pods did not restore in place: %+v", result)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	return s
}

// approachQueue returns count pods at rest on the upstream lane of approach,
// gap meters apart, with the first pod at head.
func approachQueue(approach, count int, head, gap float64) []entryPod {
	pods := make([]entryPod, count)
	for index := range pods {
		pods[index] = entryPod{approach: approach, distance: head - gap*float64(index)}
	}
	return pods
}

// entryMonitor checks the rules of the station entry tests at each tick:
// the platoon monitor, the safety observation, checkEntryLinks, and the
// coast rules. Its coastCheck hook evaluates the coast ceilings from the
// state after platoonCaps, forward and in reverse order, and requires one
// result. After the step, each pod has the ceiling of that evaluation.
type entryMonitor struct {
	s       *Simulation
	platoon *platoonMonitor
	// expected holds the ceilings of the hook, by pod index, and motion
	// the step of each such pod with the frozen inputs of the hook.
	expected map[int]float64
	motion   map[int]ordinaryMoveResult
	// speeds and stops hold the speed and the stop point of each
	// traveling pod after the last step.
	speeds, stops map[int]float64
	// coasted counts the ticks of pods with a ceiling. pairs counts the
	// ticks with two or more ceilings. chained counts the ceilings whose
	// releaser is a pod behind the owner of the resource.
	coasted, pairs, chained int
	// lanes holds each lane of the network by lane ID.
	lanes map[string]Lane
	// off drops the denied requests in the hook, so that no pod coasts.
	off bool
	// extra is nil, or a test function that the hook calls with the
	// denied requests and the ceilings that it found.
	extra func(s *Simulation, denied []deniedRequest, results []coastResult)
}

func newEntryMonitor(s *Simulation) *entryMonitor {
	m := &entryMonitor{s: s, platoon: newPlatoonMonitor(s), speeds: make(map[int]float64), stops: make(map[int]float64), lanes: make(map[string]Lane)}
	for _, lane := range s.network.Lanes {
		m.lanes[lane.ID] = lane
	}
	m.attach(s)
	return m
}

// attach installs the hook of the monitor in s.
func (m *entryMonitor) attach(s *Simulation) {
	m.s = s
	s.coastCheck = func(s *Simulation) {
		m.expected, m.motion = make(map[int]float64), make(map[int]ordinaryMoveResult)
		if s.admissionWork == nil {
			return
		}
		if m.off {
			clear(s.admissionWork.denied)
			s.admissionWork.denied = s.admissionWork.denied[:0]
			return
		}
		denied := s.admissionWork.denied
		forward := s.coastCeilings(denied)
		reversed := slices.Clone(denied)
		slices.Reverse(reversed)
		reverse := s.coastCeilings(reversed)
		if len(forward) != len(reverse) {
			panic(fmt.Sprintf("tick %d: %d ceilings forward, %d in reverse", s.tick, len(forward), len(reverse)))
		}
		for _, result := range forward {
			m.expected[result.index] = result.ceiling
		}
		for _, result := range reverse {
			if want, ok := m.expected[result.index]; !ok || want != result.ceiling {
				panic(fmt.Sprintf("tick %d: pod %d has ceiling %v in reverse, %v forward", s.tick, result.index, result.ceiling, want))
			}
		}
		for index, ceiling := range m.expected {
			v := &s.vehicles[index]
			frontier := v.blocks.end(v.reservedThrough)
			limit := frontier
			if v.link.leader != 0 {
				limit = min(limit, v.platoonCap)
			}
			lane := v.blocks.find(v.blockIndex, &v.blocks.cursors[podCursor]).lane
			m.motion[index] = coastMoveStep(&v.blocks, lane, v.distance, v.Pod.Speed, limit, ceiling)
			for r, releaseAt := range v.routeReleases {
				if (r.kind == nodeResource || r.kind == junctionResource) && releaseAt > v.distance && releaseAt <= frontier {
					panic(fmt.Sprintf("tick %d: pod %s coasts and retains %v until %v, before its reservation end %v", s.tick, v.Pod.ID, r, releaseAt, frontier))
				}
			}
		}
		diverge := resource{kind: junctionResource, id: "dest-diverge"}
		for _, d := range denied {
			if _, ok := m.expected[d.in.index]; !ok {
				continue
			}
			// Only a request for the zone of the station diverge coasts.
			holds := false
			for resources := range s.vehicles[d.in.index].blocks.spanResources(d.in.block, d.through+1) {
				holds = holds || slices.Contains(resources, diverge)
			}
			if !holds {
				panic(fmt.Sprintf("tick %d: pod %s coasts for a request without the station diverge", s.tick, s.vehicles[d.in.index].Pod.ID))
			}
			for resources := range s.vehicles[d.in.index].blocks.spanResources(d.in.block, d.through+1) {
				for _, r := range resources {
					if owner := s.ownerVehicle(s.owners[r]); owner != nil && owner.Pod.ID != s.vehicles[d.in.index].Pod.ID {
						if p := s.coastReleaser(s.owners[r], r); p != nil && p != owner {
							m.chained++
						}
					}
				}
			}
		}
		m.coasted += len(m.expected)
		if len(m.expected) >= 2 {
			m.pairs++
		}
		if m.extra != nil {
			m.extra(s, denied, forward)
		}
	}
}

// check checks the rules after a step.
func (m *entryMonitor) check(t *testing.T) {
	t.Helper()
	s := m.s
	m.platoon.check(t)
	checkEntryLinks(t, s)
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatalf("tick %d: %v", s.tick, err)
	}
	speeds, stops := make(map[int]float64), make(map[int]float64)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		ceiling, coasting := v.coast.at(s.tick)
		want, expected := m.expected[i]
		if coasting != expected || coasting && ceiling != want {
			t.Fatalf("tick %d: pod %s has ceiling %v (%v), the hook found %v (%v)", s.tick, v.Pod.ID, ceiling, coasting, want, expected)
		}
		if v.Pod.Activity != Traveling {
			continue
		}
		speeds[i], stops[i] = v.Pod.Speed, v.distance+stoppingDistance(v.Pod.Speed)
		if !coasting {
			continue
		}
		// The pod moved with the ceiling and the limit of the frozen state.
		if want := m.motion[i]; v.distance != want.distance || v.Pod.Speed != want.speed {
			t.Fatalf("tick %d: coasting pod %s moved to %v at %v m/s, want %v at %v m/s", s.tick, v.Pod.ID, v.distance, v.Pod.Speed, want.distance, want.speed)
		}
		if before, ok := m.speeds[i]; ok && before-v.Pod.Speed > maxSpeedStep {
			t.Fatalf("tick %d: coasting pod %s went from %v m/s to %v m/s", s.tick, v.Pod.ID, before, v.Pod.Speed)
		}
		if before, ok := m.stops[i]; ok && stops[i] < before-1e-9 {
			t.Fatalf("tick %d: the stop point of coasting pod %s moved back from %v to %v", s.tick, v.Pod.ID, before, stops[i])
		}
		if end := v.blocks.end(v.reservedThrough); v.distance > end+1e-9 {
			t.Fatalf("tick %d: coasting pod %s is at %v, past its reservation end %v", s.tick, v.Pod.ID, v.distance, end)
		}
	}
	m.speeds, m.stops = speeds, stops
}

// checkEntryLinks fails when a follower holds, as a resource of another
// pod, a berth, an entry node or its junction of its destination station,
// a cell of a lane from such a node, or a resource whose release distance
// is past the end of its run.
func checkEntryLinks(t *testing.T, s *Simulation) {
	t.Helper()
	from := make(map[string]string, len(s.network.Lanes))
	for _, lane := range s.network.Lanes {
		from[lane.ID] = lane.From
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.link.leader == 0 {
			continue
		}
		station, _ := s.station(v.destinationStation)
		geometry, _ := s.linkEnds(v, v.link)
		for r, releaseAt := range v.routeReleases {
			owner := s.owners[r]
			if owner.isZero() || owner.isPod(v.Pod.ID) {
				continue
			}
			if r.kind == berthResource || (r.kind == nodeResource || r.kind == junctionResource) && station.isEntry(r.id) ||
				r.kind == trackResource && station.isEntry(from[r.id]) || releaseAt > geometry {
				t.Fatalf("tick %d: follower %s holds %v of %s until %v, run end %v", s.tick, v.Pod.ID, r, owner, releaseAt, geometry)
			}
		}
	}
}

// divergeOrder records the pods in the order in which they take the
// junction of the station diverge, from the owners after each step.
type divergeOrder struct {
	last  resourceOwner
	order []string
}

func (o *divergeOrder) record(s *Simulation) {
	owner := s.owners[resource{kind: junctionResource, id: "dest-diverge"}]
	if !owner.isZero() && owner != o.last {
		o.order = append(o.order, owner.String())
	}
	o.last = owner
}
