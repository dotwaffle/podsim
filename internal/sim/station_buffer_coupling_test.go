package sim

import "testing"

// An occupied intruder that targets the receiving berth of an empty coupled
// member must not take that berth while the train is committed. The rear
// member of the train relocates empty to rear-goal and holds its single
// berth. The intruder requests rear-goal from a side station while the train
// is connected. With station buffers, its buffered head contests the berth
// (bufferClaimCanYield). Without them, it chooses the berth, and
// redistribution contests it (yieldRelocationClaims). In both modes the berth
// stays with the rear member until the group retires. After that, the
// ordinary empty-relocation yield still gives the berth to the intruder, and
// the intruder completes its journey.
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
	formed, retired, contested, yielded := int64(-1), int64(-1), false, false
	for s.tick < 30000 {
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("coupling fault at tick %d: %v", s.tick, err)
		}
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
		if retired >= 0 && s.owners[berth].isPod("intruder") && rear.Pod.StationID != "rear-goal" {
			yielded = true
		}
		if intruder.Pod.Activity == Idle && intruder.Pod.StationID == "rear-goal" && s.completed == 1 {
			break
		}
	}
	if formed < 0 || retired < 0 || !contested {
		t.Fatalf("fixture did not contest a coupled receiving berth: formed=%d retired=%d contested=%t", formed, retired, contested)
	}
	if !yielded || s.completed != 1 {
		t.Fatalf("ordinary empty relocation did not yield after retirement: yielded=%t completed=%d tick=%d", yielded, s.completed, s.tick)
	}
}
