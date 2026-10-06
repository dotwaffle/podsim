package sim

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
)

// pruneNetwork builds a random planar network for the pruning test.
// Junctions on a ring carry one-way roads, and spokes join some junctions
// to a center node. Each wedge between two junctions can hold stations
// outside the ring, entered from the first junction and left to the
// second: a parking station, an unbanked station of the shape of
// lineNetwork, a station with two banks, or a pair of twins, one outside
// and one inside the ring, whose approaches differ by up to 10 cm, so
// that their keys can tie. In some wedges, only express pods may use the
// ring road and the through lane of the station, so a road for other
// pods passes a berth. For the parking station, which is never a
// candidate, such a road can lead to the chosen station.
type pruneNetwork struct {
	rng       *rand.Rand
	network   Network
	junctions []string
	stations  int
	// wedge holds the frame of the current wedge: the middle of the ring
	// road, its direction, and the outward normal.
	middle, along, out Point
}

func (b *pruneNetwork) node(id string, u, w float64) {
	position := Point{X: b.middle.X + u*b.along.X + w*b.out.X, Y: b.middle.Y + u*b.along.Y + w*b.out.Y}
	b.network.Nodes = append(b.network.Nodes, Node{ID: id, Position: position})
}

func (b *pruneNetwork) lane(lane Lane) {
	if lane.SpeedLimit == 0 {
		lane.SpeedLimit = []float64{10, 14, 20}[b.rng.IntN(3)]
	}
	b.network.Lanes = append(b.network.Lanes, lane)
}

// restrict gives the lane the vehicle classes.
func (b *pruneNetwork) restrict(id string, classes ClassSet) {
	for index := range b.network.Lanes {
		if b.network.Lanes[index].ID == id {
			b.network.Lanes[index].VehicleClasses = classes
		}
	}
}

// lineStation adds an unbanked station in wedge a at the depth, with its
// berths away from the ring road. A negative depth puts it inside the
// ring. It returns the station ID.
func (b *pruneNetwork) lineStation(a int, depth float64, berths int, parking bool) string {
	id := fmt.Sprintf("s%d", b.stations)
	b.stations++
	side := math.Copysign(1, depth)
	station := Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", ParkingOnly: parking}
	b.node(station.Entry, -75, depth)
	b.node(station.Exit, 75, depth)
	b.lane(Lane{ID: id + "-through", From: station.Entry, To: station.Exit, SpeedLimit: 14})
	for berth := range berths {
		berthID := fmt.Sprintf("%s-%d", id, berth+1)
		offset := 60 + 40*float64(berth/2)
		if berth%2 == 1 {
			offset = -offset
		}
		b.node(berthID, 0, depth+side*offset)
		b.lane(Lane{ID: berthID + "-in", From: station.Entry, To: berthID, SpeedLimit: 14})
		b.lane(Lane{ID: berthID + "-out", From: berthID, To: station.Exit, SpeedLimit: 14})
		station.Berths = append(station.Berths, Berth{ID: berthID, Node: berthID})
	}
	b.network.Stations = append(b.network.Stations, station)
	b.lane(Lane{ID: "to-" + id, From: b.junctions[a], To: station.Entry})
	b.lane(Lane{ID: "from-" + id, From: station.Exit, To: b.junctions[(a+1)%len(b.junctions)]})
	return id
}

