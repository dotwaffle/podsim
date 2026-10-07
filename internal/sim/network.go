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
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Node is a connection point in the directed network.
type Node struct {
	ID       string `json:"id"`
	Position Point  `json:"position"`
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
	VehicleClasses  ClassSet        `json:"vehicleClasses,omitzero"`
	ID              string          `json:"id"`
	From            string          `json:"from"`
	To              string          `json:"to"`
	SpeedLimit      float64         `json:"speedLimit"`
	SeparationGroup string          `json:"separationGroup,omitempty"`
	StationID       string          `json:"stationID,omitempty"`
	StationRole     StationLaneRole `json:"stationRole,omitempty"`
	// Control adds a quadratic curve. Nil keeps the lane straight.
	Control *Point `json:"control,omitempty"`
}

// Berth is a station resource with its own connection point.
type Berth struct {
	VehicleClasses  ClassSet `json:"vehicleClasses,omitzero"`
	ID              string   `json:"id"`
	Node            string   `json:"node"`
	SeparationGroup string   `json:"separationGroup,omitempty"`
}

// StationBank groups berths behind one independent entry and exit.
type StationBank struct {
	ID       string   `json:"id"`
	Entry    string   `json:"entry"`
	Exit     string   `json:"exit"`
	BerthIDs []string `json:"berthIDs"`
}

// MaxStationBanks bounds the banks of one station.
const MaxStationBanks = 8

// Station keeps passenger access separate from through traffic.
type Station struct {
	VehicleClasses ClassSet      `json:"vehicleClasses,omitzero"`
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Entry          string        `json:"entry"`
	Exit           string        `json:"exit"`
	Berths         []Berth       `json:"berths"`
	ParkingOnly    bool          `json:"parkingOnly"`
	Banks          []StationBank `json:"banks,omitempty"`
}

