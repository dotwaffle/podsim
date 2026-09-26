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
			t.Parallel()
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

func TestPickupForecastUsesStationBeforeBerthAssignment(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	v.Pod.Activity, v.Pod.Occupied = Traveling, true
	v.phaseTicks = 0

	node, seconds, ok := s.availableAfter(v)
	if !ok || node != "market-berth" {
		t.Fatalf("forecast destination = %q, ok=%t", node, ok)
	}
	approach := s.routeSeconds(v.Route, motionEstimate{}) + float64(unloadingTicks)/TicksPerSecond
	if seconds <= approach {
		t.Fatalf("forecast omitted station access: got %.2fs, approach %.2fs", seconds, approach)
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

// finishingPodSetup places idle pod 01 at harbor and pod 02 unloading at
// busyStation, then requests a trip from market to harbor.
type finishingPodSetup struct {
	rule          FinishingPodWait
	busyStation   string
	unloadSeconds int
}

func newFinishingPodTrip(t *testing.T, setup finishingPodSetup) (*Simulation, *vehicle) {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: setup.busyStation}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFinishingPodWait(setup.rule); err != nil {
		t.Fatal(err)
	}
	busy := s.findVehicle("02")
	busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
	busy.phaseTicks = setup.unloadSeconds * TicksPerSecond
	busy.Request = &Request{ID: 1, From: "harbor", To: setup.busyStation, PartySize: 1, PodID: "02"}
	s.requestID = 1
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	return s, busy
}

func TestFinishingPodWaitRules(t *testing.T) {
	t.Parallel()
	// The idle pod at harbor needs about 62 s to reach market. A pod that
	// unloads at garden needs about 39 s of empty travel after it finishes.
	// A pod that unloads at market needs no travel.
	for _, tc := range []struct {
		name          string
		rule          FinishingPodWait
		busyStation   string
		unloadSeconds int
		hold          bool
	}{
		{"current holds for a pod inside the hold time", FinishingPodWaitCurrent, "market", 5, true},
		{"strict holds for a pod inside the hold time", FinishingPodWaitStrict, "market", 5, true},
		{"none never holds", FinishingPodWaitNone, "market", 5, false},
		{"current holds past the hold time", FinishingPodWaitCurrent, "market", 45, true},
		{"strict holds for a pod that arrives at the end of the hold time", FinishingPodWaitStrict, "market", 30, true},
		{"strict ignores a pod that finishes after the hold time", FinishingPodWaitStrict, "market", 45, false},
		{"current counts travel past the hold time", FinishingPodWaitCurrent, "garden", 5, true},
		{"strict counts travel to the pickup", FinishingPodWaitStrict, "garden", 5, false},
		{"current needs the advantage", FinishingPodWaitCurrent, "market", 70, false},
		{"strict needs the advantage", FinishingPodWaitStrict, "market", 70, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := newFinishingPodTrip(t, finishingPodSetup{rule: tc.rule, busyStation: tc.busyStation, unloadSeconds: tc.unloadSeconds})
			pending := s.Snapshot().Pending[0]
			if tc.hold {
				if pending.PodID != "" || !strings.Contains(pending.DispatchReason, "02") {
					t.Fatalf("did not hold for the finishing pod: %+v", pending)
				}
				return
			}
			if pending.PodID != "01" {
				t.Fatalf("did not send the idle pod: %+v", pending)
			}
		})
	}
}

