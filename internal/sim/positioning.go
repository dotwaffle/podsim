package sim

import "fmt"

// Positioning selects how the simulation moves idle empty pods before
// passengers ask for them.
type Positioning int

const (
	// PositioningOff moves no idle empty pod before a passenger asks for it.
	PositioningOff Positioning = iota
	// PositioningRedistribution moves idle empty pods toward the demand
	// weight of each passenger station. See SetDemandWeights.
	PositioningRedistribution
)

// SetPositioning selects the positioning mode. It returns an error for an
// unknown mode and then changes nothing. A mode other than PositioningOff
// can move a pod at the next step. Reset selects PositioningOff. The saved
// state does not keep the mode, so the session sets it again after a
// restore.
func (s *Simulation) SetPositioning(mode Positioning) error {
	if mode < PositioningOff || mode > PositioningRedistribution {
		return fmt.Errorf("unknown positioning mode %d", mode)
	}
	s.setPositioning(mode)
	return nil
}

// setPositioning selects a valid positioning mode. A mode other than
// PositioningOff moves the next check up to the current tick.
func (s *Simulation) setPositioning(mode Positioning) {
	s.positioning = mode
	if mode != PositioningOff && s.nextRedistributionTick < s.tick {
		s.nextRedistributionTick = s.tick
	}
}
