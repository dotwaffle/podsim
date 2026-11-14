package sim

import (
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// cloneRouteTrees returns a copy of a tree memo.
func cloneRouteTrees(trees *routeTrees) *routeTrees {
	if trees == nil {
		return nil
	}
	c := *trees
	c.slots, c.keys, c.previous = maps.Clone(trees.slots), slices.Clone(trees.keys), slices.Clone(trees.previous)
	return &c
}

// freshTree returns the tree of a new search with no memo.
func freshTree(n Network, graph routeGraph, input routeTreeInput) []int32 {
	var work routeSearchWork
	n.searchFromStops(networkRouteInput{class: input.class, terminalBerthsOnly: input.terminal, bankExternal: len(graph.banks.banks) > 0}, input.root, -1, nil, graph, &work)
	tree := make([]int32, len(work.previous))
	for node, lane := range work.previous {
		tree[node] = int32(lane) // #nosec G115 -- A test lane index is small.
	}
	return tree
}

// sameResults reports whether two batches give the same lanes and the same
// error text.
func sameResults(a, b []routeResult) bool {
	return slices.EqualFunc(a, b, func(x, y routeResult) bool {
		return reflect.DeepEqual(x.lanes, y.lanes) && fmt.Sprint(x.err) == fmt.Sprint(y.err)
	})
}

// TestRouteTreesMatchSearch compares each batch that reads the tree memo with
// the batch with no memo, on networks with and without station banks, with
// class rules, blocked sets, unknown nodes, and the fallback of legacy maps.
// One memo serves each blocked set, as one route epoch, for all origins and
// classes. A memo of 2 trees also checks that eviction keeps the results.
func TestRouteTreesMatchSearch(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	networks := map[string]Network{
		"shortcuts": intermediateBerthNetwork(), "legacy": ladderNetwork(), "equal-cost": gridNetwork(4),
		"line classes": altLineFleetClasses(t, -100, compact, "s1-1").network,
	}
	maps.Copy(networks, routeReferenceNetworks(t))
	for name, network := range networks {
		for _, limit := range []int{routeTreeLimit, 2} {
			t.Run(fmt.Sprintf("%s, limit %d", name, limit), func(t *testing.T) {
				t.Parallel()
				checkTreeBatches(t, network, limit)
			})
		}
	}
}

// checkTreeBatches runs the batches of TestRouteTreesMatchSearch on one
// network with a memo of limit trees.
func checkTreeBatches(t *testing.T, network Network, limit int) {
	t.Helper()
	rng := rand.New(rand.NewPCG(uint64(len(network.Nodes)), 12))
	graph := newRouteGraph(network)
	if graph.banks.err != nil {
		t.Fatal(graph.banks.err)
	}
	banks := len(graph.banks.banks) > 0
	var targetSets [][]string
	for _, station := range network.Stations {
		var targets []string
		for _, berth := range station.Berths {
			targets = append(targets, berth.Node)
		}
		targetSets = append(targetSets, targets)
	}
	if !banks {
		var all []string
		for _, node := range network.Nodes {
			all = append(all, node.ID)
		}
		targetSets = append(targetSets, append(all, "missing"))
	}
	batches, trees := 0, 0
	for set, blocked := range append(blockedRouteSets(network, rng), tieBlockedSets(network)...) {
		graph.blocked = blocked
		memo := newRouteTrees(len(network.Nodes))
		memo.limit = limit
		froms := rng.Perm(len(network.Nodes))
		if len(froms) > 40 {
			froms = froms[:12]
		}
		for _, from := range froms {
			for _, targets := range targetSets {
				for _, class := range []VehicleClass{"", CompactClass, GroupClass, ExpressClass} {
					var want, got []routeResult
					if banks {
						nodes := make([]int, len(targets))
						for index, target := range targets {
							nodes[index] = graph.nodes[target]
						}
						input := networkRouteInput{class: class}
						want, got = network.bankRoutesFrom(input, from, nodes, graph, nil), network.bankRoutesFrom(input, from, nodes, graph, memo)
					} else {
						input := routeTargetsInput{from: network.Nodes[from].ID, to: targets, class: class}
						want, got = network.treeTargets(input, graph, nil), network.treeTargets(input, graph, memo)
					}
					if !sameResults(got, want) {
						t.Fatalf("set %d, from %s to %v, class %q: got %v, want %v", set, network.Nodes[from].ID, targets, class, got, want)
					}
					batches++
				}
			}
		}
		trees += len(memo.keys)
	}
	// A memo that serves no batch, or that evicts no tree, proves nothing. The
	// large memo evicts nothing, so it holds each tree that it built.
	if limit == routeTreeLimit && (trees == 0 || trees >= batches) || limit < routeTreeLimit && trees < 2 {
		t.Fatalf("%d batches left %d trees", batches, trees)
	}
}

// TestRouteTreesEvictOldest checks the eviction order: the memo holds the
// last limit trees that entered it, and a read does not change the order.
func TestRouteTreesEvictOldest(t *testing.T) {
	t.Parallel()
	network := gridNetwork(4)
	graph := newRouteGraph(network)
	trees := newRouteTrees(len(network.Nodes))
	trees.limit = 3
	for step, check := range []struct {
		root int
		held []int
	}{
		{0, []int{0}}, {1, []int{0, 1}}, {2, []int{0, 1, 2}},
		{3, []int{1, 2, 3}}, {4, []int{2, 3, 4}},
		// An evicted tree enters again, and a read keeps the order.
		{0, []int{3, 4, 0}}, {3, []int{3, 4, 0}}, {1, []int{4, 0, 1}},
	} {
		input := routeTreeInput{root: check.root, terminal: true}
		if got := trees.tree(network, graph, input); !slices.Equal(got, freshTree(network, graph, input)) {
			t.Fatalf("step %d: the tree of %d differs from a new search", step, check.root)
		}
		if len(trees.slots) != len(check.held) {
			t.Fatalf("step %d: the memo holds %d keys, want %v", step, len(trees.slots), check.held)
		}
		for _, root := range check.held {
			input := routeTreeInput{root: root, terminal: true}
			slot, ok := trees.slots[routeTreeKey{root: int32(root), classes: graph.classTestFor(""), terminal: true}] // #nosec G115 -- A test node index is small.
			if !ok {
				t.Fatalf("step %d: the memo does not hold %d, want %v", step, root, check.held)
			}
			if !slices.Equal(trees.slot(int(slot)), freshTree(network, graph, input)) {
				t.Fatalf("step %d: the held tree of %d differs from a new search", step, root)
			}
		}
	}
}

// freshRoute returns the route that cachedRouteForClass gives with no memo:
// the search with the terminal-berth rule, and with no rule when that one
// finds no route, on the routing graph.
func freshRoute(s *Simulation, from, to string, class VehicleClass) ([]Lane, error) {
	graph := s.routingGraph()
	lanes, err := s.network.routeIndexed(networkRouteInput{from: from, to: to, class: class, terminalBerthsOnly: true}, graph)
	if errors.Is(err, ErrUnreachable) {
		lanes, err = s.network.routeIndexed(networkRouteInput{from: from, to: to, class: class}, graph)
	}
	return lanes, err
}

// checkRouteMemo compares each entry of the route memo, with its travel
// time, and each tree of the tree memo with a new search of its key on the
// routing graph. It also checks that routeOrder holds each key of the route
// memo once, and no other key.
func checkRouteMemo(t *testing.T, s *Simulation) {
	t.Helper()
	graph := s.routingGraph()
	for key, got := range s.routes {
		want, wantErr := freshRoute(s, key.from, key.to, key.class)
		if key.station {
			want, wantErr = s.network.routeIndexed(networkRouteInput{from: key.from, to: key.to, class: key.class, forbidden: s.stationForbidden}, graph)
		}
		if !sameLanes(got.lanes, want) || fmt.Sprint(got.err) != fmt.Sprint(wantErr) {
			t.Fatalf("memo entry %+v: got %v, %v, want %v, %v", key, laneIDs(got.lanes), got.err, laneIDs(want), wantErr)
		}
		if seconds := s.routeSeconds(motionEnvelopeFor(key.class), want, motionEstimate{}); !got.timed || got.seconds != seconds {
			t.Fatalf("memo entry %+v: got time %v (timed %v), want %v", key, got.seconds, got.timed, seconds)
		}
	}
	order := make(map[routeKey]bool, len(s.routeOrder))
	for _, key := range s.routeOrder {
		if _, ok := s.routes[key]; !ok || order[key] {
			t.Fatalf("routeOrder holds %+v, which is not in the memo or is a repeat", key)
		}
		order[key] = true
	}
	if len(order) != len(s.routes) {
		t.Fatalf("routeOrder holds %d keys, the memo %d", len(order), len(s.routes))
	}
	if s.routeTrees == nil {
		return
	}
	for key, slot := range s.routeTrees.slots {
		classes := []VehicleClass{topologyClass, LegacyClass, CompactClass, GroupClass, ExpressClass}
		index := slices.IndexFunc(classes, func(class VehicleClass) bool { return graph.classTestFor(class) == key.classes })
		if index < 0 {
			t.Fatalf("tree memo key %+v has no class", key)
		}
		input := routeTreeInput{root: int(key.root), class: classes[index], terminal: key.terminal}
		if !slices.Equal(s.routeTrees.slot(int(slot)), freshTree(s.network, graph, input)) {
			t.Fatalf("the held tree of %+v differs from a new search", key)
		}
	}
}

// treeFrom is the origin of the batches of checkStationBatch.
const treeFrom = "s1-exit"

// checkStationBatch runs the batch from treeFrom to the berths of s2 on an
// altLineFleet, and compares each route in the route memo with a new
// search. It also checks that the batch left the tree of treeFrom in the
// tree memo.
func checkStationBatch(t *testing.T, s *Simulation) {
	t.Helper()
	const from = treeFrom
	station, ok := s.station("s2")
	if !ok {
		t.Fatal("no station s2")
	}
	s.cacheStationRoutesForClass(from, station.Berths, "")
	for _, berth := range station.Berths {
		got, ok := s.routes[routeKey{from: from, to: berth.Node}]
		if !ok {
			t.Fatalf("the batch did not store the route from %s to %s", from, berth.Node)
		}
		want, wantErr := freshRoute(s, from, berth.Node, "")
		if !sameLanes(got.lanes, want) || fmt.Sprint(got.err) != fmt.Sprint(wantErr) {
			t.Fatalf("from %s to %s: got %v, %v, want %v, %v", from, berth.Node, laneIDs(got.lanes), got.err, laneIDs(want), wantErr)
		}
	}
	if !holdsTree(s, from) {
		t.Fatalf("the batch left no tree of %s in the memo", from)
	}
}

// holdsTree reports whether the tree memo holds the terminal-berth tree of
// node from for the legacy class.
func holdsTree(s *Simulation, from string) bool {
	node, ok := s.graph.nodes[from]
	if !ok || s.routeTrees == nil {
		return false
	}
	_, ok = s.routeTrees.slots[routeTreeKey{root: int32(node), classes: s.graph.classTestFor(""), terminal: true}] // #nosec G115 -- A test node index is small.
	return ok
}

// TestRouteTreesFollowEpoch checks each event that changes the routing graph,
// and the clone and the restore. Before each event the memo holds a tree that
// the event makes stale: the batch from s1-exit to s2 takes the lane s1-link,
// and the road through alt when s1-link is blocked. After the event, each
// batch route equals a new search.
func TestRouteTreesFollowEpoch(t *testing.T) {
	t.Parallel()
	const from = treeFrom
	// warm returns a fleet whose tree memo holds the tree of from, with the
	// lane s1-link blocked when blocked is true.
	warm := func(t *testing.T, blocked bool) *Simulation {
		t.Helper()
		s := altLineFleet(t, -100, "s1-1")
		if blocked {
			blockLanes(t, s, "s1-link")
		}
		checkStationBatch(t, s)
		if got := s.routes[routeKey{from: from, to: "s2-1"}].lanes; usesLane(got, "s1-link") == blocked {
			t.Fatalf("the warm route %v does not fit the blocked set", laneIDs(got))
		}
		return s
	}
	t.Run("blocked lane appears", func(t *testing.T) {
		t.Parallel()
		s := warm(t, false)
		blockLanes(t, s, "s1-link")
		checkStationBatch(t, s)
	})
	t.Run("blocked lane goes", func(t *testing.T) {
		t.Parallel()
		s := warm(t, true)
		s.setBlocked(nil)
		checkStationBatch(t, s)
	})
	t.Run("debris start and clear", func(t *testing.T) {
		t.Parallel()
		s := warm(t, false)
		length := s.graph.lengths[laneIndex(t, s, "s1-link")]
		id := startDebris(t, s, "s1-link", length/2, length/2+1, 0)
		checkStationBatch(t, s)
		if err := s.ClearFault(id); err != nil {
			t.Fatal(err)
		}
		checkStationBatch(t, s)
	})
	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		s := warm(t, true)
		s.Reset()
		checkStationBatch(t, s)
	})
	t.Run("network rebuild", func(t *testing.T) {
		t.Parallel()
		s := warm(t, false)
		indexes := *s.networkIndexes
		indexes.network.Nodes = append(slices.Clip(indexes.network.Nodes), Node{ID: "extra"})
		s.networkIndexes = &indexes
		s.ensureNetworkIndexes()
		if s.routeTrees != nil {
			t.Fatal("the tree memo stays after a network rebuild")
		}
	})
	t.Run("clone", func(t *testing.T) {
		t.Parallel()
		s := warm(t, true)
		before := cloneRouteTrees(s.routeTrees)
		clone := s.Clone()
		if clone.routeTrees != nil {
			t.Fatal("the clone has the tree memo")
		}
		clone.setBlocked(nil)
		checkStationBatch(t, clone)
		if !reflect.DeepEqual(before, s.routeTrees) {
			t.Error("the clone wrote the tree memo of its source")
		}
	})
	t.Run("restore", func(t *testing.T) {
		t.Parallel()
		// The restore makes a new simulation, so the memo of s does not
		// reach it. The restored batch takes the road through alt.
		s := altLineFleet(t, -100, "s1-1")
		length := s.graph.lengths[laneIndex(t, s, "s1-link")]
		startDebris(t, s, "s1-link", length/2, length/2+1, 0)
		checkStationBatch(t, s)
		restored, _, err := RestoreState(RestoreStateInput{FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}, Network: s.network, Fleet: place("s1-1"), State: s.ExportState()})
		if err != nil {
			t.Fatal(err)
		}
		if !restored.blockedActive() {
			t.Fatal("the restore has lost the blocked set")
		}
		checkStationBatch(t, restored)
	})
}

