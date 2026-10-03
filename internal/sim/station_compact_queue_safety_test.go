package sim

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestStationCompactBoundaryFallback(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	group := *s.compactGroups[0]
	group.members = group.members[:2]
	group.bounds = compactQueueBounds{start: 0, frontier: 160}
	states := []compactQueueState{{position: 100, stopBoundary: 160}, {position: 80, stopBoundary: 80}}
	before := append([]compactQueueState(nil), states...)
	if _, err := compactQueueRecoverablePlan(states, group.bounds, 0); err == nil {
		t.Fatal("boundary fixture did not expose zero landing allowance")
	}
	planned, err := s.compactPlan(&group, states)
	if err != nil || !reflect.DeepEqual(planned, states) || !reflect.DeepEqual(states, before) {
		t.Fatalf("legal stopped fallback: %+v %v", planned, err)
	}
}

func TestStationCompactOracleScope(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	for range 200 * TicksPerSecond {
		compactTick(t, s)
		if s.vehicles[3].Pod.Speed == 0 && s.vehicles[0].Pod.Speed == 0 {
			break
		}
	}
	o := s.SafetyObservation()
	if len(o.compactPairs) != 3 {
		t.Fatal("oracle does not certify exactly three direct pairs")
	}
	if _, ok := o.compactPairs[[2]string{"01", "03"}]; ok {
		t.Fatal("nonadjacent pair gained an exception")
	}
	if _, err := o.Check(); err != nil {
		t.Fatal(err)
	}
	without := o
	without.compactPairs = nil
	if _, err := without.Check(); err == nil {
		t.Fatal("uncertified observation accepted a compact gap")
	}
	changed := s.SafetyObservation()
	changed.Pods[1].Speed = 2.5
	if _, err := changed.Check(); err == nil {
		t.Fatal("changed physical pod reused an old certificate exception")
	}
	outside := s.SafetyObservation()
	pod := outside.Pods[0]
	pod.ID = "outside"
	pod.Position.X += Clearance - .1
	outside.Pods = append(outside.Pods, pod)
	outside.Locations[pod.ID] = outside.Locations["01"]
	if _, err := outside.Check(); err == nil {
		t.Fatal("outside pod gained a compact exception")
	}
}

func TestStationCompactLostResourceRejectsUnchanged(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	head := &s.vehicles[s.compactGroups[0].members[0]]
	plan, _ := s.bufferPlan(head)
	r := head.blocks.at(plan.frontier).resources[0]
	s.owners[r] = "05"
	before := s.ExportState()
	if err := s.planCompactQueues(); err == nil {
		t.Fatal("numeric frontier granted unowned track")
	}
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("failed group planning changed state or proof")
	}
	if _, err := s.SafetyObservation().Check(); err == nil {
		t.Fatal("invalid resource certificate granted an oracle exception")
	}
}

func TestStationCompactDisableRetainsRecovery(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"spacing", "buffers", "platooning", "limit"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := compactStateFixture(t)
			switch mode {
			case "spacing":
				if err := s.SetStationQueueSpacing(StationQueueOrdinary); err != nil {
					t.Fatal(err)
				}
			case "buffers":
				s.SetStationBuffers(false)
			case "platooning":
				if err := s.SetPlatooning(PlatooningOff); err != nil {
					t.Fatal(err)
				}
			case "limit":
				if err := s.SetPlatoonLimit(2); err != nil {
					t.Fatal(err)
				}
			}
			for range 1000 * TicksPerSecond {
				compactTick(t, s)
				if len(s.compactGroups) == 0 {
					break
				}
			}
			if len(s.compactGroups) != 0 {
				t.Fatal("disabled group lost finite recovery")
			}
			for i := 1; i < 4; i++ {
				if s.vehicles[i].Pod.Speed != 0 || s.vehicles[i-1].Pod.LaneDistance-s.vehicles[i].Pod.LaneDistance < 12.01 {
					t.Fatal("certificate removed before stopped ordinary gaps")
				}
			}
		})
	}
}

func TestStationCompactSingletonDischarge(t *testing.T) {
	t.Parallel()
	network := stationBufferNetwork(Example(), 8)
	for i := range network.Nodes {
		switch network.Nodes[i].ID {
		case "market-berth":
			network.Nodes[i].Position = Point{X: 5865, Y: 2080}
		case "market-exit":
			network.Nodes[i].Position = Point{X: 5890, Y: 2105}
		}
	}
	for i := range network.Lanes {
		if network.Lanes[i].StationID == "market" {
			network.Lanes[i].SpeedLimit = 2.5
		}
	}
	fleet := []Placement{{ID: "01", StationID: "harbor"}, {ID: "05", StationID: "market"}}
	s := stageBufferFleet(t, network, fleet, 1, false)
	s.owners[resource{kind: berthResource, id: "market-1"}] = "05"
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	active := false
	for range 100 * TicksPerSecond {
		compactTick(t, s)
		if len(s.compactGroups) == 1 {
			active = true
			if _, _, ok := s.divertStart(&s.vehicles[0]); ok {
				t.Fatal("singleton certificate permitted a route change")
			}
			break
		}
	}
	if !active {
		t.Fatal("singleton anticipatory hold did not activate")
	}
	state := s.ExportState()
	s = compactRestore(t, s, state, StationQueueCompactV1)
	if err := s.RequestJourney("05", "harbor"); err != nil {
		t.Fatal(err)
	}
	for range 4000 * TicksPerSecond {
		compactTick(t, s)
		if s.completed == 2 {
			break
		}
	}
	if s.completed != 2 || len(s.compactGroups) != 0 {
		t.Fatalf("singleton remained stranded after the berth became available completed=%d compact=%d state=%+v", s.completed, len(s.compactGroups), s.Snapshot())
	}
}

