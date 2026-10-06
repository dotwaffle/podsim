package sim

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

func checkExpressJourneyColdRestore(t *testing.T, live *Simulation, network Network, fleet []Placement) {
	t.Helper()
	saved := roundTripState(t, live.ExportState())
	expressEvidence(t, "saved", saved)
	services := make([]ExpressService, 0, len(live.expressServices))
	for _, service := range live.expressServices {
		services = append(services, service)
	}
	cold, result, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: saved, ExpressServices: services, OnboardPickups: live.onboardPickups})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("tick %d cold restore: %+v %v", live.tick, result, err)
	}
	checkRestoredMatches(t, live, cold)
	checkLargeColdRetainedOwners(t, live, cold)
	expressEvidence(t, "restored", cold.ExportState())
	for i := range cold.vehicles {
		if cold.vehicles[i].Pod.Speed != 0 {
			t.Fatal("ordinary restore retained speed")
		}
	}
	warm := live.Clone()
	for _, s := range []*Simulation{cold, warm} {
		s.SetStationBuffers(false)
		if err := s.SetPlatooning(PlatooningOff); err != nil {
			t.Fatal(err)
		}
		if err := s.SetOnboardPickups(false); err != nil {
			t.Fatal(err)
		}
		for s.completed < saved.RequestID && s.tick < 600*60 {
			before := largeMotionPods(s)
			s.Step()
			checkExpressMotionTick(t, s, before)
		}
		if s.completed != saved.RequestID || s.unaccountedOrders != 0 || len(s.waiting) != 0 {
			t.Fatalf("accepted continuation censored at original cap: %+v", s.ExportState())
		}
	}
	if cold.journeys != warm.journeys || math.Abs(cold.passengerDistanceMeters-warm.passengerDistanceMeters) > restoreTolerance || math.Abs(cold.riderDistanceMeters-warm.riderDistanceMeters) > restoreTolerance || math.Abs(cold.directDistanceMeters-warm.directDistanceMeters) > restoreTolerance {
		t.Fatal("ordinary restore changed accepted distance totals")
	}
	expressEvidence(t, "continued", cold.ExportState())
}

func TestExpressRestoreProductionPhases(t *testing.T) {
	t.Parallel()
	skipLong(t)
	network := largeRestoreNetwork()
	fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
	live, err := expressPhysicalFleet(network, fleet)
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
				checkExpressJourneyColdRestore(t, checkpoint, network, fleet)
			})
		}
	}
	capture("initial idle")
	if err := live.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
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
		before := largeMotionPods(live)
		live.Step()
		checkExpressMotionTick(t, live, before)
	}
	if live.completed != 1 {
		t.Fatal("first Group journey did not finish")
	}
	capture("completed idle")
	if _, err := live.SubmitTripOptions(TripOptions{From: "harbor", To: "garden", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
	for steps := 0; live.completed < 2 && steps < 30000; steps++ {
		v := findVehicle(t, live, "01")
		if !v.Pod.Occupied {
			capture("empty " + activityCode(v.Pod.Activity))
		}
		before := largeMotionPods(live)
		live.Step()
		checkExpressMotionTick(t, live, before)
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

func TestExpressRestoreIntermediateProduction(t *testing.T) {
	t.Parallel()
	network := largeRestoreNetwork()
	fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
	live, err := expressPhysicalFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := live.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := live.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	if err := live.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 10, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if _, err := live.SubmitTripOptions(TripOptions{From: "harbor", To: "garden", PartySize: 10, SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
	if v := findVehicle(t, live, "01"); len(v.Riders) != 2 || v.PassengersAboard() != 20 || !slices.Equal(v.Stops, []string{"garden", "market"}) {
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
				checkExpressJourneyColdRestore(t, checkpoint, network, fleet)
			})
		}
		before := largeMotionPods(live)
		live.Step()
		checkExpressMotionTick(t, live, before)
	}
	if live.completed != 2 || !seen[phaseUnloadingIntermediate] {
		t.Fatalf("shared Group phases missing: completed=%d, phases=%v", live.completed, seen)
	}
	if live.boarded != 2 || live.requestID != 2 || live.unaccountedOrders != 0 {
		t.Fatal("shared Group order conservation failed")
	}
}

func TestExpressRestoreContinuingAuthored(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, largeRestoreNetwork(), []Placement{{ID: "01", Class: GroupClass, StationID: "harbor", BerthID: "harbor-1"}})
	pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "garden-1", to: "market-1", lane: "garden-1-out", distance: 0}), 2, 1)
	pod.Class, pod.Activity = ExpressClass, "continuing"
	pod.StationID, pod.BerthID, pod.JourneyOrigin = "garden", "garden-1", "harbor-1"
	pod.LaneID, pod.LaneDistance, pod.Distance, pod.RiddenMeters = "", 0, 0, 700
	pod.Riders[0].From, pod.Riders[0].PartySize = "harbor", 10
	pod.Riders = append([]SavedRequest{{ID: 1, From: "harbor", To: "garden", PartySize: 10, PodID: "01", RequestedTick: 10, BoardedTick: 20, Completed: true, SharingConsent: SharedConsent, Service: OnDemandService}}, pod.Riders...)
	state := f.state(pod)
	state.RequestID, state.Boarded, state.Completed = 2, 2, 1
	state.OrderContract = ExpressOrderContract
	f.network = expressNetwork(f.network)
	f.fleet[0].Class = ExpressClass
	s, result, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: f.network, Fleet: f.fleet, State: roundTripState(t, state)})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("authored Continuing restore: %+v, %v", result, err)
	}
	v := findVehicle(t, s, "01")
	if v.Pod.Activity != Continuing || v.riddenBase != 700 || v.Riders[0].ID != 1 || v.Riders[1].ID != 2 || v.PassengersAboard() != 10 {
		t.Fatal("authored Continuing restore changed accepted state")
	}
	expressEvidence(t, "continuing", s.ExportState())
	checkIncrementalOwners(t, s)
	for steps := 0; s.completed < 2 && steps < 30000; steps++ {
		before := largeMotionPods(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
	}
	if s.completed != 2 || s.requestID != 2 || s.boarded != 2 || s.unaccountedOrders != 0 {
		t.Fatal("authored Continuing lost order conservation")
	}
}

