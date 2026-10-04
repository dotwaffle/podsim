package sim

import "slices"

// SetStationBuffers enables experimental local station buffers. The default
// is false. Disabling new admissions keeps existing buffer members draining.
// Projects select this option through their experimental settings.
func (s *Simulation) SetStationBuffers(enabled bool) { s.stationBuffers = enabled }

// NeedsBufferState reports whether a save requires the version 3 buffer
// contract, including pending admissions that have not reached the entry lane.
func (s *Simulation) NeedsBufferState() bool {
	return s.stationBuffers || slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.buffered })
}

// NeedsBufferPlatoonState reports whether fixed entry links require version 4.
func (s *Simulation) NeedsBufferPlatoonState() bool {
	return slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.link.leader != 0 && v.link.buffer })
}

// StationBufferGeometry describes conservative stopping cells. These cells
// still require ordinary track and conflict admission before a pod can enter.
type StationBufferGeometry struct {
	Station, Lane                   string
	StoppingCells                   int
	FirstStopMeters, LastStopMeters float64
}

// StationBufferGeometry returns eligible holding regions in network order.
// It does not enable buffers or modify vehicle state.
func (s *Simulation) StationBufferGeometry() []StationBufferGeometry {
	var geometry []StationBufferGeometry
	for _, lane := range s.network.Lanes {
		if lane.StationRole != StationEntryRole {
			continue
		}
		v := vehicle{destinationStation: lane.StationID}
		s.setVehicleRoute(&v, []Lane{lane})
		plan, ok := s.bufferPlan(&v)
		if !ok {
			continue
		}
		geometry = append(geometry, StationBufferGeometry{
			Station: lane.StationID, Lane: lane.ID,
			StoppingCells:   plan.frontier - plan.entryStop + 1,
			FirstStopMeters: v.blocks.end(plan.entryStop), LastStopMeters: v.blocks.end(plan.frontier),
		})
	}
	return geometry
}

type stationBufferPlan struct {
	lane                       Lane
	first, entryStop, frontier int
	start                      float64
}

// bufferPlan accepts only an entry-ending route with a contiguous interior
// of ordinary track cells. Both endpoint resource groups remain outside it.
func (s *Simulation) bufferPlan(v *vehicle) (stationBufferPlan, bool) {
	if len(v.Route) == 0 || v.destination.ID != "" {
		return stationBufferPlan{}, false
	}
	index := len(v.Route) - 1
	lane := v.Route[index]
	station, ok := s.station(v.destinationStation)
	if !ok || station.ParkingOnly || lane.StationRole != StationEntryRole || lane.StationID != station.ID || !station.isEntry(lane.To) {
		return stationBufferPlan{}, false
	}
	cells := s.laneCells[lane.ID]
	if cells == nil || cells.count() < 4 || s.laneLength(lane)/float64(cells.count()) < max(Clearance, cells.tail) {
		return stationBufferPlan{}, false
	}
	first, last := -1, -1
	for cell := 1; cell < cells.count()-1; cell++ {
		plain := !slices.ContainsFunc(cells.cell(cell), func(r resource) bool { return r.kind != trackResource })
		if plain {
			if first < 0 {
				first = cell
			}
			last = cell
		} else if first >= 0 {
			break
		}
	}
	if last-first < 1 || first < 0 {
		return stationBufferPlan{}, false
	}
	base := v.blocks.laneFirst(index)
	entryStop := reservationEnd(&v.blocks, base+first)
	frontier := reservationEnd(&v.blocks, base+last)
	if entryStop > base+last || frontier != base+last {
		return stationBufferPlan{}, false
	}
	return stationBufferPlan{lane: lane, first: base, entryStop: entryStop, frontier: frontier, start: v.blocks.lanes[index].start}, true
}

func (s *Simulation) bufferApproach(v *vehicle) bool {
	if !s.stationBuffers && !v.buffered {
		return false
	}
	if _, ok := s.bufferPlan(v); !ok {
		v.buffered, v.bufferBerth = false, ""
		return false
	}
	v.buffered = true
	return true
}

