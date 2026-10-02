package sim

import (
	"reflect"
	"testing"
)

func submitSharedTrip(s *Simulation, from, to string) error {
	_, err := s.SubmitTripOptions(TripOptions{From: from, To: to, SharingConsent: SharedConsent})
	return err
}

func requestSharedJourney(s *Simulation, pod, to string) error {
	return s.RequestJourneyOptions(pod, TripOptions{To: to, SharingConsent: SharedConsent})
}

func TestOrderPrivateAdmissionIsAtomic(t *testing.T) {
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(8); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	if err := submitSharedTrip(s, "harbor", "market"); err != nil {
		t.Fatal(err)
	}
	if len(s.vehicles[0].Riders) != 1 || s.vehicles[0].Riders[0].SharingConsent != PrivateConsent || len(s.waiting) != 1 {
		t.Fatalf("private party joined: %+v", s.Snapshot())
	}
	before := s.ExportState()
	if id, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 8}); err == nil || id != 0 {
		t.Fatalf("legacy admitted private eight: %d,%v", id, err)
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("rejected submission changed state")
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

func TestCompactWholePartyAndSeats(t *testing.T) {
	s, err := NewFleet(Example(), []Placement{{ID: "01", Class: CompactClass, StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(8); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{3, 2, 1} {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: size, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
	}
	if s.vehicles[0].PassengersAboard() != 4 || s.vehicles[0].RidersAboard() != 2 || len(s.waiting) != 1 || s.waiting[0].request.PartySize != 2 {
		t.Fatalf("party split or seats exceeded: %+v", s.Snapshot())
	}
	before := s.ExportState()
	if err := s.RequestJourneyOptions("01", TripOptions{To: "garden", PartySize: 5}); err == nil {
		t.Fatal("busy pod accepted")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("failed selected order changed state")
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

func TestVehicleClassRoutingAndRestore(t *testing.T) {
	for _, class := range []VehicleClass{LegacyClass, CompactClass} {
		t.Run(string(class), func(t *testing.T) {
			network := BankExample()
			compact, _ := NewClassSet("compact")
			network.Stations[1].Berths[0].VehicleClasses = compact
			s, err := NewFleet(network, []Placement{{ID: "01", Class: class, StationID: "origin", BerthID: "origin-1"}})
			if err != nil {
				t.Fatal(err)
			}
			_, berth, err := s.stationRouteByLoad(stationRouteInput{from: "origin-berth", station: "hub", class: class})
			if err != nil {
				t.Fatal(err)
			}
			if class == CompactClass && berth.ID != "bank-a-1" {
				t.Fatal("compact lost compatible first bank", berth)
			}
			if class == LegacyClass && berth.ID == "bank-a-1" {
				t.Fatal("legacy selected incompatible bank")
			}
			if _, err := s.routeForClass("origin-berth", "bank-a-berth", LegacyClass); err == nil {
				t.Fatal("legacy route cache accepted compact berth")
			}
			if _, err := s.routeForClass("origin-berth", "bank-a-berth", CompactClass); err != nil {
				t.Fatal("compact route polluted by legacy cache", err)
			}
			if err := s.RequestJourney("01", "hub"); err != nil {
				t.Fatal(err)
			}
			saved := s.ExportState()
			if saved.Pods[0].Class != class {
				t.Fatal("class omitted from save")
			}
			if _, _, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: saved}); err != nil {
				t.Fatal(err)
			}
			saved.Pods[0].Class = GroupClass
			for _, logical := range []bool{false, true} {
				if _, _, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: saved, LogicalOnly: logical}); err == nil {
					t.Fatal("unapproved class restored")
				}
			}
		})
	}
}

func TestOrderClassLanePolicyNoFallback(t *testing.T) {
	network := Example()
	compact, _ := NewClassSet("compact")
	for i := range network.Lanes {
		if network.Lanes[i].ID == "bypass-in" {
			network.Lanes[i].VehicleClasses = compact
		}
	}
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}, {ID: "02", Class: CompactClass, StationID: "garden", BerthID: "garden-1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []VehicleClass{LegacyClass, CompactClass} {
		route, err := s.routeForClass("harbor-berth", "market-berth", class)
		if err != nil {
			t.Fatal(err)
		}
		for _, lane := range route {
			if class == LegacyClass && !lane.VehicleClasses.Allows(string(class)) {
				t.Fatal("legacy fallback crossed compact lane")
			}
		}
	}
	before := s.ExportState()
	for _, class := range []VehicleClass{GroupClass, ExpressClass, VehicleClass("bad")} {
		if _, err := NewFleet(network, []Placement{{ID: "01", Class: class, StationID: "harbor", BerthID: "harbor-1"}}); err == nil {
			t.Fatal("unsupported placement accepted")
		}
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("class rejection changed live state")
	}
}

func TestLegacyOrderMigrationClosedAndConserved(t *testing.T) {
	s := newSharingSimulation(t)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := submitSharedTrip(s, "harbor", "market"); err != nil {
			t.Fatal(err)
		}
	}
	old := s.ExportState()
	for i := range old.Pods {
		for j := range old.Pods[i].Riders {
			old.Pods[i].Riders[j].SharingConsent = ""
			old.Pods[i].Riders[j].Service = ""
		}
	}
	original := roundTripState(t, old)
	if _, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: s.initial, State: old}); err == nil {
		t.Fatal("native restore invented consent")
	}
	migrated, err := MigrateLegacyOrderState(old)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, original) {
		t.Fatal("migration changed input")
	}
	restored, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: s.initial, State: migrated})
	if err != nil {
		t.Fatal(err)
	}
	v := &restored.vehicles[0]
	if !v.LegacyCohort || v.Riders[0].SharingConsent != LegacyUnknownConsent {
		t.Fatal("historic consent invented")
	}
	if tripErr := submitSharedTrip(restored, "harbor", "market"); tripErr != nil {
		t.Fatal(tripErr)
	}
	if len(v.Riders) != 2 || len(restored.waiting) != 1 {
		t.Fatal("closed cohort admitted new rider")
	}
	again, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: s.initial, State: roundTripState(t, restored.ExportState())})
	if err != nil || !again.vehicles[0].LegacyCohort {
		t.Fatal("closed marker lost on second restart", err)
	}
	logical, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: s.initial, State: migrated, LogicalOnly: true})
	if err != nil || len(result.Requeued) != 2 {
		t.Fatal("logical requeue lost parties", err, result)
	}
	for _, trip := range logical.waiting {
		if trip.request.SharingConsent != PrivateConsent || trip.request.Service != OnDemandService {
			t.Fatal("logical requeue retained unknown consent")
		}
	}
	if logical.vehicles[0].LegacyCohort {
		t.Fatal("logical requeue retained closed cohort")
	}
	migrated.Pods[0].Riders[0].From = "changed"
	if old.Pods[0].Riders[0].From == "changed" {
		t.Fatal("migration aliases riders")
	}
}

