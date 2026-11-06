package sim

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// frozenCounts holds the counts of the route work at the time of the reference.
type frozenCounts struct {
	searches, localFailed int64
}

// frozenRoute keeps routeIndexedWithWork from before bank routes used lane
// indexes. It shares no route assembly with the simulation. Each search has its
// own arrays. It must not change to follow the simulation.
func (n Network) frozenRoute(input networkRouteInput, graph routeGraph, counts *frozenCounts) ([]Lane, error) {
	if !input.bankRaw && len(graph.banks.banks) > 0 {
		return n.frozenBankRoute(input, graph, counts)
	}
	if graph.banks.err != nil {
		return nil, graph.banks.err
	}
	from, ok := graph.nodes[input.from]
	if !ok {
		return nil, fmt.Errorf("unknown origin %q", input.from)
	}
	to, ok := graph.nodes[input.to]
	if !ok {
		return nil, fmt.Errorf("unknown destination %q", input.to)
	}
	if !graph.nodeAllows(from, input.class) || !graph.nodeAllows(to, input.class) {
		return nil, ErrUnreachable
	}
	counts.searches++
	distance := make([]float64, len(n.Nodes))
	previous := make([]int, len(n.Nodes))
	visited := make([]bool, len(n.Nodes))
	for index := range distance {
		distance[index], previous[index] = math.Inf(1), -1
	}
	filtered := input.bankExternal || input.allowedLanes != nil || input.forbidden != nil || input.ownBerthsOnly
	distance[from] = 0
	queue := routeQueue{{node: from}}
	for len(queue) > 0 {
		item := queue.pop()
		if visited[item.node] || item.distance != distance[item.node] {
			continue
		}
		if item.node == to {
			break
		}
		visited[item.node] = true
		if input.terminalBerthsOnly && item.node != from && graph.berthStations[item.node] >= 0 {
			continue
		}
		for _, laneIndex := range graph.outgoing[item.node] {
			edge := graph.edges[laneIndex]
			if !graph.laneAllows(laneIndex, input.class) || !graph.laneOpen(laneIndex) {
				continue
			}
			if filtered && routeLaneFiltered(&input, &graph, n.Lanes, laneIndex, from, to) {
				continue
			}
			candidate := item.distance + edge.seconds
			if candidate < distance[edge.to] {
				distance[edge.to], previous[edge.to] = candidate, laneIndex
				queue.push(routeQueueItem{node: edge.to, distance: candidate})
			}
		}
	}
	if math.IsInf(distance[to], 1) {
		return nil, ErrUnreachable
	}
	count := 0
	for current := to; current != from; count++ {
		laneIndex := previous[current]
		if laneIndex < 0 {
			return nil, ErrUnreachable
		}
		current = graph.edges[laneIndex].from
	}
	if count == 0 {
		return nil, nil
	}
	route := make([]Lane, count)
	for current := to; current != from; {
		laneIndex := previous[current]
		count--
		route[count] = n.Lanes[laneIndex]
		current = graph.edges[laneIndex].from
	}
	return route, nil
}

// frozenBankRoute keeps bankRoute from before bank routes used lane indexes.
func (n Network) frozenBankRoute(input networkRouteInput, graph routeGraph, counts *frozenCounts) ([]Lane, error) {
	if graph.banks.err != nil {
		return nil, graph.banks.err
	}
	from, fromOK := graph.nodes[input.from]
	to, toOK := graph.nodes[input.to]
	if !fromOK || !toOK {
		return nil, fmt.Errorf("unknown route endpoint %q or %q", input.from, input.to)
	}
	if from == to {
		return nil, nil
	}
	source, destination := graph.banks.nodes[from], graph.banks.nodes[to]
	search := func(from, to int, allowed map[int]bool) ([]Lane, error) {
		part := input
		part.from, part.to = n.Nodes[from].ID, n.Nodes[to].ID
		part.bankRaw, part.allowedLanes = true, allowed
		part.bankExternal = allowed == nil
		return n.frozenRoute(part, graph, counts)
	}
	if source >= 0 && source == destination {
		bank := graph.banks.banks[source]
		if to == bank.exit || from == bank.entry || graph.berthStations[from] < 0 && to != bank.entry {
			allowed := bank.arrival
			if to == bank.exit {
				allowed = bank.departure
				if from == bank.entry {
					allowed = make(map[int]bool)
					for lane := range bank.local {
						if n.Lanes[lane].StationRole == StationThroughRole {
							allowed[lane] = true
						}
					}
				}
			}
			if route, err := search(from, to, allowed); err == nil {
				return route, nil
			}
			counts.localFailed++
			if input.forbidden != nil || from == bank.entry || bankArrivalNode(bank, graph, from) {
				return nil, ErrUnreachable
			}
		}
	}
	var prefix, suffix []Lane
	var err error
	if source >= 0 {
		bank := graph.banks.banks[source]
		if from != bank.entry && from != bank.exit {
			prefix, err = search(from, bank.exit, bank.departure)
			if err != nil {
				return nil, err
			}
			from = bank.exit
		}
	}
	suffixFrom, suffixTo := -1, to
	if destination >= 0 {
		bank := graph.banks.banks[destination]
		if to != bank.entry && to != bank.exit {
			suffixFrom, to = bank.entry, bank.entry
		}
	}
	if input.forbidden != nil && (len(prefix) > 0 || suffixFrom >= 0) {
		return nil, ErrUnreachable
	}
	middle, err := search(from, to, nil)
	if err != nil {
		return nil, err
	}
	if suffixFrom >= 0 {
		suffix, err = search(suffixFrom, suffixTo, graph.banks.banks[destination].arrival)
		if err != nil {
			return nil, err
		}
	}
	route := make([]Lane, 0, len(prefix)+len(middle)+len(suffix))
	route = append(route, prefix...)
	route = append(route, middle...)
	return append(route, suffix...), nil
}