// bufferHead includes assigned pods ahead, while their tails still occupy
// the entry lane. Resource ownership remains the admission authority.
func (s *Simulation) bufferHead(v *vehicle, plan stationBufferPlan) bool {
	position := v.distance - plan.start
	for index := range s.vehicles {
		other := &s.vehicles[index]
		if other == v || other.Pod.Activity != Traveling {
			continue
		}
		for laneIndex, lane := range other.Route {
			if lane.ID != plan.lane.ID {
				continue
			}
			distance := other.distance - other.blocks.lanes[laneIndex].start
			tail := max(classPairClearance(v.Pod.Class, other.Pod.Class), blockTail(other.blocks.at(other.blocks.laneFirst(laneIndex))))
			if distance > position && distance < s.laneLength(lane)+tail {
				v.Pod.BlockedBy, v.Pod.WaitReason = other.Pod.ID, TrackOccupied
				return false
			}
		}
	}
	return true
}

// grantBufferedHead checks complete berth paths without publishing an
// assignment. A denied trial changes no ownership or committed route.
func (s *Simulation) grantBufferedHead(in intent, plan stationBufferPlan) {
	v := &s.vehicles[in.index]
	if group := s.compactGroup(v); group != nil {
		return
	}
	if !s.bufferHead(v, plan) || v.link.leader != 0 || v.follower != 0 && !s.vehicles[v.follower-1].link.buffer {
		return
	}
	station, _ := s.station(v.destinationStation)
	v.bufferBerth = ""
	blockedBerth, blockedOwner := "", ""
	accept := s.berthFilterForVehicle(v)
	for _, berth := range station.Berths {
		if accept != nil && !accept(berth) {
			continue
		}
		if station.Banks != nil && station.berthEntry(berth) != plan.lane.To {
			continue
		}
		// A following pickup may target this berth without owning it.
		// Deferring to that assignment would prevent either pod advancing.
		// The complete-path grant below protects all actual reservations.
		claims, available := s.bufferBerthClaims(v, berth)
		if !available {
			if owner := s.owners[resource{kind: berthResource, id: berth.ID}]; !owner.isZero() {
				if blockedOwner == "" || s.ownerVehicle(owner) != nil && s.ownerVehicle(owner).Pod.Activity == Idle {
					blockedBerth, blockedOwner = berth.ID, owner.String()
				}
			}
			continue
		}
		suffix, err := s.stationPathForClass(plan.lane.To, berth.Node, v.Pod.Class)
		if err != nil || len(suffix) == 0 {
			continue
		}
		if v.follower != 0 && !s.bufferSuffixValid(v, suffix, berth) {
			continue
		}
		route := append(slices.Clone(v.Route), suffix...)
		if !s.rerouteKeepsDetours(v, route, berth) {
			continue
		}
		before := *v
		var follower *vehicle
		wasDraining := false
		if v.follower != 0 {
			follower = &s.vehicles[v.follower-1]
			wasDraining = follower.link.draining
			follower.link.draining = true
		}
		for _, claim := range claims {
			if claim.owner != nil {
				delete(s.owners, claim.resource)
			}
		}
		s.setVehicleRoute(v, route)
		v.destination, v.buffered, v.bufferBerth = berth, false, ""
		trial := in
		trial.through = v.blocks.len() - 1
		s.grant(trial)
		if v.reservedThrough == trial.through {
			s.finishBufferClaimYield(claims)
			return
		}
		for _, claim := range claims {
			if claim.owner != nil {
				s.owners[claim.resource] = podResourceOwner(claim.owner.Pod.ID)
			}
		}
		reason, blocker := v.Pod.WaitReason, v.Pod.BlockedBy
		*v = before
		if follower != nil {
			follower.link.draining = wasDraining
		}
		v.Pod.WaitReason, v.Pod.BlockedBy = reason, blocker
	}
	if blockedOwner != "" {
		v.Pod.BlockedBy, v.Pod.WaitReason, v.bufferBerth = blockedOwner, BerthOccupied, blockedBerth
	}
	if v.Pod.WaitReason == NoWait {
		v.Pod.WaitReason = BerthOccupied
	}
}
