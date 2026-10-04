package sim

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestOnboardPickupsPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		limit int
		mode  SharedRideMode
		valid bool
	}{
		{"one party", 1, SharedRideDropOffs, false},
		{"destination sharing", 2, SharedRideDestination, false},
		{"drop-off sharing", 2, SharedRideDropOffs, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newTraffic(t)
			if s.onboardPickups {
				t.Fatal("new simulation enables occupied pickup")
			}
			if err := s.SetSharedRidePartyLimit(tc.limit); err != nil {
				t.Fatal(err)
			}
			if err := s.SetSharedRideMode(tc.mode, DefaultSharedRideMaxStops); err != nil {
				t.Fatal(err)
			}
			err := s.SetOnboardPickups(true)
			if (err == nil) != tc.valid || s.onboardPickups != tc.valid {
				t.Fatalf("enable policy: enabled %t, error %v", s.onboardPickups, err)
			}
		})
	}
}

func newOccupiedPickupRide(t *testing.T) *Simulation {
	t.Helper()
	return newOccupiedPickupRideOnNetwork(t, Example())
}

func newOccupiedPickupRideOnNetwork(t *testing.T, network Network) *Simulation {
	t.Helper()
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOnboardPickups(true); err != nil {
		t.Fatal(err)
	}
	s.SetExperimentRecords(true)
	for _, pair := range [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"garden", "market"}} {
		if err := submitSharedTrip(s, pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	for range 300 * TicksPerSecond {
		s.Step()
		v := &s.vehicles[0]
		if v.Pod.Activity == Boarding && v.Pod.Occupied {
			return s
		}
	}
	t.Fatalf("occupied pickup did not occur: %+v", s.Snapshot())
	return nil
}

func TestOnboardPickupsActualOriginsAndCompletion(t *testing.T) {
	t.Parallel()
	s := newOccupiedPickupRide(t)
	monitorContract(t, s)
	v := &s.vehicles[0]
	if len(s.waiting) != 0 || len(v.Riders) != 3 || len(v.Boardings) != 3 || !v.Riders[1].Completed || v.Riders[2].From != "garden" ||
		v.Boardings[0].BerthID != "harbor-1" || v.Boardings[0].MetersAtBoarding != 0 || v.Boardings[2].BerthID != "garden-1" ||
		v.Boardings[2].MetersAtBoarding != v.riddenMeters() || v.riddenMeters() <= 0 || v.phaseTicks != boardingTicks {
		t.Fatalf("occupied transaction: %+v, records %+v, C %g, clock %d", v.Riders, v.Boardings, v.riddenMeters(), v.phaseTicks)
	}
	baseline := v.Boardings[2].MetersAtBoarding
	advance(s, 300*TicksPerSecond)
	if s.completed != 3 || v.RidersAboard() != 0 || v.riddenBase <= baseline || s.maxDetourRatio > maxSharedRideDetour+1e-9 {
		t.Fatalf("completion: %+v", s.Snapshot())
	}
	completions := make(map[int]requestCompletion)
	for _, completed := range s.requestCompletions {
		completions[completed.requestID] = completed
	}
	first, picked := completions[1], completions[3]
	if math.Abs(first.riddenMeters-picked.riddenMeters-baseline) > 1e-9 || first.directMeters <= picked.directMeters || picked.riddenMeters <= 0 {
		t.Fatalf("per-rider distances: first %+v, pickup %+v, baseline %g", first, picked, baseline)
	}
	if got := s.requestBoardings[2]; got.SharedWith != 1 || got.RequestID != 3 {
		t.Fatalf("sharing host: %+v", got)
	}
}

func TestOnboardPickupsAdditionalJoinKeepsClock(t *testing.T) {
	t.Parallel()
	s := newOccupiedPickupRide(t)
	advance(s, 37)
	v := &s.vehicles[0]
	clock, baseline := v.phaseTicks, v.riddenMeters()
	if err := submitSharedTrip(s, "garden", "market"); err != nil {
		t.Fatal(err)
	}
	if v.phaseTicks != clock || len(v.Boardings) != 4 || v.Boardings[3].MetersAtBoarding != baseline || v.RidersAboard() != 3 {
		t.Fatalf("additional admission reset clock or baseline: clock %d, records %+v", v.phaseTicks, v.Boardings)
	}
	if err := s.SetOnboardPickups(false); err != nil {
		t.Fatal(err)
	}
	advance(s, 300*TicksPerSecond)
	if s.completed != 4 || s.maxDetourRatio > maxSharedRideDetour+1e-9 {
		t.Fatalf("policy-off accepted journey: %+v", s.Snapshot())
	}
}

func TestOnboardPickupsRejectAtomically(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*Simulation, *waitingTrip)
	}{
		{"policy off", func(s *Simulation, _ *waitingTrip) { s.onboardPickups = false }},
		{"new private", func(_ *Simulation, trip *waitingTrip) { trip.request.SharingConsent = PrivateConsent }},
		{"existing private", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].Riders[0].SharingConsent = PrivateConsent }},
		{"full seats", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].Riders[0].PartySize = 3 }},
		{"elapsed dwell", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].phaseTicks = 0 }},
		{"committed departure", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].reservedThrough = 0 }},
		{"destination berth", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].destination = Berth{ID: "market-1"} }},
		{"wrong berth owner", func(s *Simulation, _ *waitingTrip) {
			s.owners[resource{kind: berthResource, id: "garden-1"}] = podResourceOwner("other")
		}},
		{"wrong node owner", func(s *Simulation, _ *waitingTrip) {
			s.owners[resource{kind: nodeResource, id: "garden-berth"}] = podResourceOwner("other")
		}},
		{"moving", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].Pod.Speed = 0.1 }},
		{"incompatible actual berth", func(s *Simulation, _ *waitingTrip) {
			for index := range s.network.Stations {
				if s.network.Stations[index].ID == "garden" {
					s.network.Stations[index].Berths[0].VehicleClasses = classBit(string(CompactClass))
				}
			}
		}},
		{"existing detour", func(s *Simulation, _ *waitingTrip) { s.vehicles[0].riddenBase *= 10 }},
		{"predictive rejected detour", func(s *Simulation, _ *waitingTrip) {
			if err := s.SetRoutingPolicy(PredictiveRouting); err != nil {
				panic(err)
			}
			s.vehicles[0].riddenBase *= 10
		}},
		{"assigned new stop", func(_ *Simulation, trip *waitingTrip) { trip.request.PodID, trip.request.To = "other", "harbor" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newOccupiedPickupRide(t)
			trip := waitingTrip{request: requestFromOptions(TripOptions{From: "garden", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: OnDemandService}, 4, s.tick)}
			test.edit(s, &trip)
			before, binding := s.Clone(), trip
			if s.joinOnboardPickup(&trip) {
				t.Fatal("denied candidate admitted")
			}
			if !reflect.DeepEqual(before.ExportState(), s.ExportState()) || !reflect.DeepEqual(before.owners, s.owners) || !reflect.DeepEqual(binding, trip) ||
				!slices.Equal(before.vehicles[0].Boardings, s.vehicles[0].Boardings) || before.vehicles[0].routeVersion != s.vehicles[0].routeVersion ||
				!reflect.DeepEqual(before.requestBoardings, s.requestBoardings) ||
				!reflect.DeepEqual(before.predictiveQueues, s.predictiveQueues) || !reflect.DeepEqual(before.predictivePodQueues, s.predictivePodQueues) || before.predictiveQueueTick != s.predictiveQueueTick {
				t.Fatal("denied candidate changed route, riders, records, binding, owners, or boarding events")
			}
		})
	}
}

