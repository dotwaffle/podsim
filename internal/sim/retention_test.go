package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

type chainSpec struct {
	length, speed float64
}

// straightChain returns the lanes l0, l1, ... on the X axis, from node n0
// at the origin.
func straightChain(specs ...chainSpec) Network {
	network := Network{Nodes: []Node{{ID: "n0"}}}
	x := 0.0
	for index, spec := range specs {
		x += spec.length
		network.Nodes = append(network.Nodes, Node{ID: fmt.Sprintf("n%d", index+1), Position: Point{X: x}})
		network.Lanes = append(network.Lanes, Lane{ID: fmt.Sprintf("l%d", index), From: fmt.Sprintf("n%d", index), To: fmt.Sprintf("n%d", index+1), SpeedLimit: spec.speed})
	}
	return network
}

// pathNetwork returns lanes p0, p1, ... at 2 m/s through the points in
// order.
func pathNetwork(points ...Point) Network {
	var network Network
	for index, point := range points {
		network.Nodes = append(network.Nodes, Node{ID: fmt.Sprintf("v%d", index), Position: point})
		if index > 0 {
			network.Lanes = append(network.Lanes, Lane{ID: fmt.Sprintf("p%d", index-1), From: fmt.Sprintf("v%d", index-1), To: fmt.Sprintf("v%d", index), SpeedLimit: 2})
		}
	}
	return network
}

func qualifyingLanes(network Network) map[string]float64 {
	retention := newLaneRetention(network, indexGeometryTails(network))
	got := make(map[string]float64)
	for _, lane := range network.Lanes {
		if value := retention.of(lane.ID); value > 0 {
			got[lane.ID] = value
		}
	}
	return got
}

func sameRetention(got, want map[string]float64) bool {
	return maps.EqualFunc(got, want, func(a, b float64) bool { return math.Abs(a-b) < 1e-9 })
}

func setSpeed(network Network, speed float64, ids ...string) Network {
	for index := range network.Lanes {
		for _, id := range ids {
			if network.Lanes[index].ID == id {
				network.Lanes[index].SpeedLimit = speed
			}
		}
	}
	return network
}

func TestReducedClearanceNumerics(t *testing.T) {
	t.Parallel()
	if reducedClearanceSpeed != math.Sqrt(14) {
		t.Fatalf("speed bound = %.17g", reducedClearanceSpeed)
	}
	for _, test := range []struct{ speed, want float64 }{
		{0, 5}, {1, 5.5}, {2, 7}, {math.Sqrt(14), 12}, {14, 12},
	} {
		if got := compactClearance(test.speed); got != test.want {
			t.Errorf("compactClearance(%g) = %.17g, want %g", test.speed, got, test.want)
		}
	}
	for _, test := range []struct{ up, turn, want float64 }{
		{1, 0, 6}, {2.25, 0, 7.25}, {3.5, 0, 8.5}, {49, 0, 8.5},
		{0, math.Pi / 2, 7.0710678118654755}, {1, math.Pi / 2, 8.899494936611665}, {3.5, math.Pi / 2, 13.470562748477143},
	} {
		if got := reducedRetention(test.up, test.turn); math.Abs(got-test.want) > 1e-12 {
			t.Errorf("reducedRetention(%g, %g) = %.17g, want %.17g", test.up, test.turn, got, test.want)
		}
	}
}

