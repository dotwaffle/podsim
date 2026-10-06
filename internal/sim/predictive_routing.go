package sim

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"sort"
)

const (
	predictionHorizonSeconds = 90.0
	predictionDecaySeconds   = 5.0
)

type podQueueHistory struct {
	lanes map[int]float64
}

type plannedLaneArrival struct {
	at, until float64
}

type laneForecast struct {
	initial  float64
	arrivals []plannedLaneArrival
}

// delay uses a frozen FIFO discharge model. Lane exit time never decreases
// with entry time, including at simultaneous planned arrivals.
func (f *laneForecast) delay(at float64) float64 {
	index := sort.Search(len(f.arrivals), func(i int) bool { return f.arrivals[i].at > at })
	until := f.initial
	if index > 0 {
		until = f.arrivals[index-1].until
	}
	return math.Max(0, until-at)
}

func (f *laneForecast) finish() {
	slices.SortFunc(f.arrivals, func(a, b plannedLaneArrival) int { return cmp.Compare(a.at, b.at) })
	until := f.initial
	for index := range f.arrivals {
		until = math.Max(until, f.arrivals[index].at) + queueHeadwaySeconds
		f.arrivals[index].until = until
	}
}

func stoppedForPrediction(v *vehicle) bool {
	return v.Pod.Activity == Traveling && v.Pod.WaitReason != NoWait && v.Pod.Speed < queueStoppedSpeed
}

// predictionQueues samples every assignment. The smoothed history updates
// at most once per tick, with a weight from elapsed simulation time.
func (s *Simulation) predictionQueues() []float64 {
	raw := make([]float64, len(s.network.Lanes))
	stopped := make(map[string]int)
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if stoppedForPrediction(v) {
			if lane, ok := s.graph.lanes[v.Pod.LaneID]; ok {
				raw[lane] += queueHeadwaySeconds
				stopped[v.Pod.ID] = lane
			}
		}
	}
	if s.predictivePodQueues == nil || len(s.predictiveQueues) != len(raw) {
		s.predictivePodQueues = make(map[string]podQueueHistory)
		for id, lane := range stopped {
			s.predictivePodQueues[id] = podQueueHistory{lanes: map[int]float64{lane: queueHeadwaySeconds}}
		}
		s.predictiveQueues = slices.Clone(raw)
		s.predictiveQueueTick = s.tick
	} else if elapsed := s.tick - s.predictiveQueueTick; elapsed > 0 {
		weight := -math.Expm1(-float64(elapsed) / (TicksPerSecond * predictionDecaySeconds))
		for id, entry := range s.predictivePodQueues {
			history := entry.lanes
			for lane, seconds := range history {
				if seconds *= 1 - weight; seconds < 1e-9 {
					delete(history, lane)
				} else {
					history[lane] = seconds
				}
			}
			if lane, ok := stopped[id]; ok {
				history[lane] += weight * queueHeadwaySeconds
			}
			if len(history) == 0 {
				delete(s.predictivePodQueues, id)
			}
		}
		for id, lane := range stopped {
			if s.predictivePodQueues[id].lanes == nil {
				s.predictivePodQueues[id] = podQueueHistory{lanes: map[int]float64{lane: weight * queueHeadwaySeconds}}
			}
		}
		clear(s.predictiveQueues)
		// Stable pod order keeps floating-point sums independent of map
		// iteration and fleet order.
		for _, id := range slices.Sorted(maps.Keys(s.predictivePodQueues)) {
			for lane, seconds := range s.predictivePodQueues[id].lanes {
				s.predictiveQueues[lane] += seconds
			}
		}
		s.predictiveQueueTick = s.tick
	}
	return raw
}

