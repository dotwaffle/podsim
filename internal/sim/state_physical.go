package sim

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

const (
	// restoreTolerance is the distance in meters within which the restore
	// takes two saved positions as one.
	restoreTolerance = 1e-6
	// The block budget of a restore is budgetNetworkMultiple times the blocks
	// of the whole network plus budgetLaneBlocks for each lane. Each block of
	// a route counts, and each lane of a waiting-trip route counts as one.
	budgetNetworkMultiple = 32
	budgetLaneBlocks      = 4
)

// berthRef is a berth and the ID of its station.
type berthRef struct {
	station string
	berth   Berth
}

// physicalRestore holds the work state of restorePhysical. Each slice that is
// indexed by pod follows the saved pod order, which is also the order of
// s.vehicles.
type physicalRestore struct {
	s     *Simulation
	state SavedState
	// gap is the order gap of the saved state.
	gap    int
	result RestoreResult
	berths map[string]berthRef
	// demoted marks the pods that go to a berth when the traveling pods are
	// in place.
	demoted []bool
	// routes holds each saved pod route that the restore can build, and costs
	// holds its blocks.
	routes [][]int
	costs  []int
	// tripRoutes holds each saved trip route that the restore can build.
	// unbound marks the trips whose bindings are not valid.
	tripRoutes [][]int
	unbound    []bool
	// laneBlocks holds the blocks of each network lane. cost counts the
	// blocks that the restore builds and must stay at or below budget.
	laneBlocks   []int
	cost, budget int
	requeued     []waitingTrip
}

// restorePhysical rebuilds a running simulation with each pod where the saved
// state puts it, at rest. It derives the reservations again from the saved
// positions. A traveling pod that cannot keep its place moves to a berth,
// which is a demotion. restorePhysical fails when the saved state is not
// valid, when two pods at berths conflict, or when a demoted pod finds no
// free berth.
func restorePhysical(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	gap, err := validateSavedState(input.State)
	if err != nil {
		return nil, RestoreResult{}, err
	}
	s, err := NewFleet(input.Network, input.Fleet)
	if err != nil {
		return nil, RestoreResult{}, fmt.Errorf("create the fleet: %w", err)
	}
	if err := checkSavedPodIDs(s.initial, input.State); err != nil {
		return nil, RestoreResult{}, err
	}
	if err := checkFleetSaved(s.initial, input.State); err != nil {
		return nil, RestoreResult{}, err
	}
	r := newPhysicalRestore(s, input.State, gap)
	r.restoreCounters()
	if err := r.decodePods(); err != nil {
		return nil, RestoreResult{}, err
	}
	// The saved pods can differ from the fleet that NewFleet indexed.
	s.vehicleIndexes = indexVehicles(s.vehicles)
	if err := r.checkRoutes(); err != nil {
		return nil, RestoreResult{}, err
	}
	r.limitWork()
	if err := r.buildRoutes(); err != nil {
		return nil, RestoreResult{}, err
	}
	if err := r.claimBerths(); err != nil {
		return nil, RestoreResult{}, err
	}
	r.placeTraveling()
	r.claimDestinations()
	if err := r.separate(); err != nil {
		return nil, RestoreResult{}, err
	}
	for index, demoted := range r.demoted {
		if !demoted {
			continue
		}
		if err := r.placeDemoted(index); err != nil {
			return nil, RestoreResult{}, err
		}
		r.result.Demoted = append(r.result.Demoted, s.vehicles[index].Pod.ID)
	}
	r.restoreWaiting()
	if err := s.verifyRestore(r.gap + r.result.DroppedParties); err != nil {
		return nil, RestoreResult{}, err
	}
	r.result.Tier = RestorePhysical
	return s, r.result, nil
}

