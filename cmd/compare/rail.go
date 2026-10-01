package main

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

const maxRailOfferIndices = 1_000_000

func validateRailOptions(opts options, given map[string]bool) error {
	if !slices.Contains(opts.patterns, "rail-arrivals") && !slices.Contains(opts.patterns, "rail-services") {
		return nil
	}
	for _, name := range []string{"request-every", "loads", "burst-size", "adaptive-limit", "past-limit"} {
		if given[name] {
			return fmt.Errorf("rail demand does not accept -%s: volume comes from the project plan", name)
		}
	}
	return nil
}

// Bound index backing storage before any simulation starts. Each rail result
// allocates at most one index per scheduled offer, including policy copies.
func validateRailMatrix(opts options, arms []demandArm, scenario scenario, armsPerSchedule int) error {
	copies := int64(len(opts.seeds)) * int64(len(opts.loads)) * int64(armsPerSchedule) * int64(len(opts.redistributionPolicies))
	var indices, outcomes, storage int64
	for _, arm := range arms {
		if arm.pattern != "rail-arrivals" && arm.pattern != "rail-services" {
			continue
		}
		offers := railOfferCount(scenario.railArrivals, durationTicks(opts.arrivalsFor))
		records := 0
		if arm.pattern == "rail-services" {
			planned, window := departureOfferCounts(scenario.railDepartures, durationTicks(opts.arrivalsFor))
			offers += window
			// Charge both the full live ledger and the owned report copy.
			records = planned + window
		}
		indices += copies * int64(offers)
		outcomes += copies * int64(records)
		storage += copies * (8*int64(offers) + 1024*int64(records) + 512*int64(offers))
		if indices > maxRailOfferIndices || outcomes > maxRailOfferIndices || storage > 256<<20 {
			return fmt.Errorf("rail matrix exceeds its index, outcome, or 256 MiB storage bound")
		}
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

func departureOfferCounts(plan []project.RailDeparture, end int64) (planned, window int) {
	for _, departure := range plan {
		planned += departure.Passengers
		for passenger := 1; passenger <= departure.Passengers; passenger++ {
			if project.DepartureOfferTick(departure, passenger) < end {
				window++
			}
		}
	}
	return
}

func railServiceSchedule(input scheduleInput) []scheduledRequest {
	seed := uint64(input.seed) // #nosec G115 -- Preserve signed seed bits.
	_, departures := departureOfferCounts(input.railDepartures, input.durationTicks)
	requests := make([]scheduledRequest, 0, railOfferCount(input.railArrivals, input.durationTicks)+departures)
	for _, offer := range project.RailServicesSchedule(input.railArrivals, input.railDepartures, seed) {
		if offer.Tick >= input.durationTicks {
			break
		}
		requests = append(requests, scheduledRequest{tick: offer.Tick, origin: offer.From, destination: offer.To, event: offer.Event, passenger: offer.Passenger, kind: offer.Kind, departureTick: offer.DepartureTick, walkingTicks: offer.WalkingTicks})
	}
	return requests
}

func (r scheduledRequest) serviceOffer() project.RailServiceOffer {
	return project.RailServiceOffer{RailOffer: project.RailOffer{Tick: r.tick, Event: r.event, Passenger: r.passenger, From: r.origin, To: r.destination}, Kind: r.kind, DepartureTick: r.departureTick, WalkingTicks: r.walkingTicks}
}

type connectionReport struct {
	rail.Counts
	Offers []rail.Connection `json:"offers"`
}

func reportConnections(connections *rail.Connections) *connectionReport {
	if connections == nil {
		return nil
	}
	records := connections.Records()
	if len(records) == 0 {
		return nil
	}
	return &connectionReport{Counts: connections.Counts(), Offers: records}
}

func connectionPendingWithin(connections *rail.Connections, plan []project.RailDeparture, capTick int64) bool {
	if connections == nil || connections.Counts().Unresolved == 0 {
		return false
	}
	for _, record := range connections.Records() {
		if record.Outcome != "pending" {
			continue
		}
		for _, departure := range plan {
			if departure.ID == record.Event && int64(departure.AtSeconds)*sim.TicksPerSecond <= capTick {
				return true
			}
		}
	}
	return false
}

func futureRailTargets(schedule []scheduledRequest, cursor int, connections *rail.Connections, tick int64) []sim.ForecastTarget {
	byStation := make(map[string]sim.ForecastTarget)
	for _, offer := range schedule[cursor:] {
		if offer.tick <= tick {
			continue
		}
		if offer.tick-tick > 300*sim.TicksPerSecond {
			break
		}
		if offer.kind == "departure" && connections != nil && connections.Issued(offer.event, offer.passenger) {
			continue
		}
		target := byStation[offer.origin]
		if target.Passengers == 0 {
			target = sim.ForecastTarget{Station: offer.origin, ReleaseTick: offer.tick}
		}
		target.Passengers++
		byStation[offer.origin] = target
	}
	targets := make([]sim.ForecastTarget, 0, len(byStation))
	for _, target := range byStation {
		targets = append(targets, target)
	}
	slices.SortFunc(targets, func(a, b sim.ForecastTarget) int {
		if c := cmp.Compare(a.ReleaseTick, b.ReleaseTick); c != 0 {
			return c
		}
		return cmp.Compare(a.Station, b.Station)
	})
	return targets
}
