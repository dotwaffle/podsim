package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"strings"
	"testing"
)

func newTraffic(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReservationLookaheadConfiguration(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if s.reservationLookaheadSeconds != defaultReservationLookaheadSeconds {
		t.Fatalf("default lookahead = %v, want %v", s.reservationLookaheadSeconds, defaultReservationLookaheadSeconds)
	}
	if err := s.SetReservationLookahead(0.75); err != nil {
		t.Fatal(err)
	}
	s.Reset()
	if s.reservationLookaheadSeconds != 0.75 {
		t.Fatalf("lookahead after reset = %v, want 0.75", s.reservationLookaheadSeconds)
	}

	for _, seconds := range []float64{-1, math.NaN(), math.Inf(1), maxReservationLookaheadSeconds + 1} {
		if err := s.SetReservationLookahead(seconds); err == nil {
			t.Fatalf("SetReservationLookahead(%v) accepted", seconds)
		}
	}
}

func TestResourceReleaseDistance(t *testing.T) {
	t.Parallel()
	b := block{start: 30, end: 60, lane: Lane{From: "origin"}}
	for _, test := range []struct {
		name string
		kind resourceKind
		id   string
		want float64
	}{
		{name: "junction extent includes clearance", kind: junctionResource, want: 60},
		{name: "departure node", kind: nodeResource, id: "origin", want: 30 + Clearance},
		{name: "arrival node", kind: nodeResource, id: "destination", want: 60 + Clearance},
		{name: "track cell", kind: trackResource, want: 60 + Clearance},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := resourceReleaseDistance(b, resource{kind: test.kind, id: test.id}); got != test.want {
				t.Fatalf("release distance = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIncrementalResourceRelease(t *testing.T) {
	t.Parallel()
	junction := resource{kind: junctionResource, id: "junction"}
	clearedTrack := resource{kind: trackResource, id: "lane", cell: 1}
	futureOrigin := resource{kind: nodeResource, id: "origin"}
	originBerth := resource{kind: berthResource, id: "origin-berth"}
	v := vehicle{
		Pod:      Pod{ID: "01", Activity: Traveling},
		origin:   Berth{ID: originBerth.id, Node: futureOrigin.id},
		distance: 60,
		routeReleases: map[resource]float64{
			junction:     100,
			clearedTrack: 50,
			futureOrigin: 100,
		},
	}
	s := &Simulation{
		owners: map[resource]string{
			junction: "01", clearedTrack: "01", futureOrigin: "01", originBerth: "01",
		},
		vehicles: []vehicle{v},
	}
	s.releaseCleared()
	if s.owners[clearedTrack] != "" {
		t.Fatal("cleared track remains owned")
	}
	for _, retained := range []resource{junction, futureOrigin} {
		if s.owners[retained] != "01" {
			t.Fatalf("future resource %+v was released", retained)
		}
	}
	if s.owners[originBerth] != "" {
		t.Fatal("cleared origin berth remains owned")
	}
}

func TestArrivalReleasesRouteAndKeepsBerth(t *testing.T) {
	t.Parallel()
	berth := resource{kind: berthResource, id: "destination-berth"}
	node := resource{kind: nodeResource, id: "destination-node"}
	track := resource{kind: trackResource, id: "lane", cell: 1}
	s := &Simulation{
		network: Network{
			Nodes:    []Node{{ID: node.id}},
			Stations: []Station{{ID: "destination", Berths: []Berth{{ID: berth.id, Node: node.id}}}},
		},
		owners: map[resource]string{berth: "01", node: "01", track: "01"},
		vehicles: []vehicle{{
			Pod:           Pod{ID: "01", Activity: Unloading, StationID: "destination", BerthID: berth.id},
			routeReleases: map[resource]float64{berth: 100, node: 100, track: 100},
		}},
	}
	s.releaseCleared()
	if s.owners[berth] != "01" || s.owners[node] != "01" {
		t.Fatalf("arrival berth was released: %v", s.owners)
	}
	if s.owners[track] != "" {
		t.Fatal("arrival retained old track")
	}
	if len(s.vehicles[0].routeReleases) != 0 {
		t.Fatalf("arrival retained release state: %v", s.vehicles[0].routeReleases)
	}
}

// checkRouteReleaseBound checks the promise of vehicle.nextRelease. When it
// is not 0, the pod owns each resource in routeReleases, and no release
// distance is less than it.
func checkRouteReleaseBound(t *testing.T, s *Simulation) {
	t.Helper()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.nextRelease == 0 {
			continue
		}
		for r, releaseAt := range v.routeReleases {
			if owner := s.owners[r]; owner != v.Pod.ID || releaseAt < v.nextRelease {
				t.Fatalf("tick %d: pod %s has bound %v, but it keeps %+v to %v and the owner is %q",
					s.tick, v.Pod.ID, v.nextRelease, r, releaseAt, owner)
			}
		}
	}
}

// TestRouteReleaseBoundMatchesFullScan steps each clone fixture beside a
// clone that has no release bounds. The clone checks each held resource at
// each step, as the code did before the bounds. The two must stay equal
// apart from the bounds.
func TestRouteReleaseBoundMatchesFullScan(t *testing.T) {
	t.Parallel()
	for _, fixture := range cloneFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			s := fixture.build(t)
			inputs := fixture.continuation
			for tick := range inputs.seconds * TicksPerSecond {
				full := s.Clone()
				for i := range full.vehicles {
					full.vehicles[i].nextRelease = 0
				}
				for _, sim := range []*Simulation{s, full} {
					for _, trip := range inputs.trips {
						if trip.second*TicksPerSecond != tick {
							continue
						}
						if err := sim.RequestTrip(trip.from, trip.to); err != nil {
							t.Fatal(err)
						}
					}
					sim.Step()
				}
				checkRouteReleaseBound(t, s)
				for i := range full.vehicles {
					full.vehicles[i].nextRelease = s.vehicles[i].nextRelease
				}
				if !sameState(s, full) {
					t.Fatalf("tick %d: the release bounds changed the state", s.tick)
				}
			}
		})
	}
}

