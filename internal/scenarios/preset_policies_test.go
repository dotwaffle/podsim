package scenarios

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPresetPolicyDefaults pins the service policies of each preset:
// redistribution and pickup reassignment on, virtual platoons on the London
// presets only, and sharing and onboard pickups off.
func TestPresetPolicyDefaults(t *testing.T) {
	t.Parallel()
	custom := func(name string) func() (project.Config, error) {
		return func() (project.Config, error) {
			return PresetWith(name, func(parameters *Parameters) { parameters.PassengerBerths-- })
		}
	}
	plain := func(build func() project.Config) func() (project.Config, error) {
		return func() (project.Config, error) { return build(), nil }
	}
	londonCentral := func() (project.Config, error) { return LondonCentralWith(DefaultLondonCentralOptions()) }
	londonFull := func() (project.Config, error) { return LondonFullWith(DefaultLondonFullOptions()) }
	tests := []struct {
		name         string
		config       func() (project.Config, error)
		platoonLimit int
	}{
		{"small", plain(Small), 0},
		{"busy", plain(Busy), 0},
		{"parking-constrained", plain(ParkingConstrained), 0},
		{"rail-hub", plain(RailHub), 0},
		{"rail-hub-incidents", plain(RailHubIncidents), 0},
		{"scale100", plain(Scale100), 0},
		{"compact-station", CompactStation, 0},
		{"custom small", custom("small"), 0},
		{"custom scale100", custom("scale100"), 0},
		{"london-central", plain(LondonCentral), sim.MaxPlatoonLimit},
		{"london-full", plain(LondonFull), sim.MaxPlatoonLimit},
		{"london-central options", londonCentral, sim.MaxPlatoonLimit},
		{"london-full options", londonFull, sim.MaxPlatoonLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, err := test.config()
			if err != nil {
				t.Fatal(err)
			}
			if !config.Redistribution {
				t.Error("redistribution is off")
			}
			if !config.PickupReassignment {
				t.Error("pickup reassignment is off")
			}
			if config.PlatoonLimit != test.platoonLimit {
				t.Errorf("platoon limit = %d, want %d", config.PlatoonLimit, test.platoonLimit)
			}
			if config.SharedRidePartyLimit > 1 {
				t.Errorf("party limit = %d, want at most 1", config.SharedRidePartyLimit)
			}
			if config.GeneratedOrderConsent != "" {
				t.Errorf("generated order consent = %q, want none", config.GeneratedOrderConsent)
			}
			if config.OnboardPickups {
				t.Error("onboard pickups are on")
			}
		})
	}
}

// TestConfigKeepsParameterPolicies pins that a generated ring has the policies
// of its parameters, so that a fixture with no policy stays with none.
func TestConfigKeepsParameterPolicies(t *testing.T) {
	t.Parallel()
	parameters := smallParameters()
	parameters.Redistribution, parameters.PickupReassignment = false, false
	config, err := Config(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if config.Redistribution || bool(config.PickupReassignment) {
		t.Fatalf("redistribution %t, pickup reassignment %t, want off", config.Redistribution, config.PickupReassignment)
	}
}
