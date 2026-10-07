package project

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func groupProjectFixture(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if decodeErr := jsonv2.Unmarshal(raw, &config, json.DefaultOptionsV1()); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return config
}

func TestGroupProjectExplicitAdmission(t *testing.T) {
	t.Parallel()
	config := groupProjectFixture(t)
	if err := Validate(config); err != nil {
		t.Fatal("authored group project rejected", err)
	}
	raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Config
	if decodeErr := jsonv2.Unmarshal(raw, &roundTrip, json.DefaultOptionsV1()); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if !reflect.DeepEqual(config, roundTrip) {
		t.Fatal("group project changed on round trip")
	}
	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"refused version", func(config *Config) { config.Version = 3 }},
		{"default station", func(config *Config) { config.Network.Stations[0].VehicleClasses = 0 }},
		{"default berth", func(config *Config) { config.Network.Stations[0].Berths[0].VehicleClasses = 0 }},
		{"express unsupported", func(config *Config) { config.Fleet[0].Class = sim.ExpressClass }},
		{"large short lane", func(config *Config) {
			config.Network.Nodes = append(config.Network.Nodes, sim.Node{ID: "short-a", Position: sim.Point{X: 2000, Y: 2000}}, sim.Node{ID: "short-b", Position: sim.Point{X: 2039.999, Y: 2000}})
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: "short", From: "short-a", To: "short-b", SpeedLimit: 14, VehicleClasses: config.Network.Lanes[0].VehicleClasses})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := Clone(config)
			test.mutate(&candidate)
			if err := Validate(candidate); err == nil {
				t.Fatal("invalid authored group project accepted")
			}
		})
	}
}
