package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"strings"
	"testing"
)

// These frames come from the private motion certificate, not live recruitment.
func nativeCouplingSavedFixture(t *testing.T, occupied bool, phase couplingReservationPhase, leg int) RestoreStateInput {
	t.Helper()
	input := couplingMotionFixture(t, occupied, false)
	plan, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: plan, Current: input, GroupID: "physical-pair"})
	if err != nil {
		t.Fatal(err)
	}
	state := remainingTestFind(t, c, phase, leg)
	n := input.Network
	contracts := FleetContracts{
		CouplingContract:  CompactPairV1CouplingContract,
		CouplingSites:     []CouplingSite{n.sites["assembly"], n.sites["split"]},
		CouplingCorridors: []CouplingCorridor{n.corridors["corridor"]},
	}
	fleet := []Placement{
		{ID: "front", Class: CompactClass, StationID: "origin"},
		{ID: "rear", Class: CompactClass, StationID: "front-goal"},
	}
	s, err := NewFleetWithContracts(input.Prepared.Network(), fleet, contracts)
	if err != nil {
		t.Fatal(err)
	}
	s.tick = state.Tick
	if occupied {
		s.requestID, s.boarded = 2, 2
	}
	s.owners = make(map[resource]resourceOwner)
	for _, dependency := range c.dependencies {
		if owner := c.dependencyOwner(dependency, state); !owner.isZero() {
			s.owners[dependency.Resource] = owner
		}
	}
	for _, claim := range c.reservation.PreservedClaims {
		s.owners[claim.Resource] = claim.Expected
	}
	members := c.membersAt(state)
	for i, member := range &input.Members {
		v := s.findVehicle(member.Vehicle.Pod.ID)
		v.Vehicle = member.Vehicle
		s.setVehicleRoute(v, member.Vehicle.Route)
		v.Pod = members[i].Pod
		v.origin, v.destination, v.destinationStation = member.Origin, member.Destination, member.DestinationStation
		v.distance, v.blockIndex, v.reservedThrough = state.Distances[i], state.Cells[i], c.through[i]
		v.originReleased = v.distance >= v.originTail()
		v.pending = -1
	}
	s.couplingGroups = []couplingNativeGroup{{context: c, state: state, formationTick: input.Tick}}
	if err := s.CheckContract(); err != nil {
		t.Fatal("native fixture violates the existing cabin contract", err)
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal("native fixture violates certified geometry", err)
	}
	return RestoreStateInput{
		Network: input.Prepared.Network(), Fleet: fleet, CouplingContract: contracts.CouplingContract,
		CouplingSites: contracts.CouplingSites, CouplingCorridors: contracts.CouplingCorridors, State: s.ExportState(),
	}
}

func TestCouplingNativePhaseRestore(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4}} {
			t.Run(fmt.Sprintf("occupied=%t/phase=%d/leg=%d", occupied, phase.phase, phase.leg), func(t *testing.T) {
				t.Parallel()
				input := nativeCouplingSavedFixture(t, occupied, phase.phase, phase.leg)
				original := input.State
				for range 3 {
					s, result, err := RestoreState(input)
					if err != nil || result.Tier != RestorePhysical || checkCouplingRestoreResult(result) != nil {
						t.Fatal("strict native phase restore failed", result, err)
					}
					next := s.ExportState()
					if !reflect.DeepEqual(original, next) {
						t.Fatalf("cold restore changed cabin or saved phase\noriginal=%+v\nrestored=%+v", original, next)
					}
					if !maps.Equal(s.owners, s.retainedOwners()) {
						t.Fatal("reconstructed group owners differ from retention")
					}
					if err := s.SetCouplingEnabled(false); err != nil || len(s.couplingGroups) != 1 {
						t.Fatal("off deleted a committed group", err)
					}
					clone := s.Clone()
					clone.couplingGroups[0].formationTick++
					if clone.couplingGroups[0].formationTick == s.couplingGroups[0].formationTick {
						t.Fatal("clone shares mutable group storage")
					}
					clone.Reset()
					if len(clone.couplingGroups) != 0 || clone.CouplingContract() != input.CouplingContract || len(s.couplingGroups) != 1 {
						t.Fatal("reset did not replace only the clone's group epoch")
					}
					input.State = next
				}
			})
		}
	}
}

func TestCouplingNativeRestoreRefusesPartialRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*RestoreStateInput)
	}{
		{"logical", func(i *RestoreStateInput) { i.LogicalOnly = true }},
		{"reverse", func(i *RestoreStateInput) {
			g := &i.State.CouplingGroups[0]
			g.Members[0], g.Members[1] = g.Members[1], g.Members[0]
		}},
		{"repeat", func(i *RestoreStateInput) { g := &i.State.CouplingGroups[0]; g.Members[1] = g.Members[0] }},
		{"unknown phase", func(i *RestoreStateInput) { i.State.CouplingGroups[0].Phase = "unknown" }},
		{"lost route", func(i *RestoreStateInput) { i.State.Pods[1].Route = nil }},
		{"wrong lane", func(i *RestoreStateInput) { i.State.Pods[1].LaneID = "wrong" }},
		{"wrong distance", func(i *RestoreStateInput) { i.State.Pods[1].Distance += 1 }},
		{"over cap", func(i *RestoreStateInput) { i.State.Pods[1].Route = make([]int, newRouteLimits(i.Network).pod+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := nativeCouplingSavedFixture(t, true, couplingConnected, 1)
			tc.change(&input)
			if s, _, err := RestoreState(input); err == nil || s != nil {
				t.Fatal("invalid coupling state returned a partially recovered simulation", err)
			}
		})
	}
}

