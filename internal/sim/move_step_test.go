package sim

import (
	"bytes"
	"encoding/json/v2"
	"math"
	"slices"
	"testing"
)

// frozenMove is the ordinary move caller before the motion kernel. It has
// no fault branch.
func (s *Simulation) frozenMove(v *vehicle) {
	if s.moveCompact(v) {
		return
	}
	limit := v.blocks.end(v.reservedThrough)
	if v.link.leader != 0 && v.platoonCap < limit {
		limit = v.platoonCap
	}
	available := math.Max(0, limit-v.distance)
	dt := 1.0 / TicksPerSecond
	// Semi-implicit integration preserves enough owned track to stop on the next tick.
	safe := math.Sqrt(acceleration*acceleration*dt*dt+2*acceleration*available) - acceleration*dt
	blocks := &v.blocks
	current := blocks.find(v.blockIndex, &blocks.cursors[podCursor])
	// A pod with no lane has left its berth, so it enters its current lane
	// in this tick.
	entered := current.lane + 1
	if v.Pod.LaneID == "" {
		entered = current.lane
	}
	speed := math.Min(v.Pod.Speed+acceleration*dt, math.Min(blocks.route[current.lane].SpeedLimit, math.Max(0, safe)))
	v.Pod.Speed = blocks.speedBeforeLane(current.lane, v.distance, speed)
	travel := math.Min(available, v.Pod.Speed*dt)
	v.distance += travel
	if limit-v.distance < 1e-5 {
		v.distance = limit
		v.Pod.Speed = 0
	}
	for v.distance >= current.end {
		if v.blockIndex+1 == blocks.len() {
			s.recordLaneEntries(v, entered, current.lane)
			if v.destination.ID == "" {
				v.Pod.Speed = 0
				v.Pod.WaitReason = BerthOccupied
				return
			}
			s.arrive(v)
			return
		}
		if v.blockIndex == v.reservedThrough {
			break
		}
		v.blockIndex++
		current = blocks.find(v.blockIndex, current)
	}
	lane := current.lane
	s.recordLaneEntries(v, entered, lane)
	v.Pod.LaneID, v.Pod.LaneDistance = blocks.route[lane].ID, v.distance-blocks.lanes[lane].start
	v.Pod.Position = s.lanePosition(blocks.lanes[lane].geometry, &blocks.route[lane], v.Pod.LaneDistance)
}

// frozenMoveAndMeasure is moveAndMeasure with frozenMove.
func (s *Simulation) frozenMoveAndMeasure(v *vehicle) {
	sample := MotionSample{ID: v.Pod.ID, Class: v.Pod.Class, StartSpeed: v.Pod.Speed}
	before := v.distance
	beforeLocal := v.Pod.LaneDistance
	compact := s.compactGroup(v) != nil
	occupied := v.Pod.Occupied
	s.frozenMove(v)
	travel := v.distance - before
	if compact {
		travel = v.Pod.LaneDistance - beforeLocal
	}
	sample.DistanceMeters, sample.EndSpeed = travel, v.Pod.Speed
	s.recordMotion(sample)
	if occupied {
		s.passengerDistanceMeters += travel
	} else {
		s.emptyDistanceMeters += travel
	}
}

// TestMoveStepMatchesFrozenController moves a pod without a fault with
// moveAndMeasure and with the frozen controller. The motion frame, the
// distance accounting, the saved state and the pod stay equal at each tick.
// A pod with grants to the end of its first lane comes to rest at the grant
// end with the endpoint snap.
func TestMoveStepMatchesFrozenController(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		lengths, limits []float64
		// grantLanes is the number of granted lanes. Zero grants the route.
		grantLanes int
	}{
		{"one_lane", []float64{100}, []float64{7.123456789}, 0},
		{"lower_future", []float64{100, 10, 100}, []float64{14, 8, 2.5}, 0},
		{"crossed_short_lane", []float64{100, .001, 100}, []float64{14, 2.5, 14}, 0},
		{"grant_end", []float64{100, 100}, []float64{14, 14}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, v := laneSpeedFixture(test.lengths, test.limits)
			if test.grantLanes > 0 {
				v.reservedThrough = v.blocks.lanes[test.grantLanes].first - 1
			}
			actual.vehicles = []vehicle{*v}
			v = &actual.vehicles[0]
			actual.SetMotionRecording(true)
			control := actual.Clone()
			old := &control.vehicles[0]
			for tick := range 20000 {
				actual.tick++
				control.tick++
				actual.beginMotionFrame()
				control.beginMotionFrame()
				actual.moveAndMeasure(v)
				control.frozenMoveAndMeasure(old)
				actual.publishMotionFrame()
				control.publishMotionFrame()
				af, _ := actual.MotionFrame()
				bf, _ := control.MotionFrame()
				if af.Tick != bf.Tick || !slices.Equal(af.Samples, bf.Samples) || actual.emptyDistanceMeters != control.emptyDistanceMeters || actual.passengerDistanceMeters != control.passengerDistanceMeters {
					t.Fatalf("ordinary MotionFrame or accounting differs at tick %d", tick)
				}
				a, err := json.Marshal(actual.ExportState())
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(control.ExportState())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(a, b) || v.Pod != old.Pod || v.distance != old.distance || v.blocks.cursors != old.blocks.cursors {
					t.Fatalf("ordinary caller differs from the frozen controller at tick %d", tick)
				}
				lane := v.blocks.currentLane(v.blockIndex)
				if v.Pod.Speed > lane.SpeedLimit {
					t.Fatal("actual current lane limit failed")
				}
				if v.distance == v.blocks.end(v.reservedThrough) && v.Pod.Speed == 0 {
					if test.grantLanes > 0 && v.Pod.LaneID != "lane-0" {
						t.Fatalf("the pod rests on %s, want the granted lane", v.Pod.LaneID)
					}
					break
				}
				if tick == 19999 {
					t.Fatal("the pod did not stop in the tick cap")
				}
			}
		})
	}
}
