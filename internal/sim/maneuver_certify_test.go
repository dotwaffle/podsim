package sim

import (
	"math"
	"reflect"
	"testing"
)

// TestManeuverCertificationPreset checks that the compact station of the
// design table prepares, with each pair of a group lane and another berth
// of the group certified, and each pair of berths.
func TestManeuverCertificationPreset(t *testing.T) {
	t.Parallel()
	p, err := PrepareNetwork(compactStationNetwork(6, 28.40))
	if err != nil {
		t.Fatal(err)
	}
	lanes, berths := 0, 0
	for pair := range p.certified {
		switch {
		case pair[0].kind == laneKeyKind && pair[1].kind == berthKeyKind:
			lanes++
		case pair[0].kind == berthKeyKind && pair[1].kind == berthKeyKind:
			berths++
		default:
			t.Fatalf("certified pair %+v", pair)
		}
	}
	// 5 aisle lanes and 4 berths, 10 lanes in bay 1 and 8 in each other
	// bay, each with 3 berths that are not its own.
	if want := 5*4 + (10+3*8)*3; lanes != want || berths != 6 {
		t.Fatalf("%d lane pairs and %d berth pairs, want %d and 6", lanes, berths, want)
	}
}

// TestManeuverCertificationRefusals checks the refusals of the
// certification and of the audit after it: bays at a 5 meter pitch
// collide, two berths in one place overlap, and a receiving node 1 cm
// closer to the last berth fails the audit.
func TestManeuverCertificationRefusals(t *testing.T) {
	t.Parallel()
	overlap := compactStationNetwork(6, 28.40)
	overlap.Nodes = append(overlap.Nodes, Node{ID: "twin", Position: Point{X: -1.087569 + 0.5, Y: -0.507142 + 0.5}})
	overlap.Lanes = append(overlap.Lanes,
		Lane{ID: "twin-in", From: "branch-1", To: "twin", SpeedLimit: 2, VehicleClasses: compactClassSet},
		Lane{ID: "twin-out", From: "twin", To: "branch-1", SpeedLimit: 2, VehicleClasses: compactClassSet})
	overlap.Stations[0].Berths = append(overlap.Stations[0].Berths, Berth{ID: "twin", Node: "twin", VehicleClasses: compactClassSet})
	group := &overlap.ManeuverGroups[0]
	group.LaneIDs = append(group.LaneIDs, "twin-in", "twin-out")
	group.ReverseLaneIDs = append(group.ReverseLaneIDs, "twin-out")
	group.BerthIDs = append(group.BerthIDs, "twin")
	for _, test := range []struct {
		name    string
		network Network
		want    string
	}{
		{name: "pitch", network: compactStationNetwork(5, 28.40), want: `maneuver group "compact" lane "bay-2-in-3" is not clear of berth "bay-3"`},
		{name: "overlap", network: overlap, want: `maneuver group "compact" berths "bay-1" and "twin" overlap`},
		{name: "receive", network: compactStationNetwork(6, 28.39), want: `nonincident paths "bay-4-in-4" and "leave" are less than 12 meters apart`},
	} {
		if _, err := PrepareNetwork(test.network); err == nil || err.Error() != test.want {
			t.Errorf("%s: error %v, want %q", test.name, err, test.want)
		}
		if err := test.network.ValidateBankGeometry(); err == nil || err.Error() != test.want {
			t.Errorf("%s: ValidateBankGeometry error %v, want %q", test.name, err, test.want)
		}
	}
}

