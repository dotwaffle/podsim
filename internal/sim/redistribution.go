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
		station, ok := s.network.Station(stationID)
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
	s.yieldRedistributionClaims()
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

// yieldRedistributionClaims lets passenger traffic arbitrate a remote berth locally.
// A pod keeps the claim after admission to the destination block.
func (s *Simulation) yieldRedistributionClaims() {
	for i := range s.vehicles {
		rebalancing := &s.vehicles[i]
		if !rebalancing.Rebalancing || !s.redistributionConflictsWithPassenger(rebalancing) || s.redistributionDestinationAdmitted(rebalancing) {
			continue
		}
		for _, claimed := range []resource{
			{kind: berthResource, id: rebalancing.destination.ID},
			{kind: nodeResource, id: rebalancing.destination.Node},
		} {
			if s.owners[claimed] == rebalancing.Pod.ID {
				delete(s.owners, claimed)
			}
		}
	}
}

func (s *Simulation) redistributionConflictsWithPassenger(rebalancing *vehicle) bool {
	for i := range s.vehicles {
		arrival := &s.vehicles[i]
		if arrival == rebalancing || arrival.destination.ID != rebalancing.destination.ID {
			continue
		}
		activePassenger := arrival.Request != nil && !arrival.Request.Completed &&
			(arrival.Pod.Activity == Boarding || arrival.Pod.Activity == Traveling)
		if activePassenger || s.assigned(arrival.Pod.ID) {
			return true
		}
	}
	return false
}

func (s *Simulation) redistributionDestinationAdmitted(v *vehicle) bool {
	for i := 0; i <= v.reservedThrough && i < len(v.blocks); i++ {
		for _, claimed := range v.blocks[i].resources {
			if claimed.kind == berthResource && claimed.id == v.destination.ID ||
				claimed.kind == nodeResource && claimed.id == v.destination.Node {
				return true
			}
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
		station, _ := s.network.Station(v.Pod.StationID)
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
	station, _ := s.network.Station(v.Pod.StationID)
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
