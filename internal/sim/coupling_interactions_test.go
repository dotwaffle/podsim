package sim

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// This is a stopped, aligned individual-owner fixture, not live recruitment.
func couplingReservationFixture(t *testing.T, occupied bool) couplingReservationInput {
	t.Helper()
	geometry := couplingGeometryFixture()
	compact := classBit(string(CompactClass))
	geometry.Network.Nodes = append(geometry.Network.Nodes,
		Node{ID: "origin-entry", Position: Point{X: -60, Y: 50}}, Node{ID: "origin-exit", Position: Point{X: -60, Y: -50}},
		Node{ID: "goal-front", Position: Point{X: 450, Y: 50}}, Node{ID: "goal-rear", Position: Point{X: 450, Y: -50}}, Node{ID: "goal-exit", Position: Point{X: 500}})
	geometry.Network.Stations = []Station{
		{ID: "origin", Name: "Origin", Entry: "origin-entry", Exit: "origin-exit", VehicleClasses: compact, Berths: []Berth{{ID: "origin-1", Node: "a", VehicleClasses: compact}}},
		{ID: "goal", Name: "Goal", Entry: "c", Exit: "goal-exit", VehicleClasses: compact, Berths: []Berth{{ID: "goal-front-1", Node: "goal-front", VehicleClasses: compact}, {ID: "goal-rear-1", Node: "goal-rear", VehicleClasses: compact}}},
	}
	for _, lane := range []Lane{
		{ID: "origin-in", From: "origin-entry", To: "a"}, {ID: "origin-out", From: "a", To: "origin-exit"},
		{ID: "origin-through", From: "origin-entry", To: "origin-exit"}, {ID: "goal-through", From: "c", To: "goal-exit"},
		{ID: "goal-front-in", From: "c", To: "goal-front"}, {ID: "goal-front-out", From: "goal-front", To: "goal-exit"},
		{ID: "goal-rear-in", From: "c", To: "goal-rear"}, {ID: "goal-rear-out", From: "goal-rear", To: "goal-exit"},
	} {
		lane.SpeedLimit = 7
		lane.VehicleClasses = compact
		geometry.Network.Lanes = append(geometry.Network.Lanes, lane)
	}
	prepared, err := PrepareNetwork(geometry.Network)
	if err != nil {
		t.Fatal(err)
	}
	geometry.Network = prepared.Network()
	count := prepared.laneCells["ab"].count()
	end := float64(2) * 200 / float64(count)
	geometry.Sites[0].RearStagingMeters = end
	geometry.Sites[0].FrontStagingMeters = end + 12
	geometry.Sites[0].EndMeters = 120
	n, err := prepareCouplingReservations(prepared, geometry)
	if err != nil {
		t.Fatal(err)
	}
	input := couplingReservationInput{Network: n, Prepared: prepared, CorridorID: "corridor", Tick: 10, Owners: make(map[resource]resourceOwner)}
	for i, id := range []string{"front", "rear"} {
		distance, cell := end+12, 2
		if i == 1 {
			distance, cell = end, 1
		}
		member := couplingMemberSnapshot{Distance: distance, BlockIndex: cell, ReservedThrough: cell, DestinationStation: "goal",
			Origin: geometry.Network.Stations[0].Berths[0], Destination: geometry.Network.Stations[1].Berths[i], Retained: make(map[resource]float64)}
		member.Vehicle = Vehicle{Pod: Pod{ID: id, Class: CompactClass, Activity: Traveling, LaneID: "ab", LaneDistance: distance, Position: Point{X: distance}},
			Route: cloneLanes(geometry.Network.Lanes[:2]), RelocatingTo: "goal"}
		exitID := "goal-front-in"
		if i == 1 {
			exitID = "goal-rear-in"
		}
		for _, lane := range geometry.Network.Lanes {
			if lane.ID == exitID {
				member.Vehicle.Route = append(member.Vehicle.Route, lane)
			}
		}
		for _, r := range berthResources(member.Destination) {
			input.Owners[r] = resourceOwner{kind: podOwnerKind, id: id}
		}
		if occupied {
			member.Vehicle.Pod.Activity, member.Vehicle.Pod.Occupied = Traveling, true
			member.Vehicle.RelocatingTo = ""
			member.Vehicle.Stops = []string{"goal"}
			member.Vehicle.Riders = []Request{{ID: i + 1, From: "origin", To: "goal", PartySize: 1, PodID: id, SharingConsent: PrivateConsent,
				Service: OnDemandService, RequestedTick: 1, BoardedTick: 2}}
		}
		r := resource{kind: trackResource, id: "ab", cell: cell}
		member.Retained[r] = float64(cell+1)*200/float64(count) + 12
		input.Owners[r] = resourceOwner{kind: podOwnerKind, id: id}
		input.Members[i] = member
	}
	return input
}

func TestCouplingReservationPreparationIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		change    func(*CouplingGeometryInput)
		wantError bool
	}{
		{name: "same network"},
		{name: "changed coordinate", wantError: true, change: func(input *CouplingGeometryInput) { input.Network.Nodes[1].Position.Y++ }},
		{name: "unknown marker", wantError: true, change: func(input *CouplingGeometryInput) { input.Contract = "unknown" }},
		{name: "empty explicit marker", change: func(input *CouplingGeometryInput) { input.Sites, input.Corridors = nil, nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingGeometryFixture()
			prepared, err := PrepareNetwork(input.Network)
			if err != nil {
				t.Fatal(err)
			}
			input.Network = prepared.Network()
			if test.change != nil {
				test.change(&input)
			}
			before := input.Network.clone()
			preparedBefore := newPreparedNetwork(prepared.network.clone(), newRouteGraph(prepared.network))
			n, err := prepareCouplingReservations(prepared, input)
			if (err != nil) != test.wantError {
				t.Fatalf("prepare error=%v, want error %v", err, test.wantError)
			}
			if !reflect.DeepEqual(before, input.Network) {
				t.Fatal("preparation mutated caller network")
			}
			if !couplingPreparedEqual(t, preparedBefore, prepared) {
				t.Fatal("preparation changed immutable prepared records")
			}

			if err == nil && len(input.Corridors) > 0 {
				input.Corridors[0].LaneIDs[0] = "changed"
				if n.corridors["corridor"].LaneIDs[0] != "ab" {
					t.Fatal("prepared descriptors alias caller slices")
				}
			}
		})
	}
}

func TestCouplingInteractionIndexCoverage(t *testing.T) {
	t.Parallel()
	input := couplingReservationFixture(t, false)
	n := input.Network
	for _, box := range []couplingBox{
		{low: Point{X: 40, Y: -1}, high: Point{X: 90, Y: 1}},
		{low: Point{X: 190, Y: -30}, high: Point{X: 210, Y: 30}},
		{low: Point{X: 350, Y: -6}, high: Point{X: 410, Y: 6}},
	} {
		found := make(map[int]bool)
		if err := n.index.visit(box, func(index int) error { found[index] = true; return nil }); err != nil {
			t.Fatal(err)
		}
		for i, obstacle := range n.obstacles {
			b := couplingBox{low: Point{X: math.Min(obstacle.from.X, obstacle.to.X) - obstacle.clearance, Y: math.Min(obstacle.from.Y, obstacle.to.Y) - obstacle.clearance}, high: Point{X: math.Max(obstacle.from.X, obstacle.to.X) + obstacle.clearance, Y: math.Max(obstacle.from.Y, obstacle.to.Y) + obstacle.clearance}}
			intersects := b.low.X <= box.high.X && box.low.X <= b.high.X && b.low.Y <= box.high.Y && box.low.Y <= b.high.Y
			if intersects && !found[i] {
				t.Fatalf("index omitted raw obstacle %d", i)
			}
		}
	}
}

