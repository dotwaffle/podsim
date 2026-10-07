package sim

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

func expressNetwork(network Network) Network {
	classes := allClassBits
	for i := range network.Lanes {
		network.Lanes[i].VehicleClasses = classes
	}
	for i := range network.Stations {
		network.Stations[i].VehicleClasses = classes
		for j := range network.Stations[i].Berths {
			network.Stations[i].Berths[j].VehicleClasses = classes
		}
	}
	return network
}
func expressTestFleet(t *testing.T) (*Simulation, Network, []Placement) {
	t.Helper()
	network := expressNetwork(largeRestoreNetwork())
	fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
	s, err := NewFleetWithOrderContract(network, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	return s, network, fleet
}

func TestExpressContractProfilesAndFoundation(t *testing.T) {
	for _, contract := range []OrderContract{"", ExpressOrderContract, "unknown"} {
		p, ok := LookupVehicleClassWithOrderContract(ExpressClass, contract)
		switch contract {
		case ExpressOrderContract:
			if !ok || p.Seats != 20 || p.MaxNewPartySize != 20 || p.BodyLengthMeters != 10 || !p.PhysicalSupported {
				t.Fatal(p)
			}
		case "":
			if !ok || p.PhysicalSupported || p.BodyLengthMeters != 0 || p.MaxNewPartySize != 8 {
				t.Fatal(p)
			}
		default:
			if ok {
				t.Fatal("unknown contract accepted")
			}
		}
	}
	old, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	same, err := NewFleetWithOrderContract(Example(), []Placement{{ID: "01", StationID: "harbor"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	oldRaw, err := jsonv2.Marshal(old.ExportState(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	sameRaw, err := jsonv2.Marshal(same.ExportState(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldRaw, sameRaw) || bytes.Contains(oldRaw, []byte("orderContract")) {
		t.Fatal("foundation bytes changed")
	}
	s, n, f := expressTestFleet(t)
	if _, err := NewFleet(n, f); err == nil {
		t.Fatal("foundation activated Express")
	}
	if s.Clone().OrderContract() != ExpressOrderContract {
		t.Fatal("clone lost marker")
	}
	s.Reset()
	if s.OrderContract() != ExpressOrderContract || s.Snapshot().OrderContract != ExpressOrderContract {
		t.Fatal("reset lost marker")
	}
	if math.Hypot(5, 1.25) > 6 {
		t.Fatal("centered body exceeds reviewed envelope")
	}
}

func TestExpressWholePartyAdmission(t *testing.T) {
	for _, size := range []int{-1, 1, 8, 9, 20, 21} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s, _, _ := expressTestFleet(t)
			before := s.ExportState()
			id, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: size})
			valid := size >= 1 && size <= 20
			if (err == nil) != valid {
				t.Fatal(size, id, err)
			}
			if !valid && !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("refusal changed state")
			}
			if valid && (len(s.vehicles[0].Riders) != 1 || s.vehicles[0].PassengersAboard() != size) {
				t.Fatal("whole party changed")
			}
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, class := range []VehicleClass{LegacyClass, CompactClass, GroupClass} {
		s, n, f := expressTestFleet(t)
		_ = s
		f[0].Class = class
		s, err := NewFleetWithOrderContract(n, f, ExpressOrderContract)
		if err != nil {
			t.Fatal(err)
		}
		before := s.ExportState()
		if id, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 9}); err == nil || id != 0 {
			t.Fatal("small class accepted nine")
		}
		if !reflect.DeepEqual(before, s.ExportState()) {
			t.Fatal("small refusal mutated state")
		}
	}
}

func TestExpressServicePoolingAndRegistryAtomicity(t *testing.T) {
	s, n, f := expressTestFleet(t)
	services := []ExpressService{{ID: "hub", From: "harbor", To: "market", Class: ExpressClass, PartyLimit: 20}}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "hub"}); err != nil {
			t.Fatal(err)
		}
	}
	if s.vehicles[0].RidersAboard() != 20 || s.vehicles[0].PassengersAboard() != 20 || len(s.waiting) != 0 {
		t.Fatal("authored service did not pool twenty", s.Snapshot())
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	before := s.ExportState()
	services[0].PartyLimit = 19
	if err := s.SetExpressServices(services); err == nil {
		t.Fatal("registry invalidated active cohort")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("registry refusal mutated riders")
	}
	_, _, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: f, State: before, LogicalOnly: true, ExpressServices: services})
	if err == nil {
		t.Fatal("logical tier erased authored party limit")
	}
	services[0].PartyLimit = 20
	cold, receipt, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: f, State: before, ExpressServices: services})
	if err != nil || !cleanRestore(receipt) || cold.OrderContract() != ExpressOrderContract {
		t.Fatal(receipt, err)
	}
	for _, options := range []TripOptions{{From: "harbor", To: "market", Service: ExpressServiceChoice, ServiceID: "hub"}, {From: "harbor", To: "garden", SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "hub"}, {From: "harbor", To: "market", SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "missing"}} {
		before = s.ExportState()
		if _, err := s.SubmitTripOptions(options); err == nil {
			t.Fatal("bad service admitted")
		}
		if !reflect.DeepEqual(before, s.ExportState()) {
			t.Fatal("bad service mutated state")
		}
	}
}

