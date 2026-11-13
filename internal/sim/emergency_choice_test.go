package sim

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// travelToArrivalChain steps s until pod v travels to the station in its
// arrival chain, where divertStart refuses it. An emergency on the pod
// then stays deferred until the pod arrives.
func travelToArrivalChain(t *testing.T, s *Simulation, v *vehicle, station string) {
	t.Helper()
	stepUntil(t, s, "pod "+v.Pod.ID+" in its arrival chain to "+station, func() bool {
		_, _, divertible := s.divertStart(v)
		return v.Pod.Activity == Traveling && v.destinationStation == station && !divertible
	})
}

// choiceStation is a candidate station of choiceNetwork. Its entry is at x,
// y. mirror puts the berths on the other side of the station, so that a
// station at -y is the mirror image of a station at y.
type choiceStation struct {
	id     string
	x, y   float64
	berths int
	mirror bool
}

// addLineStation adds a station of the shape of lineNetwork, with its
// entry at x, y and its exit 150 m east.
func addLineStation(network *Network, spec choiceStation, parking bool) {
	station := Station{ID: spec.id, Name: spec.id, Entry: spec.id + "-entry", Exit: spec.id + "-exit", ParkingOnly: parking}
	network.Nodes = append(network.Nodes, Node{ID: station.Entry, Position: Point{X: spec.x, Y: spec.y}}, Node{ID: station.Exit, Position: Point{X: spec.x + 150, Y: spec.y}})
	network.Lanes = append(network.Lanes, Lane{ID: spec.id + "-through", From: station.Entry, To: station.Exit, SpeedLimit: 14})
	for berth := range spec.berths {
		id := fmt.Sprintf("%s-%d", spec.id, berth+1)
		offset := 60 + 40*float64(berth/2)
		if berth%2 == 1 != spec.mirror {
			offset = -offset
		}
		network.Nodes = append(network.Nodes, Node{ID: id, Position: Point{X: spec.x + 75, Y: spec.y + offset}})
		network.Lanes = append(network.Lanes,
			Lane{ID: id + "-in", From: station.Entry, To: id, SpeedLimit: 14},
			Lane{ID: id + "-out", From: id, To: station.Exit, SpeedLimit: 14})
		station.Berths = append(station.Berths, Berth{ID: id, Node: id})
	}
	network.Stations = append(network.Stations, station)
}

// choiceNetwork returns a network with the home station h, with one berth,
// and the lane approach of 600 m from the exit of h to the node fork. The
// lane to-<id> goes from fork to the entry of each candidate station, and
// from-<id> from its exit to the node back. The lane home goes from back
// to the entry of h, and the parking station p, with the given berths,
// sits on a second road from back to h.
func choiceNetwork(stations []choiceStation, parking int) Network {
	var network Network
	addLineStation(&network, choiceStation{id: "h", berths: 1}, false)
	network.Nodes = append(network.Nodes, Node{ID: "fork", Position: Point{X: 750}}, Node{ID: "back", Position: Point{X: 2400, Y: -900}})
	network.Lanes = append(network.Lanes, Lane{ID: "approach", From: "h-exit", To: "fork", SpeedLimit: 14})
	for _, spec := range stations {
		addLineStation(&network, spec, false)
		network.Lanes = append(network.Lanes,
			Lane{ID: "to-" + spec.id, From: "fork", To: spec.id + "-entry", SpeedLimit: 14},
			Lane{ID: "from-" + spec.id, From: spec.id + "-exit", To: "back", SpeedLimit: 14})
	}
	addLineStation(&network, choiceStation{id: "p", x: 1200, y: -1300, berths: parking}, true)
	network.Lanes = append(network.Lanes,
		Lane{ID: "home", From: "back", To: "h-entry", SpeedLimit: 14},
		Lane{ID: "to-p", From: "back", To: "p-entry", SpeedLimit: 14},
		Lane{ID: "from-p", From: "p-exit", To: "h-entry", SpeedLimit: 14})
	return network
}

// choiceFleet returns a simulation on the network with emergencies on. Pod
// 01 boards a party from h to the station target at h, and each other pod
// is idle at a berth of p. The other placements put pods at more berths.
func choiceFleet(t *testing.T, network Network, parking int, target string, placements ...Placement) (*Simulation, *vehicle) {
	t.Helper()
	fleet := []Placement{{ID: "01", StationID: "h", BerthID: "h-1"}}
	for berth := range parking {
		fleet = append(fleet, Placement{ID: fmt.Sprintf("%02d", berth+2), StationID: "p", BerthID: fmt.Sprintf("p-%d", berth+1)})
	}
	for index, placement := range placements {
		placement.ID = fmt.Sprintf("%02d", len(fleet)+index+1)
		fleet = append(fleet, placement)
	}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	s.emergenciesOn = true
	v := s.findVehicle("01")
	if err := s.board(v, newTrip(s, "h", target)); err != nil {
		t.Fatal(err)
	}
	return s, v
}

