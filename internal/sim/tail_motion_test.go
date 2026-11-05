package sim

import (
	"fmt"
	"math"
	"slices"
	"testing"
)

// hairpinMotion restores a Group pod at rest on a hairpin of 204 m, and a Group
// pod behind it on the lane before the hairpin. The lane after the hairpin has
// the given separation group.
func hairpinMotion(t *testing.T, leader float64, group string) *Simulation {
	t.Helper()
	lanes := []geometryLane{{to: Point{}}, {to: Point{X: 30}, control: &Point{X: 15, Y: 200}}, {to: Point{X: 30, Y: -500}}, {to: Point{X: 30, Y: -600}}}
	network := admitLargeMotionClasses(stationNetwork(Point{Y: -1000}, lanes, 2, Point{X: 60, Y: -60}, Point{Y: -120}))
	// The origin berths are on two sides of the through lane, at the larger
	// clearance.
	for index := range network.Nodes {
		if network.Nodes[index].ID == "origin-berth-02" {
			network.Nodes[index].Position = Point{X: -4960, Y: 4940}
		}
	}
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
		if lane.ID == "l2" {
			network.Lanes[index].SeparationGroup = group
		}
	}
	route := []int{laneIndex["l0"], laneIndex["l1"], laneIndex["l2"], laneIndex["l3"]}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index, place := range []struct {
		lane, id string
		route    int
		on, past float64
	}{{"l1", "p01", 1, leader, 1000 + leader}, {"l0", "p02", 0, 950, 950}} {
		fleet = append(fleet, Placement{ID: place.id, StationID: "origin", BerthID: fmt.Sprintf("origin-%02d", index+1), Class: GroupClass})
		state.RequestID++
		state.Boarded++
		state.Pods = append(state.Pods, SavedPod{
			ID: place.id, Class: GroupClass, Activity: "traveling", Occupied: true, Origin: fleet[index].BerthID, DestinationStation: "dest",
			Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: place.id}},
			Stops:  []string{"dest"},
			Route:  route, RouteIndex: place.route, LaneID: place.lane, LaneDistance: place.on, Distance: place.past,
		})
	}
	s, result, err := RestoreState(RestoreStateInput{FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}, Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the pods did not restore in place: %+v", result)
	}
	return s
}

// TestTailHairpinStoppedFollower stops a pod on a hairpin with a chord of 30 m,
// and its follower must stop at the last legal position, at each tick at least
// 20 m from the stopped pod. At 125 m the stopped pod is past the apex, and it
// holds the start node of the hairpin, so the follower stays on the lane before
// it. With a shorter tail, the follower goes on to the other leg, under 20 m
// from the stopped pod. At 190 m the last legal cell is the third cell of the
// hairpin, whose end is at most the stopped pod less the tail.
func TestTailHairpinStoppedFollower(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		stopped float64
		lane    string
		// cells is the number of cells of the hairpin before the stop.
		cells int
	}{{stopped: 125, lane: "l0"}, {stopped: 190, lane: "l1", cells: 3}} {
		t.Run(fmt.Sprint(test.stopped), func(t *testing.T) {
			t.Parallel()
			s := hairpinMotion(t, test.stopped, "")
			if _, err := s.Fault(FaultRequest{PodID: "p01"}); err != nil {
				t.Fatal(err)
			}
			front, rear := s.findVehicle("p01"), s.findVehicle("p02")
			hairpin, approach := s.laneCells["l1"], s.laneCells["l0"]
			if hairpin.tail < 90 || hairpin.tail > 95 {
				t.Fatalf("hairpin tail %v, want about 94", hairpin.tail)
			}
			cell := s.laneLength(s.network.Lanes[s.graph.lanes["l1"]]) / float64(hairpin.count())
			smallest := math.Inf(1)
			for range 120 * TicksPerSecond {
				s.Step()
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatalf("tick %d: %v", s.tick, err)
				}
				gap := math.Hypot(front.Pod.Position.X-rear.Pod.Position.X, front.Pod.Position.Y-rear.Pod.Position.Y)
				smallest = min(smallest, gap)
				if gap < largeClearance {
					t.Fatalf("tick %d: the centers are %v m apart", s.tick, gap)
				}
			}
			if front.Pod.LaneID != "l1" || front.Pod.Speed != 0 || math.Abs(front.Pod.LaneDistance-test.stopped) > 1e-9 {
				t.Fatalf("the front pod moved: %+v", front.Pod)
			}
			// The last cell of the approach holds the start node of the hairpin.
			want := 1000 * float64(approach.count()-1) / float64(approach.count())
			if test.lane == "l1" {
				want = cell * float64(test.cells)
			}
			if rear.Pod.LaneID != test.lane || rear.Pod.Speed != 0 || math.Abs(rear.Pod.LaneDistance-want) > 2 {
				t.Fatalf("the follower is at %s %v at %v m/s, want %s %v", rear.Pod.LaneID, rear.Pod.LaneDistance, rear.Pod.Speed, test.lane, want)
			}
			t.Logf("smallest center gap %v m", smallest)
		})
	}
}

// TestTailFrontWindowAtHairpin checks that the front window of the observer
// keeps the projection bound. A pod on the hairpin has a front of about 79 m,
// and the new tail would give about 28 m, so the plane of the next lane must
// stay in the window at 50 m.
func TestTailFrontWindowAtHairpin(t *testing.T) {
	t.Parallel()
	s := hairpinMotion(t, 154, "upper")
	v := s.findVehicle("p01")
	if !v.locationsHave(s, "l2") {
		t.Fatalf("the plane of the next lane left the window: %+v", s.vehicleSafetyLocations(v, v.distance))
	}
	if want := largeEnvelopeRadius * oldLargePathBound(s.network.Polyline(s.network.Lanes[s.graph.lanes["l1"]])) / largeClearance; s.laneCells["l1"].front != want {
		t.Fatalf("front %v, want %v", s.laneCells["l1"].front, want)
	}
	far := hairpinMotion(t, 100, "upper")
	if v := far.findVehicle("p01"); v.locationsHave(far, "l2") {
		t.Fatal("the plane of the next lane is in the window past the front")
	}
}

func (v *vehicle) locationsHave(s *Simulation, lane string) bool {
	want := s.laneSafety[lane]
	return slices.Contains(s.vehicleSafetyLocations(v, v.distance), want)
}
