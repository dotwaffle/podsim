package sim

import (
	"math"
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
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	advance(s, TicksPerSecond)
	if err := s.RequestTrip("harbor", "market"); err != nil {
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
