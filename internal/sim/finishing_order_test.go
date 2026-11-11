package sim

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// finishingPodFleetOrder is finishingPod as it was before it sorted the
// pods by bound. It scans the pods in fleet order, and a pod wins only
// with an ETA less than the best so far.
func (s *Simulation) finishingPodFleetOrder(trip *waitingTrip, idle *vehicle, stationID string, limit, holdSeconds float64) *vehicle {
	bestETA := limit
	var best *vehicle
	var bounds []float64
	fits := classFits{s: s, request: &trip.request}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v == idle || !v.inService() || trip.excludes(v.Pod.ID) || !fits.pod(v) {
			continue
		}
		node, remaining, ok := s.passAvailableAfter(i)
		if !ok {
			continue
		}
		if canWin := remaining < bestETA && remaining <= holdSeconds; !canWin {
			continue
		}
		if bounds == nil {
			bounds = s.stationPickupBounds(stationID)
		}
		if index, found := s.graph.nodes[node]; found && index < len(bounds) &&
			pickupCannotImprove(remaining+bounds[index], min(bestETA, holdSeconds)) {
			continue
		}
		if !s.pickupAccess(v, trip.request) {
			continue
		}
		route, _, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: node, station: stationID, load: noBerthLoad, accept: s.pickupBerthFilter(v, trip.request)})
		if err != nil {
			continue
		}
		eta := remaining + s.routeSeconds(motionEnvelopeFor(v.Pod.Class), route, motionEstimate{})
		if eta < bestETA && eta <= holdSeconds {
			best, bestETA = v, eta
		}
	}
	return best
}

// finishingETA returns the ETA that finishingPod computes for pod v when it
// becomes available at node after remaining seconds.
func (s *Simulation) finishingETA(trip *waitingTrip, v *vehicle, node string, remaining float64) (float64, bool) {
	route, _, err := s.stationRouteByLoad(stationRouteInput{class: v.Pod.Class, from: node, station: trip.request.legOrigin(), load: noBerthLoad, accept: s.pickupBerthFilter(v, trip.request)})
	if err != nil {
		return 0, false
	}
	return remaining + s.routeSeconds(motionEnvelopeFor(v.Pod.Class), route, motionEstimate{}), true
}

// firstFinishingCandidate returns the first pod in fleet order that the
// scan in fleet order computes a route for.
func (s *Simulation) firstFinishingCandidate(trip *waitingTrip, limit, holdSeconds float64) *vehicle {
	fits := classFits{s: s, request: &trip.request}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if !v.inService() || trip.excludes(v.Pod.ID) || !fits.pod(v) || !s.pickupAccess(v, trip.request) {
			continue
		}
		if _, remaining, ok := s.passAvailableAfter(i); ok && remaining < limit && remaining <= holdSeconds {
			return v
		}
	}
	return nil
}

// finishingOrderCounts counts the cases of TestFinishingPodMatchesFleetOrder.
type finishingOrderCounts struct {
	// checks counts the comparisons, and holds the comparisons that found a
	// pod.
	checks, holds int
	// reordered counts the holds for a pod that is not the first candidate
	// in fleet order.
	reordered int
	// ties counts the equal-ETA cases in which the later pod in fleet order
	// has the smaller bound, so finishingPod computes its route first.
	ties int
}

// checkFinishingOrder compares finishingPod with finishingPodFleetOrder
// for each trip with no pod, with several limits and hold lengths. Then it
// makes an equal-ETA tie for each trip that a pod can be held for. An
// earlier pod in fleet order gets a pass memo that puts it at a berth of
// the pickup with the ETA of the winner, so its bound is that ETA.
func checkFinishingOrder(t *testing.T, s *Simulation, counts *finishingOrderCounts) {
	t.Helper()
	inf := math.Inf(1)
	for index := range s.waiting {
		trip := &s.waiting[index]
		if trip.request.PodID != "" {
			continue
		}
		stationID := trip.request.legOrigin()
		for _, limits := range [][2]float64{{inf, inf}, {60, inf}, {inf, 20}, {30, 30}} {
			limit, hold := limits[0], limits[1]
			want := s.finishingPodFleetOrder(trip, nil, stationID, limit, hold)
			got := s.finishingPod(trip, nil, stationID, limit, hold)
			counts.checks++
			if got != want {
				t.Fatalf("tick %d, trip %d, limit %v, hold %v: pod %v, fleet order gives %v", s.tick, trip.request.ID, limit, hold, podName(got), podName(want))
			}
			if got != nil {
				counts.holds++
				if first := s.firstFinishingCandidate(trip, limit, hold); first != got {
					counts.reordered++
				}
			}
		}
		checkFinishingTie(t, s, trip, counts)
	}
}

