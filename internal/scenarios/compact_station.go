package scenarios

import (
	"fmt"
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// compactAisleY is the aisle axis of the compact station.
	compactAisleY = -4.008168830740232
	// compactPitch is the distance between two berths.
	compactPitch = 6.0
	// compactBerthHeading is the parked heading of a bay, 25 degrees.
	compactBerthHeading = 25 * math.Pi / 180
	// compactAxleOffset is the distance from the rear axle of a parked
	// body to the center of its envelope.
	compactAxleOffset = 1.2
	// compactMinRadius is the minimum turn radius of a Compact pod, and
	// compactRadiusMargin the margin of each generated radius.
	compactMinRadius    = 5.0
	compactRadiusMargin = 0.001
	// compactHoldX and compactReceiveX are the holding and receiving
	// nodes on the aisle axis. Each is more than 12 meters from the
	// nearest bay path.
	compactHoldX    = -19.14
	compactReceiveX = 28.40
	// compactRoad is the length of the lane before H and after R.
	compactRoad = 30.0
	// compactManeuverSpeed is the speed limit of a group lane.
	compactManeuverSpeed = 2.0
	// compactGroupID is the ID of the maneuver group.
	compactGroupID = "compact-bays"
)

// compactArc is an arc of a bay path: its signed sweep in radians and the
// number of lanes that it has.
type compactArc struct {
	sweep  float64
	pieces int
}

// compactBays holds the two arcs of each bay. The sweeps are the
// headings of the authored station-layout21 paths. Each bay ends at the
// parked heading.
var compactBays = [4][2]compactArc{
	{{sweep: 0.7846950891157451, pieces: 2}, {sweep: -0.3483627761171624, pieces: 2}},
	{{sweep: 0.41478980371256213, pieces: 2}, {sweep: 0.02154250928601975, pieces: 1}},
	{{sweep: 0.41478980371256213, pieces: 2}, {sweep: 0.02154250928601975, pieces: 1}},
	{{sweep: 0.41478980371256213, pieces: 2}, {sweep: 0.02154250928601975, pieces: 1}},
}

// compactPiece is one lane of a bay path: its end node and its control
// point, or nil for a straight lane.
type compactPiece struct {
	end     sim.Point
	control *sim.Point
}

// compactBerth returns the rear axle of a body parked in bay index, from
// 0.
func compactBerth(index int) sim.Point {
	return sim.Point{
		X: compactPitch*float64(index) - compactAxleOffset*math.Cos(compactBerthHeading),
		Y: -compactAxleOffset * math.Sin(compactBerthHeading),
	}
}

// compactArcRadius returns the radius of an arc with pieces lanes. A
// quadratic lane with its control at the tangent intersection has a
// radius of r cos(φ/2) at its middle, for a piece sweep φ, so the
// radius is increased until the middle is at the minimum radius.
func compactArcRadius(arc compactArc) float64 {
	return compactMinRadius/math.Cos(math.Abs(arc.sweep)/float64(2*arc.pieces)) + compactRadiusMargin
}

// compactArcChord returns the displacement of an arc that starts at a
// heading.
func compactArcChord(arc compactArc, heading float64) sim.Point {
	r := compactArcRadius(arc)
	chord := 2 * r * math.Sin(math.Abs(arc.sweep)/2)
	return sim.Point{X: chord * math.Cos(heading+arc.sweep/2), Y: chord * math.Sin(heading+arc.sweep/2)}
}

// compactArcPieces appends the lanes of an arc that starts at a point and
// a heading.
func compactArcPieces(pieces []compactPiece, arc compactArc, start sim.Point, heading float64) []compactPiece {
	r, sweep := compactArcRadius(arc), arc.sweep/float64(arc.pieces)
	for range arc.pieces {
		reach := r * math.Tan(math.Abs(sweep)/2)
		control := sim.Point{X: start.X + reach*math.Cos(heading), Y: start.Y + reach*math.Sin(heading)}
		chord := 2 * r * math.Sin(math.Abs(sweep)/2)
		start = sim.Point{X: start.X + chord*math.Cos(heading+sweep/2), Y: start.Y + chord*math.Sin(heading+sweep/2)}
		heading += sweep
		pieces = append(pieces, compactPiece{end: start, control: &control})
	}
	return pieces
}

