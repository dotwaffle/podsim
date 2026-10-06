package sim

import (
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// couplingThirdPodNetwork adds to the approach journey network a third
// origin station that feeds a, and a third goal station past c. A pod from
// the third origin to the third goal follows the pair through the
// corridor.
func couplingThirdPodNetwork(n *Network) {
	compact := classBit(string(CompactClass))
	n.Nodes = append(n.Nodes,
		Node{ID: "third-origin-entry", Position: Point{X: -80, Y: 120}}, Node{ID: "third-origin-berth", Position: Point{X: -50, Y: 80}},
		Node{ID: "third-origin-exit", Position: Point{X: -25, Y: 40}},
		Node{ID: "third-entry", Position: Point{X: 1400}}, Node{ID: "third-berth", Position: Point{X: 1450, Y: 25}}, Node{ID: "third-exit", Position: Point{X: 1510}})
	for _, lane := range []Lane{
		{ID: "third-origin-in", From: "third-origin-entry", To: "third-origin-berth"}, {ID: "third-origin-out", From: "third-origin-berth", To: "third-origin-exit"},
		{ID: "third-origin-through", From: "third-origin-entry", To: "third-origin-exit"}, {ID: "third-origin-feed", From: "third-origin-exit", To: "a"},
		{ID: "third-road", From: "c", To: "third-entry"}, {ID: "third-in", From: "third-entry", To: "third-berth"},
		{ID: "third-out", From: "third-berth", To: "third-exit"}, {ID: "third-through", From: "third-entry", To: "third-exit"},
	} {
		lane.SpeedLimit, lane.VehicleClasses = 14, compact
		n.Lanes = append(n.Lanes, lane)
	}
	n.Stations = append(n.Stations,
		Station{ID: "third-origin", Name: "third-origin", Entry: "third-origin-entry", Exit: "third-origin-exit", VehicleClasses: compact,
			Berths: []Berth{{ID: "third-origin-berth", Node: "third-origin-berth", VehicleClasses: compact}}},
		Station{ID: "third-goal", Name: "third-goal", Entry: "third-entry", Exit: "third-exit", VehicleClasses: compact,
			Berths: []Berth{{ID: "third-berth", Node: "third-berth", VehicleClasses: compact}}})
}

// couplingThirdPodContracts returns the contracts of the journey network.
func couplingThirdPodContracts(n *couplingReservationNetwork) FleetContracts {
	return FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
		CouplingSites: []CouplingSite{n.sites["assembly"], n.sites["split"]}, CouplingCorridors: []CouplingCorridor{n.corridors["corridor"]}}
}