// frozenCost is the cost loop of bankNearest from before it read lane indexes.
func frozenCost(route []Lane, graph routeGraph) float64 {
	cost := 0.0
	for _, lane := range route {
		cost += graph.edges[graph.lanes[lane.ID]].seconds
	}
	return cost
}

// classBankNetwork returns a network with banks where group and express pods
// have different permissions. A class set that is empty allows only the default
// classes, so each part names its classes. Station m2 and its gates are not for
// express pods, station m4 and its gates are not for group pods, and a bypass lane
// for the other class passes each. The through lane of m6 is for group pods, so that an express
// pod must go through a berth. The berths of m0 alternate between the classes.
func classBankNetwork(t *testing.T) Network {
	t.Helper()
	group, express := largeGeometryClasses(t, "legacy", "group"), largeGeometryClasses(t, "legacy", "express")
	all := largeGeometryClasses(t, "legacy", "compact", "group", "express")
	network := missNetwork(8, 4, 16)
	for index := range network.Lanes {
		network.Lanes[index].VehicleClasses = all
	}
	for stationIndex := range network.Stations {
		station := &network.Stations[stationIndex]
		station.VehicleClasses = all
		for position := range station.Berths {
			station.Berths[position].VehicleClasses = all
		}
	}
	network.Stations[2].VehicleClasses = group
	network.Stations[4].VehicleClasses = express
	for position := range network.Stations[0].Berths {
		network.Stations[0].Berths[position].VehicleClasses = group
		if position%2 == 1 {
			network.Stations[0].Berths[position].VehicleClasses = express
		}
	}
	for index, lane := range network.Lanes {
		if lane.ID == "m6-through" {
			network.Lanes[index].VehicleClasses = group
		}
	}
	network.Lanes = append(network.Lanes,
		Lane{ID: "bypass-m2", From: "m1-after", To: "m2-after", SpeedLimit: 14, VehicleClasses: express, Control: &Point{X: 2300, Y: -400}},
		Lane{ID: "bypass-m4", From: "m3-after", To: "m4-after", SpeedLimit: 14, VehicleClasses: group, Control: &Point{X: 4300, Y: -400}},
	)
	return network
}

// routeReferenceNetworks returns the networks of the route differential.
func routeReferenceNetworks(t *testing.T) map[string]Network {
	t.Helper()
	return map[string]Network{"bank example": BankExample(), "no candidate": missNetwork(8, 5, 16), "classes": classBankNetwork(t)}
}

// retainedRoute is a route that a test holds across later searches.
type retainedRoute struct {
	got, want []Lane
	name      string
}

