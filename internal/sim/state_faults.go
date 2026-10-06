package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// SavedFaults holds the fault records and the fault counters of a saved
// state. They need the fault marker.
type SavedFaults struct {
	// Records holds the active records in serial order.
	Records  []SavedFault  `json:"records,omitempty"`
	Counters FaultCounters `json:"counters,omitzero"`
}

// SavedFault is one saved fault record. Its ID is i<Generation>.<Serial>.
// End is 0 for a fault without an end. A pod fault names the pod by its
// index in SavedState.Pods. Debris names its lane by its index in
// Network.Lanes, and has the segment from From to To. The session adapter
// writes the record as one tuple.
type SavedFault struct {
	Generation uint64  `json:"-"`
	Serial     uint64  `json:"-"`
	Debris     bool    `json:"-"`
	Start      int64   `json:"-"`
	End        int64   `json:"-"`
	Pod        int     `json:"-"`
	Lane       int     `json:"-"`
	From       float64 `json:"-"`
	To         float64 `json:"-"`
}

// errInvalidFaults marks a saved fault record that makes the whole save
// invalid. A restore with this error does not try the logical tier: a
// damaged save gets no partial recovery.
var errInvalidFaults = errors.New("invalid saved fault")

// exportFaults returns the saved records and counters, or nil when no
// record is active and each counter is 0.
func (s *Simulation) exportFaults() *SavedFaults {
	if len(s.faults) == 0 && s.faultCounters == (faultCounters{}) {
		return nil
	}
	saved := &SavedFaults{Counters: s.faultCounters.exported()}
	for _, record := range s.faults {
		fault := SavedFault{Generation: record.generation, Serial: record.serial, Start: record.start, End: record.end}
		if record.kind == debrisFault {
			fault.Debris, fault.Lane, fault.From, fault.To = true, record.lane, record.from, record.to
		} else {
			fault.Pod = record.pod
		}
		saved.Records = append(saved.Records, fault)
	}
	return saved
}

// record returns the native record of a saved record.
func (fault SavedFault) record() faultRecord {
	record := faultRecord{generation: fault.Generation, serial: fault.Serial, kind: podFault, start: fault.Start, end: fault.End, pod: fault.Pod}
	if fault.Debris {
		record = faultRecord{generation: fault.Generation, serial: fault.Serial, kind: debrisFault, start: fault.Start, end: fault.End,
			lane: fault.Lane, from: fault.From, to: fault.To}
	}
	return record
}

// counters returns the native form of saved counters.
func (c FaultCounters) counters() faultCounters {
	return faultCounters{started: c.Started, cleared: c.Cleared, evacuations: c.Evacuations, reroutes: c.Reroutes, faultWaitTicks: c.FaultWaitTicks}
}

