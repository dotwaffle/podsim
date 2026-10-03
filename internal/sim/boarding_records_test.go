package sim

import (
	"math"
	"reflect"
	"testing"
)

func recordedMetricFixture(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	harbor, _ := s.station("harbor")
	market, _ := s.station("market")
	v := &s.vehicles[0]
	v.origin, v.journeyOrigin, v.destination = harbor.Berths[0], harbor.Berths[0], market.Berths[0]
	v.Pod.Activity, v.Pod.Occupied = Unloading, true
	v.Pod.StationID, v.Pod.BerthID = market.ID, market.Berths[0].ID
	v.destinationStation = market.ID
	v.Riders = []Request{
		{ID: 1, From: "harbor", To: "market", PartySize: 1, PodID: "01", SharingConsent: SharedConsent, Service: OnDemandService},
		{ID: 2, From: "garden", To: "market", PartySize: 1, PodID: "01", SharingConsent: SharedConsent, Service: OnDemandService},
	}
	v.Boardings = []RiderBoarding{{BerthID: "harbor-1"}, {BerthID: "garden-1", MetersAtBoarding: 400}}
	v.riddenBase, v.distance = 1000, 200
	s.tick, s.requestID, s.boarded, s.recordExperiments = 1200, 2, 2, true
	return s
}

// This unit fixture checks metrics without claiming reachable occupied admission.
func TestBoardingRecordsAlightMetrics(t *testing.T) {
	t.Parallel()
	s := recordedMetricFixture(t)
	v := &s.vehicles[0]
	harbor, _ := s.station("harbor")
	garden, _ := s.station("garden")
	wantDirect := []float64{
		s.directDistance(harbor.Berths[0].Node, "market", v.destination),
		s.directDistance(garden.Berths[0].Node, "market", v.destination),
	}
	if wantDirect[0] <= 0 || wantDirect[1] <= 0 || wantDirect[0] == wantDirect[1] {
		t.Fatal("fixture needs distinct direct distances from real boarding berths")
	}
	s.alight(v)
	if s.completed != 2 || s.riderDistanceMeters != 2000 || s.directDistanceMeters != wantDirect[0]+wantDirect[1] {
		t.Fatalf("completion totals: %d, ridden %v, direct %v", s.completed, s.riderDistanceMeters, s.directDistanceMeters)
	}
	for index, wantRidden := range []float64{1200, 800} {
		got := s.requestCompletions[index]
		if got.riddenMeters != wantRidden || got.directMeters != wantDirect[index] {
			t.Fatalf("party %d completion: %+v", index, got)
		}
	}
	if v.riddenBase != 1200 || v.distance != 200 {
		t.Fatalf("final alight changed physical distance or failed to freeze total: %v %v", v.riddenBase, v.distance)
	}
	completions := append([]requestCompletion(nil), s.requestCompletions...)
	v.Pod.Activity, v.Pod.Occupied, v.RelocatingTo = Traveling, false, "harbor"
	v.distance = 5000
	if v.riddenMeters() != 1200 || !reflect.DeepEqual(completions, s.requestCompletions) {
		t.Fatal("empty movement changed passenger or completed-party distances")
	}
	if got := s.ExportState().Pods[0]; got.RiddenMeters != 1200 || len(got.Boardings) != 2 {
		t.Fatalf("empty recorded history lost its frozen total: %+v", got)
	}
}

func TestBoardingRecordsDistancePhases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		activity  Activity
		occupied  bool
		completed bool
		distance  float64
		want      float64
	}{
		{"occupied boarding", Boarding, true, false, 0, 1000},
		{"occupied travel", Traveling, true, false, 200, 1200},
		{"intermediate unload", Unloading, true, false, 0, 1000},
		{"continuing", Continuing, true, false, 0, 1000},
		{"idle history", Idle, false, true, 200, 1000},
		{"empty departure history", DepartingEmpty, false, true, 0, 1000},
		{"empty traveling history", Traveling, false, true, 5000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := recordedMetricFixture(t)
			v := &s.vehicles[0]
			v.Pod.Activity, v.Pod.Occupied, v.distance = tc.activity, tc.occupied, tc.distance
			for index := range v.Riders {
				v.Riders[index].Completed = tc.completed
			}
			if got := v.riddenMeters(); got != tc.want {
				t.Fatalf("cumulative distance %v, want %v", got, tc.want)
			}
			if got := s.Snapshot().Vehicles[0]; got.RiddenMeters != tc.want || len(got.Boardings) != 2 {
				t.Fatalf("snapshot lost phase distance: %+v", got)
			}
		})
	}
}

func TestBoardingRecordsOwnership(t *testing.T) {
	t.Parallel()
	s := recordedMetricFixture(t)
	want := s.vehicles[0].Boardings[1]
	clone, snapshot, saved := s.Clone(), s.Snapshot(), s.ExportState()
	clone.vehicles[0].Boardings[1].MetersAtBoarding = 17
	snapshot.Vehicles[0].Boardings[1].BerthID = "changed"
	saved.Pods[0].Boardings[1].MetersAtBoarding = 19
	if got := s.vehicles[0].Boardings[1]; got != want {
		t.Fatalf("view shares authoritative record: %+v", got)
	}
}

func TestBoardingRecordsLegacyCanonicalExport(t *testing.T) {
	t.Parallel()
	s := newTwoStopRide(t)
	v := s.findVehicle("01")
	wantState, wantSnapshot := s.ExportState(), s.Snapshot()
	for range v.Riders {
		v.Boardings = append(v.Boardings, RiderBoarding{BerthID: v.journeyOrigin.ID})
	}
	if len(v.Boardings) == 0 {
		t.Fatal("fixture has no real modern riders to promote")
	}
	if !reflect.DeepEqual(wantState, s.ExportState()) || !reflect.DeepEqual(wantSnapshot, s.Snapshot()) {
		t.Fatal("proven zero-baseline records changed the ordinary representation")
	}
	if len(v.Boardings) != len(v.Riders) {
		t.Fatal("canonical export removed authoritative records")
	}
	v.riddenBase = 1
	v.Boardings[0].MetersAtBoarding = math.SmallestNonzeroFloat64
	if got := s.ExportState().Pods[0]; len(got.Boardings) != len(v.Riders) || got.JourneyOrigin != "" {
		t.Fatal("positive baseline was omitted or rebased")
	}
}
