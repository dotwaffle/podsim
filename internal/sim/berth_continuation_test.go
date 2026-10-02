package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

func continuationNetwork(t *testing.T, buffered bool) Network {
	t.Helper()
	n := Example()
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n.Lanes {
		if n.Lanes[i].ID == "garden-out" {
			n.Lanes[i].VehicleClasses = compact
		}
		if buffered && n.Lanes[i].ID == "garden-approach" {
			n.Lanes[i].To = "garden-gate"
		}
	}
	n.Nodes = append(n.Nodes, Node{ID: "garden-berth-2", Position: Point{X: 480, Y: 220}})
	n.Lanes = append(n.Lanes,
		Lane{ID: "garden-in-2", From: "garden-entry", To: "garden-berth-2", SpeedLimit: 14, StationID: "garden", StationRole: StationBerthAccessRole},
		Lane{ID: "garden-out-2", From: "garden-berth-2", To: "garden-exit", SpeedLimit: 14, StationID: "garden", StationRole: StationDepartureRole})
	n.Stations[1].Berths = append(n.Stations[1].Berths, Berth{ID: "garden-2", Node: "garden-berth-2"})
	if buffered {
		n.Nodes = append(n.Nodes, Node{ID: "garden-gate", Position: Point{X: 350, Y: 205}})
		n.Lanes = append(n.Lanes, Lane{ID: "garden-entry", From: "garden-gate", To: "garden-entry", SpeedLimit: 14, StationID: "garden", StationRole: StationEntryRole})
		n = stationBufferNetwork(n, 4)
	}
	return n
}

func continuationFleet(t *testing.T, buffered bool) *Simulation {
	t.Helper()
	s, err := NewFleet(continuationNetwork(t, buffered), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(buffered)
	return s
}

func finishContinuationTrips(t *testing.T, s *Simulation, want int) {
	t.Helper()
	for range 1800 * TicksPerSecond {
		s.Step()
		if s.Tick()%TicksPerSecond == 0 {
			checkTraffic(t, s.Snapshot())
		}
		if s.completed == want {
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("accepted service did not complete: %+v", s.Snapshot())
}

func TestBerthContinuationSharedDropoffCompletes(t *testing.T) {
	t.Parallel()
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "buffered"}[buffered], func(t *testing.T) {
			t.Parallel()
			s := continuationFleet(t, buffered)
			if err := s.SetSharedRidePartyLimit(4); err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			for _, to := range []string{"market", "garden"} {
				if err := submitSharedTrip(s, "harbor", to); err != nil {
					t.Fatal(err)
				}
			}
			if s.vehicles[0].RidersAboard() != 2 || !slices.Equal(s.vehicles[0].Stops, []string{"garden", "market"}) {
				t.Fatal("viable shared plan refused", s.Snapshot())
			}
			finishContinuationTrips(t, s, 2)
			if s.Snapshot().MaxDetourRatio > maxSharedRideDetour+1e-9 {
				t.Fatal("compatible alternate exceeded the detour cap")
			}
		})
	}
}

func TestBerthContinuationPickupFindsAlternate(t *testing.T) {
	t.Parallel()
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "buffered"}[buffered], func(t *testing.T) {
			t.Parallel()
			s := continuationFleet(t, buffered)
			if _, err := s.SubmitTrip("garden", "market"); err != nil {
				t.Fatal(err)
			}
			if len(s.waiting) != 1 || s.waiting[0].request.PodID != "01" {
				t.Fatal("viable alternate pickup stayed unassigned", s.Snapshot())
			}
			if !buffered && s.vehicles[0].destination.ID != "garden-2" {
				t.Fatal("pickup send changed its compatible candidate berth")
			}
			finishContinuationTrips(t, s, 1)
		})
	}
}

func TestBerthContinuationPickupRefusesUnsafeReselection(t *testing.T) {
	t.Parallel()
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "buffered"}[buffered], func(t *testing.T) {
			t.Parallel()
			s := continuationFleet(t, buffered)
			s.owners[resource{kind: berthResource, id: "garden-1"}] = "external"
			if _, err := s.SubmitTrip("garden", "market"); err != nil {
				t.Fatal(err)
			}
			delete(s.owners, resource{kind: berthResource, id: "garden-1"})
			s.owners[resource{kind: berthResource, id: "garden-2"}] = "external"
			for range 300 * TicksPerSecond {
				s.Step()
				if s.vehicles[0].destination.ID == "garden-1" || s.vehicles[0].Pod.BerthID == "garden-1" {
					t.Fatal("reselection admitted an unusable pickup berth")
				}
			}
			v := &s.vehicles[0]
			before, owners := s.ExportState(), maps.Clone(s.owners)
			s.Step()
			if !maps.Equal(owners, s.owners) || v.Pod.BerthID == "garden-1" || s.completed != 0 || len(s.waiting) != 1 {
				t.Fatal("denied berth selection changed accepted state or ownership")
			}
			if before.Pods[0].Distance != s.ExportState().Pods[0].Distance {
				t.Fatal("denied inlet moved the stopped pod")
			}
			delete(s.owners, resource{kind: berthResource, id: "garden-2"})
			finishContinuationTrips(t, s, 1)
		})
	}
}

