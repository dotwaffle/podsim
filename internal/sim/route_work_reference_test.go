package sim

import (
	"fmt"
	"math"
)

// referenceRouteBeforeWork keeps the search from before work-array reuse.
// It checks reuse against separate storage and an unchanged search loop.
func (n Network) referenceRouteBeforeWork(input networkRouteInput, graph routeGraph) ([]Lane, error) {
	from, ok := graph.nodes[input.from]
	if !ok {
		return nil, fmt.Errorf("unknown origin %q", input.from)
	}
	to, ok := graph.nodes[input.to]
	if !ok {
		return nil, fmt.Errorf("unknown destination %q", input.to)
	}
	distance := make([]float64, len(n.Nodes))
	previous := make([]int, len(n.Nodes))
	visited := make([]bool, len(n.Nodes))
	for i := range distance {
		distance[i], previous[i] = math.Inf(1), -1
	}
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
		for _, laneIndex := range graph.outgoing[item.node] {
			edge := graph.edges[laneIndex]
			// The node indexes are equal only when the node IDs are equal.
			if input.forbidden != nil && edge.to != from && edge.to != to && input.forbidden[n.Lanes[laneIndex].To] {
				continue
			}
			if input.ownBerthsOnly && !graph.berthAllowed(edge.to, from, to) {
				continue
			}
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			if laneIndex < len(input.discharge) {
				extra += queueDelay(input.discharge[laneIndex], item.distance)
			}
			candidate := item.distance + edge.seconds + extra
			if candidate < distance[edge.to] {
				distance[edge.to], previous[edge.to] = candidate, laneIndex
				queue.push(routeQueueItem{node: edge.to, distance: candidate})
			}
		}
	}
	return n.routeLanes(graph, from, to, distance, previous)
}
