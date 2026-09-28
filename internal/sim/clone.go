package sim

import (
	"maps"
	"slices"
)

// Clone returns an independent simulation that evolves exactly like s for the
// same inputs. It shares network data that is replaced whole, copies mutable
// state, and drops pure route and length caches.
func (s *Simulation) Clone() *Simulation {
	c := *s
	// Lookups refill these caches with identical results, and dispatch
	// makes a new pass.
	c.lengths, c.routes, c.routeOrder, c.routeStations, c.pass = nil, nil, nil, nil, nil
	c.vehicles = slices.Clone(s.vehicles)
	for i := range c.vehicles {
		v := &c.vehicles[i]
		v.Riders, v.Stops = slices.Clone(v.Riders), slices.Clone(v.Stops)
		v.routeReleases = maps.Clone(v.routeReleases)
	}
	c.owners = maps.Clone(s.owners)
	if s.demo != nil {
		c.demo = new(*s.demo)
	}
	c.waiting = slices.Clone(s.waiting)
	c.requestBoardings = slices.Clone(s.requestBoardings)
	c.requestCompletions = slices.Clone(s.requestCompletions)
	c.nodePasses = slices.Clone(s.nodePasses)
	// congestionRoute writes to this map while congestionRouteCosts is set.
	// maps.Clone keeps a non-nil map non-nil.
	c.congestionRoutes = maps.Clone(s.congestionRoutes)
	return &c
}