// travelApproach steps s until pod v travels on the lane approach at least
// the distance into it, and checks that the pod can divert.
func travelApproach(t *testing.T, s *Simulation, v *vehicle, distance float64) {
	t.Helper()
	stepUntil(t, s, "pod 01 on approach", func() bool {
		return v.Pod.Activity == Traveling && v.Pod.LaneID == "approach" && v.Pod.LaneDistance >= distance
	})
	if _, _, ok := s.divertStart(v); !ok {
		t.Fatal("pod 01 cannot divert on approach")
	}
}

// viewChoice returns the station choice for pod v in s under a routing
// view of v, as the advance makes it, with the kept prefix of divertStart.
func viewChoice(t *testing.T, s *Simulation, v *vehicle) (stationCandidate, bool) {
	t.Helper()
	prefix, from, ok := s.divertStart(v)
	if !ok {
		t.Fatal("pod cannot divert")
	}
	defer s.leaveRouteView(s.enterRouteView(v))
	return s.chooseStation(v, prefix, from, true)
}

// choiceRoute returns the route that the candidate gives pod v: the kept
// prefix and the suffix of the candidate.
func choiceRoute(s *Simulation, v *vehicle, candidate stationCandidate) []Lane {
	prefix, _, _ := s.divertStart(v)
	return append(slices.Clone(v.Route[:prefix]), candidate.suffix...)
}

// stopOn makes pod w a stopped pod on the lane at the distance, as
// queueDischarge counts it. Only the readers of the queues see it: the
// test does not step s after the change.
func stopOn(w *vehicle, lane string, distance float64) {
	w.Pod.Activity, w.Pod.LaneID, w.Pod.LaneDistance = Traveling, lane, distance
	w.Pod.WaitReason, w.Pod.Speed = TrackOccupied, 0
}

// TestEmergencyChoiceBinds starts an emergency on a pod that can divert.
// The pod binds to the station of the lowest estimate, sA, which is not
// its own destination. The installed route is the candidate route, and
// RelocatingTo stays empty. The party is interrupted at sA, and the pod
// is in service again after the unload.
func TestEmergencyChoiceBinds(t *testing.T) {
	t.Parallel()
	network := choiceNetwork([]choiceStation{{id: "sB", x: 1500, y: 600, berths: 1}, {id: "sA", x: 1000, y: 300, berths: 1}, {id: "sC", x: 2000, y: 0, berths: 1}}, 1)
	s, v := choiceFleet(t, network, 1, "sC")
	travelApproach(t, s, v, 100)
	checkEmergenciesEachTick(t, s)
	candidate, ok := viewChoice(t, s.Clone(), v)
	if !ok || candidate.station.ID != "sA" {
		t.Fatalf("candidate %+v, %t, want sA", candidate.key, ok)
	}
	want := choiceRoute(s, v, candidate)
	startEmergency(t, s, v, 0)
	if v.op.purpose != opEmergencyUnload || v.destinationStation != "sA" || v.destination.ID != "sA-1" || v.RelocatingTo != "" {
		t.Fatalf("purpose %+v, destination %s %s, relocating to %q", v.op, v.destinationStation, v.destination.ID, v.RelocatingTo)
	}
	if !sameLanes(v.Route, want) {
		t.Fatalf("installed route %v, want the candidate route %v", laneIDs(v.Route), laneIDs(want))
	}
	stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
	if !slices.Equal(s.undelivered, []int{1}) || v.Pod.StationID != "sA" || v.withdrawn != 0 {
		t.Fatalf("interrupted %v at %s, holds %#x", s.undelivered, v.Pod.StationID, v.withdrawn)
	}
}

// laneIDs returns the IDs of the lanes.
func laneIDs(route []Lane) []string {
	ids := make([]string, len(route))
	for index, lane := range route {
		ids[index] = lane.ID
	}
	return ids
}

