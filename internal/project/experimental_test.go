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
			for _, value := range []string{"null", "0", "1", `"true"`, `"false"`, "[]", "{}"} {
				var config Config
				if err := codec.unmarshal(fmt.Appendf(nil, `{"pickupReassignment":%s}`, value), &config); err == nil {
					t.Fatalf("accepted pickupReassignment=%s", value)
				}
			}
			for _, enabled := range []bool{false, true} {
				config := Default()
				config.PickupReassignment = PolicyFlag(enabled)
				data, err := codec.marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("pickupReassignment")) != enabled {
					t.Fatal("wrong optional field pickupReassignment in export")
				}
				var restored Config
				if err := codec.unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				if bool(restored.PickupReassignment) != enabled {
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
	config.PickupReassignment = true
	ConfigureExperiments(simulation, config)
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if simulation.PickupSwapStats().AssignmentChecks == 0 {
		t.Fatal("enabled reassignment did not check a remote pickup")
	}
	simulation.Reset()
	config.PickupReassignment = false
	ConfigureExperiments(simulation, config)
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if simulation.PickupSwapStats() != (sim.PickupSwapStats{}) {
		t.Fatal("disabled reassignment checked a new pickup")
	}
}
