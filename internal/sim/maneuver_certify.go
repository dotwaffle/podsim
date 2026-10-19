package sim

// certifiedPairs holds the pairs of separation keys that the maneuver
// certification clears: a group lane and a berth of its group, and two
// berths of one group. The lower key is first.
type certifiedPairs map[[2]separationKey]bool

// certifyManeuvers returns the certified pairs of the maneuver groups.
func certifyManeuvers(Network, *maneuverIndex) (certifiedPairs, error) {
	return nil, nil
}

// certified reports whether two keys form a certified pair.
func (c certifiedPairs) certified(a, b separationKey) bool {
	if c == nil {
		return false
	}
	if b.kind < a.kind || b.kind == a.kind && b.index < a.index {
		a, b = b, a
	}
	return c[[2]separationKey{a, b}]
}