func TestLaneRetentionQualification(t *testing.T) {
	t.Parallel()
	curve := pathNetwork(Point{0, -100}, Point{0, 0}, Point{10, 10}, Point{110, 10})
	curve.Lanes[1].Control = &Point{0, 10}
	largeLane := straightChain(chainSpec{100, 2}, chainSpec{20, 2}, chainSpec{100, 2})
	largeLane.Lanes[2].VehicleClasses, _ = NewClassSet("legacy", "group")
	largeBerthChain := straightChain(chainSpec{100, 2}, chainSpec{20, 2}, chainSpec{100, 2})
	group, _ := NewClassSet("group")
	largeBerthChain.Stations = []Station{{ID: "large", VehicleClasses: group, Berths: []Berth{{ID: "large-1", Node: "n2", VehicleClasses: group}}}}
	diverge := straightChain(chainSpec{100, 2}, chainSpec{20, 2}, chainSpec{100, 2})
	diverge.Nodes = append(diverge.Nodes, Node{ID: "side", Position: Point{20, 100}})
	diverge.Lanes = append(diverge.Lanes, Lane{ID: "side", From: "n2", To: "side", SpeedLimit: 2})
	bend := 50 * math.Pi / 180
	sharp := pathNetwork(Point{-100, 0}, Point{0, 0}, Point{20, 0}, Point{20 + 10*math.Cos(bend), 10 * math.Sin(bend)},
		Point{20 + 10*math.Cos(bend) + 100*math.Cos(2*bend), 10*math.Sin(bend) + 100*math.Sin(2*bend)})
	fastBend := pathNetwork(Point{-100, 0}, Point{0, 0}, Point{20, 0}, Point{20, 100})
	fastBend.Lanes[0].SpeedLimit = 14
	ring := pathNetwork(Point{0, 0}, Point{20, 0}, Point{20, 20}, Point{0, 20})
	ring.Lanes = append(ring.Lanes, Lane{ID: "close", From: "v3", To: "v0", SpeedLimit: 2})
	below := math.Nextafter(math.Sqrt(14), 0)
	for _, test := range []struct {
		name    string
		network Network
		want    map[string]float64
	}{
		{"walk includes a faster predecessor", straightChain(chainSpec{100, 1}, chainSpec{10, 3}, chainSpec{20, 2}, chainSpec{100, 2}), map[string]float64{"l1": 7.25, "l2": 7.25}},
		{"walk stops at a long lane", straightChain(chainSpec{100, 14}, chainSpec{30, 2}, chainSpec{20, 2}, chainSpec{100, 2}), map[string]float64{"l1": 8.5, "l2": 6}},
		{"speed below the bound", straightChain(chainSpec{100, below}, chainSpec{20, below}, chainSpec{100, below}), map[string]float64{"l1": reducedRetention(stopDistance(below), 0)}},
		{"speed at the bound", straightChain(chainSpec{100, math.Sqrt(14)}, chainSpec{20, math.Sqrt(14)}, chainSpec{100, math.Sqrt(14)}), map[string]float64{}},
		{"preset speed", straightChain(chainSpec{100, 14}, chainSpec{20, 14}, chainSpec{100, 14}), map[string]float64{}},
		{"curve", curve, map[string]float64{}},
		{"large lane in the window", largeLane, map[string]float64{}},
		{"large berth on a window node", largeBerthChain, map[string]float64{}},
		{"diverge in the window", diverge, map[string]float64{}},
		{"right angle in the window", pathNetwork(Point{-100, 0}, Point{0, 0}, Point{20, 0}, Point{20, 100}), map[string]float64{"p1": reducedRetention(1, math.Pi/2)}},
		{"turn above a right angle", sharp, map[string]float64{}},
		{"retention not below Clearance", fastBend, map[string]float64{}},
		{"ring", ring, map[string]float64{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := qualifyingLanes(test.network); !sameRetention(got, test.want) {
				t.Fatalf("qualifying lanes = %v, want %v", got, test.want)
			}
		})
	}
	// A slow lane stops the walk, so the 3.7 m/s lane behind it does not
	// raise the bound, although its end is in the window.
	stopped := straightChain(chainSpec{100, 3.7}, chainSpec{0.2, 1}, chainSpec{3, 3.6}, chainSpec{20, 2}, chainSpec{100, 2})
	if got := qualifyingLanes(stopped)["l3"]; math.Abs(got-(5+stopDistance(3.6))) > 1e-12 {
		t.Fatalf("retention behind a stopped walk = %.17g", got)
	}
	if got := reducedRetention(1, math.Pi/2); math.Abs(got-(7/math.Cos(math.Pi/4)-1)) > 1e-12 {
		t.Fatalf("right-angle retention = %.17g", got)
	}
}

