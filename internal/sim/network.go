// Package sim provides a deterministic simulation with no display dependencies.
package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// Point is a position in meters.
type Point struct{ X, Y float64 }

// Node is a connection point in the directed network.
type Node struct {
	ID       string
	Position Point
}

// Lane is a straight, directed connection with a speed limit in meters per second.
type Lane struct {
	ID, From, To string
	SpeedLimit   float64
}

// Berth is a station resource with its own connection point.
type Berth struct{ ID, Node string }

// Station keeps passenger access separate from through traffic.
type Station struct {
	ID, Name, Entry, Exit string
	Berths                []Berth
	ParkingOnly           bool
}

// Network describes immutable geometry and connectivity during a run.
type Network struct {
	Nodes    []Node
	Lanes    []Lane
	Stations []Station
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
	a, _ := n.Node(lane.From)
	b, _ := n.Node(lane.To)
	return math.Hypot(b.Position.X-a.Position.X, b.Position.Y-a.Position.Y)
}

// Route chooses minimum free-flow travel time. Slice order breaks equal-cost ties.
func (n Network) Route(from, to string) ([]Lane, error) {
	if _, ok := n.Node(from); !ok {
		return nil, fmt.Errorf("unknown origin %q", from)
	}
	if _, ok := n.Node(to); !ok {
		return nil, fmt.Errorf("unknown destination %q", to)
	}
	distance := map[string]float64{from: 0}
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
		if current == to {
			break
		}
		visited[current] = true
		for _, lane := range n.Lanes {
			if lane.From != current {
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
	for current := to; current != from; {
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
		if station.ID == "" || stations[station.ID] || !nodes[station.Entry] || !nodes[station.Exit] || station.Entry == station.Exit || len(station.Berths) == 0 || (!station.ParkingOnly && len(station.Berths) != 1) {
			return fmt.Errorf("station %q needs valid entry, exit, and berth capacity", station.ID)
		}
		stations[station.ID] = true
		for _, berth := range station.Berths {
			if berth.ID == "" || berths[berth.ID] || berthNodes[berth.Node] || !nodes[berth.Node] || berth.Node == station.Entry || berth.Node == station.Exit {
				return fmt.Errorf("station %q has an invalid or duplicate berth", station.ID)
			}
			berths[berth.ID] = true
			berthNodes[berth.Node] = true
			// The first scenario uses explicit straight entry, exit, and through lanes.
			if !n.connected(station.Entry, berth.Node) || !n.connected(berth.Node, station.Exit) || !n.connected(station.Entry, station.Exit) {
				return fmt.Errorf("station %q needs entry, exit, and through lanes", station.ID)
			}
		}
	}
	return nil
}

func (n Network) connected(from, to string) bool {
	return slices.ContainsFunc(n.Lanes, func(lane Lane) bool { return lane.From == from && lane.To == to })
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func (n Network) clone() Network {
	n.Nodes, n.Lanes, n.Stations = slices.Clone(n.Nodes), slices.Clone(n.Lanes), slices.Clone(n.Stations)
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
