package sim

import (
	"errors"
	"slices"
	"testing"
)

// TestTripRefusalNamesCause checks that a refused trip names its cause and
// the stations by name. A party that fits only a class that does not serve
// both stations is too large, not unreachable.
func TestTripRefusalNamesCause(t *testing.T) {
	t.Parallel()
	compactOnly := func(t *testing.T) *Simulation {
		t.Helper()
		network := mixedDepotNetwork()
		for i := range network.Stations {
			if station := &network.Stations[i]; station.ID == "garden" {
				station.VehicleClasses = classBit(string(CompactClass))
				for j := range station.Berths {
					station.Berths[j].VehicleClasses = classBit(string(CompactClass))
				}
			}
		}
		s, err := NewFleetWithOrderContract(network, []Placement{
			{ID: "express", StationID: "harbor", Class: ExpressClass},
			{ID: "compact", StationID: "parking", BerthID: depotSlotID(1, 1), Class: CompactClass},
		}, ExpressOrderContract)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	legacy := func(t *testing.T, network Network) *Simulation {
		t.Helper()
		s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	noGardenApproach := Example()
	noGardenApproach.Lanes = slices.DeleteFunc(noGardenApproach.Lanes, func(l Lane) bool { return l.ID == "garden-approach" })
	for _, test := range []struct {
		name  string
		s     func(*testing.T) *Simulation
		party int
		want  error
		text  string
	}{
		{"compact fits", compactOnly, 4, nil, ""},
		{"only Express takes the party", compactOnly, 8, ErrPartyAdmission, "party of 8 does not fit a pod that serves Harbor and Garden (largest 4): party admission refused"},
		{"legacy takes one rider", func(t *testing.T) *Simulation { t.Helper(); return legacy(t, Example()) }, 2, ErrPartyAdmission, "party of 2 does not fit a pod that serves Harbor and Garden (largest 1): party admission refused"},
		{"no route", func(t *testing.T) *Simulation { t.Helper(); return legacy(t, noGardenApproach) }, 1, ErrUnreachable, "passenger route Harbor to Garden: destination is unreachable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := test.s(t)
			_, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "garden", PartySize: test.party, SharingConsent: PrivateConsent})
			if !errors.Is(err, test.want) || test.want == nil && err != nil || test.want != nil && err.Error() != test.text {
				t.Fatalf("error = %v, want %q", err, test.text)
			}
		})
	}
}
