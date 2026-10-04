package sim

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// ForecastTarget describes known future passengers at one station.
type ForecastTarget struct {
	Station     string
	ReleaseTick int64
	Passengers  int
}

// ForecastPositionResult reports bounded work and the one optional empty move.
type ForecastPositionResult struct {
	TargetsTried  int
	Searches      int
	StartAttempts int
	Pod           string
	Station       string
}

// PositionForForecast starts at most one ordinary empty rebalancing move.
// Call once after an advanced five-second tick. Invalid input changes nothing.
// Free-flow lead time does not guarantee arrival before the offer.
func (s *Simulation) PositionForForecast(targets []ForecastTarget) (ForecastPositionResult, error) {
	var result ForecastPositionResult
	if len(targets) > 300 {
		return result, errors.New("forecast must contain at most 300 stations")
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		station, ok := s.station(target.Station)
		if !ok || station.ParkingOnly || seen[target.Station] || target.Passengers < 1 || target.Passengers > 10000 || target.ReleaseTick <= s.tick || target.ReleaseTick-s.tick > 300*TicksPerSecond {
			return result, fmt.Errorf("invalid forecast target %q", target.Station)
		}
		seen[target.Station] = true
	}
	if s.paused || len(targets) == 0 || slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.PodID == "" }) || !s.guardedLoadLow() {
		return result, nil
	}
	s.ensureNetworkIndexes()
	view := guardedView{weights: make([]float64, len(s.network.Stations)), idle: make([]int, len(s.network.Stations)), assigned: s.assignedPods()}
	for i, station := range s.network.Stations {
		if !station.ParkingOnly {
			view.weights[i] = 1
		}
	}
	_, busy := s.guardedSupply(guardedSupplyInput{view: &view, demand: make([]bool, len(s.network.Stations))})
	incoming := make([]int, len(view.idle))
	supply := slices.Clone(view.idle)
	global := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Idle || v.RelocatingTo == "" {
			continue
		}
		index, ok := s.stationIndex(v.RelocatingTo)
		if !ok {
			continue
		}
		if v.Rebalancing {
			incoming[index]++
			global++
		}
		if !v.Pod.Occupied && !view.assigned[v.Pod.ID] {
			supply[index]++
		}
	}
	if global >= max(1, len(s.vehicles)/10) {
		return result, nil
	}
	rank, found := s.guardedCandidates(view)
	if !found {
		return result, nil
	}
	ordered := slices.Clone(targets)
	slices.SortFunc(ordered, func(a, b ForecastTarget) int {
		if c := cmp.Compare(a.ReleaseTick, b.ReleaseTick); c != 0 {
			return c
		}
		return cmp.Compare(a.Station, b.Station)
	})
	for _, target := range ordered {
		index, _ := s.stationIndex(target.Station)
		if supply[index] >= min(target.Passengers, 4) || incoming[index] >= 2 {
			continue
		}
		station := s.network.Stations[index]
		var available []Berth
		for _, berth := range station.Berths {
			if !busy[berth.ID] && s.owners[resource{kind: berthResource, id: berth.ID}].isZero() && s.owners[resource{kind: nodeResource, id: berth.Node}].isZero() {
				available = append(available, berth)
			}
		}
		if len(available) < 2 {
			continue
		}
		if result.TargetsTried >= 3 {
			break
		}
		result.TargetsTried++
		selected := available[0]
		sourceRank := slices.Clone(rank)
		// Moving within the target station cannot increase its supply.
		for node, fleet := range sourceRank {
			if fleet >= 0 && s.vehicles[fleet].Pod.StationID == target.Station {
				sourceRank[node] = -1
			}
		}
		result.Searches++
		node, ok := s.preferredFleetSource(preferredNearestInput{from: selected.Node, rank: sourceRank, limit: float64(target.ReleaseTick-s.tick) / TicksPerSecond, reverse: true})
		if !ok {
			continue
		}
		v := &s.vehicles[sourceRank[node]]
		result.StartAttempts++
		destination := emptyDestination{station: target.Station, berth: selected, reserveBerth: true, rebalance: true}
		route, err := s.prepareEmptyMove(v, destination)
		if err != nil || !s.forecastRouteFits(route, available, float64(target.ReleaseTick-s.tick)/TicksPerSecond) {
			continue
		}
		s.installEmptyMove(v, destination, route)
		s.rebalanceMoves++
		result.Pod, result.Station = v.Pod.ID, target.Station
		return result, nil
	}
	return result, nil
}

// forecastRouteFits checks the policy-selected route before claims change.
func (s *Simulation) forecastRouteFits(route []Lane, available []Berth, leadSeconds float64) bool {
	seconds := 0.0
	touched := make(map[string]bool)
	for _, lane := range route {
		seconds += s.laneLength(lane) / lane.SpeedLimit
		touched[lane.From], touched[lane.To] = true, true
	}
	if seconds > leadSeconds {
		return false
	}
	for _, berth := range available {
		if !touched[berth.Node] {
			return true
		}
	}
	return false
}