// TestRouteTreesServeViews checks that a batch under a routing view reads
// and writes the route memos as a batch outside a view does (section 9.1 of
// the incident emergency contract). Each stored route equals a new search,
// and the tree memo holds the tree of the batch. On a network where Compact
// pods take a faster lane that legacy pods cannot use, the tree of one class
// does not serve the batch of the other.
func TestRouteTreesServeViews(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	network := lineNetwork(lineStations(1, 1, 2, 2, 1))
	for index := range network.Lanes {
		if network.Lanes[index].ID == "s1-link" {
			network.Lanes[index].SpeedLimit = 2
		}
	}
	network.Nodes = append(network.Nodes, Node{ID: "alt", Position: Point{X: 825, Y: -100}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "alt-out", From: treeFrom, To: "alt", SpeedLimit: 14, VehicleClasses: compact},
		Lane{ID: "alt-in", From: "alt", To: "s2-entry", SpeedLimit: 14})
	s, err := NewFleet(network, place("s1-1"))
	if err != nil {
		t.Fatal(err)
	}
	station, _ := s.station("s2")
	defer s.leaveRouteView(s.enterRouteView(&s.vehicles[0]))
	checkStationBatch(t, s)
	if !usesLane(s.routes[routeKey{from: treeFrom, to: "s2-1"}].lanes, "s1-link") {
		t.Fatal("the legacy route does not take s1-link")
	}
	s.cacheStationRoutesForClass(treeFrom, station.Berths, CompactClass)
	compactRoute, ok := s.routes[routeKey{from: treeFrom, to: "s2-1", class: routeClass(CompactClass)}]
	if !ok || !usesLane(compactRoute.lanes, "alt-out") {
		t.Fatalf("the Compact batch stored %v, want a route through alt-out", laneIDs(compactRoute.lanes))
	}
	// The Compact tree is now in the memo. A new legacy batch from the same
	// root must not take it.
	s.startRouteEpoch()
	s.cacheStationRoutesForClass(treeFrom, station.Berths, CompactClass)
	s.cacheStationRoutesForClass(treeFrom, station.Berths, "")
	checkRouteMemo(t, s)
	if len(s.routes) != 2*len(station.Berths) {
		t.Fatalf("the batches stored %d routes, want %d", len(s.routes), 2*len(station.Berths))
	}
}

