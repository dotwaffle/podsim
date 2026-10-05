package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestBoardingTopologyCacheRollbackAndInvalidation(t *testing.T) {
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	frame := session.StreamFrame{State: shared.Frame()}
	frame.Routes = make([]sim.RoutePresentation, len(frame.State.Simulation.Vehicles))
	for i := range frame.Routes {
		frame.Routes[i].Origin = -1
	}
	v := &frame.State.Simulation.Vehicles[0]
	v.Riders = []sim.Request{{ID: 1, From: topology.Network.Stations[0].ID, To: topology.Network.Stations[1].ID, PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}}
	berth := topology.Network.Stations[0].Berths[0].ID
	v.Boardings = []sim.RiderBoarding{{BerthID: berth}}
	var fetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fetches++; _ = json.NewEncoder(w).Encode(topology) }))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client()}
	cache := streamTopology{}
	accepted, err := cache.state(t.Context(), client, frame)
	if err != nil {
		t.Fatal(err)
	}
	first := cache.assembler
	topology.ProjectRevision++
	frame.State.ProjectRevision++
	v.Boardings[0].BerthID = "missing"
	if _, err := cache.state(t.Context(), client, frame); err == nil {
		t.Fatal("accepted invalid new topology candidate")
	}
	if cache.assembler != first || cache.topology.ProjectRevision == topology.ProjectRevision {
		t.Fatal("rejection published cache binding")
	}
	if accepted.Simulation.Vehicles[0].Boardings[0].BerthID != berth {
		t.Fatal("candidate changed accepted state")
	}
	v.Boardings[0].BerthID = berth
	v.Pod.Class = sim.CompactClass
	if _, err := cache.state(t.Context(), client, frame); err != nil {
		t.Fatal("new revision retained old class binding", err)
	}
	if cache.assembler == first {
		t.Fatal("new revision retained old assembler")
	}
	reconnected := streamTopology{}
	v.Pod.Class = sim.LegacyClass
	if _, err := reconnected.state(t.Context(), client, frame); err != nil {
		t.Fatal("reconnect retained old class binding", err)
	}
	if fetches != 4 {
		t.Fatalf("topology fetches %d, want 4", fetches)
	}
}
