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

// checkSavedBankRoute checks a retained route against the station banks.
// It checks each bank lane in route order, and then the end of the route at
// its station. The first error is the refusal, so the order is part of the
// save format.
func checkSavedBankRoute(n Network, graph routeGraph, indexes []int, stationID, destination, origin string) error {
	// Missing and invalid lane indexes retain the existing demotion rules.
	if len(indexes) == 0 || !savedLaneIndexesValid(indexes, len(n.Lanes)) {
		return nil
	}
	route := savedBankRoute{n: n, graph: graph, indexes: indexes, stationID: stationID, destination: destination, origin: origin}
	for i := range indexes {
		if err := route.checkLane(i); err != nil {
			return err
		}
	}
	return route.checkEnd()
}

func savedLaneIndexesValid(indexes []int, lanes int) bool {
	for _, lane := range indexes {
		if lane < 0 || lane >= lanes {
			return false
		}
	}
	return true
}

// savedBankRoute is a retained route and the station, destination berth,
// and origin berth that it was saved with.
type savedBankRoute struct {
	n                              Network
	graph                          routeGraph
	indexes                        []int
	stationID, destination, origin string
}

// savedBankLane is a lane of a retained route that a bank owns.
type savedBankLane struct {
	position int // the position of the lane in the route
	index    int // the index of the lane in the network
	lane     Lane
	owner    int // the index of the bank in the bank index
	bank     bankTopology
	station  Station // the station of the bank
}

// checkLane checks the lane at position i of the route, if a bank owns it
// and it is not a through lane. It checks where the bank part of the route
// starts and ends, and then the arrival or departure fields.
func (r savedBankRoute) checkLane(i int) error {
	laneIndex := r.indexes[i]
	lane, owner := r.n.Lanes[laneIndex], r.graph.banks.lanes[laneIndex]
	if owner < 0 || lane.StationRole == StationThroughRole {
		return nil
	}
	bank := r.graph.banks.banks[owner]
	l := savedBankLane{position: i, index: laneIndex, lane: lane, owner: owner, bank: bank, station: r.n.Stations[bank.station]}
	if err := r.checkPartStart(l); err != nil {
		return err
	}
	if err := r.checkPartEnd(l); err != nil {
		return err
	}
	if lane.StationRole == StationBerthAccessRole {
		return r.checkArrival(l)
	}
	if lane.StationRole == StationDepartureRole {
		return r.checkDepartureOrigin(l)
	}
	return nil
}

// samePart reports whether the lane at the network index other is in the
// bank part of l. A bank part is a run of lanes with one bank and one
// station role.
func (r savedBankRoute) samePart(l savedBankLane, other int) bool {
	return r.graph.banks.lanes[other] == l.owner && r.n.Lanes[other].StationRole == l.lane.StationRole
}

// checkPartStart checks a lane that starts a bank part after another lane.
// An arrival starts at the bank entry, and a retained departure starts at
// the start of the route.
func (r savedBankRoute) checkPartStart(l savedBankLane) error {
	if l.position == 0 || r.samePart(l, r.indexes[l.position-1]) {
		return nil
	}
	if l.lane.StationRole == StationBerthAccessRole && r.graph.edges[l.index].from != l.bank.entry {
		return errors.New("arrival starts inside a bank")
	}
	if l.lane.StationRole == StationDepartureRole {
		return errors.New("departure starts after the retained origin")
	}
	return nil
}

// checkPartEnd checks a lane that ends a bank part before another lane. A
// retained arrival ends at the end of the route, and a departure ends at
// the bank exit.
func (r savedBankRoute) checkPartEnd(l savedBankLane) error {
	if l.position+1 == len(r.indexes) || r.samePart(l, r.indexes[l.position+1]) {
		return nil
	}
	if l.lane.StationRole == StationBerthAccessRole {
		return errors.New("retained arrival escapes its bank")
	}
	if l.lane.StationRole == StationDepartureRole && r.graph.edges[l.index].to != l.bank.exit {
		return errors.New("departure leaves before its bank exit")
	}
	return nil
}

// checkArrival checks that an arrival lane is in a bank of the route
// station, in the bank of the destination berth, and does not end at a berth
// before the end of the route.
func (r savedBankRoute) checkArrival(l savedBankLane) error {
	if l.station.ID != r.stationID {
		return errors.New("arrival uses another station's bank")
	}
	if r.destination != "" && !berthInBank(l.station, r.destination, l.bank) {
		return errors.New("arrival and destination use different banks")
	}
	if l.position+1 < len(r.indexes) && r.graph.berthStations[r.graph.edges[l.index].to] >= 0 {
		return errors.New("arrival crosses an intermediate berth")
	}
	return nil
}

// checkDepartureOrigin checks that a departure lane at the start of the
// route is in the bank of the origin berth.
func (r savedBankRoute) checkDepartureOrigin(l savedBankLane) error {
	if l.position == 0 && r.origin != "" && !berthInBank(l.station, r.origin, l.bank) {
		return errors.New("departure and origin use different banks")
	}
	return nil
}

// berthInBank reports whether station has the berth berthID in bank.
func berthInBank(station Station, berthID string, bank bankTopology) bool {
	selected, ok := station.bankForBerth(berthID)
	return ok && selected.ID == station.Banks[bank.bank].ID
}

// checkEnd checks the end of the route at a station with banks. A route to
// a berth ends at the berth or at the entry of its bank. A route with no
// berth ends at a bank entry.
func (r savedBankRoute) checkEnd() error {
	station, exists := r.n.Station(r.stationID)
	if !exists || station.Banks == nil {
		return nil
	}
	end := r.n.Lanes[r.indexes[len(r.indexes)-1]].To
	if r.destination == "" {
		if !station.isEntry(end) {
			return errors.New("berthless route does not end at a bank entry")
		}
		return nil
	}
	berth, ok := station.berth(r.destination)
	if !ok {
		return errors.New("unknown bank destination berth")
	}
	if end != berth.Node && end != station.berthEntry(berth) {
		return errors.New("retained route ends outside its destination bank")
	}
	return nil
}
