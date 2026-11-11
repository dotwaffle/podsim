package sim

import (
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The native fixtures add Express to explicit Group allowlists without changing geometry.
func expressPhysicalFleet(n Network, fleet []Placement) (*Simulation, error) {
	for i := range n.Lanes {
		if n.Lanes[i].VehicleClasses&classBit(string(GroupClass)) != 0 {
			n.Lanes[i].VehicleClasses |= classBit(string(ExpressClass))
		}
	}
	for i := range n.Stations {
		if n.Stations[i].VehicleClasses&classBit(string(GroupClass)) != 0 {
			n.Stations[i].VehicleClasses |= classBit(string(ExpressClass))
		}
		for j := range n.Stations[i].Berths {
			if n.Stations[i].Berths[j].VehicleClasses&classBit(string(GroupClass)) != 0 {
				n.Stations[i].Berths[j].VehicleClasses |= classBit(string(ExpressClass))
			}
		}
	}
	return NewFleetWithOrderContract(n, fleet, ExpressOrderContract)
}

func expressEvidence(t *testing.T, name string, value any) {
	t.Helper()
	if dir := os.Getenv("EXPRESS_EVIDENCE_DIR"); dir != "" {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		file := strings.ReplaceAll(t.Name()+"-"+name, "/", "_") + ".json"
		if err := os.WriteFile(filepath.Join(dir, file), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// checkExpressMotionTick measures integration and resource IDs independently.
// The fixed numeric bounds do not use production profile, braking, or release helpers.
func checkExpressMotionTick(t *testing.T, s *Simulation, before []Pod) float64 {
	t.Helper()
	recordExpressTick(t, s)
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	checkSafetyLocationParity(t, s)
	checkSavedRouteStart(t, s, &savedRouteStartCounts{})
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	checkIncrementalOwners(t, s)
	minimum := math.Inf(1)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		pod := v.Pod
		large := pod.Class == ExpressClass || pod.Class == GroupClass
		for _, other := range s.vehicles[i+1:] {
			if !large && other.Pod.Class != ExpressClass && other.Pod.Class != GroupClass {
				continue
			}
			gap := math.Hypot(pod.Position.X-other.Pod.Position.X, pod.Position.Y-other.Pod.Position.Y)
			minimum = min(minimum, gap)
			if gap < 20-1e-6 {
				t.Fatalf("tick %d body gap %g below 20: %+v %+v owners=%v", s.tick, gap, pod, other.Pod, s.owners)
			}
		}
		if len(before) > 0 {
			previous := before[i]
			if pod.Class != previous.Class {
				t.Fatal("class changed")
			}
			rise, drop := 2.0, 2.0
			if pod.Class == ExpressClass {
				rise, drop = expressAcceleration, expressMaximumBraking
			}
			if pod.Speed-previous.Speed > rise/TicksPerSecond+1e-6 || previous.Speed-pod.Speed > drop/TicksPerSecond+1e-6 {
				t.Fatalf("tick %d pod %s acceleration or braking: %g -> %g", s.tick, pod.ID, previous.Speed, pod.Speed)
			}
			if moved := math.Hypot(pod.Position.X-previous.Position.X, pod.Position.Y-previous.Position.Y); moved > max(previous.Speed, pod.Speed)/TicksPerSecond+1e-6 {
				t.Fatalf("tick %d position snap %g", s.tick, moved)
			}
		}
		if large && (v.link.leader != 0 || v.follower != 0) {
			t.Fatal("large pod acquired a link")
		}
		if pod.Activity != Traveling {
			continue
		}
		lane := v.blocks.currentLane(v.blockIndex)
		if pod.Speed < 0 || pod.Speed > lane.SpeedLimit+1e-6 {
			t.Fatalf("tick %d lane %s speed %g exceeds %g", s.tick, lane.ID, pod.Speed, lane.SpeedLimit)
		}
		stopRate := 2.0
		if pod.Class == ExpressClass {
			stopRate = expressMaximumBraking
		}
		if v.distance+pod.Speed*pod.Speed/(2*stopRate) > v.blocks.end(v.reservedThrough)+1e-6 {
			t.Fatalf("tick %d pod %s stopping frontier unowned", s.tick, pod.ID)
		}
		for _, b := range v.blocks.span(v.blockIndex, v.reservedThrough+1) {
			held := resource{kind: trackResource, id: b.lane.ID, cell: b.cell}
			if s.owners[held] != podResourceOwner(pod.ID) && !expressCertifiedSmallOwner(s, v, s.owners[held].podID()) {
				t.Fatalf("tick %d pod %s track %+v owner %s", s.tick, pod.ID, held, s.owners[held])
			}
		}
		if !large {
			continue
		}
		if v.distance < 20-1e-6 && (v.originReleased || s.owners[resource{kind: berthResource, id: v.origin.ID}] != podResourceOwner(pod.ID)) {
			t.Fatal("origin released before 20 meters")
		}
		for _, b := range v.blocks.span(0, v.blockIndex) {
			if b.end+20 > v.distance+1e-6 {
				held := resource{kind: trackResource, id: b.lane.ID, cell: b.cell}
				if s.owners[held] != podResourceOwner(pod.ID) {
					t.Fatalf("tick %d pod %s lost 20-meter tail %+v owner %s", s.tick, pod.ID, held, s.owners[held])
				}
			}
		}
	}
	return minimum
}

func expressAdversarialTick(t *testing.T, s *Simulation, previous *motionPodBuffer) {
	t.Helper()
	before := previous.capture(s)
	s.Step()
	checkExpressMotionTick(t, s, before)
}

// expressFixtureBoarding returns the boarding seconds of the party that
// expressMotionRequest gives a class.
func expressFixtureBoarding(class VehicleClass) int {
	if class == ExpressClass {
		return 22
	}
	return 3
}

func expressMotionRequest(t *testing.T, s *Simulation, id, destination string) {
	t.Helper()
	party := 1
	switch s.findVehicle(id).Pod.Class {
	case ExpressClass:
		party = 20
	case GroupClass:
		party = 8
	case CompactClass:
		party = 4
	case "", LegacyClass:
	case topologyClass:
		t.Fatal("invalid fixture class")
	default:
		t.Fatal("unknown fixture class")
	}
	if err := s.RequestJourneyOptions(id, TripOptions{To: destination, PartySize: party, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
}

func TestExpressMotionFollowingAndMerge(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, classes := range [][2]VehicleClass{{ExpressClass, LegacyClass}, {LegacyClass, ExpressClass}, {ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}, {ExpressClass, ExpressClass}} {
		for _, curved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-%s/curve%t", classes[0], classes[1], curved), func(t *testing.T) {
				t.Parallel()
				fleet := []Placement{{ID: "lead", Class: classes[0], StationID: "a", BerthID: "a-1"}, {ID: "follow", Class: classes[1], StationID: "a", BerthID: "a-2"}}
				s, err := expressPhysicalFleet(largeMotionNetwork(curved), fleet)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.SetPlatooning(PlatooningVirtual); err != nil {
					t.Fatal(err)
				}
				// The follower leaves its berth 2 s after the leader, whatever
				// the boarding time of each party.
				leadAt := max(0, expressFixtureBoarding(classes[1])-expressFixtureBoarding(classes[0])-2) * TicksPerSecond
				followAt := max(0, expressFixtureBoarding(classes[0])+2-expressFixtureBoarding(classes[1])) * TicksPerSecond
				gap := math.Inf(1)
				sawFollowing := false
				var previous motionPodBuffer
				for steps := range 600 * TicksPerSecond {
					if steps == leadAt {
						expressMotionRequest(t, s, "lead", "b")
					}
					if steps == followAt {
						expressMotionRequest(t, s, "follow", "b")
					}
					before := previous.capture(s)
					s.Step()
					gap = min(gap, checkExpressMotionTick(t, s, before))
					lead, follow := s.findVehicle("lead"), s.findVehicle("follow")
					if lead.Pod.LaneID == "a-link" && follow.Pod.LaneID == "a-link" && lead.Pod.LaneDistance > follow.Pod.LaneDistance {
						sawFollowing = true
					}
					if s.completed == 2 {
						break
					}
				}
				if s.completed != 2 || s.boarded != 2 || s.unaccountedOrders != 0 || !sawFollowing {
					t.Fatalf("journey evidence incomplete: completed%d boarded%d following%t", s.completed, s.boarded, sawFollowing)
				}
				expressEvidence(t, "final-state", s.ExportState())
				t.Logf("ticks%d minimum large gap%.6f", s.tick, gap)
			})
		}
	}
}

func TestExpressMotionBankAndParking(t *testing.T) {
	t.Parallel()
	n := admitLargeMotionClasses(BankExample())
	s, err := expressPhysicalFleet(n, []Placement{{ID: "group", Class: ExpressClass, StationID: "parking", BerthID: "parking-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitTripOptions(TripOptions{From: "origin", To: "hub", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
		t.Fatal(err)
	}
	sawParkingDeparture, sawBankArrival := false, false
	var previous motionPodBuffer
	for steps := 0; steps < 600*TicksPerSecond && s.completed == 0; steps++ {
		before := previous.capture(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
		v := &s.vehicles[0]
		if v.Pod.Activity == Traveling && v.RelocatingTo == "origin" && v.origin.ID == "parking-1" {
			sawParkingDeparture = true
		}
		if v.Pod.StationID == "hub" && v.Pod.BerthID != "" {
			sawBankArrival = true
		}
	}
	if s.completed != 1 || s.boarded != 1 || s.requestID != 1 || !sawParkingDeparture || !sawBankArrival {
		t.Fatal("Express parking fetch and bank arrival did not finish")
	}
}

var expressAdversary = largeAdversary{
	large:    ExpressClass,
	newFleet: expressPhysicalFleet,
	newTick: func() func(*testing.T, *Simulation) {
		var previous motionPodBuffer
		return func(t *testing.T, s *Simulation) {
			t.Helper()
			expressAdversarialTick(t, s, &previous)
		}
	},
	evidence: true,
}

func TestExpressAdversarialCurvedFollowing(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, classes := range [][2]VehicleClass{{ExpressClass, LegacyClass}, {LegacyClass, ExpressClass}, {ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}} {
		t.Run(string(classes[0])+"-"+string(classes[1]), func(t *testing.T) {
			t.Parallel()
			expressAdversary.curvedFollowing(t, classes)
		})
	}
}

func TestExpressAdversarialStationaryEndpoint(t *testing.T) {
	t.Parallel()
	for _, curved := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-24m", true: "curved-60m"}[curved], func(t *testing.T) {
			t.Parallel()
			expressAdversary.stationaryEndpoint(t, curved)
		})
	}
}

func TestExpressAdversarialLivePlaneTransitions(t *testing.T) {
	t.Parallel()
	expressAdversary.livePlaneTransitions(t)
}

func TestExpressAdversarialCurvedStoppedFollowing(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, classes := range [][2]VehicleClass{{ExpressClass, LegacyClass}, {LegacyClass, ExpressClass}, {ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}} {
		t.Run(string(classes[0])+"-"+string(classes[1]), func(t *testing.T) {
			t.Parallel()
			expressAdversary.curvedStoppedFollowing(t, classes)
		})
	}
}

func expressCertifiedSmallOwner(s *Simulation, v *vehicle, owner string) bool {
	if v.Pod.Class == ExpressClass || v.Pod.Class == GroupClass || owner == "" {
		return false
	}
	leader := v.link.leader
	for leader != 0 {
		p := &s.vehicles[leader-1]
		if p.Pod.Class == ExpressClass || p.Pod.Class == GroupClass {
			return false
		}
		if p.Pod.ID == owner {
			return true
		}
		leader = p.link.leader
	}
	return false
}
