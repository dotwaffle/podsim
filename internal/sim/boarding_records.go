package sim

import (
	"math"
)

// RiderBoarding records a party's berth and passenger distance at boarding.
// A pod with riders has one record for each rider, in the order of Riders.
type RiderBoarding struct {
	BerthID          string  `json:"berthID"`
	MetersAtBoarding float64 `json:"metersAtBoarding"`
}

// riderOrigin returns the node of the boarding berth of the rider at index.
func (s *Simulation) riderOrigin(v *vehicle, index int) string {
	station, ok := s.station(v.Riders[index].legOrigin())
	if !ok {
		return ""
	}
	berth, ok := station.berth(v.Boardings[index].BerthID)
	if !ok {
		return ""
	}
	return berth.Node
}

func (s *Simulation) keepsRiderDetours(v *vehicle, stops []string, start detourStart) bool {
	if len(v.Boardings) != len(v.Riders) {
		return false
	}
	for index, rider := range v.Riders {
		if rider.Completed {
			continue
		}
		plan := riderDetour{origin: s.riderOrigin(v, index), destination: rider.To, baseline: v.Boardings[index].MetersAtBoarding}
		ratio := s.plannedRiderDetour(plan, stops, start)
		if math.IsNaN(ratio) || ratio > maxSharedRideDetour {
			return false
		}
	}
	return true
}
