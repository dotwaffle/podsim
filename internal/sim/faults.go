package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// maxFaultSeconds is the longest fault duration.
const maxFaultSeconds = 86_400

// The errors of the fault operations. A refused operation changes nothing.
var (
	errFaultsOff     = errors.New("faults are not enabled")
	errFaultDuration = errors.New("invalid fault duration")
	errIncidentLimit = errors.New("incident limit reached")
	errUnknownPod    = errors.New("unknown pod")
	errPodFaulted    = errors.New("pod already has a fault")
	errFaultTarget   = errors.New("fault target is not supported")
	errFaultDispatch = errors.New("fault start during a dispatch pass")
	errUnknownFault  = errors.New("unknown fault")
)

// faultKind is the kind of a fault record.
type faultKind uint8

// podFault is a mechanical fault of one pod.
const podFault faultKind = iota

// faultRecord is one active fault. Its ID is i<generation>.<serial>.
type faultRecord struct {
	generation, serial uint64
	kind               faultKind
	// start is the tick at creation. end is 0 for a fault without an end.
	// Otherwise the first fault stage with tick >= end clears the fault.
	start, end int64
	// pod is the index in Simulation.vehicles of the faulted pod.
	pod int
}

// id returns the incident ID of the record.
func (r faultRecord) id() string {
	return incidentID(r.generation, r.serial)
}

// faultSettings holds the fault settings of the project.
type faultSettings struct {
	// evacuationSeconds is the time from fault onset to evacuation.
	evacuationSeconds int64
}

// faultCounters counts the fault events. Each counter stops at
// math.MaxInt64. See countFault.
type faultCounters struct {
	// started counts the faults that started, and cleared the faults that
	// a command or a duration cleared.
	started, cleared int64
	// evacuations counts the evacuations of faulted pods, and reroutes the
	// endpoint reroutes.
	evacuations, reroutes int64
	// faultWaitTicks counts the ticks of healthy pods that wait for a
	// fault, one for each pod in each tick.
	faultWaitTicks int64
}

// countFault adds 1 to counter. At math.MaxInt64 the counter keeps its
// value, so a counter never stops a transition.
func countFault(counter *int64) {
	if *counter < math.MaxInt64 {
		*counter++
	}
}

// startPodFault starts a pod fault on v. duration is 0 for a fault
// without an end, or a number of seconds from 1 to maxFaultSeconds. It
// checks each precondition, in the order of the contract, before its
// first write, and it changes nothing when one fails. It returns the
// fault ID.
func (s *Simulation) startPodFault(v *vehicle, duration int64) (string, error) {
	if !s.faultsOn {
		return "", errFaultsOff
	}
	if duration < 0 || duration > maxFaultSeconds {
		return "", errFaultDuration
	}
	if !s.faultTicksFit(duration) {
		return "", errIncidentLimit
	}
	index := -1
	if v != nil {
		index = s.vehicleIndex(v)
	}
	if index < 0 {
		return "", errUnknownPod
	}
	if v.faulted {
		return "", errPodFaulted
	}
	if !s.podFaultTarget(v) {
		return "", errFaultTarget
	}
	if s.pass != nil && s.pass.active {
		return "", errFaultDispatch
	}
	id := s.nextIncidentID()
	record := faultRecord{generation: s.incidentGeneration, serial: s.incidentSerial, kind: podFault, start: s.tick, pod: index}
	if duration > 0 {
		record.end = s.tick + duration*TicksPerSecond
	}
	s.faults = append(s.faults, record)
	v.faulted = true
	countFault(&s.faultCounters.started)
	return id, nil
}

// faultTicksFit reports whether a new fault with duration keeps the
// arithmetic of its record in range. Its end tick and its evacuation tick
// must fit in int64, and nextIncidentID must not wrap the serial.
func (s *Simulation) faultTicksFit(duration int64) bool {
	room := (math.MaxInt64 - s.tick) / TicksPerSecond
	return duration <= room &&
		s.faultSettings.evacuationSeconds <= room &&
		s.incidentSerial < math.MaxUint64
}

