package sim

import (
	"slices"
	"testing"
)

// An ordinary pod must not join a platoon behind a committed coupled
// member. The pair's native checks refuse a foreign follower, so such a
// link stops the simulation. In the shared corridor fixture, the first
// cohort forms a train, and rear2 departs alone behind it at tick 6000.
// The train must retire without an ordinary follower, and every journey
// must complete.
func TestCouplingMemberAdmitsNoOrdinaryFollower(t *testing.T) {
	t.Parallel()
	sc := couplingMultiShared(t, 5700)
	sc.trips = slices.DeleteFunc(sc.trips, func(trip couplingMultiTrip) bool { return trip.id == "blocker2" || trip.id == "front2" })
	s := sc.start(t, true)
	requests := map[string]int{}
	formed, behind := int64(-1), false
	for s.tick < 49000 {
		sc.request(t, s, true, requests)
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("coupling fault at tick %d: %v", s.tick, err)
		}
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if v.couplingID == "" {
				continue
			}
			if formed < 0 {
				formed = s.tick
			}
			if v.follower != 0 && s.vehicles[v.follower-1].couplingID == "" {
				t.Fatalf("coupled member %s has ordinary follower %s at tick %d", v.Pod.ID, s.vehicles[v.follower-1].Pod.ID, s.tick)
			}
			rear2 := s.findVehicle("rear2")
			behind = behind || rear2.Pod.LaneID != "" && rear2.Pod.LaneID == v.Pod.LaneID
		}
		if s.completed == len(sc.trips) {
			break
		}
	}
	if formed < 0 || !behind {
		t.Fatalf("fixture did not put rear2 behind a coupled member: formed=%d behind=%t", formed, behind)
	}
	if s.completed != len(sc.trips) {
		t.Fatalf("completed %d of %d journeys by tick %d", s.completed, len(sc.trips), s.tick)
	}
}
