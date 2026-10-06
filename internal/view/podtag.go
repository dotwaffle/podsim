package view

import (
	"cmp"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

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

// podTagCode returns the lines of the map tag of a pod under its fleet
// number, such as ">EUS\n<KGX". The first line is the next stop. The second
// line is the leg origin of the first rider aboard. An empty leg has no
// tracked origin, and a station without a code has no origin, so then the
// tag has the next stop only.
func podTagCode(vehicle sim.Vehicle) string {
	to := podDestinationCode(vehicle)
	if to == "" {
		return ""
	}
	to = ">" + to
	if vehicle.RelocatingTo != "" {
		return to
	}
	index := slices.IndexFunc(vehicle.Riders, func(rider sim.Request) bool { return !rider.Completed })
	if index < 0 {
		return to
	}
	rider := vehicle.Riders[index]
	if from := stationCode(cmp.Or(rider.LegFrom, rider.From)); from != "" {
		return to + "\n<" + from
	}
	return to
}