// Network describes immutable geometry and connectivity during a run.
type Network struct {
	Nodes    []Node    `json:"nodes"`
	Lanes    []Lane    `json:"lanes"`
	Stations []Station `json:"stations"`
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

// Polyline returns the points of the path that a pod follows along the
// lane. A straight lane has 2 points, and a curved lane has 65 points.
// Length, Position and the junction conflicts use the same path.
func (n Network) Polyline(lane Lane) []Point {
	return n.lanePoints(lane, make([]Point, 0, 65))
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

// RouteForClass chooses a free-flow path within the class allowlists.
// This checks compatibility metadata, not physical profile approval.
func (n Network) RouteForClass(from, to string, class VehicleClass) ([]Lane, error) {
	if _, ok := LookupVehicleClass(class); !ok {
		return nil, ErrUnknownVehicleClass
	}
	return n.route(networkRouteInput{from: from, to: to, class: class})
}

type networkRouteInput struct {
	class     VehicleClass
	from, to  string
	forbidden map[string]bool
	extraCost []float64
	// discharge holds, by lane index, the time from the route start at
	// which the queue on the lane clears. A lane adds queueDelay of this
	// time and the route cost at its start. See queueRoute.
	discharge []float64
	// forecasts hold a frozen FIFO model of planned lane entries.
	forecasts     []laneForecast
	forecastStart float64
	// ownBerthsOnly stops the search at each berth node of a station that
	// has no berth at from or at to. Thus the route cannot go through the
	// berths of a third station.
	ownBerthsOnly bool
	// terminalBerthsOnly allows a berth only at an exact route endpoint.
	terminalBerthsOnly bool
	allowedLanes       map[int]bool
	bankRaw            bool
	startCost          float64
	bankExternal       bool
}

func (n Network) route(input networkRouteInput) ([]Lane, error) {
	graph := newRouteGraph(n)
	return n.routeIndexed(input, graph)
}

func (n Network) routeIndexed(input networkRouteInput, graph routeGraph) ([]Lane, error) {
	var work routeSearchWork
	return n.routeIndexedWithWork(input, graph, &work)
}

func (n Network) routeIndexedWithWork(input networkRouteInput, graph routeGraph, work *routeSearchWork) ([]Lane, error) {
	if !input.bankRaw && len(graph.banks.banks) > 0 {
		return n.bankRoute(input, graph, work)
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
	work.reset(len(n.Nodes))
	distance, previous, visited := work.distance, work.previous, work.visited
	distance[from] = input.startCost
	work.queue = append(work.queue, routeQueueItem{node: from, distance: input.startCost})
	queue := work.queue
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
			if input.bankExternal && graph.banks.lanes[laneIndex] >= 0 && n.Lanes[laneIndex].StationRole != StationThroughRole {
				continue
			}
			if input.allowedLanes != nil && !input.allowedLanes[laneIndex] {
				continue
			}
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
			if laneIndex < len(input.forecasts) {
				extra += input.forecasts[laneIndex].delay(input.forecastStart + item.distance)
			}
			candidate := item.distance + edge.seconds + extra
			if candidate < distance[edge.to] {
				distance[edge.to], previous[edge.to] = candidate, laneIndex
				queue.push(routeQueueItem{node: edge.to, distance: candidate})
			}
		}
	}
	work.queue = queue[:0]
	return n.routeLanes(graph, from, to, distance, previous)
}

// routeLanes returns the route from node from to node to that a route
// search found. previous holds the index of the last lane of the route to
// each node.
func (n Network) routeLanes(graph routeGraph, from, to int, distance []float64, previous []int) ([]Lane, error) {
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

type routeGraph struct {
	banks    bankIndex
	nodes    map[string]int
	lanes    map[string]int
	outgoing [][]int
	// incoming holds the index of each lane that ends at a node. Searches
	// that follow lanes from their ends read it.
	incoming [][]int
	lengths  []float64
	// edges holds the route search data of each lane, so that the search
	// does not look up node IDs. Only a lane in outgoing has an edge.
	edges []routeEdge
	// berthStations holds, for each node, the index of the station that has
	// a berth at the node, or -1.
	berthStations     []int
	berthNodes        map[string]int
	nodeClasses       []uint8
	classRestrictions bool
	laneClasses       []uint8
	// blocked holds, by lane index, the lanes that an active fault blocks.
	// It is nil in the static graph s.graph and in each shared graph. Only
	// routingGraph sets it, on a copy.
	blocked []bool
}

// laneOpen reports whether no active fault blocks the lane. It is true for
// each lane when blocked is nil.
func (g routeGraph) laneOpen(lane int) bool {
	return lane >= len(g.blocked) || !g.blocked[lane]
}

// berthAllowed reports whether a route search from node from to node to can
// enter node. It reports false only for a berth node of a station that has
// no berth at from or at to.
func (g routeGraph) berthAllowed(node, from, to int) bool {
	station := g.berthStations[node]
	return station < 0 || station == g.berthStations[from] || station == g.berthStations[to]
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
		incoming: make([][]int, len(network.Nodes)), lengths: make([]float64, len(network.Lanes)), edges: make([]routeEdge, len(network.Lanes)),
		berthStations: make([]int, len(network.Nodes)), berthNodes: make(map[string]int),
	}
	for index, node := range network.Nodes {
		graph.nodes[node.ID] = index
		graph.berthStations[index] = -1
	}
	for stationIndex, station := range network.Stations {
		for _, berth := range station.Berths {
			if node, ok := graph.nodes[berth.Node]; ok {
				graph.berthStations[node] = stationIndex
				graph.berthNodes[berth.ID] = node
			}
		}
	}
	for index, lane := range network.Lanes {
		graph.lanes[lane.ID] = index
		from, fromOK := graph.nodes[lane.From]
		to, toOK := graph.nodes[lane.To]
		if !fromOK || !toOK {
			continue
		}
		graph.outgoing[from] = append(graph.outgoing[from], index)
		graph.incoming[to] = append(graph.incoming[to], index)
		graph.lengths[index] = indexedLaneLength(lane, network.Nodes[from].Position, network.Nodes[to].Position)
		graph.edges[index] = routeEdge{from: from, to: to, seconds: graph.lengths[index] / lane.SpeedLimit}
	}
	indexRouteClasses(&graph, network)
	graph.banks = indexStationBanks(network, graph)
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

// validate checks the network in a fixed order: the nodes, the lanes, each
// station with its berths, the stations of the lanes, the station banks,
// and then the routes through each station with no banks. The first error
// is the refusal, so the order is part of the result.
func (n Network) validate() error {
	nodes, err := n.validateNodes()
	if err != nil {
		return err
	}
	if err := n.validateLaneFields(nodes); err != nil {
		return err
	}
	stations := make(map[string]bool)
	if err := n.validateStationFields(nodes, stations); err != nil {
		return err
	}
	if err := n.validateLaneStations(stations); err != nil {
		return err
	}
	graph := newRouteGraph(n)
	if graph.banks.err != nil {
		return graph.banks.err
	}
	return n.validateStationRoutes(graph)
}

// validateNodes checks that each node has a unique ID and a finite
// position. It returns the position of each node.
func (n Network) validateNodes() (map[string]Point, error) {
	nodes := make(map[string]Point)
	for _, node := range n.Nodes {
		if _, exists := nodes[node.ID]; node.ID == "" || exists || !finite(node.Position.X) || !finite(node.Position.Y) {
			return nil, fmt.Errorf("invalid or duplicate node %q", node.ID)
		}
		nodes[node.ID] = node.Position
	}
	return nodes, nil
}

// validateLaneFields checks that each lane has a unique ID and valid
// fields.
func (n Network) validateLaneFields(nodes map[string]Point) error {
	lanes := make(map[string]bool)
	for _, lane := range n.Lanes {
		if lanes[lane.ID] || invalidLane(lane, nodes) {
			return fmt.Errorf("invalid or duplicate lane %q", lane.ID)
		}
		lanes[lane.ID] = true
	}
	return nil
}

// invalidLane reports whether a lane has unknown vehicle classes, no ID,
// an unknown end node, an invalid speed limit or length, or an invalid
// station role. A speed limit is at most MaxCounter, because the decoders
// refuse a larger integral number.
func invalidLane(lane Lane, nodes map[string]Point) bool {
	from, fromOK := nodes[lane.From]
	to, toOK := nodes[lane.To]
	length := indexedLaneLength(lane, from, to)
	return lane.VehicleClasses & ^allClassBits != 0 || lane.ID == "" || !fromOK || !toOK ||
		!finite(lane.SpeedLimit) || lane.SpeedLimit <= 0 || lane.SpeedLimit > MaxCounter || length <= 0 || !finite(length) ||
		!validStationLaneRole(lane.StationRole) || (lane.StationID == "") != (lane.StationRole == "")
}

// validateStationFields checks each station and then its berths, in
// station order. Station IDs, berth IDs and berth nodes are unique. It adds
// the ID of each valid station to stations.
func (n Network) validateStationFields(nodes map[string]Point, stations map[string]bool) error {
	berths, berthNodes := make(map[string]bool), make(map[string]bool)
	for _, station := range n.Stations {
		if stations[station.ID] || invalidStation(station, nodes) {
			return fmt.Errorf("station %q needs valid entry, exit, and berth capacity", station.ID)
		}
		stations[station.ID] = true
		for _, berth := range station.Berths {
			if berths[berth.ID] || berthNodes[berth.Node] || invalidBerth(berth, station, nodes) {
				return fmt.Errorf("station %q has an invalid or duplicate berth", station.ID)
			}
			berths[berth.ID] = true
			berthNodes[berth.Node] = true
		}
	}
	return nil
}

// invalidStation reports whether a station has unknown vehicle classes, no
// ID, an unknown or shared entry and exit, or no berths.
func invalidStation(station Station, nodes map[string]Point) bool {
	_, entryOK := nodes[station.Entry]
	_, exitOK := nodes[station.Exit]
	return station.VehicleClasses & ^allClassBits != 0 || station.ID == "" || !entryOK || !exitOK || station.Entry == station.Exit || len(station.Berths) == 0
}

// invalidBerth reports whether a berth has unknown vehicle classes, no ID,
// or an unknown node, or uses the entry or exit of its station.
func invalidBerth(berth Berth, station Station, nodes map[string]Point) bool {
	_, nodeOK := nodes[berth.Node]
	return berth.VehicleClasses & ^allClassBits != 0 || berth.ID == "" || !nodeOK || berth.Node == station.Entry || berth.Node == station.Exit
}

// validateLaneStations checks that the station of each lane is known.
func (n Network) validateLaneStations(stations map[string]bool) error {
	for _, lane := range n.Lanes {
		if lane.StationID != "" && !stations[lane.StationID] {
			return fmt.Errorf("lane %q has unknown station %q", lane.ID, lane.StationID)
		}
	}
	return nil
}

// validateStationRoutes checks each station with no banks. The station
// needs a through lane from its entry to its exit, and a route from its
// entry to each berth and from each berth to its exit.
func (n Network) validateStationRoutes(graph routeGraph) error {
	forbidden := n.stationForbidden()
	for _, station := range n.Stations {
		if station.Banks == nil && !n.stationRoutesExist(station, forbidden, graph) {
			return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
		}
	}
	return nil
}

// stationRoutesExist reports whether a station has its through lane and
// the routes to and from each berth, in berth order.
func (n Network) stationRoutesExist(station Station, forbidden map[string]bool, graph routeGraph) bool {
	if !n.connected(station.Entry, station.Exit) {
		return false
	}
	for _, berth := range station.Berths {
		if _, err := n.routeIndexed(networkRouteInput{class: topologyClass, from: station.Entry, to: berth.Node, forbidden: forbidden}, graph); err != nil {
			return false
		}
		if _, err := n.routeIndexed(networkRouteInput{class: topologyClass, from: berth.Node, to: station.Exit, forbidden: forbidden}, graph); err != nil {
			return false
		}
	}
	return true
}

func (n Network) stationForbidden() map[string]bool {
	forbidden := make(map[string]bool)
	for _, candidate := range n.Stations {
		forbidden[candidate.Entry] = true
		forbidden[candidate.Exit] = true
		for _, bank := range candidate.Banks {
			forbidden[bank.Entry], forbidden[bank.Exit] = true, true
		}
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
		n.Stations[i].Banks = slices.Clone(n.Stations[i].Banks)
		for j := range n.Stations[i].Banks {
			n.Stations[i].Banks[j].BerthIDs = slices.Clone(n.Stations[i].Banks[j].BerthIDs)
		}
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
