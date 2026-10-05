package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestGroupTopologyCacheRollbackAndInvalidation(t *testing.T) {
	raw, err := os.ReadFile("../project/testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if decodeErr := json.Unmarshal(raw, &config); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	shared, err := session.NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	frame := session.StreamFrame{State: shared.Frame(), Routes: []sim.RoutePresentation{{Origin: -1}}}
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
	frame.State.ProjectRevision++
	topology.ProjectRevision++
	frame.State.Simulation.Vehicles[0].Pod.BerthID = topology.Network.Stations[1].Berths[0].ID
	if _, err := cache.state(t.Context(), client, frame); err == nil {
		t.Fatal("foreign group berth accepted")
	}
	if cache.assembler != first || cache.topology.ProjectRevision == topology.ProjectRevision {
		t.Fatal("rejected group candidate retained topology")
	}
	if accepted.Simulation.Vehicles[0].Pod.BerthID != config.Fleet[0].BerthID {
		t.Fatal("rejected candidate changed accepted group state")
	}
	frame.State.Simulation.Vehicles[0].Pod.BerthID = config.Fleet[0].BerthID
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.LegacyClass
	if _, err := cache.state(t.Context(), client, frame); err != nil {
		t.Fatal("new revision retained rejected class binding", err)
	}
	if cache.assembler == first {
		t.Fatal("new revision kept old assembler")
	}
	reconnected := streamTopology{}
	frame.State.Simulation.Vehicles[0].Pod.Class = sim.GroupClass
	if _, err := reconnected.state(t.Context(), client, frame); err != nil {
		t.Fatal("reconnect retained prior class binding", err)
	}
	if fetches != 4 {
		t.Fatalf("topology fetches = %d, want 4", fetches)
	}
}
