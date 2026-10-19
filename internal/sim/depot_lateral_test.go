package sim

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
)

// narrowDepot returns the depot fixture of 3 lanes of 4 slots with lanes
// 6 m apart and the throat at throat m/s.
func narrowDepot(throat float64) Network {
	return depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 6, width: 6, throat: throat})
}

// lateralIndex returns the depot index and the lateral index of a network.
func lateralIndex(t *testing.T, network Network) (*depotIndex, *depotLateral) {
	t.Helper()
	nodes, err := network.validateNodes()
	if err != nil {
		t.Fatal(err)
	}
	depots, err := newDepotIndex(network, nodes)
	if err != nil {
		t.Fatal(err)
	}
	return depots, newDepotLateral(network, depots, indexGeometryTails(network), indexBerthKeys(network))
}

// lateralMembers returns the members of the first depot of a network by
// the ID of the lane or the berth.
func lateralMembers(t *testing.T, network Network) map[string]lateralMember {
	t.Helper()
	depots, _ := lateralIndex(t, network)
	members := newLateralBuilder(network).members(0, depots.depots[0], indexGeometryTails(network), indexBerthKeys(network))
	byID := make(map[string]lateralMember)
	for _, member := range members {
		id := network.Lanes[member.key.index].ID
		if member.key.kind == berthKeyKind {
			id = member.from
		}
		byID[id] = member
	}
	return byID
}

// lateralPair returns the clearance of two lanes or slots of a network,
// found by ID.
func lateralPair(network Network, lateral *depotLateral, first, second string) (float64, bool) {
	key := func(id string) separationKey {
		if index := networkLaneIndex(network, id); index >= 0 {
			return laneKey(index)
		}
		return berthKey(indexBerthKeys(network)[id])
	}
	return lateral.clearance(key(first), key(second))
}

// TestLateralClearance checks W(v) = 3.1 + v²/2 and the body of the sweep:
// its width is W, and each end grows by the same amount as each side.
func TestLateralClearance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ speed, want float64 }{{0, 3.1}, {depotSpeed, 4.09}, {2, 5.1}, {math.Sqrt(5.8), 6}} {
		if got := lateralClearance(test.speed); math.Abs(got-test.want) > 1e-9 {
			t.Errorf("W(%g) = %g, want %g", test.speed, got, test.want)
		}
		body := lateralBody(test.speed)
		grow := (1 + 2*stopDistance(test.speed)) / 2
		want := [4]Point{{-2.5 - grow, -1.05 - grow}, {3.7 + grow, -1.05 - grow}, {3.7 + grow, 1.05 + grow}, {-2.5 - grow, 1.05 + grow}}
		for index, corner := range body.corners {
			if math.Abs(corner.X-want[index].X) > 1e-12 || math.Abs(corner.Y-want[index].Y) > 1e-12 {
				t.Errorf("speed %g: corners %v, want %v", test.speed, body.corners, want)
			}
		}
		if width := body.corners[2].Y - body.corners[1].Y; math.Abs(width-lateralClearance(test.speed)) > 1e-12 {
			t.Errorf("speed %g: body width %g", test.speed, width)
		}
	}
}

