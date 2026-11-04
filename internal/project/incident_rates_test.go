package project

import (
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestIncidentSeedsGolden pins the PCG arguments and the first eight raw draws
// of each sampler for the demand seed 1. The values come from a separate
// SHA-256 and PCG implementation. They prove the seed rule and both byte
// orders (rules G4 and G5 of the incident stage 7 contract).
func TestIncidentSeedsGolden(t *testing.T) {
	t.Parallel()
	faults, emergencies := incidentSeeds(1)
	for _, test := range []struct {
		name  string
		seed  [2]uint64
		want  [2]uint64
		draws []uint64
	}{
		{"faults", faults, [2]uint64{0x6b183e09ae9d53df, 0x2db6986e0377d98}, []uint64{
			0xfbea17c3c4494b4e, 0xfa2bbf02a0fa30a4, 0xc7b51b2331b20d45, 0x6c5509a37c8a58c,
			0x3cd12cd79382e03b, 0x414c11b5953b34d1, 0x1bb854d1050ffda3, 0xae51dd918c0abc5c,
		}},
		{"emergencies", emergencies, [2]uint64{0xec9dc9d69e774bf0, 0xc9e8c337733b0ce0}, []uint64{
			0x6b1291299bc579a0, 0x6314f75045015b42, 0x38c32976bd45f21, 0x30a1e578dcb4ccfa,
			0x8d4a84782ee7cef6, 0x9c2a4ed1aa420f, 0xe4809d9f1b4ddaf4, 0xfc566e7f412a9f72,
		}},
	} {
		if test.seed != test.want {
			t.Errorf("%s: seed %#x, want %#x", test.name, test.seed, test.want)
		}
		stream := rand.NewPCG(test.seed[0], test.seed[1])
		got := make([]uint64, len(test.draws))
		for index := range got {
			got[index] = stream.Uint64()
		}
		if !slices.Equal(got, test.draws) {
			t.Errorf("%s: draws %#x, want %#x", test.name, got, test.draws)
		}
	}
}

// TestEffectiveIncidentRates checks the default of each absent member and the
// value of each present member.
func TestEffectiveIncidentRates(t *testing.T) {
	t.Parallel()
	config := Default()
	config.Demand.Seed = 1
	faultSeed, emergencySeed := incidentSeeds(1)
	base := sim.IncidentRates{Faults: sim.FaultRate{DebrisMeters: 2}, FaultSeed: faultSeed, EmergencySeed: emergencySeed}
	if got := EffectiveIncidentRates(config); got != base {
		t.Fatalf("no members: %+v, want %+v", got, base)
	}
	config.Faults, config.Emergencies = &FaultConfig{}, &EmergencyConfig{}
	if got := EffectiveIncidentRates(config); got != base {
		t.Fatalf("empty members: %+v, want %+v", got, base)
	}
	config.Faults = &FaultConfig{PerHour: new(12.0), DebrisShare: new(0.25), DebrisMeters: new(7.5), Duration: &FaultDuration{
		Kind: "exponential", MinSeconds: new(10), MaxSeconds: new(20), MeanSeconds: new(15),
	}}
	config.Emergencies = &EmergencyConfig{PerHour: new(3.0)}
	want := base
	want.Faults = sim.FaultRate{PerHour: 12, DebrisShare: 0.25, DebrisMeters: 7.5, Duration: sim.FaultDurationRule{
		Kind: sim.FaultDurationExponential, MinSeconds: 10, MaxSeconds: 20, MeanSeconds: 15,
	}}
	want.EmergenciesPerHour = 3
	if got := EffectiveIncidentRates(config); got != want {
		t.Fatalf("all members: %+v, want %+v", got, want)
	}
	config.Faults.Duration = &FaultDuration{Kind: "fixed", Seconds: new(600)}
	if got := EffectiveIncidentRates(config).Faults.Duration; got != (sim.FaultDurationRule{Kind: sim.FaultDurationFixed, Seconds: 600}) {
		t.Fatalf("fixed duration: %+v", got)
	}
	config.Demand.Seed = 2
	if got := EffectiveIncidentRates(config); got.FaultSeed == faultSeed || got.EmergencySeed == emergencySeed {
		t.Fatal("the seeds do not follow the demand seed")
	}
}

// TestConfigureIncidentSampling checks the start of the samplers of a project.
// Each form of a rate of 0 starts nothing, and each rate starts its own
// sampler from the stream of the demand seed. The next ticks come from a
// separate SHA-256 and PCG implementation.
func TestConfigureIncidentSampling(t *testing.T) {
	t.Parallel()
	negativeZero := math.Copysign(0, -1)
	fleet := func(t *testing.T, config Config, operations bool) *sim.Simulation {
		t.Helper()
		simulation, err := sim.NewFleet(config.Network, config.Fleet)
		if err != nil {
			t.Fatal(err)
		}
		if err := ConfigureExperiments(simulation, config, operations); err != nil {
			t.Fatal(err)
		}
		return simulation
	}
	duration := &FaultDuration{Kind: "fixed", Seconds: new(600)}
	for _, test := range []struct {
		name        string
		faults      *FaultConfig
		emergencies *EmergencyConfig
		fault       int64
		emergency   int64
	}{
		{"absent", nil, nil, 0, 0},
		{"empty", &FaultConfig{}, &EmergencyConfig{}, 0, 0},
		{"zero", &FaultConfig{PerHour: new(0.0)}, &EmergencyConfig{PerHour: new(0.0)}, 0, 0},
		{"negative zero", &FaultConfig{PerHour: &negativeZero}, &EmergencyConfig{PerHour: &negativeZero}, 0, 0},
		{"faults only", &FaultConfig{PerHour: new(60.0), Duration: duration}, nil, 1365, 0},
		{"faults at another rate", &FaultConfig{PerHour: new(1.0), Duration: duration}, nil, 81848, 0},
		{"emergencies only", nil, &EmergencyConfig{PerHour: new(30.0)}, 0, 6250},
		{"both", &FaultConfig{PerHour: new(60.0), Duration: duration}, &EmergencyConfig{PerHour: new(30.0)}, 1365, 6250},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			config.Demand.Seed = 1
			config.Faults, config.Emergencies = test.faults, test.emergencies
			simulation := fleet(t, config, true)
			if err := ConfigureIncidentSampling(simulation, config); err != nil {
				t.Fatal(err)
			}
			fault, emergency := simulation.IncidentNextTicksForTest()
			if fault != test.fault || emergency != test.emergency {
				t.Fatalf("next ticks %d and %d, want %d and %d", fault, emergency, test.fault, test.emergency)
			}
			// A second call and a new ConfigureExperiments keep a live sampler.
			if err := ConfigureExperiments(simulation, config, true); err != nil {
				t.Fatal(err)
			}
			if err := ConfigureIncidentSampling(simulation, config); err != nil {
				t.Fatal(err)
			}
			if again, other := simulation.IncidentNextTicksForTest(); again != fault || other != emergency {
				t.Fatalf("a second call moved the samplers to %d and %d", again, other)
			}
			// The operations off stop both samplers.
			if err := ConfigureExperiments(simulation, config, false); err != nil {
				t.Fatal(err)
			}
			if fault, emergency := simulation.IncidentNextTicksForTest(); fault != 0 || emergency != 0 {
				t.Fatalf("the operations off left the next ticks %d and %d", fault, emergency)
			}
		})
	}
	t.Run("operations off", func(t *testing.T) {
		t.Parallel()
		config := Default()
		config.Faults = &FaultConfig{PerHour: new(60.0), Duration: duration}
		config.Emergencies = &EmergencyConfig{PerHour: new(60.0)}
		simulation := fleet(t, config, false)
		if err := ConfigureIncidentSampling(simulation, config); err != nil {
			t.Fatal(err)
		}
		if fault, emergency := simulation.IncidentNextTicksForTest(); fault != 0 || emergency != 0 {
			t.Fatalf("next ticks %d and %d with the operations off", fault, emergency)
		}
	})
	t.Run("rate out of range", func(t *testing.T) {
		t.Parallel()
		config := Default()
		config.Emergencies = &EmergencyConfig{PerHour: new(61.0)}
		err := ConfigureIncidentSampling(fleet(t, config, true), config)
		if err == nil || !strings.Contains(err.Error(), "incident rate 61 is outside 0 to 60") {
			t.Fatalf("error %v", err)
		}
	})
}
