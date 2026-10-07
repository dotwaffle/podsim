package main

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const energyModelName = "flat-v1"

type energyProfile struct {
	MassKg              float64 `json:"mass_kg"`
	ConstantResistanceN float64 `json:"constant_resistance_n"`
	QuadraticResistance float64 `json:"quadratic_resistance_n_per_mps2"`
	DriveEfficiency     float64 `json:"drive_efficiency"`
	RecoveryFraction    float64 `json:"recovery_fraction"`
	AuxiliaryWatts      float64 `json:"auxiliary_watts"`
}

type energyModel struct {
	Model    string                             `json:"model"`
	Profiles map[sim.VehicleClass]energyProfile `json:"profiles"`
}

type authoredEnergyProfile struct {
	MassKg              *float64 `json:"mass_kg"`
	ConstantResistanceN *float64 `json:"constant_resistance_n"`
	QuadraticResistance *float64 `json:"quadratic_resistance_n_per_mps2"`
	DriveEfficiency     *float64 `json:"drive_efficiency"`
	RecoveryFraction    *float64 `json:"recovery_fraction"`
	AuxiliaryWatts      *float64 `json:"auxiliary_watts"`
}

func readEnergyModel(path string) (*energyModel, error) {
	file, err := os.Open(path) // #nosec G304 -- The operator selects this local model file.
	if err != nil {
		return nil, fmt.Errorf("open energy file: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, project.MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read energy file: %w", err)
	}
	if len(data) > project.MaxFileBytes {
		return nil, errors.New("energy file exceeds the project file size bound")
	}
	return decodeEnergyModel(data)
}

func decodeEnergyModel(data []byte) (*energyModel, error) {
	var authored struct {
		Model    *string                                     `json:"model"`
		Profiles map[sim.VehicleClass]*authoredEnergyProfile `json:"profiles"`
	}
	if err := jsonv2.Unmarshal(data, &authored, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("decode energy file: %w", err)
	}
	if authored.Model == nil || *authored.Model != energyModelName || len(authored.Profiles) == 0 {
		return nil, errors.New("energy file requires flat-v1 and authored profiles")
	}
	model := &energyModel{Model: *authored.Model, Profiles: make(map[sim.VehicleClass]energyProfile)}
	for class, raw := range authored.Profiles {
		registered, ok := sim.LookupVehicleClass(class)
		if !ok || !registered.PhysicalSupported || class != registered.Class {
			return nil, fmt.Errorf("energy class %q is not a normalized supported class", class)
		}
		if raw == nil || raw.MassKg == nil || raw.ConstantResistanceN == nil || raw.QuadraticResistance == nil ||
			raw.DriveEfficiency == nil || raw.RecoveryFraction == nil || raw.AuxiliaryWatts == nil {
			return nil, fmt.Errorf("energy class %s requires every coefficient, including zeros", class)
		}
		profile := energyProfile{*raw.MassKg, *raw.ConstantResistanceN, *raw.QuadraticResistance, *raw.DriveEfficiency, *raw.RecoveryFraction, *raw.AuxiliaryWatts}
		if err := validateEnergyProfile(profile); err != nil {
			return nil, fmt.Errorf("energy class %s: %w", class, err)
		}
		model.Profiles[class] = profile
	}
	return model, nil
}