func (s *Simulation) routeForecasts(self *vehicle) []laneForecast {
	s.ensureNetworkIndexes()
	raw := s.predictionQueues()
	forecasts := make([]laneForecast, len(raw))
	var history map[int]float64
	if self != nil {
		history = s.predictivePodQueues[self.Pod.ID].lanes
		if stoppedForPrediction(self) {
			if index, ok := s.graph.lanes[self.Pod.LaneID]; ok {
				raw[index] = math.Max(0, raw[index]-queueHeadwaySeconds)
			}
		}
	}
	for index := range forecasts {
		observed := math.Max(0, s.predictiveQueues[index]-history[index])
		forecasts[index].initial = math.Max(raw[index], observed)
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v == self {
			continue
		}
		s.addPlannedArrivals(forecasts, v)
	}
	for index := range forecasts {
		forecasts[index].finish()
	}
	return forecasts
}

// addPlannedArrivals counts future entries of an assigned route. Current
// lane occupants count through observed queues only. Predeparture routes
// include their first lane, so same-tick assignments update the forecast.
func (s *Simulation) addPlannedArrivals(forecasts []laneForecast, v *vehicle) {
	first, at := 0, float64(v.phaseTicks)/TicksPerSecond
	if v.Pod.Activity == Traveling {
		current := predictionLane(v)
		if current < 0 {
			return
		}
		lane := v.Route[current]
		at = math.Max(0, s.laneLength(lane)-v.Pod.LaneDistance) / lane.SpeedLimit
		first = current + 1
	} else if !departs(v.Pod.Activity) {
		return
	}
	for _, lane := range v.Route[first:] {
		if at > predictionHorizonSeconds {
			break
		}
		index := s.graph.lanes[lane.ID]
		forecasts[index].arrivals = append(forecasts[index].arrivals, plannedLaneArrival{at: at})
		at += s.graph.edges[index].seconds
	}
}

// predictionLane reads the current route index without mutating block cursors.
func predictionLane(v *vehicle) int {
	if len(v.blocks.lanes) < 2 || v.blockIndex < 0 || v.blockIndex >= v.blocks.len() {
		return -1
	}
	return v.blocks.locate(v.blockIndex, 0)
}

// predictionStart accounts for a moving pod's retained prefix before a
// newly assigned suffix. Other assignments start at the current clock.
func (s *Simulation) predictionStart(v *vehicle, from string) float64 {
	if v == nil || v.Pod.Activity != Traveling {
		return 0
	}
	current := predictionLane(v)
	if current < 0 {
		return 0
	}
	at := 0.0
	for index := current; index < len(v.Route); index++ {
		lane := v.Route[index]
		length := s.laneLength(lane)
		if index == current {
			length = math.Max(0, length-v.Pod.LaneDistance)
		}
		at += length / lane.SpeedLimit
		if lane.To == from {
			return at
		}
	}
	return 0
}

func (s *Simulation) forecastCost(route []Lane, forecasts []laneForecast, start float64) (seconds, cost float64) {
	for _, lane := range route {
		index := s.graph.lanes[lane.ID]
		travel := s.graph.edges[index].seconds
		cost += travel + forecasts[index].delay(start+cost)
		seconds += travel
	}
	return seconds, cost
}

// predictiveRoute changes only a new route or its uncommitted suffix. Its
// forecasts select a path without granting any physical resource.
func (s *Simulation) predictiveRoute(v *vehicle, from, to string) ([]Lane, error) {
	free, err := s.routeForClass(from, to, podClass(v))
	if err != nil || len(free) == 0 {
		return free, err
	}
	var forecasts []laneForecast
	if s.routeView != nil {
		forecasts = s.viewForecasts(v)
	} else {
		forecasts = s.routeForecasts(v)
	}
	start := s.predictionStart(v, from)
	freeSeconds, freeCost := s.forecastCost(free, forecasts, start)
	if freeCost == freeSeconds {
		return free, nil
	}
	planned, err := s.searchRoute(networkRouteInput{from: from, to: to, class: podClass(v), forecasts: forecasts, forecastStart: start, terminalBerthsOnly: true})
	if err == nil {
		seconds, cost := s.forecastCost(planned, forecasts, start)
		if freeCost-cost >= math.Max(queueMinimumSavingSeconds, queueMinimumSavingShare*freeCost) && seconds <= queueDetourLimit*freeSeconds {
			return planned, nil
		}
	}
	return free, nil
}
