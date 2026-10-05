package session

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TopologySnapshot contains geometry that changes only with the project.
type TopologySnapshot struct {
	CouplingContract  sim.CouplingContract   `json:"couplingContract,omitzero"`
	CouplingEnabled   bool                   `json:"couplingEnabled,omitzero"`
	CouplingSites     []sim.CouplingSite     `json:"couplingSites,omitzero"`
	CouplingCorridors []sim.CouplingCorridor `json:"couplingCorridors,omitzero"`
	OrderContract     sim.OrderContract      `json:"orderContract,omitzero"`
	IncidentContract  sim.IncidentContract   `json:"incidentContract,omitzero"`
	ExpressServices   []sim.ExpressService   `json:"expressServices,omitempty"`
	ProjectVersion    int                    `json:"projectVersion,omitzero"`
	ServerStart       string                 `json:"serverStart"`
	Epoch             string                 `json:"epoch"`
	ProjectRevision   uint64                 `json:"projectRevision"`
	Network           sim.Network            `json:"network"`
	Geo               *project.Geo           `json:"geo,omitzero"`
	Map               *project.MapBackground `json:"map,omitzero"`
}

// StateFrame contains the recurring state without network geometry.
// Checkpoints lists the retained save points, oldest first. Build identifies
// the server build. It is empty when the server has no build ID. ServerStart
// identifies the server process. Restore tells how the server started the
// simulation when it had a state store. Its tier is empty when the server
// rejected or could not read the saved state. It is zero when no saved state
// existed, when the server has no store, and after a reset, a demo or a
// project apply that replaces the fleet.
type StateFrame struct {
	Epoch           string          `json:"epoch"`
	Revision        uint64          `json:"revision"`
	ProjectRevision uint64          `json:"projectRevision"`
	Generation      uint64          `json:"generation"`
	Redistribution  bool            `json:"redistribution"`
	Simulation      SimulationFrame `json:"simulation"`
	Speed           int             `json:"speed"`
	SpeedReduction  SpeedReduction  `json:"speedReduction,omitzero"`
	Demand          DemandState     `json:"demand"`
	Checkpoints     []Checkpoint    `json:"checkpoints,omitempty"`
	Build           string          `json:"build,omitempty"`
	ServerStart     string          `json:"serverStart,omitempty"`
	Restore         RestoreInfo     `json:"restore,omitzero"`
}

// SimulationFrame replaces repeated route lane objects with stable lane IDs.
type SimulationFrame struct {
	CouplingContract        sim.CouplingContract    `json:"couplingContract,omitzero"`
	CouplingEnabled         bool                    `json:"couplingEnabled,omitzero"`
	CouplingGroups          []sim.CouplingGroupView `json:"couplingGroups,omitzero"`
	OrderContract           sim.OrderContract       `json:"orderContract,omitzero"`
	IncidentContract        sim.IncidentContract    `json:"incidentContract,omitzero"`
	Submitted               int                     `json:"submitted"`
	Tick                    int64                   `json:"tick"`
	Paused                  bool                    `json:"paused"`
	Vehicles                []VehicleFrame          `json:"vehicles"`
	Berths                  []sim.BerthState        `json:"berths"`
	Completed               int                     `json:"completed"`
	Demo                    bool                    `json:"demo"`
	DemoError               string                  `json:"demoError"`
	Pending                 []sim.Request           `json:"pending"`
	Wait                    sim.WaitStats           `json:"wait"`
	Journey                 sim.JourneyStats        `json:"journey"`
	PassengerDistanceMeters float64                 `json:"passengerDistanceMeters"`
	RiderDistanceMeters     float64                 `json:"riderDistanceMeters"`
	DirectDistanceMeters    float64                 `json:"directDistanceMeters"`
	MaxDetourRatio          float64                 `json:"maxDetourRatio"`
	SharedParties           int                     `json:"sharedParties"`
	SharedRidePartyLimit    int                     `json:"sharedRidePartyLimit"`
	EmptyDistanceMeters     float64                 `json:"emptyDistanceMeters"`
	RebalanceMoves          int                     `json:"rebalanceMoves"`
}

