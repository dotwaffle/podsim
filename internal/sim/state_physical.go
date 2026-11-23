package sim

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
)

const (
	// restoreTolerance is the distance in meters within which the restore
	// takes two saved positions as one.
	restoreTolerance = 1e-6
	// restoreDriftTolerance is the distance in meters within which
	// restoredDistance moves a restored pod to a block end (restore speed
	// proposal, section 4.2). The sums of lane lengths of a saved route that
	// starts at a later lane differ from the live sums in the last bits:
	// the largest drift measured was 2.1e-12 m, and this tolerance is 500
	// times that. A pod that stopped at a block end therefore lands on the
	// block end. A moving pod short of a block end by more than this stays
	// short of it, so it keeps its side of a release distance at that block
	// end, and it keeps the resources that the live pod owns.
	restoreDriftTolerance = 1e-9
	// restoreSpeedMargin is the speed in m/s by which a restored pod can be
	// above the speed limit of its lane and keep its saved speed (restore
	// speed proposal, section 4.2). A live lane crossing can publish a speed
	// above the limit of the new lane by rounding: review found 8e-15 m/s
	// on a 5 m/s lane and 3.25e-6 m/s on a slow lane at a long route
	// distance. The margin is more than 10 times the largest such excess
	// for a practical route length. It is less than one tick of braking of
	// any class (Express: 1.15 m/s² / 30 = 0.038 m/s), and the first move
	// commands at most the lane limit, so the pod sheds an accepted excess
	// in one tick within its braking rate.
	restoreSpeedMargin = 1e-3
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
	s      *Simulation
	state  SavedState
	result RestoreResult
	berths map[string]berthRef
	// demoted marks the pods that go to a berth when the traveling pods are
	// in place.
	demoted []bool
	// atRest marks the traveling pods that placeTravelingPod placed at rest
	// after the placement with their saved motion failed.
	atRest []bool
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
	// leaders holds one plus the index of the saved predecessor of each
	// pod, or 0.
	leaders []int
	// faulted marks the pods that a saved pod fault names.
	faulted []bool
	// earlier is true in the restore before the saved motion (see
	// restoreBeforeMotion) and in the scratch placement of
	// checkSavedFaultFootprints: each traveling pod places with the earlier
	// placement policy.
	earlier bool
	// projected holds the projected draining flag of the saved link of each
	// pod (see projectDraining). The restore before the saved motion takes
	// the flags of the first physical restore.
	projected []bool
	// latchDropped marks the traveling pods whose committed placement
	// dropped the saved reversal dwell latch (see latchAgrees).
	latchDropped []bool
	// routeBuilt is RestoreStateInput.routeBuilt, and linkProjected is
	// RestoreStateInput.linkProjected.
	routeBuilt, linkProjected func(index int)
}

// earlierEngine is the input of the restore before the saved motion (see
// restoreBeforeMotion): the projected draining flags of the first
// physical restore.
type earlierEngine struct {
	projected []bool
}

// restorePhysical rebuilds a running simulation with each pod where the saved
// state puts it, at its saved speed. It derives the reservations again from
// the saved positions and the saved frontiers. A traveling pod that cannot
// keep its place moves to a berth, which is a demotion. restorePhysical
// fails when the saved state is not valid, when two pods at berths
// conflict, or when a demoted pod finds no free berth. It also returns the
// projected draining flags of the saved links (see projectDraining), or
// nil when it fails before it projects them.
func restorePhysical(input RestoreStateInput, newFleet func() (*Simulation, error)) (*Simulation, RestoreResult, []bool, error) {
	return restorePhysicalEngine(input, newFleet, nil)
}

// restorePhysicalEngine is restorePhysical. With earlier not nil, each
// traveling pod places with the earlier placement policy, and the saved
// links take the projected draining flags of earlier (see
// restoreBeforeMotion).
func restorePhysicalEngine(input RestoreStateInput, newFleet func() (*Simulation, error), earlier *earlierEngine) (*Simulation, RestoreResult, []bool, error) {
	r, unaccounted, err := newRestoreEngine(input, newFleet, earlier)
	if err != nil {
		return nil, RestoreResult{}, nil, err
	}
	s := r.s
	if err := r.placeTraveling(); err != nil {
		return nil, RestoreResult{}, r.projected, err
	}
	r.claimDestinations()
	if err := r.separate(); err != nil {
		return nil, RestoreResult{}, r.projected, err
	}
	if err := r.restoreFaultedPods(); err != nil {
		return nil, RestoreResult{}, r.projected, err
	}
	if err := r.restoreEmergencies(); err != nil {
		return nil, RestoreResult{}, r.projected, err
	}
	for index, demoted := range r.demoted {
		if !demoted {
			continue
		}
		if err := r.placeDemoted(index); err != nil {
			return nil, RestoreResult{}, r.projected, err
		}
		r.result.Demoted = append(r.result.Demoted, s.vehicles[index].Pod.ID)
	}
	for index, atRest := range r.atRest {
		if atRest && !r.demoted[index] {
			r.result.AtRest = append(r.result.AtRest, s.vehicles[index].Pod.ID)
		}
	}
	for index, dropped := range r.latchDropped {
		if dropped && !r.demoted[index] {
			r.result.DroppedLatches = append(r.result.DroppedLatches, s.vehicles[index].Pod.ID)
		}
	}
	r.restoreWaiting()
	slices.Sort(r.result.Interrupted)
	if err := s.verifyRestore(input.State, nil, r.result.Interrupted, r.result.Dropped, unaccounted); err != nil {
		return nil, RestoreResult{}, r.projected, err
	}
	r.result.Tier, r.result.Unaccounted = RestorePhysical, unaccounted
	return s, r.result, r.projected, nil
}

// newRestoreEngine runs the steps of the physical tier before it places the
// traveling pods: it decodes the pods, admits and builds their routes,
// checks the links, projects the draining flags, or takes those of
// earlier, and places the debris and the pods at berths. It returns the
// restore and the unaccounted orders of the saved state.
func newRestoreEngine(input RestoreStateInput, newFleet func() (*Simulation, error), earlier *earlierEngine) (*physicalRestore, int, error) {
	unaccounted, err := validateSavedState(input.State, input.Network)
	if err != nil {
		return nil, 0, err
	}
	s, err := newFleet()
	if err != nil {
		return nil, 0, fmt.Errorf("create the fleet: %w", err)
	}
	if err := s.SetExpressServices(input.ExpressServices); err != nil {
		return nil, 0, err
	}
	if err := s.setRestoredFaults(input); err != nil {
		return nil, 0, err
	}
	if err := s.setRestoredEmergencies(input); err != nil {
		return nil, 0, err
	}
	if err := s.checkSavedClasses(input.State); err != nil {
		return nil, 0, err
	}
	if err := checkSavedPodIDs(s.initial, input.State); err != nil {
		return nil, 0, err
	}
	if err := checkFleetSaved(s.initial, input.State); err != nil {
		return nil, 0, err
	}
	r := newPhysicalRestore(s, input.State)
	r.earlier, r.routeBuilt, r.linkProjected = earlier != nil, input.routeBuilt, input.linkProjected
	if input.blockBudget > 0 {
		r.budget = input.blockBudget
	}
	r.restoreCounters()
	if err := s.SetOnboardPickups(input.OnboardPickups); err != nil {
		return nil, 0, err
	}
	if err := r.decodePods(); err != nil {
		return nil, 0, err
	}
	// The saved pods can differ from the fleet that NewFleet indexed.
	s.vehicleIndexes = indexVehicles(s.vehicles)
	if err := r.checkRoutes(); err != nil {
		return nil, 0, err
	}
	r.limitWork()
	if err := r.buildRoutes(); err != nil {
		return nil, 0, err
	}
	if err := r.checkLinks(); err != nil {
		return nil, 0, err
	}
	if earlier == nil {
		r.projectDraining()
	} else {
		r.projected = earlier.projected
	}
	// Debris has its footprint before the pods take their resources
	// (section 7.6 of the incident suspension contract).
	if err := r.placeDebris(); err != nil {
		return nil, 0, err
	}
	if err := r.claimBerths(); err != nil {
		return nil, 0, err
	}
	return r, unaccounted, nil
}

