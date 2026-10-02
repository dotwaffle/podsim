package editormodel

import (
	"container/heap"
	"math"

	"github.com/dotwaffle/podsim/internal/project"
)

type editorRouteEdge struct {
	from, to int
	seconds  float64
}

type editorRouteGraph struct {
	nodes    map[string]int
	outgoing [][]int
	edges    []editorRouteEdge
}

func inferEditorStationLanes(network map[string]any) {
	graph := editorStationGraph(network)
	if graph == nil || !editorInferableStations(network, graph.nodes) {
		return
	}
	stations, lanes := items(network["Stations"]), items(network["Lanes"])
	forbidden := make(map[int]bool)
	for _, station := range stations {
		for id := range stationCoreNodes(station) {
			forbidden[graph.nodes[id]] = true
		}
	}
	for _, station := range stations {
		roles := make(map[int]string)
		for index, lane := range lanes {
			if member(lane, "From") == member(station, "Entry") && member(lane, "To") == member(station, "Exit") {
				roles[index] = "through"
			}
		}
		for _, berth := range items(member(station, "Berths")) {
			for _, trip := range []struct{ from, to, role string }{
				{text(member(station, "Entry")), text(member(berth, "Node")), "berth-access"},
				{text(member(berth, "Node")), text(member(station, "Exit")), "departure"},
			} {
				for _, index := range graph.route(trip.from, trip.to, forbidden) {
					if roles[index] == "" {
						roles[index] = trip.role
					}
				}
			}
		}
		for index, role := range roles {
			lane := object(lanes[index])
			if editorTruthy(lane["StationID"]) || editorTruthy(lane["StationRole"]) {
				continue
			}
			lane["StationID"], lane["StationRole"] = member(station, "ID"), role
		}
	}
}

func editorInferableStations(network map[string]any, nodes map[string]int) bool {
	stationIDs, berthIDs, berthNodes := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for _, station := range items(network["Stations"]) {
		id, entry, exit := text(member(station, "ID")), text(member(station, "Entry")), text(member(station, "Exit"))
		_, entryFound := nodes[entry]
		_, exitFound := nodes[exit]
		berths := items(member(station, "Berths"))
		if object(station) == nil || id == "" || stationIDs[id] || !entryFound || !exitFound || entry == exit || len(berths) == 0 || len(berths) > project.MaxBerths {
			return false
		}
		stationIDs[id] = true
		for _, berth := range berths {
			berthID, node := text(member(berth, "ID")), text(member(berth, "Node"))
			_, found := nodes[node]
			if object(berth) == nil || berthID == "" || berthIDs[berthID] || !found || berthNodes[node] || node == entry || node == exit {
				return false
			}
			berthIDs[berthID], berthNodes[node] = true, true
		}
	}
	return true
}

func editorStationGraph(network map[string]any) *editorRouteGraph {
	nodes, lanes := items(network["Nodes"]), items(network["Lanes"])
	if len(nodes) > project.MaxNodes || len(lanes) > project.MaxLanes || len(items(network["Stations"])) > project.MaxStations {
		return nil
	}
	graph := &editorRouteGraph{nodes: make(map[string]int, len(nodes)), outgoing: make([][]int, len(nodes)), edges: make([]editorRouteEdge, len(lanes))}
	positions := make([]geometryPoint, len(nodes))
	for index, node := range nodes {
		id, position := text(member(node, "ID")), member(node, "Position")
		if _, exists := graph.nodes[id]; exists || id == "" || !finite(member(position, "X")) || !finite(member(position, "Y")) {
			return nil
		}
		graph.nodes[id], positions[index] = index, geometryPoint{number(member(position, "X")), number(member(position, "Y"))}
	}
	ids := make(map[string]bool, len(lanes))
	for index, lane := range lanes {
		id := text(member(lane, "ID"))
		from, fromFound := graph.nodes[text(member(lane, "From"))]
		to, toFound := graph.nodes[text(member(lane, "To"))]
		speed := number(member(lane, "SpeedLimit"))
		if id == "" || ids[id] || !fromFound || !toFound || !finite(speed) || speed <= 0 {
			return nil
		}
		var control *geometryPoint
		if value := member(lane, "Control"); editorTruthy(value) {
			if !finite(member(value, "X")) || !finite(member(value, "Y")) {
				return nil
			}
			control = &geometryPoint{number(member(value, "X")), number(member(value, "Y"))}
		}
		points, length := geometryPolyline(positions[from], positions[to], control), 0.0
		for i := 1; i < len(points); i++ {
			length += math.Hypot(points[i].X-points[i-1].X, points[i].Y-points[i-1].Y)
		}
		if !finite(length) || length <= 0 {
			return nil
		}
		ids[id] = true
		graph.outgoing[from] = append(graph.outgoing[from], index)
		graph.edges[index] = editorRouteEdge{from, to, length / speed}
	}
	return graph
}

func (g *editorRouteGraph) route(fromID, toID string, forbidden map[int]bool) []int {
	from, to := g.nodes[fromID], g.nodes[toID]
	costs, previous, visited := make([]float64, len(g.nodes)), make([]int, len(g.nodes)), make([]bool, len(g.nodes))
	for node := range costs {
		costs[node], previous[node] = math.Inf(1), -1
	}
	costs[from] = 0
	queue := &editorRouteQueue{{node: from}}
	for queue.Len() != 0 {
		item, ok := heap.Pop(queue).(editorRouteItem)
		if !ok {
			panic("invalid private editor route queue item")
		}
		if visited[item.node] || item.cost != costs[item.node] {
			continue
		}
		if item.node == to {
			break
		}
		visited[item.node] = true
		for _, index := range g.outgoing[item.node] {
			edge := g.edges[index]
			if edge.to != from && edge.to != to && forbidden[edge.to] {
				continue
			}
			if next := item.cost + edge.seconds; next < costs[edge.to] {
				costs[edge.to], previous[edge.to] = next, index
				heap.Push(queue, editorRouteItem{edge.to, next})
			}
		}
	}
	if math.IsInf(costs[to], 1) {
		return nil
	}
	var route []int
	for current := to; current != from; current = g.edges[route[len(route)-1]].from {
		if previous[current] < 0 || len(route) >= len(g.nodes) {
			return nil
		}
		route = append(route, previous[current])
	}
	// The consumer assigns one role to all route lanes, so order is immaterial.
	return route
}

type editorRouteItem struct {
	node int
	cost float64
}

type editorRouteQueue []editorRouteItem

func (q *editorRouteQueue) Len() int { return len(*q) }
func (q *editorRouteQueue) Less(i, j int) bool {
	return (*q)[i].cost < (*q)[j].cost || (*q)[i].cost == (*q)[j].cost && (*q)[i].node < (*q)[j].node
}
func (q *editorRouteQueue) Swap(i, j int) { (*q)[i], (*q)[j] = (*q)[j], (*q)[i] }

// Push implements heap.Interface for private editor route items.
func (q *editorRouteQueue) Push(value any) {
	item, ok := value.(editorRouteItem)
	if !ok {
		panic("invalid private editor route queue item")
	}
	*q = append(*q, item)
}

// Pop implements heap.Interface for private editor route items.
func (q *editorRouteQueue) Pop() any {
	last := len(*q) - 1
	value := (*q)[last]
	*q = (*q)[:last]
	return value
}
