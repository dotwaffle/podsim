package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// StationQueueSpacing selects the fixed-entry queue spacing profile.
type StationQueueSpacing string

const (
	// StationQueueOrdinary keeps the ordinary clearance for all queue pairs.
	StationQueueOrdinary StationQueueSpacing = "ordinary"
	// StationQueueCompactV1 selects the approved straight four-meter profile.
	StationQueueCompactV1 StationQueueSpacing = "compact-v1"
)

// SetStationQueueSpacing selects queue spacing without changing lane speeds.
// Disabling compaction retains each certificate until its recovery completes.
func (s *Simulation) SetStationQueueSpacing(mode StationQueueSpacing) error {
	if mode != StationQueueOrdinary && mode != StationQueueCompactV1 {
		return fmt.Errorf("unknown station queue spacing %q", mode)
	}
	if mode == StationQueueCompactV1 && (!s.stationBuffers || s.platoonLimit < MinPlatoonLimit || s.platoonLimit > compactQueueMaxMembers) {
		return errors.New("compact queue spacing requires station buffers and a valid platoon limit")
	}
	s.stationQueueSpacing = mode
	return nil
}

// StationQueueSpacing returns the queue spacing profile. Its default is ordinary.
func (s *Simulation) StationQueueSpacing() StationQueueSpacing {
	if s.stationQueueSpacing == "" {
		return StationQueueOrdinary
	}
	return s.stationQueueSpacing
}

// CompactQueueError returns a retained physical-controller fault, or nil.
// Such a fault pauses the simulation before any member moves.
func (s *Simulation) CompactQueueError() error { return s.compactFault }

// compactBufferGroup owns membership and a frozen recovery proof on one entry.
// Membership is head first. All positions and proof values are lane-local.
type compactBufferGroup struct {
	members    []int
	lane       string
	bounds     compactQueueBounds
	recovery   compactQueueRecovery
	recovering bool
}

type compactSafetyPair struct {
	first, second Pod
	minimum       float64
}

type compactBufferMotion struct {
	state   compactQueueState
	planned bool
}

func (s *Simulation) compactGroup(v *vehicle) *compactBufferGroup {
	if len(s.compactGroups) == 0 {
		return nil
	}
	index, ok := s.vehicleIndexes[v.Pod.ID]
	if !ok {
		return nil
	}
	for _, group := range s.compactGroups {
		if slices.Contains(group.members, index) {
			return group
		}
	}
	return nil
}

// compactEntry accepts only a physically supported straight plain holding
// region. A faulted pod does not enter.
func (s *Simulation) compactEntry(v *vehicle) (stationBufferPlan, compactQueueBounds, bool) {
	if v.couplingID != "" || v.faulted {
		return stationBufferPlan{}, compactQueueBounds{}, false
	}
	plan, ok := s.bufferPlan(v)
	if !ok || v.Pod.Activity != Traveling || !v.buffered || v.Pod.LaneID != plan.lane.ID ||
		v.link.leader != 0 && !v.link.compact || v.follower != 0 && !s.vehicles[v.follower-1].link.compact {
		return stationBufferPlan{}, compactQueueBounds{}, false
	}
	profile, supported := LookupVehicleClass(v.Pod.Class)
	if !supported || !profile.PhysicalSupported || profile.BodyLengthMeters != compactQueueBodyLength ||
		plan.lane.SpeedLimit <= 0 || plan.lane.SpeedLimit > compactQueueSpeedLimit ||
		!s.oneSpeedLimit(v, len(v.Route)-1, plan.lane.SpeedLimit) ||
		s.platoonIndexes().shapes[plan.lane.ID].curve != 0 || !s.bufferHasDischarge(v) {
		return stationBufferPlan{}, compactQueueBounds{}, false
	}
	bounds := compactQueueBounds{start: v.blocks.end(plan.entryStop) - plan.start, frontier: v.blocks.end(plan.frontier) - plan.start}
	return plan, bounds, true
}

