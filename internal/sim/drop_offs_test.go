package sim

import (
	"slices"
	"testing"
)

// newDropOffsSimulation returns the sharing fleet in drop-offs mode with a
// limit of 4 parties and the stop limit.
func newDropOffsSimulation(t *testing.T, maxStops int) *Simulation {
	t.Helper()
	s := newSharingSimulation(t)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, maxStops); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStationsOnRoute(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	for _, test := range []struct {
		from, to string
		want     []string
	}{
		{"harbor-berth", "market", []string{"garden", "market"}},
		{"harbor-berth", "garden", []string{"garden"}},
		{"garden-berth", "market", []string{"market"}},
		{"market-berth", "harbor", []string{"harbor"}},
	} {
		if got := s.stationsOnRoute(test.from, test.to); !slices.Equal(got, test.want) {
			t.Errorf("stations from %s to %s: %v, want %v", test.from, test.to, got, test.want)
		}
	}
}

func TestDropOffsJoinAddsStops(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		mode         SharedRideMode
		destinations []string
		wantStops    []string
		wantPending  int
	}{
		{name: "earlier stop", mode: SharedRideDropOffs, destinations: []string{"market", "garden"}, wantStops: []string{"garden", "market"}},
		{name: "later stop", mode: SharedRideDropOffs, destinations: []string{"garden", "market"}, wantStops: []string{"garden", "market"}},
		{name: "same stop", mode: SharedRideDropOffs, destinations: []string{"market", "garden", "market"}, wantStops: []string{"garden", "market"}},
		{name: "destination mode", mode: SharedRideDestination, destinations: []string{"market", "garden"}, wantStops: []string{"market"}, wantPending: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newDropOffsSimulation(t, DefaultSharedRideMaxStops)
			if err := s.SetSharedRideMode(test.mode, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			for _, destination := range test.destinations {
				if err := s.RequestTrip("harbor", destination); err != nil {
					t.Fatal(err)
				}
			}
			v := s.findVehicle("01")
			if !slices.Equal(v.Stops, test.wantStops) || len(s.waiting) != test.wantPending || v.destinationStation != test.wantStops[0] ||
				v.Route[len(v.Route)-1].StationID != test.wantStops[0] || len(v.Riders) != len(test.destinations)-test.wantPending {
				t.Fatalf("stops %v to %s, %d riders, %d pending", v.Stops, v.destinationStation, len(v.Riders), len(s.waiting))
			}
			advance(s, 300*TicksPerSecond)
			state := s.Snapshot()
			// Another pod serves a party that did not join.
			if state.Completed != len(test.destinations) || state.SharedParties != len(test.destinations)-1-test.wantPending {
				t.Fatalf("completed %d, shared %d", state.Completed, state.SharedParties)
			}
			if wantDetour := len(test.wantStops) > 1; (state.MaxDetourRatio > 1+1e-9) != wantDetour {
				t.Fatalf("detour ratio %.6f with stops %v", state.MaxDetourRatio, test.wantStops)
			}
		})
	}
}

func TestDropOffStopsLimits(t *testing.T) {
	t.Parallel()
	s := newDropOffsSimulation(t, 1)
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if stops, ok := s.dropOffStops(v, "garden"); !ok || !slices.Equal(stops, []string{"garden", "market"}) {
		t.Fatalf("stops %v, %t", stops, ok)
	}
	v.Stops = []string{"garden", "market"}
	if _, ok := s.dropOffStops(v, "harbor"); ok {
		t.Fatal("a stop beyond the stop limit was added")
	}
	if stops, ok := s.dropOffStops(v, "market"); !ok || !slices.Equal(stops, v.Stops) {
		t.Fatalf("an existing stop was refused: %v, %t", stops, ok)
	}
	v.Stops, v.reservedThrough = []string{"market"}, 0
	if _, ok := s.dropOffStops(v, "garden"); ok {
		t.Fatal("a pod with track added a stop")
	}
	v.reservedThrough, v.destination = -1, Berth{ID: "market-1"}
	if _, ok := s.dropOffStops(v, "garden"); ok {
		t.Fatal("a pod with a berth at its next stop added a stop")
	}
}
