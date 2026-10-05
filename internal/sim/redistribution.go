package sim

import (
	"errors"
	"math"
	"slices"
)

// redistributionCooldownTicks is the time after a rebalancing move ends
// before the pod can make another positioning move.
const redistributionCooldownTicks = 30 * TicksPerSecond

// SetDemandWeights sets relative passenger demand by origin station.
// A nil or empty map selects equal weights for all passenger stations.
func (s *Simulation) SetDemandWeights(weights map[string]float64) error {
	if len(weights) == 0 {
		s.demandWeights = nil
		return nil
	}
	cloned := make(map[string]float64, len(weights))
	total := 0.0
	for stationID, weight := range weights {
		station, ok := s.station(stationID)
		if !ok || station.ParkingOnly {
			return errors.New("demand weights require passenger stations")
		}
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
			return errors.New("demand weights must be finite and nonnegative")
		}
		cloned[stationID] = weight
		total += weight
		if math.IsInf(total, 0) {
			return errors.New("demand weight total must be finite")
		}
	}
	if total == 0 {
		return errors.New("demand weights need at least one positive value")
	}
	s.demandWeights = cloned
	return nil
}

// redistribute lets relocating pods yield their claims to passenger
// traffic. In guarded mode, it then makes the guarded check.
func (s *Simulation) redistribute() {
	s.yieldRelocationClaims()
	if s.positioning == PositioningGuarded {
		s.positionGuarded()
	}
}

// yieldRelocationClaims lets passenger traffic arbitrate a remote berth locally.
// A relocating pod yields its destination claims when a different pod brings
// a passenger to that berth. See passengerArrivals.
// An empty pod keeps the claim after admission to the destination block.
// A released pod that yields a claim goes to the nearest free berth at once.
// See parkReleased. In guarded mode, a rebalancing pod that yields a claim
// becomes a released pod, and it also goes to the nearest free berth at
// once.
//
// The loop makes the passenger arrivals at the first relocating pod. In the
// loop, only parkReleased changes the pods and the waiting trips, so the
// loop makes the passenger arrivals again after each call to parkReleased.
func (s *Simulation) yieldRelocationClaims() {
	var arrivals map[string]passengerArrival
	for i := range s.vehicles {
		relocating := &s.vehicles[i]
		// A coupled member's receiving claim belongs to the committed train.
		if relocating.RelocatingTo == "" || relocating.couplingID != "" {
			continue
		}
		if arrivals == nil {
			arrivals = s.passengerArrivals()
		}
		if !arrivals[relocating.destination.ID].conflictsWith(relocating) {
			continue
		}
		claims := [...]resource{
			{kind: berthResource, id: relocating.destination.ID},
			{kind: nodeResource, id: relocating.destination.Node},
		}
		// A pod that holds no claim has nothing to yield. This check comes
		// before the admission check, because it costs less.
		holds := s.owners[claims[0]] == podResourceOwner(relocating.Pod.ID) || s.owners[claims[1]] == podResourceOwner(relocating.Pod.ID)
		if !holds || s.relocationDestinationAdmitted(relocating) {
			continue
		}
		for _, claimed := range claims {
			s.releaseOwned(relocating, claimed)
		}
		if relocating.released {
			s.parkReleased(relocating)
			arrivals = nil
		} else if s.positioning == PositioningGuarded && relocating.Rebalancing {
			relocating.Rebalancing, relocating.released = false, true
			s.parkReleased(relocating)
			arrivals = nil
		}
	}
}

// passengerArrival records one pod that brings a passenger to a berth, and
// whether a different pod also does. first is that pod. more is true when a
// different pod also brings a passenger.
type passengerArrival struct {
	first *vehicle
	more  bool
}

// with returns the arrival after it records that v brings a passenger. A
// second record of the same pod changes nothing.
func (a passengerArrival) with(v *vehicle) passengerArrival {
	switch {
	case a.first == nil:
		a.first = v
	case a.first != v:
		a.more = true
	}
	return a
}

// conflictsWith reports whether a pod other than v brings a passenger. A
// pickup pod can be relocating and assigned. It does not conflict with
// itself.
func (a passengerArrival) conflictsWith(v *vehicle) bool {
	return a.first != nil && (a.more || a.first != v)
}

// passengerArrivals returns the passenger arrivals at the destination berth
// of each relocating pod, keyed by berth ID. A pod brings a passenger to its
// destination berth when it has an active passenger that boards or travels,
// or when a waiting trip names it. Pod IDs are unique, so findVehicle finds
// the pod that a waiting trip names. The empty berth ID is also a key when a
// relocating pod has no destination berth. The map has no key for a berth
// that no relocating pod goes to. The map is correct only while the pods and
// the waiting trips do not change.
func (s *Simulation) passengerArrivals() map[string]passengerArrival {
	relocating := 0
	for i := range s.vehicles {
		if s.vehicles[i].RelocatingTo != "" {
			relocating++
		}
	}
	arrivals := make(map[string]passengerArrival, relocating)
	for i := range s.vehicles {
		if v := &s.vehicles[i]; v.RelocatingTo != "" {
			arrivals[v.destination.ID] = passengerArrival{}
		}
	}
	add := func(v *vehicle) {
		if arrival, ok := arrivals[v.destination.ID]; ok {
			arrivals[v.destination.ID] = arrival.with(v)
		}
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if (v.Pod.Activity == Boarding || v.Pod.Activity == Continuing || v.Pod.Activity == Traveling) && v.RidersAboard() > 0 {
			add(v)
		}
	}
	for _, trip := range s.waiting {
		if v := s.findVehicle(trip.request.PodID); v != nil {
			add(v)
		}
	}
	return arrivals
}

// relocationDestinationAdmitted reports whether the reserved track of v
// reaches its destination berth. newLaneCells adds a berth resource only to
// the last cell of a lane that ends at the berth. The check does not use the
// destination node: the first cell of a route holds its start node, and a
// released pod can go back to its origin berth.
func (s *Simulation) relocationDestinationAdmitted(v *vehicle) bool {
	claim := resource{kind: berthResource, id: v.destination.ID}
	for resources := range v.blocks.spanResources(0, min(v.reservedThrough+1, v.blocks.len())) {
		if slices.Contains(resources, claim) {
			return true
		}
	}
	return false
}

func (s *Simulation) moveAndMeasure(v *vehicle) {
	sample := MotionSample{ID: v.Pod.ID, Class: v.Pod.Class, StartSpeed: v.Pod.Speed}
	before := v.distance
	beforeLocal := v.Pod.LaneDistance
	compact := s.compactGroup(v) != nil
	occupied := v.Pod.Occupied
	s.move(v)
	travel := v.distance - before
	if compact {
		travel = v.Pod.LaneDistance - beforeLocal
	}
	sample.DistanceMeters, sample.EndSpeed = travel, v.Pod.Speed
	s.recordMotion(sample)
	if occupied {
		s.passengerDistanceMeters += travel
	} else {
		s.emptyDistanceMeters += travel
	}
}
