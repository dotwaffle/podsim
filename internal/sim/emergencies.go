package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// maxEmergencies is the number of active emergency records (product choice
// P19 of the incident emergency contract).
const maxEmergencies = 4

// The errors of the emergency start. A refused start changes nothing.
var (
	errEmergenciesOff    = errors.New("emergencies are not enabled")
	errEmergencyDispatch = errors.New("emergency start during a dispatch pass")
	errNoPassenger       = errors.New("pod carries no passenger")
	errPodEmergency      = errors.New("pod already has an emergency")
	errEmergencyLimit    = errors.New("emergency limit reached")
	errOrderNotAboard    = errors.New("order is not aboard the pod")
)

// emergencyRecord is one active emergency. Its ID is i<generation>.<serial>.
// The phase is not stored: each reader derives it from the purpose of the
// pod (section 4.2 of the incident emergency contract).
type emergencyRecord struct {
	generation, serial uint64
	// start is the tick of the start.
	start int64
	// pod is the index in Simulation.vehicles of the emergency pod.
	pod int
	// order is the order ID of the party. The record holds the order ID
	// and not the rider index, because the rider indexes change while a
	// deferred pod lets riders alight or board.
	order int
}

// id returns the incident ID of the record.
func (r emergencyRecord) id() string {
	return incidentID(r.generation, r.serial)
}

// emergencyCounters counts the emergency events. Each counter stops at
// math.MaxInt64. See addCount.
type emergencyCounters struct {
	// started counts the records that started, and ended the records that
	// the emergency stage ended. A restore or a reset does not count.
	started, ended int64
	// emergencyTicks is the sum over the emergency stages of the number of
	// records after the stage.
	emergencyTicks int64
}

// addCount adds n, which is not negative, to counter. At math.MaxInt64 the
// counter keeps its value, so a counter never stops a transition.
func addCount(counter *int64, n int64) {
	if n > math.MaxInt64-*counter {
		*counter = math.MaxInt64
		return
	}
	*counter += n
}

// partyIndex returns the index in Riders of the active rider of v with the
// order ID, or -1.
func (v *vehicle) partyIndex(order int) int {
	return slices.IndexFunc(v.Riders, func(rider Request) bool { return rider.ID == order && !rider.Completed })
}

// emergencyOf returns the index in s.emergencies of the record that names
// v, or -1.
func (s *Simulation) emergencyOf(v *vehicle) int {
	return slices.IndexFunc(s.emergencies, func(r emergencyRecord) bool { return &s.vehicles[r.pod] == v })
}

// Emergency starts an emergency for a party of pod podID at a command
// boundary, and then runs the monitor once (section 5.2 of the incident
// emergency contract). orderID 0 selects the first active rider in Riders
// order. It returns the record ID. It checks each precondition of section
// 6.1, in order, before its first write, and a refusal changes nothing. A
// paused simulation accepts the start.
func (s *Simulation) Emergency(podID string, orderID int) (string, error) {
	if !s.emergenciesOn {
		return "", errEmergenciesOff
	}
	v := s.findVehicle(podID)
	index := -1
	if v != nil {
		index = s.vehicleIndex(v)
	}
	switch {
	case index < 0:
		return "", errUnknownPod
	case s.pass != nil && s.pass.active:
		return "", errEmergencyDispatch
	case !v.carriesPassengers():
		return "", errNoPassenger
	case s.emergencyOf(v) >= 0:
		return "", errPodEmergency
	case len(s.emergencies) >= maxEmergencies:
		return "", errEmergencyLimit
	}
	party := slices.IndexFunc(v.Riders, func(rider Request) bool { return !rider.Completed && (orderID == 0 || rider.ID == orderID) })
	if party < 0 {
		return "", errOrderNotAboard
	}
	if s.incidentSerial == math.MaxUint64 {
		return "", errIncidentLimit
	}
	id := s.nextIncidentID()
	record := emergencyRecord{generation: s.incidentGeneration, serial: s.incidentSerial, start: s.tick, pod: index, order: v.Riders[party].ID}
	s.emergencies = append(s.emergencies, record)
	addCount(&s.emergencyCounters.started, 1)
	// A coupling or approach member gets its hold after it leaves the
	// group (section 5.6). For every other pod, the preflight meets each
	// precondition of withdrawService, so the call cannot fail. Unless the
	// pod is faulted, this is the first hold, and it releases the pending
	// pickups of the pod.
	if v.couplingID == "" && !s.couplingApproachMember(v.Pod.ID) {
		_ = s.withdrawService(v, emergencyHold)
	}
	s.advanceEmergency(record)
	s.observe()
	return id, nil
}

