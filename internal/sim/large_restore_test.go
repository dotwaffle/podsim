package sim

import (
	"maps"
	"math"
	"slices"
	"testing"
)

func largeRestoreNetwork() Network {
	n := lineNetwork([]lineStation{{id: "harbor", berths: 1}, {id: "garden", berths: 1}, {id: "market", berths: 1}})
	for i := range n.Lanes {
		switch n.Lanes[i].ID {
		case "harbor-1-out":
			n.Lanes[i].ID = "harbor-out"
		case "harbor-link":
			n.Lanes[i].ID = "approach-branch"
			n.Lanes[i].StationID, n.Lanes[i].StationRole = "garden", StationApproachRole
		case "garden-link":
			n.Lanes[i].StationID, n.Lanes[i].StationRole = "market", StationApproachRole
		case "return-up":
			n.Lanes[i].StationID, n.Lanes[i].StationRole = "harbor", StationApproachRole
		}
	}
	for i := range n.Nodes {
		n.Nodes[i].Position.X *= 2
		n.Nodes[i].Position.Y *= 2
	}
	classes := classBit(string(LegacyClass)) | classBit(string(CompactClass)) | classBit(string(GroupClass))
	for i := range n.Lanes {
		n.Lanes[i].VehicleClasses = classes
		if p := n.Lanes[i].Control; p != nil {
			n.Lanes[i].Control = &Point{X: p.X * 2, Y: p.Y * 2}
		}
	}
	for i := range n.Stations {
		n.Stations[i].VehicleClasses = classes
		for j := range n.Stations[i].Berths {
			n.Stations[i].Berths[j].VehicleClasses = classes
		}
	}
	return n
}

func TestLargeRestoreOriginRetention(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		network  Network
		distance float64
		held     bool
	}{
		{"large geometry before tail", largeRestoreNetwork(), 16, true},
		{"large geometry at tail", largeRestoreNetwork(), 20, false},
		{"ordinary before tail", Example(), 11, true},
		{"ordinary at tail", Example(), 12, false},
		{"ordinary after tail", Example(), 16, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newRestoreFleetFixture(t, test.network, []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
			pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "harbor-out", distance: test.distance}), 1, 1)
			s, result, err := f.restore(roundTripState(t, f.state(pod)))
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("cold restore: %+v, %v", result, err)
			}
			v := findVehicle(t, s, "01")
			if v.originReleased == test.held {
				t.Fatalf("originReleased=%v, want %v (tail %v)", v.originReleased, !test.held, v.originTail())
			}
			berth := resource{kind: berthResource, id: "harbor-1"}
			if got := s.owners[berth] == "01"; got != test.held {
				t.Fatalf("incremental origin owner=%v, want %v", got, test.held)
			}
			if got := s.retainedOwners()[berth] == "01"; got != test.held {
				t.Fatalf("reconstructed origin owner=%v, want %v", got, test.held)
			}
			if got := slices.Contains(v.footprint(v.reservedThrough, v.distance), berth); got != test.held {
				t.Fatalf("footprint origin=%v, want %v", got, test.held)
			}
			checkIncrementalOwners(t, s)
		})
	}
}

func TestLargeRestoreTrimRetention(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		network  Network
		distance float64
		first    string
	}{
		{"large before tail", largeRestoreNetwork(), 16, "harbor-out"},
		{"large at tail", largeRestoreNetwork(), 20, "approach-branch"},
		{"ordinary after tail", Example(), 16, "approach-branch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newRestoreFleetFixture(t, test.network, []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
			pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "approach-branch", distance: test.distance}), 1, 1)
			s, result, err := f.restore(f.state(pod))
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("initial restore: %+v, %v", result, err)
			}
			saved := roundTripState(t, s.ExportState())
			if got := test.network.Lanes[saved.Pods[0].Route[0]].ID; got != test.first {
				t.Fatalf("first retained lane=%s, want %s", got, test.first)
			}
			cold, coldResult, err := f.restore(saved)
			if err != nil || coldResult.Tier != RestorePhysical {
				t.Fatalf("trimmed cold restore: %+v, %v", coldResult, err)
			}
			if !maps.Equal(s.owners, cold.owners) {
				t.Fatalf("trim changed owners: before=%v, after=%v", s.owners, cold.owners)
			}
			if got, want := findVehicle(t, cold, "01").riddenMeters(), findVehicle(t, s, "01").riddenMeters(); math.Abs(got-want) > restoreTolerance {
				t.Fatalf("trim changed ridden distance: got %v, want %v", got, want)
			}
			if cold.completed != s.completed || cold.boarded != s.boarded || len(findVehicle(t, cold, "01").Riders) != 1 {
				t.Fatal("trim changed order conservation")
			}
			checkIncrementalOwners(t, cold)
		})
	}
}

