package sim

// ParkingGap is a passenger berth that accepts a class, from which a pod
// of the class has no route to parking, or to which it has no route from
// parking. Such a pod cannot leave the berth for parking when another pod
// needs the berth.
type ParkingGap struct {
	StationID, BerthID string
	Class              VehicleClass
	// ToParking and FromParking report which of the two routes exist.
	ToParking, FromParking bool
}

// ParkingGaps returns the parking gaps of n for each class of classes, in
// the order of the classes, the stations, and the berths. A parking goal
// of a class is a berth of a parking station that accepts the class, or the
// entry of a depot station that accepts it. A class with no parking goal
// has no gaps. The routes follow the rules of route choice, on a network
// with no blocked lane.
func (n Network) ParkingGaps(classes []VehicleClass) []ParkingGap {
	graph := newRouteGraph(n)
	depots := make(map[string]bool, len(n.Depots))
	for _, depot := range n.Depots {
		depots[depot.Station] = true
	}
	var gaps []ParkingGap
	for _, class := range classes {
		rank := make([]int, len(n.Nodes))
		for node := range rank {
			rank[node] = -1
		}
		goals := 0
		for _, station := range n.Stations {
			if !station.ParkingOnly || !station.VehicleClasses.Allows(string(class)) {
				continue
			}
			if depots[station.ID] {
				if node, ok := graph.nodes[station.Entry]; ok {
					rank[node], goals = goals, goals+1
				}
				continue
			}
			for _, berth := range station.Berths {
				if node, ok := graph.nodes[berth.Node]; ok && berthAllows(station, berth, class) {
					rank[node], goals = goals, goals+1
				}
			}
		}
		if goals == 0 {
			continue
		}
		for _, station := range n.Stations {
			if station.ParkingOnly {
				continue
			}
			for _, berth := range station.Berths {
				if !berthAllows(station, berth, class) {
					continue
				}
				input := preferredNearestInput{class: class, from: berth.Node, rank: rank}
				_, to := n.preferredNearestIndexed(input, graph)
				input.reverse = true
				_, from := n.preferredNearestIndexed(input, graph)
				if !to || !from {
					gaps = append(gaps, ParkingGap{StationID: station.ID, BerthID: berth.ID, Class: class, ToParking: to, FromParking: from})
				}
			}
		}
	}
	return gaps
}
