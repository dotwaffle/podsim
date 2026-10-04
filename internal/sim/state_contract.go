package sim

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// podPhase is a case of the phase contract of a saved pod. The contract has
// more cases than the activity codes. A traveling pod can be empty or carry
// riders, and an unloading pod can be at its last stop or at an
// intermediate stop.
type podPhase int

const (
	phaseIdle podPhase = iota
	phaseDepartingEmpty
	phaseBoarding
	phaseTravelingEmpty
	phaseTravelingOccupied
	phaseUnloadingFinal
	phaseUnloadingIntermediate
	phaseContinuing
)

// stopRule tells which stops a phase needs.
type stopRule int

const (
	// noStops forbids stops.
	noStops stopRule = iota
	// routeStops needs one stop for each destination of the active riders,
	// with the destination station of the pod first.
	routeStops
	// finalStop forbids stops, and each active rider goes to the station of
	// the pod.
	finalStop
	// laterStops needs one stop for each destination of the active riders
	// other than the station of the pod. Each active rider goes to the
	// station of the pod or to a stop.
	laterStops
)

// phaseRule holds the fields that a phase needs and the fields that it
// forbids. The zero value of each flag forbids the field or the value.
type phaseRule struct {
	// active needs at least one active rider. Otherwise the phase forbids
	// active riders.
	active bool
	// history allows completed riders.
	history bool
	// occupied needs Occupied. Otherwise the phase forbids it.
	occupied bool
	// atBerth needs a station and a berth. Otherwise the phase forbids them.
	atBerth bool
	// relocating needs RelocatingTo, equal to DestinationStation. It allows
	// the rebalancing flag and the destination claim. Otherwise the phase
	// forbids each of them.
	relocating bool
	// minPhase and maxPhase bound PhaseTicks.
	minPhase, maxPhase int
	stops              stopRule
	// startsAtBerth needs Origin equal to BerthID.
	startsAtBerth bool
	// hasOrigin needs Origin.
	hasOrigin bool
	// boardsHere needs each rider to board at the station of the pod, with
	// the journey origin at the berth of the pod.
	boardsHere bool
	// atDestination needs Destination equal to BerthID, and
	// DestinationStation equal to StationID.
	atDestination bool
	// hasDestination needs Destination.
	hasDestination bool
}

// phaseRules is the phase contract. Both restore tiers check each saved pod
// against the rule of its phase before they use the pod. A live simulation
// must meet the contract after each tick and each command.
var phaseRules = [...]phaseRule{
	phaseIdle:           {history: true, atBerth: true},
	phaseDepartingEmpty: {history: true, atBerth: true, relocating: true, startsAtBerth: true, hasDestination: true},
	phaseBoarding: {
		active: true, atBerth: true, maxPhase: boardingTicks, stops: routeStops, startsAtBerth: true, boardsHere: true,
	},
	phaseTravelingEmpty:    {history: true, relocating: true, hasOrigin: true},
	phaseTravelingOccupied: {active: true, history: true, occupied: true, stops: routeStops, hasOrigin: true},
	phaseUnloadingFinal: {
		active: true, history: true, occupied: true, atBerth: true, minPhase: 1, maxPhase: unloadingTicks,
		stops: finalStop, atDestination: true,
	},
	// A pod that finished unloading at an intermediate stop and has no route
	// to its next stop stays unloading with a zero phase, and tries again.
	phaseUnloadingIntermediate: {
		active: true, history: true, occupied: true, atBerth: true, maxPhase: unloadingTicks, stops: laterStops,
		atDestination: true,
	},
	phaseContinuing: {active: true, history: true, occupied: true, atBerth: true, stops: routeStops, startsAtBerth: true},
}

