package sim

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
)

// TestBankRoutesFromMatchesReference compares each route of bankRoutesFrom
// with the frozen route of the same search, with the terminal-berth rule first
// and the fallback without it, as cachedRouteForClass searches. The targets of
// one call are the berths of one station, and the routes of a call share their
// prefix and middle searches, so a part that a route gives to a later route
// with a different result would show here.
func TestBankRoutesFromMatchesReference(t *testing.T) {
	t.Parallel()
	for name, network := range routeReferenceNetworks(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(uint64(len(network.Nodes)), 11))
			graph := newRouteGraph(network)
			if graph.banks.err != nil {
				t.Fatal(graph.banks.err)
			}
			routes, failed := 0, 0
			for set, blocked := range append(blockedRouteSets(network, rng), tieBlockedSets(network)...) {
				graph.blocked = blocked
				froms := rng.Perm(len(network.Nodes))
				if len(froms) > 40 {
					froms = froms[:12]
				}
				for _, from := range froms {
					for _, station := range network.Stations {
						targets := make([]int, 0, len(station.Berths))
						for _, berth := range station.Berths {
							targets = append(targets, graph.nodes[berth.Node])
						}
						for _, class := range []VehicleClass{"", GroupClass, ExpressClass} {
							got := network.bankRoutesFrom(networkRouteInput{class: class}, from, targets, graph)
							for index, to := range targets {
								input := networkRouteInput{from: network.Nodes[from].ID, to: network.Nodes[to].ID, class: class, terminalBerthsOnly: true}
								var counts frozenCounts
								want, wantErr := network.frozenRoute(input, graph, &counts)
								if errors.Is(wantErr, ErrUnreachable) {
									input.terminalBerthsOnly = false
									want, wantErr = network.frozenRoute(input, graph, &counts)
								}
								if !reflect.DeepEqual(got[index].lanes, want) || fmt.Sprint(got[index].err) != fmt.Sprint(wantErr) {
									t.Fatalf("set %d, %s to %s, class %q: got %v, %v, want %v, %v", set, input.from, input.to, class, got[index].lanes, got[index].err, want, wantErr)
								}
								if wantErr != nil {
									failed++
								} else {
									routes++
								}
							}
						}
					}
				}
			}
			if routes == 0 || failed == 0 {
				t.Fatalf("%d routes and %d failures, want some of each", routes, failed)
			}
		})
	}
}
