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

const (
	// MaxRailArrivals bounds the number of scheduled train arrivals.
	MaxRailArrivals = 256
	// MaxRailPassengers bounds all passenger offers in one plan.
	MaxRailPassengers = 10_000
	// MaxRailRelease bounds passenger offers at one simulation tick.
	MaxRailRelease = 200
	// MaxRailDestinations bounds the weighted destinations of one arrival.
	MaxRailDestinations = 16
)

// RailArrival releases passenger requests after an arrival and walking delay.
type RailArrival struct {
	ID             string            `json:"id"`
	Station        string            `json:"station"`
	AtSeconds      int               `json:"atSeconds"`
	WalkingSeconds int               `json:"walkingSeconds"`
	Passengers     int               `json:"passengers"`
	Destinations   []RailDestination `json:"destinations"`
}

// RailDestination gives one destination and its integer sampling weight.
type RailDestination struct {
	Station string `json:"station"`
	Weight  int    `json:"weight"`
}

// RailOffer identifies one passenger released after the physics of Tick.
type RailOffer struct {
	Tick      int64
	Event     string
	Passenger int
	From      string
	To        string
}

func validateRailArrivals(arrivals []RailArrival, network sim.Network) error {
	if len(arrivals) > MaxRailArrivals {
		return fmt.Errorf("project must contain at most %d rail arrivals", MaxRailArrivals)
	}
	passenger := make(map[string]bool)
	for _, station := range PassengerStations(network) {
		passenger[station.ID] = true
	}
	ids := make(map[string]bool, len(arrivals))
	releases := make(map[int64]int, len(arrivals))
	total := 0
	for index, arrival := range arrivals {
		if arrival.ID == "" || len(arrival.ID) > maxIDLength || ids[arrival.ID] {
			return fmt.Errorf("rail arrival %d has an invalid or duplicate ID", index+1)
		}
		ids[arrival.ID] = true
		if !passenger[arrival.Station] {
			return fmt.Errorf("rail arrival %s needs a passenger hub", quoteID(arrival.ID))
		}
		if arrival.AtSeconds < 0 || arrival.AtSeconds > 86400 || arrival.WalkingSeconds < 0 || arrival.WalkingSeconds > 3600 || arrival.AtSeconds+arrival.WalkingSeconds > 86400 {
			return fmt.Errorf("rail arrival %s has an invalid arrival time or walking delay", quoteID(arrival.ID))
		}
		if arrival.Passengers < 1 || arrival.Passengers > MaxRailRelease {
			return fmt.Errorf("rail arrival %s must offer 1 to %d passengers", quoteID(arrival.ID), MaxRailRelease)
		}
		total += arrival.Passengers
		if total > MaxRailPassengers {
			return fmt.Errorf("rail arrivals must offer at most %d passengers", MaxRailPassengers)
		}
		tick := railReleaseTick(arrival)
		releases[tick] += arrival.Passengers
		if releases[tick] > MaxRailRelease {
			return fmt.Errorf("rail arrivals at tick %d exceed %d passengers", tick, MaxRailRelease)
		}
		if err := validateRailDestinations(arrival, passenger); err != nil {
			return err
		}
	}
	return nil
}

func validateRailDestinations(arrival RailArrival, passenger map[string]bool) error {
	if len(arrival.Destinations) < 1 || len(arrival.Destinations) > MaxRailDestinations {
		return fmt.Errorf("rail arrival %s needs 1 to %d destinations", quoteID(arrival.ID), MaxRailDestinations)
	}
	seen := make(map[string]bool, len(arrival.Destinations))
	for _, destination := range arrival.Destinations {
		if !passenger[destination.Station] || destination.Station == arrival.Station || seen[destination.Station] {
			return fmt.Errorf("rail arrival %s has an invalid or duplicate destination %s", quoteID(arrival.ID), quoteID(destination.Station))
		}
		if destination.Weight < 1 || destination.Weight > 1_000_000 {
			return fmt.Errorf("rail arrival %s destination %s needs a weight from 1 to 1000000", quoteID(arrival.ID), quoteID(destination.Station))
		}
		seen[destination.Station] = true
	}
	return nil
}

func railReleaseTick(arrival RailArrival) int64 {
	return max(1, int64(arrival.AtSeconds+arrival.WalkingSeconds)*sim.TicksPerSecond)
}

// RailSchedule returns an owned offered schedule for a validated plan.
// Each event has a separate random stream. Ties keep event and passenger order.
func RailSchedule(arrivals []RailArrival, seed uint64) []RailOffer {
	var offers []RailOffer
	for _, arrival := range arrivals {
		var prefix [8]byte
		binary.LittleEndian.PutUint64(prefix[:], seed)
		digest := sha256.Sum256(append(prefix[:], arrival.ID...))
		rng := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(digest[:8]), binary.LittleEndian.Uint64(digest[8:16])))
		total := 0
		for _, destination := range arrival.Destinations {
			total += destination.Weight
		}
		for passenger := range arrival.Passengers {
			target := rng.IntN(total)
			for _, destination := range arrival.Destinations {
				if target < destination.Weight {
					offers = append(offers, RailOffer{Tick: railReleaseTick(arrival), Event: arrival.ID, Passenger: passenger + 1, From: arrival.Station, To: destination.Station})
					break
				}
				target -= destination.Weight
			}
		}
	}
	slices.SortStableFunc(offers, func(a, b RailOffer) int { return cmp.Compare(a.Tick, b.Tick) })
	return offers
}

func cloneRailArrivals(arrivals []RailArrival) []RailArrival {
	cloned := slices.Clone(arrivals)
	for index := range cloned {
		cloned[index].Destinations = slices.Clone(cloned[index].Destinations)
	}
	return cloned
}