func TestStationCompactCurvesAndSpeedsReject(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"curve", "higher speed", "unequal station speed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			network := stationBufferNetwork(Example(), 8)
			for i := range network.Lanes {
				network.Lanes[i].SpeedLimit = 2.5
				if mode == "higher speed" {
					network.Lanes[i].SpeedLimit = math.Nextafter(2.5, math.Inf(1))
				}
			}
			for i := range network.Lanes {
				lane := &network.Lanes[i]
				if mode == "unequal station speed" && lane.ID == "market-in" {
					lane.SpeedLimit = 2.4
				}
				if lane.ID != "market-approach" {
					continue
				}
				if mode == "curve" {
					from, _ := network.Node(lane.From)
					to, _ := network.Node(lane.To)
					lane.Control = &Point{X: (from.Position.X + to.Position.X) / 2, Y: (from.Position.Y+to.Position.Y)/2 + 60}
				}
				if mode == "higher speed" {
					lane.SpeedLimit = math.Nextafter(2.5, math.Inf(1))
				}
			}
			s := stagedBufferQueue(t, 2, network, false)
			if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
				t.Fatal(err)
			}
			if _, _, ok := s.compactEntry(&s.vehicles[0]); ok {
				t.Fatal("invalid physical entry passed compact eligibility")
			}
			s.formCompactQueues()
			if len(s.compactGroups) != 0 {
				t.Fatal("invalid physical entry admitted a compact certificate")
			}
		})
	}
}

func TestStationCompactLateAdmissionRejectsUnchanged(t *testing.T) {
	t.Parallel()
	s := occupiedBufferQueue(t, PlatooningOff)
	for range 180 * TicksPerSecond {
		compactTick(t, s)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	before := s.ExportState()
	s.formCompactQueues()
	if len(s.compactGroups) != 0 || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("late headroom reservation changed a stopped ordinary queue")
	}
}

func TestStationCompactExtensionRejectsNoLandingAllowance(t *testing.T) {
	t.Parallel()
	s := occupiedBufferQueue(t, PlatooningVirtual)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	s.admit()
	head := &s.vehicles[0]
	plan, bounds, ok := s.compactEntry(head)
	if !ok || head.reservedThrough != plan.frontier {
		t.Fatal("head did not own early frontier")
	}
	// Build a valid moving follower whose continuous stop is its owned boundary.
	// Setup moves the head past an ordinary release tail and grants the free cell.
	head.Pod.LaneDistance += Clearance + platoonMargin
	head.distance = plan.start + head.Pod.LaneDistance
	head.Pod.Position = s.position(plan.lane, head.Pod.LaneDistance)
	for head.distance > head.blocks.end(head.blockIndex) {
		head.blockIndex++
	}
	s.releaseVehicleResources(head)
	follower := &s.vehicles[1]
	s.grant(intent{index: 1, block: follower.reservedThrough + 1, id: follower.Pod.ID})
	follower.Pod.Speed = 2.5
	follower.Pod.LaneDistance = follower.blocks.end(follower.reservedThrough) - follower.blocks.lanes[len(follower.Route)-1].start - stoppingDistance(2.5)
	follower.distance = follower.blocks.lanes[len(follower.Route)-1].start + follower.Pod.LaneDistance
	follower.Pod.Position = s.position(follower.Route[len(follower.Route)-1], follower.Pod.LaneDistance)
	for follower.distance > follower.blocks.end(follower.blockIndex) {
		follower.blockIndex++
	}
	s.releaseVehicleResources(follower)
	state := compactQueueState{position: head.Pod.LaneDistance, speed: head.Pod.Speed, stopBoundary: bounds.frontier}
	proof, err := compactQueueSingletonRecovery(state, bounds)
	if err != nil {
		t.Fatal(err)
	}
	group := &compactBufferGroup{members: []int{0}, lane: plan.lane.ID, bounds: bounds, recovery: proof}
	if err := compactQueueHoldingValidate(s.compactHoldingState(group, state), bounds, s.platoonLimit); err != nil {
		t.Fatal(err)
	}
	s.compactGroups = []*compactBufferGroup{group}
	checkIncrementalOwners(t, s)
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	before := s.ExportState()
	s.tryCompactFollower(1, 0)
	if follower.link.leader != 0 || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("unsafe extension changed membership or proof")
	}
}

