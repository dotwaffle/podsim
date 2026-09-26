package sim

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

// waitForFinishingPodFull is waitForFinishingPod as it was before the loop
// skipped the empty routes that cannot win. It computes the ETA of each
// busy pod.
func (s *Simulation) waitForFinishingPodFull(trip *waitingTrip, idle *vehicle, assigned map[string]bool) bool {
	if s.finishingPodWait == FinishingPodWaitNone {
		return false
	}
	if trip.deferUntil != 0 && s.tick >= trip.deferUntil {
		return false
	}
	if trip.deferCheck > s.tick {
		trip.request.DispatchReason = "Waiting for pod " + trip.deferPodID + " to finish"
		return true
	}
	station, _ := s.station(trip.request.From)
	route, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: idle, station: trip.request.From, assigned: assigned})
	if !ok {
		return false
	}
	bestETA := s.pickupSeconds(idle, route) - pickupAdvantageSeconds
	holdSeconds := math.Inf(1)
	if s.finishingPodWait == FinishingPodWaitStrict {
		holdUntil := trip.deferUntil
		if holdUntil == 0 {
			holdUntil = s.tick + maxDispatchDeferral
		}
		holdSeconds = float64(holdUntil-s.tick) / TicksPerSecond
	}
	var best *vehicle
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v == idle {
			continue
		}
		node, remaining, ok := s.availableAfter(v)
		if !ok {
			continue
		}
		eta := remaining + s.emptySeconds(node, station.Berths[0].Node)
		if eta < bestETA && eta <= holdSeconds {
			best, bestETA = v, eta
		}
	}
	if best == nil {
		return false
	}
	if trip.deferUntil == 0 {
		trip.deferUntil = s.tick + maxDispatchDeferral
	}
	trip.deferCheck = s.tick + TicksPerSecond
	trip.deferPodID = best.Pod.ID
	trip.request.DispatchReason = "Waiting for pod " + best.Pod.ID + " to finish"
	return true
}

// holdScenario is a busy simulation for the tests that compare a hold check
// with a full check. Six pods on the example network with a second Market
// berth serve a trip every 5 s for 240 s.
type holdScenario struct {
	rule       FinishingPodWait
	congestion bool
}