func TestStrictFinishingPodWaitUsesRemainingHold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rule FinishingPodWait
		want string
	}{
		{"current keeps the hold", FinishingPodWaitCurrent, ""},
		{"strict sends the idle pod", FinishingPodWaitStrict, "01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, busy := newFinishingPodTrip(t, finishingPodSetup{rule: tc.rule, busyStation: "market", unloadSeconds: 5})
			// The forecast stays at 5 s for 10 s, then moves to 25 s. Only
			// 20 s of the hold remain, so the strict rule stops the hold.
			for tick := range 12 * TicksPerSecond {
				busy.phaseTicks = 5 * TicksPerSecond
				if tick >= 10*TicksPerSecond {
					busy.phaseTicks = 25 * TicksPerSecond
				}
				s.Step()
			}
			if got := s.Snapshot().Pending[0].PodID; got != tc.want {
				t.Fatalf("pickup pod = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestKeepHoldBetweenChecks checks that a trip on hold between two checks of
// waitForFinishingPod gets the result of the full dispatch pass. Pod 02
// unloads at Market and holds the trip from Market. The hold starts at tick
// 0, and the next check is at 1 s.
func TestKeepHoldBetweenChecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// change changes the simulation at 0.5 s, before the next check.
		change     func(s *Simulation, busy *vehicle)
		wantPod    string
		wantReason string
	}{
		{
			name:       "hold continues",
			change:     func(*Simulation, *vehicle) {},
			wantReason: "Waiting for pod 02 to finish",
		},
		{
			name:    "a pod becomes idle at the pickup station",
			change:  func(_ *Simulation, busy *vehicle) { busy.phaseTicks = 1 },
			wantPod: "02",
		},
		{
			name: "no pod is available",
			change: func(s *Simulation, _ *vehicle) {
				s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 3, From: "harbor", To: "garden", PartySize: 1, PodID: "01"}})
			},
			wantReason: "Waiting for an available pod",
		},
		{
			name:    "the hold time ends before the next check",
			change:  func(s *Simulation, _ *vehicle) { s.waiting[0].deferUntil = s.tick + 1 },
			wantPod: "01",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, busy := newFinishingPodTrip(t, finishingPodSetup{rule: FinishingPodWaitCurrent, busyStation: "market", unloadSeconds: 5})
			if trip := s.waiting[0]; trip.request.PodID != "" || trip.deferCheck != s.tick+TicksPerSecond {
				t.Fatalf("the trip is not on hold: %+v", trip)
			}
			advance(s, TicksPerSecond/2)
			tc.change(s, busy)
			s.Step()
			var trip Request
			for _, pending := range s.Snapshot().Pending {
				if pending.ID == 2 {
					trip = pending
				}
			}
			if tc.wantPod == "02" {
				if trip.ID != 0 || busy.Pod.Activity != Boarding || busy.Request == nil || busy.Request.ID != 2 {
					t.Fatalf("pod 02 did not board the trip at once: %+v, %+v", trip, busy.Vehicle)
				}
				return
			}
			if trip.PodID != tc.wantPod || trip.DispatchReason != tc.wantReason && tc.wantReason != "" {
				t.Fatalf("trip 2 has pod %q and reason %q, want pod %q and reason %q", trip.PodID, trip.DispatchReason, tc.wantPod, tc.wantReason)
			}
		})
	}
}

// TestKeepHoldWaitsForCongestionRefresh checks that dispatch does the full
// pass when the congestion costs are due for a refresh. The first route
// query of the pass then refreshes them, as before.
func TestKeepHoldWaitsForCongestionRefresh(t *testing.T) {
	t.Parallel()
	s, _ := newFinishingPodTrip(t, finishingPodSetup{rule: FinishingPodWaitCurrent, busyStation: "market", unloadSeconds: 5})
	s.SetCongestionRouting(true)
	pass := dispatchPass{assigned: map[string]bool{}}
	if s.keepHold(&s.waiting[0], &pass) {
		t.Fatal("keepHold kept the hold when a congestion refresh was due")
	}
	s.refreshCongestionCosts()
	if !s.keepHold(&s.waiting[0], &pass) {
		t.Fatal("keepHold did not keep the hold after the refresh")
	}
}

func TestSetFinishingPodWaitRejectsUnknownRule(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []FinishingPodWait{-1, FinishingPodWaitNone + 1} {
		if err := s.SetFinishingPodWait(rule); err == nil {
			t.Fatalf("rule %d was accepted", rule)
		}
	}
}
