package sim

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// These are verifier unit inputs, not admitted tiny-lane train fixtures.
func TestCouplingMotionSpeedBoundaryConsumer(t *testing.T) {
	t.Parallel()
	lengths, limits := []float64{50}, []float64{14}
	for range 1000 {
		lengths = append(lengths, .01)
		limits = append(limits, 14)
	}
	lengths = append(lengths, 100)
	limits = append(limits, 2.5)
	for _, test := range []struct {
		name            string
		lengths, limits []float64
		start, speed    float64
		deny            bool
	}{
		{"thousand_future_lanes", lengths, limits, 49.9, 14, true},
		{"crossed_low_lane", []float64{50, .01, 100}, []float64{14, 2.5, 14}, 49.9, 14, true},
		{"terminal_low_lane", []float64{50, .001}, []float64{14, .25}, 49.9, 14, true},
		{"safe_constant_cap", lengths, limits, 49.9, 2.5, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := laneSpeedFixture(test.lengths, test.limits)
			detachIndexes(s)
			s.geometry = buildLaneGeometry(s.network)
			s.setVehicleRoute(v, s.network.Lanes)
			last := len(v.blocks.route) - 1
			v.blocks.lanes[last+1].start = v.blocks.lanes[last].start + v.blocks.lanes[last].length
			end := test.start + test.speed/60
			err := couplingMotionSpeedProof(&v.blocks, test.start, end, test.speed, 2)
			if errors.Is(err, errCouplingMotionInvariant) != test.deny {
				t.Fatalf("speed boundary consumer err=%v want deny=%v", err, test.deny)
			}
		})
	}
}

func TestCouplingMotionCompleteClosureAndClock(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, true, false)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	context, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	original := make(map[resource]bool)
	for _, claim := range reservation.Claims {
		original[claim.Resource] = true
	}
	var extra resource
	found := false
	for _, claim := range context.claims {
		if !original[claim.Resource] && claim.Resource.kind == trackResource {
			extra = claim.Resource
			found = true
			break
		}
	}
	if !found {
		t.Fatal("positive closure fixture has no new actual exit cell")
	}
	for _, owner := range []resourceOwner{{kind: groupOwnerKind, id: "front"}, {kind: podOwnerKind, id: "foreign"}, {kind: ownerKind(99), id: "front"}} {
		bad := cloneCouplingInput(input)
		bad.Owners[extra] = owner
		before := cloneCouplingInput(bad)
		out, denialErr := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: bad, GroupID: "pair"})
		if !errors.Is(denialErr, errCouplingReservationDenied) || out != nil || !couplingInputsEqual(before, bad) {
			t.Fatalf("complete exit foreign owner accepted: %v", denialErr)
		}
	}
	late := cloneCouplingInput(input)
	late.Tick = math.MaxInt64
	lateReservation, err := planCouplingReservation(late)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: lateReservation, Current: late, GroupID: "pair"}); !errors.Is(err, errCouplingReservationDenied) {
		t.Fatal("actual combined motion clock overflow accepted")
	}
	for _, ids := range [][]string{{"front"}, {"rear"}, {"foreign", "foreign"}, {""}} {
		if _, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair", ForeignIDs: ids}); !errors.Is(err, errCouplingReservationDenied) {
			t.Fatal("invalid complete fleet identity list accepted")
		}
	}
}

func TestCouplingMotionContextAndOutputCopies(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, true, false)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		t.Fatal(err)
	}
	actual := maps.Clone(input.Owners)
	applyCouplingTestWrites(t, actual, initial.Writes)
	view, err := sealCouplingMotionOwners(c, initial.State, actual)
	if err != nil {
		t.Fatal(err)
	}
	arguments := couplingMotionInput{Context: c, Previous: initial.State, Owners: view}
	control, err := planCouplingMotion(arguments)
	if err != nil {
		t.Fatal(err)
	}
	input.Members[0].Vehicle.Route[0].SpeedLimit = .1
	input.Members[0].Vehicle.Riders[0].PartySize = 4
	input.Members[0].Vehicle.Stops[0] = "changed"
	reservation.Claims[0].Resource.id = "changed"
	reservation.members[0].Vehicle.Riders[0].SharingConsent = SharedConsent
	copied, err := planCouplingMotion(arguments)
	if err != nil || !couplingMotionOutputsEqual(control, copied) {
		t.Fatal("immutable context aliases mutable reservation input")
	}
	copied.Members[0].Riders[0].PartySize = 4
	copied.Members[0].Stops[0] = "changed"
	copied.Connector.Corners[0].X += 1
	copied.Bodies[0].Corners[0].Y += 1
	again, err := planCouplingMotion(arguments)
	if err != nil || !couplingMotionOutputsEqual(control, again) {
		t.Fatal("motion output aliases the context or another output")
	}
}