// SetEmergencies turns the emergency start and the emergency stage on or
// off. The session turns them on for a project with the emergency marker
// (section 10.1 of the incident emergency contract). It refuses to turn
// emergencies off while a record exists, because then no emergency stage
// could end the record. Reset keeps the switch.
func (s *Simulation) SetEmergencies(enabled bool) error {
	if !enabled && len(s.emergencies) > 0 {
		return fmt.Errorf("%d emergencies are active", len(s.emergencies))
	}
	s.emergenciesOn = enabled
	return nil
}

// emergencyStage runs in Step after the fault stage and before dispatch,
// only when emergencies are on (section 5.3 of the incident emergency
// contract). For each record, in serial order, it ends the record when the
// pod carries no passenger, or when the record is deferred and the party
// is not an active rider. Otherwise it advances the record. Last, it adds
// the number of remaining records to emergencyTicks.
//
// It runs after the unloading loop and the fault stage, so an unload or an
// evacuation in this tick ends its record in this tick. It runs before
// dispatch, so a pod that the end releases is supply in this tick.
func (s *Simulation) emergencyStage() {
	for index := 0; index < len(s.emergencies); {
		record := s.emergencies[index]
		v := &s.vehicles[record.pod]
		if !v.carriesPassengers() || v.op.purpose == opService && v.partyIndex(record.order) < 0 {
			s.endEmergency(index)
			continue
		}
		s.advanceEmergency(record)
		index++
	}
	addCount(&s.emergencyCounters.emergencyTicks, int64(len(s.emergencies)))
}

// advanceEmergency moves the pod of the record toward its unload (section
// 5.4 of the incident emergency contract). A faulted pod and a coupling or
// approach member stay deferred. Otherwise the pod gets the emergency hold
// when it lacks it. A deferred pod at a berth then starts its unload
// there. A traveling pod that can divert binds to the station of the
// station choice on its cadence. Each other deferred pod keeps its route:
// a pod in its arrival chain arrives at its berth, and the next stage
// starts the unload there. A bound or unloading pod keeps its station.
//
// Each operation checks its own preconditions before any change. A
// refusal leaves the pod deferred, and the next stage tries again.
func (s *Simulation) advanceEmergency(record emergencyRecord) {
	v := &s.vehicles[record.pod]
	if v.faulted || v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) {
		return
	}
	if v.withdrawn&emergencyHold == 0 {
		// The pod is not faulted and not a coupling or approach member,
		// and the stage and the start run outside dispatch, so the call
		// cannot fail.
		_ = s.withdrawService(v, emergencyHold)
	}
	// A bound or unloading pod needs no step: arrive and the unloading
	// loop do the rest.
	if v.op.purpose != opService {
		return
	}
	party := v.partyIndex(record.order)
	if party < 0 {
		return
	}
	switch v.Pod.Activity {
	case Boarding, Continuing, Unloading:
		// startOperationalUnload refuses a platoon or compact member,
		// which stays deferred.
		_ = s.startOperationalUnload(v, emergencyHold, 1<<party)
	default:
		s.chooseEmergencyStation(record, 1<<party)
	}
}

// endEmergency removes the record at index, counts it, and returns the pod
// to service from the emergency hold when it has the hold (section 5.5 of
// the incident emergency contract). The stage ends a record only when the
// purpose is not owned by the hold and the pod is not a coupling member,
// so restoreService does not refuse. A faulted pod keeps its fault hold.
func (s *Simulation) endEmergency(index int) {
	record := s.emergencies[index]
	s.emergencies = slices.Delete(s.emergencies, index, index+1)
	s.dropEmergencyMiss(record.serial)
	addCount(&s.emergencyCounters.ended, 1)
	if v := &s.vehicles[record.pod]; v.withdrawn&emergencyHold != 0 {
		_ = s.restoreService(v, emergencyHold)
	}
}

