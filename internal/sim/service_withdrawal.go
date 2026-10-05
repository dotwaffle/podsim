package sim

import "fmt"

// serviceHold is one cause that withdraws a pod from service. Each hold is
// a separate bit, so one cause cannot clear the hold of another cause. A
// later cause kind takes the next bit.
type serviceHold uint8

const (
	faultHold serviceHold = 1 << iota
	emergencyHold
	knownServiceHolds = faultHold | emergencyHold
)

// inService reports whether v has no hold. A pod with a hold is not supply
// for any order: each supply path tests inService and skips the pod.
func (v *vehicle) inService() bool {
	return v.withdrawn == 0
}

// oneServiceHold reports whether hold is exactly one bit of
// knownServiceHolds. A zero value, an unknown bit, and a mask of two holds
// are not one hold.
func oneServiceHold(hold serviceHold) bool {
	return hold&knownServiceHolds == hold && hold != 0 && hold&(hold-1) == 0
}

// withdrawService adds hold to v. Each supply path then skips v. The
// operation changes no route, physical destination, speed, or owner (W4).
// It refuses, and changes nothing, when hold is not exactly one known
// hold, when v already has hold, when v is a coupling member, or during a
// dispatch pass. A coupling member has a coupling ID or approaches its
// partner; its caller waits for the split. The callers run at a command
// boundary or at a fixed place in Step outside dispatch.
//
// The first hold also releases the pending pickups of v. See
// releasePickups. A later hold releases nothing, because no trip can name
// a withdrawn pod.
func (s *Simulation) withdrawService(v *vehicle, hold serviceHold) error {
	if !oneServiceHold(hold) {
		return fmt.Errorf("pod %s: service hold %#x is not one known hold", v.Pod.ID, hold)
	}
	if v.withdrawn&hold != 0 {
		return fmt.Errorf("pod %s: service hold %#x is already set", v.Pod.ID, hold)
	}
	if v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) {
		return fmt.Errorf("pod %s: service withdrawal of a coupling member", v.Pod.ID)
	}
	if s.pass != nil && s.pass.active {
		return fmt.Errorf("pod %s: service withdrawal during a dispatch pass", v.Pod.ID)
	}
	first := v.withdrawn == 0
	v.withdrawn |= hold
	if first {
		s.releasePickups(v)
	}
	return nil
}

// restoreService removes hold from v. It is the inverse of withdrawService
// for supply membership: it changes nothing else. The pod stays withdrawn
// while it has another hold. It refuses, and changes nothing, when hold is
// not exactly one known hold, when v does not have hold, when hold owns
// the operational purpose of v (invariant W5), or when v is a coupling
// member. The policy of the hold first calls rebindOperationalOwner, or it
// waits until an arrival clears the purpose. A withdrawn pod can become a
// coupling member, and its caller waits for the split, as for
// withdrawService.
func (s *Simulation) restoreService(v *vehicle, hold serviceHold) error {
	if !oneServiceHold(hold) {
		return fmt.Errorf("pod %s: service hold %#x is not one known hold", v.Pod.ID, hold)
	}
	if v.withdrawn&hold == 0 {
		return fmt.Errorf("pod %s: service hold %#x is not set", v.Pod.ID, hold)
	}
	if v.op.owner == hold {
		return fmt.Errorf("pod %s: service hold %#x owns the operational purpose", v.Pod.ID, hold)
	}
	if v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) {
		return fmt.Errorf("pod %s: service restore of a coupling member", v.Pod.ID)
	}
	v.withdrawn &^= hold
	return nil
}
