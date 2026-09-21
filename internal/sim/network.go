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

// Lane is a directed connection with a speed limit in meters per second.
type Lane struct {
	ID         string  `json:"ID"`
	From       string  `json:"From"`
	To         string  `json:"To"`
	SpeedLimit float64 `json:"SpeedLimit"`
	// Control adds a quadratic curve. Nil keeps the lane straight.
	Control *Point `json:",omitempty"`
}

// Berth is a station resource with its own connection point.
type Berth struct {
	ID   string `json:"ID"`
	Node string `json:"Node"`
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
}

func (n Network) route(input networkRouteInput) ([]Lane, error) {
	if _, ok := n.Node(input.from); !ok {
		return nil, fmt.Errorf("unknown origin %q", input.from)
	}
	if _, ok := n.Node(input.to); !ok {
		return nil, fmt.Errorf("unknown destination %q", input.to)
	}
	distance := map[string]float64{input.from: 0}
	visited := make(map[string]bool)
	previous := make(map[string]Lane)
	for {
		current, best := "", math.Inf(1)
		for _, node := range n.Nodes {
			if d, ok := distance[node.ID]; ok && !visited[node.ID] && d < best {
				current, best = node.ID, d
			}
		}
		if current == "" {
			return nil, ErrUnreachable
		}
		if current == input.to {
			break
		}
		visited[current] = true
		for _, lane := range n.Lanes {
			if lane.From != current {
				continue
			}
			if lane.To != input.to && input.forbidden[lane.To] {
				continue
			}
			candidate := best + n.Length(lane)/lane.SpeedLimit
			old, exists := distance[lane.To]
			if !exists || candidate < old {
				distance[lane.To], previous[lane.To] = candidate, lane
			}
		}
	}
	var route []Lane
	for current := input.to; current != input.from; {
		lane := previous[current]
		route = append(route, lane)
		current = lane.From
	}
	slices.Reverse(route)
	return route, nil
}

func (n Network) validate() error {
	nodes := make(map[string]bool)
	for _, node := range n.Nodes {
		if node.ID == "" || nodes[node.ID] || !finite(node.Position.X) || !finite(node.Position.Y) {
			return fmt.Errorf("invalid or duplicate node %q", node.ID)
		}
		nodes[node.ID] = true
	}
	lanes := make(map[string]bool)
	for _, lane := range n.Lanes {
		if lane.ID == "" || lanes[lane.ID] || !nodes[lane.From] || !nodes[lane.To] ||
			!finite(lane.SpeedLimit) || lane.SpeedLimit <= 0 || n.Length(lane) <= 0 || !finite(n.Length(lane)) {
			return fmt.Errorf("invalid or duplicate lane %q", lane.ID)
		}
		lanes[lane.ID] = true
	}
	stations, berths, berthNodes := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for _, station := range n.Stations {
		if station.ID == "" || stations[station.ID] || !nodes[station.Entry] || !nodes[station.Exit] || station.Entry == station.Exit || len(station.Berths) == 0 {
			return fmt.Errorf("station %q needs valid entry, exit, and berth capacity", station.ID)
		}
		stations[station.ID] = true
		for _, berth := range station.Berths {
			if berth.ID == "" || berths[berth.ID] || berthNodes[berth.Node] || !nodes[berth.Node] || berth.Node == station.Entry || berth.Node == station.Exit {
				return fmt.Errorf("station %q has an invalid or duplicate berth", station.ID)
			}
			berths[berth.ID] = true
			berthNodes[berth.Node] = true
		}
	}
	for _, station := range n.Stations {
		if !n.connected(station.Entry, station.Exit) {
			return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
		}
		for _, berth := range station.Berths {
			if _, err := n.stationPath(station.Entry, berth.Node); err != nil {
				return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
			}
			if _, err := n.stationPath(berth.Node, station.Exit); err != nil {
				return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
			}
		}
	}
	return nil
}

// stationPath finds an access path without crossing another station boundary.
func (n Network) stationPath(from, to string) ([]Lane, error) {
	forbidden := make(map[string]bool)
	for _, candidate := range n.Stations {
		forbidden[candidate.Entry] = true
		forbidden[candidate.Exit] = true
		for _, berth := range candidate.Berths {
			forbidden[berth.Node] = true
		}
	}
	delete(forbidden, from)
	delete(forbidden, to)
	return n.route(networkRouteInput{from: from, to: to, forbidden: forbidden})
}

func (n Network) connected(from, to string) bool {
	return slices.ContainsFunc(n.Lanes, func(lane Lane) bool { return lane.From == from && lane.To == to })
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

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
