package sim

// SetCongestionRouting selects occupied-track route costs for new assignments.
// Existing routes and movement arbitration do not change.
func (s *Simulation) SetCongestionRouting(enabled bool) {
	s.congestionRouting = enabled
	s.nextCongestionRouteRefresh = 0
	s.congestionRouteCosts = nil
	s.congestionRoutes = nil
}