// validateSavedState checks the bounds of the queue and the contract of the
// saved state. See checkContract. It returns the unaccounted orders of the
// saved state.
func validateSavedState(state SavedState, network Network) (int, error) {
	limit := MaxWaitingTripsForOrderContract(state.OrderContract)
	if len(state.Waiting) > limit {
		return 0, errors.New("too many saved waiting trips")
	}
	// The logical tier requeues each held rider with the waiting orders, so
	// the queue that a restore can make must fit the same bound. Otherwise a
	// restored session could save a state that a restore refuses. An
	// active demo adds its remaining orders later.
	held := len(state.Waiting) + savedDemoOrders(state.Demo)
	for _, pod := range state.Pods {
		for _, rider := range pod.Riders {
			if !rider.Completed {
				held++
			}
		}
	}
	if held > limit {
		return 0, errors.New("too many saved outstanding orders")
	}
	return state.checkContract(network)
}

func (state SavedState) validateCounters() error {
	counters := []int64{
		state.Tick, int64(state.Completed), int64(state.RequestID), int64(state.Boarded), state.TotalWaitTicks,
		state.MaxWaitTicks, state.NextRedistributionTick, int64(state.RebalanceMoves), int64(state.SharedParties),
		int64(state.Journeys), state.TotalJourneyTicks, state.MaxJourneyTicks,
		int64(state.Interrupted), int64(state.InterruptedPassengers),
	}
	distances := []float64{
		state.PassengerDistanceMeters, state.EmptyDistanceMeters, state.RiderDistanceMeters, state.DirectDistanceMeters,
		state.MaxDetourRatio,
	}
	switch {
	case slices.ContainsFunc(counters, func(counter int64) bool { return counter < 0 }),
		slices.ContainsFunc(distances, func(distance float64) bool { return !finite(distance) || distance < 0 }):
		return errors.New("a saved counter is negative or not finite")
	case state.Completed > state.RequestID || state.Boarded > state.RequestID:
		return errors.New("the saved state completed or boarded more orders than it submitted")
	case state.InterruptedPassengers < state.Interrupted || state.Completed > state.RequestID-state.Interrupted:
		return errors.New("the saved interrupted orders are not valid")
	case state.Journeys > state.Completed || state.MaxJourneyTicks > state.TotalJourneyTicks:
		return errors.New("the saved journey totals are not valid")
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
	return validSavedOptionsWithOrderContract(request, state.OrderContract) && request.ID >= 1 && request.ID <= state.RequestID && request.PartySize >= 1 &&
		request.RequestedTick >= 0 && request.RequestedTick <= state.Tick && len(request.DispatchReason) <= maxSavedText &&
		request.BoardedTick >= 0 && request.BoardedTick <= state.Tick
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

func newPhysicalRestore(s *Simulation, state SavedState) *physicalRestore {
	r := &physicalRestore{
		s: s, state: state, berths: make(map[string]berthRef),
		demoted: make([]bool, len(state.Pods)), atRest: make([]bool, len(state.Pods)), latchDropped: make([]bool, len(state.Pods)), projected: make([]bool, len(state.Pods)), routes: make([][]int, len(state.Pods)), costs: make([]int, len(state.Pods)),
		tripRoutes: make([][]int, len(state.Waiting)), unbound: make([]bool, len(state.Waiting)),
		faulted: savedFaultedPods(state),
	}
	r.laneBlocks, r.budget = physicalBlockBudget(s)
	for _, station := range s.network.Stations {
		for _, berth := range station.Berths {
			r.berths[berth.ID] = berthRef{station: station.ID, berth: berth}
		}
	}
	return r
}

// physicalBlockBudget returns the blocks of each network lane of s and the
// block budget of the physical restore, from the prepared network of s
// (see laneBlockBudget). No caller writes to the slice. A simulation that
// a test builds without a prepared network gets them from its lanes.
func physicalBlockBudget(s *Simulation) ([]int, int) {
	if s.laneBlocks == nil {
		lengths := make([]float64, len(s.network.Lanes))
		for index, lane := range s.network.Lanes {
			lengths[index] = s.laneLength(lane)
		}
		return laneBlockBudget(lengths)
	}
	return s.laneBlocks, s.blockBudget
}

// laneBlockBudget returns the blocks of each network lane, with the lane
// lengths in lengths, and the block budget of the physical restore: the
// blocks of the network budgetNetworkMultiple times, plus budgetLaneBlocks
// for each lane, but at most budgetMaxBlocks.
func laneBlockBudget(lengths []float64) ([]int, int) {
	laneBlocks := make([]int, len(lengths))
	networkBlocks := 0
	for index, length := range lengths {
		laneBlocks[index] = laneBlockCount(length)
		networkBlocks += laneBlocks[index]
	}
	return laneBlocks, min(budgetNetworkMultiple*networkBlocks+budgetLaneBlocks*len(lengths), budgetMaxBlocks)
}

func (r *physicalRestore) restoreCounters() {
	s, state := r.s, r.state
	s.setSavedCounters(state)
	s.demoError = state.DemoError
	if state.Demo != nil {
		s.demo = &demoRun{secondSent: state.Demo.SecondSent, followupsSent: state.Demo.FollowupsSent}
	}
	s.owners = make(map[resource]resourceOwner)
	s.vehicles = make([]vehicle, len(state.Pods))
}

// savedSharedRideMode returns the shared ride mode and the stop limit of a
// saved state, with the defaults for the zero values.
func savedSharedRideMode(state SavedState) (SharedRideMode, int) {
	mode, maxStops := state.SharedRideMode, state.SharedRideMaxStops
	if mode == "" {
		mode = DefaultSharedRideMode
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
	s.interrupted, s.interruptedPassengers = state.Interrupted, state.InterruptedPassengers
	s.sharedRidePartyLimit = state.SharedRidePartyLimit
	s.sharedRideMode, s.sharedRideMaxStops = savedSharedRideMode(state)
	s.incidentSerial = state.IncidentSerial
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
		Pod:       Pod{ID: saved.ID, Class: saved.Class, Activity: activity, Occupied: saved.Occupied},
		Boardings: slices.Clone(saved.Boardings), Stops: slices.Clone(saved.Stops), RelocatingTo: saved.RelocatingTo, Rebalancing: saved.Rebalancing,
		phaseTicks: saved.PhaseTicks, rebalanceAfter: saved.RebalanceAfter, destinationStation: saved.DestinationStation,
		pending: -1, reservedThrough: -1, withdrawn: serviceHold(saved.Withdrawn), parkedSince: saved.ParkedSince,
		op: operationalDestination{purpose: opPurpose(saved.Purpose), owner: serviceHold(saved.Owner), interrupt: saved.Interrupt},
	}
	if v.op.purpose == opRefuge && activity == Unloading {
		v.Pod.WaitReason = refugeHolding
	}
	v.released = saved.Released && releasable(v)
	for _, rider := range saved.Riders {
		v.Riders = append(v.Riders, Request(rider))
	}
	if v.RidersAboard() > 0 || len(v.Boardings) > 0 {
		v.riddenBase = saved.RiddenMeters
	}
	if !r.passengerRiders(v) {
		return errors.New("a rider or a stop is not at a passenger station")
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
	if !valid || saved.PhaseTicks < 0 || saved.PhaseTicks > maxPhaseTicks(activity, saved.Class) {
		return errors.New("a value is out of range")
	}
	v.Pod.StationID, v.Pod.BerthID = berth.station, berth.berth.ID
	return nil
}

// maxPhaseTicks returns the largest phase count of a pod of a class at a
// berth.
func maxPhaseTicks(activity Activity, class VehicleClass) int {
	switch activity {
	case Boarding:
		return maxBoardingPhase(class)
	case Unloading:
		return maxUnloadingPhase(class)
	default:
		return 0
	}
}

// passengerRiders reports whether each rider aboard a pod and each stop of
// the pod use passenger stations.
func (r *physicalRestore) passengerRiders(v *vehicle) bool {
	for _, rider := range v.Riders {
		if !rider.Completed && (!r.s.passengerStation(rider.From) || !r.s.passengerStation(rider.To) || !r.s.optionalPassengerStation(rider.LegFrom)) {
			return false
		}
	}
	return !slices.ContainsFunc(v.Stops, func(stop string) bool { return !r.s.passengerStation(stop) })
}

func (s *Simulation) passengerStation(id string) bool {
	station, ok := s.station(id)
	return ok && !station.ParkingOnly
}

// optionalPassengerStation reports whether id is empty or a passenger
// station. An order without a leg origin has an empty LegFrom.
func (s *Simulation) optionalPassengerStation(id string) bool {
	return id == "" || s.passengerStation(id)
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
// each resource that the pod holds. It also demotes the pods behind the pod
// in its platoon, because they can hold cells of the pod, and it ends the
// link of the pod.
func (r *physicalRestore) demote(index int) {
	v := &r.s.vehicles[index]
	if v.follower != 0 {
		r.demote(v.follower - 1)
	}
	if v.link.leader != 0 {
		r.s.unlink(v)
	}
	r.demoted[index] = true
	r.cost -= r.costs[index]
	r.costs[index], r.routes[index] = 0, nil
	v.replaceRoute(nil)
	v.blocks, v.routeLengths, v.blockStarts, v.terminal = blockList{}, nil, nil, terminalCheck{}
	maps.DeleteFunc(r.s.owners, func(_ resource, owner resourceOwner) bool { return owner.isPod(v.Pod.ID) })
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
		if err := r.buildRoute(index, indexes); err != nil {
			return err
		}
	}
	return nil
}

// buildRoute gives the pod at index its saved route, the lane indexes
// that checkRoutes accepted. It demotes a traveling pod whose route does
// not connect it to its destination.
func (r *physicalRestore) buildRoute(index int, indexes []int) error {
	if r.routeBuilt != nil {
		r.routeBuilt(index)
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
		return nil
	}
	r.s.setVehicleRoute(v, route)
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
	return ok && station.isEntry(end)
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
			if owner := r.s.owners[claimed]; !owner.isZero() {
				return fmt.Errorf("pods %s and %s are at berth %s", owner, v.Pod.ID, berth.ID)
			}
			r.s.owners[claimed] = podResourceOwner(v.Pod.ID)
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

// checkLinks checks the saved platoon links and keeps them in r.leaders. A
// link must name another traveling pod, and a pod can have one follower. A
// platoon must not form a loop, hold more than MaxPlatoonLimit pods, or
// have two turns. checkSavedLink checks the run of each link of two pods
// that keep their routes.
func (r *physicalRestore) checkLinks() error {
	indexes := make(map[string]int, len(r.state.Pods))
	for index, saved := range r.state.Pods {
		indexes[saved.ID] = index
	}
	r.leaders = make([]int, len(r.state.Pods))
	followed := make([]bool, len(r.state.Pods))
	for index, saved := range r.state.Pods {
		if saved.Platoon == nil {
			continue
		}
		leader, ok := indexes[saved.Platoon.Leader]
		switch {
		case !ok || leader == index:
			return fmt.Errorf("pod %s names a platoon predecessor that is not valid", saved.ID)
		case r.s.vehicles[index].Pod.Activity != Traveling || r.s.vehicles[leader].Pod.Activity != Traveling:
			return fmt.Errorf("pod %s has a platoon link, and it or its predecessor is not traveling", saved.ID)
		case followed[leader]:
			return fmt.Errorf("pod %s has two platoon followers", saved.Platoon.Leader)
		}
		followed[leader] = true
		r.leaders[index] = leader + 1
	}
	for index := range r.leaders {
		if r.linkDepth(index) > MaxPlatoonLimit {
			return fmt.Errorf("the platoon of pod %s is a loop or holds more than %d pods", r.state.Pods[index].ID, MaxPlatoonLimit)
		}
		if r.leaders[index] == 0 {
			continue
		}
		link, ahead := r.state.Pods[index].Platoon, r.state.Pods[r.leaders[index]-1].Platoon
		if ahead != nil && ahead.Turn != link.Turn {
			return fmt.Errorf("the platoon of pod %s has two turns", r.state.Pods[index].ID)
		}
		if err := r.checkSavedLink(index); err != nil {
			return fmt.Errorf("pod %s: %w", r.state.Pods[index].ID, err)
		}
	}
	return nil
}

// checkSavedLink checks the saved run of the link of the pod at index. Both
// routes must hold the lanes of the run and a lane after it. No lane of the
// run can be a berth access lane or start at an entry node of the saved
// destination station of either pod. Then, in a second pass, an entry lane
// of the run must be an entry lane of the saved destination station of
// both pods, as sharedLane allows. The turn must be within platoonMaxTurn, and
// the total turn of the route of the pod from its lane to the end of the
// run must be within the turn. As when a link forms, the rest of both
// routes and the lanes of both destination stations must have one speed
// limit, because a pod slows down at once to a lower limit. Last, no lane
// of the run can be in a maneuver group, and then no lane can be a depot
// lane. It does not check a pod that lost its route.
func (r *physicalRestore) checkSavedLink(index int) error {
	v, leader := &r.s.vehicles[index], &r.s.vehicles[r.leaders[index]-1]
	saved := r.state.Pods[index]
	link := saved.Platoon
	if v.Route == nil || leader.Route == nil {
		return nil
	}
	switch {
	case link.Lane < 0 || link.Lane >= len(v.Route) || link.LeaderLane < 0 || link.LeaderLane >= len(leader.Route) ||
		link.Lanes < 1 || link.Lanes >= len(v.Route)-link.Lane || link.Lanes >= len(leader.Route)-link.LeaderLane:
		return errors.New("the platoon run is not on both routes")
	case math.IsNaN(link.Turn) || link.Turn < 0 || link.Turn > platoonMaxTurn:
		return fmt.Errorf("the platoon turn %g is not between 0 and %g", link.Turn, platoonMaxTurn)
	}
	stations := [2]string{saved.DestinationStation, r.state.Pods[r.leaders[index]-1].DestinationStation}
	for k := range link.Lanes {
		lane := &v.Route[link.Lane+k]
		if lane.ID != leader.Route[link.LeaderLane+k].ID || lane.StationRole == StationBerthAccessRole || r.entryNode(stations, lane.From) {
			return errors.New("the platoon run is not on both routes")
		}
	}
	for _, lane := range v.Route[link.Lane : link.Lane+link.Lanes] {
		if lane.StationRole == StationEntryRole && (lane.StationID != stations[0] || lane.StationID != stations[1]) {
			return errors.New("the platoon run holds the entry lane of another station")
		}
	}
	if last := link.Lane + link.Lanes - 1; saved.RouteIndex >= 0 && saved.RouteIndex <= last && r.s.runTurn(v.Route, saved.RouteIndex, last) > link.Turn+platoonTurnSlack {
		return fmt.Errorf("the platoon run turns more than %g", link.Turn)
	}
	// A pod with a route index out of range is demoted, and then the link
	// is not restored.
	ahead := r.state.Pods[r.leaders[index]-1].RouteIndex
	if saved.RouteIndex < 0 || saved.RouteIndex >= len(v.Route) || ahead < 0 || ahead >= len(leader.Route) {
		return nil
	}
	if limit := v.Route[saved.RouteIndex].SpeedLimit; !r.s.oneSpeedLimit(v, saved.RouteIndex, limit) || !r.s.oneSpeedLimit(leader, ahead, limit) {
		return errors.New("the routes of the platoon have more than one speed limit")
	}
	if slices.ContainsFunc(v.Route[link.Lane:link.Lane+link.Lanes], func(lane Lane) bool { return r.s.maneuvers.groupLane(lane.ID) }) {
		return errors.New("the platoon run holds a lane of a maneuver group")
	}
	if slices.ContainsFunc(v.Route[link.Lane:link.Lane+link.Lanes], func(lane Lane) bool { return r.s.depots.depotLane(lane.ID) }) {
		return errors.New("the platoon run holds a lane of a depot")
	}
	return nil
}

// entryNode reports whether node is an entry node of one of stations.
func (r *physicalRestore) entryNode(stations [2]string, node string) bool {
	for _, id := range stations {
		if station, ok := r.s.station(id); ok && station.isEntry(node) {
			return true
		}
	}
	return false
}

// linkDepth returns the number of pods from the pod at index to the front
// of its saved platoon. It stops after MaxPlatoonLimit+1 pods, so a loop
// also gives a depth larger than MaxPlatoonLimit.
func (r *physicalRestore) linkDepth(index int) int {
	depth := 1
	for leader := r.leaders[index]; leader != 0 && depth <= MaxPlatoonLimit; leader = r.leaders[leader-1] {
		depth++
	}
	return depth
}

// placeTraveling puts each traveling pod on its route. It places the pods in
// saved order by their depth in their platoons, so each predecessor is in
// place before its follower. It demotes a pod whose values are not valid or
// whose resources another pod holds.
func (r *physicalRestore) placeTraveling() error {
	order := make([]int, 0, len(r.state.Pods))
	for index := range r.state.Pods {
		if r.s.vehicles[index].Pod.Activity == Traveling {
			order = append(order, index)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(r.linkDepth(a), r.linkDepth(b)) })
	for _, index := range order {
		if r.demoted[index] {
			continue
		}
		leader := r.leaders[index] - 1
		if leader >= 0 && r.demoted[leader] {
			leader = -1
		}
		placed, err := r.placeTravelingPod(index, leader)
		if err != nil {
			return fmt.Errorf("pod %s: %w", r.state.Pods[index].ID, err)
		}
		if !placed {
			r.demote(index)
		}
	}
	return nil
}

// placeTravelingPod places the traveling pod at index. It plans the
// placement with the first policy of the pod (see firstPolicy). When that
// plan fails without an error and its input gave the pod a speed or a
// lead, it plans the placement at rest (see atRestRetry). It commits the
// plan that succeeds, and returns false only when both plans fail
// (restore speed proposal, section 4.2). A failed plan leaves the pod as
// it was, so the second plan starts from the same pod. An error of a plan
// is an error of the placement. leader is the index of the placed
// predecessor of the pod, or -1.
func (r *physicalRestore) placeTravelingPod(index, leader int) (bool, error) {
	first := r.placementInput(index, r.firstPolicy(index))
	plan, placed, err := r.planTravelPlacement(index, leader, first)
	if !placed && err == nil && (first.speed != 0 || first.lead > 0) {
		plan, placed, err = r.planTravelPlacement(index, leader, r.placementInput(index, atRestRetry))
		r.atRest[index] = placed && err == nil
	}
	if !placed || err != nil {
		return false, err
	}
	r.commitTravelPlacement(index, plan)
	return true, nil
}

// placementPolicy names the saved values that one placement of a traveling
// pod reads (see placementInput).
type placementPolicy uint8

const (
	// primaryWhole places a pod with a whole saved route in the exact frame,
	// with its saved speed, lead, latch, and link flag.
	primaryWhole placementPolicy = iota
	// primaryTrimmed places a pod with a trimmed saved route in the trimmed
	// frame, with its saved speed, lead, latch, and link flag.
	primaryTrimmed
	// atRestRetry places a pod in the frame of its primary policy, at rest
	// with the derived frontier. It keeps the saved latch when the latch
	// agrees with that placement.
	atRestRetry
	// earlierPlacement places a pod as the restore did before the saved
	// motion: in the earlier frame, at rest, with the derived frontier and
	// no latch. It reads no saved speed, lead, or latch.
	earlierPlacement
)

// firstPolicy returns the policy of the first placement of the traveling
// pod at index.
func (r *physicalRestore) firstPolicy(index int) placementPolicy {
	if r.earlier {
		return earlierPlacement
	}
	return primaryPolicy(r.state.Pods[index])
}

// primaryPolicy returns the primary placement policy of the saved pod
// saved, from its trimmed route marker.
func primaryPolicy(saved SavedPod) placementPolicy {
	if saved.RouteTrimmed {
		return primaryTrimmed
	}
	return primaryWhole
}

// frame returns the position frame of a placement of the saved pod saved
// with policy p. A placement at rest keeps the frame of the primary
// policy of the pod.
func (p placementPolicy) frame(saved SavedPod) positionFrame {
	switch p {
	case primaryWhole:
		return wholeFrame
	case primaryTrimmed:
		return trimmedFrame
	case atRestRetry:
		return primaryPolicy(saved).frame(saved)
	default:
		if saved.RouteTrimmed {
			return earlierTrimmedFrame
		}
		return earlierWholeFrame
	}
}

// drainingRule tells which draining flag a placement gives a saved link.
type drainingRule uint8

const (
	// rawDraining keeps the saved flag of the live link.
	rawDraining drainingRule = iota
	// heldDraining keeps the saved flag when the placement holds the end
	// block of the link, and takes the projected flag otherwise.
	heldDraining
	// projectedDraining takes the projected flag (see projectDraining).
	projectedDraining
)

// placementInput holds what one placement of a traveling pod reads from
// its saved pod. A placement reads the saved speed, lead, and latch only
// from its input.
type placementInput struct {
	frame    positionFrame
	speed    float64
	lead     int
	latch    reversal
	draining drainingRule
}

// placementInput returns the input of a placement of the traveling pod at
// index with policy.
func (r *physicalRestore) placementInput(index int, policy placementPolicy) placementInput {
	saved := r.state.Pods[index]
	frame := policy.frame(saved)
	latch := reversal{lane: saved.ReversalLane, ticks: saved.ReversalTicks}
	switch policy {
	case primaryWhole, primaryTrimmed:
		return placementInput{frame: frame, speed: r.restoredSpeed(index), lead: saved.ReservedAhead, latch: latch, draining: rawDraining}
	case atRestRetry:
		return placementInput{frame: frame, latch: latch, draining: heldDraining}
	default:
		return placementInput{frame: frame, draining: projectedDraining}
	}
}

// linkDraining returns the draining flag that a placement with input gives
// the saved link of the pod at index, with the frontier through and the
// end block end. A follower that reserved the end block of its link
// drains, but live link maintenance sets the flag only in the next tick
// (see maintainLink), so the saved flag can be false at the tick of that
// grant.
func (r *physicalRestore) linkDraining(index int, input placementInput, through, end int) bool {
	raw := r.state.Pods[index].Platoon.Draining
	switch input.draining {
	case heldDraining:
		if through < end {
			return r.projected[index]
		}
		return raw
	case projectedDraining:
		return r.projected[index]
	default:
		return raw
	}
}

// projectDraining finds the projected draining flag of the saved link of
// each traveling pod that keeps its route, before any placement: the
// saved flag, or true when the saved frontier holds the end block of the
// link. It decodes the saved frontier once, in the frame of the primary
// placement, so a later placement with another frame or a shorter
// frontier keeps the state that live link maintenance finds (see
// linkDraining). A saved frontier that is not valid gives no evidence of
// the grant of the end block.
func (r *physicalRestore) projectDraining() {
	for index, saved := range r.state.Pods {
		leader := r.leaders[index] - 1
		v := &r.s.vehicles[index]
		if leader < 0 || r.demoted[index] || r.demoted[leader] || v.Route == nil || r.s.vehicles[leader].Route == nil {
			continue
		}
		if r.linkProjected != nil {
			r.linkProjected(index)
		}
		r.projected[index] = saved.Platoon.Draining
		if saved.RouteIndex < 0 || saved.RouteIndex >= len(v.Route) {
			continue
		}
		frame := primaryPolicy(saved).frame(saved)
		position := r.s.decodeTravelPosition(&v.blocks, saved, frame)
		link := platoonLink{lane: saved.Platoon.Lane, leaderLane: saved.Platoon.LeaderLane, lanes: saved.Platoon.Lanes, turn: saved.Platoon.Turn}
		if through, ok := r.savedFrontier(v, saved.ClaimsDestination, saved.ReservedAhead, position.leadBase, frame); ok && through >= r.s.linkEnd(v, link) {
			r.projected[index] = true
		}
	}
}

// travelPlan is a placement of a traveling pod that
// commitTravelPlacement installs.
type travelPlan struct {
	lane         int
	laneDistance float64
	distance     float64
	// block is the block that the pod occupies, and through is its
	// reservation frontier.
	block, through int
	speed          float64
	footprint      []resource
	// leader is the index of the predecessor of the pod, or -1 with no
	// link.
	leader       int
	link         platoonLink
	latch        reversal
	latchDropped bool
	// pending is the block of the request that the pod waits for, or -1.
	pending int
}

// planTravelPlacement plans the placement of the traveling pod at index
// with input. It derives the reservations of the pod from its saved
// position and its saved frontier (see savedFrontier), on copies of the
// pod and of its predecessor, against the placed pods and their claims,
// in the frame of input. It writes nothing to the
// pod, to another pod, or to the restore. leader is the index of its
// placed predecessor, or -1. With a predecessor, the pod links to it with
// the saved link. It reports false when a saved value is not valid, when
// another pod holds a resource that the pod needs, or when the pod cannot
// stop within its reservation (see stopsInReservation). A resource that a
// pod ahead in the platoon holds is not in the way in a block that the pod
// can reserve as a platoon member. It returns an error when the pod holds
// a resource of a debris footprint, or when the pods or their stop points
// are closer than the clearance of the saved link.
func (r *physicalRestore) planTravelPlacement(index, leader int, input placementInput) (travelPlan, bool, error) {
	// A lookup writes the scan hints of the blocks that it reads, so the
	// plan reads only copies of the pod and of its predecessor.
	candidate, saved := r.s.vehicles[index], r.state.Pods[index]
	c := &candidate
	if saved.RouteIndex < 0 || saved.RouteIndex >= len(c.Route) {
		return travelPlan{}, false, nil
	}
	if saved.LaneID != "" && saved.LaneID != c.Route[saved.RouteIndex].ID {
		return travelPlan{}, false, nil
	}
	position := r.s.decodeTravelPosition(&c.blocks, saved, input.frame)
	through, ok := r.savedFrontier(c, saved.ClaimsDestination, input.lead, position.leadBase, input.frame)
	if !ok {
		return travelPlan{}, false, nil
	}
	plan := travelPlan{
		lane: position.lane, laneDistance: position.laneDistance, distance: position.distance,
		block: position.leadBase, through: through, speed: input.speed, leader: -1, pending: -1,
	}
	plan.block = placedBlock(&c.blocks, position, through, input.frame)
	if plan.distance < c.originTail() && (c.origin.ID == "" || c.Route[0].From != c.origin.Node) {
		return travelPlan{}, false, nil
	}
	if c.destination.ID == "" && r.noBerthDemotes(c, through) {
		return travelPlan{}, false, nil
	}
	c.distance, c.blockIndex, c.Pod.Speed = plan.distance, plan.block, plan.speed
	plan.footprint = c.footprint(through, plan.distance)
	if slices.ContainsFunc(plan.footprint, func(claimed resource) bool { return r.s.owners[claimed].kind == faultOwnerKind }) {
		return travelPlan{}, false, fmt.Errorf("%w: the pod holds a resource of a debris footprint", errInvalidFaults)
	}
	var ahead *vehicle
	if leader >= 0 {
		predecessor := r.s.vehicles[leader]
		ahead = &predecessor
		link, err := r.savedLink(c, ahead, saved.Platoon)
		if err != nil {
			return travelPlan{}, false, err
		}
		link.draining = r.linkDraining(index, input, through, link.end)
		// An overspeed follower fails this plan before the stop-point check
		// of its link can fail the physical tier, and placeTravelingPod then
		// plans it at rest.
		if overLaneLimit(c, saved.RouteIndex) {
			return travelPlan{}, false, nil
		}
		if err := checkLinkStopPoints(c, ahead, link); err != nil {
			return travelPlan{}, false, err
		}
		if !r.linkClaims(c, ahead, link, through) {
			return travelPlan{}, false, nil
		}
		link.leader = leader + 1
		c.link, plan.leader, plan.link = link, leader, link
	} else if slices.ContainsFunc(plan.footprint, func(claimed resource) bool { return !r.s.owners[claimed].isZero() }) {
		return travelPlan{}, false, nil
	}
	plan.latch, plan.latchDropped = r.s.latchAgrees(&c.blocks, saved.RouteIndex, c.distance, c.Pod.Speed, c.blocks.end(through), input.latch)
	c.reversal = plan.latch
	if !r.stopsInReservation(c, ahead, saved.RouteIndex, through) {
		return travelPlan{}, false, nil
	}
	if saved.Waiting && through+1 < c.blocks.len() {
		plan.pending = through + 1
	}
	return plan, true, nil
}

// commitTravelPlacement installs plan, a placement of the traveling pod at
// index that planTravelPlacement found: its position, its link, its latch,
// and its claims.
func (r *physicalRestore) commitTravelPlacement(index int, plan travelPlan) {
	v, saved := &r.s.vehicles[index], r.state.Pods[index]
	v.distance, v.blockIndex, v.Pod.Speed = plan.distance, plan.block, plan.speed
	if plan.leader >= 0 {
		r.s.link(index, plan.leader, plan.link)
	}
	v.reversal, r.latchDropped[index] = plan.latch, plan.latchDropped
	for _, claimed := range plan.footprint {
		if r.s.owners[claimed].isZero() {
			r.s.owners[claimed] = podResourceOwner(v.Pod.ID)
		}
	}
	for _, b := range v.blocks.span(0, plan.through+1) {
		for _, claimed := range b.resources {
			if release := resourceReleaseDistance(b, claimed); release > plan.distance {
				v.retainRouteResource(claimed, release)
			}
		}
	}
	v.reservedThrough = plan.through
	v.originReleased = plan.distance >= v.originTail()
	lane := v.Route[plan.lane]
	v.Pod.LaneDistance = plan.laneDistance
	v.Pod.Position = r.s.position(lane, plan.laneDistance)
	v.Pod.LaneID = lane.ID
	if plan.pending >= 0 {
		v.pending, v.waitSince = plan.pending, saved.WaitSince
	}
}

// noBerthDemotes reports whether a pod with no berth and a reservation through
// the block moves to a berth. A pod with a passenger route chooses a berth when
// its next request starts on the final lane of its route. Until that choice
// succeeds, the pod can hold cells of the final lane, so it keeps its place, and
// its next request tries again. With no block left to reserve, it makes no more
// requests, so it never chooses. A pod that does not choose never gets a berth,
// so it does not keep a reservation into the final lane. A pod that goes to a
// depot entry binds its slot when it owns the last cell of the incoming lane, so
// it stays, and the next bindDepotArrivals binds it.
func (r *physicalRestore) noBerthDemotes(v *vehicle, through int) bool {
	if r.s.depotEntryWait(v) {
		return false
	}
	if through >= v.blocks.len()-1 {
		return true
	}
	lastLane, _ := routeLaneBlocks(&v.blocks, len(v.Route)-1)
	return through >= lastLane && !choosesBerth(v, r.savedAssigned)
}

// savedAssigned reports whether a saved waiting trip names the pod.
func (r *physicalRestore) savedAssigned(podID string) bool {
	return slices.ContainsFunc(r.state.Waiting, func(trip SavedTrip) bool { return trip.Request.PodID == podID })
}

// savedLink returns the link of the traveling pod v to its placed
// predecessor ahead, from the saved link saved. The follower must be at
// least the clearance of the link behind the predecessor (see also
// checkLinkStopPoints).
func (r *physicalRestore) savedLink(v, ahead *vehicle, saved *SavedPlatoonLink) (platoonLink, error) {
	link := platoonLink{
		lane: saved.Lane, leaderLane: saved.LeaderLane, lanes: saved.Lanes,
		turn: saved.Turn, clearance: linkClearance(saved.Turn), draining: saved.Draining,
	}
	link.end = r.s.linkEnd(v, link)
	if gap := leaderPosition(v, ahead, link) - v.distance; gap < link.clearance-3*restoreTolerance {
		return platoonLink{}, fmt.Errorf("the pod is %.6f m behind its platoon predecessor, less than the clearance %.6f m", gap, link.clearance)
	}
	return link, nil
}

// checkLinkStopPoints returns an error when the stop point of the restored
// follower v is less than the clearance of link behind the stop point of
// its predecessor ahead, as when a link forms, so that its cap is not
// behind its stop point.
func checkLinkStopPoints(v, ahead *vehicle, link platoonLink) error {
	stop := leaderPosition(v, ahead, link) + stoppingDistance(ahead.Pod.Speed) - (v.distance + stoppingDistance(v.Pod.Speed))
	if stop < link.clearance-3*restoreTolerance {
		return fmt.Errorf("the stop point of the pod is %.6f m behind the stop point of its platoon predecessor, less than the clearance %.6f m", stop, link.clearance)
	}
	return nil
}

// linkClaims reports whether the restored follower v can claim the blocks up
// to through with link to its placed predecessor leader. Another pod can hold a resource of these blocks only
// when it is ahead of v in its platoon, the resource is not a berth, the
// block is in the run and not after the end block of the link, and the
// predecessor reserved the block. A linked grant has the same rules.
func (r *physicalRestore) linkClaims(v, leader *vehicle, link platoonLink, through int) bool {
	shared := false
	for block, b := range v.blocks.span(0, through+1) {
		for _, claimed := range b.resources {
			owner := r.s.owners[claimed]
			if owner.isZero() || resourceReleaseDistance(b, claimed) <= v.distance {
				continue
			}
			if block < v.blocks.laneFirst(link.lane) || claimed.kind == berthResource ||
				!owner.isPod(leader.Pod.ID) && !r.s.ownerAheadInPlatoon(leader, owner) {
				return false
			}
			shared = true
		}
	}
	return !shared || through <= link.end && leaderBlock(v, leader, link, through) <= leader.reservedThrough
}

// latchAgrees reports whether the saved reversal dwell latch agrees with
// the restored position of a traveling pod on blocks: at the route
// distance in the lane at route index lane, at speed, with its frontier
// at the route distance limit. Live motion keeps these rules (see
// reversalLimit). A latched lane k stops the pod at the start of lane k,
// so the pod is not past that start. A pod counts dwell ticks only at rest
// at that start. The pod latched k with the search of reversalLimit. A
// frontier does not move back, and the search latches the lane after a
// done lane only in a tick in which the pod can move on from the start of
// the done lane. So the search with no done lane, from the restored
// position and with limit, finds k. A pod that a platoon cap or a fault
// holds at that start can have such a latch, and the placement drops it,
// so the pod dwells there again. The dwell at a done lane ended at the
// start of that lane, so the pod is not short of it. The comparisons are
// exact in the restored frame. A trimmed
// route restores in its own frame, where the sums of lane lengths can
// differ in the last bits, so a latch at a lane start of such a route can
// fail them. The placement then drops the latch, and the dwell
// starts again, as before the saved latch. checkSavedReversals checked
// the lane against the saved route. An empty latch agrees. latchAgrees
// returns the latch of the placement: latch when it agrees, and an empty
// latch with dropped true when it does not.
func (s *Simulation) latchAgrees(blocks *blockList, lane int, distance, speed, limit float64, latch reversal) (selected reversal, dropped bool) {
	if !s.latchFits(blocks, lane, distance, speed, limit, latch) {
		return reversal{}, true
	}
	return latch, false
}

// latchFits reports whether latch agrees with the placement (see
// latchAgrees).
func (s *Simulation) latchFits(blocks *blockList, lane int, distance, speed, limit float64, latch reversal) bool {
	k := max(latch.lane, -latch.lane)
	if k == 0 {
		return true
	}
	point := blocks.lanes[k].start
	switch {
	case latch.lane < 0:
		return distance >= point
	case distance > point, latch.ticks > 0 && (distance < point || speed > 0):
		return false
	}
	return s.nextReversal(blocks, lane, distance, 0, limit) == k
}

// placedBlock returns the block in which a restore places a traveling pod
// at position on blocks, with the reservation frontier through, in frame:
// the tied block in an exact frame, and otherwise the lead base block. The
// export checks its frame with it (see savedMotion).
func placedBlock(blocks *blockList, position travelPosition, through int, frame positionFrame) int {
	if frame.exact {
		return tiedBlock(blocks, position.lane, position.leadBase, through, position.distance)
	}
	return position.leadBase
}

// tiedBlock returns the block that a restored traveling pod at distance in
// the lane at route index lane occupies, with the reservation frontier
// through. current is the lead base block of the decoded position (see
// travelPosition), the first block that ends at or after the distance.
// Live motion moves a pod past the end of a block when the pod holds the
// next block (see publishVehicleTravel). So when the distance is the exact
// end of a block before the last block of the lane, the pod is in the next
// block if through holds it. Only the exact frame keeps the exact distance
// of the live pod. The placement picks the occupied block after it knows
// the frontier, and the frontier of the export and of the restore counts
// from the lead base block (see savedMotion), so the frontier does not
// change.
func tiedBlock(blocks *blockList, lane, current, through int, distance float64) int {
	if _, last := routeLaneBlocks(blocks, lane); current < last && current < through && blocks.peekEnd(current) == distance {
		return current + 1
	}
	return current
}

type restoredDistanceInput struct {
	blocks       *blockList
	first, last  int
	laneDistance float64
	saved        float64
	// snap is the largest distance from a block end that snaps to it.
	snap float64
}

// restoredDistance returns the route distance of a pod at laneDistance in the
// lane of blocks first to last. The saved route can start at a later lane
// than the live route, so its sums of lane lengths can differ in the last
// bits. The saved distance within restoreTolerance and a block end within
// input.snap therefore win. A pod that stopped at a block end then
// releases the same resources as the live pod. The snap is
// restoreDriftTolerance, and restoreTolerance in the restore before the
// saved motion (see restoreBeforeMotion).
//
// The block end is the first one, from the last block of the lane before
// and in order, within input.snap. The block ends increase, and so does
// end-distance in floating point. The first end with end-distance of at
// least -input.snap is thus the only possible first match, and a binary
// search finds it. The export calls this for each pod under the session
// lock, and a lane can have thousands of blocks.
func restoredDistance(input restoredDistanceInput) float64 {
	distance := input.blocks.at(input.first).laneStart + input.laneDistance
	if math.Abs(distance-input.saved) <= restoreTolerance {
		distance = input.saved
	}
	from := max(0, input.first-1)
	found := from + sort.Search(input.last+1-from, func(offset int) bool {
		return input.blocks.peekEnd(from+offset)-distance >= -input.snap
	})
	if found <= input.last {
		if end := input.blocks.peekEnd(found); math.Abs(end-distance) <= input.snap {
			return end
		}
	}
	return distance
}

// peekEnd returns the route distance at the end of the block at index. It
// writes nothing to the list, as peekSpan.
func (l *blockList) peekEnd(index int) float64 {
	lane := l.locate(index, l.scan)
	return l.cellEnd(lane, index-l.lanes[lane].first)
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
// distance. Before the pod passes its origin retention tail, it also holds the
// origin berth and node. A set finds the repeated resources, because a
// saved route can reserve many blocks. It writes no block cursor, so a
// refused fault start can read the footprint of a pod fault.
func (v *vehicle) footprint(through int, distance float64) []resource {
	var held []resource
	seen := make(map[resource]bool)
	add := func(claimed resource) {
		if !seen[claimed] {
			seen[claimed] = true
			held = append(held, claimed)
		}
	}
	for _, b := range v.blocks.peekSpan(0, through+1) {
		for _, claimed := range b.resources {
			if resourceReleaseDistance(b, claimed) > distance {
				add(claimed)
			}
		}
	}
	if distance < v.originTail() {
		for _, claimed := range berthResources(v.origin) {
			add(claimed)
		}
	}
	return held
}

// claimDestinations gives each relocating pod the destination claim that it
// held, when no other pod holds the berth or its node. Route admission still
// protects a berth without a claim. A pod bound to a depot slot holds the
// slot only through its restored reservation, so it gets no claim.
func (r *physicalRestore) claimDestinations() {
	for index, saved := range r.state.Pods {
		v := &r.s.vehicles[index]
		if r.demoted[index] || !saved.ClaimsDestination || v.RelocatingTo == "" || v.destination.ID == "" {
			continue
		}
		if _, slot := r.s.depots.slot(v.destination.ID); slot {
			continue
		}
		claims := berthResources(v.destination)
		if slices.ContainsFunc(claims[:], func(claimed resource) bool {
			owner := r.s.owners[claimed]
			return !owner.isZero() && !owner.isPod(v.Pod.ID)
		}) {
			continue
		}
		for _, claimed := range claims {
			r.s.owners[claimed] = podResourceOwner(v.Pod.ID)
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
// holds counts as free. A demoted pod loses its operational purpose. The
// marked riders of an emergency unload end interrupted, and its other
// riders return to the queue: they never board again. Passengers who all
// boarded at one berth before the pod moved (ridersAtOrigin) board again at
// an origin berth with a route as in board. Other passengers return to the
// request queue. When no such berth is free, when
// the route does not fit in the block budget, or when the stops from that
// berth take a rider over maxSharedRideDetour, the request goes back to the
// queue with its party count and the pod waits empty. An empty pod goes to
// the first free berth in this order: its destination, its origin, a berth
// of its destination station, then any berth. A pod placed at a depot slot
// parks at the restore tick.
func (r *physicalRestore) placeDemoted(index int) error {
	v := &r.s.vehicles[index]
	emergency := v.op.purpose == opEmergencyUnload
	if emergency {
		r.interruptMarked(v)
	}
	v.op = operationalDestination{}
	if emergency {
		r.requeue(v)
	}
	if v.RidersAboard() > 0 {
		from, _ := r.s.station(v.boardingStation())
		if berth, ok := r.freeBerth(v, from.Berths); v.ridersAtOrigin() && ok && r.boardAgain(v, berth) {
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
	v.replaceRoute(nil)
	v.blocks, v.routeLengths, v.blockStarts, v.terminal = blockList{}, nil, nil, terminalCheck{}
	v.RelocatingTo, v.Rebalancing, v.released = "", false, false
	v.origin, v.destination, v.destinationStation = Berth{}, berth, station
	if _, slot := r.s.depots.slot(berth.ID); slot {
		v.parkedSince = max(r.s.tick, 1)
	}
	return nil
}

// freeBerth returns the first of candidates that no other pod holds. A
// depot slot must also be open (see depotSlotOpen).
func (r *physicalRestore) freeBerth(v *vehicle, candidates []Berth) (Berth, bool) {
	for _, berth := range candidates {
		if berth.ID == "" || !r.s.berthAvailableTo(v, berth) || !r.s.depotSlotOpen(v, berth) {
			continue
		}
		claims := berthResources(berth)
		if !slices.ContainsFunc(claims[:], func(claimed resource) bool {
			owner := r.s.owners[claimed]
			return !owner.isZero() && !owner.isPod(v.Pod.ID)
		}) {
			return berth, true
		}
	}
	return Berth{}, false
}

// ridersAtOrigin reports whether each rider of pod v boarded at one berth
// before the pod moved, so that the riders can board again at their origin.
// A pod that boards a party in an onboard pickup does not board again.
func (v *vehicle) ridersAtOrigin() bool {
	if len(v.Boardings) != len(v.Riders) || v.Pod.Activity == Boarding && v.Pod.Occupied {
		return false
	}
	for _, record := range v.Boardings {
		if record.BerthID != v.Boardings[0].BerthID || record.MetersAtBoarding != 0 {
			return false
		}
	}
	return true
}

// boardAgain puts a pod with passengers at a berth of their origin, ready to
// depart. It returns false when the route does not exist or does not fit in
// the block budget. The berth becomes the boarding berth of the riders, so
// their direct distances change. Thus, when cappedDetours is true, it also
// returns false when the plan of the stops from the berth takes a rider
// over maxSharedRideDetour. The completed riders and their records leave
// the pod.
func (r *physicalRestore) boardAgain(v *vehicle, berth Berth) bool {
	route, err := r.s.stationApproachRouteForClass(berth.Node, v.Stops[0], v.Pod.Class)
	if err != nil {
		return false
	}
	riders := make([]Request, 0, len(v.Riders))
	boardings := make([]RiderBoarding, 0, len(v.Riders))
	for _, rider := range v.Riders {
		if !rider.Completed {
			rider.PodID, rider.DispatchReason = v.Pod.ID, ""
			riders = append(riders, rider)
			boardings = append(boardings, RiderBoarding{BerthID: berth.ID})
		}
	}
	station, _ := r.s.station(v.Stops[0])
	candidate := *v
	candidate.Riders, candidate.Boardings = riders, boardings
	start := detourStart{class: v.Pod.Class, from: berth.Node, ridden: r.s.lanesMeters(route), entry: station.routeEntry(route, Berth{})}
	if r.s.cappedDetours() && !r.s.keepsRiderDetours(&candidate, v.Stops, start) {
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
	v.Riders, v.Boardings, v.riddenBase = riders, boardings, 0
	v.Pod.Activity, v.Pod.StationID = Boarding, r.berths[berth.ID].station
	v.RelocatingTo, v.Rebalancing, v.released = "", false, false
	v.origin, v.destination, v.destinationStation = berth, Berth{}, v.Stops[0]
	r.s.setVehicleRoute(v, route)
	return true
}

// interruptMarked ends the marked riders of an emergency unload as
// interrupted at the restore (incident contract, section 9.6). The riders
// leave the pod with their aligned records. The restore reports them in
// RestoreResult.Interrupted, not through DrainInterruptions.
func (r *physicalRestore) interruptMarked(v *vehicle) {
	r.result.Interrupted = append(r.result.Interrupted, r.s.interruptMarkedRiders(v)...)
}

// interruptMarkedRiders removes each active rider of v that the interrupt
// set of v marks, and counts its order as interrupted. It returns the
// order IDs. It does not change undelivered.
func (s *Simulation) interruptMarkedRiders(v *vehicle) []int {
	var interrupted []int
	riders, boardings := v.Riders[:0:0], v.Boardings[:0:0]
	for index, rider := range v.Riders {
		if !rider.Completed && v.op.interrupt&(1<<index) != 0 {
			s.interrupted++
			s.interruptedPassengers += rider.PartySize
			interrupted = append(interrupted, rider.ID)
			continue
		}
		riders = append(riders, rider)
		boardings = append(boardings, v.Boardings[index])
	}
	v.Riders, v.Boardings = riders, boardings
	return interrupted
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
	if len(v.Boardings) > 0 {
		v.riddenBase = 0
	}
	v.Riders, v.Stops, v.Boardings = nil, nil, nil
}

// requeuedTrip returns the queued trip for a rider of a pod. The trip is
// boarded, so board and joinSharedRide do not record the boarding again.
func requeuedTrip(rider Request) waitingTrip {
	rider.PodID, rider.DispatchReason = "", ""
	return waitingTrip{request: rider, boarded: true}
}

// moveTo puts a pod at rest at a berth. The pod releases each other resource.
func (r *physicalRestore) moveTo(v *vehicle, berth Berth) {
	maps.DeleteFunc(r.s.owners, func(_ resource, owner resourceOwner) bool { return owner.isPod(v.Pod.ID) })
	clear(v.routeReleases)
	for _, claimed := range berthResources(berth) {
		r.s.owners[claimed] = podResourceOwner(v.Pod.ID)
	}
	node, _ := r.s.network.Node(berth.Node)
	v.Pod = Pod{ID: v.Pod.ID, Class: v.Pod.Class, Position: node.Position, BerthID: berth.ID}
	v.phaseTicks, v.blockIndex, v.reservedThrough, v.pending = 0, 0, -1, -1
	v.distance, v.originReleased = 0, false
}

// restoreWaiting rebuilds the queue in saved order. A requeued request goes
// in before the first saved trip with a larger ID. The restore drops a saved
// request that is not valid. It clears the pod
// bindings of a trip when one of them is not valid, and keeps a deferral
// deadline that is in range. When the restore drops a trip or clears its
// pod, and no kept trip names that pod, the empty pod on its way to the
// pickup has no order. The restore releases it as dispatch does, so that it
// can take new work at once.
func (r *physicalRestore) restoreWaiting() {
	s := r.s
	var orphaned []string
	requeued := slices.Clone(r.requeued)
	slices.SortStableFunc(requeued, func(a, b waitingTrip) int { return cmp.Compare(a.request.ID, b.request.ID) })
	for index, saved := range r.state.Waiting {
		for len(requeued) > 0 && requeued[0].request.ID < saved.Request.ID {
			s.waiting = append(s.waiting, requeued[0])
			requeued = requeued[1:]
		}
		request := Request(saved.Request)
		if !r.state.validTrip(saved.Request, saved.Boarded) ||
			!s.passengerStation(request.From) || !s.passengerStation(request.To) || !s.optionalPassengerStation(request.LegFrom) {
			r.result.Dropped = append(r.result.Dropped, request.ID)
			r.result.DroppedParties++
			orphaned = append(orphaned, request.PodID)
			continue
		}
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
		deferUntil: saved.DeferUntil, deferCheck: saved.DeferCheck, deferPodID: saved.DeferPodID, excludedPod: saved.ExcludedPod,
	}
	unbound := r.unbound[index] || !r.activePod(request.PodID) || !r.activePod(trip.deferPodID) ||
		trip.deferCheck < 0 || trip.deferCheck > r.s.tick+TicksPerSecond
	if v := r.s.findVehicle(request.PodID); v != nil && !r.s.podFitsRequest(v, request) {
		unbound = true
	}
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
// each resource owner, the orders of state, and the contract. completed,
// interrupted, and dropped list the orders that the restore completed,
// interrupted, and dropped. unaccounted counts the unaccounted orders of
// state. The dropped orders add to them.
func (s *Simulation) verifyRestore(state SavedState, completed, interrupted, dropped []int, unaccounted int) error {
	for index := range s.vehicles {
		s.updateStationPhase(&s.vehicles[index])
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		return fmt.Errorf("check the restored pods: %w", err)
	}
	if !maps.Equal(s.owners, s.retainedOwners()) {
		return errors.New("the resource owners differ from the retention rules")
	}
	if err := s.reconcileOrders(state, completed, interrupted, dropped); err != nil {
		return fmt.Errorf("check the restored orders: %w", err)
	}
	s.unaccountedOrders = unaccounted + len(dropped)
	if err := s.CheckContract(); err != nil {
		return fmt.Errorf("check the restored state: %w", err)
	}
	return nil
}