// TestLateralCandidate checks each clause of a lateral pair before the
// sweep, on members of the 6 m depot fixture.
func TestLateralCandidate(t *testing.T) {
	t.Parallel()
	members := lateralMembers(t, narrowDepot(depotSpeed))
	base, other := members["parking-in-1"], members["parking-in-2"]
	if !lateralCandidate(&base, &other) {
		t.Fatal("the two lead-ins are not a candidate")
	}
	slot := members[depotSlotID(2, 1)]
	if !lateralCandidate(&base, &slot) || slot.speed != 0 {
		t.Fatalf("a lead-in and a slot of another lane: speed %g", slot.speed)
	}
	for _, test := range []struct {
		name   string
		change func(a, b *lateralMember)
	}{
		{"one lane path", func(a, b *lateralMember) { b.path = a.path }},
		{"two depots", func(a, b *lateralMember) { b.depot = a.depot + 1 }},
		{"shared start", func(a, b *lateralMember) { b.from = a.from }},
		{"start at the end", func(a, b *lateralMember) { b.to = a.from }},
		{"end at the start", func(a, b *lateralMember) { b.from = a.to }},
		{"shared end", func(a, b *lateralMember) { b.to = a.to }},
		{"large or tail first", func(a, _ *lateralMember) { a.small = false }},
		{"large or tail second", func(_, b *lateralMember) { b.small = false }},
		{"fast first", func(a, _ *lateralMember) { a.speed = reducedClearanceSpeed }},
		{"fast second", func(_, b *lateralMember) { b.speed = reducedClearanceSpeed }},
		{"no limit", func(a, _ *lateralMember) { a.speed = math.NaN() }},
	} {
		a, b := base, other
		test.change(&a, &b)
		if lateralCandidate(&a, &b) {
			t.Errorf("%s: a candidate", test.name)
		}
	}
	slow := other
	slow.speed = math.Nextafter(reducedClearanceSpeed, 0)
	if !lateralCandidate(&base, &slow) {
		t.Error("a lane just below the gate is not a candidate")
	}
	// A large class or a geometry tail makes a lane not small. A lane that
	// admits a large class has a tail too, so the class test uses no
	// tails.
	network := narrowDepot(depotSpeed)
	findLane(&network, "parking-in-2").VehicleClasses = ClassSet(1<<2) | ClassSet(1<<0)
	large, _ := lateralIndex(t, network)
	for _, member := range newLateralBuilder(network).members(0, large.depots[0], nil, indexBerthKeys(network)) {
		if member.key.kind == laneKeyKind && network.Lanes[member.key.index].ID == "parking-in-2" && member.small {
			t.Error("a lane that admits a large class is small")
		}
	}
	tails := indexGeometryTails(narrowDepot(depotSpeed))
	tails["parking-in-2"] = geometryTails{tail: 1}
	plain := narrowDepot(depotSpeed)
	depots, _ := lateralIndex(t, plain)
	for _, member := range newLateralBuilder(plain).members(0, depots.depots[0], tails, indexBerthKeys(plain)) {
		if member.key.kind == laneKeyKind && plain.Lanes[member.key.index].ID == "parking-in-2" && member.small {
			t.Error("a lane with a geometry tail is small")
		}
	}
}

// lateralSteps returns the steps of the lane with an ID in a network.
func lateralSteps(network Network, id string) []sweepStep {
	b := newLateralBuilder(network)
	lane := network.Lanes[networkLaneIndex(network, id)]
	return b.steps(lane, b.points(lane))
}

// sweepStatusOf sweeps two step lists with the body of a speed.
func sweepStatusOf(first, second []sweepStep, speed float64) sweepStatus {
	visits := 0
	return sweepPair(first, second, lateralBody(speed), lateralOptions, &visits)
}

