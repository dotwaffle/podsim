package editormodel

import "github.com/dotwaffle/podsim/internal/sim"

func draftLaneMinimum(lane any) float64 {
	classes, valid := draftClassSet(lane)
	if !valid {
		return 2 * sim.Clearance
	}
	return sim.LaneMinimumLength(sim.Lane{VehicleClasses: classes})
}

func draftPairClearance(first, second any) float64 {
	return max(draftLaneMinimum(first), draftLaneMinimum(second)) / 2
}

func hasLargeGeometry(network any) bool {
	for _, lane := range items(member(network, "lanes")) {
		if draftLaneMinimum(lane) > 2*sim.Clearance {
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
