package sim

import (
	"errors"
	"math"
	"slices"
)

const (
	redistributionIntervalTicks = 10 * TicksPerSecond
	redistributionCooldownTicks = 30 * TicksPerSecond
)

// SetRedistribution controls proactive movement of idle empty pods.
func (s *Simulation) SetRedistribution(enabled bool) {
	s.redistribution = enabled
	if enabled && s.nextRedistributionTick < s.tick {
		s.nextRedistributionTick = s.tick
	}
}

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

func (s *Simulation) redistribute() {
	s.yieldRelocationClaims()
	if !s.redistribution || s.tick < s.nextRedistributionTick || len(s.waiting) > 0 {
		return
	}
	supply, eligible := s.redistributionSupply()
	if eligible == 0 {
		return
	}
	desired := s.redistributionTargets(eligible)
	destination, berth, ok := s.redistributionDestination(supply, desired)
	if !ok {
		return
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if !s.redistributionCandidate(v, supply, desired) {
			continue
		}
		if err := s.startEmptyMove(v, emptyDestination{station: destination, berth: berth, reserveBerth: true, rebalance: true}); err != nil {
			continue
		}
		s.rebalanceMoves++
		s.nextRedistributionTick = s.tick + redistributionIntervalTicks
		return
	}
}

// yieldRelocationClaims lets passenger traffic arbitrate a remote berth locally.
// A relocating pod yields its destination claims when a different pod brings
// a passenger to that berth. See passengerArrivals.
// An empty pod keeps the claim after admission to the destination block.
// A released pod that yields a claim goes to the nearest free berth at once.
// See parkReleased.
//
// The loop makes the passenger arrivals at the first relocating pod. In the
// loop, only parkReleased changes the pods and the waiting trips, so the
// loop makes the passenger arrivals again after each call to parkReleased.
func (s *Simulation) yieldRelocationClaims() {
	var arrivals map[string]passengerArrival
	for i := range s.vehicles {
		relocating := &s.vehicles[i]
		if relocating.RelocatingTo == "" {
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
		holds := s.owners[claims[0]] == relocating.Pod.ID || s.owners[claims[1]] == relocating.Pod.ID
		if !holds || s.relocationDestinationAdmitted(relocating) {
			continue
		}
		for _, claimed := range claims {
			s.releaseOwned(relocating, claimed)
		}
		if relocating.released {
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
		if (v.Pod.Activity == Boarding || v.Pod.Activity == Traveling) && v.Request != nil && !v.Request.Completed {
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
// reaches its destination berth. routeBlocks adds a berth resource only to
// the last cell of a lane that ends at the berth. The check does not use the
// destination node: the first cell of a route holds its start node, and a
// released pod can go back to its origin berth.
func (s *Simulation) relocationDestinationAdmitted(v *vehicle) bool {
	claim := resource{kind: berthResource, id: v.destination.ID}
	for _, b := range v.blocks[:min(v.reservedThrough+1, len(v.blocks))] {
		if slices.Contains(b.resources, claim) {
			return true
		}
	}
	return false
}

func (s *Simulation) redistributionSupply() (map[string]int, int) {
	supply := make(map[string]int)
	eligible := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Occupied || s.assigned(v.Pod.ID) {
			continue
		}
		if v.Rebalancing {
			supply[v.RelocatingTo]++
			eligible++
			continue
		}
		if v.Pod.Activity != Idle {
			continue
		}
		eligible++
		station, _ := s.station(v.Pod.StationID)
		if !station.ParkingOnly {
			supply[station.ID]++
		}
	}
	return supply, eligible
}

type stationTarget struct {
	id       string
	weight   float64
	capacity int
	target   int
	fraction float64
}

func (s *Simulation) redistributionTargets(eligible int) map[string]int {
	var targets []stationTarget
	totalWeight := 0.0
	for _, station := range s.network.Stations {
		if station.ParkingOnly {
			continue
		}
		weight := 1.0
		if s.demandWeights != nil {
			weight = s.demandWeights[station.ID]
		}
		targets = append(targets, stationTarget{id: station.ID, weight: weight, capacity: len(station.Berths)})
		totalWeight += weight
	}
	remaining := eligible
	for i := range targets {
		exact := float64(eligible) * targets[i].weight / totalWeight
		targets[i].target = min(targets[i].capacity, int(math.Floor(exact)))
		targets[i].fraction = exact - math.Floor(exact)
		remaining -= targets[i].target
	}
	slices.SortFunc(targets, func(a, b stationTarget) int {
		if a.fraction != b.fraction {
			if a.fraction > b.fraction {
				return -1
			}
			return 1
		}
		if a.weight != b.weight {
			if a.weight > b.weight {
				return -1
			}
			return 1
		}
		return compareString(a.id, b.id)
	})
	for remaining > 0 {
		added := false
		for i := range targets {
			if targets[i].weight == 0 || targets[i].target >= targets[i].capacity {
				continue
			}
			targets[i].target++
			remaining--
			added = true
			if remaining == 0 {
				break
			}
		}
		if !added {
			break
		}
	}
	result := make(map[string]int, len(targets))
	for _, target := range targets {
		result[target.id] = target.target
	}
	return result
}

func (s *Simulation) redistributionDestination(supply, desired map[string]int) (string, Berth, bool) {
	for _, station := range s.network.Stations {
		if station.ParkingOnly || supply[station.ID] >= desired[station.ID] {
			continue
		}
		for _, berth := range station.Berths {
			if s.owners[resource{kind: berthResource, id: berth.ID}] == "" && s.owners[resource{kind: nodeResource, id: berth.Node}] == "" {
				return station.ID, berth, true
			}
		}
	}
	return "", Berth{}, false
}

func (s *Simulation) redistributionCandidate(v *vehicle, supply, desired map[string]int) bool {
	if v.Pod.Activity != Idle || v.Pod.Occupied || s.assigned(v.Pod.ID) || v.rebalanceAfter > s.tick {
		return false
	}
	station, _ := s.station(v.Pod.StationID)
	return station.ParkingOnly || supply[station.ID] > desired[station.ID]
}

func compareString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (s *Simulation) moveAndMeasure(v *vehicle) {
	before := v.distance
	occupied := v.Pod.Occupied
	s.move(v)
	travel := v.distance - before
	if occupied {
		s.passengerDistanceMeters += travel
	} else {
		s.emptyDistanceMeters += travel
	}
}
