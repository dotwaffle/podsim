package sim

import (
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// incidentRatesForTest has both rates at 60, a uniform duration, and fixed
// seeds.
func incidentRatesForTest() IncidentRates {
	return IncidentRates{
		Faults: FaultRate{PerHour: 60, DebrisShare: 0.5, DebrisMeters: 2,
			Duration: FaultDurationRule{Kind: FaultDurationUniform, MinSeconds: 60, MaxSeconds: 600}},
		EmergenciesPerHour: 60,
		FaultSeed:          [2]uint64{1, 2},
		EmergencySeed:      [2]uint64{3, 4},
	}
}

// samplerFleet returns three pods at their berths on the example network,
// with both switches on.
func samplerFleet(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestIncidentUniformGolden pins the first eight draws for the PCG arguments
// 1 and 2, with the form of the contract. The values come from a separate
// PCG implementation, which also gives the published raw values of NewPCG(1, 2).
func TestIncidentUniformGolden(t *testing.T) {
	t.Parallel()
	want := []float64{0.6764556596678251, 0.4613862177205994, 0.5085473976760264, 0.4297927436037299,
		0.797802349388613, 0.3883664855410056, 0.8192750264193612, 0.3381816951746133}
	sampler := incidentSampler{stream: *rand.NewPCG(1, 2)}
	for index, draw := range want {
		if got := sampler.uniform(); got != draw {
			t.Fatalf("draw %d is %v, want %v", index, got, draw)
		}
	}
}

// TestNextIncidentTick checks the interval and the terminal rule of section
// 5.1.1 of the incident stage 7 contract.
func TestNextIncidentTick(t *testing.T) {
	t.Parallel()
	largest := 1 - 1.0/(1<<53)
	// interval is the contract formula. At 60 Hz, the median at a rate of 60
	// is 2496, and the largest u at a rate of 60 is 132253.
	interval := func(u, perHour float64) int64 {
		return int64(math.Ceil(-math.Log(1-u) * (3600 * TicksPerSecond) / perHour))
	}
	median := interval(0.5, 60)
	// The terminal rate scales with the tick rate. It is about 8.8e-10 at 60 Hz.
	aboveTerminal, belowTerminal := 1e-9*TicksPerSecond/60, 8e-10*TicksPerSecond/60
	for _, test := range []struct {
		name       string
		tick       int64
		u, perHour float64
		want       int64
	}{
		{"u 0 at rate 60", 1000, 0, 60, 1001},
		{"u 0 at rate 1e-300", 1000, 0, 1e-300, 1001},
		{"small u is one tick", 0, 1e-9, 60, 1},
		{"median at rate 60", 0, 0.5, 60, median},
		{"largest u at rate 60", 0, largest, 60, interval(largest, 60)},
		{"interval starts at the tick", 500, largest, 60, 500 + interval(largest, 60)},
		{"largest u at rate 1e-300", 0, largest, 1e-300, MaxCounter},
		{"median at rate 1e-300", 0, 0.5, 1e-300, MaxCounter},
		{"+Inf", 0, largest, 5e-324, MaxCounter},
		{"just above the terminal rate", 0, largest, aboveTerminal, interval(largest, aboveTerminal)},
		{"just below the terminal rate", 0, largest, belowTerminal, MaxCounter},
		{"result one below the limit", MaxCounter - median - 1, 0.5, 60, MaxCounter - 1},
		{"result at the limit", MaxCounter - median, 0.5, 60, MaxCounter},
		{"result above the limit", MaxCounter - 1, 0.5, 60, MaxCounter},
		{"tick at the limit", MaxCounter, 0, 60, MaxCounter},
	} {
		if got := nextIncidentTick(test.tick, test.u, test.perHour); got != test.want {
			t.Errorf("%s: %d, want %d", test.name, got, test.want)
		}
	}
}

// TestIncidentSamplerDue checks the due rule: the terminal value never comes
// due, also when the tick reaches it, and a stopped sampler has no event.
func TestIncidentSamplerDue(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		live bool
		next int64
		tick int64
		want bool
	}{
		{"before", true, 100, 99, false},
		{"at", true, 100, 100, true},
		{"after", true, 100, 101, true},
		{"stopped", false, 100, 100, false},
		{"terminal before the limit", true, MaxCounter, 100, false},
		{"terminal at the limit", true, MaxCounter, MaxCounter, false},
	} {
		sampler := incidentSampler{live: test.live, nextTick: test.next, perHour: 60}
		if got := sampler.due(test.tick); got != test.want {
			t.Errorf("%s: %v, want %v", test.name, got, test.want)
		}
	}
}

// TestStartIncidentSampling checks the start rule of section 5.1.2 of the
// incident stage 7 contract: each sampler needs a rate above 0 and its
// switch on, its first interval starts at the current tick, and a live
// sampler keeps its stream.
func TestStartIncidentSampling(t *testing.T) {
	t.Parallel()
	t.Run("no rate", func(t *testing.T) {
		t.Parallel()
		for _, rate := range []float64{0, math.Copysign(0, -1)} {
			s := samplerFleet(t)
			if err := s.StartIncidentSampling(IncidentRates{Faults: FaultRate{PerHour: rate}, EmergenciesPerHour: rate}); err != nil {
				t.Fatal(err)
			}
			if s.faultSampler != (incidentSampler{}) || s.emergencySampler != (incidentSampler{}) {
				t.Fatalf("rate %v started a sampler", rate)
			}
		}
	})
	t.Run("switch off", func(t *testing.T) {
		t.Parallel()
		s := samplerFleet(t)
		if err := s.SetFaults(false, FaultSettings{}); err != nil {
			t.Fatal(err)
		}
		if err := s.StartIncidentSampling(incidentRatesForTest()); err != nil {
			t.Fatal(err)
		}
		if s.faultSampler.live || !s.emergencySampler.live {
			t.Fatalf("fault sampler live %v, emergency sampler live %v", s.faultSampler.live, s.emergencySampler.live)
		}
	})
	t.Run("first interval", func(t *testing.T) {
		t.Parallel()
		s := samplerFleet(t)
		s.tick = 1000
		rates := incidentRatesForTest()
		if err := s.StartIncidentSampling(rates); err != nil {
			t.Fatal(err)
		}
		for name, test := range map[string]struct {
			sampler *incidentSampler
			seed    [2]uint64
		}{"fault": {&s.faultSampler, rates.FaultSeed}, "emergency": {&s.emergencySampler, rates.EmergencySeed}} {
			stream := rand.NewPCG(test.seed[0], test.seed[1])
			u := (&incidentSampler{stream: *stream}).uniform()
			stream.Uint64()
			if want := nextIncidentTick(1000, u, 60); test.sampler.nextTick != want || test.sampler.stream != *stream {
				t.Errorf("%s: next tick %d, want %d, or the first interval used another draw", name, test.sampler.nextTick, want)
			}
		}
		if s.faultRate != rates.Faults {
			t.Fatalf("fault rate %+v, want %+v", s.faultRate, rates.Faults)
		}
	})
	t.Run("live sampler keeps its stream", func(t *testing.T) {
		t.Parallel()
		s := samplerFleet(t)
		rates := incidentRatesForTest()
		if err := s.StartIncidentSampling(rates); err != nil {
			t.Fatal(err)
		}
		fault, emergency, rate := s.faultSampler, s.emergencySampler, s.faultRate
		s.tick = 7
		other := IncidentRates{Faults: FaultRate{PerHour: 1}, EmergenciesPerHour: 1, FaultSeed: [2]uint64{9, 9}, EmergencySeed: [2]uint64{9, 9}}
		if err := s.StartIncidentSampling(other); err != nil {
			t.Fatal(err)
		}
		if s.faultSampler != fault || s.emergencySampler != emergency || s.faultRate != rate {
			t.Fatal("a second start changed a live sampler")
		}
	})
	t.Run("rate out of range", func(t *testing.T) {
		t.Parallel()
		for _, rate := range []float64{-1, 60.5, math.NaN(), math.Inf(1)} {
			for _, rates := range []IncidentRates{
				{Faults: FaultRate{PerHour: rate}, EmergenciesPerHour: 1},
				{Faults: FaultRate{PerHour: 1}, EmergenciesPerHour: rate},
			} {
				s := samplerFleet(t)
				err := s.StartIncidentSampling(rates)
				if err == nil || !strings.HasSuffix(err.Error(), "is outside 0 to 60") || !strings.HasPrefix(err.Error(), "incident rate ") {
					t.Fatalf("rate %v: error %v", rate, err)
				}
				if s.faultSampler.live || s.emergencySampler.live {
					t.Fatalf("rate %v: a refused call started a sampler", rate)
				}
			}
		}
	})
}

// TestIncidentSamplerStops checks that a switch that turns off and Reset stop
// a sampler, and that no other call changes one.
func TestIncidentSamplerStops(t *testing.T) {
	t.Parallel()
	started := func(t *testing.T) *Simulation {
		t.Helper()
		s := samplerFleet(t)
		if err := s.StartIncidentSampling(incidentRatesForTest()); err != nil {
			t.Fatal(err)
		}
		return s
	}
	t.Run("faults off", func(t *testing.T) {
		t.Parallel()
		s := started(t)
		if err := s.SetFaults(false, FaultSettings{}); err != nil {
			t.Fatal(err)
		}
		if s.faultSampler != (incidentSampler{}) || s.faultRate != (FaultRate{}) || !s.emergencySampler.live {
			t.Fatalf("fault sampler %+v, rate %+v, emergency sampler %+v", s.faultSampler, s.faultRate, s.emergencySampler)
		}
	})
	t.Run("emergencies off", func(t *testing.T) {
		t.Parallel()
		s := started(t)
		if err := s.SetEmergencies(false); err != nil {
			t.Fatal(err)
		}
		if s.emergencySampler != (incidentSampler{}) || !s.faultSampler.live {
			t.Fatalf("fault sampler %+v, emergency sampler %+v", s.faultSampler, s.emergencySampler)
		}
	})
	t.Run("reset", func(t *testing.T) {
		t.Parallel()
		s := started(t)
		s.Reset()
		if s.faultSampler != (incidentSampler{}) || s.faultRate != (FaultRate{}) || s.emergencySampler != (incidentSampler{}) {
			t.Fatalf("fault sampler %+v, rate %+v, emergency sampler %+v", s.faultSampler, s.faultRate, s.emergencySampler)
		}
	})
	t.Run("switches on keep the stream", func(t *testing.T) {
		t.Parallel()
		s := started(t)
		fault, emergency := s.faultSampler, s.emergencySampler
		if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 10}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetEmergencies(true); err != nil {
			t.Fatal(err)
		}
		if s.faultSampler != fault || s.emergencySampler != emergency {
			t.Fatal("a switch that stays on changed a sampler")
		}
	})
	t.Run("refused switch keeps the sampler", func(t *testing.T) {
		t.Parallel()
		s := started(t)
		if _, err := s.Fault(FaultRequest{PodID: "01"}); err != nil {
			t.Fatal(err)
		}
		fault := s.faultSampler
		if err := s.SetFaults(false, FaultSettings{}); err == nil {
			t.Fatal("faults turned off with an active fault")
		}
		if s.faultSampler != fault {
			t.Fatal("a refused switch changed the sampler")
		}
	})
}

