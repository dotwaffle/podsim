package sim

import (
	"fmt"
	"slices"
)

// routeTreeLimit is the most trees that routeTrees keeps, and routeTreeBytes
// bounds the lanes of the trees. A tree takes 4 bytes for each node, so 512
// trees of LondonFull (5,740 nodes) take 11.8 MB, and a network at the limit
// of 12,000 nodes keeps 349 trees in 16 MiB. The storage grows with the
// trees.
const (
	routeTreeLimit = 512
	routeTreeBytes = 16 << 20
)

// routeTreeKey selects the search of a tree: the root node, the class test,
// and the terminal-berth rule. The graph is the routing graph of the route
// epoch, and the network does not change.
type routeTreeKey struct {
	root     int32
	classes  classTest
	terminal bool
}

// routeTrees keeps the shortest-path trees of the batches of
// cacheStationRoutesForClass. The pickup swap pass asks for the routes from a
// few origins to the berths of many stations, and each batch from an origin
// makes most of the same tree again.
//
// A tree is the search with no target, so the route to each node is the
// route of a search that stops at the node. That search is the first part of
// the tree search, with the same pops and the same writes: a pop is never
// below the pop before it, a lane cost is not negative, and the relaxation
// is strict, so a settled node keeps its distance and its previous lane. Thus
// a route from the memo is the route of a new search, and the memo changes no
// result.
//
// The trees depend on the blocked lanes, so startRouteEpoch clears the memo.
// Clone drops it, and no save holds it. A batch under a routing view reads and
// writes it, as a batch outside a view does.
type routeTrees struct {
	// nodes is the node count of the network, and limit is the most trees.
	nodes, limit int
	// slots gives the slot of each key in the memo.
	slots map[routeTreeKey]int32
	// keys holds the key of each slot that holds a tree.
	keys []routeTreeKey
	// previous holds, from index slot*nodes, the last lane of the route to
	// each node in the tree of the slot. It holds -1 for the root and for each
	// node that the search does not reach.
	previous []int32
	// next is the slot of the next new tree. The slots fill in order, and then
	// a new tree replaces the oldest tree, so the memo is first in, first out.
	next int
}

// newRouteTrees returns an empty memo for a network of nodes nodes.
func newRouteTrees(nodes int) *routeTrees {
	limit := routeTreeLimit
	if nodes > 0 {
		limit = max(2, min(routeTreeLimit, routeTreeBytes/(4*nodes)))
	}
	return &routeTrees{nodes: nodes, limit: limit}
}

// routeTreeInput is the search of one tree.
type routeTreeInput struct {
	root     int
	class    VehicleClass
	terminal bool
}

// tree returns the tree of input, and makes the search on a miss. graph must
// be the routing graph of the simulation of t. The result is a part of the
// memo, and it stays valid only until the next call of tree. A hit does not
// move next, so a hit on the oldest tree returns the slot that the next miss
// writes.
func (t *routeTrees) tree(n Network, graph routeGraph, input routeTreeInput) []int32 {
	key := routeTreeKey{root: int32(input.root), classes: graph.classTestFor(input.class), terminal: input.terminal} // #nosec G115 -- A node index is below MaxNodes.
	if slot, ok := t.slots[key]; ok {
		return t.slot(int(slot))
	}
	slot := t.next
	if len(t.keys) < t.limit {
		if t.slots == nil {
			t.slots = make(map[routeTreeKey]int32)
		}
		t.keys = append(t.keys, key)
		t.grow()
	} else {
		delete(t.slots, t.keys[slot])
		t.keys[slot] = key
	}
	t.next = (slot + 1) % t.limit
	t.slots[key] = int32(slot) // #nosec G115 -- A slot is below routeTreeLimit.
	tree := t.slot(slot)
	work := takeBankSearchWork()
	search := networkRouteInput{class: input.class, terminalBerthsOnly: input.terminal, bankExternal: len(graph.banks.banks) > 0}
	n.searchFromStops(search, input.root, -1, nil, graph, work)
	for node, lane := range work.previous {
		tree[node] = int32(lane) // #nosec G115 -- A lane index is below MaxLanes.
	}
	bankSearchWork.Put(work)
	return tree
}