func TestRouteReleaseBoundAtReleaseDistance(t *testing.T) {
	t.Parallel()
	track := resource{kind: trackResource, id: "lane", cell: 1}
	s := &Simulation{
		owners: map[resource]string{track: "01"},
		vehicles: []vehicle{{
			Pod:            Pod{ID: "01", Activity: Traveling},
			distance:       60,
			originReleased: true,
			routeReleases:  map[resource]float64{track: 60},
			nextRelease:    60,
		}},
	}
	s.releaseCleared()
	if s.owners[track] != "" || len(s.vehicles[0].routeReleases) != 0 {
		t.Fatalf("pod at the release distance kept the track: owners %v, releases %v", s.owners, s.vehicles[0].routeReleases)
	}
	if s.vehicles[0].nextRelease != math.Inf(1) {
		t.Fatalf("bound with no held resource = %v, want +Inf", s.vehicles[0].nextRelease)
	}
}

// TestRouteReleaseBoundFollowsNewResource admits a resource with a release
// distance before the bound. A junction in a short cell after the stopping
// point has such a distance.
func TestRouteReleaseBoundFollowsNewResource(t *testing.T) {
	t.Parallel()
	track := resource{kind: trackResource, id: "lane", cell: 1}
	junction := resource{kind: junctionResource, id: "junction"}
	s := &Simulation{
		owners:   map[resource]string{track: "01", junction: "01"},
		vehicles: []vehicle{{Pod: Pod{ID: "01", Activity: Traveling}, distance: 40, originReleased: true}},
	}
	v := &s.vehicles[0]
	v.retainRouteResource(track, 100)
	s.releaseCleared()
	if v.nextRelease != 100 {
		t.Fatalf("bound = %v, want 100", v.nextRelease)
	}
	v.retainRouteResource(junction, 50)
	v.distance = 60
	s.releaseCleared()
	if s.owners[junction] != "" || s.owners[track] != "01" {
		t.Fatalf("owners after the junction = %v, want only the track", s.owners)
	}
}

