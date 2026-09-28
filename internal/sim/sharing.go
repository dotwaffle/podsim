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
	// DefaultSharedRideMode is the mode of a new simulation, and the mode
	// of a project or a saved state that does not give a mode.
	DefaultSharedRideMode = SharedRideDropOffs
)

// SharedRideJoin selects the parties that can join a boarding pod by
// their pickup state.
type SharedRideJoin string

const (
	// SharedRideJoinUnassigned lets only a party with no pod join a
	// boarding pod.
	SharedRideJoinUnassigned SharedRideJoin = "unassigned"
	// SharedRideJoinReassignExisting also lets a party with an empty pod on
	// its way join a boarding pod at its origin, when the boarding pod
	// already stops at the destination of the party. Dispatch then
	// releases the pod of the party. See releasePickup.
	SharedRideJoinReassignExisting SharedRideJoin = "reassign-existing"
	// DefaultSharedRideJoin is the join policy of a new simulation, and the
	// policy of a project or a saved state that does not give a policy.
	DefaultSharedRideJoin = SharedRideJoinUnassigned
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

// SetSharedRideJoin sets the join policy. With a party limit of 1, no
// party joins a pod, so the policy has no effect.
func (s *Simulation) SetSharedRideJoin(join SharedRideJoin) error {
	defer s.observe()
	if err := validateSharedRideJoin(join); err != nil {
		return err
	}
	s.sharedRideJoin = join
	return nil
}

func validateSharedRideJoin(join SharedRideJoin) error {
	if join != SharedRideJoinUnassigned && join != SharedRideJoinReassignExisting {
		return fmt.Errorf("shared ride join policy must be %q or %q", SharedRideJoinUnassigned, SharedRideJoinReassignExisting)
	}
	return nil
}
