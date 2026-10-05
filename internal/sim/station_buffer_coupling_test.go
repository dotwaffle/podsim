package sim

import (
	"fmt"
	"testing"
)

// An occupied intruder that targets the receiving berth of an empty coupled
// member must not take that berth while the train is committed. The rear
// member of the train relocates empty to rear-goal and holds its single
// berth. The intruder requests rear-goal from a side station while the train
// is connected. With station buffers, its buffered head contests the berth
// (bufferClaimCanYield). Without them, it chooses the berth, and
// redistribution contests it (yieldRelocationClaims). In both modes the berth
// stays with the rear member until the group retires. After that, the
// ordinary empty-relocation yield still gives the berth to the intruder, and
// the intruder completes its journey. The empty intruder then blocks the
// berth, and it parks at a parking-only station after rear-goal. The rear
// member must then take the berth and settle with no stale claim.
func TestStationBufferKeepsCoupledReceivingBerth(t *testing.T) {
	t.Parallel()
	coupledReceivingBerthContest(t, true)
}

func TestRedistributionKeepsCoupledReceivingBerth(t *testing.T) {
	t.Parallel()
	coupledReceivingBerthContest(t, false)
}

func coupledReceivingBerthContest(t *testing.T, buffers bool) {
	t.Helper()
	network := couplingMultiBase(t)
	compact := classBit(string(CompactClass))
	network.Nodes = append(network.Nodes, Node{ID: "m", Position: Point{X: 1300, Y: -75}},
		Node{ID: "side-entry", Position: Point{X: 1000, Y: -300}}, Node{ID: "side-berth", Position: Point{X: 1040, Y: -280}}, Node{ID: "side-exit", Position: Point{X: 1080, Y: -260}})
	lanes := network.Lanes[:0]
	for _, lane := range network.Lanes {
		if lane.ID != "rear-road" {
			lanes = append(lanes, lane)
		}
	}
	network.Lanes = lanes
	// The side feed merges after junction c, so the train does not hold the
	// intruder at c while it is committed.
	for _, lane := range []Lane{{ID: "rear-road-a", From: "c", To: "m"}, {ID: "rear-road-b", From: "m", To: "rear-entry", StationRole: StationEntryRole, StationID: "rear-goal"},
		{ID: "side-in", From: "side-entry", To: "side-berth"}, {ID: "side-out", From: "side-berth", To: "side-exit"}, {ID: "side-through", From: "side-entry", To: "side-exit"},
		{ID: "side-feed", From: "side-exit", To: "m"}} {
		lane.SpeedLimit, lane.VehicleClasses = 14, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: "side", Name: "Side", Entry: "side-entry", Exit: "side-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: "side-berth", Node: "side-berth", VehicleClasses: compact}}})
	// Parking after rear-goal lets the idle intruder clear the berth for the
	// rear member after the yield.
	network.Nodes = append(network.Nodes, Node{ID: "park-entry", Position: Point{X: 1600, Y: -175}},
		Node{ID: "park-berth", Position: Point{X: 1640, Y: -200}}, Node{ID: "park-exit", Position: Point{X: 1680, Y: -175}})
	for _, lane := range []Lane{{ID: "park-road", From: "rear-exit", To: "park-entry"}, {ID: "park-in", From: "park-entry", To: "park-berth"},
		{ID: "park-out", From: "park-berth", To: "park-exit"}, {ID: "park-through", From: "park-entry", To: "park-exit"}} {
		lane.SpeedLimit, lane.VehicleClasses = 14, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: "park", Name: "Park", Entry: "park-entry", Exit: "park-exit", VehicleClasses: compact, ParkingOnly: true,
		Berths: []Berth{{ID: "park-berth", Node: "park-berth", VehicleClasses: compact}}})
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	sc := couplingMultiScenario{prepared: p, contracts: FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
		CouplingSites: couplingMultiSites(t, p, ""), CouplingCorridors: []CouplingCorridor{couplingMultiCorridor("")}},
		trips: []couplingMultiTrip{{"blocker", "origin", "block-goal", 1, 0}, {"front", "front-origin", "front-goal", 1, 0}, {"rear", "rear-origin", "rear-goal", 2, 0},
			{"intruder", "side", "rear-goal", 1, -1}}}
	s := sc.start(t, true)
	s.SetStationBuffers(buffers)
	sc.request(t, s, false, map[string]int{})
	berth := resource{kind: berthResource, id: "rear-goal-1"}
	formed, retired, yielded, recovered := int64(-1), int64(-1), int64(-1), int64(-1)
	contested, unsettled := false, "not started"
	// The blocker drives slow lanes, so the fleet settles near tick 31000.
	for s.tick < 40000 {
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("coupling fault at tick %d: %v", s.tick, err)
		}
		checkIncrementalOwners(t, s)
		rear, intruder := s.findVehicle("rear"), s.findVehicle("intruder")
		switch {
		case rear.couplingID != "" && formed < 0:
			formed = s.tick
		case rear.couplingID == "" && formed >= 0 && retired < 0:
			retired = s.tick
		}
		// The connected phase starts well after formation; this delay puts
		// the intruder on the entry lane while the train is committed.
		if formed >= 0 && s.tick == formed+600 {
			if err := s.RequestJourneyOptions("intruder", TripOptions{To: "rear-goal", PartySize: 1, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
		}
		if rear.couplingID != "" {
			if !s.owners[berth].isPod("rear") {
				t.Fatalf("coupled rear member lost its receiving berth to %v at tick %d", s.owners[berth], s.tick)
			}
			contested = contested || intruder.Pod.Occupied && (buffers && intruder.buffered || !buffers && intruder.destination.ID == berth.id)
		}
		if yielded < 0 && retired >= 0 && s.owners[berth].isPod("intruder") && rear.Pod.StationID != "rear-goal" {
			yielded = s.tick
		}
		if recovered < 0 && yielded >= 0 && rear.Pod.Activity == Idle && rear.Pod.StationID != "" {
			recovered = s.tick
		}
		if unsettled = unsettledPod(s); recovered >= 0 && s.completed == 1 && unsettled == "" {
			break
		}
	}
	if formed < 0 || retired < 0 || !contested {
		t.Fatalf("fixture did not contest a coupled receiving berth: formed=%d retired=%d contested=%t", formed, retired, contested)
	}
	if yielded < 0 || s.completed < 1 {
		t.Fatalf("ordinary empty relocation did not yield after retirement: yielded=%d completed=%d tick=%d", yielded, s.completed, s.tick)
	}
	if recovered < 0 || unsettled != "" {
		rear := s.findVehicle("rear")
		t.Fatalf("pods did not settle after the yield: retired=%d yielded=%d recovered=%d tick=%d unsettled=%s rear=%+v",
			retired, yielded, recovered, s.tick, unsettled, rear.Pod)
	}
	t.Logf("formed=%d retired=%d yielded=%d recovered=%d settled=%d", formed, retired, yielded, recovered, s.tick)
	checkSettledOwners(t, s)
	rear, intruder := s.findVehicle("rear"), s.findVehicle("intruder")
	if rear.Pod.BerthID != berth.id || !s.owners[berth].isPod("rear") {
		t.Fatalf("rear settled at %s/%s, and berth %s is owned by %v", rear.Pod.StationID, rear.Pod.BerthID, berth.id, s.owners[berth])
	}
	if s.completed != 1 || len(s.waiting) != 0 || len(intruder.Riders) != 1 || !intruder.Riders[0].Completed {
		t.Fatalf("intruder journey did not complete once: completed=%d waiting=%d riders=%+v", s.completed, len(s.waiting), intruder.Riders)
	}
}