func TestCouplingReservationUnconnectedCrossing(t *testing.T) {
	t.Parallel()
	base := couplingGeometryFixture()
	for _, test := range []struct {
		name    string
		classes ClassSet
		group   string
	}{
		{name: "Compact", classes: classBit(string(CompactClass))},
		{name: "Legacy", classes: classBit(string(LegacyClass))},
		{name: "Group", classes: classBit(string(GroupClass))},
		{name: "Express", classes: classBit(string(ExpressClass))},
		{name: "plane label cannot waive XY", classes: classBit(string(CompactClass)), group: "other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := base
			input.Network = base.Network.clone()
			input.Network.Nodes = append(input.Network.Nodes, Node{ID: "north", Position: Point{X: 150, Y: 100}}, Node{ID: "south", Position: Point{X: 150, Y: -100}})
			input.Network.Lanes = append(input.Network.Lanes, Lane{ID: "crossing", From: "north", To: "south", SpeedLimit: 7, VehicleClasses: test.classes, SeparationGroup: test.group})
			prepared, err := PrepareNetwork(input.Network)
			if largeClassSet(test.classes) {
				// Existing native geometry rejects this large crossing before planning.
				if err == nil || !strings.Contains(err.Error(), "nonincident paths") || !strings.Contains(err.Error(), "less than 20 meters") {
					t.Fatalf("large native crossing preflight error=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			input.Network = prepared.Network()
			n, err := prepareCouplingReservations(prepared, input)
			if err != nil {
				t.Fatal(err)
			}
			blocks, err := n.routeBlocks(input.Network.Lanes[:2])
			if err != nil {
				t.Fatal(err)
			}
			if err := n.proveInteractions(&blocks, 0, blocks.len()-1); !errors.Is(err, errCouplingReservationDenied) {
				t.Fatalf("unconnected crossing error=%v", err)
			}
		})
	}
}

func TestCouplingIncidentResourcesAndLargeTail(t *testing.T) {
	t.Parallel()
	for _, class := range []VehicleClass{CompactClass, LegacyClass, GroupClass, ExpressClass} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			geometry := couplingGeometryFixture()
			geometry.Network.Nodes = append(geometry.Network.Nodes, Node{ID: "north", Position: Point{X: 400, Y: 150}})
			geometry.Network.Lanes = append(geometry.Network.Lanes, Lane{ID: "branch", From: "c", To: "north", SpeedLimit: 7, VehicleClasses: classBit(string(class))})
			p, err := PrepareNetwork(geometry.Network)
			if err != nil {
				t.Fatal(err)
			}
			geometry.Network = p.Network()
			n, err := prepareCouplingReservations(p, geometry)
			if err != nil {
				t.Fatal(err)
			}
			blocks, err := n.routeBlocks(geometry.Network.Lanes[:2])
			if err != nil {
				t.Fatal(err)
			}
			if err := n.proveInteractions(&blocks, 0, blocks.len()-1); err != nil {
				t.Fatal(err)
			}
			if class == GroupClass || class == ExpressClass {
				if p.laneCells["bc"].tail < 20 {
					t.Fatal("large interface tail was reduced")
				}
			}
			last := blocks.at(blocks.len() - 1)
			if !slices.Contains(last.resources, resource{kind: junctionResource, id: "c"}) {
				t.Fatal("incident conflict has no common junction resource")
			}
		})
	}
}

// Use endpoint and projection arithmetic independently of the production distance helper.
func TestCouplingSegmentOracle(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		a, b, c, d Point
		want       float64
	}{
		{name: "cross", a: Point{X: -10}, b: Point{X: 10}, c: Point{Y: -10}, d: Point{Y: 10}},
		{name: "parallel", a: Point{}, b: Point{X: 10}, c: Point{Y: 7}, d: Point{X: 10, Y: 7}, want: 7},
		{name: "point", a: Point{}, b: Point{}, c: Point{X: 3, Y: 4}, d: Point{X: 3, Y: 4}, want: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := couplingSegmentsDistance(test.a, test.b, test.c, test.d); math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("distance=%v want %v", got, test.want)
			}
		})
	}
}