func TestCouplingMotionAuthoredLowerLimits(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, true, true)
	network := input.Prepared.Network()
	for i, lane := range network.Lanes {
		switch lane.ID {
		case "ab":
			network.Lanes[i].SpeedLimit = 14
		case "bc":
			network.Lanes[i].SpeedLimit = 1.123456789
		case "rear-road":
			network.Lanes[i].SpeedLimit = .75123456789
		}
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	corridor := input.Network.corridors[input.CorridorID]
	geometry := CouplingGeometryInput{Contract: CompactPairV1CouplingContract, Network: prepared.Network(), Sites: []CouplingSite{input.Network.sites[corridor.AssemblySiteID], input.Network.sites[corridor.SplitSiteID]}, Corridors: []CouplingCorridor{corridor}}
	n, err := prepareCouplingReservations(prepared, geometry)
	if err != nil {
		t.Fatal(err)
	}
	input.Network, input.Prepared = n, prepared
	for i := range input.Members {
		for j, lane := range input.Members[i].Vehicle.Route {
			input.Members[i].Vehicle.Route[j] = n.lanes[lane.ID].lane
		}
	}
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ticks > 50000 {
		t.Fatal("lower-limit fixture exceeds declared oracle cap")
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		t.Fatal(err)
	}
	actual := maps.Clone(input.Owners)
	applyCouplingTestWrites(t, actual, initial.Writes)
	view, err := sealCouplingMotionOwners(c, initial.State, actual)
	if err != nil {
		t.Fatal(err)
	}
	state := initial.State
	for !state.Finished {
		previous := state
		step, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: previous, Owners: view})
		if err != nil {
			t.Fatal(err)
		}
		state = step.State
		couplingIndependentBodyOracle(t, c, previous, step)
		couplingIndependentLaneOracle(t, c, previous, state)
		applyCouplingTestWrites(t, actual, step.Writes)
		if len(step.Writes) > 0 {
			view, err = advanceCouplingMotionOwners(view, previous, state, step.Writes, actual)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("lower-limit total_ticks=%d connected_cap=%.17g front_drain_cap=%.17g rear_drain_cap=%.17g", c.ticks, c.legs[1].cap, c.legs[3].cap, c.legs[4].cap)
}

func couplingMotionOutputsEqual(a, b couplingMotionStep) bool {
	if a.State != b.State || a.Samples != b.Samples || a.Bodies != b.Bodies || !slices.Equal(a.Writes, b.Writes) || !reflect.DeepEqual(a.Members, b.Members) {
		return false
	}
	if a.Connector == nil || b.Connector == nil {
		return a.Connector == nil && b.Connector == nil
	}
	return *a.Connector == *b.Connector
}

// TestCouplingMotionSharedBranchExit pins the exit refusals of a pair whose
// members go to two berths of one station. Each cell of the shared branch
// after the split is a joint dependency, so the joint release moves up to
// the station entry. The pods have no stops, so the receiving boundary of
// each member is its berth at the end of its route. A short branch leaves
// no stopping room between the joint release and the berth. A longer
// branch gives that room, but the exit closure then reaches the berth.
func TestCouplingMotionSharedBranchExit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		branch float64
		want   string
	}{
		{"short_branch", 30, "actual continuation cannot close joint release and stopping room"},
		{"long_branch", 60, "exit closure reaches an actual receiving boundary"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingSharedBranchFixture(t, test.branch)
			reservation, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
			if c != nil || !errors.Is(err, errCouplingReservationDenied) || err.Error() != test.want+": coupling reservation denied" {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

// couplingSharedBranchFixture is couplingMotionFixture with the front
// station at branch meters past the split and a second berth, which the
// rear takes. Both members then take the same road from the split.
func couplingSharedBranchFixture(t *testing.T, branch float64) couplingReservationInput {
	t.Helper()
	compact := classBit(string(CompactClass))
	input := couplingMotionFixtureWith(t, false, false, func(n *Network) {
		for i := range n.Nodes {
			switch n.Nodes[i].ID {
			case "front-entry":
				n.Nodes[i].Position = Point{X: 420 + branch}
			case "front-berth":
				n.Nodes[i].Position = Point{X: 470 + branch, Y: 25}
			case "front-exit":
				n.Nodes[i].Position = Point{X: 530 + branch}
			}
		}
		n.Nodes = append(n.Nodes, Node{ID: "front-berth-2", Position: Point{X: 470 + branch, Y: -25}})
		for _, lane := range []Lane{{ID: "front-in-2", From: "front-entry", To: "front-berth-2"}, {ID: "front-out-2", From: "front-berth-2", To: "front-exit"}} {
			lane.SpeedLimit, lane.VehicleClasses = 7.123456789, compact
			n.Lanes = append(n.Lanes, lane)
		}
		for i := range n.Stations {
			if n.Stations[i].ID == "front-goal" {
				n.Stations[i].Berths = append(n.Stations[i].Berths, Berth{ID: "front-goal-2", Node: "front-berth-2", VehicleClasses: compact})
			}
		}
	})
	rear := &input.Members[1]
	for _, r := range berthResources(rear.Destination) {
		delete(input.Owners, r)
	}
	station, _ := input.Prepared.Network().Station("front-goal")
	rear.Destination = station.Berths[1]
	for _, r := range berthResources(rear.Destination) {
		input.Owners[r] = podResourceOwner("rear")
	}
	rear.DestinationStation, rear.Vehicle.RelocatingTo = "front-goal", "front-goal"
	rear.Vehicle.Route = rear.Vehicle.Route[:2]
	lanes := input.Prepared.Network().Lanes
	for _, id := range []string{"front-road", "front-in-2"} {
		i := slices.IndexFunc(lanes, func(lane Lane) bool { return lane.ID == id })
		if i < 0 {
			t.Fatalf("no lane %s", id)
		}
		rear.Vehicle.Route = append(rear.Vehicle.Route, lanes[i])
	}
	return input
}