// validateSavedState checks the rules that do not need the network. It
// returns the order gap of the saved state.
func validateSavedState(state SavedState) (int, error) {
	if len(state.Pods) == 0 || len(state.Pods) > maxSavedPods {
		return 0, fmt.Errorf("the saved state has %d pods, want 1 to %d", len(state.Pods), maxSavedPods)
	}
	if err := state.validateCounters(); err != nil {
		return 0, err
	}
	active := make(map[int]bool)
	for index, pod := range state.Pods {
		if pod.ID == "" || index > 0 && pod.ID <= state.Pods[index-1].ID {
			return 0, fmt.Errorf("saved pod %q is empty or out of order", pod.ID)
		}
		if pod.Parties < 0 || pod.Parties > MaxSharedRideParties {
			return 0, fmt.Errorf("pod %s has %d parties", pod.ID, pod.Parties)
		}
		if pod.Request != nil && !state.validRequest(*pod.Request) {
			return 0, fmt.Errorf("pod %s has a request that is not valid", pod.ID)
		}
		if !pod.carriesPassengers() {
			continue
		}
		if pod.Parties < 1 || active[pod.Request.ID] {
			return 0, fmt.Errorf("pod %s carries request %d with %d parties", pod.ID, pod.Request.ID, pod.Parties)
		}
		active[pod.Request.ID] = true
	}
	for _, trip := range state.Waiting {
		if trip.Parties < 0 || trip.Parties > MaxSharedRideParties {
			return 0, fmt.Errorf("queued request %d has %d parties", trip.Request.ID, trip.Parties)
		}
	}
	gap := state.ordersGap()
	if gap < 0 {
		return 0, fmt.Errorf("the saved state holds %d more parties than it submitted", -gap)
	}
	return gap, nil
}

func (state SavedState) validateCounters() error {
	counters := []int64{
		state.Tick, int64(state.Completed), int64(state.RequestID), int64(state.Boarded), state.TotalWaitTicks,
		state.MaxWaitTicks, state.NextRedistributionTick, int64(state.RebalanceMoves), int64(state.SharedParties),
	}
	distances := []float64{state.PassengerDistanceMeters, state.EmptyDistanceMeters}
	switch {
	case slices.ContainsFunc(counters, func(counter int64) bool { return counter < 0 }),
		slices.ContainsFunc(distances, func(distance float64) bool { return !finite(distance) || distance < 0 }):
		return errors.New("a saved counter is negative or not finite")
	case state.Completed > state.RequestID || state.Boarded > state.RequestID:
		return errors.New("the saved state completed or boarded more orders than it submitted")
	case state.SharedRidePartyLimit < 1 || state.SharedRidePartyLimit > MaxSharedRideParties:
		return fmt.Errorf("shared ride party limit %d is out of range", state.SharedRidePartyLimit)
	case len(state.DemoError) > maxSavedText:
		return errors.New("the saved demo error is too long")
	default:
		return nil
	}
}

func (state SavedState) validRequest(request SavedRequest) bool {
	return request.ID >= 1 && request.ID <= state.RequestID && request.PartySize >= 1 &&
		request.RequestedTick >= 0 && request.RequestedTick <= state.Tick && len(request.DispatchReason) <= maxSavedText
}

// checkSavedPodIDs checks that each saved pod is a fleet pod. A demo fleet
// can also have the parked demo pods, because they stay after the demo ends.
func checkSavedPodIDs(fleet []Placement, state SavedState) error {
	demoFleet := isDemoFleet(fleet)
	if state.Demo != nil && !demoFleet {
		return errors.New("the saved traffic demo needs the demo fleet")
	}
	known := make(map[string]bool, len(fleet)+len(demoParkedPods))
	for _, placement := range fleet {
		known[placement.ID] = true
	}
	if demoFleet {
		for _, id := range demoParkedPods {
			known[id] = true
		}
	}
	for _, pod := range state.Pods {
		if !known[pod.ID] {
			return fmt.Errorf("saved pod %s is not in the fleet", pod.ID)
		}
	}
	return nil
}

// checkFleetSaved checks that the saved state has each fleet pod. Only the
// physical tier needs this, because the logical tier puts each fleet pod at
// its initial berth.
func checkFleetSaved(fleet []Placement, state SavedState) error {
	for _, placement := range fleet {
		if !slices.ContainsFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == placement.ID }) {
			return fmt.Errorf("fleet pod %s is not in the saved state", placement.ID)
		}
	}
	return nil
}

func newPhysicalRestore(s *Simulation, state SavedState, gap int) *physicalRestore {
	r := &physicalRestore{
		s: s, state: state, gap: gap, berths: make(map[string]berthRef),
		demoted: make([]bool, len(state.Pods)), routes: make([][]int, len(state.Pods)), costs: make([]int, len(state.Pods)),
		tripRoutes: make([][]int, len(state.Waiting)), unbound: make([]bool, len(state.Waiting)),
		laneBlocks: make([]int, len(s.network.Lanes)),
	}
	networkBlocks := 0
	for index, lane := range s.network.Lanes {
		r.laneBlocks[index] = laneBlockCount(s.laneLength(lane))
		networkBlocks += r.laneBlocks[index]
	}
	r.budget = budgetNetworkMultiple*networkBlocks + budgetLaneBlocks*len(s.network.Lanes)
	for _, station := range s.network.Stations {
		for _, berth := range station.Berths {
			r.berths[berth.ID] = berthRef{station: station.ID, berth: berth}
		}
	}
	return r
}