// TestEmergencyChoiceCommitment binds a pod to sA, and then puts a queue
// on the road to sA, so that a new choice would take sB. On the next
// cadence tick, the advance keeps sA: the station never changes after the
// bind.
func TestEmergencyChoiceCommitment(t *testing.T) {
	t.Parallel()
	network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1100, y: -300, berths: 1}}, 12)
	s, v := choiceFleet(t, network, 12, "sB")
	travelApproach(t, s, v, 450)
	startEmergency(t, s, v, 0)
	if v.destinationStation != "sA" {
		t.Fatalf("the pod binds to %s, want sA", v.destinationStation)
	}
	advance(s, emergencyChoiceTicks)
	if (s.tick-s.emergencies[0].start)%emergencyChoiceTicks != 0 {
		t.Fatalf("tick %d is not a cadence tick", s.tick)
	}
	for index := range 12 {
		stopOn(s.findVehicle(fmt.Sprintf("%02d", index+2)), "to-sA", 10+10*float64(index))
	}
	if other := s.Clone(); func() string {
		candidate, _ := viewChoice(t, other, &other.vehicles[0])
		return candidate.station.ID
	}() != "sB" {
		t.Fatal("the queue does not make sB win a new choice")
	}
	route := slices.Clone(v.Route)
	s.advanceEmergency(s.emergencies[0])
	if v.destinationStation != "sA" || !sameLanes(v.Route, route) {
		t.Fatalf("the bound pod moved to %s", v.destinationStation)
	}
}

// TestEmergencyChoiceEstimate checks the terms of the estimate. A queue on
// the road to the nearest station makes a farther station win. A stopped
// pod behind the emergency pod on its lane adds nothing, and one ahead of
// it adds one headway. Mirror stations tie, and the lower station index
// wins. Two choices from one state give one result.
func TestEmergencyChoiceEstimate(t *testing.T) {
	t.Parallel()
	t.Run("queue", func(t *testing.T) {
		t.Parallel()
		network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1100, y: -300, berths: 1}}, 12)
		s, v := choiceFleet(t, network, 12, "sB")
		travelApproach(t, s, v, 500)
		if candidate, _ := viewChoice(t, s.Clone(), v); candidate.station.ID != "sA" {
			t.Fatalf("with no queue, the choice is %s, want sA", candidate.station.ID)
		}
		for index := range 12 {
			stopOn(s.findVehicle(fmt.Sprintf("%02d", index+2)), "to-sA", 10+10*float64(index))
		}
		startEmergency(t, s, v, 0)
		if v.destinationStation != "sB" {
			t.Fatalf("with a queue on to-sA, the pod binds to %s, want sB", v.destinationStation)
		}
	})
	t.Run("own lane", func(t *testing.T) {
		t.Parallel()
		network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}}, 1)
		s, v := choiceFleet(t, network, 1, "sA")
		travelApproach(t, s, v, 200)
		base, _ := viewChoice(t, s.Clone(), v)
		for _, test := range []struct {
			name     string
			distance float64
			ticks    int64
		}{{"behind", v.Pod.LaneDistance - 50, 0}, {"ahead", v.Pod.LaneDistance + 50, queueHeadwaySeconds * TicksPerSecond}} {
			c := s.Clone()
			stopOn(c.findVehicle("02"), "approach", test.distance)
			candidate, _ := viewChoice(t, c, c.findVehicle("01"))
			if candidate.key.tick-base.key.tick != test.ticks {
				t.Errorf("a stopped pod %s adds %d ticks, want %d", test.name, candidate.key.tick-base.key.tick, test.ticks)
			}
		}
	})
	t.Run("ties", func(t *testing.T) {
		t.Parallel()
		for _, order := range [][]string{{"sA", "sB"}, {"sB", "sA"}} {
			specs := map[string]choiceStation{"sA": {id: "sA", x: 1000, y: 300, berths: 1}, "sB": {id: "sB", x: 1000, y: -300, berths: 1, mirror: true}}
			network := choiceNetwork([]choiceStation{specs[order[0]], specs[order[1]]}, 1)
			s, v := choiceFleet(t, network, 1, order[1])
			travelApproach(t, s, v, 100)
			first, _ := viewChoice(t, s.Clone(), v)
			second, _ := viewChoice(t, s.Clone(), v)
			if first.station.ID != order[0] || first.key != second.key {
				t.Fatalf("order %v: the choices are %s %+v and %s %+v, want %s twice", order, first.station.ID, first.key, second.station.ID, second.key, order[0])
			}
		}
		keys := []choiceKey{{2, 0, 1}, {1, 2, 2}, {2, 0, 0}, {1, 1, 3}}
		slices.SortFunc(keys, compareChoiceKeys)
		if want := []choiceKey{{1, 1, 3}, {1, 2, 2}, {2, 0, 0}, {2, 0, 1}}; !slices.Equal(keys, want) {
			t.Fatalf("key order %v, want %v", keys, want)
		}
	})
}

