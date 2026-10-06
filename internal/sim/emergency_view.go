package sim

// The phases of EmergencyView (section 4.2 of the incident emergency
// contract).
const (
	EmergencyPhaseDeferred  = "deferred"
	EmergencyPhaseBound     = "bound"
	EmergencyPhaseUnloading = "unloading"
)

// EmergencyView is one active emergency of a snapshot (section 11.4 of the
// incident emergency contract). OrderID is the order ID of the party, and
// Phase is derived from the pod. Every member is required.
type EmergencyView struct {
	ID        string `json:"id"`
	PodID     string `json:"podID"`
	OrderID   int    `json:"orderID"`
	Phase     string `json:"phase"`
	StartTick int64  `json:"startTick"`
}

// EmergenciesView holds the active emergencies in serial order and the
// emergency counters. Active is nil when no emergency is active.
type EmergenciesView struct {
	Active   []EmergencyView   `json:"active,omitempty"`
	Counters EmergencyCounters `json:"counters,omitzero"`
}

// emergenciesView returns the active emergencies and the counters of s.
func (s *Simulation) emergenciesView() EmergenciesView {
	view := EmergenciesView{Counters: s.emergencyCounters.exported()}
	if len(s.emergencies) > 0 {
		view.Active = make([]EmergencyView, len(s.emergencies))
	}
	for index, record := range s.emergencies {
		v := &s.vehicles[record.pod]
		view.Active[index] = EmergencyView{ID: record.id(), PodID: v.Pod.ID, OrderID: record.order, Phase: emergencyPhase(v), StartTick: record.start}
	}
	return view
}

// emergencyPhase returns the phase of the record of v (section 4.2 of the
// incident emergency contract). A pod with purpose 1 unloads when it is
// Unloading at the berth of its destination, and is bound otherwise. Each
// other pod is deferred: the next emergency stage ends a record whose pod
// has another purpose.
func emergencyPhase(v *vehicle) string {
	switch {
	case v.op.purpose != opEmergencyUnload:
		return EmergencyPhaseDeferred
	case v.Pod.Activity == Unloading && v.Pod.BerthID == v.destination.ID:
		return EmergencyPhaseUnloading
	default:
		return EmergencyPhaseBound
	}
}
