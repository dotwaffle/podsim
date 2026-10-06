package sim

import (
	"errors"
	"fmt"
	"slices"
)

// EmergencyContract selects the emergency operations of the incident
// emergency contract. It requires the incident marker. Without it, no
// saved state has an emergency member.
type EmergencyContract string

// EmergencyV1Contract permits the emergency operations and the emergency
// members.
const EmergencyV1Contract EmergencyContract = "emergency-v1"

// ErrUnknownEmergencyContract means that the emergency marker is not
// EmergencyV1Contract.
var ErrUnknownEmergencyContract = errors.New("unknown emergency contract")

// ValidateEmergencyContracts accepts no emergency marker, and
// EmergencyV1Contract with the incident marker. The emergency marker does
// not need the fault marker.
func ValidateEmergencyContracts(emergency EmergencyContract, incident IncidentContract) error {
	switch {
	case emergency == "":
		return nil
	case emergency != EmergencyV1Contract:
		return ErrUnknownEmergencyContract
	case incident == "":
		return errors.New("the emergency contract requires the incident contract")
	}
	return nil
}

// SavedEmergencies holds the emergency records and the emergency counters
// of a saved state. They need the emergency marker.
type SavedEmergencies struct {
	// Records holds the active records in serial order.
	Records  []SavedEmergency  `json:"records,omitempty"`
	Counters EmergencyCounters `json:"counters,omitzero"`
}

// SavedEmergency is one saved emergency record. Its ID is
// i<Generation>.<Serial>. Pod is the index of the pod in SavedState.Pods,
// and Order is the order ID of the party. The phase is in the purpose of
// the saved pod. The session adapter writes the record as one tuple.
type SavedEmergency struct {
	Generation uint64 `json:"-"`
	Serial     uint64 `json:"-"`
	Start      int64  `json:"-"`
	Pod        int    `json:"-"`
	Order      int    `json:"-"`
}

// EmergencyCounters holds the emergency counters (section 10.5 of the
// incident emergency contract). Each counter stops at math.MaxInt64.
type EmergencyCounters struct {
	// Started counts the records that started, and Ended the records that
	// the emergency stage ended. A restore or a reset does not count.
	Started int64 `json:"started,omitzero"`
	Ended   int64 `json:"ended,omitzero"`
	// EmergencyTicks is the sum over the emergency stages of the number of
	// records after the stage.
	EmergencyTicks int64 `json:"emergencyTicks,omitzero"`
}

// errInvalidEmergencies marks saved emergency data that makes the whole
// save invalid. The check runs before either restore tier, so the save
// gets no logical fallback.
var errInvalidEmergencies = errors.New("invalid saved emergency")

// exported returns the saved form of the counters.
func (c emergencyCounters) exported() EmergencyCounters {
	return EmergencyCounters{Started: c.started, Ended: c.ended, EmergencyTicks: c.emergencyTicks}
}

// counters returns the native form of saved counters.
func (c EmergencyCounters) counters() emergencyCounters {
	return emergencyCounters{started: c.Started, ended: c.Ended, emergencyTicks: c.EmergencyTicks}
}

// record returns the native record of a saved record.
func (e SavedEmergency) record() emergencyRecord {
	return emergencyRecord{generation: e.Generation, serial: e.Serial, start: e.Start, pod: e.Pod, order: e.Order}
}

// exportEmergencies returns the saved records and counters, or nil when no
// record is active and each counter is 0.
func (s *Simulation) exportEmergencies() *SavedEmergencies {
	if len(s.emergencies) == 0 && s.emergencyCounters == (emergencyCounters{}) {
		return nil
	}
	saved := &SavedEmergencies{Counters: s.emergencyCounters.exported()}
	for _, record := range s.emergencies {
		saved.Records = append(saved.Records, SavedEmergency{
			Generation: record.generation, Serial: record.serial, Start: record.start, Pod: record.pod, Order: record.order,
		})
	}
	return saved
}