func TestLargeRestoreLinksRejectBeforeTiers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*RestoreStateInput)
	}{
		{"group follower", func(i *RestoreStateInput) {
			i.State.Pods[1].Class = GroupClass
			i.State.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01"}
		}},
		{"group leader", func(i *RestoreStateInput) {
			i.State.Pods[0].Class = GroupClass
			i.State.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01"}
		}},
		{"express follower", func(i *RestoreStateInput) {
			i.State.Pods[1].Class = ExpressClass
			i.State.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01"}
		}},
		{"large fleet mismatch", func(i *RestoreStateInput) {
			i.Fleet[0].Class = GroupClass
			i.State.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01"}
		}},
		{"large compact head", func(i *RestoreStateInput) {
			i.State.Pods[0].Class = GroupClass
			i.State.Pods[0].CompactQueue = &SavedCompactQueue{Members: []string{"02"}}
		}},
		{"large compact member", func(i *RestoreStateInput) {
			i.State.Pods[1].Class = GroupClass
			i.State.Pods[0].CompactQueue = &SavedCompactQueue{Members: []string{"02"}}
		}},
		{"large compact link", func(i *RestoreStateInput) {
			i.State.Pods[0].Class = GroupClass
			i.State.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01", Kind: "compact-buffer-v1"}
		}},
	}
	for _, test := range tests {
		for _, logical := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/physical", true: "/logical"}[logical], func(t *testing.T) {
				t.Parallel()
				f := newRestoreFixture(t, Example())
				input := RestoreStateInput{Network: f.network, Fleet: slices.Clone(f.fleet), State: f.state(), LogicalOnly: logical}
				test.edit(&input)
				if err := checkLargeLinkFields(input); err == nil {
					t.Fatal("large link passed the field guard")
				}
				s, _, err := restoreState(input, func() (*Simulation, error) { t.Fatal("large link reached restore tier"); return nil, nil })
				if err == nil || s != nil {
					t.Fatalf("large link accepted: %v", err)
				}
			})
		}
	}
}

func TestLargeRestoreUnrelatedDestinationTail(t *testing.T) {
	t.Parallel()
	n := Example()
	// Only the destination node of Harbor's first lane admits large traffic.
	for i := range n.Lanes {
		if n.Lanes[i].ID == "approach-branch" {
			n.Lanes[i].VehicleClasses = classBit(string(LegacyClass)) | classBit(string(GroupClass))
		}
	}
	f := newRestoreFleetFixture(t, n, []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "harbor-out", distance: 16}), 1, 1)
	s, result, err := f.restore(f.state(pod))
	if err != nil || result.Tier != RestorePhysical {
		t.Fatalf("restore: %+v, %v", result, err)
	}
	v := findVehicle(t, s, "01")
	if v.originTail() != Clearance || !v.originReleased || berthOwner(s, "harbor-1") != "" {
		t.Fatal("unrelated destination enlarged origin retention")
	}
}

func TestLargeRestoreRecordedOriginMismatch(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, largeRestoreNetwork(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "harbor-out", distance: 16}), 1, 2)
	pod.Boardings = []RiderBoarding{{BerthID: "harbor-1"}, {BerthID: "harbor-1", MetersAtBoarding: 7}}
	pod.RiddenMeters = 10
	pod.Origin = "garden-1"
	state := f.state(pod)
	s, result, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, BoardingRecords: true})
	if err != nil || result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"01"}) || !slices.Equal(result.Requeued, []int{1, 2}) {
		t.Fatalf("wrong retained origin: %+v, %v", result, err)
	}
	checkRecordedRequeue(t, state, s)
}

