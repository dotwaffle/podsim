package sim

import "fmt"

// SharedRideMode selects the parties that can join a boarding pod.
type SharedRideMode string

const (
	// SharedRideDestination lets a party join a pod that goes to the
	// destination of the party.
	SharedRideDestination SharedRideMode = "destination"
	// SharedRideDropOffs also lets a party join a pod that passes the
	// destination of the party, or that can extend its journey to it. The
	// pod then stops at that destination on the way.
	SharedRideDropOffs SharedRideMode = "drop-offs"
)

const (
	// DefaultSharedRideMaxStops is the default limit of the intermediate
	// stops of a pod in drop-offs mode.
	DefaultSharedRideMaxStops = 3
	// MaxSharedRideStops bounds the intermediate stops of a pod. With the
	// last stop, a pod has at most one stop for each party.
	MaxSharedRideStops = MaxSharedRideParties - 1
	// maxSharedRideDetour bounds the detour ratio of each rider of a pod
	// in drop-offs mode. The ratio is the distance that the rider rides
	// over the free-flow distance to its destination, as alight measures
	// it. See cappedStops, legRoute and rerouteKeepsDetours.
	maxSharedRideDetour = 1.5
)

// SetSharedRidePartyLimit controls same-origin parties that may join a pod
// before it departs. One disables sharing.
func (s *Simulation) SetSharedRidePartyLimit(limit int) error {
	defer s.observe()
	if limit < 1 || limit > MaxSharedRideParties {
		return fmt.Errorf("shared ride party limit must be 1 to %d", MaxSharedRideParties)
	}
	s.sharedRidePartyLimit = limit
	return nil
}

// SetSharedRideMode sets the sharing mode and the limit of the intermediate
// stops of a pod. A pod stops before its last stop only in drop-offs mode.
func (s *Simulation) SetSharedRideMode(mode SharedRideMode, maxStops int) error {
	defer s.observe()
	if err := validateSharedRideMode(mode, maxStops); err != nil {
		return err
	}
	s.sharedRideMode, s.sharedRideMaxStops = mode, maxStops
	return nil
}

func validateSharedRideMode(mode SharedRideMode, maxStops int) error {
	if mode != SharedRideDestination && mode != SharedRideDropOffs {
		return fmt.Errorf("shared ride mode must be %q or %q", SharedRideDestination, SharedRideDropOffs)
	}
	if maxStops < 1 || maxStops > MaxSharedRideStops {
		return fmt.Errorf("shared ride stop limit must be 1 to %d", MaxSharedRideStops)
	}
	return nil
}