// TestManeuverCertificationStatus checks that only a clear sweep
// certifies. Bays 2 to 4 at a 5 meter pitch collide. At a 5.28 meter pitch
// the lane to bay 3 passes bay 2 close enough that a coarse tolerance finds
// only a potential overlap, and a small work allowance does not finish.
func TestManeuverCertificationStatus(t *testing.T) {
	t.Parallel()
	certifier := func(pitch float64) *maneuverCertifier {
		network := compactStationNetwork(pitch, 28.40)
		maneuvers, err := newManeuverIndex(network, indexNodePositions(network.Nodes))
		if err != nil {
			t.Fatal(err)
		}
		return newManeuverCertifier(network, maneuvers)
	}
	c := certifier(5)
	if status := sweepSteps(c.steps("bay-2-in-3"), c.envelopes["bay-3"], certifyOptions); status != sweepCollision {
		t.Fatalf("status %d at a 5 meter pitch, want a collision", status)
	}
	c = certifier(5.28)
	for _, test := range []struct {
		options sweepOptions
		want    sweepStatus
	}{
		{options: certifyOptions, want: sweepClear},
		{options: sweepOptions{tolerance: 100, visits: 8192}, want: sweepPotential},
		{options: sweepOptions{tolerance: 1e-3, visits: 1}, want: sweepUnresolved},
	} {
		if status := sweepSteps(c.steps("bay-3-in-2"), c.envelopes["bay-2"], test.options); status != test.want {
			t.Errorf("options %+v: status %d, want %d", test.options, status, test.want)
		}
		_, err := c.certify(test.options)
		if test.want == sweepClear && err != nil || test.want != sweepClear && err == nil {
			t.Errorf("options %+v: certify error %v", test.options, err)
		}
	}
}

// TestManeuverCertificationJoin checks a collision that only the join step
// at a node finds. The aisle turns up after bay 2, to a corner below the
// envelope of bay 2, and then down to the receiving node. Each segment is
// clear, but the rotation at the corner sweeps the front of the body into
// the envelope.
func TestManeuverCertificationJoin(t *testing.T) {
	t.Parallel()
	network := withoutLane(tallBayNetwork(2, 7), "aisle-out")
	network.Nodes = append(network.Nodes, Node{ID: "corner", Position: Point{13, 8.93}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "corner-up", From: "branch-2", To: "corner", SpeedLimit: 2, VehicleClasses: compactClassSet},
		Lane{ID: "aisle-out", From: "corner", To: "receive", SpeedLimit: 2, VehicleClasses: compactClassSet})
	network.ManeuverGroups[0].LaneIDs = append(network.ManeuverGroups[0].LaneIDs, "corner-up", "aisle-out")
	want := `maneuver group "bays-group" lane "corner-up" is not clear of berth "bay-2"`
	if _, err := PrepareNetwork(network); err == nil || err.Error() != want {
		t.Fatalf("error %v, want %q", err, want)
	}
	// Nine centimeters lower, the rotation clears the envelope.
	network.Nodes[len(network.Nodes)-1].Position.Y = 8.84
	if _, err := PrepareNetwork(network); err != nil {
		t.Fatal(err)
	}
}

