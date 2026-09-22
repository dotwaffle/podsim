package connectapi

import (
	"errors"

	podsimv1 "github.com/dotwaffle/podsim/internal/gen/podsim/v1"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func topologyToProto(topology session.TopologySnapshot) *podsimv1.GetTopologyResponse {
	return &podsimv1.GetTopologyResponse{
		Epoch: topology.Epoch, ProjectRevision: topology.ProjectRevision,
		Network: networkToProto(topology.Network),
	}
}

func topologyFromProto(topology *podsimv1.GetTopologyResponse) session.TopologySnapshot {
	return session.TopologySnapshot{
		Epoch: topology.GetEpoch(), ProjectRevision: topology.GetProjectRevision(),
		Network: networkFromProto(topology.GetNetwork()),
	}
}

func projectToProto(state session.ProjectState) *podsimv1.GetProjectResponse {
	return &podsimv1.GetProjectResponse{Revision: state.Revision, Project: configToProto(state.Project)}
}

func configToProto(config project.Config) *podsimv1.Project {
	fleet := make([]*podsimv1.Placement, len(config.Fleet))
	for index, placement := range config.Fleet {
		fleet[index] = &podsimv1.Placement{Id: placement.ID, StationId: placement.StationID, BerthId: placement.BerthID}
	}
	profiles := make([]*podsimv1.DemandProfile, len(config.DemandProfiles))
	for index, profile := range config.DemandProfiles {
		profiles[index] = demandProfileToProto(profile)
	}
	return &podsimv1.Project{
		Version: protocolInt32(config.Version), Name: config.Name, Network: networkToProto(config.Network),
		Fleet: fleet, Demand: demandConfigToProto(config.Demand), DemandProfiles: profiles,
		SharedRidePartyLimit: protocolInt32(config.SharedRidePartyLimit), Redistribution: config.Redistribution,
	}
}

func configFromProto(config *podsimv1.Project) project.Config {
	if config == nil {
		return project.Config{}
	}
	fleet := make([]sim.Placement, len(config.GetFleet()))
	for index, placement := range config.GetFleet() {
		fleet[index] = sim.Placement{ID: placement.GetId(), StationID: placement.GetStationId(), BerthID: placement.GetBerthId()}
	}
	var profiles []project.DemandProfile
	if config.DemandProfiles != nil {
		profiles = make([]project.DemandProfile, len(config.GetDemandProfiles()))
	}
	for index, profile := range config.GetDemandProfiles() {
		profiles[index] = demandProfileFromProto(profile)
	}
	return project.Config{
		Version: int(config.GetVersion()), Name: config.GetName(), Network: networkFromProto(config.GetNetwork()),
		Fleet: fleet, Demand: demandConfigFromProto(config.GetDemand()), DemandProfiles: profiles,
		SharedRidePartyLimit: int(config.GetSharedRidePartyLimit()), Redistribution: config.GetRedistribution(),
	}
}

func demandProfileToProto(profile project.DemandProfile) *podsimv1.DemandProfile {
	bands := make([]*podsimv1.DemandBand, len(profile.Bands))
	for index, band := range profile.Bands {
		bands[index] = &podsimv1.DemandBand{
			Id: band.ID, Name: band.Name, StartMinute: protocolInt32(band.StartMinute), DurationMinutes: protocolInt32(band.DurationMinutes),
		}
	}
	flows := make([]*podsimv1.DemandFlow, len(profile.Flows))
	for index, flow := range profile.Flows {
		flows[index] = &podsimv1.DemandFlow{From: flow.From, To: flow.To, Weights: flow.Weights}
	}
	return &podsimv1.DemandProfile{Id: profile.ID, Name: profile.Name, Bands: bands, Flows: flows}
}

func demandProfileFromProto(profile *podsimv1.DemandProfile) project.DemandProfile {
	var bands []project.DemandBand
	if profile.Bands != nil {
		bands = make([]project.DemandBand, len(profile.GetBands()))
	}
	for index, band := range profile.GetBands() {
		bands[index] = project.DemandBand{
			ID: band.GetId(), Name: band.GetName(), StartMinute: int(band.GetStartMinute()), DurationMinutes: int(band.GetDurationMinutes()),
		}
	}
	var flows []project.DemandFlow
	if profile.Flows != nil {
		flows = make([]project.DemandFlow, len(profile.GetFlows()))
	}
	for index, flow := range profile.GetFlows() {
		flows[index] = project.DemandFlow{From: flow.GetFrom(), To: flow.GetTo(), Weights: append([]float64(nil), flow.GetWeights()...)}
	}
	return project.DemandProfile{ID: profile.GetId(), Name: profile.GetName(), Bands: bands, Flows: flows}
}

func demandConfigToProto(config project.DemandConfig) *podsimv1.DemandConfig {
	return &podsimv1.DemandConfig{
		Enabled: config.Enabled, PerMinute: protocolInt32(config.PerMinute), Pattern: config.Pattern,
		Seed: config.Seed, Destination: config.Destination, Profile: config.Profile, Band: config.Band,
	}
}

func demandConfigFromProto(config *podsimv1.DemandConfig) project.DemandConfig {
	if config == nil {
		return project.DemandConfig{}
	}
	return project.DemandConfig{
		Enabled: config.GetEnabled(), PerMinute: int(config.GetPerMinute()), Pattern: config.GetPattern(),
		Seed: config.GetSeed(), Destination: config.GetDestination(), Profile: config.GetProfile(), Band: config.GetBand(),
	}
}

func networkToProto(network sim.Network) *podsimv1.Network {
	nodes := make([]*podsimv1.Node, len(network.Nodes))
	for index, node := range network.Nodes {
		nodes[index] = &podsimv1.Node{Id: node.ID, Position: pointToProto(node.Position)}
	}
	lanes := make([]*podsimv1.Lane, len(network.Lanes))
	for index, lane := range network.Lanes {
		lanes[index] = &podsimv1.Lane{
			Id: lane.ID, From: lane.From, To: lane.To, SpeedLimit: lane.SpeedLimit,
			SeparationGroup: lane.SeparationGroup, StationId: lane.StationID, StationRole: string(lane.StationRole),
		}
		if lane.Control != nil {
			lanes[index].Control = pointToProto(*lane.Control)
		}
	}
	stations := make([]*podsimv1.Station, len(network.Stations))
	for index, station := range network.Stations {
		berths := make([]*podsimv1.Berth, len(station.Berths))
		for berthIndex, berth := range station.Berths {
			berths[berthIndex] = &podsimv1.Berth{Id: berth.ID, Node: berth.Node, SeparationGroup: berth.SeparationGroup}
		}
		stations[index] = &podsimv1.Station{
			Id: station.ID, Name: station.Name, Entry: station.Entry, Exit: station.Exit,
			Berths: berths, ParkingOnly: station.ParkingOnly,
		}
	}
	return &podsimv1.Network{Nodes: nodes, Lanes: lanes, Stations: stations}
}

func networkFromProto(network *podsimv1.Network) sim.Network {
	if network == nil {
		return sim.Network{}
	}
	nodes := make([]sim.Node, len(network.GetNodes()))
	for index, node := range network.GetNodes() {
		nodes[index] = sim.Node{ID: node.GetId(), Position: pointFromProto(node.GetPosition())}
	}
	lanes := make([]sim.Lane, len(network.GetLanes()))
	for index, lane := range network.GetLanes() {
		lanes[index] = sim.Lane{
			ID: lane.GetId(), From: lane.GetFrom(), To: lane.GetTo(), SpeedLimit: lane.GetSpeedLimit(),
			SeparationGroup: lane.GetSeparationGroup(), StationID: lane.GetStationId(), StationRole: sim.StationLaneRole(lane.GetStationRole()),
		}
		if lane.Control != nil {
			lanes[index].Control = new(pointFromProto(lane.Control))
		}
	}
	stations := make([]sim.Station, len(network.GetStations()))
	for index, station := range network.GetStations() {
		berths := make([]sim.Berth, len(station.GetBerths()))
		for berthIndex, berth := range station.GetBerths() {
			berths[berthIndex] = sim.Berth{ID: berth.GetId(), Node: berth.GetNode(), SeparationGroup: berth.GetSeparationGroup()}
		}
		stations[index] = sim.Station{
			ID: station.GetId(), Name: station.GetName(), Entry: station.GetEntry(), Exit: station.GetExit(),
			Berths: berths, ParkingOnly: station.GetParkingOnly(),
		}
	}
	return sim.Network{Nodes: nodes, Lanes: lanes, Stations: stations}
}

func pointToProto(point sim.Point) *podsimv1.Point {
	return &podsimv1.Point{X: point.X, Y: point.Y}
}

func pointFromProto(point *podsimv1.Point) sim.Point {
	if point == nil {
		return sim.Point{}
	}
	return sim.Point{X: point.GetX(), Y: point.GetY()}
}

func commandFromProto(request *podsimv1.CommandRequest) (session.Command, error) {
	command := session.Command{Client: request.GetClient(), Sequence: request.GetSequence(), Epoch: request.GetEpoch()}
	switch action := request.GetCommand().(type) {
	case *podsimv1.CommandRequest_Trip:
		command.Action, command.Origin, command.Destination = "trip", action.Trip.GetOrigin(), action.Trip.GetDestination()
	case *podsimv1.CommandRequest_Pause:
		command.Action, command.Paused = "pause", action.Pause.GetPaused()
	case *podsimv1.CommandRequest_Speed:
		command.Action, command.Speed = "speed", int(action.Speed.GetSpeed())
	case *podsimv1.CommandRequest_Reset_:
		command.Action = "reset"
	case *podsimv1.CommandRequest_Demo:
		command.Action = "demo"
	case *podsimv1.CommandRequest_Demand:
		command.Action, command.Demand = "demand", demandConfigFromProto(action.Demand.GetDemand())
	case *podsimv1.CommandRequest_Project:
		config := configFromProto(action.Project.GetProject())
		command.Action, command.ProjectRevision, command.Project = "project", action.Project.GetProjectRevision(), &config
	default:
		return session.Command{}, errors.New("command action is required")
	}
	return command, nil
}

func replyToProto(reply session.Reply) *podsimv1.CommandResponse {
	return &podsimv1.CommandResponse{
		Epoch: reply.Epoch, Revision: reply.Revision, ProjectRevision: reply.ProjectRevision,
		Generation: reply.Generation, OrderId: protocolInt32(reply.OrderID), ErrorCode: errorCodeToProto(reply.ErrorCode), Error: reply.Error,
	}
}

func protocolInt32(value int) int32 {
	if value < -1<<31 || value > 1<<31-1 {
		panic("protocol integer exceeds int32")
	}
	return int32(value) // #nosec G115 -- the range check above proves this conversion safe.
}

func errorCodeToProto(code session.CommandErrorCode) podsimv1.CommandErrorCode {
	return map[session.CommandErrorCode]podsimv1.CommandErrorCode{
		session.SessionChanged:   podsimv1.CommandErrorCode_COMMAND_ERROR_CODE_SESSION_CHANGED,
		session.InvalidCommand:   podsimv1.CommandErrorCode_COMMAND_ERROR_CODE_INVALID_COMMAND,
		session.ExpiredCommand:   podsimv1.CommandErrorCode_COMMAND_ERROR_CODE_EXPIRED_COMMAND,
		session.SequenceConflict: podsimv1.CommandErrorCode_COMMAND_ERROR_CODE_SEQUENCE_CONFLICT,
		session.ClientLimit:      podsimv1.CommandErrorCode_COMMAND_ERROR_CODE_CLIENT_LIMIT,
		session.CommandRejected:  podsimv1.CommandErrorCode_COMMAND_ERROR_CODE_COMMAND_REJECTED,
	}[code]
}
