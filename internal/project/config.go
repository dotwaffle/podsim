// Package project defines the portable Podsim project format.
package project

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	currentVersion = 1
	maxIDLength    = 64
	maxNameLength  = 80
	maxPods        = 200
	maxBerths      = 200
	maxStations    = 100
	maxNodes       = 2000
	maxLanes       = 4000
)

// DemandConfig controls deterministic arrivals per simulated minute.
type DemandConfig struct {
	Enabled     bool   `json:"enabled"`
	PerMinute   int    `json:"perMinute"`
	Pattern     string `json:"pattern"`
	Seed        uint64 `json:"seed"`
	Destination string `json:"destination,omitempty"`
}

// Config is the versioned, portable scenario configuration.
type Config struct {
	Version        int             `json:"version"`
	Name           string          `json:"name"`
	Network        sim.Network     `json:"network"`
	Fleet          []sim.Placement `json:"fleet"`
	Demand         DemandConfig    `json:"demand"`
	Redistribution bool            `json:"redistribution"`
}

// Default returns the supplied example project.
func Default() Config {
	return Config{
		Version: currentVersion,
		Name:    "Podsim example",
		Network: sim.Example(),
		Fleet: []sim.Placement{
			{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
			{ID: "02", StationID: "garden", BerthID: "garden-1"},
		},
		Demand: DemandConfig{PerMinute: 2, Pattern: "balanced", Seed: 1},
	}
}

// Validate checks format bounds and confirms that the scenario can start.
func Validate(config Config) error {
	if config.Version != currentVersion {
		return fmt.Errorf("project version must be %d", currentVersion)
	}
	if strings.TrimSpace(config.Name) == "" || len(config.Name) > maxNameLength {
		return fmt.Errorf("project name must contain 1 to %d characters", maxNameLength)
	}
	if len(config.Network.Nodes) == 0 || len(config.Network.Nodes) > maxNodes {
		return fmt.Errorf("network must contain 1 to %d nodes", maxNodes)
	}
	if len(config.Network.Lanes) == 0 || len(config.Network.Lanes) > maxLanes {
		return fmt.Errorf("network must contain 1 to %d lanes", maxLanes)
	}
	if len(config.Network.Stations) < 2 || len(config.Network.Stations) > maxStations {
		return fmt.Errorf("network must contain 2 to %d stations", maxStations)
	}
	if len(config.Fleet) == 0 || len(config.Fleet) > maxPods {
		return fmt.Errorf("fleet must contain 1 to %d pods", maxPods)
	}
	if err := validateNames(config); err != nil {
		return err
	}
	if err := ValidateDemand(config.Demand, config.Network); err != nil {
		return err
	}
	if _, err := sim.NewFleet(config.Network, config.Fleet); err != nil {
		return fmt.Errorf("invalid project scenario: %w", err)
	}
	passenger := PassengerStations(config.Network)
	if len(passenger) < 2 {
		return errors.New("network needs at least two passenger stations")
	}
	adjacent := make(map[string][]string, len(config.Network.Nodes))
	for _, lane := range config.Network.Lanes {
		adjacent[lane.From] = append(adjacent[lane.From], lane.To)
	}
	for _, from := range passenger {
		for _, origin := range from.Berths {
			reachable := directedReachable(adjacent, origin.Node)
			for _, to := range passenger {
				if from.ID == to.ID {
					continue
				}
				for _, destination := range to.Berths {
					if !reachable[destination.Node] {
						return fmt.Errorf("passenger route %q berth %q to %q berth %q: %w", from.ID, origin.ID, to.ID, destination.ID, sim.ErrUnreachable)
					}
				}
			}
		}
	}
	return nil
}

func directedReachable(adjacent map[string][]string, start string) map[string]bool {
	reachable := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[current] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reachable
}

func validateNames(config Config) error {
	validID := func(id string) bool { return id != "" && len(id) <= maxIDLength }
	for _, node := range config.Network.Nodes {
		if !validID(node.ID) {
			return fmt.Errorf("node ID must contain 1 to %d characters", maxIDLength)
		}
	}
	for _, lane := range config.Network.Lanes {
		if !validID(lane.ID) || !validID(lane.From) || !validID(lane.To) {
			return fmt.Errorf("lane IDs must contain 1 to %d characters", maxIDLength)
		}
	}
	for _, station := range config.Network.Stations {
		if !validID(station.ID) || !validID(station.Entry) || !validID(station.Exit) {
			return fmt.Errorf("station IDs must contain 1 to %d characters", maxIDLength)
		}
		if strings.TrimSpace(station.Name) == "" || len(station.Name) > maxNameLength {
			return fmt.Errorf("station name must contain 1 to %d characters", maxNameLength)
		}
		if len(station.Berths) == 0 || len(station.Berths) > maxBerths {
			return fmt.Errorf("station %q must contain 1 to %d berths", station.ID, maxBerths)
		}
		for _, berth := range station.Berths {
			if !validID(berth.ID) || !validID(berth.Node) {
				return fmt.Errorf("berth IDs must contain 1 to %d characters", maxIDLength)
			}
		}
	}
	for _, placement := range config.Fleet {
		if !validID(placement.ID) || !validID(placement.StationID) || placement.BerthID != "" && !validID(placement.BerthID) {
			return fmt.Errorf("fleet IDs must contain 1 to %d characters", maxIDLength)
		}
	}
	return nil
}

// ValidateDemand checks demand settings against the active passenger stations.
func ValidateDemand(config DemandConfig, network sim.Network) error {
	if config.PerMinute < 1 || config.PerMinute > 120 {
		return errors.New("demand rate must be 1 to 120 orders per simulated minute")
	}
	if config.Pattern != "balanced" && config.Pattern != "market" && config.Pattern != "destination" {
		return errors.New("demand pattern must be balanced, market, or destination")
	}
	if len(config.Destination) > maxIDLength {
		return fmt.Errorf("demand destination must contain at most %d characters", maxIDLength)
	}
	if config.Pattern != "destination" {
		return nil
	}
	station, ok := network.Station(config.Destination)
	if !ok {
		return fmt.Errorf("unknown demand destination %q", config.Destination)
	}
	if station.ParkingOnly {
		return errors.New("demand destination must be a passenger station")
	}
	return nil
}

// PassengerStations returns the passenger stations in network order.
func PassengerStations(network sim.Network) []sim.Station {
	stations := make([]sim.Station, 0, len(network.Stations))
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			stations = append(stations, station)
		}
	}
	return stations
}

// Clone returns a detached project configuration.
func Clone(config Config) Config {
	clone := config
	clone.Network = CloneNetwork(config.Network)
	clone.Fleet = append([]sim.Placement(nil), config.Fleet...)
	return clone
}

// CloneNetwork returns detached network slices.
func CloneNetwork(network sim.Network) sim.Network {
	clone := network
	clone.Nodes = append([]sim.Node(nil), network.Nodes...)
	clone.Lanes = append([]sim.Lane(nil), network.Lanes...)
	for i := range clone.Lanes {
		if clone.Lanes[i].Control != nil {
			clone.Lanes[i].Control = new(*clone.Lanes[i].Control)
		}
	}
	clone.Stations = append([]sim.Station(nil), network.Stations...)
	for i := range clone.Stations {
		clone.Stations[i].Berths = append([]sim.Berth(nil), network.Stations[i].Berths...)
	}
	return clone
}
