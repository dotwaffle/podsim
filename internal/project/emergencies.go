package project

import (
	"errors"
	"math"
)

// EmergencyConfig holds the emergencies settings of a project. Each member
// is optional. A nil member has its default: PerHour 0.
type EmergencyConfig struct {
	// PerHour is the scenario emergency rate. It must be 0, because the
	// scenario sampler is not available.
	PerHour *float64 `json:"perHour,omitzero"`
}

// widestEmergencies has the longest canonical encoding of the emergencies
// settings that Validate accepts. Validate measures each project with it.
// Validate accepts a rate of negative zero,
// which JSON writes as -0.
var widestEmergencies = EmergencyConfig{PerHour: new(math.Copysign(0, -1))}

// WidestOperationSettings returns the widest faults and emergencies settings
// that Validate measures each project with. A test that sizes a project near
// the file limit needs them. The values share their pointers with the
// package, so a caller must not change what the pointers hold.
func WidestOperationSettings() (FaultConfig, EmergencyConfig) {
	return widestFaults, widestEmergencies
}

// validateEmergencies checks the emergencies settings of a project with an
// emergencies member.
func validateEmergencies(config Config) error {
	if config.Emergencies == nil {
		return nil
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