// TestLaneRetentionFixtureIdentity lists the qualifying lanes of the
// network fixtures of this package. internal/scenarios lists those of the
// presets, which have none.
func TestLaneRetentionFixtureIdentity(t *testing.T) {
	t.Parallel()
	line := setSpeed(lineNetwork(lineStations(2, 2, 2, 2)), 0.1, "s0-through")
	line.Lanes = append(line.Lanes, Lane{ID: "alternative", From: "s2-exit", To: "s1-entry", SpeedLimit: 5})
	for _, test := range []struct {
		name    string
		network Network
		want    map[string]float64
	}{
		{"example", Example(), map[string]float64{}},
		{"curved example", curvedExample(), map[string]float64{}},
		{"bank example", BankExample(), map[string]float64{}},
		{"merge corridor", mergeCorridor(true, 90), map[string]float64{}},
		{"lane speed exit", setSpeed(mergeCorridor(false, 0), 2.5, "exit"), map[string]float64{"exit": 8.5}},
		{"mixed-speed platoon approach", setSpeed(mergeCorridor(false, 0), 1, "approach"), map[string]float64{}},
		{"large motion link", setSpeed(largeMotionNetwork(false), 2.5, "a-link"), map[string]float64{}},
		{"preferred example", setSpeed(Example(), 0.1, "garden-through", "bypass-merge"), map[string]float64{}},
		{"preferred line", line, map[string]float64{}},
		{"intermediate berths", intermediateBerthNetwork(), map[string]float64{}},
	} {
		if got := qualifyingLanes(test.network); !sameRetention(got, test.want) {
			t.Errorf("%s: qualifying lanes = %v, want %v", test.name, got, test.want)
		}
	}
}

func TestRetentionReleaseDistances(t *testing.T) {
	t.Parallel()
	network := straightChain(chainSpec{100, 2}, chainSpec{20, 2}, chainSpec{100, 14})
	retention := newLaneRetention(network, indexGeometryTails(network))
	if got := retention.of("l1"); got != 6 {
		t.Fatalf("retention = %g", got)
	}
	track := resource{kind: trackResource, id: "l1"}
	if got := releaseDistance(track, releaseInput{from: "n1", start: 110, end: 120, retention: 6}); got != 126 {
		t.Fatalf("track release = %g", got)
	}
	if got := releaseDistance(resource{kind: nodeResource, id: "n1"}, releaseInput{from: "n1", start: 100, end: 110, retention: 6}); got != 106 {
		t.Fatalf("node release = %g", got)
	}
	s := &Simulation{networkIndexes: &networkIndexes{network: network, retention: retention}}
	s.laneCells = indexLaneCells(laneCellsIndexInput{network: network, retention: retention})
	if s.laneCells["l1"].retention != 6 || s.laneCells["l0"].retention != 0 || s.laneCells["l2"].retention != 0 {
		t.Fatal("lane cells do not hold the retention index")
	}
	blocks, _ := s.routeBlocks(network.Lanes)
	v := &vehicle{blocks: blocks}
	for _, b := range blocks.span(0, blocks.len()) {
		for _, r := range b.resources {
			v.retainRouteResource(r, resourceReleaseDistance(b, r))
		}
		if b.lane.ID == "l1" && v.routeReleases[resource{kind: trackResource, id: "l1", cell: b.cell}] != b.end+6 {
			t.Fatalf("cell %d releases at %g, want its end plus 6", b.cell, v.routeReleases[resource{kind: trackResource, id: "l1", cell: b.cell}])
		}
	}
	// Each node keeps the larger release of its two lanes.
	for node, want := range map[string]float64{"n1": 112, "n2": 132} {
		if got := v.routeReleases[resource{kind: nodeResource, id: node}]; got != want {
			t.Fatalf("node %s releases at %g, want %g", node, got, want)
		}
	}
	v.blocks, _ = s.routeBlocks(network.Lanes[1:])
	if got := v.originTail(); got != 6 {
		t.Fatalf("origin tail = %g", got)
	}
	v.Pod.Class = GroupClass
	if got := v.originTail(); got != largeClearance {
		t.Fatalf("large origin tail = %g", got)
	}
}

