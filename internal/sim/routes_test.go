package sim

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestCachedRoutesMatchNetwork(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range simulation.network.Nodes {
		for _, to := range simulation.network.Nodes {
			want, wantErr := simulation.network.Route(from.ID, to.ID)
			for range 2 {
				got, gotErr := simulation.route(from.ID, to.ID)
				if !reflect.DeepEqual(got, want) || !errors.Is(gotErr, wantErr) {
					t.Fatalf("route %s to %s differs: %v", from.ID, to.ID, gotErr)
				}
			}
		}
	}
	simulation.Reset()
	if err := simulation.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	snapshot := simulation.Snapshot()
	snapshot.Vehicles[0].Route[0].To = "changed"
	state := simulation.Snapshot()
	if state.Vehicles[0].Route[0].To == "changed" {
		t.Fatal("snapshot changed cached route")
	}
}

func TestRouteCacheCapacity(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	simulation.routes = make(map[routeKey]routeResult)
	for index := range routeCacheLimit {
		simulation.routes[routeKey{from: string(rune(index)), to: "unused"}] = routeResult{}
	}
	want, wantErr := simulation.network.Route("harbor-berth", "market-berth")
	got, gotErr := simulation.route("harbor-berth", "market-berth")
	if !reflect.DeepEqual(got, want) || !errors.Is(gotErr, wantErr) {
		t.Fatal("eviction changed route")
	}
	if len(simulation.routes) > routeCacheLimit {
		t.Fatal("route cache exceeded limit")
	}
}

func TestRouteCacheEvictsOldestEntry(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	simulation.routes = make(map[routeKey]routeResult)
	for index := range routeCacheLimit {
		key := routeKey{from: fmt.Sprintf("from-%d", index), to: "destination"}
		simulation.cacheRoute(key, routeResult{})
	}
	oldest := routeKey{from: "from-0", to: "destination"}
	newest := routeKey{from: "new", to: "destination"}
	simulation.cacheRoute(newest, routeResult{})
	if _, exists := simulation.routes[oldest]; exists {
		t.Fatal("oldest route remained after capacity eviction")
	}
	if _, exists := simulation.routes[newest]; !exists {
		t.Fatal("new route missing after capacity eviction")
	}
	if len(simulation.routes) != routeCacheLimit {
		t.Fatalf("route cache size = %d, want %d", len(simulation.routes), routeCacheLimit)
	}
}

func TestStationRoutesFillRouteCache(t *testing.T) {
	t.Parallel()
	for _, congestion := range []bool{false, true} {
		simulation, err := New(ladderNetwork(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		simulation.SetCongestionRouting(congestion)
		filled := 0
		for _, node := range simulation.network.Nodes {
			for _, station := range simulation.network.Stations {
				simulation.cacheStationRoutes(node.ID, station.Berths)
				for _, berth := range station.Berths {
					cached, ok := simulation.routes[routeKey{from: node.ID, to: berth.Node}]
					if !ok {
						continue
					}
					want, wantErr := simulation.network.Route(node.ID, berth.Node)
					if !reflect.DeepEqual(cached.lanes, want) || fmt.Sprint(cached.err) != fmt.Sprint(wantErr) {
						t.Fatalf("cached route %s to %s = %v, %v, want %v, %v", node.ID, berth.Node, cached.lanes, cached.err, want, wantErr)
					}
					filled++
				}
			}
		}
		if congestion && (filled > 0 || simulation.congestionRouteCosts != nil) {
			t.Fatalf("congestion routing filled %d routes or refreshed its costs", filled)
		}
		if !congestion && filled == 0 {
			t.Fatal("no station route in the cache")
		}
	}
}
