package sim

import (
	"cmp"
	"fmt"
	"slices"
)

// depotParked reports whether v reads its parked tick: v is idle at a depot
// slot, or it moves up in one depot lane.
func (s *Simulation) depotParked(v *vehicle) bool {
	if _, ok := s.depotIdleSlot(v); ok {
		return true
	}
	return v.Pod.Activity != Idle && depotMoveUp(s.depots, v.origin.ID, v.destination.ID)
}

// depotMoveUp reports whether a move from the slot origin to the slot
// destination is a move-up: both slots are in one depot lane, and the
// destination is the slot ahead of the origin.
func depotMoveUp(depots *depotIndex, origin, destination string) bool {
	from, fromOK := depots.slot(origin)
	to, toOK := depots.slot(destination)
	return fromOK && toOK && from.depot == to.depot && from.lane == to.lane && from.slot == to.slot+1
}

// depotSlotOpen reports whether a restore can place the demoted pod v at a
// berth. A berth that is not a depot slot is open. A slot is open when it
// is the slot behind the rearmost taken slot of its lane, and no pod of the
// lane moves, because a demoted pod has no place in the lane order. The
// view skips v, so its old slot does not count.
func (s *Simulation) depotSlotOpen(v *vehicle, berth Berth) bool {
	slot, ok := s.depots.slot(berth.ID)
	if !ok {
		return true
	}
	rear := -1
	for i := range s.vehicles {
		other := &s.vehicles[i]
		at, ok := s.depotSlotOf(other)
		if other == v || !ok || at.depot != slot.depot || at.lane != slot.lane {
			continue
		}
		if other.Pod.Activity != Idle {
			return false
		}
		rear = max(rear, at.slot)
	}
	return slot.slot == rear+1
}

// depotEntryBlock returns the last block of the incoming lane of the depot
// whose slot v names as its destination. It reports false when v names no
// slot or its route does not hold that lane.
func (s *Simulation) depotEntryBlock(v *vehicle) (int, bool) {
	slot, ok := s.depots.slot(v.destination.ID)
	if !ok {
		return 0, false
	}
	incoming := s.depots.depots[slot.depot].incoming.ID
	lane := slices.IndexFunc(v.Route, func(lane Lane) bool { return lane.ID == incoming })
	if lane < 0 {
		return 0, false
	}
	_, last := routeLaneBlocks(&v.blocks, lane)
	return last, true
}

// checkSavedDepotFields checks the parked ticks and the lane order of the
// saved pods. For each pod in order, it refuses a negative parked tick, and
// then a parked tick after the saved tick. Then, for each pod in order, it
// refuses a parked tick on a pod that is not idle at a depot slot and does
// not move up in one depot lane. Last, it refuses a depot lane whose pods
// do not have strictly increasing slots from the front to the back. The
// simulation never writes such a state.
func checkSavedDepotFields(input RestoreStateInput) error {
	for _, pod := range input.State.Pods {
		switch {
		case pod.ParkedSince < 0:
			return fmt.Errorf("pod %q has the negative parked tick %d", pod.ID, pod.ParkedSince)
		case pod.ParkedSince > 0 && pod.ParkedSince > input.State.Tick:
			return fmt.Errorf("pod %q has the parked tick %d after the saved tick %d", pod.ID, pod.ParkedSince, input.State.Tick)
		}
	}
	if len(input.Network.Depots) == 0 {
		for _, pod := range input.State.Pods {
			if pod.ParkedSince != 0 {
				return fmt.Errorf("pod %q has a parked tick and is not parked in a depot lane", pod.ID)
			}
		}
		return nil
	}
	depots, err := newDepotIndex(input.Network, indexNodePositions(input.Network.Nodes))
	if err != nil {
		return err
	}
	for _, pod := range input.State.Pods {
		_, idle := depots.slot(pod.BerthID)
		idle = idle && pod.Activity == activityCode(Idle)
		moving := pod.Activity != activityCode(Idle) && depotMoveUp(depots, pod.Origin, pod.Destination)
		if pod.ParkedSince != 0 && !idle && !moving {
			return fmt.Errorf("pod %q has a parked tick and is not parked in a depot lane", pod.ID)
		}
	}
	return checkSavedDepotOrder(input, depots)
}

// depotPlace is the place of a saved pod of a depot lane: its position
// along the lane path, and its slot.
type depotPlace struct {
	lane     int
	distance float64
	slot     int
}

// checkSavedDepotOrder checks the lane order of each depot lane. The pods
// of a lane are the pods idle at its slots and the other pods that name one
// of its slots as their destination. A pod is at its lane and lane distance
// when it has a lane of the arrival path (see position). A pod at a slot,
// or at the slot that it leaves, is at the end of the lane that ends at
// that slot. Any other pod is behind the lane path.
func checkSavedDepotOrder(input RestoreStateInput, depots *depotIndex) error {
	places := make(map[[2]int][]depotPlace)
	for _, pod := range input.State.Pods {
		idle := pod.Activity == activityCode(Idle)
		name := pod.Destination
		if idle {
			name = pod.BerthID
		}
		slot, ok := depots.slot(name)
		if !ok {
			continue
		}
		info := &depots.depots[slot.depot]
		path := &info.lanes[slot.lane]
		place := depotPlace{lane: -1, slot: slot.slot}
		if lane := path.position(info.incoming, pod.LaneID); pod.LaneID != "" && lane >= 0 {
			place.lane, place.distance = lane, pod.LaneDistance
		} else if at, ok := depots.slot(pod.BerthID); idle && ok {
			place.lane, place.distance = path.slotPosition(at.slot)
		} else if at, ok := depots.slot(pod.Origin); !idle && pod.LaneID == "" && ok && at.depot == slot.depot && at.lane == slot.lane {
			place.lane, place.distance = path.slotPosition(at.slot)
		}
		key := [2]int{slot.depot, slot.lane}
		places[key] = append(places[key], place)
	}
	for depot, info := range depots.depots {
		for lane := range info.lanes {
			lanePlaces := places[[2]int{depot, lane}]
			slices.SortFunc(lanePlaces, func(a, b depotPlace) int {
				return cmp.Or(cmp.Compare(b.lane, a.lane), cmp.Compare(b.distance, a.distance), cmp.Compare(a.slot, b.slot))
			})
			for i := 1; i < len(lanePlaces); i++ {
				if lanePlaces[i].slot <= lanePlaces[i-1].slot {
					return fmt.Errorf("depot station %q lane %d has pods out of slot order", info.station.ID, lane+1)
				}
			}
		}
	}
	return nil
}

// position returns the index of a lane in the arrival path of a depot lane:
// the incoming lane, the fan path, the lead-in, and then the chain lanes
// from the tail to the head. It returns -1 for any other lane.
func (p depotPath) position(incoming Lane, id string) int {
	if id == incoming.ID {
		return 0
	}
	if index := slices.IndexFunc(p.suffix(0), func(lane Lane) bool { return lane.ID == id }); index >= 0 {
		return index + 1
	}
	return -1
}

// slotPosition returns the place of a slot in the arrival path: the end of
// the lane that ends at the slot.
func (p depotPath) slotPosition(slot int) (int, float64) {
	return len(p.suffix(slot)), positionAtEnd
}

// positionAtEnd is the lane distance of a pod at the end of a lane in a
// lane order check. It is past each lane distance on the lane.
const positionAtEnd = 1e300
