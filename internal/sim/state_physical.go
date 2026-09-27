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
	// of the whole network plus budgetLaneBlocks for each lane, but at most
	// budgetMaxBlocks. Each block of a route counts, and each lane of a
	// waiting-trip route counts as one. The routes share the cells of each
	// lane (see laneCells), so the memory of a route grows with its lanes.
	// The restore walks each block of each route, so the budget bounds the
	// work.
	//
	// In runs of one hour at 30 and 120 orders per minute, with the most
	// pods of each preset, the saved routes had at most 5.5 times the
	// blocks of the network (Scale100) and at most 46,246 blocks (London
	// with 200 pods). When each of 200 pods holds the longest route
	// between two stations, the routes have 19 times the blocks of
	// Scale100 and about 113,000 blocks in London. The multiple keeps room
	// for these cases in a small network. With budgetMaxBlocks, the work of
	// a restore does not grow with the network. A restore of 200 pods, each
	// on a route of about 33,000 blocks, keeps the pods that fit in this
	// budget. It takes less than 0.1 seconds and allocates less than 25 MB.
	budgetNetworkMultiple = 32
	budgetLaneBlocks      = 4
	budgetMaxBlocks       = 256_000
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
		if err := state.validateRiders(pod, active); err != nil {
			return 0, fmt.Errorf("pod %s: %w", pod.ID, err)
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
	case validateSharedRideMode(savedSharedRideMode(state)) != nil:
		return errors.New("the saved shared ride mode or stop limit is not valid")
	case len(state.DemoError) > maxSavedText:
		return errors.New("the saved demo error is too long")
	default:
		return nil
	}
}

func (state SavedState) validRequest(request SavedRequest) bool {
	return request.ID >= 1 && request.ID <= state.RequestID && request.PartySize >= 1 &&
		request.RequestedTick >= 0 && request.RequestedTick <= state.Tick && len(request.DispatchReason) <= maxSavedText &&
		request.BoardedTick >= 0 && request.BoardedTick <= state.Tick
}

// validateRiders checks the riders and the stops of a saved pod. A rider
// that did not leave the pod must have a request ID that no other such rider
// has, and active holds the IDs that the pods before it use. In a pod that
// boards or travels, each such rider must leave the pod at a stop, and the
// pod goes to its first stop. In an unloading pod, a rider with no later stop
// must go to the destination station of the pod, where the pod unloads.
// Otherwise the restore would complete the journey of that rider at the
// wrong station. An unloading pod must also be at its destination berth,
// because the next leg of the pod starts there.
func (state SavedState) validateRiders(pod SavedPod, active map[int]bool) error {
	if len(pod.Riders) > MaxSharedRideParties || len(pod.Stops) > MaxSharedRideParties {
		return fmt.Errorf("the pod has %d riders and %d stops", len(pod.Riders), len(pod.Stops))
	}
	for _, rider := range pod.Riders {
		if !state.validRequest(rider) {
			return fmt.Errorf("rider %d is not valid", rider.ID)
		}
	}
	if !pod.carriesPassengers() {
		return nil
	}
	unloading := pod.Activity == activityCode(Unloading)
	if unloading && (pod.Destination != pod.BerthID || pod.DestinationStation != pod.StationID) {
		return errors.New("the unloading pod is not at its destination")
	}
	for _, rider := range pod.Riders {
		if rider.Completed {
			continue
		}
		if active[rider.ID] {
			return fmt.Errorf("rider %d is in two pods", rider.ID)
		}
		active[rider.ID] = true
		if !slices.Contains(pod.Stops, rider.To) && (!unloading || rider.To != pod.DestinationStation) {
			return fmt.Errorf("rider %d has no stop", rider.ID)
		}
	}
	if !unloading && (len(pod.Stops) == 0 || pod.Stops[0] != pod.DestinationStation) {
		return errors.New("the first stop is not the destination")
	}
	return nil
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
	r.budget = min(budgetNetworkMultiple*networkBlocks+budgetLaneBlocks*len(s.network.Lanes), budgetMaxBlocks)
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

// savedSharedRideMode returns the shared ride mode and the stop limit of a
// saved state, with the defaults for the zero values.
func savedSharedRideMode(state SavedState) (SharedRideMode, int) {
	mode, maxStops := state.SharedRideMode, state.SharedRideMaxStops
	if mode == "" {
		mode = SharedRideDestination
	}
	if maxStops == 0 {
		maxStops = DefaultSharedRideMaxStops
	}
	return mode, maxStops
}

// setSavedCounters sets the clock, the paused flag, the counters and the
// shared ride settings of a saved state. Both restore tiers keep them.
func (s *Simulation) setSavedCounters(state SavedState) {
	s.tick, s.paused, s.completed, s.requestID = state.Tick, state.Paused, state.Completed, state.RequestID
	s.boarded, s.totalWaitTicks, s.maxWaitTicks = state.Boarded, state.TotalWaitTicks, state.MaxWaitTicks
	s.nextRedistributionTick = state.NextRedistributionTick
	s.passengerDistanceMeters, s.emptyDistanceMeters = state.PassengerDistanceMeters, state.EmptyDistanceMeters
	s.rebalanceMoves, s.sharedParties = state.RebalanceMoves, state.SharedParties
	s.journeys, s.totalJourneyTicks, s.maxJourneyTicks = state.Journeys, state.TotalJourneyTicks, state.MaxJourneyTicks
	s.riderDistanceMeters, s.directDistanceMeters = state.RiderDistanceMeters, state.DirectDistanceMeters
	s.maxDetourRatio = state.MaxDetourRatio
	s.sharedRidePartyLimit = state.SharedRidePartyLimit
	s.sharedRideMode, s.sharedRideMaxStops = savedSharedRideMode(state)
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
		Pod:   Pod{ID: saved.ID, Activity: activity, Occupied: saved.Occupied},
		Stops: slices.Clone(saved.Stops), RelocatingTo: saved.RelocatingTo, Rebalancing: saved.Rebalancing,
		phaseTicks: saved.PhaseTicks, rebalanceAfter: saved.RebalanceAfter, destinationStation: saved.DestinationStation,
		pending: -1, reservedThrough: -1,
	}
	v.released = saved.Released && releasable(v)
	for _, rider := range saved.Riders {
		v.Riders = append(v.Riders, Request(rider))
	}
	if v.carriesPassengers() {
		v.riddenBase = saved.RiddenMeters
	}
	if err := r.checkPassengers(v); err != nil {
		return err
	}
	origin, originOK := r.berths[saved.Origin]
	destination, destinationOK := r.berths[saved.Destination]
	v.origin, v.destination, v.journeyOrigin = origin.berth, destination.berth, origin.berth
	journeyOrigin, journeyOriginOK := r.berths[saved.JourneyOrigin]
	if journeyOriginOK {
		v.journeyOrigin = journeyOrigin.berth
	}
	valid := (originOK || saved.Origin == "") && (destinationOK || saved.Destination == "") &&
		(journeyOriginOK || saved.JourneyOrigin == "") &&
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
	case (activity == Boarding || activity == Unloading || activity == Continuing || activity == Traveling && v.RelocatingTo == "") &&
		!v.carriesPassengers():
		return errors.New("the pod has no active request")
	case (activity == Idle || activity == DepartingEmpty || v.RelocatingTo != "") && v.Pod.Occupied:
		return errors.New("an empty pod is occupied")
	case activity == DepartingEmpty && v.RelocatingTo == "":
		return errors.New("an empty departure has no station to relocate to")
	case v.carriesPassengers() && !r.passengerRiders(v):
		return errors.New("a rider or a stop is not at a passenger station")
	default:
		return nil
	}
}

