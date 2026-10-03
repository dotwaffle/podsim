package sim

import (
	"fmt"
	"reflect"
	"testing"
)

func TestExpressRegistryBoundAndRuntime(t *testing.T) {
	s, n, fleet := expressTestFleet(t)
	services := make([]ExpressService, 0, 300)
	for i := range 300 {
		services = append(services, ExpressService{ID: fmt.Sprintf("service-%03d", i), Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20})
	}
	if err := ValidateExpressServicesWithOrderContract(n, services, ExpressOrderContract); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatal(err)
	}
	before := s.ExportState()
	if err := s.SetExpressServices(append(services, ExpressService{ID: "overflow", Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20})); err == nil {
		t.Fatal("301 services accepted")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("registry overflow changed state")
	}
	for range 20 {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: services[0].ID}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.vehicles[0].Riders) != 20 || s.vehicles[0].PassengersAboard() != 20 {
		t.Fatal("20 singleton service parties did not pool")
	}
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: services[0].ID}); err != nil {
		t.Fatal(err)
	}
	if len(s.waiting) != 1 || len(s.vehicles[0].Riders) != 20 {
		t.Fatal("service overfilled")
	}
	expressEvidence(t, "service-20-with-one-pending", s.ExportState())
	restored := false
	for s.completed < 21 && s.tick < 600*60 {
		before := largeMotionPods(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
		if !restored && s.vehicles[0].Pod.Activity == Traveling {
			state := roundTripState(t, s.ExportState())
			cold, receipt, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: fleet, State: state, ExpressServices: services})
			if err != nil || !cleanRestore(receipt) {
				t.Fatal(receipt, err)
			}
			checkRestoredMatches(t, s, cold)
			checkLargeColdRetainedOwners(t, s, cold)
			s = cold
			restored = true
		}
	}
	if !restored || s.completed != 21 || s.boarded != 21 || s.unaccountedOrders != 0 {
		t.Fatal("service continuation censored or lost orders")
	}
	expressEvidence(t, "service-completed", s.ExportState())
}

func TestExpressRouteAdmissionAndRestore(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Network)
	}{
		{"omitted masks", func(n *Network) {
			for i := range n.Lanes {
				n.Lanes[i].VehicleClasses = 0
			}
			for i := range n.Stations {
				n.Stations[i].VehicleClasses = 0
				for j := range n.Stations[i].Berths {
					n.Stations[i].Berths[j].VehicleClasses = 0
				}
			}
		}},
		{"destination station", func(n *Network) { n.Stations[2].VehicleClasses = classBit(string(GroupClass)) }},
		{"destination berth", func(n *Network) { n.Stations[2].Berths[0].VehicleClasses = classBit(string(GroupClass)) }},
		{"every motion lane", func(n *Network) {
			for i := range n.Lanes {
				n.Lanes[i].VehicleClasses = classBit(string(GroupClass))
			}
		}},
		{"parking endpoint", func(n *Network) { n.Stations[2].ParkingOnly = true }},
		{"malformed mask", func(n *Network) { n.Lanes[0].VehicleClasses = allClassBits | 1<<7 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, n, fleet := expressTestFleet(t)
			state := s.ExportState()
			state.RequestID = 1
			state.Waiting = []SavedTrip{{Request: SavedRequest(requestFromOptions(TripOptions{From: "harbor", To: "market", PartySize: 20, SharingConsent: PrivateConsent, Service: OnDemandService}, 1, 0))}}
			test.change(&n)
			if live, err := NewFleetWithOrderContract(n, fleet, ExpressOrderContract); err == nil {
				before := live.ExportState()
				if id, err := live.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 20}); err == nil || id != 0 {
					t.Fatal("incompatible whole party admitted")
				}
				if !reflect.DeepEqual(before, live.ExportState()) {
					t.Fatal("failed route admission changed state")
				}
			}
			for _, logical := range []bool{false, true} {
				if restored, _, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: fleet, State: state, LogicalOnly: logical}); err == nil || restored != nil {
					t.Fatal("incompatible route passed restore", logical)
				}
			}
		})
	}
}
