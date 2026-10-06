package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

// maxFaultSeconds is the longest fault duration, and
// maxEvacuationSeconds the longest evacuation delay.
const (
	maxFaultSeconds      = 86_400
	maxEvacuationSeconds = 3_600
)

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
	errUnknownLane   = errors.New("unknown lane")
	errDebrisSegment = errors.New("invalid debris segment")
	errDebrisOverlap = errors.New("debris overlaps a pod or another fault")
	errDebrisClaim   = errors.New("debris meets a reserved resource")
	errDebrisLimit   = errors.New("debris limit reached")
)

// FaultContract selects the fault operations of the incident suspension
// contract. It requires the incident marker. Without it, no saved state or
// stream message has a fault member.
type FaultContract string

// FaultV1Contract permits the fault operations and the fault members.
const FaultV1Contract FaultContract = "fault-v1"

// ErrUnknownFaultContract means that the fault marker is not
// FaultV1Contract.
var ErrUnknownFaultContract = errors.New("unknown fault contract")

// ValidateFaultContracts accepts no fault marker, and FaultV1Contract with
// the incident marker.
func ValidateFaultContracts(fault FaultContract, incident IncidentContract) error {
	switch {
	case fault == "":
		return nil
	case fault != FaultV1Contract:
		return ErrUnknownFaultContract
	case incident == "":
		return errors.New("the fault contract requires the incident contract")
	}
	return nil
}

// faultKind is the kind of a fault record.
type faultKind uint8

// The fault kinds. A pod fault is a mechanical fault of one pod. Debris is
// an obstacle on a lane segment.
const (
	podFault faultKind = iota
	debrisFault
)