// TestLateralSweep checks the moving-pair sweep: straight lanes just
// farther and just closer than W, the front corner at the end of a sharp
// fan and the rear overhang at the start of a throat, both with centerlines
// at least W apart, a body that only its turn brings near the other, and a
// body that passes the other only in the middle of its step.
func TestLateralSweep(t *testing.T) {
	t.Parallel()
	speed := depotSpeed
	w := lateralClearance(speed)
	straight := func(y float64) []sweepStep {
		return []sweepStep{{start: sweepPose{x: -50, y: y}, dx: 100}}
	}
	if got := sweepStatusOf(straight(0), straight(w+1e-3), speed); got != sweepClear {
		t.Errorf("straight lanes at W + 1 mm: %v", got)
	}
	if got := sweepStatusOf(straight(0), straight(w-1e-3), speed); got == sweepClear {
		t.Error("straight lanes at W - 1 mm are clear")
	}
	curve := func(from, control, to Point, neighbor float64) (Network, float64) {
		network := Network{
			Nodes: []Node{{ID: "a", Position: from}, {ID: "b", Position: to}, {ID: "c", Position: Point{X: -60, Y: neighbor}}, {ID: "d", Position: Point{X: 60, Y: neighbor}}},
			Lanes: []Lane{{ID: "curve", From: "a", To: "b", SpeedLimit: speed, Control: &control}, {ID: "straight", From: "c", To: "d", SpeedLimit: speed}},
		}
		b := newLateralBuilder(network)
		gap := math.Inf(1)
		points, line := b.points(network.Lanes[0]), b.points(network.Lanes[1])
		for i := 1; i < len(points); i++ {
			gap = min(gap, bankSegmentGap(points[i-1], points[i], line[0], line[1], math.Inf(1)))
		}
		return network, gap
	}
	// The fan ends at the origin, heading east, after it rises 20 m in
	// 10 m. Its front corner rises above the end pose before the end.
	fan, gap := curve(Point{X: -10, Y: -20}, Point{X: -5, Y: 0}, Point{}, w+0.01)
	if gap < w {
		t.Fatalf("fan centerline gap %g", gap)
	}
	if got := sweepStatusOf(lateralSteps(fan, "curve"), lateralSteps(fan, "straight"), speed); got == sweepClear {
		t.Error("the front corner of a sharp fan end is clear")
	}
	// The throat starts at the origin, heading east, and falls 4 m in 20
	// m. Its rear overhang rises by about 0.01 * rear². With a rear of
	// 1.3 m, as for the maneuver body, the sweep would clear it.
	throat, gap := curve(Point{}, Point{X: 10, Y: 0}, Point{X: 20, Y: -4}, w+0.1)
	if gap < w {
		t.Fatalf("throat centerline gap %g", gap)
	}
	if got := sweepStatusOf(lateralSteps(throat, "curve"), lateralSteps(throat, "straight"), speed); got == sweepClear {
		t.Error("the rear overhang at a throat start is clear")
	}
	short := newSweepBody(sweepRear+(w/2-sweepHalfWidth), sweepFront+(w/2-sweepHalfWidth), w/2)
	visits := 0
	if got := sweepPair(lateralSteps(throat, "curve"), lateralSteps(throat, "straight"), short, lateralOptions, &visits); got != sweepClear {
		t.Errorf("the throat with a rear of 1.3 m: %v", got)
	}
	// A body turns a quarter turn at the origin. A parked body on the
	// diagonal, 5.05 m from the origin, meets only the turn.
	turn := []sweepStep{rotation(Point{}, 0, math.Pi/2)}
	far := lateralBody(speed).reach
	if far <= 5.05 {
		t.Fatalf("reach %g", far)
	}
	behind := 5.05 + lateralRear + (w/2 - sweepHalfWidth)
	parked := []sweepStep{{start: sweepPose{x: behind * math.Sqrt2 / 2, y: behind * math.Sqrt2 / 2, heading: math.Pi / 4}}}
	ends := []sweepStep{{start: sweepPose{}}, {start: sweepPose{heading: math.Pi / 2}}}
	if got := sweepStatusOf(parked, ends, speed); got != sweepClear {
		t.Fatalf("the parked body meets an end pose of the turn: %v", got)
	}
	if got := sweepStatusOf(parked, turn, speed); got == sweepClear {
		t.Error("a turn of the second body is clear")
	}
	if got := sweepStatusOf(turn, parked, speed); got == sweepClear {
		t.Error("a turn of the first body is clear")
	}
	// A body passes a parked body only in the middle of its step.
	beside := []sweepStep{{start: sweepPose{}}}
	if got := sweepStatusOf(beside, straight(w-0.5), speed); got == sweepClear {
		t.Error("a body that passes in the middle of a step is clear")
	}
}

