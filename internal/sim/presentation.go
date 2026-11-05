package sim

import (
	"fmt"
	"slices"
)

// MotionRouteLimit bounds the ordered window and unique display lanes of a stream vehicle.
const MotionRouteLimit = 1024

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

// routeWindowCache holds the lane indexes of one route window of a vehicle.
// A window of a route has end > 0, so the zero value matches no window. Each
// value depends only on the route of the key and on the prepared network,
// which a simulation never replaces. A vehicle copy and a Clone share the
// slices, so no code may write them. A save does not hold the cache.
type routeWindowCache struct {
	version        uint64
	start, end     int
	display, lanes []int
}

// PresentationSnapshot copies display state without copying complete routes.
// The caller owns synchronization, as for Snapshot. Indexes refer to Network.
// Display and Lanes of each route are shared and read-only. They can be the
// same arrays as in an earlier snapshot.
func (s *Simulation) PresentationSnapshot() (Snapshot, []RoutePresentation, error) {
	state := s.snapshot(false)
	routes, err := s.presentationRoutes(false)
	if err != nil {
		return Snapshot{}, nil, err
	}
	return state, routes, nil
}

// RebuiltPresentationRoutesForTest returns the routes of PresentationSnapshot from a
// new build of each window. It does not read or change a cached window. Tests
// in other packages use it to check the cache.
func (s *Simulation) RebuiltPresentationRoutesForTest() ([]RoutePresentation, error) {
	return s.presentationRoutes(true)
}

func (s *Simulation) presentationRoutes(rebuild bool) ([]RoutePresentation, error) {
	// The prepared network already owns immutable indexes in wire order.
	lanes, nodes := s.graph.lanes, s.graph.nodes
	routes := make([]RoutePresentation, len(s.vehicles))
	var seen []bool
	for i := range s.vehicles {
		v := &s.vehicles[i]
		// A route replacement cannot change the identity at this limit, so
		// this refusal comes before the cache.
		if v.routeVersion == MaxCounter {
			return nil, fmt.Errorf("route identity exhausted for %q", v.Pod.ID)
		}
		route := RoutePresentation{Identity: v.routeVersion, Origin: -1}
		if len(v.Route) > 0 {
			origin, ok := nodes[v.Route[0].From]
			if !ok {
				return nil, fmt.Errorf("unknown route origin %q", v.Route[0].From)
			}
			route.Origin = origin
			current := 0
			if v.Pod.LaneID != "" {
				if len(v.blocks.lanes) < 2 || v.blockIndex < 0 || v.blockIndex >= v.blocks.len() {
					return nil, fmt.Errorf("invalid route blocks for %q", v.Pod.ID)
				}
				current = v.blocks.locate(v.blockIndex, 0)
			} else if v.distance > 0 {
				current = len(v.Route)
			}
			if current < 0 || current > len(v.Route) {
				return nil, fmt.Errorf("invalid route occurrence for %q", v.Pod.ID)
			}
			start := max(0, current-MotionRouteLimit/2)
			end := min(len(v.Route), start+MotionRouteLimit)
			route.Start, route.Current = uint64(start), uint64(current)
			route.Before, route.After = start > 0, end < len(v.Route)
			window := v.window
			if rebuild || window.version != v.routeVersion || window.start != start || window.end != end {
				if seen == nil {
					seen = make([]bool, len(lanes))
				} else {
					clear(seen)
				}
				display, indexes, err := buildRouteWindow(v.Route[start:end], lanes, seen)
				if err != nil {
					return nil, err
				}
				window = routeWindowCache{version: v.routeVersion, start: start, end: end, display: display, lanes: indexes}
				if !rebuild {
					v.window = window
				}
			}
			route.Display, route.Lanes = window.display, window.lanes
		}
		routes[i] = route
	}
	return routes, nil
}

// buildRouteWindow returns the sorted unique lane indexes of route and the lane
// index of each occurrence. seen is clear, with one entry for each lane. The
// results have no spare capacity, so an append cannot reach the shared arrays.
func buildRouteWindow(route []Lane, lanes map[string]int, seen []bool) (display, indexes []int, err error) {
	indexes = make([]int, 0, len(route))
	display = make([]int, 0, len(route))
	for _, lane := range route {
		index, ok := lanes[lane.ID]
		if !ok {
			return nil, nil, fmt.Errorf("unknown route lane %q", lane.ID)
		}
		if !seen[index] {
			display = append(display, index)
			seen[index] = true
		}
		indexes = append(indexes, index)
	}
	slices.Sort(display)
	return slices.Clip(display), indexes, nil
}
