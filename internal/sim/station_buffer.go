package sim

import "slices"

// SetStationBuffers enables experimental local station buffers. The default
// is false. Disabling new admissions keeps existing buffer members draining.
// Projects and ordinary version 2 saved states do not enable this option.
func (s *Simulation) SetStationBuffers(enabled bool) { s.stationBuffers = enabled }

// NeedsBufferState reports whether a save requires the version 3 buffer
// contract, including pending admissions that have not reached the entry lane.
func (s *Simulation) NeedsBufferState() bool {
	return s.stationBuffers || slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.buffered })
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
	if !ok || station.ParkingOnly || lane.StationRole != StationEntryRole || lane.StationID != station.ID || lane.To != station.Entry {
		return stationBufferPlan{}, false
	}
	cells := s.laneCells[lane.ID]
	if cells == nil || cells.count() < 4 || s.laneLength(lane)/float64(cells.count()) < Clearance {
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
			if distance > position && distance < s.laneLength(lane)+Clearance {
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
	if !s.bufferHead(v, plan) || v.coupled() {
		return
	}
	station, _ := s.station(v.destinationStation)
	v.bufferBerth = ""
	blockedBerth, blockedOwner := "", ""
	for _, berth := range station.Berths {
		if !s.berthAvailableFor(v, berth) {
			if owner := s.owners[resource{kind: berthResource, id: berth.ID}]; owner != "" {
				if blockedOwner == "" || s.findVehicle(owner) != nil && s.findVehicle(owner).Pod.Activity == Idle {
					blockedBerth, blockedOwner = berth.ID, owner
				}
			}
			continue
		}
		suffix, err := s.stationPath(station.Entry, berth.Node)
		if err != nil || len(suffix) == 0 {
			continue
		}
		before := *v
		route := append(slices.Clone(v.Route), suffix...)
		s.setVehicleRoute(v, route)
		v.destination, v.buffered, v.bufferBerth = berth, false, ""
		trial := in
		trial.through = v.blocks.len() - 1
		s.grant(trial)
		if v.reservedThrough == trial.through {
			return
		}
		reason, blocker := v.Pod.WaitReason, v.Pod.BlockedBy
		*v = before
		v.Pod.WaitReason, v.Pod.BlockedBy = reason, blocker
	}
	if blockedOwner != "" {
		v.Pod.BlockedBy, v.Pod.WaitReason, v.bufferBerth = blockedOwner, BerthOccupied, blockedBerth
	}
	if v.Pod.WaitReason == NoWait {
		v.Pod.WaitReason = BerthOccupied
	}
}
