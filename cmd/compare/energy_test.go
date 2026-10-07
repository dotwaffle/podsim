package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func energyTestModel() *energyModel {
	return &energyModel{Model: energyModelName, Profiles: map[sim.VehicleClass]energyProfile{
		sim.LegacyClass: {MassKg: 100, DriveEfficiency: 1},
	}}
}

func energyTestJSON(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(energyTestModel())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEnergyAuthoredModel(t *testing.T) {
	t.Parallel()
	valid := energyTestJSON(t)
	if model, err := decodeEnergyModel([]byte(valid)); err != nil || model.Profiles[sim.LegacyClass].AuxiliaryWatts != 0 {
		t.Fatalf("authored zero rejected: %v", err)
	}
	tests := map[string]string{
		"root null": "null", "model null": strings.Replace(valid, `"flat-v1"`, `null`, 1),
		"unknown model":     strings.Replace(valid, "flat-v1", "flat-v2", 1),
		"unknown member":    strings.Replace(valid, `"mass_kg":100`, `"mass_kg":100,"extra":0`, 1),
		"case alias":        strings.Replace(valid, `"mass_kg"`, `"Mass_kg"`, 1),
		"duplicate member":  strings.Replace(valid, `"mass_kg":100`, `"mass_kg":100,"mass_kg":200`, 1),
		"duplicate model":   strings.Replace(valid, `"model":"flat-v1"`, `"model":"flat-v1","model":"flat-v1"`, 1),
		"duplicate class":   strings.Replace(valid, `"legacy":`, `"legacy":{},"legacy":`, 1),
		"class null":        `{"model":"flat-v1","profiles":{"legacy":null}}`,
		"profiles null":     `{"model":"flat-v1","profiles":null}`,
		"unknown class":     strings.Replace(valid, "legacy", "future", 1),
		"unsupported class": strings.Replace(valid, "legacy", "express", 1),
		"empty class":       strings.Replace(valid, `"legacy"`, `""`, 1),
		"nonfinite":         strings.Replace(valid, `"mass_kg":100`, `"mass_kg":1e999`, 1),
		"trailing":          valid + ` {}`,
	}
	for _, field := range []string{"mass_kg", "constant_resistance_n", "quadratic_resistance_n_per_mps2", "drive_efficiency", "recovery_fraction", "auxiliary_watts"} {
		var members map[string]json.RawMessage
		profile, err := json.Marshal(energyTestModel().Profiles[sim.LegacyClass])
		if err != nil {
			t.Fatal(err)
		}
		if decodeErr := json.Unmarshal(profile, &members); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		delete(members, field)
		missing, err := json.Marshal(members)
		if err != nil {
			t.Fatal(err)
		}
		tests["missing "+field] = `{"model":"flat-v1","profiles":{"legacy":` + string(missing) + `}}`
		members[field] = json.RawMessage(`null`)
		null, err := json.Marshal(members)
		if err != nil {
			t.Fatal(err)
		}
		tests["null "+field] = `{"model":"flat-v1","profiles":{"legacy":` + string(null) + `}}`
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeEnergyModel([]byte(input)); err == nil {
				t.Fatalf("accepted %s", input)
			}
		})
	}
	for _, test := range []struct {
		name, field string
		value       float64
	}{
		{"mass zero", "mass_kg", 0}, {"mass negative", "mass_kg", -1}, {"constant negative", "constant_resistance_n", -1},
		{"quadratic negative", "quadratic_resistance_n_per_mps2", -1}, {"drive zero", "drive_efficiency", 0},
		{"drive above one", "drive_efficiency", 1.01}, {"recovery negative", "recovery_fraction", -1},
		{"recovery above one", "recovery_fraction", 1.01}, {"aux negative", "auxiliary_watts", -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var raw map[string]any
			if err := json.Unmarshal([]byte(valid), &raw); err != nil {
				t.Fatal(err)
			}
			profiles, ok := raw["profiles"].(map[string]any)
			if !ok {
				t.Fatal("profiles")
			}
			profile, ok := profiles["legacy"].(map[string]any)
			if !ok {
				t.Fatal("profile")
			}
			profile[test.field] = test.value
			data, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeEnergyModel(data); err == nil {
				t.Fatal("accepted invalid domain")
			}
		})
	}
}