func (s *Simulation) compactStates(group *compactBufferGroup) ([]compactQueueState, error) {
	if len(group.members) < 1 || len(group.members) > compactQueueMaxMembers {
		return nil, errors.New("invalid compact group size")
	}
	states := make([]compactQueueState, len(group.members))
	for i, index := range group.members {
		if index < 0 || index >= len(s.vehicles) {
			return nil, errors.New("compact queue member index is invalid")
		}
		v := &s.vehicles[index]
		plan, bounds, ok := s.compactEntry(v)
		if !ok || !s.compactOwned(v) || plan.lane.ID != group.lane || bounds != group.bounds || v.reservedThrough < plan.entryStop || v.reservedThrough > plan.frontier {
			return nil, fmt.Errorf("compact queue member %s lost its entry or owned frontier", v.Pod.ID)
		}
		states[i] = compactQueueState{position: v.Pod.LaneDistance, speed: v.Pod.Speed, stopBoundary: v.blocks.end(v.reservedThrough) - plan.start}
		if i > 0 && (v.link.leader != group.members[i-1]+1 || !v.link.compact) {
			return nil, errors.New("compact queue membership does not match its links")
		}
		if i == 0 && v.link.leader != 0 || i+1 < len(group.members) && v.follower != group.members[i+1]+1 ||
			i+1 == len(group.members) && v.follower != 0 {
			return nil, errors.New("compact queue contains an outside link")
		}
	}
	return states, nil
}

func (s *Simulation) compactEnabled() bool {
	return s.StationQueueSpacing() == StationQueueCompactV1 && s.stationBuffers && s.platooning == PlatooningVirtual
}

// formCompactQueues admits only complete feasible candidate snapshots.
func (s *Simulation) formCompactQueues() {
	if !s.compactEnabled() {
		return
	}
	ahead := s.lanePredecessors()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if s.compactGroup(v) != nil || v.coupled() {
			continue
		}
		plan, bounds, ok := s.compactEntry(v)
		if !ok || v.Pod.LaneDistance < bounds.start || v.reservedThrough != plan.frontier {
			continue
		}
		state := compactQueueState{position: v.Pod.LaneDistance, speed: v.Pod.Speed, stopBoundary: bounds.frontier}
		group := &compactBufferGroup{members: []int{i}, lane: plan.lane.ID, bounds: bounds}
		if compactQueueHoldingValidate(s.compactHoldingState(group, state), bounds, s.platoonLimit) != nil {
			continue
		}
		proof, err := compactQueueSingletonRecovery(state, bounds)
		if err != nil {
			continue
		}
		group.recovery = proof
		s.compactGroups = append(s.compactGroups, group)
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.link.leader != 0 || v.follower != 0 || s.compactGroup(v) != nil || ahead[i] == 0 {
			continue
		}
		s.tryCompactFollower(i, ahead[i]-1)
	}
}

func (s *Simulation) tryCompactFollower(index, leaderIndex int) {
	v, leader := &s.vehicles[index], &s.vehicles[leaderIndex]
	group := s.compactGroup(leader)
	if group == nil || group.recovering || leader.follower != 0 || len(group.members) >= s.platoonLimit || len(group.members) >= compactQueueMaxMembers {
		return
	}
	plan, bounds, ok := s.compactEntry(v)
	if !ok || bounds != group.bounds || plan.lane.ID != group.lane || v.Pod.LaneDistance < bounds.start {
		return
	}
	pitch := s.laneLength(plan.lane) / float64(s.laneCells[plan.lane.ID].count())
	if leader.Pod.LaneDistance-v.Pod.LaneDistance > pitch+bufferRecruitmentSlack {
		return
	}
	candidate := &compactBufferGroup{members: append(slices.Clone(group.members), index), lane: group.lane, bounds: bounds}
	states, err := s.compactStates(group)
	if err != nil || compactQueueHoldingValidate(s.compactHoldingState(group, states[0]), bounds, s.platoonLimit) != nil {
		return
	}
	states = append(states, compactQueueState{position: v.Pod.LaneDistance, speed: v.Pod.Speed, stopBoundary: v.blocks.end(v.reservedThrough) - plan.start})
	proof, err := compactQueueRecoveryAdmission(states, bounds)
	if err != nil || !s.compactOutsideFits(candidate, states) {
		return
	}
	link := platoonLink{buffer: true, compact: true, terminalCell: plan.frontier - plan.first, first: plan.entryStop + 1,
		lane: len(v.Route) - 1, leaderLane: len(leader.Route) - 1, lanes: 1, clearance: compactQueueStandstillGap}
	_, link.end = linkEnds(&v.blocks, link)
	if link.end < link.first || v.reservedThrough > link.end {
		return
	}
	s.link(index, leaderIndex, link)
	group.members, group.recovery = candidate.members, proof
}