// bankStation adds a station with two banks in wedge a, the second bank
// 500 m farther out than the first. Each bank has an entry road from
// junction a, an arrival node, its berths, a departure node, and an exit
// road to junction a+1.
func (b *pruneNetwork) bankStation(a int, depth float64) {
	id := fmt.Sprintf("s%d", b.stations)
	b.stations++
	station := Station{ID: id, Name: id}
	for bank := range 2 {
		prefix := fmt.Sprintf("%s-%c", id, 'a'+bank)
		u, w := -250.0, depth+500*float64(bank)
		entry, arrival, departure, exit := prefix+"-entry", prefix+"-arrival", prefix+"-departure", prefix+"-exit"
		b.node(entry, u, w)
		b.node(arrival, u+60, w+80)
		b.node(departure, u+240, w+80)
		b.node(exit, u+300, w)
		lane := func(name, from, to string, role StationLaneRole) {
			b.lane(Lane{ID: name, From: from, To: to, SpeedLimit: 14, StationID: id, StationRole: role})
		}
		lane(prefix+"-through", entry, exit, StationThroughRole)
		lane(prefix+"-arrive", entry, arrival, StationBerthAccessRole)
		lane(prefix+"-depart", departure, exit, StationDepartureRole)
		var ids []string
		for berth := range 1 + b.rng.IntN(2) {
			berthID := fmt.Sprintf("%s-%d", prefix, berth+1)
			b.node(berthID, u+150, w+120+60*float64(berth))
			lane(berthID+"-in", arrival, berthID, StationBerthAccessRole)
			lane(berthID+"-out", berthID, departure, StationDepartureRole)
			station.Berths = append(station.Berths, Berth{ID: berthID, Node: berthID})
			ids = append(ids, berthID)
		}
		station.Banks = append(station.Banks, StationBank{ID: string(rune('a' + bank)), Entry: entry, Exit: exit, BerthIDs: ids})
		b.lane(Lane{ID: "to-" + prefix, From: b.junctions[a], To: entry, StationID: id, StationRole: StationEntryRole})
		b.lane(Lane{ID: "from-" + prefix, From: exit, To: b.junctions[(a+1)%len(b.junctions)], StationID: id, StationRole: StationExitRole})
	}
	station.Entry, station.Exit = station.Banks[0].Entry, station.Banks[0].Exit
	b.network.Stations = append(b.network.Stations, station)
}

// newPruneNetwork returns a random network for the seed, and the number of
// berths of its parking station, which is the first station.
func newPruneNetwork(seed uint64) (Network, int) {
	b := &pruneNetwork{rng: rand.New(rand.NewPCG(seed, 17))}
	count := 6 + b.rng.IntN(6)
	radius := 3000.0
	points := make([]Point, count)
	b.network.Nodes = append(b.network.Nodes, Node{ID: "center"})
	for index := range count {
		angle := 2 * math.Pi * float64(index) / float64(count)
		points[index] = Point{X: math.Round(radius * math.Cos(angle)), Y: math.Round(radius * math.Sin(angle))}
		id := fmt.Sprintf("j%d", index)
		b.network.Nodes = append(b.network.Nodes, Node{ID: id, Position: points[index]})
		b.junctions = append(b.junctions, id)
		switch b.rng.IntN(4) {
		case 0:
			b.lane(Lane{ID: "in" + id, From: id, To: "center"})
		case 1:
			b.lane(Lane{ID: "out" + id, From: "center", To: id})
		default:
		}
	}
	express, _ := NewClassSet("express")
	parking := 2 + b.rng.IntN(4)
	for a := range count {
		next := points[(a+1)%count]
		length := pointDistance(points[a], next)
		b.middle = Point{X: (points[a].X + next.X) / 2, Y: (points[a].Y + next.Y) / 2}
		b.along = Point{X: (next.X - points[a].X) / length, Y: (next.Y - points[a].Y) / length}
		b.out = Point{X: b.along.Y, Y: -b.along.X}
		if b.out.X*b.middle.X+b.out.Y*b.middle.Y < 0 {
			b.out = Point{X: -b.out.X, Y: -b.out.Y}
		}
		ring := Lane{ID: fmt.Sprintf("ring%d", a), From: b.junctions[a], To: b.junctions[(a+1)%count]}
		depth := 300 + b.rng.Float64()*300
		switch kind := b.rng.IntN(10); {
		case a == 0:
			// A pod passes a berth of the parking station when only express
			// pods may use the ring road and the through lane, and the
			// parking station is never a candidate.
			if id := b.lineStation(a, depth, parking, true); b.rng.IntN(2) == 0 {
				ring.VehicleClasses = express
				b.restrict(id+"-through", express)
			}
		case kind < 2:
			b.bankStation(a, depth)
		case kind < 4:
			first, second := depth, -(depth + b.rng.Float64()*0.1)
			if b.rng.IntN(2) == 0 {
				first, second = -second, -first
			}
			b.lineStation(a, first, 1, false)
			b.lineStation(a, second, 1, false)
		case kind < 9:
			id := b.lineStation(a, depth, 1+b.rng.IntN(3), false)
			if b.rng.IntN(2) == 0 {
				ring.VehicleClasses = express
				b.restrict(id+"-through", express)
			}
		default:
		}
		b.lane(ring)
	}
	return b.network, parking
}

