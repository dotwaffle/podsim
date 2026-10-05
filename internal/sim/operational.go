package sim

import (
	"errors"
	"fmt"
	"slices"
)

// opPurpose is the reason for the physical destination of a pod (incident
// contract, section 9.1).
type opPurpose uint8

const (
	// opService is the default: the physical destination serves the stops
	// of the riders.
	opService opPurpose = iota
	// opEmergencyUnload unloads every active rider at the berth. A marked
	// rider is interrupted, an unmarked rider at its destination completes,
	// and each other rider is transferred.
	opEmergencyUnload
	// opRefuge holds at the berth with the riders aboard until
	// resumeFromRefuge.
	opRefuge
	// opEmptyRecovery takes an empty pod to the berth, where it becomes
	// idle.
	opEmptyRecovery
)

// name returns the name of a purpose in Vehicle.Operational. Service has
// the empty name.
func (purpose opPurpose) name() string {
	switch purpose {
	case opEmergencyUnload:
		return OperationalEmergencyUnload
	case opRefuge:
		return OperationalRefuge
	case opEmptyRecovery:
		return OperationalEmptyRecovery
	default:
		return ""
	}
}

// refugeHolding is the wait reason of a pod that holds at its refuge.
const refugeHolding WaitReason = "Holding at refuge"

// operationalDestination is the purpose of the physical destination of a
// pod. destination and destinationStation of the pod hold the place. Each
// transition that ends a purpose clears it and keeps the holds of the pod.
type operationalDestination struct {
	purpose opPurpose
	// owner is the hold that owns the purpose. Each purpose other than
	// service needs one held bit (invariant W5).
	owner serviceHold
	// interrupt marks, by index in Riders, the active riders whose orders
	// end interrupted at an emergency unload. Bit i is rider i. The rider
	// order does not change while a purpose is set, because only the
	// arrival action and evacuate remove riders, and both clear the purpose.
	interrupt uint32
}

// operationalTarget is the input of setOperationalDestination.
type operationalTarget struct {
	purpose   opPurpose
	owner     serviceHold
	interrupt uint32
	station   string
	berth     string
}

// holdsOwner reports whether owner is one known hold that v has.
func (v *vehicle) holdsOwner(owner serviceHold) bool {
	return oneServiceHold(owner) && v.withdrawn&owner != 0
}

// marksActiveRiders reports whether each bit of interrupt names an active
// rider of v.
func (v *vehicle) marksActiveRiders(interrupt uint32) bool {
	if len(v.Riders) < 32 && interrupt>>len(v.Riders) != 0 {
		return false
	}
	for index, rider := range v.Riders {
		if interrupt&(1<<index) != 0 && rider.Completed {
			return false
		}
	}
	return true
}

// operationalMember returns an error when v is a coupling, compact, or
// platoon member. The operations of section 9 refuse such a pod. Its
// caller waits until the pod leaves the group.
func (s *Simulation) operationalMember(v *vehicle) error {
	if v.couplingID != "" || s.couplingApproachMember(v.Pod.ID) || v.coupled() || s.compactGroup(v) != nil {
		return fmt.Errorf("pod %s is a member of a train, a compact queue, or a platoon", v.Pod.ID)
	}
	return nil
}

// rebindOperationalOwner moves the operational purpose of v to the hold to,
// which v also has. A recovery that outlives the cause that started it uses
// this transition before the policy of that cause calls restoreService. It
// refuses, and changes nothing, when v has no purpose, when to is not one
// held hold, or when to already owns the purpose.
func (s *Simulation) rebindOperationalOwner(v *vehicle, to serviceHold) error {
	switch {
	case v.op.purpose == opService:
		return fmt.Errorf("pod %s has no operational purpose", v.Pod.ID)
	case !v.holdsOwner(to):
		return fmt.Errorf("pod %s: service hold %#x is not one held hold", v.Pod.ID, to)
	case to == v.op.owner:
		return fmt.Errorf("pod %s: service hold %#x already owns the purpose", v.Pod.ID, to)
	}
	v.op.owner = to
	return nil
}