// grow adds the storage of one tree to previous. The capacity doubles up to
// the storage of limit trees. A clone that makes few trees, such as the
// clone of a trip check, thus allocates little. An earlier array keeps the
// trees that a caller still reads.
func (t *routeTrees) grow() {
	size := len(t.previous) + t.nodes
	if size > cap(t.previous) {
		grown := make([]int32, size, min(t.limit*t.nodes, max(size, 2*cap(t.previous))))
		copy(grown, t.previous)
		t.previous = grown
	}
	t.previous = t.previous[:size]
}

// slot returns the tree of a slot.
func (t *routeTrees) slot(slot int) []int32 {
	return t.previous[slot*t.nodes : (slot+1)*t.nodes]
}

// treeLanes returns the route from node from, the root of tree, to node to.
// It gives the result of routeLanes for the search of the tree.
func (n Network) treeLanes(graph routeGraph, tree []int32, from, to int) ([]Lane, error) {
	count := 0
	for current := to; current != from; count++ {
		lane := tree[current]
		if lane < 0 {
			return nil, ErrUnreachable
		}
		current = graph.edges[lane].from
	}
	if count == 0 {
		return nil, nil
	}
	route := make([]Lane, count)
	for current := to; current != from; {
		lane := tree[current]
		count--
		route[count] = n.Lanes[lane]
		current = graph.edges[lane].from
	}
	return route, nil
}

// appendTreePath adds the lane indexes of the route from node from, the root
// of tree, to node to, in route order, to path. It gives the result of
// (*routeSearchWork).appendPath for the search of the tree. On an error, it
// adds nothing.
func appendTreePath(path []int, graph routeGraph, tree []int32, from, to int) ([]int, error) {
	start := len(path)
	for current := to; current != from; {
		lane := tree[current]
		if lane < 0 {
			return path[:start], ErrUnreachable
		}
		path = append(path, int(lane))
		current = graph.edges[lane].from
	}
	slices.Reverse(path[start:])
	return path, nil
}

// treeTargets returns the routes of routesFromTargets for preferredTargets
// of input, a forward search. With trees, it reads the terminal-berth search
// from the memo. A target that the tree does not reach takes the fallback
// search of preferredTargets.
func (n Network) treeTargets(input routeTargetsInput, graph routeGraph, trees *routeTrees) []routeResult {
	start, ok := graph.nodes[input.from]
	if trees == nil || !ok || !graph.nodeAllows(start, input.class) {
		return n.routesFromTargets(n.preferredTargets(input, graph), graph)
	}
	results := make([]routeResult, len(input.to))
	var tree []int32
	var fallback []string
	var indexes []int
	for index, id := range input.to {
		goal, found := graph.nodes[id]
		if !found || !graph.nodeAllows(goal, input.class) {
			results[index].err = fmt.Errorf("unknown destination %q", id)
			continue
		}
		if tree == nil {
			tree = trees.tree(n, graph, routeTreeInput{root: start, class: input.class, terminal: true})
		}
		lanes, err := n.treeLanes(graph, tree, start, goal)
		if err != nil {
			fallback, indexes = append(fallback, id), append(indexes, index)
			continue
		}
		results[index].lanes = lanes
	}
	if len(fallback) > 0 {
		input.to, input.terminalBerthsOnly = fallback, false
		for index, result := range n.routesFromTargets(n.routeTargets(input, graph), graph) {
			results[indexes[index]] = result
		}
	}
	return results
}

// batchTrees returns the tree memo of the batches of
// cacheStationRoutesForClass. A batch under a routing view reads and writes it
// too.
func (s *Simulation) batchTrees() *routeTrees {
	if s.routeTrees == nil {
		s.routeTrees = newRouteTrees(len(s.network.Nodes))
	}
	return s.routeTrees
}
