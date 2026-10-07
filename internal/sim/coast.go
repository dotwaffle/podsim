package sim

import "math"

// deniedRequest is a request that grant refused because another pod holds
// a track, node, or junction resource of it. in is a copy of the intent,
// and through is the last block of the request.
type deniedRequest struct {
	in      intent
	through int
}

// coastCap is the coast ceiling of a pod in the tick tick, when set is
// true. The zero value has no ceiling.
type coastCap struct {
	ceiling float64
	tick    int64
	set     bool
}

// at returns the ceiling and true when the pod has a ceiling in tick.
func (c coastCap) at(tick int64) (float64, bool) {
	return c.ceiling, c.set && c.tick == tick
}

// coastResult is the coast ceiling of the pod at index in s.vehicles.
type coastResult struct {
	index   int
	ceiling float64
}

// coastCaps sets the coast ceiling of each pod whose request grant refused
// in this tick, as coastCeilings finds it. It runs after platoonCaps and
// before the pods move, and it reads only the state after platoonCaps, so
// the order of the pods does not change a result. A ceiling applies only
// in the tick in which coastCaps sets it (see move). Then coastCaps clears
// the denied requests.
func (s *Simulation) coastCaps() {
	work := s.admissionWork
	if work == nil {
		return
	}
	for _, result := range s.coastCeilings(work.denied) {
		s.vehicles[result.index].coast = coastCap{ceiling: result.ceiling, tick: s.tick, set: true}
	}
	clear(work.denied)
	work.denied = work.denied[:0]
}

// coastCeilings returns the coast ceilings of the denied requests. With
// platoons on, a pod that waits for the junction zone of a station
// diverge, the node where an entry lane starts, can coast: it holds back
// from its reservation end and starts again so that it rolls into the
// zone when the zone frees. A ceiling only lowers the commanded speed. See
// coastCeiling for the rules. The result does not depend on the order of
// denied.
func (s *Simulation) coastCeilings(denied []deniedRequest) []coastResult {
	if s.platooning == PlatooningOff || len(denied) == 0 {
		return nil
	}
	diverges := s.platoonIndexes().diverges
	// rivals holds the indexes in denied of the requests whose span holds
	// each diverge junction.
	rivals := make(map[resource][]int)
	junctions := make([]resource, len(denied))
	for k, d := range denied {
		if r, ok := divergeJunction(&s.vehicles[d.in.index].blocks, d, diverges); ok {
			junctions[k] = r
			rivals[r] = append(rivals[r], k)
		}
	}
	var results []coastResult
	for k, d := range denied {
		if junctions[k] == (resource{}) {
			continue
		}
		if ceiling, ok := s.coastCeiling(denied, k, rivals[junctions[k]]); ok {
			results = append(results, coastResult{index: d.in.index, ceiling: ceiling})
		}
	}
	return results
}

// divergeJunction returns the first junction resource of a station
// diverge in the span of d.
func divergeJunction(blocks *blockList, d deniedRequest, diverges map[string]bool) (resource, bool) {
	for resources := range blocks.spanResources(d.in.block, d.through+1) {
		for _, r := range resources {
			if r.kind == junctionResource && diverges[r.id] {
				return r, true
			}
		}
	}
	return resource{}, false
}

