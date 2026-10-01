package project

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// MaxRailDeparturePassengers bounds the saved outbound connection ledger.
const MaxRailDeparturePassengers = 3000

// RailDeparture offers pod trips to a hub before a scheduled train leaves.
type RailDeparture struct {
	ID                  string       `json:"id"`
	Station             string       `json:"station"`
	AtSeconds           int          `json:"atSeconds"`
	WalkingSeconds      int          `json:"walkingSeconds"`
	RequestFromSeconds  int          `json:"requestFromSeconds"`
	RequestUntilSeconds int          `json:"requestUntilSeconds"`
	Passengers          int          `json:"passengers"`
	Origins             []RailOrigin `json:"origins"`
}

// RailOrigin gives one origin and its integer sampling weight.
type RailOrigin struct {
	Station string `json:"station"`
	Weight  int    `json:"weight"`
}

// RailServiceOffer identifies an arrival or a departure passenger offer.
// DepartureTick and WalkingTicks are zero for an arrival.
type RailServiceOffer struct {
	RailOffer
	Kind          string
	DepartureTick int64
	WalkingTicks  int64
}

func validateRailServices(arrivals []RailArrival, departures []RailDeparture, network sim.Network) error {
	if err := validateRailArrivals(arrivals, network); err != nil {
		return err
	}
	if len(arrivals)+len(departures) > MaxRailArrivals {
		return fmt.Errorf("rail plans must contain at most %d combined events", MaxRailArrivals)
	}
	passenger := make(map[string]bool)
	for _, station := range PassengerStations(network) {
		passenger[station.ID] = true
	}
	ids := make(map[string]bool, len(departures))
	releases := make(map[int64]int)
	total, outbound := 0, 0
	for _, arrival := range arrivals {
		total += arrival.Passengers
		releases[railReleaseTick(arrival)] += arrival.Passengers
	}
	for index, departure := range departures {
		if err := validateRailDeparture(departure, passenger, ids); err != nil {
			return fmt.Errorf("rail departure %d: %w", index+1, err)
		}
		ids[departure.ID] = true
		total += departure.Passengers
		outbound += departure.Passengers
		if outbound > MaxRailDeparturePassengers {
			return fmt.Errorf("rail departures must offer at most %d passengers", MaxRailDeparturePassengers)
		}
		if total > MaxRailPassengers {
			return fmt.Errorf("rail plans must offer at most %d combined passengers", MaxRailPassengers)
		}
		for ordinal := 1; ordinal <= departure.Passengers; ordinal++ {
			tick := DepartureOfferTick(departure, ordinal)
			releases[tick]++
			if releases[tick] > MaxRailRelease {
				return fmt.Errorf("rail plans at tick %d exceed %d passengers", tick, MaxRailRelease)
			}
		}
	}
	return nil
}

func validateRailDeparture(departure RailDeparture, passenger, ids map[string]bool) error {
	if departure.ID == "" || len(departure.ID) > maxIDLength || ids[departure.ID] {
		return fmt.Errorf("invalid or duplicate ID %s", quoteID(departure.ID))
	}
	if !passenger[departure.Station] {
		return fmt.Errorf("%s needs a passenger hub", quoteID(departure.ID))
	}
	if departure.AtSeconds < 1 || departure.AtSeconds > 86400 ||
		departure.WalkingSeconds < 0 || departure.WalkingSeconds > 3600 ||
		departure.RequestFromSeconds < 0 || departure.RequestUntilSeconds < departure.RequestFromSeconds ||
		departure.RequestUntilSeconds >= departure.AtSeconds-departure.WalkingSeconds {
		return fmt.Errorf("%s has an invalid departure time, walking delay, or request window", quoteID(departure.ID))
	}
	if departure.Passengers < 1 || departure.Passengers > MaxRailRelease {
		return fmt.Errorf("%s must offer 1 to %d passengers", quoteID(departure.ID), MaxRailRelease)
	}
	return validateRailOrigins(departure, passenger)
}

