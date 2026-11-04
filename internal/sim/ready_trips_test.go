package sim

import (
	"slices"
	"testing"
)

// readyTripsFixture returns a simulation with pods 01 and 02 idle at s1, and
// pod 03 idle at s0. It has one waiting trip from s1 to s0 for each pod ID in
// pods. An empty ID is a trip with no pod.
func readyTripsFixture(t *testing.T, pods ...string) *Simulation {
	t.Helper()
	s := newLineSimulation(t, lineStations(0, 1, 2), place("s1-1", "s1-2", "s0-1"))
	for _, pod := range pods {
		s.requestID++
		request := requestFromOptions(TripOptions{From: "s1", To: "s0", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, s.requestID, 0)
		request.PodID = pod
		s.waiting = append(s.waiting, waitingTrip{request: request})
	}
	return s
}

// TestReadyTripsSelectPodsReadyAtThePickup checks that readyTrips holds, in
// queue order, each trip with a pod in service that is idle at its pickup.
func TestReadyTripsSelectPodsReadyAtThePickup(t *testing.T) {
	t.Parallel()
	s := readyTripsFixture(t, "", "01", "03", "02", "missing")
	if got, want := s.readyTrips(new(dispatchPass)), []int{1, 3}; !slices.Equal(got, want) {
		t.Errorf("ready trips = %v, want %v", got, want)
	}
	s.findVehicle("01").Pod.Activity = Traveling
	if got, want := s.readyTrips(new(dispatchPass)), []int{3}; !slices.Equal(got, want) {
		t.Errorf("with pod 01 away, ready trips = %v, want %v", got, want)
	}
	s.findVehicle("02").withdrawn = 1
	if got := s.readyTrips(new(dispatchPass)); len(got) != 0 {
		t.Errorf("with pod 02 out of service, ready trips = %v, want none", got)
	}
}

// TestReadyTripsFollowPassReset checks that the pass keeps the trips until a
// reset, and that a reset makes the next call read the queue again.
func TestReadyTripsFollowPassReset(t *testing.T) {
	t.Parallel()
	s := readyTripsFixture(t, "01", "")
	pass := new(dispatchPass)
	if got, want := s.readyTrips(pass), []int{0}; !slices.Equal(got, want) {
		t.Fatalf("ready trips = %v, want %v", got, want)
	}
	s.waiting[0].request.PodID, s.waiting[1].request.PodID = "", "02"
	if got, want := s.readyTrips(pass), []int{0}; !slices.Equal(got, want) {
		t.Errorf("before the reset, ready trips = %v, want %v", got, want)
	}
	pass.reset()
	if got, want := s.readyTrips(pass), []int{1}; !slices.Equal(got, want) {
		t.Errorf("after the reset, ready trips = %v, want %v", got, want)
	}
}

// TestDispatchSeesReadyTripsOfThePass checks that a new pass does not keep
// the ready trips of the last pass. Dispatch must promote the pod that the
// later trip holds now.
func TestDispatchSeesReadyTripsOfThePass(t *testing.T) {
	t.Parallel()
	s := readyTripsFixture(t, "", "01")
	pass := new(dispatchPass)
	s.waiting[1].request.PodID = ""
	pass.begin(s.waiting)
	if got := s.readyTrips(pass); len(got) != 0 {
		t.Fatalf("ready trips = %v, want none", got)
	}
	s.waiting[1].request.PodID = "01"
	pass.begin(s.waiting)
	if !s.promoteReadyPickup(0, pass) {
		t.Fatal("no promotion from the later trip")
	}
	if s.waiting[0].request.PodID != "01" {
		t.Errorf("pod of the first trip = %q", s.waiting[0].request.PodID)
	}
}

// TestPromotionTakesTheFirstLaterTrip checks that a promotion reads only the
// trips after the trip that it serves, and takes the first of them.
func TestPromotionTakesTheFirstLaterTrip(t *testing.T) {
	t.Parallel()
	s := readyTripsFixture(t, "01", "", "02")
	if !s.promoteReadyPickup(1, new(dispatchPass)) {
		t.Fatal("no promotion")
	}
	if got := []string{s.waiting[0].request.PodID, s.waiting[1].request.PodID, s.waiting[2].request.PodID}; !slices.Equal(got, []string{"01", "02", ""}) {
		t.Errorf("pods = %q", got)
	}
}
