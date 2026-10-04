package sim

import (
	"fmt"
	"math"
	"strconv"
	"testing"
)

func TestMoveBrakesBeforeLowerSpeedLane(t *testing.T) {
	t.Parallel()
	shortLengths, shortLimits := []float64{50}, []float64{14}
	for range 1000 {
		shortLengths = append(shortLengths, 0.01)
		shortLimits = append(shortLimits, 14)
	}
	shortLengths = append(shortLengths, 10)
	shortLimits = append(shortLimits, 2.5)
	for _, test := range []struct {
		name    string
		lengths []float64
		limits  []float64
	}{
		{"next lane", []float64{100, 100}, []float64{14, 2.5}},
		{"thousand short lanes", shortLengths, shortLimits},
		{"four lanes ahead", []float64{50, 1, 1, 1, 100}, []float64{14, 14, 14, 14, 2.5}},
		{"successive limits", []float64{100, 10, 100}, []float64{14, 8, 2.5}},
		{"short lower lane", []float64{100, 0.001, 100}, []float64{14, 2.5, 14}},
		{"short terminal lane", []float64{100, 0.001}, []float64{14, 2.5}},
		{"small lower limit", []float64{100, 10}, []float64{14, 0.25}},
		{"higher limit", []float64{100, 100}, []float64{2.5, 14}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := laneSpeedFixture(test.lengths, test.limits)
			seen, braked := make(map[string]bool), false
			for tick := range 20000 {
				before, oldLane := v.Pod.Speed, v.Pod.LaneID
				oldIndex := v.blocks.routeLane(v.blockIndex)
				s.move(v)
				lane := v.blocks.currentLane(v.blockIndex)
				if v.Pod.Speed < 0 || v.Pod.Speed > lane.SpeedLimit {
					t.Fatalf("tick %d: lane %s speed %.17g exceeds limit %.17g", tick, lane.ID, v.Pod.Speed, lane.SpeedLimit)
				}
				if math.Abs(v.Pod.Speed-before) > acceleration/TicksPerSecond+1e-9 {
					t.Fatalf("tick %d: speed changed from %.17g to %.17g", tick, before, v.Pod.Speed)
				}
				for i := oldIndex + 1; i <= v.blocks.routeLane(v.blockIndex); i++ {
					if v.Pod.Speed > v.Route[i].SpeedLimit {
						t.Fatalf("tick %d: crossed lane %s at speed %.17g above limit %.17g", tick, v.Route[i].ID, v.Pod.Speed, v.Route[i].SpeedLimit)
					}
				}
				if oldLane == "lane-0" && v.Pod.Speed < before {
					braked = true
				}
				seen[lane.ID] = true
				if v.distance == v.blocks.end(v.reservedThrough) && v.Pod.Speed == 0 {
					break
				}
			}
			if !seen["lane-"+strconv.Itoa(len(test.limits)-1)] || v.distance != v.blocks.end(v.reservedThrough) {
				t.Fatal("pod did not reach the terminal lane")
			}
			if test.limits[0] > test.limits[1] && !braked {
				t.Fatal("pod did not brake before the lower-speed lane")
			}
		})
	}
}

func laneSpeedFixture(lengths, limits []float64) (*Simulation, *vehicle) {
	network := Network{Nodes: []Node{{ID: "node-0"}}}
	distance := 0.0
	for i, length := range lengths {
		distance += length
		network.Nodes = append(network.Nodes, Node{ID: "node-" + strconv.Itoa(i+1), Position: Point{X: distance}})
		network.Lanes = append(network.Lanes, Lane{ID: "lane-" + strconv.Itoa(i), From: "node-" + strconv.Itoa(i), To: "node-" + strconv.Itoa(i+1), SpeedLimit: limits[i]})
	}
	s := &Simulation{network: network}
	v := &vehicle{Pod: Pod{ID: "pod", Activity: Traveling, LaneID: "lane-0", Speed: limits[0]}}
	s.setVehicleRoute(v, network.Lanes)
	v.reservedThrough = v.blocks.len() - 1
	return s, v
}

func TestLaneSpeedPhysicalRestore(t *testing.T) {
	t.Parallel()
	for _, mode := range []Platooning{PlatooningOff, PlatooningVirtual} {
		t.Run(strconv.Itoa(int(mode)), func(t *testing.T) {
			t.Parallel()
			network := mergeCorridor(false, 0)
			for i := range network.Lanes {
				if network.Lanes[i].ID == "exit" {
					network.Lanes[i].SpeedLimit = 2.5
				}
			}
			s := restoreCorridor(t, network, []corridorPod{
				{route: []string{"main", "exit", "approach"}, station: "dest", distance: corridorFeedLength - 100},
				{route: []string{"main", "exit", "approach"}, station: "dest", distance: corridorFeedLength - 100 - corridorQueueGap},
			})
			if err := s.SetPlatooning(mode); err != nil {
				t.Fatal(err)
			}
			restored, entered, braked := false, false, false
			for range 3000 {
				before := largeMotionPods(s)
				s.Step()
				checkIncrementalOwners(t, s)
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				for i := range s.vehicles {
					v := &s.vehicles[i]
					if v.Pod.Speed > v.blocks.currentLane(v.blockIndex).SpeedLimit || math.Abs(v.Pod.Speed-before[i].Speed) > maxSpeedStep {
						t.Fatalf("tick %d: invalid speed change on %s: %.17g to %.17g", s.tick, v.Pod.LaneID, before[i].Speed, v.Pod.Speed)
					}
					if v.link.leader != 0 {
						t.Fatal("mixed-speed route formed a virtual platoon")
					}
				}
				head := &s.vehicles[0]
				entered = entered || head.Pod.LaneID == "exit"
				if head.Pod.LaneID == "main" && head.Pod.Speed < before[0].Speed {
					braked = true
					if !restored {
						var result RestoreResult
						var err error
						s, result, err = RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: roundTripState(t, s.ExportState())})
						if err != nil || !cleanRestore(result) {
							t.Fatalf("restore during braking: %+v, %v", result, err)
						}
						if err := s.SetPlatooning(mode); err != nil {
							t.Fatal(err)
						}
						restored = true
					}
				}
			}
			if !restored || !entered || !braked {
				t.Fatalf("restored=%v entered=%v braked=%v", restored, entered, braked)
			}
		})
	}
}