// TestRouteReleaseBoundDroppedWithOwner gives a relocating pod its
// destination claims as route resources. A yield or a redirect then deletes
// the claims. The pod must check its entries again at the next release, as
// the code did before the bounds, and remove the entries of the claims.
func TestRouteReleaseBoundDroppedWithOwner(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		drop func(s *Simulation, v *vehicle)
	}{
		{
			name: "yield",
			drop: func(s *Simulation, v *vehicle) {
				pickup := s.findVehicle("02")
				pickup.destination = v.destination
				s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 1, From: "market", To: "garden", PodID: "02"}})
				s.yieldRelocationClaims()
			},
		},
		{
			name: "redirect",
			drop: func(s *Simulation, v *vehicle) {
				s.redirect(v, redirection{route: v.Route, berth: v.destination, station: v.destinationStation})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newClaimConflictSimulation(t)
			s.Step()
			v := s.findVehicle("01")
			claims := berthResources(v.destination)
			track := resource{kind: trackResource, id: "held", cell: 0}
			s.owners[track] = v.Pod.ID
			for index, claimed := range append(claims[:], track) {
				if s.owners[claimed] != v.Pod.ID {
					t.Fatalf("pod 01 does not own %+v", claimed)
				}
				v.retainRouteResource(claimed, 1000+float64(index))
			}
			s.releasePassedResources(v)
			if v.nextRelease == 0 || v.distance >= v.nextRelease {
				t.Fatalf("pod 01 at %v has bound %v, so the next release does not skip", v.distance, v.nextRelease)
			}
			test.drop(s, v)
			for _, claimed := range claims {
				if s.owners[claimed] != "" {
					t.Fatalf("claim %+v has owner %q", claimed, s.owners[claimed])
				}
			}
			// A new resource must not end the check, also when its release
			// distance is not a number.
			extra := resource{kind: trackResource, id: "held", cell: 1}
			s.owners[extra] = v.Pod.ID
			v.retainRouteResource(extra, math.NaN())
			checkRouteReleaseBound(t, s)
			s.releasePassedResources(v)
			for _, claimed := range claims {
				if _, kept := v.routeReleases[claimed]; kept {
					t.Fatalf("route releases keep claim %+v after its owner went: %v", claimed, v.routeReleases)
				}
			}
			if _, kept := v.routeReleases[extra]; !kept || v.routeReleases[track] != 1002 {
				t.Fatalf("route releases lost an owned track: %v", v.routeReleases)
			}
		})
	}
}

func checkIncrementalOwners(t *testing.T, s *Simulation) {
	t.Helper()
	if want := s.retainedOwners(); !maps.Equal(s.owners, want) {
		t.Fatalf("incremental owners differ from retention scan at tick %d:\n got %v\nwant %v", s.tick, s.owners, want)
	}
}

// checkTraffic measures visible positions, independently of controller ownership.
func checkTraffic(t *testing.T, state Snapshot) {
	t.Helper()
	for i, a := range state.Vehicles {
		if !finite(a.Pod.Position.X) || !finite(a.Pod.Position.Y) || a.Pod.Speed < 0 || a.Pod.Speed > 14 {
			t.Fatalf("invalid pod at tick %d: %+v", state.Tick, a.Pod)
		}
		for _, b := range state.Vehicles[i+1:] {
			gap := math.Hypot(a.Pod.Position.X-b.Pod.Position.X, a.Pod.Position.Y-b.Pod.Position.Y)
			if gap < Clearance-1e-6 {
				t.Fatalf("tick %d: pods %s and %s only %.5fm apart: %+v %+v", state.Tick, a.Pod.ID, b.Pod.ID, gap, a.Pod, b.Pod)
			}
		}
	}
	for _, b := range state.Berths {
		occupants := 0
		for _, v := range state.Vehicles {
			if v.Pod.BerthID == b.ID {
				occupants++
				if b.Occupant != v.Pod.ID || b.ReservedBy != v.Pod.ID {
					t.Fatalf("invalid berth state %+v", b)
				}
			}
		}
		if occupants > 1 {
			t.Fatalf("berth capacity exceeded: %+v", b)
		}
	}
}