// depotChainNetwork adds to BankExample a straight depot chain of 1 m/s
// lanes, with slot nodes 11 m apart. The two middle lanes qualify with a
// retention of 5.25 m. With parallel, a second chain lies offset meters
// beside it. With berths, each slot node holds a berth of one station,
// which only the bank audit accepts: the berths of a station that is not
// a bank cannot be in series.
func depotChainNetwork(parallel bool, offset float64, berths bool) Network {
	network := BankExample()
	add := func(prefix string, y, side float64) {
		node := func(index int) string { return fmt.Sprintf("%s-d%d", prefix, index) }
		for index, x := range []float64{-100, 0, 11, 22, 122} {
			network.Nodes = append(network.Nodes, Node{ID: node(index), Position: Point{x, y}})
		}
		for index := range 4 {
			network.Lanes = append(network.Lanes, Lane{ID: fmt.Sprintf("%s-c%d", prefix, index), From: node(index), To: node(index + 1), SpeedLimit: 1})
		}
		network.Lanes = append(network.Lanes, Lane{ID: prefix + "-through", From: node(0), To: node(4), SpeedLimit: 14, Control: &Point{11, y + side*150}})
		if !berths {
			return
		}
		station := Station{ID: prefix, Name: prefix, Entry: node(0), Exit: node(4)}
		for index := 1; index <= 3; index++ {
			station.Berths = append(station.Berths, Berth{ID: fmt.Sprintf("%s-%d", prefix, index), Node: node(index)})
		}
		network.Stations = append(network.Stations, station)
	}
	add("depot", 5000, -1)
	if parallel {
		add("other", 5000+offset, 1)
	}
	return network
}

