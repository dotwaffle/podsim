package scenarios

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// minimumBerthPitch is the smallest distance between two berth rows. A berth
// chain has links of this length, and project validation needs lanes of at
// least 2*sim.Clearance. The extra meter keeps a link that the generator
// turns to a station heading above that limit, because the turn can make it
// a little shorter.
const minimumBerthPitch = 2*sim.Clearance + 1

// checkBerthPitch returns an error when the berth pitch is less than
// minimumBerthPitch or is not finite.
func checkBerthPitch(pitch float64) error {
	if !(pitch >= minimumBerthPitch) || math.IsInf(pitch, 1) {
		return fmt.Errorf("berth pitch must be a finite distance of at least %.0f meters", minimumBerthPitch)
	}
	return nil
}

// layoutConflict is one geometry problem in a generated network.
type layoutConflict struct {
	// station is the ID of the station that has the problem.
	station string
	// first and second name the items: lane IDs, node IDs, or station
	// names. second is empty when one item has the problem.
	first, second string
	// reason tells what is wrong.
	reason string
	// soft is true for a problem that the presets allow. The separation
	// oracle does not depend on it.
	soft bool
}

// describe returns the conflict as text, with the name and the ID of its
// station.
func (conflict layoutConflict) describe(network sim.Network) string {
	station := conflict.station
	if found, ok := network.Station(conflict.station); ok {
		station = fmt.Sprintf("%s (%s)", found.Name, found.ID)
	}
	if conflict.second == "" {
		return fmt.Sprintf("station %s: %s %s", station, conflict.first, conflict.reason)
	}
	return fmt.Sprintf("station %s: %s %s %s", station, conflict.first, conflict.reason, conflict.second)
}

// layoutError returns an error for the first hard conflict, or nil when
// there is no hard conflict. The error also gives the number of hard
// conflicts.
func layoutError(network sim.Network, conflicts []layoutConflict) error {
	hard := 0
	var first layoutConflict
	for _, conflict := range conflicts {
		if conflict.soft {
			continue
		}
		if hard == 0 {
			first = conflict
		}
		hard++
	}
	if hard == 0 {
		return nil
	}
	return fmt.Errorf("layout conflict at %s (%d hard conflicts)", first.describe(network), hard)
}

// countSoft returns the number of soft conflicts.
func countSoft(conflicts []layoutConflict) int {
	count := 0
	for _, conflict := range conflicts {
		if conflict.soft {
			count++
		}
	}
	return count
}

// auditLane is a lane as straight parts, with the box that holds them.
type auditLane struct {
	lane      sim.Lane
	parts     []londonSegment
	low, high sim.Point
}

// newAuditLanes returns each lane of the network as the straight parts of
// the path that a pod follows. The audit uses the same path as the
// simulator, so the clearances that it finds are the clearances of the
// pods.
func newAuditLanes(network sim.Network) []auditLane {
	lanes := make([]auditLane, 0, len(network.Lanes))
	for _, lane := range network.Lanes {
		points := network.Polyline(lane)
		parts := make([]londonSegment, 0, len(points)-1)
		for index := 1; index < len(points); index++ {
			parts = append(parts, londonSegment{from: points[index-1], to: points[index]})
		}
		audit := auditLane{lane: lane, parts: parts, low: parts[0].from, high: parts[0].from}
		for _, part := range parts {
			for _, point := range []sim.Point{part.from, part.to} {
				audit.low = sim.Point{X: min(audit.low.X, point.X), Y: min(audit.low.Y, point.Y)}
				audit.high = sim.Point{X: max(audit.high.X, point.X), Y: max(audit.high.Y, point.Y)}
			}
		}
		lanes = append(lanes, audit)
	}
	return lanes
}

// apart reports whether the boxes of the two lanes are at least the given
// distance apart.
func (lane auditLane) apart(other auditLane, distance float64) bool {
	return other.low.X-lane.high.X >= distance || lane.low.X-other.high.X >= distance ||
		other.low.Y-lane.high.Y >= distance || lane.low.Y-other.high.Y >= distance
}