func TestTrafficDemoSafetyAndProgress(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	waits := make(map[WaitReason]int)
	slows := make(map[WaitReason]int)
	mergeSlows := 0
	mergePasses := make(map[string]bool)
	for range 900 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		checkIncrementalOwners(t, s)
		for _, v := range state.Vehicles {
			if v.Pod.WaitReason != NoWait {
				if waits[v.Pod.WaitReason] == 0 {
					t.Logf("first %s: tick %d pod %s", v.Pod.WaitReason, state.Tick, v.Pod.ID)
				}
				waits[v.Pod.WaitReason]++
				if v.Pod.Speed < 13 {
					slows[v.Pod.WaitReason]++
					if v.Pod.LaneID == "garden-merge" || v.Pod.LaneID == "bypass-merge" {
						mergeSlows++
					}
				}
			}
			if v.Pod.LaneID == "market-approach" {
				mergePasses[v.Pod.ID] = true
			}
		}
		if state.Completed == demoJourneys && !state.Demo {
			break
		}
	}
	state := s.Snapshot()
	t.Logf("finished tick %d, waits %v, slows %v, passes %v", state.Tick, waits, slows, mergePasses)
	if state.Completed != demoJourneys || state.Demo {
		t.Fatalf("demo did not finish: %+v", state)
	}
	if len(mergePasses) != 4 {
		t.Fatalf("all pods did not pass merge: %v", mergePasses)
	}
	if waits[JunctionOccupied] == 0 || mergeSlows == 0 {
		t.Fatal("demo did not exercise junction contention")
	}
	if waits[BerthOccupied] == 0 {
		t.Fatal("demo did not exercise a full berth")
	}
}

func TestFullBerthWaitAndClearance(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(withoutParking(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	blockPassengerClearingRoutes(s)
	advance(s, 180*TicksPerSecond)
	state := s.Snapshot()
	checkTraffic(t, state)
	waiting := state.Vehicles[0].Pod
	if waiting.WaitReason != ParkingUnavailable || waiting.Speed > 0.01 || waiting.LaneID != "market-in" || waiting.BerthID != "" ||
		waiting.StationPhase != AccessingBerth || waiting.ManeuverStationID != "market" {
		t.Fatalf("arrival must wait on the inlet outside the occupied berth: %+v", waiting)
	}
	if state.Completed != 0 {
		t.Fatal("blocked arrival completed")
	}
	delete(s.routes, routeKey{from: "market-berth", to: "garden-berth"})
	if err = s.RequestJourney("02", "garden"); err != nil {
		t.Fatal(err)
	}
	for range 360 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 2 {
			break
		}
	}
	if state = s.Snapshot(); state.Completed != 2 {
		t.Fatalf("berth departure did not release waiting arrival: %+v", state)
	}
}

func TestFleetResetPauseAndOrder(t *testing.T) {
	t.Parallel()
	a := newTraffic(t)
	b, err := NewFleet(Example(), []Placement{{ID: "02", StationID: "garden"}, {ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	initial := a.Snapshot()
	for _, s := range []*Simulation{a, b} {
		if err := s.StartDemo(); err != nil {
			t.Fatal(err)
		}
	}
	for range 120 * TicksPerSecond {
		a.Step()
		b.Step()
		if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
			t.Fatalf("fleet order changed outcome at tick %d", a.Snapshot().Tick)
		}
	}
	a.SetPaused(true)
	paused := a.Snapshot()
	advance(a, 600)
	if !reflect.DeepEqual(paused, a.Snapshot()) {
		t.Fatal("pause advanced fleet or script")
	}
	a.Reset()
	if !reflect.DeepEqual(initial, a.Snapshot()) {
		t.Fatal("reset left fleet or resource state")
	}
}

func TestFleetValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		placements []Placement
	}{
		{"empty", nil},
		{"duplicate ID", []Placement{{ID: "01", StationID: "harbor"}, {ID: "01", StationID: "garden"}}},
		{"duplicate berth", []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "harbor"}}},
		{"unknown station", []Placement{{ID: "01", StationID: "missing"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewFleet(Example(), tc.placements); err == nil {
				t.Fatal("invalid fleet accepted")
			}
		})
	}
}

