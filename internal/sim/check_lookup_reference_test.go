package sim

import (
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// referenceLaneIndexes is laneIndexes before the route lane indexes came
// from the lane cells. It looks up the ID of each lane.
func referenceLaneIndexes(s *Simulation, route []Lane, limit int) []int {
	if len(route) == 0 || len(route) > limit {
		return nil
	}
	indexes := make([]int, len(route))
	for index, lane := range route {
		indexes[index] = s.graph.lanes[lane.ID]
	}
	return indexes
}

// referenceExportRoute returns the saved route of the pod v as exportPod
// built it before the route lane indexes came from the lane cells.
func referenceExportRoute(s *Simulation, v *vehicle, limits routeLimits) []int {
	switch v.Pod.Activity {
	case Boarding, DepartingEmpty, Continuing:
		return referenceLaneIndexes(s, v.Route, limits.pod)
	case Traveling:
		if v.blocks.len() == 0 {
			return nil
		}
		plan := s.planSavedRoutes(limits, math.MaxInt, nil)
		start := plan.pods[s.vehicleIndexes[v.Pod.ID]].start
		return referenceLaneIndexes(s, v.Route[start:], limits.pod)
	default:
		return nil
	}
}

// referenceSpanEnvelope is appendSpanEnvelope before the route lane
// indexes came from the lane cells and the planes of rule 2 from plane
// numbers. It looks up the ID of each route lane, and it applies rule 2
// with clusterSafety (see referenceClusters).
func referenceSpanEnvelope(s *Simulation, locations []SafetyLocation, v *vehicle, span *reachSpan, choice envelopeChoice) []SafetyLocation {
	r := s.envelopes
	lanes := len(r.lanes)
	var selected, candidates []int
	for index, number := range span.locations {
		origin := r.keyOrigin(span.keys[index])
		switch {
		case number >= lanes:
			if number == choice.originBerth && origin == choice.origin {
				selected = append(selected, number)
			}
		case origin >= 0 && origin == choice.origin:
			selected = append(selected, number)
		default:
			candidates = append(candidates, number)
		}
	}
	if len(candidates) > 0 && v.Pod.Activity == Traveling {
		slices.Sort(candidates)
		for _, lane := range v.Route {
			if number, ok := s.graph.lanes[lane.ID]; ok {
				if _, found := slices.BinarySearch(candidates, number); found {
					selected = append(selected, number)
				}
			}
		}
	}
	return referenceSelected(s.networkIndexes, locations, selected)
}

// referenceSelected is appendSelected before the planes came from plane
// numbers. It compares the planes, and it applies rule 2 with
// clusterSafety (see referenceClusters).
func referenceSelected(n *networkIndexes, locations []SafetyLocation, selected []int) []SafetyLocation {
	slices.Sort(selected)
	start := len(locations)
	for _, number := range slices.Compact(selected) {
		if location := n.envelopes.locations[number]; !slices.Contains(locations[start:], location) {
			locations = append(locations, location)
		}
	}
	return referenceClusters(n, locations, start)
}

// referenceClusters is appendClusters before the planes of rule 2 came
// from plane numbers. It looks up clusterSafety by the plane of each
// location.
func referenceClusters(n *networkIndexes, locations []SafetyLocation, start int) []SafetyLocation {
	if len(n.clusterSafety) == 0 {
		return locations
	}
	end := len(locations)
	var applied []*SafetyLocation
	for index := start; index < end; index++ {
		planes := n.clusterSafety[locations[index]]
		if len(planes) == 0 || slices.Contains(applied, &planes[0]) {
			continue
		}
		applied = append(applied, &planes[0])
		for _, extra := range planes {
			if !slices.Contains(locations[start:], extra) {
				locations = append(locations, extra)
			}
		}
	}
	return locations
}

// referenceVehicleEnvelope is appendVehicleSafetyLocations with
// referenceSpanEnvelope.
func referenceVehicleEnvelope(s *Simulation, v *vehicle) []SafetyLocation {
	if s.envelopes == nil {
		return nil
	}
	span, choice, ok := s.envelopeSpan(v)
	if !ok {
		return nil
	}
	return referenceSpanEnvelope(s, nil, v, span, choice)
}

// checkLookupCounts counts the coverage of TestCheckLookupsMatchReference.
type checkLookupCounts struct {
	ticks, envelopes, clustered, routes, cellHits, mapLookups, mismatched int
	activities                                                            map[Activity]int
}

// checkLookupTick compares the envelopes, the observation envelopes, and the
// saved routes of s with their references at one tick. random picks a
// route of another pod for each pod, which the blocks of the pod do not
// cover.
func checkLookupTick(t *testing.T, s *Simulation, random *rand.Rand, counts *checkLookupCounts) {
	t.Helper()
	counts.ticks++
	o := s.SafetyObservation()
	state := s.ExportState()
	saved := make(map[string]SavedPod, len(state.Pods))
	for _, pod := range state.Pods {
		saved[pod.ID] = pod
	}
	limits := newRouteLimits(s.network)
	for index := range s.vehicles {
		v := &s.vehicles[index]
		got, want := s.appendVehicleSafetyLocations(nil, v), referenceVehicleEnvelope(s, v)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tick %d pod %s: envelope %v, reference %v", s.tick, v.Pod.ID, got, want)
		}
		if len(o.envelopes) > 0 && !slices.Equal(o.envelopes[index], want) {
			t.Fatalf("tick %d pod %s: observation envelope %v, reference %v", s.tick, v.Pod.ID, o.envelopes[index], want)
		}
		if len(want) > 0 {
			counts.envelopes++
			if span, choice, ok := s.envelopeSpan(v); ok && len(want) > len(referenceUnclustered(s, v, span, choice)) {
				counts.clustered++
			}
		}
		route := referenceExportRoute(s, v, limits)
		if !reflect.DeepEqual(saved[v.Pod.ID].Route, route) {
			t.Fatalf("tick %d pod %s (%v): saved route %v, reference %v", s.tick, v.Pod.ID, v.Pod.Activity, saved[v.Pod.ID].Route, route)
		}
		if route != nil {
			counts.routes++
			counts.activities[v.Pod.Activity]++
		}
		for position := range v.Route {
			if position < len(v.blocks.lanes)-1 {
				if _, ok := v.blocks.lanes[position].cells.networkIndex(v.Route[position].ID); ok {
					counts.cellHits++
					continue
				}
			}
			counts.mapLookups++
		}
		// A route that the blocks do not cover takes the lookup of each
		// lane ID.
		other := s.vehicles[random.IntN(len(s.vehicles))].Route
		first := random.IntN(len(v.Route) + 1)
		if got, want := s.routeLaneIndexes(other, &v.blocks, first, limits.pod), referenceLaneIndexes(s, other, limits.pod); !reflect.DeepEqual(got, want) {
			t.Fatalf("tick %d pod %s: indexes %v of another route, reference %v", s.tick, v.Pod.ID, got, want)
		} else if len(other) > 0 && !slices.Equal(other, v.Route[first:]) {
			counts.mismatched++
		}
	}
}