func TestRetentionBankAuditByPosition(t *testing.T) {
	t.Parallel()
	network := depotChainNetwork(false, 0, false)
	if got := qualifyingLanes(network); !sameRetention(got, map[string]float64{"depot-c1": 5.25, "depot-c2": 5.25}) {
		t.Fatalf("qualifying lanes = %v", got)
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	// The slot at d2 is 11 m from the start of the lead-out lane. The
	// window of depot-c1 certifies the first 13 m of that lane, the window
	// of depot-c2 the first 24 m, and the rest keeps Clearance. The lane
	// before the slots is 11 m from the lead-out lane too.
	r := p.retention
	pieces := r.pairPieces(auditMember{spans: r.nodeSpans("depot-d2")}, auditMember{spans: r.laneSpans("depot-c3"), length: 100, speed: 1})
	want := []pairPiece{{first: [2]float64{0, 0}, second: [2]float64{0, 13}, clearance: 5.5}, {first: [2]float64{0, 0}, second: [2]float64{13, 24}, clearance: 5.5}, {first: [2]float64{0, 0}, second: [2]float64{24, 100}, clearance: Clearance}}
	if fmt.Sprint(pieces) != fmt.Sprint(want) {
		t.Fatalf("slot pieces = %v, want %v", pieces, want)
	}
	pieces = r.pairPieces(auditMember{spans: r.laneSpans("depot-c1"), length: 11, speed: 1}, auditMember{spans: r.laneSpans("depot-c3"), length: 100, speed: 1})
	want = []pairPiece{{first: [2]float64{0, 11}, second: [2]float64{0, 13}, clearance: 5.5}, {first: [2]float64{0, 11}, second: [2]float64{13, 24}, clearance: 5.5}, {first: [2]float64{0, 11}, second: [2]float64{24, 100}, clearance: Clearance}}
	if fmt.Sprint(pieces) != fmt.Sprint(want) {
		t.Fatalf("lane pieces = %v, want %v", pieces, want)
	}
	// Berths on the slot nodes are 11 m apart, and 11 m from the lanes
	// beside them.
	if err := depotChainNetwork(false, 0, true).ValidateBankGeometry(); err != nil {
		t.Fatalf("slot berths: %v", err)
	}
	if err := depotChainNetwork(true, 7, true).ValidateBankGeometry(); err == nil || !strings.Contains(err.Error(), "are less than 12 meters apart") {
		t.Fatalf("parallel slot berths: %v", err)
	}
	// Two parallel chains share no window, so their pairs keep Clearance.
	for _, test := range []struct {
		offset float64
		refuse bool
	}{{7, true}, {11.9, true}, {12.5, false}} {
		_, err := PrepareNetwork(depotChainNetwork(true, test.offset, false))
		if refused := err != nil && strings.Contains(err.Error(), "are less than 12 meters apart"); refused != test.refuse {
			t.Fatalf("parallel chains %g m apart: %v", test.offset, err)
		}
	}
	short := depotChainNetwork(false, 0, false)
	for index := range short.Nodes {
		if short.Nodes[index].ID == "depot-d2" {
			short.Nodes[index].Position.X = 10.4
		}
	}
	if _, err := PrepareNetwork(short); err == nil || err.Error() != `lane "depot-c1" must be at least 10.5 meters long` {
		t.Fatalf("short qualifying lane: %v", err)
	}
}

// TestRetentionAuditMatchesObserver samples positions on each pair of depot
// paths. Where a piece of the bank audit needs less than Clearance, the
// safety observation certifies the two pods with at most that clearance.
func TestRetentionAuditMatchesObserver(t *testing.T) {
	t.Parallel()
	network := depotChainNetwork(true, 12.5, false)
	if err := depotChainNetwork(true, 12.5, true).ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	r := p.retention
	type path struct {
		lane   Lane
		node   string
		length float64
	}
	var paths []path
	for _, lane := range p.network.Lanes {
		if strings.HasPrefix(lane.ID, "depot-c") || strings.HasPrefix(lane.ID, "other-c") {
			paths = append(paths, path{lane: lane, length: p.network.Length(lane)})
		}
	}
	for _, node := range []string{"depot-d1", "depot-d2", "depot-d3", "other-d1", "other-d2", "other-d3"} {
		paths = append(paths, path{node: node})
	}
	member := func(item path) auditMember {
		if item.node != "" {
			return auditMember{spans: r.nodeSpans(item.node)}
		}
		return auditMember{spans: r.laneSpans(item.lane.ID), length: item.length, speed: item.lane.SpeedLimit}
	}
	chain := func(item path, distance float64) safetyChain {
		pod := Pod{LaneID: item.lane.ID, LaneDistance: distance, Speed: item.lane.SpeedLimit}
		if item.node != "" {
			pod = Pod{}
		}
		return safetyChain{positions: r.chainPositions(pod, item.node), speed: pod.Speed}
	}
	reduced := 0
	for i, first := range paths {
		for _, second := range paths[i+1:] {
			for _, piece := range r.pairPieces(member(first), member(second)) {
				if piece.clearance == Clearance {
					continue
				}
				reduced++
				for a := range 11 {
					for b := range 11 {
						x := piece.first[0] + float64(a)*(piece.first[1]-piece.first[0])/10
						y := piece.second[0] + float64(b)*(piece.second[1]-piece.second[0])/10
						clearance, ok := certifiedClearance(chain(first, x), chain(second, y))
						if !ok || clearance > piece.clearance {
							t.Fatalf("%v at %g and %v at %g: audit %g, observation %g %v", first, x, second, y, piece.clearance, clearance, ok)
						}
					}
				}
			}
		}
	}
	if reduced == 0 {
		t.Fatal("no piece needs less than Clearance")
	}
}

func corridorRestore(t *testing.T, network Network, pods []corridorPod) RestoreResult {
	t.Helper()
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index, pod := range pods {
		id := fmt.Sprintf("p%02d", index+1)
		fleet = append(fleet, Placement{ID: id, StationID: "dest", BerthID: fmt.Sprintf("dest-%02d", index+1)})
		state.RequestID++
		state.Boarded++
		route := make([]int, len(pod.route))
		for position, lane := range pod.route {
			route[position] = laneIndex[lane]
		}
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Activity: "traveling", Occupied: true, Origin: "origin-1", DestinationStation: pod.station,
			Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: pod.station, PartySize: 1, PodID: id}},
			Stops:  []string{pod.station},
			Route:  route, LaneID: pod.route[0], LaneDistance: pod.distance, Distance: pod.distance,
		})
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical {
		t.Fatalf("restore tier: %+v", result)
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	return result
}