// contactTolerance is the largest distance, in meters, at which two lane
// parts touch. It is much larger than the rounding errors of the lane
// paths, and much smaller than a clearance.
const contactTolerance = 1e-6

// crosses reports whether a part of one lane crosses or touches a part of
// the other lane. A touch is an end of a part on the other part, which
// includes two parts that overlap along a line. Two lanes that touch only
// at a node that both lanes have are connected, so they do not cross.
func (lane auditLane) crosses(other auditLane) bool {
	if lane.apart(other, contactTolerance) {
		return false
	}
	joints := lane.joints(other)
	for _, a := range lane.parts {
		if slices.ContainsFunc(other.parts, func(b londonSegment) bool { return a.crosses(b) || touches(a, b, joints) }) {
			return true
		}
	}
	return false
}

// joints returns the positions of the nodes that both lanes have.
func (lane auditLane) joints(other auditLane) []sim.Point {
	var joints []sim.Point
	if lane.lane.From == other.lane.From || lane.lane.From == other.lane.To {
		joints = append(joints, lane.parts[0].from)
	}
	if lane.lane.To == other.lane.From || lane.lane.To == other.lane.To {
		joints = append(joints, lane.parts[len(lane.parts)-1].to)
	}
	return joints
}

// touches reports whether an end of one part is on the other part at a
// point that is not one of the joints.
func touches(a, b londonSegment, joints []sim.Point) bool {
	atJoint := func(point sim.Point) bool {
		return slices.ContainsFunc(joints, func(joint sim.Point) bool {
			return math.Hypot(point.X-joint.X, point.Y-joint.Y) <= contactTolerance
		})
	}
	ends := [...]struct {
		point sim.Point
		part  londonSegment
	}{{a.from, b}, {a.to, b}, {b.from, a}, {b.to, a}}
	for _, end := range ends {
		if end.part.distance(end.point) <= contactTolerance && !atJoint(end.point) {
			return true
		}
	}
	return false
}

// near reports whether the two lanes cross or come nearer than the limit.
func (lane auditLane) near(other auditLane, limit float64) bool {
	if lane.apart(other, limit) {
		return false
	}
	for _, a := range lane.parts {
		if slices.ContainsFunc(other.parts, func(b londonSegment) bool { return a.near(b, limit) }) {
			return true
		}
	}
	return false
}

// sharesNode reports whether the two lanes have a common end node.
func sharesNode(a, b sim.Lane) bool {
	return a.From == b.From || a.From == b.To || a.To == b.From || a.To == b.To
}

// auditPlanarLayout returns the hard conflicts of a ring or mesh network.
// Two lanes that have no common node must not cross, and must be at least
// sim.Clearance apart.
// A conflict between lanes of two stations names the second station too.
func auditPlanarLayout(network sim.Network) []layoutConflict {
	names := make(map[string]string, len(network.Stations))
	for _, station := range network.Stations {
		names[station.ID] = station.Name
	}
	lanes := newAuditLanes(network)
	var conflicts []layoutConflict
	for index, first := range lanes {
		for _, second := range lanes[index+1:] {
			if sharesNode(first.lane, second.lane) || !first.near(second, sim.Clearance) {
				continue
			}
			station := first.lane.StationID
			if station == "" {
				station = second.lane.StationID
			}
			other := fmt.Sprintf("lane %q", second.lane.ID)
			if second.lane.StationID != "" && second.lane.StationID != station {
				other = fmt.Sprintf("%s lane %q", names[second.lane.StationID], second.lane.ID)
			}
			reason := fmt.Sprintf("is nearer than %.0f meters to", sim.Clearance)
			if first.crosses(second) {
				reason = "crosses"
			}
			conflicts = append(conflicts, layoutConflict{
				station: station, first: fmt.Sprintf("lane %q", first.lane.ID), second: other, reason: reason,
			})
		}
	}
	return conflicts
}

// londonLaneKind sorts the lanes of a London network for the audit.
type londonLaneKind int

const (
	londonMovementLane londonLaneKind = iota
	londonLinkLane
	londonRoadLane
	londonCoreLane
)