func TestExpressRestoreLinksRejectBeforeTiers(t *testing.T) {
	for _, classes := range [][2]VehicleClass{{ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}} {
		for _, kind := range []string{"virtual", "buffer-v1", "compact-buffer-v1", "compact-head"} {
			for _, logical := range []bool{false, true} {
				t.Run(string(classes[0])+"-"+string(classes[1])+"/"+kind+map[bool]string{false: "/physical", true: "/logical"}[logical], func(t *testing.T) {
					n := expressNetwork(largeMotionNetwork(false))
					fleet := []Placement{{ID: "01", Class: classes[0], StationID: "a", BerthID: "a-1"}, {ID: "02", Class: classes[1], StationID: "a", BerthID: "a-2"}}
					s, err := NewFleetWithOrderContract(n, fleet, ExpressOrderContract)
					if err != nil {
						t.Fatal(err)
					}
					state := s.ExportState()
					if kind == "compact-head" {
						state.Pods[0].CompactQueue = &SavedCompactQueue{Members: []string{"02"}}
					} else {
						nativeKind := kind
						if kind == "virtual" {
							nativeKind = ""
						}
						if kind == "buffer-v1" {
							nativeKind = "buffer"
						}
						state.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01", Kind: nativeKind}
					}
					input := RestoreStateInput{OrderContract: ExpressOrderContract, Network: n, Fleet: fleet, State: state, LogicalOnly: logical}
					if _, _, err := restoreState(input, func() (*Simulation, error) { t.Fatal("large link reached restore tier"); return nil, nil }); err == nil {
						t.Fatal("large link accepted")
					}
				})
			}
		}
	}
}

func TestExpressMixedPartyWholeCapacity(t *testing.T) {
	s, _, _ := expressTestFleet(t)
	if err := s.SetSharedRidePartyLimit(3); err != nil {
		t.Fatal(err)
	}
	for i, party := range []int{12, 9, 8} {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: party, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(i, err)
		}
	}
	v := &s.vehicles[0]
	if len(v.Riders) != 2 || v.PassengersAboard() != 20 || len(s.waiting) != 1 || s.waiting[0].request.PartySize != 9 {
		t.Fatal("whole-party mixed capacity split or overfilled", s.ExportState())
	}
	if v.Riders[0].PartySize != 12 || v.Riders[1].PartySize != 8 {
		t.Fatal("party facts changed")
	}
	before := s.ExportState()
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 21, SharingConsent: SharedConsent}); err == nil {
		t.Fatal("party 21 accepted")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("rejection changed facts")
	}
	for s.completed < 3 && s.tick < 600*60 {
		before := largeMotionPods(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
	}
	if s.completed != 3 || s.boarded != 3 || s.requestID != 3 {
		t.Fatal("mixed whole parties censored")
	}
	expressEvidence(t, "completed", s.ExportState())
}
