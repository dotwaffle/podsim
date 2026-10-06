package sim

import (
	"math"
	"testing"
)

// TestIncidentReportBehindFaultedPod faults the head of a station buffer.
// The pod behind it waits for a track cell that the faulted pod owns, so
// it is blocked by the incident, with the fault ID. Each tick counts one
// fault wait tick. The clear ends the report at once, and the pod waits
// again with the ordinary report of a pod ahead.
func TestIncidentReportBehindFaultedPod(t *testing.T) {
	t.Parallel()
	s, release := bufferQueue(t)
	s.faultsOn = true
	head, behind := s.findVehicle("02"), s.findVehicle("01")
	if behind.Pod.WaitReason != TrackOccupied || behind.Pod.BlockedBy != head.Pod.ID {
		t.Fatalf("before the fault, the pod behind reports %q by %q", behind.Pod.WaitReason, behind.Pod.BlockedBy)
	}
	id := startFault(t, s, head, 0)
	checkFaultsEachTick(t, s)
	release()
	s.Step()
	if behind.Pod.WaitReason != BlockedByIncident || behind.Pod.BlockedBy != id {
		t.Fatalf("the pod behind reports %q by %q, want %q by %s", behind.Pod.WaitReason, behind.Pod.BlockedBy, BlockedByIncident, id)
	}
	waits := s.faultCounters.faultWaitTicks
	for range 10 {
		s.Step()
	}
	if got := s.faultCounters.faultWaitTicks - waits; got != 10 {
		t.Fatalf("10 ticks of one waiting pod count %d fault wait ticks", got)
	}
	s.paused = true
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	if behind.Pod.WaitReason != NoWait || behind.Pod.BlockedBy != "" {
		t.Fatalf("after a paused clear, the pod behind reports %q by %q", behind.Pod.WaitReason, behind.Pod.BlockedBy)
	}
	s.paused = false
	waits = s.faultCounters.faultWaitTicks
	s.Step()
	if s.faultCounters.faultWaitTicks != waits || behind.Pod.WaitReason == BlockedByIncident {
		t.Fatalf("after the clear, the pod behind reports %q, and %d wait ticks count", behind.Pod.WaitReason, s.faultCounters.faultWaitTicks-waits)
	}
}

