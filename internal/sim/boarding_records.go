package sim

import (
	"math"
	"slices"
)

// RiderBoarding records a party's berth and passenger distance at boarding.
type RiderBoarding struct {
	BerthID          string  `json:"BerthID"`
	MetersAtBoarding float64 `json:"MetersAtBoarding"`
}

// legacyBoardingRecords reports whether the old representation preserves each record.
func (v *vehicle) legacyBoardingRecords() bool {
	if len(v.Boardings) == 0 {
		return true
	}
	if len(v.Boardings) != len(v.Riders) || !v.carriesPassengers() || v.journeyOrigin.ID == "" ||
		v.Pod.Activity == Boarding && v.Pod.Occupied {
		return false
	}
	for index, record := range v.Boardings {
		if record.BerthID != v.journeyOrigin.ID || record.MetersAtBoarding != 0 ||
			v.Riders[index].From != v.Riders[0].From ||
			v.Riders[index].Completed && slices.Contains(v.Stops, v.Riders[index].To) {
			return false
		}
	}
	return true
}

func (s *Simulation) riderOrigin(v *vehicle, index int) string {
	if len(v.Boardings) == 0 {
		return v.journeyOrigin.Node
	}
	station, ok := s.station(v.Riders[index].From)
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
	if len(v.Boardings) == 0 {
		return s.plannedDetour(v.journeyOrigin.Node, stops, start) <= maxSharedRideDetour
	}
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
