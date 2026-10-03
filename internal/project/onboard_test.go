package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestOnboardProjectPresence(t *testing.T) {
	for _, decoder := range []struct {
		name   string
		decode func([]byte, any) error
	}{
		{"v1", json.Unmarshal}, {"v2", func(raw []byte, value any) error { return jsonv2.Unmarshal(raw, value) }},
	} {
		for _, version := range []int{1, 2, 3} {
			for _, value := range []string{`true`, `false`, `null`, `0`, `"true"`, `{}`} {
				t.Run(fmt.Sprintf("%s/version%d/%s", decoder.name, version, value), func(t *testing.T) {
					config := Default()
					before := Clone(config)
					raw := fmt.Appendf(nil, `{"version":%d,"onboardPickups":%s}`, version, value)
					err := decoder.decode(raw, &config)
					valid := version == ServiceVersion && (value == "true" || value == "false")
					if (err == nil) != valid {
						t.Fatalf("presence validation: %v", err)
					}
					if !valid && !reflect.DeepEqual(config, before) {
						t.Fatal("rejected project changed previous storage")
					}
					if valid && config.OnboardPickups != (value == "true") {
						t.Fatal("decoded policy changed")
					}
				})
			}
		}
	}
}

func TestOnboardProjectValidation(t *testing.T) {
	for _, test := range []struct {
		name           string
		version, limit int
		mode           sim.SharedRideMode
		enabled, valid bool
	}{
		{"legacy-default", 1, 1, "", false, true},
		{"service-off", 3, 1, sim.SharedRideDestination, false, true},
		{"default-sharing-mode", 3, 2, "", true, true},
		{"drop-offs", 3, 8, sim.SharedRideDropOffs, true, true},
		{"legacy-enabled", 1, 2, sim.SharedRideDropOffs, true, false},
		{"sharing-one", 3, 1, sim.SharedRideDropOffs, true, false},
		{"sharing-omitted", 3, 0, sim.SharedRideDropOffs, true, false},
		{"destination", 3, 2, sim.SharedRideDestination, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
			config.Version = test.version
			config.SharedRidePartyLimit = test.limit
			config.SharedRideMode = test.mode
			config.OnboardPickups = test.enabled
			if err := Validate(config); (err == nil) != test.valid {
				t.Fatalf("project validation: %v", err)
			}
		})
	}
}

func TestOnboardProjectConfigurationOrder(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit int
		mode  sim.SharedRideMode
		valid bool
	}{
		{"drop-offs", 2, sim.SharedRideDropOffs, true}, {"sharing-one", 1, sim.SharedRideDropOffs, false}, {"destination", 2, sim.SharedRideDestination, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := Default()
			config.Version = ServiceVersion
			config.OnboardPickups = true
			config.SharedRidePartyLimit = test.limit
			config.SharedRideMode = test.mode
			simulation, err := sim.NewFleet(config.Network, config.Fleet)
			if err != nil {
				t.Fatal(err)
			}
			if configureErr := ConfigureSharedRides(simulation, config); (configureErr == nil) != test.valid {
				t.Fatalf("runtime policy configuration: %v", configureErr)
			}
		})
	}
}

func TestOnboardProjectCanonicalOmission(t *testing.T) {
	config := Default()
	raw, err := jsonv2.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("onboardPickups")) {
		t.Fatal("default project exports opt-in")
	}
	config.Version = ServiceVersion
	config.SharedRidePartyLimit = 2
	config.OnboardPickups = true
	raw, err = jsonv2.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"onboardPickups":true`)) {
		t.Fatal("enabled project omitted policy")
	}
	var decoded Config
	if decodeErr := jsonv2.Unmarshal(raw, &decoded); decodeErr != nil || !reflect.DeepEqual(config, decoded) {
		t.Fatalf("policy round trip: %v", decodeErr)
	}
}