func TestEnergyAnalyticWork(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		profile        energyProfile
		sample         sim.MotionSample
		draw, recovery float64
	}{
		{"constant speed", energyProfile{MassKg: 100, ConstantResistanceN: 5, QuadraticResistance: 2, DriveEfficiency: .5}, sim.MotionSample{DistanceMeters: 1.0 / 60, StartSpeed: 1, EndSpeed: 1}, 7.0 / 30, 0},
		{"acceleration", energyProfile{MassKg: 100, DriveEfficiency: .5}, sim.MotionSample{StartSpeed: 0, EndSpeed: 2}, 400, 0},
		{"braking", energyProfile{MassKg: 100, DriveEfficiency: 1, RecoveryFraction: .5}, sim.MotionSample{StartSpeed: 2, EndSpeed: 0}, 0, 100},
		{"resistance reduces recovery", energyProfile{MassKg: 100, ConstantResistanceN: 50, DriveEfficiency: 1, RecoveryFraction: .5}, sim.MotionSample{DistanceMeters: 1, StartSpeed: 2, EndSpeed: 0}, 0, 75},
		{"drag exceeds braking", energyProfile{MassKg: 100, ConstantResistanceN: 300, DriveEfficiency: .5, RecoveryFraction: .5}, sim.MotionSample{DistanceMeters: 1, StartSpeed: 2, EndSpeed: 0}, 200, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draw, recovery, err := sampleEnergy(test.sample, test.profile)
			if err != nil || math.Abs(draw-test.draw) > 1e-12 || math.Abs(recovery-test.recovery) > 1e-12 {
				t.Fatalf("draw=%g recovery=%g err=%v", draw, recovery, err)
			}
		})
	}
}

func TestEnergyClockAuxiliaryAndOwnership(t *testing.T) {
	t.Parallel()
	model := energyTestModel()
	p := model.Profiles[sim.LegacyClass]
	p.AuxiliaryWatts = 60
	model.Profiles[sim.LegacyClass] = p
	model.Profiles[sim.CompactClass] = energyProfile{MassKg: 200, DriveEfficiency: 1, AuxiliaryWatts: 120}
	model.Profiles[sim.GroupClass] = energyProfile{MassKg: 300, DriveEfficiency: 1, AuxiliaryWatts: 180}
	meter, err := newEnergyMeter(model, []sim.Placement{{ID: "a"}, {ID: "b", Class: sim.LegacyClass}, {ID: "c", Class: sim.CompactClass}, {ID: "d", Class: sim.GroupClass}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	delete(model.Profiles, sim.LegacyClass)
	for tick := int64(8); tick <= 10; tick++ {
		if err := meter.consume(sim.MotionFrame{Tick: tick}); err != nil {
			t.Fatal(err)
		}
	}
	if meter.report.AuxiliaryJoules != 21 || meter.report.NetJoules != 21 || meter.report.ClassCounts[sim.LegacyClass] != 2 {
		t.Fatalf("full fleet auxiliary: %+v", meter.report)
	}
	before := meter.snapshot()
	if err := meter.consume(sim.MotionFrame{Tick: 10}); err != nil || !reflect.DeepEqual(before, meter.snapshot()) {
		t.Fatal("duplicate changed meter")
	}
	before.Profiles[sim.LegacyClass] = energyProfile{}
	before.ClassCounts[sim.LegacyClass] = 0
	if meter.report.Profiles[sim.LegacyClass].MassKg != 100 || meter.report.ClassCounts[sim.LegacyClass] != 2 {
		t.Fatal("report shared storage")
	}
	if err := meter.consume(sim.MotionFrame{Tick: 11, Samples: []sim.MotionSample{{ID: "a", Class: sim.LegacyClass, StartSpeed: 2}}}); err != nil {
		t.Fatal(err)
	}
	if meter.report.RecoveredJoules != 0 {
		t.Fatal("authored zero recovery changed")
	}
}

func TestEnergyAtomicRejection(t *testing.T) {
	t.Parallel()
	valid := sim.MotionSample{ID: "a", Class: sim.LegacyClass, EndSpeed: 1}
	for _, test := range []struct {
		name   string
		change func(*energyMeter, *sim.MotionFrame)
	}{
		{"missed", func(_ *energyMeter, f *sim.MotionFrame) { f.Tick = 2 }},
		{"regression", func(_ *energyMeter, f *sim.MotionFrame) { f.Tick = -1 }},
		{"unknown vehicle", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].ID = "missing" }},
		{"wrong class", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].Class = sim.GroupClass }},
		{"duplicate", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].ID = "a" }},
		{"distance negative", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].DistanceMeters = -1 }},
		{"speed start negative", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].StartSpeed = -1 }},
		{"speed end negative", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].EndSpeed = -1 }},
		{"nan", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].DistanceMeters = math.NaN() }},
		{"inf", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].EndSpeed = math.Inf(1) }},
		{"sample overflow", func(_ *energyMeter, f *sim.MotionFrame) { f.Samples[1].EndSpeed = math.MaxFloat64 }},
		{"draw overflow", func(m *energyMeter, _ *sim.MotionFrame) {
			p := m.report.Profiles[sim.LegacyClass]
			p.DriveEfficiency = math.SmallestNonzeroFloat64
			m.report.Profiles[sim.LegacyClass] = p
		}},
		{"total overflow", func(m *energyMeter, f *sim.MotionFrame) {
			m.report.TractionJoules = math.MaxFloat64
			f.Samples[1].EndSpeed = 1e153
		}},
		{"aux overflow", func(m *energyMeter, _ *sim.MotionFrame) {
			m.report.AuxiliaryJoules = math.MaxFloat64
			m.auxiliaryPerTick = math.MaxFloat64
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m, err := newEnergyMeter(energyTestModel(), []sim.Placement{{ID: "a"}, {ID: "b"}}, 0)
			if err != nil {
				t.Fatal(err)
			}
			frame := sim.MotionFrame{Tick: 1, Samples: []sim.MotionSample{valid, {ID: "b", Class: sim.LegacyClass, EndSpeed: 1}}}
			test.change(m, &frame)
			before := m.snapshot()
			if err := m.consume(frame); err == nil {
				t.Fatal("accepted invalid frame")
			}
			if !reflect.DeepEqual(before, m.snapshot()) {
				t.Fatal("rejection changed totals or clock")
			}
		})
	}
}

func TestEnergyFleetValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		model *energyModel
		fleet []sim.Placement
		tick  int64
	}{
		{"missing", energyTestModel(), []sim.Placement{{ID: "a", Class: sim.CompactClass}}, 0},
		{"unsupported", energyTestModel(), []sim.Placement{{ID: "a", Class: sim.ExpressClass}}, 0},
		{"unknown", energyTestModel(), []sim.Placement{{ID: "a", Class: "future"}}, 0},
		{"duplicate IDs", energyTestModel(), []sim.Placement{{ID: "a"}, {ID: "a"}}, 0},
		{"empty ID", energyTestModel(), []sim.Placement{{}}, 0},
		{"clock", energyTestModel(), []sim.Placement{{ID: "a"}}, -1},
		{"model", &energyModel{Model: "future"}, nil, 0},
		{"nonfinite", &energyModel{Model: energyModelName, Profiles: map[sim.VehicleClass]energyProfile{sim.LegacyClass: {MassKg: math.NaN(), DriveEfficiency: 1}}}, nil, 0},
		{"aux product", &energyModel{Model: energyModelName, Profiles: map[sim.VehicleClass]energyProfile{sim.LegacyClass: {MassKg: 100, DriveEfficiency: 1, AuxiliaryWatts: math.MaxFloat64}}}, []sim.Placement{{ID: "a"}, {ID: "b"}}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := newEnergyMeter(test.model, test.fleet, test.tick); err == nil {
				t.Fatal("accepted invalid fleet/model")
			}
		})
	}
}

func TestEnergyDuplicateFrameValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		sample sim.MotionSample
	}{
		{"identity", sim.MotionSample{ID: "unknown", Class: sim.LegacyClass}},
		{"class", sim.MotionSample{ID: "a", Class: sim.GroupClass}},
		{"negative", sim.MotionSample{ID: "a", Class: sim.LegacyClass, StartSpeed: -1}},
		{"nonfinite", sim.MotionSample{ID: "a", Class: sim.LegacyClass, EndSpeed: math.NaN()}},
		{"arithmetic", sim.MotionSample{ID: "a", Class: sim.LegacyClass, EndSpeed: math.MaxFloat64}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			meter, err := newEnergyMeter(energyTestModel(), []sim.Placement{{ID: "a"}}, 0)
			if err != nil {
				t.Fatal(err)
			}
			before := meter.snapshot()
			if err := meter.consume(sim.MotionFrame{Tick: 0, Samples: []sim.MotionSample{test.sample}}); err == nil {
				t.Fatal("malformed duplicate accepted")
			}
			if !reflect.DeepEqual(before, meter.snapshot()) {
				t.Fatal("malformed duplicate changed meter")
			}
		})
	}
	meter, err := newEnergyMeter(energyTestModel(), []sim.Placement{{ID: "a"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	meter.report.TractionJoules = math.MaxFloat64
	before := meter.snapshot()
	if err := meter.consume(sim.MotionFrame{Tick: 0, Samples: []sim.MotionSample{{ID: "a", Class: sim.LegacyClass, EndSpeed: 1e153}}}); err != nil || !reflect.DeepEqual(before, meter.snapshot()) {
		t.Fatal("valid duplicate added energy or overflowed existing totals")
	}
}

func TestEnergyFileBound(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "energy.json")
	if err := os.WriteFile(path, []byte(energyTestJSON(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEnergyModel(path); err != nil {
		t.Fatal(err)
	}
	valid := energyTestJSON(t)
	padded := valid + strings.Repeat(" ", project.MaxFileBytes+1-len(valid))
	if err := os.WriteFile(path, []byte(padded), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readEnergyModel(path); err == nil {
		t.Fatal("oversize file accepted")
	}
	if _, err := readEnergyModel(path + ".missing"); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestEnergyRunAndReportParity(t *testing.T) {
	t.Parallel()
	input := smallBurstInput(t)
	input.duration = 3 * time.Minute
	input.arrivalsFor = time.Minute
	input.schedule = slices.DeleteFunc(slices.Clone(input.schedule), func(r scheduledRequest) bool { return r.tick >= durationTicks(input.arrivalsFor) })
	base, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	input.energy = energyTestModel()
	p := input.energy.Profiles[sim.LegacyClass]
	p.ConstantResistanceN = 2
	p.DriveEfficiency = .8
	p.RecoveryFraction = .5
	p.AuxiliaryWatts = 60
	input.energy.Profiles[sim.LegacyClass] = p
	enabled, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	energy := enabled.Energy
	if energy == nil || energy.TractionJoules <= 0 || energy.RecoveredJoules <= 0 || energy.WindowEndTick != durationTicks(input.duration) {
		t.Fatalf("missing actual motion: %+v", energy)
	}
	if math.Abs(energy.AuxiliaryJoules-float64(len(input.scenario.fleet))*float64(energy.WindowEndTick)) > 1e-8 {
		t.Fatal("aux clock mismatch")
	}
	enabled.Energy = nil
	if !reflect.DeepEqual(base, enabled) {
		t.Fatal("meter changed comparison outcome")
	}
	enabled.Energy = energy
	for _, format := range []string{"json", "csv", "table"} {
		var old, on, cleared bytes.Buffer
		if err := writeReport(writeReportInput{output: &old, format: format, results: []result{base}}); err != nil {
			t.Fatal(err)
		}
		if err := writeReport(writeReportInput{output: &on, format: format, results: []result{enabled}}); err != nil {
			t.Fatal(err)
		}
		withoutEnergy := enabled
		withoutEnergy.Energy = nil
		if err := writeReport(writeReportInput{output: &cleared, format: format, results: []result{withoutEnergy}}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(old.Bytes(), cleared.Bytes()) || strings.Contains(old.String(), "energy") {
			t.Fatal("disabled report changed")
		}
		if !strings.Contains(on.String(), "flat-v1") || !strings.Contains(on.String(), "mass_kg") {
			t.Fatal("enabled report omitted authored model")
		}
		if format == "json" {
			var decoded report
			if err := json.Unmarshal(on.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.SchemaVersion != 15 || len(decoded.Results) != 1 || !reflect.DeepEqual(decoded.Results[0].Energy, energy) {
				t.Fatal("energy report round trip")
			}
		}
		if format == "csv" {
			rows, err := csv.NewReader(&on).ReadAll()
			if err != nil || len(rows) != 2 || len(rows[0]) != len(rows[1]) {
				t.Fatalf("CSV shape: %v", err)
			}
		}
	}
}

func TestEnergyCLIAndServiceClock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "energy.json")
	if err := os.WriteFile(path, []byte(energyTestJSON(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-duration", "2s", "-request-every", "1s", "-redistribution-policies", "off", "-energy-file", path, "-format", "json"}
	var out, stderr bytes.Buffer
	if code := runCLI(cliInput{args: args, stdout: &out, stderr: &stderr}); code != 0 {
		t.Fatalf("CLI code=%d %s", code, &stderr)
	}
	var decoded report
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Results) != 1 || decoded.Results[0].Energy == nil || decoded.Results[0].Energy.WindowEndTick != 120 {
		t.Fatal("CLI lost energy or added arms")
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{"balanced", "rail-services"} {
		input := runInput{energy: energyTestModel(), scenario: caseStudy, duration: 3 * time.Second, arrivalsFor: time.Second, requestEvery: time.Second, pattern: pattern, policy: "off", routingPolicy: "free-flow", sharingLimit: 1, queueLimit: 10, stopWhenDrained: true}
		outcome, runErr := run(input)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if outcome.Energy.WindowEndTick != 60 || outcome.ActualEndSeconds != 1 {
			t.Fatalf("early drain clock %s: %+v", pattern, outcome)
		}
	}
	// The baseline is the restored/current tick, so enabling never fills earlier time.
	s, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		s.Step()
	}
	m, err := newEnergyMeter(energyTestModel(), caseStudy.fleet, s.Tick())
	if err != nil {
		t.Fatal(err)
	}
	if err := consumeEnergy(m, s); err == nil {
		t.Fatal("disabled recorder accepted")
	}
	s.SetMotionRecording(true)
	s.Step()
	if err := consumeEnergy(m, s); err != nil {
		t.Fatal(err)
	}
	s.SetPaused(true)
	s.Step()
	before := m.snapshot()
	if err := consumeEnergy(m, s); err != nil || !reflect.DeepEqual(before, m.snapshot()) {
		t.Fatal("pause counted twice")
	}
	s.Reset()
	if err := consumeEnergy(m, s); err == nil {
		t.Fatal("reset accepted old meter window")
	}
}

func TestEnergyNativeNonlegacyMotion(t *testing.T) {
	t.Parallel()
	for _, class := range []sim.VehicleClass{sim.CompactClass, sim.GroupClass} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			caseStudy, err := loadScenario(filepath.Join("..", "..", "internal", "project", "testdata", "group_public.json"), "")
			if err != nil {
				t.Fatal(err)
			}
			caseStudy.fleet[0].Class = class
			s, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
			if err != nil {
				t.Fatal(err)
			}
			model := &energyModel{Model: energyModelName, Profiles: map[sim.VehicleClass]energyProfile{class: {MassKg: 200, ConstantResistanceN: 2, DriveEfficiency: 1, RecoveryFraction: 1, AuxiliaryWatts: 60}}}
			meter, err := newEnergyMeter(model, caseStudy.fleet, s.Tick())
			if err != nil {
				t.Fatal(err)
			}
			s.SetMotionRecording(true)
			if err := s.RequestJourney(caseStudy.fleet[0].ID, "market"); err != nil {
				t.Fatal(err)
			}
			arrived, moving := false, false
			for range 300 * sim.TicksPerSecond {
				s.Step()
				if err := consumeEnergy(meter, s); err != nil {
					t.Fatal(err)
				}
				frame, _ := s.MotionFrame()
				for _, sample := range frame.Samples {
					if sample.Class != class {
						t.Fatal("native class lost")
					}
					moving = moving || sample.DistanceMeters > 0
				}
				if s.Snapshot().Vehicles[0].Pod.Activity == sim.Unloading {
					arrived = true
					break
				}
			}
			if !arrived || !moving || meter.report.TractionJoules <= 0 || meter.report.RecoveredJoules <= 0 {
				t.Fatalf("nonlegacy motion not exercised: %+v", meter.report)
			}
			state := s.MetricsSnapshot()
			want := 2*(state.PassengerDistanceMeters+state.EmptyDistanceMeters) + float64(s.Tick())
			if math.Abs(meter.report.NetJoules-want) > 1e-6 || meter.report.ClassCounts[class] != 1 {
				t.Fatalf("actual rest-to-rest energy %g, analytic %g", meter.report.NetJoules, want)
			}
		})
	}
}
