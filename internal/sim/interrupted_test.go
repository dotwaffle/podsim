package sim

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// checkOrderBalance checks the order balance of the incident contract,
// section 8.3: each submitted order is complete, interrupted, queued,
// aboard, or unaccounted. Each interrupted order has at least one
// passenger, and the saved form and the snapshot have the same counters.
func checkOrderBalance(s *Simulation) error {
	aboard := 0
	for index := range s.vehicles {
		aboard += s.vehicles[index].RidersAboard()
	}
	if s.requestID != s.completed+s.interrupted+len(s.waiting)+aboard+s.unaccountedOrders {
		return fmt.Errorf("%d submitted orders, but %d complete, %d interrupted, %d queued, %d aboard, and %d unaccounted",
			s.requestID, s.completed, s.interrupted, len(s.waiting), aboard, s.unaccountedOrders)
	}
	if s.interrupted < 0 || s.interruptedPassengers < s.interrupted || len(s.undelivered) > s.interrupted {
		return fmt.Errorf("%d interrupted orders with %d passengers and %d undelivered", s.interrupted, s.interruptedPassengers, len(s.undelivered))
	}
	state, snapshot := s.ExportState(), s.Snapshot()
	if state.Interrupted != s.interrupted || state.InterruptedPassengers != s.interruptedPassengers ||
		snapshot.Interrupted != s.interrupted || snapshot.InterruptedPassengers != s.interruptedPassengers {
		return fmt.Errorf("saved counters %d and %d, snapshot counters %d and %d, want %d and %d",
			state.Interrupted, state.InterruptedPassengers, snapshot.Interrupted, snapshot.InterruptedPassengers,
			s.interrupted, s.interruptedPassengers)
	}
	return nil
}

// TestInterruptRider checks the effect of an interruption (incident
// contract, section 8.2): the rider and its aligned boarding record leave
// the pod, the order counts as interrupted and waits for delivery, and no
// completion, journey, distance, or experiment record changes.
func TestInterruptRider(t *testing.T) {
	t.Parallel()
	s, v := pooledLegFleet(t, true)
	s.SetExperimentRecords(true)
	v.Boardings = []RiderBoarding{{BerthID: "s0-1"}, {BerthID: "s0-1"}}
	v.riddenBase, v.distance = 40, 10
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	// The party size of the interrupted rider is not 1, so the passenger
	// count shows it. The pod is checked again only after the rider left.
	v.Riders[0].PartySize = 2
	before := s.ExportState()
	completions, records := slices.Clone(s.stepCompletions), slices.Clone(s.requestCompletions)
	s.interruptRider(v, 0)
	if len(v.Riders) != 1 || v.Riders[0].ID != 3 || len(v.Boardings) != 1 {
		t.Fatalf("pod 01 has riders %+v and records %+v", v.Riders, v.Boardings)
	}
	if s.interrupted != 1 || s.interruptedPassengers != 2 || !slices.Equal(s.undelivered, []int{2}) {
		t.Fatalf("interrupted %d with %d passengers, undelivered %v", s.interrupted, s.interruptedPassengers, s.undelivered)
	}
	if !slices.Equal(s.stepCompletions, completions) || !slices.Equal(s.requestCompletions, records) {
		t.Fatal("the interruption wrote a completion record")
	}
	after := s.ExportState()
	unchanged := func(state SavedState) []any {
		return []any{state.Completed, state.Boarded, state.Journeys, state.TotalJourneyTicks, state.MaxJourneyTicks,
			state.RiderDistanceMeters, state.DirectDistanceMeters, state.MaxDetourRatio, state.RequestID, state.TotalWaitTicks}
	}
	if !reflect.DeepEqual(unchanged(after), unchanged(before)) {
		t.Fatalf("counters %v, want %v", unchanged(after), unchanged(before))
	}
	if v.riddenBase != 40 || v.distance != 10 {
		t.Fatalf("ridden base %v and distance %v changed", v.riddenBase, v.distance)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	if err := checkOrderBalance(s); err != nil {
		t.Fatal(err)
	}
	if got := s.DrainInterruptions(); !slices.Equal(got, []int{2}) {
		t.Fatalf("drained %v, want [2]", got)
	}
	if got := s.DrainInterruptions(); got != nil || s.interrupted != 1 {
		t.Fatalf("second drain %v with %d interrupted orders", got, s.interrupted)
	}
}

// TestInterruptRiderRefusals checks that the public entry refuses each
// failed precondition and changes nothing. A case with want pins the
// error text.
func TestInterruptRiderRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		pod    string
		order  int
		change func(*Simulation, *vehicle)
		want   string
	}{
		{name: "unknown pod", pod: "09", order: 2},
		{name: "unknown order", pod: "01", order: 9},
		{name: "order of another pod", pod: "02", order: 2},
		{name: "completed rider", pod: "01", order: 2, change: func(_ *Simulation, v *vehicle) { v.Riders[0].Completed = true }},
		{name: "unshared destination", pod: "01", order: 2, change: func(_ *Simulation, v *vehicle) { v.Riders[1].To = "s1" }},
		{name: "completed companion", pod: "01", order: 2, change: func(_ *Simulation, v *vehicle) { v.Riders[1].Completed = true }},
		{name: "misaligned records", pod: "01", order: 2, change: func(_ *Simulation, v *vehicle) { v.Boardings = []RiderBoarding{{BerthID: "s0-1"}} }},
		{name: "dispatch pass", pod: "01", order: 2, change: func(s *Simulation, _ *vehicle) { s.pass = &dispatchPass{active: true} }},
		{name: "no incident marker", pod: "01", order: 2, change: func(s *Simulation, _ *vehicle) { s.incidentContract = "" }},
		{name: "platoon member", pod: "01", order: 2, change: func(_ *Simulation, v *vehicle) { v.follower = 2 },
			want: "pod 01: interruption of a platoon member"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := pooledLegFleet(t, true)
			s.incidentContract = IncidentV1Contract
			if test.change != nil {
				test.change(s, v)
			}
			before, riders := s.ExportState(), slices.Clone(v.Riders)
			err := s.InterruptRider(test.pod, test.order)
			if err == nil {
				t.Fatal("the interruption is accepted")
			}
			if test.want != "" && err.Error() != test.want {
				t.Fatalf("error %q, want %q", err, test.want)
			}
			if !reflect.DeepEqual(s.ExportState(), before) || !reflect.DeepEqual(v.Riders, riders) ||
				s.interrupted != 0 || s.interruptedPassengers != 0 || s.undelivered != nil {
				t.Fatal("a refused interruption changed the state")
			}
		})
	}
	s, _ := pooledLegFleet(t, true)
	s.incidentContract = IncidentV1Contract
	if err := s.InterruptRider("01", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.InterruptRider("01", 3); err == nil || !strings.Contains(err.Error(), "no other active rider") {
		t.Fatalf("interruption of the last rider: %v", err)
	}
}

