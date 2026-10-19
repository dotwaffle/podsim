package sim

// scopedNetwork returns a copy of network with prefix on each ID and offset
// added to each position and control point. Each part of a joined network
// keeps its own stations, depots, and maneuver groups.
func scopedNetwork(network Network, prefix string, offset Point) Network {
	id := func(value string) string {
		if value == "" {
			return ""
		}
		return prefix + value
	}
	ids := func(values []string) []string {
		var scoped []string
		for _, value := range values {
			scoped = append(scoped, id(value))
		}
		return scoped
	}
	move := func(point Point) Point { return Point{X: point.X + offset.X, Y: point.Y + offset.Y} }
	var scoped Network
	for _, node := range network.Nodes {
		scoped.Nodes = append(scoped.Nodes, Node{ID: id(node.ID), Position: move(node.Position)})
	}
	for _, lane := range network.Lanes {
		lane.ID, lane.From, lane.To = id(lane.ID), id(lane.From), id(lane.To)
		lane.SeparationGroup, lane.StationID = id(lane.SeparationGroup), id(lane.StationID)
		if lane.Control != nil {
			lane.Control = new(move(*lane.Control))
		}
		scoped.Lanes = append(scoped.Lanes, lane)
	}
	for _, station := range network.Stations {
		station.ID, station.Entry, station.Exit = id(station.ID), id(station.Entry), id(station.Exit)
		berths := make([]Berth, 0, len(station.Berths))
		for _, berth := range station.Berths {
			berth.ID, berth.Node, berth.SeparationGroup = id(berth.ID), id(berth.Node), id(berth.SeparationGroup)
			berths = append(berths, berth)
		}
		station.Berths = berths
		var banks []StationBank
		for _, bank := range station.Banks {
			banks = append(banks, StationBank{ID: id(bank.ID), Entry: id(bank.Entry), Exit: id(bank.Exit), BerthIDs: ids(bank.BerthIDs)})
		}
		station.Banks = banks
		scoped.Stations = append(scoped.Stations, station)
	}
	for _, depot := range network.Depots {
		lanes := make([]DepotLane, 0, len(depot.Lanes))
		for _, lane := range depot.Lanes {
			lanes = append(lanes, DepotLane{BerthIDs: ids(lane.BerthIDs)})
		}
		scoped.Depots = append(scoped.Depots, Depot{Station: id(depot.Station), Lanes: lanes})
	}
	for _, group := range network.ManeuverGroups {
		scoped.ManeuverGroups = append(scoped.ManeuverGroups, ManeuverGroup{
			ID: id(group.ID), StationID: id(group.StationID), LaneIDs: ids(group.LaneIDs),
			BerthIDs: ids(group.BerthIDs), ReverseLaneIDs: ids(group.ReverseLaneIDs),
		})
	}
	return scoped
}

// joinNetworks returns one network with the parts, which have no common ID
// and no common lane path.
func joinNetworks(parts ...Network) Network {
	var joined Network
	for _, part := range parts {
		joined.Nodes = append(joined.Nodes, part.Nodes...)
		joined.Lanes = append(joined.Lanes, part.Lanes...)
		joined.Stations = append(joined.Stations, part.Stations...)
		joined.Depots = append(joined.Depots, part.Depots...)
		joined.ManeuverGroups = append(joined.ManeuverGroups, part.ManeuverGroups...)
	}
	return joined
}

// scopedPlacements returns the placements with prefix on each ID.
func scopedPlacements(placements []Placement, prefix string) []Placement {
	scoped := make([]Placement, 0, len(placements))
	for _, placement := range placements {
		placement.ID, placement.StationID = prefix+placement.ID, prefix+placement.StationID
		if placement.BerthID != "" {
			placement.BerthID = prefix + placement.BerthID
		}
		scoped = append(scoped, placement)
	}
	return scoped
}
