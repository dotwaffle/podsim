package project

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func expressProject(t *testing.T) Config {
	t.Helper()
	config := Default()
	config.Version = ExpressVersion
	config.OrderContract = sim.ExpressOrderContract
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Nodes {
		config.Network.Nodes[i].Position.X *= 4
		config.Network.Nodes[i].Position.Y *= 4
	}
	for i := range config.Network.Lanes {
		lane := &config.Network.Lanes[i]
		lane.VehicleClasses = classes
		if lane.Control != nil {
			lane.Control = &sim.Point{X: lane.Control.X * 4, Y: lane.Control.Y * 4}
		}
	}
	for i := range config.Network.Stations {
		station := &config.Network.Stations[i]
		station.VehicleClasses = classes
		for j := range station.Berths {
			station.Berths[j].VehicleClasses = classes
		}
	}
	config.Fleet = []sim.Placement{{ID: "express", Class: sim.ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
	config.ExpressServices = []sim.ExpressService{{ID: "pair", From: "harbor", To: "market", Class: sim.ExpressClass, PartyLimit: 20}}
	return config
}

func TestExpressProjectRoundTrip(t *testing.T) {
	config := expressProject(t)
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, got) {
		t.Fatal("project4 changed authored facts")
	}
	if err = Validate(got); err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.NewFleetWithOrderContract(got.Network, got.Fleet, got.OrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureSharedRides(simulation, got); err != nil {
		t.Fatal(err)
	}
	if _, err := simulation.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", PartySize: 20}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"orderContract":"express-v1"`)) {
		t.Fatal("marker omitted")
	}
}

func TestExpressProjectMarkersFailAtomically(t *testing.T) {
	for _, raw := range []string{`{"version":4}`, `{"version":4,"orderContract":null}`, `{"version":4,"orderContract":""}`, `{"version":4,"orderContract":"unknown"}`, `{"version":3,"orderContract":"express-v1"}`, `{"version":2,"orderContract":"express-v1"}`, `{"version":1,"orderContract":"express-v1"}`, `{"version":4,"orderContract":"express-v1","orderContract":"express-v1"}`} {
		got := Default()
		before := Clone(got)
		if err := json.Unmarshal([]byte(raw), &got); err == nil {
			t.Fatal("invalid marker accepted", raw)
		}
		if !reflect.DeepEqual(before, got) {
			t.Fatal("failed decode mutated project")
		}
	}
	for _, version := range []int{1, 2, 3, 4} {
		config := expressProject(t)
		config.Version = version
		if version == 4 {
			config.OrderContract = ""
		}
		if err := Validate(config); err == nil {
			t.Fatal("version/contract mismatch", version)
		}
	}
	raw, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("orderContract")) {
		t.Fatal("foundation field appeared")
	}
}
