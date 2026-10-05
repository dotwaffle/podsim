package sim

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
)

// newPickupFleet returns a simulation on the example network with pods 01,
// 02, and so on at the stations.
func newPickupFleet(t *testing.T, stations ...string) *Simulation {
	t.Helper()
	placements := make([]Placement, len(stations))
	for i, station := range stations {
		placements[i] = Placement{ID: fmt.Sprintf("%02d", i+1), StationID: station}
	}
	s, err := NewFleet(Example(), placements)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// newTrip returns a new order of one party from one station to another.
func newTrip(s *Simulation, from, to string) waitingTrip {
	s.requestID++
	return waitingTrip{request: Request{
		SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID, From: from, To: to, PartySize: 1, RequestedTick: s.tick,
	}}
}

// unloadFor makes pod v unload a passenger at its station for the given
// ticks. A trip from the station can wait for v to finish.
func unloadFor(v *vehicle, ticks int) {
	v.Pod.Activity, v.Pod.Occupied, v.phaseTicks = Unloading, true, ticks
}

// checkExclusions checks the exclusion invariants of the incident
// contract, section 5.2. X1: a trip with an exclusion never boarded, and
// neither its pod nor its hold is the excluded pod. X2: the excluded pod is
// a pod of the fleet.
func checkExclusions(s *Simulation) error {
	for _, trip := range s.waiting {
		if trip.excludedPod == "" {
			continue
		}
		if trip.request.PodID == trip.excludedPod || trip.deferPodID == trip.excludedPod || trip.boarded {
			return fmt.Errorf("X1: trip %d excludes pod %s, but has pod %q, hold %q, boarded %t",
				trip.request.ID, trip.excludedPod, trip.request.PodID, trip.deferPodID, trip.boarded)
		}
		if s.findVehicle(trip.excludedPod) == nil {
			return fmt.Errorf("X2: trip %d excludes pod %s, which is not in the fleet", trip.request.ID, trip.excludedPod)
		}
	}
	return nil
}

// TestReleasePickupsPendingPickups checks the release of each kind of
// pending pickup (incident contract, section 5.5). Each never-boarded trip
// of the withdrawn pod keeps its identity, request time, hold deadline, and
// queue index, loses its pod and hold, and excludes the pod. No owner
// changes, and the other trips do not change.
func TestReleasePickupsPendingPickups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// build returns the pod to withdraw and the queue indexes of its
		// pending pickups.
		build func(t *testing.T) (*Simulation, *vehicle, []int)
		// released is the released flag of the pod after the withdrawal.
		released bool
	}{
		{
			// Pods 01 and 02 both go to Market for two trips. Pod 02 is
			// empty on its pickup route.
			name: "empty pod on its pickup route",
			build: func(t *testing.T) (*Simulation, *vehicle, []int) {
				t.Helper()
				s := newTraffic(t)
				for range 2 {
					if err := s.RequestTrip("market", "harbor"); err != nil {
						t.Fatal(err)
					}
				}
				advance(s, 2*TicksPerSecond)
				v := s.findVehicle(s.waiting[1].request.PodID)
				if v == nil || v.Pod.Activity != Traveling || v.RelocatingTo != "market" || s.waiting[0].request.PodID == v.Pod.ID {
					t.Fatalf("no pod travels to the pickup of the second trip: %+v", s.waiting)
				}
				return s, v, []int{1}
			},
			released: true,
		},
		{
			// Pod 01 is idle at Harbor and bound to the second trip.
			name: "idle pod bound at the pickup station",
			build: func(t *testing.T) (*Simulation, *vehicle, []int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "garden")
				bound := newTrip(s, "harbor", "market")
				bound.request.PodID = "01"
				bound.request.DispatchReason = "Waiting for destination access"
				s.waiting = []waitingTrip{newTrip(s, "garden", "market"), bound, newTrip(s, "market", "garden")}
				v := s.findVehicle("01")
				return s, v, []int{1}
			},
		},
		{
			// Pod 02 unloads at Market, and three trips from Market wait
			// for it. One hold has no deadline yet.
			name: "pod named by three active holds",
			build: func(t *testing.T) (*Simulation, *vehicle, []int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "market")
				advance(s, TicksPerSecond)
				v := s.findVehicle("02")
				unloadFor(v, TicksPerSecond)
				s.waiting = []waitingTrip{newTrip(s, "harbor", "garden")}
				for i, until := range []int64{s.tick + maxDispatchDeferral, 0, s.tick + 1} {
					trip := newTrip(s, "market", "harbor")
					trip.request.RequestedTick = int64(i)
					trip.deferUntil, trip.deferCheck, trip.deferPodID = until, s.tick+TicksPerSecond, "02"
					trip.request.DispatchReason = "Waiting for pod 02 to finish"
					s.waiting = append(s.waiting, trip)
				}
				return s, v, []int{1, 2, 3}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v, pickups := test.build(t)
			before := slices.Clone(s.waiting)
			owners := maps.Clone(s.owners)
			motion := motionOf(s, v)
			probe := s.Clone()
			if n := probe.releasePickups(probe.findVehicle(v.Pod.ID)); n != len(pickups) {
				t.Fatalf("releasePickups released %d trips, want %d", n, len(pickups))
			}
			if err := s.withdrawService(v, faultHold); err != nil {
				t.Fatal(err)
			}
			if len(s.waiting) != len(before) {
				t.Fatalf("the queue has %d trips, want %d", len(s.waiting), len(before))
			}
			for i, trip := range s.waiting {
				old := before[i]
				if !slices.Contains(pickups, i) {
					if !reflect.DeepEqual(trip, old) {
						t.Errorf("trip %d changed\nbefore %+v\nafter  %+v", i, old, trip)
					}
					continue
				}
				if trip.request.ID != old.request.ID || trip.request.RequestedTick != old.request.RequestedTick || trip.deferUntil != old.deferUntil ||
					trip.request.From != old.request.From || trip.request.To != old.request.To || trip.boarded {
					t.Errorf("trip %d lost its identity\nbefore %+v\nafter  %+v", i, old, trip)
				}
				if trip.request.PodID != "" || trip.request.DispatchReason != "" || trip.route != nil || trip.destination != (Berth{}) ||
					trip.deferCheck != 0 || trip.deferPodID != "" || trip.excludedPod != v.Pod.ID {
					t.Errorf("trip %d is not released from pod %s: %+v", i, v.Pod.ID, trip)
				}
			}
			if !maps.Equal(s.owners, owners) {
				t.Error("the release changed a resource owner")
			}
			after := motionOf(s, v)
			if after.released != test.released {
				t.Errorf("pod %s released %t, want %t", v.Pod.ID, after.released, test.released)
			}
			after.withdrawn, after.released = motion.withdrawn, motion.released
			if !reflect.DeepEqual(after, motion) {
				t.Errorf("the release moved pod %s\nbefore %+v\nafter  %+v", v.Pod.ID, motion, after)
			}
			if err := checkExclusions(s); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestReleasePickupsStaleDeferral checks the trips that name the pod only
// in stale deferral metadata: a trip bound to another pod, and a trip
// whose hold is past its deadline. The release clears the metadata, and
// the trips keep their pod, route, berth, reason, deadline, and exclusion.
// The trip bound to another pod keeps its earlier exclusion of pod 01.
func TestReleasePickupsStaleDeferral(t *testing.T) {
	t.Parallel()
	s := newPickupFleet(t, "harbor", "market", "garden")
	advance(s, TicksPerSecond)
	bound := newTrip(s, "garden", "harbor")
	bound.request.PodID, bound.request.DispatchReason = "03", "Waiting for destination access"
	bound.route = []Lane{{ID: "garden-out"}}
	bound.destination = Berth{ID: "harbor-1", Node: "harbor-berth"}
	bound.deferUntil, bound.deferCheck, bound.deferPodID = s.tick+maxDispatchDeferral, s.tick+TicksPerSecond, "02"
	bound.excludedPod = "01"
	expired := newTrip(s, "market", "garden")
	expired.request.DispatchReason = "Waiting for an available pod"
	expired.deferUntil, expired.deferCheck, expired.deferPodID = s.tick, s.tick, "02"
	s.waiting = []waitingTrip{bound, expired}
	before := slices.Clone(s.waiting)
	pods := slices.Clone(s.vehicles)
	if n := s.releasePickups(s.findVehicle("02")); n != 0 {
		t.Fatalf("releasePickups released %d trips, want 0", n)
	}
	for i := range s.waiting {
		want := before[i]
		want.deferCheck, want.deferPodID = 0, ""
		if !reflect.DeepEqual(s.waiting[i], want) {
			t.Errorf("trip %d\ngot  %+v\nwant %+v", i, s.waiting[i], want)
		}
	}
	for i := range pods {
		if pods[i].released != s.vehicles[i].released {
			t.Errorf("pod %s released %t", pods[i].Pod.ID, s.vehicles[i].released)
		}
	}
}

// TestReleasePickupsContinuations checks that a trip that boarded before,
// such as a transferred party or a rider that a restore queued again,
// gets no exclusion (decision 1).
func TestReleasePickupsContinuations(t *testing.T) {
	t.Parallel()
	s := newPickupFleet(t, "harbor", "market")
	for range 2 {
		trip := newTrip(s, "harbor", "garden")
		trip.boarded, trip.request.BoardedTick, trip.request.PodID = true, 0, "01"
		s.waiting = append(s.waiting, trip)
	}
	held := newTrip(s, "market", "garden")
	held.boarded, held.deferUntil, held.deferCheck, held.deferPodID = true, maxDispatchDeferral, TicksPerSecond, "01"
	s.waiting = append(s.waiting, held)
	if err := s.withdrawService(s.findVehicle("01"), emergencyHold); err != nil {
		t.Fatal(err)
	}
	for i, trip := range s.waiting {
		if trip.request.PodID != "" || trip.deferPodID != "" || trip.excludedPod != "" || !trip.boarded {
			t.Errorf("trip %d: %+v", i, trip)
		}
	}
}

// TestSecondReleaseReplacesExclusion checks a trip released from pod 01
// that then waits for pod 02 to finish (incident contract, section 5.5).
// The hold names another pod, so it is allowed. The withdrawal of pod 02
// releases the trip again, and the exclusion names pod 02. The trip can
// then get pod 01, keeps the exclusion of pod 02 while pod 01 goes to the
// pickup, and never gets pod 02.
func TestSecondReleaseReplacesExclusion(t *testing.T) {
	t.Parallel()
	// Pod 01 is at Garden, pod 02 unloads at Market for 1 s, and pod 03 is
	// at Harbor. The trip from Market is bound to pod 01.
	s := newPickupFleet(t, "garden", "market", "harbor")
	a, b, c := s.findVehicle("01"), s.findVehicle("02"), s.findVehicle("03")
	unloadFor(b, TicksPerSecond)
	trip := newTrip(s, "market", "harbor")
	trip.request.PodID = "01"
	s.waiting = []waitingTrip{trip}
	if err := s.withdrawService(a, faultHold); err != nil {
		t.Fatal(err)
	}
	if got := s.waiting[0]; got.excludedPod != "01" || got.request.PodID != "" {
		t.Fatalf("after the first release: %+v", got)
	}
	if !s.waitForFinishingPod(&s.waiting[0], c, nil) || s.waiting[0].deferPodID != "02" || s.waiting[0].excludedPod != "01" {
		t.Fatalf("the trip does not wait for pod 02: %+v", s.waiting[0])
	}
	if err := checkExclusions(s); err != nil {
		t.Fatal(err)
	}
	if err := s.withdrawService(b, emergencyHold); err != nil {
		t.Fatal(err)
	}
	if got := s.waiting[0]; got.excludedPod != "02" || got.deferPodID != "" || got.deferCheck != 0 {
		t.Fatalf("after the second release: %+v", got)
	}
	if err := s.restoreService(a, faultHold); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreService(b, emergencyHold); err != nil {
		t.Fatal(err)
	}
	s.dispatch()
	if got := s.waiting[0]; got.request.PodID != "01" || got.excludedPod != "02" {
		t.Fatalf("after the restores, the trip has pod %q and excludes %q, want pod 01 and an exclusion of pod 02", got.request.PodID, got.excludedPod)
	}
	stepUntil(t, s, "the trip leaves the queue", func() bool {
		if len(s.waiting) > 0 && (s.waiting[0].request.PodID == "02" || s.waiting[0].excludedPod != "02") {
			t.Fatalf("tick %d: the trip %+v", s.tick, s.waiting[0])
		}
		return len(s.waiting) == 0
	})
	if !slices.ContainsFunc(a.Riders, func(rider Request) bool { return rider.ID == 1 }) {
		t.Fatalf("pod 01 does not carry the trip: %+v", a.Riders)
	}
}

// exclusionGateCase is one row of the exclusion gate table of the incident
// contract, section 5.3. build returns a state in which the path selects
// pod v for the trip at index trip when the trip has no exclusion. selects
// runs the path and reports whether it did so.
type exclusionGateCase struct {
	name    string
	build   func(t *testing.T) (s *Simulation, v *vehicle, trip int)
	selects func(t *testing.T, s *Simulation, v *vehicle, trip int) bool
}

// swapGives runs the periodic pickup swaps and reports whether the trip
// has pod v after them.
func swapGives(_ *testing.T, s *Simulation, v *vehicle, trip int) bool {
	s.SetPickupSwaps(true)
	s.swapPickups()
	return s.waiting[trip].request.PodID == v.Pod.ID
}

func exclusionGateCases() []exclusionGateCase {
	// waitAt returns pod 01 at Garden, pod 02 at Harbor, and a trip from
	// Market. Pod 01 is nearer to Market.
	waitAt := func(t *testing.T) (*Simulation, *vehicle, int) {
		t.Helper()
		s := newPickupFleet(t, "garden", "harbor")
		s.waiting = []waitingTrip{newTrip(s, "market", "harbor")}
		v := s.findVehicle("01")
		return s, v, 0
	}
	return []exclusionGateCase{
		{
			name: "remote selection", build: waitAt,
			selects: func(t *testing.T, s *Simulation, v *vehicle, trip int) bool {
				t.Helper()
				s.dispatch()
				got := s.waiting[trip]
				if got.request.PodID == "" {
					t.Fatalf("the trip has no pod: %+v", got)
				}
				return got.request.PodID == v.Pod.ID
			},
		},
		{
			name: "local selection",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newPickupFleet(t, "market", "harbor")
				s.waiting = []waitingTrip{newTrip(s, "market", "harbor")}
				v := s.findVehicle("01")
				return s, v, 0
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle, trip int) bool {
				return s.localPickupForRequest(s.waiting[trip].request, s.waiting[trip].excludedPod, &dispatchPass{assigned: map[string]bool{}}) == v
			},
		},
		{
			// Pod 01 goes to Market for the second trip. Pod 02 is ready
			// there for the third trip. The first trip has no pod.
			name: "promotion",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "market")
				if err := s.sendPickup(s.findVehicle("01"), "market"); err != nil {
					t.Fatal(err)
				}
				s.waiting = []waitingTrip{newTrip(s, "market", "harbor"), newTrip(s, "market", "harbor"), newTrip(s, "market", "harbor")}
				s.waiting[1].request.PodID, s.waiting[2].request.PodID = "01", "02"
				v := s.findVehicle("02")
				return s, v, 0
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle, trip int) bool {
				return s.promoteReadyPickup(trip) && s.waiting[trip].request.PodID == v.Pod.ID
			},
		},
		{
			// Pod 01 goes to Market for the first trip. Pod 02 is ready
			// there for the second trip, so promotion gives pod 01 to the
			// second trip.
			name: "promotion, later trip",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "market")
				if err := s.sendPickup(s.findVehicle("01"), "market"); err != nil {
					t.Fatal(err)
				}
				s.waiting = []waitingTrip{newTrip(s, "market", "harbor"), newTrip(s, "market", "harbor")}
				s.waiting[0].request.PodID, s.waiting[1].request.PodID = "01", "02"
				v := s.findVehicle("01")
				return s, v, 1
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle, trip int) bool {
				return s.promoteReadyPickup(0) && s.waiting[trip].request.PodID == v.Pod.ID
			},
		},
		{
			// A swap gives pod 02 to the first trip.
			name: "pickup swap, first trip",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := crossedPickupFixture(t)
				v := s.findVehicle("02")
				return s, v, 0
			},
			selects: swapGives,
		},
		{
			// A swap gives pod 01 to the second trip.
			name: "pickup swap, second trip",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := crossedPickupFixture(t)
				v := s.findVehicle("01")
				return s, v, 1
			},
			selects: swapGives,
		},
		{
			// A transfer gives the idle pod 02 to the trip of pod 01.
			name: "pickup transfer",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := pickupTransferFixture(t, "idle")
				v := s.findVehicle("02")
				return s, v, 0
			},
			selects: swapGives,
		},
		{
			// Dispatch gives the released pod 01 to the first trip, and the
			// check at assignment swaps it for pod 02.
			name: "reassignment at assignment",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := crossedPickupFixture(t)
				s.waiting[0].request.PodID = ""
				s.findVehicle("01").released = true
				s.SetPickupSwaps(true)
				v := s.findVehicle("02")
				return s, v, 0
			},
			selects: func(t *testing.T, s *Simulation, v *vehicle, trip int) bool {
				t.Helper()
				s.dispatch()
				if stats := s.PickupSwapStats(); stats.AssignmentChecks != 1 || s.waiting[trip].request.PodID == "" {
					t.Fatalf("dispatch did not check the assignment: %+v %+v", stats, s.waiting)
				}
				return s.waiting[trip].request.PodID == v.Pod.ID
			},
		},
		{
			// Pod 01 boards a shared party at Harbor for Garden.
			name: "shared-ride join",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "market")
				if err := s.SetSharedRidePartyLimit(2); err != nil {
					t.Fatal(err)
				}
				if err := requestSharedJourney(s, "01", "garden"); err != nil {
					t.Fatal(err)
				}
				s.waiting = []waitingTrip{newTrip(s, "harbor", "garden")}
				v := s.findVehicle("01")
				return s, v, 0
			},
			selects: func(_ *testing.T, s *Simulation, _ *vehicle, trip int) bool {
				return s.joinSharedRide(&s.waiting[trip], &dispatchPass{})
			},
		},
		{
			name: "onboard pickup",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newOccupiedPickupRide(t)
				advance(s, 37)
				v := &s.vehicles[0]
				s.waiting = append(s.waiting, newTrip(s, v.Pod.StationID, "market"))
				return s, v, len(s.waiting) - 1
			},
			selects: func(_ *testing.T, s *Simulation, _ *vehicle, trip int) bool {
				return s.joinOnboardPickup(&s.waiting[trip])
			},
		},
		{
			// Pod 02 unloads at Market. Pod 01 at Harbor is the pickup pod.
			name: "finishing-pod hold",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "market")
				unloadFor(s.findVehicle("02"), TicksPerSecond)
				s.waiting = []waitingTrip{newTrip(s, "market", "harbor")}
				v := s.findVehicle("02")
				return s, v, 0
			},
			selects: func(_ *testing.T, s *Simulation, v *vehicle, trip int) bool {
				return s.waitForFinishingPod(&s.waiting[trip], s.findVehicle("01"), nil) && s.waiting[trip].deferPodID == v.Pod.ID
			},
		},
		{
			// The trip is on hold for pod 02 until the next check. An
			// exclusion with a hold breaks X1, so this gate is defensive.
			name: "hold refresh",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s, v := finishingPodFixture(true)(t)
				return s, v, 0
			},
			selects: func(_ *testing.T, s *Simulation, _ *vehicle, trip int) bool {
				return s.keepHold(&s.waiting[trip], &dispatchPass{assigned: map[string]bool{}})
			},
		},
		{
			// The trip is on hold for pod 02 until the next check, and
			// waitForFinishingPod checks the hold again. This gate is
			// defensive, as for keepHold.
			name: "hold check",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s, v := finishingPodFixture(true)(t)
				return s, v, 0
			},
			selects: func(_ *testing.T, s *Simulation, _ *vehicle, trip int) bool {
				return s.waitForFinishingPod(&s.waiting[trip], s.findVehicle("01"), nil)
			},
		},
		{
			// The trip is on hold for pod 02, and pod 01 at Harbor is the
			// pickup pod. The hold names the pod that the pickup pod
			// waits for only when a pickup pod is available.
			name: "hold reason",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s, _ := finishingPodFixture(true)(t)
				v := s.findVehicle("01")
				return s, v, 0
			},
			selects: func(t *testing.T, s *Simulation, _ *vehicle, trip int) bool {
				t.Helper()
				if !s.keepHold(&s.waiting[trip], &dispatchPass{assigned: map[string]bool{}}) {
					t.Fatal("keepHold ends the hold for pod 02")
				}
				return s.waiting[trip].request.DispatchReason == "Waiting for pod 02 to finish"
			},
		},
		{
			// The dispatch gates keep an excluded trip from its pod, so
			// this gate is defensive.
			name: "boarding",
			build: func(t *testing.T) (*Simulation, *vehicle, int) {
				t.Helper()
				s := newPickupFleet(t, "harbor", "market")
				s.waiting = []waitingTrip{newTrip(s, "harbor", "garden")}
				v := s.findVehicle("01")
				return s, v, 0
			},
			selects: func(t *testing.T, s *Simulation, v *vehicle, trip int) bool {
				t.Helper()
				err := s.board(v, s.waiting[trip])
				if err != nil && !errors.Is(err, ErrPartyAdmission) {
					t.Fatalf("board: %v, want nil or ErrPartyAdmission", err)
				}
				return err == nil
			},
		},
	}
}