func finiteEnergy(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func validateEnergyProfile(p energyProfile) error {
	if !finiteEnergy(p.MassKg, p.ConstantResistanceN, p.QuadraticResistance, p.DriveEfficiency, p.RecoveryFraction, p.AuxiliaryWatts) ||
		p.MassKg <= 0 || p.ConstantResistanceN < 0 || p.QuadraticResistance < 0 || p.AuxiliaryWatts < 0 ||
		p.DriveEfficiency <= 0 || p.DriveEfficiency > 1 || p.RecoveryFraction < 0 || p.RecoveryFraction > 1 {
		return errors.New("energy coefficients must be finite and within the model domains")
	}
	return nil
}

type energyReport struct {
	Model           string                             `json:"model"`
	Profiles        map[sim.VehicleClass]energyProfile `json:"profiles"`
	MassAssumption  string                             `json:"mass_assumption"`
	ClassCounts     map[sim.VehicleClass]int           `json:"class_counts"`
	WindowStartTick int64                              `json:"window_start_tick"`
	WindowEndTick   int64                              `json:"window_end_tick"`
	TractionJoules  float64                            `json:"traction_joules"`
	RecoveredJoules float64                            `json:"recovered_joules"`
	AuxiliaryJoules float64                            `json:"auxiliary_joules"`
	NetJoules       float64                            `json:"net_joules"`
}

type energyMeter struct {
	report           energyReport
	classes          map[string]sim.VehicleClass
	auxiliaryPerTick float64
}

func newEnergyMeter(model *energyModel, fleet []sim.Placement, tick int64) (*energyMeter, error) {
	if model == nil {
		return nil, nil
	}
	if model.Model != energyModelName || tick < 0 {
		return nil, errors.New("energy meter requires flat-v1 and a nonnegative start tick")
	}
	meter := &energyMeter{classes: make(map[string]sim.VehicleClass), report: energyReport{
		Model: model.Model, Profiles: maps.Clone(model.Profiles), MassAssumption: "authored fixed effective mass includes assumed payload",
		ClassCounts: make(map[sim.VehicleClass]int), WindowStartTick: tick, WindowEndTick: tick,
	}}
	for class, profile := range model.Profiles {
		registered, ok := sim.LookupVehicleClass(class)
		if !ok || !registered.PhysicalSupported || class != registered.Class {
			return nil, fmt.Errorf("unsupported energy class %q", class)
		}
		if err := validateEnergyProfile(profile); err != nil {
			return nil, fmt.Errorf("energy class %s: %w", class, err)
		}
	}
	for _, placement := range fleet {
		profile, ok := sim.LookupVehicleClass(placement.Class)
		if !ok || !profile.PhysicalSupported {
			return nil, fmt.Errorf("unsupported fleet energy class %q", placement.Class)
		}
		if _, ok := model.Profiles[profile.Class]; !ok {
			return nil, fmt.Errorf("missing energy profile for fleet class %s", profile.Class)
		}
		if _, duplicate := meter.classes[placement.ID]; duplicate || placement.ID == "" {
			return nil, errors.New("energy fleet requires unique nonempty vehicle IDs")
		}
		meter.classes[placement.ID] = profile.Class
		meter.report.ClassCounts[profile.Class]++
	}
	for _, class := range slices.Sorted(maps.Keys(meter.report.ClassCounts)) {
		meter.auxiliaryPerTick += float64(meter.report.ClassCounts[class]) * model.Profiles[class].AuxiliaryWatts / sim.TicksPerSecond
	}
	if !finiteEnergy(meter.auxiliaryPerTick) {
		return nil, errors.New("energy fleet auxiliary arithmetic is nonfinite")
	}
	return meter, nil
}

func sampleEnergy(sample sim.MotionSample, profile energyProfile) (draw, recovered float64, err error) {
	if !finiteEnergy(sample.DistanceMeters, sample.StartSpeed, sample.EndSpeed) || sample.DistanceMeters < 0 || sample.StartSpeed < 0 || sample.EndSpeed < 0 {
		return 0, 0, errors.New("energy sample distance and speeds must be finite and nonnegative")
	}
	mean := sample.DistanceMeters * sim.TicksPerSecond
	resistance := (profile.ConstantResistanceN + profile.QuadraticResistance*mean*mean) * sample.DistanceMeters
	kinetic := profile.MassKg * (sample.EndSpeed*sample.EndSpeed - sample.StartSpeed*sample.StartSpeed) / 2
	wheel := kinetic + resistance
	draw, recovered = max(wheel, 0)/profile.DriveEfficiency, max(-wheel, 0)*profile.RecoveryFraction
	if !finiteEnergy(mean, resistance, kinetic, wheel, draw, recovered) {
		return 0, 0, errors.New("energy sample arithmetic is nonfinite")
	}
	return draw, recovered, nil
}

func (m *energyMeter) consume(frame sim.MotionFrame) error {
	duplicate := frame.Tick == m.report.WindowEndTick
	if frame.Tick < m.report.WindowEndTick || !duplicate && frame.Tick-m.report.WindowEndTick != 1 {
		return errors.New("energy frame clock must advance by exactly one tick")
	}
	var traction, recovery float64
	seen := make(map[string]bool, len(frame.Samples))
	for _, sample := range frame.Samples {
		class, ok := m.classes[sample.ID]
		if !ok || class != sample.Class || seen[sample.ID] {
			return errors.New("energy sample must identify one normalized fleet vehicle")
		}
		seen[sample.ID] = true
		draw, recovered, err := sampleEnergy(sample, m.report.Profiles[class])
		if err != nil {
			return fmt.Errorf("energy vehicle %s: %w", sample.ID, err)
		}
		traction += draw
		recovery += recovered
	}
	if !finiteEnergy(traction, recovery) {
		return errors.New("energy frame arithmetic is nonfinite")
	}
	if duplicate {
		return nil
	}
	next := m.report
	next.TractionJoules += traction
	next.RecoveredJoules += recovery
	next.AuxiliaryJoules += m.auxiliaryPerTick
	next.NetJoules = next.TractionJoules + next.AuxiliaryJoules - next.RecoveredJoules
	if !finiteEnergy(next.TractionJoules, next.RecoveredJoules, next.AuxiliaryJoules, next.NetJoules) {
		return errors.New("energy total arithmetic is nonfinite")
	}
	next.WindowEndTick = frame.Tick
	m.report = next
	return nil
}

func (m *energyMeter) snapshot() *energyReport {
	if m == nil {
		return nil
	}
	report := m.report
	report.Profiles, report.ClassCounts = maps.Clone(report.Profiles), maps.Clone(report.ClassCounts)
	return &report
}

func consumeEnergy(meter *energyMeter, simulation *sim.Simulation) error {
	if meter == nil {
		return nil
	}
	frame, ok := simulation.MotionFrame()
	if !ok || frame.Tick != simulation.Tick() {
		return errors.New("energy recording has no successful frame for the current tick")
	}
	return meter.consume(frame)
}

var energyCSVHeader = []string{"energy_model", "energy_profiles", "energy_mass_assumption", "energy_class_counts", "energy_window_start_tick", "energy_window_end_tick", "energy_traction_joules", "energy_recovered_joules", "energy_auxiliary_joules", "energy_net_joules"}

func energyCSVRow(report *energyReport) ([]string, error) {
	if report == nil {
		return make([]string, len(energyCSVHeader)), nil
	}
	profiles, err := jsonv2.Marshal(report.Profiles, json.DefaultOptionsV1())
	if err != nil {
		return nil, fmt.Errorf("encode energy profiles: %w", err)
	}
	counts, err := jsonv2.Marshal(report.ClassCounts, json.DefaultOptionsV1())
	if err != nil {
		return nil, fmt.Errorf("encode energy class counts: %w", err)
	}
	return []string{report.Model, string(profiles), report.MassAssumption, string(counts), strconv.FormatInt(report.WindowStartTick, 10), strconv.FormatInt(report.WindowEndTick, 10),
		floatText(report.TractionJoules), floatText(report.RecoveredJoules), floatText(report.AuxiliaryJoules), floatText(report.NetJoules)}, nil
}

func writeTableEnergy(output io.Writer, results []result) error {
	for _, outcome := range results {
		if outcome.Energy == nil {
			continue
		}
		data, err := jsonv2.Marshal(outcome.Energy, json.DefaultOptionsV1())
		if err != nil {
			return fmt.Errorf("encode table energy: %w", err)
		}
		if _, err := fmt.Fprintf(output, "energy %s/%s seed %d: %s\n", outcome.Pattern, outcome.Policy, outcome.Seed, data); err != nil {
			return fmt.Errorf("write table energy: %w", err)
		}
	}
	return nil
}