// coastCeiling returns the ceiling of the pod v of denied[k], whose span
// holds a station diverge junction. rivals holds the indexes in denied of
// the requests whose span holds the same junction, k included. v gets a
// ceiling only when each rule holds:
//
//  1. v is traveling, it is not faulted, it has no emergency record, and
//     it is not a large class.
//  2. v is not a follower whose request linkedSpan accepts.
//  3. Another denied request waits for the junction.
//  4. v retains no node or junction resource whose release distance is
//     after its position and at most its reservation end. So coast does
//     not delay a node or junction release of v within its reservation.
//  5. Each resource of the span that another pod owns has a releaser
//     (see coastReleaser) that moves.
//
// release is the largest time until a releaser reaches its release
// distance, plus one tick. v is first when its intent precedes the
// intent of each rival in admission order at that time. The hold point is
// hold before the reservation end, with hold the smaller of the gap and
// L²/(4 × acceleration), where L is the speed limit of the first requested
// block. A first pod that can reach the zone from the hold point within
// release does not coast. Otherwise the ceiling is the speed that stops
// the pod at the hold point, but never less than the speed that keeps its
// stop point. So the stop point of a coasting pod never moves back, and
// its speed drops by at most acceleration × dt in a tick.
func (s *Simulation) coastCeiling(denied []deniedRequest, k int, rivals []int) (float64, bool) {
	d := denied[k]
	v := &s.vehicles[d.in.index]
	if v.Pod.Activity != Traveling || v.faulted || s.emergencyOf(v) >= 0 || largeVehicleClass(v.Pod.Class) {
		return 0, false
	}
	if v.link.leader != 0 && s.linkedSpan(v, d.in.block, d.through) {
		return 0, false
	}
	if len(rivals) < 2 {
		return 0, false
	}
	frontier := v.blocks.end(v.reservedThrough)
	for r, releaseAt := range v.routeReleases {
		if (r.kind == nodeResource || r.kind == junctionResource) && releaseAt > v.distance && releaseAt <= frontier {
			return 0, false
		}
	}
	release, ok := s.coastRelease(v, d)
	if !ok {
		return 0, false
	}
	release += 1.0 / TicksPerSecond
	at := s.tick + int64(math.Ceil(release*TicksPerSecond))
	first := true
	for _, rival := range rivals {
		if rival != k && compareAdmission(denied[rival].in, d.in, at) < 0 {
			first = false
		}
	}
	limit := v.blocks.lane(d.in.block).SpeedLimit
	gap := max(0, frontier-v.distance)
	hold := min(limit*limit/(4*acceleration), gap)
	if first && release <= math.Sqrt(hold/acceleration) {
		return 0, false
	}
	return max(stopSpeed(gap-hold), keepSpeed(v.Pod.Speed)), true
}

// coastRelease returns the largest time until the releaser of each
// resource of the span of d that another pod owns reaches its release
// distance. It reports false when such a resource has no releaser that
// moves.
func (s *Simulation) coastRelease(v *vehicle, d deniedRequest) (float64, bool) {
	release, found := 0.0, false
	for resources := range v.blocks.spanResources(d.in.block, d.through+1) {
		for _, r := range resources {
			owner := s.owners[r]
			if owner.isZero() || owner.isPod(v.Pod.ID) {
				continue
			}
			p := s.coastReleaser(owner, r)
			if p == nil || p == v || p.Pod.Activity != Traveling || p.faulted || p.Pod.Speed == 0 {
				return 0, false
			}
			lane := p.blocks.currentLane(p.blockIndex)
			if t := coastTravelTime(p.routeReleases[r]-p.distance, p.Pod.Speed, lane.SpeedLimit); !found || t > release {
				release, found = t, true
			}
		}
	}
	return release, found
}

// coastReleaser returns the pod that frees r last: the owner, or the last
// pod behind it in its platoon that retains r past its position. An owner
// passes r to the first such pod behind it (see releaseRouteResource), and
// that pod passes it on in the same way. It returns nil when the owner is
// not a pod or does not retain r.
func (s *Simulation) coastReleaser(owner resourceOwner, r resource) *vehicle {
	u := s.ownerVehicle(owner)
	if u == nil {
		return nil
	}
	if _, retained := u.routeReleases[r]; !retained {
		return nil
	}
	p := u
	for follower := u.follower; follower != 0; follower = s.vehicles[follower-1].follower {
		member := &s.vehicles[follower-1]
		if releaseAt, ok := member.routeReleases[r]; ok && releaseAt > member.distance {
			p = member
		}
	}
	return p
}

// coastTravelTime returns the time to travel distance from speed, at full
// acceleration up to top and then at top.
func coastTravelTime(distance, speed, top float64) float64 {
	if distance <= 0 {
		return 0
	}
	top = max(top, speed)
	if top <= 0 {
		return math.Inf(1)
	}
	ramp := (top*top - speed*speed) / (2 * acceleration)
	if distance <= ramp {
		return (math.Sqrt(speed*speed+2*acceleration*distance) - speed) / acceleration
	}
	return (top-speed)/acceleration + (distance-ramp)/top
}

// stopSpeed returns the commanded speed after which a pod stops in
// distance: c × dt + c²/(2 × acceleration) = distance.
func stopSpeed(distance float64) float64 {
	step := acceleration / TicksPerSecond
	return math.Sqrt(step*step+2*acceleration*max(0, distance)) - step
}

// keepSpeed returns the commanded speed that keeps the stop point of a pod
// at speed: c × dt + c²/(2 × acceleration) = speed²/(2 × acceleration). It
// is at least speed - acceleration × dt.
func keepSpeed(speed float64) float64 {
	step := acceleration / TicksPerSecond
	return math.Sqrt(speed*speed+step*step) - step
}
