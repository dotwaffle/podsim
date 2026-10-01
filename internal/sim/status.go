package sim

// Tick returns the simulation clock. Callers provide synchronization.
func (s *Simulation) Tick() int64 { return s.tick }

// Paused reports whether Step leaves the simulation clock unchanged.
func (s *Simulation) Paused() bool { return s.paused }

// DemoRunning reports whether the scripted traffic demo is active.
func (s *Simulation) DemoRunning() bool { return s.demo != nil }

// PendingCount returns the number of requests that have not started boarding.
func (s *Simulation) PendingCount() int { return len(s.waiting) }
