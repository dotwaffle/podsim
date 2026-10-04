package sim

import (
	"slices"
	"testing"
)

func couplingApproachJourneyNetwork(t *testing.T) (*PreparedNetwork, *couplingReservationNetwork) {
	t.Helper()
	base := couplingMotionFixture(t, false, false)
	geometry := CouplingGeometryInput{Contract: CompactPairV1CouplingContract, Network: base.Prepared.Network(),
		Sites: []CouplingSite{base.Network.sites["assembly"], base.Network.sites["split"]}, Corridors: []CouplingCorridor{base.Network.corridors["corridor"]}}
	network := &geometry.Network
	for i := range network.Nodes {
		node := &network.Nodes[i]
		switch node.ID {
		case "b":
			node.Position.X = 600
		case "c":
			node.Position.X = 1200
		case "front-entry", "rear-entry", "front-berth", "rear-berth", "front-exit", "rear-exit":
			node.Position.X += 780
		}
	}
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = 14
	}
	compact := classBit(string(CompactClass))
	for _, side := range []struct {
		id string
		y  float64
	}{{"front-origin", 1}, {"rear-origin", -1}} {
		entry, berth, exit := side.id+"-entry", side.id+"-berth", side.id+"-exit"
		network.Nodes = append(network.Nodes, Node{ID: entry, Position: Point{X: -80, Y: 60 * side.y}},
			Node{ID: berth, Position: Point{X: -50, Y: 40 * side.y}}, Node{ID: exit, Position: Point{X: -25, Y: 20 * side.y}})
		for _, lane := range []Lane{{ID: side.id + "-in", From: entry, To: berth}, {ID: side.id + "-out", From: berth, To: exit},
			{ID: side.id + "-through", From: entry, To: exit}, {ID: side.id + "-feed", From: exit, To: "a"}} {
			lane.SpeedLimit, lane.VehicleClasses = 14, compact
			network.Lanes = append(network.Lanes, lane)
		}
		network.Stations = append(network.Stations, Station{ID: side.id, Name: side.id, Entry: entry, Exit: exit, VehicleClasses: compact,
			Berths: []Berth{{ID: berth, Node: berth, VehicleClasses: compact}}})
	}
	network.Nodes = append(network.Nodes, Node{ID: "block-entry", Position: Point{X: 500, Y: 40}},
		Node{ID: "block-berth", Position: Point{X: 750, Y: 300}}, Node{ID: "block-exit", Position: Point{X: 800, Y: 250}})
	for _, lane := range []Lane{{ID: "block-road", From: "b", To: "block-entry"}, {ID: "block-in", From: "block-entry", To: "block-berth"},
		{ID: "block-out", From: "block-berth", To: "block-exit"}, {ID: "block-through", From: "block-entry", To: "block-exit"}} {
		lane.SpeedLimit, lane.VehicleClasses = 1, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: "block-goal", Name: "Block goal", Entry: "block-entry", Exit: "block-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: "block-berth", Node: "block-berth", VehicleClasses: compact}}})
	p, err := PrepareNetwork(*network)
	if err != nil {
		t.Fatal(err)
	}
	// Author the site from immutable cells before any fleet or journey exists.
	// The ordinary b-junction zone admits as one movement, so E is its frontier.
	cells := p.laneCells["ab"]
	zone := -1
	for cell := range cells.count() {
		if slices.Contains(cells.cell(cell), resource{kind: junctionResource, id: "b"}) {
			zone = cell
			break
		}
	}
	if zone < 2 {
		t.Fatal("authored blocker branch lacks an upstream ordinary conflict zone")
	}
	e := cellOffset(zone, cells.count(), p.graph.lengths[p.graph.lanes["ab"]])
	geometry.Sites[0] = CouplingSite{ID: "assembly", LaneID: "ab", StartMeters: e - 30, EndMeters: e + 40, RearStagingMeters: e, FrontStagingMeters: e + 12}
	geometry.Network = p.Network()
	n, err := prepareCouplingReservations(p, geometry)
	if err != nil {
		t.Fatal(err)
	}
	return p, n
}

