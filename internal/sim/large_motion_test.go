package sim

import (
	"fmt"
	"math"
	"testing"
)

func admitLargeMotionClasses(n Network) Network {
	classes := classBit(string(LegacyClass)) | classBit(string(CompactClass)) | classBit(string(GroupClass))
	for index := range n.Lanes {
		n.Lanes[index].VehicleClasses = classes
	}
	for index := range n.Stations {
		n.Stations[index].VehicleClasses = classes
		for berth := range n.Stations[index].Berths {
			n.Stations[index].Berths[berth].VehicleClasses = classes
		}
	}
	return n
}

func largeMotionNetwork(curved bool) Network {
	n := lineNetwork([]lineStation{{id: "a", berths: 2}, {id: "b", berths: 2}, {id: "c", berths: 2}, {id: "parking", berths: 2, parking: true}})
	for index := range n.Nodes {
		n.Nodes[index].Position.X *= 2
		n.Nodes[index].Position.Y *= 2
	}
	for index := range n.Lanes {
		lane := &n.Lanes[index]
		if lane.ID == "a-link" {
			lane.StationID, lane.StationRole = "b", StationEntryRole
			if curved {
				lane.Control = &Point{X: 450, Y: 60}
			}
		}
	}
	return admitLargeMotionClasses(n)
}

// checkLargeMotionTick measures visible center gaps and integration directly.
// Its numeric bounds do not call the production profile or stopping helpers.
func checkLargeMotionTick(t *testing.T, s *Simulation, before []Pod) float64 {
	t.Helper()
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	checkIncrementalOwners(t, s)
	minimum := math.Inf(1)
	for index := range s.vehicles {
		v := &s.vehicles[index]
		pod := v.Pod
		for _, other := range s.vehicles[index+1:] {
			gap := math.Hypot(pod.Position.X-other.Pod.Position.X, pod.Position.Y-other.Pod.Position.Y)
			bound := 12.0
			if pod.Class == GroupClass || other.Pod.Class == GroupClass {
				bound = 20
				minimum = min(minimum, gap)
			}
			if gap < bound-1e-6 {
				t.Fatalf("tick %d independent %g-meter gap: %+v %+v", s.tick, gap, pod, other.Pod)
			}
		}
		if len(before) != 0 {
			previous := before[index]
			if pod.Class != previous.Class {
				t.Fatal("live vehicle class changed")
			}
			if math.Abs(pod.Speed-previous.Speed) > 2.0/60+1e-6 {
				t.Fatalf("tick %d acceleration or braking exceeded 2 m/s2: %g -> %g", s.tick, previous.Speed, pod.Speed)
			}
			if movement := math.Hypot(pod.Position.X-previous.Position.X, pod.Position.Y-previous.Position.Y); movement > max(pod.Speed, previous.Speed)/60+1e-6 {
				t.Fatalf("tick %d position snapped by %g meters", s.tick, movement)
			}
		}
		if pod.Activity != Traveling {
			continue
		}
		lane := v.blocks.currentLane(v.blockIndex)
		if pod.Speed > lane.SpeedLimit+1e-6 || pod.Speed < 0 {
			t.Fatal("authored speed bound failed")
		}
		if pod.Class != GroupClass {
			continue
		}
		if v.coupled() {
			t.Fatal("operating Group entered a virtual or compact link")
		}
		stop := v.blocks.end(v.reservedThrough)
		if v.distance+pod.Speed*pod.Speed/4 > stop+1e-6 {
			t.Fatal("Group lost its continuously owned stopping bound")
		}
		for _, b := range v.blocks.span(v.blockIndex, v.reservedThrough+1) {
			if s.owners[resource{kind: trackResource, id: b.lane.ID, cell: b.cell}] != pod.ID {
				t.Fatal("Group stopping path contains unowned track")
			}
		}
	}
	return minimum
}

func largeMotionPods(s *Simulation) []Pod {
	pods := make([]Pod, len(s.vehicles))
	for index := range s.vehicles {
		pods[index] = s.vehicles[index].Pod
	}
	return pods
}