// TestRouteTreesKeepHeldTree checks that a batch of bankRoutesFrom reads the
// tree of its own key after a later tree enters the memo. On tieBankNetwork,
// the way through the berth plain-1 is the cheapest way past m2, so the tree
// with the terminal-berth rule and the tree without it differ after m2. The
// station far, after m3, has no banks, so each of its berths reads the tree
// of the batch. A blocked lane makes far-1 unreachable. The batch from
// m0-after to far thus reads the terminal tree for far-1, adds the open tree
// for the fallback of far-1, and reads the terminal tree again for far-2.
// Before the batch, the memo is full, and the next new tree replaces the
// terminal tree.
func TestRouteTreesKeepHeldTree(t *testing.T) {
	t.Parallel()
	network := tieBankNetwork()
	// A fleet does not accept the lane of no length or the slow lanes.
	extra := func(id string) bool {
		return strings.HasPrefix(id, "zero") || strings.HasPrefix(id, "slow") || strings.HasPrefix(id, "hidden")
	}
	network.Nodes = slices.DeleteFunc(network.Nodes, func(node Node) bool { return extra(node.ID) })
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane Lane) bool { return extra(lane.ID) })
	// The station plain moves away from the diamond of m2, which becomes
	// slow, so the way through plain-1 stays the cheapest way.
	for index, node := range network.Nodes {
		if position, ok := map[string]Point{"plain-entry": {X: 2820, Y: 150}, "plain-1": {X: 2850, Y: 190}, "plain-exit": {X: 2880, Y: 150}}[node.ID]; ok {
			network.Nodes[index].Position = position
		}
	}
	for index, lane := range network.Lanes {
		if strings.HasPrefix(lane.ID, "m2-up-") || strings.HasPrefix(lane.ID, "m2-down-") {
			network.Lanes[index].SpeedLimit = 2
		}
	}
	network.Nodes = append(network.Nodes,
		Node{ID: "far-entry", Position: Point{X: 3810, Y: -200}}, Node{ID: "far-1", Position: Point{X: 3850, Y: -240}},
		Node{ID: "far-2", Position: Point{X: 3850, Y: -160}}, Node{ID: "far-exit", Position: Point{X: 3890, Y: -200}})
	for _, lane := range [][2]string{{"m3-after", "far-entry"}, {"far-entry", "far-1"}, {"far-entry", "far-2"}, {"far-1", "far-exit"}, {"far-2", "far-exit"}, {"far-exit", "m3-join"}} {
		network.Lanes = append(network.Lanes, Lane{ID: lane[0] + "-" + lane[1], From: lane[0], To: lane[1], SpeedLimit: 14})
	}
	network.Lanes = append(network.Lanes, Lane{ID: "far-through", From: "far-entry", To: "far-exit", SpeedLimit: 14})
	network.Stations = append(network.Stations, Station{
		ID: "far", Name: "far", Entry: "far-entry", Exit: "far-exit",
		Berths: []Berth{{ID: "far-1", Node: "far-1"}, {ID: "far-2", Node: "far-2"}},
	})
	s, err := NewFleet(network, place("m0-1"))
	if err != nil {
		t.Fatal(err)
	}
	blockLanes(t, s, "far-entry-far-1")
	const from = "m0-after"
	trees := s.batchTrees()
	trees.limit = 2
	m4, _ := s.station("m4")
	far, _ := s.station("far")
	// The first batch adds the terminal tree of from, and the second batch
	// the tree of another root.
	s.cacheStationRoutesForClass(from, m4.Berths, "")
	s.cacheStationRoutesForClass("m1-after", m4.Berths, "")
	key := routeTreeKey{root: int32(s.graph.nodes[from]), classes: s.graph.classTestFor(""), terminal: true} // #nosec G115 -- A test node index is small.
	open := key
	open.terminal = false
	if slot, ok := trees.slots[key]; len(trees.keys) != trees.limit || !ok || int(slot) != trees.next {
		t.Fatalf("the memo holds %d of %d trees, and the terminal tree of %s is in slot %d (held %v), want the next slot %d", len(trees.keys), trees.limit, from, slot, ok, trees.next)
	}
	if _, ok := trees.slots[open]; ok {
		t.Fatalf("the memo already holds the open tree of %s", from)
	}
	s.cacheStationRoutesForClass(from, far.Berths, "")
	if _, ok := trees.slots[open]; !ok {
		t.Fatalf("the batch did not add the open tree of %s", from)
	}
	for _, berth := range far.Berths {
		got, ok := s.routes[routeKey{from: from, to: berth.Node}]
		if !ok {
			t.Fatalf("the batch did not store the route to %s", berth.Node)
		}
		want, wantErr := freshRoute(s, from, berth.Node, "")
		if !sameLanes(got.lanes, want) || fmt.Sprint(got.err) != fmt.Sprint(wantErr) {
			t.Errorf("from %s to %s: got %v, %v, want %v, %v", from, berth.Node, laneIDs(got.lanes), got.err, laneIDs(want), wantErr)
		}
	}
	// The open tree of from reaches far-2 through plain-1, and the terminal
	// tree does not.
	graph := s.routingGraph()
	node := func(id string) int { return s.graph.nodes[id] }
	terminalPath, err := appendTreePath(nil, graph, freshTree(s.network, graph, routeTreeInput{root: node(from), terminal: true}), node(from), node("far-2"))
	if err != nil {
		t.Fatal(err)
	}
	openPath, err := appendTreePath(nil, graph, freshTree(s.network, graph, routeTreeInput{root: node(from)}), node(from), node("far-2"))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(terminalPath, openPath) {
		t.Error("the two trees of the fixture give the same path to far-2")
	}
	checkRouteMemo(t, s)
}
