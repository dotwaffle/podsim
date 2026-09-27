package sim

import "slices"

// JourneyStats measures the time from request to alighting of the parties
// that left a pod at their destination since reset.
type JourneyStats struct {
	AverageSeconds float64 `json:"AverageSeconds"`
	MaxSeconds     float64 `json:"MaxSeconds"`
}

func (s *Simulation) journeyStats() JourneyStats {
	if s.journeys == 0 {
		return JourneyStats{}
	}
	return JourneyStats{
		AverageSeconds: float64(s.totalJourneyTicks) / float64(s.journeys) / TicksPerSecond,
		MaxSeconds:     float64(s.maxJourneyTicks) / TicksPerSecond,
	}
}

// carriesPassengers reports whether a pod carries parties that have not
// arrived. After a journey, a pod keeps its riders with Completed true, so
// the riders alone do not tell.
func (v *vehicle) carriesPassengers() bool {
	return (v.Pod.Activity == Boarding || v.Pod.Occupied) && v.RidersAboard() > 0
}

// RidersAboard returns the number of riders that have not left the pod.
func (v *Vehicle) RidersAboard() int {
	aboard := 0
	for index := range v.Riders {
		if !v.Riders[index].Completed {
			aboard++
		}
	}
	return aboard
}

// PassengersAboard returns the passengers of the riders that have not left
// the pod.
func (v *Vehicle) PassengersAboard() int {
	passengers := 0
	for index := range v.Riders {
		if !v.Riders[index].Completed {
			passengers += v.Riders[index].PartySize
		}
	}
	return passengers
}

// boardingStation returns the origin station of the riders aboard a pod.
// All riders of a pod board at one station. It returns "" for a pod with no
// rider aboard.
func (v *vehicle) boardingStation() string {
	for _, rider := range v.Riders {
		if !rider.Completed {
			return rider.From
		}
	}
	return ""
}

// riddenMeters returns the distance that the riders of the pod rode since
// they boarded.
func (v *vehicle) riddenMeters() float64 { return v.riddenBase + v.distance }

// alight completes each rider of a pod that unloaded at a stop. A rider
// leaves the pod when the pod has no later stop for it. alight adds the
// journey and the distances of each such rider to the totals.
func (s *Simulation) alight(v *vehicle) {
	ridden := v.riddenMeters()
	direct, directKnown := -1.0, false
	for index := range v.Riders {
		rider := &v.Riders[index]
		if rider.Completed || slices.Contains(v.Stops, rider.To) {
			continue
		}
		if !directKnown {
			direct, directKnown = s.directDistance(v.origin.Node, rider.To, v.destination), true
		}
		rider.Completed = true
		s.completed++
		journey := s.tick - rider.RequestedTick
		s.journeys++
		s.totalJourneyTicks += journey
		s.maxJourneyTicks = max(s.maxJourneyTicks, journey)
		s.riderDistanceMeters += ridden
		if direct > 0 {
			s.directDistanceMeters += direct
			s.maxDetourRatio = max(s.maxDetourRatio, ridden/direct)
		}
		if s.recordExperiments {
			s.requestCompletions = append(s.requestCompletions, requestCompletion{
				requestID: rider.ID, tick: s.tick, riddenMeters: ridden, directMeters: max(direct, 0),
			})
		}
	}
}

// directDistance returns the free-flow distance from a node to a berth of a
// station: the shortest route to the station entry, then the station path
// to the berth. A pod on a route of stationApproachRoute that stops at that
// berth rides this distance. It returns -1 when no such route exists.
//
// route gives the free-flow route with each routing policy, so the direct
// distance does not change with the policy or with congestion.
func (s *Simulation) directDistance(from, stationID string, berth Berth) float64 {
	station, ok := s.station(stationID)
	if !ok || from == "" || berth.Node == "" {
		return -1
	}
	approach, err := s.route(from, station.Entry)
	if err != nil {
		return -1
	}
	path, err := s.stationPath(station.Entry, berth.Node)
	if err != nil {
		return -1
	}
	distance := 0.0
	for _, lane := range approach {
		distance += s.laneLength(lane)
	}
	for _, lane := range path {
		distance += s.laneLength(lane)
	}
	return distance
}
