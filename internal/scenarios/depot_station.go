package scenarios

import (
	"fmt"
	"math"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// depotPitch is the distance in meters between two slots of a depot
	// lane, and depotWidth the distance between two depot lanes.
	depotPitch = 6.0
	depotWidth = 12.0
	// depotLead is the length of the lead-in and of the shortest run lane.
	// It is more than twice the reduced retention of a small pod for any
	// fan turn. The shortest run lane keeps the throat out of the windows
	// of the chain lanes and of the lead-out, so they qualify with no turn.
	depotLead = 2*sim.Clearance + 0.01
	// depotLeadOut is the length of the lead-out, the depot speed lane from
	// the head slot to the run lane. The run lane is faster than the depot
	// speed, so the lane gap audit needs sim.Clearance between it and the
	// chain lane to the head slot. The lead-out is also more than twice its
	// reduced retention with no turn.
	depotLeadOut = sim.Clearance + 0.01
	// depotStagger is the distance by which each lead-in starts after the
	// lead-in of the lane outside it, and by which each run lane ends after
	// the run lane of the lane inside it. A fan or a throat that turns
	// across the lanes inside it then stays sim.Clearance from them.
	depotStagger = 1.0
	// depotThroatSpeed is the speed limit of the run lanes and of the
	// curved throat lanes.
	depotThroatSpeed = 5.0
)

// depotLayout is the depot of a ring Parking station: lanes depot lanes of
// slots slots each.
type depotLayout struct {
	lanes, slots int
}

// fan returns the length along the station of each fan and throat lane.
// It lets the outer fan reach its lane, and it is at least 2*sim.Clearance,
// because a curved lane does not qualify.
func (layout depotLayout) fan() float64 {
	return max(math.Ceil(depotWidth*math.Sqrt(2*float64(layout.lanes)))+1, 2*sim.Clearance)
}

// start returns the distance from the entry to the start of the lead-in of
// lane, which counts from 1 at the lane nearest to the through lane.
func (layout depotLayout) start(lane int) float64 {
	return layout.fan() + float64(layout.lanes-lane)*depotStagger
}

// slot returns the distance from the entry to a slot of lane. slot 1 is
// the head.
func (layout depotLayout) slot(lane, slot int) float64 {
	return layout.start(lane) + depotLead + depotPitch*float64(layout.slots-slot)
}

// clear returns the distance from the entry to the end of the lead-out of
// lane.
func (layout depotLayout) clear(lane int) float64 {
	return layout.slot(lane, 1) + depotLeadOut
}

// tail returns the distance from the entry to the end of the run lane of
// lane.
func (layout depotLayout) tail(lane int) float64 {
	return layout.clear(1) + depotLead + float64(lane-1)*depotStagger
}

// length returns the distance from the entry to the exit.
func (layout depotLayout) length() float64 {
	return layout.tail(layout.lanes) + layout.fan()
}

// nodes and laneCount return the number of nodes and lanes of the depot
// station, without its entry and exit nodes and its through lane.
func (layout depotLayout) nodes() int     { return layout.lanes * (layout.slots + 3) }
func (layout depotLayout) laneCount() int { return layout.lanes * (layout.slots + 4) }

// checkDepotLayout checks the lane and slot counts of a depot. The caller
// refuses a negative lane count, and a depot has at least one lane. The
// slot check divides, so a large slot count cannot wrap the product. The
// geometry and the placements of the depot need these bounds.
func checkDepotLayout(layout depotLayout) error {
	if layout.lanes > sim.MaxDepotLanes {
		return fmt.Errorf("parking lanes must be at most %d", sim.MaxDepotLanes)
	}
	if layout.slots < 1 || layout.slots > project.MaxBerths/layout.lanes {
		return fmt.Errorf("the depot has %d lanes of %d slots, want 1 to %d slots in all", layout.lanes, layout.slots, project.MaxBerths)
	}
	return nil
}

// depotSlotID returns the berth ID of a depot slot.
func depotSlotID(lane, slot int) string {
	return fmt.Sprintf("parking-%d-%02d", lane, slot)
}

