package sim

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// pickupLoadCounts counts the coverage of TestPickupBerthFitsLoadFree.
type pickupLoadCounts struct {
	// compared counts the berth and request pairs, and refused the pairs
	// with no onward route.
	compared, refused int
	// unviewed counts the pairs compared with no routing view, as
	// localPickupForRequest checks them.
	unviewed int
	// rewritten counts the pairs from a berth of a bank to a station with
	// banks, where stationBankEntryOn ranks the entries from the bank
	// exit. departureRefused counts those that have an entry but no
	// departure from the berth to the bank exit.
	rewritten, departureRefused int
	// entryChanged counts the pairs where berthLoad and noBerthLoad select
	// different entries.
	entryChanged int
}

// loadRankedPickupFits is pickupBerthFitsRequest with the berth load
// ranking of stationApproachRouteForClass, as it was before the onward
// check gave noBerthLoad.
func loadRankedPickupFits(s *Simulation, v *vehicle, request Request, berth Berth) bool {
	station, ok := s.station(request.legOrigin())
	if !ok || !berthAllows(station, berth, v.Pod.Class) {
		return false
	}
	selected, ok := station.berth(berth.ID)
	if !ok || selected.Node != berth.Node {
		return false
	}
	_, err := s.stationApproachRouteForClass(berth.Node, request.To, v.Pod.Class)
	return err == nil
}

// comparePickupLoadFree compares pickupBerthFitsRequest with
// loadRankedPickupFits for each berth of the leg origin of each waiting
// trip, and of each hub to the other hub, for one pod of each class. Each
// pod gets its routing view, as in pickupAccess. The answer reads the
// request only through its leg origin and its destination.
func comparePickupLoadFree(t *testing.T, s *Simulation, hubs [2]string, counts *pickupLoadCounts) {
	t.Helper()
	requests := []Request{{From: hubs[0], To: hubs[1]}, {From: hubs[1], To: hubs[0]}}
	for _, trip := range s.waiting {
		request := Request{From: trip.request.legOrigin(), To: trip.request.To}
		if !slices.Contains(requests, request) {
			requests = append(requests, request)
		}
	}
	var pods []*vehicle
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if !slices.ContainsFunc(pods, func(w *vehicle) bool { return w.Pod.Class == v.Pod.Class }) {
			pods = append(pods, v)
		}
	}
	for _, v := range pods {
		// localPickupForRequest checks the same pairs with no routing view.
		for _, request := range requests {
			origin, _ := s.station(request.From)
			for _, berth := range origin.Berths {
				_, err := s.stationApproachRouteForClass(berth.Node, request.To, v.Pod.Class)
				if got, want := s.stationReachable(berth.Node, request.To, v.Pod.Class), err == nil; got != want {
					t.Fatalf("tick %d: pod %s (%s), no view, berth %s to %s: stationReachable %t, load-ranked %t", s.tick, v.Pod.ID, v.Pod.Class, berth.ID, request.To, got, want)
				}
				counts.unviewed++
			}
		}
		func() {
			defer s.leaveRouteView(s.enterRouteView(v))
			for _, request := range requests {
				origin, _ := s.station(request.From)
				for _, berth := range origin.Berths {
					comparePickupPair(t, s, v, request, berth, counts)
				}
			}
		}()
	}
}

// comparePickupPair compares one pair, and counts it.
func comparePickupPair(t *testing.T, s *Simulation, v *vehicle, request Request, berth Berth, counts *pickupLoadCounts) {
	t.Helper()
	got, want := s.pickupBerthFitsRequest(v, request, berth), loadRankedPickupFits(s, v, request, berth)
	if got != want {
		t.Fatalf("tick %d: pod %s (%s), berth %s to %s: pickupBerthFitsRequest %t, load-ranked %t", s.tick, v.Pod.ID, v.Pod.Class, berth.ID, request.To, got, want)
	}
	counts.compared++
	if !want {
		counts.refused++
	}
	destination, _ := s.station(request.To)
	if destination.Banks == nil {
		return
	}
	if node, ok := s.graph.nodes[berth.Node]; !ok || s.graph.banks.nodes[node] < 0 {
		return
	}
	// Count only pairs that reach the onward check, so that a refusal
	// counts a failed departure and not a berth of another class.
	if origin, _ := s.station(request.legOrigin()); !berthAllows(origin, berth, v.Pod.Class) {
		return
	}
	counts.rewritten++
	loaded, loadedErr := s.stationBankEntryOn(false, berth.Node, destination, s.berthLoad, v.Pod.Class, nil)
	free, freeErr := s.stationBankEntryOn(false, berth.Node, destination, noBerthLoad, v.Pod.Class, nil)
	if loadedErr == nil && !want {
		counts.departureRefused++
	}
	if loadedErr == nil && freeErr == nil && loaded != free {
		counts.entryChanged++
	}
}