func TestExpressOnDemandPartyAndStopBounds(t *testing.T) {
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(8); err != nil {
		t.Fatal(err)
	}
	for range 9 {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
	}
	if s.vehicles[0].RidersAboard() != 8 || len(s.waiting) != 1 {
		t.Fatal("on-demand party bound changed")
	}
	if err := s.SetSharedRidePartyLimit(20); err == nil {
		t.Fatal("on-demand limit expanded")
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, 8); err == nil {
		t.Fatal("intermediate-stop limit expanded")
	}
}

func TestExpressRestoreMarkersAndRouteBindings(t *testing.T) {
	s, n, f := expressTestFleet(t)
	state := s.ExportState()
	for _, contract := range []OrderContract{"", "unknown"} {
		if _, _, err := RestoreState(RestoreStateInput{OrderContract: contract, Network: n, Fleet: f, State: state, LogicalOnly: true}); err == nil {
			t.Fatal("marker mismatch admitted")
		}
	}
	state.RequestID = 1
	state.Waiting = []SavedTrip{{Request: SavedRequest(requestFromOptions(TripOptions{From: "harbor", To: "market", PartySize: 20, SharingConsent: PrivateConsent, Service: OnDemandService}, 1, 0)), Route: []int{0}}}
	if _, _, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: f, State: state, LogicalOnly: true}); err == nil {
		t.Fatal("unbound route admitted")
	}
	state.Waiting[0].Request.PodID = "missing"
	if _, err := validateSavedState(state); err == nil {
		t.Fatal("missing target admitted")
	}
	state.Waiting[0].Request.PodID = "01"
	state.Waiting = append(state.Waiting, state.Waiting[0])
	state.Waiting[1].Request.ID = 2
	state.RequestID = 2
	if _, err := validateSavedState(state); err == nil {
		t.Fatal("duplicate target admitted")
	}
}

