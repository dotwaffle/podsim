package project

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestConfigJSONRoundTripAndClone(t *testing.T) {
	t.Parallel()
	want := Default()
	want.Redistribution = true
	want.Demand = DemandConfig{Enabled: true, PerMinute: 30, Pattern: "destination", Seed: 9, Destination: "garden"}
	want.Network.Lanes[0].SeparationGroup = "surface"
	want.Network.Stations[0].Berths[0].SeparationGroup = "surface"
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed config\n got: %#v\nwant: %#v", got, want)
	}
	clone := Clone(want)
	clone.Network.Nodes[0].ID = "changed"
	clone.Network.Stations[0].Berths[0].ID = "changed"
	clone.Fleet[0].ID = "changed"
	if strings.Contains(string(mustJSON(t, want)), "changed") {
		t.Fatal("clone aliases config storage")
	}
}

func TestValidateRejectsMalformedProjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"version", func(config *Config) { config.Version = 2 }},
		{"name", func(config *Config) { config.Name = "" }},
		{"long name", func(config *Config) { config.Name = strings.Repeat("x", maxNameLength+1) }},
		{"long node ID", func(config *Config) { config.Network.Nodes[0].ID = strings.Repeat("x", maxIDLength+1) }},
		{"long lane separation group", func(config *Config) { config.Network.Lanes[0].SeparationGroup = strings.Repeat("x", maxIDLength+1) }},
		{"long berth separation group", func(config *Config) {
			config.Network.Stations[0].Berths[0].SeparationGroup = strings.Repeat("x", maxIDLength+1)
		}},
		{"long station name", func(config *Config) { config.Network.Stations[0].Name = strings.Repeat("x", maxNameLength+1) }},
		{"berth bound", func(config *Config) { config.Network.Stations[3].Berths = make([]sim.Berth, maxBerths+1) }},
		{"nan", func(config *Config) { config.Network.Nodes[0].Position.X = math.NaN() }},
		{"duplicate node", func(config *Config) { config.Network.Nodes[1].ID = config.Network.Nodes[0].ID }},
		{"duplicate lane", func(config *Config) { config.Network.Lanes[1].ID = config.Network.Lanes[0].ID }},
		{"duplicate station", func(config *Config) { config.Network.Stations[1].ID = config.Network.Stations[0].ID }},
		{"duplicate pod", func(config *Config) { config.Fleet[1].ID = config.Fleet[0].ID }},
		{"occupied berth", func(config *Config) { config.Fleet[1].StationID = config.Fleet[0].StationID }},
		{"rate low", func(config *Config) { config.Demand.PerMinute = 0 }},
		{"rate high", func(config *Config) { config.Demand.PerMinute = 121 }},
		{"pattern", func(config *Config) { config.Demand.Pattern = "rush" }},
		{"parking destination", func(config *Config) {
			config.Demand.Pattern, config.Demand.Destination = "destination", "parking"
		}},
		{"unreachable", func(config *Config) { config.Network.Lanes = config.Network.Lanes[:3] }},
		{"pod bound", func(config *Config) {
			config.Fleet = make([]sim.Placement, maxPods+1)
		}},
		{"station bound", func(config *Config) {
			config.Network.Stations = make([]sim.Station, maxStations+1)
		}},
		{"node bound", func(config *Config) { config.Network.Nodes = make([]sim.Node, maxNodes+1) }},
		{"lane bound", func(config *Config) { config.Network.Lanes = make([]sim.Lane, maxLanes+1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			test.change(&config)
			if err := Validate(config); err == nil {
				t.Fatal("accepted malformed project")
			}
		})
	}
}

func TestValidateRequiresTwoReachablePassengerStations(t *testing.T) {
	t.Parallel()
	config := Default()
	for i := range config.Network.Stations[:3] {
		config.Network.Stations[i].ParkingOnly = true
	}
	if err := Validate(config); err == nil {
		t.Fatal("accepted one passenger station")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
