package project

import (
	"errors"
	"math"

	"github.com/dotwaffle/podsim/internal/sim"
)

// EmergencyContract selects the emergency operations of the incident
// emergency contract. It requires the incident marker. The simulation owns
// the type, as it owns the incident marker.
type EmergencyContract = sim.EmergencyContract

// EmergencyV1Contract permits the emergency command and the emergencies
// settings.
const EmergencyV1Contract = sim.EmergencyV1Contract

// EmergencyConfig holds the emergencies settings of a project with the
// emergency marker. Each member is optional. A nil member has its default:
// PerHour 0.
type EmergencyConfig struct {
	// PerHour is the scenario emergency rate. It must be 0, because the
	// scenario sampler is not available.
	PerHour *float64 `json:"perHour,omitzero"`
}

// errUnknownEmergencyContract means that the emergency marker is not
// EmergencyV1Contract.
var errUnknownEmergencyContract = errors.New("emergency contract must be emergency-v1")

// widestEmergencies has the longest canonical encoding of the emergencies
// settings that Validate accepts. Validate measures a project with the
// emergency marker with it. Validate accepts a rate of negative zero,
// which JSON writes as -0.
var widestEmergencies = EmergencyConfig{PerHour: new(math.Copysign(0, -1))}

// validateEmergencyContract checks the emergency marker and the
// emergencies settings. The marker needs the incident marker and the
// settings. It does not need the fault marker. The settings need the
// marker.
func validateEmergencyContract(config Config) error {
	switch {
	case config.EmergencyContract == "" && config.Emergencies != nil:
		return errors.New("emergencies require emergencyContract emergency-v1")
	case config.EmergencyContract == "":
		return nil
	case config.EmergencyContract != EmergencyV1Contract:
		return errUnknownEmergencyContract
	case config.IncidentContract == "":
		return errors.New("emergencyContract requires incidentContract incident-v1")
	case config.Emergencies == nil:
		return errors.New("emergencyContract requires emergencies")
	}
	// A rate above 0 needs the scenario sampler. NaN is not 0.
	if value := config.Emergencies.PerHour; value != nil && *value != 0 {
		return errors.New("emergencies perHour must be 0")
	}
	return nil
}

// cloneEmergencies returns a copy of emergencies that shares no memory
// with it.
func cloneEmergencies(emergencies *EmergencyConfig) *EmergencyConfig {
	if emergencies == nil {
		return nil
	}
	return &EmergencyConfig{PerHour: clonePointer(emergencies.PerHour)}
}
