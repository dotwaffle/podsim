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
func ConfigureExperiments(simulation *sim.Simulation, config Config) {
	simulation.SetStationBuffers(bool(config.StationBuffers))
	simulation.SetPickupSwaps(bool(config.PickupReassignment))
}