// TestDepotLateralPairs checks the lateral pairs of the 6 m depot fixture:
// the lead-ins of neighbor lanes, a fan end beside the lead-in of the next
// lane, and two slots, at W of the higher speed limit; no pair with a fast
// throat or with lanes 4 m apart; no pair with a shared node; no pair that
// one window also holds; and symmetric neighbors in lane order.
func TestDepotLateralPairs(t *testing.T) {
	t.Parallel()
	network := narrowDepot(2)
	_, lateral := lateralIndex(t, network)
	if lateral.unresolved != 0 {
		t.Fatalf("%d unresolved pairs", lateral.unresolved)
	}
	for _, pair := range [][2]string{{"parking-in-1", "parking-in-2"}, {"parking-fan-1", "parking-in-2"}, {"parking-in-2", "parking-fan-3"}, {"parking-chain-1-1", depotSlotID(2, 1)}} {
		if got, ok := lateralPair(network, lateral, pair[0], pair[1]); !ok || got != lateralClearance(depotSpeed) {
			t.Errorf("%v: %g %v", pair, got, ok)
		}
	}
	if got, ok := lateralPair(network, lateral, depotSlotID(1, 2), depotSlotID(2, 2)); !ok || got != lateralClearance(0) {
		t.Errorf("two slots: %g %v", got, ok)
	}
	if got, ok := lateralPair(network, lateral, "parking-throat-1", "parking-out-2"); !ok || got != lateralClearance(2) {
		t.Errorf("throat at 2 m/s and lead-out: %g %v, want %g", got, ok, lateralClearance(2))
	}
	fast := narrowDepot(0)
	_, fastLateral := lateralIndex(t, fast)
	if _, ok := lateralPair(fast, fastLateral, "parking-throat-1", "parking-out-2"); ok {
		t.Error("a throat at 5 m/s forms a lateral pair")
	}
	tight := depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 6, width: 4, throat: 2})
	_, tightLateral := lateralIndex(t, tight)
	if _, ok := lateralPair(tight, tightLateral, "parking-in-1", "parking-in-2"); ok {
		t.Error("lead-ins 4 m apart form a lateral pair")
	}
	nodes := func(key separationKey) [2]string {
		if key.kind == laneKeyKind {
			lane := network.Lanes[key.index]
			return [2]string{lane.From, lane.To}
		}
		for _, station := range network.Stations {
			for _, berth := range station.Berths {
				if indexBerthKeys(network)[berth.ID] == key.index {
					return [2]string{berth.Node, berth.Node}
				}
			}
		}
		t.Fatalf("no berth %d", key.index)
		return [2]string{}
	}
	r := newLaneRetention(network, indexGeometryTails(network))
	spans := func(key separationKey) []windowSpan {
		if key.kind == laneKeyKind {
			return r.laneSpans(network.Lanes[key.index].ID)
		}
		return r.nodeSpans(nodes(key)[0])
	}
	for pair := range lateral.pairs {
		a, b := nodes(pair[0]), nodes(pair[1])
		if a[0] == b[0] || a[0] == b[1] || a[1] == b[0] || a[1] == b[1] {
			t.Errorf("pair %v shares a node", pair)
		}
		for _, first := range spans(pair[0]) {
			for _, second := range spans(pair[1]) {
				if first.window == second.window {
					t.Errorf("window %d holds the lateral pair %v", first.window, pair)
				}
			}
		}
	}
	for lane, neighbors := range lateral.neighbors {
		if !slices.IsSorted(neighbors) {
			t.Errorf("lane %d neighbors %v", lane, neighbors)
		}
		for _, neighbor := range neighbors {
			if !slices.Contains(lateral.neighbors[neighbor], lane) {
				t.Errorf("lane %d has neighbor %d, but not the reverse", lane, neighbor)
			}
			if _, ok := lateral.clearance(laneKey(lane), laneKey(neighbor)); !ok {
				t.Errorf("neighbors %d and %d are not a pair", lane, neighbor)
			}
		}
	}
}

// TestDepotLateralCost checks the work of the certification on the
// largest legal depot, 8 lanes of 25 slots, and on a larger synthetic
// depot: no pair runs out of work, and the visits stay within a bound.
func TestDepotLateralCost(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		lanes, slots int
		visits       int
	}{{8, 25, 50000}, {8, 100, 180000}} {
		network := depotNetwork(depotLayout{lanes: test.lanes, slots: test.slots, pitch: 6, width: 6, throat: 2})
		_, lateral := lateralIndex(t, network)
		t.Logf("%d lanes of %d slots: %d pairs, %d visits", test.lanes, test.slots, len(lateral.pairs), lateral.visits)
		if lateral.unresolved != 0 || lateral.visits > test.visits || len(lateral.pairs) == 0 {
			t.Errorf("%d lanes of %d slots: %d unresolved, %d visits, %d pairs", test.lanes, test.slots, lateral.unresolved, lateral.visits, len(lateral.pairs))
		}
	}
}

