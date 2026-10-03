package sim

import (
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestOnboardAdversarialOwnOriginDetour(t *testing.T) {
	t.Parallel()
	s := newOccupiedPickupRide(t)
	v := &s.vehicles[0]
	// This authored completion isolates a later-origin rider from the old host.
	v.Riders[0].Completed = true
	market, _ := s.station("market")
	berth := market.Berths[0]
	index := slices.IndexFunc(v.Riders, func(rider Request) bool { return !rider.Completed })
	if index != 2 || v.Boardings[index].MetersAtBoarding <= 0 || v.Riders[0].From == v.Riders[index].From {
		t.Fatalf("fixture lacks completed old host and active later origin: %+v", v.Vehicle)
	}
	own := s.directDistanceForClass(s.riderOrigin(v, index), market.ID, berth, v.Pod.Class)
	old := s.directDistanceForClass(v.journeyOrigin.Node, market.ID, berth, v.Pod.Class)
	if !finite(own) || own <= 0 || old <= own {
		t.Fatalf("fixture lacks distinct direct distances: own %g old %g", own, old)
	}
	for _, test := range []struct {
		name  string
		ratio float64
		keep  bool
	}{{"within own cap", 1.4, true}, {"old denominator masks breach", 1.6, false}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			local := s.Clone()
			v := &local.vehicles[0]
			arrival := v.Boardings[index].MetersAtBoarding + test.ratio*own
			wrong := (arrival - v.Boardings[index].MetersAtBoarding) / old
			if wrong > maxSharedRideDetour {
				t.Fatalf("old denominator does not mask this case: %g", wrong)
			}
			start := detourStart{class: v.Pod.Class, from: v.origin.Node, berth: berth, ridden: arrival}
			if keep := local.keepsRiderDetours(v, []string{market.ID}, start); keep != test.keep {
				t.Fatalf("production wrapper kept %t, want %t: own ratio %g, old ratio %g", keep, test.keep, test.ratio, wrong)
			}
		})
	}
}

func TestOnboardAdversarialIntermediateRestore(t *testing.T) {
	t.Parallel()
	live := newOccupiedPickupRide(t)
	monitorReassign(t, live)
	if err := submitSharedTrip(live, "garden", "harbor"); err != nil {
		t.Fatal(err)
	}
	v := &live.vehicles[0]
	stepUntil(t, live, "recorded intermediate market unloading", func() bool {
		return v.Pod.Activity == Unloading && v.Pod.StationID == "market" && len(v.Stops) == 1 && v.Stops[0] == "harbor"
	})
	if v.phaseTicks <= 0 || v.RidersAboard() != 3 || len(v.Boardings) != 4 || live.completed != 1 {
		t.Fatalf("fixture missed intermediate alighting: %+v", live.Snapshot())
	}
	cumulative := v.riddenMeters()
	saved := roundTripState(t, live.ExportState())
	pod := saved.Pods[0]
	if pod.Activity != "unloading" || pod.RiddenMeters != cumulative || pod.Distance != 0 || len(pod.Boardings) != 4 || pod.JourneyOrigin != "" {
		t.Fatalf("intermediate export lost full passenger distance: %+v, C %g", pod, cumulative)
	}
	fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
	restored, result, err := RestoreState(RestoreStateInput{Network: live.network, Fleet: fleet, State: saved, BoardingRecords: true, OnboardPickups: true})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("intermediate restore: %+v %v", result, err)
	}
	monitorReassign(t, restored)
	restored.SetExperimentRecords(true)
	checkRestoredMatches(t, live, restored)
	baselines := slices.Clone(v.Boardings)
	restoredBase := restored.vehicles[0].riddenBase
	if restoredBase != cumulative {
		t.Fatalf("restored stationary base %g, want %g", restoredBase, cumulative)
	}
	for range 600 * TicksPerSecond {
		live.Step()
		restored.Step()
		if live.completed == 4 && restored.completed == 4 {
			break
		}
	}
	if live.completed != 4 || restored.completed != 4 || countUnaccounted(t, live) != 0 || countUnaccounted(t, restored) != 0 ||
		!slices.Equal(baselines, restored.vehicles[0].Boardings) || !slices.Equal(baselines, v.Boardings) ||
		math.Abs(live.riderDistanceMeters-restored.riderDistanceMeters) > 1e-8 || math.Abs(live.directDistanceMeters-restored.directDistanceMeters) > 1e-8 ||
		live.totalJourneyTicks != restored.totalJourneyTicks || live.maxJourneyTicks != restored.maxJourneyTicks {
		t.Fatalf("continued intermediate restore changed completion or conservation: live %+v restored %+v", live.Snapshot(), restored.Snapshot())
	}
	for _, requestID := range []int{1, 3, 4} {
		original := slices.IndexFunc(live.requestCompletions, func(done requestCompletion) bool { return done.requestID == requestID })
		cold := slices.IndexFunc(restored.requestCompletions, func(done requestCompletion) bool { return done.requestID == requestID })
		if original < 0 || cold < 0 {
			t.Fatalf("request %d lacks an actual post-restore completion", requestID)
		}
		a, b := live.requestCompletions[original], restored.requestCompletions[cold]
		if a.tick != b.tick || math.Abs(a.riddenMeters-b.riddenMeters) > 1e-8 || math.Abs(a.directMeters-b.directMeters) > 1e-8 {
			t.Fatalf("request %d completion changed: live %+v restored %+v", requestID, a, b)
		}
	}
}