// TestExclusionGates checks each path of the exclusion gate table of the
// incident contract, section 5.3. Without an exclusion, the path selects
// the pod for the trip. With the exclusion of the pod, the path does not,
// the trip gets another pod or waits, and the exclusion stays.
func TestExclusionGates(t *testing.T) {
	t.Parallel()
	for _, test := range exclusionGateCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v, trip := test.build(t)
			if !test.selects(t, s, v, trip) {
				t.Fatal("control: the path does not select the pod for the trip without an exclusion")
			}
			s, v, trip = test.build(t)
			s.waiting[trip].excludedPod = v.Pod.ID
			if test.selects(t, s, v, trip) {
				t.Fatal("the path selects the excluded pod")
			}
			if got := s.waiting[trip]; got.request.PodID == v.Pod.ID || got.excludedPod != v.Pod.ID {
				t.Fatalf("the trip has the excluded pod, or lost its exclusion: %+v", got)
			}
		})
	}
}

// TestExclusionCacheKey checks that two trips with the same options and
// different exclusions do not share a cached pickup pod in one dispatch
// pass. Pod 01 is the only pod. The first trip excludes it and waits, and
// the second trip gets it.
func TestExclusionCacheKey(t *testing.T) {
	t.Parallel()
	s := newPickupFleet(t, "garden")
	s.waiting = []waitingTrip{newTrip(s, "market", "harbor"), newTrip(s, "market", "harbor")}
	if s.waiting[0].request.options() != s.waiting[1].request.options() {
		t.Fatal("the trips have different options")
	}
	s.waiting[0].excludedPod = "01"
	s.dispatch()
	if s.waiting[0].request.PodID != "" || s.waiting[0].excludedPod != "01" {
		t.Fatalf("the first trip has pod %q and excludes %q", s.waiting[0].request.PodID, s.waiting[0].excludedPod)
	}
	if s.waiting[1].request.PodID != "01" {
		t.Fatalf("the second trip has pod %q, want 01", s.waiting[1].request.PodID)
	}
}

