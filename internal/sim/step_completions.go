package sim

import "slices"

// StepCompletion identifies a party that finished unloading during a tick.
// A tick completes at most fleet size times MaxSharedRideParties parties.
type StepCompletion struct {
	RequestID    int
	AlightedTick int64
}

// StepCompletions returns owned records for the latest advanced tick.
// Paused steps preserve the records. Reset and restore clear them.
// Call it only while no other goroutine uses the simulation.
func (s *Simulation) StepCompletions() []StepCompletion {
	return slices.Clone(s.stepCompletions)
}