func TestLargeRestoreRecordedFallbackConservation(t *testing.T) {
	t.Parallel()
	for _, logical := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical demotion", true: "logical"}[logical], func(t *testing.T) {
			t.Parallel()
			f := newRestoreFleetFixture(t, largeRestoreNetwork(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
			pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "harbor-out", distance: 16}), 1, 2)
			pod.Boardings = []RiderBoarding{{BerthID: "harbor-1"}, {BerthID: "harbor-1", MetersAtBoarding: 7}}
			pod.RiddenMeters = 10
			pod.Route = nil
			state := f.state(pod)
			s, result, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, BoardingRecords: true, LogicalOnly: logical})
			if err != nil || !slices.Equal(result.Requeued, []int{1, 2}) {
				t.Fatalf("record fallback: %+v, %v", result, err)
			}
			checkRecordedRequeue(t, state, s)
		})
	}
}

func TestLargeRestoreClassMismatch(t *testing.T) {
	t.Parallel()
	for _, logical := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical", true: "logical"}[logical], func(t *testing.T) {
			t.Parallel()
			f := newRestoreFleetFixture(t, largeRestoreNetwork(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1", Class: CompactClass}})
			state := f.state()
			state.Pods[0].Class = LegacyClass
			s, _, err := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, LogicalOnly: logical})
			if err == nil || s != nil {
				t.Fatal("saved/fleet class mismatch restored")
			}
		})
	}
}

func TestLargeRestoreExpandedJunctionExtent(t *testing.T) {
	t.Parallel()
	b := block{lane: Lane{From: "origin"}, start: 40, end: 70, tail: 20, fromTail: 20}
	for _, test := range []struct {
		name     string
		resource resource
		want     float64
	}{
		{"junction already expanded", resource{kind: junctionResource, id: "crossing"}, 70},
		{"origin node", resource{kind: nodeResource, id: "origin"}, 60},
		{"track tail", resource{kind: trackResource, id: "lane"}, 90},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := resourceReleaseDistance(b, test.resource); got != test.want {
				t.Fatalf("release=%v, want %v", got, test.want)
			}
		})
	}
}

func TestLargeRestoreContractLinks(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	state := f.state()
	state.Pods[0].Class = GroupClass
	state.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01"}
	if _, err := state.checkContract(); err == nil {
		t.Fatal("live contract accepted a large linked pod")
	}
}

func checkLargeJourneyColdRestore(t *testing.T, live *Simulation, network Network, fleet []Placement) {
	t.Helper()
	saved := roundTripState(t, live.ExportState())
	cold, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: saved})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("tick %d cold restore: %+v, %v", live.tick, result, err)
	}
	checkRestoredMatches(t, live, cold)
	checkLargeColdRetainedOwners(t, live, cold)
	for steps := 0; cold.completed < saved.RequestID && steps < 30000; steps++ {
		cold.Step()
		checkIncrementalOwners(t, cold)
	}
	if cold.completed != saved.RequestID || cold.unaccountedOrders != 0 || len(cold.waiting) != 0 {
		t.Fatalf("cold continuation lost orders: %+v", cold.ExportState())
	}
	warm := live.Clone()
	for steps := 0; warm.completed < saved.RequestID && steps < 30000; steps++ {
		warm.Step()
	}
	if cold.journeys != warm.journeys || math.Abs(cold.passengerDistanceMeters-warm.passengerDistanceMeters) > restoreTolerance || math.Abs(cold.riderDistanceMeters-warm.riderDistanceMeters) > restoreTolerance || math.Abs(cold.directDistanceMeters-warm.directDistanceMeters) > restoreTolerance {
		t.Fatalf("cold continuation changed distance totals: cold occupied=%v ridden=%v direct=%v, warm occupied=%v ridden=%v direct=%v", cold.passengerDistanceMeters, cold.riderDistanceMeters, cold.directDistanceMeters, warm.passengerDistanceMeters, warm.riderDistanceMeters, warm.directDistanceMeters)
	}
}