// pruneCase builds the simulation of one case of the pruning test. Pod 01
// rides between two random passenger stations, and the case steps until
// the pod can divert at a random tick. It then adds a class rule, a
// blocked set, a routing policy, and stopped pods. It returns nil when the
// seed gives no usable case.
func pruneCase(t *testing.T, seed uint64) *Simulation {
	t.Helper()
	network, parking := newPruneNetwork(seed)
	rng := rand.New(rand.NewPCG(seed, 29))
	var passenger []Station
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			passenger = append(passenger, station)
		}
	}
	origin := passenger[rng.IntN(len(passenger))]
	target := passenger[rng.IntN(len(passenger))]
	if origin.ID == target.ID {
		return nil
	}
	if rng.IntN(3) == 0 {
		express, _ := NewClassSet("express")
		for range 2 {
			station := &network.Stations[rng.IntN(len(network.Stations))]
			if station.ID != origin.ID && station.ID != target.ID && !station.ParkingOnly {
				station.Berths[rng.IntN(len(station.Berths))].VehicleClasses = express
			}
		}
	}
	fleet := []Placement{{ID: "01", StationID: origin.ID, BerthID: origin.Berths[0].ID}}
	parkingStation := network.Stations[0]
	for berth := range parking {
		fleet = append(fleet, Placement{ID: fmt.Sprintf("%02d", berth+2), StationID: parkingStation.ID, BerthID: parkingStation.Berths[berth].ID})
	}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatalf("seed %d: %v", seed, err)
	}
	s.incidentContract, s.emergenciesOn = IncidentV1Contract, true
	v := s.findVehicle("01")
	if err := s.board(v, newTrip(s, origin.ID, target.ID)); err != nil {
		return nil
	}
	ticks := 1 + rng.IntN(40*TicksPerSecond)
	for tick := 0; ; tick++ {
		if v.Pod.Activity != Boarding && v.Pod.Activity != Traveling {
			return nil
		}
		if _, _, ok := s.divertStart(v); ok && tick >= ticks && v.Pod.Activity == Traveling {
			break
		}
		s.Step()
	}
	if rng.IntN(3) == 0 {
		lanes := []string{s.network.Lanes[rng.IntN(len(s.network.Lanes))].ID}
		blockLanes(t, s, lanes...)
	}
	if err := s.SetRoutingPolicy(RoutingPolicy(rng.IntN(4))); err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(s.vehicles); index++ {
		if rng.IntN(2) == 0 {
			lane := s.network.Lanes[rng.IntN(len(s.network.Lanes))]
			stopOn(&s.vehicles[index], lane.ID, rng.Float64()*s.laneLength(lane))
		}
	}
	if err := s.withdrawService(v, emergencyHold); err != nil {
		t.Fatal(err)
	}
	return s
}

// scanResult is the result of one station choice and its installation.
type scanResult struct {
	found  bool
	key    choiceKey
	route  []string
	routes int64
}

// scan runs the station choice of pod 01 of s, pruned or exhaustive, and
// installs the pair, under one routing view.
func scan(t *testing.T, s *Simulation, prune bool) scanResult {
	t.Helper()
	v := s.findVehicle("01")
	prefix, from, ok := s.divertStart(v)
	if !ok {
		t.Fatal("the pod cannot divert")
	}
	defer s.leaveRouteView(s.enterRouteView(v))
	routes := s.searchCounters.routes
	candidate, found := s.chooseStation(v, prefix, from, prune)
	result := scanResult{found: found, key: candidate.key, routes: s.searchCounters.routes - routes}
	if found {
		if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: 1, station: candidate.station.ID, berth: candidate.berth.ID}); err != nil {
			t.Fatalf("the installation refuses the candidate: %v", err)
		}
		result.route = laneIDs(v.Route)
	}
	return result
}

// TestEmergencyChoicePruning compares the pruned scan with the exhaustive
// scan in 1,000 random cases. Both scans run from clones of one state,
// and they give the same pair and the same installed route. The pruned
// scan makes fewer route searches in at least half the cases, so the stop
// rule runs. Some cases have equal tick keys at two stations.
//
//nolint:tparallel // The group waits for its parallel cases before the counts.
func TestEmergencyChoicePruning(t *testing.T) {
	t.Parallel()
	skipLong(t)
	const cases = 1000
	type outcome struct {
		used, fewer, tied bool
	}
	outcomes := make([]outcome, cases)
	t.Run("cases", func(t *testing.T) {
		for seed := range uint64(cases) {
			t.Run(strconv.FormatUint(seed, 10), func(t *testing.T) {
				t.Parallel()
				s := pruneCase(t, seed)
				for attempt := uint64(1); s == nil; attempt++ {
					s = pruneCase(t, seed+attempt*cases)
				}
				pruned, exhaustive := scan(t, s.Clone(), true), scan(t, s.Clone(), false)
				if pruned.found != exhaustive.found || pruned.key != exhaustive.key || !slices.Equal(pruned.route, exhaustive.route) {
					t.Fatalf("pruned %+v, exhaustive %+v", pruned, exhaustive)
				}
				outcomes[seed] = outcome{used: true, fewer: pruned.routes < exhaustive.routes, tied: tiedKeys(t, s.Clone())}
			})
		}
	})
	fewer, tied := 0, 0
	for _, outcome := range outcomes {
		if !outcome.used {
			t.Fatal("a case did not run")
		}
		if outcome.fewer {
			fewer++
		}
		if outcome.tied {
			tied++
		}
	}
	if fewer < cases/2 || tied == 0 {
		t.Fatalf("the pruned scan made fewer route searches in %d cases, and %d cases had equal tick keys", fewer, tied)
	}
}