// TestEmergencyChoiceCandidates checks the candidate of each entry group.
// A station whose first compatible berth has no route stays a candidate
// through its second berth. An available berth is the candidate of its
// group before an occupied one. A banked station with one unreachable
// bank stays a candidate through the reachable bank.
func TestEmergencyChoiceCandidates(t *testing.T) {
	t.Parallel()
	t.Run("second berth", func(t *testing.T) {
		t.Parallel()
		network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 2}, {id: "sB", x: 1100, y: -300, berths: 1}}, 1)
		express, _ := NewClassSet("express")
		for index := range network.Lanes {
			if network.Lanes[index].ID == "sA-1-in" {
				network.Lanes[index].VehicleClasses = express
			}
		}
		s, v := choiceFleet(t, network, 1, "sB")
		travelApproach(t, s, v, 100)
		checkEmergenciesEachTick(t, s)
		startEmergency(t, s, v, 0)
		if v.destination.ID != "sA-2" {
			t.Fatalf("the pod binds to %s, want sA-2", v.destination.ID)
		}
	})
	t.Run("available berth", func(t *testing.T) {
		t.Parallel()
		network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 2}, {id: "sB", x: 1100, y: -300, berths: 1}}, 1)
		s, v := choiceFleet(t, network, 1, "sB", Placement{StationID: "sA", BerthID: "sA-1"})
		travelApproach(t, s, v, 100)
		checkEmergenciesEachTick(t, s)
		startEmergency(t, s, v, 0)
		if v.destination.ID != "sA-2" {
			t.Fatalf("the pod binds to %s, want the free berth sA-2", v.destination.ID)
		}
	})
	t.Run("bank", func(t *testing.T) {
		t.Parallel()
		network := BankExample()
		express, _ := NewClassSet("express")
		for index := range network.Lanes {
			if network.Lanes[index].ID == "a-entry" {
				network.Lanes[index].VehicleClasses = express
			}
		}
		s, err := NewFleet(network, []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}})
		if err != nil {
			t.Fatal(err)
		}
		s.emergenciesOn = true
		v := s.findVehicle("01")
		if err := s.board(v, newTrip(s, "origin", "hub")); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, s, "pod 01 on origin-exit", func() bool { return v.Pod.Activity == Traveling && v.Pod.LaneID == "origin-exit" })
		checkEmergenciesEachTick(t, s)
		startEmergency(t, s, v, 0)
		if v.destination.ID != "bank-b-1" {
			t.Fatalf("the pod binds to %s, want bank-b-1", v.destination.ID)
		}
	})
}

// routingState is a copy of the free-flow route memo of a simulation
// (section 7 of the incident emergency contract), and of the tree memo of
// its batches.
type routingState struct {
	routes       map[routeKey]routeResult
	routeOrder   []routeKey
	hasRouteMemo bool
	trees        *routeTrees
}

// copyRouting returns a copy of the routing state of s.
func copyRouting(s *Simulation) routingState {
	return routingState{routes: cloneRoutes(s.routes), routeOrder: slices.Clone(s.routeOrder), hasRouteMemo: s.routes != nil, trees: cloneRouteTrees(s.routeTrees)}
}

// viewNetwork is choiceNetwork with the stations sA and sB, and a second
// road from fork to sA through the node alt. The road through alt is 16
// percent slower than the lane to-sA.
func viewNetwork() Network {
	network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1500, y: -600, berths: 1}}, 2)
	network.Nodes = append(network.Nodes, Node{ID: "alt", Position: Point{X: 1000, Y: 100}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "alt-out", From: "fork", To: "alt", SpeedLimit: 14},
		Lane{ID: "alt-in", From: "alt", To: "sA-entry", SpeedLimit: 14})
	return network
}

// TestEmergencyChoiceRoutingView runs the choice and the installation. Both
// route memos do not change, and the installed route is the candidate route.
func TestEmergencyChoiceRoutingView(t *testing.T) {
	t.Parallel()
	s, v := choiceFleet(t, viewNetwork(), 2, "sB")
	travelApproach(t, s, v, 100)
	checkEmergenciesEachTick(t, s)
	candidate, ok := viewChoice(t, s.Clone(), v)
	if !ok || candidate.station.ID != "sA" {
		t.Fatalf("candidate %s, %t, want sA", candidate.station.ID, ok)
	}
	want := choiceRoute(s, v, candidate)
	before := copyRouting(s)
	startEmergency(t, s, v, 0)
	//nolint:govet // deepequalerrors: route errors compare by value on purpose.
	if after := copyRouting(s); !reflect.DeepEqual(before, after) {
		t.Fatalf("the choice changed the routing state:\n%+v\n%+v", before, after)
	}
	if v.op.purpose != opEmergencyUnload || !sameLanes(v.Route, want) {
		t.Fatalf("installed route %v, want the candidate route %v", laneIDs(v.Route), laneIDs(want))
	}
	if usesLane(v.Route, "alt-in") {
		t.Fatalf("the route %v goes through alt", laneIDs(v.Route))
	}
}
