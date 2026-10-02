package sim

import (
	"cmp"
	"fmt"
	"math/bits"
	"slices"
)

// Positioning selects how the simulation moves idle empty pods before
// passengers ask for them.
type Positioning int

const (
	// PositioningOff moves no idle empty pod before a passenger asks for it.
	PositioningOff Positioning = iota
	// PositioningGuarded moves an idle empty pod to a demand station that
	// has no pod, and moves a bumped idle pod to a near berth. It moves pods
	// only while the request rate is low. At a higher rate, the simulation
	// runs as with PositioningOff. See positionGuarded and guardedClear.
	PositioningGuarded
)

const (
	// guardedCheckTicks is the time between two guarded checks.
	guardedCheckTicks = 5 * TicksPerSecond
	// guardedReachSeconds is the highest route cost of a guarded move. A pod
	// with passengers that becomes available in this time supplies its
	// destination station.
	guardedReachSeconds = 180.0
	// guardedLivenessTicks is the longest time after the newest request
	// that the gate stays open.
	guardedLivenessTicks = 180 * TicksPerSecond
	// guardedHorizonMinutes is the time in which a demand station expects
	// at least half a request.
	guardedHorizonMinutes = 15
	// guardedFleetRateShare sets the rate limit of the gate. The gate is
	// active below one request per minute for each guardedFleetRateShare
	// pods.
	guardedFleetRateShare = 20
	// guardedWorkingNumerator and guardedWorkingDenominator give the
	// largest share of the fleet that can work while the gate is open.
	guardedWorkingNumerator   = 2
	guardedWorkingDenominator = 5
	// guardedDeficitTries is the largest number of deficit stations that
	// one check tries.
	guardedDeficitTries = 3
	// ticksPerMinute is the number of ticks in a simulated minute.
	ticksPerMinute = 60 * TicksPerSecond
)

// SetPositioning selects the positioning mode. It returns an error for an
// unknown mode and then changes nothing. A mode other than PositioningOff
// can move a pod at the next step. Reset selects PositioningOff. The saved
// state does not keep the mode, so the session sets it again after a
// restore.
func (s *Simulation) SetPositioning(mode Positioning) error {
	if mode < PositioningOff || mode > PositioningGuarded {
		return fmt.Errorf("unknown positioning mode %d", mode)
	}
	s.setPositioning(mode)
	return nil
}

// Positioning returns the positioning mode.
func (s *Simulation) Positioning() Positioning { return s.positioning }

// SetDemandRate sets the demand rate that the guarded gate reads, in
// requests per minute. It returns an error for a negative rate and then
// changes nothing. A rate of 0 selects the rate at the newest request,
// which is the mean rate since Reset. Reset selects 0. The saved state does
// not keep the rate, so the session sets it again after a restore.
func (s *Simulation) SetDemandRate(perMinute int) error {
	if perMinute < 0 {
		return fmt.Errorf("negative demand rate %d", perMinute)
	}
	s.demandRate = perMinute
	return nil
}

// DemandRate returns the demand rate that SetDemandRate set.
func (s *Simulation) DemandRate() int { return s.demandRate }

// setPositioning selects a valid positioning mode. A mode other than
// PositioningOff moves the next check up to the current tick.
func (s *Simulation) setPositioning(mode Positioning) {
	s.positioning = mode
	if mode != PositioningOff && s.nextRedistributionTick < s.tick {
		s.nextRedistributionTick = s.tick
	}
}

// guardedGate is the state of the guarded positioning gate at one tick.
// The gate reads only saved state and the settings that the session sets,
// so a restore gives the same gate.
type guardedGate struct {
	// count and ticks give the request rate that the gate reads, as count
	// requests in ticks. With a demand rate, they are the demand rate and
	// ticksPerMinute. Otherwise, they are the ID and the request tick of
	// the newest request. They are 0 when the gate is not active.
	count int
	ticks int64
	// active is true when a request exists and the rate is less than one
	// request per minute for each guardedFleetRateShare pods. See
	// guardedRateLow.
	active bool
	// open is true when active is true, the newest request is at most
	// guardedLivenessTicks old, the positioning moves are fewer than the
	// boardings, each waiting trip has a pod, and at most the working share
	// of the fleet works.
	open bool
}

