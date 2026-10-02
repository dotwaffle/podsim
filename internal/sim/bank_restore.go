package sim

import (
	"errors"
	"fmt"
)

// checkSavedBankRoutes runs before either restore tier can discard a route.
func checkSavedBankRoutes(input RestoreStateInput) error {
	if !input.Network.hasStationBanks() {
		return nil
	}
	graph := newRouteGraph(input.Network)
	if graph.banks.err != nil {
		return graph.banks.err
	}
	for _, pod := range input.State.Pods {
		if err := checkSavedBankRoute(input.Network, graph, pod.Route, pod.DestinationStation, pod.Destination, pod.Origin); err != nil {
			return fmt.Errorf("pod %q bank route: %w", pod.ID, err)
		}
	}
	for _, trip := range input.State.Waiting {
		if err := checkSavedBankRoute(input.Network, graph, trip.Route, trip.Request.To, "", ""); err != nil {
			return fmt.Errorf("request %d bank route: %w", trip.Request.ID, err)
		}
	}
	return nil
}

func checkSavedBankRoute(n Network, graph routeGraph, indexes []int, stationID, destination, origin string) error {
	if len(indexes) == 0 {
		return nil
	}
	// Missing and invalid lane indexes retain the existing demotion rules.
	for _, lane := range indexes {
		if lane < 0 || lane >= len(n.Lanes) {
			return nil
		}
	}
	for i, laneIndex := range indexes {
		lane := n.Lanes[laneIndex]
		owner := graph.banks.lanes[laneIndex]
		if owner < 0 || lane.StationRole == StationThroughRole {
			continue
		}
		bank := graph.banks.banks[owner]
		station := n.Stations[bank.station]
		if i > 0 {
			previous := indexes[i-1]
			if graph.banks.lanes[previous] != owner || n.Lanes[previous].StationRole != lane.StationRole {
				if lane.StationRole == StationBerthAccessRole && graph.edges[laneIndex].from != bank.entry {
					return errors.New("arrival starts inside a bank")
				}
				if lane.StationRole == StationDepartureRole {
					return errors.New("departure starts after the retained origin")
				}
			}
		}
		if i+1 < len(indexes) {
			next := indexes[i+1]
			if graph.banks.lanes[next] != owner || n.Lanes[next].StationRole != lane.StationRole {
				if lane.StationRole == StationBerthAccessRole {
					return errors.New("retained arrival escapes its bank")
				}
				if lane.StationRole == StationDepartureRole && graph.edges[laneIndex].to != bank.exit {
					return errors.New("departure leaves before its bank exit")
				}
			}
		}
		if lane.StationRole == StationBerthAccessRole {
			if station.ID != stationID {
				return errors.New("arrival uses another station's bank")
			}
			if destination != "" {
				selected, ok := station.bankForBerth(destination)
				if !ok || selected.ID != station.Banks[bank.bank].ID {
					return errors.New("arrival and destination use different banks")
				}
			}
			if i+1 < len(indexes) && graph.berthStations[graph.edges[laneIndex].to] >= 0 {
				return errors.New("arrival crosses an intermediate berth")
			}
		}
		if lane.StationRole == StationDepartureRole && i == 0 && origin != "" {
			selected, ok := station.bankForBerth(origin)
			if !ok || selected.ID != station.Banks[bank.bank].ID {
				return errors.New("departure and origin use different banks")
			}
		}
	}
	station, exists := n.Station(stationID)
	if !exists || station.Banks == nil {
		return nil
	}
	end := n.Lanes[indexes[len(indexes)-1]].To
	if destination != "" {
		berth, ok := station.berth(destination)
		if !ok {
			return errors.New("unknown bank destination berth")
		}
		if end != berth.Node && end != station.berthEntry(berth) {
			return errors.New("retained route ends outside its destination bank")
		}
	} else if !station.isEntry(end) {
		return errors.New("berthless route does not end at a bank entry")
	}
	return nil
}