func TestCouplingForeignResourceReleaseCoverage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                    string
		kind                    resourceKind
		targetTail, foreignTail float64
		wantError               bool
	}{
		{name: "distant shared node releases before cell clears", kind: nodeResource, wantError: true},
		{name: "large partial source tail", kind: nodeResource, targetTail: 20, foreignTail: 10, wantError: true},
		{name: "whole junction cells", kind: junctionResource},
		{name: "whole node cells", kind: nodeResource, targetTail: 30, foreignTail: 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := resource{kind: test.kind, id: "shared"}
			b := block{lane: Lane{ID: "target", From: "shared"}, start: 0, end: 30, fromTail: test.targetTail, resources: []resource{r}}
			obstacle := couplingObstacle{lane: "foreign", cell: 0, from: Point{X: 15, Y: -15}, to: Point{X: 15, Y: 15}, clearance: 20, resources: []resource{r},
				release: releaseInput{from: "shared", start: 0, end: 30, fromTail: test.foreignTail}}
			n := new(couplingReservationNetwork)
			if err := n.proveCellInteraction(b, Point{}, Point{X: 30}, 0, obstacle); (err != nil) != test.wantError {
				t.Fatalf("cell release coverage error=%v want error %v", err, test.wantError)
			}
		})
	}
}

func TestCouplingUncoveredParallelAndCurve(t *testing.T) {
	t.Parallel()
	for _, curve := range []bool{false, true} {
		t.Run(map[bool]string{false: "close parallel", true: "canonical curved crossing"}[curve], func(t *testing.T) {
			t.Parallel()
			geometry := couplingGeometryFixture()
			a, b := Point{Y: 5}, Point{X: 400, Y: 5}
			var control *Point
			if curve {
				a, b = Point{X: 50, Y: 100}, Point{X: 250, Y: 100}
				control = &Point{X: 150, Y: -100}
			}
			geometry.Network.Nodes = append(geometry.Network.Nodes, Node{ID: "foreign-from", Position: a}, Node{ID: "foreign-to", Position: b})
			geometry.Network.Lanes = append(geometry.Network.Lanes, Lane{ID: "foreign", From: "foreign-from", To: "foreign-to", Control: control, SpeedLimit: 7, VehicleClasses: classBit(string(CompactClass))})
			p, err := PrepareNetwork(geometry.Network)
			if err != nil {
				t.Fatal(err)
			}
			geometry.Network = p.Network()
			n, err := prepareCouplingReservations(p, geometry)
			if err != nil {
				t.Fatal(err)
			}
			blocks, err := n.routeBlocks(geometry.Network.Lanes[:2])
			if err != nil {
				t.Fatal(err)
			}
			if err := n.proveInteractions(&blocks, 0, blocks.len()-1); err == nil {
				t.Fatal("uncovered foreign geometry passed")
			}
			if curve && len(p.geometry["foreign"].segments) != 64 {
				t.Fatal("foreign curve did not use canonical motion segments")
			}
		})
	}
}

func TestCouplingDistantSharedNodeDoesNotCoverParallelCells(t *testing.T) {
	t.Parallel()
	geometry := couplingGeometryFixture()
	geometry.Network.Nodes = append(geometry.Network.Nodes, Node{ID: "foreign-end", Position: Point{X: 400, Y: 5}})
	geometry.Network.Lanes = append(geometry.Network.Lanes, Lane{ID: "foreign", From: "a", To: "foreign-end", SpeedLimit: 7, VehicleClasses: classBit(string(CompactClass)), SeparationGroup: "other"})
	p, err := PrepareNetwork(geometry.Network)
	if err != nil {
		t.Fatal(err)
	}
	geometry.Network = p.Network()
	n, err := prepareCouplingReservations(p, geometry)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := n.routeBlocks(geometry.Network.Lanes[:2])
	if err != nil {
		t.Fatal(err)
	}
	first := blocks.at(0)
	if !slices.Contains(first.resources, resource{kind: nodeResource, id: "a"}) || !slices.Contains(p.laneCells["foreign"].cell(0), resource{kind: nodeResource, id: "a"}) {
		t.Fatal("fixture lacks the distant shared node")
	}
	if err := n.proveInteractions(&blocks, 0, blocks.len()-1); err == nil {
		t.Fatal("a shared resource elsewhere covered unprotected parallel cells")
	}
}

