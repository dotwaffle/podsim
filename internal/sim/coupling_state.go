package sim

// CouplingPhase names a committed physical pair phase.
type CouplingPhase string

const (
	// CouplingClosing holds the front pod while the rear approaches its pin.
	CouplingClosing CouplingPhase = "closing"
	// CouplingLatching holds both pods for the fixed latch dwell.
	CouplingLatching CouplingPhase = "latching"
	// CouplingConnected moves both cabins at their fixed center spacing.
	CouplingConnected CouplingPhase = "connected"
	// CouplingUnlatching holds both cabins for the fixed split dwell.
	CouplingUnlatching CouplingPhase = "unlatching"
	// CouplingOpening moves the front cabin to ordinary separation.
	CouplingOpening CouplingPhase = "opening"
	// CouplingDraining retains the pair until both exits clear joint resources.
	CouplingDraining CouplingPhase = "draining"
)

// SavedCouplingProgress identifies the remaining maneuver and drain order.
// Member routes and distances hold spatial progress without a second route.
type SavedCouplingProgress struct {
	Leg              int `json:"leg"`
	DrainFirstMember int `json:"drainFirstMember"`
}

// SavedCouplingGroup preserves committed ordered membership and phase.
// Completed groups retire before the simulation exports its stable frame.
type SavedCouplingGroup struct {
	ID             string                `json:"id"`
	Members        [2]string             `json:"members"`
	FormationTick  int64                 `json:"formationTick"`
	CorridorID     string                `json:"corridorID"`
	AssemblySiteID string                `json:"assemblySiteID"`
	SplitSiteID    string                `json:"splitSiteID"`
	Phase          CouplingPhase         `json:"phase"`
	DwellTicks     int                   `json:"dwellTicks"`
	Progress       SavedCouplingProgress `json:"progress"`
}

type couplingNativeGroup struct {
	context       *couplingMotionContext
	state         couplingMotionState
	formationTick int64
}

func (s *Simulation) couplingMember(id string) bool {
	for _, group := range s.couplingGroups {
		for _, member := range &group.context.reservation.members {
			if member.Vehicle.Pod.ID == id {
				return true
			}
		}
	}
	return false
}

func (s *Simulation) savedCouplingGroups() []SavedCouplingGroup {
	if len(s.couplingGroups) == 0 {
		return nil
	}
	groups := make([]SavedCouplingGroup, len(s.couplingGroups))
	for i, group := range s.couplingGroups {
		c := group.context
		corridor := c.reservation.network.corridors[c.reservation.corridorID]
		leg := group.state.Leg
		switch group.state.Phase {
		case couplingLatching:
			leg = 0
		case couplingUnlatching:
			leg = 1
		default:
		}
		groups[i] = SavedCouplingGroup{
			ID:            c.owner.id,
			Members:       [2]string{c.reservation.members[0].Vehicle.Pod.ID, c.reservation.members[1].Vehicle.Pod.ID},
			FormationTick: group.formationTick, CorridorID: corridor.ID,
			AssemblySiteID: corridor.AssemblySiteID, SplitSiteID: corridor.SplitSiteID,
			Phase: savedCouplingPhase(group.state.Phase), DwellTicks: group.state.Dwell,
			Progress: SavedCouplingProgress{Leg: leg, DrainFirstMember: c.drainOrder[0]},
		}
	}
	return groups
}

func savedCouplingPhase(phase couplingReservationPhase) CouplingPhase {
	switch phase {
	case couplingClosing:
		return CouplingClosing
	case couplingLatching:
		return CouplingLatching
	case couplingConnected:
		return CouplingConnected
	case couplingUnlatching:
		return CouplingUnlatching
	case couplingOpening:
		return CouplingOpening
	case couplingDraining:
		return CouplingDraining
	default:
		return ""
	}
}
