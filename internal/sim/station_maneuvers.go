package sim

// StationPhase describes a pod's current physical station maneuver.
type StationPhase string

// Station phases identify the station maneuver that a pod is performing.
const (
	ApproachingStation StationPhase = "Approaching station"
	EnteringStation    StationPhase = "Entering station"
	AccessingBerth     StationPhase = "Accessing berth"
	PassingStation     StationPhase = "Passing station"
	AtBerth            StationPhase = "At berth"
	DepartingBerth     StationPhase = "Departing berth"
	ExitingStation     StationPhase = "Exiting station"
)

// inferStationLaneRoles gives legacy station-local paths the same observable
// roles as new projects. Explicit project roles always win.
func inferStationLaneRoles(n *Network) {
	graph := newRouteGraph(*n)
	forbidden := n.stationForbidden()
	for _, station := range n.Stations {
		for _, lane := range n.Lanes {
			if lane.From == station.Entry && lane.To == station.Exit {
				setStationLaneRole(n, graph, lane.ID, station.ID, StationThroughRole)
			}
		}
		for _, berth := range station.Berths {
			arrival, err := n.routeIndexed(networkRouteInput{from: station.Entry, to: berth.Node, forbidden: forbidden}, graph)
			if err == nil {
				for _, lane := range arrival {
					setStationLaneRole(n, graph, lane.ID, station.ID, StationBerthAccessRole)
				}
			}
			departure, err := n.routeIndexed(networkRouteInput{from: berth.Node, to: station.Exit, forbidden: forbidden}, graph)
			if err == nil {
				for _, lane := range departure {
					setStationLaneRole(n, graph, lane.ID, station.ID, StationDepartureRole)
				}
			}
		}
	}
}

func setStationLaneRole(n *Network, graph routeGraph, laneID, stationID string, role StationLaneRole) {
	index, ok := graph.lanes[laneID]
	if !ok || n.Lanes[index].StationRole != "" {
		return
	}
	n.Lanes[index].StationID = stationID
	n.Lanes[index].StationRole = role
}

func (s *Simulation) updateStationPhase(v *vehicle) {
	if v.Pod.BerthID != "" {
		v.Pod.StationPhase = AtBerth
		v.Pod.ManeuverStationID = v.Pod.StationID
		return
	}
	lane := currentManeuverLane(v)
	if lane == nil {
		v.Pod.StationPhase = ""
		v.Pod.ManeuverStationID = ""
		return
	}
	if phase := phaseForStationRole(lane.StationRole); phase != "" {
		v.Pod.StationPhase = phase
		v.Pod.ManeuverStationID = lane.StationID
		return
	}
	index := v.blocks.locate(v.firstBlockForLane(lane.ID), 0)
	if index >= 0 && index+1 < len(v.Route) {
		next := v.Route[index+1]
		if next.StationRole == StationEntryRole || next.StationRole == StationBerthAccessRole {
			v.Pod.StationPhase = ApproachingStation
			v.Pod.ManeuverStationID = next.StationID
			return
		}
	}
	v.Pod.StationPhase = ""
	v.Pod.ManeuverStationID = ""
}

// currentManeuverLane returns the lane of the block that v is in, or nil.
func currentManeuverLane(v *vehicle) *Lane {
	if v.blockIndex < 0 || v.blockIndex >= v.blocks.len() {
		return nil
	}
	return v.blocks.currentLane(v.blockIndex)
}

func phaseForStationRole(role StationLaneRole) StationPhase {
	switch role {
	case StationApproachRole:
		return ApproachingStation
	case StationEntryRole:
		return EnteringStation
	case StationBerthAccessRole:
		return AccessingBerth
	case StationThroughRole:
		return PassingStation
	case StationDepartureRole:
		return DepartingBerth
	case StationExitRole:
		return ExitingStation
	default:
		return ""
	}
}
