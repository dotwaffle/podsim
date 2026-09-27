package main

import (
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// requestStats holds the wait and journey columns of one arm, in seconds,
// the occupancy, and the mean detour ratio.
type requestStats struct {
	waitP95, journeyAverage, journeyP95, journeyMaximum float64
	occupancy, detourMean                               float64
}

// requestTimeStats gives the wait and journey columns from the request
// timings of the simulation and the snapshot at the end of the arm.
//
// The waits are the waits of the boarded parties and the elapsed waits of the
// pending requests at the end, the same set as the wait average of the
// simulation. The journeys are the times from request to alight of the
// parties that completed. A party that joined a shared ride has its own
// wait and journey.
//
// The occupancy is the rider distance of the completed parties divided by
// the occupied distance of their pod journeys. Each pod journey has one
// party that boarded the pod, and the other parties joined it at the same
// berth. Thus the party that rode the farthest rode the occupied distance
// of the journey up to its last completed stop.
//
// The mean detour ratio is the mean, over the completed parties with a
// free-flow route, of the rider distance divided by the free-flow distance.
func requestTimeStats(timings []sim.RequestTiming, state sim.Snapshot) requestStats {
	waits := make([]int64, 0, len(timings)+len(state.Pending))
	journeys := make([]int64, 0, len(timings))
	// farthest holds the longest rider distance of each pod journey, by the
	// request ID of the party that boarded the pod.
	farthest := make(map[int]float64)
	riderMeters, podMeters, detourTotal, detours := 0.0, 0.0, 0.0, 0
	for _, timing := range timings {
		waits = append(waits, timing.BoardedTick-timing.RequestedTick)
		if timing.CompletedTick < 0 {
			continue
		}
		journeys = append(journeys, timing.CompletedTick-timing.RequestedTick)
		riderMeters += timing.RiddenMeters
		lead := timing.RequestID
		if timing.SharedWith != 0 {
			lead = timing.SharedWith
		}
		farthest[lead] = max(farthest[lead], timing.RiddenMeters)
		if timing.DirectMeters > 0 {
			detourTotal += timing.RiddenMeters / timing.DirectMeters
			detours++
		}
	}
	// The sum follows the order of the timings, so that the result does not
	// depend on map order.
	for _, timing := range timings {
		if meters, ok := farthest[timing.RequestID]; ok && timing.SharedWith == 0 {
			podMeters += meters
			delete(farthest, timing.RequestID)
		}
	}
	for _, request := range state.Pending {
		waits = append(waits, state.Tick-request.RequestedTick)
	}
	slices.Sort(waits)
	slices.Sort(journeys)
	stats := requestStats{waitP95: tickSeconds(percentile95(waits)), journeyP95: tickSeconds(percentile95(journeys))}
	if len(journeys) > 0 {
		total := int64(0)
		for _, journey := range journeys {
			total += journey
		}
		stats.journeyAverage = tickSeconds(total) / float64(len(journeys))
		stats.journeyMaximum = tickSeconds(journeys[len(journeys)-1])
	}
	if podMeters > 0 {
		stats.occupancy = riderMeters / podMeters
	}
	if detours > 0 {
		stats.detourMean = detourTotal / float64(detours)
	}
	return stats
}

// percentile95 gives the nearest-rank 95th percentile of sorted values: the
// value at position ceil(0.95 n), counted from 1. It gives 0 for no values.
func percentile95(sorted []int64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (95*len(sorted) + 99) / 100
	return sorted[rank-1]
}

func tickSeconds(ticks int64) float64 { return float64(ticks) / sim.TicksPerSecond }