// pickupDebrisLanes returns the lanes for the debris of
// TestPickupBerthFitsLoadFree: the departure lanes of station departure,
// and the lanes into the last entry of station approach.
func pickupDebrisLanes(t *testing.T, s *Simulation, departure, approach string) (departures, approaches []int) {
	t.Helper()
	target, ok := s.station(approach)
	if !ok {
		t.Fatalf("no station %s", approach)
	}
	entry := target.Entry
	if len(target.Banks) > 0 {
		entry = target.Banks[len(target.Banks)-1].Entry
	}
	for index, lane := range s.network.Lanes {
		if lane.StationID == departure && lane.StationRole == StationDepartureRole {
			departures = append(departures, index)
		}
		if lane.To == entry {
			approaches = append(approaches, index)
		}
	}
	if len(departures) == 0 || len(approaches) == 0 {
		t.Fatalf("%d departure lanes at %s and %d lanes into %s", len(departures), departure, len(approaches), entry)
	}
	return departures, approaches
}

// startPickupDebris starts debris of 1 m at the middle of the first lane
// of lanes that takes it, for 240 s. It reports false when no lane takes
// it, for example because pods claim each segment.
func startPickupDebris(s *Simulation, lanes []int) bool {
	duration := int64(240)
	for _, index := range lanes {
		middle := s.graph.lengths[index] / 2
		from, to := middle-0.5, middle+0.5
		if _, err := s.Fault(FaultRequest{LaneID: s.network.Lanes[index].ID, FromMeters: &from, ToMeters: &to, DurationSeconds: &duration}); err == nil {
			return true
		}
	}
	return false
}

// runPickupLoadFree runs the preset configuration of p on network for 10
// simulated minutes with demand between the hubs and across the network.
// Debris on a departure lane of Waterloo starts after 30 s, and debris
// before the last entry of Bank after 300 s. Each second with a blocked
// set, comparePickupLoadFree compares the answers.
func runPickupLoadFree(t *testing.T, p sharedExpressProject, network Network) pickupLoadCounts {
	t.Helper()
	s, err := NewFleetWithOrderContract(network, p.Fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	p.configure(t, s)
	hubs := [2]string{sharedExpressWaterloo, sharedExpressBank}
	services := make(map[string]string)
	for _, service := range p.ExpressServices {
		services[service.From] = service.ID
	}
	var passenger []string
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			passenger = append(passenger, station.ID)
		}
	}
	departures, approaches := pickupDebrisLanes(t, s, sharedExpressWaterloo, sharedExpressBank)
	rng := rand.New(rand.NewPCG(5, 8))
	var counts pickupLoadCounts
	departed, approached := false, false
	const end = 10 * 60 * TicksPerSecond
	for s.tick < end {
		if s.tick%TicksPerSecond == 0 {
			second := s.tick / TicksPerSecond
			if second%20 == 0 {
				from := hubs[second/20%2]
				to := hubs[1-second/20%2]
				// A refused party is only less demand.
				_, _ = s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: 4, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: services[from]})
				_, _ = s.SubmitTripOptions(TripOptions{From: to, To: from, PartySize: 1, SharingConsent: SharedConsent})
			}
			if len(s.waiting) < 120 {
				from, to := passenger[rng.IntN(len(passenger))], passenger[rng.IntN(len(passenger))]
				if from != to {
					_, _ = s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: 1, SharingConsent: SharedConsent})
				}
			}
			if !departed && second >= 30 {
				departed = startPickupDebris(s, departures)
			}
			if !approached && second >= 300 {
				approached = startPickupDebris(s, approaches)
			}
			if s.blockedActive() {
				comparePickupLoadFree(t, s, hubs, &counts)
			}
		}
		s.Step()
	}
	if !departed || !approached {
		t.Fatalf("debris on the departure %t, before the entry %t", departed, approached)
	}
	return counts
}

// TestPickupBerthFitsLoadFree checks that pickupBerthFitsRequest, which
// gives noBerthLoad to the station approach, gives the answer of the
// load-ranked approach of stationApproachRouteForClass while the blocked
// set is not empty. The load selects only among bank entries that the
// bank exit reaches, so the answer is the departure from the berth to its
// bank exit. london-full:dedicated has the banked hubs, and its runs must
// refuse a pair on the departure and select another entry by load.
// london-full:shared has no banks.
func TestPickupBerthFitsLoadFree(t *testing.T) {
	skipLong(t)
	t.Parallel()
	p := readSharedExpressProject(t)
	for _, c := range []struct {
		name    string
		network Network
		banked  bool
	}{
		{"london-full:dedicated", expressNetworkFixture(t), true},
		{"london-full:shared", p.Network, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			counts := runPickupLoadFree(t, p, c.network)
			t.Logf("%+v", counts)
			if counts.compared == 0 || counts.refused == 0 || counts.refused == counts.compared || counts.unviewed == 0 {
				t.Fatalf("coverage %+v: need pairs with and without a route", counts)
			}
			if c.banked && (counts.rewritten == 0 || counts.departureRefused == 0 || counts.entryChanged == 0) {
				t.Fatalf("coverage %+v: need bank pairs, a refused departure, and a changed entry", counts)
			}
		})
	}
}
