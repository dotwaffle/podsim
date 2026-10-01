package main

import (
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const maxRailOfferIndices = 1_000_000

func validateRailOptions(opts options, given map[string]bool) error {
	if !slices.Contains(opts.patterns, "rail-arrivals") {
		return nil
	}
	for _, name := range []string{"request-every", "loads", "burst-size", "adaptive-limit", "past-limit"} {
		if given[name] {
			return fmt.Errorf("rail-arrivals does not accept -%s: volume comes from the project arrival plan", name)
		}
	}
	return nil
}

// Bound index backing storage before any simulation starts. Each rail result
// allocates at most one index per scheduled offer, including policy copies.
func validateRailMatrix(opts options, arms []demandArm, scenario scenario, armsPerSchedule int) error {
	copies := len(opts.seeds) * len(opts.loads) * armsPerSchedule * len(opts.redistributionPolicies)
	total := 0
	for _, arm := range arms {
		if arm.pattern != "rail-arrivals" {
			continue
		}
		offers := railOfferCount(scenario.railArrivals, durationTicks(opts.arrivalsFor))
		if offers > 0 && copies > (maxRailOfferIndices-total)/offers {
			return fmt.Errorf("rail matrix must retain at most %d offered indices across all results", maxRailOfferIndices)
		}
		total += copies * offers
	}
	return nil
}

func railOfferCount(arrivals []project.RailArrival, endTick int64) int {
	count := 0
	for _, arrival := range arrivals {
		tick := max(int64(1), int64(arrival.AtSeconds+arrival.WalkingSeconds)*sim.TicksPerSecond)
		if tick < endTick {
			count += arrival.Passengers
		}
	}
	return count
}

func railDemandSchedule(input scheduleInput) []scheduledRequest {
	seed := uint64(input.seed) // #nosec G115 -- Preserve the signed seed's bits for deterministic event input.
	offers := project.RailSchedule(input.railArrivals, seed)
	requests := make([]scheduledRequest, 0, railOfferCount(input.railArrivals, input.durationTicks))
	for _, offer := range offers {
		if offer.Tick >= input.durationTicks {
			break
		}
		requests = append(requests, scheduledRequest{
			tick: offer.Tick, origin: offer.From, destination: offer.To, event: offer.Event, passenger: offer.Passenger,
		})
	}
	return requests
}
