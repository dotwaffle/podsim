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

// faultDepotFleet returns a simulation with faults on, on a depot network,
// with pods at the passenger stations and the given depot slots.
func faultDepotFleet(t *testing.T, network Network, slots ...string) *Simulation {
	t.Helper()
	placements := depotPlacements(network, 0)
	for _, slot := range slots {
		placements = append(placements, Placement{ID: "pod-" + slot, StationID: "parking", BerthID: slot})
	}
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
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
// lane, and it blocks lanes 1 and 3. The debris owns its footprint, a pod
// sent to the depot waits before the entry, and a save restores the same
// owners. Debris at the end of the fan of lane 3 is refused, because the
// lead-in of lane 2 is shorter than 13 m, and its cell within Clearance of
// the debris holds the tail slot. Debris in the middle of a fan blocks the
// fans of the other lanes through the entry junction.
func TestDebrisLateralNeighbors(t *testing.T) {
	t.Parallel()
	network := depotNetwork(depotLayout{lanes: 3, slots: 4, pitch: 6, width: 6, leadIn: 60, throat: 2})
	s := faultDepotFleet(t, network, depotSlotID(1, 1), depotSlotID(3, 1))
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
	mover := sendToDepot(t, s, "market-pod")
	id := startDebris(t, s, "parking-in-2", 0, 1, 0)
	for _, lane := range []string{"parking-in-1", "parking-in-3", "parking-fan-1", "parking-fan-3"} {
		if !laneBlocked(s, lane) {
			t.Errorf("lane %s is not blocked", lane)
		}
	}
	for _, r := range footprint {
		if s.owners[r] != (resourceOwner{kind: faultOwnerKind, id: id}) {
			t.Fatalf("%v has the owner %v", r, s.owners[r])
		}
	}
	// The pod sent to the depot stops before the entry and waits.
	stepDepot(t, s, 300*TicksPerSecond, func() bool {
		return mover.Pod.WaitReason == BlockedByIncident && mover.Pod.BlockedBy == id && mover.Pod.Speed == 0
	})
	if mover.Pod.LaneID != "return-to-parking" {
		t.Fatalf("mover waits on %q", mover.Pod.LaneID)
	}
	if err := s.checkDebrisOwners(); err != nil {
		t.Fatal(err)
	}
	restored := physicalSave(t, s, "lateral debris")
	if got := restored.debrisFootprint(lane, 0, 1); !slices.Equal(got, footprint) {
		t.Fatalf("restored footprint %v, want %v", got, footprint)
	}
	for _, r := range footprint {
		if restored.owners[r] != s.owners[r] {
			t.Fatalf("%v restores the owner %v, want %v", r, restored.owners[r], s.owners[r])
		}
	}
	short := faultDepotFleet(t, narrowDepot(2))
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
	fans := faultDepotFleet(t, network)
	fan := laneIndex(t, fans, "parking-fan-1")
	middle := fans.graph.lengths[fan] / 2
	startDebris(t, fans, "parking-fan-1", middle, middle+1, 0)
	for _, id := range []string{"parking-fan-2", "parking-fan-3"} {
		if !laneBlocked(fans, id) {
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
		s := faultDepotFleet(t, network)
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

// TestLateralAuditMatchesObserver checks that the bank audit, the editor
// pair pieces, and the safety observation agree on each lateral pair of a
// 6 m depot. The audit and PairPieces give one whole piece at the
// clearance of the pair. Pods with the keys of the pair pass at that
// clearance and fail just below it. Pods with the keys of a pair that is
// not lateral keep Clearance.
func TestLateralAuditMatchesObserver(t *testing.T) {
	t.Parallel()
	network := narrowDepot(2)
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	audit, editor := bankAudit{retention: p.retention, depots: p.depots, lateral: p.lateral}, NewLaneClearance(network)
	path := func(key separationKey) bankAuditPath {
		if key.kind == laneKeyKind {
			lane := p.network.Lanes[key.index]
			return bankAuditPath{key: key, member: auditMember{spans: p.retention.laneSpans(lane.ID), length: p.graph.lengths[key.index], speed: lane.SpeedLimit}}
		}
		return bankAuditPath{key: key}
	}
	observe := func(a, b separationKey, gap float64) error {
		first, second := Pod{ID: "a", Class: CompactClass}, Pod{ID: "b", Class: CompactClass, Position: Point{X: gap}}
		o := SafetyObservation{Pods: []Pod{first, second}, keys: map[string]podKey{"a": {pod: first, key: a}, "b": {pod: second, key: b}}, lateral: p.lateral}
		_, err := o.Check()
		return err
	}
	lanes := 0
	for pair, want := range p.lateral.pairs {
		first, second := path(pair[0]), path(pair[1])
		pieces := audit.pieces(first, second)
		if len(pieces) != 1 || pieces[0].clearance != want || pieces[0].first != [2]float64{0, first.member.length} || pieces[0].second != [2]float64{0, second.member.length} {
			t.Fatalf("pair %v: audit pieces %+v, want one piece at %g", pair, pieces, want)
		}
		if pair[0].kind == laneKeyKind && pair[1].kind == laneKeyKind {
			lanes++
			a, b := p.network.Lanes[pair[0].index], p.network.Lanes[pair[1].index]
			got := editor.PairPieces(a, b, first.member.length, second.member.length)
			if wantPieces := []PairPiece{{First: pieces[0].first, Second: pieces[0].second, Clearance: want}}; !reflect.DeepEqual(got, wantPieces) {
				t.Fatalf("%s and %s: editor pieces %+v, want %+v", a.ID, b.ID, got, wantPieces)
			}
		}
		if err := observe(pair[0], pair[1], want); err != nil {
			t.Fatalf("pair %v at %g: %v", pair, want, err)
		}
		if err := observe(pair[1], pair[0], want-1e-3); err == nil {
			t.Fatalf("pair %v below %g passes", pair, want)
		}
	}
	if lanes == 0 {
		t.Fatal("no lateral pair of two lanes")
	}
	in1, through := laneKey(p.graph.lanes["parking-in-1"]), laneKey(p.graph.lanes["harbor-through"])
	if _, ok := p.lateral.clearance(in1, through); ok {
		t.Fatal("a lead-in and a through lane form a lateral pair")
	}
	if err := observe(in1, through, lateralClearance(depotSpeed)); err == nil {
		t.Fatal("a pair that is not lateral passes at the lateral clearance")
	}
}

// TestDepotLateralDeparture checks the observation of a pod that leaves
// the head slot of lane 1 while the head of lane 2 is idle 6.08 m away.
// On the tick that it leaves, the pod has no lane and no berth, and the
// origin case of separationKey gives it the key of its slot.
func TestDepotLateralDeparture(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(narrowDepot(2), []Placement{
		{ID: "lane-1-head", StationID: "parking", BerthID: depotSlotID(1, 1)},
		{ID: "lane-2-head", StationID: "parking", BerthID: depotSlotID(2, 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	v, head := s.findVehicle("lane-1-head"), s.findVehicle("lane-2-head")
	if gap := pointDistance(v.Pod.Position, head.Pod.Position); math.Abs(gap-math.Sqrt(37)) > 1e-9 {
		t.Fatalf("heads %g m apart", gap)
	}
	harbor, _ := s.station("harbor")
	if err := s.startEmptyMove(v, emptyDestination{station: "harbor", berth: harbor.Berths[0], reserveBerth: true}); err != nil {
		t.Fatal(err)
	}
	leaving := 0
	stepDepot(t, s, 60*TicksPerSecond, func() bool {
		if v.Pod.LaneID == "" && v.Pod.BerthID == "" {
			leaving++
		}
		return v.Pod.LaneID == "parking-throat-1"
	})
	if leaving == 0 || head.Pod.Activity != Idle || head.Pod.BerthID != depotSlotID(2, 1) {
		t.Fatalf("%d leaving ticks, head %+v", leaving, head.Pod)
	}
}

// TestDepotLateralSpeeds runs the liveness test on a depot with lanes
// 6 m apart, with arrivals, move-ups, releases, pod faults, and
// withdrawals. At each tick, each pod on a lane with a lateral neighbor
// is at most at the speed limit of the lane, so its stop distance is at
// most the one that the lateral clearance assumes. Pods with lateral keys
// must come closer than Clearance at some tick.
func TestDepotLateralSpeeds(t *testing.T) {
	t.Parallel()
	network := depotNetwork(depotLayout{lanes: 2, slots: 3, pitch: 6, width: 6, throat: 2})
	lateral, near := 0, 0
	depotLiveness(t, network, func(s *Simulation) {
		o := s.SafetyObservation()
		for index := range s.vehicles {
			pod := s.vehicles[index].Pod
			lane, ok := s.graph.lanes[pod.LaneID]
			if !ok || len(s.lateral.neighborLanes(lane)) == 0 {
				continue
			}
			lateral++
			if limit := s.network.Lanes[lane].SpeedLimit; pod.Speed > limit {
				t.Fatalf("tick %d: pod %s at %g m/s on %s, limit %g", s.tick, pod.ID, pod.Speed, pod.LaneID, limit)
			}
		}
		for i, a := range o.Pods {
			for _, b := range o.Pods[i+1:] {
				first, firstOK := o.keys[a.ID]
				second, secondOK := o.keys[b.ID]
				if _, ok := s.lateral.clearance(first.key, second.key); ok && firstOK && secondOK && pointDistance(a.Position, b.Position) < Clearance {
					near++
				}
			}
		}
	})
	if lateral == 0 || near == 0 {
		t.Fatalf("%d pod ticks on lateral lanes, %d close lateral pairs", lateral, near)
	}
	t.Logf("%d pod ticks on lateral lanes, %d close lateral pairs", lateral, near)
}

// TestCheckSeparationDepotAllocations bounds the allocations of the key
// pass of Check on a depot network with lateral pairs. The separation
// index of an observation with keys makes one allocation more than the
// same observation without them, the keys of the index, for 3 pods and
// for 12. The observation without keys is too close, so the test
// compares the index and not Check.
func TestCheckSeparationDepotAllocations(t *testing.T) {
	network := narrowDepot(2)
	for _, count := range []int{3, 12} {
		s, err := NewFleet(network, depotPlacements(network, count))
		if err != nil {
			t.Fatal(err)
		}
		o := s.SafetyObservation()
		if len(o.keys) != len(o.Pods) || o.lateral == nil {
			t.Fatalf("the observation has %d keys", len(o.keys))
		}
		if _, err := o.Check(); err != nil {
			t.Fatal(err)
		}
		bare := o
		bare.keys, bare.lateral = nil, nil
		got := testing.AllocsPerRun(20, func() { _ = o.separationIndex() })
		want := testing.AllocsPerRun(20, func() { _ = bare.separationIndex() })
		t.Logf("%d depot pods: %v allocations, without keys: %v", count, got, want)
		if got > want+1 {
			t.Fatalf("%d depot pods: the index makes %v allocations, want at most %v", count, got, want+1)
		}
	}
}
