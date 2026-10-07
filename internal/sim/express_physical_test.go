package sim

import (
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
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
			if math.Abs(pod.Speed-previous.Speed) > 2.0/60+1e-6 {
				t.Fatalf("tick %d acceleration: %g -> %g", s.tick, previous.Speed, pod.Speed)
			}
			if moved := math.Hypot(pod.Position.X-previous.Position.X, pod.Position.Y-previous.Position.Y); moved > max(previous.Speed, pod.Speed)/60+1e-6 {
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
		if v.distance+pod.Speed*pod.Speed/4 > v.blocks.end(v.reservedThrough)+1e-6 {
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

func expressAdversarialTick(t *testing.T, s *Simulation) {
	t.Helper()
	before := largeMotionPods(s)
	s.Step()
	checkExpressMotionTick(t, s, before)
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
				expressMotionRequest(t, s, "lead", "b")
				gap := math.Inf(1)
				sawFollowing := false
				for steps := range 600 * 60 {
					if steps == 2*60 {
						expressMotionRequest(t, s, "follow", "b")
					}
					before := largeMotionPods(s)
					s.Step()
					gap = min(gap, checkExpressMotionTick(t, s, before))
					lead, follow := s.findVehicle("lead"), s.findVehicle("follow")
					if lead.Pod.LaneID == "a-link" && follow.Pod.LaneID == "a-link" && lead.Pod.LaneDistance > follow.Pod.LaneDistance {
						sawFollowing = true
					}
					if s.completed == 2 && s.tick > 2*60 {
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
	for steps := 0; steps < 600*60 && s.completed == 0; steps++ {
		before := largeMotionPods(s)
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

func TestExpressAdversarialCurvedFollowing(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, classes := range [][2]VehicleClass{{ExpressClass, LegacyClass}, {LegacyClass, ExpressClass}, {ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}} {
		t.Run(string(classes[0])+"-"+string(classes[1]), func(t *testing.T) {
			t.Parallel()
			n := largeAdversarialNetwork(t, true)
			for i := range n.Lanes {
				if n.Lanes[i].ID == "return" {
					n.Lanes[i].Control = &Point{X: 800, Y: 2400}
				}
			}
			s, err := expressPhysicalFleet(n, []Placement{{ID: "one", Class: classes[0], StationID: "parking", BerthID: "parking-1"}, {ID: "two", Class: classes[1], StationID: "parking", BerthID: "parking-2"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"one", "two"} {
				if err := s.RequestJourney(id, "market"); err != nil {
					t.Fatal(err)
				}
			}
			overlap, moved := false, false
			minimum := math.Inf(1)
			for range 1000 * TicksPerSecond {
				expressAdversarialTick(t, s)
				a, b := &s.vehicles[0], &s.vehicles[1]
				if a.Pod.LaneID == "return" && b.Pod.LaneID == "return" {
					overlap = true
					moved = moved || a.Pod.Speed > 0 && b.Pod.Speed > 0
					minimum = min(minimum, pointDistance(a.Pod.Position, b.Pod.Position))
				}
				if s.completed == 2 {
					break
				}
			}
			if !overlap || !moved || s.completed != 2 {
				t.Fatalf("real curved following missing overlap=%v moved=%v completed=%d state=%+v", overlap, moved, s.completed, s.Snapshot())
			}
			expressEvidence(t, "final-state", s.ExportState())
			t.Logf("same curve moving pair minimum=%.9g tick=%d", minimum, s.tick)
		})
	}
}

func TestExpressAdversarialStationaryEndpoint(t *testing.T) {
	t.Parallel()
	for _, curved := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-24m", true: "curved-60m"}[curved], func(t *testing.T) {
			t.Parallel()
			n := largeAdversarialNetwork(t, false)
			entry, _ := n.Node("market-entry")
			for i := range n.Nodes {
				if n.Nodes[i].ID == "market-berth" {
					n.Nodes[i].Position = Point{X: entry.Position.X, Y: entry.Position.Y + 24}
					if curved {
						n.Nodes[i].Position.Y = entry.Position.Y + 58
					}
				}
			}
			var incoming Lane
			for i := range n.Lanes {
				if n.Lanes[i].ID != "market-in" {
					continue
				}
				if curved {
					low, high := 0.0, 100.0
					for range 60 {
						mid := (low + high) / 2
						n.Lanes[i].Control = &Point{X: entry.Position.X + mid, Y: entry.Position.Y + 29}
						if n.Length(n.Lanes[i]) < 60.001 {
							low = mid
						} else {
							high = mid
						}
					}
					n.Lanes[i].Control = &Point{X: entry.Position.X + high, Y: entry.Position.Y + 29}
				}
				incoming = n.Lanes[i]
			}
			s, err := expressPhysicalFleet(n, []Placement{{ID: "group", Class: ExpressClass, StationID: "market"}, {ID: "small", Class: CompactClass, StationID: "harbor"}})
			if err != nil {
				t.Fatal(err)
			}
			cells := s.laneCells[incoming.ID]
			if curved {
				if cells.count() != 3 {
					t.Fatalf("curve needs three real cells, got %d", cells.count())
				}
				pitch := n.Length(incoming) / float64(cells.count())
				end, _ := n.Node(incoming.To)
				if gap := pointDistance(n.Position(incoming, n.Length(incoming)-pitch), end.Position); gap >= 20 {
					t.Fatalf("curve lacks unsafe unextended endpoint boundary: %g", gap)
				}
			} else if cells.count() != 2 {
				t.Fatal("old24m lane lost its two12m cells")
			}
			if err := s.RequestJourney("small", "market"); err != nil {
				t.Fatal(err)
			}
			stopped := false
			for range 600 * TicksPerSecond {
				expressAdversarialTick(t, s)
				group, small := s.findVehicle("group"), s.findVehicle("small")
				if group.Pod.BerthID != "market-1" {
					t.Fatal("endpoint occupant ceased being stationary")
				}
				if small.Pod.Activity == Traveling && small.Pod.Speed == 0 && small.Pod.BlockedBy == "group" {
					stopped = true
					break
				}
			}
			if curved && s.findVehicle("small").Pod.LaneID != "market-in" {
				t.Fatalf("curved endpoint test did not enter actual incoming lane: %+v", s.findVehicle("small").Pod)
			}
			if !stopped || s.completed != 0 {
				t.Fatalf("real stationary endpoint was not reached completed=%d state=%+v", s.completed, s.Snapshot())
			}
			for range 2 * TicksPerSecond {
				expressAdversarialTick(t, s)
			}
			endpoint, _ := n.Node(incoming.To)
			for cell := range cells.count() {
				boundary := cellOffset(cell+1, cells.count(), n.Length(incoming))
				if math.Hypot(n.Position(incoming, boundary).X-endpoint.Position.X, n.Position(incoming, boundary).Y-endpoint.Position.Y) < 20 {
					if !slices.Contains(cells.cell(cell), resource{kind: nodeResource, id: incoming.To}) {
						t.Fatal("actual endpoint cell lacks occupied node")
					}
				}
			}
			if s.owners[resource{kind: nodeResource, id: "market-berth"}] != podResourceOwner("group") {
				t.Fatal("stationary Group lost endpoint node ownership")
			}
		})
	}
}

func TestExpressAdversarialLivePlaneTransitions(t *testing.T) {
	t.Parallel()
	n := largeAdversarialNetwork(t, true)
	for i := range n.Lanes {
		n.Lanes[i].SeparationGroup = "incoming"
		if n.Lanes[i].ID == "return" {
			n.Lanes[i].SeparationGroup = "outgoing"
			n.Lanes[i].Control = &Point{X: 800, Y: 2400}
		}
	}
	for i := range n.Stations {
		for j := range n.Stations[i].Berths {
			n.Stations[i].Berths[j].SeparationGroup = "berth"
		}
	}
	s, err := expressPhysicalFleet(n, []Placement{{ID: "group", Class: ExpressClass, StationID: "parking", BerthID: "parking-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("group", "market"); err != nil {
		t.Fatal(err)
	}
	origin, front, rear, arrival := false, false, false, false
	for range 1000 * TicksPerSecond {
		expressAdversarialTick(t, s)
		v := &s.vehicles[0]
		o := s.SafetyObservation()
		envelope, ok := o.envelopes[v.Pod.ID]
		if !ok || envelope.pod != v.Pod {
			t.Fatal("live body envelope is not bound to its unchanged pod")
		}
		has := func(plane string) bool {
			return slices.ContainsFunc(envelope.locations, func(l SafetyLocation) bool { return l.SeparationGroup == plane })
		}
		if v.Pod.Activity == Traveling && !v.originReleased {
			if !origin {
				// A nearby probe on the retained origin plane must not receive a
				// separation exemption from the actual departing body proof.
				probe := Pod{ID: "probe", Class: LegacyClass, Position: Point{X: v.Pod.Position.X + 1, Y: v.Pod.Position.Y}}
				location := SafetyLocation{SeparationGroup: "berth", From: "probe-node", To: "probe-node"}
				o.Pods = append(o.Pods, probe)
				o.Locations[probe.ID] = location
				o.envelopes[probe.ID] = safetyEnvelope{pod: probe, locations: []SafetyLocation{location}}
				if _, err := o.checkSeparation(); err == nil {
					t.Fatal("live origin body plane granted a false exemption")
				}
			}
			origin = true
			if !has("berth") || !has("incoming") {
				t.Fatal("live departure lost origin body planes")
			}
		}
		for i, lane := range v.Route {
			if lane.ID != "return" {
				continue
			}
			start, end := v.blocks.lanes[i].start, v.blocks.lanes[i+1].start
			if v.Pod.LaneID != "return" && v.distance < start && start-v.distance <= 6 {
				front = true
				if !has("outgoing") {
					t.Fatal("live front body plane transition omitted")
				}
			}
			if v.Pod.LaneID != "return" && v.distance > end && v.distance-end < 20 {
				rear = true
				if !has("outgoing") {
					t.Fatal("live retained incoming body plane omitted")
				}
			}
		}
		if v.Pod.BerthID == "market-1" {
			arrival = true
			if !has("berth") || !has("incoming") {
				t.Fatal("live arrival lost incident body plane")
			}
			break
		}
	}
	if !origin || !front || !rear || !arrival {
		t.Fatalf("live plane phases absent origin=%v front=%v rear=%v arrival=%v", origin, front, rear, arrival)
	}
}

func TestExpressAdversarialCurvedStoppedFollowing(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, classes := range [][2]VehicleClass{{ExpressClass, LegacyClass}, {LegacyClass, ExpressClass}, {ExpressClass, CompactClass}, {CompactClass, ExpressClass}, {ExpressClass, ExpressClass}, {ExpressClass, GroupClass}, {GroupClass, ExpressClass}} {
		t.Run(string(classes[0])+"-"+string(classes[1]), func(t *testing.T) {
			t.Parallel()
			n := largeAdversarialNetwork(t, true)
			for i := range n.Lanes {
				if n.Lanes[i].ID == "market-in" {
					n.Lanes[i].Control = &Point{X: 3080, Y: 1205}
				}
				if n.Lanes[i].ID == "market-out" {
					n.Lanes[i].VehicleClasses = largeGeometryClasses(t, "legacy", "compact")
				}
			}
			s, err := expressPhysicalFleet(n, []Placement{{ID: "leader", Class: classes[0], StationID: "garden"}, {ID: "follower", Class: classes[1], StationID: "harbor"}, {ID: "occupant", Class: ExpressClass, StationID: "market"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"leader", "follower"} {
				if err := s.RequestJourney(id, "market"); err != nil {
					t.Fatal(err)
				}
			}
			stable := 0
			gap := math.Inf(1)
			for range 600 * TicksPerSecond {
				expressAdversarialTick(t, s)
				leader, follower := s.findVehicle("leader"), s.findVehicle("follower")
				if leader.Pod.LaneID == "market-in" && follower.Pod.LaneID == "market-in" && leader.Pod.Speed == 0 && follower.Pod.Speed == 0 {
					stable++
					gap = pointDistance(leader.Pod.Position, follower.Pod.Position)
				} else {
					stable = 0
				}
				if stable >= TicksPerSecond {
					break
				}
			}
			if stable < TicksPerSecond || gap >= 40 || s.completed != 0 {
				t.Fatalf("close actual curved stop absent stable=%d gap=%g completed=%d state=%+v", stable, gap, s.completed, s.Snapshot())
			}
			if s.findVehicle("leader").Pod.LaneDistance <= s.findVehicle("follower").Pod.LaneDistance {
				t.Fatal("authored Group/Small queue order did not occur")
			}
			expressEvidence(t, "stopped-state", s.ExportState())
			t.Logf("independent stopped curve gap %.12g", gap)
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
