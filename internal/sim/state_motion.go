package sim

import (
	"fmt"
	"sort"
)

// This file holds the saved motion of a traveling pod: its speed and the
// blocks that it reserved past the derived frontier (restore speed
// proposal, section 3.1).

// savedFaultedPods marks the saved pods that a saved pod fault names.
// checkSavedFaults checks the indexes first.
func savedFaultedPods(state SavedState) []bool {
	faulted := make([]bool, len(state.Pods))
	if state.Faults == nil {
		return faulted
	}
	for _, fault := range state.Faults.Records {
		if !fault.Debris && fault.Pod >= 0 && fault.Pod < len(faulted) {
			faulted[fault.Pod] = true
		}
	}
	return faulted
}

// checkSavedMotion checks the saved speed and the saved reservation lead
// of each pod, in saved order (restore speed proposal, section 4.2). For
// each pod, it refuses a speed that is not finite or is negative, then a
// speed or a lead on a pod that is not traveling, also a member with the
// value 0 (see SavedPod.SetMotionMembers), and then a negative lead. A
// speed above a lane limit is not a refusal: the physical tier places the
// pod at rest (see placeTravelingPod). checkRestoreInput runs it after
// checkSavedFaultFootprints.
func checkSavedMotion(input RestoreStateInput) error {
	for _, pod := range input.State.Pods {
		switch {
		case !finite(pod.Speed) || pod.Speed < 0:
			return fmt.Errorf("pod %s has speed %g, not a finite value of 0 or more", pod.ID, pod.Speed)
		case pod.Activity != activityCode(Traveling) && (pod.zeroMotion || pod.Speed != 0 || pod.ReservedAhead != 0):
			return fmt.Errorf("pod %s is not traveling and has a speed or a reservation ahead", pod.ID)
		case pod.ReservedAhead < 0:
			return fmt.Errorf("pod %s reserves %d blocks ahead, not 0 or more", pod.ID, pod.ReservedAhead)
		}
	}
	return nil
}

// checkSavedRouteMarkers refuses the trimmed route marker on a pod that is
// not traveling, also with the value false (see SavedPod.SetMarkerMember),
// and then a true marker on a traveling pod with no saved route, in saved
// order (exact restore coordinates proposal, EC-D3). Only the saved route
// of a traveling pod can start after the first lane of its route.
func checkSavedRouteMarkers(input RestoreStateInput) error {
	for _, pod := range input.State.Pods {
		switch {
		case pod.Activity != activityCode(Traveling) && (pod.zeroMarker || pod.RouteTrimmed):
			return fmt.Errorf("pod %s is not traveling and has a trimmed route", pod.ID)
		case pod.RouteTrimmed && len(pod.Route) == 0:
			return fmt.Errorf("pod %s has a trimmed route and no saved route", pod.ID)
		}
	}
	return nil
}

// checkSavedReversals checks the saved reversal dwell latch of each pod,
// in saved order (exact restore coordinates proposal, EC-D8). For each pod,
// it refuses either latch member on a pod that is not traveling, also a
// member with the value 0 (see SavedPod.SetReversalMembers), then ticks
// out of the range of a dwell, then ticks with no latched lane, and then a
// lane outside the saved route ahead of the pod. A latched lane is at or
// after the lane of the pod, and a lane with its dwell done is at most one
// lane after it. The lane is compared in its signed form, so no value
// overflows. Last, it refuses a lane whose reverse flag is the flag of the
// lane before it, on a new fleet. A lane index that the network does not
// hold is left to the physical tier, which demotes such a pod. The
// physical tier checks the latch against the restored position of the pod
// (see latchAgrees).
func checkSavedReversals(input RestoreStateInput, newFleet func() (*Simulation, error)) error {
	latched := false
	for _, pod := range input.State.Pods {
		lane, ticks := pod.ReversalLane, pod.ReversalTicks
		switch {
		case pod.Activity != activityCode(Traveling) && (pod.zeroReversal || lane != 0 || ticks != 0):
			return fmt.Errorf("pod %s is not traveling and has a reversal dwell latch", pod.ID)
		case ticks < 0 || ticks >= reversalDwellTicks:
			return fmt.Errorf("pod %s has %d reversal dwell ticks, not 0 to %d", pod.ID, ticks, reversalDwellTicks-1)
		case ticks != 0 && lane <= 0:
			return fmt.Errorf("pod %s has reversal dwell ticks and no latched lane", pod.ID)
		case lane > 0 && (lane < pod.RouteIndex || lane >= len(pod.Route)),
			lane < 0 && (-(lane+1) > pod.RouteIndex || -(lane+1) >= len(pod.Route)-1):
			return fmt.Errorf("pod %s has the reversal dwell lane %d outside its saved route ahead of it", pod.ID, lane)
		}
		latched = latched || lane != 0
	}
	if !latched {
		return nil
	}
	s, err := newFleet()
	if err != nil {
		return nil //nolint:nilerr // Each tier calls the fleet factory and reports its error.
	}
	lanes := s.network.Lanes
	reverse := func(index int) bool { return s.maneuvers != nil && s.maneuvers.reverse[lanes[index].ID] }
	for _, pod := range input.State.Pods {
		k := max(pod.ReversalLane, -pod.ReversalLane)
		if k == 0 {
			continue
		}
		before, at := pod.Route[k-1], pod.Route[k]
		if before < 0 || before >= len(lanes) || at < 0 || at >= len(lanes) {
			continue
		}
		if reverse(before) == reverse(at) {
			return fmt.Errorf("pod %s has the reversal dwell lane %d, where the reverse flag does not change", pod.ID, pod.ReversalLane)
		}
	}
	return nil
}