// faultRecord is one active fault. Its ID is i<generation>.<serial>.
type faultRecord struct {
	generation, serial uint64
	kind               faultKind
	// start is the tick at creation. end is 0 for a fault without an end.
	// Otherwise the first fault stage with tick >= end clears the fault.
	start, end int64
	// pod is the index in Simulation.vehicles of the faulted pod, for a
	// pod fault only.
	pod int
	// lane is the lane index, and from and to the segment on the lane, for
	// debris only.
	lane     int
	from, to float64
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

// FaultSettings holds the fault settings of a project.
type FaultSettings struct {
	// EvacuationSeconds is the time from fault onset to evacuation, from 0
	// to 3,600 seconds.
	EvacuationSeconds int
}

// SetFaults turns the fault operations and the fault stage on or off, with
// the settings. It refuses a delay out of range, and it refuses to turn
// faults off while a fault is active, because then no clear could end the
// fault. Reset keeps the settings.
func (s *Simulation) SetFaults(enabled bool, settings FaultSettings) error {
	if settings.EvacuationSeconds < 0 || settings.EvacuationSeconds > maxEvacuationSeconds {
		return fmt.Errorf("evacuation delay %d s is outside 0 to %d s", settings.EvacuationSeconds, maxEvacuationSeconds)
	}
	if !enabled && len(s.faults) > 0 {
		return fmt.Errorf("%d faults are active", len(s.faults))
	}
	s.faultsOn = enabled
	s.faultSettings = faultSettings{evacuationSeconds: int64(settings.EvacuationSeconds)}
	return nil
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
	// The preflight refuses each pod that withdrawService refuses, so the
	// call cannot fail. A pod in its fault recovery keeps its hold, and its
	// pickups are not released again.
	if v.withdrawn&faultHold == 0 {
		_ = s.withdrawService(v, faultHold)
	}
	s.surrenderServiceClaims(v)
	v.faulted = true
	if v.Pod.Activity == Traveling {
		v.faultCap = math.Min(v.distance+stoppingDistance(v.Pod.Speed), v.blocks.end(v.reservedThrough))
	}
	v.pending = -1
	s.rebuildBlocked()
	countFault(&s.faultCounters.started)
	return id, nil
}

// surrenderServiceClaims releases each resource of the destination berth
// of v that v owns and that claimKind classifies as claimService
// (amendment D3 of the incident suspension contract). The pod keeps every
// other claim and grant, its route, its destination and its purpose.
// claimKind tests the physical kinds first, so a destination resource in
// the stopping grant of v stays, and an occupied pod has no service claim.
func (s *Simulation) surrenderServiceClaims(v *vehicle) {
	if v.destination.ID == "" {
		return
	}
	for _, r := range berthResources(v.destination) {
		if s.owners[r].isPod(v.Pod.ID) && s.claimKind(v, r) == claimService {
			s.releaseOwned(v, r)
		}
	}
}

// The wait reasons of the incident suspension contract (section 9.6).
// Admission writes the reasons of a faulted pod, with its own fault ID in
// BlockedBy. A healthy pod is blocked by an incident when a fault owns the
// resource that it needs, or a faulted pod owns it. Then BlockedBy is the
// fault ID. A pod that finds no route for its next leg while the blocked
// set is not empty has no forward route, and BlockedBy is empty, because a
// failed search names no single fault.
const (
	FaultBraking      WaitReason = "Fault braking"
	FaultStopped      WaitReason = "Fault stopped"
	BlockedByIncident WaitReason = "Blocked by incident"
	NoForwardRoute    WaitReason = "No forward route"
)

// reportFault writes the wait report of the faulted pod v: "Fault
// braking" while it moves, and "Fault stopped" at rest, with its fault ID.
func (s *Simulation) reportFault(v *vehicle) {
	v.Pod.WaitReason, v.Pod.BlockedBy = FaultStopped, s.podFaultID(v)
	if v.Pod.Speed > 0 {
		v.Pod.WaitReason = FaultBraking
	}
}

// podFaultID returns the ID of the pod fault on v, or an empty string when
// v has no pod fault.
func (s *Simulation) podFaultID(v *vehicle) string {
	if !v.faulted {
		return ""
	}
	for _, record := range s.faults {
		if record.kind == podFault && &s.vehicles[record.pod] == v {
			return record.id()
		}
	}
	return ""
}

// incidentBlocker returns the fault ID when the owner is a fault, or a pod
// with a pod fault. Otherwise it returns false, and the existing wait
// report applies. Without a record, no owner is a fault or a faulted pod.
func (s *Simulation) incidentBlocker(owner resourceOwner) (string, bool) {
	if len(s.faults) == 0 {
		return "", false
	}
	if owner.kind == faultOwnerKind {
		return owner.id, true
	}
	if other := s.ownerVehicle(owner); other != nil && other.faulted {
		return s.podFaultID(other), true
	}
	return "", false
}

// reportIncident writes the report of a healthy pod that an incident
// blocks, and reports true, when the owner is a fault or a faulted pod.
// Otherwise it writes nothing and reports false.
func (s *Simulation) reportIncident(v *vehicle, owner resourceOwner) bool {
	id, ok := s.incidentBlocker(owner)
	if ok {
		v.Pod.WaitReason, v.Pod.BlockedBy = BlockedByIncident, id
	}
	return ok
}

// reportBlockedBerths writes the report of a pod whose terminal berth
// choice failed while the blocked set is not empty. When a berth of the
// destination station is blocked, the pod is blocked by the incident with
// the lowest serial that blocks a berth of the station. Otherwise the
// report of the previous tick ends: the failure has no incident cause that
// one fault ID can name. With an empty blocked set, it writes nothing.
func (s *Simulation) reportBlockedBerths(v *vehicle) {
	if !s.blockedActive() {
		return
	}
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
	station, ok := s.station(v.destinationStation)
	if !ok {
		return
	}
	// The records are in serial order, so the first record that blocks a
	// berth of the station has the lowest serial.
	for _, record := range s.faults {
		id := record.id()
		for _, berth := range station.Berths {
			claims := berthResources(berth)
			if s.blocked.by[claims[0]] == id || s.blocked.by[claims[1]] == id {
				v.Pod.WaitReason, v.Pod.BlockedBy = BlockedByIncident, id
				return
			}
		}
	}
}

// incidentWait reports whether the wait report of v is one that an
// incident causes for a healthy pod.
func incidentWait(v *vehicle) bool {
	return v.Pod.WaitReason == BlockedByIncident || v.Pod.WaitReason == NoForwardRoute
}

// podFaultFootprint returns the resources that the pod fault on v blocks
// (section 6.5 of the incident suspension contract). A traveling pod
// blocks each resource of its grants that it holds at its cap. The grants
// and the cap do not change while the fault lasts, so the footprint is
// fixed. A pod at a berth blocks the berth and its node. The pod owns each
// resource of its footprint.
func (s *Simulation) podFaultFootprint(v *vehicle) []resource {
	if v.Pod.Activity == Traveling {
		return v.footprint(v.reservedThrough, v.faultCap)
	}
	station, _ := s.station(v.Pod.StationID)
	berth, _ := station.berth(v.Pod.BerthID)
	claims := berthResources(berth)
	return claims[:]
}

// rebuildBlocked sets the blocked set from the footprints of the records.
// Each operation that changes a footprint calls it before it returns: a
// fault start, a clear, and the arrival of a faulted pod.
func (s *Simulation) rebuildBlocked() {
	s.setBlocked(s.faultFootprints())
}

// faultFootprints returns the footprints of the records, in record order.
func (s *Simulation) faultFootprints() []faultFootprint {
	var footprints []faultFootprint
	for _, record := range s.faults {
		var resources []resource
		if record.kind == debrisFault {
			resources = s.debrisFootprint(record.lane, record.from, record.to)
		} else {
			resources = s.podFaultFootprint(&s.vehicles[record.pod])
		}
		footprints = append(footprints, faultFootprint{id: record.id(), resources: resources})
	}
	return footprints
}

// remainingRouteBlocked reports whether an active fault blocks a lane of
// the remaining route of v, from its current lane to its endpoint.
func (s *Simulation) remainingRouteBlocked(v *vehicle) bool {
	if s.blocked.lanes == nil || v.blocks.len() == 0 {
		return false
	}
	return s.routeBlocked(v.Route[v.blocks.locate(v.blockIndex, 0):])
}

// routeBlocked reports whether an active fault blocks a lane of route.
func (s *Simulation) routeBlocked(route []Lane) bool {
	if s.blocked.lanes == nil {
		return false
	}
	for _, lane := range route {
		if index, ok := s.graph.lanes[lane.ID]; ok && s.blocked.lanes[index] {
			return true
		}
	}
	return false
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
	s.removeFault(index, false)
	return nil
}

// removeFault ends the record at index. The faulted pod is no longer
// faulted, and it has no cap. It returns to service, unless its fault hold
// owns the purpose of a fault recovery: then the hold release rule
// releases the hold after the arrival. Debris releases its footprint: at
// once at a command boundary, or at the release boundary of the tick when
// inStage is true, so that no pod takes a resource in the tick in which it
// is released. A wait report that names the fault ends, so a paused command
// boundary does not show the ID of a removed record. The next admission
// writes each report again.
func (s *Simulation) removeFault(index int, inStage bool) {
	record := s.faults[index]
	s.faults = slices.Delete(s.faults, index, index+1)
	if record.kind == debrisFault {
		s.releaseDebris(record, inStage)
	}
	if record.kind == podFault {
		v := &s.vehicles[record.pod]
		v.faulted, v.faultCap = false, 0
		// The pod has the hold (F1) and is not a coupling member (F2), so
		// the call cannot fail.
		if v.op.owner != faultHold {
			_ = s.restoreService(v, faultHold)
		}
	}
	s.rebuildBlocked()
	s.endFaultReports(record.id())
	countFault(&s.faultCounters.cleared)
}

// endFaultReports ends each wait report that names one of the fault IDs.
func (s *Simulation) endFaultReports(ids ...string) {
	for i := range s.vehicles {
		pod := &s.vehicles[i].Pod
		if pod.BlockedBy != "" && slices.Contains(ids, pod.BlockedBy) {
			pod.WaitReason, pod.BlockedBy = NoWait, ""
		}
	}
}

// faultStage runs in Step after the unloading loop and before dispatch,
// only when faults are on (section 5.5 of the incident suspension
// contract). It clears each record whose duration ended, in serial order.
// Then it evacuates each faulted pod at rest with an active rider once its
// evacuation tick is reached. The clears come first, so a fault that ends
// at or before its evacuation tick never evacuates. Then it runs the
// reroute pass when it is due, and it applies the hold release rule. Last,
// it counts the healthy pods that wait for an incident.
func (s *Simulation) faultStage() {
	for index := 0; index < len(s.faults); {
		if end := s.faults[index].end; end != 0 && end <= s.tick {
			s.removeFault(index, true)
			continue
		}
		index++
	}
	for _, record := range s.faults {
		if record.kind != podFault {
			continue
		}
		v := &s.vehicles[record.pod]
		if s.tick >= s.evacuateTick(record) && v.Pod.Speed == 0 && v.RidersAboard() > 0 && s.evacuate(v) == nil {
			countFault(&s.faultCounters.evacuations)
		}
	}
	s.reroutePass()
	s.releaseFaultHolds()
	// The reports are those of the previous tick: admission and motion
	// of this tick have not run yet.
	for index := range s.vehicles {
		if v := &s.vehicles[index]; !v.faulted && incidentWait(v) {
			countFault(&s.faultCounters.faultWaitTicks)
		}
	}
}

// evacuateTick returns the first tick at which the fault stage evacuates
// the pod of the record. The preflight of the fault start keeps it in
// int64.
func (s *Simulation) evacuateTick(record faultRecord) int64 {
	return record.start + s.faultSettings.evacuationSeconds*TicksPerSecond
}

// releaseFaultHolds applies the hold release rule (section 5.6 of the
// incident suspension contract). A pod with the fault hold returns to
// service when it has no pod fault, the hold owns no purpose, and the pod
// is not a coupling or approach member. A fault recovery thus keeps its
// hold until its arrival clears the purpose (W5), and a member keeps it
// until the split.
func (s *Simulation) releaseFaultHolds() {
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.withdrawn&faultHold == 0 || v.faulted || v.op.owner == faultHold || v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) {
			continue
		}
		// The tests above are the refusals of restoreService, so the call
		// cannot fail.
		_ = s.restoreService(v, faultHold)
	}
}