// TestAssignmentKeepsExclusion checks that a trip that excludes pod 01
// keeps the exclusion when pod 02 receives it, and that pod 01 never gets
// it, also after pod 01 is in service again. assignPickup keeps the
// exclusion and the deferral fields.
func TestAssignmentKeepsExclusion(t *testing.T) {
	t.Parallel()
	s := newPickupFleet(t, "garden", "harbor")
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	a := s.findVehicle(s.waiting[0].request.PodID)
	if err := s.withdrawService(a, faultHold); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreService(a, faultHold); err != nil {
		t.Fatal(err)
	}
	s.dispatch()
	if trip := s.waiting[0]; trip.request.PodID == "" || trip.request.PodID == a.Pod.ID || trip.excludedPod != a.Pod.ID {
		t.Fatalf("the trip has pod %q and excludes %q", trip.request.PodID, trip.excludedPod)
	}
	b := s.findVehicle(s.waiting[0].request.PodID)
	monitorExclusions(t, s)
	stepUntil(t, s, "the trip leaves the queue", func() bool { return len(s.waiting) == 0 })
	if !slices.ContainsFunc(b.Riders, func(rider Request) bool { return rider.ID == 1 }) {
		t.Fatalf("pod %s does not carry the trip: %+v", b.Pod.ID, b.Riders)
	}
	trip := waitingTrip{deferUntil: 5, deferCheck: 3, deferPodID: "01", excludedPod: "02"}
	assignPickup(&trip, s.findVehicle("01"))
	if want := (waitingTrip{request: Request{PodID: "01"}, deferUntil: 5, deferCheck: 3, deferPodID: "01", excludedPod: "02"}); !reflect.DeepEqual(trip, want) {
		t.Fatalf("assignPickup gives %+v, want %+v", trip, want)
	}
}