// TestManeuverCertificationBerthSteps checks that the in-lane of a berth
// ends with a rotation at the berth node from its last segment heading to
// the parked heading, and that the reverse lane from the berth starts with
// the opposite rotation.
func TestManeuverCertificationBerthSteps(t *testing.T) {
	t.Parallel()
	network := compactStationNetwork(6, 28.40)
	maneuvers, err := newManeuverIndex(network, indexNodePositions(network.Nodes))
	if err != nil {
		t.Fatal(err)
	}
	c := newManeuverCertifier(network, maneuvers)
	berth := network.Stations[0].Berths[1]
	node, _ := network.Node(berth.Node)
	parked := c.envelopes[berth.ID].heading
	if math.Abs(parked-25*math.Pi/180) > 1e-5 {
		t.Fatalf("parked heading %g", parked)
	}
	find := func(lane string, from, to float64) bool {
		for _, step := range c.steps(lane) {
			if step.start.x == node.Position.X && step.start.y == node.Position.Y && step.dx == 0 && step.dy == 0 &&
				math.Abs(step.start.heading-from) < 1e-12 && math.Abs(step.start.heading+step.sweep-to) < 1e-12 {
				return true
			}
		}
		return false
	}
	if !find("bay-2-in-4", c.last["bay-2-in-4"], parked) {
		t.Error("the in-lane has no berth step")
	}
	if !find("bay-2-out-4", parked, c.first["bay-2-out-4"]) {
		t.Error("the reverse lane has no berth step")
	}
	if math.Abs(math.Remainder(c.first["bay-2-out-4"]-c.last["bay-2-in-4"], 2*math.Pi)) > 1e-9 {
		t.Error("the body turns around at the berth")
	}
	// Between two segments of the curved lane, the body rotates at their
	// shared point.
	steps, vertices := c.steps("bay-2-in-4"), 0
	for i := 2; i < len(steps); i++ {
		a, b := steps[i-2], steps[i]
		if a.dx == 0 && a.dy == 0 || b.dx == 0 && b.dy == 0 {
			continue
		}
		turn := steps[i-1]
		if turn.dx != 0 || turn.dy != 0 || turn.start.x != b.start.x || turn.start.y != b.start.y ||
			turn.start.heading != a.start.heading || math.Abs(turn.start.heading+turn.sweep-b.start.heading) > 1e-12 {
			t.Fatalf("step %d is not the rotation between two segments: %+v", i-1, turn)
		}
		vertices++
	}
	if vertices == 0 {
		t.Fatal("no vertex rotations")
	}
}

// TestManeuverCertificationLaneSteps checks the steps of tall bay lanes.
// The body faces against the travel direction on a reverse lane. A lane
// that leaves a branch node starts with a join rotation from the heading
// of each group lane that ends there, and a lane that ends at a branch
// node ends with a join rotation to the heading of each group lane that
// starts there.
func TestManeuverCertificationLaneSteps(t *testing.T) {
	t.Parallel()
	network := tallBayNetwork(2, 7)
	maneuvers, err := newManeuverIndex(network, indexNodePositions(network.Nodes))
	if err != nil {
		t.Fatal(err)
	}
	c := newManeuverCertifier(network, maneuvers)
	segments := 0
	for _, step := range c.steps("bay-1-out-inner-1") {
		if step.dx != 0 || step.dy != 0 {
			segments++
			if math.Abs(step.start.heading-math.Pi/2) > 1e-12 || step.dy >= 0 {
				t.Errorf("reverse segment step %+v", step)
			}
		}
	}
	if segments == 0 {
		t.Fatal("no segment steps")
	}
	join := func(lane string, from, to float64) bool {
		for _, step := range c.steps(lane) {
			if step.start.x == 0 && step.start.y == 0 && step.dx == 0 && step.dy == 0 &&
				math.Abs(step.start.heading-from) < 1e-12 && math.Abs(step.start.heading+step.sweep-to) < 1e-12 {
				return true
			}
		}
		return false
	}
	if !join("aisle-2", math.Pi/2, 0) {
		t.Error("aisle-2 has no join rotation from the bay")
	}
	if !join("aisle-1", 0, math.Pi/2) {
		t.Error("aisle-1 has no join rotation into the bay")
	}
	if !join("bay-1-out-branch-1", math.Pi/2, 0) {
		t.Error("the bay has no join rotation to aisle-2")
	}
}