// derivedFrontier returns the reservation frontier that a restore derives
// for the traveling pod v at block current. It is reservationEnd of
// current, except at a depot entry. A pod binds its depot slot when its
// reservation ends at the entry, and the binding adds the lanes after the
// entry to its route. Until a grant takes its slot, its reservation still
// ends at the entry. The grant after the entry takes each lane up to the
// slot. claims tells whether v claims its destination, and from is the
// route index of the start of the saved route. frame gives the depot entry
// rule (see depotEntryBlock). entry is the entry block of a pod whose
// reservation cannot pass the entry, or -1.
func (s *Simulation) derivedFrontier(v *vehicle, from, current int, claims bool, frame positionFrame) (through, entry int) {
	through, entry = reservationEnd(&v.blocks, current), -1
	if last, ok := s.depotEntryBlock(v, from, current, frame.entry); ok && current <= last {
		if claims {
			through = max(through, reservationEnd(&v.blocks, last+1))
		} else {
			through, entry = min(through, last), last
		}
	}
	return through, entry
}

// restoredBlock returns the block of a restored pod at a route distance in
// the lane of blocks first to last: the first block that ends at or after
// the distance, or last. Thus the saved route index picks the lane, also
// at an exact lane boundary. The block ends increase, so a binary search
// finds the block (see restoredDistance).
func restoredBlock(blocks *blockList, first, last int, distance float64) int {
	return min(last, first+sort.Search(last+1-first, func(offset int) bool {
		return blocks.peekEnd(first+offset) >= distance
	}))
}

// savedMotion returns the saved reservation lead of the traveling pod v,
// whose saved route starts at route index start, and whose saved position
// is in pod, in frame (see exportPlan.frame). The lead counts the blocks of
// the reservation past the frontier that a restore of pod derives. The
// export decodes the position with the decoder of the restore, in the
// frame of the primary placement (see decodeTravelPosition). The lead
// counts from the lead base block of that position. The lead is negative
// when the reservation ends before the derived frontier, and exportPod
// writes 0 then. fits reports whether the restore in frame, with the
// saved lead, gives the pod its live route distance bit for bit, its live
// block, its live frontier, and its live reversal latch: the decoded
// position is the live one (see decodeTravelPosition), the restore
// accepts the frontier (see savedThrough), places the pod in its block
// (see placedBlock), and keeps the saved latch (see latchAgrees). A pod
// whose published lane distance is stale, as right after a redirect, can
// decode behind its live position (exact restore coordinates proposal,
// EC-D11). A fault can hold a pod at the start of a lane with its dwell
// done, after its search latched the next lane. The restore drops that
// latch. Only the blocks of the whole frame hold the lanes past the lane
// of the pod, so fits is false in another frame: fitFrames reads fits only
// in the whole frame. savedMotion writes nothing to s: it keeps the scan
// hint of the blocks of v.
func (s *Simulation) savedMotion(v *vehicle, start int, pod SavedPod, frame savedFrame) (lead int, fits bool) {
	blocks := &v.blocks
	scan := blocks.scan
	base := blocks.laneFirst(start)
	position := s.decodeTravelPosition(frame.blocks, pod, frame.decode)
	derived, entry := s.derivedFrontier(v, start, base+position.leadBase, pod.ClaimsDestination, frame.decode)
	lead = v.reservedThrough - derived
	through, ok := savedThrough(blocks, derived, entry, lead)
	block := base + placedBlock(frame.blocks, position, through-base, frame.decode)
	blocks.scan = scan
	if !frame.decode.exact || !ok || through != v.reservedThrough || block != v.blockIndex || position.distance+frame.offset != v.distance {
		return lead, false
	}
	lane, ticks := savedReversal(v, start)
	latch := reversal{lane: lane, ticks: ticks}
	return lead, s.latchFits(frame.blocks, pod.RouteIndex, position.distance, savedSpeed(v), frame.blocks.peekEnd(through-base), latch)
}

