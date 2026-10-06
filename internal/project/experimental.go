package project

import (
	"bytes"
	"errors"

	"github.com/dotwaffle/podsim/internal/sim"
)

// PolicyFlag is a Boolean project setting that rejects JSON null.
// Its zero value is false and is omitted from canonical project exports.
type PolicyFlag bool

// UnmarshalJSON accepts only Boolean values in both JSON decoder versions.
func (flag *PolicyFlag) UnmarshalJSON(data []byte) error {
	switch string(bytes.TrimSpace(data)) {
	case "true":
		*flag = true
	case "false":
		*flag = false
	default:
		return errors.New("experimental policy setting must be true or false")
	}
	return nil
}

// ConfigureExperiments applies the project's experimental controllers.
// Disabling buffers keeps existing saved members draining.
// ConfigurePlatoons must run first for a compact queue project.
// It also turns the fault operations on, with the faults settings, for a
// project with the fault marker, and off for every other project. It does
// the same for the emergency operations and the emergency marker. It
// cannot turn faults off while a fault is active, or emergencies while an
// emergency is active, so a caller that changes the project makes a new
// fleet first.
func ConfigureExperiments(simulation *sim.Simulation, config Config) error {
	if err := validateStationQueueSpacing(config); err != nil {
		return err
	}
	simulation.SetStationBuffers(bool(config.StationBuffers))
	if err := simulation.SetStationQueueSpacing(EffectiveStationQueueSpacing(config)); err != nil {
		return err
	}
	simulation.SetPickupSwaps(bool(config.PickupReassignment))
	if err := simulation.SetFaults(config.FaultContract != "", EffectiveFaultSettings(config)); err != nil {
		return err
	}
	return simulation.SetEmergencies(config.EmergencyContract != "")
}

// EffectiveFaultSettings returns the fault settings of a project with the
// fault marker, with the default for each absent member, and the zero
// settings for every other project. A restore gets the settings of the
// saved project from it.
func EffectiveFaultSettings(config Config) sim.FaultSettings {
	var faults sim.FaultSettings
	if config.FaultContract != "" {
		faults.EvacuationSeconds = evacuationSeconds(config.Faults)
	}
	return faults
}