// TestIncidentSamplerClone checks that a clone has its own streams: draws on
// the clone do not move the original, and the original then gives the draws
// that the clone gave.
func TestIncidentSamplerClone(t *testing.T) {
	t.Parallel()
	s := samplerFleet(t)
	if err := s.StartIncidentSampling(incidentRatesForTest()); err != nil {
		t.Fatal(err)
	}
	c := s.Clone()
	for name, samplers := range map[string][2]*incidentSampler{
		"fault":     {&s.faultSampler, &c.faultSampler},
		"emergency": {&s.emergencySampler, &c.emergencySampler},
	} {
		original, clone := samplers[0], samplers[1]
		want := make([]float64, 4)
		for index := range want {
			want[index] = clone.uniform()
		}
		got := make([]float64, 4)
		for index := range got {
			got[index] = original.uniform()
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s: the original gave %v, the clone gave %v", name, got, want)
		}
	}
	if s.faultSampler.stream == s.emergencySampler.stream {
		t.Fatal("the samplers share one stream")
	}
}

// TestIncidentSamplerInvariants checks F13 and E9 of the incident stage 7
// contract: CheckContract refuses each state that they forbid, and accepts
// the terminal value at each tick.
func TestIncidentSamplerInvariants(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(s *Simulation, sampler *incidentSampler)
		want   string
	}{
		{"switch off", func(s *Simulation, sampler *incidentSampler) {
			if sampler == &s.faultSampler {
				s.faultsOn = false
			} else {
				s.emergenciesOn = false
			}
		}, "live with its switch off"},
		{"no rate", func(_ *Simulation, sampler *incidentSampler) { sampler.perHour = 0 }, "live with the rate 0"},
		{"next tick is the tick", func(s *Simulation, sampler *incidentSampler) { sampler.nextTick = s.tick }, "is not after the tick"},
		{"next tick before the tick", func(s *Simulation, sampler *incidentSampler) { sampler.nextTick = s.tick - 1 }, "is not after the tick"},
		{"next tick after the tick", func(s *Simulation, sampler *incidentSampler) { sampler.nextTick = s.tick + 1 }, ""},
		{"terminal value", func(_ *Simulation, sampler *incidentSampler) { sampler.nextTick = MaxCounter }, ""},
		{"terminal value at the limit", func(s *Simulation, sampler *incidentSampler) {
			s.tick = MaxCounter
			sampler.nextTick = MaxCounter
		}, ""},
	} {
		for name, sampler := range map[string]func(*Simulation) *incidentSampler{
			"F13": func(s *Simulation) *incidentSampler { return &s.faultSampler },
			"E9":  func(s *Simulation) *incidentSampler { return &s.emergencySampler },
		} {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				s := samplerFleet(t)
				s.tick = 100
				rates := incidentRatesForTest()
				if name == "F13" {
					rates.EmergenciesPerHour = 0
				} else {
					rates.Faults.PerHour = 0
				}
				if err := s.StartIncidentSampling(rates); err != nil {
					t.Fatal(err)
				}
				test.change(s, sampler(s))
				err := s.CheckContract()
				if test.want == "" {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if err == nil || !strings.HasPrefix(err.Error(), name+": ") || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error %v, want %s with %q", err, name, test.want)
				}
			})
		}
	}
}