// guardedGate returns the gate at the current tick. It checks the
// conditions in order of cost, and it stops at the first one that fails.
// When the session sets a demand rate, the gate reads that rate. Otherwise,
// the rate at the newest request is ID/requested. In compare, request k
// comes at k times the interval, so this is the offered rate from the
// first request. The liveness always reads the newest request.
func (s *Simulation) guardedGate() guardedGate {
	id, requested, ok := s.newestRequest()
	if !ok {
		return guardedGate{}
	}
	count, ticks := id, requested
	if s.demandRate > 0 {
		count, ticks = s.demandRate, ticksPerMinute
	}
	if !guardedRateLow(count, ticks, len(s.vehicles)) {
		return guardedGate{}
	}
	gate := guardedGate{count: count, ticks: ticks, active: true}
	gate.open = s.tick-requested <= guardedLivenessTicks && s.rebalanceMoves < s.boarded &&
		!slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.PodID == "" }) &&
		s.guardedLoadLow()
	return gate
}

// guardedRateLow reports whether count requests in ticks are fewer than one
// request per minute for each guardedFleetRateShare pods of fleet. A tick
// count of 0 or less gives no rate, so the result is then false. A restore
// accepts large IDs and ticks, so the products use 128 bits and cannot
// overflow.
func guardedRateLow(count int, ticks int64, fleet int) bool {
	if count <= 0 || ticks <= 0 || fleet <= 0 {
		return false
	}
	offeredHigh, offeredLow := bits.Mul64(uint64(count), ticksPerMinute*guardedFleetRateShare)
	limitHigh, limitLow := bits.Mul64(uint64(fleet), uint64(ticks))
	return offeredHigh < limitHigh || offeredHigh == limitHigh && offeredLow < limitLow
}

// newestRequest returns the ID and the request tick of the request with the
// highest ID in the waiting trips and in the first riders of the pods. A pod
// keeps its riders after the trip. Only the first rider of a pod counts, so
// a party that joined a shared ride does not change the result. The newest
// request can be missing, for
// example after a shared ride takes it, or when a pod boards an older trip.
// Then the result is the newest request that remains. It reports false
// when no request remains.
func (s *Simulation) newestRequest() (int, int64, bool) {
	id, requested := 0, int64(0)
	for index := range s.waiting {
		if request := &s.waiting[index].request; request.ID > id {
			id, requested = request.ID, request.RequestedTick
		}
	}
	for index := range s.vehicles {
		if riders := s.vehicles[index].Riders; len(riders) > 0 && riders[0].ID > id {
			id, requested = riders[0].ID, riders[0].RequestedTick
		}
	}
	return id, requested, id > 0
}

// guardedLoadLow reports whether at most the working share of the fleet
// works. A pod works when it has a request that is not complete, or when
// a waiting trip names it. See Snapshot.WorkingVehicles.
func (s *Simulation) guardedLoadLow() bool {
	assigned := s.assignedPods()
	working := 0
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.RidersAboard() > 0 || assigned[v.Pod.ID] {
			working++
		}
	}
	return working*guardedWorkingDenominator <= guardedWorkingNumerator*len(s.vehicles)
}

// assignedPods returns the pods that the waiting trips name.
func (s *Simulation) assignedPods() map[string]bool {
	assigned := make(map[string]bool, len(s.waiting))
	for _, trip := range s.waiting {
		if trip.request.PodID != "" {
			assigned[trip.request.PodID] = true
		}
	}
	return assigned
}

// guardedView holds the deficit stations at one open check, and the data
// that the candidate rule needs.
type guardedView struct {
	// deficits are in order of weight, highest first, and then in network
	// order.
	deficits []guardedDeficit
	// weights holds the demand weight of each station, by station index.
	// It is 0 for a parking station.
	weights []float64
	// idle counts the idle pods that no waiting trip names at each
	// station, by station index.
	idle []int
	// assigned holds the pods that the waiting trips name.
	assigned map[string]bool
}

// guardedDeficit is a demand station with no supply.
type guardedDeficit struct {
	station Station
	// index is the index of the station in the network.
	index int
	// berths holds the available berths of the station in berth order. It
	// holds at least two berths, so a move to the first berth leaves one.
	berths []Berth
}

