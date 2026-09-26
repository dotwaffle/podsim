// Package sim provides a deterministic simulation with no display dependencies.
package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// Point is a position in meters.
type Point struct {
	X float64 `json:"X"`
	Y float64 `json:"Y"`
}

// Node is a connection point in the directed network.
type Node struct {
	ID       string `json:"ID"`
	Position Point  `json:"Position"`
}

// StationLaneRole identifies a lane's function in one station maneuver.
type StationLaneRole string

// Station lane roles identify each part of a station path.
const (
	StationApproachRole    StationLaneRole = "approach"
	StationEntryRole       StationLaneRole = "entry"
	StationBerthAccessRole StationLaneRole = "berth-access"
	StationThroughRole     StationLaneRole = "through"
	StationDepartureRole   StationLaneRole = "departure"
	StationExitRole        StationLaneRole = "exit"
)

// Lane is a directed connection with a speed limit in meters per second.
type Lane struct {
	ID              string          `json:"ID"`
	From            string          `json:"From"`
	To              string          `json:"To"`
	SpeedLimit      float64         `json:"SpeedLimit"`
	SeparationGroup string          `json:"SeparationGroup,omitempty"`
	StationID       string          `json:"StationID,omitempty"`
	StationRole     StationLaneRole `json:"StationRole,omitempty"`
	// Control adds a quadratic curve. Nil keeps the lane straight.
	Control *Point `json:",omitempty"`
}

// Berth is a station resource with its own connection point.
type Berth struct {
	ID              string `json:"ID"`
	Node            string `json:"Node"`
	SeparationGroup string `json:"SeparationGroup,omitempty"`
}

// Station keeps passenger access separate from through traffic.
type Station struct {
	ID          string  `json:"ID"`
	Name        string  `json:"Name"`
	Entry       string  `json:"Entry"`
	Exit        string  `json:"Exit"`
	Berths      []Berth `json:"Berths"`
	ParkingOnly bool    `json:"ParkingOnly"`
}

// Network describes immutable geometry and connectivity during a run.
type Network struct {
	Nodes    []Node    `json:"Nodes"`
	Lanes    []Lane    `json:"Lanes"`
	Stations []Station `json:"Stations"`
}

// ErrUnreachable means no directed route connects the requested nodes.
var ErrUnreachable = errors.New("destination is unreachable")

// Node returns a node by its stable ID.
func (n Network) Node(id string) (Node, bool) {
	for _, node := range n.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return Node{}, false
}

// Station returns a station by its stable ID.
func (n Network) Station(id string) (Station, bool) {
	for _, station := range n.Stations {
		if station.ID == id {
			return station, true
		}
	}
	return Station{}, false
}

// Length returns the lane length in meters for a validated network.
func (n Network) Length(lane Lane) float64 {
	var storage [65]Point
	points := n.lanePoints(lane, storage[:0])
	length := 0.0
	for i := 1; i < len(points); i++ {
		length += pointDistance(points[i-1], points[i])
	}
	return length
}

// Position returns a point at a distance along the lane, clamped to its ends.
func (n Network) Position(lane Lane, distance float64) Point {
	var storage [65]Point
	points := n.lanePoints(lane, storage[:0])
	for i := 1; i < len(points); i++ {
		length := pointDistance(points[i-1], points[i])
		if distance <= length && length > 0 {
			t := max(0, distance) / length
			return Point{X: points[i-1].X + t*(points[i].X-points[i-1].X), Y: points[i-1].Y + t*(points[i].Y-points[i-1].Y)}
		}
		distance -= length
	}
	return points[len(points)-1]
}

// Use the same polyline for length, movement, and browser interpolation.
func (n Network) lanePoints(lane Lane, points []Point) []Point {
	a, _ := n.Node(lane.From)
	b, _ := n.Node(lane.To)
	if lane.Control == nil {
		return append(points, a.Position, b.Position)
	}
	points = points[:65]
	for i := range points {
		t := float64(i) / 64
		u := 1 - t
		points[i] = Point{X: u*u*a.Position.X + 2*u*t*lane.Control.X + t*t*b.Position.X, Y: u*u*a.Position.Y + 2*u*t*lane.Control.Y + t*t*b.Position.Y}
	}
	return points
}

func pointDistance(a, b Point) float64 { return math.Hypot(b.X-a.X, b.Y-a.Y) }

// Route chooses minimum free-flow travel time. Slice order breaks equal-cost ties.
func (n Network) Route(from, to string) ([]Lane, error) {
	return n.route(networkRouteInput{from: from, to: to})
}

type networkRouteInput struct {
	from, to  string
	forbidden map[string]bool
	extraCost []float64
}

func (n Network) route(input networkRouteInput) ([]Lane, error) {
	graph := newRouteGraph(n)
	return n.routeIndexed(input, graph)
}