// checkFinishingTie gives an earlier pod the ETA of the winner, and checks
// that both scans hold the trip for the earlier pod.
func checkFinishingTie(t *testing.T, s *Simulation, trip *waitingTrip, counts *finishingOrderCounts) {
	t.Helper()
	inf := math.Inf(1)
	stationID := trip.request.legOrigin()
	winner := s.finishingPod(trip, nil, stationID, inf, inf)
	if winner == nil {
		return
	}
	station, _ := s.station(stationID)
	bounds := s.stationPickupBounds(stationID)
	winnerIndex := winner.index(s)
	node, remaining, _ := s.availableAfter(winner)
	winnerETA, ok := s.finishingETA(trip, winner, node, remaining)
	if !ok {
		t.Fatalf("tick %d, trip %d: no route for pod %s", s.tick, trip.request.ID, winner.Pod.ID)
	}
	fits := classFits{s: s, request: &trip.request}
	for i := range winnerIndex {
		v := &s.vehicles[i]
		if !v.inService() || trip.excludes(v.Pod.ID) || !fits.pod(v) || !s.pickupAccess(v, trip.request) {
			continue
		}
		berth := station.Berths[0].Node
		if eta, ok := s.finishingETA(trip, v, berth, winnerETA); !ok || eta != winnerETA {
			continue
		}
		pass := &dispatchPass{active: true, finish: make([]finishMemo, len(s.vehicles))}
		for j := range s.vehicles {
			memo := &pass.finish[j]
			memo.node, memo.remaining, memo.ok = s.availableAfter(&s.vehicles[j])
			memo.known = true
		}
		pass.finish[i] = finishMemo{node: berth, remaining: winnerETA, ok: true, known: true}
		s.pass = pass
		want := s.finishingPodFleetOrder(trip, nil, stationID, inf, inf)
		got := s.finishingPod(trip, nil, stationID, inf, inf)
		s.pass = nil
		if want != v {
			t.Fatalf("tick %d, trip %d: fleet order holds for %v, want the earlier pod %s", s.tick, trip.request.ID, podName(want), v.Pod.ID)
		}
		if got != want {
			t.Fatalf("tick %d, trip %d: tie of pods %s and %s at %v s: pod %v, fleet order gives %s",
				s.tick, trip.request.ID, v.Pod.ID, winner.Pod.ID, winnerETA, podName(got), want.Pod.ID)
		}
		if remaining+bounds[s.graph.nodes[node]] < winnerETA+bounds[s.graph.nodes[berth]] {
			counts.ties++
		}
		return
	}
}

// index returns the fleet index of pod v.
func (v *vehicle) index(s *Simulation) int {
	for i := range s.vehicles {
		if &s.vehicles[i] == v {
			return i
		}
	}
	return -1
}

func podName(v *vehicle) string {
	if v == nil {
		return "none"
	}
	return v.Pod.ID
}

// TestFinishingPodMatchesFleetOrder checks that finishingPod holds a trip
// for the pod that the scan in fleet order holds it for. A busy London
// network with 160 pods gets three random trips each second while fewer
// than 120 trips wait. Each 10 seconds, the test compares the two scans for each trip with no pod, and
// for a tie that it makes.
func TestFinishingPodMatchesFleetOrder(t *testing.T) {
	t.Parallel()
	skipLong(t)
	network := scenarioNetworks(t)["London"]
	var placements []Placement
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			if len(placements) < 160 {
				placements = append(placements, Placement{ID: fmt.Sprintf("%03d", len(placements)+1), StationID: station.ID, BerthID: berth.ID})
			}
		}
	}
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(20260929, 1))
	var counts finishingOrderCounts
	started := time.Now()
	for tick := range 600 * TicksPerSecond {
		if tick%TicksPerSecond == 0 && len(s.waiting) < 120 {
			for range 3 {
				from := network.Stations[rng.IntN(len(network.Stations))].ID
				to := network.Stations[rng.IntN(len(network.Stations))].ID
				if from != to {
					_ = s.RequestTrip(from, to)
				}
			}
		}
		if tick%(10*TicksPerSecond) == 0 && tick >= 60*TicksPerSecond {
			checkFinishingOrder(t, s, &counts)
		}
		s.Step()
	}
	t.Logf("%+v, %d waiting, %v", counts, len(s.waiting), time.Since(started))
	if counts.holds == 0 || counts.reordered == 0 || counts.ties == 0 {
		t.Fatalf("%+v: want holds, holds for a pod that is not the first in fleet order, and ties", counts)
	}
}
