package sim

import "testing"

func TestSameDestinationPartiesShareBoardingPod(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	if err := s.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	state := s.Snapshot()
	vehicle := state.Vehicles[0]
	if state.Submitted != 2 || len(state.Pending) != 0 || state.SharedParties != 1 ||
		vehicle.Pod.Activity != Boarding || vehicle.Parties != 2 || vehicle.Request == nil || vehicle.Request.PartySize != 2 {
		t.Fatalf("same-destination parties did not share: %+v", state)
	}
	advance(s, 300*TicksPerSecond)
	if state = s.Snapshot(); state.Completed != 2 || !state.Vehicles[0].Request.Completed || state.Vehicles[0].Parties != 0 {
		t.Fatalf("shared parties did not complete: %+v", state)
	}
}

func TestSameDestinationSharingHonorsPolicyAndDestination(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		limit        int
		destinations []string
		wantParties  int
		wantShared   int
		wantPending  int
	}{
		{name: "disabled", limit: 1, destinations: []string{"market", "market"}, wantParties: 1, wantPending: 1},
		{name: "capacity", limit: 2, destinations: []string{"market", "market", "market"}, wantParties: 2, wantShared: 1, wantPending: 1},
		{name: "different destination", limit: 2, destinations: []string{"market", "garden"}, wantParties: 1, wantPending: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newSharingSimulation(t)
			if err := s.SetSharedRidePartyLimit(test.limit); err != nil {
				t.Fatal(err)
			}
			for _, destination := range test.destinations {
				if err := s.RequestTrip("harbor", destination); err != nil {
					t.Fatal(err)
				}
			}
			state := s.Snapshot()
			if state.Vehicles[0].Parties != test.wantParties || state.SharedParties != test.wantShared || len(state.Pending) != test.wantPending {
				t.Fatalf("sharing policy mismatch: %+v", state)
			}
		})
	}
}

func TestSharedRidePartyLimitRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	s := newSharingSimulation(t)
	for _, limit := range []int{0, MaxSharedRideParties + 1} {
		if err := s.SetSharedRidePartyLimit(limit); err == nil {
			t.Fatalf("accepted party limit %d", limit)
		}
	}
}

func newSharingSimulation(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
