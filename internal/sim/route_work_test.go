package sim

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
)

func TestRouteWorkMatchesOriginalSearch(t *testing.T) {
	t.Parallel()
	var work routeSearchWork
	for _, network := range []Network{gridNetwork(6), Example(), berthGuardNetwork(), gridNetwork(8)} {
		graph := newRouteGraph(network)
		ids := []string{"missing"}
		for _, node := range network.Nodes {
			ids = append(ids, node.ID)
		}
		for _, costs := range routeSearchCosts(network) {
			for _, guard := range []bool{false, true} {
				for _, forbidden := range []map[string]bool{nil, network.stationForbidden()} {
					for _, from := range ids {
						for _, to := range ids {
							input := networkRouteInput{from: from, to: to, forbidden: forbidden,
								extraCost: costs, discharge: costs, ownBerthsOnly: guard}
							want, wantErr := network.referenceRouteBeforeWork(input, graph)
							got, gotErr := network.routeIndexedWithWork(input, graph, &work)
							if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
								t.Fatalf("from%s to%s guard%v: got%v,%v want%v,%v", from, to, guard, got, gotErr, want, wantErr)
							}
						}
					}
				}
			}
		}
	}
}

func TestRouteWorkPreservesReturnedRoutes(t *testing.T) {
	t.Parallel()
	s := &Simulation{networkIndexes: &networkIndexes{network: gridNetwork(8)}}
	s.ensureNetworkIndexes()
	first, err := s.route("n-0-0", "n-7-7")
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(first)
	work := s.routeWork
	distanceStorage := &work.distance[0]
	queueStorage := &work.queue[:cap(work.queue)][0]
	for _, input := range []networkRouteInput{
		{from: "n-7-7", to: "n-0-0"},
		{from: "n-0-0", to: "n-1-0"},
		{from: "missing", to: "n-0-0"},
		{from: "n-0-0", to: "missing"},
		{from: "n-0-0", to: "n-0-0"},
	} {
		s.searchRoute(input)
	}
	if !slices.Equal(first, want) || s.routeWork != work ||
		&work.distance[0] != distanceStorage || &work.queue[:cap(work.queue)][0] != queueStorage {
		t.Fatal("another search changed a cached result or replaced its work arrays")
	}
	cached, err := s.route("n-0-0", "n-7-7")
	if err != nil || !slices.Equal(cached, want) {
		t.Fatal("cached route changed after work-array reuse")
	}
	clone := s.Clone()
	if clone.routeWork != nil {
		t.Fatal("Clone retained mutable search storage")
	}
	if _, err := clone.route("n-0-0", "n-7-7"); err != nil {
		t.Fatal(err)
	}
	if clone.routeWork == work || &clone.routeWork.distance[0] == &work.distance[0] {
		t.Fatal("Clone reused its source's search storage")
	}
	clone.routeWork.distance[0] = -1
	if work.distance[0] == -1 {
		t.Fatal("clone search changed source storage")
	}
}

func TestRouteWorkValidatesBeforeArrayAllocation(t *testing.T) {
	t.Parallel()
	network := Example()
	graph := newRouteGraph(network)
	var work routeSearchWork
	for _, input := range []networkRouteInput{{from: "missing", to: "market-entry"}, {from: "market-entry", to: "missing"}} {
		if _, err := network.routeIndexedWithWork(input, graph, &work); err == nil {
			t.Fatal("unknown endpoint succeeded")
		}
		if work.distance != nil || work.previous != nil || work.visited != nil || work.queue != nil {
			t.Fatal("invalid endpoints allocated search arrays")
		}
	}
}

func TestRouteWorkResetAndRebuild(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if _, err := s.route("market-entry", "harbor-entry"); err != nil {
		t.Fatal(err)
	}
	work := s.routeWork
	s.Reset()
	if s.routeWork != work {
		t.Fatal("Reset discarded fixed-network search storage")
	}
	detachIndexes(s)
	s.network.Nodes = append(slices.Clone(s.network.Nodes), Node{ID: "disconnected"})
	s.ensureNetworkIndexes()
	if s.routeWork != nil {
		t.Fatal("graph rebuild retained search storage")
	}
	if _, err := s.route("market-entry", "disconnected"); err == nil || len(s.routeWork.distance) != len(s.network.Nodes) {
		t.Fatal("rebuilt search did not reflect the disconnected node")
	}
}

func TestRouteWorkPreparedFleetIsolation(t *testing.T) {
	t.Parallel()
	prepared, err := PrepareNetwork(Example())
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepared.NewFleet([]Placement{{ID: "one", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepared.NewFleet([]Placement{{ID: "two", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for _, s := range []*Simulation{first, second} {
		workers.Go(func() {
			for range 50 {
				if _, err := s.searchRoute(networkRouteInput{from: "market-entry", to: "harbor-entry"}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	if first.routeWork == second.routeWork || &first.routeWork.distance[0] == &second.routeWork.distance[0] {
		t.Fatal("prepared fleets shared mutable search storage")
	}
}

func BenchmarkRouteSearchWork(b *testing.B) {
	network := gridNetwork(20)
	graph := newRouteGraph(network)
	input := networkRouteInput{from: "n-0-0", to: "n-19-19"}
	for _, reuse := range []bool{false, true} {
		name := "fresh"
		if reuse {
			name = "reused"
		}
		b.Run(name, func(b *testing.B) {
			var work routeSearchWork
			network.routeIndexedWithWork(input, graph, &work)
			b.ReportAllocs()
			for b.Loop() {
				if reuse {
					network.routeIndexedWithWork(input, graph, &work)
				} else {
					network.routeIndexed(input, graph)
				}
			}
		})
	}
}