// referenceUnclustered returns the envelope of referenceSpanEnvelope
// before rule 2.
func referenceUnclustered(s *Simulation, v *vehicle, span *reachSpan, choice envelopeChoice) []SafetyLocation {
	indexes := *s.networkIndexes
	indexes.clusterSafety = nil
	return referenceSpanEnvelope(&Simulation{networkIndexes: &indexes, vehicles: s.vehicles}, nil, v, span, choice)
}

// TestCheckLookupsMatchReference runs the shared-track preset with trips,
// Express parties, faults, and emergencies, and it compares the safety envelope and the
// saved route of each pod at each tick with the references from before the
// lookups by lane cells and by plane number.
func TestCheckLookupsMatchReference(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("the preset run runs without -race")
	}
	p := readSharedExpressProject(t)
	s, err := NewFleetWithOrderContract(p.Network, p.Fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	p.configure(t, s)
	if len(s.clusterSafety) == 0 || s.envelopes == nil {
		t.Fatal("the preset has no maneuver cluster or no envelope reach")
	}
	ticks := 9000
	if testing.Short() {
		ticks = 900
	}
	random := rand.New(rand.NewPCG(1, 2))
	counts := checkLookupCounts{activities: map[Activity]int{}}
	services := map[string]string{}
	for _, service := range p.ExpressServices {
		services[service.From] = service.ID
	}
	stations := s.network.Stations
	for range ticks {
		// The demand: a trip between two stations each second, and an
		// Express party each 20 s. A refused trip is part of the run.
		if s.tick%TicksPerSecond == 0 {
			from, to := stations[random.IntN(len(stations))].ID, stations[random.IntN(len(stations))].ID
			_, _ = s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: 1 + random.IntN(3), SharingConsent: SharedConsent})
		}
		if s.tick%(20*TicksPerSecond) == 0 {
			from, to := sharedExpressWaterloo, sharedExpressBank
			if s.tick%(40*TicksPerSecond) != 0 {
				from, to = to, from
			}
			_, _ = s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: 13, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: services[from]})
		}
		s.Step()
		checkLookupTick(t, s, random, &counts)
	}
	t.Logf("%+v", counts)
	if counts.envelopes == 0 || counts.clustered == 0 || counts.cellHits == 0 || counts.mismatched == 0 {
		t.Fatalf("the run lacks coverage: %+v", counts)
	}
	// Boarding, DepartingEmpty, and Continuing share one call of
	// routeLaneIndexes in exportPod. The short run has no DepartingEmpty pod.
	activities := []Activity{Boarding, Traveling}
	if !testing.Short() {
		activities = append(activities, DepartingEmpty)
	}
	for _, activity := range activities {
		if counts.activities[activity] == 0 {
			t.Fatalf("no saved route of a %v pod: %+v", activity, counts)
		}
	}
}

