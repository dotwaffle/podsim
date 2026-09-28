package sim

// Paused reports whether Step leaves the simulation clock unchanged.
func (s *Simulation) Paused() bool { return s.paused }

// DemoRunning reports whether the scripted traffic demo is active.
func (s *Simulation) DemoRunning() bool { return s.demo != nil }
