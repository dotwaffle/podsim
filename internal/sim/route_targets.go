package sim

import (
	"errors"
	"fmt"
	"math"
)

type routeTargetsInput struct {
	from               string
	to                 []string
	terminalBerthsOnly bool
	reverse            bool
}

// routeTargetResult retains one target's cost and its search tree. Cost-only
// callers do not need to allocate the lanes of every candidate route.
type routeTargetResult struct {
	from, to int
	seconds  float64
	work     *routeSearchWork
	reverse  bool
	err      error
}

// routeTargets searches all targets with the same costs and tie order as
// routeIndexed in the forward direction. A terminal berth can finish
// a route but cannot extend it. Reverse results remain separate from the
// forward route cache.
func (n Network) routeTargets(input routeTargetsInput, graph routeGraph) []routeTargetResult {
	results := make([]routeTargetResult, len(input.to))
	start, ok := graph.nodes[input.from]
	if !ok {
		for index := range results {
			results[index].err = fmt.Errorf("unknown origin %q", input.from)
		}
		return results
	}
	goals := make([]bool, len(n.Nodes))
	remaining := 0
	for index, id := range input.to {
		goal, found := graph.nodes[id]
		if !found {
			results[index].err = fmt.Errorf("unknown destination %q", id)
			continue
		}
		results[index].from, results[index].to = start, goal
		results[index].reverse = input.reverse
		if !goals[goal] {
			goals[goal] = true
			remaining++
		}
	}
	if remaining == 0 {
		return results
	}
	work := new(routeSearchWork)
	work.reset(len(n.Nodes))
	distance, previous, visited := work.distance, work.previous, work.visited
	distance[start] = 0
	queue := routeQueue{{node: start}}
	adjacent := graph.outgoing
	if input.reverse {
		adjacent = graph.incoming
	}
	for remaining > 0 && len(queue) > 0 {
		item := queue.pop()
		if visited[item.node] || item.distance != distance[item.node] {
			continue
		}
		if goals[item.node] {
			remaining--
			if remaining == 0 {
				break
			}
		}
		visited[item.node] = true
		if input.terminalBerthsOnly && item.node != start && graph.berthStations[item.node] >= 0 {
			continue
		}
		for _, laneIndex := range adjacent[item.node] {
			edge := graph.edges[laneIndex]
			next := edge.to
			if input.reverse {
				next = edge.from
			}
			candidate := item.distance + edge.seconds
			if candidate < distance[next] {
				distance[next], previous[next] = candidate, laneIndex
				queue.push(routeQueueItem{node: next, distance: candidate})
			}
		}
	}
	for index := range results {
		result := &results[index]
		if result.err != nil {
			continue
		}
		result.seconds, result.work = distance[result.to], work
		if math.IsInf(result.seconds, 1) {
			result.err = ErrUnreachable
		}
	}
	return results
}

// preferredTargets keeps a terminal-berth route whenever it exists. An
// unreachable target retains its original unrestricted route for legacy maps.
func (n Network) preferredTargets(input routeTargetsInput, graph routeGraph) []routeTargetResult {
	input.terminalBerthsOnly = true
	results := n.routeTargets(input, graph)
	var fallback []string
	var indexes []int
	for index, result := range results {
		if errors.Is(result.err, ErrUnreachable) {
			fallback = append(fallback, input.to[index])
			indexes = append(indexes, index)
		}
	}
	if len(fallback) > 0 {
		input.to, input.terminalBerthsOnly = fallback, false
		for index, result := range n.routeTargets(input, graph) {
			results[indexes[index]] = result
		}
	}
	return results
}

// targetRoute follows a target's own tree. Reverse searches record the
// next forward lane, so their traversal already follows travel order.
func (n Network) targetRoute(target routeTargetResult, graph routeGraph) ([]Lane, error) {
	if !target.reverse {
		return n.routeLanes(graph, target.from, target.to, target.work.distance, target.work.previous)
	}
	var route []Lane
	for node := target.to; node != target.from; {
		lane := target.work.previous[node]
		if lane < 0 {
			return nil, ErrUnreachable
		}
		route = append(route, n.Lanes[lane])
		node = graph.edges[lane].to
	}
	return route, nil
}

func (n Network) routesFromTargets(targets []routeTargetResult, graph routeGraph) []routeResult {
	results := make([]routeResult, len(targets))
	for index, target := range targets {
		results[index].err = target.err
		if target.err == nil {
			results[index].lanes, results[index].err = n.targetRoute(target, graph)
		}
	}
	return results
}

type preferredNearestInput struct {
	from string
	rank []int
	// limit is an inclusive free-flow bound. Zero means no bound.
	limit   float64
	reverse bool
}

// preferredNearestIndexed ranks each goal by its own preferred or fallback
// free-flow cost. Rank breaks ties. A preferred route is not a global tier.
func (n Network) preferredNearestIndexed(input preferredNearestInput, graph routeGraph) (int, bool) {
	var goals []string
	for node, rank := range input.rank {
		if rank >= 0 {
			goals = append(goals, n.Nodes[node].ID)
		}
	}
	targets := n.preferredTargets(routeTargetsInput{from: input.from, to: goals, reverse: input.reverse}, graph)
	if input.reverse {
		return n.preferredSource(input, targets, graph)
	}
	best, bestSeconds := -1, math.Inf(1)
	for _, target := range targets {
		if target.err != nil || input.limit > 0 && target.seconds > input.limit {
			continue
		}
		if best < 0 || target.seconds < bestSeconds || target.seconds == bestSeconds && input.rank[target.to] < input.rank[best] {
			best, bestSeconds = target.to, target.seconds
		}
	}
	return best, best >= 0
}

// preferredSource checks near-minimum reverse costs in the forward
// direction. Addition order can change floating-point ties or a reach
// boundary. Only the short list needs a forward search.
func (n Network) preferredSource(input preferredNearestInput, targets []routeTargetResult, graph routeGraph) (int, bool) {
	minimum := math.Inf(1)
	for _, target := range targets {
		if target.err == nil {
			minimum = min(minimum, target.seconds)
		}
	}
	if math.IsInf(minimum, 1) {
		return -1, false
	}
	// A shortest path has fewer edges than nodes. This allowance covers
	// accumulated rounding in both directions without changing route costs.
	tolerance := max(1, minimum) * float64(len(n.Nodes)) * 4 * (math.Nextafter(1, 2) - 1)
	if input.limit > 0 && minimum > input.limit+tolerance {
		return -1, false
	}
	best, bestSeconds := -1, math.Inf(1)
	var work routeSearchWork
	for _, target := range targets {
		if target.err != nil || target.seconds > minimum+tolerance {
			continue
		}
		search := networkRouteInput{from: n.Nodes[target.to].ID, to: input.from, terminalBerthsOnly: true}
		_, err := n.routeIndexedWithWork(search, graph, &work)
		if errors.Is(err, ErrUnreachable) {
			search.terminalBerthsOnly = false
			_, err = n.routeIndexedWithWork(search, graph, &work)
		}
		if err != nil {
			continue
		}
		seconds := work.distance[target.from]
		if input.limit > 0 && seconds > input.limit {
			continue
		}
		if best < 0 || seconds < bestSeconds || seconds == bestSeconds && input.rank[target.to] < input.rank[best] {
			best, bestSeconds = target.to, seconds
		}
	}
	return best, best >= 0
}