func TestOnboardPickupsNewRiderDetour(t *testing.T) {
	t.Parallel()
	network := Example()
	for index := range network.Nodes {
		if network.Nodes[index].ID == "market-berth" {
			network.Nodes[index].Position.Y = 900
		}
	}
	s := newOccupiedPickupRideOnNetwork(t, network)
	trip := waitingTrip{request: requestFromOptions(TripOptions{From: "garden", To: "harbor", PartySize: 1, SharingConsent: SharedConsent, Service: OnDemandService}, 4, s.tick)}
	v := &s.vehicles[0]
	route, err := s.stationApproachForStops(v.origin.Node, []string{"market", "harbor"}, v.Pod.Class)
	if err != nil {
		t.Fatal(err)
	}
	plan := riderDetour{origin: v.origin.Node, destination: "harbor", baseline: v.riddenMeters()}
	start := detourStart{class: v.Pod.Class, from: v.origin.Node, ridden: v.riddenMeters() + s.lanesMeters(route)}
	if got := s.plannedRiderDetour(plan, []string{"market", "harbor"}, start); got <= maxSharedRideDetour {
		t.Fatalf("fixture lacks an excessive new rider detour: %g", got)
	}
	before := s.ExportState()
	if s.joinOnboardPickup(&trip) || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("new rider detour admitted or changed accepted state")
	}
}