// setOperationalDestination gives a traveling or departing pod a new
// physical destination berth with a purpose (incident contract, section
// 9.3). The new route keeps the prefix of divertStart, so the distance,
// the block indexes, and the reserved span keep their meaning. The pod
// releases its revocable claims on the old destination berth, as redirect
// does. The stops of the riders do not change.
//
// An empty recovery also sets RelocatingTo, and it claims the new berth
// when no other pod holds it, as the restore of a relocation does. Route
// admission still protects a berth without a claim.
//
// It refuses, and changes nothing, when a precondition of section 9.3
// fails.
func (s *Simulation) setOperationalDestination(v *vehicle, to operationalTarget) error {
	if v.Pod.Activity != Traveling && v.Pod.Activity != DepartingEmpty {
		return fmt.Errorf("pod %s is not traveling or departing", v.Pod.ID)
	}
	prefix, from, ok := s.divertStart(v)
	if !ok {
		return fmt.Errorf("pod %s cannot divert", v.Pod.ID)
	}
	if err := s.checkOperationalTarget(v, to); err != nil {
		return fmt.Errorf("pod %s: %w", v.Pod.ID, err)
	}
	station, _ := s.station(to.station)
	berth, ok := station.berth(to.berth)
	if !ok || !berthAllows(station, berth, v.Pod.Class) {
		return fmt.Errorf("pod %s: berth %q of station %s does not allow the pod", v.Pod.ID, to.berth, station.ID)
	}
	suffix, err := s.assignedRoute(v, from, berth.Node)
	if err != nil {
		return fmt.Errorf("pod %s: no route to berth %s: %w", v.Pod.ID, berth.ID, err)
	}
	route := append(slices.Clone(v.Route[:prefix]), suffix...)
	if len(route) == 0 {
		return fmt.Errorf("pod %s is at berth %s", v.Pod.ID, berth.ID)
	}
	for _, r := range berthResources(v.destination) {
		if s.revocable(v, r) {
			s.releaseOwned(v, r)
		}
	}
	s.setVehicleRoute(v, route)
	v.destination, v.destinationStation = berth, station.ID
	v.op = operationalDestination{purpose: to.purpose, owner: to.owner, interrupt: to.interrupt}
	v.buffered, v.bufferBerth = false, ""
	if to.purpose == opEmptyRecovery {
		v.RelocatingTo, v.Rebalancing, v.released = station.ID, false, false
		if s.berthAvailableTo(v, berth) {
			for _, r := range berthResources(berth) {
				s.owners[r] = podResourceOwner(v.Pod.ID)
			}
		}
	}
	v.pending = -1
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
	return nil
}

// checkOperationalTarget checks the purpose, the owner, the interrupt set,
// and the station of a target for v.
func (s *Simulation) checkOperationalTarget(v *vehicle, to operationalTarget) error {
	station, ok := s.station(to.station)
	switch {
	case to.purpose == opService || to.purpose > opEmptyRecovery:
		return fmt.Errorf("purpose %d is not an operational purpose", to.purpose)
	case !v.holdsOwner(to.owner):
		return fmt.Errorf("service hold %#x is not one held hold", to.owner)
	case to.purpose != opEmptyRecovery && !v.carriesPassengers():
		return errors.New("the pod carries no passenger")
	case to.purpose == opEmptyRecovery && (v.Pod.Occupied || v.RidersAboard() > 0):
		return errors.New("an empty recovery needs an empty pod")
	case !ok:
		return fmt.Errorf("station %q does not exist", to.station)
	case to.purpose == opEmergencyUnload && station.ParkingOnly:
		return fmt.Errorf("station %s is not a passenger station", station.ID)
	case to.purpose != opEmergencyUnload && to.interrupt != 0:
		return errors.New("only an emergency unload interrupts riders")
	case !v.marksActiveRiders(to.interrupt):
		return errors.New("the interrupt set names a rider that is not active")
	case to.purpose == opRefuge && slices.Contains(v.Stops, station.ID):
		return fmt.Errorf("refuge %s is a stop of the riders", station.ID)
	}
	return nil
}

