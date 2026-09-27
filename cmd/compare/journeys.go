package main

import (
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// requestStats holds the wait and journey columns of one arm, in seconds,
// and the occupancy.
type requestStats struct {
	waitP95, journeyAverage, journeyP95, journeyMaximum float64
	occupancy                                           float64
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
// the distance of their pod journeys. Each pod journey has one party that
// boarded the pod, and the other parties joined it.
func requestTimeStats(timings []sim.RequestTiming, state sim.Snapshot) requestStats {
	waits := make([]int64, 0, len(timings)+len(state.Pending))
	journeys := make([]int64, 0, len(timings))
	riderMeters, podMeters := 0.0, 0.0
	for _, timing := range timings {
		waits = append(waits, timing.BoardedTick-timing.RequestedTick)
		if timing.CompletedTick < 0 {
			continue
		}
		journeys = append(journeys, timing.CompletedTick-timing.RequestedTick)
		riderMeters += timing.RiddenMeters
		if timing.SharedWith == 0 {
			podMeters += timing.RiddenMeters
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