func TestLegacyPendingSizesRemainBlocked(t *testing.T) {
	s := newSharingSimulation(t)
	old := s.ExportState()
	old.RequestID = 2
	old.Waiting = []SavedTrip{{Request: SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 2}}, {Request: SavedRequest{ID: 2, From: "harbor", To: "market", PartySize: 12}}}
	migrated, err := MigrateLegacyOrderState(old)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated.Waiting[1].Request.LegacyPartySize || migrated.Waiting[0].Request.LegacyPartySize {
		t.Fatal("legacy size marker is inaccurate")
	}
	for _, logical := range []bool{false, true} {
		restored, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: s.initial, State: migrated, LogicalOnly: logical})
		if err != nil || len(result.Dropped) != 0 {
			t.Fatal("migration dropped historical party", err, result)
		}
		advance(restored, 2*TicksPerSecond)
		if len(restored.waiting) != 2 || restored.waiting[0].request.PartySize != 2 || restored.waiting[1].request.PartySize != 12 || restored.requestID != 2 || restored.completed != 0 {
			t.Fatal("blocked legacy party was lost or split", restored.Snapshot())
		}
		for _, trip := range restored.waiting {
			if trip.request.PodID != "" || trip.request.SharingConsent != PrivateConsent || trip.request.DispatchReason == "" {
				t.Fatal("legacy party lacked blocked reason", trip.request)
			}
		}
		if err := restored.CheckContract(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpressRegistryIsOwnedAndAtomic(t *testing.T) {
	network := Example()
	all, _ := NewClassSet("legacy", "compact", "group", "express")
	for i := range network.Lanes {
		network.Lanes[i].VehicleClasses = all
	}
	for i := range network.Stations {
		network.Stations[i].VehicleClasses = all
		for j := range network.Stations[i].Berths {
			network.Stations[i].Berths[j].VehicleClasses = all
		}
	}
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	services := []ExpressService{{ID: "hub-pair", From: "harbor", To: "market", Class: ExpressClass, PartyLimit: 20}}
	if err := ValidateExpressServices(network, services); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatal(err)
	}
	services[0].To = "garden"
	if s.expressServices["hub-pair"].To != "market" {
		t.Fatal("registry aliases caller input")
	}
	before := s.Clone()
	for _, records := range [][]ExpressService{
		{{ID: "hub-pair", From: "harbor", To: "market", Class: ExpressClass, PartyLimit: 21}},
		{{ID: "hub-pair", From: "harbor", To: "market", Class: CompactClass, PartyLimit: 4}},
		{{ID: "hub-pair", From: "harbor", To: "market", Class: ExpressClass, PartyLimit: 20}, {ID: "hub-pair", From: "garden", To: "market", Class: ExpressClass, PartyLimit: 20}},
		make([]ExpressService, MaxExpressServices+1),
	} {
		if err := s.SetExpressServices(records); err == nil {
			t.Fatal("invalid registry accepted")
		}
		if !reflect.DeepEqual(before.expressServices, s.expressServices) {
			t.Fatal("registry rejection changed state")
		}
	}
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "hub-pair"}); err == nil {
		t.Fatal("logical express seats granted physical operation")
	}
	if s.requestID != 0 || len(s.waiting) != 0 {
		t.Fatal("unsupported express order allocated identity")
	}
	clone := s.Clone()
	delete(clone.expressServices, "hub-pair")
	if len(s.expressServices) != 1 {
		t.Fatal("clone aliases registry")
	}
	// Registry metadata must prove a full class-compatible path, including arrival.
	network.Lanes[0].VehicleClasses = 0
	network.Stations[2].Berths[0].VehicleClasses = 0
	if err := ValidateExpressServices(network, []ExpressService{{ID: "pair", From: "harbor", To: "market", Class: ExpressClass, PartyLimit: 20}}); err == nil {
		t.Fatal("registry accepted incompatible destination berth")
	}
}

