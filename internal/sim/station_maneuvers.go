package sim

import "slices"

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
// roles as new projects. Explicit project roles always win. A lane of a
// maneuver group gets no role: routeStationPhase names its phase.
func inferStationLaneRoles(n *Network) {
	graph := newRouteGraph(*n)
	var grouped map[string]bool
	for _, group := range n.ManeuverGroups {
		if grouped == nil {
			grouped = make(map[string]bool)
		}
		for _, id := range group.LaneIDs {
			grouped[id] = true
		}
	}
	forbidden := n.stationForbidden()
	// through holds the IDs of the lanes between two nodes, in lane order.
	through := make(map[[2]string][]string)
	for _, lane := range n.Lanes {
		through[[2]string{lane.From, lane.To}] = append(through[[2]string{lane.From, lane.To}], lane.ID)
	}
	// The searches share one work area, as in validateStationRoutes.
	var work routeSearchWork
	for _, station := range n.Stations {
		if station.Banks != nil {
			continue
		}
		for _, id := range through[[2]string{station.Entry, station.Exit}] {
			setStationLaneRole(n, graph, grouped, id, station.ID, StationThroughRole)
		}
		for _, berth := range station.Berths {
			berthForbidden := n.depotForbidden(forbidden, berth.ID)
			arrival, err := n.routeIndexedWithWork(networkRouteInput{class: topologyClass, from: station.Entry, to: berth.Node, forbidden: berthForbidden}, graph, &work)
			if err == nil {
				for _, lane := range arrival {
					setStationLaneRole(n, graph, grouped, lane.ID, station.ID, StationBerthAccessRole)
				}
			}
			departure, err := n.routeIndexedWithWork(networkRouteInput{class: topologyClass, from: berth.Node, to: station.Exit, forbidden: berthForbidden}, graph, &work)
			if err == nil {
				for _, lane := range departure {
					setStationLaneRole(n, graph, grouped, lane.ID, station.ID, StationDepartureRole)
				}
			}
		}
	}
}

func setStationLaneRole(n *Network, graph routeGraph, grouped map[string]bool, laneID, stationID string, role StationLaneRole) {
	index, ok := graph.lanes[laneID]
	if !ok || n.Lanes[index].StationRole != "" || grouped[laneID] {
		return
	}
	n.Lanes[index].StationID = stationID
	n.Lanes[index].StationRole = role
}

// stationPhaseCheck holds the phase for one block of the current route.
// replaceRoute clears it, including when routeVersion cannot increase.
type stationPhaseCheck struct {
	blockIndex int
	phase      StationPhase
	stationID  string
	valid      bool
}

func (s *Simulation) updateStationPhase(v *vehicle) {
	if v.Pod.BerthID != "" {
		v.Pod.StationPhase = AtBerth
		v.Pod.ManeuverStationID = v.Pod.StationID
		return
	}
	if !v.stationPhase.valid || v.stationPhase.blockIndex != v.blockIndex {
		phase, stationID := routeStationPhase(v, s.maneuvers)
		v.stationPhase = stationPhaseCheck{blockIndex: v.blockIndex, phase: phase, stationID: stationID, valid: true}
	}
	v.Pod.StationPhase = v.stationPhase.phase
	v.Pod.ManeuverStationID = v.stationPhase.stationID
}

// routeStationPhase returns the phase of the lane that v is in. A lane of a
// maneuver group with no role is in the run of group lanes at the start of
// the route, after the origin berth, or in the run at the end, before the
// route berth. The first is DepartingBerth and the second AccessingBerth.
// The phase reads the route around the route index of the block of v,
// because a route can use a lane more than once.
func routeStationPhase(v *vehicle, maneuvers *maneuverIndex) (StationPhase, string) {
	index := currentManeuverIndex(v)
	if index < 0 {
		return "", ""
	}
	lane := &v.blocks.route[index]
	if phase := phaseForStationRole(lane.StationRole); phase != "" {
		return phase, lane.StationID
	}
	if group, ok := maneuvers.laneGroup(lane.ID); ok {
		grouped := func(lane Lane) bool { return maneuvers.groupLane(lane.ID) }
		switch {
		case !slices.ContainsFunc(v.Route[:index], func(lane Lane) bool { return !grouped(lane) }):
			return DepartingBerth, group.StationID
		case !slices.ContainsFunc(v.Route[index+1:], func(lane Lane) bool { return !grouped(lane) }):
			return AccessingBerth, group.StationID
		}
	}
	if index+1 < len(v.Route) {
		next := v.Route[index+1]
		if next.StationRole == StationEntryRole || next.StationRole == StationBerthAccessRole {
			return ApproachingStation, next.StationID
		}
	}
	return "", ""
}

// currentManeuverIndex returns the route index of the lane of the block
// that v is in, or -1.
func currentManeuverIndex(v *vehicle) int {
	if v.blockIndex < 0 || v.blockIndex >= v.blocks.len() {
		return -1
	}
	return v.blocks.currentRouteLane(v.blockIndex)
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