// TestBankRouteMatchesFrozen compares each route with the frozen route: each
// lane, the error text, the cost bits, and the counts of the searches. One work
// value serves all searches, and the test reads each returned route again after
// the later searches, so a route that shares storage with the work would change.
func TestBankRouteMatchesFrozen(t *testing.T) {
	t.Parallel()
	for name, network := range routeReferenceNetworks(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(len(network.Nodes)), 8))
			graph := newRouteGraph(network)
			if graph.banks.err != nil {
				t.Fatal(graph.banks.err)
			}
			var work routeSearchWork
			var retained []retainedRoute
			routes, multiple, failed := 0, 0, 0
			forbidden := map[string]bool{network.Nodes[len(network.Nodes)/2].ID: true}
			inputs := []networkRouteInput{{}, {terminalBerthsOnly: true}, {ownBerthsOnly: true}, {forbidden: forbidden}}
			for set, blocked := range blockedRouteSets(network, rng) {
				graph.blocked = blocked
				for _, class := range []VehicleClass{"", GroupClass, ExpressClass} {
					for shape, base := range inputs {
						for range 150 {
							input := base
							input.class = class
							input.from, input.to = network.Nodes[rng.IntN(len(network.Nodes))].ID, network.Nodes[rng.IntN(len(network.Nodes))].ID
							label := fmt.Sprintf("set %d, class %q, shape %d, %s to %s", set, class, shape, input.from, input.to)
							var counts frozenCounts
							want, wantErr := network.frozenRoute(input, graph, &counts)
							searches, localFailed := work.searches, work.localFailed
							got, gotErr := network.routeIndexedWithWork(input, graph, &work)
							if errorText(gotErr) != errorText(wantErr) {
								t.Fatalf("%s: error %v, want %v", label, gotErr, wantErr)
							}
							if (got == nil) != (want == nil) || !slices.Equal(got, want) {
								t.Fatalf("%s: route %v, want %v", label, laneIDs(got), laneIDs(want))
							}
							if a, b := math.Float64bits(frozenCost(got, graph)), math.Float64bits(frozenCost(want, graph)); a != b {
								t.Fatalf("%s: cost bits %x, want %x", label, a, b)
							}
							if work.searches-searches != counts.searches || work.localFailed-localFailed != counts.localFailed {
								t.Fatalf("%s: %d searches and %d local failures, want %d and %d", label, work.searches-searches, work.localFailed-localFailed, counts.searches, counts.localFailed)
							}
							if wantErr != nil {
								failed++
							}
							if len(want) > 1 {
								multiple++
								retained = append(retained, retainedRoute{got: got, want: slices.Clone(want), name: label})
							}
							routes++
						}
					}
				}
				for _, route := range retained {
					if !slices.Equal(route.got, route.want) {
						t.Fatalf("%s: a later search changes the route: %v, want %v", route.name, laneIDs(route.got), laneIDs(route.want))
					}
				}
			}
			if multiple == 0 || failed == 0 {
				t.Fatalf("%d routes: %d of more than one lane and %d errors, want some of each", routes, multiple, failed)
			}
		})
	}
}

// TestBankPathsMatchFrozen compares the path of each candidate of one call, as
// bankNearest makes it with its shared parts, with the frozen route of that
// candidate: each lane, the error text, and the cost bits. It does not compare
// the search counts, because the shared parts save searches. A reverse call has
// no shared parts, so it also compares the counts. Each candidate follows the
// others in the call, so a slot that holds the wrong lanes or shares them with
// the work changes a later candidate.
func TestBankPathsMatchFrozen(t *testing.T) {
	t.Parallel()
	for name, network := range routeReferenceNetworks(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(len(network.Nodes)), 9))
			graph := newRouteGraph(network)
			found, missed, shared := 0, 0, 0
			for set, blocked := range blockedRouteSets(network, rng) {
				graph.blocked = blocked
				for _, class := range []VehicleClass{"", GroupClass, ExpressClass} {
					for range 6 {
						origin := rng.IntN(len(network.Nodes))
						for _, reverse := range []bool{false, true} {
							var work routeSearchWork
							var terminal, open *bankRouteParts
							if !reverse {
								terminal = &bankRouteParts{middle: make([]bankRoutePart, len(graph.banks.banks))}
								open = &bankRouteParts{middle: make([]bankRoutePart, len(graph.banks.banks))}
							}
							for node := range network.Nodes {
								from, to := origin, node
								if reverse {
									from, to = to, from
								}
								label := fmt.Sprintf("set %d, class %q, %s to %s", set, class, network.Nodes[from].ID, network.Nodes[to].ID)
								var counts frozenCounts
								input := networkRouteInput{from: network.Nodes[from].ID, to: network.Nodes[to].ID, class: class}
								strict := input
								strict.terminalBerthsOnly = true
								want, wantErr := network.frozenRoute(strict, graph, &counts)
								if errors.Is(wantErr, ErrUnreachable) {
									want, wantErr = network.frozenRoute(input, graph, &counts)
								}
								searches := work.searches
								path, err := network.bankPathNodes(networkRouteInput{terminalBerthsOnly: true, class: class, parts: terminal}, from, to, graph, &work)
								if errors.Is(err, ErrUnreachable) {
									path, err = network.bankPathNodes(networkRouteInput{class: class, parts: open}, from, to, graph, &work)
								}
								if errorText(err) != errorText(wantErr) {
									t.Fatalf("%s: error %v, want %v", label, err, wantErr)
								}
								got := make([]Lane, len(path))
								cost := 0.0
								for index, lane := range path {
									got[index] = network.Lanes[lane]
									cost += graph.edges[lane].seconds
								}
								if !slices.Equal(got, want) {
									t.Fatalf("%s: path %v, want %v", label, laneIDs(got), laneIDs(want))
								}
								if a, b := math.Float64bits(cost), math.Float64bits(frozenCost(want, graph)); a != b {
									t.Fatalf("%s: cost bits %x, want %x", label, a, b)
								}
								if reverse && work.searches-searches != counts.searches {
									t.Fatalf("%s: %d searches, want %d", label, work.searches-searches, counts.searches)
								}
								if wantErr == nil {
									found++
								} else {
									missed++
								}
								if !reverse && counts.searches > work.searches-searches {
									shared++
								}
							}
						}
					}
				}
			}
			if found == 0 || missed == 0 || shared == 0 {
				t.Fatalf("%d paths, %d errors, and %d with shared searches, want some of each", found, missed, shared)
			}
		})
	}
}

