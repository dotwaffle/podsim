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
	c.couplingGroups = slices.Clone(s.couplingGroups)
	c.couplingFleet = nil
	c.couplingApproaches = slices.Clone(s.couplingApproaches)
	c.couplingAttempts = maps.Clone(s.couplingAttempts)
	s.cloneMotion(&c)
	s.cloneCompactQueues(&c)
	// Lookups refill these caches with identical results, and dispatch
	// makes a new pass.
	c.lengths, c.routes, c.routeOrder, c.routeStations, c.pass = nil, nil, nil, nil, nil
	c.pickupBounds = nil
	c.staticConnected, c.staticRoutes = nil, nil
	c.routeWork = nil
	c.admissionWork = nil
	c.vehicles = slices.Clone(s.vehicles)
	for i := range c.vehicles {
		v := &c.vehicles[i]
		v.Riders, v.Stops = slices.Clone(v.Riders), slices.Clone(v.Stops)
		v.Boardings = slices.Clone(v.Boardings)
		v.routeReleases = maps.Clone(v.routeReleases)
	}
	c.owners = maps.Clone(s.owners)
	c.faults = slices.Clone(s.faults)
	c.faultReleased = slices.Clone(s.faultReleased)
	c.expressServices = maps.Clone(s.expressServices)
	if s.pickupSwaps != nil {
		c.pickupSwaps = new(*s.pickupSwaps)
		c.pickupSwaps.cooldown = maps.Clone(s.pickupSwaps.cooldown)
		c.pickupSwaps.records = slices.Clone(s.pickupSwaps.records)
	}
	if s.demo != nil {
		c.demo = new(*s.demo)
	}
	c.waiting = slices.Clone(s.waiting)
	c.undelivered = slices.Clone(s.undelivered)
	c.requestBoardings = slices.Clone(s.requestBoardings)
	c.requestCompletions = slices.Clone(s.requestCompletions)
	c.stepCompletions = slices.Clone(s.stepCompletions)
	c.nodePasses = slices.Clone(s.nodePasses)
	// congestionRoute writes to this map while congestionRouteCosts is set.
	// maps.Clone keeps a non-nil map non-nil.
	c.congestionRoutes = maps.Clone(s.congestionRoutes)
	c.predictiveQueues = slices.Clone(s.predictiveQueues)
	c.predictivePodQueues = maps.Clone(s.predictivePodQueues)
	for id, history := range c.predictivePodQueues {
		history.lanes = maps.Clone(history.lanes)
		c.predictivePodQueues[id] = history
	}
	c.platoonOrder, c.platoonAhead, c.platoonLanes = nil, nil, nil
	return &c
}