// checkSavedEmergencies checks the saved emergency data before either
// restore tier (section 11.5 of the incident emergency contract). It reads
// only the saved records and pod tuples, so the logical tier cannot remove
// the evidence that it needs. Records and counters need the marker. With the
// marker, the records meet E1, and the saved pods meet E2 to E5 and E8.
// Each failure makes the whole save invalid.
func checkSavedEmergencies(input RestoreStateInput) error {
	if err := ValidateEmergencyContracts(input.EmergencyContract, input.IncidentContract); err != nil {
		return err
	}
	state := input.State
	if input.EmergencyContract == "" {
		if state.Emergencies != nil {
			return errors.New("saved emergencies need the emergency contract")
		}
		return nil
	}
	parties, err := savedEmergencyParties(state)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidEmergencies, err)
	}
	for index, pod := range state.Pods {
		if err := checkSavedEmergencyPod(pod, parties[index]); err != nil {
			return fmt.Errorf("%w: %w", errInvalidEmergencies, err)
		}
	}
	return nil
}

// savedEmergencyParties checks the saved records and counters by invariant
// E1, and returns the order ID of the party of each saved pod, or 0 for a
// pod that no record names.
func savedEmergencyParties(state SavedState) ([]int, error) {
	parties := make([]int, len(state.Pods))
	saved := state.Emergencies
	if saved == nil {
		return parties, nil
	}
	counters := saved.Counters
	if counters.Started < 0 || counters.Ended < 0 || counters.EmergencyTicks < 0 {
		return nil, errors.New("an emergency counter is negative")
	}
	if len(saved.Records) > MaxEmergencies {
		return nil, fmt.Errorf("%d emergency records, more than %d", len(saved.Records), MaxEmergencies)
	}
	var faults []SavedFault
	if state.Faults != nil {
		faults = state.Faults.Records
	}
	for index, record := range saved.Records {
		id := incidentID(record.Generation, record.Serial)
		switch {
		case record.Serial == 0 || record.Serial > state.IncidentSerial:
			return nil, fmt.Errorf("emergency %s has a serial outside 1 to %d", id, state.IncidentSerial)
		case index > 0 && record.Serial <= saved.Records[index-1].Serial:
			return nil, fmt.Errorf("emergency %s is not after the record before it in serial order", id)
		case slices.ContainsFunc(faults, func(fault SavedFault) bool { return fault.Serial == record.Serial }):
			return nil, fmt.Errorf("emergency %s has the serial of a fault record", id)
		case record.Start < 0 || record.Start > state.Tick:
			return nil, fmt.Errorf("emergency %s starts at tick %d, outside 0 to %d", id, record.Start, state.Tick)
		case record.Order <= 0:
			return nil, fmt.Errorf("emergency %s has the order %d, which is not positive", id, record.Order)
		case record.Pod < 0 || record.Pod >= len(state.Pods) || parties[record.Pod] != 0:
			return nil, fmt.Errorf("emergency %s names the pod index %d out of range or with another record", id, record.Pod)
		}
		parties[record.Pod] = record.Order
	}
	return parties, nil
}

// checkSavedEmergencyPod checks a saved pod by invariants E2 to E5 and E8.
// party is the order ID of the party of the record that names the pod, or
// 0 when no record names it.
func checkSavedEmergencyPod(pod SavedPod, party int) error {
	purpose := opPurpose(pod.Purpose)
	switch {
	case serviceHold(pod.Withdrawn)&emergencyHold != 0 && party == 0:
		return fmt.Errorf("E2: pod %s has the emergency hold and no emergency record", pod.ID)
	case purpose == opEmergencyUnload && serviceHold(pod.Owner) != emergencyHold:
		return fmt.Errorf("E4: the emergency unload of pod %s has the owner %#x", pod.ID, pod.Owner)
	case purpose == opEmergencyUnload && party == 0:
		return fmt.Errorf("E4: pod %s has an emergency unload and no emergency record", pod.ID)
	case party == 0 || purpose == opService:
		return nil
	}
	active := slices.ContainsFunc(pod.Riders, func(rider SavedRequest) bool { return !rider.Completed })
	if purpose != opEmergencyUnload {
		if active {
			return fmt.Errorf("E8: pod %s of an emergency has the purpose %d and active riders", pod.ID, purpose)
		}
		return nil
	}
	if !active {
		return fmt.Errorf("E5: pod %s of an emergency unload carries no passenger", pod.ID)
	}
	index := slices.IndexFunc(pod.Riders, func(rider SavedRequest) bool { return rider.ID == party && !rider.Completed })
	switch {
	case index < 0:
		return fmt.Errorf("E3: the party %d of the emergency of pod %s is not an active rider", party, pod.ID)
	case index >= 32 || pod.Interrupt != 1<<index:
		return fmt.Errorf("E3: pod %s interrupts the riders %#x, not only the party %d", pod.ID, pod.Interrupt, party)
	}
	return nil
}

