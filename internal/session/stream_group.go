package session

import (
	"errors"

	"github.com/dotwaffle/podsim/internal/sim"
)

func (a *StreamAssembler) groupBindings(frame StreamFrame) error {
	classes := make(map[string]sim.VehicleClass, len(frame.State.Simulation.Vehicles))
	for _, vehicle := range frame.State.Simulation.Vehicles {
		classes[vehicle.Pod.ID] = vehicle.Pod.Class
	}
	for index, vehicle := range frame.State.Simulation.Vehicles {
		pod := vehicle.Pod
		if pod.Class == sim.GroupClass && vehicle.PlatoonIndex != 0 || vehicle.PlatoonID != "" && (pod.Class == sim.GroupClass || classes[vehicle.PlatoonID] == sim.GroupClass) {
			return errors.New("group vehicles cannot have platoon links")
		}
		if pod.Class != sim.GroupClass {
			continue
		}
		if pod.StationID != "" || pod.BerthID != "" {
			berth, found := a.boardingBerths[pod.BerthID]
			if !found || berth.station != pod.StationID || !berth.stationClasses.Allows(string(pod.Class)) || !berth.classes.Allows(string(pod.Class)) {
				return errors.New("group vehicle is not at a compatible station berth")
			}
		}
		if pod.LaneID != "" && !a.groupLanes[pod.LaneID] {
			return errors.New("group vehicle is on an incompatible lane")
		}
		for _, indexes := range [][]int{frame.Routes[index].Display, frame.Routes[index].Lanes} {
			for _, laneIndex := range indexes {
				if laneIndex < 0 || laneIndex >= len(a.topology.Network.Lanes) || !a.groupLanes[a.topology.Network.Lanes[laneIndex].ID] {
					return errors.New("group route contains an incompatible lane")
				}
			}
		}
	}
	return nil
}
