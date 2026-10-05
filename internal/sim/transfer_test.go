package sim

import (
	"reflect"
	"slices"
	"testing"
)

// pooledLegFleet returns the simulation of newLegFleet with pods at s0-1
// and s1-1 and a party limit of 4. Pod 01 boards orders 2 and 3 from s0
// to s2. When queued is true, orders 1 and 4 wait at s2. Otherwise they do
// not exist, and pod 01 boards orders 1 and 2.
func pooledLegFleet(t *testing.T, queued bool) (*Simulation, *vehicle) {
	t.Helper()
	s := newLegFleet(t, "s0-1", "s1-1")
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	var before waitingTrip
	if queued {
		before = newTrip(s, "s2", "s0")
	}
	v := s.findVehicle("01")
	if err := s.board(v, newTrip(s, "s0", "s2")); err != nil {
		t.Fatal(err)
	}
	joined := newTrip(s, "s0", "s2")
	if !s.joinSharedRide(&joined, newPass(s)) {
		t.Fatal("order 3 did not join pod 01")
	}
	if queued {
		s.waiting = []waitingTrip{before, newTrip(s, "s2", "s1")}
	}
	return s, v
}

// TestTransferRider checks the effect of a transfer (incident contract,
// section 7.3): the rider and its aligned boarding record leave the pod,
// and the trip waits at its order ID position as a boarded trip with the
// leg origin, no pod, and no exclusion.
func TestTransferRider(t *testing.T) {
	t.Parallel()
	s, v := pooledLegFleet(t, true)
	v.Boardings = []RiderBoarding{{BerthID: "s0-1"}, {BerthID: "s0-1", MetersAtBoarding: 0}}
	rider := v.Riders[0]
	if err := s.transferRider(v, 0, "s1"); err != nil {
		t.Fatal(err)
	}
	if len(v.Riders) != 1 || v.Riders[0].ID != 3 || len(v.Boardings) != 1 {
		t.Fatalf("pod 01 has riders %+v and records %+v", v.Riders, v.Boardings)
	}
	ids := []int{}
	for _, trip := range s.waiting {
		ids = append(ids, trip.request.ID)
	}
	if !slices.Equal(ids, []int{1, 2, 4}) {
		t.Fatalf("queue %v, want order 2 at its order ID position", ids)
	}
	want := rider
	want.PodID, want.DispatchReason, want.LegFrom = "", "", "s1"
	if got := s.waiting[1]; !reflect.DeepEqual(got, waitingTrip{request: want, boarded: true}) {
		t.Fatalf("transferred trip %+v, want %+v", got, waitingTrip{request: want, boarded: true})
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	// A transfer at the order origin also sets the leg origin.
	if err := s.transferRider(v, 0, "s0"); err != nil {
		t.Fatal(err)
	}
	if trip := s.waiting[2]; trip.request.ID != 3 || trip.request.LegFrom != "s0" || len(v.Riders) != 0 || len(v.Boardings) != 0 {
		t.Fatalf("transfer at the order origin: %+v, pod %+v", trip, v.Riders)
	}
}

// TestTransferRiderRefusals checks that a transfer that fails a
// precondition returns an error and changes nothing.
func TestTransferRiderRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		index   int
		station string
		change  func(*vehicle)
	}{
		{name: "negative index", index: -1, station: "s1"},
		{name: "index past the riders", index: 2, station: "s1"},
		{name: "completed rider", station: "s1", change: func(v *vehicle) { v.Riders[0].Completed = true }},
		{name: "destination", station: "s2"},
		{name: "parking station", station: "p"},
		{name: "unknown station", station: "nowhere"},
		{name: "empty station", station: ""},
		{name: "misaligned records", station: "s1", change: func(v *vehicle) { v.Boardings = []RiderBoarding{{BerthID: "s0-1"}} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := pooledLegFleet(t, true)
			if test.change != nil {
				test.change(v)
			}
			before, waiting := s.ExportState(), slices.Clone(s.waiting)
			if err := s.transferRider(v, test.index, test.station); err == nil {
				t.Fatal("the transfer is accepted")
			}
			if !reflect.DeepEqual(s.ExportState(), before) || !reflect.DeepEqual(s.waiting, waiting) {
				t.Fatal("a refused transfer changed the state")
			}
		})
	}
}

// TestContinuationFeasible checks that a continuation is feasible only
// from a leg origin with a path for some fleet pod.
func TestContinuationFeasible(t *testing.T) {
	t.Parallel()
	s, _, _ := newStrandedFleet(t)
	request := newTrip(s, "harbor", "market").request
	if !s.continuationFeasible(request, "harbor") || s.continuationFeasible(request, "garden") {
		t.Fatal("feasibility does not follow the path from the leg origin")
	}
}

// checkTransferSave checks a save of s: the state contract, a clean
// physical restore that matches s, and a logical restore. It returns the
// physical restore.
func checkTransferSave(t *testing.T, s *Simulation, input RestoreStateInput) *Simulation {
	t.Helper()
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	input.State = s.ExportState()
	restored, result, err := RestoreState(input)
	if err != nil || !cleanRestore(result) {
		t.Fatalf("physical restore: %+v %v", result, err)
	}
	checkRestoredMatches(t, s, restored)
	input.LogicalOnly = true
	if _, result, err := RestoreState(input); err != nil || len(result.Dropped) != 0 {
		t.Fatalf("logical restore: %+v %v", result, err)
	}
	return restored
}

