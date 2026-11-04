package sim

import (
	"errors"
	"fmt"
	"slices"
)

// SavedEmergencies holds the emergency records, the emergency counters, and
// the emergency sampler of a saved state. They need the emergency switch.
type SavedEmergencies struct {
	// Records holds the active records in serial order.
	Records  []SavedEmergency  `json:"records,omitempty"`
	Counters EmergencyCounters `json:"counters,omitzero"`
	// Random and NextTick are the members of the same name of SavedFaults,
	// for the emergency sampler.
	Random   []byte `json:"random,omitzero"`
	NextTick *int64 `json:"nextTick,omitzero"`
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
// incident emergency contract). Each counter stops at MaxCounter.
type EmergencyCounters struct {
	// Started counts the records that started, and Ended the records that
	// the emergency stage ended. A restore or a reset does not count.
	Started int64 `json:"started,omitzero"`
	Ended   int64 `json:"ended,omitzero"`
	// EmergencyTicks is the sum over the emergency stages of the number of
	// records after the stage.
	EmergencyTicks int64 `json:"emergencyTicks,omitzero"`
	// Sampled counts the sampler events that came due, and Skipped those
	// with no start. Only the sampler changes them.
	Sampled int64 `json:"sampled,omitzero"`
	Skipped int64 `json:"skipped,omitzero"`
	// EndedTicks is the sum of the lengths of the records that ended, and
	// MaxTicks the longest of them. They move only while the emergency
	// sampler is live.
	EndedTicks int64 `json:"endedTicks,omitzero"`
	MaxTicks   int64 `json:"maxTicks,omitzero"`
}

// errInvalidEmergencies marks saved emergency data that makes the whole
// save invalid. The check runs before either restore tier, so the save
// gets no logical fallback.
var errInvalidEmergencies = errors.New("invalid saved emergency")

// exported returns the saved form of the counters.
func (c emergencyCounters) exported() EmergencyCounters {
	return EmergencyCounters{Started: c.started, Ended: c.ended, EmergencyTicks: c.emergencyTicks,
		Sampled: c.sampled, Skipped: c.skipped, EndedTicks: c.endedTicks, MaxTicks: c.maxTicks}
}

// counters returns the native form of saved counters.
func (c EmergencyCounters) counters() emergencyCounters {
	return emergencyCounters{started: c.Started, ended: c.Ended, emergencyTicks: c.EmergencyTicks,
		sampled: c.Sampled, skipped: c.Skipped, endedTicks: c.EndedTicks, maxTicks: c.MaxTicks}
}

// record returns the native record of a saved record.
func (e SavedEmergency) record() emergencyRecord {
	return emergencyRecord{generation: e.Generation, serial: e.Serial, start: e.Start, pod: e.Pod, order: e.Order}
}

// exportEmergencies returns the saved records, counters, and sampler, or nil
// when no record is active, each counter is 0, and the sampler is not live.
func (s *Simulation) exportEmergencies() *SavedEmergencies {
	if len(s.emergencies) == 0 && s.emergencyCounters == (emergencyCounters{}) && !s.emergencySampler.live {
		return nil
	}
	saved := &SavedEmergencies{Counters: s.emergencyCounters.exported()}
	saved.Random, saved.NextTick = s.emergencySampler.savedMembers()
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
// the evidence that it needs. Records, counters, and sampler need the switch.
// With the switch on, the records meet E1, the sampler meets section 4.3 of
// the incident stage 7 contract, and the saved pods meet E2 to E5 and E8. Each
// failure makes the whole save invalid.
func checkSavedEmergencies(input RestoreStateInput) error {
	state := input.State
	if !input.EmergenciesOn {
		if state.Emergencies != nil {
			return errors.New("saved emergencies need emergencies on")
		}
		return nil
	}
	parties, err := savedEmergencyParties(state)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidEmergencies, err)
	}
	var random []byte
	var nextTick *int64
	if state.Emergencies != nil {
		random, nextTick = state.Emergencies.Random, state.Emergencies.NextTick
	}
	if err := checkSavedSampler(input, random, nextTick, input.Rates.EmergenciesPerHour); err != nil {
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
	if counters.Started < 0 || counters.Ended < 0 || counters.EmergencyTicks < 0 ||
		counters.Sampled < 0 || counters.Skipped < 0 || counters.EndedTicks < 0 || counters.MaxTicks < 0 {
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

// CheckIncidentPolicy is the production policy validator of a saved state.
// It refuses the states that have no owner to release them: a refuge with
// any owner, an empty recovery with the emergency owner, and a save of the
// traffic demo with a hold, a purpose, an owner, an interrupt, a fault
// member, or an emergency member. It reads only the saved state, because
// the logical tier removes the demo fleet and clears its holds and
// purposes. No production path makes these states. The stage 1 test entry
// IncidentForTest can, and RestoreState still restores them, so the session
// runs this check before RestoreState.
func CheckIncidentPolicy(input RestoreStateInput) error {
	state := input.State
	for _, pod := range state.Pods {
		purpose := opPurpose(pod.Purpose)
		switch {
		case purpose == opRefuge:
			return fmt.Errorf("saved pod %s has a refuge", pod.ID)
		case purpose == opEmptyRecovery && serviceHold(pod.Owner) == emergencyHold:
			return fmt.Errorf("saved pod %s has an empty recovery with the emergency owner", pod.ID)
		}
	}
	if !demoSave(input) {
		return nil
	}
	for _, pod := range state.Pods {
		if pod.Withdrawn != 0 || pod.Purpose != 0 || pod.Owner != 0 || pod.Interrupt != 0 {
			return fmt.Errorf("saved demo pod %s has a service hold or an operational purpose", pod.ID)
		}
	}
	if state.Faults != nil || state.Emergencies != nil {
		return errors.New("saved demo fleet has fault or emergency state")
	}
	return nil
}

// demoSave reports whether a saved state is a save of the traffic demo: it
// has the demo member, or more pods than the fleet.
func demoSave(input RestoreStateInput) bool {
	return input.State.Demo != nil || len(input.State.Pods) > len(input.Fleet)
}

// setRestoredEmergencies gives a restored simulation the emergency switch
// of input. A saved sampler continues with its stream, its next tick, and the
// rate of input. The session turns the operations off again for the fleet of
// the traffic demo.
func (s *Simulation) setRestoredEmergencies(input RestoreStateInput) error {
	s.emergenciesOn = input.EmergenciesOn
	saved := input.State.Emergencies
	if !input.EmergenciesOn || saved == nil || saved.NextTick == nil {
		return nil
	}
	return s.emergencySampler.restore(saved.Random, *saved.NextTick, input.Rates.EmergenciesPerHour)
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
// With the emergency switch on, each pod returns to service from the
// emergency hold: the tier puts each pod at its initial berth with no
// purpose, so the hold owns no purpose.
// With the switch off, the pods keep the hold, as the stage 1 tier keeps
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