// TestClassBankNetworkSeparatesClasses checks that the classes of the fixture get
// different routes, so that the comparisons of the fixture depend on the class.
func TestClassBankNetworkSeparatesClasses(t *testing.T) {
	t.Parallel()
	network := classBankNetwork(t)
	graph := newRouteGraph(network)
	if graph.banks.err != nil {
		t.Fatal(graph.banks.err)
	}
	routes := func(class VehicleClass, from, to string) string {
		var counts frozenCounts
		route, err := network.frozenRoute(networkRouteInput{from: from, to: to, class: class, terminalBerthsOnly: true}, graph, &counts)
		return fmt.Sprint(laneIDs(route), err)
	}
	for _, test := range []struct{ name, from, to string }{
		{"lane and gate of a group station", "m1-after", "m3-entry"},
		{"lane and gate of an express station", "m3-after", "m5-entry"},
		{"through lane", "m5-after", "m7-entry"},
		{"group station berth", "m1-after", "m2-1"},
		{"express station berth", "m3-after", "m4-1"},
		{"berth", "return-end", "m0-1"},
	} {
		if group, express := routes(GroupClass, test.from, test.to), routes(ExpressClass, test.from, test.to); group == express {
			t.Errorf("%s: both classes get %s", test.name, group)
		}
	}
}

// TestBankNearestUsesClass compares bankNearest with the reference for each
// class on the class fixture, and checks that the classes choose different
// nodes.
func TestBankNearestUsesClass(t *testing.T) {
	t.Parallel()
	network := classBankNetwork(t)
	graph := newRouteGraph(network)
	different := 0
	for _, rank := range append(berthRanks(network, graph), gateRanks(network, graph)...) {
		for _, from := range network.Nodes {
			var nodes [2]int
			for index, class := range []VehicleClass{GroupClass, ExpressClass} {
				input := preferredNearestInput{from: from.ID, rank: rank, class: class}
				want, wantOK := network.referenceBankNearest(input, graph)
				got, gotOK := network.bankNearest(input, graph)
				if got != want || gotOK != wantOK {
					t.Fatalf("class %s from %s: got %d, %t, want %d, %t", class, from.ID, got, gotOK, want, wantOK)
				}
				nodes[index] = got
			}
			if nodes[0] != nodes[1] {
				different++
			}
		}
	}
	if different == 0 {
		t.Fatal("the classes always choose the same node")
	}
}

// TestNearestFreeBerthUsesClassOnBanks compares the choice of group and express
// pods with the reference, from each node, on the class fixture.
func TestNearestFreeBerthUsesClassOnBanks(t *testing.T) {
	t.Parallel()
	network := classBankNetwork(t)
	s, err := NewFleet(network, []Placement{
		{ID: "01", StationID: "m0", BerthID: "m0-1"},
		{ID: "02", StationID: "m0", BerthID: "m0-2"},
		{ID: "03", StationID: "m6", BerthID: "m6-1"},
		{ID: "04", StationID: "m6", BerthID: "m6-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only the class of the choice matters here, so the pods need no profile
	// of their class.
	group, express := s.findVehicle("01"), s.findVehicle("02")
	group.Pod.Class, express.Pod.Class = GroupClass, ExpressClass
	different := 0
	for _, node := range network.Nodes {
		var choices [2]string
		for index, v := range []*vehicle{group, express} {
			berth, station, ok := s.nearestFreeBerth(v, node.ID)
			wantBerth, wantStation, wantOK := s.referenceNearestFreeBerth(v, node.ID)
			if berth != wantBerth || station != wantStation || ok != wantOK {
				t.Fatalf("pod %s from %s: got %q in %q, %t, want %q in %q, %t", v.Pod.ID, node.ID, berth.ID, station, ok, wantBerth.ID, wantStation, wantOK)
			}
			choices[index] = berth.ID
		}
		if choices[0] != choices[1] {
			different++
		}
	}
	if different == 0 {
		t.Fatal("the classes always choose the same berth")
	}
}
