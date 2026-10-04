package sim

import (
	"fmt"
	"math"
	"testing"
)

func TestExpressLaneSpeedMixedRestore(t *testing.T) {
	t.Parallel()
	for _, classes := range [][2]VehicleClass{{ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}} {
		t.Run(fmt.Sprintf("%s-%s", classes[0], classes[1]), func(t *testing.T) {
			t.Parallel()
			network := largeMotionNetwork(false)
			for i := range network.Lanes {
				if network.Lanes[i].ID == "a-link" {
					network.Lanes[i].SpeedLimit = 2.5
				}
			}
			fleet := []Placement{{ID: "lead", Class: classes[0], StationID: "a", BerthID: "a-1"}, {ID: "follow", Class: classes[1], StationID: "a", BerthID: "a-2"}}
			s, err := expressPhysicalFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			request := func(id string) {
				t.Helper()
				party := 4
				if s.findVehicle(id).Pod.Class == ExpressClass {
					party = 20
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
				gap = math.Min(gap, checkExpressMotionTick(t, s, before))
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
						if v.Pod.Class == ExpressClass {
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
					if !restored && v.Pod.Class == ExpressClass && (v.Pod.LaneID == "a-1-out" || v.Pod.LaneID == "a-2-out") && v.Pod.Speed > 2.5 && v.Pod.Speed < before[i].Speed {
						var result RestoreResult
						expressEvidence(t, "braking", s.ExportState())
						s, result, err = RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: roundTripState(t, s.ExportState())})
						if err != nil || !cleanRestore(result) {
							t.Fatalf("Group restore during braking: %+v, %v", result, err)
						}
						if err := s.SetPlatooning(PlatooningVirtual); err != nil {
							t.Fatal(err)
						}
						expressEvidence(t, "restored-braking", s.ExportState())
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
