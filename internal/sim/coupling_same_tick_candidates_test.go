package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

// Two candidates for one corridor cannot be ready in the same tick. A ready
// candidate has its front at rest at the corridor's front staging point and
// its rear at the rear staging point (coupling_approach.go:150,153), both
// lane-local offsets of the corridor's assembly lane
// (coupling_approach_context.go:112,119). Two such candidates would put two
// fronts at one point of one lane. A candidate must also plan its reservation
// (coupling_approach.go:154), which claims every cell from the start of the
// corridor's first lane through its exit (coupling_reservation.go:435-441)
// and refuses any foreign owner (coupling_reservation.go:145-146). So a
// second pair on the same corridor path is never ready while the first pair
// holds its cells.
//
// The nearest natural contest uses two corridors whose exits share junction
// a-c. Copy b of the single-pair journey network is turned 90 degrees about
// a-c and merged into it. Before formation no pod owns a-c, so both pairs
// become ready at the same boundary and both reservations plan.
func couplingSameTickScenario(t *testing.T) couplingMultiScenario {
	t.Helper()
	base := couplingMultiBase(t)
	var network, turned Network
	couplingMultiCopy(&network, base, "a-", 0)
	couplingMultiCopy(&turned, base, "b-", 0)
	// The quarter turn about a-c at (1200, 0) is exact for these coordinates.
	turn := func(p Point) Point { return Point{X: 1200 - p.Y, Y: p.X - 1200} }
	for _, node := range turned.Nodes {
		if node.ID != "b-c" {
			node.Position = turn(node.Position)
			network.Nodes = append(network.Nodes, node)
		}
	}
	for _, lane := range turned.Lanes {
		if lane.Control != nil {
			control := turn(*lane.Control)
			lane.Control = &control
		}
		if lane.From == "b-c" {
			lane.From = "a-c"
		}
		if lane.To == "b-c" {
			lane.To = "a-c"
		}
		network.Lanes = append(network.Lanes, lane)
	}
	network.Stations = append(network.Stations, turned.Stations...)
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	contracts := FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true}
	var trips []couplingMultiTrip
	for _, prefix := range []string{"a-", "b-"} {
		contracts.CouplingSites = append(contracts.CouplingSites, couplingMultiSites(t, p, prefix)...)
		contracts.CouplingCorridors = append(contracts.CouplingCorridors, couplingMultiCorridor(prefix))
		trips = append(trips, couplingMultiTrip{prefix + "blocker", prefix + "origin", prefix + "block-goal", 1, 0},
			couplingMultiTrip{prefix + "front", prefix + "front-origin", prefix + "front-goal", 1, 0},
			couplingMultiTrip{prefix + "rear", prefix + "rear-origin", prefix + "rear-goal", 2, 0})
	}
	return couplingMultiScenario{prepared: p, contracts: contracts, trips: trips}
}

// The state of one candidate at a tick boundary.
type couplingSameTickCandidate struct {
	members  [2]string
	state    couplingApproachState
	versions [2]uint64
	routes   [2][]Lane
	owned    map[resource]resourceOwner
	claims   map[resource]bool
	ready    bool
}

func couplingSameTickCandidates(s *Simulation) []couplingSameTickCandidate {
	var result []couplingSameTickCandidate
	for _, a := range s.couplingApproaches {
		front, rear := s.findVehicle(a.context.members[0].id), s.findVehicle(a.context.members[1].id)
		c := couplingSameTickCandidate{members: [2]string{front.Pod.ID, rear.Pod.ID}, state: a.state, owned: make(map[resource]resourceOwner)}
		for i, v := range []*vehicle{front, rear} {
			c.versions[i], c.routes[i] = v.routeVersion, cloneLanes(v.Route)
		}
		for r, owner := range s.owners {
			if owner.isPod(front.Pod.ID) || owner.isPod(rear.Pod.ID) {
				c.owned[r] = owner
			}
		}
		if a.state.Phase == couplingApproachWaiting && front.follower == 0 && rear.link.leader == 0 && rear.Pod.Speed == 0 && rear.distance == a.context.rearTarget {
			if reservation, err := planCouplingReservation(a.context.formationInput(s, front, rear)); err == nil {
				c.ready, c.claims = true, make(map[resource]bool, len(reservation.Claims))
				for _, claim := range reservation.Claims {
					c.claims[claim.Resource] = true
				}
			}
		}
		result = append(result, c)
	}
	return result
}