// compactBay returns the branch node and the lanes of the path to bay
// index, from 0. The path leaves the aisle at heading 0, turns by the
// first arc, runs straight, and turns by the second arc to the berth. The
// branch x and the straight length are solved so that the path ends at
// the berth.
func compactBay(index int) (sim.Point, []compactPiece) {
	first, second := compactBays[index][0], compactBays[index][1]
	berth := compactBerth(index)
	a, b := compactArcChord(first, 0), compactArcChord(second, first.sweep)
	straight := (berth.Y - compactAisleY - a.Y - b.Y) / math.Sin(first.sweep)
	branch := sim.Point{X: berth.X - a.X - b.X - straight*math.Cos(first.sweep), Y: compactAisleY}
	pieces := compactArcPieces(nil, first, branch, 0)
	start := pieces[len(pieces)-1].end
	middle := sim.Point{X: start.X + straight*math.Cos(first.sweep), Y: start.Y + straight*math.Sin(first.sweep)}
	pieces = append(pieces, compactPiece{end: middle})
	return branch, compactArcPieces(pieces, second, middle, first.sweep)
}

// compactLayout holds a compact station: its entry, holding, receiving,
// and exit nodes and the nodes of its bays, its approach, bypass, and
// leave lanes and then its group lanes, its bays, and its maneuver group.
type compactLayout struct {
	nodes  []sim.Node
	lanes  []sim.Lane
	berths []sim.Berth
	group  sim.ManeuverGroup
}

// compactStation returns the compact station of the station ID. at moves
// a point of the station, or nil keeps the points. The station moves as a
// rigid body, so at must keep distances. Each node, lane, and berth ID and
// the group ID start with prefix.
func compactStation(at func(sim.Point) sim.Point, prefix, station string) compactLayout {
	move := func(point sim.Point) sim.Point { return point }
	if at != nil {
		move = at
	}
	moveControl := func(control *sim.Point) *sim.Point {
		if control == nil || at == nil {
			return control
		}
		return new(at(*control))
	}
	y := compactAisleY
	entryX, exitX := compactHoldX-compactRoad, compactReceiveX+compactRoad
	id := func(name string) string { return prefix + name }
	layout := compactLayout{
		nodes: []sim.Node{
			{ID: id("entry"), Position: move(sim.Point{X: entryX, Y: y})}, {ID: id("hold"), Position: move(sim.Point{X: compactHoldX, Y: y})},
			{ID: id("receive"), Position: move(sim.Point{X: compactReceiveX, Y: y})}, {ID: id("exit"), Position: move(sim.Point{X: exitX, Y: y})},
		},
		lanes: []sim.Lane{
			{ID: id("approach"), From: id("entry"), To: id("hold"), SpeedLimit: speedLimit},
			{ID: id("bypass"), From: id("entry"), To: id("exit"), SpeedLimit: speedLimit, Control: moveControl(&sim.Point{X: (entryX + exitX) / 2, Y: y - 32})},
			{ID: id("leave"), From: id("receive"), To: id("exit"), SpeedLimit: speedLimit},
		},
		group: sim.ManeuverGroup{ID: id(compactGroupID), StationID: station},
	}
	// A group lane or berth admits only Compact pods.
	compactClasses, _ := sim.NewClassSet(string(sim.CompactClass))
	addLane := func(name, from, to string, control *sim.Point, reverse bool) {
		layout.lanes = append(layout.lanes, sim.Lane{ID: id(name), From: id(from), To: id(to), SpeedLimit: compactManeuverSpeed, VehicleClasses: compactClasses, Control: moveControl(control)})
		layout.group.LaneIDs = append(layout.group.LaneIDs, id(name))
		if reverse {
			layout.group.ReverseLaneIDs = append(layout.group.ReverseLaneIDs, id(name))
		}
	}
	previous := "hold"
	for index := range compactBays {
		branch, pieces := compactBay(index)
		bay := index + 1
		branchID := fmt.Sprint("branch-", bay)
		layout.nodes = append(layout.nodes, sim.Node{ID: id(branchID), Position: move(branch)})
		addLane(fmt.Sprint("aisle-", bay), previous, branchID, nil, false)
		previous = branchID
		nodes := []string{branchID}
		for k, piece := range pieces {
			name := fmt.Sprintf("bay-%d-node-%d", bay, k+1)
			if k == len(pieces)-1 {
				name = fmt.Sprint("berth-", bay)
			}
			layout.nodes = append(layout.nodes, sim.Node{ID: id(name), Position: move(piece.end)})
			nodes = append(nodes, name)
		}
		for k, piece := range pieces {
			addLane(fmt.Sprintf("bay-%d-in-%d", bay, k+1), nodes[k], nodes[k+1], piece.control, false)
		}
		for k, piece := range slices.Backward(pieces) {
			addLane(fmt.Sprintf("bay-%d-out-%d", bay, k+1), nodes[k+1], nodes[k], piece.control, true)
		}
		berthID := id(fmt.Sprint("bay-", bay))
		layout.berths = append(layout.berths, sim.Berth{ID: berthID, Node: id(nodes[len(nodes)-1]), VehicleClasses: compactClasses})
		layout.group.BerthIDs = append(layout.group.BerthIDs, berthID)
	}
	addLane("aisle-out", previous, "receive", nil, false)
	return layout
}

