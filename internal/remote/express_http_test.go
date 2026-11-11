package remote

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func remoteExpressProject(t *testing.T) project.Config {
	t.Helper()
	raw, err := os.ReadFile("../project/testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err = jsonv2.Unmarshal(raw, &config, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	config.OrderContract = sim.ExpressOrderContract
	classes, err := sim.NewClassSet("group", "express")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Lanes {
		config.Network.Lanes[i].VehicleClasses = classes
	}
	for i := range config.Network.Stations {
		station := &config.Network.Stations[i]
		station.VehicleClasses = classes
		for j := range station.Berths {
			station.Berths[j].VehicleClasses = classes
		}
	}
	config.Fleet[0].Class = sim.ExpressClass
	return config
}

func TestExpressRemoteHTTPAndTripMarker(t *testing.T) {
	shared, err := session.NewWithProject(remoteExpressProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	server := httptest.NewServer(shared.Handler("missing"))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client(), commands: make(chan session.Command, 1), connected: true}
	state := httpState(t, server.Client(), server.URL)
	if state.Simulation.OrderContract != sim.ExpressOrderContract {
		t.Fatal("HTTP lost contract")
	}
	client.state = state
	if err = client.Submit(session.Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 20}); err != nil {
		t.Fatal(err)
	}
	command := <-client.commands
	if command.OrderContract != sim.ExpressOrderContract || command.Epoch != state.Epoch {
		t.Fatal("trip lost explicit contract or source epoch")
	}
}
