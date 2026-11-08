package project

import (
	"fmt"
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

// validateParkingRoutes refuses a project with a parking gap. The error
// gives the count of the berths with a gap and names the first.
func validateParkingRoutes(config Config) error {
	gaps := ParkingGaps(config)
	if len(gaps) == 0 {
		return nil
	}
	gap := gaps[0]
	direction := "to and from parking"
	switch {
	case gap.ToParking:
		direction = "from parking"
	case gap.FromParking:
		direction = "to parking"
	}
	return fmt.Errorf("parking routes: %d berths have a gap, the first is berth %s of station %s, with no %s route %s", len(gaps), gap.BerthID, gap.StationID, gap.Class, direction)
}
