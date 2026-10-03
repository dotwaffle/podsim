package sim

import (
	"math"
	"slices"
)

// riderDetour supplies one active rider's boarding node, cumulative baseline,
// and destination. An empty destination selects the legacy all-stop calculation.
type riderDetour struct {
	origin, destination string
	baseline            float64
}

// plannedRiderDetour evaluates only this rider's destination with the actual
// vehicle class and conservative berth continuations. start.ridden is cumulative
// distance to the first entry or chosen berth. A bank plan needs its actual
// source node, selected entry, or chosen berth. It does not read vehicle history.
func (s *Simulation) plannedRiderDetour(rider riderDetour, stops []string, start detourStart) float64 {
	if rider.origin == "" || rider.destination == "" || !slices.Contains(stops, rider.destination) ||
		!finite(rider.baseline) || rider.baseline < 0 || !finite(start.ridden) || start.ridden < rider.baseline {
		return math.Inf(1)
	}
	if s.network.hasStationBanks() {
		if start.from == "" && start.entry == "" && start.berth.ID == "" {
			return math.Inf(1)
		}
		return s.plannedRiderBankDetour(rider, stops, start)
	}
	return s.plannedBerthDetour(rider, stops, start)
}

func (s *Simulation) plannedArrivalDetour(rider riderDetour, stop string, berth Berth, class VehicleClass, arrival, legacyDirect float64) float64 {
	if rider.destination == "" {
		return arrival / legacyDirect
	}
	if stop != rider.destination {
		return 1
	}
	direct := s.directDistanceForClass(rider.origin, stop, berth, class)
	if !finite(arrival) || !finite(direct) || direct <= 0 {
		return math.Inf(1)
	}
	return (arrival - rider.baseline) / direct
}