// TestRouteLaneIndexesWithoutPreparedCells checks the lookup of lane cells
// that have no index, and of a lane that the network does not hold: each
// takes the lookup of its ID, and an unknown lane gets index 0.
func TestRouteLaneIndexesWithoutPreparedCells(t *testing.T) {
	t.Parallel()
	p := readSharedExpressProject(t)
	s, err := NewFleetWithOrderContract(p.Network, p.Fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	route := slices.Clone(s.network.Lanes[:8])
	blocks, _ := s.routeBlocks(route)
	unindexed := blocks
	unindexed.lanes = slices.Clone(blocks.lanes)
	for index := range unindexed.lanes[:len(route)] {
		cells := *unindexed.lanes[index].cells
		cells.indexed = false
		unindexed.lanes[index].cells = &cells
	}
	unknown := slices.Clone(route)
	unknown[3].ID = "no-such-lane"
	for _, tc := range []struct {
		name   string
		route  []Lane
		blocks *blockList
		first  int
	}{
		{"prepared", route, &blocks, 0},
		{"unindexed", route, &unindexed, 0},
		{"shifted", route, &blocks, 1},
		{"unknown", unknown, &blocks, 0},
		{"nil blocks", route, nil, 0},
		{"no blocks", route, &blockList{}, 0},
	} {
		got, want := s.routeLaneIndexes(tc.route, tc.blocks, tc.first, len(tc.route)), referenceLaneIndexes(s, tc.route, len(tc.route))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: indexes %v, reference %v", tc.name, got, want)
		}
	}
	if s.routeLaneIndexes(route, &blocks, 0, len(route)-1) != nil || s.routeLaneIndexes(nil, &blocks, 0, 8) != nil {
		t.Fatal("a route that is empty or over the limit has indexes")
	}
}

