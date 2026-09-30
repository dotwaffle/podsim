package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestExperimentalPolicyJSON(t *testing.T) {
	t.Parallel()
	for _, codec := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{
		{name: "legacy", marshal: json.Marshal, unmarshal: json.Unmarshal},
		{name: "v2", marshal: func(value any) ([]byte, error) { return jsonv2.Marshal(value) }, unmarshal: func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }},
	} {
		t.Run(codec.name, func(t *testing.T) {
			t.Parallel()
			for _, field := range []string{"stationBuffers", "pickupReassignment"} {
				for _, value := range []string{"null", "0", "1", `"true"`, `"false"`, "[]", "{}"} {
					var config Config
					if err := codec.unmarshal(fmt.Appendf(nil, `{"%s":%s}`, field, value), &config); err == nil {
						t.Fatalf("accepted %s=%s", field, value)
					}
				}
			}
			for _, enabled := range []bool{false, true} {
				config := Default()
				config.StationBuffers, config.PickupReassignment = PolicyFlag(enabled), PolicyFlag(enabled)
				data, err := codec.marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"stationBuffers", "pickupReassignment"} {
					if bytes.Contains(data, []byte(field)) != enabled {
						t.Fatalf("wrong optional field %s in export", field)
					}
				}
				var restored Config
				if err := codec.unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				if bool(restored.StationBuffers) != enabled || bool(restored.PickupReassignment) != enabled {
					t.Fatal("project round trip changed experimental flags")
				}
				if err := Validate(restored); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestConfigureExperiments(t *testing.T) {
	t.Parallel()
	config := Default()
	simulation, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	config.StationBuffers, config.PickupReassignment = true, true
	ConfigureExperiments(simulation, config)
	if !simulation.NeedsBufferState() {
		t.Fatal("enabled buffers did not require version 3 state")
	}
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if simulation.PickupSwapStats().AssignmentChecks == 0 {
		t.Fatal("enabled reassignment did not check a remote pickup")
	}
	simulation.Reset()
	config.StationBuffers, config.PickupReassignment = false, false
	ConfigureExperiments(simulation, config)
	if simulation.NeedsBufferState() {
		t.Fatal("disabled buffers retained enablement")
	}
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if simulation.PickupSwapStats() != (sim.PickupSwapStats{}) {
		t.Fatal("disabled reassignment checked a new pickup")
	}
}