// newCouplingThirdPodJourney is the approach journey with a pod third,
// which moves empty to the third goal after the rear departs. Call
// followCouplingPair before each step. When the pair forms, the third pod
// waits on the assembly lane behind the rear and holds a track cell that
// the train releases at formation.
func newCouplingThirdPodJourney(t *testing.T, occupied bool) (*Simulation, *couplingReservationNetwork) {
	t.Helper()
	p, n := couplingApproachJourneyNetworkWith(t, couplingThirdPodNetwork)
	s, err := p.NewFleetWithContracts([]Placement{
		{ID: "blocker", Class: CompactClass, StationID: "origin"},
		{ID: "front", Class: CompactClass, StationID: "front-origin"},
		{ID: "rear", Class: CompactClass, StationID: "rear-origin"},
		{ID: "third", Class: CompactClass, StationID: "third-origin"},
	}, couplingThirdPodContracts(n))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	for _, trip := range []struct {
		id, goal string
		size     int
	}{{"blocker", "block-goal", 1}, {"front", "front-goal", 1}, {"rear", "rear-goal", 2}} {
		if occupied {
			if err := s.RequestJourneyOptions(trip.id, TripOptions{To: trip.goal, PartySize: trip.size, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		station, _ := s.station(trip.goal)
		if err := s.startEmptyMove(s.findVehicle(trip.id), emptyDestination{station: trip.goal, berth: station.Berths[0], reserveBerth: true}); err != nil {
			t.Fatal(err)
		}
	}
	s.SetMotionRecording(true)
	return s, n
}

// followCouplingPair starts the empty move of the third pod in the first
// tick after the rear departs.
func followCouplingPair(t *testing.T, s *Simulation) {
	t.Helper()
	third := s.findVehicle("third")
	if third.Pod.Activity != Idle || third.Pod.StationID != "third-origin" || s.findVehicle("rear").distance == 0 {
		return
	}
	station, _ := s.station("third-goal")
	if err := s.startEmptyMove(third, emptyDestination{station: "third-goal", berth: station.Berths[0], reserveBerth: true}); err != nil {
		t.Fatal(err)
	}
}

func couplingThirdPodDigest(t *testing.T, s *Simulation) [32]byte {
	t.Helper()
	raw, err := json.Marshal(s.ExportState(), json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

// couplingThirdPodHeld returns each resource that the third pod owns and
// that the group lists with no train owner.
func couplingThirdPodHeld(s *Simulation, g couplingNativeGroup) []resource {
	var held []resource
	for i, claim := range g.context.claims {
		if s.owners[claim.Resource].isPod("third") && g.context.dependencyOwner(g.context.dependencies[i], g.state).isZero() {
			held = append(held, claim.Resource)
		}
	}
	return held
}

// checkCouplingThirdPodSweep checks that the center of the third pod stays
// Clearance from the center of each member while it moves in one tick.
func checkCouplingThirdPodSweep(t *testing.T, s *Simulation, before map[string]Point) {
	t.Helper()
	third := s.findVehicle("third")
	for _, id := range []string{"front", "rear"} {
		member := s.findVehicle(id)
		if member.couplingID == "" {
			continue
		}
		for _, point := range []Point{before[id], member.Pod.Position} {
			if gap := segmentPointDistance(point, before["third"], third.Pod.Position); gap < Clearance-separationTolerance {
				t.Fatalf("tick %d: third pod sweeps %.6f m from member %s", s.tick, gap, id)
			}
		}
	}
}

// couplingThirdPodDone reports whether the train retired and the pair and
// the third pod are idle, with the third pod at its goal.
func couplingThirdPodDone(s *Simulation) bool {
	for _, id := range []string{"front", "rear", "third"} {
		if s.findVehicle(id).Pod.Activity != Idle {
			return false
		}
	}
	return len(s.couplingGroups) == 0 && s.findVehicle("third").Pod.StationID == "third-goal"
}

// stepCouplingThirdPod runs one tick with the checks of each tick: the
// native boundary checks, the member motion oracle, and a swept center
// check of the third pod.
func stepCouplingThirdPod(t *testing.T, s *Simulation) {
	t.Helper()
	motions := couplingMemberMotions(s)
	positions := make(map[string]Point)
	for _, id := range []string{"front", "rear", "third"} {
		positions[id] = s.findVehicle(id).Pod.Position
	}
	followCouplingPair(t, s)
	s.Step()
	checkCouplingApproachNativeBoundary(t, s)
	if err := checkCouplingMemberMotion(s, motions); err != nil {
		t.Fatalf("tick %d: %v", s.tick, err)
	}
	checkCouplingThirdPodSweep(t, s, positions)
}

// A third pod follows the pair from before the approach through formation,
// each group phase, both drain legs, and retirement, with the checks of
// stepCouplingThirdPod. The journeys run empty and with riders. At the
// first tick of each group phase and drain leg, the saved state restores
// twice. Each restored run exports the saved state, passes the same checks
// to the end of the journey, and ends with the same state as its twin. A
// restored run does not match the uninterrupted run, because a physical
// restore starts each pod again at speed 0.
func TestCouplingThirdPodJourney(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, occupied := range []bool{false, true} {
		t.Run(fmt.Sprintf("occupied=%t", occupied), func(t *testing.T) {
			t.Parallel()
			couplingThirdPodJourney(t, occupied)
		})
	}
}

func couplingThirdPodJourney(t *testing.T, occupied bool) {
	t.Helper()
	s, n := newCouplingThirdPodJourney(t, occupied)
	contracts := couplingThirdPodContracts(n)
	restore := func(saved SavedState) *Simulation {
		t.Helper()
		cold, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: saved,
			CouplingContract: contracts.CouplingContract, CouplingEnabled: true, CouplingSites: contracts.CouplingSites, CouplingCorridors: contracts.CouplingCorridors})
		if err != nil || result.Tier != RestorePhysical {
			t.Fatalf("tick %d: restore failed: %v %v", s.tick, result, err)
		}
		if !reflect.DeepEqual(cold.ExportState(), saved) {
			t.Fatalf("tick %d: restore changed the saved state", s.tick)
		}
		cold.SetMotionRecording(true)
		return cold
	}
	var restored [][2]*Simulation
	var keys [][2]int
	formed, retired := int64(-1), int64(-1)
	for s.tick < 20000 && !couplingThirdPodDone(s) {
		stepCouplingThirdPod(t, s)
		if len(s.couplingGroups) == 0 {
			if formed >= 0 && retired < 0 {
				retired = s.tick
			}
			continue
		}
		g := s.couplingGroups[0]
		if formed < 0 {
			formed = s.tick
			third := s.findVehicle("third")
			held := couplingThirdPodHeld(s, g)
			if third.Pod.LaneID != "ab" || third.Pod.LaneDistance >= s.findVehicle("rear").Pod.LaneDistance || len(held) == 0 {
				t.Fatalf("formation at tick %d has no third pod behind the rear on a released resource: %+v, held %v", s.tick, third.Pod, held)
			}
			t.Logf("formation tick %d, third pod at %v m holds %v", s.tick, third.Pod.LaneDistance, held)
		}
		key := [2]int{int(g.state.Phase), g.state.Leg}
		if slices.Contains(keys, key) {
			continue
		}
		keys = append(keys, key)
		saved := s.ExportState()
		restored = append(restored, [2]*Simulation{restore(saved), restore(saved)})
	}
	if retired < 0 || !couplingThirdPodDone(s) {
		t.Fatalf("journey did not finish by tick %d: formation %d, retirement %d, third %+v", s.tick, formed, retired, s.findVehicle("third").Pod)
	}
	want := [][2]int{{int(couplingClosing), 0}, {int(couplingLatching), -1}, {int(couplingConnected), 1}, {int(couplingUnlatching), -1},
		{int(couplingOpening), 2}, {int(couplingDraining), 3}, {int(couplingDraining), 4}}
	if !slices.Equal(keys, want) {
		t.Fatalf("journey has phases %v, want %v", keys, want)
	}
	for i, runs := range restored {
		cold, twin := runs[0], runs[1]
		for cold.tick < s.tick+3000 && !couplingThirdPodDone(cold) {
			stepCouplingThirdPod(t, cold)
			twin.Step()
		}
		if !couplingThirdPodDone(cold) || couplingThirdPodDigest(t, cold) != couplingThirdPodDigest(t, twin) {
			t.Fatalf("restore of phase %v did not finish at tick %d, or its twin differs", want[i], cold.tick)
		}
	}
	t.Logf("formation %d, retirement %d, end %d", formed, retired, s.tick)
}

// Faults and emergencies with the third pod behind a formed train. A fault
// on the third pod starts. An emergency on the rear member waits for the
// split. Debris on cells 2 and 3 of the assembly lane, which the train
// releases at formation but still lists, is refused after both members
// leave that lane, and starts after the train retires. While a member is
// on the lane, its remaining route refuses the debris.
func TestCouplingThirdPodIncidents(t *testing.T) {
	t.Parallel()
	skipLong(t)
	s, _ := newCouplingThirdPodJourney(t, true)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	checkEmergenciesEachTick(t, s)
	debris := debrisCommand("ab", 90)
	formed, refused, split := false, false, false
	for s.tick < 20000 && !split {
		followCouplingPair(t, s)
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		rear := s.findVehicle("rear")
		switch {
		case len(s.couplingGroups) != 0 && !formed:
			formed = true
			if len(couplingThirdPodHeld(s, s.couplingGroups[0])) == 0 {
				t.Fatal("formation has no third pod on a released resource")
			}
			if _, err := podFaultCommand("third")(s); err != nil {
				t.Fatalf("tick %d: fault on the third pod: %v", s.tick, err)
			}
			if _, err := s.Emergency("rear", 0); err != nil {
				t.Fatalf("tick %d: emergency on the rear member: %v", s.tick, err)
			}
		case len(s.couplingGroups) != 0 && (rear.withdrawn != 0 || s.emergencyOf(rear) < 0):
			t.Fatalf("tick %d: the deferred member has the holds %#x", s.tick, rear.withdrawn)
		case len(s.couplingGroups) != 0 && !refused && rear.Pod.LaneID != "ab" && s.findVehicle("front").Pod.LaneID != "ab":
			refused = true
			g := s.couplingGroups[0]
			lane := slices.IndexFunc(s.network.Lanes, func(lane Lane) bool { return lane.ID == "ab" })
			for _, r := range s.debrisFootprint(lane, 90, 92) {
				i := slices.IndexFunc(g.context.claims, func(claim couplingClaim) bool { return claim.Resource == r })
				if i < 0 || !g.context.dependencyOwner(g.context.dependencies[i], g.state).isZero() || !s.owners[r].isZero() {
					t.Fatalf("debris resource %+v is not a free released resource of the claim list", r)
				}
			}
			if _, err := debris(s); !errors.Is(err, errFaultTarget) {
				t.Fatalf("tick %d: debris on a listed released resource: %v", s.tick, err)
			}
		case formed && len(s.couplingGroups) == 0:
			split = true
		}
	}
	if !refused || !split {
		t.Fatal("the train did not leave the assembly lane and retire")
	}
	s.Step()
	if err := s.CouplingError(); err != nil {
		t.Fatal(err)
	}
	if rear := s.findVehicle("rear"); rear.withdrawn != emergencyHold {
		t.Fatalf("the stage after the split did not withdraw the rear: holds %#x", rear.withdrawn)
	}
	if _, err := debris(s); err != nil {
		t.Fatalf("tick %d: debris after retirement: %v", s.tick, err)
	}
	if !slices.ContainsFunc(s.faults, func(f faultRecord) bool { return f.kind == debrisFault }) {
		t.Fatal("debris after retirement has no record")
	}
}
