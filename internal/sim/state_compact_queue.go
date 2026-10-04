package sim

import (
	"errors"
	"slices"
)

// SavedCompactQueue preserves one compact-v1 group in a save-6 head record.
// All positions and proof values use the shared entry lane's local coordinates.
type SavedCompactQueue struct {
	Kind          string    `json:"kind"`
	Phase         string    `json:"phase"`
	Lane          string    `json:"lane"`
	Members       []string  `json:"members"`
	Start         float64   `json:"start"`
	Frontier      float64   `json:"frontier"`
	StopCells     []int     `json:"stopCells"`
	Speeds        []float64 `json:"speeds"`
	Targets       []float64 `json:"targets"`
	LandingSpeeds []float64 `json:"landingSpeeds"`
}

type restoredCompactMember struct {
	saved  *SavedCompactQueue
	group  *compactBufferGroup
	offset int
}

func hasCompactCertificate(state SavedState) bool {
	return slices.ContainsFunc(state.Pods, func(p SavedPod) bool {
		return p.CompactQueue != nil || p.Platoon != nil && p.Platoon.Kind == "compact-buffer-v1"
	})
}

func checkCompactFields(input RestoreStateInput) error {
	mode := input.StationQueueSpacing
	if mode != "" && mode != StationQueueOrdinary && mode != StationQueueCompactV1 {
		return errors.New("invalid restored station queue spacing")
	}
	if mode == StationQueueCompactV1 && !input.StationBuffers {
		return errors.New("compact policy requires restored station buffers")
	}
	if input.PlatoonLimit != 0 && (input.PlatoonLimit < MinPlatoonLimit || input.PlatoonLimit > MaxPlatoonLimit) {
		return errors.New("invalid restored platoon limit")
	}
	if !hasCompactCertificate(input.State) {
		return nil
	}
	if !input.CompactQueues || !input.StationBuffers || !input.BufferPlatoons {
		return errors.New("compact certificate requires the save-6 physical contracts")
	}
	claimed := make(map[string]bool)
	for _, pod := range input.State.Pods {
		if link := pod.Platoon; link != nil && link.Kind == "compact-buffer-v1" {
			if link.TerminalCell == nil || *link.TerminalCell < 0 || link.Turn != 0 || link.Lanes != 1 || link.Draining {
				return errors.New("invalid compact predecessor link fields")
			}
		}
		q := pod.CompactQueue
		if q == nil {
			continue
		}
		n := len(q.Members)
		if q.Kind != "compact-buffer-v1" || q.Phase != "compact" && q.Phase != "recovering" || q.Lane == "" ||
			n < 1 || n > compactQueueMaxMembers || len(q.StopCells) != n || len(q.Speeds) != n || len(q.Targets) != n || len(q.LandingSpeeds) != n ||
			q.Members[0] != pod.ID || pod.Platoon != nil || !compactQueueBoundsValid(compactQueueBounds{start: q.Start, frontier: q.Frontier}) {
			return errors.New("invalid compact head certificate fields")
		}
		for i, id := range q.Members {
			if id == "" || claimed[id] || q.StopCells[i] < 0 || !finite(q.Speeds[i]) || q.Speeds[i] < 0 || q.Speeds[i] > compactQueueSpeedLimit || !finite(q.Targets[i]) || !finite(q.LandingSpeeds[i]) {
				return errors.New("invalid compact member fields")
			}
			claimed[id] = true
		}
	}
	for _, pod := range input.State.Pods {
		if pod.Platoon != nil && pod.Platoon.Kind == "compact-buffer-v1" && !claimed[pod.ID] {
			return errors.New("compact follower has no head certificate")
		}
	}
	return nil
}

func (s *Simulation) savedCompactQueue(v *vehicle) *SavedCompactQueue {
	group := s.compactGroup(v)
	if group == nil || s.vehicles[group.members[0]].Pod.ID != v.Pod.ID {
		return nil
	}
	q := &SavedCompactQueue{Kind: "compact-buffer-v1", Phase: "compact", Lane: group.lane, Start: group.bounds.start, Frontier: group.bounds.frontier,
		Members: make([]string, len(group.members)), StopCells: make([]int, len(group.members)), Speeds: make([]float64, len(group.members)),
		Targets: slices.Clone(group.recovery.targets), LandingSpeeds: slices.Clone(group.recovery.landingSpeeds)}
	if group.recovering {
		q.Phase = "recovering"
	}
	for i, index := range group.members {
		member := &s.vehicles[index]
		q.Members[i], q.StopCells[i], q.Speeds[i] = member.Pod.ID, member.reservedThrough-member.blocks.laneFirst(len(member.Route)-1), member.Pod.Speed
	}
	return q
}

