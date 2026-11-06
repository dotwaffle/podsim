package scenarios

import (
	"math/rand/v2"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestExpressMixedFleetProgress runs rail-hub with all classes and one Express,
// one Legacy, and one Group pod in turn, under the express-v1 contract. Parties
// of 1 to 20 passengers arrive for 10 minutes. Each order completes, no traveling
// pod stands still for 5 minutes, and the separation oracle runs at each tick.
// The test detects a lock, and it is not a capacity claim. The Express arm of
// cmd/compare has one class and one passenger for each order, so this test is
// the retained run of a mixed fleet with large parties. A short run has one
// seed.
func TestExpressMixedFleetProgress(t *testing.T) {
	t.Parallel()
	seeds := []uint64{1, 2, 3}
	if testing.Short() {
		seeds = seeds[:1]
	}
	for _, seed := range seeds {
		t.Run(strconv.FormatUint(seed, 10), func(t *testing.T) {
			t.Parallel()
			runExpressMixedFleet(t, seed)
		})
	}
}

func runExpressMixedFleet(t *testing.T, seed uint64) {
	t.Helper()
	const (
		arrivals   = 10 * 60 * sim.TicksPerSecond
		every      = 15 * sim.TicksPerSecond
		deadline   = 40 * 60 * sim.TicksPerSecond
		stillLimit = 5 * 60 * sim.TicksPerSecond
	)
	s, classes, stations := expressMixedFleet(t)
	random := rand.New(rand.NewPCG(seed, 0))
	moved := make(map[string]int)
	boarding := make(map[string]bool)
	moving := make(map[sim.VehicleClass]bool)
	longestExpressPhase := 0
	done := func() bool {
		snapshot := s.MetricsSnapshot()
		return snapshot.Submitted > 0 && snapshot.Completed == snapshot.Submitted
	}
	for tick := 0; tick < arrivals || !done(); tick++ {
		if tick >= deadline {
			snapshot := s.MetricsSnapshot()
			t.Fatalf("%d of %d orders complete at the deadline", snapshot.Completed, snapshot.Submitted)
		}
		if tick < arrivals && tick%every == 0 {
			origin := random.IntN(len(stations))
			destination := (origin + 1 + random.IntN(len(stations)-1)) % len(stations)
			if _, err := s.SubmitTripOptions(sim.TripOptions{
				From: stations[origin], To: stations[destination],
				PartySize: 1 + random.IntN(20), SharingConsent: sim.SharedConsent,
			}); err != nil {
				t.Fatalf("tick %d: %v", tick, err)
			}
		}
		s.Step()
		observation := s.SafetyObservation()
		if _, err := observation.Check(); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		for _, pod := range observation.Pods {
			if pod.Speed > 0 {
				moving[classes[pod.ID]] = true
			}
			if pod.Activity != sim.Traveling || pod.Speed > 0 {
				moved[pod.ID] = tick
			} else if tick-moved[pod.ID] > stillLimit {
				t.Fatalf("pod %s has stood still from tick %d to %d", pod.ID, moved[pod.ID], tick)
			}
			starts := pod.Activity == sim.Boarding && !boarding[pod.ID]
			boarding[pod.ID] = pod.Activity == sim.Boarding
			if starts && classes[pod.ID] == sim.ExpressClass {
				longestExpressPhase = max(longestExpressPhase, savedPhase(s, pod.ID))
			}
		}
	}
	if !moving[sim.ExpressClass] || !moving[sim.LegacyClass] || !moving[sim.GroupClass] {
		t.Fatalf("moving classes %v", moving)
	}
	if longestExpressPhase <= 3*sim.TicksPerSecond {
		t.Fatalf("the longest Express boarding phase is %d ticks, so no load dwell ran", longestExpressPhase)
	}
}

// savedPhase returns the phase count of a pod.
func savedPhase(s *sim.Simulation, id string) int {
	for _, pod := range s.ExportState().Pods {
		if pod.ID == id {
			return pod.PhaseTicks
		}
	}
	return 0
}

// expressMixedFleet returns the simulation of the mixed fleet, the class of each
// pod, and the IDs of the stations that take orders.
func expressMixedFleet(t *testing.T) (*sim.Simulation, map[string]sim.VehicleClass, []string) {
	t.Helper()
	all, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	config := RailHub()
	for index := range config.Network.Lanes {
		config.Network.Lanes[index].VehicleClasses = all
	}
	var stations []string
	for index := range config.Network.Stations {
		station := &config.Network.Stations[index]
		if !station.ParkingOnly {
			stations = append(stations, station.ID)
		}
		station.VehicleClasses = all
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = all
		}
	}
	turn := []sim.VehicleClass{sim.ExpressClass, sim.LegacyClass, sim.GroupClass}
	classes := make(map[string]sim.VehicleClass, len(config.Fleet))
	for index := range config.Fleet {
		config.Fleet[index].Class = turn[index%len(turn)]
		classes[config.Fleet[index].ID] = config.Fleet[index].Class
	}
	s, err := sim.NewFleetWithOrderContract(config.Network, config.Fleet, sim.ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	return s, classes, stations
}