// savedSpeed returns the saved speed of the traveling pod v. A faulted pod
// saves no speed.
func savedSpeed(v *vehicle) float64 {
	if v.faulted {
		return 0
	}
	return v.Pod.Speed
}

// positionFrame is the frame in which a restore decodes the saved position
// of a traveling pod (see decodeTravelPosition). snap is the largest
// distance from a block end that snaps to it (see restoredDistance). An
// exact frame keeps the saved route distance of a whole route bit for bit,
// so it also keeps a pod that waits at its route end (see endWait) and a
// pod at an exact block end (see tiedBlock). entry is the depot entry
// rule of the frame (see depotEntryBlock).
type positionFrame struct {
	snap  float64
	exact bool
	entry depotEntryRule
}

// depotEntryRule tells which incoming lane of a depot is the entry of a pod
// bound to a slot of the depot, on a route that passes that lane more than
// one time (see depotEntryBlock).
type depotEntryRule uint8

const (
	// lastEntry is the last incoming lane of the route. A pod binds its slot
	// at the end of its route, and the binding adds the lanes after the
	// entry, so the entry of the live route is its last incoming lane.
	lastEntry depotEntryRule = iota
	// firstEntry is the first incoming lane from the start of the saved
	// route, as version 13 found it on its trimmed routes.
	firstEntry
	// aheadEntry is the first incoming lane from the lane of the pod.
	aheadEntry
)

var (
	// wholeFrame decodes a whole saved route, whose blocks are the blocks of
	// the live route.
	wholeFrame = positionFrame{exact: true, entry: lastEntry}
	// trimmedFrame decodes a trimmed saved route, whose sums of lane lengths
	// can differ from those of the live route in the last bits.
	trimmedFrame = positionFrame{snap: restoreDriftTolerance, entry: firstEntry}
	// earlierTrimmedFrame and earlierWholeFrame decode a trimmed and a whole
	// saved route as the restore did before the saved motion (see
	// restoreBeforeMotion). Version 13 saved only trimmed routes, so a
	// whole route takes the next entry ahead of the pod.
	earlierTrimmedFrame = positionFrame{snap: restoreTolerance, entry: firstEntry}
	earlierWholeFrame   = positionFrame{snap: restoreTolerance, entry: aheadEntry}
)

// travelPosition is the decoded position of a saved traveling pod.
type travelPosition struct {
	// laneDistance is the published lane distance, within restoreTolerance
	// of the lane.
	laneDistance float64
	// distance is the route distance of the pod.
	distance float64
	// lane is the route index of the saved lane.
	lane int
	// leadBase is the block that the saved lead counts from (see
	// savedFrontier). It is not always the block that the pod occupies
	// (see tiedBlock).
	leadBase int
}

// decodeTravelPosition returns the position of the saved traveling pod
// saved on blocks, the blocks of its saved route, in frame. The export
// and each placement of the restore decode with it, so the lead of the
// export and of the restore count from the same block. saved.RouteIndex
// is a route index of blocks. The published lane distance always moves
// to within restoreTolerance of the lane. The route distance comes from
// the lane distance, with the saved distance or a block end winning when
// it is close (see restoredDistance), and the lead base block is the
// first block of the lane that ends at or after it (see restoredBlock).
// At a route end with no destination berth, as at a full depot entry,
// live motion keeps the pod at the end of its last block and does not
// publish its lane distance again (see publishVehicleTravel). So the
// saved lane distance can be short of the saved route distance, also in
// an earlier block. The exact frame keeps the saved route distance at
// that end and the last block (see endWait).
func (s *Simulation) decodeTravelPosition(blocks *blockList, saved SavedPod, frame positionFrame) travelPosition {
	lane := saved.RouteIndex
	laneDistance := min(max(saved.LaneDistance, -restoreTolerance), s.laneLength(blocks.route[lane])+restoreTolerance)
	if frame.exact && endWait(blocks, saved) {
		return travelPosition{laneDistance: laneDistance, distance: saved.Distance, lane: lane, leadBase: blocks.len() - 1}
	}
	first, last := routeLaneBlocks(blocks, lane)
	distance := restoredDistance(restoredDistanceInput{
		blocks: blocks, first: first, last: last, laneDistance: laneDistance, saved: saved.Distance, snap: frame.snap,
	})
	return travelPosition{laneDistance: laneDistance, distance: distance, lane: lane, leadBase: restoredBlock(blocks, first, last, distance)}
}