// compactStationNetwork returns the network of the compact station
// preset: four bays for Compact pods in one maneuver group, the roads to
// and from them, and a loop through an ordinary station.
func compactStationNetwork() sim.Network {
	layout := compactStation(nil, "", "compact")
	network := sim.Network{
		Nodes:          layout.nodes,
		Lanes:          layout.lanes,
		Stations:       []sim.Station{{ID: "compact", Name: "Compact bays", Entry: "entry", Exit: "exit", Berths: layout.berths}},
		ManeuverGroups: []sim.ManeuverGroup{layout.group},
	}
	addCompactTown(&network, compactHoldX-compactRoad, compactReceiveX+compactRoad)
	return network
}

// addCompactTown adds an ordinary station with three berths below the
// compact station, as a ring station is laid out, and the loop lanes
// between the two stations.
func addCompactTown(network *sim.Network, entryX, exitX float64) {
	center := sim.Point{X: (entryX + exitX) / 2, Y: compactAisleY - 150}
	entry, exit := sim.Point{X: center.X + stationHalf, Y: center.Y}, sim.Point{X: center.X - stationHalf, Y: center.Y}
	network.Nodes = append(network.Nodes, sim.Node{ID: "town-entry", Position: entry}, sim.Node{ID: "town-exit", Position: exit})
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: "loop-down", From: "exit", To: "town-entry", SpeedLimit: speedLimit},
		sim.Lane{ID: "town-through", From: "town-entry", To: "town-exit", SpeedLimit: speedLimit},
		sim.Lane{ID: "loop-up", From: "town-exit", To: "entry", SpeedLimit: speedLimit},
	)
	station := sim.Station{ID: "town", Name: "Town", Entry: "town-entry", Exit: "town-exit"}
	for index := range 3 {
		depth := berthOffset + berthSpacing*float64(index)
		approach := stationHalf + 80 + 60*float64(index)
		node := fmt.Sprint("town-berth-", index+1)
		network.Nodes = append(network.Nodes, sim.Node{ID: node, Position: sim.Point{X: center.X, Y: center.Y - depth}})
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: fmt.Sprint("town-in-", index+1), From: "town-entry", To: node, SpeedLimit: speedLimit, Control: &sim.Point{X: center.X + approach, Y: center.Y - depth/2}},
			sim.Lane{ID: fmt.Sprint("town-out-", index+1), From: node, To: "town-exit", SpeedLimit: speedLimit, Control: &sim.Point{X: center.X - approach, Y: center.Y - depth/2}},
		)
		station.Berths = append(station.Berths, sim.Berth{ID: fmt.Sprint("town-", index+1), Node: node})
	}
	network.Stations = append(network.Stations, station)
}

// CompactStation returns the compact station preset. Six Compact pods
// start in three bays and in the three berths of the town station.
func CompactStation() (project.Config, error) {
	config := project.Default()
	config.Name, config.Network = "Compact station", compactStationNetwork()
	config.Fleet = []sim.Placement{
		{ID: "01", StationID: "compact", BerthID: "bay-1", Class: sim.CompactClass},
		{ID: "02", StationID: "compact", BerthID: "bay-2", Class: sim.CompactClass},
		{ID: "03", StationID: "compact", BerthID: "bay-3", Class: sim.CompactClass},
		{ID: "04", StationID: "town", BerthID: "town-1", Class: sim.CompactClass},
		{ID: "05", StationID: "town", BerthID: "town-2", Class: sim.CompactClass},
		{ID: "06", StationID: "town", BerthID: "town-3", Class: sim.CompactClass},
	}
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate compact station: %w", err)
	}
	return config, nil
}
