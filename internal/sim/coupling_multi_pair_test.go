package sim

import (
	"math"
	"slices"
	"strings"
	"testing"
)

type couplingMultiTrip struct {
	id, origin, goal string
	size             int
	// The ordinary request or empty move starts at this tick.
	tick int64
}

type couplingMultiScenario struct {
	prepared  *PreparedNetwork
	contracts FleetContracts
	trips     []couplingMultiTrip
}

// Add a one-berth Compact station whose exit feeds node "a".
func couplingMultiAddOrigin(network *Network, id string, entry, berth, exit Point) {
	compact := classBit(string(CompactClass))
	network.Nodes = append(network.Nodes, Node{ID: id + "-entry", Position: entry}, Node{ID: id + "-berth", Position: berth}, Node{ID: id + "-exit", Position: exit})
	for _, lane := range []Lane{{ID: id + "-in", From: id + "-entry", To: id + "-berth"}, {ID: id + "-out", From: id + "-berth", To: id + "-exit"},
		{ID: id + "-through", From: id + "-entry", To: id + "-exit"}, {ID: id + "-feed", From: id + "-exit", To: "a"}} {
		lane.SpeedLimit, lane.VehicleClasses = 14, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: id + "-berth", Node: id + "-berth", VehicleClasses: compact}}})
}

// Add a one-berth Compact goal whose road leaves node "c".
func couplingMultiAddGoal(network *Network, id string, y float64) {
	compact := classBit(string(CompactClass))
	network.Nodes = append(network.Nodes, Node{ID: id + "-entry", Position: Point{X: 1400, Y: y}},
		Node{ID: id + "-berth", Position: Point{X: 1450, Y: y + 25*math.Copysign(1, y)}}, Node{ID: id + "-exit", Position: Point{X: 1510, Y: y + 25*math.Copysign(1, y)}})
	for _, lane := range []Lane{{ID: id + "-road", From: "c", To: id + "-entry"}, {ID: id + "-in", From: id + "-entry", To: id + "-berth"},
		{ID: id + "-through", From: id + "-entry", To: id + "-exit"}, {ID: id + "-out", From: id + "-berth", To: id + "-exit"}} {
		lane.SpeedLimit, lane.VehicleClasses = 14, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: id + "-berth", Node: id + "-berth", VehicleClasses: compact}}})
}

// Copy a network with renamed records and a lateral offset.
func couplingMultiCopy(dst *Network, src Network, prefix string, dy float64) {
	rename := func(id string) string {
		if id == "" {
			return ""
		}
		return prefix + id
	}
	for _, node := range src.Nodes {
		node.ID, node.Position.Y = rename(node.ID), node.Position.Y+dy
		dst.Nodes = append(dst.Nodes, node)
	}
	for _, lane := range src.Lanes {
		lane.ID, lane.From, lane.To, lane.StationID = rename(lane.ID), rename(lane.From), rename(lane.To), rename(lane.StationID)
		if lane.Control != nil {
			control := *lane.Control
			control.Y += dy
			lane.Control = &control
		}
		dst.Lanes = append(dst.Lanes, lane)
	}
	for _, station := range src.Stations {
		station.ID, station.Name, station.Entry, station.Exit = rename(station.ID), rename(station.Name), rename(station.Entry), rename(station.Exit)
		station.Berths = slices.Clone(station.Berths)
		for i := range station.Berths {
			station.Berths[i].ID, station.Berths[i].Node = rename(station.Berths[i].ID), rename(station.Berths[i].Node)
		}
		dst.Stations = append(dst.Stations, station)
	}
}