func TestValidateFleetMatchesNewFleet(t *testing.T) {
	t.Parallel()
	pods := []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}}
	for _, tc := range []struct {
		name       string
		change     func(*Network)
		placements []Placement
		want       string
	}{
		{name: "valid", placements: pods},
		{name: "duplicate node", change: func(n *Network) { n.Nodes[1].ID = n.Nodes[0].ID }, placements: pods, want: "duplicate node"},
		{name: "short lane", change: func(n *Network) { n.Nodes[3].Position = Point{X: 210, Y: 260} }, placements: pods, want: "must be at least"},
		{name: "empty fleet", want: "at least one pod"},
		{name: "duplicate ID", placements: []Placement{{ID: "01", StationID: "harbor"}, {ID: "01", StationID: "garden"}}, want: "duplicate pod"},
		{name: "unknown station", placements: []Placement{{ID: "01", StationID: "missing"}}, want: "unknown start station"},
		{name: "occupied berth", placements: []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "harbor"}}, want: "occupied initial berth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := Example()
			if tc.change != nil {
				tc.change(&network)
			}
			err := ValidateFleet(network, tc.placements)
			if _, fleetErr := NewFleet(network, tc.placements); fmt.Sprint(err) != fmt.Sprint(fleetErr) {
				t.Fatalf("ValidateFleet() = %v, NewFleet() = %v", err, fleetErr)
			}
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateFleet() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateFleet() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestQueueBehindStoppedPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(withoutParking(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"01", "02"} {
		if err := s.RequestJourney(id, "market"); err != nil {
			t.Fatal(err)
		}
	}
	blockPassengerClearingRoutes(s)
	followed := false
	for range 240 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		for _, v := range state.Vehicles {
			if v.Pod.WaitReason == TrackOccupied {
				followed = true
			}
		}
	}
	state := s.Snapshot()
	a, b := state.Vehicles[0].Pod, state.Vehicles[1].Pod
	if !followed || a.LaneID != "market-in" || b.LaneID != "market-in" || a.Speed > 0.01 || b.Speed > 0.01 {
		t.Fatalf("no stopped inlet queue: followed=%v pods=%+v %+v", followed, a, b)
	}
	if state.Completed != 0 {
		t.Fatal("a blocked arrival completed")
	}
}

func TestFullBerthDoesNotBlockThroughLane(t *testing.T) {
	t.Parallel()
	for _, short := range []bool{false, true} {
		name := "supplied inlet"
		if short {
			name = "two-cell inlet"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := withoutParking()
			if short {
				for i := range network.Nodes {
					if network.Nodes[i].ID == "market-berth" {
						network.Nodes[i].Position = Point{X: 730, Y: 310}
					}
				}
			}
			checkThroughTraffic(t, network)
		})
	}
}