func TestLargeRestoreGroupProductionPhases(t *testing.T) {
	t.Parallel()
	network := largeRestoreNetwork()
	fleet := []Placement{{ID: "01", Class: GroupClass, StationID: "harbor", BerthID: "harbor-1"}}
	live, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	capture := func(name string) {
		if !seen[name] {
			seen[name] = true
			checkpoint := live.Clone()
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				checkLargeJourneyColdRestore(t, checkpoint, network, fleet)
			})
		}
	}
	capture("initial idle")
	if err := live.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 8, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
	for steps := 0; live.completed < 1 && steps < 30000; steps++ {
		v := findVehicle(t, live, "01")
		capture(activityCode(v.Pod.Activity))
		if v.Pod.Activity == Traveling && v.distance >= 12 && v.distance < 20 {
			if v.originReleased || berthOwner(live, "harbor-1") != "01" {
				t.Fatal("production Group released origin before 20 m")
			}
			capture("occupied origin tail")
		}
		if v.Pod.Activity == Traveling {
			b := v.blocks.at(v.blockIndex)
			local := v.distance - b.laneStart
			if b.lane.ID == "approach-branch" && local >= 12 && local < 20 {
				saved := live.ExportState().Pods[0]
				if network.Lanes[saved.Route[0]].ID != "harbor-out" {
					t.Fatal("production Group trim lost 20 m lane tail")
				}
				capture("occupied trim tail")
			}
		}
		live.Step()
		checkIncrementalOwners(t, live)
	}
	if live.completed != 1 {
		t.Fatal("first Group journey did not finish")
	}
	capture("completed idle")
	if _, err := live.SubmitTripOptions(TripOptions{From: "harbor", To: "garden", PartySize: 8, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
	for steps := 0; live.completed < 2 && steps < 30000; steps++ {
		v := findVehicle(t, live, "01")
		if !v.Pod.Occupied {
			capture("empty " + activityCode(v.Pod.Activity))
		}
		live.Step()
		checkIncrementalOwners(t, live)
	}
	if live.completed != 2 {
		t.Fatal("second Group journey did not finish")
	}
	for _, name := range []string{"initial idle", "boarding", "traveling", "unloading", "occupied origin tail", "occupied trim tail", "completed idle", "empty departing", "empty traveling"} {
		if !seen[name] {
			t.Errorf("production phase %s not observed", name)
		}
	}
	if live.journeys != 2 || live.boarded != 2 || live.requestID != 2 || live.unaccountedOrders != 0 {
		t.Fatal("production Group journey conservation failed")
	}
}

func TestLargeRestoreGroupIntermediateProduction(t *testing.T) {
	t.Parallel()
	network := largeRestoreNetwork()
	fleet := []Placement{{ID: "01", Class: GroupClass, StationID: "harbor", BerthID: "harbor-1"}}
	live, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := live.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	if err := live.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 4, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if _, err := live.SubmitTripOptions(TripOptions{From: "harbor", To: "garden", PartySize: 4, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if v := findVehicle(t, live, "01"); len(v.Riders) != 2 || v.PassengersAboard() != 8 || !slices.Equal(v.Stops, []string{"garden", "market"}) {
		t.Fatalf("Group parties did not join: %+v", live.Snapshot())
	}
	seen := make(map[podPhase]bool)
	for steps := 0; live.completed < 2 && steps < 30000; steps++ {
		saved := live.ExportState().Pods[0]
		phase, err := phaseOf(saved)
		if err != nil {
			t.Fatal(err)
		}
		if phase == phaseUnloadingIntermediate && !seen[phase] {
			seen[phase] = true
			checkpoint := live.Clone()
			t.Run("intermediate unloading", func(t *testing.T) {
				t.Parallel()
				checkLargeJourneyColdRestore(t, checkpoint, network, fleet)
			})
		}
		live.Step()
		checkIncrementalOwners(t, live)
	}
	if live.completed != 2 || !seen[phaseUnloadingIntermediate] {
		t.Fatalf("shared Group phases missing: completed=%d, phases=%v", live.completed, seen)
	}
	if live.boarded != 2 || live.requestID != 2 || live.unaccountedOrders != 0 {
		t.Fatal("shared Group order conservation failed")
	}
}

func TestLargeRestoreInvalidGeometry(t *testing.T) {
	t.Parallel()
	network := largeRestoreNetwork()
	berth, _ := network.Node("harbor-1")
	for i := range network.Nodes {
		if network.Nodes[i].ID == "harbor-exit" {
			network.Nodes[i].Position = Point{X: berth.Position.X + 30, Y: berth.Position.Y}
		}
	}
	fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
	f := newRestoreFleetFixture(t, largeRestoreNetwork(), fleet)
	for _, logical := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical", true: "logical"}[logical], func(t *testing.T) {
			t.Parallel()
			s, _, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: f.state(), LogicalOnly: logical})
			if err == nil || s != nil {
				t.Fatal("restore accepted a 30 m large-admitting lane")
			}
		})
	}
	ordinary := network.clone()
	for i := range ordinary.Lanes {
		ordinary.Lanes[i].VehicleClasses = 0
	}
	for i := range ordinary.Stations {
		ordinary.Stations[i].VehicleClasses = 0
		for j := range ordinary.Stations[i].Berths {
			ordinary.Stations[i].Berths[j].VehicleClasses = 0
		}
	}
	if _, err := NewFleet(ordinary, fleet); err != nil {
		t.Fatalf("ordinary 30 m lane rejected: %v", err)
	}
}

