package sim

import "testing"

// pickupFor returns the pod that pickupPodForRequest chooses for a new trip
// from s1 to s3.
func pickupFor(s *Simulation) *vehicle {
	return s.pickupPodForRequest(newTrip(s, "s1", "s3").request, "", &dispatchPass{assigned: map[string]bool{}})
}

// TestPickupSkipsClosedOrigin checks the origin test of pickupPodForRequest
// on altLineFleet. A blocked s2-link cuts each route into s3, so no berth
// of s1 passes the pickup filter of a trip from s1 to s3: the origin is
// closed. A try of a pod idle at another station stores its failure in
// pickupMisses, so an empty memo shows that dispatch made no try.
func TestPickupSkipsClosedOrigin(t *testing.T) {
	t.Parallel()
	t.Run("fault clear", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1")
		length := s.graph.lengths[laneIndex(t, s, "s2-link")]
		id := startDebris(t, s, "s2-link", length/2, length/2+1, 0)
		s.waiting = []waitingTrip{newTrip(s, "s1", "s3")}
		s.dispatch()
		if s.waiting[0].request.PodID != "" || len(s.pickupMisses) != 0 {
			t.Fatalf("with the origin closed, the trip has pod %q, memo %v", s.waiting[0].request.PodID, s.pickupMisses)
		}
		if err := s.ClearFault(id); err != nil {
			t.Fatal(err)
		}
		s.dispatch()
		if s.waiting[0].request.PodID != "01" {
			t.Errorf("after the clear, the trip has pod %q", s.waiting[0].request.PodID)
		}
	})
	t.Run("idle at origin", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1", "s1-1")
		blockLanes(t, s, "s2-link")
		if v := pickupFor(s); v == nil || v.Pod.ID != "02" {
			t.Errorf("with the origin closed, the pod idle at the origin is not chosen: %v", v)
		}
	})
	t.Run("open origin", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1")
		blockLanes(t, s, "return")
		if v := pickupFor(s); v == nil || v.Pod.ID != "01" {
			t.Errorf("with the origin open and a lane blocked, pod 01 is not chosen: %v", v)
		}
	})
	// The second pod of a class gets the verdict that the first one stored.
	t.Run("closed for two pods", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "s0-1", "p-1")
		blockLanes(t, s, "s2-link")
		if v := pickupFor(s); v != nil || len(s.pickupMisses) != 0 {
			t.Errorf("with the origin closed, pod %v, memo %v", v, s.pickupMisses)
		}
	})
	t.Run("open for two pods", func(t *testing.T) {
		t.Parallel()
		s := altLineFleet(t, -100, "p-1", "s0-1")
		blockLanes(t, s, "return")
		if v := pickupFor(s); v == nil || v.Pod.ID != "02" {
			t.Errorf("with the origin open, pod 02 is not chosen: %v", v)
		}
	})
	t.Run("class", func(t *testing.T) {
		t.Parallel()
		compact, err := NewClassSet("compact")
		if err != nil {
			t.Fatal(err)
		}
		// Only a Compact pod can take alt-out, the second road out of s1.
		s := altLineFleetClasses(t, -100, compact, "s0-1", "p-1")
		s.findVehicle("02").Pod.Class = CompactClass
		blockLanes(t, s, "s1-link")
		if v := pickupFor(s); v == nil || v.Pod.ID != "02" {
			t.Errorf("the origin closed to Legacy hides the Compact pod: %v", v)
		}
	})
}