// TestCloneCopiesExclusion checks that a clone has its own copy of the
// exclusion of a trip.
func TestCloneCopiesExclusion(t *testing.T) {
	t.Parallel()
	s := newPickupFleet(t, "garden", "harbor")
	s.waiting = []waitingTrip{newTrip(s, "market", "harbor")}
	s.waiting[0].excludedPod = "01"
	clone := s.Clone()
	if clone.waiting[0].excludedPod != "01" || !sameState(s, clone) {
		t.Fatalf("the clone excludes %q", clone.waiting[0].excludedPod)
	}
	s.waiting[0].excludedPod = ""
	if clone.waiting[0].excludedPod != "01" {
		t.Fatal("a change of the source changed the clone")
	}
}

// monitorExclusions checks the state contract, W2, X1, and X2 at each
// observation of s. It also checks that a trip with an exclusion at one
// observation is not bound to, does not wait for, and does not ride in its
// excluded pod at the next. Only a withdrawal changes an exclusion, and no
// withdrawal runs inside Step. monitorExclusions returns the counts of the
// exclusions that it saw, of the observations of a trip with an exclusion
// and a pod, and of the exclusions that ended when the trip left the
// queue.
func monitorExclusions(tb testing.TB, s *Simulation) (seen, bound, ended *int) {
	tb.Helper()
	seen, bound, ended = new(int), new(int), new(int)
	excluded := map[int]string{}
	s.monitor = func(s *Simulation) {
		tb.Helper()
		if err := s.CheckContract(); err != nil {
			tb.Fatalf("tick %d: the live state breaks the contract: %v", s.tick, err)
		}
		if err := checkExclusions(s); err != nil {
			tb.Fatalf("tick %d: %v", s.tick, err)
		}
		queued := map[int]waitingTrip{}
		for _, trip := range s.waiting {
			queued[trip.request.ID] = trip
			for _, pod := range []string{trip.request.PodID, trip.deferPodID} {
				if v := s.findVehicle(pod); v != nil && !v.inService() {
					tb.Fatalf("tick %d: W2: trip %d names the withdrawn pod %s", s.tick, trip.request.ID, pod)
				}
			}
		}
		for id, pod := range excluded {
			if trip, ok := queued[id]; ok {
				if trip.request.PodID == pod || trip.deferPodID == pod {
					tb.Fatalf("tick %d: trip %d has or waits for its excluded pod %s", s.tick, id, pod)
				}
				continue
			}
			if v := s.findVehicle(pod); slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID == id }) {
				tb.Fatalf("tick %d: trip %d rides in its excluded pod %s", s.tick, id, pod)
			}
			*ended++
		}
		next := map[int]string{}
		for _, trip := range s.waiting {
			if trip.excludedPod == "" {
				continue
			}
			if excluded[trip.request.ID] != trip.excludedPod {
				*seen++
			}
			if trip.request.PodID != "" {
				*bound++
			}
			next[trip.request.ID] = trip.excludedPod
		}
		excluded = next
	}
	s.observe()
	return seen, bound, ended
}

