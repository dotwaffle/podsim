package sim

import (
	"slices"
	"testing"
)

// emergencyPlatoon restores two pods with a rider each at rest on a
// straight path of four lanes of 300 m to the destination station, with
// virtual platoons on. The first step links the follower, pod p02, to the
// leader, pod p01, with a run of two lanes, and the run grows to three
// lanes after about 55 seconds.
func emergencyPlatoon(t *testing.T) (s *Simulation, leader, follower *vehicle) {
	t.Helper()
	lanes := []geometryLane{{to: Point{X: 300}}, {to: Point{X: 600}}, {to: Point{X: 900}}, {to: Point{X: 1200}}}
	s = restoreGeometry(t, geometryNetwork(Point{}, lanes, 2), len(lanes), 2, 200, 60)
	s.emergenciesOn = true
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	return s, &s.vehicles[0], &s.vehicles[1]
}

// TestEmergencyPlatoonDrain checks the platoon rule of section 5.6 of the
// incident emergency contract. The control run without an emergency grows
// the run of the link at tick grow. An emergency at either end of the link
// at the tick before makes the link drain on each tick, and the run does
// not grow. The link ends, no new link forms, and the pod then binds on
// its cadence. The leader then unloads.
func TestEmergencyPlatoonDrain(t *testing.T) {
	t.Parallel()
	var grow int64
	for _, end := range []string{"control", "leader", "follower"} {
		s, leader, follower := emergencyPlatoon(t)
		s.Step()
		if follower.link.leader == 0 || follower.link.draining {
			t.Fatalf("%s: the first step did not link the pods: %+v", end, follower.link)
		}
		lanes := follower.link.lanes
		if end == "control" {
			stepUntil(t, s, "a longer run", func() bool { return follower.link.lanes > lanes })
			grow = s.tick
			continue
		}
		for s.tick < grow-1 {
			s.Step()
		}
		if follower.link.leader == 0 || follower.link.lanes != lanes || follower.link.draining {
			t.Fatalf("%s: tick %d: the link changed before the emergency: %+v", end, s.tick, follower.link)
		}
		checkEmergenciesEachTick(t, s)
		v := leader
		if end == "follower" {
			v = follower
		}
		startEmergency(t, s, v, 0)
		start := s.tick
		if v.op.purpose != opService || v.withdrawn != emergencyHold {
			t.Fatalf("%s: the platoon member has the purpose %+v and the holds %#x", end, v.op, v.withdrawn)
		}
		for follower.link.leader != 0 {
			s.Step()
			if follower.link.leader != 0 && (!follower.link.draining || follower.link.lanes != lanes) {
				t.Fatalf("%s: tick %d: the link does not drain: %+v", end, s.tick, follower.link)
			}
			if s.tick > start+600 {
				t.Fatalf("%s: the link did not end", end)
			}
		}
		stepUntil(t, s, "the bind", func() bool {
			if follower.link.leader != 0 || leader.follower != 0 {
				t.Fatalf("%s: tick %d: the pods link again", end, s.tick)
			}
			return v.op.purpose == opEmergencyUnload
		})
		if (s.tick-start)%60 != 0 || v.destination.ID != "dest-1" {
			t.Fatalf("%s: the pod binds to %q at tick %d, %d ticks after the start", end, v.destination.ID, s.tick, s.tick-start)
		}
		// The leader stays at the only berth after it unloads, so only an
		// emergency on the leader ends.
		if end == "follower" {
			continue
		}
		stepUntil(t, s, "the end of the emergency", func() bool {
			if follower.link.leader != 0 || leader.follower != 0 {
				t.Fatalf("%s: tick %d: the pods link again", end, s.tick)
			}
			return len(s.emergencies) == 0
		})
	}
}

// TestEmergencyPlatoonRefusal checks that tryLink refuses a pair with an
// emergency pod at either end (section 5.6 of the incident emergency
// contract). The emergency starts before the first step, so the pods never
// link. The control run links them on the first step.
func TestEmergencyPlatoonRefusal(t *testing.T) {
	t.Parallel()
	for _, end := range []string{"leader", "follower"} {
		s, leader, follower := emergencyPlatoon(t)
		checkEmergenciesEachTick(t, s)
		v := leader
		if end == "follower" {
			v = follower
		}
		startEmergency(t, s, v, 0)
		for range 30 * TicksPerSecond {
			s.Step()
			if follower.link.leader != 0 || leader.follower != 0 {
				t.Fatalf("%s: tick %d: the pods link", end, s.tick)
			}
		}
	}
}

// TestEmergencyCompactMember checks the compact rule of section 5.6 of the
// incident emergency contract. A pod in a compact queue gets the hold at
// the start and stays deferred while it is in the group. After it leaves
// the group, it binds on its cadence and unloads.
func TestEmergencyCompactMember(t *testing.T) {
	t.Parallel()
	s := departingBufferQueue(t)
	s.emergenciesOn = true
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	for s.CoupledPods() != 4 {
		compactTick(t, s)
		if s.tick > 1000*TicksPerSecond {
			t.Fatal("the compact queue did not form")
		}
	}
	v := s.findVehicle("03")
	if s.compactGroup(v) == nil || !v.carriesPassengers() {
		t.Fatalf("pod 03 is not an occupied compact member: %+v", v.Pod)
	}
	checkEmergenciesEachTick(t, s)
	party := v.Riders[0].ID
	startEmergency(t, s, v, 0)
	start := s.tick
	for s.compactGroup(v) != nil {
		if v.op.purpose != opService || v.withdrawn != emergencyHold {
			t.Fatalf("tick %d: the compact member has the purpose %+v and the holds %#x", s.tick, v.op, v.withdrawn)
		}
		compactTick(t, s)
		if s.tick > start+6000 {
			t.Fatal("the pod did not leave the group")
		}
	}
	for v.op.purpose == opService {
		compactTick(t, s)
		if s.tick > start+12000 {
			t.Fatal("the pod did not bind")
		}
	}
	if (s.tick-start)%60 != 0 || v.destination.ID != "market-1" {
		t.Fatalf("the pod binds to %q at tick %d, %d ticks after the start", v.destination.ID, s.tick, s.tick-start)
	}
	for len(s.emergencies) != 0 {
		compactTick(t, s)
		if s.tick > start+20000 {
			t.Fatal("the emergency did not end")
		}
	}
	if !slices.Contains(s.undelivered, party) {
		t.Fatalf("interrupted %v, want the party %d", s.undelivered, party)
	}
}