// TestRetentionRestoreAgreement restores stopped pods on the qualifying
// exit lane. Two pods on one chain, 9.5 m apart across a cell boundary,
// restore in place: the retention releases the cell at its end plus 8.5 m,
// and the observation certifies them. Two pods on parallel chains 7 m apart
// share no window, so the later ID is demoted.
func TestRetentionRestoreAgreement(t *testing.T) {
	t.Parallel()
	network := setSpeed(mergeCorridor(false, 0), 2.5, "exit")
	route := []string{"exit", "approach"}
	result := corridorRestore(t, network, []corridorPod{
		{route: route, station: "dest", distance: 69},
		{route: route, station: "dest", distance: 59.5},
	})
	if len(result.Demoted) != 0 {
		t.Fatalf("one chain: %+v", result)
	}
	// The observer accepts this pair, but the leader still owns the cell
	// that ends at 60 m until it reaches 68.5 m. Restore also checks the
	// resources, so it demotes the follower.
	result = corridorRestore(t, network, []corridorPod{
		{route: route, station: "dest", distance: 66},
		{route: route, station: "dest", distance: 59.5},
	})
	if len(result.Demoted) != 1 || result.Demoted[0] != "p02" {
		t.Fatalf("leader cell: %+v", result)
	}
	parallel := network
	parallel.Nodes = append(slices.Clip(parallel.Nodes),
		Node{ID: "other-start", Position: Point{X: -corridorFeedLength, Y: 7}}, Node{ID: "other-merge", Position: Point{Y: 7}},
		Node{ID: "other-end", Position: Point{X: corridorExitLength, Y: 7}})
	parallel.Lanes = append(slices.Clip(parallel.Lanes),
		Lane{ID: "other-main", From: "other-start", To: "other-merge", SpeedLimit: 14},
		Lane{ID: "other-exit", From: "other-merge", To: "other-end", SpeedLimit: 2.5},
		Lane{ID: "other-approach", From: "other-end", To: "dest-entry", SpeedLimit: 14})
	// The other approach turns by a small angle toward the destination.
	turn := math.Atan2(7, corridorApproachLength)
	if got := qualifyingLanes(parallel); !sameRetention(got, map[string]float64{"exit": 8.5, "other-exit": reducedRetention(49, turn)}) {
		t.Fatalf("parallel qualifying lanes = %v", got)
	}
	result = corridorRestore(t, parallel, []corridorPod{
		{route: route, station: "dest", distance: 100},
		{route: []string{"other-exit", "other-approach"}, station: "dest", distance: 100},
	})
	if len(result.Demoted) != 1 || result.Demoted[0] != "p02" {
		t.Fatalf("parallel chains: %+v", result)
	}
}