// startOperationalUnload starts an emergency unload at the berth where v
// is (incident contract, section 9.3). Its effect is the arrival of an
// emergency unload at that berth: the pod unloads, and the end of the
// interval runs finishOperationalUnload. The station of the berth leaves
// the stops. A pod that already unloads keeps a phase count of 1 or more.
// The pod keeps its owners. Unused grants go at the release boundary.
//
// It refuses, and changes nothing, when a precondition of section 9.3
// fails.
func (s *Simulation) startOperationalUnload(v *vehicle, owner serviceHold, interrupt uint32) error {
	if v.Pod.Activity != Boarding && v.Pod.Activity != Continuing && v.Pod.Activity != Unloading || v.Pod.BerthID == "" {
		return fmt.Errorf("pod %s is not boarding, continuing, or unloading at a berth", v.Pod.ID)
	}
	if !v.carriesPassengers() {
		return fmt.Errorf("pod %s carries no passenger", v.Pod.ID)
	}
	if err := s.operationalMember(v); err != nil {
		return err
	}
	station, ok := s.station(v.Pod.StationID)
	if !ok || station.ParkingOnly {
		return fmt.Errorf("pod %s is not at a passenger station", v.Pod.ID)
	}
	berth, ok := station.berth(v.Pod.BerthID)
	if !ok {
		return fmt.Errorf("pod %s is not at a berth of station %s", v.Pod.ID, station.ID)
	}
	if !v.holdsOwner(owner) {
		return fmt.Errorf("pod %s: service hold %#x is not one held hold", v.Pod.ID, owner)
	}
	if !v.marksActiveRiders(interrupt) {
		return fmt.Errorf("pod %s: the interrupt set names a rider that is not active", v.Pod.ID)
	}
	if v.Pod.Activity != Unloading || v.phaseTicks < 1 {
		v.phaseTicks = unloadingTicks
	}
	v.Pod.Activity, v.Pod.Occupied, v.Pod.StationPhase, v.Pod.ManeuverStationID = Unloading, true, AtBerth, station.ID
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
	v.destination, v.destinationStation = berth, station.ID
	v.buffered, v.bufferBerth = false, ""
	v.reservedThrough, v.pending = -1, -1
	v.Stops = withoutStop(v.Stops, station.ID)
	v.op = operationalDestination{purpose: opEmergencyUnload, owner: owner, interrupt: interrupt}
	return nil
}

// withoutStop returns stops without station, or nil when no stop remains.
// It does not change the array of stops.
func withoutStop(stops []string, station string) []string {
	stops = slices.DeleteFunc(slices.Clone(stops), func(stop string) bool { return stop == station })
	if len(stops) == 0 {
		return nil
	}
	return stops
}

// resumeFromRefuge ends the refuge hold of v and continues to the next
// stop, as continueJourney does after an intermediate stop. When no route
// to the next stop exists, the pod keeps its purpose and holds, and
// resumeFromRefuge returns an error.
func (s *Simulation) resumeFromRefuge(v *vehicle) error {
	if v.op.purpose != opRefuge || v.Pod.Activity != Unloading || v.Pod.BerthID != v.destination.ID {
		return fmt.Errorf("pod %s does not hold at a refuge", v.Pod.ID)
	}
	op := v.op
	v.op = operationalDestination{}
	s.continueJourney(v)
	if v.Pod.Activity != Continuing {
		v.op = op
		return fmt.Errorf("pod %s has no route to its next stop", v.Pod.ID)
	}
	return nil
}

// finishOperationalUnload ends the emergency unload of v at the end of its
// unloading interval (incident contract, section 9.5). Each active rider,
// in Riders order, has one outcome. A marked rider is interrupted, also at
// its destination. An unmarked rider at its destination completes. Each
// other rider is transferred to the queue at the station, also without a
// feasible continuation. Then the pod freezes the distance of its
// retained boarding records and becomes idle at the berth. The holds stay.
func (s *Simulation) finishOperationalUnload(v *vehicle) {
	ridden := v.riddenMeters()
	station := v.Pod.StationID
	// interruptRider and queueTransfer remove the rider, so the index of
	// a later rider moves down by the number of removed riders.
	removed := 0
	for original := range len(v.Riders) {
		index := original - removed
		rider := v.Riders[index]
		switch {
		case rider.Completed:
		case v.op.interrupt&(1<<original) != 0:
			s.interruptRider(v, index)
			removed++
		case rider.To == station:
			s.completeRider(v, index, ridden)
		default:
			s.queueTransfer(v, index, station)
			removed++
		}
	}
	if len(v.Boardings) > 0 {
		v.riddenBase = ridden
	}
	s.settleIdleAtBerth(v)
}