// checkSavedFaults checks the saved fault records and counters before
// either restore tier, with the rules of section 13.5 of the incident
// suspension contract that do not need the network. Records and counters
// need the fault marker, and the marker needs the incident marker. The
// records are in strictly increasing serial order up to the saved serial,
// and at most maxDebrisFaults are debris. Each start is from 0 to the
// saved tick, each end is 0 or after its start, and each evacuation tick
// fits in int64. A pod record names a saved pod with the fault hold, at
// most one record names a pod, and the pod is a supported target: no
// coupling member, no platoon leader or follower, and no compact queue
// head (invariant F2). A save of the traffic demo has no record, because
// the demo project has no fault marker (section 12.7). The logical tier
// drops the demo and the records, so only this check refuses such a save.
// Each failure makes the save invalid.
func checkSavedFaults(input RestoreStateInput) error {
	if err := ValidateFaultContracts(input.FaultContract, input.IncidentContract); err != nil {
		return err
	}
	saved := input.State.Faults
	if saved == nil {
		return nil
	}
	if input.FaultContract == "" {
		return errors.New("saved faults need the fault contract")
	}
	counters := saved.Counters
	if counters.Started < 0 || counters.Cleared < 0 || counters.Evacuations < 0 || counters.Reroutes < 0 || counters.FaultWaitTicks < 0 {
		return fmt.Errorf("%w: a fault counter is negative", errInvalidFaults)
	}
	state := input.State
	if state.Demo != nil && len(saved.Records) > 0 {
		return fmt.Errorf("%w: the traffic demo has fault records", errInvalidFaults)
	}
	leaders := make(map[string]bool)
	for _, pod := range state.Pods {
		if pod.Platoon != nil {
			leaders[pod.Platoon.Leader] = true
		}
	}
	faulted := make([]bool, len(state.Pods))
	debris := 0
	// SetFaults refuses a delay out of its range in each tier, and the
	// range keeps this product in int64.
	evacuation := int64(min(max(input.Faults.EvacuationSeconds, 0), maxEvacuationSeconds)) * TicksPerSecond
	for index, fault := range saved.Records {
		id := incidentID(fault.Generation, fault.Serial)
		switch {
		case index > 0 && fault.Serial <= saved.Records[index-1].Serial:
			return fmt.Errorf("%w: fault %s is not after the record before it in serial order", errInvalidFaults, id)
		case fault.Serial > state.IncidentSerial:
			return fmt.Errorf("%w: fault %s has a serial above the saved serial %d", errInvalidFaults, id, state.IncidentSerial)
		case fault.Start < 0 || fault.Start > state.Tick:
			return fmt.Errorf("%w: fault %s starts at tick %d, outside 0 to %d", errInvalidFaults, id, fault.Start, state.Tick)
		case fault.End != 0 && fault.End <= fault.Start:
			return fmt.Errorf("%w: fault %s ends at tick %d, not after its start", errInvalidFaults, id, fault.End)
		case fault.Start > math.MaxInt64-evacuation:
			return fmt.Errorf("%w: the evacuation tick of fault %s is out of range", errInvalidFaults, id)
		}
		if fault.Debris {
			debris++
			if debris > maxDebrisFaults {
				return fmt.Errorf("%w: more than %d debris records", errInvalidFaults, maxDebrisFaults)
			}
			continue
		}
		if fault.Pod < 0 || fault.Pod >= len(state.Pods) || faulted[fault.Pod] {
			return fmt.Errorf("%w: fault %s names the pod index %d out of range or with another record", errInvalidFaults, id, fault.Pod)
		}
		faulted[fault.Pod] = true
		pod := state.Pods[fault.Pod]
		switch {
		case pod.Withdrawn&uint8(faultHold) == 0:
			return fmt.Errorf("%w: fault %s names pod %s without the fault hold", errInvalidFaults, id, pod.ID)
		case state.couplingMember(pod.ID) || pod.Platoon != nil || leaders[pod.ID] || pod.CompactQueue != nil:
			return fmt.Errorf("%w: fault %s names pod %s in a group", errInvalidFaults, id, pod.ID)
		}
	}
	return nil
}

// setFaultContract gives a restored simulation the fault marker of input.
// With the marker, the fault operations are on with the settings of input.
// The session turns them off again for the fleet of the traffic demo, which
// has no record.
func (s *Simulation) setFaultContract(input RestoreStateInput) error {
	s.faultContract = input.FaultContract
	if input.FaultContract == "" {
		return nil
	}
	return s.SetFaults(true, input.Faults)
}

// savedDebris checks each saved debris record against the network:
// the segment meets precondition 5 of the debris start, and its footprint
// meets no footprint of another debris record. It returns the footprint of
// each debris record, by record index. Each failure makes the save
// invalid (section 7.6 of the incident suspension contract).
func (s *Simulation) savedDebris(saved *SavedFaults) (map[int][]resource, error) {
	if saved == nil {
		return nil, nil
	}
	footprints := make(map[int][]resource)
	held := make(map[resource]bool)
	for index, fault := range saved.Records {
		if !fault.Debris {
			continue
		}
		id := incidentID(fault.Generation, fault.Serial)
		footprint, err := s.debrisSegment(fault.Lane, fault.From, fault.To)
		if err != nil {
			return nil, fmt.Errorf("%w: debris %s: %w", errInvalidFaults, id, err)
		}
		for _, r := range footprint {
			if held[r] {
				return nil, fmt.Errorf("%w: debris %s meets the footprint of another debris record", errInvalidFaults, id)
			}
			held[r] = true
		}
		footprints[index] = footprint
	}
	return footprints, nil
}

// checkSavedFaultFootprints checks the saved debris before either restore
// tier (sections 7.6 and 13.5 of the incident suspension contract): each
// segment meets precondition 5, and no debris footprint meets another
// debris footprint or the footprint of a pod fault (F6). The logical tier
// drops the records without a placed pod, and the physical tier can fail
// before it places the faulted pods. So only this check keeps either tier
// from a save that breaks F6. It places the debris and then each faulted
// traveling pod in a scratch fleet, with the code of the physical tier,
// and it refuses the save only when that code finds a conflict. A faulted
// pod that the physical tier cannot place has no footprint, and the tiers
// handle it. A faulted pod at a berth needs no check, because a debris
// footprint holds no berth and no berth node.
func checkSavedFaultFootprints(input RestoreStateInput, newFleet func() (*Simulation, error)) error {
	saved := input.State.Faults
	if saved == nil || !slices.ContainsFunc(saved.Records, func(fault SavedFault) bool { return fault.Debris }) {
		return nil
	}
	s, err := newFleet()
	if err != nil {
		return nil //nolint:nilerr // Each tier creates the fleet again and reports the error.
	}
	r := newPhysicalRestore(s, input.State)
	r.restoreCounters()
	if err := r.placeDebris(); err != nil {
		return err
	}
	for _, fault := range saved.Records {
		if fault.Debris {
			continue
		}
		if err := r.placeFaultedPod(fault.Pod); err != nil {
			return err
		}
	}
	return nil
}