func TestRetentionSafetyCertification(t *testing.T) {
	t.Parallel()
	check := func(network Network, pods []Pod) error {
		t.Helper()
		p, err := PrepareNetwork(network)
		if err != nil {
			t.Fatal(err)
		}
		s, err := p.NewFleet([]Placement{{ID: "x", StationID: "dest", BerthID: "dest-01"}})
		if err != nil {
			t.Fatal(err)
		}
		observation := SafetyObservation{Pods: pods, Locations: make(map[string]SafetyLocation)}
		for index, pod := range pods {
			pods[index].Position = network.Position(laneByID(network, pod.LaneID), pod.LaneDistance)
			observation.Locations[pod.ID] = s.laneSafety[pod.LaneID]
		}
		s.chainSafety(&observation)
		_, err = observation.Check()
		return err
	}
	qualifying := setSpeed(mergeCorridor(false, 0), 2.5, "exit")
	for _, test := range []struct {
		name    string
		network Network
		pods    []Pod
		fails   bool
	}{
		{"follower at rest", qualifying, []Pod{{ID: "a", LaneID: "exit", LaneDistance: 105.1, Speed: 2.5}, {ID: "b", LaneID: "exit", LaneDistance: 100}}, false},
		{"follower at rest too close", qualifying, []Pod{{ID: "a", LaneID: "exit", LaneDistance: 104.9}, {ID: "b", LaneID: "exit", LaneDistance: 100}}, true},
		{"moving follower", qualifying, []Pod{{ID: "a", LaneID: "exit", LaneDistance: 108.2}, {ID: "b", LaneID: "exit", LaneDistance: 100, Speed: 2.5}}, false},
		{"moving follower too close", qualifying, []Pod{{ID: "a", LaneID: "exit", LaneDistance: 108}, {ID: "b", LaneID: "exit", LaneDistance: 100, Speed: 2.5}}, true},
		{"follower on the upstream lane", qualifying, []Pod{{ID: "a", LaneID: "exit", LaneDistance: 3}, {ID: "b", LaneID: "main", LaneDistance: corridorFeedLength - 3}}, false},
		{"follower past the qualifying lane", qualifying, []Pod{{ID: "a", LaneID: "approach", LaneDistance: 7}, {ID: "b", LaneID: "approach", LaneDistance: 2}}, true},
		{"merge in the window", setSpeed(mergeCorridor(true, 90), 2.5, "exit"), []Pod{{ID: "a", LaneID: "exit", LaneDistance: 111.9}, {ID: "b", LaneID: "exit", LaneDistance: 100}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := check(test.network, test.pods)
			if _, ok := errors.AsType[*SeparationError](err); ok != test.fails || err != nil && !ok {
				t.Fatalf("check: %v", err)
			}
		})
	}
	// An observation without the window positions certifies no pair.
	pods := []Pod{{ID: "a", Position: Point{X: 6}}, {ID: "b"}}
	if _, err := (SafetyObservation{Pods: pods}).Check(); err == nil {
		t.Fatal("an observation without an index certified a pair")
	}
}

func laneByID(network Network, id string) Lane {
	for _, lane := range network.Lanes {
		if lane.ID == id {
			return lane
		}
	}
	return Lane{}
}

// TestSavedRouteOriginTail checks that a saved route keeps the lanes that
// the origin tail of the retained route still covers. The pod is 9 m into
// approach and released exit, the qualifying lane before it, at 8.5 m.
// approach does not qualify, so its origin tail is Clearance. A saved
// route that started at approach would put the pod short of that tail on
// a route that does not start at its origin, and the restore would demote
// it.
func TestSavedRouteOriginTail(t *testing.T) {
	t.Parallel()
	network := setSpeed(mergeCorridor(false, 0), 2.5, "exit")
	route := []string{"exit", "approach", "dest-berth-01-in"}
	indexes := make([]int, len(route))
	for position, id := range route {
		indexes[position] = slices.IndexFunc(network.Lanes, func(lane Lane) bool { return lane.ID == id })
	}
	state := SavedState{SharedRidePartyLimit: 1, RequestID: 1, Boarded: 1, Pods: []SavedPod{{
		ID: "p01", Activity: "traveling", Occupied: true, Origin: "origin-1", Destination: "dest-01", DestinationStation: "dest",
		Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "origin", To: "dest", PartySize: 1, PodID: "p01"}},
		Stops:  []string{"dest"},
		Route:  indexes, RouteIndex: 1, LaneID: "approach", LaneDistance: 9, Distance: corridorExitLength + 9,
	}}}
	fleet := []Placement{{ID: "p01", StationID: "dest", BerthID: "dest-02"}}
	for round := range 2 {
		s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
		if err != nil {
			t.Fatal(err)
		}
		pod := s.vehicles[0].Pod
		if result.Tier != RestorePhysical || len(result.Demoted) != 0 || pod.Activity != Traveling || pod.LaneID != "approach" || pod.LaneDistance != 9 {
			t.Fatalf("restore %d: %+v, pod on %q at %g", round, result, pod.LaneID, pod.LaneDistance)
		}
		state = s.ExportState()
		if saved := state.Pods[0]; len(saved.Route) != len(route) || saved.RouteIndex != 1 || saved.Distance != corridorExitLength+9 {
			t.Fatalf("restore %d: saved route %v at index %d, distance %g", round, saved.Route, saved.RouteIndex, saved.Distance)
		}
	}
}