func (r *physicalRestore) restoreCounters() {
	s, state := r.s, r.state
	s.setSavedCounters(state)
	s.demoError = state.DemoError
	if state.Demo != nil {
		s.demo = &demoRun{secondSent: state.Demo.SecondSent, followupsSent: state.Demo.FollowupsSent}
	}
	s.owners = make(map[resource]string)
	s.vehicles = make([]vehicle, len(state.Pods))
}

// setSavedCounters sets the clock, the paused flag, the counters and the
// shared ride party limit of a saved state. Both restore tiers keep them.
func (s *Simulation) setSavedCounters(state SavedState) {
	s.tick, s.paused, s.completed, s.requestID = state.Tick, state.Paused, state.Completed, state.RequestID
	s.boarded, s.totalWaitTicks, s.maxWaitTicks = state.Boarded, state.TotalWaitTicks, state.MaxWaitTicks
	s.nextRedistributionTick = state.NextRedistributionTick
	s.passengerDistanceMeters, s.emptyDistanceMeters = state.PassengerDistanceMeters, state.EmptyDistanceMeters
	s.rebalanceMoves, s.sharedParties = state.RebalanceMoves, state.SharedParties
	s.sharedRidePartyLimit = state.SharedRidePartyLimit
}

// decodePods fills a vehicle for each saved pod. It fails for a pod that Step
// cannot run and for a pod at a berth with a value out of range. It demotes a
// traveling pod with a value out of range.
func (r *physicalRestore) decodePods() error {
	for index, saved := range r.state.Pods {
		if err := r.decodePod(index, saved); err != nil {
			return fmt.Errorf("pod %s: %w", saved.ID, err)
		}
	}
	return nil
}

func (r *physicalRestore) decodePod(index int, saved SavedPod) error {
	activity, ok := activityOfCode(saved.Activity)
	if !ok {
		return fmt.Errorf("unknown activity %q", saved.Activity)
	}
	v := &r.s.vehicles[index]
	*v = vehicle{
		Pod:     Pod{ID: saved.ID, Activity: activity, Occupied: saved.Occupied},
		Parties: saved.Parties, RelocatingTo: saved.RelocatingTo, Rebalancing: saved.Rebalancing,
		phaseTicks: saved.PhaseTicks, rebalanceAfter: saved.RebalanceAfter, destinationStation: saved.DestinationStation,
		pending: -1, reservedThrough: -1,
	}
	if saved.Request != nil {
		v.Request = new(Request(*saved.Request))
	}
	if err := r.checkPassengers(v); err != nil {
		return err
	}
	origin, originOK := r.berths[saved.Origin]
	destination, destinationOK := r.berths[saved.Destination]
	v.origin, v.destination = origin.berth, destination.berth
	valid := (originOK || saved.Origin == "") && (destinationOK || saved.Destination == "") &&
		saved.WaitSince >= 0 && saved.WaitSince <= r.state.Tick
	if activity == Traveling {
		if !valid || saved.PhaseTicks != 0 || !finite(saved.LaneDistance) || !finite(saved.Distance) || saved.Distance < 0 {
			r.demote(index)
		}
		return nil
	}
	berth, ok := r.berths[saved.BerthID]
	if !ok || berth.station != saved.StationID {
		return fmt.Errorf("unknown berth %q at station %q", saved.BerthID, saved.StationID)
	}
	if !valid || saved.PhaseTicks < 0 || saved.PhaseTicks > maxPhaseTicks(activity) {
		return errors.New("a value is out of range")
	}
	v.Pod.StationID, v.Pod.BerthID = berth.station, berth.berth.ID
	return nil
}

// maxPhaseTicks returns the largest phase count of a pod at a berth.
func maxPhaseTicks(activity Activity) int {
	switch activity {
	case Boarding:
		return boardingTicks
	case Unloading:
		return unloadingTicks
	default:
		return 0
	}
}