func TestLaneSpeedKeepsUnrestrictedTargets(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		limits []float64
		speed  float64
	}{
		{"equal limits", []float64{14, 14, 14}, 14},
		{"increasing limits", []float64{2.5, 8, 14}, 2.5},
		{"slower target", []float64{14, 2.5, 1}, 0.5},
		{"zero target", []float64{14, 2.5, 1}, 0},
		{"large finite limits", []float64{math.MaxFloat64, math.MaxFloat64, math.MaxFloat64}, math.MaxFloat64},
		{"small finite limits", []float64{math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64}, math.SmallestNonzeroFloat64},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, v := laneSpeedFixture([]float64{1, 1, 1}, test.limits)
			if got := v.blocks.speedBeforeLane(0, 0, test.speed); got != test.speed {
				t.Fatalf("speed %.17g, want unchanged target %.17g", got, test.speed)
			}
		})
	}
}

func TestLaneSpeedLargeMixedRestore(t *testing.T) {
	t.Parallel()
	for _, classes := range [][2]VehicleClass{{GroupClass, CompactClass}, {CompactClass, GroupClass}} {
		t.Run(fmt.Sprintf("%s-%s", classes[0], classes[1]), func(t *testing.T) {
			t.Parallel()
			network := largeMotionNetwork(false)
			for i := range network.Lanes {
				if network.Lanes[i].ID == "a-link" {
					network.Lanes[i].SpeedLimit = 2.5
				}
			}
			fleet := []Placement{{ID: "lead", Class: classes[0], StationID: "a", BerthID: "a-1"}, {ID: "follow", Class: classes[1], StationID: "a", BerthID: "a-2"}}
			s, err := NewFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			request := func(id string) {
				t.Helper()
				party := 4
				if s.findVehicle(id).Pod.Class == GroupClass {
					party = 8
				}
				if requestErr := s.RequestJourneyOptions(id, TripOptions{To: "b", PartySize: party, SharingConsent: PrivateConsent}); requestErr != nil {
					t.Fatal(requestErr)
				}
			}
			request("lead")
			restored, retained, released := false, false, false
			entered := make(map[string]bool)
			gap := math.Inf(1)
			for tick := range 12000 {
				if tick == 2*TicksPerSecond {
					request("follow")
				}
				before := largeMotionPods(s)
				s.Step()
				gap = math.Min(gap, checkLargeMotionTick(t, s, before))
				for i := range s.vehicles {
					v := &s.vehicles[i]
					if v.Pod.Activity != Traveling {
						continue
					}
					if v.Pod.Speed > v.blocks.currentLane(v.blockIndex).SpeedLimit {
						t.Fatalf("tick %d: pod %s exceeds lane speed", s.tick, v.Pod.ID)
					}
					if v.Pod.LaneID == "a-link" {
						entered[v.Pod.ID] = true
						if v.Pod.Class == GroupClass {
							owner := s.owners[resource{kind: nodeResource, id: "a-exit"}]
							if v.Pod.LaneDistance < 20 {
								if owner != podResourceOwner(v.Pod.ID) {
									t.Fatal("Group released the entry node before its 20-meter tail")
								}
								retained = true
							} else if owner != podResourceOwner(v.Pod.ID) {
								released = true
							}
						}
					}
					if !restored && v.Pod.Class == GroupClass && (v.Pod.LaneID == "a-1-out" || v.Pod.LaneID == "a-2-out") && v.Pod.Speed > 2.5 && v.Pod.Speed < before[i].Speed {
						var result RestoreResult
						s, result, err = RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: roundTripState(t, s.ExportState())})
						if err != nil || !cleanRestore(result) {
							t.Fatalf("Group restore during braking: %+v, %v", result, err)
						}
						if err := s.SetPlatooning(PlatooningVirtual); err != nil {
							t.Fatal(err)
						}
						restored = true
						break
					}
				}
				if restored && retained && released && len(entered) == 2 {
					break
				}
			}
			if !restored || !retained || !released || len(entered) != 2 {
				t.Fatalf("restored=%v retained=%v released=%v entered=%v", restored, retained, released, entered)
			}
			t.Logf("ticks %d minimum large gap %.6f", s.tick, gap)
		})
	}
}
