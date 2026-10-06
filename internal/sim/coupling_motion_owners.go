package sim

import (
	"slices"
	"sort"
)

type couplingMotionRole uint8

const (
	couplingJointOwner couplingMotionRole = iota + 1
	couplingSingleOwner
	couplingTransferredOwner
	couplingReleasedOwner
)

type couplingOwnerEvent struct {
	tick  uint64
	index int
	write couplingOwnerWrite
}

type couplingMotionOwnerView struct {
	context     *couplingMotionContext
	eventCursor int
	owners      map[resource]resourceOwner
}

// couplingDependencyReleased reports whether the train has released d at
// the member distances. Each member that uses d has reached its release
// threshold. Before draining, d is not a site, and the front axis has
// reached the axis threshold plus Clearance. frontOrigin is the route
// distance of the front at the start of the assembly lane.
//
// This conservative axis bound remains valid through the complete opening.
// A released claim never returns to group ownership in a later phase.
func couplingDependencyReleased(d couplingDependency, distances [2]float64, frontOrigin float64, draining bool) bool {
	for i, used := range d.MemberUse {
		if used && distances[i] < d.MemberRelease[i] {
			return false
		}
	}
	if draining {
		return true
	}
	return !d.Site && (!d.AxisUse || distances[0]-frontOrigin >= d.AxisRelease+Clearance)
}

func (c *couplingMotionContext) dependencyOwner(d couplingDependency, state couplingMotionState) resourceOwner {
	if couplingDependencyReleased(d, state.Distances, c.reservation.axisOrigins[0], state.Phase == couplingDraining) {
		return resourceOwner{}
	}
	if state.Finished && !couplingJointDependency(d) {
		for i, used := range d.MemberUse {
			if used {
				return podResourceOwner(c.reservation.members[i].Vehicle.Pod.ID)
			}
		}
	}
	return c.owner
}

func couplingDependencyRole(d couplingDependency, owner resourceOwner) couplingMotionRole {
	if owner.isZero() {
		return couplingReleasedOwner
	}
	if owner.kind == podOwnerKind {
		return couplingTransferredOwner
	}
	if couplingJointDependency(d) {
		return couplingJointOwner
	}
	return couplingSingleOwner
}

// Each resource needs one bounded integer search, independent of elapsed ticks.
func (c *couplingMotionContext) prepareOwnerEvents() error {
	initial, err := c.stateAt(0)
	if err != nil {
		return err
	}
	terminal, err := c.stateAt(c.ticks)
	if err != nil {
		return err
	}
	for i, d := range c.dependencies {
		before, after := c.dependencyOwner(d, initial), c.dependencyOwner(d, terminal)
		if couplingJointDependency(d) && !after.isZero() {
			return couplingDenied("full drain does not retire every joint dependency")
		}
		if before == after {
			continue
		}
		low, high := uint64(1), c.ticks
		for low < high {
			middle := low + (high-low)/2
			state, err := c.stateAt(middle)
			if err != nil {
				return err
			}
			if c.dependencyOwner(d, state) == before {
				low = middle + 1
			} else {
				high = middle
			}
		}
		state, err := c.stateAt(low)
		if err != nil {
			return err
		}
		next := c.dependencyOwner(d, state)
		c.events = append(c.events, couplingOwnerEvent{tick: low, index: i, write: couplingOwnerWrite{Resource: d.Resource, Expected: before, Next: next, Role: couplingDependencyRole(d, next)}})
	}
	slices.SortFunc(c.events, func(a, b couplingOwnerEvent) int {
		if a.tick < b.tick {
			return -1
		}
		if a.tick > b.tick {
			return 1
		}
		return a.index - b.index
	})
	c.drainageProved = true
	return nil
}

func (c *couplingMotionContext) ownerEventCursor(elapsed uint64) int {
	return sort.Search(len(c.events), func(i int) bool { return c.events[i].tick > elapsed })
}

// The view copies actual relevant owners at commitment or after external invalidation.
// A later adapter must discard it after every relevant external ledger write.
// No caller revision or hash can construct this proof.
func sealCouplingMotionOwners(c *couplingMotionContext, state couplingMotionState, actual map[resource]resourceOwner) (*couplingMotionOwnerView, error) {
	if c == nil {
		return nil, couplingMotionInvariant("missing owner context")
	}
	expected, err := c.stateAt(state.Elapsed)
	if err != nil || expected != state {
		return nil, couplingMotionInvariant("owner seal has a stale motion stamp")
	}
	view := &couplingMotionOwnerView{context: c, eventCursor: c.ownerEventCursor(state.Elapsed), owners: make(map[resource]resourceOwner)}
	for _, d := range c.dependencies {
		owner := c.dependencyOwner(d, state)
		if owner.isZero() {
			continue
		}
		if actual[d.Resource] != owner {
			return nil, couplingMotionInvariant("actual ledger lacks an exact unresolved owner")
		}
		view.owners[d.Resource] = actual[d.Resource]
	}
	for _, claim := range c.reservation.PreservedClaims {
		_, adopted := slices.BinarySearchFunc(c.claims, claim.Resource, func(claim couplingClaim, r resource) int { return compareCouplingResource(claim.Resource, r) })
		if adopted {
			continue
		}
		if actual[claim.Resource] != claim.Expected {
			return nil, couplingMotionInvariant("actual receiving owner changed")
		}
		view.owners[claim.Resource] = actual[claim.Resource]
	}
	return view, nil
}

func (c *couplingMotionContext) checkMotionOwners(state couplingMotionState, view *couplingMotionOwnerView) error {
	if view == nil || view.context != c || view.eventCursor != c.ownerEventCursor(state.Elapsed) {
		return couplingMotionInvariant("owner view did not advance through actual role changes")
	}
	return nil
}
