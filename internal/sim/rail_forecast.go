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
	if err := s.checkForecastTargets(targets); err != nil {
		return result, err
	}
	if s.forecastBlocked(targets) {
		return result, nil
	}
	s.ensureNetworkIndexes()
	view := s.forecastView()
	supply := s.forecastSupply(&view)
	if supply.global >= max(1, len(s.vehicles)/10) {
		return result, nil
	}
	rank, found := s.guardedCandidates(view)
	if !found {
		return result, nil
	}
	for _, target := range sortedForecastTargets(targets) {
		available := s.forecastBerths(target, supply)
		if len(available) < 2 {
			continue
		}
		if result.TargetsTried >= 3 {
			break
		}
		result.TargetsTried++
		if s.startForecastMove(target, available, rank, &result) {
			return result, nil
		}
	}
	return result, nil
}

// checkForecastTargets checks the target count, and then each target in
// input order.
func (s *Simulation) checkForecastTargets(targets []ForecastTarget) error {
	if len(targets) > 600 {
		return errors.New("forecast must contain at most 600 stations")
	}
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if s.forecastTargetInvalid(target, seen) {
			return fmt.Errorf("invalid forecast target %q", target.Station)
		}
		seen[target.Station] = true
	}
	return nil
}

// forecastTargetInvalid reports an unknown, parking, or repeated station, a
// passenger count out of range, or a release tick outside the horizon.
func (s *Simulation) forecastTargetInvalid(target ForecastTarget, seen map[string]bool) bool {
	station, ok := s.station(target.Station)
	if !ok || station.ParkingOnly || seen[target.Station] {
		return true
	}
	return target.Passengers < 1 || target.Passengers > 10000 || target.ReleaseTick <= s.tick || target.ReleaseTick-s.tick > 300*TicksPerSecond
}

// forecastBlocked reports a paused simulation, an empty forecast, a waiting
// trip with no pod, or a working share that is too high.
func (s *Simulation) forecastBlocked(targets []ForecastTarget) bool {
	return s.paused || len(targets) == 0 || slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.PodID == "" }) || !s.guardedLoadLow()
}

// forecastView returns a guarded view with weight 1 at each passenger
// station.
func (s *Simulation) forecastView() guardedView {
	view := guardedView{weights: make([]float64, len(s.network.Stations)), idle: make([]int, len(s.network.Stations)), assigned: s.assignedPods()}
	for i, station := range s.network.Stations {
		if !station.ParkingOnly {
			view.weights[i] = 1
		}
	}
	return view
}

// forecastStationSupply holds the supply data that PositionForForecast
// reads.
type forecastStationSupply struct {
	// busy holds the berths that waiting trips go to.
	busy map[string]bool
	// incoming counts the rebalancing pods that move to each station, by
	// station index.
	incoming []int
	// supply counts the idle pods and the empty unassigned pods that move
	// to each station, by station index.
	supply []int
	// global counts all rebalancing pods.
	global int
}

// forecastSupply fills the idle counts of view, and then adds the pods that
// move to each station.
func (s *Simulation) forecastSupply(view *guardedView) forecastStationSupply {
	_, busy := s.guardedSupply(guardedSupplyInput{view: view, demand: make([]bool, len(s.network.Stations))})
	out := forecastStationSupply{busy: busy, incoming: make([]int, len(view.idle)), supply: slices.Clone(view.idle)}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Idle || v.RelocatingTo == "" || !v.inService() {
			continue
		}
		index, ok := s.stationIndex(v.RelocatingTo)
		if !ok {
			continue
		}
		if v.Rebalancing {
			out.incoming[index]++
			out.global++
		}
		if !v.Pod.Occupied && !view.assigned[v.Pod.ID] {
			out.supply[index]++
		}
	}
	return out
}

// sortedForecastTargets returns a copy of targets in release tick order, and
// then in station order.
func sortedForecastTargets(targets []ForecastTarget) []ForecastTarget {
	ordered := slices.Clone(targets)
	slices.SortFunc(ordered, func(a, b ForecastTarget) int {
		if c := cmp.Compare(a.ReleaseTick, b.ReleaseTick); c != 0 {
			return c
		}
		return cmp.Compare(a.Station, b.Station)
	})
	return ordered
}

// forecastBerths returns the free berths of the target station. It returns
// nil if the station has enough supply or incoming pods.
func (s *Simulation) forecastBerths(target ForecastTarget, supply forecastStationSupply) []Berth {
	index, _ := s.stationIndex(target.Station)
	if supply.supply[index] >= min(target.Passengers, 4) || supply.incoming[index] >= 2 {
		return nil
	}
	var available []Berth
	for _, berth := range s.network.Stations[index].Berths {
		if !supply.busy[berth.ID] && s.owners[resource{kind: berthResource, id: berth.ID}].isZero() && s.owners[resource{kind: nodeResource, id: berth.Node}].isZero() {
			available = append(available, berth)
		}
	}
	return available
}

// startForecastMove searches for a source pod for the first available
// berth, and starts its move if the route fits the lead time. It counts the
// search and the start attempt in result.
func (s *Simulation) startForecastMove(target ForecastTarget, available []Berth, rank []int, result *ForecastPositionResult) bool {
	selected := available[0]
	sourceRank := s.forecastSourceRank(rank, target.Station)
	result.Searches++
	node, ok := s.preferredFleetSource(preferredNearestInput{from: selected.Node, rank: sourceRank, limit: float64(target.ReleaseTick-s.tick) / TicksPerSecond, reverse: true})
	if !ok {
		return false
	}
	v := &s.vehicles[sourceRank[node]]
	result.StartAttempts++
	destination := emptyDestination{station: target.Station, berth: selected, reserveBerth: true, rebalance: true}
	route, err := s.prepareEmptyMove(v, destination)
	if err != nil || !s.forecastRouteFits(route, available, float64(target.ReleaseTick-s.tick)/TicksPerSecond) {
		return false
	}
	s.installEmptyMove(v, destination, route)
	s.rebalanceMoves++
	result.Pod, result.Station = v.Pod.ID, target.Station
	return true
}

// forecastSourceRank returns a copy of rank without the pods at the target
// station. Moving within the target station cannot increase its supply.
func (s *Simulation) forecastSourceRank(rank []int, station string) []int {
	sourceRank := slices.Clone(rank)
	for node, fleet := range sourceRank {
		if fleet >= 0 && s.vehicles[fleet].Pod.StationID == station {
			sourceRank[node] = -1
		}
	}
	return sourceRank
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