func TestStationCompactUnsupportedProfileReject(t *testing.T) {
	t.Parallel()
	s := occupiedBufferQueue(t, PlatooningVirtual)
	for _, class := range []VehicleClass{GroupClass, ExpressClass, "unknown"} {
		s.vehicles[0].Pod.Class = class
		if _, _, ok := s.compactEntry(&s.vehicles[0]); ok {
			t.Fatal("unsupported body admitted compact formation")
		}
	}
}

func TestStationCompactCapacityIncreaseRecovers(t *testing.T) {
	t.Parallel()
	s := occupiedBufferQueue(t, PlatooningVirtual)
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	for range 200 * TicksPerSecond {
		compactTick(t, s)
		if len(s.compactGroups) == 1 && s.vehicles[0].Pod.LaneDistance > 413 && s.vehicles[0].Pod.Speed == 0 {
			break
		}
	}
	if len(s.compactGroups) != 1 || len(s.compactGroups[0].members) != 2 {
		t.Fatal("two-member hold did not settle")
	}
	if err := s.SetPlatoonLimit(4); err != nil {
		t.Fatal(err)
	}
	if !s.compactGroups[0].recovering {
		t.Fatal("increased capacity consumed unavailable future headroom")
	}
	state := s.ExportState()
	r := compactRestore(t, s, state, StationQueueCompactV1)
	for range 1000 * TicksPerSecond {
		compactTick(t, r)
		if len(r.compactGroups) == 0 {
			break
		}
	}
	if len(r.compactGroups) != 0 {
		t.Fatal("increased-capacity group failed retained recovery")
	}
}

func TestStationCompactGroupPlanFailureIsAtomic(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	if err := s.SetStationQueueSpacing(StationQueueOrdinary); err != nil {
		t.Fatal(err)
	}
	// The second group has lost its entry. Planning the first group must not
	// publish its recovery phase before the complete snapshot succeeds.
	s.compactGroups = append(s.compactGroups, &compactBufferGroup{members: []int{4}, lane: "unavailable"})
	before := cloneCompactGroups(s.compactGroups)
	poses := s.Snapshot()
	if s.planCompactQueues() == nil {
		t.Fatal("invalid second group produced a plan")
	}
	if !reflect.DeepEqual(before, s.compactGroups) || !reflect.DeepEqual(poses, s.Snapshot()) {
		t.Fatal("failed whole-snapshot plan published a phase or motion")
	}
}

func TestStationCompactCloneStorage(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	if err := s.planCompactQueues(); err != nil {
		t.Fatal(err)
	}
	s.compactFault = errors.New("immutable clone fixture fault")
	clone := s.Clone()
	checked := make(map[string]bool)
	checkCloneStorage(t, cloneStorageCheck{path: "Simulation", source: reflect.ValueOf(s).Elem(), clone: reflect.ValueOf(clone).Elem(), checked: checked})
	for _, key := range []string{"Simulation.compactGroups", "Simulation.compactNextGroups", "Simulation.compactMotions", "Simulation.compactFault",
		"compactBufferGroup.members", "compactBufferGroup.recovery", "compactQueueRecovery.targets", "compactQueueRecovery.landingSpeeds"} {
		if !checked[key] {
			t.Fatalf("real compact clone fixture did not cover %s", key)
		}
	}
}

func TestStationCompactSuffixDeniedBeforeRecovery(t *testing.T) {
	t.Parallel()
	s := compactHeldDepartureQueue(t)
	for range 200 * TicksPerSecond {
		compactTick(t, s)
		if len(s.compactGroups) == 1 && len(s.compactGroups[0].members) == 4 && s.vehicles[0].Pod.Speed == 0 &&
			s.vehicles[0].Pod.LaneDistance-s.vehicles[3].Pod.LaneDistance < 20 {
			break
		}
	}
	if err := s.RequestJourney("05", "harbor"); err != nil {
		t.Fatal(err)
	}
	for range 200 * TicksPerSecond {
		if len(s.compactGroups) == 1 && !s.compactGroups[0].recovering && s.compactCanDischarge(s.compactGroups[0]) {
			head := &s.vehicles[s.compactGroups[0].members[0]]
			plan, ok := s.bufferPlan(head)
			if !ok {
				t.Fatal("retained head lost its fixed entry")
			}
			before := s.ExportState()
			s.grantBufferedHead(intent{index: s.compactGroups[0].members[0], block: head.reservedThrough + 1, id: head.Pod.ID}, plan)
			if !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("available berth installed a suffix before ordinary recovery")
			}
			return
		}
		compactTick(t, s)
	}
	t.Fatal("real departure did not expose a ready retained compact head")
}