// compactOutsideFits retains ordinary separation for every uncertified pair.
func (s *Simulation) compactOutsideFits(group *compactBufferGroup, states []compactQueueState) bool {
	for member, index := range group.members {
		v := &s.vehicles[index]
		lane := v.Route[len(v.Route)-1]
		position := s.position(lane, states[member].position)
		for otherIndex := range s.vehicles {
			if slices.Contains(group.members, otherIndex) {
				continue
			}
			other := &s.vehicles[otherIndex].Pod
			location := s.laneSafety[other.LaneID]
			if other.BerthID != "" {
				location = s.berthSafety[other.BerthID]
			}
			separated := safetyLocationsSeparated(s.laneSafety[lane.ID], location)
			if largeVehicleClass(other.Class) {
				separated = envelopeLocationsSeparated(s.vehicleSafetyLocations(v, v.blocks.lanes[len(v.Route)-1].start+states[member].position), s.vehicleSafetyLocations(&s.vehicles[otherIndex], s.vehicles[otherIndex].distance))
			}
			if separated {
				continue
			}
			if math.Hypot(position.X-other.Position.X, position.Y-other.Position.Y) < classPairClearance(v.Pod.Class, other.Class) {
				return false
			}
		}
	}
	return true
}

func (s *Simulation) compactRecovery(group *compactBufferGroup, states []compactQueueState) error {
	if group.recovering {
		return nil
	}
	var proof compactQueueRecovery
	var err error
	if len(states) == 1 {
		proof, err = compactQueueSingletonRecovery(states[0], group.bounds)
	} else {
		proof, err = compactQueueRecoveryAdmission(states, group.bounds)
	}
	if err != nil {
		return err
	}
	group.recovery, group.recovering = proof, true
	return nil
}

// compactHeadSpeed chooses a feasible next speed while preserving future capacity.
func compactHeadSpeed(state compactQueueState, bounds compactQueueBounds, capacity int, limit float64) (float64, error) {
	lo, hi := compactQueueSpeedRange(state.speed)
	hi = min(hi, limit)
	fits := func(speed float64) bool {
		_, err := compactQueueHoldingStep(state, bounds, capacity, speed)
		return err == nil
	}
	if hi < lo || !fits(lo) {
		return 0, errors.New("compact queue head cannot preserve its holding reserve with legal braking")
	}
	speed := compactQueueLargestSpeed(lo, hi, fits)
	if lo == 0 && compactQueueStep(state, speed).position == state.position {
		speed = 0
	}
	return speed, nil
}

func (s *Simulation) compactHoldingState(group *compactBufferGroup, state compactQueueState) compactQueueState {
	head := &s.vehicles[group.members[0]]
	plan, _ := s.bufferPlan(head)
	link := platoonLink{buffer: true, terminalCell: plan.frontier - plan.first, first: plan.entryStop + 1, lane: len(head.Route) - 1, lanes: 1}
	_, end := linkEnds(&head.blocks, link)
	if end >= link.first {
		state.stopBoundary = min(state.stopBoundary, head.blocks.end(end)-plan.start)
	}
	return state
}

// compactCanDischarge checks suffix availability without changing claims.
func (s *Simulation) compactCanDischarge(group *compactBufferGroup) bool {
	return s.probeCompactDischarge(group).available
}

func (s *Simulation) planCompactQueues() error {
	if len(s.compactGroups) == 0 {
		s.compactMotions, s.compactNextGroups = nil, nil
		return nil
	}
	s.compactMotions = make([]compactBufferMotion, len(s.vehicles))
	s.compactNextGroups = nil
	for _, original := range s.compactGroups {
		copyGroup := *original
		group := &copyGroup
		states, err := s.compactStates(group)
		if err != nil {
			return err
		}
		if !s.compactEnabled() || len(group.members) > s.platoonLimit || s.compactCanDischarge(group) ||
			compactQueueHoldingValidate(s.compactHoldingState(group, states[0]), group.bounds, s.platoonLimit) != nil {
			if recoveryErr := s.compactRecovery(group, states); recoveryErr != nil {
				return recoveryErr
			}
		}
		planned, err := s.compactPlan(group, states)
		if err != nil {
			return err
		}
		if !s.compactOutsideFits(group, planned) {
			return errors.New("compact queue plan conflicts with outside traffic")
		}
		s.compactNextGroups = append(s.compactNextGroups, group)
		for i, index := range group.members {
			s.compactMotions[index] = compactBufferMotion{state: planned[i], planned: true}
		}
	}
	return nil
}