// Real native departures and a moving blocker must create this starting queue.
// This does not install an approach adapter into Step or prove mechanical travel.
func TestCouplingApproachActualNativeStartingQueue(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		occupied bool
	}{{"empty", false}, {"occupied", true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, n := couplingApproachJourneyNetwork(t)
			s, err := p.NewFleet([]Placement{{ID: "blocker", Class: CompactClass, StationID: "origin"},
				{ID: "front", Class: CompactClass, StationID: "front-origin"}, {ID: "rear", Class: CompactClass, StationID: "rear-origin"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			if err := s.SetPlatoonLimit(2); err != nil {
				t.Fatal(err)
			}
			for _, trip := range []struct {
				id, goal string
				size     int
			}{{"blocker", "block-goal", 1}, {"front", "front-goal", 1}, {"rear", "rear-goal", 2}} {
				if test.occupied {
					if err := s.RequestJourneyOptions(trip.id, TripOptions{To: trip.goal, PartySize: trip.size, SharingConsent: PrivateConsent}); err != nil {
						t.Fatal(err)
					}
				} else {
					station, _ := s.network.Station(trip.goal)
					if err := s.startEmptyMove(s.findVehicle(trip.id), emptyDestination{station: trip.goal, berth: station.Berths[0], reserveBerth: true}); err != nil {
						t.Fatal(err)
					}
				}
			}
			departed := make(map[string]bool)
			e := n.sites["assembly"].RearStagingMeters
			t.Logf("authored E=%v, ab cells=%d", e, p.laneCells["ab"].count())
			stops, pairedStops := make(map[float64]bool), make(map[float64]bool)
			for range 12000 {
				s.Step()
				checkIncrementalOwners(t, s)
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				for _, id := range []string{"blocker", "front", "rear"} {
					v := s.findVehicle(id)
					departed[id] = departed[id] || v.Pod.Activity == Traveling && v.distance > 0
				}
				front, rear := s.findVehicle("front"), s.findVehicle("rear")
				if front.Pod.LaneID == "ab" && front.Pod.Speed == 0 && (!stops[front.Pod.LaneDistance] || rear.Pod.Speed == 0 && !pairedStops[front.Pod.LaneDistance]) {
					stops[front.Pod.LaneDistance] = true
					pairedStops[front.Pod.LaneDistance] = rear.Pod.Speed == 0
					t.Logf("native stop tick=%d front=%v through=%d follower=%d link=%d rear=%s:%v speed=%v through=%d link=%d pending=%t blocker=%s:%v speed=%v", s.tick,
						front.Pod.LaneDistance, front.reservedThrough, front.follower, front.link.leader, rear.Pod.LaneID, rear.Pod.LaneDistance, rear.Pod.Speed,
						rear.reservedThrough, rear.link.leader, s.holdsPending(rear), s.findVehicle("blocker").Pod.LaneID, s.findVehicle("blocker").Pod.LaneDistance, s.findVehicle("blocker").Pod.Speed)
				}
				if front.Pod.LaneID != "ab" || front.Pod.LaneDistance != e || front.Pod.Speed != 0 || rear.Pod.Speed != 0 || rear.link.leader == 0 || front.link.leader != 0 {
					continue
				}
				if !departed["blocker"] || !departed["front"] || !departed["rear"] || !s.holdsPending(rear) || front.follower == 0 {
					t.Fatal("queue lacks real departures, native link, or borrowed owner")
				}
				if test.occupied && (rear.PassengersAboard() != 2 || len(rear.Riders) != 1 || rear.Riders[0].SharingConsent != PrivateConsent || rear.Riders[0].Completed) {
					t.Fatal("actual native queue changed its whole private party")
				}
				input := couplingApproachPrepareInput{Simulation: s, Network: n, Prepared: p, CorridorID: "corridor", Members: [2]string{"front", "rear"}, Enabled: true}
				c, state, err := prepareCouplingApproach(input)
				if err != nil {
					t.Fatalf("actual queue failed approach preparation at tick %d: %v", s.tick, err)
				}
				if state.WaitTick != -1 || c.start != front.distance || c.target-c.start != 12 || !slices.EqualFunc(c.members[0].route, front.Route, func(a, b Lane) bool { return a.ID == b.ID }) {
					t.Fatal("actual queue preparation changed timing, route, or coordinates")
				}
				t.Logf("actual native starting queue at tick %d, front E=%v, rear local=%v, blocker local=%v", s.tick, e, rear.Pod.LaneDistance, s.findVehicle("blocker").Pod.LaneDistance)
				return
			}
			t.Fatalf("actual native journeys never reached the authored starting queue: front=%+v rear=%+v", s.findVehicle("front").Pod, s.findVehicle("rear").Pod)
		})
	}
}