// run calls check four times each simulated second, before the step.
func (scenario holdScenario) run(t *testing.T, check func(s *Simulation)) {
	t.Helper()
	s, err := NewFleet(twoBerthMarket(), []Placement{
		{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"},
		{ID: "03", StationID: "market", BerthID: "market-1"}, {ID: "04", StationID: "market", BerthID: "market-2"},
		{ID: "05", StationID: "parking", BerthID: "parking-1"}, {ID: "06", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFinishingPodWait(scenario.rule); err != nil {
		t.Fatal(err)
	}
	s.SetCongestionRouting(scenario.congestion)
	s.SetRedistribution(true)
	stations := []string{"harbor", "garden", "market"}
	for tick := range 400 * TicksPerSecond {
		if trip := tick / (5 * TicksPerSecond); tick%(5*TicksPerSecond) == 0 && trip < 48 {
			from, to := stations[trip%3], stations[(trip+1+trip/3%2)%3]
			if err := s.RequestTrip(from, to); err != nil {
				t.Fatal(err)
			}
		}
		if tick%(TicksPerSecond/4) == 0 {
			check(s)
		}
		s.Step()
	}
}

// sameHoldState reports whether a and b hold the same state apart from the
// route caches. A cached route is equal to a new route, so the content of a
// route cache does not change a result.
func sameHoldState(a, b *Simulation) bool {
	strip := func(s *Simulation) *Simulation {
		c := stripCaches(s)
		c.congestionRoutes = nil
		return c
	}
	// DeepEqual compares route errors by value on purpose. See sameState.
	return reflect.DeepEqual(strip(a), strip(b)) //nolint:govet // deepequalerrors: route errors compare by value on purpose.
}

// waitingAssignments returns the pods of the waiting trips, as dispatch
// does at the start of a pass.
func waitingAssignments(s *Simulation) map[string]bool {
	assigned := make(map[string]bool)
	for _, trip := range s.waiting {
		if trip.request.PodID != "" {
			assigned[trip.request.PodID] = true
		}
	}
	return assigned
}

// TestFinishingPodWaitMatchesFullLoop checks waitForFinishingPod beside
// waitForFinishingPodFull for each waiting trip and each pod that can
// travel to the pickup. The two must give the same result and the same
// state apart from the route caches.
func TestFinishingPodWaitMatchesFullLoop(t *testing.T) {
	t.Parallel()
	for _, scenario := range []holdScenario{
		{rule: FinishingPodWaitCurrent}, {rule: FinishingPodWaitStrict},
		{rule: FinishingPodWaitCurrent, congestion: true}, {rule: FinishingPodWaitStrict, congestion: true},
	} {
		t.Run(fmt.Sprintf("%+v", scenario), func(t *testing.T) {
			t.Parallel()
			holds, sends := 0, 0
			scenario.run(t, func(s *Simulation) {
				assigned := waitingAssignments(s)
				probe := s.Clone()
				for index, trip := range s.waiting {
					for pod := range probe.vehicles {
						idle := &probe.vehicles[pod]
						_, _, ok := probe.pickupRouteWithAssignments(pickupRouteInput{pod: idle, station: trip.request.From, assigned: assigned})
						if !ok || idle.Pod.Activity == Idle && idle.Pod.StationID == trip.request.From {
							continue
						}
						fast, full := s.Clone(), s.Clone()
						// Check the hold now, as at the next check of the trip.
						fast.waiting[index].deferCheck, full.waiting[index].deferCheck = 0, 0
						got := fast.waitForFinishingPod(&fast.waiting[index], &fast.vehicles[pod], assigned)
						want := full.waitForFinishingPodFull(&full.waiting[index], &full.vehicles[pod], assigned)
						if got != want || !sameHoldState(fast, full) {
							t.Fatalf("tick %d, trip %d, pod %s: hold %v, want %v, or the states differ: %+v, want %+v",
								s.tick, trip.request.ID, idle.Pod.ID, got, want, fast.waiting[index], full.waiting[index])
						}
						if got {
							holds++
						} else {
							sends++
						}
					}
				}
			})
			if holds == 0 || sends == 0 {
				t.Fatalf("the scenario checked %d holds and %d sends, want both", holds, sends)
			}
		})
	}
}

// TestFinishingPodWaitAtBoundary holds a trip for pod 02, which unloads at
// the pickup station. Its time before it is available is one tick less
// than the ETA of the idle pod minus the advantage. Its empty route has no
// lanes, so it wins by less than one tick.
func TestFinishingPodWaitAtBoundary(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	idle := s.findVehicle("01")
	route, _, ok := s.pickupRoute(idle, "market")
	if !ok {
		t.Fatal("pod 01 cannot reach Market")
	}
	bestETA := s.pickupSeconds(idle, route) - pickupAdvantageSeconds
	busy := s.findVehicle("02")
	busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
	busy.phaseTicks = int(math.Ceil(bestETA*TicksPerSecond)) - 1
	busy.Request = &Request{ID: 1, From: "harbor", To: "market", PartySize: 1, PodID: "02"}
	s.requestID = 1
	if remaining := float64(busy.phaseTicks) / TicksPerSecond; remaining >= bestETA || remaining < bestETA-1.0/TicksPerSecond {
		t.Fatalf("pod 02 is available after %v s, want just less than %v s", remaining, bestETA)
	}
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	if pending := s.Snapshot().Pending[0]; pending.PodID != "" || pending.DispatchReason != "Waiting for pod 02 to finish" {
		t.Fatalf("the trip did not wait for pod 02: %+v", pending)
	}
}

// TestFinishingPodWaitKeepsCongestionRefresh checks that the loop does not
// skip the route query that refreshes the congestion costs. Pod 01 is idle
// at the pickup station, so its pickup makes no route query, and no busy
// pod can win.
func TestFinishingPodWaitKeepsCongestionRefresh(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "market"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	busy := s.findVehicle("02")
	busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
	busy.phaseTicks = 5 * TicksPerSecond
	s.SetCongestionRouting(true)
	s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 1, From: "market", To: "harbor", PartySize: 1}})
	full := s.Clone()
	if s.waitForFinishingPod(&s.waiting[0], s.findVehicle("01"), map[string]bool{}) ||
		full.waitForFinishingPodFull(&full.waiting[0], full.findVehicle("01"), map[string]bool{}) {
		t.Fatal("the trip waits for a pod that cannot win")
	}
	if full.congestionRefreshDue() {
		t.Fatal("the full loop did not refresh the congestion costs")
	}
	if !sameHoldState(s, full) {
		t.Fatalf("the congestion refresh moved: next refresh at tick %d, want %d", s.nextCongestionRouteRefresh, full.nextCongestionRouteRefresh)
	}
}

// TestEmptySecondsIsNotNegative checks the fact that the skip in
// waitForFinishingPod uses. Each empty route to a pickup berth takes zero
// or more seconds.
func TestEmptySecondsIsNotNegative(t *testing.T) {
	t.Parallel()
	for _, network := range []Network{Example(), twoBerthMarket()} {
		s, err := New(network, "harbor")
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range network.Nodes {
			for _, station := range network.Stations {
				if seconds := s.emptySeconds(node.ID, station.Berths[0].Node); !(seconds >= 0) {
					t.Fatalf("empty route %s to %s takes %v s", node.ID, station.Berths[0].Node, seconds)
				}
			}
		}
	}
}

// localPickupFull is localPickup as it was before the free pods. It reads
// each pod.
func (s *Simulation) localPickupFull(stationID string, assigned map[string]bool) *vehicle {
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Idle && v.Pod.StationID == stationID && !assigned[v.Pod.ID] {
			return v
		}
	}
	return nil
}

