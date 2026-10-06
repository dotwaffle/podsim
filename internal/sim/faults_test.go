package sim

import (
	"errors"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

// faultFixture is claimKindFixture with faults on. Pod 01 travels to
// Market, and pod 02 is idle at Garden.
func faultFixture(t *testing.T) (s *Simulation, traveling, idle *vehicle) {
	t.Helper()
	s, traveling, idle = claimKindFixture(t)
	s.faultsOn = true
	s.faultSettings.evacuationSeconds = 300
	return s, traveling, idle
}

// startFault starts a pod fault and fails the test on an error.
func startFault(t *testing.T, s *Simulation, v *vehicle, duration int64) string {
	t.Helper()
	id, err := s.startPodFault(v, duration)
	if err != nil {
		t.Fatalf("fault on pod %s: %v", v.Pod.ID, err)
	}
	return id
}

// sameRouteSlice reports whether a and b are the same route slice, so a
// pod kept its route and did not get an equal copy.
func sameRouteSlice(a, b []Lane) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// faultIDs returns the IDs of the active records, in record order.
func faultIDs(s *Simulation) []string {
	ids := make([]string, 0, len(s.faults))
	for _, record := range s.faults {
		ids = append(ids, record.id())
	}
	return ids
}

// TestStartPodFaultRecord checks the record, the ID, the duration and the
// counter of a pod fault on a traveling pod and on a pod at a berth.
func TestStartPodFaultRecord(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		duration int64
		end      int64
	}{
		{"no end", 0, 0},
		{"one second", 1, TicksPerSecond},
		{"longest", maxFaultSeconds, maxFaultSeconds * TicksPerSecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, traveling, idle := faultFixture(t)
			s.SetIncidentGeneration(7)
			s.incidentSerial = 41
			first := startFault(t, s, traveling, test.duration)
			second := startFault(t, s, idle, 0)
			if first != "i7.42" || second != "i7.43" {
				t.Fatalf("IDs %s and %s, want i7.42 and i7.43", first, second)
			}
			want := []faultRecord{
				{generation: 7, serial: 42, kind: podFault, start: s.tick, pod: s.vehicleIndex(traveling)},
				{generation: 7, serial: 43, kind: podFault, start: s.tick, pod: s.vehicleIndex(idle)},
			}
			if test.end != 0 {
				want[0].end = s.tick + test.end
			}
			if !slices.Equal(s.faults, want) {
				t.Fatalf("records %+v, want %+v", s.faults, want)
			}
			if !traveling.faulted || !idle.faulted || s.faultCounters.started != 2 {
				t.Fatalf("faulted %t and %t, started %d", traveling.faulted, idle.faulted, s.faultCounters.started)
			}
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestPodFaultActivities checks that a pod fault is accepted on a pod at a
// berth in each berth activity, and refused on a pod in a berth activity
// with no berth.
func TestPodFaultActivities(t *testing.T) {
	t.Parallel()
	for _, activity := range []Activity{Idle, Boarding, Unloading, Continuing, DepartingEmpty} {
		t.Run(string(activity), func(t *testing.T) {
			t.Parallel()
			s, _, idle := faultFixture(t)
			idle.Pod.Activity = activity
			startFault(t, s, idle, 0)
			s, _, idle = faultFixture(t)
			idle.Pod.Activity, idle.Pod.BerthID = activity, ""
			before := s.Clone()
			if _, err := s.startPodFault(idle, 0); !errors.Is(err, errFaultTarget) {
				t.Fatalf("no berth: error %v, want %v", err, errFaultTarget)
			}
			if !sameState(before, s) {
				t.Fatal("the refused fault changed the state")
			}
		})
	}
}

// TestPodFaultRefusals checks each refusal of a pod fault against the
// refusal oracle: the state after the call equals a clone from before it.
// Each case then removes its cause, and the same call succeeds.
func TestPodFaultRefusals(t *testing.T) {
	t.Parallel()
	limit := int64(math.MaxInt64)
	tests := []struct {
		name     string
		duration int64
		want     error
		// cause sets up the refusal and returns the pod of the call. undo
		// removes the cause.
		cause func(s *Simulation, traveling, idle *vehicle) *vehicle
		undo  func(s *Simulation, v *vehicle)
	}{
		{"faults off", 0, errFaultsOff,
			func(s *Simulation, traveling, _ *vehicle) *vehicle { s.faultsOn = false; return traveling },
			func(s *Simulation, _ *vehicle) { s.faultsOn = true }},
		{"negative duration", -1, errFaultDuration, nil, nil},
		{"duration above the limit", maxFaultSeconds + 1, errFaultDuration, nil, nil},
		{"end tick past the limit", maxFaultSeconds, errIncidentLimit,
			func(s *Simulation, traveling, _ *vehicle) *vehicle {
				s.tick = limit - maxFaultSeconds*TicksPerSecond + 1
				return traveling
			},
			func(s *Simulation, _ *vehicle) { s.tick-- }},
		{"evacuation tick past the limit", 0, errIncidentLimit,
			func(s *Simulation, traveling, _ *vehicle) *vehicle {
				s.faultSettings.evacuationSeconds = 3600
				s.tick = limit - 3600*TicksPerSecond + 1
				return traveling
			},
			func(s *Simulation, _ *vehicle) { s.tick-- }},
		{"serial at the limit", 0, errIncidentLimit,
			func(s *Simulation, traveling, _ *vehicle) *vehicle {
				s.incidentSerial = math.MaxUint64
				return traveling
			},
			func(s *Simulation, _ *vehicle) { s.incidentSerial-- }},
		{"no pod", 0, errUnknownPod,
			func(*Simulation, *vehicle, *vehicle) *vehicle { return nil }, nil},
		{"pod outside the fleet", 0, errUnknownPod,
			func(_ *Simulation, traveling, _ *vehicle) *vehicle { outside := *traveling; return &outside }, nil},
		{"second fault", 0, errPodFaulted,
			func(s *Simulation, traveling, _ *vehicle) *vehicle {
				if _, err := s.startPodFault(traveling, 0); err != nil {
					panic(err)
				}
				return traveling
			},
			func(s *Simulation, _ *vehicle) {
				if err := s.clearFault(s.faults[0].id()); err != nil {
					panic(err)
				}
			}},
		{"platoon leader", 0, errFaultTarget,
			func(_ *Simulation, traveling, _ *vehicle) *vehicle { traveling.follower = 2; return traveling },
			func(_ *Simulation, v *vehicle) { v.follower = 0 }},
		{"platoon follower", 0, errFaultTarget,
			func(_ *Simulation, traveling, _ *vehicle) *vehicle { traveling.link.leader = 2; return traveling },
			func(_ *Simulation, v *vehicle) { v.link.leader = 0 }},
		{"compact queue member", 0, errFaultTarget,
			func(s *Simulation, traveling, _ *vehicle) *vehicle {
				s.compactGroups = []*compactBufferGroup{{members: []int{s.vehicleIndex(traveling)}}}
				return traveling
			},
			func(s *Simulation, _ *vehicle) { s.compactGroups = nil }},
		{"dispatch pass", 0, errFaultDispatch,
			func(s *Simulation, _, idle *vehicle) *vehicle { s.pass.active = true; return idle },
			func(s *Simulation, _ *vehicle) { s.pass.active = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, traveling, idle := faultFixture(t)
			v := traveling
			if test.cause != nil {
				v = test.cause(s, traveling, idle)
			}
			before := s.Clone()
			if _, err := s.startPodFault(v, test.duration); !errors.Is(err, test.want) {
				t.Fatalf("error %v, want %v", err, test.want)
			}
			if !sameState(before, s) {
				t.Fatal("the refused fault changed the state")
			}
			if test.undo == nil {
				return
			}
			// Control: the same call succeeds once the cause is gone.
			test.undo(s, v)
			if _, err := s.startPodFault(v, test.duration); err != nil {
				t.Fatalf("control: %v", err)
			}
		})
	}
}

// TestPodFaultTicksAtTheLimit checks that the largest end tick and
// evacuation tick, and the last serial, are accepted.
func TestPodFaultTicksAtTheLimit(t *testing.T) {
	t.Parallel()
	s, traveling, _ := faultFixture(t)
	s.tick = math.MaxInt64 - maxFaultSeconds*TicksPerSecond
	startFault(t, s, traveling, maxFaultSeconds)
	if s.faults[0].end != math.MaxInt64 {
		t.Fatalf("end tick %d, want %d", s.faults[0].end, int64(math.MaxInt64))
	}
	evacuating, traveling, idle := faultFixture(t)
	evacuating.faultSettings.evacuationSeconds = 3600
	evacuating.tick = math.MaxInt64 - 3600*TicksPerSecond
	startFault(t, evacuating, traveling, 0)
	evacuating.incidentSerial = math.MaxUint64 - 1
	if id := startFault(t, evacuating, idle, 0); id != "i0.18446744073709551615" {
		t.Fatalf("ID %s, want the last serial", id)
	}
}

// TestPodFaultPreconditionOrder checks the order of the preconditions. The
// call starts with each precondition false, and the test makes them true
// one at a time. Each call refuses with the error of the first false one.
func TestPodFaultPreconditionOrder(t *testing.T) {
	t.Parallel()
	s, traveling, _ := faultFixture(t)
	s.faultsOn, s.incidentSerial = false, math.MaxUint64
	traveling.faulted, traveling.follower, s.pass.active = true, 2, true
	var v *vehicle
	duration := int64(-1)
	for _, step := range []struct {
		want error
		fix  func()
	}{
		{errFaultsOff, func() { s.faultsOn = true }},
		{errFaultDuration, func() { duration = 0 }},
		{errIncidentLimit, func() { s.incidentSerial = 0 }},
		{errUnknownPod, func() { v = traveling }},
		{errPodFaulted, func() { traveling.faulted = false }},
		{errFaultTarget, func() { traveling.follower = 0 }},
		{errFaultDispatch, func() { s.pass.active = false }},
	} {
		if _, err := s.startPodFault(v, duration); !errors.Is(err, step.want) {
			t.Fatalf("error %v, want %v", err, step.want)
		}
		step.fix()
	}
	startFault(t, s, v, duration)
}

// TestClearFault checks a clear at a paused command boundary. The record
// ends, the pod is no longer faulted, and each wait report that names the
// fault ends. A report that names another fault or a pod stays. A second
// clear, and a clear with faults off, change nothing.
func TestClearFault(t *testing.T) {
	t.Parallel()
	s, traveling, idle := faultFixture(t)
	s.SetPaused(true)
	first := startFault(t, s, traveling, 0)
	second := startFault(t, s, idle, 0)
	traveling.Pod.WaitReason, traveling.Pod.BlockedBy = "Blocked by incident", first
	idle.Pod.WaitReason, idle.Pod.BlockedBy = "Blocked by incident", second
	if err := s.clearFault(first); err != nil {
		t.Fatal(err)
	}
	if traveling.faulted || !idle.faulted || !slices.Equal(faultIDs(s), []string{second}) {
		t.Fatalf("faulted %t and %t, records %v", traveling.faulted, idle.faulted, faultIDs(s))
	}
	if traveling.Pod.WaitReason != NoWait || traveling.Pod.BlockedBy != "" {
		t.Fatalf("pod 01 waits with %q by %q", traveling.Pod.WaitReason, traveling.Pod.BlockedBy)
	}
	if idle.Pod.BlockedBy != second || s.faultCounters.cleared != 1 {
		t.Fatalf("pod 02 blocked by %q, cleared %d", idle.Pod.BlockedBy, s.faultCounters.cleared)
	}
	before := s.Clone()
	if err := s.clearFault(first); !errors.Is(err, errUnknownFault) {
		t.Fatalf("second clear: error %v, want %v", err, errUnknownFault)
	}
	s.faultsOn = false
	if err := s.clearFault(second); !errors.Is(err, errFaultsOff) {
		t.Fatalf("clear with faults off: error %v, want %v", err, errFaultsOff)
	}
	s.faultsOn = true
	if !sameState(before, s) {
		t.Fatal("a refused clear changed the state")
	}
	// A clear wins over the wait report of a pod.
	s.vehicles[0].Pod.BlockedBy = "02"
	if err := s.clearFault(second); err != nil {
		t.Fatal(err)
	}
	if s.vehicles[0].Pod.BlockedBy != "02" || len(s.faults) != 0 || s.faultCounters.cleared != 2 {
		t.Fatalf("blocked by %q, records %v, cleared %d", s.vehicles[0].Pod.BlockedBy, faultIDs(s), s.faultCounters.cleared)
	}
}

// TestClearFaultOfAbandonedTimeline checks a rewind. A fault that started
// after the checkpoint has an ID that no record of the rewound simulation
// has, also after a new fault with the same serial.
func TestClearFaultOfAbandonedTimeline(t *testing.T) {
	t.Parallel()
	s, traveling, idle := faultFixture(t)
	s.SetIncidentGeneration(1)
	kept := startFault(t, s, traveling, 0)
	checkpoint := s.Clone()
	abandoned := startFault(t, s, idle, 0)
	checkpoint.SetIncidentGeneration(2)
	fresh := startFault(t, checkpoint, &checkpoint.vehicles[s.vehicleIndex(idle)], 0)
	if kept != "i1.1" || abandoned != "i1.2" || fresh != "i2.2" {
		t.Fatalf("IDs %s, %s and %s", kept, abandoned, fresh)
	}
	before := checkpoint.Clone()
	if err := checkpoint.clearFault(abandoned); !errors.Is(err, errUnknownFault) {
		t.Fatalf("error %v, want %v", err, errUnknownFault)
	}
	if !sameState(before, checkpoint) {
		t.Fatal("the refused clear changed the state")
	}
	for _, id := range []string{kept, fresh} {
		if err := checkpoint.clearFault(id); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFaultStageClearsAtTheEnd checks the timed clears of the fault stage.
// A record clears in the tick in which the tick reaches its end, and not
// before. Two adjacent records with the same end clear in the same tick.
// A record with no end, and the later records, stay in serial order. A
// paused simulation does not clear.
func TestFaultStageClearsAtTheEnd(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "parking", BerthID: "parking-1"}, {ID: "04", StationID: "parking", BerthID: "parking-2"}})
	if err != nil {
		t.Fatal(err)
	}
	s.faultsOn = true
	ids := make([]string, 4)
	for index, duration := range []int64{1, 1, 0, 2} {
		ids[index] = startFault(t, s, &s.vehicles[index], duration)
	}
	end := s.tick + TicksPerSecond
	for s.tick < end-1 {
		s.Step()
	}
	if len(s.faults) != 4 {
		t.Fatalf("tick %d: records %v, want each record", s.tick, faultIDs(s))
	}
	s.Step()
	if want := []string{ids[2], ids[3]}; !slices.Equal(faultIDs(s), want) {
		t.Fatalf("tick %d: records %v, want %v", s.tick, faultIDs(s), want)
	}
	if s.vehicles[0].faulted || s.vehicles[1].faulted || !s.vehicles[2].faulted || s.faultCounters.cleared != 2 {
		t.Fatalf("faulted %t %t %t, cleared %d", s.vehicles[0].faulted, s.vehicles[1].faulted, s.vehicles[2].faulted, s.faultCounters.cleared)
	}
	s.SetPaused(true)
	for range 2 * TicksPerSecond {
		s.Step()
	}
	if len(s.faults) != 2 {
		t.Fatalf("a paused simulation cleared a record: %v", faultIDs(s))
	}
	s.SetPaused(false)
	for range TicksPerSecond {
		s.Step()
	}
	if !slices.Equal(faultIDs(s), []string{ids[2]}) || s.faultCounters.cleared != 3 {
		t.Fatalf("records %v, cleared %d", faultIDs(s), s.faultCounters.cleared)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestFaultCountersSaturate checks that each counter stops at
// math.MaxInt64, and that a fault start and a timed clear complete with a
// counter at the maximum.
func TestFaultCountersSaturate(t *testing.T) {
	t.Parallel()
	s, traveling, _ := faultFixture(t)
	maximum := int64(math.MaxInt64)
	s.faultCounters = faultCounters{started: maximum - 1, cleared: maximum, evacuations: maximum, reroutes: maximum, faultWaitTicks: maximum}
	startFault(t, s, traveling, 1)
	startFault(t, s, s.findVehicle("02"), 0)
	for range TicksPerSecond {
		s.Step()
	}
	if len(s.faults) != 1 || traveling.faulted {
		t.Fatalf("the timed clear did not complete: records %v", faultIDs(s))
	}
	c := &s.faultCounters
	for _, counter := range []*int64{&c.evacuations, &c.reroutes, &c.faultWaitTicks} {
		countFault(counter)
	}
	want := faultCounters{started: maximum, cleared: maximum, evacuations: maximum, reroutes: maximum, faultWaitTicks: maximum}
	if *c != want {
		t.Fatalf("counters %+v, want each at the maximum", *c)
	}
}

// TestResetEndsFaults checks that Reset clears the records, the counters
// and the blocked set, with the routes of the blocked epoch. It keeps the
// incident serial and the fault settings.
func TestResetEndsFaults(t *testing.T) {
	t.Parallel()
	s, traveling, _ := faultFixture(t)
	startFault(t, s, traveling, 0)
	s.setBlocked([]faultFootprint{{id: "i0.1", resources: []resource{{kind: berthResource, id: "garden-1"}}}})
	if _, err := s.routeForClass("harbor-berth", "garden-berth", LegacyClass); err == nil {
		t.Fatal("the route to the blocked berth exists")
	}
	s.Reset()
	if s.faults != nil || s.faultCounters != (faultCounters{}) || s.blockedActive() || s.rerouteDue {
		t.Fatalf("records %v, counters %+v, blocked %t, reroute %t", faultIDs(s), s.faultCounters, s.blockedActive(), s.rerouteDue)
	}
	if _, err := s.routeForClass("harbor-berth", "garden-berth", LegacyClass); err != nil {
		t.Fatalf("a route of the blocked epoch stays after the reset: %v", err)
	}
	if !s.faultsOn || s.faultSettings.evacuationSeconds != 300 || s.incidentSerial != 1 {
		t.Fatalf("faults on %t, settings %+v, serial %d", s.faultsOn, s.faultSettings, s.incidentSerial)
	}
	if id := startFault(t, s, s.findVehicle("01"), 0); id != "i0.2" {
		t.Fatalf("ID after the reset %s, want i0.2", id)
	}
}

// TestFaultCheckpointReplay checks that a clone with active records
// replays exactly, through a timed clear, and that the source does not
// change the records of the clone.
func TestFaultCheckpointReplay(t *testing.T) {
	t.Parallel()
	s, traveling, idle := faultFixture(t)
	startFault(t, s, traveling, 2)
	startFault(t, s, idle, 0)
	checkpoint := s.Clone()
	twin := s.Clone()
	for range 3 * TicksPerSecond {
		s.Step()
	}
	if err := s.clearFault(s.faults[0].id()); err != nil {
		t.Fatal(err)
	}
	if len(checkpoint.faults) != 2 || !sameState(checkpoint, twin) {
		t.Fatalf("the source changed the clone: records %v", faultIDs(checkpoint))
	}
	for range 3 * TicksPerSecond {
		checkpoint.Step()
	}
	if err := checkpoint.clearFault(checkpoint.faults[0].id()); err != nil {
		t.Fatal(err)
	}
	if !sameState(checkpoint, s) {
		t.Fatal("the replay differs from the source")
	}
}

// TestFaultsOnWithoutFaults checks that faults on with no record changes
// no state: a run with faults on equals a run with faults off, apart from
// the settings.
func TestFaultsOnWithoutFaults(t *testing.T) {
	t.Parallel()
	off, on := newTraffic(t), newTraffic(t)
	for _, s := range []*Simulation{off, on} {
		if err := s.StartDemo(); err != nil {
			t.Fatal(err)
		}
	}
	on.faultsOn, on.faultSettings.evacuationSeconds = true, 300
	for range 120 * TicksPerSecond {
		off.Step()
		on.Step()
	}
	on.faultsOn, on.faultSettings = false, faultSettings{}
	if off.completed == 0 || !sameState(off, on) {
		t.Fatalf("the runs differ: completed %d and %d", off.completed, on.completed)
	}
}

// TestFaultOwnerKind checks the names of a fault owner.
func TestFaultOwnerKind(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	owner := resourceOwner{kind: faultOwnerKind, id: "i3.4"}
	if owner.String() != "i3.4" || owner.podID() != "" || owner.isPod("i3.4") || s.ownerVehicle(owner) != nil {
		t.Fatalf("fault owner %q, pod %q", owner.String(), owner.podID())
	}
}

// TestCheckFaults checks that CheckContract refuses each damaged fault
// record, each faulted flag without a record, a faulted pod without the
// fault hold or in a group, and a cap outside the pod and its grants.
func TestCheckFaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		damage func(s *Simulation)
		want   string
	}{
		{"unknown kind", func(s *Simulation) { s.faults[1].kind = debrisFault + 1 }, "unknown kind"},
		{"duplicate record", func(s *Simulation) { s.faults = append(s.faults, s.faults[1]) }, "serial order"},
		{"serials out of order", func(s *Simulation) { s.faults[0], s.faults[1] = s.faults[1], s.faults[0] }, "serial order"},
		{"negative start", func(s *Simulation) { s.faults[1].start = -1 }, "starts at tick"},
		{"future start", func(s *Simulation) { s.faults[1].start = s.tick + 1 }, "starts at tick"},
		{"end at the start", func(s *Simulation) { s.faults[1].end = s.faults[1].start }, "not after its start"},
		{"negative end", func(s *Simulation) { s.faults[1].end = -1 }, "not after its start"},
		{"negative pod index", func(s *Simulation) { s.faults[1].pod = -1 }, "outside the fleet"},
		{"pod index past the fleet", func(s *Simulation) { s.faults[1].pod = len(s.vehicles) }, "outside the fleet"},
		{"two records of one pod", func(s *Simulation) { s.faults[1].pod = s.faults[0].pod }, "two fault records"},
		{"record of a pod that is not faulted", func(s *Simulation) { s.vehicles[s.faults[1].pod].faulted = false }, "not faulted"},
		{"faulted pod without a record", func(s *Simulation) { s.faults = s.faults[:1] }, "no fault record"},
		{"faulted pod with no records", func(s *Simulation) { s.faults = nil; s.vehicles[0].faulted = true }, "no fault record"},
		{"record of a pod without the fault hold", func(s *Simulation) { s.vehicles[s.faults[1].pod].withdrawn = 0 }, "no fault hold"},
		{"faulted platoon follower", func(s *Simulation) { s.vehicles[s.faults[0].pod].link.leader = 2 }, "member of a compact queue or a platoon"},
		{"cap behind the pod", func(s *Simulation) {
			v := &s.vehicles[s.faults[0].pod]
			v.faultCap = math.Nextafter(v.distance, 0)
		}, "outside its distance"},
		{"cap past the grants", func(s *Simulation) {
			v := &s.vehicles[s.faults[0].pod]
			v.faultCap = math.Nextafter(v.blocks.end(v.reservedThrough), math.Inf(1))
		}, "outside its distance"},
		{"cap that is not a number", func(s *Simulation) { s.vehicles[s.faults[0].pod].faultCap = math.NaN() }, "outside its distance"},
		{"cleared blocked lane", func(s *Simulation) {
			lanes := slices.Clone(s.blocked.lanes)
			lanes[slices.Index(lanes, true)] = false
			s.blocked.lanes = lanes
		}, "blocked set differs"},
		{"changed blocking fault", func(s *Simulation) {
			by := maps.Clone(s.blocked.by)
			for r := range by {
				by[r] = "i9.9"
				break
			}
			s.blocked.by = by
		}, "blocked set differs"},
		{"emergency hold with faults on", func(s *Simulation) { s.vehicles[s.faults[1].pod].withdrawn |= emergencyHold }, "not only the fault hold"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, traveling, idle := faultFixture(t)
			startFault(t, s, traveling, 0)
			startFault(t, s, idle, 1)
			if err := s.CheckContract(); err != nil {
				t.Fatalf("before the damage: %v", err)
			}
			test.damage(s)
			err := s.CheckContract()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %v, want one with %q", err, test.want)
			}
		})
	}
}

// TestSetFaults checks the fault switch and its refusals. A refused call
// changes nothing.
func TestSetFaults(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: maxEvacuationSeconds}); err != nil {
		t.Fatal(err)
	}
	if !s.faultsOn || s.faultSettings.evacuationSeconds != maxEvacuationSeconds {
		t.Fatalf("faults on %t, settings %+v", s.faultsOn, s.faultSettings)
	}
	id := startFault(t, s, s.findVehicle("01"), 0)
	before := s.Clone()
	for _, test := range []struct {
		name     string
		enabled  bool
		settings FaultSettings
	}{
		{"negative delay", true, FaultSettings{EvacuationSeconds: -1}},
		{"delay above the limit", true, FaultSettings{EvacuationSeconds: maxEvacuationSeconds + 1}},
		{"off with an active fault", false, FaultSettings{}},
	} {
		if err := s.SetFaults(test.enabled, test.settings); err == nil {
			t.Fatalf("%s: accepted", test.name)
		}
		if !sameState(before, s) {
			t.Fatalf("%s: the refused call changed the state", test.name)
		}
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(false, FaultSettings{}); err != nil || s.faultsOn {
		t.Fatalf("faults off: error %v, faults on %t", err, s.faultsOn)
	}
}