// TestStrandedTransfer checks an Express party that a transfer leaves at
// garden, where no Express pod has a path to its destination (incident
// contract, section 7.6). The trip waits with the reason for a missing
// certified vehicle, and both restore tiers keep it. A saved trip of the
// same shape with a pod, a route, a hold, or no boarding is refused.
func TestStrandedTransfer(t *testing.T) {
	t.Parallel()
	s, network, fleet := newStrandedFleet(t)
	services := []ExpressService{{ID: "harbor-market", Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20}}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "harbor-market"}); err != nil {
			t.Fatal(err)
		}
	}
	v := s.findVehicle("01")
	if len(v.Riders) != 2 {
		t.Fatalf("pod 01 has %+v", v.Riders)
	}
	if s.continuationFeasible(v.Riders[0], "garden") {
		t.Fatal("the continuation from garden is feasible")
	}
	if err := s.transferRider(v, 0, "garden"); err != nil {
		t.Fatal(err)
	}
	input := RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, ExpressServices: services}
	// The command boundary after the transfer, then the end of the tick
	// after the next dispatch.
	checkTransferSave(t, s, input)
	s.Step()
	restored := checkTransferSave(t, s, input)
	if len(s.waiting) != 1 || s.waiting[0].request.PodID != "" ||
		s.waiting[0].request.DispatchReason != "Waiting for a certified vehicle that fits this party and route" {
		t.Fatalf("the stranded trip %+v", s.waiting)
	}
	if trip := restored.waiting[0]; trip.request.LegFrom != "garden" || !trip.boarded {
		t.Fatalf("the restored trip %+v", trip)
	}
	// An on-demand order needs no service pair.
	onDemand := input
	onDemand.State = s.ExportState()
	onDemand.State.Waiting[0].Request.Service, onDemand.State.Waiting[0].Request.ServiceID = OnDemandService, ""
	if err := checkContractRestoreSemantics(onDemand); err != nil {
		t.Fatalf("the stranded on-demand order is refused: %v", err)
	}
	saved := s.ExportState()
	for name, change := range map[string]func(*SavedTrip){
		"pod":           func(trip *SavedTrip) { trip.Request.PodID = "01" },
		"route":         func(trip *SavedTrip) { trip.Route = []int{0} },
		"hold pod":      func(trip *SavedTrip) { trip.DeferPodID = "01" },
		"hold check":    func(trip *SavedTrip) { trip.DeferCheck = saved.Tick + TicksPerSecond },
		"never boarded": func(trip *SavedTrip) { trip.Boarded, trip.Request.BoardedTick = false, 0 },
		"service pair":  func(trip *SavedTrip) { trip.Request.ServiceID = "unknown" },
		"unknown origin": func(trip *SavedTrip) {
			trip.Request.From, trip.Request.Service, trip.Request.ServiceID = "nowhere", OnDemandService, ""
		},
		// The fleet of this case has no Express pod.
		"no admitting class": func(*SavedTrip) {},
	} {
		state := s.ExportState()
		change(&state.Waiting[0])
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := input
			input.State = state
			if name == "no admitting class" {
				input.Fleet = []Placement{{ID: "01", Class: GroupClass, StationID: "harbor", BerthID: "harbor-1"}}
			}
			for _, logical := range []bool{false, true} {
				input.LogicalOnly = logical
				err := checkContractRestoreSemantics(input)
				if err == nil || err.Error() != "saved Express order has no compatible vehicle path" {
					t.Fatalf("logical %t: %v", logical, err)
				}
				if restored, _, err := RestoreState(input); err == nil || restored != nil {
					t.Fatalf("logical %t: the restore accepts the trip", logical)
				}
			}
		})
	}
}

// TestTransferHistory checks a transferred party through its whole
// journey (incident contract, sections 7.2 and 14.5). After the transfer,
// after it boards at its leg origin, and after it completes, each save
// passes the state contract and both restore tiers. The completed rider
// keeps its leg origin, and no completed rider gets one that it did not
// have.
func TestTransferHistory(t *testing.T) {
	t.Parallel()
	s, v := pooledLegFleet(t, false)
	if err := s.transferRider(v, 0, "s1"); err != nil {
		t.Fatal(err)
	}
	input := RestoreStateInput{Network: s.network, Fleet: s.initial}
	checkTransferSave(t, s, input)
	monitorContract(t, s)
	leg := s.findVehicle("02")
	for s.tick < 60*TicksPerSecond && len(leg.Riders) == 0 {
		s.Step()
	}
	if len(leg.Riders) != 1 || leg.Riders[0].ID != 1 || leg.Riders[0].LegFrom != "s1" || leg.Pod.StationID != "s1" {
		t.Fatalf("pod 02 has %+v at %q", leg.Riders, leg.Pod.StationID)
	}
	checkTransferSave(t, s, input)
	for s.tick < 600*TicksPerSecond && (s.completed < 2 || leg.Pod.Activity != Idle || v.Pod.Activity != Idle) {
		s.Step()
	}
	if s.completed != 2 || !leg.Riders[0].Completed || leg.Pod.Activity != Idle {
		t.Fatalf("completed %d, pod 02 %s with %+v", s.completed, leg.Pod.Activity, leg.Riders)
	}
	restored := checkTransferSave(t, s, input)
	for _, pod := range []string{"01", "02"} {
		live, copied := s.findVehicle(pod), restored.findVehicle(pod)
		if !reflect.DeepEqual(copied.Riders, live.Riders) {
			t.Fatalf("pod %s history\n got %+v\nwant %+v", pod, copied.Riders, live.Riders)
		}
	}
	if got := restored.findVehicle("02").Riders[0].LegFrom; got != "s1" {
		t.Fatalf("the completed transferred rider has the leg origin %q", got)
	}
	if got := restored.findVehicle("01").Riders[0].LegFrom; got != "" {
		t.Fatalf("the completed plain rider has the leg origin %q", got)
	}
}