func TestOnboardPickupsActualPhaseRestores(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"boarding", "traveling", "unloading", "idle", "departing empty", "traveling empty"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			live := newOccupiedPickupRide(t)
			monitorContract(t, live)
			if phase != "boarding" {
				for range 300 * TicksPerSecond {
					live.Step()
					v := &live.vehicles[0]
					if phase == "traveling" && v.Pod.Activity == Traveling && v.distance > 50 ||
						phase == "unloading" && v.Pod.Activity == Unloading ||
						(phase == "idle" || phase == "departing empty" || phase == "traveling empty") && v.Pod.Activity == Idle {
						break
					}
				}
			}
			if phase == "departing empty" || phase == "traveling empty" {
				harbor, _ := live.station("harbor")
				if err := live.startEmptyMove(&live.vehicles[0], emptyDestination{station: harbor.ID, berth: harbor.Berths[0], reserveBerth: true}); err != nil {
					t.Fatal(err)
				}
				if phase == "traveling empty" {
					for range 300 * TicksPerSecond {
						live.Step()
						if v := &live.vehicles[0]; v.Pod.Activity == Traveling && v.distance > 50 {
							break
						}
					}
				}
			}
			saved := roundTripState(t, live.ExportState())
			if len(saved.Pods[0].Boardings) != 3 || saved.Pods[0].Boardings[2].MetersAtBoarding <= 0 {
				t.Fatal("actual exporter omitted positive baseline records")
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: live.network, Fleet: []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}, State: saved, BoardingRecords: true})
			if err != nil || !cleanRestore(result) {
				t.Fatalf("actual phase restore: %+v %v", result, err)
			}
			checkRestoredMatches(t, live, restored)
			monitorContract(t, restored)
			baseline := live.vehicles[0].Boardings[2].MetersAtBoarding
			advance(live, 300*TicksPerSecond)
			advance(restored, 300*TicksPerSecond)
			if live.completed != 3 || restored.completed != 3 || math.Abs(live.riderDistanceMeters-restored.riderDistanceMeters) > 1e-8 ||
				math.Abs(live.directDistanceMeters-restored.directDistanceMeters) > 1e-8 || math.Abs(live.vehicles[0].riddenMeters()-restored.vehicles[0].riddenMeters()) > 1e-8 ||
				restored.vehicles[0].Boardings[2].MetersAtBoarding != baseline || countUnaccounted(t, restored) != 0 {
				t.Fatalf("continued distance or conservation changed: live %+v restored %+v", live.Snapshot(), restored.Snapshot())
			}
		})
	}
}

