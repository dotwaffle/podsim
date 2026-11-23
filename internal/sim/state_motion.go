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

// derivedFrontier returns the reservation frontier that a restore derives
// for the traveling pod v at block current. It is reservationEnd of
// current, except at a depot entry. A pod binds its depot slot when its
// reservation ends at the entry, and the binding adds the lanes after the
// entry to its route. Until a grant takes its slot, its reservation still
// ends at the entry. The grant after the entry takes each lane up to the
// slot. claims tells whether v claims its destination, and from is the
// route index of the start of the saved route (see depotEntryBlock). entry
// is the entry block of a pod whose reservation cannot pass the entry, or
// -1.
func (s *Simulation) derivedFrontier(v *vehicle, from, current int, claims bool) (through, entry int) {
	through, entry = reservationEnd(&v.blocks, current), -1
	if last, ok := s.depotEntryBlock(v, from); ok && current <= last {
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

// savedMotion returns the saved speed and the saved reservation lead of
// the traveling pod v, whose saved route starts at route index start, and
// whose saved position is in pod. The lead counts the blocks of the
// reservation past the frontier that a restore of pod derives. The export
// finds the block of the restore with the code of the restore, on the
// blocks of the saved route up to the current lane. The saved route
// starts at a lane start, and its sums of lane lengths can differ from
// those of the live route in the last bits. A faulted pod saves no speed.
// The lead is negative when the reservation ends before the derived
// frontier, and exportPod writes 0 then. savedMotion writes nothing to s:
// it keeps the scan hint of the blocks of v.
func (s *Simulation) savedMotion(v *vehicle, start int, pod SavedPod) (float64, int) {
	saved, _ := s.routeBlocks(v.Route[start : start+pod.RouteIndex+1])
	_, _, current := s.restoredPosition(&saved, pod.RouteIndex, pod.LaneDistance, pod.Distance, restoreDriftTolerance)
	blocks := &v.blocks
	scan := blocks.scan
	derived, _ := s.derivedFrontier(v, start, blocks.laneFirst(start)+current, pod.ClaimsDestination)
	blocks.scan = scan
	speed := v.Pod.Speed
	if v.faulted {
		speed = 0
	}
	return speed, v.reservedThrough - derived
}

// savedFrontier returns the reservation frontier of the restored traveling
// pod v at block current: the derived frontier plus the saved lead (restore
// speed proposal, section 6.2). A grant takes the blocks to reservationEnd
// of its request, so a chain of grants ends at a block F where
// reservationEnd(F) is F. A pod bound to a depot slot that it does not
// claim also ends its reservation at the depot entry, and never past it.
// It reports false for any other frontier and for a frontier past the
// saved route. With no lead or a negative lead, the frontier is the
// derived one.
func (r *physicalRestore) savedFrontier(v *vehicle, saved SavedPod, current int) (int, bool) {
	derived, entry := r.s.derivedFrontier(v, 0, current, saved.ClaimsDestination)
	// checkSavedMotion refuses a negative lead after the scratch placement
	// of checkSavedFaultFootprints, which can see one.
	if saved.ReservedAhead <= 0 {
		return derived, true
	}
	if saved.ReservedAhead > v.blocks.len()-1-derived {
		return 0, false
	}
	through := derived + saved.ReservedAhead
	switch {
	case entry >= 0 && through > entry:
		return 0, false
	case entry >= 0 && through == entry:
		return through, true
	default:
		return through, reservationEnd(&v.blocks, through) == through
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