// smallClassWidthsKnown checks that each class of a list is large or one
// of the small classes whose body the lateral sweep models: 4 m long and
// at most 1.6 m wide. A new small class must be added here only when its
// width is at most 1.6 m, or the body width of the sweep must change.
func smallClassWidthsKnown(names []string) error {
	for _, name := range names {
		class := VehicleClass(name)
		spec, ok := LookupVehicleClass(class)
		if !ok {
			return fmt.Errorf("class %q has no registry entry", name)
		}
		if largeVehicleClass(class) {
			continue
		}
		if class != LegacyClass && class != CompactClass || spec.BodyLengthMeters != compactBody {
			return fmt.Errorf("small class %q has no reviewed body width", name)
		}
	}
	return nil
}

// TestSmallClassWidth checks the classes of the registry against the body
// of the lateral sweep.
func TestSmallClassWidth(t *testing.T) {
	t.Parallel()
	if err := smallClassWidthsKnown(classSetNames[:]); err != nil {
		t.Fatal(err)
	}
	if err := smallClassWidthsKnown(append(slices.Clone(classSetNames[:]), "wide")); err == nil {
		t.Fatal("an unknown class passes")
	}
}

// unauditedFleet returns a simulation with faults on whose network is
// network, with the indexes of PrepareNetwork but without its bank audit.
// The pods are at the passenger stations of Example. The audit refuses
// depot lanes that are less than Clearance apart until it accepts lateral
// pairs.
func unauditedFleet(t *testing.T, network Network) *Simulation {
	t.Helper()
	if err := network.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := NewFleet(testDepotNetwork(), depotPlacements(testDepotNetwork(), 0))
	if err != nil {
		t.Fatal(err)
	}
	owned := network.clone()
	inferStationLaneRoles(&owned)
	detachIndexes(s)
	s.network, s.graph = owned, routeGraph{}
	s.ensureNetworkIndexes()
	s.incidentContract = IncidentV1Contract
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	return s
}

// ownFootprint returns the footprint of debris without the lateral
// neighbors of the lane.
func ownFootprint(s *Simulation, lane int, from, to float64) []resource {
	indexes := s.networkIndexes
	bare := *indexes
	bare.lateral = nil
	s.networkIndexes = &bare
	defer func() { s.networkIndexes = indexes }()
	return s.debrisFootprint(lane, from, to)
}