// pickupAvailableFull is pickupAvailable as it was before the free pods and
// noBerthLoad. It returns the first pod that pickupRouteWithAssignments
// accepts, or nil.
func (s *Simulation) pickupAvailableFull(stationID string, assigned map[string]bool) *vehicle {
	load := s.berthLoads()
	for i := range s.vehicles {
		if _, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: &s.vehicles[i], station: stationID, assigned: assigned, load: load}); ok {
			return &s.vehicles[i]
		}
	}
	return nil
}

// podID returns the ID of v, or "" when v is nil.
func podID(v *vehicle) string {
	if v == nil {
		return ""
	}
	return v.Pod.ID
}

// TestHoldChecksMatchFullScan checks localPickup and pickupAvailable beside
// copies of the old functions, which read each pod and compute the berth
// loads. The results must be equal. The states must be equal, route caches
// included, because the two make the same route queries in the same order.
func TestHoldChecksMatchFullScan(t *testing.T) {
	t.Parallel()
	for _, congestion := range []bool{false, true} {
		t.Run(fmt.Sprintf("congestion %v", congestion), func(t *testing.T) {
			t.Parallel()
			var locals, idle, moving, none int
			holdScenario{rule: FinishingPodWaitCurrent, congestion: congestion}.run(t, func(s *Simulation) {
				assigned := waitingAssignments(s)
				fast, full := s.Clone(), s.Clone()
				pass := dispatchPass{assigned: assigned}
				for _, station := range s.network.Stations {
					local, wantLocal := fast.localPickup(station.ID, &pass), full.localPickupFull(station.ID, assigned)
					if podID(local) != podID(wantLocal) {
						t.Fatalf("tick %d, %s: local pickup %q, want %q", s.tick, station.ID, podID(local), podID(wantLocal))
					}
					if local != nil {
						locals++
					}
					if station.ParkingOnly {
						continue
					}
					available, first := fast.pickupAvailable(station.ID, &pass), full.pickupAvailableFull(station.ID, assigned)
					if available != (first != nil) {
						t.Fatalf("tick %d, %s: pickup available %v, want pod %q", s.tick, station.ID, available, podID(first))
					}
					switch {
					case first == nil:
						none++
					case first.Pod.Activity == Idle:
						idle++
					default:
						moving++
					}
				}
				if !reflect.DeepEqual(fast, full) { //nolint:govet // deepequalerrors: route errors compare by value on purpose.
					t.Fatalf("tick %d: the states differ", s.tick)
				}
			})
			if locals == 0 || idle == 0 || moving == 0 || none == 0 {
				t.Fatalf("the scenario found %d local pickups, and %d idle, %d moving and %d missing first pickups, want each", locals, idle, moving, none)
			}
		})
	}
}

// TestDispatchPassResetFindsNewFreePods checks that a reset of the pass
// finds the free pods again. Pod 01 is idle at Harbor, but a trip holds it.
// Pod 02 travels with a passenger, so it is not free. It then becomes idle
// at Garden.
func TestDispatchPassResetFindsNewFreePods(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	busy := s.findVehicle("02")
	busy.Pod.Activity, busy.Pod.Occupied = Traveling, true
	pass := dispatchPass{assigned: map[string]bool{"01": true}, pickups: map[string]*vehicle{"market": nil}}
	if s.localPickup("harbor", &pass) != nil || s.localPickup("garden", &pass) != nil || s.pickupAvailable("market", &pass) {
		t.Fatal("found a pickup pod before pod 02 became idle")
	}
	busy.Pod.Activity, busy.Pod.Occupied = Idle, false
	pass.reset()
	if _, known := pass.pickups["market"]; known {
		t.Fatal("reset kept the pickup pod of Market")
	}
	if s.localPickup("garden", &pass) != busy || !s.pickupAvailable("market", &pass) {
		t.Fatal("did not find pod 02 after the reset")
	}
}
