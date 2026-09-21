package sim

import (
	"errors"
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