// tiedKeys reports whether two stations of the exhaustive scan of pod 01
// have candidates with the same tick key.
func tiedKeys(t *testing.T, s *Simulation) bool {
	t.Helper()
	v := s.findVehicle("01")
	prefix, from, _ := s.divertStart(v)
	defer s.leaveRouteView(s.enterRouteView(v))
	ticks := make(map[int64]int)
	for index, station := range s.network.Stations {
		if station.ParkingOnly {
			continue
		}
		choice := stationChoice{s: s, v: v, from: from, prefix: prefix, discharge: s.routeDischarge(v)}
		choice.base = choice.advance(estimateCursor{distance: v.distance}, v.Route[:prefix])
		choice.evaluate(index)
		if choice.found {
			ticks[choice.best.key.tick]++
			if ticks[choice.best.key.tick] > 1 {
				return true
			}
		}
	}
	return false
}

// TestEmergencyChoiceLongPrefix gives pod 01 a route that repeats a loop,
// with a kept prefix of more than 8,000 lanes. With the term guard
// holding, the pruned scan builds the tree and gives the pair of the
// exhaustive scan. With a prefix that fails the guard, the choice builds
// no tree, and it evaluates every station, as the exhaustive scan does.
func TestEmergencyChoiceLongPrefix(t *testing.T) {
	t.Parallel()
	network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1500, y: -600, berths: 2}, {id: "sC", x: 1100, y: -300, berths: 1}}, 1)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "h", BerthID: "h-1"}, {ID: "02", StationID: "p", BerthID: "p-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.ensureNetworkIndexes()
	lane := func(id string) Lane { return s.network.Lanes[s.graph.lanes[id]] }
	loop := []Lane{lane("approach"), lane("to-sA"), lane("sA-through"), lane("from-sA"), lane("home"), lane("h-through")}
	for _, test := range []struct {
		loops int
		guard bool
	}{{1400, true}, {4000, false}} {
		route := []Lane{lane("h-1-out")}
		for range test.loops {
			route = append(route, loop...)
		}
		route = route[:len(route)-1]
		if len(route) <= 8000 {
			t.Fatalf("the prefix has %d lanes", len(route))
		}
		c := s.Clone()
		v := c.findVehicle("01")
		v.Pod.Activity = Traveling
		c.setVehicleRoute(v, route)
		if terms := 4*len(route) + 7*len(c.network.Nodes) + 2*len(c.vehicles); terms <= pruneTermLimit != test.guard {
			t.Fatalf("%d terms", terms)
		}
		results := make([]stationCandidate, 2)
		counters := make([]searchCounters, 2)
		for index, prune := range []bool{true, false} {
			d := c.Clone()
			w := d.findVehicle("01")
			func() {
				defer d.leaveRouteView(d.enterRouteView(w))
				candidate, ok := d.chooseStation(w, len(route), "h-exit", prune)
				if !ok {
					t.Fatal("no candidate")
				}
				results[index], counters[index] = candidate, d.searchCounters
			}()
		}
		if results[0].key != results[1].key || !sameLanes(results[0].suffix, results[1].suffix) {
			t.Fatalf("%d loops: pruned %+v, exhaustive %+v", test.loops, results[0].key, results[1].key)
		}
		if built := counters[0].trees > 0; built != test.guard || !test.guard && counters[0].routes != counters[1].routes {
			t.Fatalf("%d loops: the pruned scan built a tree: %t, with %d route searches, and the exhaustive scan %d", test.loops, built, counters[0].routes, counters[1].routes)
		}
	}
}