// phaseOf returns the phase of a saved pod. It uses the activity, the
// Occupied flag of a traveling pod, and the stops of an unloading pod. The
// rule of the phase then checks the other fields.
func phaseOf(pod SavedPod) (podPhase, error) {
	activity, ok := activityOfCode(pod.Activity)
	if !ok {
		return 0, fmt.Errorf("unknown activity %q", pod.Activity)
	}
	switch activity {
	case DepartingEmpty:
		return phaseDepartingEmpty, nil
	case Boarding:
		return phaseBoarding, nil
	case Traveling:
		if pod.Occupied {
			return phaseTravelingOccupied, nil
		}
		return phaseTravelingEmpty, nil
	case Unloading:
		if len(pod.Stops) > 0 {
			return phaseUnloadingIntermediate, nil
		}
		return phaseUnloadingFinal, nil
	case Continuing:
		return phaseContinuing, nil
	default:
		return phaseIdle, nil
	}
}

// ruleForPod retains the boarding phase while allowing an accepted occupied pickup.
func ruleForPod(pod SavedPod, phase podPhase) phaseRule {
	if phase == phaseBoarding && pod.Occupied {
		return phaseRule{active: true, history: true, occupied: true, atBerth: true, maxPhase: boardingTicks, stops: routeStops, startsAtBerth: true}
	}
	return phaseRules[phase]
}

// savedRiders splits the riders of a saved pod into the active riders,
// which did not leave the pod, and the completed history. It does not read
// the activity or the flags of the pod.
func savedRiders(pod SavedPod) (active, history []SavedRequest) {
	for _, rider := range pod.Riders {
		if rider.Completed {
			history = append(history, rider)
		} else {
			active = append(active, rider)
		}
	}
	return active, history
}

// checkContract checks the rules of a saved state that do not need the
// network: the counters, the phase contract of each pod, and the identity
// of each order. It returns the unaccounted orders: the orders that the
// state submitted but that are not complete, not in the queue and not
// aboard a pod. A live simulation has none.
func (state SavedState) checkContract() (int, error) {
	if err := ValidateOrderContract(state.OrderContract); err != nil {
		return 0, err
	}
	if state.OrderContract == ExpressOrderContract && len(state.Waiting) > MaxExpressWaitingTrips {
		return 0, errors.New("too many saved waiting trips")
	}
	if err := state.checkContractRoutes(); err != nil {
		return 0, err
	}
	if len(state.Pods) == 0 || len(state.Pods) > maxSavedPods {
		return 0, fmt.Errorf("the saved state has %d pods, want 1 to %d", len(state.Pods), maxSavedPods)
	}
	if err := checkLargeLinkFields(RestoreStateInput{State: state}); err != nil {
		return 0, err
	}
	if err := state.validateCounters(); err != nil {
		return 0, err
	}
	// orders holds where each order ID is: in the queue, or with a pod.
	orders := make(map[int]string)
	use := func(id int, where string) error {
		if other, ok := orders[id]; ok {
			return fmt.Errorf("order %d is %s and %s", id, other, where)
		}
		orders[id] = where
		return nil
	}
	held := 0
	for index, pod := range state.Pods {
		if pod.ID == "" || index > 0 && pod.ID <= state.Pods[index-1].ID {
			return 0, fmt.Errorf("saved pod %q is empty or out of order", pod.ID)
		}
		if err := state.checkPod(pod); err != nil {
			return 0, fmt.Errorf("pod %s: %w", pod.ID, err)
		}
		for _, rider := range pod.Riders {
			if err := use(rider.ID, "in pod "+pod.ID); err != nil {
				return 0, err
			}
			if !rider.Completed {
				held++
			}
		}
	}
	// A queued order that is not valid does not stop the restore. Each tier
	// drops it and reports it. See validTrip.
	for _, trip := range state.Waiting {
		if !validSavedOptionsWithOrderContract(trip.Request, state.OrderContract) || trip.Request.SharingConsent == LegacyUnknownConsent {
			return 0, errors.New("pending order lacks valid effective options")
		}
		if err := use(trip.Request.ID, "in the queue"); err != nil {
			return 0, err
		}
		held++
	}
	if state.OrderContract == ExpressOrderContract && held > MaxExpressWaitingTrips {
		return 0, errors.New("too many outstanding Express orders")
	}
	unaccounted := state.RequestID - state.Completed - held
	if unaccounted < 0 {
		return 0, fmt.Errorf("the saved state holds %d more orders than it submitted", -unaccounted)
	}
	return unaccounted, nil
}