// The assembly rear staging point is the start of the ordinary b-junction zone
// on lane ab, as in the single-pair journey network.
func couplingMultiSites(t *testing.T, p *PreparedNetwork, prefix string) []CouplingSite {
	t.Helper()
	ab := prefix + "ab"
	cells := p.laneCells[ab]
	zone := -1
	for cell := range cells.count() {
		if slices.Contains(cells.cell(cell), resource{kind: junctionResource, id: prefix + "b"}) {
			zone = cell
			break
		}
	}
	if zone < 2 {
		t.Fatal("authored corridor lacks an upstream ordinary conflict zone")
	}
	e := cellOffset(zone, cells.count(), p.graph.lengths[p.graph.lanes[ab]])
	return []CouplingSite{{ID: prefix + "assembly", LaneID: ab, StartMeters: e - 30, EndMeters: e + 40, RearStagingMeters: e, FrontStagingMeters: e + 12},
		{ID: prefix + "split", LaneID: prefix + "bc", StartMeters: 50, EndMeters: 140, RearStagingMeters: 70, FrontStagingMeters: 82}}
}

func couplingMultiCorridor(prefix string) CouplingCorridor {
	return CouplingCorridor{ID: prefix + "corridor", AssemblySiteID: prefix + "assembly", SplitSiteID: prefix + "split", LaneIDs: []string{prefix + "ab", prefix + "bc"}}
}

func couplingMultiBase(t *testing.T) Network {
	t.Helper()
	p, _ := couplingApproachJourneyNetwork(t)
	return p.Network()
}

// Two disjoint copies of the single-pair journey network share one fleet.
// The second copy starts its trips after the stagger.
func couplingMultiParallel(t *testing.T, stagger int64) couplingMultiScenario {
	t.Helper()
	base := couplingMultiBase(t)
	var network Network
	var trips []couplingMultiTrip
	for i, prefix := range []string{"a-", "b-"} {
		couplingMultiCopy(&network, base, prefix, float64(i)*3000)
		trips = append(trips, couplingMultiTrip{prefix + "blocker", prefix + "origin", prefix + "block-goal", 1, int64(i) * stagger},
			couplingMultiTrip{prefix + "front", prefix + "front-origin", prefix + "front-goal", 1, int64(i) * stagger},
			couplingMultiTrip{prefix + "rear", prefix + "rear-origin", prefix + "rear-goal", 2, int64(i) * stagger})
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	contracts := FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true}
	for _, prefix := range []string{"a-", "b-"} {
		contracts.CouplingSites = append(contracts.CouplingSites, couplingMultiSites(t, p, prefix)...)
		contracts.CouplingCorridors = append(contracts.CouplingCorridors, couplingMultiCorridor(prefix))
	}
	return couplingMultiScenario{prepared: p, contracts: contracts, trips: trips}
}

