package sim

import "slices"

// RequestTiming gives the boarding and completion of one passenger request.
// Each party of a shared ride has its own timing.
type RequestTiming struct {
	RequestID     int   `json:"RequestID"`
	RequestedTick int64 `json:"RequestedTick"`
	// BoardedTick is the tick at which the party started to board. For a
	// party that joined a shared ride, it is the tick of the join.
	BoardedTick int64 `json:"BoardedTick"`
	// CompletedTick is the tick at which the party left the pod at its
	// destination, or -1 before that.
	CompletedTick int64 `json:"CompletedTick"`
	// RiddenMeters is the distance that the party rode, from the berth
	// where it boarded to the berth where it left the pod. It is 0 before
	// completion.
	RiddenMeters float64 `json:"RiddenMeters"`
	// DirectMeters is the free-flow distance between the same two berths.
	// It is 0 before completion, and when no free-flow route exists.
	DirectMeters float64 `json:"DirectMeters"`
	// SharedWith is 0 for a party that boarded its own pod. For a party that
	// joined a shared ride, it is the ID of the first request of the pod.
	SharedWith int `json:"SharedWith"`
	// Reassigned is true for a party that joined a shared ride while it
	// had a pod on its way. Dispatch released that pod. See
	// SharedRideJoinReassignExisting.
	Reassigned bool `json:"Reassigned"`
}

// requestCompletion records that one party left a pod at its destination.
type requestCompletion struct {
	requestID                  int
	tick                       int64
	riddenMeters, directMeters float64
}

// NodePass records that a pod entered a lane at the start node of the lane.
type NodePass struct {
	Tick int64  `json:"Tick"`
	Node string `json:"Node"`
}

// SetExperimentRecords turns the records of RequestTimings, NodePasses and
// SeatScreen on or off. They are off by default, so a long server session
// does not keep a record for each request and each lane. The records do not
// change the simulation. Turning them off clears them.
func (s *Simulation) SetExperimentRecords(enabled bool) {
	s.recordExperiments = enabled
	if !enabled {
		s.requestBoardings, s.requestCompletions, s.nodePasses = nil, nil, nil
		s.seatScreen = SeatScreen{}
		// The census flags of the waiting trips go with the counters, so
		// each set flag is a count in SeatScreen.
		for index := range s.waiting {
			trip := &s.waiting[index]
			trip.joinEligibleAssigned, trip.joinEligibleExistingStop = false, false
		}
	}
}

// NodePasses returns a pass for each lane that a pod entered since the last
// reset or restore while the records were on, in tick order. A pod passes
// the start node of each lane of its route, from its berth to the start of
// the last lane. It does not pass the node at the end of its route. It
// gives nil when the records are off. It does not change the simulation.
func (s *Simulation) NodePasses() []NodePass {
	if !s.recordExperiments {
		return nil
	}
	return slices.Clone(s.nodePasses)
}

// recordLaneEntries records a node pass for each route lane of v from first
// to last, at the current tick.
func (s *Simulation) recordLaneEntries(v *vehicle, first, last int) {
	if !s.recordExperiments {
		return
	}
	for lane := first; lane <= last; lane++ {
		s.nodePasses = append(s.nodePasses, NodePass{Tick: s.tick, Node: v.blocks.route[lane].From})
	}
}

// RequestTimings returns a timing for each request that boarded since the
// last reset or restore while the records were on, in boarding order. It
// gives nil when the records are off. It does not change the simulation. A
// party that boarded before a restore has no timing.
func (s *Simulation) RequestTimings() []RequestTiming {
	if !s.recordExperiments {
		return nil
	}
	ends := make(map[int]requestCompletion, len(s.requestCompletions))
	for _, end := range s.requestCompletions {
		ends[end.requestID] = end
	}
	timings := make([]RequestTiming, len(s.requestBoardings))
	for index, timing := range s.requestBoardings {
		timing.CompletedTick = -1
		if end, ok := ends[timing.RequestID]; ok {
			timing.CompletedTick, timing.RiddenMeters, timing.DirectMeters = end.tick, end.riddenMeters, end.directMeters
		}
		timings[index] = timing
	}
	return timings
}
