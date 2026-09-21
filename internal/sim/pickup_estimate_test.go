package sim

import (
	"strings"
	"testing"
)

func TestWaitForFinishingPod(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		unloadSeconds int
		wait          bool
	}{
		{"closer pod finishes soon", 5, true},
		{"closer pod takes too long", 120, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			busy := s.findVehicle("02")
			busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
			busy.phaseTicks = tc.unloadSeconds * TicksPerSecond
			busy.Request = &Request{ID: 1, From: "harbor", To: "garden", PartySize: 1, PodID: "02"}
			s.requestID = 1
			if err := s.RequestTrip("market", "harbor"); err != nil {
				t.Fatal(err)
			}
			pending := s.Snapshot().Pending[0]
			if tc.wait {
				if pending.PodID != "" || !strings.Contains(pending.DispatchReason, "02") || s.findVehicle("01").Pod.Activity != Idle {
					t.Fatalf("did not wait for nearer pod: %+v", pending)
				}
				advance(s, tc.unloadSeconds*TicksPerSecond)
				if got := s.Snapshot().Pending[0].PodID; got != "02" {
					t.Fatalf("finishing pod was not assigned: %q", got)
				}
			} else if pending.PodID != "01" {
				t.Fatalf("did not send faster idle pod: %+v", pending)
			}
		})
	}
}

func TestForecastWaitIsBounded(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	busy := s.findVehicle("02")
	busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
	busy.phaseTicks = 5 * TicksPerSecond
	busy.Request = &Request{ID: 1, From: "harbor", To: "garden", PartySize: 1, PodID: "02"}
	s.requestID = 1
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	// Keep delaying completion to model a forecast that never becomes true.
	for range maxDispatchDeferral {
		busy.phaseTicks = 5*TicksPerSecond + 1
		s.Step()
	}
	if got := s.Snapshot().Pending[0].PodID; got != "01" {
		t.Fatalf("forecast kept idle pod waiting indefinitely: %q", got)
	}
}

func TestPickupForecastIncludesCommittedPassengerTrip(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "parking")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	// A previous trip must not replace the committed pickup's destination.
	v.Request = &Request{ID: 99, From: "market", To: "garden", Completed: true}
	node, seconds, ok := s.availableAfter(v)
	if !ok || node != "market-berth" {
		t.Fatalf("forecast ignored committed passenger trip: node=%s ok=%v", node, ok)
	}
	emptyRemaining := s.routeSeconds(v.Route, motionEstimate{})
	if seconds <= emptyRemaining+float64(boardingTicks+unloadingTicks)/TicksPerSecond {
		t.Fatal("forecast omitted passenger travel")
	}
}

func TestWaitStatistics(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.RequestTrip("harbor", "garden"); err != nil {
			t.Fatal(err)
		}
	}
	advance(s, 10*TicksPerSecond)
	state := s.Snapshot()
	if state.Wait.AverageSeconds != 5 || state.Wait.MaxSeconds != 10 {
		t.Fatalf("pending wait not counted: %+v", state.Wait)
	}
	s.SetPaused(true)
	advance(s, 100)
	if s.Snapshot().Wait != state.Wait {
		t.Fatal("pause increased wait")
	}
	s.SetPaused(false)
	for range 900 * TicksPerSecond {
		s.Step()
		if len(s.waiting) == 0 {
			break
		}
	}
	state = s.Snapshot()
	if len(state.Pending) != 0 || state.Wait.MaxSeconds != float64(state.Tick)/TicksPerSecond || state.Wait.AverageSeconds != state.Wait.MaxSeconds/2 {
		t.Fatalf("wait did not stop at boarding: %+v", state)
	}
	advance(s, 100)
	if s.Snapshot().Wait != state.Wait {
		t.Fatal("boarding time counted as pickup wait")
	}
	s.Reset()
	if s.Snapshot().Wait != (WaitStats{}) {
		t.Fatal("reset retained wait statistics")
	}
}

func TestFourOutstandingOrdersComplete(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	for range 4 {
		if err := s.RequestTrip("harbor", "market"); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.waiting) != 3 {
		t.Fatalf("expected one boarding and three queued orders, got %d queued", len(s.waiting))
	}
	for range 1500 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.completed == 4 {
			break
		}
	}
	if s.completed != 4 || len(s.waiting) != 0 || s.requestID != 4 {
		t.Fatalf("four orders did not complete: %+v", s.Snapshot())
	}
}