// A second trio requests the same corridor and sites after the first pair.
func couplingMultiShared(t *testing.T, second int64) couplingMultiScenario {
	t.Helper()
	network := couplingMultiBase(t)
	couplingMultiAddOrigin(&network, "blocker2-origin", Point{X: -200, Y: 130}, Point{X: -170, Y: 110}, Point{X: -145, Y: 90})
	couplingMultiAddOrigin(&network, "front2-origin", Point{X: -200, Y: 230}, Point{X: -170, Y: 210}, Point{X: -145, Y: 190})
	couplingMultiAddOrigin(&network, "rear2-origin", Point{X: -200, Y: -230}, Point{X: -170, Y: -210}, Point{X: -145, Y: -190})
	// A second slow branch mirrors block-goal, so a second blocker can park.
	compact := classBit(string(CompactClass))
	network.Nodes = append(network.Nodes, Node{ID: "block2-entry", Position: Point{X: 500, Y: -40}},
		Node{ID: "block2-berth", Position: Point{X: 750, Y: -300}}, Node{ID: "block2-exit", Position: Point{X: 800, Y: -250}})
	for _, lane := range []Lane{{ID: "block2-road", From: "b", To: "block2-entry"}, {ID: "block2-in", From: "block2-entry", To: "block2-berth"},
		{ID: "block2-out", From: "block2-berth", To: "block2-exit"}, {ID: "block2-through", From: "block2-entry", To: "block2-exit"}} {
		lane.SpeedLimit, lane.VehicleClasses = 1, compact
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, Station{ID: "block2-goal", Name: "Block 2 goal", Entry: "block2-entry", Exit: "block2-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: "block2-berth", Node: "block2-berth", VehicleClasses: compact}}})
	couplingMultiAddGoal(&network, "front2-goal", 400)
	couplingMultiAddGoal(&network, "rear2-goal", -400)
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	contracts := FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
		CouplingSites: couplingMultiSites(t, p, ""), CouplingCorridors: []CouplingCorridor{couplingMultiCorridor("")}}
	trips := []couplingMultiTrip{{"blocker", "origin", "block-goal", 1, 0}, {"front", "front-origin", "front-goal", 1, 0}, {"rear", "rear-origin", "rear-goal", 2, 0},
		{"blocker2", "blocker2-origin", "block2-goal", 1, second}, {"front2", "front2-origin", "front2-goal", 1, second + 300}, {"rear2", "rear2-origin", "rear2-goal", 2, second + 300}}
	return couplingMultiScenario{prepared: p, contracts: contracts, trips: trips}
}

func (sc couplingMultiScenario) start(t *testing.T, enabled bool) *Simulation {
	t.Helper()
	placements := make([]Placement, 0, len(sc.trips))
	for _, trip := range sc.trips {
		placements = append(placements, Placement{ID: trip.id, Class: CompactClass, StationID: trip.origin})
	}
	contracts := sc.contracts
	contracts.CouplingEnabled = enabled
	s, err := sc.prepared.NewFleetWithContracts(placements, contracts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	s.SetMotionRecording(true)
	return s
}

// Start each ordinary request or empty move at its trip tick.
func (sc couplingMultiScenario) request(t *testing.T, s *Simulation, occupied bool, requests map[string]int) {
	t.Helper()
	for _, trip := range sc.trips {
		if trip.tick != s.tick {
			continue
		}
		if occupied {
			if err := s.RequestJourneyOptions(trip.id, TripOptions{To: trip.goal, PartySize: trip.size, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
			v := s.findVehicle(trip.id)
			requests[trip.id] = v.Riders[len(v.Riders)-1].ID
		} else {
			station, _ := s.station(trip.goal)
			if err := s.startEmptyMove(s.findVehicle(trip.id), emptyDestination{station: trip.goal, berth: station.Berths[0], reserveBerth: true}); err != nil {
				t.Fatal(err)
			}
			requests[trip.id] = 0
		}
	}
}

type couplingMultiGroup struct {
	id                    string
	members               [2]string
	corridor              string
	request               int64
	wait, deadline        int64
	formation, retirement int64
	phases                map[couplingReservationPhase]int64
	riders                [2][]Request
}

type couplingMultiApproach struct {
	members        [2]string
	moving         bool
	wait, deadline int64
}

type couplingMultiResult struct {
	groups      []*couplingMultiGroup
	overlap     int64
	firstShared int64
	idle        int64
}

var couplingMultiPhases = []couplingReservationPhase{couplingClosing, couplingLatching, couplingConnected, couplingUnlatching, couplingOpening, couplingDraining}

// Run one cohort until every pod is idle, with the single-pair native checks
// at every tick and registry checks before them.
func runCouplingMulti(t *testing.T, sc couplingMultiScenario, occupied, enabled bool, horizon int64) couplingMultiResult {
	t.Helper()
	s := sc.start(t, enabled)
	requests := make(map[string]int, len(sc.trips))
	requestTicks := make(map[string]int64, len(sc.trips))
	for _, trip := range sc.trips {
		requestTicks[trip.id] = trip.tick
	}
	sc.request(t, s, occupied, requests)
	result := couplingMultiResult{firstShared: -1}
	groups := make(map[string]*couplingMultiGroup)
	podGroup := make(map[string]string)
	approaches := make(map[string]*couplingMultiApproach)
	completed := make(map[int]bool)
	var measured float64
	for s.tick < horizon {
		s.Step()
		live := make(map[string]bool, len(s.couplingGroups))
		members := make(map[string]string)
		for _, g := range s.couplingGroups {
			id := g.context.owner.id
			pair := [2]string{g.context.reservation.members[0].Vehicle.Pod.ID, g.context.reservation.members[1].Vehicle.Pod.ID}
			live[id] = true
			for _, pod := range pair {
				if other, ok := members[pod]; ok {
					t.Fatalf("pod %s is a member of live groups %s and %s at tick %d", pod, other, id, s.tick)
				}
				members[pod] = id
				if first, ok := podGroup[pod]; ok && first != id {
					t.Fatalf("pod %s was recruited into groups %s and %s at tick %d", pod, first, id, s.tick)
				}
				podGroup[pod] = id
				if s.findVehicle(pod).couplingID != id {
					t.Fatalf("member %s does not name its group %s at tick %d", pod, id, s.tick)
				}
			}
			log := groups[id]
			if log == nil {
				log = &couplingMultiGroup{id: id, members: pair, corridor: g.context.reservation.corridorID, formation: g.formationTick, retirement: -1,
					request: max(requestTicks[pair[0]], requestTicks[pair[1]]), wait: -1, deadline: -1, phases: make(map[couplingReservationPhase]int64)}
				a := approaches[pair[0]]
				if g.formationTick != s.tick || a == nil || a.members != pair || !a.moving || a.wait < 0 || a.deadline-a.wait != 300 || g.formationTick > a.deadline ||
					s.findVehicle(pair[1]).link.leader != 0 || s.findVehicle(pair[0]).follower != 0 {
					t.Fatalf("group %s lacks a powered staging approach, retired link, or original deadline: %+v", id, a)
				}
				log.wait, log.deadline = a.wait, a.deadline
				for i, pod := range pair {
					log.riders[i] = slices.Clone(s.findVehicle(pod).Riders)
					if occupied != (len(log.riders[i]) == 1) {
						t.Fatalf("group %s member %s has the wrong cohort: %+v", id, pod, log.riders[i])
					}
				}
				groups[id] = log
				result.groups = append(result.groups, log)
			}
			if g.formationTick != log.formation || g.context.reservation.corridorID != log.corridor {
				t.Fatalf("group %s changed its formation tick or corridor", id)
			}
			if _, seen := log.phases[g.state.Phase]; !seen {
				log.phases[g.state.Phase] = s.tick
			}
			for i, pod := range pair {
				if !slices.Equal(s.findVehicle(pod).Riders, log.riders[i]) {
					t.Fatalf("group %s changed the order, party, consent, or timing of %s at tick %d", id, pod, s.tick)
				}
			}
		}
		for _, log := range groups {
			if log.retirement >= 0 || live[log.id] {
				continue
			}
			log.retirement = s.tick
			for _, phase := range couplingMultiPhases {
				if _, seen := log.phases[phase]; !seen {
					t.Fatalf("group %s skipped mechanical phase %s", log.id, savedCouplingPhase(phase))
				}
			}
			for _, pod := range log.members {
				if s.findVehicle(pod).couplingID != "" {
					t.Fatalf("retired group %s left member %s bound", log.id, pod)
				}
			}
		}
		if len(live) >= 2 {
			result.overlap++
			if result.firstShared < 0 {
				result.firstShared = s.tick
			}
		}
		for _, a := range s.couplingApproaches {
			pair := [2]string{a.context.members[0].id, a.context.members[1].id}
			for _, pod := range pair {
				if _, ok := members[pod]; ok {
					t.Fatalf("pod %s is in an approach and a live group at tick %d", pod, s.tick)
				}
			}
			log := approaches[pair[0]]
			if log == nil || log.members != pair {
				log = &couplingMultiApproach{members: pair, wait: -1, deadline: -1}
				approaches[pair[0]] = log
			}
			log.moving = log.moving || a.state.Phase == couplingApproachMoving
			if a.state.WaitTick >= 0 {
				if log.wait < 0 {
					log.wait, log.deadline = a.state.WaitTick, a.state.DeadlineTick
				}
				if a.state.WaitTick != log.wait || a.state.DeadlineTick != log.deadline {
					t.Fatalf("approach %v reset its intentional wait", pair)
				}
			}
		}
		if !enabled && (len(s.couplingApproaches) != 0 || len(s.couplingGroups) != 0) {
			t.Fatalf("disabled coupling recruited at tick %d", s.tick)
		}
		checkCouplingApproachNativeBoundary(t, s)
		frame, _ := s.MotionFrame()
		for _, sample := range frame.Samples {
			measured += sample.DistanceMeters
		}
		if math.Abs(measured-s.passengerDistanceMeters-s.emptyDistanceMeters) > 1e-6 {
			t.Fatal("native motion samples disagree with distance accounting")
		}
		for _, completion := range s.StepCompletions() {
			if completed[completion.RequestID] {
				t.Fatal("native completion replayed", completion)
			}
			completed[completion.RequestID] = true
		}
		sc.request(t, s, occupied, requests)
		idle := len(requests) == len(sc.trips) && len(s.couplingGroups) == 0 && len(s.couplingApproaches) == 0
		for _, v := range s.vehicles {
			idle = idle && v.Pod.Activity == Idle
		}
		if !idle {
			continue
		}
		result.idle = s.tick
		for _, trip := range sc.trips {
			if v := s.findVehicle(trip.id); v.Pod.StationID != trip.goal || occupied && !completed[requests[trip.id]] {
				t.Fatalf("journey %s did not complete at %s: %+v", trip.id, trip.goal, v.Pod)
			}
		}
		if occupied && (s.completed != len(sc.trips) || s.boarded != len(sc.trips) || len(completed) != len(sc.trips)) {
			t.Fatalf("party completion or boarding conservation failed: completed=%d boarded=%d", s.completed, s.boarded)
		}
		for _, log := range result.groups {
			t.Logf("group %s members=%s,%s corridor=%s request=%d wait=%d deadline=%d formation=%d closing=%d latching=%d connected=%d unlatching=%d opening=%d draining=%d retirement=%d",
				log.id, log.members[0], log.members[1], log.corridor, log.request, log.wait, log.deadline, log.formation, log.phases[couplingClosing], log.phases[couplingLatching],
				log.phases[couplingConnected], log.phases[couplingUnlatching], log.phases[couplingOpening], log.phases[couplingDraining], log.retirement)
		}
		t.Logf("groups=%d overlapTicks=%d firstOverlap=%d fleetIdle=%d horizon=%d", len(result.groups), result.overlap, result.firstShared, result.idle, horizon)
		return result
	}
	for _, v := range s.vehicles {
		t.Logf("%s %+v", v.Pod.ID, v.Pod)
	}
	t.Fatalf("fleet missed its %d-tick idle bound", horizon)
	return result
}

// Two pairs form from ordinary departures. The parallel rows use two
// corridors, and the staggered row adopts its second group while the first
// group is committed. The shared row sends a second trio through the same
// corridor and sites after the first group drains. Each row has a disabled
// control with the same placements and trips.
func TestCouplingMultiPairNaturalFormation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		build func(*testing.T) couplingMultiScenario
		// The pairs that must form, in formation order, and their corridors.
		pairs     [2][2]string
		corridors [2]string
		shared    bool
		// Measured fleet idle is driven by the 1 m/s blockers: 31086 to 31385
		// (sync), 34086 to 34385 (staggered), and 43955 to 44254 (shared).
		horizon int64
	}{
		{"parallel-sync", func(t *testing.T) couplingMultiScenario { t.Helper(); return couplingMultiParallel(t, 0) },
			[2][2]string{{"a-front", "a-rear"}, {"b-front", "b-rear"}}, [2]string{"a-corridor", "b-corridor"}, false, 36000},
		{"parallel-staggered", func(t *testing.T) couplingMultiScenario { t.Helper(); return couplingMultiParallel(t, 3000) },
			[2][2]string{{"a-front", "a-rear"}, {"b-front", "b-rear"}}, [2]string{"a-corridor", "b-corridor"}, false, 39000},
		{"shared-sequential", func(t *testing.T) couplingMultiScenario { t.Helper(); return couplingMultiShared(t, 12000) },
			[2][2]string{{"front", "rear"}, {"front2", "rear2"}}, [2]string{"corridor", "corridor"}, true, 49000},
	} {
		for _, occupied := range []bool{false, true} {
			for _, enabled := range []bool{true, false} {
				name := tc.name + "/" + map[bool]string{false: "empty", true: "occupied"}[occupied] + "/" + map[bool]string{false: "disabled", true: "enabled"}[enabled]
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					result := runCouplingMulti(t, tc.build(t), occupied, enabled, tc.horizon)
					if !enabled {
						if len(result.groups) != 0 {
							t.Fatal("disabled control formed a group")
						}
						return
					}
					if len(result.groups) != 2 {
						t.Fatalf("natural fleet formed %d groups, want two", len(result.groups))
					}
					// One corridor admits a second pair only after the first
					// group has released its sites; separate corridors overlap.
					if tc.shared != (result.overlap == 0) || tc.shared && result.groups[1].formation <= result.groups[0].retirement {
						t.Fatalf("groups shared %d ticks, want shared corridor=%t", result.overlap, tc.shared)
					}
					pods := make(map[string]bool)
					for i, g := range result.groups {
						if g.members != tc.pairs[i] || g.corridor != tc.corridors[i] {
							t.Fatalf("group %s has members %v on %s", g.id, g.members, g.corridor)
						}
						pods[g.members[0]], pods[g.members[1]] = true, true
						// The single-pair bounds apply from each pair's request tick.
						if g.formation > g.request+12000 || g.retirement < 0 || g.retirement > g.request+18000 {
							t.Fatalf("group %s missed its formation or retirement bound: %+v", g.id, *g)
						}
					}
					if len(pods) != 4 {
						t.Fatal("groups share a member")
					}
				})
			}
		}
	}
}