// londonKind returns the kind of a lane in a London network.
func londonKind(lane sim.Lane) londonLaneKind {
	switch {
	case strings.HasPrefix(lane.ID, "london-link-"):
		return londonLinkLane
	case lane.StationID == "":
		return londonMovementLane
	case strings.Contains(lane.ID, "-road-in-"), strings.Contains(lane.ID, "-road-out-"):
		return londonRoadLane
	}
	return londonCoreLane
}

// londonAuditInput holds the values that the London audit needs in addition
// to the network.
type londonAuditInput struct {
	// positions holds the TfL position of each passenger station.
	positions map[string]sim.Point
	// gateways holds the gateway station of each Parking facility.
	gateways map[string]string
}

// londonAuditStation holds the lanes and the area of one London station.
type londonAuditStation struct {
	station     sim.Station
	core, roads []auditLane
	all         []auditLane
	area        [4]sim.Point
	low, high   sim.Point
}

// auditLondonLayout returns the conflicts of a London network. These are
// hard conflicts:
//
//   - a core lane that crosses a guideway or a movement lane
//   - a lane that crosses a lane of another station, other than a road lane
//     of a Parking facility and a road lane of its gateway station
//   - a core lane nearer than londonStationGap to a core lane of another
//     station
//   - a core node inside the area of another station
//   - a road lane shorter than londonShortRoad
//
// These are soft conflicts:
//
//   - a passenger station whose berths are nearer to the TfL position of
//     another station than to its own TfL position
//   - a TfL position of another station inside the area of a station
//
// Road lanes can cross guideways and movement lanes, and movement lanes can
// cross each other, because each of these lanes has its own separation
// group.
func auditLondonLayout(network sim.Network, input londonAuditInput) []layoutConflict {
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	lanes := newAuditLanes(network)
	stations := make(map[string]*londonAuditStation, len(network.Stations))
	for _, station := range network.Stations {
		last := station.Berths[len(station.Berths)-1].ID
		stations[station.ID] = &londonAuditStation{
			station: station,
			area:    [4]sim.Point{nodes[station.Entry], nodes[station.Exit], nodes[last+"-departure"], nodes[last+"-arrival"]},
		}
	}
	var guideways []auditLane
	var conflicts []layoutConflict
	for _, lane := range lanes {
		kind := londonKind(lane.lane)
		if kind == londonLinkLane || kind == londonMovementLane {
			guideways = append(guideways, lane)
			continue
		}
		station, ok := stations[lane.lane.StationID]
		if !ok {
			continue
		}
		if len(station.all) == 0 {
			station.low, station.high = lane.low, lane.high
		}
		station.low = sim.Point{X: min(station.low.X, lane.low.X), Y: min(station.low.Y, lane.low.Y)}
		station.high = sim.Point{X: max(station.high.X, lane.high.X), Y: max(station.high.Y, lane.high.Y)}
		station.all = append(station.all, lane)
		if kind == londonRoadLane {
			station.roads = append(station.roads, lane)
			if length := network.Length(lane.lane); length < londonShortRoad {
				conflicts = append(conflicts, layoutConflict{
					station: station.station.ID, first: fmt.Sprintf("road lane %q", lane.lane.ID),
					reason: fmt.Sprintf("is %.1f meters, shorter than %.0f meters", length, londonShortRoad),
				})
			}
			continue
		}
		station.core = append(station.core, lane)
	}
	for _, source := range network.Stations {
		station := stations[source.ID]
		for _, core := range station.core {
			for _, guideway := range guideways {
				if core.crosses(guideway) {
					conflicts = append(conflicts, layoutConflict{
						station: station.station.ID, first: fmt.Sprintf("lane %q", core.lane.ID), second: fmt.Sprintf("guideway %q", guideway.lane.ID), reason: "crosses",
					})
				}
			}
		}
	}
	for index, first := range network.Stations {
		for _, second := range network.Stations[index+1:] {
			conflicts = append(conflicts, auditLondonStationPair(stations[first.ID], stations[second.ID], input, nodes)...)
		}
	}
	return append(conflicts, auditLondonPositions(network, stations, input)...)
}