// validTrip reports whether a queued order is valid. A queued order is not
// complete, and its two stations differ. It has a boarding time only when a
// restore queued it again after it boarded, and then the boarding time is
// not before the request time.
func (state SavedState) validTrip(request SavedRequest, boarded bool) bool {
	return state.validRequest(request) && request.From != request.To && !request.Completed &&
		(boarded && request.BoardedTick >= request.RequestedTick || !boarded && request.BoardedTick == 0)
}

// checkPod checks a saved pod against the rule of its phase.
func (state SavedState) checkPod(pod SavedPod) error {
	if err := checkSavedBoardingsWithOrderContract(pod, state.OrderContract); err != nil {
		return err
	}
	phase, err := phaseOf(pod)
	if err != nil {
		return err
	}
	rule := ruleForPod(pod, phase)
	active, history := savedRiders(pod)
	if err := checkSavedAdmissionWithOrderContract(pod, active, state.OrderContract); err != nil {
		return err
	}
	if err := state.checkPodRiders(pod, rule, active, history); err != nil {
		return err
	}
	flags := pod
	// Committed passengers can retain an individual receiving claim.
	// Validate that flag before checking the ordinary phase flags.
	if pod.ClaimsDestination && phase == phaseTravelingOccupied &&
		state.CouplingContract == CompactPairV1CouplingContract && state.couplingMember(pod.ID) {
		if pod.Destination == "" {
			return errors.New("committed receiving claim lacks a berth")
		}
		flags.ClaimsDestination = false
	}
	if err := checkPodFlags(flags, rule); err != nil {
		return err
	}
	if err := checkPodPlace(pod, rule); err != nil {
		return err
	}
	return checkPodStops(pod, rule, active, history)
}

// checkPodRiders checks the riders of a pod. The riders of a pod boarded at
// one station unless boarding records prove their origins. Each names the pod.
func (state SavedState) checkPodRiders(pod SavedPod, rule phaseRule, active, history []SavedRequest) error {
	switch {
	case len(pod.Riders) > MaxStoredRidersForOrderContract(pod.Class, state.OrderContract) || len(pod.Stops) > MaxSharedRideParties:
		return fmt.Errorf("the pod has %d riders and %d stops", len(pod.Riders), len(pod.Stops))
	case rule.active && len(active) == 0:
		return errors.New("the pod has no active rider")
	case !rule.active && len(active) > 0:
		return fmt.Errorf("the pod has active rider %d", active[0].ID)
	case !rule.history && len(history) > 0:
		return fmt.Errorf("the pod has completed rider %d", history[0].ID)
	}
	for _, rider := range pod.Riders {
		if !state.validRequest(rider) || rider.From == rider.To || rider.BoardedTick < rider.RequestedTick {
			return fmt.Errorf("rider %d is not valid", rider.ID)
		}
		if rider.PodID != pod.ID || len(pod.Boardings) == 0 && rider.From != pod.Riders[0].From {
			return fmt.Errorf("rider %d is not a rider of this journey", rider.ID)
		}
		if rule.boardsHere && rider.From != pod.StationID {
			return fmt.Errorf("rider %d does not board at the station of the pod", rider.ID)
		}
	}
	return nil
}