// TestInterruptionConservation runs shared rides and interrupts a rider
// whenever another rider of its pod shares its destination. The order
// balance and the state contract hold after each tick and each
// interruption (incident contract, section 8.3).
func TestInterruptionConservation(t *testing.T) {
	t.Parallel()
	skipLong(t)
	s := newLegFleet(t, "s0-1", "s1-1")
	s.incidentContract = IncidentV1Contract
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	s.monitor = func(s *Simulation) {
		if err := s.CheckContract(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		if err := checkOrderBalance(s); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
	}
	trips := []struct{ from, to string }{{"s0", "s2"}, {"s0", "s2"}, {"s1", "s2"}, {"s1", "s2"}, {"s2", "s0"}, {"s2", "s0"}}
	var delivered []int
	for second := 0; second < 3600 && (second < 400 || s.requestID != s.completed+s.interrupted); second++ {
		if second%40 == 0 && second < 400 {
			for _, trip := range trips {
				if _, err := s.SubmitTripOptions(TripOptions{From: trip.from, To: trip.to, SharingConsent: SharedConsent}); err != nil {
					t.Fatal(err)
				}
			}
		}
		for range TicksPerSecond {
			s.Step()
			s.interruptSharedRider(t)
		}
		delivered = append(delivered, s.DrainInterruptions()...)
	}
	if s.interrupted < 3 || s.completed == 0 || len(delivered) != s.interrupted || s.interruptedPassengers != s.interrupted {
		t.Fatalf("%d interrupted with %d passengers, %d delivered, %d completed", s.interrupted, s.interruptedPassengers, len(delivered), s.completed)
	}
	if s.requestID != s.completed+s.interrupted {
		t.Fatalf("%d orders are not done", s.requestID-s.completed-s.interrupted)
	}
}

// interruptSharedRider interrupts the first active rider whose destination
// another active rider of its pod shares, at most once per pod and tick.
// Pod 01 interrupts while it travels and pod 02 while it unloads.
func (s *Simulation) interruptSharedRider(t *testing.T) {
	t.Helper()
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.Pod.Activity != map[string]Activity{"01": Traveling, "02": Unloading}[v.Pod.ID] {
			continue
		}
		for _, rider := range v.Riders {
			shared := !rider.Completed && slices.ContainsFunc(v.Riders, func(other Request) bool {
				return other.ID != rider.ID && !other.Completed && other.To == rider.To
			})
			if shared {
				if err := s.InterruptRider(v.Pod.ID, rider.ID); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
}

// TestInterruptedCountersRestore checks that both restore tiers and a
// clone keep the counters, that a reset clears them, and that a restore
// without the incident marker refuses them (incident contract, sections
// 8.3 and 8.7).
func TestInterruptedCountersRestore(t *testing.T) {
	t.Parallel()
	s, v := pooledLegFleet(t, false)
	s.interruptRider(v, 0)
	clone := s.Clone()
	clone.undelivered[0] = 9
	if !slices.Equal(s.undelivered, []int{1}) || clone.interrupted != 1 || clone.interruptedPassengers != 1 {
		t.Fatalf("clone: %v, %d, %d; source %v", clone.undelivered, clone.interrupted, clone.interruptedPassengers, s.undelivered)
	}
	input := RestoreStateInput{Network: s.network, Fleet: s.initial, IncidentContract: IncidentV1Contract, State: s.ExportState()}
	for _, logical := range []bool{false, true} {
		input.LogicalOnly = logical
		restored, result, err := RestoreState(input)
		if err != nil || len(result.Dropped) != 0 || result.Unaccounted != 0 || len(result.Interrupted) != 0 {
			t.Fatalf("logical %t: %+v %v", logical, result, err)
		}
		if restored.interrupted != 1 || restored.interruptedPassengers != 1 || restored.undelivered != nil {
			t.Fatalf("logical %t: %d, %d, %v", logical, restored.interrupted, restored.interruptedPassengers, restored.undelivered)
		}
		if err := checkOrderBalance(restored); err != nil {
			t.Fatal(err)
		}
	}
	input.IncidentContract = ""
	if _, _, err := RestoreState(input); err == nil || !strings.Contains(err.Error(), "incident contract") {
		t.Fatalf("restore without the marker: %v", err)
	}
	s.Reset()
	if s.interrupted != 0 || s.interruptedPassengers != 0 || s.undelivered != nil {
		t.Fatalf("reset kept %d, %d, %v", s.interrupted, s.interruptedPassengers, s.undelivered)
	}
}

// TestInterruptedCounterValidation checks the counter rules of the saved
// state (incident contract, section 8.3).
func TestInterruptedCounterValidation(t *testing.T) {
	t.Parallel()
	s, v := pooledLegFleet(t, false)
	s.interruptRider(v, 0)
	valid := s.ExportState()
	if _, err := valid.checkContract(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*SavedState)
	}{
		{"negative orders", func(state *SavedState) { state.Interrupted = -1 }},
		{"negative passengers", func(state *SavedState) { state.InterruptedPassengers = -1 }},
		{"fewer passengers than orders", func(state *SavedState) { state.InterruptedPassengers = 0 }},
		{"more outcomes than orders", func(state *SavedState) { state.Completed, state.Interrupted, state.InterruptedPassengers = 1, 2, 2 }},
		{"order balance", func(state *SavedState) { state.Completed = 1 }},
	} {
		state := valid
		test.change(&state)
		if _, err := state.checkContract(); err == nil {
			t.Fatalf("%s: the state is accepted", test.name)
		}
	}
	// The order balance also refuses more outcomes than orders, so check
	// the counter rule on its own.
	state := valid
	state.Completed, state.Interrupted, state.InterruptedPassengers = 1, 2, 2
	if err := state.validateCounters(); err == nil {
		t.Fatal("the counters accept more outcomes than orders")
	}
	state = valid
	state.Interrupted, state.InterruptedPassengers = 0, 0
	if unaccounted, err := state.checkContract(); err != nil || unaccounted != 1 {
		t.Fatalf("a state without the counters has %d unaccounted orders: %v", unaccounted, err)
	}
}