func TestExpressNativeBoundPreflight(t *testing.T) {
	_, network, fleet := expressTestFleet(t)
	for _, test := range []struct {
		name   string
		change func(*Network, *[]Placement)
	}{
		{"zero fleet", func(_ *Network, f *[]Placement) { *f = nil }},
		{"fleet overflow", func(_ *Network, f *[]Placement) { *f = make([]Placement, 301) }},
		{"node count", func(n *Network, _ *[]Placement) { n.Nodes = make([]Node, 5001) }},
		{"coordinate", func(n *Network, _ *[]Placement) { n.Nodes[0].Position.X = 100001 }},
		{"nonfinite", func(n *Network, _ *[]Placement) { n.Nodes[0].Position.X = math.NaN() }},
		{"control", func(n *Network, _ *[]Placement) { n.Lanes[0].Control = &Point{X: 100001} }},
		{"degree", func(n *Network, _ *[]Placement) {
			for i := range 65 {
				lane := n.Lanes[0]
				lane.ID = fmt.Sprintf("degree-%d", i)
				lane.Control = &Point{X: float64(i), Y: float64(i)}
				n.Lanes = append(n.Lanes, lane)
			}
		}},
		{"lane station UTF-8", func(n *Network, _ *[]Placement) { n.Lanes[0].StationID = string([]byte{255}) }},
		{"node UTF-8", func(n *Network, _ *[]Placement) { n.Nodes[0].ID = string([]byte{255}) }},
		{"bank berth UTF-8", func(n *Network, _ *[]Placement) {
			n.Stations[0].Banks = []StationBank{{ID: "bank", Entry: n.Stations[0].Entry, Exit: n.Stations[0].Exit, BerthIDs: []string{string([]byte{255})}}}
		}},
		{"ID", func(_ *Network, f *[]Placement) { (*f)[0].ID = string(make([]byte, 65)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			n := network.clone()
			f := append([]Placement(nil), fleet...)
			test.change(&n, &f)
			if err := ValidateFleetWithOrderContract(n, f, ExpressOrderContract); err == nil {
				t.Fatal("native bound accepted")
			}
			if _, err := NewFleetWithOrderContract(n, f, ExpressOrderContract); err == nil {
				t.Fatal("constructor bound accepted")
			}
		})
	}
}

func TestExpressMaximumRecoveryAndAggregate(t *testing.T) {
	// Two berths at each of 300 stations hold the largest Express fleet.
	specs := make([]lineStation, 300)
	for i := range specs {
		specs[i] = lineStation{id: fmt.Sprintf("s%d", i), berths: 2}
	}
	n := lineNetwork(specs)
	n = expressNetwork(n)
	fleet := make([]Placement, 0, expressMaxPods)
	services := make([]ExpressService, 0, 300)
	for i, st := range n.Stations {
		services = append(services, ExpressService{ID: st.ID, Class: ExpressClass, From: st.ID, To: n.Stations[(i+1)%len(n.Stations)].ID, PartyLimit: 20})
		for _, berth := range st.Berths {
			fleet = append(fleet, Placement{ID: fmt.Sprintf("p%03d", len(fleet)), Class: ExpressClass, StationID: st.ID, BerthID: berth.ID})
		}
	}
	s, err := NewFleetWithOrderContract(n, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatal(err)
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		to := ""
		for _, service := range services {
			if service.From == v.Pod.StationID {
				to = service.To
			}
		}
		options := TripOptions{From: v.Pod.StationID, To: to, PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: v.Pod.StationID}
		s.requestID++
		request := requestFromOptions(options, s.requestID, s.tick)
		if err := s.board(v, waitingTrip{request: request}); err != nil {
			t.Fatal(err)
		}
		lead := request.ID
		for range 19 {
			s.requestID++
			request = requestFromOptions(options, s.requestID, s.tick)
			v.Riders = append(v.Riders, s.boardingRider(waitingTrip{request: request}, v, lead))
			s.sharedParties++
		}
	}
	boarded := expressMaxPods * MaxExpressParties
	for range MaxSavedWaitingTrips {
		s.requestID++
		s.waiting = append(s.waiting, waitingTrip{request: requestFromOptions(TripOptions{From: "s0", To: "s1", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "s0"}, s.requestID, s.tick)})
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	if len(fleet) != expressMaxPods || len(state.Waiting) != MaxSavedWaitingTrips || s.outstandingOrders() != MaxExpressWaitingTrips {
		t.Fatal("seed shape")
	}
	if dir := os.Getenv("EXPRESS_EVIDENCE_DIR"); dir != "" {
		raw, err := jsonv2.Marshal(state, json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("recovery-%d-plus-%d.state.json", MaxSavedWaitingTrips, boarded)), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := s.ExportState()
	if id, err := s.SubmitTripOptions(TripOptions{From: "s0", To: "s1"}); err == nil || id != 0 {
		t.Fatal("outstanding aggregate bypass")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("overflow mutated seed")
	}
	input := RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: fleet, State: state, LogicalOnly: true, ExpressServices: services}
	for attempt := range 3 {
		cold, receipt, err := RestoreState(input)
		if err != nil {
			t.Fatal(attempt, err)
		}
		if receipt.Tier != RestoreLogical || receipt.Unaccounted != 0 || len(receipt.Dropped) != 0 || len(cold.waiting) != MaxExpressWaitingTrips || cold.boarded != boarded {
			t.Fatal("recovery lost facts", receipt, len(cold.waiting))
		}
		for i, trip := range cold.waiting {
			if trip.request.ID != i+1 || trip.request.Service != ExpressServiceChoice || trip.request.SharingConsent != SharedConsent || trip.request.PartySize != 1 || trip.boarded != (i < boarded) || trip.request.PodID != "" || len(trip.route) != 0 {
				t.Fatal("recovery identity/options/bindings changed", i)
			}
		}
		before = cold.ExportState()
		if id, err := cold.SubmitTripOptions(TripOptions{From: "s0", To: "s1"}); err == nil || id != 0 {
			t.Fatal("pending overflow bypass")
		}
		if err := cold.RequestJourneyOptions("p000", TripOptions{To: "s1"}); err == nil {
			t.Fatal("selected pod bypassed aggregate")
		}
		if !reflect.DeepEqual(before, cold.ExportState()) {
			t.Fatal("recovery overflow mutated state")
		}
		input.State = before
	}
	input.State.RequestID++
	input.State.Waiting = append(input.State.Waiting, SavedTrip{Request: SavedRequest(requestFromOptions(TripOptions{From: "s0", To: "s1", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, input.State.RequestID, 0))})
	if _, _, err := RestoreState(input); err == nil {
		t.Fatal("8601 seed admitted")
	}
}

// TestExpressNativeCountBounds puts the node, lane, and station counts of
// an Express network at the project limits and one more. Only a count
// over a limit gives the count refusal.
func TestExpressNativeCountBounds(t *testing.T) {
	t.Parallel()
	const refusal = "the Express fleet or network exceeds project bounds"
	network := func(nodes, lanes, stations int) Network {
		n := Network{Nodes: make([]Node, nodes), Lanes: make([]Lane, lanes), Stations: make([]Station, stations)}
		for i := range n.Nodes {
			n.Nodes[i].ID = fmt.Sprintf("n%d", i)
		}
		return n
	}
	placements := []Placement{{ID: "01", StationID: "s"}}
	for _, test := range []struct {
		name                   string
		nodes, lanes, stations int
	}{
		{"nodes", 12000, 1, 2},
		{"lanes", 1, 20000, 2},
		{"stations", 1, 1, 600},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateContractFleetBounds(network(test.nodes, test.lanes, test.stations), placements, ExpressOrderContract); err == nil || err.Error() == refusal {
				t.Fatalf("at the limit: %v", err)
			}
			over := network(test.nodes, test.lanes, test.stations)
			switch test.name {
			case "nodes":
				over.Nodes = append(over.Nodes, Node{ID: "extra"})
			case "lanes":
				over.Lanes = append(over.Lanes, Lane{})
			default:
				over.Stations = append(over.Stations, Station{})
			}
			if err := validateContractFleetBounds(over, placements, ExpressOrderContract); err == nil || err.Error() != refusal {
				t.Fatalf("over the limit: %v", err)
			}
		})
	}
}

func TestExpressNativeGeometryBudgets(t *testing.T) {
	// Each bundle has 2 * 64 * 63 = 8,064 junction pairs.
	for _, bundles := range []int{31, 32} {
		n := Network{}
		for i := range bundles {
			from, to := fmt.Sprintf("f%d", i), fmt.Sprintf("t%d", i)
			n.Nodes = append(n.Nodes, Node{ID: from, Position: Point{X: float64(i) * 100, Y: 0}}, Node{ID: to, Position: Point{X: float64(i) * 100, Y: 50}})
			for j := range 64 {
				n.Lanes = append(n.Lanes, Lane{ID: fmt.Sprintf("l%d-%d", i, j), From: from, To: to, SpeedLimit: 12, Control: &Point{X: float64(i)*100 + float64(j), Y: 25}})
			}
		}
		if err := validateContractGeometryBudget(n); (err == nil) != (bundles == 31) {
			t.Fatalf("junction bundles %d: %v", bundles, err)
		}
	}
	for _, lanes := range []int{9, 10} {
		n := Network{}
		for i := range lanes {
			from, to := fmt.Sprintf("f%d", i), fmt.Sprintf("t%d", i)
			n.Nodes = append(n.Nodes, Node{ID: from, Position: Point{X: -99999, Y: float64(i) * 50}}, Node{ID: to, Position: Point{X: 99999, Y: float64(i) * 50}})
			n.Lanes = append(n.Lanes, Lane{ID: from, From: from, To: to, SpeedLimit: 12})
		}
		if err := validateContractGeometryBudget(n); (err == nil) != (lanes == 9) {
			t.Fatalf("long lanes %d: %v", lanes, err)
		}
	}
}

func TestExpressUTF8AdmissionAndService(t *testing.T) {
	s, n, _ := expressTestFleet(t)
	before := s.ExportState()
	for _, o := range []TripOptions{{From: "harbor", To: string([]byte{255})}, {From: "harbor", To: "market", Service: ExpressServiceChoice, SharingConsent: SharedConsent, ServiceID: string([]byte{255})}} {
		if _, err := s.SubmitTripOptions(o); err == nil {
			t.Fatal("invalid UTF-8 order accepted")
		}
	}
	service := ExpressService{ID: string([]byte{255}), Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20}
	if err := s.SetExpressServices([]ExpressService{service}); err == nil {
		t.Fatal("invalid UTF-8 registry accepted")
	}
	if err := ValidateExpressServicesWithOrderContract(n, []ExpressService{service}, ExpressOrderContract); err == nil {
		t.Fatal("invalid UTF-8 registry validation accepted")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("failed UTF-8 admission changed state")
	}
}

// TestRestoreRejectsPartyAboveContractBound checks that a saved order
// above the party bound of its contract does not restore.
func TestRestoreRejectsPartyAboveContractBound(t *testing.T) {
	t.Parallel()
	for _, contract := range []OrderContract{"", ExpressOrderContract} {
		s, network, fleet := newTraffic(t), Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}}
		if contract == ExpressOrderContract {
			s, network, fleet = expressTestFleet(t)
		}
		state := s.ExportState()
		state.RequestID = 1
		options := TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}
		state.Waiting = []SavedTrip{{Request: SavedRequest(requestFromOptions(options, 1, 0))}}
		for _, size := range []int{newPartyLimit(contract), newPartyLimit(contract) + 1} {
			state.Waiting[0].Request.PartySize = size
			_, _, err := RestoreState(RestoreStateInput{OrderContract: contract, Network: network, Fleet: fleet, State: state, LogicalOnly: true})
			if valid := size == newPartyLimit(contract); (err == nil) != valid {
				t.Errorf("contract %q party %d: %v", contract, size, err)
			}
		}
	}
}