// guardedView returns the view for an open gate.
//
// With no demand weights, each passenger station has a weight of 1. W is
// the sum of the weights of the passenger stations, and N is the number of
// passenger stations with a weight of more than 0. A station is a demand station when it is a passenger
// station with at least two berths, its weight w is more than 0, w*N is at
// least W, and it expects at least half a request in guardedHorizonMinutes
// at the rate of the gate.
//
// A demand station is a deficit station when it has no supply, no waiting
// trip starts there, and it has at least two available berths. A berth is
// available when no pod holds the berth or its node, no pod that is not
// idle goes to it, and no waiting trip goes to it. See guardedSupply for
// the supply.
func (s *Simulation) guardedView(gate guardedGate) guardedView {
	stations := s.network.Stations
	view := guardedView{weights: make([]float64, len(stations)), idle: make([]int, len(stations)), assigned: s.assignedPods()}
	total, weighted := 0.0, 0
	for index, station := range stations {
		if station.ParkingOnly {
			continue
		}
		view.weights[index] = 1
		if s.demandWeights != nil {
			view.weights[index] = s.demandWeights[station.ID]
		}
		if view.weights[index] > 0 {
			total += view.weights[index]
			weighted++
		}
	}
	demand := make([]bool, len(stations))
	for index, station := range stations {
		weight := view.weights[index]
		demand[index] = !station.ParkingOnly && len(station.Berths) >= 2 && weight > 0 &&
			weight*float64(weighted) >= total &&
			weight*2*guardedHorizonMinutes*float64(gate.count)*ticksPerMinute >= total*float64(gate.ticks)
	}
	supplied, busy := s.guardedSupply(guardedSupplyInput{view: &view, demand: demand})
	for index, station := range stations {
		if !demand[index] || supplied[index] {
			continue
		}
		var available []Berth
		for _, berth := range station.Berths {
			if !busy[berth.ID] && s.owners[resource{kind: berthResource, id: berth.ID}] == "" &&
				s.owners[resource{kind: nodeResource, id: berth.Node}] == "" {
				available = append(available, berth)
			}
		}
		if len(available) >= 2 {
			view.deficits = append(view.deficits, guardedDeficit{station: station, index: index, berths: available})
		}
	}
	slices.SortStableFunc(view.deficits, func(a, b guardedDeficit) int {
		return cmp.Compare(view.weights[b.index], view.weights[a.index])
	})
	return view
}

// guardedSupplyInput is the input of guardedSupply.
type guardedSupplyInput struct {
	// view gets the idle counts. It must hold the pods that the waiting
	// trips name.
	view *guardedView
	// demand is true for each demand station, by station index.
	demand []bool
}

// guardedSupply returns, by station index, the demand stations with supply
// or with a waiting trip that starts there. It also returns the berths
// that a pod that is not idle or a waiting trip goes to, and the berths
// that the route of a pod that is not idle enters after its claims. It
// counts the idle pods in the view. A station has supply when it has an idle pod or
// a pod that unloads at its last stop, when an empty pod that no waiting
// trip names goes to it, or when it is the last stop of a pod with
// passengers that becomes available in guardedReachSeconds or less. The estimate is the one of availableAfter,
// but it also counts a stopped pod.
func (s *Simulation) guardedSupply(input guardedSupplyInput) ([]bool, map[string]bool) {
	supplied := make([]bool, len(input.demand))
	busy := make(map[string]bool)
	for _, trip := range s.waiting {
		if index, ok := s.stationIndex(trip.request.From); ok {
			supplied[index] = true
		}
		if trip.destination.ID != "" {
			busy[trip.destination.ID] = true
		}
	}
	var inbound []*vehicle
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Idle {
			if v.destination.ID != "" {
				busy[v.destination.ID] = true
			}
			// A diversion can start inside the berth access of a station, so
			// that the new route crosses a berth that the pod does not claim.
			// A guarded move that claims that berth would stop the pod
			// before the berth, and each pod would wait for the other.
			for resources := range v.blocks.spanResources(v.reservedThrough+1, v.blocks.len()) {
				for _, r := range resources {
					if r.kind == berthResource {
						busy[r.id] = true
					}
				}
			}
		}
		station := ""
		switch {
		case v.Pod.Activity == Idle && !input.view.assigned[v.Pod.ID]:
			station = v.Pod.StationID
			if index, ok := s.stationIndex(station); ok {
				input.view.idle[index]++
			}
		case v.Pod.Activity == Unloading && len(v.Stops) == 0:
			station = v.Pod.StationID
		case !v.Pod.Occupied && v.RelocatingTo != "" && !input.view.assigned[v.Pod.ID]:
			station = v.RelocatingTo
		case v.Pod.Occupied && (v.Pod.Activity == Traveling || v.Pod.Activity == Continuing || v.Pod.Activity == Unloading):
			inbound = append(inbound, v)
		}
		if index, ok := s.stationIndex(station); ok {
			supplied[index] = true
		}
	}
	for _, v := range inbound {
		index, ok := s.stationIndex(v.lastStop())
		if !ok || !input.demand[index] || supplied[index] {
			continue
		}
		if _, seconds, ok := s.finishEstimate(v); ok && seconds <= guardedReachSeconds {
			supplied[index] = true
		}
	}
	return supplied, busy
}

