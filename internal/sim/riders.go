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
			direct, directKnown = s.directDistance(v.journeyOrigin.Node, rider.To, v.destination), true
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

// departs reports whether a pod at a berth with the activity leaves when
// its phase ends and it has track.
func departs(activity Activity) bool {
	return activity == Boarding || activity == DepartingEmpty || activity == Continuing
}

// lastStop returns the station where the riders of a pod leave the last
// time. It is the destination station of a pod without stops.
func (v *vehicle) lastStop() string {
	if len(v.Stops) > 0 {
		return v.Stops[len(v.Stops)-1]
	}
	return v.destinationStation
}

// continueJourney starts the next leg of a pod that unloaded at an
// intermediate stop. The leg goes from the berth of the pod to the next
// stop, on the route of legRoute, as for board. The riders keep
// their distance, and the pod waits for track as a boarding pod does. When
// no route to the next stop exists, the pod stays unloading, and the next
// step tries again. Project validation connects each pair of passenger
// stations, so this does not occur in a valid project.
func (s *Simulation) continueJourney(v *vehicle) {
	berth := v.destination
	route, err := s.legRoute(v, leg{origin: v.journeyOrigin.Node, from: berth.Node, stops: v.Stops, ridden: v.riddenMeters()})
	if err != nil {
		return
	}
	v.riddenBase += v.distance
	v.origin, v.destination, v.destinationStation = berth, Berth{}, v.Stops[0]
	s.setVehicleRoute(v, route)
	v.Pod.Activity, v.Pod.Occupied, v.Pod.WaitReason, v.Pod.BlockedBy = Continuing, true, NoWait, ""
	v.phaseTicks, v.blockIndex, v.reservedThrough = 0, 0, -1
	v.originReleased = false
	v.distance, v.pending = 0, -1
}