// checkPassengers fails for a pod whose passengers Step cannot handle. Step
// completes the request of a pod when it unloads, and a pod unloads when it
// arrives without a station to relocate to.
func (r *physicalRestore) checkPassengers(v *vehicle) error {
	activity := v.Pod.Activity
	switch {
	case (activity == Boarding || activity == Unloading || activity == Traveling && v.RelocatingTo == "") && !v.carriesPassengers():
		return errors.New("the pod has no active request")
	case (activity == Idle || activity == DepartingEmpty || v.RelocatingTo != "") && v.Pod.Occupied:
		return errors.New("an empty pod is occupied")
	case activity == DepartingEmpty && v.RelocatingTo == "":
		return errors.New("an empty departure has no station to relocate to")
	case v.carriesPassengers() && (!r.s.passengerStation(v.Request.From) || !r.s.passengerStation(v.Request.To)):
		return errors.New("the request does not join two passenger stations")
	default:
		return nil
	}
}

func (s *Simulation) passengerStation(id string) bool {
	station, ok := s.station(id)
	return ok && !station.ParkingOnly
}

// routed reports whether a pod at an activity uses its route.
func routed(activity Activity) bool {
	return activity == Boarding || activity == DepartingEmpty || activity == Traveling
}

// checkRoutes checks the length and the lane indexes of each saved route before
// the restore builds a route. ExportState leaves out only a route that is
// longer than its limit, so a missing pod route counts as one that is too
// long.
func (r *physicalRestore) checkRoutes() error {
	limits := newRouteLimits(r.s.network)
	for index, saved := range r.state.Pods {
		v := &r.s.vehicles[index]
		if !routed(v.Pod.Activity) || r.demoted[index] {
			continue
		}
		switch {
		case len(saved.Route) == 0 || len(saved.Route) > limits.pod:
			r.result.OverCap++
			r.demote(index)
		case !r.knownLanes(saved.Route):
			if v.Pod.Activity != Traveling {
				return fmt.Errorf("pod %s: the route has an unknown lane", v.Pod.ID)
			}
			r.demote(index)
		default:
			r.routes[index] = saved.Route
			for _, lane := range saved.Route {
				r.costs[index] += r.laneBlocks[lane]
			}
			r.cost += r.costs[index]
		}
	}
	for index, trip := range r.state.Waiting {
		switch {
		case len(trip.Route) == 0:
		case len(trip.Route) > limits.trip:
			r.result.OverCap++
			r.unbound[index] = true
		case !r.knownLanes(trip.Route):
			r.unbound[index] = true
		default:
			r.tripRoutes[index] = trip.Route
			r.cost += len(trip.Route)
		}
	}
	return nil
}

func (r *physicalRestore) knownLanes(route []int) bool {
	return !slices.ContainsFunc(route, func(lane int) bool { return lane < 0 || lane >= len(r.s.network.Lanes) })
}

// limitWork keeps the blocks that the restore builds within the budget. It
// demotes the pods with the most route blocks first, and then drops the
// longest waiting-trip routes, until the cost fits.
func (r *physicalRestore) limitWork() {
	if r.cost <= r.budget {
		return
	}
	pods := make([]int, 0, len(r.costs))
	for index, cost := range r.costs {
		if cost > 0 {
			pods = append(pods, index)
		}
	}
	slices.SortFunc(pods, func(a, b int) int {
		return cmp.Or(cmp.Compare(r.costs[b], r.costs[a]), cmp.Compare(r.state.Pods[a].ID, r.state.Pods[b].ID))
	})
	for _, index := range pods {
		if r.cost <= r.budget {
			return
		}
		r.demote(index)
		r.result.OverBudget++
	}
	trips := make([]int, 0, len(r.tripRoutes))
	for index, route := range r.tripRoutes {
		if route != nil {
			trips = append(trips, index)
		}
	}
	slices.SortStableFunc(trips, func(a, b int) int { return cmp.Compare(len(r.tripRoutes[b]), len(r.tripRoutes[a])) })
	for _, index := range trips {
		if r.cost <= r.budget {
			return
		}
		r.cost -= len(r.tripRoutes[index])
		r.tripRoutes[index] = nil
		r.result.OverBudget++
	}
}

// demote marks a pod for a berth. It drops the route of the pod and releases
// each resource that the pod holds.
func (r *physicalRestore) demote(index int) {
	v := &r.s.vehicles[index]
	r.demoted[index] = true
	r.cost -= r.costs[index]
	r.costs[index], r.routes[index] = 0, nil
	v.Route, v.blocks, v.blockStarts = nil, nil, nil
	maps.DeleteFunc(r.s.owners, func(_ resource, owner string) bool { return owner == v.Pod.ID })
	clear(v.routeReleases)
}