// withdrawalInjector withdraws one pod of a run at a time and restores it
// later. It prefers a pod that a trip waits for, then a pod that a trip is
// bound to, then the next pod of the fleet in turn.
type withdrawalInjector struct {
	every, restoreAfter int64
	// pod is the withdrawn pod, or nil. restoreAt is the tick of its
	// restore.
	pod       *vehicle
	hold      serviceHold
	restoreAt int64
	turn      int
	// withdrawals counts the withdrawals.
	withdrawals int
}

// step runs one tick of s and then withdraws or restores a pod.
func (w *withdrawalInjector) step(t *testing.T, s *Simulation) {
	t.Helper()
	s.Step()
	switch {
	case w.pod != nil && s.tick >= w.restoreAt:
		if err := s.restoreService(w.pod, w.hold); err != nil {
			t.Fatalf("tick %d: restore pod %s: %v", s.tick, w.pod.Pod.ID, err)
		}
		w.pod = nil
	case w.pod == nil && s.tick%w.every == 0:
		v := w.target(s)
		w.turn++
		w.hold = faultHold
		if w.turn%2 == 0 {
			w.hold = emergencyHold
		}
		if err := s.withdrawService(v, w.hold); err != nil {
			// A coupling member refuses. The next turn picks another pod.
			return
		}
		w.pod, w.restoreAt = v, s.tick+w.restoreAfter
		w.withdrawals++
	default:
		return
	}
	s.observe()
}

