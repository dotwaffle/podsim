package project

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/dotwaffle/podsim/internal/sim"
)

// defaultDebrisMeters is the length of scenario debris of a project with no
// debrisMeters member.
const defaultDebrisMeters = 2

// EffectiveIncidentRates returns the scenario rates of a project, with the
// default for each absent member, and the sampler seeds of its demand seed.
func EffectiveIncidentRates(config Config) sim.IncidentRates {
	rates := sim.IncidentRates{Faults: sim.FaultRate{DebrisMeters: defaultDebrisMeters}}
	rates.FaultSeed, rates.EmergencySeed = incidentSeeds(config.Demand.Seed)
	if faults := config.Faults; faults != nil {
		rates.Faults.PerHour = valueOf(faults.PerHour)
		rates.Faults.DebrisShare = valueOf(faults.DebrisShare)
		if faults.DebrisMeters != nil {
			rates.Faults.DebrisMeters = *faults.DebrisMeters
		}
		if duration := faults.Duration; duration != nil {
			rates.Faults.Duration = sim.FaultDurationRule{
				Kind: duration.Kind, Seconds: valueOf(duration.Seconds), MinSeconds: valueOf(duration.MinSeconds),
				MaxSeconds: valueOf(duration.MaxSeconds), MeanSeconds: valueOf(duration.MeanSeconds),
			}
		}
	}
	if config.Emergencies != nil {
		rates.EmergenciesPerHour = valueOf(config.Emergencies.PerHour)
	}
	return rates
}

// ConfigureIncidentSampling starts the samplers of a project on simulation.
// A caller makes this call after each ConfigureExperiments with the
// operations on. A rate of 0 starts nothing, so no stream exists and no draw
// happens. A live sampler keeps its stream, so the call is safe after a
// restore.
func ConfigureIncidentSampling(simulation *sim.Simulation, config Config) error {
	return simulation.StartIncidentSampling(EffectiveIncidentRates(config))
}

// incidentSeeds returns the PCG arguments of the fault sampler and of the
// emergency sampler for a demand seed. Each sampler has its own stream, and
// neither uses the demand stream.
func incidentSeeds(seed uint64) (faults, emergencies [2]uint64) {
	return incidentSeed("podsim-faults-v1", seed), incidentSeed("podsim-emergencies-v1", seed)
}

// incidentSeed returns the first 16 bytes of SHA-256(label || bigEndian(seed)),
// as two little-endian values. The approved contract fixes the big-endian
// seed. RailSchedule hashes in the same way, with a little-endian seed.
func incidentSeed(label string, seed uint64) [2]uint64 {
	digest := sha256.Sum256(binary.BigEndian.AppendUint64([]byte(label), seed))
	return [2]uint64{binary.LittleEndian.Uint64(digest[:8]), binary.LittleEndian.Uint64(digest[8:16])}
}

// valueOf returns the value of pointer, or the zero value for nil.
func valueOf[T any](pointer *T) T {
	if pointer == nil {
		var zero T
		return zero
	}
	return *pointer
}