// passengerRiders reports whether each rider aboard a pod and each stop of
// the pod use passenger stations.
func (r *physicalRestore) passengerRiders(v *vehicle) bool {
	for _, rider := range v.Riders {
		if !rider.Completed && (!r.s.passengerStation(rider.From) || !r.s.passengerStation(rider.To)) {
			return false
		}
	}
	return !slices.ContainsFunc(v.Stops, func(stop string) bool { return !r.s.passengerStation(stop) })
}

func (s *Simulation) passengerStation(id string) bool {
	station, ok := s.station(id)
	return ok && !station.ParkingOnly
}

// routed reports whether a pod at an activity uses its route.
func routed(activity Activity) bool {
	return departs(activity) || activity == Traveling
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
	v.Route, v.blocks, v.routeLengths, v.blockStarts, v.terminal = nil, blockList{}, nil, nil, terminalCheck{}
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
		ready := departs(v.Pod.Activity) && v.phaseTicks == 0
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
	first, last := routeLaneBlocks(&v.blocks, saved.RouteIndex)
	laneDistance := min(max(saved.LaneDistance, -restoreTolerance), r.s.laneLength(lane)+restoreTolerance)
	distance := restoredDistance(restoredDistanceInput{
		blocks: &v.blocks, first: first, last: last, laneDistance: laneDistance, saved: saved.Distance,
	})
	// The saved route index picks the lane, also at an exact lane boundary.
	index := last
	for candidate, b := range v.blocks.span(first, last+1) {
		if b.end >= distance {
			index = candidate
			break
		}
	}
	through := reservationEnd(&v.blocks, index)
	if distance < Clearance && (v.origin.ID == "" || v.Route[0].From != v.origin.Node) {
		return false
	}
	// A pod that has no berth yet chooses one before it reserves the last lane.
	if lastLane, _ := routeLaneBlocks(&v.blocks, len(v.Route)-1); v.destination.ID == "" && through >= lastLane {
		return false
	}
	footprint := v.footprint(through, distance)
	if slices.ContainsFunc(footprint, func(claimed resource) bool { return r.s.owners[claimed] != "" }) {
		return false
	}
	for _, claimed := range footprint {
		r.s.owners[claimed] = v.Pod.ID
	}
	for _, b := range v.blocks.span(0, through+1) {
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
	if saved.Waiting && through+1 < v.blocks.len() {
		v.pending, v.waitSince = through+1, saved.WaitSince
	}
	return true
}

type restoredDistanceInput struct {
	blocks       *blockList
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
	distance := input.blocks.at(input.first).laneStart + input.laneDistance
	if math.Abs(distance-input.saved) <= restoreTolerance {
		distance = input.saved
	}
	for _, b := range input.blocks.span(max(0, input.first-1), input.last+1) {
		if math.Abs(b.end-distance) <= restoreTolerance {
			return b.end
		}
	}
	return distance
}

// routeLaneBlocks returns the first and the last block of the lane at a route
// index. It counts lane starts, because a route can hold a lane more than
// once.
func routeLaneBlocks(blocks *blockList, routeIndex int) (first, last int) {
	switch {
	case blocks.len() == 0:
		return -1, -1
	case routeIndex < 0:
		return -1, -1
	case routeIndex >= len(blocks.route):
		return -1, blocks.len() - 1
	default:
		return blocks.laneFirst(routeIndex), blocks.laneFirst(routeIndex+1) - 1
	}
}

// footprint returns the resources that a traveling pod holds when it has
// reserved blocks 0 to through and is at a route distance. These are the
// resources of those blocks that the pod has not passed by their release
// distance. Before the pod is Clearance from its origin, it also holds the
// origin berth and node. A set finds the repeated resources, because a
// saved route can reserve many blocks.
func (v *vehicle) footprint(through int, distance float64) []resource {
	var held []resource
	seen := make(map[resource]bool)
	add := func(claimed resource) {
		if !seen[claimed] {
			seen[claimed] = true
			held = append(held, claimed)
		}
	}
	for _, b := range v.blocks.span(0, through+1) {
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
		from, _ := r.s.station(v.boardingStation())
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
	v.Route, v.blocks, v.routeLengths, v.blockStarts, v.terminal = nil, blockList{}, nil, nil, terminalCheck{}
	v.RelocatingTo, v.Rebalancing, v.released = "", false, false
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
	route, err := r.s.stationApproachRoute(berth.Node, v.Stops[0])
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
	riders := make([]Request, 0, len(v.Riders))
	for _, rider := range v.Riders {
		if !rider.Completed {
			rider.PodID, rider.DispatchReason = v.Pod.ID, ""
			riders = append(riders, rider)
		}
	}
	v.Riders, v.riddenBase = riders, 0
	v.Pod.Activity, v.Pod.StationID = Boarding, r.berths[berth.ID].station
	v.RelocatingTo, v.Rebalancing, v.released = "", false, false
	v.origin, v.destination, v.destinationStation = berth, Berth{}, v.Stops[0]
	v.journeyOrigin = berth
	r.s.setVehicleRoute(v, route)
	return true
}

// requeue returns each rider aboard a pod to the queue as one trip. The
// boarding of the riders stays recorded.
func (r *physicalRestore) requeue(v *vehicle) {
	for _, rider := range v.Riders {
		if !rider.Completed {
			r.requeued = append(r.requeued, requeuedTrip(rider))
			r.result.Requeued = append(r.result.Requeued, rider.ID)
		}
	}
	v.Riders, v.Stops = nil, nil
}

// requeuedTrip returns the queued trip for a rider of a pod. The trip is
// boarded, so board and joinSharedRide do not record the boarding again.
func requeuedTrip(rider Request) waitingTrip {
	rider.PodID, rider.DispatchReason = "", ""
	return waitingTrip{request: rider, boarded: true}
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
// deadline that is in range. When the restore drops a trip or clears its
// pod, and no kept trip names that pod, the empty pod on its way to the
// pickup has no order. The restore releases it as dispatch does, so that it
// can take new work at once.
func (r *physicalRestore) restoreWaiting() {
	s := r.s
	carried := make(map[int]bool)
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.carriesPassengers() {
			for _, rider := range v.Riders {
				if !rider.Completed {
					carried[rider.ID] = true
				}
			}
		}
	}
	var orphaned []string
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
			r.result.DroppedParties++
			orphaned = append(orphaned, request.PodID)
			continue
		}
		carried[request.ID] = true
		trip := r.restoreTrip(index, request)
		if trip.request.PodID != request.PodID {
			orphaned = append(orphaned, request.PodID)
		}
		s.waiting = append(s.waiting, trip)
	}
	s.waiting = append(s.waiting, requeued...)
	for _, id := range orphaned {
		if v := s.findVehicle(id); v != nil && !s.assigned(id) {
			s.releasePickup(v)
		}
	}
}

func (r *physicalRestore) restoreTrip(index int, request Request) waitingTrip {
	saved := r.state.Waiting[index]
	trip := waitingTrip{
		request: request, boarded: saved.Boarded,
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
