package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

// referenceStationLaneRoles is inferStationLaneRoles before the shared
// route work and the through lane index: it scans the lanes for the
// through lanes of each station, and each route search has its own work.
func referenceStationLaneRoles(n *Network) {
	graph := newRouteGraph(*n)
	var grouped map[string]bool
	for _, group := range n.ManeuverGroups {
		if grouped == nil {
			grouped = make(map[string]bool)
		}
		for _, id := range group.LaneIDs {
			grouped[id] = true
		}
	}
	forbidden := n.stationForbidden()
	for _, station := range n.Stations {
		if station.Banks != nil {
			continue
		}
		for _, lane := range n.Lanes {
			if lane.From == station.Entry && lane.To == station.Exit {
				setStationLaneRole(n, graph, grouped, lane.ID, station.ID, StationThroughRole)
			}
		}
		for _, berth := range station.Berths {
			berthForbidden := n.depotForbidden(forbidden, berth.ID)
			arrival, err := n.routeIndexed(networkRouteInput{class: topologyClass, from: station.Entry, to: berth.Node, forbidden: berthForbidden}, graph)
			if err == nil {
				for _, lane := range arrival {
					setStationLaneRole(n, graph, grouped, lane.ID, station.ID, StationBerthAccessRole)
				}
			}
			departure, err := n.routeIndexed(networkRouteInput{class: topologyClass, from: berth.Node, to: station.Exit, forbidden: berthForbidden}, graph)
			if err == nil {
				for _, lane := range departure {
					setStationLaneRole(n, graph, grouped, lane.ID, station.ID, StationDepartureRole)
				}
			}
		}
	}
}

// withoutRoles returns a copy of a network with no lane roles, so that
// the inference sets each role.
func withoutRoles(network Network) Network {
	owned := network.clone()
	for index := range owned.Lanes {
		owned.Lanes[index].StationID, owned.Lanes[index].StationRole = "", ""
	}
	return owned
}

// TestStationLaneRolesMatchReference checks that the inferred lane roles
// are the roles of the reference, on the scenario networks, the compact
// station, the bank example and a depot, with and without their roles.
func TestStationLaneRolesMatchReference(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	networks["compact station"], _ = loadCompactStation(t)
	networks["bank example"] = BankExample()
	networks["depot"] = testDepotNetwork()
	// A second through lane of a station gets the role after the first.
	ring := networks["ring 4x12"].clone()
	for _, lane := range ring.Lanes {
		if lane.StationRole == StationThroughRole {
			extra := lane
			extra.ID, extra.StationID, extra.StationRole = lane.ID+"-2", "", ""
			ring.Lanes = append(ring.Lanes, extra)
		}
	}
	networks["two through lanes"] = ring
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		for _, network := range []Network{networks[name], withoutRoles(networks[name])} {
			want, got := network.clone(), network.clone()
			referenceStationLaneRoles(&want)
			inferStationLaneRoles(&got)
			if !reflect.DeepEqual(got.Lanes, want.Lanes) {
				t.Errorf("%s: the lane roles differ from the reference", name)
			}
		}
	}
}