func TestOnboardPickupsRepeatedHistoryRetirement(t *testing.T) {
	t.Parallel()
	s := newOccupiedPickupRide(t)
	monitorContract(t, s)
	if err := submitSharedTrip(s, "garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	nextDestination := map[string]string{"market": "garden", "harbor": "market", "garden": "harbor"}
	for range 12 {
		v := &s.vehicles[0]
		pickup := v.Stops[0]
		oldRecords := make(map[int]RiderBoarding)
		for index, rider := range v.Riders {
			oldRecords[rider.ID] = v.Boardings[index]
		}
		if err := submitSharedTrip(s, pickup, nextDestination[pickup]); err != nil {
			t.Fatal(err)
		}
		joined := false
		for range 600 * TicksPerSecond {
			s.Step()
			if v.Pod.Activity == Boarding && v.Pod.Occupied && v.Pod.StationID == pickup {
				joined = true
				break
			}
		}
		if !joined || len(v.Riders) != min(len(oldRecords)+1, MaxSharedRideParties) || len(v.Boardings) != len(v.Riders) {
			t.Fatalf("repeated pickup failed at %s: %+v", pickup, s.Snapshot())
		}
		for index, rider := range v.Riders {
			if old, retained := oldRecords[rider.ID]; retained && v.Boardings[index] != old {
				t.Fatal("history retirement changed a retained origin or baseline")
			}
		}
		if v.Boardings[len(v.Boardings)-1].MetersAtBoarding != v.riddenMeters() || countUnaccounted(t, s) != 0 {
			t.Fatal("repeated pickup lost a baseline or an order")
		}
		if len(v.Riders) == MaxSharedRideParties {
			oldest := s.requestID - MaxSharedRideParties + 1
			if v.Riders[0].ID != oldest {
				t.Fatalf("history did not retire oldest first: oldest %d riders %+v", oldest, v.Riders)
			}
		}
	}
	v := &s.vehicles[0]
	if v.Boardings[0].MetersAtBoarding <= 0 {
		t.Fatal("history retirement rebased a positive baseline")
	}
	advance(s, 600*TicksPerSecond)
	if s.completed != s.requestID || len(s.requestCompletions) != s.requestID || s.maxDetourRatio > maxSharedRideDetour+1e-9 || len(v.Riders) > MaxSharedRideParties || countUnaccounted(t, s) != 0 {
		t.Fatalf("repeated pickup completion: %+v", s.Snapshot())
	}
}

func TestOnboardPickupsCompletedHistoryAllowsOrdinaryCostedBoarding(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(loopNetwork(1260), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetCongestionRouting(true)
	s.ensureNetworkIndexes()
	costs := make([]float64, len(s.network.Lanes))
	costs[s.graph.lanes["approach-branch"]] = 1e6
	s.congestionRouteCosts, s.nextCongestionRouteRefresh = costs, math.MaxInt64
	s.congestionRoutes = make(map[routeKey]routeResult)
	v := &s.vehicles[0]
	// This authored idle history isolates route selection for the next private trip.
	v.Riders = []Request{{ID: 1, From: "garden", To: "harbor", PartySize: 1, SharingConsent: SharedConsent, Service: OnDemandService, PodID: "01", Completed: true}}
	v.Boardings, v.riddenBase = []RiderBoarding{{BerthID: "garden-1", MetersAtBoarding: 100}}, 300
	plain := s.Clone()
	plain.vehicles[0].Boardings = nil
	trip := waitingTrip{request: requestFromOptions(TripOptions{From: "harbor", To: "garden", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}, 2, s.tick)}
	if err := s.board(v, trip); err != nil {
		t.Fatal(err)
	}
	if err := plain.board(&plain.vehicles[0], trip); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(v.Route, func(lane Lane) bool { return lane.ID == "loop-out" }) || !reflect.DeepEqual(s.ExportState(), plain.ExportState()) || len(v.Boardings) != 0 || v.riddenBase != 0 {
		t.Fatal("completed records changed the next private route or retained old distance")
	}
}

func TestOnboardPickupsFinalDwellTickRefusesJoin(t *testing.T) {
	t.Parallel()
	s := newOccupiedPickupRide(t)
	advance(s, boardingTicks-1)
	v := &s.vehicles[0]
	if v.phaseTicks != 1 {
		t.Fatal("fixture missed the last boarding tick")
	}
	// The waiting order arrives for dispatch after Step consumes the last tick.
	s.requestID++
	s.waiting = append(s.waiting, waitingTrip{request: requestFromOptions(TripOptions{From: "garden", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: OnDemandService}, s.requestID, s.tick)})
	s.Step()
	if len(v.Boardings) != 3 || len(v.Riders) != 3 || len(s.waiting) != 1 || v.Pod.Activity == Boarding {
		t.Fatal("a new party joined after the boarding clock expired")
	}
}

func TestOnboardPickupsDisablePreservesAcceptedInterval(t *testing.T) {
	t.Parallel()
	s := recordedMetricFixture(t)
	v := &s.vehicles[0]
	v.Pod.Activity, v.Pod.Occupied, v.phaseTicks = Boarding, true, 37
	s.onboardPickups = true
	want := s.ExportState()
	if err := s.SetOnboardPickups(false); err != nil {
		t.Fatal(err)
	}
	if s.onboardPickups || !reflect.DeepEqual(want, s.ExportState()) || v.phaseTicks != 37 {
		t.Fatal("policy disable changed accepted riders, records, route, or dwell")
	}
}