// buildRoutes builds the route and the blocks of each pod that keeps its
// route. A route must connect its lanes, start at the berth of a pod at a
// berth, and end at the destination berth, or at the entry of the
// destination station before the pod has a berth there.
func (r *physicalRestore) buildRoutes() error {
	for index, indexes := range r.routes {
		if indexes == nil {
			continue
		}
		v := &r.s.vehicles[index]
		route, ok := r.lanes(indexes)
		if ok {
			ok = r.routeEndsMatch(v, route)
		}
		if !ok {
			if v.Pod.Activity != Traveling {
				return fmt.Errorf("pod %s: the route does not connect the pod to its destination", v.Pod.ID)
			}
			r.demote(index)
			continue
		}
		r.s.setVehicleRoute(v, route)
	}
	return nil
}

// lanes resolves lane indexes that knownLanes accepted. It returns false when
// a lane does not start where the lane before it ends.
func (r *physicalRestore) lanes(indexes []int) ([]Lane, bool) {
	route := make([]Lane, len(indexes))
	for position, index := range indexes {
		route[position] = r.s.network.Lanes[index]
		if position > 0 && route[position-1].To != route[position].From {
			return nil, false
		}
	}
	return route, true
}

func (r *physicalRestore) routeEndsMatch(v *vehicle, route []Lane) bool {
	if v.RelocatingTo != "" && v.RelocatingTo != v.destinationStation {
		return false
	}
	if v.Pod.Activity != Traveling {
		berth := r.berths[v.Pod.BerthID].berth
		if v.origin.ID != berth.ID || route[0].From != berth.Node {
			return false
		}
	}
	end := route[len(route)-1].To
	if v.destination.ID != "" {
		return r.berths[v.destination.ID].station == v.destinationStation && end == v.destination.Node
	}
	station, ok := r.s.station(v.destinationStation)
	return ok && end == station.Entry
}

func berthResources(berth Berth) [2]resource {
	return [2]resource{{kind: berthResource, id: berth.ID}, {kind: nodeResource, id: berth.Node}}
}

// claimBerths gives each pod at a berth its berth and the berth node. A pod
// that is ready to depart keeps its wait for admission.
func (r *physicalRestore) claimBerths() error {
	for index, saved := range r.state.Pods {
		v := &r.s.vehicles[index]
		if v.Pod.Activity == Traveling {
			continue
		}
		berth := r.berths[v.Pod.BerthID].berth
		for _, claimed := range berthResources(berth) {
			if owner := r.s.owners[claimed]; owner != "" {
				return fmt.Errorf("pods %s and %s are at berth %s", owner, v.Pod.ID, berth.ID)
			}
			r.s.owners[claimed] = v.Pod.ID
		}
		node, _ := r.s.network.Node(berth.Node)
		v.Pod.Position = node.Position
		ready := (v.Pod.Activity == Boarding || v.Pod.Activity == DepartingEmpty) && v.phaseTicks == 0
		if saved.Waiting && ready && !r.demoted[index] {
			v.pending, v.waitSince = 0, saved.WaitSince
		}
	}
	return nil
}

// placeTraveling puts each traveling pod on its route in saved order. It
// demotes a pod whose values are not valid or whose resources another pod
// holds.
func (r *physicalRestore) placeTraveling() {
	for index, saved := range r.state.Pods {
		v := &r.s.vehicles[index]
		if v.Pod.Activity == Traveling && !r.demoted[index] && !r.placeTravelingPod(v, saved) {
			r.demote(index)
		}
	}
}