// VehicleFrame contains dynamic vehicle data and its ordered route IDs.
type VehicleFrame struct {
	CouplingID   string              `json:"couplingID,omitzero"`
	Boardings    []sim.RiderBoarding `json:"boardings,omitempty"`
	RiddenMeters float64             `json:"riddenMeters,omitzero"`
	Pod          sim.Pod             `json:"pod"`
	Riders       []sim.Request       `json:"riders,omitempty"`
	Stops        []string            `json:"stops,omitempty"`
	RouteLaneIDs []string            `json:"routeLaneIDs"`
	RelocatingTo string              `json:"relocatingTo"`
	Rebalancing  bool                `json:"rebalancing"`
	// PlatoonID and PlatoonIndex are the platoon of a coupled pod. See
	// sim.Vehicle.
	PlatoonID    string `json:"platoonID,omitempty"`
	PlatoonIndex int    `json:"platoonIndex,omitzero"`
}

// FrameState combines one matching topology snapshot and state frame.
func FrameState(topology TopologySnapshot, frame StateFrame) (State, error) {
	return frameState(topology, frame, false)
}

func frameState(topology TopologySnapshot, frame StateFrame, immutable bool) (State, error) {
	if !validSpeed(frame.Speed) {
		return State{}, fmt.Errorf("state frame speed %d is not 1, 2, 5, 15 or 60", frame.Speed)
	}
	if err := checkTopologyProjectVersion(topology); err != nil {
		return State{}, err
	}
	if err := couplingFrameBinding(topology, frame.Simulation); err != nil {
		return State{}, err
	}
	if topology.OrderContract != frame.Simulation.OrderContract {
		return State{}, errors.New("topology order contract does not match state")
	}
	if err := incidentFrameBinding(topology, frame.Simulation); err != nil {
		return State{}, err
	}
	if topology.ServerStart != frame.ServerStart || topology.Epoch != frame.Epoch || topology.ProjectRevision != frame.ProjectRevision {
		return State{}, errors.New("topology revision does not match state frame")
	}
	lanes := make(map[string]sim.Lane, len(topology.Network.Lanes))
	if !immutable {
		for _, lane := range topology.Network.Lanes {
			lanes[lane.ID] = lane
		}
	}
	vehicles := make([]sim.Vehicle, len(frame.Simulation.Vehicles))
	for index, vehicle := range frame.Simulation.Vehicles {
		var route []sim.Lane
		if vehicle.RouteLaneIDs != nil {
			route = make([]sim.Lane, len(vehicle.RouteLaneIDs))
		}
		for routeIndex, laneID := range vehicle.RouteLaneIDs {
			lane, ok := lanes[laneID]
			if !ok {
				return State{}, fmt.Errorf("vehicle %q route references unknown lane %q", vehicle.Pod.ID, laneID)
			}
			route[routeIndex] = lane
		}
		vehicles[index] = sim.Vehicle{
			CouplingID: vehicle.CouplingID,
			Boardings:  slices.Clone(vehicle.Boardings), RiddenMeters: vehicle.RiddenMeters,
			Pod: vehicle.Pod, Riders: slices.Clone(vehicle.Riders), Stops: slices.Clone(vehicle.Stops), Route: route,
			RelocatingTo: vehicle.RelocatingTo, Rebalancing: vehicle.Rebalancing,
			PlatoonID: vehicle.PlatoonID, PlatoonIndex: vehicle.PlatoonIndex,
		}
	}
	snapshot := frame.Simulation
	if snapshot.OrderContract == sim.ExpressOrderContract {
		snapshot.Pending = slices.Clone(snapshot.Pending)
	}
	network := topology.Network
	geo, background := topology.Geo, topology.Map
	if !immutable {
		network = project.CloneNetwork(network)
		if geo != nil {
			geo = new(*geo)
		}
		if background != nil {
			background = new(*background)
		}
	}
	state := State{
		Epoch: frame.Epoch, Revision: frame.Revision, ProjectRevision: frame.ProjectRevision,
		Generation: frame.Generation, Redistribution: frame.Redistribution,
		Network: network, Geo: geo, Map: background,
		Simulation: sim.Snapshot{
			CouplingContract: snapshot.CouplingContract, CouplingEnabled: snapshot.CouplingEnabled,
			CouplingGroups: cloneCouplingGroups(snapshot.CouplingGroups),
			OrderContract:  snapshot.OrderContract, IncidentContract: snapshot.IncidentContract,
			Submitted: snapshot.Submitted, Tick: snapshot.Tick, Paused: snapshot.Paused,
			Vehicles: vehicles, Berths: snapshot.Berths, Completed: snapshot.Completed,
			Demo: snapshot.Demo, DemoError: snapshot.DemoError, Pending: snapshot.Pending,
			Wait: snapshot.Wait, Journey: snapshot.Journey, PassengerDistanceMeters: snapshot.PassengerDistanceMeters,
			RiderDistanceMeters: snapshot.RiderDistanceMeters, DirectDistanceMeters: snapshot.DirectDistanceMeters,
			MaxDetourRatio: snapshot.MaxDetourRatio,
			SharedParties:  snapshot.SharedParties, SharedRidePartyLimit: snapshot.SharedRidePartyLimit,
			EmptyDistanceMeters: snapshot.EmptyDistanceMeters, RebalanceMoves: snapshot.RebalanceMoves,
		},
		Speed: frame.Speed, SpeedReduction: frame.SpeedReduction, Demand: frame.Demand, Checkpoints: slices.Clone(frame.Checkpoints),
		Build: frame.Build, ServerStart: frame.ServerStart, Restore: frame.Restore,
	}
	if !immutable && topology.CouplingContract != "" {
		validator, err := newCouplingFrameValidator(topology)
		if err != nil {
			return State{}, err
		}
		if err := validator.Validate(state.Simulation); err != nil {
			return State{}, err
		}
	}
	return state, nil
}

