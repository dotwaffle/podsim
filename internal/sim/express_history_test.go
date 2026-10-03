package sim

import (
	"reflect"
	"testing"
)

func newExpressOccupiedPickupRide(t *testing.T, network Network) *Simulation {
	t.Helper()
	s, err := NewFleetWithOrderContract(network, []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOnboardPickups(true); err != nil {
		t.Fatal(err)
	}
	s.SetExperimentRecords(true)
	for _, pair := range [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"garden", "market"}} {
		if err := submitSharedTrip(s, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for range 300 * TicksPerSecond {
		before := largeMotionPods(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
		v := &s.vehicles[0]
		if v.Pod.Activity == Boarding && v.Pod.Occupied {
			return s
		}
	}
	t.Fatalf("occupied pickup did not occur: %+v", s.Snapshot())
	return nil
}
func TestExpressRepeatedHistoryRetirement(t *testing.T) {
	t.Parallel()
	s := newExpressOccupiedPickupRide(t, expressNetwork(largeRestoreNetwork()))
	monitorContract(t, s)
	if err := submitSharedTrip(s, "garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	nextDestination := map[string]string{"market": "garden", "harbor": "market", "garden": "harbor"}
	for range 22 {
		v := &s.vehicles[0]
		pickup := v.Stops[0]
		oldRecords := make(map[int]RiderBoarding)
		for index, rider := range v.Riders {
			oldRecords[rider.ID] = v.Boardings[index]
		}
		if err := submitSharedTrip(s, pickup, nextDestination[pickup]); err != nil {
			t.Fatal(err)
		}
		joined := false
		for range 600 * TicksPerSecond {
			before := largeMotionPods(s)
			s.Step()
			checkExpressMotionTick(t, s, before)
			if v.Pod.Activity == Boarding && v.Pod.Occupied && v.Pod.StationID == pickup {
				joined = true
				break
			}
		}
		if !joined || len(v.Riders) != min(len(oldRecords)+1, MaxExpressParties) || len(v.Boardings) != len(v.Riders) {
			t.Fatalf("repeated pickup failed at %s: %+v", pickup, s.Snapshot())
		}
		for index, rider := range v.Riders {
			if old, retained := oldRecords[rider.ID]; retained && v.Boardings[index] != old {
				t.Fatal("history retirement changed a retained origin or baseline")
			}
		}
		if v.Boardings[len(v.Boardings)-1].MetersAtBoarding != v.riddenMeters() || countUnaccounted(t, s) != 0 {
			t.Fatal("repeated pickup lost a baseline or an order")
		}
		if len(v.Riders) == MaxExpressParties {
			oldest := s.requestID - MaxExpressParties + 1
			if v.Riders[0].ID != oldest {
				t.Fatalf("history did not retire oldest first: oldest %d riders %+v", oldest, v.Riders)
			}
		}
	}
	expressEvidence(t, "rollover", s.ExportState())
	v := &s.vehicles[0]
	if v.Boardings[0].MetersAtBoarding <= 0 {
		t.Fatal("history retirement rebased a positive baseline")
	}
	for range 600 * TicksPerSecond {
		before := largeMotionPods(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
	}
	if s.completed != s.requestID || len(s.requestCompletions) != s.requestID || s.maxDetourRatio > maxSharedRideDetour+1e-9 || len(v.Riders) > MaxExpressParties || countUnaccounted(t, s) != 0 {
		t.Fatalf("repeated pickup completion: %+v", s.Snapshot())
	}
}

func TestExpressFoundationAndObservationParity(t *testing.T) {
	n := Example()
	fleet := []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "parking", BerthID: "parking-1"}}
	off, err := NewFleet(n, fleet)
	if err != nil {
		t.Fatal(err)
	}
	opt, err := NewFleetWithOrderContract(n, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	pairs := [][2]string{{"harbor", "market"}, {"garden", "harbor"}, {"market", "garden"}}
	for tick := range 12000 {
		if tick%300 == 0 {
			pair := pairs[(tick/300)%len(pairs)]
			for _, s := range []*Simulation{off, opt} {
				if _, err := s.SubmitTripOptions(TripOptions{From: pair[0], To: pair[1]}); err != nil {
					t.Fatal(err)
				}
			}
		}
		off.Step()
		before := largeMotionPods(opt)
		opt.Step()
		checkExpressMotionTick(t, opt, before)
		a, b := off.ExportState(), opt.ExportState()
		b.OrderContract = ""
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("foundation parity drift tick %d", tick)
		}
	}
	expressEvidence(t, "foundation", off.ExportState())
	expressEvidence(t, "opt-in-old-only", opt.ExportState())
	plain := opt.Clone()
	observed := opt.Clone()
	for range 1200 {
		plain.Step()
		before := largeMotionPods(observed)
		observed.Step()
		checkExpressMotionTick(t, observed, before)
		if !reflect.DeepEqual(plain.ExportState(), observed.ExportState()) {
			t.Fatal("oracle altered native trajectory")
		}
	}
}
