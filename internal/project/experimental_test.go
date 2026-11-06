package project

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
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
	ConfigureExperiments(simulation, config, false)
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if simulation.PickupSwapStats().AssignmentChecks == 0 {
		t.Fatal("enabled reassignment did not check a remote pickup")
	}
	simulation.Reset()
	config.PickupReassignment = false
	ConfigureExperiments(simulation, config, false)
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if simulation.PickupSwapStats() != (sim.PickupSwapStats{}) {
		t.Fatal("disabled reassignment checked a new pickup")
	}
}

// TestDefaultPolicies pins the example project: redistribution and pickup
// reassignment on, and sharing, platoons, and onboard pickups off. A project
// file that omits the two members has both off.
func TestDefaultPolicies(t *testing.T) {
	t.Parallel()
	config := Default()
	if !config.Redistribution || !bool(config.PickupReassignment) {
		t.Fatalf("redistribution %t, pickup reassignment %t, want on", config.Redistribution, config.PickupReassignment)
	}
	if config.SharedRidePartyLimit != 1 || config.PlatoonLimit != 0 || config.OnboardPickups || config.GeneratedOrderConsent != "" {
		t.Fatalf("party limit %d, platoon limit %d, onboard %t, consent %q", config.SharedRidePartyLimit, config.PlatoonLimit, config.OnboardPickups, config.GeneratedOrderConsent)
	}
	raw, err := jsonv2.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]jsontext.Value
	if err = jsonv2.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"redistribution", "pickupReassignment"} {
		if _, ok := members[name]; !ok {
			t.Fatalf("the encoded project has no %s member", name)
		}
		delete(members, name)
	}
	raw, err = jsonv2.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	var omitted Config
	if err = jsonv2.Unmarshal(raw, &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.Redistribution || bool(omitted.PickupReassignment) {
		t.Fatalf("omitted members give redistribution %t, pickup reassignment %t, want off", omitted.Redistribution, omitted.PickupReassignment)
	}
}
