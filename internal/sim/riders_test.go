package sim

import (
	"math"
	"strings"
	"testing"
)

// TestJourneyAndDetourTotals checks the journey and distance totals of two
// parties that share a ride on the free-flow route.
func TestJourneyAndDetourTotals(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	if err := s.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	advance(s, TicksPerSecond)
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	var state Snapshot
	for state = s.Snapshot(); state.Completed < 2; state = s.Snapshot() {
		if state.Tick > 600*TicksPerSecond {
			t.Fatalf("the shared ride did not complete: %+v", state)
		}
		s.Step()
	}
	first := float64(state.Tick) / TicksPerSecond
	second := first - 1
	if state.Journey.MaxSeconds != first || state.Journey.AverageSeconds != (first+second)/2 {
		t.Fatalf("journey stats %+v at %.0f s", state.Journey, first)
	}
	if state.RiderDistanceMeters <= 0 || math.Abs(state.RiderDistanceMeters-2*state.PassengerDistanceMeters) > 1e-6 ||
		math.Abs(state.RiderDistanceMeters-state.DirectDistanceMeters) > 1e-6 || math.Abs(state.MaxDetourRatio-1) > 1e-9 {
		t.Fatalf("rider %.3f m, direct %.3f m, pod %.3f m, detour %.9f", state.RiderDistanceMeters,
			state.DirectDistanceMeters, state.PassengerDistanceMeters, state.MaxDetourRatio)
	}
}

// TestDirectDistanceIgnoresCongestionRouting checks that the direct distance
// is the free-flow distance with congestion routing on, and that the call
// does not compute the congestion costs.
func TestDirectDistanceIgnoresCongestionRouting(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	station, _ := s.station("market")
	berth := station.Berths[0]
	harbor, _ := s.station("harbor")
	from := harbor.Berths[0].Node
	free := s.directDistance(from, "market", berth)
	if free <= 0 {
		t.Fatalf("free-flow distance %.3f", free)
	}
	s.SetCongestionRouting(true)
	if got := s.directDistance(from, "market", berth); got != free || s.congestionRouteCosts != nil {
		t.Fatalf("direct distance with congestion routing is %.3f, want %.3f, costs %v", got, free, s.congestionRouteCosts)
	}
	if got := s.directDistance(from, "parking", berth); got != -1 {
		t.Fatalf("direct distance to a berth of another station is %.3f, want -1", got)
	}
}

// newTwoStopRide returns a simulation with pod 01 boarding at harbor with a
// party for garden and a party for market. The route to market passes the
// approach of garden.
func newTwoStopRide(t *testing.T) *Simulation {
	t.Helper()
	s := newSharingSimulation(t)
	if err := submitSharedTrip(s, "harbor", "garden"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if v.Pod.Activity != Boarding {
		t.Fatalf("pod 01 is %s", v.Pod.Activity)
	}
	s.requestID++
	s.boarded++
	v.Riders = append(v.Riders, Request{
		SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID, From: "harbor", To: "market", PartySize: 1, PodID: "01", RequestedTick: s.tick, BoardedTick: s.tick,
	})
	v.Stops = []string{"garden", "market"}
	return s
}

// runTwoStopRide steps s until both parties complete, checks the pod after
// the intermediate stop, and returns the final snapshot. After each step in
// which the pod is at the intermediate stop or on its way to market with
// one party aboard, it calls change, which can return a new simulation.
func runTwoStopRide(t *testing.T, s *Simulation, change func(*Simulation) *Simulation) Snapshot {
	t.Helper()
	continued := false
	for state := s.Snapshot(); state.Completed < 2; state = s.Snapshot() {
		if state.Tick > 600*TicksPerSecond {
			t.Fatalf("the two stop ride did not complete: %+v", state)
		}
		if pod := state.Vehicles[0]; len(pod.Stops) == 1 && pod.Pod.StationID != "market" {
			continued = true
			if !pod.Pod.Occupied || (state.Completed == 1) != pod.Riders[0].Completed || pod.Riders[1].Completed || pod.Stops[0] != "market" {
				t.Fatalf("pod after the intermediate stop: %+v", pod)
			}
			if change != nil {
				s = change(s)
			}
		}
		s.Step()
	}
	if !continued {
		t.Fatal("the pod did not stop at garden")
	}
	return s.Snapshot()
}

func TestPodContinuesAfterIntermediateStop(t *testing.T) {
	t.Parallel()
	s := newTwoStopRide(t)
	harbor, _ := s.station("harbor")
	garden, _ := s.station("garden")
	market, _ := s.station("market")
	from := harbor.Berths[0].Node
	toGarden := s.directDistance(from, "garden", garden.Berths[0])
	toMarket := s.directDistance(garden.Berths[0].Node, "market", market.Berths[0])
	direct := s.directDistance(from, "market", market.Berths[0])
	state := runTwoStopRide(t, s, nil)
	ridden := toGarden + toMarket
	if math.Abs(state.RiderDistanceMeters-(toGarden+ridden)) > 1e-6 || math.Abs(state.DirectDistanceMeters-(toGarden+direct)) > 1e-6 ||
		math.Abs(state.MaxDetourRatio-ridden/direct) > 1e-9 || state.MaxDetourRatio <= 1 {
		t.Fatalf("rider %.3f m, direct %.3f m, detour %.6f, want legs %.3f and %.3f, direct %.3f",
			state.RiderDistanceMeters, state.DirectDistanceMeters, state.MaxDetourRatio, toGarden, toMarket, direct)
	}
	pod := state.Vehicles[0]
	if pod.Pod.StationID != "market" || pod.RidersAboard() != 0 || len(pod.Stops) != 0 {
		t.Fatalf("pod after the last stop: %+v", pod)
	}
}

// TestIntermediateStopSurvivesRestore checks that a physical restore at the
// intermediate stop or on the last leg keeps the journey origin and the
// ridden distance. A pod waits as Continuing only when it does not get
// track at once, so the continuing case ends the unloading in the test. A
// restore does not keep the speed of a moving pod, so the test does not
// compare the times.
func TestIntermediateStopSurvivesRestore(t *testing.T) {
	t.Parallel()
	restore := func(t *testing.T, s *Simulation) *Simulation {
		t.Helper()
		restored, result, err := RestoreState(RestoreStateInput{
			Network: Example(), Fleet: []Placement{
				{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
				{ID: "02", StationID: "garden", BerthID: "garden-1"},
			},
			State: roundTripState(t, s.ExportState()),
		})
		if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 || len(result.Requeued) != 0 {
			t.Fatalf("restore: %v, %+v", err, result)
		}
		monitorContract(t, restored)
		return restored
	}
	continueNow := func(s *Simulation) {
		if v := s.findVehicle("01"); v.Pod.Activity == Unloading {
			s.alight(v)
			s.continueJourney(v)
		}
	}
	for _, test := range []struct {
		name     string
		activity Activity
		prepare  func(*Simulation)
	}{
		{name: "unloading", activity: Unloading},
		{name: "continuing", activity: Unloading, prepare: continueNow},
		{name: "traveling", activity: Traveling},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			run := func(restoring bool) Snapshot {
				done := false
				return runTwoStopRide(t, newTwoStopRide(t), func(s *Simulation) *Simulation {
					if done || s.findVehicle("01").Pod.Activity != test.activity {
						return s
					}
					done = true
					if test.prepare != nil {
						test.prepare(s)
					}
					if !restoring {
						return s
					}
					if test.prepare != nil && s.findVehicle("01").Pod.Activity != Continuing {
						t.Fatalf("pod is %s", s.findVehicle("01").Pod.Activity)
					}
					return restore(t, s)
				})
			}
			want, got := run(false), run(true)
			if got.RiderDistanceMeters != want.RiderDistanceMeters || got.DirectDistanceMeters != want.DirectDistanceMeters ||
				got.MaxDetourRatio != want.MaxDetourRatio {
				t.Fatalf("restored run: rider %.6f m, direct %.6f m, detour %.9f, want %.6f m, %.6f m, %.9f",
					got.RiderDistanceMeters, got.DirectDistanceMeters, got.MaxDetourRatio,
					want.RiderDistanceMeters, want.DirectDistanceMeters, want.MaxDetourRatio)
			}
		})
	}
}

