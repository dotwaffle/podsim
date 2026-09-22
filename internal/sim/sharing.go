package sim

import "fmt"

// SetSharedRidePartyLimit controls same-origin, same-destination parties that
// may join a pod before it departs. One disables sharing.
func (s *Simulation) SetSharedRidePartyLimit(limit int) error {
	if limit < 1 || limit > MaxSharedRideParties {
		return fmt.Errorf("shared ride party limit must be 1 to %d", MaxSharedRideParties)
	}
	s.sharedRidePartyLimit = limit
	return nil
}
