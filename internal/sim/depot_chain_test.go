package sim

import (
	"slices"
	"strings"
	"testing"
)

// shortDepotNetwork is a depot of lanes lanes with slots slots at the 6 m
// pitch, so each chain lane is shorter than twice its retention.
func shortDepotNetwork(lanes, slots int) Network {
	return depotNetwork(depotLayout{lanes: lanes, slots: slots, pitch: 6, width: 12})
}

// TestDepotShortChainLanes checks that a qualifying chain lane needs only
// its retention, and that each of its cells holds the slot at its end and
// both of its nodes. The lead-in keeps twice its retention and ordinary
// cells.
func TestDepotShortChainLanes(t *testing.T) {
	t.Parallel()
	prepared, err := PrepareNetwork(shortDepotNetwork(2, 4))
	if err != nil {
		t.Fatal(err)
	}
	chain := *findLane(&prepared.network, "parking-chain-2-2")
	retention := prepared.retention.of(chain.ID)
	if retention < 5.49 || retention > 5.5 {
		t.Fatalf("chain retention %g", retention)
	}
	if minimum := prepared.depots.chainMinimum(chain, prepared.retention); minimum != retention {
		t.Fatalf("chain minimum %g, want %g", minimum, retention)
	}
	leadIn := *findLane(&prepared.network, "parking-in-2")
	if minimum := prepared.depots.chainMinimum(leadIn, prepared.retention); minimum != 2*prepared.retention.of(leadIn.ID) {
		t.Fatalf("lead-in minimum %g", minimum)
	}
	through := *findLane(&prepared.network, "parking-through")
	if minimum := prepared.depots.chainMinimum(through, prepared.retention); minimum != LaneMinimumLength(through) {
		t.Fatalf("through minimum %g", minimum)
	}
	cells := prepared.laneCells[chain.ID]
	want := []resource{{kind: berthResource, id: depotSlotID(2, 2)}, {kind: nodeResource, id: chain.From}, {kind: nodeResource, id: chain.To}}
	for cell := range cells.count() {
		got := cells.cell(cell)
		if !slices.Equal(got[:3], want) || slices.ContainsFunc(got[3:], func(r resource) bool { return r.kind != trackResource && r.kind != junctionResource }) {
			t.Fatalf("chain cell %d holds %+v", cell, got)
		}
	}
	if slices.Contains(prepared.laneCells[leadIn.ID].cell(0), (resource{kind: berthResource, id: depotSlotID(2, 4)})) {
		t.Fatal("the first lead-in cell holds the tail slot")
	}
}

// TestDepotChainQualification checks the example of a 20 m lead-out that
// ends at the exit: chain lane 2 to 1 does not qualify, because its window
// reaches the exit, and chain lane 3 to 2 does. The window of lane 3 to 2
// still covers the slot pairs, so the audit passes, and the length check
// refuses lane 2 to 1 with the class minimum.
func TestDepotChainQualification(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane Lane) bool { return lane.ID == "parking-throat-1" })
	network.Nodes = slices.DeleteFunc(network.Nodes, func(node Node) bool { return node.ID == "parking-tail-1" })
	head := findNode(&network, depotSlotID(1, 1)).Position
	exit := findNode(&network, "parking-exit")
	exit.Position = Point{X: head.X - 20, Y: head.Y}
	findLane(&network, "parking-out-1").To = "parking-exit"
	findLane(&network, "parking-through").Control = &Point{X: (620 + exit.Position.X) / 2, Y: 360}
	retention := newLaneRetention(network, indexGeometryTails(network))
	if retention.of("parking-chain-1-1") != 0 || retention.of("parking-chain-1-2") == 0 {
		t.Fatalf("retention 2 to 1 %g, 3 to 2 %g", retention.of("parking-chain-1-1"), retention.of("parking-chain-1-2"))
	}
	if _, err := PrepareNetwork(network); err == nil || err.Error() != `lane "parking-chain-1-1" must be at least 24 meters long` {
		t.Fatalf("got %v", err)
	}
}

// TestDepotAuditClip checks that the audit clips each lane to the windows
// of the chain lanes: at the depot speed, the lead-in passes against slot
// K-1 and the lead-out against slot 2, and at 2 m/s each pair needs more
// than the 6 m pitch.
func TestDepotAuditClip(t *testing.T) {
	t.Parallel()
	if _, err := PrepareNetwork(shortDepotNetwork(1, 4)); err != nil {
		t.Fatal(err)
	}
	for _, lane := range []string{"parking-in-1", "parking-out-1"} {
		network := shortDepotNetwork(1, 4)
		findLane(&network, lane).SpeedLimit = 2
		if _, err := PrepareNetwork(network); err == nil || !strings.Contains(err.Error(), `"`+lane+`"`) || !strings.HasSuffix(err.Error(), "are less than 7 meters apart") {
			t.Fatalf("%s at 2 m/s: %v", lane, err)
		}
	}
}

// TestDepotAtomicSpanGrant checks that no pod takes a cell of a short
// chain lane while another pod holds the slot at its end: the head leaves
// slot 1, and the pod at slot 2 that starts toward slot 1 in the same tick
// waits until the head releases slot 1, and then moves up.
func TestDepotAtomicSpanGrant(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, depotPlacements(network, 2))
	if err != nil {
		t.Fatal(err)
	}
	head, behind := s.findVehicle("depot-pod-01"), s.findVehicle("depot-pod-02")
	station, _ := s.station("parking")
	slot1, _ := station.berth(depotSlotID(1, 1))
	harbor, _ := s.station("harbor")
	if err := s.startEmptyMove(head, emptyDestination{station: "harbor", berth: harbor.Berths[0]}); err != nil {
		t.Fatal(err)
	}
	if err := s.startEmptyMove(behind, emptyDestination{station: "parking", berth: slot1}); err != nil {
		t.Fatal(err)
	}
	held := func() bool {
		return s.owners[resource{kind: berthResource, id: slot1.ID}].isPod(head.Pod.ID) || s.owners[resource{kind: nodeResource, id: slot1.Node}].isPod(head.Pod.ID)
	}
	waited := false
	for range 60 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		switch {
		case behind.reservedThrough >= 0 && held():
			t.Fatalf("tick %d: the pod behind holds a chain cell while the head holds slot 1", s.tick)
		case behind.reservedThrough < 0 && held():
			waited = true
		case behind.Pod.Activity == Idle && behind.Pod.BerthID == slot1.ID:
			if !waited {
				t.Fatal("the pod behind never waited for the head")
			}
			return
		}
	}
	t.Fatalf("the pod behind did not move up: %+v", behind.Pod)
}

// TestDepotPlatoonGate checks that a depot lane joins no platoon run, also
// a lead-out with a departure role that both routes continue after.
func TestDepotPlatoonGate(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, depotPlacements(network, 2))
	if err != nil {
		t.Fatal(err)
	}
	route := []Lane{*findLane(&s.network, "parking-out-1"), *findLane(&s.network, "parking-throat-1"), *findLane(&s.network, "return"), *findLane(&s.network, "harbor-approach")}
	if route[0].StationRole != StationDepartureRole {
		t.Fatalf("lead-out role %q", route[0].StationRole)
	}
	v, leader := s.findVehicle("depot-pod-01"), s.findVehicle("depot-pod-02")
	for _, pod := range []*vehicle{v, leader} {
		pod.Route, pod.destinationStation = route, "harbor"
	}
	if s.sharedLane(v, leader, 0, 0) {
		t.Fatal("the lead-out can join a run")
	}
	if !s.sharedLane(v, leader, 2, 2) {
		t.Fatal("the return lane cannot join a run")
	}
}