// Two natural pairs on two corridors become ready in one tick and both
// reservations claim junction a-c. Exactly one train forms. The other
// candidate is refused at the batch's new-claim check, keeps its partner
// hold, routes, and owners, and later runs its journeys without coupling.
func TestCouplingSameTickSharedExitCandidates(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "occupied"}[occupied], func(t *testing.T) {
			t.Parallel()
			sc := couplingSameTickScenario(t)
			s := sc.start(t, true)
			requests := make(map[string]int, len(sc.trips))
			sc.request(t, s, occupied, requests)
			completed := make(map[int]bool)
			groups := make(map[string][2]string)
			var contest, refusedEnd int64 = -1, -1
			var refused [2]string
			junction := resource{kind: junctionResource, id: "a-c"}
			for s.tick < 36000 {
				candidates := couplingSameTickCandidates(s)
				s.Step()
				checkCouplingApproachNativeBoundary(t, s)
				for _, completion := range s.StepCompletions() {
					if completed[completion.RequestID] {
						t.Fatal("completion replayed", completion)
					}
					completed[completion.RequestID] = true
				}
				for _, g := range s.couplingGroups {
					id := g.context.owner.id
					if _, seen := groups[id]; !seen {
						groups[id] = [2]string{g.context.reservation.members[0].Vehicle.Pod.ID, g.context.reservation.members[1].Vehicle.Pod.ID}
					}
				}
				if contest < 0 && len(groups) != 0 {
					contest = s.tick
					if len(candidates) != 2 || !candidates[0].ready || !candidates[1].ready ||
						!candidates[0].claims[junction] || !candidates[1].claims[junction] {
						t.Fatalf("first formation at tick %d was not a same-tick contest over %s", s.tick, junction.id)
					}
					if len(s.couplingGroups) != 1 || len(groups) != 1 {
						t.Fatalf("same-tick contest formed %d trains", len(s.couplingGroups))
					}
					g := s.couplingGroups[0]
					formed := groups[g.context.owner.id]
					loser := slices.IndexFunc(candidates, func(c couplingSameTickCandidate) bool { return c.members != formed })
					if loser < 0 || s.owners[junction] != g.context.owner {
						t.Fatalf("formed train %v does not own %s against a refused candidate", formed, junction.id)
					}
					want := candidates[loser]
					refused = want.members
					if len(s.couplingApproaches) != 1 {
						t.Fatalf("refused candidate %v left the registry", refused)
					}
					a := s.couplingApproaches[0]
					front := s.findVehicle(refused[0])
					if [2]string{a.context.members[0].id, a.context.members[1].id} != refused || a.state.Phase != couplingApproachWaiting ||
						a.state.WaitTick != want.state.WaitTick || a.state.DeadlineTick != want.state.DeadlineTick ||
						front.distance != a.context.target || front.Pod.Speed != 0 {
						t.Fatalf("refused candidate %v lost its partner hold: %+v", refused, a.state)
					}
					after := couplingSameTickCandidates(s)[0]
					for i, id := range refused {
						if s.findVehicle(id).couplingID != "" || after.versions[i] != want.versions[i] || !reflect.DeepEqual(after.routes[i], want.routes[i]) {
							t.Fatalf("refused member %s changed its group, route, or route version", id)
						}
					}
					if !maps.Equal(after.owned, want.owned) {
						t.Fatalf("refused candidate %v changed its owners: before=%v after=%v", refused, want.owned, after.owned)
					}
				}
				if contest >= 0 && refusedEnd < 0 && !slices.ContainsFunc(s.couplingApproaches, func(a couplingNativeApproach) bool {
					return a.context.members[0].id == refused[0]
				}) {
					refusedEnd = s.tick
				}
				sc.request(t, s, occupied, requests)
				idle := len(requests) == len(sc.trips) && len(s.couplingGroups) == 0 && len(s.couplingApproaches) == 0
				for _, v := range s.vehicles {
					idle = idle && v.Pod.Activity == Idle
				}
				if !idle {
					continue
				}
				if contest < 0 || len(groups) != 1 || refusedEnd < 0 {
					t.Fatalf("fixture lost its same-tick contest: contest=%d groups=%v refusedEnd=%d", contest, groups, refusedEnd)
				}
				for _, trip := range sc.trips {
					if v := s.findVehicle(trip.id); v.Pod.StationID != trip.goal || occupied && !completed[requests[trip.id]] {
						t.Fatalf("journey %s did not complete at %s: %+v", trip.id, trip.goal, v.Pod)
					}
				}
				t.Logf("contest=%d groups=%v refused=%v candidateEnded=%d idle=%d", contest, groups, refused, refusedEnd, s.tick)
				return
			}
			t.Fatalf("fleet missed its idle bound: contest=%d groups=%v", contest, groups)
		})
	}
}