// checkEmergencies checks invariants E1 to E5, E7, and E8 of the incident
// emergency contract (section 8). CheckContract checks E6 with the coupling
// members, and checkFaults the state part of F11. E2 and E4 hold only with
// emergencies on: without them, the stage 1 operations can set the
// emergency hold and purpose 1 with another owner.
func (s *Simulation) checkEmergencies() error {
	if !s.emergenciesOn && len(s.emergencies) > 0 {
		return fmt.Errorf("E7: %d emergency records exist with emergencies off", len(s.emergencies))
	}
	if len(s.emergencies) > maxEmergencies {
		return fmt.Errorf("E1: %d emergency records, more than %d", len(s.emergencies), maxEmergencies)
	}
	recorded := make([]bool, len(s.vehicles))
	for index, record := range s.emergencies {
		if err := s.checkEmergencyRecord(index, record); err != nil {
			return err
		}
		if recorded[record.pod] {
			return fmt.Errorf("E1: pod %s has two emergency records", s.vehicles[record.pod].Pod.ID)
		}
		recorded[record.pod] = true
	}
	if !s.emergenciesOn {
		return nil
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		switch {
		case v.withdrawn&emergencyHold != 0 && !recorded[index]:
			return fmt.Errorf("E2: pod %s has the emergency hold and no emergency record", v.Pod.ID)
		case v.op.purpose == opEmergencyUnload && v.op.owner != emergencyHold:
			return fmt.Errorf("E4: the emergency unload of pod %s has the owner %#x", v.Pod.ID, v.op.owner)
		case v.op.purpose == opEmergencyUnload && !recorded[index]:
			return fmt.Errorf("E4: pod %s has an emergency unload and no emergency record", v.Pod.ID)
		}
	}
	return nil
}

// checkEmergencyRecord checks one record: E1 for its serial, start, and
// pod, and E3, E5, and E8 for its pod.
func (s *Simulation) checkEmergencyRecord(index int, record emergencyRecord) error {
	id := record.id()
	switch {
	case record.serial == 0 || record.serial > s.incidentSerial:
		return fmt.Errorf("E1: emergency %s has a serial outside 1 to %d", id, s.incidentSerial)
	case index > 0 && record.serial <= s.emergencies[index-1].serial:
		return fmt.Errorf("E1: emergency %s is not after emergency %s in serial order", id, s.emergencies[index-1].id())
	case record.start < 0 || record.start > s.tick:
		return fmt.Errorf("E1: emergency %s starts at tick %d, outside 0 to %d", id, record.start, s.tick)
	case record.pod < 0 || record.pod >= len(s.vehicles):
		return fmt.Errorf("E1: emergency %s names the pod index %d outside the fleet", id, record.pod)
	case slices.ContainsFunc(s.faults, func(fault faultRecord) bool { return fault.serial == record.serial }):
		return fmt.Errorf("E1: emergency %s has the serial of a fault record", id)
	}
	v := &s.vehicles[record.pod]
	switch v.op.purpose {
	case opService:
		return nil
	case opEmergencyUnload:
		party := v.partyIndex(record.order)
		switch {
		case party < 0:
			return fmt.Errorf("E3: the party %d of emergency %s is not an active rider of pod %s", record.order, id, v.Pod.ID)
		case v.op.interrupt != 1<<party:
			return fmt.Errorf("E3: pod %s interrupts the riders %#x, not only the party of emergency %s", v.Pod.ID, v.op.interrupt, id)
		case !v.carriesPassengers():
			return fmt.Errorf("E5: pod %s of emergency %s carries no passenger", v.Pod.ID, id)
		}
		return nil
	default:
		if v.RidersAboard() > 0 {
			return fmt.Errorf("E8: pod %s of emergency %s has the purpose %d and active riders", v.Pod.ID, id, v.op.purpose)
		}
		return nil
	}
}
