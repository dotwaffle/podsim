package editormodel

import "github.com/dotwaffle/podsim/internal/sim"

// draftClearance is the lane clearance index of one draft network, so the
// editor applies the lane minimums and the pair clearances of the server.
// Its zero value keeps the class bounds. grouped holds the lanes of the
// maneuver groups, which have no minimum length.
type draftClearance struct {
	index   sim.LaneClearance
	grouped map[string]string
}

// newDraftClearance returns the index of a draft network. A draft with an
// invalid class set gets the zero index.
func newDraftClearance(network any) draftClearance {
	clearance := draftClearance{grouped: draftManeuverLanes(network)}
	if typed, ok := draftClearanceNetwork(network); ok {
		clearance.index = sim.NewLaneClearance(typed)
	}
	return clearance
}

// draftManeuverLanes returns the maneuver group ID of each group lane of a
// draft, by lane ID. The first group of a lane wins.
func draftManeuverLanes(network any) map[string]string {
	groups := make(map[string]string)
	for _, group := range items(member(network, "maneuverGroups")) {
		for _, lane := range items(member(group, "laneIDs")) {
			if _, found := groups[text(lane)]; !found {
				groups[text(lane)] = text(member(group, "id"))
			}
		}
	}
	return groups
}

// hasManeuverGroups reports whether a draft has a maneuver group. Such a
// draft gets the full validation of the server.
func hasManeuverGroups(network any) bool {
	return len(items(member(network, "maneuverGroups"))) > 0
}

// draftClearanceNetwork copies the members of a draft that the index reads.
// The depots let the index find the chain lanes.
func draftClearanceNetwork(network any) (sim.Network, bool) {
	var typed sim.Network
	for _, node := range items(member(network, "nodes")) {
		position := member(node, "position")
		typed.Nodes = append(typed.Nodes, sim.Node{ID: text(member(node, "id")), Position: sim.Point{X: number(member(position, "x")), Y: number(member(position, "y"))}})
	}
	for _, item := range items(member(network, "lanes")) {
		lane, ok := draftSimLane(item)
		if !ok {
			return sim.Network{}, false
		}
		typed.Lanes = append(typed.Lanes, lane)
	}
	for _, item := range items(member(network, "stations")) {
		classes, valid := draftClassSet(item)
		if !valid {
			return sim.Network{}, false
		}
		station := sim.Station{ID: text(member(item, "id")), VehicleClasses: classes}
		for _, berth := range items(member(item, "berths")) {
			classes, valid := draftClassSet(berth)
			if !valid {
				return sim.Network{}, false
			}
			station.Berths = append(station.Berths, sim.Berth{ID: text(member(berth, "id")), Node: text(member(berth, "node")), VehicleClasses: classes})
		}
		typed.Stations = append(typed.Stations, station)
	}
	for _, item := range items(member(network, "depots")) {
		depot := sim.Depot{Station: text(member(item, "station"))}
		for _, lane := range items(member(item, "lanes")) {
			var ids []string
			for _, id := range items(member(lane, "berthIDs")) {
				ids = append(ids, text(id))
			}
			depot.Lanes = append(depot.Lanes, sim.DepotLane{BerthIDs: ids})
		}
		typed.Depots = append(typed.Depots, depot)
	}
	return typed, true
}

// draftSimLane returns the members of a draft lane that the index reads. ok
// is false for an invalid class set.
func draftSimLane(item any) (sim.Lane, bool) {
	classes, valid := draftClassSet(item)
	lane := sim.Lane{ID: text(member(item, "id")), From: text(member(item, "from")), To: text(member(item, "to")),
		SpeedLimit: number(member(item, "speedLimit")), VehicleClasses: classes}
	if control := member(item, "control"); control != nil {
		lane.Control = &sim.Point{X: number(member(control, "x")), Y: number(member(control, "y"))}
	}
	return lane, valid
}

// laneMinimum returns the minimum length of a draft lane. A lane of a
// maneuver group has none, and the server checks only that its length is
// positive. A lane with an invalid class set needs 2*sim.Clearance.
func (c draftClearance) laneMinimum(lane any) float64 {
	if _, grouped := c.grouped[text(member(lane, "id"))]; grouped {
		return 0
	}
	typed, valid := draftSimLane(lane)
	if !valid {
		return 2 * sim.Clearance
	}
	return c.index.LaneMinimum(typed)
}

// pairPieces divides two nonincident draft lanes with the given lengths into
// pieces with one clearance each, as the bank audit of the server does. A
// lane with an invalid class set counts as a small lane.
func (c draftClearance) pairPieces(first, second any, firstLength, secondLength float64) []sim.PairPiece {
	a, _ := draftSimLane(first)
	b, _ := draftSimLane(second)
	return c.index.PairPieces(a, b, firstLength, secondLength)
}

// draftClassMinimum returns the class bound of the minimum length of a
// draft lane. A lane with an invalid class set needs 2*sim.Clearance.
func draftClassMinimum(lane any) float64 {
	classes, valid := draftClassSet(lane)
	if !valid {
		return 2 * sim.Clearance
	}
	return sim.LaneMinimumLength(sim.Lane{VehicleClasses: classes})
}

func hasLargeGeometry(network any) bool {
	for _, lane := range items(member(network, "lanes")) {
		if draftClassMinimum(lane) > 2*sim.Clearance {
			return true
		}
	}
	for _, station := range items(member(network, "stations")) {
		classes, valid := draftClassSet(station)
		if !valid {
			continue
		}
		for _, berth := range items(member(station, "berths")) {
			allowed, valid := draftClassSet(berth)
			if valid && (classes.Allows("group") && allowed.Allows("group") || classes.Allows("express") && allowed.Allows("express")) {
				return true
			}
		}
	}
	return false
}

func draftGeometryClassesValid(network any) bool {
	for _, lane := range items(member(network, "lanes")) {
		if _, valid := draftClassSet(lane); !valid {
			return false
		}
	}
	for _, station := range items(member(network, "stations")) {
		if _, valid := draftClassSet(station); !valid {
			return false
		}
		for _, berth := range items(member(station, "berths")) {
			if _, valid := draftClassSet(berth); !valid {
				return false
			}
		}
	}
	return true
}