// placeTravelingPod derives the reservations of a traveling pod from its
// saved position and claims them. It returns false when a saved value is not
// valid or when another pod holds a resource that the pod needs.
func (r *physicalRestore) placeTravelingPod(v *vehicle, saved SavedPod) bool {
	if saved.RouteIndex < 0 || saved.RouteIndex >= len(v.Route) {
		return false
	}
	lane := v.Route[saved.RouteIndex]
	if saved.LaneID != "" && saved.LaneID != lane.ID {
		return false
	}
	first, last := routeLaneBlocks(v.blocks, saved.RouteIndex)
	laneDistance := min(max(saved.LaneDistance, -restoreTolerance), r.s.laneLength(lane)+restoreTolerance)
	distance := restoredDistance(restoredDistanceInput{
		blocks: v.blocks, first: first, last: last, laneDistance: laneDistance, saved: saved.Distance,
	})
	// The saved route index picks the lane, also at an exact lane boundary.
	index := slices.IndexFunc(v.blocks[first:last+1], func(b block) bool { return b.end >= distance })
	if index < 0 {
		index = last
	} else {
		index += first
	}
	through := reservationEnd(v.blocks, index)
	if distance < Clearance && (v.origin.ID == "" || v.Route[0].From != v.origin.Node) {
		return false
	}
	// A pod that has no berth yet chooses one before it reserves the last lane.
	if lastLane, _ := routeLaneBlocks(v.blocks, len(v.Route)-1); v.destination.ID == "" && through >= lastLane {
		return false
	}
	footprint := v.footprint(through, distance)
	if slices.ContainsFunc(footprint, func(claimed resource) bool { return r.s.owners[claimed] != "" }) {
		return false
	}
	for _, claimed := range footprint {
		r.s.owners[claimed] = v.Pod.ID
	}
	for _, b := range v.blocks[:through+1] {
		for _, claimed := range b.resources {
			if release := resourceReleaseDistance(b, claimed); release > distance {
				v.retainRouteResource(claimed, release)
			}
		}
	}
	v.distance, v.blockIndex, v.reservedThrough = distance, index, through
	v.originReleased = distance >= Clearance
	v.Pod.LaneDistance = laneDistance
	v.Pod.Position = r.s.position(lane, laneDistance)
	if saved.LaneID != "" {
		v.Pod.LaneID = lane.ID
	}
	if saved.Waiting && through+1 < len(v.blocks) {
		v.pending, v.waitSince = through+1, saved.WaitSince
	}
	return true
}

type restoredDistanceInput struct {
	blocks       []block
	first, last  int
	laneDistance float64
	saved        float64
}

// restoredDistance returns the route distance of a pod at laneDistance in the
// lane of blocks first to last. The saved route can start at a later lane
// than the live route, so its sums of lane lengths can differ in the last
// bits. The saved distance and a block end within restoreTolerance therefore
// win. A pod that stopped at a block end then releases the same resources as
// the live pod.
func restoredDistance(input restoredDistanceInput) float64 {
	distance := input.blocks[input.first].laneStart + input.laneDistance
	if math.Abs(distance-input.saved) <= restoreTolerance {
		distance = input.saved
	}
	for _, b := range input.blocks[max(0, input.first-1) : input.last+1] {
		if math.Abs(b.end-distance) <= restoreTolerance {
			return b.end
		}
	}
	return distance
}

// routeLaneBlocks returns the first and the last block of the lane at a route
// index. It counts lane starts, because a route can hold a lane more than
// once.
func routeLaneBlocks(blocks []block, routeIndex int) (first, last int) {
	lane, first := 0, -1
	for index, b := range blocks {
		if index > 0 && b.cell == 0 {
			lane++
		}
		if lane > routeIndex {
			return first, index - 1
		}
		if lane == routeIndex && first < 0 {
			first = index
		}
	}
	return first, len(blocks) - 1
}

// footprint returns the resources that a traveling pod holds when it has
// reserved blocks 0 to through and is at a route distance. These are the
// resources of those blocks that the pod has not passed by their release
// distance. Before the pod is Clearance from its origin, it also holds the
// origin berth and node.
func (v *vehicle) footprint(through int, distance float64) []resource {
	var held []resource
	add := func(claimed resource) {
		if !slices.Contains(held, claimed) {
			held = append(held, claimed)
		}
	}
	for _, b := range v.blocks[:through+1] {
		for _, claimed := range b.resources {
			if resourceReleaseDistance(b, claimed) > distance {
				add(claimed)
			}
		}
	}
	if distance < Clearance {
		for _, claimed := range berthResources(v.origin) {
			add(claimed)
		}
	}
	return held
}

// claimDestinations gives each relocating pod the destination claim that it
// held, when no other pod holds the berth or its node. Route admission still
// protects a berth without a claim.
func (r *physicalRestore) claimDestinations() {
	for index, saved := range r.state.Pods {
		v := &r.s.vehicles[index]
		if r.demoted[index] || !saved.ClaimsDestination || v.RelocatingTo == "" || v.destination.ID == "" {
			continue
		}
		claims := berthResources(v.destination)
		if slices.ContainsFunc(claims[:], func(claimed resource) bool {
			owner := r.s.owners[claimed]
			return owner != "" && owner != v.Pod.ID
		}) {
			continue
		}
		for _, claimed := range claims {
			r.s.owners[claimed] = v.Pod.ID
		}
	}
}

