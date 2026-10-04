package sim

import (
	"fmt"
	"slices"
)

// MotionRouteLimit bounds the ordered route window of a stream vehicle.
const MotionRouteLimit = 2048

// RoutePresentation describes a route without its unbounded traveled prefix.
// Motion and OriginNode are verified client geometry, not wire fields.
type RoutePresentation struct {
	Identity   uint64 `json:"identity,string"`
	Display    []int  `json:"display"`
	Origin     int    `json:"origin"`
	Lanes      []int  `json:"lanes"`
	Start      uint64 `json:"start,string"`
	Current    uint64 `json:"current,string"`
	Before     bool   `json:"before"`
	After      bool   `json:"after"`
	Motion     []Lane `json:"-"`
	OriginNode string `json:"-"`
}

// PresentationSnapshot copies display state without copying complete routes.
// The caller owns synchronization, as for Snapshot. Indexes refer to Network.
func (s *Simulation) PresentationSnapshot() (Snapshot, []RoutePresentation, error) {
	view, err := s.CouplingPresentation()
	if err != nil {
		return Snapshot{}, nil, err
	}
	state := s.snapshot(false)
	s.bindCouplingView(&state, view)
	lanes := make(map[string]int, len(s.network.Lanes))
	nodes := make(map[string]int, len(s.network.Nodes))
	for i, lane := range s.network.Lanes {
		lanes[lane.ID] = i
	}
	for i, node := range s.network.Nodes {
		nodes[node.ID] = i
	}
	routes := make([]RoutePresentation, len(s.vehicles))
	var seen []bool
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.routeVersion == ^uint64(0) {
			return Snapshot{}, nil, fmt.Errorf("route identity exhausted for %q", v.Pod.ID)
		}
		route := RoutePresentation{Identity: v.routeVersion, Origin: -1}
		if len(v.Route) > 0 {
			origin, ok := nodes[v.Route[0].From]
			if !ok {
				return Snapshot{}, nil, fmt.Errorf("unknown route origin %q", v.Route[0].From)
			}
			route.Origin = origin
			current := 0
			if v.Pod.LaneID != "" {
				if len(v.blocks.lanes) < 2 || v.blockIndex < 0 || v.blockIndex >= v.blocks.len() {
					return Snapshot{}, nil, fmt.Errorf("invalid route blocks for %q", v.Pod.ID)
				}
				current = v.blocks.locate(v.blockIndex, 0)
			} else if v.distance > 0 {
				current = len(v.Route)
			}
			if current < 0 || current > len(v.Route) {
				return Snapshot{}, nil, fmt.Errorf("invalid route occurrence for %q", v.Pod.ID)
			}
			start := max(0, current-MotionRouteLimit/2)
			end := min(len(v.Route), start+MotionRouteLimit)
			route.Start, route.Current = uint64(start), uint64(current)
			route.Before, route.After = start > 0, end < len(v.Route)
			if seen == nil {
				seen = make([]bool, len(lanes))
			} else {
				clear(seen)
			}
			for j, lane := range v.Route {
				index, ok := lanes[lane.ID]
				if !ok {
					return Snapshot{}, nil, fmt.Errorf("unknown route lane %q", lane.ID)
				}
				if !seen[index] {
					route.Display = append(route.Display, index)
					seen[index] = true
				}
				if j >= start && j < end {
					route.Lanes = append(route.Lanes, index)
				}
			}
			slices.Sort(route.Display)
		}
		routes[i] = route
	}
	return state, routes, nil
}