// stationIndex returns the index of a station in the network. It reports
// false at once for an empty ID.
func (s *Simulation) stationIndex(id string) (int, bool) {
	if id == "" {
		return -1, false
	}
	if index, ok := s.stationIndexes[id]; ok && index < len(s.network.Stations) && s.network.Stations[index].ID == id {
		return index, true
	}
	index := slices.IndexFunc(s.network.Stations, func(station Station) bool { return station.ID == id })
	return index, index >= 0
}

// positionGuarded moves one idle empty pod to a deficit station when the
// gate is open. It checks every guardedCheckTicks. It tries the first
// guardedDeficitTries deficit stations in order. For each one, it looks
// for the candidate pod with the lowest route cost to the first available
// berth, up to guardedReachSeconds. The lower fleet index wins a tie. The
// first pod that it finds reserves the berth and goes there as a
// rebalancing move.
func (s *Simulation) positionGuarded() {
	if s.tick < s.nextRedistributionTick {
		return
	}
	s.nextRedistributionTick = s.tick + guardedCheckTicks
	gate := s.guardedGate()
	if !gate.open {
		return
	}
	s.ensureNetworkIndexes()
	view := s.guardedView(gate)
	if len(view.deficits) == 0 {
		return
	}
	rank, ok := s.guardedCandidates(view)
	if !ok {
		return
	}
	for _, deficit := range view.deficits[:min(len(view.deficits), guardedDeficitTries)] {
		target := deficit.berths[0]
		node, found := s.preferredFleetSource(preferredNearestInput{
			from: target.Node, rank: rank, limit: guardedReachSeconds, reverse: true,
		})
		if !found {
			continue
		}
		v := &s.vehicles[rank[node]]
		if s.startEmptyMove(v, emptyDestination{station: deficit.station.ID, berth: target, reserveBerth: true, rebalance: true}) != nil {
			continue
		}
		s.rebalanceMoves++
		return
	}
}

// guardedCandidates returns the fleet index of each candidate pod at the
// node of its berth, and -1 at each other node. A candidate is an idle
// empty pod that no waiting trip names and that has no rebalance
// cooldown. It must be at a parking station, at a station with a weight of
// 0, or at a station with at least two idle pods that no waiting trip
// names. Thus a lone idle pod at a weighted station never moves. It
// reports false when no pod is a candidate.
func (s *Simulation) guardedCandidates(view guardedView) ([]int, bool) {
	rank := make([]int, len(s.network.Nodes))
	for i := range rank {
		rank[i] = -1
	}
	found := false
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Idle || v.Pod.Occupied || v.Pod.BerthID == "" || view.assigned[v.Pod.ID] || v.rebalanceAfter > s.tick {
			continue
		}
		index, ok := s.stationIndex(v.Pod.StationID)
		if !ok {
			continue
		}
		station := s.network.Stations[index]
		if !station.ParkingOnly && view.weights[index] > 0 && view.idle[index] < 2 {
			continue
		}
		berth, ok := station.berth(v.Pod.BerthID)
		if !ok {
			continue
		}
		if node, ok := s.graph.nodes[berth.Node]; ok {
			rank[node], found = i, true
		}
	}
	return rank, found
}

