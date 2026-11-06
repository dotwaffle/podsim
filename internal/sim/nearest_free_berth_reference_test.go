package sim

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// referenceNearestFreeBerth keeps nearestFreeBerth from before its filter used
// node indexes and before bankNearest shared searches.
func (s *Simulation) referenceNearestFreeBerth(v *vehicle, from string) (Berth, string, bool) {
	busy := make(map[string]bool)
	for i := range s.vehicles {
		other := &s.vehicles[i]
		if other != v && other.Pod.Activity != Idle && other.destination.ID != "" {
			busy[other.destination.ID] = true
		}
	}
	for _, trip := range s.waiting {
		if trip.destination.ID != "" {
			busy[trip.destination.ID] = true
		}
	}
	type candidate struct {
		berth   Berth
		station string
	}
	rank := make([]int, len(s.network.Nodes))
	for i := range rank {
		rank[i] = -1
	}
	var candidates []candidate
	add := func(berth Berth, station string) {
		node, ok := s.graph.nodes[berth.Node]
		if !ok || rank[node] >= 0 || busy[berth.ID] || s.berthBlocked(berth) || !s.referenceBerthAvailableTo(v, berth) {
			return
		}
		if berth.Node == from && v.Pod.Activity == Traveling {
			return
		}
		if v.Pod.Activity == Traveling && !v.originReleased && berth.ID == v.origin.ID {
			return
		}
		rank[node] = len(candidates)
		candidates = append(candidates, candidate{berth: berth, station: station})
	}
	addDepot := func(station Station) bool {
		if !s.depots.isDepot(station.ID) {
			return false
		}
		if node, ok := s.graph.nodes[station.Entry]; ok && rank[node] < 0 && s.depotGoal(v, station) {
			rank[node] = len(candidates)
			candidates = append(candidates, candidate{station: station.ID})
		}
		return true
	}
	current, hasCurrent := s.station(v.destinationStation)
	if hasCurrent && !addDepot(current) {
		if berth, ok := current.berth(v.destination.ID); ok {
			add(berth, current.ID)
		}
		for _, berth := range current.Berths {
			add(berth, current.ID)
		}
	}
	for _, station := range s.network.Stations {
		if hasCurrent && station.ID == current.ID || addDepot(station) {
			continue
		}
		for _, berth := range station.Berths {
			add(berth, station.ID)
		}
	}
	if len(candidates) == 0 {
		return Berth{}, "", false
	}
	input := preferredNearestInput{from: from, rank: rank, class: v.Pod.Class}
	graph := s.routingGraph()
	var node int
	var ok bool
	if len(graph.banks.banks) > 0 {
		node, ok = s.network.referenceBankNearest(input, graph)
	} else {
		node, ok = s.network.preferredNearestIndexed(input, graph)
	}
	if !ok {
		return Berth{}, "", false
	}
	chosen := candidates[rank[node]]
	return chosen.berth, chosen.station, true
}

// referenceBerthAvailableTo keeps berthAvailableTo from before the filter used
// node indexes.
func (s *Simulation) referenceBerthAvailableTo(v *vehicle, berth Berth) bool {
	if node, ok := s.graph.nodes[berth.Node]; !ok || !s.graph.nodeAllows(node, v.Pod.Class) {
		return false
	}
	for _, r := range berthResources(berth) {
		if owner := s.owners[r]; !owner.isZero() && !owner.isPod(v.Pod.ID) {
			return false
		}
	}
	return true
}

// berthChoiceStats counts what a differential run reached, so that the test can
// refuse a run that did not reach the cases that it must compare.
type berthChoiceStats struct {
	compared, found, missed, parked, moved, multiple, blockedPasses, depots int
}

// compareBerthChoice compares the choice with the reference for one pod.
func compareBerthChoice(t *testing.T, s *Simulation, v *vehicle, stats *berthChoiceStats) (string, bool) {
	t.Helper()
	_, from, ok := s.divertStart(v)
	if !ok {
		return "", false
	}
	berth, station, found := s.nearestFreeBerth(v, from)
	wantBerth, wantStation, wantFound := s.referenceNearestFreeBerth(v, from)
	if berth.ID != wantBerth.ID || berth.Node != wantBerth.Node || station != wantStation || found != wantFound {
		t.Fatalf("tick %d, pod %s, from %s: got %q in %q, %t, want %q in %q, %t", s.tick, v.Pod.ID, from, berth.ID, station, found, wantBerth.ID, wantStation, wantFound)
	}
	stats.compared++
	if found {
		stats.found++
		if berth.ID == "" {
			stats.depots++
		}
	} else {
		stats.missed++
	}
	return fmt.Sprint(station, "/", berth.ID), true
}