func largeMotionRequest(t *testing.T, s *Simulation, id, destination string) {
	t.Helper()
	v := s.findVehicle(id)
	party := 1
	switch v.Pod.Class {
	case GroupClass:
		party = 8
	case CompactClass:
		party = 4
	case "", LegacyClass:
	case ExpressClass, topologyClass:
		t.Fatal("fixture uses a profile outside Group qualification")
	default:
		t.Fatal("fixture uses an unknown vehicle class")
	}
	if err := s.RequestJourneyOptions(id, TripOptions{To: destination, PartySize: party, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
}

func TestLargeMotionFollowingAndMerge(t *testing.T) {
	t.Parallel()
	for _, classes := range [][2]VehicleClass{{GroupClass, LegacyClass}, {CompactClass, GroupClass}, {GroupClass, GroupClass}} {
		for _, curved := range []bool{false, true} {
			for _, buffers := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-%s/curve%t/buffer%t", classes[0], classes[1], curved, buffers), func(t *testing.T) {
					t.Parallel()
					fleet := []Placement{{ID: "lead", Class: classes[0], StationID: "a", BerthID: "a-1"}, {ID: "follow", Class: classes[1], StationID: "a", BerthID: "a-2"}}
					if buffers {
						fleet = append(fleet, Placement{ID: "block-one", Class: GroupClass, StationID: "b", BerthID: "b-1"}, Placement{ID: "block-two", Class: CompactClass, StationID: "b", BerthID: "b-2"})
					}
					s, err := NewFleet(largeMotionNetwork(curved), fleet)
					if err != nil {
						t.Fatal(err)
					}
					s.SetStationBuffers(buffers)
					if err := s.SetPlatooning(PlatooningVirtual); err != nil {
						t.Fatal(err)
					}
					largeMotionRequest(t, s, "lead", "b")
					gap := math.Inf(1)
					sawFollowing, sawHolding := false, false
					for steps := range 600 * 60 {
						if steps == 2*60 {
							largeMotionRequest(t, s, "follow", "b")
						}
						before := largeMotionPods(s)
						s.Step()
						gap = min(gap, checkLargeMotionTick(t, s, before))
						lead, follow := s.findVehicle("lead"), s.findVehicle("follow")
						if lead.Pod.LaneID == "a-link" && follow.Pod.LaneID == "a-link" && lead.Pod.LaneDistance > follow.Pod.LaneDistance {
							sawFollowing = true
						}
						for _, v := range []*vehicle{lead, follow} {
							if v.buffered && v.Pod.Activity == Traveling && v.Pod.Speed == 0 && v.destination.ID == "" {
								sawHolding = true
							}
						}
						if s.completed == 2 && s.tick > 2*60 {
							break
						}
					}
					if s.completed != 2 || s.boarded != 2 || s.unaccountedOrders != 0 || !sawFollowing || buffers && !sawHolding {
						t.Fatalf("journey evidence incomplete: completed%d boarded%d following%t holding%t", s.completed, s.boarded, sawFollowing, sawHolding)
					}
					t.Logf("ticks%d minimum large gap%.6f", s.tick, gap)
				})
			}
		}
	}
}

func TestLargeMotionBankAndParking(t *testing.T) {
	t.Parallel()
	n := admitLargeMotionClasses(BankExample())
	s, err := NewFleet(n, []Placement{{ID: "group", Class: GroupClass, StationID: "parking", BerthID: "parking-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitTripOptions(TripOptions{From: "origin", To: "hub", PartySize: 8, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
	sawParkingDeparture, sawBankArrival := false, false
	for steps := 0; steps < 600*60 && s.completed == 0; steps++ {
		before := largeMotionPods(s)
		s.Step()
		checkLargeMotionTick(t, s, before)
		v := &s.vehicles[0]
		if v.Pod.Activity == Traveling && v.RelocatingTo == "origin" && v.origin.ID == "parking-1" {
			sawParkingDeparture = true
		}
		if v.Pod.StationID == "hub" && v.Pod.BerthID != "" {
			sawBankArrival = true
		}
	}
	if s.completed != 1 || s.boarded != 1 || s.requestID != 1 || !sawParkingDeparture || !sawBankArrival {
		t.Fatal("Group parking fetch and bank arrival did not finish")
	}
}