// The route blocks of a pod end at the route length (see
// TestRouteBlocksEndAtRouteLength). Thus the member pose search of a
// coupling restore finds each lane of the route, and also the last lane. It
// refuses a distance at the route end.
func TestCouplingMotionPoseAtRouteEnd(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	route, err := s.route(s.network.Stations[0].Berths[0].Node, s.network.Stations[1].Berths[0].Node)
	if err != nil || len(route) < 2 {
		t.Fatalf("route %v: %v", route, err)
	}
	blocks, lengths := s.routeBlocks(route)
	end := 0.0
	for i, length := range lengths {
		if blocks.lanes[i].start != end {
			t.Fatalf("lane %d starts at %v, want %v", i, blocks.lanes[i].start, end)
		}
		if lane, _, _, err := couplingMotionPose(&blocks, end+length/2); err != nil || lane != i {
			t.Fatalf("the pose in the middle of lane %d is in lane %d: %v", i, lane, err)
		}
		end += length
	}
	if blocks.lanes[len(route)].start != end {
		t.Fatalf("the route ends at %v, want %v", blocks.lanes[len(route)].start, end)
	}
	if _, _, _, err := couplingMotionPose(&blocks, end); err == nil || !strings.HasPrefix(err.Error(), "motion leaves its actual route") {
		t.Fatalf("the pose at the route end gives %v", err)
	}
}

// A physical restore can cut the start of a saved route, and the sums of
// the lane lengths of the cut route round differently. A restored pod then
// has a lane distance that differs in the last bits from its route distance
// minus its lane start. These values come from the blocker of the third pod
// journey at the first tick of drain leg 3. The native ordinary pose check
// accepts such a pose only while it is the restore publication.
func TestRestoredLanePose(t *testing.T) {
	t.Parallel()
	start, laneDistance, distance := 107.70329614269009, 4.374771491260958, 112.07806763395104
	if distance-start == laneDistance {
		t.Fatal("the restored pose is an exact ordinary publication")
	}
	fact := nativeForeignFact{pod: Pod{LaneDistance: laneDistance}, distance: distance, restoredPose: true}
	if !restoredLanePose(&fact, start) {
		t.Fatal("the restore publication is refused")
	}
	fact.restoredPose = false
	if restoredLanePose(&fact, start) {
		t.Fatal("a pose that no restore published is accepted")
	}
	fact.restoredPose = true
	for _, offset := range []float64{-2 * restoreTolerance, 2 * restoreTolerance} {
		fact.distance = start + laneDistance + offset
		if !restoredLanePose(&fact, start) {
			t.Fatalf("a restore publication %v from the lane distance is refused", offset)
		}
		fact.distance = math.Nextafter(fact.distance, fact.distance+offset)
		if restoredLanePose(&fact, start) {
			t.Fatalf("a pose more than %v from the lane distance is accepted", offset)
		}
	}
}

// A restore at the first tick of drain leg 3 of the third pod journey
// places the blocker with a restored pose that is not an exact ordinary
// publication. The first native tick accepts it, and the ordinary motion
// of that tick publishes an exact pose.
func TestCouplingRestoredOrdinaryPose(t *testing.T) {
	t.Parallel()
	s, n := newCouplingThirdPodJourney(t, false)
	for s.tick < 20000 && (len(s.couplingGroups) == 0 || s.couplingGroups[0].state.Phase != couplingDraining) {
		followCouplingPair(t, s)
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
	}
	if len(s.couplingGroups) == 0 || s.couplingGroups[0].state.Leg != 3 {
		t.Fatalf("the journey has no drain leg 3 at tick %d", s.tick)
	}
	contracts := couplingThirdPodContracts(n)
	cold, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(),
		CouplingContract: contracts.CouplingContract, CouplingEnabled: true, CouplingSites: contracts.CouplingSites, CouplingCorridors: contracts.CouplingCorridors})
	if err != nil || result.Tier != RestorePhysical {
		t.Fatalf("restore failed: %v %v", result, err)
	}
	v := cold.findVehicle("blocker")
	start := v.blocks.at(v.blockIndex).laneStart
	if v.Pod.Activity != Traveling || !v.restoredPose || v.distance-start == v.Pod.LaneDistance {
		t.Fatalf("the restored blocker has an exact ordinary publication: %+v, restored %t", v.Pod, v.restoredPose)
	}
	cold.Step()
	if err := cold.CouplingError(); err != nil {
		t.Fatal(err)
	}
	start = v.blocks.at(v.blockIndex).laneStart
	if v.restoredPose || v.distance-start != v.Pod.LaneDistance {
		t.Fatalf("the first tick did not publish an exact ordinary pose: %+v, restored %t", v.Pod, v.restoredPose)
	}
}