// TestIncidentVehicleOrder checks rule G11 of the incident stage 7 contract:
// s.vehicles is in pod ID order through a new fleet with placements in
// reverse order, a reset, a clone, and both restore tiers, and a restore
// refuses saved pods that are out of that order. The samplers walk
// s.vehicles and sort nothing.
func TestIncidentVehicleOrder(t *testing.T) {
	t.Parallel()
	placements := []Placement{{ID: "03", StationID: "market"}, {ID: "02", StationID: "garden"}, {ID: "01", StationID: "harbor"}}
	check := func(t *testing.T, where string, s *Simulation) {
		t.Helper()
		got := make([]string, len(s.vehicles))
		for index := range s.vehicles {
			got[index] = s.vehicles[index].Pod.ID
		}
		if want := []string{"01", "02", "03"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: pods %v, want %v", where, got, want)
		}
	}
	s, err := NewFleet(Example(), placements)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "new fleet", s)
	s.Step()
	s.Reset()
	check(t, "reset", s)
	check(t, "clone", s.Clone())
	for _, logical := range []bool{false, true} {
		restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: placements, State: s.ExportState(), LogicalOnly: logical})
		if err != nil {
			t.Fatal(err)
		}
		if logical != (result.Tier == RestoreLogical) {
			t.Fatalf("the restore has the tier %v", result.Tier)
		}
		check(t, string(result.Tier), restored)
		state := s.ExportState()
		state.Pods[0], state.Pods[1] = state.Pods[1], state.Pods[0]
		_, _, err = RestoreState(RestoreStateInput{Network: s.network, Fleet: placements, State: state, LogicalOnly: logical})
		if err == nil || !strings.Contains(err.Error(), `saved pod "01" is empty or out of order`) {
			t.Fatalf("a restore of saved pods out of order: %v", err)
		}
	}
}