func (w *withdrawalInjector) target(s *Simulation) *vehicle {
	for _, trip := range s.waiting {
		if trip.request.PodID == "" && trip.deferPodID != "" && w.turn%3 == 0 {
			return s.findVehicle(trip.deferPodID)
		}
	}
	for _, trip := range s.waiting {
		if trip.request.PodID != "" && w.turn%3 != 2 {
			return s.findVehicle(trip.request.PodID)
		}
	}
	return &s.vehicles[w.turn%len(s.vehicles)]
}

// TestExclusionInvariantsWithWithdrawals runs fixtures of the dispatch,
// pickup swap, and onboard pickup suites with orders and injected
// withdrawals. The state contract, W2, X1, and X2 hold at each tick, and
// no trip gets the pod that it excludes (incident contract, section 5.5).
func TestExclusionInvariantsWithWithdrawals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		build func(t *testing.T) *Simulation
		pairs [][2]string
		// check runs after the run and checks that the suite feature ran.
		check func(t *testing.T, s *Simulation)
	}{
		{
			name: "dispatch",
			build: func(t *testing.T) *Simulation {
				t.Helper()
				return newPickupFleet(t, "harbor", "garden", "market")
			},
			pairs: [][2]string{{"market", "harbor"}, {"harbor", "garden"}, {"garden", "market"}, {"market", "garden"}, {"harbor", "market"}},
		},
		{
			name: "pickup swaps",
			build: func(t *testing.T) *Simulation {
				t.Helper()
				s := crossedPickupFixture(t)
				s.SetPickupSwaps(true)
				return s
			},
			pairs: [][2]string{{"s3", "s0"}, {"s1", "s2"}, {"s0", "s3"}, {"s2", "s1"}},
			check: func(t *testing.T, s *Simulation) {
				t.Helper()
				if stats := s.PickupSwapStats(); stats.Swaps+stats.Transfers == 0 {
					t.Errorf("no pickup swap or transfer: %+v", stats)
				}
			},
		},
		{
			name: "onboard pickups",
			build: func(t *testing.T) *Simulation {
				t.Helper()
				s := newPickupFleet(t, "harbor", "garden", "market")
				if err := s.SetSharedRidePartyLimit(4); err != nil {
					t.Fatal(err)
				}
				if err := s.SetOnboardPickups(true); err != nil {
					t.Fatal(err)
				}
				return s
			},
			pairs: [][2]string{{"garden", "market"}, {"harbor", "market"}, {"market", "harbor"}, {"harbor", "garden"}},
			check: func(t *testing.T, s *Simulation) {
				t.Helper()
				if s.sharedParties == 0 {
					t.Error("no party joined a ride")
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := test.build(t)
			seen, bound, ended := monitorExclusions(t, s)
			// Orders and withdrawals run for 240 s. Then the run continues
			// without withdrawals until each trip with an exclusion boarded.
			w := &withdrawalInjector{every: 10 * TicksPerSecond, restoreAfter: 5 * TicksPerSecond}
			for tick := range 240 * TicksPerSecond {
				if tick%(6*TicksPerSecond) == 0 {
					pair := test.pairs[(tick/(6*TicksPerSecond))%len(test.pairs)]
					if err := submitSharedTrip(s, pair[0], pair[1]); err != nil {
						t.Fatal(err)
					}
				}
				w.step(t, s)
			}
			for w.pod != nil {
				w.step(t, s)
			}
			excluded := func(trip waitingTrip) bool { return trip.excludedPod != "" }
			for range 3600 * TicksPerSecond {
				if !slices.ContainsFunc(s.waiting, excluded) {
					break
				}
				s.Step()
			}
			if i := slices.IndexFunc(s.waiting, excluded); i >= 0 {
				t.Errorf("trip %+v still has an exclusion", s.waiting[i])
			}
			if w.withdrawals == 0 || *seen == 0 || *bound == 0 || *ended == 0 {
				t.Fatalf("%d withdrawals, %d exclusions, %d bound observations, %d ended exclusions", w.withdrawals, *seen, *bound, *ended)
			}
			t.Logf("%d withdrawals, %d exclusions, %d bound observations, %d ended exclusions", w.withdrawals, *seen, *bound, *ended)
			if test.check != nil {
				test.check(t, s)
			}
		})
	}
}