// TestIncidentReportOfBufferHead faults an idle pod at the only berth of
// Market. The head of the station buffer finds the berth held by the
// faulted pod, so it is blocked by the incident. The pod behind the head
// waits for a healthy pod, so it reports the pod ahead.
func TestIncidentReportOfBufferHead(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{
		{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market", BerthID: "market-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	s.faultsOn = true
	s.SetStationBuffers(true)
	id := startFault(t, s, s.findVehicle("03"), 0)
	checkFaultsEachTick(t, s)
	for _, pod := range []string{"01", "02"} {
		if err := s.RequestJourney(pod, "market"); err != nil {
			t.Fatal(err)
		}
	}
	head, behind := s.findVehicle("02"), s.findVehicle("01")
	stepUntil(t, s, "a queue of two at the frontier", func() bool {
		return head.buffered && behind.buffered && head.distance > 0 && head.Pod.Speed == 0 && behind.Pod.Speed == 0 && behind.Pod.BlockedBy == head.Pod.ID
	})
	s.Step()
	if head.Pod.WaitReason != BlockedByIncident || head.Pod.BlockedBy != id {
		t.Fatalf("the head reports %q by %q, want %q by %s", head.Pod.WaitReason, head.Pod.BlockedBy, BlockedByIncident, id)
	}
	if behind.Pod.WaitReason != TrackOccupied || behind.Pod.BlockedBy != head.Pod.ID {
		t.Fatalf("the pod behind reports %q by %q, want the pod ahead", behind.Pod.WaitReason, behind.Pod.BlockedBy)
	}
}

// TestIncidentReportOfBufferQueue faults a pod in a station buffer whose
// tail is still on the entry lane, after it passed the frontier. The
// healthy head behind it is blocked by the incident.
func TestIncidentReportOfBufferQueue(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.faultsOn = true
	ahead, head := &s.vehicles[0], &s.vehicles[1]
	plan := stationBufferPlan{lane: Lane{ID: "market-approach"}, start: 0}
	if !s.bufferHead(head, plan) {
		t.Fatal("the head is not a buffer head with no pod ahead")
	}
	// The pod ahead travels on the entry lane, ahead of the head.
	route, _ := s.route("garden-berth", "market-entry")
	s.setVehicleRoute(ahead, route)
	ahead.Pod.Activity = Traveling
	lane := -1
	for index, l := range ahead.Route {
		if l.ID == plan.lane.ID {
			lane = index
		}
	}
	ahead.distance = ahead.blocks.lanes[lane].start + 30
	head.distance = 10
	if s.bufferHead(head, plan) || head.Pod.WaitReason != TrackOccupied || head.Pod.BlockedBy != ahead.Pod.ID {
		t.Fatalf("behind a healthy pod, the head reports %q by %q", head.Pod.WaitReason, head.Pod.BlockedBy)
	}
	ahead.Pod.Speed = 0
	ahead.faulted = true
	s.faults = append(s.faults, faultRecord{generation: 1, serial: 7, kind: podFault, pod: 0})
	if s.bufferHead(head, plan) || head.Pod.WaitReason != BlockedByIncident || head.Pod.BlockedBy != "i1.7" {
		t.Fatalf("behind a faulted pod, the head reports %q by %q", head.Pod.WaitReason, head.Pod.BlockedBy)
	}
}

// TestIncidentReportAtTerminalBerthChoice faults an idle pod at the only
// berth of s2. A pod with riders to s2 then finds no berth at its terminal
// berth choice, so it is blocked by the incident at the station entry.
// The clear ends the report, and the pod takes the berth in the next tick.
func TestIncidentReportAtTerminalBerthChoice(t *testing.T) {
	t.Parallel()
	s := newLegFleet(t, "s0-1", "s2-1")
	s.incidentContract = IncidentV1Contract
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	checkFaultsEachTick(t, s)
	id := startFault(t, s, s.findVehicle("02"), 0)
	v := s.findVehicle("01")
	if err := s.board(v, newTrip(s, "s0", "s2")); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 waits at the entry of s2", func() bool { return v.Pod.Speed == 0 && v.Pod.Activity == Traveling && v.distance > 0 })
	for range 10 {
		s.Step()
		if v.destination.ID != "" || v.Pod.WaitReason != BlockedByIncident || v.Pod.BlockedBy != id {
			t.Fatalf("tick %d: pod 01 has the berth %q and reports %q by %q", s.tick, v.destination.ID, v.Pod.WaitReason, v.Pod.BlockedBy)
		}
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	if v.Pod.WaitReason != NoWait || v.Pod.BlockedBy != "" {
		t.Fatalf("after the clear, pod 01 reports %q by %q", v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	s.Step()
	if v.destination.ID != "s2-1" {
		t.Fatalf("after the clear, pod 01 has the berth %q", v.destination.ID)
	}
}

// TestReportBlockedBerths checks the report of a failed terminal berth
// choice. With an empty blocked set, it writes nothing. With a blocked
// lane and no blocked berth of the station, it ends the report of the
// previous tick. With blocked berths, it names the fault with the lowest
// serial that blocks a berth of the station.
func TestReportBlockedBerths(t *testing.T) {
	t.Parallel()
	s := newLegFleet(t, "s0-1", "s1-1", "s1-2")
	s.faultsOn = true
	v := s.findVehicle("01")
	v.destinationStation = "s1"
	stale := func() { v.Pod.WaitReason, v.Pod.BlockedBy = TrackOccupied, "02" }
	stale()
	s.reportBlockedBerths(v)
	if v.Pod.WaitReason != TrackOccupied || v.Pod.BlockedBy != "02" {
		t.Fatalf("with no blocked set, the report is %q by %q", v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	blockLanes(t, s, "s0-link")
	s.reportBlockedBerths(v)
	if v.Pod.WaitReason != NoWait || v.Pod.BlockedBy != "" {
		t.Fatalf("with no blocked berth, the report is %q by %q", v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	// Records of serials 3 and 5 block s1-2 and s1-1. A record of serial 2
	// blocks a lane only.
	s.faults = []faultRecord{{generation: 1, serial: 2, kind: debrisFault}, {generation: 1, serial: 3, kind: podFault}, {generation: 1, serial: 5, kind: podFault}}
	s.setBlocked([]faultFootprint{
		{id: "i1.2", resources: []resource{track("s0-link", 0)}},
		{id: "i1.3", resources: []resource{{kind: berthResource, id: "s1-2"}}},
		{id: "i1.5", resources: []resource{{kind: nodeResource, id: "s1-1"}}},
	})
	stale()
	s.reportBlockedBerths(v)
	if v.Pod.WaitReason != BlockedByIncident || v.Pod.BlockedBy != "i1.3" {
		t.Fatalf("with blocked berths, the report is %q by %q, want i1.3", v.Pod.WaitReason, v.Pod.BlockedBy)
	}
}

// TestNoForwardRouteReport places debris on the only lane from s1 to s2
// while pod 01 unloads at s1 with a rider to s2. The next leg has no route,
// so the pod waits with "No forward route" and no fault ID, and each tick
// counts one fault wait tick. After the clear, the next attempt finds the
// route, and the report ends.
func TestNoForwardRouteReport(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s1", "s2")
	stepUntil(t, s, "the unload at s1", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks > 1 })
	checkFaultsEachTick(t, s)
	id := startDebris(t, s, "s1-link", 50, 60, 0)
	stepUntil(t, s, "the failed next leg", func() bool { return v.phaseTicks == 0 })
	s.Step()
	if v.Pod.Activity != Unloading || v.Pod.WaitReason != NoForwardRoute || v.Pod.BlockedBy != "" {
		t.Fatalf("pod 01 is %s and reports %q by %q", v.Pod.Activity, v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	waits := s.faultCounters.faultWaitTicks
	for range 5 {
		s.Step()
	}
	if got := s.faultCounters.faultWaitTicks - waits; got != 5 {
		t.Fatalf("5 ticks count %d fault wait ticks", got)
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if v.Pod.Activity == Unloading || v.Pod.WaitReason == NoForwardRoute {
		t.Fatalf("after the clear, pod 01 is %s and reports %q", v.Pod.Activity, v.Pod.WaitReason)
	}
}

// TestNoForwardRouteEndsWithBlockedSet checks that a failed next leg with
// an empty blocked set ends a report of "No forward route" and writes no
// other report.
func TestNoForwardRouteEndsWithBlockedSet(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s1", "s2")
	stepUntil(t, s, "the unload at s1", func() bool { return v.Pod.Activity == Unloading })
	v.Stops = []string{"unknown"}
	v.Pod.WaitReason = NoForwardRoute
	s.continueJourney(v)
	if v.Pod.WaitReason != NoWait {
		t.Fatalf("with an empty blocked set, the report is %q", v.Pod.WaitReason)
	}
	v.Pod.WaitReason, v.Pod.BlockedBy = TrackOccupied, "02"
	s.continueJourney(v)
	if v.Pod.WaitReason != TrackOccupied || v.Pod.BlockedBy != "02" {
		t.Fatalf("with an empty blocked set, the report %q by %q changed", v.Pod.WaitReason, v.Pod.BlockedBy)
	}
}

// TestFaultWaitTicksCountHealthyPods checks that the fault stage counts
// each healthy pod that waits for an incident once, and no faulted pod.
func TestFaultWaitTicksCountHealthyPods(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	reasons := []WaitReason{BlockedByIncident, NoForwardRoute}
	for index := range s.vehicles {
		s.vehicles[index].Pod.WaitReason = reasons[index]
	}
	s.faultStage()
	if s.faultCounters.faultWaitTicks != 2 {
		t.Fatalf("two waiting pods count %d", s.faultCounters.faultWaitTicks)
	}
	s.vehicles[0].faulted = true
	s.faultStage()
	if s.faultCounters.faultWaitTicks != 3 {
		t.Fatalf("one healthy waiting pod counts %d", s.faultCounters.faultWaitTicks-2)
	}
	s.vehicles[0].faulted = false
	s.vehicles[1].Pod.WaitReason = TrackOccupied
	s.faultCounters.faultWaitTicks = math.MaxInt64
	s.faultStage()
	if s.faultCounters.faultWaitTicks != math.MaxInt64 {
		t.Fatalf("the counter is %d after the maximum", s.faultCounters.faultWaitTicks)
	}
}
