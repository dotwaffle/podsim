package sim

// reversalDwellTicks is the time in ticks that a pod stays at rest where
// the reverse flag of its route changes.
const reversalDwellTicks = TicksPerSecond

// reversal is the dwell latch of a pod. A pod faces against its direction
// of travel on a reverse lane of a maneuver group, so it stops and stays
// at rest for reversalDwellTicks at the start of each route lane whose
// reverse flag differs from the lane before it.
//
// lane is the route index k of the latched lane, 0 when the latch is
// empty, and -k after the dwell at k. ticks counts the ticks at rest at the
// start of lane k. The latch is a value, so Clone copies it. A save keeps
// the latch of a traveling pod, and the restore sets it again (see
// savedReversal).
type reversal struct {
	lane, ticks int
}

// clearReversal empties the dwell latch. A new route and an arrival call
// it.
func (v *vehicle) clearReversal() { v.reversal = reversal{} }

// reversalLimit returns limit, capped at the start of the latched lane.
// current is the route index of the lane of the pod.
//
// With no latched lane, it searches for the first lane k whose reverse
// flag differs from lane k-1, after the lane of the pod, or from that lane
// when the pod is at its start, and after the last lane with a dwell. Only
// lanes that start by limit can stop the pod, and a lane enters this range
// only when the pod can stop by its start, so the cap never needs more
// braking than the reservation. At the start of lane k at rest, each tick
// without a fault counts, and after reversalDwellTicks the latch records k
// as done.
func (s *Simulation) reversalLimit(v *vehicle, current int, limit float64) float64 {
	if s.maneuvers == nil || len(s.maneuvers.reverse) == 0 {
		return limit
	}
	blocks := &v.blocks
	if v.reversal.lane <= 0 {
		k := s.nextReversal(blocks, current, v.distance, -v.reversal.lane, limit)
		if k < 0 {
			return limit
		}
		v.reversal = reversal{lane: k}
	}
	point := blocks.lanes[v.reversal.lane].start
	if v.distance == point && v.Pod.Speed == 0 && !v.faulted {
		v.reversal.ticks++
		if v.reversal.ticks >= reversalDwellTicks {
			v.reversal = reversal{lane: -v.reversal.lane}
			return limit
		}
	}
	return min(limit, point)
}

// nextReversal returns the lane that a pod with no latched lane latches
// (see reversalLimit), or -1 when it latches no lane. The pod is at
// distance on blocks, in the lane at route index current, done is the
// route index of the last lane with a dwell, or 0, and limit is the limit
// of its move.
func (s *Simulation) nextReversal(blocks *blockList, current int, distance float64, done int, limit float64) int {
	if s.maneuvers == nil {
		return -1
	}
	from := current
	if current > 0 && distance == blocks.lanes[current].start {
		from = current - 1
	}
	from = max(from, done)
	for k := from + 1; k < len(blocks.route) && blocks.lanes[k].start <= limit; k++ {
		if s.maneuvers.reverse[blocks.route[k].ID] != s.maneuvers.reverse[blocks.route[k-1].ID] {
			return k
		}
	}
	return -1
}
