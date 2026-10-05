package sim

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
