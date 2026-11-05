package sim

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

// referenceBankNearest keeps bankNearest from before it reused one work
// value, with a separate search for each candidate.
func (n Network) referenceBankNearest(input preferredNearestInput, graph routeGraph) (int, bool) {
	best, seconds := -1, math.Inf(1)
	for node, rank := range input.rank {
		if rank < 0 {
			continue
		}
		from, to := input.from, n.Nodes[node].ID
		if input.reverse {
			from, to = to, from
		}
		route, err := n.routeIndexed(networkRouteInput{from: from, to: to, terminalBerthsOnly: true, class: input.class}, graph)
		if errors.Is(err, ErrUnreachable) {
			route, err = n.routeIndexed(networkRouteInput{from: from, to: to, class: input.class}, graph)
		}
		if err != nil {
			continue
		}
		cost := 0.0
		for _, lane := range route {
			cost += graph.edges[graph.lanes[lane.ID]].seconds
		}
		if input.limit > 0 && cost > input.limit {
			continue
		}
		if best < 0 || cost < seconds || cost == seconds && rank < input.rank[best] {
			best, seconds = node, cost
		}
	}
	return best, best >= 0
}

// blockedRouteSets returns the blocked sets of the route differential: none,
// the through lanes of the parking stations, which is the set of the
// no-candidate fixture, the through lanes of the stations with no banks, so
// that a route must pass a berth, and random sets of about 2 and 15 percent of
// the lanes.
func blockedRouteSets(network Network, rng *rand.Rand) [][]bool {
	sets := [][]bool{nil}
	through := make([]bool, len(network.Lanes))
	plain := make([]bool, len(network.Lanes))
	parking, banks := make(map[string]bool), make(map[string]bool)
	for _, station := range network.Stations {
		parking[station.ID], banks[station.ID] = station.ParkingOnly, station.Banks != nil
	}
	for index, lane := range network.Lanes {
		through[index] = lane.StationRole == StationThroughRole && parking[lane.StationID]
		plain[index] = lane.StationRole == StationThroughRole && !banks[lane.StationID]
	}
	sets = append(sets, through, plain)
	for _, share := range []float64{0.02, 0.15} {
		for range 3 {
			set := make([]bool, len(network.Lanes))
			for index := range set {
				set[index] = rng.Float64() < share
			}
			sets = append(sets, set)
		}
	}
	return sets
}

// berthRanks returns ranks of the berth nodes of the network: all in berth
// order, so that a tie goes to the first berth, all in reverse order, and the
// berths of the stations with banks only, so that a nearer berth of a station
// with no banks cannot hide the result of the search without the terminal-berth
// rule.
func berthRanks(network Network, graph routeGraph) [][]int {
	ranks := [][]int{make([]int, len(network.Nodes)), make([]int, len(network.Nodes)), make([]int, len(network.Nodes))}
	for _, rank := range ranks {
		for node := range rank {
			rank[node] = -1
		}
	}
	index := 0
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			node := graph.nodes[berth.Node]
			index++
			ranks[0][node], ranks[1][node] = index, math.MaxInt-index
			if station.Banks != nil {
				ranks[2][node] = index
			}
		}
	}
	return ranks
}

// TestBankNearestMatchesReference compares the node and the result of
// bankNearest with the reference, on networks with banks and with blocked
// lanes. The candidates of one call share one work value, so a value that a
// search leaves in the work would move a later result.
func TestBankNearestMatchesReference(t *testing.T) {
	t.Parallel()
	for name, network := range map[string]Network{"bank example": BankExample(), "no candidate": missNetwork(8, 5, 16)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(len(network.Nodes)), 3))
			graph := newRouteGraph(network)
			found, missed := 0, 0
			for set, blocked := range blockedRouteSets(network, rng) {
				graph.blocked = blocked
				froms := rng.Perm(len(network.Nodes))
				if len(froms) > 40 {
					froms = froms[:12]
				}
				for _, from := range froms {
					for rankIndex, rank := range berthRanks(network, graph) {
						for _, reverse := range []bool{false, true} {
							for _, limit := range []float64{0, 40} {
								input := preferredNearestInput{from: network.Nodes[from].ID, rank: rank, limit: limit, reverse: reverse}
								want, wantOK := network.referenceBankNearest(input, graph)
								got, gotOK := network.bankNearest(input, graph)
								if got != want || gotOK != wantOK {
									t.Fatalf("set %d, from %s, rank %d, reverse %t, limit %g: got %d, %t, want %d, %t", set, input.from, rankIndex, reverse, limit, got, gotOK, want, wantOK)
								}
								if wantOK {
									found++
								} else {
									missed++
								}
							}
						}
					}
				}
			}
			if found == 0 || missed == 0 {
				t.Fatalf("%d calls find a node and %d find none, want some of each", found, missed)
			}
		})
	}
}

// TestNearestFreeBerthChangesNoMemo checks that the choice of a free berth on
// a network with banks, with the blocked set of the no-candidate fixture,
// leaves the route memos, the search counters, and the route work of the
// simulation as they were.
func TestNearestFreeBerthChangesNoMemo(t *testing.T) {
	t.Parallel()
	network := missNetwork(8, 5, 16)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "m0", BerthID: "m0-1"}, {ID: "02", StationID: "m2", BerthID: "m2-1"}})
	if err != nil {
		t.Fatal(err)
	}
	var footprint faultFootprint
	for _, station := range network.Stations {
		if station.ParkingOnly {
			footprint.resources = append(footprint.resources, track(station.ID+"-through", 0))
		}
	}
	footprint.id = "i0.1"
	s.setBlocked([]faultFootprint{footprint})
	if _, err := s.route("m0-1", "m2-1"); err != nil && !errors.Is(err, ErrUnreachable) {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	before := memosOf(s)
	berth, station, ok := s.nearestFreeBerth(v, "m0-exit")
	if after := memosOf(s); !reflect.DeepEqual(after, before) {
		t.Fatal("the choice changes a memo, a counter, or the route work")
	}
	if got := fmt.Sprint(berth.ID, " ", station, " ", ok); got != "m1-1 m1 true" {
		t.Fatalf("the choice is %s", got)
	}
}
