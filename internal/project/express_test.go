package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func expressProject(t *testing.T) Config {
	t.Helper()
	config := Default()
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
	raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err = jsonv2.Unmarshal(raw, &got, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, got) {
		t.Fatal("Express project changed authored facts")
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
	for _, raw := range []string{`{"version":1,"orderContract":null}`, `{"version":1,"orderContract":""}`, `{"version":1,"orderContract":"unknown"}`, `{"version":4,"orderContract":"express-v1"}`, `{"version":3,"orderContract":"express-v1"}`, `{"version":2,"orderContract":"express-v1"}`, `{"version":1,"orderContract":"express-v1","orderContract":"express-v1"}`} {
		got := Default()
		before := Clone(got)
		if err := jsonv2.Unmarshal([]byte(raw), &got, json.DefaultOptionsV1()); err == nil {
			t.Fatal("invalid marker accepted", raw)
		}
		if !reflect.DeepEqual(before, got) {
			t.Fatal("failed decode mutated project")
		}
	}
	for _, version := range []int{2, 3, 4, 5} {
		config := expressProject(t)
		config.Version = version
		if err := Validate(config); err == nil {
			t.Fatal("refused project version accepted", version)
		}
	}
	unmarked := expressProject(t)
	unmarked.OrderContract = ""
	if err := Validate(unmarked); err == nil {
		t.Fatal("Express features accepted without the order contract")
	}
	raw, err := jsonv2.Marshal(Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("orderContract")) {
		t.Fatal("foundation field appeared")
	}
}
