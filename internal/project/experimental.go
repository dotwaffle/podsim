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
// It also turns the fault and emergency operations on or off, as operations
// says, with the faults settings. It cannot turn faults off while a fault is
// active, or emergencies while an emergency is active, so a caller that
// changes the project makes a new fleet first.
func ConfigureExperiments(simulation *sim.Simulation, config Config, operations bool) error {
	simulation.SetPickupSwaps(bool(config.PickupReassignment))
	if err := simulation.SetFaults(operations, EffectiveFaultSettings(config)); err != nil {
		return err
	}
	return simulation.SetEmergencies(operations)
}

// EffectiveFaultSettings returns the fault settings of a project, with the
// default for each absent member. A restore gets the settings of the saved
// project from it.
func EffectiveFaultSettings(config Config) sim.FaultSettings {
	return sim.FaultSettings{EvacuationSeconds: evacuationSeconds(config.Faults)}
}