func (n Network) routeIndexed(input networkRouteInput, graph routeGraph) ([]Lane, error) {
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
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			candidate := item.distance + edge.seconds + extra
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

// nearestInput is the input of nearestIndexed.
type nearestInput struct {
	from string
	// rank holds a rank for each node index. It is -1 for a node that is not
	// a goal. Between goals with the same route cost, the lower rank wins.
	rank      []int
	extraCost []float64
}

// nearestIndexed returns the index of the goal node with the lowest route
// cost from input.from. The cost is the same as in routeIndexed without
// forbidden nodes. It reports false when no goal is reachable.
func (n Network) nearestIndexed(input nearestInput, graph routeGraph) (int, bool) {
	from, ok := graph.nodes[input.from]
	if !ok {
		return 0, false
	}
	distance := make([]float64, len(n.Nodes))
	visited := make([]bool, len(n.Nodes))
	for i := range distance {
		distance[i] = math.Inf(1)
	}
	distance[from] = 0
	best, bestDistance := -1, math.Inf(1)
	queue := routeQueue{{node: from}}
	for len(queue) > 0 {
		item := queue.pop()
		if item.distance > bestDistance {
			break
		}
		if visited[item.node] || item.distance != distance[item.node] {
			continue
		}
		visited[item.node] = true
		if rank := input.rank[item.node]; rank >= 0 {
			if best < 0 || rank < input.rank[best] {
				best, bestDistance = item.node, item.distance
			}
			continue
		}
		for _, laneIndex := range graph.outgoing[item.node] {
			edge := graph.edges[laneIndex]
			extra := 0.0
			if laneIndex < len(input.extraCost) {
				extra = input.extraCost[laneIndex]
			}
			candidate := item.distance + edge.seconds + extra
			if candidate < distance[edge.to] {
				distance[edge.to] = candidate
				queue.push(routeQueueItem{node: edge.to, distance: candidate})
			}
		}
	}
	return best, best >= 0
}

type routeGraph struct {
	nodes    map[string]int
	lanes    map[string]int
	outgoing [][]int
	lengths  []float64
	// edges holds the route search data of each lane, so that the search
	// does not look up node IDs. Only a lane in outgoing has an edge.
	edges []routeEdge
}

// routeEdge is a lane in the route search. from and to are the node indexes
// of the lane ends. seconds is the free-flow travel time of the lane.
type routeEdge struct {
	from, to int
	seconds  float64
}

func newRouteGraph(network Network) routeGraph {
	graph := routeGraph{
		nodes: make(map[string]int, len(network.Nodes)), lanes: make(map[string]int, len(network.Lanes)), outgoing: make([][]int, len(network.Nodes)),
		lengths: make([]float64, len(network.Lanes)), edges: make([]routeEdge, len(network.Lanes)),
	}
	for index, node := range network.Nodes {
		graph.nodes[node.ID] = index
	}
	for index, lane := range network.Lanes {
		graph.lanes[lane.ID] = index
		from, fromOK := graph.nodes[lane.From]
		to, toOK := graph.nodes[lane.To]
		if !fromOK || !toOK {
			continue
		}
		graph.outgoing[from] = append(graph.outgoing[from], index)
		graph.lengths[index] = indexedLaneLength(lane, network.Nodes[from].Position, network.Nodes[to].Position)
		graph.edges[index] = routeEdge{from: from, to: to, seconds: graph.lengths[index] / lane.SpeedLimit}
	}
	return graph
}

func indexedLaneLength(lane Lane, from, to Point) float64 {
	if lane.Control == nil {
		return pointDistance(from, to)
	}
	length, previous := 0.0, from
	for i := 1; i <= 64; i++ {
		t := float64(i) / 64
		u := 1 - t
		point := Point{X: u*u*from.X + 2*u*t*lane.Control.X + t*t*to.X, Y: u*u*from.Y + 2*u*t*lane.Control.Y + t*t*to.Y}
		length += pointDistance(previous, point)
		previous = point
	}
	return length
}

type routeQueueItem struct {
	node     int
	distance float64
}

type routeQueue []routeQueueItem

func (q *routeQueue) push(item routeQueueItem) {
	*q = append(*q, item)
	for child := len(*q) - 1; child > 0; {
		parent := (child - 1) / 2
		if !routeQueueLess((*q)[child], (*q)[parent]) {
			break
		}
		(*q)[parent], (*q)[child] = (*q)[child], (*q)[parent]
		child = parent
	}
}

func (q *routeQueue) pop() routeQueueItem {
	root := (*q)[0]
	last := (*q)[len(*q)-1]
	*q = (*q)[:len(*q)-1]
	if len(*q) == 0 {
		return root
	}
	(*q)[0] = last
	for parent := 0; ; {
		left := parent*2 + 1
		if left >= len(*q) {
			break
		}
		child := left
		right := left + 1
		if right < len(*q) && routeQueueLess((*q)[right], (*q)[left]) {
			child = right
		}
		if !routeQueueLess((*q)[child], (*q)[parent]) {
			break
		}
		(*q)[parent], (*q)[child] = (*q)[child], (*q)[parent]
		parent = child
	}
	return root
}

func routeQueueLess(a, b routeQueueItem) bool {
	if a.distance == b.distance {
		return a.node < b.node
	}
	return a.distance < b.distance
}

func (n Network) validate() error {
	nodes := make(map[string]Point)
	for _, node := range n.Nodes {
		if _, exists := nodes[node.ID]; node.ID == "" || exists || !finite(node.Position.X) || !finite(node.Position.Y) {
			return fmt.Errorf("invalid or duplicate node %q", node.ID)
		}
		nodes[node.ID] = node.Position
	}
	lanes := make(map[string]bool)
	for _, lane := range n.Lanes {
		from, fromOK := nodes[lane.From]
		to, toOK := nodes[lane.To]
		length := indexedLaneLength(lane, from, to)
		if lane.ID == "" || lanes[lane.ID] || !fromOK || !toOK ||
			!finite(lane.SpeedLimit) || lane.SpeedLimit <= 0 || length <= 0 || !finite(length) ||
			!validStationLaneRole(lane.StationRole) || (lane.StationID == "") != (lane.StationRole == "") {
			return fmt.Errorf("invalid or duplicate lane %q", lane.ID)
		}
		lanes[lane.ID] = true
	}
	stations, berths, berthNodes := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for _, station := range n.Stations {
		_, entryOK := nodes[station.Entry]
		_, exitOK := nodes[station.Exit]
		if station.ID == "" || stations[station.ID] || !entryOK || !exitOK || station.Entry == station.Exit || len(station.Berths) == 0 {
			return fmt.Errorf("station %q needs valid entry, exit, and berth capacity", station.ID)
		}
		stations[station.ID] = true
		for _, berth := range station.Berths {
			_, nodeOK := nodes[berth.Node]
			if berth.ID == "" || berths[berth.ID] || berthNodes[berth.Node] || !nodeOK || berth.Node == station.Entry || berth.Node == station.Exit {
				return fmt.Errorf("station %q has an invalid or duplicate berth", station.ID)
			}
			berths[berth.ID] = true
			berthNodes[berth.Node] = true
		}
	}
	for _, lane := range n.Lanes {
		if lane.StationID != "" && !stations[lane.StationID] {
			return fmt.Errorf("lane %q has unknown station %q", lane.ID, lane.StationID)
		}
	}
	graph := newRouteGraph(n)
	forbidden := n.stationForbidden()
	for _, station := range n.Stations {
		if !n.connected(station.Entry, station.Exit) {
			return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
		}
		for _, berth := range station.Berths {
			if _, err := n.routeIndexed(networkRouteInput{from: station.Entry, to: berth.Node, forbidden: forbidden}, graph); err != nil {
				return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
			}
			if _, err := n.routeIndexed(networkRouteInput{from: berth.Node, to: station.Exit, forbidden: forbidden}, graph); err != nil {
				return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
			}
		}
	}
	return nil
}

func (n Network) stationForbidden() map[string]bool {
	forbidden := make(map[string]bool)
	for _, candidate := range n.Stations {
		forbidden[candidate.Entry] = true
		forbidden[candidate.Exit] = true
		for _, berth := range candidate.Berths {
			forbidden[berth.Node] = true
		}
	}
	return forbidden
}

func (n Network) connected(from, to string) bool {
	return slices.ContainsFunc(n.Lanes, func(lane Lane) bool { return lane.From == from && lane.To == to })
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func validStationLaneRole(role StationLaneRole) bool {
	switch role {
	case "", StationApproachRole, StationEntryRole, StationBerthAccessRole,
		StationThroughRole, StationDepartureRole, StationExitRole:
		return true
	default:
		return false
	}
}

func (n Network) clone() Network {
	n.Nodes, n.Lanes, n.Stations = slices.Clone(n.Nodes), cloneLanes(n.Lanes), slices.Clone(n.Stations)
	for i := range n.Stations {
		n.Stations[i].Berths = slices.Clone(n.Stations[i].Berths)
	}
	return n
}

// berth returns the first berth when id is empty.
func (station Station) berth(id string) (Berth, bool) {
	for _, berth := range station.Berths {
		if id == "" || berth.ID == id {
			return berth, true
		}
	}
	return Berth{}, false
}

func cloneLanes(lanes []Lane) []Lane {
	lanes = slices.Clone(lanes)
	for i := range lanes {
		if lanes[i].Control != nil {
			lanes[i].Control = new(*lanes[i].Control)
		}
	}
	return lanes
}