// podFaultTarget reports whether a pod fault on v is supported. The pod
// travels, or it is at a berth in a berth activity. A coupling member, an
// approach member, a platoon leader or follower, and a compact queue member
// are not supported.
func (s *Simulation) podFaultTarget(v *vehicle) bool {
	switch {
	case v.couplingID != "":
		return false
	case s.couplingApproachMember(v.Pod.ID):
		return false
	case v.follower != 0:
		return false
	case v.link.leader != 0:
		return false
	case s.compactGroup(v) != nil:
		return false
	}
	switch v.Pod.Activity {
	case Traveling:
		return true
	case Idle, Boarding, Unloading, Continuing, DepartingEmpty:
		return v.Pod.BerthID != ""
	default:
		return false
	}
}

// vehicleIndex returns the index of v in s.vehicles, or -1.
func (s *Simulation) vehicleIndex(v *vehicle) int {
	if index, ok := s.vehicleIndexes[v.Pod.ID]; ok && index < len(s.vehicles) && &s.vehicles[index] == v {
		return index
	}
	for index := range s.vehicles {
		if &s.vehicles[index] == v {
			return index
		}
	}
	return -1
}

// clearFault ends the active fault with the ID. A record that is no longer
// active, also one of an earlier generation, is unknown.
func (s *Simulation) clearFault(id string) error {
	if !s.faultsOn {
		return errFaultsOff
	}
	index := slices.IndexFunc(s.faults, func(r faultRecord) bool { return r.id() == id })
	if index < 0 {
		return errUnknownFault
	}
	s.removeFault(index)
	return nil
}

// removeFault ends the record at index. The faulted pod is no longer
// faulted. A wait report that names the fault ends, so a paused command
// boundary does not show the ID of a removed record. The next admission
// writes each report again.
func (s *Simulation) removeFault(index int) {
	record := s.faults[index]
	s.faults = slices.Delete(s.faults, index, index+1)
	if record.kind == podFault {
		s.vehicles[record.pod].faulted = false
	}
	id := record.id()
	for i := range s.vehicles {
		pod := &s.vehicles[i].Pod
		if pod.BlockedBy == id {
			pod.WaitReason, pod.BlockedBy = NoWait, ""
		}
	}
	countFault(&s.faultCounters.cleared)
}

// faultStage runs in Step after the unloading loop and before dispatch,
// only when faults are on. It clears each record whose duration ended, in
// serial order.
func (s *Simulation) faultStage() {
	for index := 0; index < len(s.faults); {
		if end := s.faults[index].end; end != 0 && end <= s.tick {
			s.removeFault(index)
			continue
		}
		index++
	}
}

// checkFaults checks the fault records (invariant F5) and their faulted
// pods (the record part of F1). The serials of the records increase, so no
// two records have one ID. Each start is from 0 to the current tick, and
// each end is 0 or after its start. Each record names a pod of the fleet,
// and a pod is faulted exactly when one record names it.
func (s *Simulation) checkFaults() error {
	var recorded []bool
	if len(s.faults) > 0 {
		recorded = make([]bool, len(s.vehicles))
	}
	for index, record := range s.faults {
		id := record.id()
		switch {
		case record.kind != podFault:
			return fmt.Errorf("fault %s has the unknown kind %d", id, record.kind)
		case index > 0 && record.serial <= s.faults[index-1].serial:
			return fmt.Errorf("fault %s is not after fault %s in serial order", id, s.faults[index-1].id())
		case record.start < 0 || record.start > s.tick:
			return fmt.Errorf("fault %s starts at tick %d, outside 0 to %d", id, record.start, s.tick)
		case record.end != 0 && record.end <= record.start:
			return fmt.Errorf("fault %s ends at tick %d, not after its start %d", id, record.end, record.start)
		case record.pod < 0 || record.pod >= len(s.vehicles):
			return fmt.Errorf("fault %s names the pod index %d outside the fleet", id, record.pod)
		case recorded[record.pod]:
			return fmt.Errorf("pod %s has two fault records", s.vehicles[record.pod].Pod.ID)
		case !s.vehicles[record.pod].faulted:
			return fmt.Errorf("fault %s names pod %s, which is not faulted", id, s.vehicles[record.pod].Pod.ID)
		}
		recorded[record.pod] = true
	}
	for index := range s.vehicles {
		if s.vehicles[index].faulted && (len(recorded) == 0 || !recorded[index]) {
			return fmt.Errorf("pod %s is faulted with no fault record", s.vehicles[index].Pod.ID)
		}
	}
	return nil
}