func TestBerthContinuationUnavailableCandidateIsAtomic(t *testing.T) {
	t.Parallel()
	s := continuationFleet(t, false)
	request := requestFromOptions(TripOptions{From: "garden", To: "market", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, 1, 0)
	before, owners := s.ExportState(), maps.Clone(s.owners)
	station, _ := s.station("garden")
	if s.pickupBerthFitsRequest(&s.vehicles[0], request, station.Berths[0]) {
		t.Fatal("legacy accepted compact-only departure")
	}
	if !reflect.DeepEqual(before, s.ExportState()) || !maps.Equal(owners, s.owners) {
		t.Fatal("compatibility trial mutated the simulation")
	}
}

func bankContinuationFleet(t *testing.T, buffered, alternate bool) *Simulation {
	t.Helper()
	n := BankExample()
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n.Lanes {
		if n.Lanes[i].ID == "bank-a-out" {
			n.Lanes[i].VehicleClasses = compact
		}
	}
	if alternate {
		n.Nodes = append(n.Nodes, Node{ID: "bank-a-berth-2", Position: Point{600, -100}})
		n.Lanes = append(n.Lanes,
			Lane{ID: "bank-a-in-2", From: "bank-a-arrival", To: "bank-a-berth-2", SpeedLimit: 14, StationID: "hub", StationRole: StationBerthAccessRole},
			Lane{ID: "bank-a-out-2", From: "bank-a-berth-2", To: "bank-a-departure", SpeedLimit: 14, StationID: "hub", StationRole: StationDepartureRole})
		n.Stations[1].Berths = append(n.Stations[1].Berths, Berth{ID: "bank-a-2", Node: "bank-a-berth-2"})
		n.Stations[1].Banks[0].BerthIDs = append(n.Stations[1].Banks[0].BerthIDs, "bank-a-2")
	}
	n.Stations[2].ParkingOnly = false
	if buffered {
		n = stationBufferNetwork(n, 4)
	}
	s, err := NewFleet(n, []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(buffered)
	return s
}

func TestBerthContinuationBanksChooseUsableGate(t *testing.T) {
	t.Parallel()
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "buffered"}[buffered], func(t *testing.T) {
			t.Parallel()
			s := bankContinuationFleet(t, buffered, false)
			if _, err := s.SubmitTrip("hub", "origin"); err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[0]
			station, _ := s.station("hub")
			if len(s.waiting) != 1 || s.waiting[0].request.PodID != "01" || station.routeEntry(v.Route, v.destination) != "bank-b-entry" {
				t.Fatal("pickup did not choose the usable independent bank", s.Snapshot())
			}
			finishContinuationTrips(t, s, 1)
		})
	}
}

func TestBerthContinuationBanksSharedDropoffCompletes(t *testing.T) {
	t.Parallel()
	for _, buffered := range []bool{false, true} {
		for _, alternate := range []bool{false, true} {
			t.Run(map[bool]string{false: "terminal", true: "buffered"}[buffered]+map[bool]string{false: "/other-bank", true: "/same-bank"}[alternate], func(t *testing.T) {
				t.Parallel()
				s := bankContinuationFleet(t, buffered, alternate)
				if err := s.SetSharedRidePartyLimit(4); err != nil {
					t.Fatal(err)
				}
				if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
					t.Fatal(err)
				}
				for _, to := range []string{"parking", "hub"} {
					if err := submitSharedTrip(s, "origin", to); err != nil {
						t.Fatal(err)
					}
				}
				if s.vehicles[0].RidersAboard() != 2 || !slices.Equal(s.vehicles[0].Stops, []string{"hub", "parking"}) {
					t.Fatal("usable bank was omitted from the shared detour plan", s.Snapshot())
				}
				finishContinuationTrips(t, s, 2)
			})
		}
	}
}

