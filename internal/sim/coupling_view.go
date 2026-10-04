package sim

import "slices"

// CouplingGroupView binds ordered cabins to their checked mechanical geometry.
// CommonSpeed exists only while both cabins share a connected speed or dwell.
// ManeuverEnvelope describes protected space, not an attached connector.
type CouplingGroupView struct {
	SavedCouplingGroup
	Profile          CouplingContract     `json:"profile"`
	OwnerID          string               `json:"ownerID"`
	ResourceClaims   int                  `json:"resourceClaims"`
	CommonSpeed      *float64             `json:"commonSpeed,omitzero"`
	Bodies           [2]CouplingRectangle `json:"bodies"`
	Connector        *CouplingRectangle   `json:"connector,omitzero"`
	ManeuverEnvelope *CouplingRectangle   `json:"maneuverEnvelope,omitzero"`
}

// CouplingPresentation is a detached observation of the independent contract.
type CouplingPresentation struct {
	Contract CouplingContract
	Enabled  bool
	Groups   []CouplingGroupView
}

// CouplingPresentation checks actual membership, motion, and retained owners.
// The caller owns synchronization, as for Snapshot. A failure publishes no view.
func (s *Simulation) CouplingPresentation() (CouplingPresentation, error) {
	if err := s.CouplingError(); err != nil {
		return CouplingPresentation{}, err
	}
	contract := s.CouplingContract()
	if contract == "" {
		if s.couplingEnabled || len(s.couplingGroups) != 0 {
			return CouplingPresentation{}, couplingMotionInvariant("unmarked observation has physical coupling state")
		}
		return CouplingPresentation{}, nil
	}
	if _, ok := LookupCouplingProfile(contract); !ok {
		return CouplingPresentation{}, ErrUnknownCouplingContract
	}
	if len(s.couplingGroups) > len(s.vehicles)/2 {
		return CouplingPresentation{}, couplingMotionInvariant("observed groups exceed half the fleet")
	}
	view := CouplingPresentation{Contract: contract, Enabled: s.couplingEnabled}
	groups, members := make(map[string]bool, len(s.couplingGroups)), make(map[string]bool, 2*len(s.couplingGroups))
	for _, group := range s.couplingGroups {
		c := group.context
		if c == nil || c.reservation.network != s.couplingNetwork || c.owner.kind != groupOwnerKind ||
			!boundedContractID(c.owner.id) || groups[c.owner.id] || group.formationTick != c.formationTick ||
			group.formationTick < 0 || group.formationTick > s.tick || group.state.Tick != s.tick || group.state.Finished {
			return CouplingPresentation{}, couplingMotionInvariant("observed group has an invalid identity or clock")
		}
		groups[c.owner.id] = true
		if err := s.checkCouplingViewMembers(group, members); err != nil {
			return CouplingPresentation{}, err
		}
	}
	counts := make(map[string]int, len(groups))
	for _, owner := range s.owners {
		if owner.kind == groupOwnerKind {
			if !groups[owner.id] {
				return CouplingPresentation{}, couplingMotionInvariant("observed ledger has an orphan group owner")
			}
			counts[owner.id]++
		}
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if (v.couplingID != "") != members[v.Pod.ID] {
			return CouplingPresentation{}, couplingMotionInvariant("observed vehicle has an orphan mechanical membership")
		}
	}
	metadata := s.savedCouplingGroups()
	for i, group := range s.couplingGroups {
		geometry, err := s.couplingGroupView(group, metadata[i], counts[group.context.owner.id])
		if err != nil {
			return CouplingPresentation{}, err
		}
		view.Groups = append(view.Groups, geometry)
	}
	return view, nil
}