// evacuate interrupts every active rider of a stopped pod with a fault hold
// (incident contract, section 8.2). It completes no rider, also a rider
// whose destination is the station of the berth. At a berth the pod then
// becomes idle there. On a lane it continues empty to its destination
// station as an empty recovery that the fault hold owns.
//
// It refuses, and changes nothing, when the pod has no fault hold, moves,
// is a coupling, compact, or platoon member, or is not at a berth or on a
// lane with passengers.
func (s *Simulation) evacuate(v *vehicle) error {
	if v.withdrawn&faultHold == 0 {
		return fmt.Errorf("pod %s has no fault hold", v.Pod.ID)
	}
	if v.Pod.Speed != 0 {
		return fmt.Errorf("pod %s moves", v.Pod.ID)
	}
	if err := s.operationalMember(v); err != nil {
		return err
	}
	switch v.Pod.Activity {
	case Idle, Boarding, Continuing, Unloading:
		if v.Pod.BerthID == "" {
			return fmt.Errorf("pod %s is not at a berth", v.Pod.ID)
		}
	case Traveling:
		if !v.carriesPassengers() {
			return fmt.Errorf("pod %s carries no passenger on its lane", v.Pod.ID)
		}
	default:
		return fmt.Errorf("pod %s cannot be evacuated while %s", v.Pod.ID, v.Pod.Activity)
	}
	if len(v.Boardings) > 0 && len(v.Boardings) != len(v.Riders) {
		return fmt.Errorf("pod %s: the boarding records do not align with the riders", v.Pod.ID)
	}
	ridden := v.riddenMeters()
	for index := 0; index < len(v.Riders); {
		if v.Riders[index].Completed {
			index++
			continue
		}
		s.interruptRider(v, index)
	}
	if len(v.Boardings) > 0 {
		v.riddenBase = ridden
	}
	if v.Pod.Activity != Traveling {
		s.settleIdleAtBerth(v)
		return nil
	}
	s.evacuateLane(v)
	return nil
}

// evacuateLane makes a traveling pod whose riders left an empty recovery to
// its destination station that the fault hold owns. It replaces any earlier
// purpose. The pod keeps its route, distance, block state, owners, and
// buffer membership. A route that ends at a station entry gets its berth
// as a passenger route does. See assignTerminalBerth.
func (s *Simulation) evacuateLane(v *vehicle) {
	v.Pod.Occupied, v.Stops = false, nil
	if len(v.Riders) == 0 {
		v.Riders, v.Boardings = nil, nil
	}
	v.RelocatingTo, v.Rebalancing, v.released = v.destinationStation, false, false
	v.op = operationalDestination{purpose: opEmptyRecovery, owner: faultHold}
}

// settleIdleAtBerth gives v the complete idle state at its current berth
// (incident contract, section 8.2). The pod keeps its owners and its
// retained route resources: the berth stays owned, and other grants go at
// the release boundary. Completed history stays with its aligned records.
// The purpose clears, and the holds stay.
func (s *Simulation) settleIdleAtBerth(v *vehicle) {
	station, _ := s.station(v.Pod.StationID)
	berth, _ := station.berth(v.Pod.BerthID)
	node, _ := s.network.Node(berth.Node)
	v.Pod = Pod{
		ID: v.Pod.ID, Class: v.Pod.Class, Position: node.Position, Activity: Idle,
		StationID: station.ID, BerthID: berth.ID, StationPhase: AtBerth, ManeuverStationID: station.ID,
	}
	v.phaseTicks, v.blockIndex, v.distance = 0, 0, 0
	v.reservedThrough, v.pending = -1, -1
	v.originReleased = false
	v.Stops = nil
	v.op = operationalDestination{}
	v.buffered, v.bufferBerth = false, ""
	v.RelocatingTo, v.Rebalancing, v.released = "", false, false
	v.origin, v.destination, v.destinationStation = Berth{}, berth, station.ID
	v.replaceRoute(nil)
	v.blocks, v.routeLengths, v.blockStarts, v.terminal = blockList{}, nil, nil, terminalCheck{}
	if len(v.Riders) == 0 {
		v.Riders, v.Boardings = nil, nil
	}
}
