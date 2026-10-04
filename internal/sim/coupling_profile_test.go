package sim

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCouplingProfile(t *testing.T) {
	t.Parallel()
	want := CouplingProfile{
		Contract: CompactPairV1CouplingContract, Class: CompactClass, Members: 2, SeatsPerMember: 4,
		BodyLengthMeters: 4, BodyWidthMeters: 2, PinOffsetMeters: 2,
		FreeGapMeters: 0.5, ConnectorWidthMeters: 0.3, CenterSpacingMeters: 4.5, OccupiedLengthMeters: 8.5,
		Acceleration: 2, Braking: 2, ManeuverSpeed: 0.5, ManeuverAcceleration: 0.5, ManeuverBraking: 0.5,
		LatchTicks: 120, UnlatchTicks: 120, PartnerWaitTicks: 300,
	}
	got, ok := LookupCouplingProfile(CompactPairV1CouplingContract)
	if !ok || got != want {
		t.Fatalf("profile = %+v, want %+v", got, want)
	}
	got.BodyLengthMeters = 100
	again, _ := LookupCouplingProfile(CompactPairV1CouplingContract)
	if again != want {
		t.Fatal("caller changed the selected model")
	}
	for _, contract := range []CouplingContract{"", "unknown", "express-v1"} {
		if _, ok := LookupCouplingProfile(contract); ok {
			t.Fatalf("unexpected profile for %q", contract)
		}
		if _, err := CouplingSiteRoom(contract); !errors.Is(err, ErrUnknownCouplingContract) {
			t.Fatal(contract, err)
		}
	}
}

func TestCouplingSiteRoom(t *testing.T) {
	t.Parallel()
	room, err := CouplingSiteRoom(CompactPairV1CouplingContract)
	if err != nil {
		t.Fatal(err)
	}
	// Independent arithmetic uses the approved speed, braking, and tick values.
	stop := 0.5*0.5/(2*0.5) + 0.5/60
	margin := 2.0 + 12.0 + stop
	want := CouplingRoom{StagingSpacingMeters: 12, OpeningTravelMeters: 7.5,
		StoppingMeters: stop, BoundaryMarginMeters: margin,
		RequiredLengthMeters: 12 + 7.5 + 2*margin, BodyWidthMeters: 2}
	if room != want {
		t.Fatalf("room = %+v, want %+v", room, want)
	}
	input := couplingGeometryFixture()
	site := &input.Sites[0]
	site.StartMeters = 0
	site.RearStagingMeters = margin
	site.FrontStagingMeters = margin + 12
	site.EndMeters = room.RequiredLengthMeters
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatalf("exact room rejected: %v", err)
	}
	site.EndMeters = math.Nextafter(site.EndMeters, 0)
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrInvalidCouplingGeometry) {
		t.Fatalf("one-ulp-short room accepted: %v", err)
	}
}

func TestCouplingDisabledGeometry(t *testing.T) {
	t.Parallel()
	input := CouplingGeometryInput{Network: Network{Nodes: []Node{{ID: "", Position: Point{X: math.NaN()}}}}}
	before := input.Network.Nodes[0]
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal(err)
	}
	if input.Network.Nodes[0].ID != before.ID || !math.IsNaN(input.Network.Nodes[0].Position.X) {
		t.Fatal("ordinary input changed")
	}
	input.Contract = CompactPairV1CouplingContract
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal("geometry-free opt-in rejected", err)
	}
	input.Contract = "unknown"
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrUnknownCouplingContract) {
		t.Fatal("unknown geometry-free marker accepted", err)
	}
	input.Contract = ""
	input.Sites = []CouplingSite{{ID: "unmarked"}}
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrUnknownCouplingContract) {
		t.Fatal("unmarked descriptor accepted", err)
	}
	input.Sites = nil
	input.Corridors = []CouplingCorridor{{ID: "unmarked"}}
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrUnknownCouplingContract) {
		t.Fatal("unmarked corridor accepted", err)
	}
	input.Contract = "unknown"
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrUnknownCouplingContract) {
		t.Fatal("unknown marker accepted", err)
	}
	profile, _ := LookupVehicleClass(CompactClass)
	if !reflect.DeepEqual(profile, VehicleClassSpec{CompactClass, 4, 4, 4, true}) {
		t.Fatal("foundation class changed")
	}
}
