package project

import (
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// ParkingGaps returns the parking gaps of the network of config for the
// classes of its fleet, in the order in which the classes first appear in
// the fleet. A pod with no class is a Legacy pod. See sim.Network.ParkingGaps.
func ParkingGaps(config Config) []sim.ParkingGap {
	var classes []sim.VehicleClass
	for _, placement := range config.Fleet {
		class := placement.Class
		if class == "" {
			class = sim.LegacyClass
		}
		if !slices.Contains(classes, class) {
			classes = append(classes, class)
		}
	}
	return config.Network.ParkingGaps(classes)
}