// unsettledPod returns "" when no coupling group remains and each pod is
// idle at a station with no relocation. Otherwise it describes the first
// thing that is not settled.
func unsettledPod(s *Simulation) string {
	if len(s.couplingGroups) != 0 {
		return fmt.Sprintf("%d coupling groups", len(s.couplingGroups))
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Idle || v.Pod.StationID == "" || v.RelocatingTo != "" {
			return fmt.Sprintf("%s %s on %q relocating to %q", v.Pod.ID, v.Pod.Activity, v.Pod.LaneID, v.RelocatingTo)
		}
	}
	return ""
}

// checkSettledOwners requires that each owner entry is the berth or berth
// node of the idle pod that owns it. Call it only after unsettledPod returns "".
func checkSettledOwners(t *testing.T, s *Simulation) {
	t.Helper()
	for r, owner := range s.owners {
		v := s.ownerVehicle(owner)
		if v == nil {
			t.Fatalf("settled owner %v of %v is not a pod", owner, r)
		}
		station, _ := s.station(v.Pod.StationID)
		berth, _ := station.berth(v.Pod.BerthID)
		if r != (resource{kind: berthResource, id: berth.ID}) && r != (resource{kind: nodeResource, id: berth.Node}) {
			t.Fatalf("idle pod %s at %s keeps a stale claim on %v", v.Pod.ID, berth.ID, r)
		}
	}
}