func (r *physicalRestore) prepareCompactGroups() error {
	r.compactMembers = make(map[int]restoredCompactMember)
	for _, head := range r.state.Pods {
		q := head.CompactQueue
		if q == nil {
			continue
		}
		group := &compactBufferGroup{lane: q.Lane, bounds: compactQueueBounds{start: q.Start, frontier: q.Frontier}, recovering: q.Phase == "recovering",
			recovery: compactQueueRecovery{bounds: compactQueueBounds{start: q.Start, frontier: q.Frontier}, targets: slices.Clone(q.Targets), landingSpeeds: slices.Clone(q.LandingSpeeds)}}
		states := make([]compactQueueState, len(q.Members))
		for i, id := range q.Members {
			index, ok := r.s.vehicleIndexes[id]
			if !ok || r.demoted[index] {
				return errors.New("compact member lost its route or identity")
			}
			v, saved := &r.s.vehicles[index], r.state.Pods[index]
			plan, ok := r.s.bufferPlan(v)
			if !ok || !v.buffered || saved.Activity != "traveling" || saved.RouteIndex != len(v.Route)-1 || saved.LaneID != q.Lane || plan.lane.ID != q.Lane ||
				q.Start != v.blocks.end(plan.entryStop)-plan.start || q.Frontier != v.blocks.end(plan.frontier)-plan.start ||
				q.StopCells[i] > plan.frontier-plan.first || q.StopCells[i]+plan.first < plan.entryStop {
				return errors.New("compact member is outside its saved plain entry")
			}
			if saved.Distance != saved.LaneDistance || saved.RouteIndex != 0 {
				return errors.New("compact pose does not use its canonical saved entry coordinate")
			}
			if err := r.checkBufferPose(index); err != nil {
				return err
			}
			profile, supported := LookupVehicleClass(v.Pod.Class)
			if !supported || !profile.PhysicalSupported || profile.BodyLengthMeters != compactQueueBodyLength || r.s.platoonIndexes().shapes[q.Lane].curve != 0 ||
				plan.lane.SpeedLimit <= 0 || plan.lane.SpeedLimit > compactQueueSpeedLimit || !r.s.oneSpeedLimit(v, len(v.Route)-1, plan.lane.SpeedLimit) || !r.s.bufferHasDischarge(v) {
				return errors.New("compact member has an invalid physical profile or route")
			}
			if i > 0 {
				link := saved.Platoon
				if link == nil || link.Kind != "compact-buffer-v1" || link.Leader != q.Members[i-1] || link.Lane != saved.RouteIndex || link.LeaderLane != r.state.Pods[group.members[i-1]].RouteIndex || *link.TerminalCell != plan.frontier-plan.first {
					return errors.New("compact saved links do not match head membership")
				}
			} else if saved.Platoon != nil {
				return errors.New("compact head has a predecessor")
			}
			group.members = append(group.members, index)
			r.compactMembers[index] = restoredCompactMember{saved: q, group: group, offset: i}
			states[i] = compactQueueState{position: saved.LaneDistance, speed: q.Speeds[i], stopBoundary: v.blocks.end(plan.first+q.StopCells[i]) - plan.start}
		}
		if err := r.checkCompactProof(group, states); err != nil {
			return err
		}
		r.compactGroups = append(r.compactGroups, group)
	}
	return nil
}

func (r *physicalRestore) checkCompactProof(group *compactBufferGroup, states []compactQueueState) error {
	if group.recovering {
		return compactQueueRecoveryValidate(states, group.recovery)
	}
	var proof compactQueueRecovery
	var err error
	if len(states) == 1 {
		if holdErr := compactQueueHoldingValidate(r.s.compactHoldingState(group, states[0]), group.bounds, r.s.platoonLimit); holdErr != nil {
			return holdErr
		}
		proof, err = compactQueueSingletonRecovery(states[0], group.bounds)
	} else {
		proof, err = compactQueueRecoveryAdmission(states, group.bounds)
	}
	if err != nil {
		return err
	}
	if !slices.Equal(proof.targets, group.recovery.targets) || !slices.Equal(proof.landingSpeeds, group.recovery.landingSpeeds) {
		return errors.New("compact saved proof differs from deterministic admission")
	}
	return nil
}

func (r *physicalRestore) checkSavedCompactLink(index int) error {
	member, ok := r.compactMembers[index]
	if !ok || member.offset == 0 || r.leaders[index]-1 != member.group.members[member.offset-1] {
		return errors.New("compact link has no matching certificate")
	}
	v := &r.s.vehicles[index]
	plan, _ := r.s.bufferPlan(v)
	link := platoonLink{buffer: true, terminalCell: plan.frontier - plan.first, first: plan.entryStop + 1, lane: len(v.Route) - 1, lanes: 1}
	_, end := linkEnds(&v.blocks, link)
	if plan.first+member.saved.StopCells[member.offset] > end {
		return errors.New("compact member stopping cells exceed its shareable run")
	}
	return nil
}

func (r *physicalRestore) finishCompactRestore(input RestoreStateInput) error {
	r.s.compactGroups = r.compactGroups
	for _, group := range r.compactGroups {
		states, err := r.s.compactStates(group)
		if err != nil {
			return err
		}
		if err := r.checkCompactProof(group, states); err != nil {
			return err
		}
		if !r.s.compactOutsideFits(group, states) {
			return errors.New("compact saved group conflicts with outside traffic")
		}
		if input.StationQueueSpacing != StationQueueCompactV1 {
			group.recovering = true
		}
	}
	if len(r.compactGroups) > 0 || input.StationQueueSpacing == StationQueueCompactV1 {
		r.s.stationBuffers = input.StationBuffers
		r.s.stationQueueSpacing = input.StationQueueSpacing
	}
	return nil
}

// compactOwned checks the full retained footprint, not just a numeric boundary.
func (s *Simulation) compactOwned(v *vehicle) bool {
	for block, b := range v.blocks.span(0, v.reservedThrough+1) {
		for _, r := range b.resources {
			releaseAt := resourceReleaseDistance(b, r)
			if releaseAt <= v.distance {
				continue
			}
			if retained, ok := v.routeReleases[r]; !ok || retained < releaseAt {
				return false
			}
			owner := s.owners[r]
			if owner.isPod(v.Pod.ID) {
				continue
			}
			if owner.isZero() || !v.link.compact || r.kind != trackResource || block < v.link.first || block > v.link.end || !s.ownerAheadInPlatoon(v, owner) {
				return false
			}
		}
	}
	return true
}