// separate checks the pods in place for separation and berth use. In each
// pair that is too close, it demotes the traveling pod with the later ID. It
// fails when neither pod of the pair is traveling.
func (r *physicalRestore) separate() error {
	for {
		observation := r.s.SafetyObservation()
		pods := observation.Pods[:0]
		for index, pod := range observation.Pods {
			if !r.demoted[index] || pod.Activity != Traveling {
				pods = append(pods, pod)
			}
		}
		observation.Pods = pods
		_, err := observation.Check()
		if err == nil {
			return nil
		}
		separation, ok := errors.AsType[*SeparationError](err)
		if !ok {
			return fmt.Errorf("check the restored pods: %w", err)
		}
		index := -1
		for candidate := range r.s.vehicles {
			v := &r.s.vehicles[candidate]
			if v.Pod.Activity == Traveling && (v.Pod.ID == separation.First || v.Pod.ID == separation.Second) {
				index = candidate
			}
		}
		if index < 0 {
			return fmt.Errorf("check the restored pods at berths: %w", err)
		}
		r.demote(index)
	}
}

// placeDemoted moves a demoted pod to a free berth. A berth that the pod
// holds counts as free. A pod with passengers boards again at a berth of
// their origin with a route as in board. When no such berth is free, or when
// the route does not fit in the block budget, the request goes back to the
// queue with its party count and the pod waits empty. An empty pod goes to
// the first free berth in this order: its destination, its origin, a berth
// of its destination station, then any berth.
func (r *physicalRestore) placeDemoted(index int) error {
	v := &r.s.vehicles[index]
	if v.carriesPassengers() {
		from, _ := r.s.station(v.Request.From)
		if berth, ok := r.freeBerth(v, from.Berths); ok && r.boardAgain(v, berth) {
			return nil
		}
		r.requeue(v)
	}
	candidates := []Berth{v.destination, v.origin}
	for _, stationID := range []string{v.destinationStation, v.RelocatingTo} {
		if station, ok := r.s.station(stationID); ok {
			candidates = append(candidates, station.Berths...)
		}
	}
	for _, station := range r.s.network.Stations {
		candidates = append(candidates, station.Berths...)
	}
	berth, ok := r.freeBerth(v, candidates)
	if !ok {
		return fmt.Errorf("no berth is free for pod %s", v.Pod.ID)
	}
	r.moveTo(v, berth)
	station := r.berths[berth.ID].station
	v.Pod.Activity, v.Pod.StationID = Idle, station
	v.Route, v.blocks, v.blockStarts = nil, nil, nil
	v.Parties, v.RelocatingTo, v.Rebalancing = 0, "", false
	v.origin, v.destination, v.destinationStation = Berth{}, berth, station
	return nil
}

func (r *physicalRestore) freeBerth(v *vehicle, candidates []Berth) (Berth, bool) {
	for _, berth := range candidates {
		if berth.ID == "" {
			continue
		}
		claims := berthResources(berth)
		if !slices.ContainsFunc(claims[:], func(claimed resource) bool {
			owner := r.s.owners[claimed]
			return owner != "" && owner != v.Pod.ID
		}) {
			return berth, true
		}
	}
	return Berth{}, false
}

// boardAgain puts a pod with passengers at a berth of their origin, ready to
// depart. It returns false when the route does not exist or does not fit in
// the block budget.
func (r *physicalRestore) boardAgain(v *vehicle, berth Berth) bool {
	route, err := r.s.stationApproachRoute(berth.Node, v.Request.To)
	if err != nil {
		return false
	}
	cost := 0
	for _, lane := range route {
		cost += laneBlockCount(r.s.laneLength(lane))
	}
	if r.cost+cost > r.budget {
		r.result.OverBudget++
		return false
	}
	r.cost += cost
	r.moveTo(v, berth)
	request := *v.Request
	request.PodID, request.DispatchReason = v.Pod.ID, ""
	v.Request = &request
	v.Pod.Activity, v.Pod.StationID = Boarding, r.berths[berth.ID].station
	v.RelocatingTo, v.Rebalancing = "", false
	v.origin, v.destination, v.destinationStation = berth, Berth{}, request.To
	r.s.setVehicleRoute(v, route)
	return true
}

// requeue returns the request of a pod to the queue with its party count. The
// boarding of the parties stays recorded.
func (r *physicalRestore) requeue(v *vehicle) {
	r.requeued = append(r.requeued, requeuedTrip(*v.Request, v.Parties))
	r.result.Requeued = append(r.result.Requeued, v.Request.ID)
	v.Request = nil
}

// requeuedTrip returns the queued trip for the request of a pod that carried
// parties. The trip keeps the party count, so board and joinSharedRide do not
// record the boarding again.
func requeuedTrip(request Request, parties int) waitingTrip {
	request.PodID, request.DispatchReason = "", ""
	return waitingTrip{request: request, parties: max(1, parties)}
}