func validateRailOrigins(departure RailDeparture, passenger map[string]bool) error {
	if len(departure.Origins) < 1 || len(departure.Origins) > MaxRailDestinations {
		return fmt.Errorf("%s needs 1 to %d origins", quoteID(departure.ID), MaxRailDestinations)
	}
	seen := make(map[string]bool, len(departure.Origins))
	for _, origin := range departure.Origins {
		if !passenger[origin.Station] || origin.Station == departure.Station || seen[origin.Station] {
			return fmt.Errorf("%s has an invalid or duplicate origin %s", quoteID(departure.ID), quoteID(origin.Station))
		}
		if origin.Weight < 1 || origin.Weight > 1_000_000 {
			return fmt.Errorf("%s origin %s needs a weight from 1 to 1000000", quoteID(departure.ID), quoteID(origin.Station))
		}
		seen[origin.Station] = true
	}
	return nil
}

// DepartureOfferTick returns the release tick for a one-based passenger ordinal.
// The departure and ordinal must satisfy the validated plan limits.
func DepartureOfferTick(departure RailDeparture, ordinal int) int64 {
	start := int64(departure.RequestFromSeconds) * sim.TicksPerSecond
	end := int64(departure.RequestUntilSeconds) * sim.TicksPerSecond
	if departure.Passengers > 1 {
		start += int64(ordinal-1) * (end - start) / int64(departure.Passengers-1)
	}
	return max(1, start)
}

// RailServicesSchedule returns an owned schedule for validated rail plans.
// Arrival ties precede departures. Each kind keeps event and passenger order.
// Arrival choices retain the RailSchedule stream unchanged.
func RailServicesSchedule(arrivals []RailArrival, departures []RailDeparture, seed uint64) []RailServiceOffer {
	count := 0
	for _, arrival := range arrivals {
		count += arrival.Passengers
	}
	for _, departure := range departures {
		count += departure.Passengers
	}
	if count == 0 {
		return nil
	}
	offers := make([]RailServiceOffer, 0, count)
	for _, offer := range RailSchedule(arrivals, seed) {
		offers = append(offers, RailServiceOffer{RailOffer: offer, Kind: "arrival"})
	}
	for _, departure := range departures {
		offers = appendDepartureOffers(offers, departure, seed)
	}
	slices.SortStableFunc(offers, func(a, b RailServiceOffer) int { return cmp.Compare(a.Tick, b.Tick) })
	return offers
}

func appendDepartureOffers(offers []RailServiceOffer, departure RailDeparture, seed uint64) []RailServiceOffer {
	// The fixed domain exceeds every valid arrival ID, so an arrival cannot
	// reproduce this hash input by placing the domain inside its ID.
	var prefix [8 + maxIDLength + 1]byte
	binary.LittleEndian.PutUint64(prefix[:], seed)
	copy(prefix[8:], "rail-departure")
	digest := sha256.Sum256(append(prefix[:], departure.ID...))
	rng := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(digest[:8]), binary.LittleEndian.Uint64(digest[8:16])))
	total := 0
	for _, origin := range departure.Origins {
		total += origin.Weight
	}
	for ordinal := 1; ordinal <= departure.Passengers; ordinal++ {
		target := rng.IntN(total)
		for _, origin := range departure.Origins {
			if target < origin.Weight {
				offers = append(offers, RailServiceOffer{
					RailOffer: RailOffer{Tick: DepartureOfferTick(departure, ordinal), Event: departure.ID,
						Passenger: ordinal, From: origin.Station, To: departure.Station},
					Kind: "departure", DepartureTick: int64(departure.AtSeconds) * sim.TicksPerSecond,
					WalkingTicks: int64(departure.WalkingSeconds) * sim.TicksPerSecond,
				})
				break
			}
			target -= origin.Weight
		}
	}
	return offers
}

func cloneRailDepartures(departures []RailDeparture) []RailDeparture {
	cloned := slices.Clone(departures)
	for index := range cloned {
		cloned[index].Origins = slices.Clone(cloned[index].Origins)
	}
	return cloned
}