func (s *Simulation) compactPlan(group *compactBufferGroup, states []compactQueueState) ([]compactQueueState, error) {
	if group.recovering {
		planned, _, err := compactQueueRecoveryStep(states, group.recovery)
		return planned, err
	}
	head := &s.vehicles[group.members[0]]
	speed, err := compactHeadSpeed(s.compactHoldingState(group, states[0]), group.bounds, s.platoonLimit, head.Route[len(head.Route)-1].SpeedLimit)
	if err != nil {
		return nil, err
	}
	// A partially formed group waits for adjacent members. Driving its head
	// away would leave the next ordinary pod several occupied cells behind.
	if len(states) < s.platoonLimit {
		lo, hi := compactQueueSpeedRange(states[0].speed)
		speed = lo
		plan, _ := s.bufferPlan(head)
		pitch := s.laneLength(plan.lane) / float64(s.laneCells[plan.lane.ID].count())
		target := group.bounds.start + pitch
		if len(states) == 1 && states[0].position < target {
			fits := func(next float64) bool {
				_, holdErr := compactQueueHoldingStep(s.compactHoldingState(group, states[0]), group.bounds, s.platoonLimit, next)
				return holdErr == nil && compactQueueRecoveryCandidate(states[0], next, target, group.bounds)
			}
			if !fits(lo) {
				return nil, errors.New("compact singleton cannot brake before its first holding target")
			}
			speed = compactQueueLargestSpeed(lo, min(hi, head.Route[len(head.Route)-1].SpeedLimit), fits)
		}
	}
	if len(states) == 1 {
		planned, holdErr := compactQueueHoldingStep(states[0], group.bounds, s.platoonLimit, speed)
		if holdErr != nil {
			return nil, holdErr
		}
		proof, singletonErr := compactQueueSingletonRecovery(planned, group.bounds)
		if singletonErr != nil {
			return nil, singletonErr
		}
		group.recovery = proof
		return []compactQueueState{planned}, nil
	}
	planned, err := compactQueueRecoverablePlan(states, group.bounds, speed)
	if err == nil {
		planned, err = compactRemovePlateaus(states, planned, group.bounds)
	}
	if err != nil {
		planned, err = compactBrakingPlan(states, group.bounds)
		if err != nil {
			return nil, err
		}
	}
	group.recovery, err = compactQueueRecoveryAdmission(planned, group.bounds)
	return planned, err
}

// moveCompact applies an exact planned reference position and speed together.
// It does not run the ordinary endpoint snap or recompute the command.
func (s *Simulation) moveCompact(v *vehicle) bool {
	if len(s.compactMotions) == 0 {
		return false
	}
	index := s.vehicleIndexes[v.Pod.ID]
	if index >= len(s.compactMotions) || !s.compactMotions[index].planned {
		return false
	}
	state := s.compactMotions[index].state
	lane := len(v.Route) - 1
	v.distance = v.blocks.lanes[lane].start + state.position
	v.Pod.Speed, v.Pod.LaneDistance = state.speed, state.position
	for v.blockIndex < v.reservedThrough && v.distance >= v.blocks.end(v.blockIndex) {
		v.blockIndex++
	}
	v.Pod.LaneID = v.Route[lane].ID
	v.Pod.Position = s.lanePosition(v.blocks.lanes[lane].geometry, &v.Route[lane], state.position)
	return true
}

func cloneCompactGroups(groups []*compactBufferGroup) []*compactBufferGroup {
	cloned := slices.Clone(groups)
	for i, group := range groups {
		copyGroup := *group
		copyGroup.members = slices.Clone(group.members)
		copyGroup.recovery.targets = slices.Clone(group.recovery.targets)
		copyGroup.recovery.landingSpeeds = slices.Clone(group.recovery.landingSpeeds)
		cloned[i] = &copyGroup
	}
	return cloned
}

func (s *Simulation) cloneCompactQueues(c *Simulation) {
	c.compactGroups = cloneCompactGroups(s.compactGroups)
	c.compactNextGroups = cloneCompactGroups(s.compactNextGroups)
	c.compactMotions = slices.Clone(s.compactMotions)
}

// finishCompactQueues converts a stopped recovered group to ordinary draining links.
// Existing resource tails remain owned until ordinary release transfers them.
func (s *Simulation) finishCompactQueues() {
	kept := s.compactGroups[:0]
	var finished []int
	for _, group := range s.compactGroups {
		states, err := s.compactStates(group)
		done := group.recovering && err == nil
		for i, state := range states {
			done = done && state.speed == 0 && state.position == group.recovery.targets[i]
		}
		if !done {
			kept = append(kept, group)
			continue
		}
		for _, index := range group.members[1:] {
			v := &s.vehicles[index]
			v.link.compact, v.link.clearance, v.link.draining = false, compactQueueOrdinaryGap, true
		}
		finished = append(finished, group.members[0])
	}
	s.compactGroups = kept
	for _, index := range finished {
		head := &s.vehicles[index]
		plan, ok := s.bufferPlan(head)
		if ok {
			// Recovery can finish before stock lookahead requests a suffix.
			s.grantBufferedHead(intent{index: index, block: head.reservedThrough + 1, id: head.Pod.ID}, plan)
		}
	}
}