// CheckIncidentPolicy is the production policy validator of a saved state
// (section 8 of the incident emergency contract). It reads only the saved
// state. With the incident marker and without the emergency marker, it
// refuses a saved pod with the emergency hold, in its holds or as the
// owner of its purpose, and a saved pod with an emergency unload, because
// no emergency record exists without the marker (E7). No production path
// makes these states. The stage 1 test entry IncidentForTest can, and
// RestoreState still restores them, so the session runs this check before
// RestoreState. With the emergency marker, RestoreState refuses the same
// states by E2 and E4.
func CheckIncidentPolicy(input RestoreStateInput) error {
	if input.IncidentContract == "" || input.EmergencyContract != "" {
		return nil
	}
	for _, pod := range input.State.Pods {
		switch {
		case serviceHold(pod.Withdrawn)&emergencyHold != 0 || serviceHold(pod.Owner) == emergencyHold:
			return fmt.Errorf("saved pod %s has the emergency hold without the emergency contract", pod.ID)
		case opPurpose(pod.Purpose) == opEmergencyUnload:
			return fmt.Errorf("saved pod %s has an emergency unload without the emergency contract", pod.ID)
		}
	}
	return nil
}

// setEmergencyContract gives a restored simulation the emergency marker of
// input: with the marker, the emergency start and the emergency stage are
// on. The session turns them off again for the fleet of the traffic demo,
// which keeps the marker.
func (s *Simulation) setEmergencyContract(input RestoreStateInput) {
	s.emergencyContract = input.EmergencyContract
	s.emergenciesOn = input.EmergencyContract != ""
}

// restoreEmergencies restores the records and the counters after each pod
// is in place (section 10.7 of the incident emergency contract). A record
// pod that the tier demotes fails the tier: no record ends to keep the
// other pods in place, and the stage 1 fallback rules then apply. The
// phase of each record comes from the restored purpose of its pod, and a
// deferred record advances in the next emergency stage.
func (r *physicalRestore) restoreEmergencies() error {
	saved := r.state.Emergencies
	if saved == nil {
		return nil
	}
	s := r.s
	for _, e := range saved.Records {
		if r.demoted[e.Pod] {
			return fmt.Errorf("the restore would demote pod %s, which has emergency %s", s.vehicles[e.Pod].Pod.ID, incidentID(e.Generation, e.Serial))
		}
	}
	s.emergencies = make([]emergencyRecord, 0, len(saved.Records))
	for _, e := range saved.Records {
		s.emergencies = append(s.emergencies, e.record())
	}
	s.emergencyCounters = saved.Counters.counters()
	return nil
}

// dropSavedEmergencies ends each saved record in the logical tier
// (section 10.7 of the incident emergency contract). The counters stay.
// With the emergency marker, each pod returns to service from the
// emergency hold: the tier puts each pod at its initial berth with no
// purpose, so the hold owns no purpose.
// Without the marker, the pods keep the hold, as the stage 1 tier keeps
// each hold. It returns the number of records that ended.
func (s *Simulation) dropSavedEmergencies(saved *SavedEmergencies) (int, error) {
	if s.emergenciesOn {
		for index := range s.vehicles {
			v := &s.vehicles[index]
			if v.withdrawn&emergencyHold == 0 {
				continue
			}
			if err := s.restoreService(v, emergencyHold); err != nil {
				return 0, err
			}
		}
	}
	if saved == nil {
		return 0, nil
	}
	s.emergencyCounters = saved.Counters.counters()
	return len(saved.Records), nil
}