// checkFaults checks the fault records (invariant F5), their faulted pods
// (F1, F2 and the bounds of F3), and the debris owners (F6 to F8). The
// serials of the records increase, so no two records have one ID. Each
// start is from 0 to the current tick, and each end is 0 or after its
// start. Each pod record names a pod of the fleet with the fault hold, and
// a pod is faulted exactly when one record names it. A faulted pod is in
// no group, and a faulted traveling pod has a finite cap between its
// distance and its grant end. At most maxDebrisFaults records are debris,
// each on a segment that a debris start accepts.
//
// With faults on, it also checks F10 and F11: the blocked set is the set
// of the record footprints, and the fault hold is the only hold. With
// faults off, the stage 1 operations can use each hold, and no record
// exists.
func (s *Simulation) checkFaults() error {
	if len(s.faultReleased) > 0 {
		return fmt.Errorf("%d released debris resources stay after the release boundary", len(s.faultReleased))
	}
	var recorded []bool
	if len(s.faults) > 0 {
		recorded = make([]bool, len(s.vehicles))
	}
	debris := 0
	for index, record := range s.faults {
		id := record.id()
		switch {
		case record.kind != podFault && record.kind != debrisFault:
			return fmt.Errorf("fault %s has the unknown kind %d", id, record.kind)
		case index > 0 && record.serial <= s.faults[index-1].serial:
			return fmt.Errorf("fault %s is not after fault %s in serial order", id, s.faults[index-1].id())
		case record.start < 0 || record.start > s.tick:
			return fmt.Errorf("fault %s starts at tick %d, outside 0 to %d", id, record.start, s.tick)
		case record.end != 0 && record.end <= record.start:
			return fmt.Errorf("fault %s ends at tick %d, not after its start %d", id, record.end, record.start)
		}
		if record.kind == debrisFault {
			debris++
			if debris > maxDebrisFaults {
				return fmt.Errorf("more than %d debris faults are active", maxDebrisFaults)
			}
			if _, err := s.debrisSegment(record.lane, record.from, record.to); err != nil {
				return fmt.Errorf("debris %s on lane %d from %g to %g: %w", id, record.lane, record.from, record.to, err)
			}
			continue
		}
		switch {
		case record.pod < 0 || record.pod >= len(s.vehicles):
			return fmt.Errorf("fault %s names the pod index %d outside the fleet", id, record.pod)
		case recorded[record.pod]:
			return fmt.Errorf("pod %s has two fault records", s.vehicles[record.pod].Pod.ID)
		case !s.vehicles[record.pod].faulted:
			return fmt.Errorf("fault %s names pod %s, which is not faulted", id, s.vehicles[record.pod].Pod.ID)
		case s.vehicles[record.pod].withdrawn&faultHold == 0:
			return fmt.Errorf("fault %s names pod %s, which has no fault hold", id, s.vehicles[record.pod].Pod.ID)
		}
		recorded[record.pod] = true
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if !v.faulted {
			continue
		}
		if len(recorded) == 0 || !recorded[index] {
			return fmt.Errorf("pod %s is faulted with no fault record", v.Pod.ID)
		}
		if err := s.operationalMember(v); err != nil {
			return fmt.Errorf("faulted %w", err)
		}
		if v.Pod.Activity == Traveling && (v.reservedThrough < 0 || !finite(v.faultCap) || v.distance > v.faultCap || v.faultCap > v.blocks.end(v.reservedThrough)) {
			return fmt.Errorf("faulted pod %s has the cap %g outside its distance %g and its grants", v.Pod.ID, v.faultCap, v.distance)
		}
	}
	if err := s.checkDebrisOwners(); err != nil || !s.faultsOn {
		return err
	}
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.withdrawn&^faultHold != 0 {
			return fmt.Errorf("pod %s has the service holds %#x, not only the fault hold", v.Pod.ID, v.withdrawn)
		}
	}
	return s.checkBlocked()
}