// Compare every immutable record. Validation has already proved nil bank errors.
func couplingPreparedEqual(t *testing.T, a, b *PreparedNetwork) bool {
	t.Helper()
	if a.graph.banks.err != nil || b.graph.banks.err != nil {
		t.Fatal("prepared graph retains a bank error")
	}
	fields := []struct {
		name          string
		first, second any
	}{
		{"network", a.network, b.network}, {"station indexes", a.stationIndexes, b.stationIndexes},
		{"station exclusions", a.stationForbidden, b.stationForbidden}, {"geometry", a.geometry, b.geometry},
		{"conflicts", a.junctionConflicts, b.junctionConflicts}, {"berth resources", a.berthResources, b.berthResources},
		{"cells", a.laneCells, b.laneCells}, {"resource lanes", a.resourceLanes, b.resourceLanes}, {"lane safety", a.laneSafety, b.laneSafety}, {"berth safety", a.berthSafety, b.berthSafety},
		{"graph nodes", a.graph.nodes, b.graph.nodes}, {"graph lanes", a.graph.lanes, b.graph.lanes},
		{"outgoing", a.graph.outgoing, b.graph.outgoing}, {"incoming", a.graph.incoming, b.graph.incoming},
		{"lengths", a.graph.lengths, b.graph.lengths}, {"edges", a.graph.edges, b.graph.edges},
		{"berth stations", a.graph.berthStations, b.graph.berthStations}, {"berth nodes", a.graph.berthNodes, b.graph.berthNodes},
		{"node classes", a.graph.nodeClasses, b.graph.nodeClasses}, {"lane classes", a.graph.laneClasses, b.graph.laneClasses},
		{"class restrictions", a.graph.classRestrictions, b.graph.classRestrictions},
		{"banks", a.graph.banks.banks, b.graph.banks.banks}, {"bank nodes", a.graph.banks.nodes, b.graph.banks.nodes}, {"bank lanes", a.graph.banks.lanes, b.graph.banks.lanes},
	}
	for _, field := range fields {
		if !reflect.DeepEqual(field.first, field.second) {
			t.Log("prepared record changed:", field.name)
			return false
		}
	}
	return true
}

func TestCouplingReservationExternalPreparedIdentity(t *testing.T) {
	t.Parallel()
	formation := couplingReservationFixture(t, false)
	n := formation.Network
	input := CouplingGeometryInput{Contract: n.contract, Network: formation.Prepared.Network(),
		Sites:     []CouplingSite{n.sites["assembly"], n.sites["split"]},
		Corridors: []CouplingCorridor{n.corridors["corridor"]}}
	input.Corridors[0].LaneIDs = slices.Clone(input.Corridors[0].LaneIDs)
	for i := range input.Network.Nodes {
		if input.Network.Nodes[i].ID == "origin-entry" {
			input.Network.Nodes[i].Position.Y++
		}
	}
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal("external-only change must preserve valid authored geometry", err)
	}
	before := input.Network.clone()
	preparedBefore := newPreparedNetwork(formation.Prepared.network.clone(), newRouteGraph(formation.Prepared.network))
	if _, err := prepareCouplingReservations(formation.Prepared, input); !errors.Is(err, errCouplingReservationDenied) {
		t.Fatalf("external authored/prepared mismatch error=%v", err)
	}
	if !reflect.DeepEqual(before, input.Network) || !couplingPreparedEqual(t, preparedBefore, formation.Prepared) {
		t.Fatal("external identity denial changed authored or prepared records")
	}
}