// TestDebrisLateralNeighbors checks the debris footprint at 6 m spacing.
// Debris at the start of a 60 m lead-in of lane 2 holds the end of the
// fan and the lead-in cells of lanes 1 and 3 after the cells of its own
// lane, and it blocks lanes 1 and 3. Debris at the end of the fan of lane
// 3 is refused, because the lead-in of lane 2 is shorter than 13 m, and
// its cell within Clearance of the debris holds the tail slot. Debris in
// the middle of a fan blocks the fans of the other lanes through the
// entry junction.
func TestDebrisLateralNeighbors(t *testing.T) {
	t.Parallel()
	network := depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 6, width: 6, leadIn: 60, throat: 2})
	s := unauditedFleet(t, network)
	lane := laneIndex(t, s, "parking-in-2")
	own := ownFootprint(s, lane, 0, 1)
	footprint := s.debrisFootprint(lane, 0, 1)
	if !slices.Equal(footprint[:len(own)], own) || len(footprint) == len(own) {
		t.Fatalf("footprint %v does not start with the cells of its lane %v", footprint, own)
	}
	for _, id := range []string{"parking-fan-1", "parking-in-1", "parking-fan-3", "parking-in-3"} {
		if !slices.Contains(footprint, track(id, 0)) && !slices.Contains(footprint, track(id, s.laneCells[id].count()-1)) {
			t.Errorf("footprint has no cell of %s", id)
		}
	}
	if _, err := s.debrisSegment(lane, 0, 1); err != nil {
		t.Fatalf("debris at the start of the lead-in: %v", err)
	}
	startDebris(t, s, "parking-in-2", 0, 1, 0)
	for _, id := range []string{"parking-in-1", "parking-in-3", "parking-fan-1", "parking-fan-3"} {
		if !s.blocked.lanes[laneIndex(t, s, id)] {
			t.Errorf("lane %s is not blocked", id)
		}
	}
	short := unauditedFleet(t, narrowDepot(2))
	fan3 := laneIndex(t, short, "parking-fan-3")
	length := short.graph.lengths[fan3]
	if got := ownFootprint(short, fan3, length-1, length); slices.ContainsFunc(got, func(r resource) bool {
		return r.kind == berthResource || r.kind == nodeResource && len(short.berthResources[r.id]) > 0
	}) {
		t.Fatalf("own cells at the fan end hold a berth: %v", got)
	}
	if _, err := short.debrisSegment(fan3, length-1, length); !errors.Is(err, errDebrisSegment) {
		t.Fatalf("debris near a neighbor slot: %v", err)
	}
	// Debris in the middle of the fan of lane 1 blocks the other fans.
	fans := unauditedFleet(t, network)
	fan := laneIndex(t, fans, "parking-fan-1")
	middle := fans.graph.lengths[fan] / 2
	startDebris(t, fans, "parking-fan-1", middle, middle+1, 0)
	for _, id := range []string{"parking-fan-2", "parking-fan-3"} {
		if !fans.blocked.lanes[laneIndex(t, fans, id)] {
			t.Errorf("debris in the middle of the fan of lane 1 leaves %s open", id)
		}
	}
}

// rotateNetwork turns each node and control point of a network by an
// angle about the origin.
func rotateNetwork(network Network, angle float64) Network {
	s, c := math.Sincos(angle)
	turn := func(p Point) Point { return Point{X: p.X*c - p.Y*s, Y: p.X*s + p.Y*c} }
	network = network.clone()
	for i := range network.Nodes {
		network.Nodes[i].Position = turn(network.Nodes[i].Position)
	}
	for i := range network.Lanes {
		if network.Lanes[i].Control != nil {
			network.Lanes[i].Control = new(turn(*network.Lanes[i].Control))
		}
	}
	return network
}

// TestDebrisFootprintTwelve checks that a depot with lanes Clearance apart
// keeps the footprint of the lane alone for debris on each depot lane,
// also when the depot is turned so that neighbor lanes are exactly
// Clearance apart in their boxes. PrepareNetwork builds the same lateral
// index.
func TestDebrisFootprintTwelve(t *testing.T) {
	t.Parallel()
	p, err := PrepareNetwork(testDepotNetwork())
	if err != nil {
		t.Fatal(err)
	}
	if _, want := lateralIndex(t, p.network); p.lateral == nil || !reflect.DeepEqual(p.lateral, want) {
		t.Fatalf("prepared lateral index %+v", p.lateral)
	}
	for _, angle := range []float64{0, math.Pi / 6} {
		network := rotateNetwork(testDepotNetwork(), angle)
		s := unauditedFleet(t, network)
		neighbors := 0
		for lane, item := range s.network.Lanes {
			if !s.depots.depotLane(item.ID) {
				continue
			}
			neighbors += len(s.lateral.neighborLanes(lane))
			length := s.graph.lengths[lane]
			for from := 0.0; from < length; from += 0.5 {
				to := min(from+1, length)
				if got, want := s.debrisFootprint(lane, from, to), ownFootprint(s, lane, from, to); !slices.Equal(got, want) {
					t.Fatalf("angle %g: debris on %s at %g: footprint %v, want %v", angle, item.ID, from, got, want)
				}
			}
		}
		if neighbors == 0 {
			t.Fatalf("angle %g: no lateral neighbors", angle)
		}
	}
}
