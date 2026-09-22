package connectapi

import (
	podsimv1 "github.com/dotwaffle/podsim/internal/gen/podsim/v1"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func stateToProto(frame session.StateFrame) *podsimv1.GetStateResponse {
	return &podsimv1.GetStateResponse{
		Epoch: frame.Epoch, Revision: frame.Revision, ProjectRevision: frame.ProjectRevision,
		Generation: frame.Generation, Redistribution: frame.Redistribution,
		Simulation: simulationToProto(frame.Simulation), Speed: protocolInt32(frame.Speed),
		Demand: &podsimv1.DemandState{
			Config: demandConfigToProto(frame.Demand.Config), Generated: protocolInt32(frame.Demand.Generated),
			Skipped: protocolInt32(frame.Demand.Skipped), Error: frame.Demand.Error,
		},
	}
}

func stateFromProto(frame *podsimv1.GetStateResponse) session.StateFrame {
	demand := frame.GetDemand()
	return session.StateFrame{
		Epoch: frame.GetEpoch(), Revision: frame.GetRevision(), ProjectRevision: frame.GetProjectRevision(),
		Generation: frame.GetGeneration(), Redistribution: frame.GetRedistribution(),
		Simulation: simulationFromProto(frame.GetSimulation()), Speed: int(frame.GetSpeed()),
		Demand: session.DemandState{
			Config: demandConfigFromProto(demand.GetConfig()), Generated: int(demand.GetGenerated()),
			Skipped: int(demand.GetSkipped()), Error: demand.GetError(),
		},
	}
}

func simulationToProto(frame session.SimulationFrame) *podsimv1.SimulationFrame {
	vehicles := make([]*podsimv1.VehicleFrame, len(frame.Vehicles))
	for index, vehicle := range frame.Vehicles {
		vehicles[index] = &podsimv1.VehicleFrame{
			Pod: podToProto(vehicle.Pod), Request: requestToProto(vehicle.Request), RouteLaneIds: vehicle.RouteLaneIDs,
			Parties: protocolInt32(vehicle.Parties), RelocatingTo: vehicle.RelocatingTo, Rebalancing: vehicle.Rebalancing,
		}
	}
	berths := make([]*podsimv1.BerthState, len(frame.Berths))
	for index, berth := range frame.Berths {
		berths[index] = &podsimv1.BerthState{Id: berth.ID, Occupant: berth.Occupant, ReservedBy: berth.ReservedBy}
	}
	pending := make([]*podsimv1.Request, len(frame.Pending))
	for index := range frame.Pending {
		pending[index] = requestToProto(&frame.Pending[index])
	}
	return &podsimv1.SimulationFrame{
		Submitted: protocolInt32(frame.Submitted), Tick: frame.Tick, Paused: frame.Paused,
		Vehicles: vehicles, Berths: berths, Completed: protocolInt32(frame.Completed), Demo: frame.Demo,
		DemoError: frame.DemoError, Pending: pending,
		Wait:                    &podsimv1.WaitStats{AverageSeconds: frame.Wait.AverageSeconds, MaxSeconds: frame.Wait.MaxSeconds},
		PassengerDistanceMeters: frame.PassengerDistanceMeters, SharedParties: protocolInt32(frame.SharedParties),
		SharedRidePartyLimit: protocolInt32(frame.SharedRidePartyLimit), EmptyDistanceMeters: frame.EmptyDistanceMeters,
		RebalanceMoves: protocolInt32(frame.RebalanceMoves),
	}
}

func simulationFromProto(frame *podsimv1.SimulationFrame) session.SimulationFrame {
	if frame == nil {
		return session.SimulationFrame{}
	}
	vehicles := make([]session.VehicleFrame, len(frame.GetVehicles()))
	for index, vehicle := range frame.GetVehicles() {
		vehicles[index] = session.VehicleFrame{
			Pod: podFromProto(vehicle.GetPod()), Request: requestFromProto(vehicle.Request), RouteLaneIDs: vehicle.GetRouteLaneIds(),
			Parties: int(vehicle.GetParties()), RelocatingTo: vehicle.GetRelocatingTo(), Rebalancing: vehicle.GetRebalancing(),
		}
	}
	berths := make([]sim.BerthState, len(frame.GetBerths()))
	for index, berth := range frame.GetBerths() {
		berths[index] = sim.BerthState{ID: berth.GetId(), Occupant: berth.GetOccupant(), ReservedBy: berth.GetReservedBy()}
	}
	var pending []sim.Request
	if frame.Pending != nil {
		pending = make([]sim.Request, len(frame.GetPending()))
	}
	for index, request := range frame.GetPending() {
		pending[index] = *requestFromProto(request)
	}
	return session.SimulationFrame{
		Submitted: int(frame.GetSubmitted()), Tick: frame.GetTick(), Paused: frame.GetPaused(),
		Vehicles: vehicles, Berths: berths, Completed: int(frame.GetCompleted()), Demo: frame.GetDemo(),
		DemoError: frame.GetDemoError(), Pending: pending,
		Wait:                    sim.WaitStats{AverageSeconds: frame.GetWait().GetAverageSeconds(), MaxSeconds: frame.GetWait().GetMaxSeconds()},
		PassengerDistanceMeters: frame.GetPassengerDistanceMeters(), SharedParties: int(frame.GetSharedParties()),
		SharedRidePartyLimit: int(frame.GetSharedRidePartyLimit()), EmptyDistanceMeters: frame.GetEmptyDistanceMeters(),
		RebalanceMoves: int(frame.GetRebalanceMoves()),
	}
}

func podToProto(pod sim.Pod) *podsimv1.Pod {
	return &podsimv1.Pod{
		Id: pod.ID, Position: pointToProto(pod.Position), Activity: string(pod.Activity), StationId: pod.StationID,
		BerthId: pod.BerthID, LaneId: pod.LaneID, LaneDistance: pod.LaneDistance, Speed: pod.Speed,
		Occupied: pod.Occupied, WaitReason: string(pod.WaitReason), BlockedBy: pod.BlockedBy,
		StationPhase: string(pod.StationPhase), ManeuverStationId: pod.ManeuverStationID,
	}
}

func podFromProto(pod *podsimv1.Pod) sim.Pod {
	if pod == nil {
		return sim.Pod{}
	}
	return sim.Pod{
		ID: pod.GetId(), Position: pointFromProto(pod.GetPosition()), Activity: sim.Activity(pod.GetActivity()),
		StationID: pod.GetStationId(), BerthID: pod.GetBerthId(), LaneID: pod.GetLaneId(),
		LaneDistance: pod.GetLaneDistance(), Speed: pod.GetSpeed(), Occupied: pod.GetOccupied(),
		WaitReason: sim.WaitReason(pod.GetWaitReason()), BlockedBy: pod.GetBlockedBy(),
		StationPhase: sim.StationPhase(pod.GetStationPhase()), ManeuverStationID: pod.GetManeuverStationId(),
	}
}

func requestToProto(request *sim.Request) *podsimv1.Request {
	if request == nil {
		return nil
	}
	return &podsimv1.Request{
		Id: protocolInt32(request.ID), From: request.From, To: request.To, PartySize: protocolInt32(request.PartySize),
		PodId: request.PodID, Completed: request.Completed, RequestedTick: request.RequestedTick, DispatchReason: request.DispatchReason,
	}
}

func requestFromProto(request *podsimv1.Request) *sim.Request {
	if request == nil {
		return nil
	}
	return &sim.Request{
		ID: int(request.GetId()), From: request.GetFrom(), To: request.GetTo(), PartySize: int(request.GetPartySize()),
		PodID: request.GetPodId(), Completed: request.GetCompleted(), RequestedTick: request.GetRequestedTick(),
		DispatchReason: request.GetDispatchReason(),
	}
}
