package session

import (
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func (s *Session) presentationFrame() (StreamFrame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.presentationFrameLocked()
}

func (s *Session) presentationFrameLocked() (StreamFrame, error) {
	if err := s.couplingError(); err != nil {
		return StreamFrame{}, err
	}
	snapshot, routes, err := s.simulation.PresentationSnapshot()
	if err != nil {
		if project.HasCouplingContract(s.project) {
			return StreamFrame{}, s.retainCouplingViewError(err)
		}
		return StreamFrame{}, err
	}
	if len(s.build) > 64 {
		return StreamFrame{}, errors.New("stream build ID exceeds 64 bytes")
	}
	state := State{Epoch: s.epoch, Revision: s.revision, ProjectRevision: s.projectRevision, Generation: s.generation,
		Redistribution: s.project.Redistribution && !snapshot.Demo, Simulation: snapshot, Speed: s.speed, SpeedReduction: s.speedReduction, Demand: s.demand.state,
		Checkpoints: s.checkpointList(), Build: s.build, ServerStart: s.serverStart, Restore: s.restore}
	return StreamFrame{State: stateFrame(state), Routes: routes}, nil
}

// StreamAssembler caches verified topology and immutable expanded route data.
type StreamAssembler struct {
	coupling       *sim.CouplingViewValidator
	passengerPaths map[passengerPathKey]bool
	classLanes     map[sim.VehicleClass]map[string]bool
	classes        map[string]sim.VehicleClass
	topology       TopologySnapshot
	lanes          map[string]bool
	laneIndexes    map[string]int
	groupLanes     map[string]bool
	stations       map[string]bool
	// passengerStations holds the stations that are not parking-only.
	passengerStations map[string]bool
	berths            map[string]bool
	boardingBerths    map[string]boardingBerth
	previous          StreamFrame
	state             State
}

// NewStreamAssembler takes ownership of a detached topology snapshot. The
// contract markers of the topology select the stream rules.
func NewStreamAssembler(topology TopologySnapshot) (*StreamAssembler, error) {
	if err := checkTopologyProjectVersion(topology); err != nil {
		return nil, err
	}
	if err := sim.ValidateOrderContract(topology.OrderContract); err != nil {
		return nil, err
	}
	if err := sim.ValidateIncidentContract(topology.IncidentContract); err != nil {
		return nil, err
	}
	if err := sim.ValidateFaultContracts(topology.FaultContract, topology.IncidentContract); err != nil {
		return nil, err
	}
	if err := sim.ValidateEmergencyContracts(topology.EmergencyContract, topology.IncidentContract); err != nil {
		return nil, err
	}
	// A coupling member of the topology selects the coupling marker.
	markers := contractMarkers{order: topology.OrderContract}
	if hasCouplingTopology(topology) {
		markers.coupling = sim.CompactPairV1CouplingContract
	}
	if markers != (contractMarkers{}) {
		if err := validateStreamTopology(topology, markers); err != nil {
			return nil, err
		}
	}
	if len(topology.Network.Lanes) > project.MaxLanes || len(topology.Network.Nodes) > project.MaxNodes {
		return nil, errors.New("topology exceeds supported limits")
	}
	a := &StreamAssembler{topology: topology, lanes: make(map[string]bool, len(topology.Network.Lanes)), laneIndexes: make(map[string]int, len(topology.Network.Lanes)), groupLanes: make(map[string]bool, len(topology.Network.Lanes)), stations: map[string]bool{}, passengerStations: map[string]bool{}, berths: map[string]bool{}, boardingBerths: map[string]boardingBerth{}}
	if markers.coupling != "" {
		validator, err := newCouplingFrameValidator(topology)
		if err != nil {
			return nil, err
		}
		a.coupling = validator
	}
	if markers.order == sim.ExpressOrderContract {
		a.passengerPaths = map[passengerPathKey]bool{}
	}
	nodes := map[string]bool{}
	stationClasses := map[string]sim.ClassSet{}
	for _, node := range topology.Network.Nodes {
		if node.ID == "" || nodes[node.ID] {
			return nil, errors.New("invalid topology node")
		}
		nodes[node.ID] = true
	}
	for _, station := range topology.Network.Stations {
		if station.ID == "" || a.stations[station.ID] {
			return nil, errors.New("invalid topology station")
		}
		a.stations[station.ID] = true
		a.passengerStations[station.ID] = !station.ParkingOnly
		stationClasses[station.ID] = station.VehicleClasses
		for _, berth := range station.Berths {
			if berth.ID == "" || a.berths[berth.ID] || !nodes[berth.Node] {
				return nil, errors.New("invalid topology berth")
			}
			a.berths[berth.ID] = true
			a.boardingBerths[berth.ID] = boardingBerth{station: station.ID, stationClasses: station.VehicleClasses, classes: berth.VehicleClasses, parkingOnly: station.ParkingOnly}
		}
	}
	for _, l := range topology.Network.Lanes {
		if l.ID == "" || a.lanes[l.ID] || !nodes[l.From] || !nodes[l.To] {
			return nil, errors.New("duplicate topology lane")
		}
		a.lanes[l.ID] = true
		a.laneIndexes[l.ID] = len(a.laneIndexes)
		admitted := l.VehicleClasses.Allows(string(sim.GroupClass))
		if l.StationID != "" {
			classes, found := stationClasses[l.StationID]
			admitted = admitted && found && classes.Allows(string(sim.GroupClass))
		}
		a.groupLanes[l.ID] = admitted
	}
	if topology.OrderContract == sim.ExpressOrderContract {
		a.indexClassLanes()
	}
	return a, nil
}

// State reconstructs a candidate without mutating earlier views.
func (a *StreamAssembler) State(f StreamFrame) (State, error) {
	if err := a.serviceOrders(f); err != nil {
		return State{}, err
	}
	if len(f.Routes) != len(f.State.Simulation.Vehicles) {
		return State{}, errors.New("missing route presentation")
	}
	state, err := frameState(a.topology, f.State, true)
	if err != nil {
		return State{}, err
	}
	if err := checkIncidentFrame(f.State.Simulation); err != nil {
		return State{}, err
	}
	if err := checkFaultFrame(f.State.Simulation); err != nil {
		return State{}, err
	}
	if err := a.faultLanes(f.State.Simulation); err != nil {
		return State{}, err
	}
	if err := checkEmergencyFrame(f.State.Simulation); err != nil {
		return State{}, err
	}
	if err := a.references(f); err != nil {
		return State{}, err
	}
	seen := map[string]bool{}
	for i := range state.Simulation.Vehicles {
		v := &state.Simulation.Vehicles[i]
		r := f.Routes[i]
		if seen[v.Pod.ID] || v.Pod.ID == "" || v.Pod.LaneID != "" && !a.lanes[v.Pod.LaneID] {
			return State{}, errors.New("invalid vehicle identity or lane")
		}
		seen[v.Pod.ID] = true
		if err := a.motionPod(r, v.Pod); err != nil {
			return State{}, err
		}
		if i < len(a.previous.Routes) && reflect.DeepEqual(r, a.previous.Routes[i]) {
			v.Route = a.state.Simulation.Vehicles[i].Route
			v.Presentation = a.state.Simulation.Vehicles[i].Presentation
			continue
		}
		if len(r.Display) > project.MaxLanes || len(r.Lanes) > sim.MotionRouteLimit || r.Current < r.Start || r.Current-r.Start > uint64(len(r.Lanes)) || r.Before != (r.Start > 0) {
			return State{}, errors.New("invalid motion window")
		}
		if len(r.Lanes) == 0 {
			if len(r.Display) != 0 || r.Origin != -1 || r.Current != 0 || r.Start != 0 || r.After {
				return State{}, errors.New("invalid empty route")
			}
		} else if r.Origin < 0 || r.Origin >= len(a.topology.Network.Nodes) {
			return State{}, errors.New("invalid route origin")
		}
		display := make(map[int]bool, len(r.Display))
		last := -1
		for _, index := range r.Display {
			if index <= last || index >= len(a.topology.Network.Lanes) {
				return State{}, fmt.Errorf("invalid display lane index %d", index)
			}
			last = index
			display[index] = true
			v.Route = append(v.Route, a.topology.Network.Lanes[index])
		}
		for motionIndex, index := range r.Lanes {
			if !display[index] {
				return State{}, errors.New("motion lane missing from display")
			}
			lane := a.topology.Network.Lanes[index]
			if motionIndex > 0 && r.Motion[motionIndex-1].To != lane.From {
				return State{}, errors.New("disconnected motion path")
			}
			r.Motion = append(r.Motion, lane)
		}
		if v.Pod.LaneID != "" && (r.Current-r.Start >= uint64(len(r.Motion)) || r.Motion[r.Current-r.Start].ID != v.Pod.LaneID) {
			return State{}, errors.New("motion occurrence does not match pod lane")
		}
		if r.Origin >= 0 {
			r.OriginNode = a.topology.Network.Nodes[r.Origin].ID
		}
		v.Presentation = &r
	}
	if err := a.couplingView(state); err != nil {
		return State{}, err
	}
	if err := a.rememberClasses(f); err != nil {
		return State{}, err
	}
	a.previous = ownStreamBoardings(f)
	a.state = state
	if a.topology.OrderContract == sim.ExpressOrderContract || a.coupling != nil {
		a.state = ownAssemblerState(state)
		a.state.Simulation.Berths = slices.Clone(state.Simulation.Berths)
		a.state.Simulation.CouplingGroups = cloneCouplingGroups(state.Simulation.CouplingGroups)
	}
	// Only private containers are retained. The geometry and routes are immutable.
	a.previous.Routes = slices.Clone(f.Routes)
	return state, nil
}

func (a *StreamAssembler) motionPod(r sim.RoutePresentation, p sim.Pod) error {
	if p.LaneID == "" {
		return nil
	}
	if r.Current < r.Start || r.Current-r.Start >= uint64(len(r.Lanes)) {
		return errors.New("pod occurrence outside motion window")
	}
	index := r.Lanes[r.Current-r.Start]
	if index < 0 || index >= len(a.topology.Network.Lanes) || a.topology.Network.Lanes[index].ID != p.LaneID {
		return errors.New("motion occurrence does not match pod lane")
	}
	return nil
}
func optionalReference(index map[string]bool, id string) bool { return id == "" || index[id] }

// validLegOrigin reports whether the leg origin of r is absent, or is a
// passenger station other than the destination (incident contract,
// section 7.1).
func (a *StreamAssembler) validLegOrigin(r sim.Request) bool {
	return r.LegFrom == "" || a.passengerStations[r.LegFrom] && r.LegFrom != r.To
}
func (a *StreamAssembler) references(f StreamFrame) error {
	if err := a.groupBindings(f); err != nil {
		return err
	}
	snapshot := f.State.Simulation
	pods := map[string]bool{}
	// A wait report names a pod or an active fault (incident suspension
	// contract, section 9.6).
	faults := faultIDs(snapshot)
	pendingLimit := maxSavedTrips
	if a.topology.OrderContract == sim.ExpressOrderContract {
		pendingLimit = sim.MaxExpressWaitingTrips
	}
	if len(snapshot.Pending) > pendingLimit {
		return errors.New("too many pending requests")
	}
	for _, v := range snapshot.Vehicles {
		if v.Pod.ID == "" || pods[v.Pod.ID] {
			return errors.New("duplicate pod")
		}
		pods[v.Pod.ID] = true
	}
	request := func(r sim.Request) bool {
		return a.stations[r.From] && a.validLegOrigin(r) && a.stations[r.To] && optionalReference(pods, r.PodID)
	}
	for _, v := range snapshot.Vehicles {
		p := v.Pod
		if err := a.vehicleBoardings(v); err != nil {
			return err
		}
		if !optionalReference(a.stations, p.StationID) || !optionalReference(a.stations, p.ManeuverStationID) || !optionalReference(a.stations, v.RelocatingTo) || !optionalReference(a.berths, p.BerthID) || !optionalReference(pods, p.BlockedBy) && !faults[p.BlockedBy] || !optionalReference(pods, v.PlatoonID) {
			return errors.New("invalid pod reference")
		}
		if len(v.Riders) > sim.MaxStoredRidersForOrderContract(v.Pod.Class, a.topology.OrderContract) || len(v.Stops) > 8 {
			return errors.New("too many riders or stops")
		}
		for _, r := range v.Riders {
			if !request(r) {
				return errors.New("invalid rider reference")
			}
		}
		for _, stop := range v.Stops {
			if !a.stations[stop] {
				return errors.New("invalid stop reference")
			}
		}
	}
	for _, r := range snapshot.Pending {
		if !request(r) {
			return errors.New("invalid pending reference")
		}
	}
	seen := map[string]bool{}
	for _, b := range snapshot.Berths {
		if !a.berths[b.ID] || seen[b.ID] || !optionalReference(pods, b.Occupant) || !optionalReference(pods, b.ReservedBy) {
			return errors.New("invalid berth reference")
		}
		seen[b.ID] = true
	}
	return nil
}