func (s *Simulation) checkCouplingViewMembers(group couplingNativeGroup, seen map[string]bool) error {
	c, state := group.context, group.state
	canonical, err := c.stateAt(state.Elapsed)
	if err != nil || canonical != state {
		return couplingMotionInvariant("observed progress differs from its exact certificate")
	}
	if corridor, ok := s.couplingNetwork.corridors[c.reservation.corridorID]; !ok ||
		corridor.ID != c.reservation.corridorID || savedCouplingPhase(state.Phase) == "" {
		return couplingMotionInvariant("observed corridor or phase is unknown")
	}
	expected := c.membersAt(state)
	for i, member := range &c.reservation.members {
		id := member.Vehicle.Pod.ID
		v := s.findVehicle(id)
		if v == nil || seen[id] || v.couplingID != c.owner.id || v.Pod.Class != CompactClass ||
			v.link.leader != 0 || v.follower != 0 || v.distance != state.Distances[i] || v.routeVersion != member.RouteVersion {
			return couplingMotionInvariant("observed group has an invalid cabin binding")
		}
		seen[id] = true
		pod := expected[i].Pod
		pod.StationPhase, pod.ManeuverStationID = v.Pod.StationPhase, v.Pod.ManeuverStationID
		if pod != v.Pod || !slices.Equal(v.Riders, member.Vehicle.Riders) || !slices.Equal(v.Stops, member.Vehicle.Stops) ||
			v.RelocatingTo != member.Vehicle.RelocatingTo || !couplingViewRoutesEqual(v.Route, member.Vehicle.Route) {
			return couplingMotionInvariant("observed cabin differs from its certified pose or journey")
		}
		if !v.legacyBoardingRecords() && (!slices.Equal(v.Boardings, expected[i].Boardings) || v.riddenMeters() != expected[i].RiddenMeters) {
			return couplingMotionInvariant("observed cabin differs from its boarding or mileage facts")
		}
	}
	return nil
}

func (s *Simulation) couplingGroupView(group couplingNativeGroup, metadata SavedCouplingGroup, actualClaims int) (CouplingGroupView, error) {
	c, state := group.context, group.state
	view := CouplingGroupView{SavedCouplingGroup: metadata, Profile: s.CouplingContract(), OwnerID: c.owner.id}
	for _, dependency := range c.dependencies {
		owner := c.dependencyOwner(dependency, state)
		if !owner.isZero() && s.owners[dependency.Resource] != owner {
			return CouplingGroupView{}, couplingMotionInvariant("observed ledger lacks a retained owner")
		}
		if owner == c.owner {
			view.ResourceClaims++
		}
	}
	for _, claim := range c.reservation.PreservedClaims {
		_, adopted := slices.BinarySearchFunc(c.claims, claim.Resource, func(claim couplingClaim, r resource) int {
			return compareCouplingResource(claim.Resource, r)
		})
		if !adopted && s.owners[claim.Resource] != claim.Expected {
			return CouplingGroupView{}, couplingMotionInvariant("observed receiving owner changed")
		}
	}
	if view.ResourceClaims != actualClaims {
		return CouplingGroupView{}, couplingMotionInvariant("observed group owns an unexplained resource")
	}
	bodies, connector, err := c.motionBodiesAt(state)
	if err != nil {
		return CouplingGroupView{}, err
	}
	view.Bodies = bodies
	switch state.Phase {
	case couplingLatching, couplingConnected, couplingUnlatching:
		if state.Speeds[0] != state.Speeds[1] || connector == nil {
			return CouplingGroupView{}, couplingMotionInvariant("connected observation lacks common motion or connector")
		}
		view.CommonSpeed, view.Connector = new(state.Speeds[0]), connector
	case couplingClosing, couplingOpening:
		view.ManeuverEnvelope = connector
	case couplingDraining:
	default:
		return CouplingGroupView{}, couplingMotionInvariant("observed mechanical phase is unknown")
	}
	return view, nil
}

// CheckedSnapshot returns a detached fleet observation with checked train data.
// Failed native motion or view validation returns no snapshot.
func (s *Simulation) CheckedSnapshot() (Snapshot, error) {
	view, err := s.CouplingPresentation()
	if err != nil {
		return Snapshot{}, err
	}
	state := s.snapshot(true)
	s.bindCouplingView(&state, view)
	return state, nil
}

func (s *Simulation) bindCouplingView(state *Snapshot, view CouplingPresentation) {
	state.CouplingContract, state.CouplingEnabled, state.CouplingGroups = view.Contract, view.Enabled, view.Groups
	if view.Contract != "" {
		for i := range state.Vehicles {
			state.Vehicles[i].CouplingID = s.vehicles[i].couplingID
		}
	}
}

// Certificates own cloned geometry. Compare values, not slice addresses.
func couplingViewRoutesEqual(first, second []Lane) bool {
	if len(first) != len(second) {
		return false
	}
	for i, a := range first {
		b := second[i]
		if (a.Control == nil) != (b.Control == nil) || a.Control != nil && *a.Control != *b.Control {
			return false
		}
		a.Control, b.Control = nil, nil
		if a != b {
			return false
		}
	}
	return true
}