// moveTo puts a pod at rest at a berth. The pod releases each other resource.
func (r *physicalRestore) moveTo(v *vehicle, berth Berth) {
	maps.DeleteFunc(r.s.owners, func(_ resource, owner string) bool { return owner == v.Pod.ID })
	clear(v.routeReleases)
	for _, claimed := range berthResources(berth) {
		r.s.owners[claimed] = v.Pod.ID
	}
	node, _ := r.s.network.Node(berth.Node)
	v.Pod = Pod{ID: v.Pod.ID, Position: node.Position, BerthID: berth.ID}
	v.phaseTicks, v.blockIndex, v.reservedThrough, v.pending = 0, 0, -1, -1
	v.distance, v.originReleased = 0, false
}

// restoreWaiting rebuilds the queue in saved order. A requeued request goes
// in before the first saved trip with a larger ID. The restore drops a saved
// request that is not valid or that a pod already carries. It clears the pod
// bindings of a trip when one of them is not valid, and keeps a deferral
// deadline that is in range.
func (r *physicalRestore) restoreWaiting() {
	s := r.s
	carried := make(map[int]bool)
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.carriesPassengers() {
			carried[v.Request.ID] = true
		}
	}
	requeued := slices.Clone(r.requeued)
	slices.SortStableFunc(requeued, func(a, b waitingTrip) int { return cmp.Compare(a.request.ID, b.request.ID) })
	for _, trip := range requeued {
		carried[trip.request.ID] = true
	}
	for index, saved := range r.state.Waiting {
		for len(requeued) > 0 && requeued[0].request.ID < saved.Request.ID {
			s.waiting = append(s.waiting, requeued[0])
			requeued = requeued[1:]
		}
		request := Request(saved.Request)
		if !r.state.validRequest(saved.Request) || carried[request.ID] ||
			!s.passengerStation(request.From) || !s.passengerStation(request.To) {
			r.result.Dropped = append(r.result.Dropped, request.ID)
			r.result.DroppedParties += max(1, saved.Parties)
			continue
		}
		carried[request.ID] = true
		s.waiting = append(s.waiting, r.restoreTrip(index, request))
	}
	s.waiting = append(s.waiting, requeued...)
}

func (r *physicalRestore) restoreTrip(index int, request Request) waitingTrip {
	saved := r.state.Waiting[index]
	trip := waitingTrip{
		request: request, parties: saved.Parties,
		deferUntil: saved.DeferUntil, deferCheck: saved.DeferCheck, deferPodID: saved.DeferPodID,
	}
	unbound := r.unbound[index] || !r.activePod(request.PodID) || !r.activePod(trip.deferPodID) ||
		trip.deferCheck < 0 || trip.deferCheck > r.s.tick+TicksPerSecond
	if !r.s.deferralInRange(trip.deferUntil) {
		trip.deferUntil, unbound = 0, true
	}
	if indexes := r.tripRoutes[index]; indexes != nil && !unbound {
		route, ok := r.lanes(indexes)
		trip.route, unbound = route, !ok
	}
	if unbound {
		trip.request.PodID, trip.route, trip.deferCheck, trip.deferPodID = "", nil, 0, ""
	}
	return trip
}

// deferralInRange reports whether a saved deferral deadline is one that a
// live simulation can set. The deadline is at most maxDispatchDeferral after
// the current tick.
func (s *Simulation) deferralInRange(until int64) bool {
	return until >= 0 && until <= s.tick+maxDispatchDeferral
}

// activePod reports whether a trip can name a pod. The pod must exist and
// keep its place.
func (r *physicalRestore) activePod(id string) bool {
	if id == "" {
		return true
	}
	index := slices.IndexFunc(r.state.Pods, func(pod SavedPod) bool { return pod.ID == id })
	return index >= 0 && !r.demoted[index]
}

// verifyRestore derives the station phases of a restored simulation and
// checks the result: pod separation and berth use, the retention rules for
// each resource owner, and the order gap, which must be gap.
func (s *Simulation) verifyRestore(gap int) error {
	for index := range s.vehicles {
		s.updateStationPhase(&s.vehicles[index])
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		return fmt.Errorf("check the restored pods: %w", err)
	}
	if !maps.Equal(s.owners, s.retainedOwners()) {
		return errors.New("the resource owners differ from the retention rules")
	}
	if got := s.ordersGap(); got != gap {
		return fmt.Errorf("the restore changed the order gap to %d, want %d", got, gap)
	}
	return nil
}
