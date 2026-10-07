package sim

import (
	"math"
	"slices"
	"testing"
)

func largeAdversarialNetwork(t *testing.T, allLarge bool) Network {
	t.Helper()
	n := stationBufferNetwork(Example(), 4)
	classes := largeGeometryClasses(t, "legacy", "compact", "group")
	for i := range n.Stations {
		n.Stations[i].VehicleClasses = classes
		n.Stations[i].ParkingOnly = false
		for j := range n.Stations[i].Berths {
			n.Stations[i].Berths[j].VehicleClasses = classes
		}
	}
	if allLarge {
		for i := range n.Lanes {
			n.Lanes[i].VehicleClasses = classes
		}
	}
	return n
}

// This check measures visible centers and scans ownership independently of the
// large envelope oracle. These journey fixtures use one physical plane.
func largeAdversarialTick(t *testing.T, s *Simulation) {
	t.Helper()
	s.Step()
	for i, first := range s.vehicles {
		for _, second := range s.vehicles[i+1:] {
			if !largeVehicleClass(first.Pod.Class) && !largeVehicleClass(second.Pod.Class) {
				continue
			}
			gap := math.Hypot(first.Pod.Position.X-second.Pod.Position.X, first.Pod.Position.Y-second.Pod.Position.Y)
			if gap < 20-1e-6 {
				t.Fatalf("independent large center bound tick %d gap %.12g: %+v %+v owners=%v", s.tick, gap, first.Pod, second.Pod, s.owners)
			}
		}
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	checkIncrementalOwners(t, s)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if largeVehicleClass(v.Pod.Class) && (v.link.leader != 0 || v.follower != 0) {
			t.Fatal("actual Group acquired a link")
		}
		if v.Pod.Activity == Traveling && v.distance+stoppingDistance(v.Pod.Speed) > v.blocks.end(v.reservedThrough)+1e-8 {
			t.Fatal("actual journey has an unowned stopping point")
		}
	}
}

func TestLargeAdversarialCurvedFollowing(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, classes := range [][2]VehicleClass{{GroupClass, LegacyClass}, {LegacyClass, GroupClass}, {GroupClass, CompactClass}, {CompactClass, GroupClass}, {GroupClass, GroupClass}} {
		t.Run(string(classes[0])+"-"+string(classes[1]), func(t *testing.T) {
			t.Parallel()
			n := largeAdversarialNetwork(t, true)
			for i := range n.Lanes {
				if n.Lanes[i].ID == "return" {
					n.Lanes[i].Control = &Point{X: 800, Y: 2400}
				}
			}
			s, err := NewFleet(n, []Placement{{ID: "one", Class: classes[0], StationID: "parking", BerthID: "parking-1"}, {ID: "two", Class: classes[1], StationID: "parking", BerthID: "parking-2"}})
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
				largeAdversarialTick(t, s)
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
			t.Logf("same curve moving pair minimum=%.9g tick=%d", minimum, s.tick)
		})
	}
}

func TestLargeAdversarialStationaryEndpoint(t *testing.T) {
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
			s, err := NewFleet(n, []Placement{{ID: "group", Class: GroupClass, StationID: "market"}, {ID: "small", Class: CompactClass, StationID: "harbor"}})
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
				largeAdversarialTick(t, s)
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
				largeAdversarialTick(t, s)
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

func TestLargeAdversarialLivePlaneTransitions(t *testing.T) {
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
	s, err := NewFleet(n, []Placement{{ID: "group", Class: GroupClass, StationID: "parking", BerthID: "parking-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("group", "market"); err != nil {
		t.Fatal(err)
	}
	origin, front, rear, arrival := false, false, false, false
	for range 1000 * TicksPerSecond {
		largeAdversarialTick(t, s)
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

func TestLargeAdversarialCurvedStoppedFollowing(t *testing.T) {
	t.Parallel()
	for _, classes := range [][2]VehicleClass{{GroupClass, LegacyClass}, {LegacyClass, GroupClass}, {GroupClass, CompactClass}, {CompactClass, GroupClass}, {GroupClass, GroupClass}} {
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
			s, err := NewFleet(n, []Placement{{ID: "leader", Class: classes[0], StationID: "garden"}, {ID: "follower", Class: classes[1], StationID: "harbor"}, {ID: "occupant", Class: GroupClass, StationID: "market"}})
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
				largeAdversarialTick(t, s)
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
			t.Logf("independent stopped curve gap %.12g", gap)
		})
	}
}