// TestManeuverSweepStatus checks sweeps that touch an envelope only inside a step:
// a translation through the envelope, and a rotation whose front corner
// passes into the envelope between two clear end poses.
func TestManeuverSweepStatus(t *testing.T) {
	t.Parallel()
	through := sweepStep{start: sweepPose{}, dx: 20}
	if status := sweepSteps([]sweepStep{through}, sweepRectangle{x: 10}, certifyOptions); status != sweepCollision {
		t.Errorf("translation status %d, want a collision", status)
	}
	if status := sweepSteps([]sweepStep{through}, sweepRectangle{x: 10, y: 4}, certifyOptions); status != sweepClear {
		t.Errorf("clear translation status %d", status)
	}
	// The front corner is 3.846 meters from the rear axle. At headings
	// 150 and 30 degrees, and at 90 degrees, each corner is below 3.71.
	reach := math.Hypot(sweepFront, sweepHalfWidth)
	turn := rotation(Point{}, 150*math.Pi/180, 30*math.Pi/180)
	for _, test := range []struct {
		low  float64
		want sweepStatus
	}{
		{low: reach - 0.05, want: sweepCollision},
		{low: reach + 0.01, want: sweepClear},
	} {
		envelope := sweepRectangle{y: test.low + envelopeLength/2, heading: math.Pi / 2}
		if status := sweepSteps([]sweepStep{turn}, envelope, certifyOptions); status != test.want {
			t.Errorf("rotation with the envelope from %g: status %d, want %d", test.low, status, test.want)
		}
	}
}

// TestManeuverSafetyKeys checks the separation keys of the observation.
// Two pods parked in adjacent bays are 6 meters apart, and only their
// certified berth pair lets Check pass. A stale key, a lane with the ID of
// a berth, and a traveling pod with no lane at a positive distance never
// skip a pair. A network without groups adds no keys.
func TestManeuverSafetyKeys(t *testing.T) {
	t.Parallel()
	network := compactStationNetwork(6, 28.40)
	fleet := []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}, {ID: "02", StationID: "bays", BerthID: "bay-2", Class: CompactClass}}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	observation := s.SafetyObservation()
	if _, err = observation.Check(); err != nil {
		t.Fatal(err)
	}
	stale := observation
	stale.Pods = append([]Pod(nil), observation.Pods...)
	stale.Pods[1].Speed = 0.5
	if _, err = stale.Check(); err == nil {
		t.Fatal("a stale key skipped a pair")
	}
	bare := observation
	bare.keys = nil
	if _, err = bare.Check(); err == nil {
		t.Fatal("an observation without keys skipped a pair")
	}
	// The pod 02 travels with no lane, at a positive distance from its
	// berth.
	v := &s.vehicles[1]
	v.Pod.Activity, v.Pod.BerthID, v.Pod.StationID, v.origin, v.distance = Traveling, "", "", network.Stations[0].Berths[1], 0.5
	if _, ok := s.separationKey(v); ok {
		t.Fatal("a traveling pod with no lane at a positive distance has a key")
	}
	if _, err = s.SafetyObservation().Check(); err == nil {
		t.Fatal("a pod with no key skipped a pair")
	}
	v.distance = 0
	if key, ok := s.separationKey(v); !ok || key != berthKey(1) {
		t.Fatalf("origin key %+v, %v", key, ok)
	}
	v.Pod.Position.X += 0.01
	if _, ok := s.separationKey(v); ok {
		t.Fatal("a pod away from its origin berth node has the origin key")
	}
	// A lane with the ID "bay-2" is a lane, not the berth.
	renamed := compactStationNetwork(6, 28.40)
	renamed.Lanes[networkLaneIndex(renamed, "approach")].ID = "bay-2"
	r, err := NewFleet(renamed, fleet[:1])
	if err != nil {
		t.Fatal(err)
	}
	w := &r.vehicles[0]
	w.Pod = Pod{ID: "01", Class: CompactClass, Activity: Traveling, LaneID: "bay-2", Position: Point{-25, compactY}}
	if key, ok := r.separationKey(w); !ok || key.kind != laneKeyKind || r.certified.certified(key, berthKey(0)) {
		t.Fatalf("lane key %+v, %v", key, ok)
	}
	plain, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.SafetyObservation(); got.keys != nil || got.certified != nil {
		t.Fatal("a network without groups has keys")
	}
	if !reflect.DeepEqual(plain.SafetyObservation(), plain.SafetyObservation()) {
		t.Fatal("observations differ")
	}
}