// Longer origin feeds put the route distance at E in a higher binade than
// E, so the lane-local coordinate does not round-trip. Natural staging
// must still form its pair. Measured fleet idle with the slower blocker is
// 59196 to 59495 ticks.
func TestCouplingApproachInexactStagingOffset(t *testing.T) {
	t.Parallel()
	network := couplingMultiBase(t)
	for i := range network.Nodes {
		if id := network.Nodes[i].ID; strings.HasPrefix(id, "front-origin-") || strings.HasPrefix(id, "rear-origin-") {
			network.Nodes[i].Position.X -= 540
		}
	}
	// A slower blocker holds junction b until the later rear has queued.
	for i := range network.Lanes {
		if strings.HasPrefix(network.Lanes[i].ID, "block-") {
			network.Lanes[i].SpeedLimit = 0.5
		}
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	sites := couplingMultiSites(t, p, "")
	sc := couplingMultiScenario{prepared: p, contracts: FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
		CouplingSites: sites, CouplingCorridors: []CouplingCorridor{couplingMultiCorridor("")}},
		trips: []couplingMultiTrip{{"blocker", "origin", "block-goal", 1, 0}, {"front", "front-origin", "front-goal", 1, 0}, {"rear", "rear-origin", "rear-goal", 2, 0}}}
	// Guard the fixture: the front's route distance at E does not round-trip.
	guard := sc.start(t, true)
	sc.request(t, guard, false, make(map[string]int))
	front := guard.findVehicle("front")
	lane := slices.IndexFunc(front.Route, func(lane Lane) bool { return lane.ID == "ab" })
	origin, e := front.blocks.lanes[lane].start, sites[0].RearStagingMeters
	if lane < 0 || (origin+e)-origin == e {
		t.Fatalf("fixture prefix %.17g and offset %.17g round-trip exactly", origin, e)
	}
	for _, occupied := range []bool{false, true} {
		result := runCouplingMulti(t, sc, occupied, true, 66000)
		if len(result.groups) != 1 || result.groups[0].members != [2]string{"front", "rear"} || result.groups[0].formation > 12000 {
			t.Fatalf("inexact staging offset formed %d groups", len(result.groups))
		}
	}
}