// TestAppendSelectedMatchesReference compares appendSelected with its
// reference on random envelope reaches whose locations repeat planes, and
// whose clusters share slices and hold planes that no location has.
func TestAppendSelectedMatchesReference(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(3, 4))
	plane := func() SafetyLocation {
		return SafetyLocation{SeparationGroup: string(rune('a' + random.IntN(3))), From: string(rune('n' + random.IntN(4))), To: string(rune('n' + random.IntN(4)))}
	}
	repeated, clustered := 0, 0
	for trial := range 2000 {
		r := &envelopeReach{}
		for range 1 + random.IntN(12) {
			r.locations = append(r.locations, plane())
		}
		var clusters map[SafetyLocation][]SafetyLocation
		for range random.IntN(4) {
			var planes []SafetyLocation
			for range 1 + random.IntN(5) {
				if location := plane(); !slices.Contains(planes, location) {
					planes = append(planes, location)
				}
			}
			if clusters == nil {
				clusters = make(map[SafetyLocation][]SafetyLocation)
			}
			for range 1 + random.IntN(3) {
				clusters[r.locations[random.IntN(len(r.locations))]] = planes
			}
		}
		r.numberPlanes(clusters)
		n := &networkIndexes{envelopes: r, clusterSafety: clusters}
		for range 20 {
			var selected []int
			for range random.IntN(2 * len(r.locations)) {
				selected = append(selected, random.IntN(len(r.locations)))
			}
			prefix := []SafetyLocation{plane()}
			want := referenceSelected(n, slices.Clone(prefix), slices.Clone(selected))
			got := n.appendSelected(slices.Clone(prefix), slices.Clone(selected))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("trial %d: selected %v of %v with clusters %v: envelope %v, reference %v", trial, selected, r.locations, clusters, got, want)
			}
			before := referenceSelected(&networkIndexes{envelopes: r}, slices.Clone(prefix), slices.Clone(selected))
			if len(want) > len(before) {
				clustered++
			}
			if len(before)-1 < len(slices.Compact(slices.Sorted(slices.Values(selected)))) {
				repeated++
			}
		}
	}
	if repeated == 0 || clustered == 0 {
		t.Fatalf("the trials lack coverage: %d with a repeated plane, %d with rule 2", repeated, clustered)
	}
}

// referenceIndexSafety is indexSafety before the lane locations and the
// planes of rule 2 came from laneSafetyOf and clusterSafetyOf.
func referenceIndexSafety(n *networkIndexes) {
	n.laneSafety = make(map[string]SafetyLocation, len(n.network.Lanes))
	n.berthSafety = make(map[string]SafetyLocation)
	n.berthNodes = make(map[string]string)
	for _, lane := range n.network.Lanes {
		n.laneSafety[lane.ID] = SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}
	}
	for _, station := range n.network.Stations {
		for _, berth := range station.Berths {
			n.berthSafety[berth.ID] = SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}
			n.berthNodes[berth.ID] = berth.Node
		}
	}
	n.clusterSafety = nil
	shared := make(map[*int][]SafetyLocation)
	for index, cluster := range maneuverClusters(n.network) {
		if cluster == nil {
			continue
		}
		planes, done := shared[&cluster[0]]
		if !done {
			planes = make([]SafetyLocation, 0, len(cluster))
			for _, lane := range cluster {
				if location := n.laneSafety[n.network.Lanes[lane].ID]; !slices.Contains(planes, location) {
					planes = append(planes, location)
				}
			}
			shared[&cluster[0]] = planes
		}
		if n.clusterSafety == nil {
			n.clusterSafety = make(map[SafetyLocation][]SafetyLocation)
		}
		n.clusterSafety[n.laneSafety[n.network.Lanes[index].ID]] = planes
	}
}

// TestIndexSafetyMatchesReference compares indexSafety with its reference
// on the shared-track preset and on the London Express preset, which have
// maneuver groups.
func TestIndexSafetyMatchesReference(t *testing.T) {
	t.Parallel()
	for _, network := range []Network{sharedExpressNetwork(t), readLondonExpressProject(t).Network} {
		got, want := &networkIndexes{network: network}, &networkIndexes{network: network}
		got.indexSafety()
		referenceIndexSafety(want)
		if len(want.clusterSafety) == 0 {
			t.Fatal("the network has no maneuver cluster")
		}
		if !reflect.DeepEqual(got.laneSafety, want.laneSafety) || !reflect.DeepEqual(got.berthSafety, want.berthSafety) ||
			!reflect.DeepEqual(got.berthNodes, want.berthNodes) || !reflect.DeepEqual(got.clusterSafety, want.clusterSafety) {
			t.Fatal("the safety indexes differ from the reference")
		}
	}
}