// endWait reports whether the saved traveling pod saved waits at the end
// of its route with no destination berth, at the exact end of blocks, the
// blocks of its saved route. It writes nothing to blocks, because the
// export reads the blocks of the live route.
func endWait(blocks *blockList, saved SavedPod) bool {
	return saved.Destination == "" && blocks.len() > 0 && saved.RouteIndex == len(blocks.route)-1 &&
		saved.Distance == blocks.peekEnd(blocks.len()-1)
}

// savedFrontier returns the reservation frontier of the restored traveling
// pod v with the lead base block base, in frame: the derived frontier plus
// the saved lead lead (restore speed proposal, section 6.2). claims tells whether v
// claims its destination. A grant takes the blocks to reservationEnd of
// its request, so a chain of grants ends at a block F where
// reservationEnd(F) is F. A pod bound to a depot slot that it does not
// claim also ends its reservation at the depot entry, and never past it.
// It reports false for any other frontier and for a frontier past the
// saved route. With no lead or a negative lead, the frontier is the
// derived one.
func (r *physicalRestore) savedFrontier(v *vehicle, claims bool, lead, base int, frame positionFrame) (int, bool) {
	derived, entry := r.s.derivedFrontier(v, 0, base, claims, frame)
	return savedThrough(&v.blocks, derived, entry, lead)
}

// savedThrough returns the reservation frontier on blocks with the derived
// frontier derived, the depot entry block entry or -1 (see
// derivedFrontier), and the saved lead lead (see savedFrontier). It reports
// false for a frontier that the restore refuses. The export checks its
// frame with it (see savedMotion).
func savedThrough(blocks *blockList, derived, entry, lead int) (int, bool) {
	// checkSavedMotion refuses a negative lead after the scratch placement
	// of checkSavedFaultFootprints, which reads no lead.
	if lead <= 0 {
		return derived, true
	}
	if lead > blocks.len()-1-derived {
		return 0, false
	}
	through := derived + lead
	switch {
	case entry >= 0 && through > entry:
		return 0, false
	case entry >= 0 && through == entry:
		return through, true
	default:
		return through, reservationEnd(blocks, through) == through
	}
}

// restoredSpeed returns the speed that the restore gives the saved pod at
// index. A faulted pod restores at rest.
func (r *physicalRestore) restoredSpeed(index int) float64 {
	if r.faulted[index] {
		return 0
	}
	return r.state.Pods[index].Speed
}

// overLaneLimit reports whether the restored traveling pod v is more than
// restoreSpeedMargin above the speed limit of the lane at route index
// lane. Such a pod restores at rest (see placeTravelingPod), or moves to
// a berth when it cannot (restore speed proposal, section 4.2).
func overLaneLimit(v *vehicle, lane int) bool {
	return v.Pod.Speed > v.blocks.route[lane].SpeedLimit+restoreSpeedMargin
}

// stopsInReservation reports whether the restored traveling pod v, in the
// lane at route index lane with blocks 0 to through reserved, is at most
// restoreSpeedMargin above the speed limit of that lane, can stop by the
// limit of move at the maximum braking rate of its class, and can slow to
// each lower speed limit ahead at that rate (restore speed proposal,
// sections 3.1 and 4.2). The limit is the end of block through, the
// platoon cap of v to leader when leader is not nil, and the reversal
// limit. A pod at rest can always stop.
//
// The lane is the lane at the route index, which is the lane that the
// first move reads, also for a save without a lane ID. A pod within the
// margin keeps its saved speed. Each distance comparison allows
// restoreTolerance, because the restore can move a distance by that much.
func (r *physicalRestore) stopsInReservation(v, leader *vehicle, lane, through int) bool {
	speed := v.Pod.Speed
	if speed == 0 {
		return true
	}
	if overLaneLimit(v, lane) {
		return false
	}
	limit := v.blocks.end(through)
	if leader != nil {
		limit = min(limit, followerCap(v, leader))
	}
	// reversalLimit can latch a lane, and the first move latches it again.
	latch := v.reversal
	limit = r.s.reversalLimit(v, lane, limit)
	v.reversal = latch
	envelope := motionEnvelopeFor(v.Pod.Class)
	reach := envelope.stoppingDistance(speed)
	if v.distance+reach > limit+restoreTolerance {
		return false
	}
	blocks := &v.blocks
	for next := lane + 1; next < len(blocks.route); next++ {
		remaining := blocks.lanes[next].start - v.distance
		if remaining > reach {
			break
		}
		lower := blocks.route[next].SpeedLimit
		if lower < speed && (speed*speed-lower*lower)/(2*envelope.maximumBraking) > remaining+restoreTolerance {
			return false
		}
	}
	return true
}
