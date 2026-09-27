package sim

// SetCongestionRouting selects occupied-track route costs for the routes
// that pods start. Estimates and the choices of dispatch, positioning and
// parking keep free-flow costs. Existing routes and movement arbitration do
// not change.
func (s *Simulation) SetCongestionRouting(enabled bool) {
	s.congestionRouting = enabled
	s.nextCongestionRouteRefresh = 0
	s.congestionRouteCosts = nil
	s.congestionRoutes = nil
}
