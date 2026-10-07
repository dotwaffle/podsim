package sim

import "slices"

// The debris limits (product choice P1 of the incident suspension
// contract): the number of active debris faults, and the length of a
// debris segment in meters.
const (
	maxDebrisFaults = 64
	maxDebrisMeters = 50
)

// startDebris starts debris on the segment [from, to] of the lane with the
// index lane. duration is 0 for debris without an end, or a number of
// seconds from 1 to maxFaultSeconds. It checks each precondition, in the
// order of section 7.3 of the incident suspension contract, before its
// first write, and it changes nothing when one fails. Debris starts only on
// free resources that no pod claims, so it revokes no grant, and no pod
// has it inside its stopping distance. It returns the fault ID.
func (s *Simulation) startDebris(lane int, from, to float64, duration int64) (string, error) {
	if !s.faultsOn {
		return "", errFaultsOff
	}
	if duration < 0 || duration > maxFaultSeconds {
		return "", errFaultDuration
	}
	if !s.faultTicksFit(duration) {
		return "", errIncidentLimit
	}
	footprint, err := s.debrisSegment(lane, from, to)
	if err != nil {
		return "", err
	}
	if s.activeDebris() >= maxDebrisFaults {
		return "", errDebrisLimit
	}
	if s.meetsFaultFootprint(footprint) {
		return "", errDebrisOverlap
	}
	if err := s.debrisClaim(footprint); err != nil {
		return "", err
	}
	if s.pass != nil && s.pass.active {
		return "", errFaultDispatch
	}
	id := s.nextIncidentID()
	record := faultRecord{generation: s.incidentGeneration, serial: s.incidentSerial, kind: debrisFault, start: s.tick, lane: lane, from: from, to: to}
	if duration > 0 {
		record.end = s.tick + duration*TicksPerSecond
	}
	s.faults = append(s.faults, record)
	owner := resourceOwner{kind: faultOwnerKind, id: id}
	for _, r := range footprint {
		s.owners[r] = owner
	}
	s.rebuildBlocked()
	countFault(&s.faultCounters.started)
	return id, nil
}

// debrisSegment returns the footprint of debris on the segment [from, to]
// of the lane with the index lane. It refuses an unknown lane, and a
// segment that is not finite, is empty, is outside the lane, or is longer
// than maxDebrisMeters. It also refuses a segment whose footprint holds a
// berth or the node of a berth. The cell that holds a berth resource also
// holds the berth node, so the node test alone refuses each such segment.
// The berth test keeps the rule of the contract explicit.
func (s *Simulation) debrisSegment(lane int, from, to float64) ([]resource, error) {
	s.ensureNetworkIndexes()
	if lane < 0 || lane >= len(s.network.Lanes) {
		return nil, errUnknownLane
	}
	// Each comparison is false for NaN, and an infinite bound fails the
	// length or the 0 test, so this test refuses each value that is not
	// finite.
	if !(0 <= from && from < to && to <= s.graph.lengths[lane] && to-from <= maxDebrisMeters) {
		return nil, errDebrisSegment
	}
	footprint := s.debrisFootprint(lane, from, to)
	for _, r := range footprint {
		if r.kind == berthResource || r.kind == nodeResource && len(s.berthResources[r.id]) > 0 {
			return nil, errDebrisSegment
		}
	}
	return footprint, nil
}

// debrisFootprint returns the footprint of debris on the segment [from,
// to] of the lane with the index lane (section 7.2 of the incident
// suspension contract): each resource of each cell of the lane that meets
// [from - Clearance, to + Clearance]. A cell meets the interval when the
// closed ranges share a point. A cell at a lane end holds the node and the
// junction resources, so debris near a junction also blocks the lanes that
// share it. The footprint is a function of the network and the segment.
func (s *Simulation) debrisFootprint(lane int, from, to float64) []resource {
	cells := s.laneCells[s.network.Lanes[lane].ID]
	length := s.graph.lengths[lane]
	low, high := from-Clearance, to+Clearance
	var footprint []resource
	count := cells.count()
	for cell := range count {
		if cellOffset(cell+1, count, length) < low {
			continue
		}
		if cellOffset(cell, count, length) > high {
			break
		}
		for _, r := range cells.cell(cell) {
			if !slices.Contains(footprint, r) {
				footprint = append(footprint, r)
			}
		}
	}
	return footprint
}

// activeDebris returns the number of debris records.
func (s *Simulation) activeDebris() int {
	count := 0
	for _, record := range s.faults {
		if record.kind == debrisFault {
			count++
		}
	}
	return count
}

// meetsFaultFootprint reports whether the footprint holds a resource of
// the footprint of an active fault.
func (s *Simulation) meetsFaultFootprint(footprint []resource) bool {
	for _, held := range s.faultFootprints() {
		for _, r := range held.resources {
			if slices.Contains(footprint, r) {
				return true
			}
		}
	}
	return false
}

// debrisClaim returns nil when each resource of the footprint has no owner
// and no pod retains it. Otherwise it returns errDebrisOverlap when a
// claimed resource is under the body of a pod, and errDebrisClaim for any
// other claim. A grant is a claim, so this test also refuses debris inside
// the stopping distance of a pod, and debris never revokes a claim.
func (s *Simulation) debrisClaim(footprint []resource) error {
	var claimed []resource
	for _, r := range footprint {
		if !s.owners[r].isZero() || s.podRetains(r) {
			claimed = append(claimed, r)
		}
	}
	if len(claimed) == 0 {
		return nil
	}
	for index := range s.vehicles {
		for _, r := range claimed {
			if s.claimOccupied(&s.vehicles[index], r) {
				return errDebrisOverlap
			}
		}
	}
	return errDebrisClaim
}

// podRetains reports whether a pod has a routeReleases entry for r.
func (s *Simulation) podRetains(r resource) bool {
	for index := range s.vehicles {
		if s.vehicles[index].retains(r) {
			return true
		}
	}
	return false
}

// releaseDebris releases each resource of the footprint of the debris
// record that the debris owns. In the fault stage, it adds the resources
// to faultReleased, and Step releases them at the end of the tick.
// Otherwise it releases them at once.
func (s *Simulation) releaseDebris(record faultRecord, inStage bool) {
	owner := resourceOwner{kind: faultOwnerKind, id: record.id()}
	for _, r := range s.debrisFootprint(record.lane, record.from, record.to) {
		switch {
		case s.owners[r] != owner:
		case inStage:
			s.faultReleased = append(s.faultReleased, r)
		default:
			delete(s.owners, r)
		}
	}
}

// releaseFaultResources releases the debris resources that the clears of
// the fault stage left in faultReleased. Step calls it at the end of a
// tick with faults on, so faultReleased is empty at each boundary. The
// admission of the tick saw the fault owners of the cleared records, so it
// can name them in a wait report. Those reports end here, and a completed
// tick names no removed record.
func (s *Simulation) releaseFaultResources() {
	if s.faultReleased == nil {
		return
	}
	var ids []string
	for _, r := range s.faultReleased {
		owner := s.owners[r]
		if owner.kind != faultOwnerKind {
			continue
		}
		delete(s.owners, r)
		if !slices.Contains(ids, owner.id) {
			ids = append(ids, owner.id)
		}
	}
	s.faultReleased = nil
	s.endFaultReports(ids...)
}