// TestRestoreRejectsUnloadingPodAwayFromDestination checks that both
// restore tiers reject a pod that unloads at the intermediate stop when its
// saved destination is not its berth. The next leg of the pod starts at the
// destination berth, so without the check the pod could not continue, and
// the party for market would never arrive.
func TestRestoreRejectsUnloadingPodAwayFromDestination(t *testing.T) {
	t.Parallel()
	s := newTwoStopRide(t)
	stepUntil(t, s, "pod 01 unloading at garden", func() bool {
		pod := s.findVehicle("01").Pod
		return pod.Activity == Unloading && pod.StationID == "garden"
	})
	fleet := []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
	}
	saved := roundTripState(t, s.ExportState())
	if _, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: fleet, State: saved}); err != nil || result.Tier != RestorePhysical {
		t.Fatalf("restore of the saved state: %v, %+v", err, result)
	}
	for _, test := range []struct {
		name string
		edit func(*SavedPod)
	}{
		{name: "no destination", edit: func(pod *SavedPod) { pod.Destination = "" }},
		{name: "other destination berth", edit: func(pod *SavedPod) { pod.Destination = "harbor-1" }},
		{name: "other destination station", edit: func(pod *SavedPod) { pod.DestinationStation = "market" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := roundTripState(t, s.ExportState())
			test.edit(&state.Pods[0])
			restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: fleet, State: state})
			const want = "pod 01: the unloading pod is not at its destination"
			if err == nil || restored != nil || result.PhysicalError == nil || !strings.Contains(result.PhysicalError.Error(), want) ||
				strings.Count(err.Error(), want) != 2 {
				t.Fatalf("restore: %v, %+v, want both tiers to fail with %q", err, result, want)
			}
		})
	}
}

// TestFinishEstimateIncludesLaterStops checks that a pod with a later stop
// becomes available at its last stop, after the later leg.
func TestFinishEstimateIncludesLaterStops(t *testing.T) {
	t.Parallel()
	s := newTwoStopRide(t)
	v := s.findVehicle("01")
	node, seconds, ok := s.finishEstimate(v)
	single := *v
	single.Stops = []string{"garden"}
	gardenNode, gardenSeconds, gardenOK := s.finishEstimate(&single)
	market, _ := s.station("market")
	garden, _ := s.station("garden")
	if !ok || !gardenOK || node != market.Berths[0].Node || gardenNode != garden.Berths[0].Node ||
		seconds <= gardenSeconds+float64(unloadingTicks)/TicksPerSecond || v.lastStop() != "market" {
		t.Fatalf("estimate %s after %.1f s, single stop %s after %.1f s, last stop %s", node, seconds, gardenNode, gardenSeconds, v.lastStop())
	}
}

// directDistance is directDistanceForClass for LegacyClass.
func (s *Simulation) directDistance(from, stationID string, berth Berth) float64 {
	return s.directDistanceForClass(from, stationID, berth, LegacyClass)
}