// guardedClear moves an idle empty pod that blocks a berth, when the gate
// is active. It reports false when the gate is not active or when it finds
// no berth. Then the pod does not move, and the caller uses the rules of
// off mode.
//
// When the gate is open, the pod goes to the available berth of a deficit
// station with the lowest route cost, up to guardedReachSeconds.
// The move reserves the berth and is a rebalancing move, but it does not
// count as a positioning move. Otherwise, the pod goes to the free parking
// berth with the lowest route cost. Between parking berths with the same
// cost, the order of park wins.
func (s *Simulation) guardedClear(blocker *vehicle) bool {
	gate := s.guardedGate()
	if !gate.active {
		return false
	}
	s.ensureNetworkIndexes()
	from, _ := s.station(blocker.Pod.StationID)
	origin, _ := from.berth(blocker.Pod.BerthID)
	if gate.open && s.guardedBumpToDeficit(guardedBump{blocker: blocker, from: origin.Node, gate: gate}) {
		return true
	}
	return s.guardedBumpToParking(guardedBump{blocker: blocker, from: origin.Node})
}

// guardedBump is the input of a guarded bump.
type guardedBump struct {
	blocker *vehicle
	// from is the node of the berth of the blocker.
	from string
	gate guardedGate
}

// bumpGoal is a berth that a bumped pod can go to.
type bumpGoal struct {
	station string
	berth   Berth
}

// guardedBumpToDeficit moves the blocker to the available berth of a
// deficit station with the lowest route cost, up to guardedReachSeconds.
// Between berths with the same cost, the deficit order wins, then the
// berth order. The blocker is an idle pod that no waiting trip names, so
// its station has supply and is not a deficit station.
func (s *Simulation) guardedBumpToDeficit(bump guardedBump) bool {
	view := s.guardedView(bump.gate)
	rank := make([]int, len(s.network.Nodes))
	for i := range rank {
		rank[i] = -1
	}
	var goals []bumpGoal
	for _, deficit := range view.deficits {
		for _, berth := range deficit.berths {
			if node, ok := s.graph.nodes[berth.Node]; ok && rank[node] < 0 {
				rank[node] = len(goals)
				goals = append(goals, bumpGoal{station: deficit.station.ID, berth: berth})
			}
		}
	}
	if len(goals) == 0 {
		return false
	}
	node, ok := s.network.preferredNearestIndexed(preferredNearestInput{
		from: bump.from, rank: rank, class: bump.blocker.Pod.Class, limit: guardedReachSeconds,
	}, s.graph)
	if !ok {
		return false
	}
	goal := goals[rank[node]]
	return s.startEmptyMove(bump.blocker, emptyDestination{station: goal.station, berth: goal.berth, reserveBerth: true, rebalance: true}) == nil
}

// guardedBumpToParking moves the blocker to the parking berth with the
// lowest route cost whose berth and node no pod holds.
func (s *Simulation) guardedBumpToParking(bump guardedBump) bool {
	rank := make([]int, len(s.network.Nodes))
	for i := range rank {
		rank[i] = -1
	}
	var goals []bumpGoal
	for _, station := range s.network.Stations {
		if !station.ParkingOnly {
			continue
		}
		for _, berth := range station.Berths {
			if s.owners[resource{kind: berthResource, id: berth.ID}] != "" || s.owners[resource{kind: nodeResource, id: berth.Node}] != "" {
				continue
			}
			if node, ok := s.graph.nodes[berth.Node]; ok && rank[node] < 0 {
				rank[node] = len(goals)
				goals = append(goals, bumpGoal{station: station.ID, berth: berth})
			}
		}
	}
	if len(goals) == 0 {
		return false
	}
	node, ok := s.network.preferredNearestIndexed(preferredNearestInput{from: bump.from, rank: rank, class: bump.blocker.Pod.Class}, s.graph)
	if !ok {
		return false
	}
	goal := goals[rank[node]]
	return s.startEmptyMove(bump.blocker, emptyDestination{station: goal.station, berth: goal.berth, reserveBerth: true}) == nil
}