// checkBlocked checks invariant F10: the blocked set is the set of the
// footprints of the records, with the same fault IDs.
func (s *Simulation) checkBlocked() error {
	want := s.blockedFrom(s.faultFootprints())
	if !slices.Equal(want.lanes, s.blocked.lanes) || !maps.Equal(want.berths, s.blocked.berths) || !maps.Equal(want.by, s.blocked.by) {
		return errors.New("the blocked set differs from the footprints of the fault records")
	}
	return nil
}

// checkDebrisOwners checks invariants F6 and F7. The debris footprints are
// disjoint from each other and from each pod fault footprint, and the
// debris owns each resource of its footprint. No other resource has a
// fault owner.
func (s *Simulation) checkDebrisOwners() error {
	var held map[resource]string
	for _, record := range s.faults {
		if record.kind != debrisFault {
			continue
		}
		id := record.id()
		for _, r := range s.debrisFootprint(record.lane, record.from, record.to) {
			if other, ok := held[r]; ok {
				return fmt.Errorf("debris %s and %s share the resource %v", other, id, r)
			}
			if held == nil {
				held = make(map[resource]string)
			}
			held[r] = id
			if owner := s.owners[r]; owner != (resourceOwner{kind: faultOwnerKind, id: id}) {
				return fmt.Errorf("debris %s does not own the resource %v of its footprint, %q does", id, r, owner)
			}
		}
	}
	if held != nil {
		for _, record := range s.faults {
			if record.kind != podFault {
				continue
			}
			for _, r := range s.podFaultFootprint(&s.vehicles[record.pod]) {
				if id, ok := held[r]; ok {
					return fmt.Errorf("debris %s meets the footprint of fault %s at %v", id, record.id(), r)
				}
			}
		}
	}
	for r, owner := range s.owners {
		if id, ok := held[r]; owner.kind == faultOwnerKind && (!ok || id != owner.id) {
			return fmt.Errorf("fault owner %s holds the resource %v outside the footprint of its debris", owner.id, r)
		}
	}
	return nil
}
