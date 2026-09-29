package view

import "github.com/dotwaffle/podsim/internal/sim"

// podDestinationCode names the next stop of the current passenger or empty leg.
// Idle pods retain their last riders, so they have no destination tag.
func podDestinationCode(vehicle sim.Vehicle) string {
	if vehicle.Pod.Activity == sim.Idle {
		return ""
	}
	if vehicle.RelocatingTo != "" {
		return stationCode(vehicle.RelocatingTo)
	}
	if len(vehicle.Stops) > 0 {
		return stationCode(vehicle.Stops[0])
	}
	return ""
}