// checkPodFlags checks the occupancy, the phase count, the relocation and
// the numbers of a pod.
func checkPodFlags(pod SavedPod, rule phaseRule) error {
	// A restore ignores the released flag of a pod that dispatch cannot
	// release, so the contract does not check it.
	relocation := pod.RelocatingTo != "" || pod.Rebalancing || pod.ClaimsDestination
	switch {
	case pod.StationBuffered && (pod.Destination != "" || pod.Rebalancing ||
		pod.Activity != "traveling" && pod.Activity != "boarding" && pod.Activity != "continuing" && pod.Activity != "departing"):
		return errors.New("the station buffer flag does not agree with the phase")
	case pod.Occupied != rule.occupied:
		return fmt.Errorf("occupied is %t", pod.Occupied)
	case pod.PhaseTicks < rule.minPhase || pod.PhaseTicks > rule.maxPhase:
		return fmt.Errorf("phase %d is out of range", pod.PhaseTicks)
	case !rule.relocating && relocation:
		return errors.New("the pod has an empty move")
	case rule.relocating && (pod.RelocatingTo == "" || pod.RelocatingTo != pod.DestinationStation):
		return errors.New("the empty move does not go to the destination station")
	case pod.ClaimsDestination && pod.Destination == "":
		return errors.New("the empty move flags do not agree")
	case pod.RebalanceAfter < 0 || !finite(pod.RiddenMeters) || pod.RiddenMeters < 0:
		return errors.New("a value is negative or not finite")
	default:
		return nil
	}
}

// checkPodPlace checks the station, the berth, the origin and the
// destination of a pod. checkPodRiders checks the station of the riders.
func checkPodPlace(pod SavedPod, rule phaseRule) error {
	journeyOrigin := cmp.Or(pod.JourneyOrigin, pod.Origin)
	switch {
	case rule.atBerth && (pod.StationID == "" || pod.BerthID == ""):
		return errors.New("the pod is not at a berth")
	case !rule.atBerth && (pod.StationID != "" || pod.BerthID != ""):
		return errors.New("a moving pod is at a berth")
	case rule.startsAtBerth && pod.Origin != pod.BerthID:
		return errors.New("the route does not start at the berth of the pod")
	case rule.hasOrigin && pod.Origin == "":
		return errors.New("the pod has no origin")
	case rule.boardsHere && journeyOrigin != pod.BerthID:
		return errors.New("the journey does not start at the berth of the pod")
	case rule.active && len(pod.Boardings) == 0 && journeyOrigin == "":
		return errors.New("the journey has no origin")
	case rule.atDestination && (pod.Destination != pod.BerthID || pod.DestinationStation != pod.StationID):
		return errors.New("the unloading pod is not at its destination")
	case rule.hasDestination && pod.Destination == "" && !pod.StationBuffered:
		return errors.New("the pod has no destination berth")
	default:
		return nil
	}
}

// checkPodStops checks the stops of a pod against the destinations of its
// active riders. Without boarding records, completed riders cannot have later stops.
func checkPodStops(pod SavedPod, rule phaseRule, active, history []SavedRequest) error {
	for index, stop := range pod.Stops {
		if stop == "" || slices.Contains(pod.Stops[:index], stop) {
			return fmt.Errorf("stop %q is not valid", stop)
		}
	}
	for _, rider := range history {
		if len(pod.Boardings) == 0 && slices.Contains(pod.Stops, rider.To) {
			return fmt.Errorf("completed rider %d has a stop", rider.ID)
		}
	}
	// wanted holds the stops that the active riders need.
	var wanted []string
	for _, rider := range active {
		if rule.stops == laterStops && rider.To == pod.StationID {
			continue
		}
		if rule.stops == finalStop && rider.To != pod.StationID {
			return fmt.Errorf("rider %d does not leave the pod at its station", rider.ID)
		}
		if !slices.Contains(wanted, rider.To) {
			wanted = append(wanted, rider.To)
		}
	}
	switch rule.stops {
	case noStops, finalStop:
		if len(pod.Stops) > 0 {
			return errors.New("the pod has stops")
		}
	case routeStops, laterStops:
		if len(pod.Stops) != len(wanted) || slices.ContainsFunc(wanted, func(to string) bool { return !slices.Contains(pod.Stops, to) }) {
			return errors.New("the stops are not the destinations of the riders")
		}
		if rule.stops == laterStops && slices.Contains(pod.Stops, pod.StationID) {
			return errors.New("the pod stops again at its station")
		}
		if rule.stops == routeStops && pod.Stops[0] != pod.DestinationStation {
			return errors.New("the first stop is not the destination")
		}
	}
	return nil
}

