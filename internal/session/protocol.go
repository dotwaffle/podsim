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
	Epoch           string      `json:"epoch"`
	ProjectRevision uint64      `json:"projectRevision"`
	Network         sim.Network `json:"network"`
}

// StateFrame contains the recurring state without network geometry.
// Checkpoints lists the retained save points, oldest first. Build identifies
// the server build. It is empty when the server has no build ID. Restore
// tells how the server started the simulation when it had a state store.
// Its tier is empty when the server rejected or could not read the saved
// state. It is zero when no saved state existed, when the server has no
// store, and after a reset, a demo or a project apply.
type StateFrame struct {
	Epoch           string          `json:"epoch"`
	Revision        uint64          `json:"revision"`
	ProjectRevision uint64          `json:"projectRevision"`
	Generation      uint64          `json:"generation"`
	Redistribution  bool            `json:"redistribution"`
	Simulation      SimulationFrame `json:"simulation"`
	Speed           int             `json:"speed"`
	Demand          DemandState     `json:"demand"`
	Checkpoints     []Checkpoint    `json:"checkpoints,omitempty"`
	Build           string          `json:"build,omitempty"`
	Restore         RestoreInfo     `json:"restore,omitzero"`
}

// SimulationFrame replaces repeated route lane objects with stable lane IDs.
type SimulationFrame struct {
	Submitted               int              `json:"Submitted"`
	Tick                    int64            `json:"Tick"`
	Paused                  bool             `json:"Paused"`
	Vehicles                []VehicleFrame   `json:"Vehicles"`
	Berths                  []sim.BerthState `json:"Berths"`
	Completed               int              `json:"Completed"`
	Demo                    bool             `json:"Demo"`
	DemoError               string           `json:"DemoError"`
	Pending                 []sim.Request    `json:"Pending"`
	Wait                    sim.WaitStats    `json:"Wait"`
	PassengerDistanceMeters float64          `json:"PassengerDistanceMeters"`
	SharedParties           int              `json:"SharedParties"`
	SharedRidePartyLimit    int              `json:"SharedRidePartyLimit"`
	EmptyDistanceMeters     float64          `json:"EmptyDistanceMeters"`
	RebalanceMoves          int              `json:"RebalanceMoves"`
}

// VehicleFrame contains dynamic vehicle data and its ordered route IDs.
type VehicleFrame struct {
	Pod          sim.Pod      `json:"Pod"`
	Request      *sim.Request `json:"Request"`
	RouteLaneIDs []string     `json:"RouteLaneIDs"`
	Parties      int          `json:"Parties,omitempty"`
	RelocatingTo string       `json:"RelocatingTo"`
	Rebalancing  bool         `json:"Rebalancing"`
}

// FrameState combines one matching topology snapshot and state frame.
func FrameState(topology TopologySnapshot, frame StateFrame) (State, error) {
	if topology.Epoch != frame.Epoch || topology.ProjectRevision != frame.ProjectRevision {
		return State{}, errors.New("topology revision does not match state frame")
	}
	lanes := make(map[string]sim.Lane, len(topology.Network.Lanes))
	for _, lane := range topology.Network.Lanes {
		lanes[lane.ID] = lane
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
			Pod: vehicle.Pod, Request: vehicle.Request, Route: route,
			Parties: vehicle.Parties, RelocatingTo: vehicle.RelocatingTo, Rebalancing: vehicle.Rebalancing,
		}
	}
	snapshot := frame.Simulation
	return State{
		Epoch: frame.Epoch, Revision: frame.Revision, ProjectRevision: frame.ProjectRevision,
		Generation: frame.Generation, Redistribution: frame.Redistribution,
		Network: project.CloneNetwork(topology.Network),
		Simulation: sim.Snapshot{
			Submitted: snapshot.Submitted, Tick: snapshot.Tick, Paused: snapshot.Paused,
			Vehicles: vehicles, Berths: snapshot.Berths, Completed: snapshot.Completed,
			Demo: snapshot.Demo, DemoError: snapshot.DemoError, Pending: snapshot.Pending,
			Wait: snapshot.Wait, PassengerDistanceMeters: snapshot.PassengerDistanceMeters,
			SharedParties: snapshot.SharedParties, SharedRidePartyLimit: snapshot.SharedRidePartyLimit,
			EmptyDistanceMeters: snapshot.EmptyDistanceMeters, RebalanceMoves: snapshot.RebalanceMoves,
		},
		Speed: frame.Speed, Demand: frame.Demand, Checkpoints: slices.Clone(frame.Checkpoints),
		Build: frame.Build, Restore: frame.Restore,
	}, nil
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
			Pod: vehicle.Pod, Request: vehicle.Request, RouteLaneIDs: routeIDs,
			Parties: vehicle.Parties, RelocatingTo: vehicle.RelocatingTo, Rebalancing: vehicle.Rebalancing,
		}
	}
	snapshot := state.Simulation
	return StateFrame{
		Epoch: state.Epoch, Revision: state.Revision, ProjectRevision: state.ProjectRevision,
		Generation: state.Generation, Redistribution: state.Redistribution,
		Simulation: SimulationFrame{
			Submitted: snapshot.Submitted, Tick: snapshot.Tick, Paused: snapshot.Paused,
			Vehicles: vehicles, Berths: snapshot.Berths, Completed: snapshot.Completed,
			Demo: snapshot.Demo, DemoError: snapshot.DemoError, Pending: snapshot.Pending,
			Wait: snapshot.Wait, PassengerDistanceMeters: snapshot.PassengerDistanceMeters,
			SharedParties: snapshot.SharedParties, SharedRidePartyLimit: snapshot.SharedRidePartyLimit,
			EmptyDistanceMeters: snapshot.EmptyDistanceMeters, RebalanceMoves: snapshot.RebalanceMoves,
		},
		Speed: state.Speed, Demand: state.Demand, Checkpoints: slices.Clone(state.Checkpoints),
		Build: state.Build, Restore: state.Restore,
	}
}
