package project

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// ParkingGaps returns the parking gaps of the network of config for the
// classes of its fleet, in the order in which the classes first appear in
// the fleet. A pod with no class is a Legacy pod. See sim.Network.ParkingGaps.
func ParkingGaps(config Config) []sim.ParkingGap {
	return config.Network.ParkingGaps(fleetClasses(config.Fleet))
}

// fleetClasses returns the classes of fleet, in the order in which they
// first appear. A pod with no class is a Legacy pod.
func fleetClasses(fleet []sim.Placement) []sim.VehicleClass {
	var classes []sim.VehicleClass
	for _, placement := range fleet {
		class := placement.Class
		if class == "" {
			class = sim.LegacyClass
		}
		if !slices.Contains(classes, class) {
			classes = append(classes, class)
		}
	}
	return classes
}

// ParkingRoutes keeps the last result of the parking route check, for a
// caller that validates projects with the same routes again and again, such
// as the editor after each node move. The zero value keeps no result. Use it
// from one goroutine at a time.
//
// The key of the result is all that the check reads: the classes of the
// fleet in order, and the network without the node positions and the lane
// control points. The check asks only whether a route exists. A position or
// a control point changes the cost of a lane, and not the lanes that a
// route can use, while the route costs cannot overflow (see
// sim.Network.RouteCostsBounded). A network with costs that can overflow
// gets a new check, and its result is not kept, so the kept result and the
// network of each call both have bounded costs. Each other member of the
// network is in the key, also a member that the check does not read.
type ParkingRoutes struct {
	topology sim.Network
	classes  []sim.VehicleClass
	gaps     []sim.ParkingGap
	kept     bool
}

// routeTopology returns network without the node positions and the lane
// control points. The result shares the other members with network.
func routeTopology(network sim.Network) sim.Network {
	topology := network
	topology.Nodes = slices.Clone(network.Nodes)
	for index := range topology.Nodes {
		topology.Nodes[index].Position = sim.Point{}
	}
	topology.Lanes = slices.Clone(network.Lanes)
	for index := range topology.Lanes {
		topology.Lanes[index].Control = nil
	}
	return topology
}

// Gaps returns ParkingGaps(config). It reads the kept result when the key
// of config is the key of that result and the route costs of config.Network
// are bounded. The kept key shares members with config.Network, so the
// caller must not change that network later. The caller can change the
// result.
func (p *ParkingRoutes) Gaps(config Config) []sim.ParkingGap {
	classes := fleetClasses(config.Fleet)
	if !config.Network.RouteCostsBounded() {
		return config.Network.ParkingGaps(classes)
	}
	topology := routeTopology(config.Network)
	if !p.kept || !slices.Equal(p.classes, classes) || !reflect.DeepEqual(p.topology, topology) {
		p.topology, p.classes, p.gaps, p.kept = topology, classes, config.Network.ParkingGaps(classes), true
	}
	return slices.Clone(p.gaps)
}

// validateParkingRoutes refuses a project with a parking gap. The error
// gives the count of the berths with a gap and names the first.
func validateParkingRoutes(config Config) error {
	return parkingRefusal(ParkingGaps(config))
}

// parkingRefusal returns the refusal of validateParkingRoutes for the gaps
// of a project.
func parkingRefusal(gaps []sim.ParkingGap) error {
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