func TestBerthContinuationBanksRefuseUnsafeReselection(t *testing.T) {
	t.Parallel()
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal", true: "buffered"}[buffered], func(t *testing.T) {
			t.Parallel()
			s := bankContinuationFleet(t, buffered, true)
			if _, err := s.SubmitTrip("hub", "origin"); err != nil {
				t.Fatal(err)
			}
			station, _ := s.station("hub")
			if station.routeEntry(s.vehicles[0].Route, s.vehicles[0].destination) != "bank-a-entry" {
				t.Fatal("expected the bank with a usable local alternate")
			}
			s.owners[resource{kind: berthResource, id: "bank-a-2"}] = "external"
			for range 300 * TicksPerSecond {
				s.Step()
				if s.vehicles[0].destination.ID == "bank-a-1" || s.vehicles[0].Pod.BerthID == "bank-a-1" || s.vehicles[0].Pod.BerthID == "bank-b-1" {
					t.Fatal("blocked local alternate escaped its bank or chose an unusable departure")
				}
			}
			owners, distance := maps.Clone(s.owners), s.vehicles[0].distance
			s.Step()
			if !maps.Equal(owners, s.owners) || distance != s.vehicles[0].distance || s.completed != 0 {
				t.Fatal("denied bank admission changed ownership or motion")
			}
			delete(s.owners, resource{kind: berthResource, id: "bank-a-2"})
			finishContinuationTrips(t, s, 1)
		})
	}
}

func TestVehicleClassSurvivesPassengerAndEmptyArrival(t *testing.T) {
	t.Parallel()
	for _, class := range []VehicleClass{"", LegacyClass, CompactClass} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{{ID: "01", Class: class, StationID: "harbor", BerthID: "harbor-1"}})
			if err != nil {
				t.Fatal(err)
			}
			waitIdle := func(completed int, station string) {
				t.Helper()
				for range 900 * TicksPerSecond {
					s.Step()
					v := &s.vehicles[0]
					if v.Pod.Class != class {
						t.Fatalf("arrival changed immutable class: got %q, want %q", v.Pod.Class, class)
					}
					if v.Pod.Activity == Idle && v.Pod.StationID == station && s.completed == completed {
						if err := s.CheckContract(); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatal("arrival did not complete", s.Snapshot())
			}
			size := 1
			if class == CompactClass {
				size = 4
			}
			for i, endpoints := range [][2]string{{"harbor", "market"}, {"market", "garden"}} {
				if _, err := s.SubmitTripOptions(TripOptions{From: endpoints[0], To: endpoints[1], PartySize: size, SharingConsent: PrivateConsent}); err != nil {
					t.Fatal(err)
				}
				if s.vehicles[0].PassengersAboard() != size || s.vehicles[0].RidersAboard() != 1 {
					t.Fatal("whole private party did not board")
				}
				waitIdle(i+1, endpoints[1])
			}
			station, _ := s.station("harbor")
			if err := s.startEmptyMove(&s.vehicles[0], emptyDestination{station: station.ID, berth: station.Berths[0]}); err != nil {
				t.Fatal(err)
			}
			waitIdle(2, "harbor")
			if s.ExportState().Pods[0].Class != class || s.Snapshot().Vehicles[0].Pod.Class != class {
				t.Fatal("arrival lost class in exported state")
			}
		})
	}
}

func TestBerthContinuationBanksExtendFirstStop(t *testing.T) {
	t.Parallel()
	s := bankContinuationFleet(t, false, false)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"hub", "parking"} {
		if err := submitSharedTrip(s, "origin", to); err != nil {
			t.Fatal(err)
		}
	}
	if s.vehicles[0].RidersAboard() != 2 || !slices.Equal(s.vehicles[0].Stops, []string{"hub", "parking"}) {
		t.Fatal("unchanged first stop refused a usable alternate bank", s.Snapshot())
	}
	finishContinuationTrips(t, s, 2)
}

func TestBerthContinuationCandidateMatchesSend(t *testing.T) {
	t.Parallel()
	s := continuationFleet(t, false)
	request := requestFromOptions(TripOptions{From: "garden", To: "market", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, 1, 0)
	before, owners := s.ExportState(), maps.Clone(s.owners)
	route, berth, ok := s.candidateRouteForRequest(&s.vehicles[0], request, nil)
	if !ok || berth.ID != "garden-2" || len(route) == 0 || route[len(route)-1].To != berth.Node {
		t.Fatal("candidate did not search the usable alternate")
	}
	if !reflect.DeepEqual(before, s.ExportState()) || !maps.Equal(owners, s.owners) {
		t.Fatal("candidate trial changed orders or ownership")
	}
	if err := s.sendPickupForRequest(&s.vehicles[0], request); err != nil {
		t.Fatal(err)
	}
	if s.vehicles[0].destination != berth {
		t.Fatal("sendPickup changed its compatible candidate")
	}
}

func TestBerthContinuationInitialTerminalSelection(t *testing.T) {
	t.Parallel()
	s := continuationFleet(t, false)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"market", "garden"} {
		if err := submitSharedTrip(s, "harbor", to); err != nil {
			t.Fatal(err)
		}
	}
	v := &s.vehicles[0]
	// Test selection at the last road block, before another admission pass
	// can reevaluate the selected berth.
	v.reservedThrough = v.blocks.len() - 2
	if !s.assignTerminalBerth(v) || v.destination.ID != "garden-2" {
		t.Fatal("initial terminal selection admitted an unusable departure")
	}
}