// compactSafety grants private direct-neighbor exceptions from validated groups.
func (s *Simulation) compactSafety(observation *SafetyObservation) {
	if len(s.compactGroups) == 0 {
		return
	}
	observation.compactPairs = make(map[[2]string]compactSafetyPair)
	for _, group := range s.compactGroups {
		states, err := s.compactStates(group)
		if err == nil {
			switch {
			case group.recovering || len(states) == 1:
				err = compactQueueRecoveryValidate(states, group.recovery)
			default:
				_, err = compactQueueRecoveryAdmission(states, group.bounds)
			}
		}
		if err != nil {
			observation.compactError = err
			return
		}
		for i := 1; i < len(group.members); i++ {
			leader := s.vehicles[group.members[i-1]].Pod.ID
			follower := s.vehicles[group.members[i]].Pod.ID
			observation.compactPairs[[2]string{leader, follower}] = compactSafetyPair{first: s.vehicles[group.members[i-1]].Pod, second: s.vehicles[group.members[i]].Pod, minimum: compactQueueEnvelope(states[i].speed, states[i-1].speed)}
		}
	}
}

func (s *Simulation) compactFrontierCandidate(v *vehicle, plan stationBufferPlan) bool {
	profile, ok := LookupVehicleClass(v.Pod.Class)
	if !ok || !profile.PhysicalSupported || profile.BodyLengthMeters != compactQueueBodyLength || v.coupled() || s.compactGroup(v) != nil ||
		plan.lane.SpeedLimit <= 0 || plan.lane.SpeedLimit > compactQueueSpeedLimit || s.platoonIndexes().shapes[plan.lane.ID].curve != 0 ||
		!s.oneSpeedLimit(v, len(v.Route)-1, plan.lane.SpeedLimit) {
		return false
	}
	trial := *v
	return s.bufferHead(&trial, plan)
}

// compactBrakingPlan rejects a greedy command that lacks constructive recovery.
// It uses the original snapshot and validates both phases before applying motion.
func compactBrakingPlan(states []compactQueueState, bounds compactQueueBounds) ([]compactQueueState, error) {
	if _, err := compactQueueRecoveryAdmission(states, bounds); err != nil {
		return nil, err
	}
	planned := make([]compactQueueState, len(states))
	for i, state := range states {
		lo, _ := compactQueueSpeedRange(state.speed)
		planned[i] = compactQueueStep(state, lo)
	}
	if _, err := compactQueueRecoveryAdmission(planned, bounds); err != nil {
		return nil, err
	}
	return planned, nil
}

// compactRemovePlateaus brakes zero-travel velocities inside the legal interval.
// Replanning each following member keeps its envelope against the actual command.
func compactRemovePlateaus(states, planned []compactQueueState, bounds compactQueueBounds) ([]compactQueueState, error) {
	for i := 1; i < len(states); i++ {
		lo, hi := compactQueueSpeedRange(states[i].speed)
		leader := &planned[i-1]
		fits := func(speed float64) bool { return compactQueueFits(compactQueueStep(states[i], speed), bounds, leader) }
		if !fits(lo) {
			return nil, errors.New("compact plateau follower cannot brake within its envelope")
		}
		speed := compactQueueLargestSpeed(lo, hi, fits)
		if lo == 0 && compactQueueStep(states[i], speed).position == states[i].position {
			speed = 0
		}
		planned[i] = compactQueueStep(states[i], speed)
	}
	if _, err := compactQueueRecoveryAdmission(planned, bounds); err != nil {
		return nil, err
	}
	return planned, nil
}

// NeedsCompactQueueState reports an active compact policy or retained certificate.
func (s *Simulation) NeedsCompactQueueState() bool {
	return s.StationQueueSpacing() == StationQueueCompactV1 || len(s.compactGroups) > 0
}

// prepareCompactLimit freezes feasible recovery before capacity consumes its reserve.
func (s *Simulation) prepareCompactLimit(limit int) error {
	groups := cloneCompactGroups(s.compactGroups)
	for _, group := range groups {
		if group.recovering {
			continue
		}
		states, err := s.compactStates(group)
		if err != nil {
			return err
		}
		if len(states) > limit || compactQueueHoldingValidate(s.compactHoldingState(group, states[0]), group.bounds, limit) != nil {
			if recoveryErr := s.compactRecovery(group, states); recoveryErr != nil {
				return recoveryErr
			}
		}
	}
	s.compactGroups = groups
	return nil
}