func checkThroughTraffic(t *testing.T, network Network) {
	t.Helper()
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}, {ID: "03", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	blockPassengerClearingRoutes(s)
	advance(s, 180*TicksPerSecond)
	if got := s.Snapshot().Vehicles[0].Pod; got.WaitReason != ParkingUnavailable {
		t.Fatalf("expected berth queue: %+v", got)
	}
	if err := s.RequestJourney("03", "harbor"); err != nil {
		t.Fatal(err)
	}
	passed := false
	for range 360 * TicksPerSecond {
		s.Step()
		state := s.Snapshot()
		checkTraffic(t, state)
		if state.Vehicles[2].Pod.LaneID == "market-through" {
			passed = true
		}
		if state.Completed == 1 {
			break
		}
	}
	state := s.Snapshot()
	if !passed || state.Completed != 1 || state.Vehicles[2].Pod.StationID != "harbor" {
		t.Fatalf("through traffic blocked: passed=%v state=%+v", passed, state)
	}
	if state.Vehicles[0].Pod.WaitReason != ParkingUnavailable || state.Vehicles[1].Pod.BerthID != "market-1" {
		t.Fatal("the occupied berth unexpectedly cleared")
	}
}

func TestDemoRejectsBrokenScriptBeforeReset(t *testing.T) {
	t.Parallel()
	for _, broken := range []string{"bypass-in", "garden-merge", "return-start"} {
		t.Run(broken, func(t *testing.T) {
			t.Parallel()
			n := Example()
			for i := range n.Lanes {
				if n.Lanes[i].ID != broken {
					continue
				}
				if broken == "bypass-in" {
					n.Lanes[i].ID = "renamed"
				} else {
					n.Lanes = append(n.Lanes[:i], n.Lanes[i+1:]...)
				}
				break
			}
			s, err := NewFleet(n, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			advance(s, 50)
			before := s.Snapshot()
			if err := s.StartDemo(); err == nil {
				t.Fatal("invalid demo accepted")
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("invalid demo reset the current simulation")
			}
		})
	}
}

func TestDemoReportsInterruptedRequest(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("02", "harbor"); err != nil {
		t.Fatal(err)
	}
	for range 120 * TicksPerSecond {
		s.Step()
		if !s.Snapshot().Demo {
			break
		}
	}
	state := s.Snapshot()
	if state.Demo || state.DemoError == "" {
		t.Fatalf("demo failed silently: %+v", state)
	}
}

// withoutParking retains the track geometry but provides no empty-pod destination.
func withoutParking() Network {
	n := Example()
	n.Stations = n.Stations[:3]
	for index := range n.Lanes {
		if n.Lanes[index].StationID == "parking" {
			n.Lanes[index].StationID = ""
			n.Lanes[index].StationRole = ""
		}
	}
	return n
}

// blockPassengerClearingRoutes keeps these tests focused on an unavailable berth destination.
func blockPassengerClearingRoutes(s *Simulation) {
	for _, destination := range []string{"harbor-berth", "garden-berth"} {
		s.routes[routeKey{from: "market-berth", to: destination}] = routeResult{err: ErrUnreachable}
	}
}

func TestDemoStartsWithParkedPodsAndDispatchesBoth(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	initial := s.Snapshot()
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	state := s.Snapshot()
	if len(state.Vehicles) != 4 {
		t.Fatalf("fleet size %d", len(state.Vehicles))
	}
	for _, id := range []string{"03", "04"} {
		v := s.findVehicle(id)
		if v.Pod.StationID != "parking" || v.Pod.Activity != Idle || v.Pod.Occupied {
			t.Fatalf("pod %s did not start parked", id)
		}
	}
	carried := make(map[string]bool)
	maxPending := 0
	for range 900 * TicksPerSecond {
		s.Step()
		state = s.Snapshot()
		maxPending = max(maxPending, len(state.Pending))
		for _, v := range state.Vehicles {
			if v.Pod.Occupied {
				carried[v.Pod.ID] = true
			}
		}
		if !state.Demo {
			break
		}
	}
	if state.DemoError != "" || state.Completed != demoJourneys || state.Submitted != demoJourneys || len(state.Pending) != 0 {
		t.Fatalf("incomplete demo: %+v", state)
	}
	if !carried["03"] || !carried["04"] || maxPending < 2 {
		t.Fatalf("parked pods or queue not exercised: carried%v pending%d", carried, maxPending)
	}
	s.Reset()
	if !reflect.DeepEqual(initial, s.Snapshot()) {
		t.Fatal("reset retained demo pods")
	}
}