// authoredPrivateHistory fills the display array without claiming a reachable private chain.
func authoredPrivateHistory(t *testing.T) *Simulation {
	t.Helper()
	s := newOccupiedPickupRide(t)
	v := &s.vehicles[0]
	riders, records := slices.Clone(v.Riders), slices.Clone(v.Boardings)
	history := riders[1]
	history.ID, history.Completed, history.SharingConsent = 4, true, PrivateConsent
	v.Riders = []Request{history, riders[0], riders[1], riders[2]}
	v.Boardings = []RiderBoarding{records[1], records[0], records[1], records[2]}
	for index := range 4 {
		history.ID = 5 + index
		v.Riders = append(v.Riders, history)
		v.Boardings = append(v.Boardings, records[1])
	}
	s.requestID, s.boarded, s.completed = 8, 8, 6
	if err := s.CheckContract(); err != nil {
		t.Fatalf("authored private history is invalid: %v", err)
	}
	return s
}

func TestOnboardAdversarialPrivateHistoryRetirement(t *testing.T) {
	t.Parallel()
	for _, accept := range []bool{false, true} {
		name := "reject after retirement preview"
		if accept {
			name = "retire oldest completed private"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := authoredPrivateHistory(t)
			v := &s.vehicles[0]
			if !accept {
				v.riddenBase *= 10
			}
			before := s.Clone()
			retained := make(map[int]RiderBoarding)
			for index, rider := range v.Riders {
				retained[rider.ID] = v.Boardings[index]
			}
			if accept {
				if err := submitSharedTrip(s, "garden", "market"); err != nil {
					t.Fatal(err)
				}
				if len(v.Riders) != MaxSharedRideParties || len(v.Boardings) != len(v.Riders) || slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID == 4 }) || v.Riders[len(v.Riders)-1].ID != 9 || v.Boardings[len(v.Boardings)-1].MetersAtBoarding != v.riddenMeters() {
					t.Fatalf("retirement failed to remove only oldest completed party: %+v", v.Vehicle)
				}
				for index, rider := range v.Riders {
					if old, ok := retained[rider.ID]; ok && old != v.Boardings[index] {
						t.Fatalf("retained party %d changed baseline", rider.ID)
					}
				}
				last := s.requestBoardings[len(s.requestBoardings)-1]
				if last.RequestID != 9 || last.SharedWith != 1 || s.completed != before.completed || s.journeys != before.journeys || !reflect.DeepEqual(s.requestCompletions, before.requestCompletions) || countUnaccounted(t, s) != 0 {
					t.Fatalf("retirement changed host, completed counters, timings, or conservation: %+v", last)
				}
				if err := s.CheckContract(); err != nil {
					t.Fatal(err)
				}
			} else {
				trip := waitingTrip{request: requestFromOptions(TripOptions{From: "garden", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: OnDemandService}, 9, s.tick)}
				binding := trip
				if s.joinOnboardPickup(&trip) {
					t.Fatal("excessive existing detour admitted")
				}
				if !reflect.DeepEqual(s.ExportState(), before.ExportState()) || !reflect.DeepEqual(v.Riders, before.vehicles[0].Riders) || !slices.Equal(v.Boardings, before.vehicles[0].Boardings) || v.routeVersion != before.vehicles[0].routeVersion ||
					!reflect.DeepEqual(s.owners, before.owners) || !reflect.DeepEqual(trip, binding) || !reflect.DeepEqual(s.requestBoardings, before.requestBoardings) || !reflect.DeepEqual(s.requestCompletions, before.requestCompletions) {
					t.Fatal("rejected retirement preview changed arrays, route, ownership, binding, or metrics")
				}
			}
		})
	}
}