// auditLondonStationPair returns the hard conflicts between two London
// stations.
func auditLondonStationPair(first, second *londonAuditStation, input londonAuditInput, nodes map[string]sim.Point) []layoutConflict {
	if second.low.X-first.high.X >= londonStationGap || first.low.X-second.high.X >= londonStationGap ||
		second.low.Y-first.high.Y >= londonStationGap || first.low.Y-second.high.Y >= londonStationGap {
		return nil
	}
	shared := input.gateways[first.station.ID] == second.station.ID || input.gateways[second.station.ID] == first.station.ID
	var conflicts []layoutConflict
	for _, a := range first.all {
		for _, b := range second.all {
			roads := londonKind(a.lane) == londonRoadLane && londonKind(b.lane) == londonRoadLane
			switch {
			case roads && shared:
			case a.crosses(b):
				conflicts = append(conflicts, layoutConflict{
					station: first.station.ID, first: fmt.Sprintf("lane %q", a.lane.ID),
					second: fmt.Sprintf("%s lane %q", second.station.Name, b.lane.ID), reason: "crosses",
				})
			case londonKind(a.lane) == londonCoreLane && londonKind(b.lane) == londonCoreLane && a.near(b, londonStationGap):
				conflicts = append(conflicts, layoutConflict{
					station: first.station.ID, first: fmt.Sprintf("lane %q", a.lane.ID),
					second: fmt.Sprintf("%s lane %q", second.station.Name, b.lane.ID), reason: fmt.Sprintf("is nearer than %.0f meters to", londonStationGap),
				})
			}
		}
	}
	for _, pair := range [][2]*londonAuditStation{{first, second}, {second, first}} {
		for _, lane := range pair[1].core {
			for _, node := range []string{lane.lane.From, lane.lane.To} {
				if inArea(nodes[node], pair[0].area) {
					conflicts = append(conflicts, layoutConflict{
						station: pair[1].station.ID, first: fmt.Sprintf("node %q", node),
						second: pair[0].station.Name, reason: "is inside the area of",
					})
				}
			}
		}
	}
	return conflicts
}

// auditLondonPositions returns the soft conflicts of the London stations
// with the TfL station positions.
func auditLondonPositions(network sim.Network, stations map[string]*londonAuditStation, input londonAuditInput) []layoutConflict {
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	var conflicts []layoutConflict
	for _, station := range network.Stations {
		own := station.ID
		if gateway, ok := input.gateways[station.ID]; ok {
			own = gateway
		}
		var middle sim.Point
		for _, berth := range station.Berths {
			middle = add(middle, scale(nodes[berth.Node], 1/float64(len(station.Berths))))
		}
		ownPosition := input.positions[own]
		ownDistance := math.Hypot(middle.X-ownPosition.X, middle.Y-ownPosition.Y)
		for _, other := range network.Stations {
			position, ok := input.positions[other.ID]
			if !ok || other.ID == own {
				continue
			}
			if !station.ParkingOnly && math.Hypot(middle.X-position.X, middle.Y-position.Y) < ownDistance {
				conflicts = append(conflicts, layoutConflict{
					station: station.ID, first: "berths", second: other.Name, reason: "are nearer to the TfL position of", soft: true,
				})
			}
			if inArea(position, stations[station.ID].area) {
				conflicts = append(conflicts, layoutConflict{
					station: station.ID, first: "the TfL position of " + other.Name, reason: "is inside the station area", soft: true,
				})
			}
		}
	}
	return conflicts
}

// newLondonAuditInput returns the audit input of the London source.
func newLondonAuditInput(source londonSource) londonAuditInput {
	input := londonAuditInput{
		positions: make(map[string]sim.Point, len(source.Stations)),
		gateways:  make(map[string]string, len(londonParkingFacilities)),
	}
	for _, station := range source.Stations {
		input.positions[station.ID] = londonPoint(station.Latitude, station.Longitude)
	}
	for _, parking := range londonParkingFacilities {
		input.gateways[parking.ID] = parking.Gateway
	}
	return input
}

// LondonSoftConflicts returns the number of soft layout conflicts of a
// London network.
func LondonSoftConflicts(network sim.Network) (int, error) {
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		return 0, err
	}
	return countSoft(auditLondonLayout(network, newLondonAuditInput(source))), nil
}