func stateFrame(state State) StateFrame {
	vehicles := make([]VehicleFrame, len(state.Simulation.Vehicles))
	for index, vehicle := range state.Simulation.Vehicles {
		var routeIDs []string
		if vehicle.Route != nil {
			routeIDs = make([]string, len(vehicle.Route))
		}
		for routeIndex, lane := range vehicle.Route {
			routeIDs[routeIndex] = lane.ID
		}
		vehicles[index] = VehicleFrame{
			CouplingID: vehicle.CouplingID,
			Boardings:  slices.Clone(vehicle.Boardings), RiddenMeters: vehicle.RiddenMeters,
			Pod: vehicle.Pod, Riders: slices.Clone(vehicle.Riders), Stops: slices.Clone(vehicle.Stops), RouteLaneIDs: routeIDs,
			RelocatingTo: vehicle.RelocatingTo, Rebalancing: vehicle.Rebalancing,
			PlatoonID: vehicle.PlatoonID, PlatoonIndex: vehicle.PlatoonIndex,
		}
	}
	snapshot := state.Simulation
	return StateFrame{
		Epoch: state.Epoch, Revision: state.Revision, ProjectRevision: state.ProjectRevision,
		Generation: state.Generation, Redistribution: state.Redistribution,
		Simulation: SimulationFrame{
			CouplingContract: snapshot.CouplingContract, CouplingEnabled: snapshot.CouplingEnabled,
			CouplingGroups: cloneCouplingGroups(snapshot.CouplingGroups),
			OrderContract:  snapshot.OrderContract, IncidentContract: snapshot.IncidentContract,
			Submitted: snapshot.Submitted, Tick: snapshot.Tick, Paused: snapshot.Paused,
			Vehicles: vehicles, Berths: snapshot.Berths, Completed: snapshot.Completed,
			Demo: snapshot.Demo, DemoError: snapshot.DemoError, Pending: snapshot.Pending,
			Wait: snapshot.Wait, Journey: snapshot.Journey, PassengerDistanceMeters: snapshot.PassengerDistanceMeters,
			RiderDistanceMeters: snapshot.RiderDistanceMeters, DirectDistanceMeters: snapshot.DirectDistanceMeters,
			MaxDetourRatio: snapshot.MaxDetourRatio,
			SharedParties:  snapshot.SharedParties, SharedRidePartyLimit: snapshot.SharedRidePartyLimit,
			EmptyDistanceMeters: snapshot.EmptyDistanceMeters, RebalanceMoves: snapshot.RebalanceMoves,
		},
		Speed: state.Speed, SpeedReduction: state.SpeedReduction, Demand: state.Demand, Checkpoints: slices.Clone(state.Checkpoints),
		Build: state.Build, ServerStart: state.ServerStart, Restore: state.Restore,
	}
}

// incidentFrameBinding refuses a frame whose incident marker is not the
// marker of its topology, and an unknown marker. A delta carries no
// marker, so the marker of a stream changes only with a new topology.
func incidentFrameBinding(topology TopologySnapshot, frame SimulationFrame) error {
	if err := sim.ValidateIncidentContract(topology.IncidentContract); err != nil {
		return err
	}
	if topology.IncidentContract != frame.IncidentContract {
		return errors.New("topology incident contract does not match state")
	}
	return nil
}
