package project

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// validateCharacters checks that each ID of config uses only
// sim.IDCharacters, and that each name is UTF-8 text without a control
// character. It checks the project name, the network, the fleet, the
// demand profiles, the rail services, the express services and the
// demand settings, in that order.
func validateCharacters(config Config) error {
	if err := checkName(config.Name); err != nil {
		return err
	}
	if err := ValidateNetworkText(config.Network, config.ExpressServices); err != nil {
		return err
	}
	ids := make([]string, 0, 3*len(config.Fleet)+3)
	for _, placement := range config.Fleet {
		ids = append(ids, placement.ID, placement.StationID, placement.BerthID)
	}
	if err := checkIDs(ids...); err != nil {
		return err
	}
	if err := checkProfileText(config.DemandProfiles); err != nil {
		return err
	}
	ids = ids[:0]
	for _, arrival := range config.RailArrivals {
		ids = append(ids, arrival.ID, arrival.Station)
		for _, destination := range arrival.Destinations {
			ids = append(ids, destination.Station)
		}
	}
	for _, departure := range config.RailDepartures {
		ids = append(ids, departure.ID, departure.Station)
		for _, origin := range departure.Origins {
			ids = append(ids, origin.Station)
		}
	}
	ids = append(ids, config.Demand.Destination, config.Demand.Profile, config.Demand.Band)
	return checkIDs(ids...)
}

// ValidateNetworkText checks that each ID of network and of services uses
// only sim.IDCharacters, and that each station name is UTF-8 text without
// a control character. It checks the nodes, the lanes, the stations with
// their berths and banks, and then the services. A topology decode uses it
// for the network and the services that the server sends.
func ValidateNetworkText(network sim.Network, services []sim.ExpressService) error {
	for _, node := range network.Nodes {
		if err := checkIDs(node.ID); err != nil {
			return err
		}
	}
	for _, lane := range network.Lanes {
		if err := checkIDs(lane.ID, lane.From, lane.To, lane.StationID, lane.SeparationGroup); err != nil {
			return err
		}
	}
	for _, station := range network.Stations {
		if err := checkStationText(station); err != nil {
			return err
		}
	}
	for _, service := range services {
		if err := checkIDs(service.ID, service.From, service.To); err != nil {
			return err
		}
	}
	return nil
}

func checkStationText(station sim.Station) error {
	if err := checkIDs(station.ID, station.Entry, station.Exit); err != nil {
		return err
	}
	if err := checkName(station.Name); err != nil {
		return err
	}
	for _, berth := range station.Berths {
		if err := checkIDs(berth.ID, berth.Node, berth.SeparationGroup); err != nil {
			return err
		}
	}
	for _, bank := range station.Banks {
		if err := checkIDs(bank.ID, bank.Entry, bank.Exit); err != nil {
			return err
		}
		if err := checkIDs(bank.BerthIDs...); err != nil {
			return err
		}
	}
	return nil
}

func checkProfileText(profiles []DemandProfile) error {
	for _, profile := range profiles {
		if err := checkIDs(profile.ID); err != nil {
			return err
		}
		if err := checkName(profile.Name); err != nil {
			return err
		}
		for _, band := range profile.Bands {
			if err := checkIDs(band.ID); err != nil {
				return err
			}
			if err := checkName(band.Name); err != nil {
				return err
			}
		}
		for _, flow := range profile.Flows {
			if err := checkIDs(flow.From, flow.To); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkIDs refuses the first of ids with a character that is not one of
// sim.IDCharacters.
func checkIDs(ids ...string) error {
	for _, id := range ids {
		if !sim.ValidIDText(id) {
			return fmt.Errorf("ID %s has a character other than A-Z, a-z, 0-9, '.', '+' or '-'", quoteID(id))
		}
	}
	return nil
}

// checkName refuses a name that is not UTF-8 or that has a control
// character.
func checkName(name string) error {
	if !sim.ValidText(name) {
		return fmt.Errorf("name %s has a control character or is not UTF-8", quoteID(name))
	}
	return nil
}
