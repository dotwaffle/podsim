package sim

// The kinds and the phases of FaultView (section 13.4 of the incident
// suspension contract).
const (
	FaultKindPod        = "pod"
	FaultKindDebris     = "debris"
	FaultPhaseBraking   = "braking"
	FaultPhaseStopped   = "stopped"
	FaultPhaseEvacuated = "evacuated"
)

// FaultView is one active fault of a snapshot. A pod fault has PodID,
// Phase and EvacuateTick. Debris has LaneID, FromMeters and ToMeters.
// EndTick is 0 for a fault without an end. The pointers keep a value of 0
// apart from an absent member.
type FaultView struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	PodID        string   `json:"podID,omitzero"`
	LaneID       string   `json:"laneID,omitzero"`
	FromMeters   *float64 `json:"fromMeters,omitzero"`
	ToMeters     *float64 `json:"toMeters,omitzero"`
	Phase        string   `json:"phase,omitzero"`
	StartTick    int64    `json:"startTick"`
	EndTick      int64    `json:"endTick,omitzero"`
	EvacuateTick *int64   `json:"evacuateTick,omitzero"`
}

// FaultCounters holds the fault counters. Each counter stops at
// MaxCounter.
type FaultCounters struct {
	// Started counts the faults that started, and Cleared the faults that
	// a command or a duration cleared.
	Started int64 `json:"started,omitzero"`
	Cleared int64 `json:"cleared,omitzero"`
	// Evacuations counts the evacuations of faulted pods, and Reroutes
	// the endpoint reroutes.
	Evacuations int64 `json:"evacuations,omitzero"`
	Reroutes    int64 `json:"reroutes,omitzero"`
	// FaultWaitTicks counts the ticks of healthy pods that wait for a
	// fault, one for each pod in each tick.
	FaultWaitTicks int64 `json:"faultWaitTicks,omitzero"`
}

// FaultsView holds the active faults in serial order and the fault
// counters. Active is nil when no fault is active.
type FaultsView struct {
	Active   []FaultView   `json:"active,omitempty"`
	Counters FaultCounters `json:"counters,omitzero"`
}

// exported returns the counters in their exported form.
func (c faultCounters) exported() FaultCounters {
	return FaultCounters{Started: c.started, Cleared: c.cleared, Evacuations: c.evacuations, Reroutes: c.reroutes, FaultWaitTicks: c.faultWaitTicks}
}

// faultsView returns the active faults and the counters of s.
func (s *Simulation) faultsView() FaultsView {
	view := FaultsView{Counters: s.faultCounters.exported()}
	if len(s.faults) > 0 {
		view.Active = make([]FaultView, len(s.faults))
	}
	for index, record := range s.faults {
		fault := FaultView{ID: record.id(), StartTick: record.start, EndTick: record.end}
		if record.kind == debrisFault {
			fault.Kind, fault.LaneID = FaultKindDebris, s.network.Lanes[record.lane].ID
			fault.FromMeters, fault.ToMeters = new(record.from), new(record.to)
		} else {
			v := &s.vehicles[record.pod]
			evacuate := s.evacuateTick(record)
			fault.Kind, fault.PodID, fault.Phase, fault.EvacuateTick = FaultKindPod, v.Pod.ID, s.faultPhase(v, evacuate), &evacuate
		}
		view.Active[index] = fault
	}
	return view
}

// faultPhase returns the phase of the pod fault on v (section 4.1 of the
// incident suspension contract). The pod brakes while it moves. At rest,
// it is evacuated from its evacuation tick when it has no active rider.
func (s *Simulation) faultPhase(v *vehicle, evacuate int64) string {
	switch {
	case v.Pod.Speed > 0:
		return FaultPhaseBraking
	case s.tick >= evacuate && v.RidersAboard() == 0:
		return FaultPhaseEvacuated
	default:
		return FaultPhaseStopped
	}
}
