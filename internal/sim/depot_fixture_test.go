package sim

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// depotLayout describes a test depot that replaces the parking siding of
// Example. The lanes run west from the entry at (620, 460), each width
// meters south of the one before it, and pitch meters separate the slots of
// a lane.
type depotLayout struct {
	lanes, slots int
	pitch, width float64
}

// depotSpeed is the depot speed for a 6 m pitch: the speed at which
// compactClearance is 1 cm less than the pitch.
var depotSpeed = math.Sqrt(acceleration * (6 - 0.01 - compactBody - 1))

// depotSlotID returns the berth ID of a slot. lane and slot count from 1,
// and slot 1 is the head.
func depotSlotID(lane, slot int) string { return fmt.Sprintf("parking-%d-%d", lane, slot) }

// depotStagger is the distance by which the lead-in of each lane starts
// east of the lead-in of the lane north of it. The lead-outs end in the
// same way. Thus a fan or a throat that turns toward the next lane stays
// more than Clearance from it, also along the polyline of the curve.
const depotStagger = 1.0

// depotNetwork returns Example with a depot in place of its parking siding.
// Each fan lane curves from the entry to its lead-in and arrives parallel
// to the lanes, and each throat lane leaves its lead-out parallel to the
// lanes. Their curvature keeps them Clearance from the next lane. The
// lead-in is 2R long for the largest fan turn plus 1 cm, and the shortest
// lead-out is 2*Clearance long plus 1 cm.
func depotNetwork(layout depotLayout) Network {
	example := Example()
	network := Network{}
	for _, node := range example.Nodes {
		if !strings.HasPrefix(node.ID, "parking-berth") {
			network.Nodes = append(network.Nodes, node)
		}
	}
	for _, lane := range example.Lanes {
		if lane.StationID != "parking" {
			network.Lanes = append(network.Lanes, lane)
		}
	}
	network.Stations = slices.DeleteFunc(example.Stations, func(station Station) bool { return station.ID == "parking" })
	entry := Point{X: 620, Y: 460}
	// A curved fan or throat lane does not qualify, so it needs
	// 2*Clearance.
	fan := max(math.Ceil(layout.width*math.Sqrt(2*float64(layout.lanes)))+1, 2*Clearance)
	turn := math.Atan(2 * layout.width * float64(layout.lanes) / fan)
	leadIn := 2*reducedRetention(stopDistance(depotSpeed), turn) + 0.01
	chain := layout.pitch * float64(layout.slots-1)
	// The lead-in of lane n starts at start(n), and its lead-out ends at
	// tail(n).
	start := func(lane int) float64 { return entry.X - fan - float64(layout.lanes-lane)*depotStagger }
	tail := func(lane int) float64 {
		return start(1) - leadIn - chain - (2*Clearance + 0.01) - float64(lane-1)*depotStagger
	}
	exit := Point{X: tail(layout.lanes) - fan, Y: entry.Y}
	for i := range network.Nodes {
		if network.Nodes[i].ID == "parking-exit" {
			network.Nodes[i].Position = exit
		}
	}
	station := Station{ID: "parking", Name: "Parking", Entry: "parking-entry", Exit: "parking-exit", ParkingOnly: true}
	depot := Depot{Station: "parking"}
	addNode := func(id string, x, y float64) {
		network.Nodes = append(network.Nodes, Node{ID: id, Position: Point{X: x, Y: y}})
	}
	addLane := func(id, from, to string, speed float64, control *Point) {
		network.Lanes = append(network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: speed, Control: control})
	}
	for lane := 1; lane <= layout.lanes; lane++ {
		y := entry.Y + layout.width*float64(lane)
		lead, out := fmt.Sprintf("parking-lead-%d", lane), fmt.Sprintf("parking-tail-%d", lane)
		head := start(lane) - leadIn - chain
		addNode(lead, start(lane), y)
		addNode(out, tail(lane), y)
		addLane(fmt.Sprintf("parking-fan-%d", lane), "parking-entry", lead, depotSpeed, &Point{X: (entry.X + start(lane)) / 2, Y: y})
		var ids []string
		for slot := 1; slot <= layout.slots; slot++ {
			id := depotSlotID(lane, slot)
			addNode(id, head+layout.pitch*float64(slot-1), y)
			station.Berths = append(station.Berths, Berth{ID: id, Node: id})
			ids = append(ids, id)
			if slot > 1 {
				addLane(fmt.Sprintf("parking-chain-%d-%d", lane, slot-1), id, depotSlotID(lane, slot-1), depotSpeed, nil)
			}
		}
		addLane(fmt.Sprintf("parking-in-%d", lane), lead, depotSlotID(lane, layout.slots), depotSpeed, nil)
		addLane(fmt.Sprintf("parking-out-%d", lane), depotSlotID(lane, 1), out, depotSpeed, nil)
		addLane(fmt.Sprintf("parking-throat-%d", lane), out, "parking-exit", 5, &Point{X: (tail(lane) + exit.X) / 2, Y: y})
		depot.Lanes = append(depot.Lanes, DepotLane{BerthIDs: ids})
	}
	addLane("parking-through", "parking-entry", "parking-exit", 14, nil)
	network.Stations = append(network.Stations, station)
	network.Depots = []Depot{depot}
	return network
}

// depotPlacements returns a pod at each passenger station and count pods
// in the depot, placed from the head of each lane in lane order.
func depotPlacements(network Network, count int) []Placement {
	placements := []Placement{
		{ID: "harbor-pod", StationID: "harbor"}, {ID: "garden-pod", StationID: "garden"}, {ID: "market-pod", StationID: "market"},
	}
	lanes := network.Depots[0].Lanes
	for slot := 0; count > 0; slot++ {
		for _, lane := range lanes {
			if count > 0 && slot < len(lane.BerthIDs) {
				placements = append(placements, Placement{ID: fmt.Sprintf("depot-pod-%02d", len(placements)-2), StationID: "parking", BerthID: lane.BerthIDs[slot]})
				count--
			}
		}
	}
	return placements
}

// findLane returns a pointer to the lane with an ID.
func findLane(network *Network, id string) *Lane {
	for i := range network.Lanes {
		if network.Lanes[i].ID == id {
			return &network.Lanes[i]
		}
	}
	panic("unknown lane " + id)
}

// findNode returns a pointer to the node with an ID.
func findNode(network *Network, id string) *Node {
	for i := range network.Nodes {
		if network.Nodes[i].ID == id {
			return &network.Nodes[i]
		}
	}
	panic("unknown node " + id)
}