func TestOnboardAdversarialAssignedPickup(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		join   SharedRideJoin
		to     string
		accept bool
	}{
		{"default keeps assignment", DefaultSharedRideJoin, "market", false},
		{"reassign existing stop", SharedRideJoinReassignExisting, "market", true},
		{"reassign rejects new stop", SharedRideJoinReassignExisting, "harbor", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}, {ID: "02", StationID: "parking", BerthID: "parking-1"}}
			s := newScreenSimulation(t, Example(), fleet, 4, SharedRideDropOffs)
			if test.join != DefaultSharedRideJoin {
				if err := s.SetSharedRideJoin(test.join); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetOnboardPickups(true); err != nil {
				t.Fatal(err)
			}
			for _, to := range []string{"market", "garden"} {
				if err := submitSharedTrip(s, "harbor", to); err != nil {
					t.Fatal(err)
				}
			}
			host, own := s.findVehicle("01"), s.findVehicle("02")
			stepUntil(t, s, "host unloads at garden", func() bool { return host.Pod.Activity == Unloading && host.Pod.StationID == "garden" })
			if err := submitSharedTrip(s, "garden", test.to); err != nil {
				t.Fatal(err)
			}
			if len(s.waiting) != 1 || s.waiting[0].request.ID != 3 || s.waiting[0].request.PodID != "02" || !releasable(own) {
				t.Fatalf("real order has no releasable assigned pod: %+v", s.waiting)
			}
			beforeDenied := s.Clone()
			s.dispatch()
			if own.released || !reflect.DeepEqual(beforeDenied.ExportState(), s.ExportState()) || !reflect.DeepEqual(beforeDenied.owners, s.owners) || !reflect.DeepEqual(beforeDenied.requestBoardings, s.requestBoardings) {
				t.Fatal("dispatch released or changed an assigned pickup while the occupied host still unloaded")
			}
			stepUntil(t, s, "last garden unloading tick", func() bool { return host.Pod.Activity == Unloading && host.phaseTicks == 1 })
			route, destination, version := slices.Clone(own.Route), own.destination, own.routeVersion
			s.Step()
			if test.accept {
				if len(s.waiting) != 0 || host.Pod.Activity != Boarding || !host.Pod.Occupied || host.phaseTicks != boardingTicks || len(host.Boardings) != 3 || host.Riders[2].ID != 3 || host.Riders[2].PodID != "01" || host.Boardings[2].MetersAtBoarding != host.riddenMeters() || !own.released || s.assigned("02") || s.pass.assigned["02"] {
					t.Fatalf("assigned occupied admission did not release only its pickup: host %+v own %+v waiting %+v", host.Vehicle, own.Vehicle, s.waiting)
				}
				timing := s.requestBoardings[len(s.requestBoardings)-1]
				if timing.RequestID != 3 || !timing.Reassigned || timing.SharedWith != 1 || timing.BoardedTick != s.tick {
					t.Fatalf("reassignment timing: %+v", timing)
				}
			} else if len(s.waiting) != 1 || s.waiting[0].request.PodID != "02" || len(host.Riders) != 2 || len(host.Boardings) != 0 || own.released || !slices.Equal(own.Route, route) || own.destination != destination || own.routeVersion != version || len(s.requestBoardings) != 2 {
				t.Fatalf("rejected assigned pickup changed binding, release, or route: host %+v own %+v waiting %+v", host.Vehicle, own.Vehicle, s.waiting)
			}
			stepUntil(t, s, "all three real orders complete", func() bool { return s.completed == 3 })
			if countUnaccounted(t, s) != 0 || len(s.requestCompletions) != 3 || s.maxDetourRatio > maxSharedRideDetour+1e-9 {
				t.Fatalf("assigned run lost conservation or detour cap: %+v", s.Snapshot())
			}
		})
	}
}