func TestRestoreOrderBoundsPreserveOfflineSubmission(t *testing.T) {
	s := newSharingSimulation(t)
	s.requestID = MaxSavedWaitingTrips + 1
	for id := 1; id <= MaxSavedWaitingTrips+1; id++ {
		s.waiting = append(s.waiting, waitingTrip{request: requestFromOptions(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, id, 0)})
	}
	id, err := s.SubmitTrip("harbor", "market")
	if err != nil || id != MaxSavedWaitingTrips+2 {
		t.Fatal("offline submission acquired a new ceiling", id, err)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal("live offline state acquired a restore-only ceiling", err)
	}
	if _, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: s.initial, State: s.ExportState()}); err == nil {
		t.Fatal("restore accepted over-bound state")
	}
}

func classPickupFixture(t *testing.T, currentClass VehicleClass, readyBerth string) *Simulation {
	t.Helper()
	network := BankExample()
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	for i := range network.Lanes {
		if network.Lanes[i].ID == "bank-a-out" {
			network.Lanes[i].VehicleClasses = compact
		}
	}
	s, err := NewFleet(network, []Placement{{ID: "01", Class: currentClass, StationID: "origin", BerthID: "origin-1"}, {ID: "02", StationID: "hub", BerthID: readyBerth}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sendPickup(s.findVehicle("01"), "hub"); err != nil {
		t.Fatal(err)
	}
	s.requestID = 1
	request := requestFromOptions(TripOptions{From: "hub", To: "origin", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, 1, 0)
	request.PodID = "01"
	s.waiting = []waitingTrip{{request: request}}
	s.SetPickupSwaps(true)
	return s
}

func TestOrderPickupChangesKeepCompatibleBerths(t *testing.T) {
	for _, action := range []string{"transfer", "swap", "promote-ready", "promote-inbound"} {
		t.Run(action, func(t *testing.T) {
			currentClass, readyBerth := CompactClass, "bank-a-1"
			if action == "promote-inbound" {
				currentClass, readyBerth = LegacyClass, "bank-b-1"
			}
			s := classPickupFixture(t, currentClass, readyBerth)
			ready := s.findVehicle("02")
			if !s.podFitsRequest(ready, s.waiting[0].request) {
				t.Fatal("station-level fit must remain possible through bank b")
			}
			if action != "transfer" {
				request := requestFromOptions(TripOptions{From: "hub", To: "origin", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, 2, 0)
				request.PodID = "02"
				if action == "swap" {
					request.From, request.To = "origin", "hub"
				}
				s.waiting = append(s.waiting, waitingTrip{request: request})
				s.requestID = 2
			}
			before := s.ExportState()
			var changed bool
			switch action {
			case "transfer":
				changed = s.tryPickupTransfer(0, ready)
			case "swap":
				changed = s.tryPickupSwap(0, 1)
			default:
				changed = s.promoteReadyPickup(0)
			}
			if changed || !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("incompatible continuation changed an assignment")
			}
			if (action == "transfer" || action == "swap") && s.PickupSwapStats().RouteFailures != 1 {
				t.Fatal("missing continuation refusal", s.PickupSwapStats())
			}
		})
	}
}

func TestOrderPickupChangesAllowCompatibleBerths(t *testing.T) {
	for _, action := range []string{"transfer", "promote"} {
		t.Run(action, func(t *testing.T) {
			s := classPickupFixture(t, CompactClass, "bank-b-1")
			var changed bool
			if action == "transfer" {
				changed = s.tryPickupTransfer(0, s.findVehicle("02"))
			} else {
				request := s.waiting[0].request
				request.ID, request.PodID = 2, "02"
				s.waiting = append(s.waiting, waitingTrip{request: request})
				s.requestID = 2
				changed = s.promoteReadyPickup(0)
			}
			if !changed || s.waiting[0].request.PodID != "02" {
				t.Fatal("compatible berth could not serve the older request")
			}
			if err := s.board(s.findVehicle("02"), s.waiting[0]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOrderFinishEstimateUsesCompatibleUnbankedBerth(t *testing.T) {
	for _, restriction := range []string{"berth", "arrival-lane", "all-berths"} {
		t.Run(restriction, func(t *testing.T) {
			network := lineNetwork(lineStations(0, 2, 2, 2))
			legacy, err := NewClassSet("legacy")
			if err != nil {
				t.Fatal(err)
			}
			destination := &network.Stations[2]
			if restriction == "arrival-lane" {
				for i := range network.Lanes {
					if network.Lanes[i].To == destination.Berths[0].Node {
						network.Lanes[i].VehicleClasses = legacy
					}
				}
			} else {
				destination.Berths[0].VehicleClasses = legacy
				if restriction == "all-berths" {
					destination.Berths[1].VehicleClasses = legacy
				}
			}
			s, err := NewFleet(network, []Placement{{ID: "01", Class: CompactClass, StationID: "s0", BerthID: "s0-1"}})
			if err != nil {
				t.Fatal(err)
			}
			v := s.findVehicle("01")
			if err := s.sendPickup(v, "s1"); err != nil {
				t.Fatal(err)
			}
			request := requestFromOptions(TripOptions{From: "s1", To: "s2", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, 1, 0)
			request.PodID = "01"
			route, _ := s.stationApproachRouteForClass(v.destination.Node, "s2", CompactClass)
			s.waiting = []waitingTrip{{request: request, route: route}}
			node, seconds, ok := s.finishEstimate(v)
			if restriction == "all-berths" {
				if ok {
					t.Fatal("forecast accepts an incompatible destination")
				}
				return
			}
			if !ok || node != destination.Berths[1].Node || seconds <= 0 {
				t.Fatalf("forecast chose incompatible berth: %s %.2f %v", node, seconds, ok)
			}
		})
	}
}