// CheckContract checks the saved form of a simulation against the phase
// contract and the order count that a restore checks. The saved form must
// have the unaccounted orders that the simulation counts. A live
// simulation meets the contract after each tick and each command. Tests
// call CheckContract to check a run.
func (s *Simulation) CheckContract() error {
	unaccounted, err := s.ExportState().checkContract()
	if err != nil {
		return err
	}
	if unaccounted != s.unaccountedOrders {
		return fmt.Errorf("the state has %d unaccounted orders, want %d", unaccounted, s.unaccountedOrders)
	}
	return nil
}

// observe runs the monitor of a simulation.
func (s *Simulation) observe() {
	if s.monitor != nil {
		s.monitor(s)
	}
}

// reconcileOrders checks the orders of a restored simulation against its
// saved state, by order ID. Each order that the saved state queues or that a
// saved pod carries is in one place after the restore: in the queue, aboard
// a pod, in completed or in dropped. A requeued order is in the queue. The
// restore adds no order, keeps the submitted count, and adds completed to
// the completed count.
func (s *Simulation) reconcileOrders(state SavedState, completed, dropped []int) error {
	saved := make(map[int]bool, len(state.Waiting))
	for _, pod := range state.Pods {
		active, _ := savedRiders(pod)
		for _, rider := range active {
			saved[rider.ID] = true
		}
	}
	for _, trip := range state.Waiting {
		saved[trip.Request.ID] = true
	}
	// found holds the place of each order after the restore.
	found := make(map[int]string, len(saved))
	place := func(id int, where string) error {
		if !saved[id] {
			return fmt.Errorf("order %d is %s, but the saved state does not hold it", id, where)
		}
		if other, ok := found[id]; ok {
			return fmt.Errorf("order %d is %s and %s", id, other, where)
		}
		found[id] = where
		return nil
	}
	for _, trip := range s.waiting {
		if err := place(trip.request.ID, "in the queue"); err != nil {
			return err
		}
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		for _, rider := range v.Riders {
			if rider.Completed {
				continue
			}
			if err := place(rider.ID, "in pod "+v.Pod.ID); err != nil {
				return err
			}
		}
	}
	for _, id := range completed {
		if err := place(id, "complete"); err != nil {
			return err
		}
	}
	for _, id := range dropped {
		if err := place(id, "dropped"); err != nil {
			return err
		}
	}
	for _, id := range slices.Sorted(maps.Keys(saved)) {
		if _, ok := found[id]; !ok {
			return fmt.Errorf("the restore lost order %d", id)
		}
	}
	if s.requestID != state.RequestID || s.completed != state.Completed+len(completed) {
		return fmt.Errorf("the restore has %d submitted and %d completed orders, want %d and %d",
			s.requestID, s.completed, state.RequestID, state.Completed+len(completed))
	}
	return nil
}

// checkLargeLinkFields rejects large physical links before either restore tier.
// Both saved and fleet classes count, so a class mismatch cannot hide a link.
func checkLargeLinkFields(input RestoreStateInput) error {
	largeIDs := make(map[string]bool)
	for _, pod := range input.State.Pods {
		if largeVehicleClass(pod.Class) {
			largeIDs[pod.ID] = true
		}
	}
	for _, pod := range input.Fleet {
		if largeVehicleClass(pod.Class) {
			largeIDs[pod.ID] = true
		}
	}
	if len(largeIDs) == 0 {
		return nil
	}
	for _, pod := range input.State.Pods {
		if pod.Platoon != nil && (largeIDs[pod.ID] || largeIDs[pod.Platoon.Leader]) {
			return fmt.Errorf("pod %s: large vehicle classes cannot have platoon links", pod.ID)
		}
		if pod.CompactQueue == nil {
			continue
		}
		if largeIDs[pod.ID] {
			return fmt.Errorf("pod %s: large vehicle classes cannot have compact queue certificates", pod.ID)
		}
		for _, member := range pod.CompactQueue.Members {
			if largeIDs[member] {
				return fmt.Errorf("pod %s: compact queue includes large pod %s", pod.ID, member)
			}
		}
	}
	return nil
}
