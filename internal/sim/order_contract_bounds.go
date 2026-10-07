package sim

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

// The explicit native contract uses the existing project geometry budgets.
const (
	expressMaxPods          = 600
	expressMaxNodes         = 12000
	expressMaxLanes         = 20000
	expressMaxStations      = 600
	expressMaxBerths        = 200
	expressMaxCoordinate    = 100000.0
	expressMaxNodeLanes     = 64
	expressMaxJunctionPairs = 250000
	expressMaxNetworkBlocks = 64000
)

func boundedContractID(id string) bool {
	return id != "" && len(id) <= 64 && utf8.ValidString(id)
}
func contractPointFits(p Point) bool {
	return math.Abs(p.X) <= expressMaxCoordinate && math.Abs(p.Y) <= expressMaxCoordinate
}

func validateContractFleetBounds(network Network, placements []Placement, contract OrderContract) error {
	if err := ValidateOrderContract(contract); err != nil {
		return err
	}
	if contract == "" {
		return nil
	}
	if len(placements) < 1 || len(placements) > expressMaxPods || len(network.Nodes) < 1 || len(network.Nodes) > expressMaxNodes || len(network.Lanes) < 1 || len(network.Lanes) > expressMaxLanes || len(network.Stations) < 2 || len(network.Stations) > expressMaxStations {
		return errors.New("the Express fleet or network exceeds project bounds")
	}
	for _, p := range placements {
		if !boundedContractID(p.ID) || !boundedContractID(p.StationID) || p.BerthID != "" && !boundedContractID(p.BerthID) {
			return errors.New("the Express placement ID exceeds bounds")
		}
	}
	if err := validateContractNetworkRecords(network); err != nil {
		return err
	}
	return validateContractGeometryBudget(network)
}

// validateContractNetworkRecords checks the nodes, then the lanes, then
// each station with its berths and banks.
func validateContractNetworkRecords(network Network) error {
	nodes, err := contractNodeSet(network.Nodes)
	if err != nil {
		return err
	}
	for _, l := range network.Lanes {
		if !contractLaneValid(l, nodes) {
			return errors.New("invalid Express lane record")
		}
	}
	for _, st := range network.Stations {
		if err := validateContractStation(st, nodes); err != nil {
			return err
		}
	}
	return nil
}

func contractNodeSet(list []Node) (map[string]bool, error) {
	nodes := make(map[string]bool, len(list))
	for _, n := range list {
		if !boundedContractID(n.ID) || !contractPointFits(n.Position) || nodes[n.ID] {
			return nil, errors.New("invalid Express node")
		}
		nodes[n.ID] = true
	}
	return nodes, nil
}

func contractLaneValid(l Lane, nodes map[string]bool) bool {
	return boundedContractID(l.ID) && nodes[l.From] && nodes[l.To] && contractSeparationGroupValid(l.SeparationGroup) &&
		(l.StationID == "" || boundedContractID(l.StationID)) && contractClassesValid(l.VehicleClasses) &&
		(l.Control == nil || contractPointFits(*l.Control))
}

// validateContractStation checks the station record, then each berth, then
// each bank with its berth IDs.
func validateContractStation(st Station, nodes map[string]bool) error {
	if !contractStationValid(st) {
		return errors.New("invalid Express station record")
	}
	for _, b := range st.Berths {
		if !contractBerthValid(b, nodes) {
			return errors.New("invalid Express berth record")
		}
	}
	for _, b := range st.Banks {
		if err := validateContractBank(b); err != nil {
			return err
		}
	}
	return nil
}

func contractStationValid(st Station) bool {
	return boundedContractID(st.ID) && boundedContractID(st.Entry) && boundedContractID(st.Exit) &&
		len(st.Name) <= 80 && strings.TrimSpace(st.Name) != "" && utf8.ValidString(st.Name) &&
		len(st.Berths) >= 1 && len(st.Berths) <= expressMaxBerths && len(st.Banks) <= MaxStationBanks && contractClassesValid(st.VehicleClasses)
}

func contractBerthValid(b Berth, nodes map[string]bool) bool {
	return boundedContractID(b.ID) && nodes[b.Node] && contractSeparationGroupValid(b.SeparationGroup) && contractClassesValid(b.VehicleClasses)
}

func validateContractBank(b StationBank) error {
	if !boundedContractID(b.ID) || !boundedContractID(b.Entry) || !boundedContractID(b.Exit) || len(b.BerthIDs) > expressMaxBerths {
		return errors.New("invalid Express bank record")
	}
	for _, id := range b.BerthIDs {
		if !boundedContractID(id) {
			return errors.New("invalid Express bank berth ID")
		}
	}
	return nil
}

func contractSeparationGroupValid(group string) bool {
	return len(group) <= 64 && utf8.ValidString(group)
}

func contractClassesValid(classes ClassSet) bool {
	return classes & ^allClassBits == 0
}

func validateContractGeometryBudget(network Network) error {
	type lanePath struct {
		from, to string
		curved   bool
		control  Point
	}
	paths := make(map[lanePath]bool, len(network.Lanes))
	degrees := make(map[string]int, len(network.Nodes))
	for _, lane := range network.Lanes {
		key := lanePath{from: lane.From, to: lane.To}
		if lane.Control != nil {
			key.curved, key.control = true, *lane.Control
		}
		if paths[key] {
			return errors.New("the Express lanes duplicate a path")
		}
		paths[key] = true
		degrees[lane.From]++
		degrees[lane.To]++
		if degrees[lane.From] > expressMaxNodeLanes || degrees[lane.To] > expressMaxNodeLanes {
			return errors.New("the Express node lane degree exceeds project bound")
		}
	}
	pairs := 0
	for _, count := range network.JunctionPairs() {
		pairs += count
		if pairs > expressMaxJunctionPairs {
			return errors.New("the Express junction pairs exceed project bound")
		}
	}
	blocks := 0
	for _, count := range network.LaneBlocks() {
		blocks += count
		if blocks > expressMaxNetworkBlocks {
			return errors.New("the Express lane blocks exceed project bound")
		}
	}
	return nil
}