// depotGeometry returns the Parking station of a ring as a depot. The
// depot lanes run along the station from the entry side to the exit side,
// depotWidth apart and outside the through lane. A curved fan lane goes
// from the entry to the lead-in of each lane. A straight lead-out leaves
// the head slot of each lane, a straight run lane goes on from it, and a
// curved throat lane goes from the run lane to the exit. The lead-in, the
// chain lanes and the lead-out run at the depot speed. The run lane and the
// throat run at depotThroatSpeed, so a departing pod speeds up after
// depotLeadOut.
func depotGeometry(parameters stationParameters, layout depotLayout) (sim.Station, []sim.Node, []sim.Lane, sim.Depot) {
	theta := 2 * math.Pi * float64(parameters.index) / float64(parameters.count)
	radius := 180 * float64(parameters.count)
	center := sim.Point{X: radius * math.Cos(theta), Y: radius * math.Sin(theta)}
	tangent := sim.Point{X: -math.Sin(theta), Y: math.Cos(theta)}
	radial := sim.Point{X: math.Cos(theta), Y: math.Sin(theta)}
	half := layout.length() / 2
	// at returns the point along meters from the entry and across meters
	// outside the through lane.
	at := func(along, across float64) sim.Point {
		return add(add(center, scale(tangent, along-half)), scale(radial, across))
	}
	prefix := fmt.Sprintf("s%02d", parameters.index+1)
	entryID, exitID := stationNodeID(parameters.index, "entry"), stationNodeID(parameters.index, "exit")
	speed := sim.DepotSpeed(depotPitch)
	station := sim.Station{ID: "parking", Name: "Parking", Entry: entryID, Exit: exitID, ParkingOnly: true}
	depot := sim.Depot{Station: station.ID}
	nodes := []sim.Node{{ID: entryID, Position: at(0, 0)}, {ID: exitID, Position: at(layout.length(), 0)}}
	lanes := []sim.Lane{{
		ID: stationLaneID(parameters.index, "through"), From: entryID, To: exitID, SpeedLimit: speedLimit,
		StationID: station.ID, StationRole: sim.StationThroughRole,
	}}
	for lane := 1; lane <= layout.lanes; lane++ {
		across := depotWidth * float64(lane)
		lead, cleared, tail := fmt.Sprintf("%s-lead-%d", prefix, lane), fmt.Sprintf("%s-clear-%d", prefix, lane), fmt.Sprintf("%s-tail-%d", prefix, lane)
		slotNode := func(slot int) string { return fmt.Sprintf("%s-slot-%d-%02d", prefix, lane, slot) }
		nodes = append(nodes, sim.Node{ID: lead, Position: at(layout.start(lane), across)},
			sim.Node{ID: cleared, Position: at(layout.clear(lane), across)}, sim.Node{ID: tail, Position: at(layout.tail(lane), across)})
		lanes = append(lanes,
			sim.Lane{ID: fmt.Sprintf("%s-fan-%d", prefix, lane), From: entryID, To: lead, SpeedLimit: speed, Control: new(at(layout.start(lane)/2, across))},
			sim.Lane{ID: fmt.Sprintf("%s-in-%d", prefix, lane), From: lead, To: slotNode(layout.slots), SpeedLimit: speed},
		)
		var ids []string
		for slot := layout.slots; slot >= 1; slot-- {
			nodes = append(nodes, sim.Node{ID: slotNode(slot), Position: at(layout.slot(lane, slot), across)})
			if slot < layout.slots {
				lanes = append(lanes, sim.Lane{ID: fmt.Sprintf("%s-chain-%d-%02d", prefix, lane, slot), From: slotNode(slot + 1), To: slotNode(slot), SpeedLimit: speed})
			}
		}
		for slot := 1; slot <= layout.slots; slot++ {
			station.Berths = append(station.Berths, sim.Berth{ID: depotSlotID(lane, slot), Node: slotNode(slot)})
			ids = append(ids, depotSlotID(lane, slot))
		}
		lanes = append(lanes,
			sim.Lane{ID: fmt.Sprintf("%s-out-%d", prefix, lane), From: slotNode(1), To: cleared, SpeedLimit: speed},
			sim.Lane{ID: fmt.Sprintf("%s-run-%d", prefix, lane), From: cleared, To: tail, SpeedLimit: depotThroatSpeed},
			sim.Lane{ID: fmt.Sprintf("%s-throat-%d", prefix, lane), From: tail, To: exitID, SpeedLimit: depotThroatSpeed, Control: new(at((layout.tail(lane)+layout.length())/2, across))},
		)
		depot.Lanes = append(depot.Lanes, sim.DepotLane{BerthIDs: ids})
	}
	return station, nodes, lanes, depot
}

// depotPlacements returns count pods at the depot slots from the head:
// slot 1 of each lane in lane order, then slot 2, and so on. first is the
// number of the first pod.
func depotPlacements(layout depotLayout, count, first int) []sim.Placement {
	placements := make([]sim.Placement, 0, count)
	for slot := 1; len(placements) < count; slot++ {
		for lane := 1; lane <= layout.lanes && len(placements) < count; lane++ {
			placements = append(placements, sim.Placement{
				ID: fmt.Sprintf("pod-%03d", first+len(placements)), StationID: "parking", BerthID: depotSlotID(lane, slot),
			})
		}
	}
	return placements
}