// placeFaultedPod places the saved traveling pod at index in the scratch
// fleet of checkSavedFaultFootprints, with the steps of the physical tier
// for a pod without a platoon link (invariant F2). It returns only the
// error that the footprint of the pod meets a debris footprint. A pod that
// the physical tier would demote, or that fails a check of the physical
// tier, has no footprint, and gives no error.
func (r *physicalRestore) placeFaultedPod(index int) error {
	if !r.routeFaultedPod(index) {
		return nil
	}
	// Without a leader, placeTravelingPod fails only when the footprint
	// meets debris.
	_, err := r.placeTravelingPod(index, -1)
	return err
}

// routeFaultedPod decodes the saved pod at index and builds its route, as
// the physical tier does. It reports whether the pod travels and has a
// route to place. A pod that buildRoute demotes has no route, and
// placeTravelingPod does not place it.
func (r *physicalRestore) routeFaultedPod(index int) bool {
	saved := r.state.Pods[index]
	return r.decodePod(index, saved) == nil && !r.demoted[index] && r.s.vehicles[index].Pod.Activity == Traveling &&
		len(saved.Route) > 0 && len(saved.Route) <= newRouteLimits(r.s.network).pod && r.knownLanes(saved.Route) &&
		r.buildRoute(index, saved.Route) == nil
}

// placeDebris checks the saved debris and gives each resource of each
// debris footprint to its fault owner, before the restore places a pod.
// A pod that then needs one of these resources makes the save invalid.
func (r *physicalRestore) placeDebris() error {
	footprints, err := r.s.savedDebris(r.state.Faults)
	if err != nil {
		return err
	}
	for index, footprint := range footprints {
		fault := r.state.Faults.Records[index]
		owner := resourceOwner{kind: faultOwnerKind, id: incidentID(fault.Generation, fault.Serial)}
		for _, claimed := range footprint {
			r.s.owners[claimed] = owner
		}
	}
	return nil
}

// restoreFaultedPods restores the records and the counters after each pod
// is in place (section 12.7 of the incident suspension contract). A
// faulted pod that the tier demotes fails the tier: no record ends to keep
// the other pods in place. A faulted traveling pod is at rest, so its cap
// is its distance. The pod waits for no grant, and it gives up each
// service claim that claimDestinations gave back to it. The blocked set
// is built again.
func (r *physicalRestore) restoreFaultedPods() error {
	saved := r.state.Faults
	if saved == nil {
		return nil
	}
	s := r.s
	for _, fault := range saved.Records {
		if !fault.Debris && r.demoted[fault.Pod] {
			return fmt.Errorf("the restore would demote pod %s, which has fault %s", s.vehicles[fault.Pod].Pod.ID, incidentID(fault.Generation, fault.Serial))
		}
	}
	s.faults, s.faultCounters = make([]faultRecord, 0, len(saved.Records)), saved.Counters.counters()
	for _, fault := range saved.Records {
		record := fault.record()
		s.faults = append(s.faults, record)
		if fault.Debris {
			continue
		}
		v := &s.vehicles[record.pod]
		v.faulted, v.pending = true, -1
		if v.Pod.Activity == Traveling {
			v.faultCap = v.distance
		}
		s.surrenderServiceClaims(v)
	}
	// The blocked set changes from empty, so the rebuild starts a route
	// epoch, and the reroute pass is due.
	s.rebuildBlocked()
	return nil
}

// dropSavedFaults ends each saved record in the logical tier (section 12.7
// of the incident suspension contract). checkSavedFaultFootprints checked
// the debris records before the tier. The counters stay, and each pod
// loses the fault hold: the tier puts each pod at its initial berth with
// no purpose. It returns the number of records that ended.
func (s *Simulation) dropSavedFaults(saved *SavedFaults) int {
	if s.faultContract != "" {
		for index := range s.vehicles {
			s.vehicles[index].withdrawn &^= faultHold
		}
	}
	if saved == nil {
		return 0
	}
	s.faultCounters = saved.Counters.counters()
	return len(saved.Records)
}
