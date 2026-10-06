package sim

import "slices"

// bufferPickup trims a new pickup route at its eligible station entry.
// It keeps every reserved block and leaves ineligible pickups unchanged.
func (s *Simulation) bufferPickup(v *vehicle) {
	if !s.stationBuffers || v.linked() {
		return
	}
	station, ok := s.station(v.destinationStation)
	if !ok || station.ParkingOnly {
		return
	}
	for index, lane := range slices.Backward(v.Route) {
		if !station.isEntry(lane.To) {
			continue
		}
		candidate := *v
		candidate.destination = Berth{}
		s.setVehicleRoute(&candidate, slices.Clone(v.Route[:index+1]))
		plan, eligible := s.bufferPlan(&candidate)
		if !eligible || candidate.reservedThrough > plan.frontier || candidate.distance > candidate.blocks.end(plan.frontier) {
			return
		}
		candidate.buffered, candidate.bufferBerth = true, ""
		*v = candidate
		return
	}
}