// Future track grants depend on speed, which ordinary saves do not retain.
// The current footprint and each retained berth, node, and junction must survive.
func checkLargeColdRetainedOwners(t *testing.T, live, cold *Simulation) {
	t.Helper()
	for _, v := range live.vehicles {
		required := make(map[resource]bool)
		if v.Pod.Activity == Traveling {
			for _, b := range v.blocks.span(0, v.blockIndex+1) {
				for _, held := range b.resources {
					if resourceReleaseDistance(b, held) > v.distance {
						required[held] = true
					}
				}
			}
		}
		for held, owner := range live.owners {
			if owner == v.Pod.ID && (required[held] || held.kind != trackResource) && cold.owners[held] != owner {
				t.Fatalf("cold restore lost retained resource %+v of %s at tick %d", held, owner, live.tick)
			}
		}
	}
}

func TestLargeRestoreGroupContinuingAuthored(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, largeRestoreNetwork(), []Placement{{ID: "01", Class: GroupClass, StationID: "harbor", BerthID: "harbor-1"}})
	pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "garden-1", to: "market-1", lane: "garden-1-out", distance: 0}), 2, 1)
	pod.Class, pod.Activity = GroupClass, "continuing"
	pod.StationID, pod.BerthID, pod.JourneyOrigin = "garden", "garden-1", "harbor-1"
	pod.LaneID, pod.LaneDistance, pod.Distance, pod.RiddenMeters = "", 0, 0, 700
	pod.Riders[0].From, pod.Riders[0].PartySize = "harbor", 4
	pod.Riders = append([]SavedRequest{{ID: 1, From: "harbor", To: "garden", PartySize: 4, PodID: "01", RequestedTick: 10, BoardedTick: 20, Completed: true, SharingConsent: SharedConsent, Service: OnDemandService}}, pod.Riders...)
	state := f.state(pod)
	state.RequestID, state.Boarded, state.Completed = 2, 2, 1
	s, result, err := f.restore(roundTripState(t, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("authored Continuing restore: %+v, %v", result, err)
	}
	v := findVehicle(t, s, "01")
	if v.Pod.Activity != Continuing || v.riddenBase != 700 || v.Riders[0].ID != 1 || v.Riders[1].ID != 2 || v.PassengersAboard() != 4 {
		t.Fatal("authored Continuing restore changed accepted state")
	}
	checkIncrementalOwners(t, s)
	for steps := 0; s.completed < 2 && steps < 30000; steps++ {
		s.Step()
		checkIncrementalOwners(t, s)
	}
	if s.completed != 2 || s.requestID != 2 || s.boarded != 2 || s.unaccountedOrders != 0 {
		t.Fatal("authored Continuing lost order conservation")
	}
}