// compareReleasePass runs one parking pass on a clone of s, with a blocked set
// when blocked is true: each pod that can be released is released, and goes to
// its berth in fleet order, as in parkUnclaimedReleased. Each parked pod changes
// the answer for the next pod, so this comparison finds a stale view of the
// pods, the trips, or the owners. moved counts the pods whose choice changed
// during the pass.
func compareReleasePass(t *testing.T, s *Simulation, rng *rand.Rand, blocked bool, stats *berthChoiceStats) {
	t.Helper()
	c := s.Clone()
	if blocked {
		footprint := faultFootprint{id: "i0.1"}
		for _, lane := range c.network.Lanes {
			if rng.IntN(8) == 0 {
				footprint.resources = append(footprint.resources, track(lane.ID, 0))
			}
		}
		for _, station := range c.network.Stations {
			for _, berth := range station.Berths {
				if rng.IntN(4) == 0 {
					footprint.resources = append(footprint.resources, resource{kind: berthResource, id: berth.ID})
				}
			}
		}
		c.setBlocked([]faultFootprint{footprint})
		if len(c.blocked.berths) > 0 {
			stats.blockedPasses++
		}
	}
	first := make(map[string]string)
	for i := range c.vehicles {
		v := &c.vehicles[i]
		if choice, ok := compareBerthChoice(t, c, v, stats); ok {
			first[v.Pod.ID] = choice
		}
		c.releasePickup(v)
	}
	parked := 0
	for i := range c.vehicles {
		v := &c.vehicles[i]
		if !v.released || !v.inService() || c.owners[resource{kind: berthResource, id: v.destination.ID}] == podResourceOwner(v.Pod.ID) {
			continue
		}
		if choice, ok := compareBerthChoice(t, c, v, stats); ok && choice != first[v.Pod.ID] {
			stats.moved++
		}
		c.parkReleased(v)
		parked++
	}
	stats.parked += parked
	if parked > 1 {
		stats.multiple++
	}
}

// TestNearestFreeBerthMatchesReference compares the berth choice with the
// reference over real runs with incidents. At a regular tick, it also makes
// parking passes of several pods on a clone, with the blocked set of the run and
// with a random blocked set of lanes and berths.
func TestNearestFreeBerthMatchesReference(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	tests := []struct {
		name        string
		network     Network
		pods, ticks int
		every       int
		depot, pass bool
	}{
		{name: "banks", network: missNetwork(10, 5, 20), pods: 24, ticks: 1800, every: 30, pass: true},
		{name: "bank example", network: BankExample(), pods: 10, ticks: 1800, every: 30},
		{name: "no banks", network: lineNetwork(lineStations(0, 4, 4, 4, 4, 4, 4)), pods: 12, ticks: 1800, every: 30, pass: true},
		{name: "depot", network: shortDepotNetwork(2, 3), pods: 6, ticks: 1800, every: 30, depot: true},
		{name: "rail hub", network: networks["rail hub"], pods: 40, ticks: 1200, every: 60, pass: true},
		{name: "London", network: networks["London"], pods: 40, ticks: 300, every: 150, pass: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, stations := berthChoiceRun(t, test.network, test.pods)
			rng := rand.New(rand.NewPCG(uint64(len(test.network.Nodes)), 5))
			var stats berthChoiceStats
			for tick := range test.ticks {
				if tick%5 == 0 {
					if from, to := stations[rng.IntN(len(stations))], stations[rng.IntN(len(stations))]; from != to {
						_ = s.RequestTrip(from, to)
					}
				}
				s.Step()
				if tick%test.every == test.every-1 {
					compareReleasePass(t, s, rng, false, &stats)
					compareReleasePass(t, s, rng, true, &stats)
				}
			}
			t.Logf("%+v", stats)
			if stats.found == 0 || stats.blockedPasses == 0 || test.pass && (stats.moved == 0 || stats.multiple == 0) || test.depot && stats.depots == 0 {
				t.Fatalf("the run does not reach each case: %+v", stats)
			}
		})
	}
}

// berthChoiceRun returns a fleet on the network with incident sampling, and the
// passenger stations of the network.
func berthChoiceRun(t *testing.T, network Network, pods int) (*Simulation, []string) {
	t.Helper()
	var fleet []Placement
	var stations []string
	for _, station := range network.Stations {
		if station.ParkingOnly {
			continue
		}
		stations = append(stations, station.ID)
		for _, berth := range station.Berths {
			if len(fleet) < pods {
				fleet = append(fleet, Placement{ID: fmt.Sprintf("%03d", len(fleet)+1), StationID: station.ID, BerthID: berth.ID})
			}
		}
	}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	s.emergenciesOn = true
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	rates := IncidentRates{
		Faults:             FaultRate{PerHour: 60, DebrisShare: 0.25, DebrisMeters: 2, Duration: FaultDurationRule{Kind: "fixed", Seconds: 60}},
		EmergenciesPerHour: 60,
		FaultSeed:          [2]uint64{1, uint64(len(network.Nodes))},
		EmergencySeed:      [2]uint64{2, uint64(len(network.Nodes))},
	}
	if err := s.StartIncidentSampling(rates); err != nil {
		t.Fatal(err)
	}
	return s, stations
}
