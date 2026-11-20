package scenarios

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// sharedTrackGeometry holds the geometry changes of the shared-track
// preset (docs/design/shared-track-preset.md, section 3.5).
type sharedTrackGeometry struct {
	// part is the size of each part of the tight shape at the two hubs:
	// side, throat to entry, entry to the first row, and pitch.
	part float64
	// throat holds the depth of the diverge and the merge from the
	// junction center of a hub, by station. A hub that is not in the map
	// keeps londonStationThroatDepth.
	throat map[string]float64
	// turn holds the turn of a hub about its junction center, in degrees,
	// by station. A positive turn increases the heading of the axis.
	turn map[string]float64
	// curve is a hub lane that gets a control point offset meters from the
	// middle of its chord, on the side away from the junction center.
	curve  string
	offset float64
	// remove holds the junction lanes that the change removes.
	remove []string
}

// sharedTrackPresetGeometry returns the changes of section 3.5 of the
// design: hub parts of 41 m, the Waterloo throat at 90 m, Bank turned by
// +5 degrees, a curve on the Waterloo road-in-05, and two junction lanes
// removed. A part of 40 m gives lanes of 40.0 m that preparation refuses
// by rounding.
func sharedTrackPresetGeometry() sharedTrackGeometry {
	return sharedTrackGeometry{
		part:   41,
		throat: map[string]float64{expressWaterloo: 90},
		turn:   map[string]float64{expressBank: 5},
		curve:  expressWaterloo + "-road-in-05", offset: 30,
		remove: []string{expressWaterloo + "-move-05-01", expressBank + "-move-08-06"},
	}
}

// sharedTrackRoute holds the lanes of the shared route between Waterloo and
// Bank, in both directions (section 1 of the design).
var sharedTrackRoute = []string{
	expressWaterloo + "-access-out", expressWaterloo + "-road-out-01", "london-link-100-ba-1", "london-link-100-ba-2", expressBank + "-road-in-08", expressBank + "-access-in",
	expressBank + "-access-out", expressBank + "-road-out-08", "london-link-100-ab-1", "london-link-100-ab-2", expressWaterloo + "-road-in-01", expressWaterloo + "-access-in",
}

// GenerateLondonFullSharedExpress returns the opt-in LondonFull Shared
// Express preset without a cache. It is decision D11 of the shape
// clearance design: LondonFull with Express on the main-line track of link
// 100 between Waterloo and Bank, shared with Compact pods. Its 16 Express
// pods start in the Express parking of decision S4 beside Waterloo. It
// builds on LondonFull with straight roads (see sharedTrackBase).
func GenerateLondonFullSharedExpress() (project.Config, error) {
	config, err := sharedTrackBase()
	if err != nil {
		return project.Config{}, err
	}
	return londonFullSharedExpressPreset(config)
}

// sharedTrackBase returns the LondonFull project that the preset builds
// on: LondonFull with straight roads. Decision FL-D4 B of
// docs/contracts/london-fillets-proposal.md keeps the preset on this base
// until the maintainer accepts the evidence of the filleted LondonFull.
func sharedTrackBase() (project.Config, error) {
	return generateLondonFull(func(options *LondonOptions) { options.RoadFillets = false })
}

// londonFullSharedExpressPreset returns the preset from c, which is a
// LondonFull project that the caller owns. It adds the shared route and
// the Express parking, and it runs the layout audit of the preset.
func londonFullSharedExpressPreset(c project.Config) (project.Config, error) {
	source, err := decodeLondonFullSource()
	if err != nil {
		return project.Config{}, err
	}
	c, err = londonFullSharedExpress(c, sharedTrackPresetGeometry())
	if err == nil {
		err = addSharedTrackParking(&c, sharedTrackPresetParking())
	}
	if err == nil {
		err = layoutError(c.Network, auditLondonLayout(c.Network, sharedTrackLayoutInput(source)))
	}
	if err != nil {
		return project.Config{}, err
	}
	return c, nil
}

// sharedTrackLayoutInput returns the input of the London layout audit for
// the preset. The Express parking is a Parking facility of Waterloo, so
// that its road from the comb to the arrival portal of link 100 can cross
// the curved Waterloo road-in-05 (section 3.7 of the design).
func sharedTrackLayoutInput(source londonSource) londonAuditInput {
	input := newLondonAuditInput(source)
	input.gateways[expressParking] = expressWaterloo
	return input
}

// The portals of link 100 at Waterloo.
const (
	sharedTrackArrivalPortal   = "london-link-100-b-arrival"
	sharedTrackDeparturePortal = "london-link-100-b-departure"
)

// sharedTrackParking holds the site of the Express parking comb.
type sharedTrackParking struct {
	// center is the middle of the entry and the exit of the comb.
	center sim.Point
	// heading is the direction, in degrees, in which the rows follow each
	// other from the through lane.
	heading float64
	// part is the length of each berth lane and row link, and the pitch
	// of the rows.
	part float64
}

// sharedTrackPresetParking returns the site P4 of section 3.7 of the
// design: between the Waterloo rows and link 100, with the rows at a
// heading of -35 degrees. A part of 40 m gives lanes that project
// validation refuses by rounding, so 41 m is the value.
func sharedTrackPresetParking() sharedTrackParking {
	return sharedTrackParking{center: sim.Point{X: 1000, Y: 40}, heading: -35, part: 41}
}

// addSharedTrackParking adds the Express parking comb, its two roads, and
// the Express pods to c. A pod from Waterloo reaches the comb from the
// departure portal of link 100, and a pod from the comb reaches Waterloo
// from the arrival portal.
func addSharedTrackParking(c *project.Config, parking sharedTrackParking) error {
	express, err := sim.NewClassSet("express")
	if err != nil {
		return err
	}
	n := &c.Network
	for _, id := range []string{sharedTrackArrivalPortal, sharedTrackDeparturePortal} {
		if _, ok := n.Node(id); !ok {
			return fmt.Errorf("unknown node %q", id)
		}
	}
	heading := parking.heading * math.Pi / 180
	outward := sim.Point{X: math.Cos(heading), Y: math.Sin(heading)}
	along := sim.Point{X: outward.Y, Y: -outward.X}
	entry := sim.Point{X: parking.center.X - parking.part*along.X, Y: parking.center.Y - parking.part*along.Y}
	at := func(a, o float64) sim.Point {
		return sim.Point{X: entry.X + a*along.X + o*outward.X, Y: entry.Y + a*along.Y + o*outward.Y}
	}
	addExpressComb(n, express, at, parking.part, parking.part)
	station, _ := n.Station(expressParking)
	in, out := expressParking+"-road-in-01", expressParking+"-road-out-01"
	addExpressLane(n, in, sharedTrackDeparturePortal, station.Entry, expressParking, sim.StationApproachRole, in, express)
	addExpressLane(n, out, station.Exit, sharedTrackArrivalPortal, expressParking, sim.StationExitRole, out, express)
	for i := range expressPods {
		c.Fleet = append(c.Fleet, sim.Placement{ID: fmt.Sprintf("express-%02d", i+1), Class: sim.ExpressClass, StationID: expressParking, BerthID: fmt.Sprintf("xp-%02d", i+1)})
	}
	return nil
}

// londonFullSharedExpress adds the shared route, the geometry changes, and
// the Express services to c, which is a LondonFull project that the caller
// owns. It adds no Express pod: its network is that of variant 3.5 of the
// shared-track study.
func londonFullSharedExpress(c project.Config, geometry sharedTrackGeometry) (project.Config, error) {
	mixed, err := sim.NewClassSet("legacy", "compact", "express")
	if err != nil {
		return project.Config{}, err
	}
	n := &c.Network
	setSharedTrackClasses(n, mixed)
	if err := geometry.apply(n); err != nil {
		return project.Config{}, err
	}
	c.Name = "LondonFull Shared Express"
	for _, pair := range [][2]string{{expressWaterloo, expressBank}, {expressBank, expressWaterloo}} {
		c.ExpressServices = append(c.ExpressServices, sim.ExpressService{
			ID: "xs-" + pair[0][8:] + "-" + pair[1][8:], From: pair[0], To: pair[1], Class: sim.ExpressClass, PartyLimit: 20,
		})
	}
	c.OrderContract = sim.ExpressOrderContract
	return c, nil
}

// setSharedTrackClasses sets classes on the lanes of the shared route, on
// each lane of the two hubs except the roads that are not on the route,
// and on the two hubs and their berths.
func setSharedTrackClasses(n *sim.Network, classes sim.ClassSet) {
	hubs := []string{expressWaterloo, expressBank}
	for i := range n.Lanes {
		lane := &n.Lanes[i]
		hub := slices.Contains(hubs, lane.StationID) && !strings.Contains(lane.ID, "-road-")
		if hub || slices.Contains(sharedTrackRoute, lane.ID) {
			lane.VehicleClasses = classes
		}
	}
	for i := range n.Stations {
		station := &n.Stations[i]
		if !slices.Contains(hubs, station.ID) {
			continue
		}
		station.VehicleClasses = classes
		for j := range station.Berths {
			station.Berths[j].VehicleClasses = classes
		}
	}
}

// sharedTrackFrame is the frame of a London station: its junction center,
// the axis from the diverge to the merge, and the outward axis to the
// berth rows.
type sharedTrackFrame struct {
	center, along, outward sim.Point
}

func (f sharedTrackFrame) at(along, outward float64) sim.Point {
	return sim.Point{X: f.center.X + along*f.along.X + outward*f.outward.X, Y: f.center.Y + along*f.along.Y + outward*f.outward.Y}
}

// sharedTrackStationFrame recovers the frame of a tight London station
// from the positions of its diverge, merge, entry, and exit.
func sharedTrackStationFrame(positions map[string]sim.Point, station sim.Station) (sharedTrackFrame, error) {
	diverge, merge := positions[station.ID+"-diverge"], positions[station.ID+"-merge"]
	entry, exit := positions[station.Entry], positions[station.Exit]
	width := math.Hypot(merge.X-diverge.X, merge.Y-diverge.Y)
	if math.Abs(width-2*londonStationThroatOffset) > 1e-6 {
		return sharedTrackFrame{}, fmt.Errorf("%s: throat width %g m", station.ID, width)
	}
	along := sim.Point{X: (merge.X - diverge.X) / width, Y: (merge.Y - diverge.Y) / width}
	middle := sim.Point{X: (diverge.X + merge.X) / 2, Y: (diverge.Y + merge.Y) / 2}
	core := sim.Point{X: (entry.X+exit.X)/2 - middle.X, Y: (entry.Y+exit.Y)/2 - middle.Y}
	length := math.Hypot(core.X, core.Y)
	outward := sim.Point{X: core.X / length, Y: core.Y / length}
	if math.Abs(along.X*outward.X+along.Y*outward.Y) > 1e-9 {
		return sharedTrackFrame{}, fmt.Errorf("%s: the axes of the frame are not perpendicular", station.ID)
	}
	return sharedTrackFrame{center: sim.Point{X: middle.X - londonStationThroatDepth*outward.X, Y: middle.Y - londonStationThroatDepth*outward.Y}, along: along, outward: outward}, nil
}

// apply changes the network in place. It moves the hub nodes, turns the
// hubs, sets the control point, and removes the lanes.
func (g sharedTrackGeometry) apply(n *sim.Network) error {
	nodes := make(map[string]int, len(n.Nodes))
	for index, node := range n.Nodes {
		nodes[node.ID] = index
	}
	positions := func() map[string]sim.Point {
		positions := make(map[string]sim.Point, len(n.Nodes))
		for _, node := range n.Nodes {
			positions[node.ID] = node.Position
		}
		return positions
	}
	var missing error
	set := func(id string, position sim.Point) {
		index, ok := nodes[id]
		if !ok {
			missing = cmp.Or(missing, fmt.Errorf("unknown node %q", id))
			return
		}
		n.Nodes[index].Position = position
	}
	frames := make(map[string]sharedTrackFrame)
	for _, id := range []string{expressWaterloo, expressBank} {
		station, ok := n.Station(id)
		if !ok {
			return fmt.Errorf("unknown station %q", id)
		}
		frame, err := sharedTrackStationFrame(positions(), station)
		if err != nil {
			return err
		}
		frames[id] = frame
		throat := cmp.Or(g.throat[id], londonStationThroatDepth)
		set(id+"-diverge", frame.at(-londonStationThroatOffset, throat))
		set(id+"-merge", frame.at(londonStationThroatOffset, throat))
		set(station.Entry, frame.at(-g.part, throat+g.part))
		set(station.Exit, frame.at(g.part, throat+g.part))
		for row, berth := range station.Berths {
			depth := throat + g.part + g.part*float64(row+1)
			set(berth.ID+"-arrival", frame.at(-g.part, depth))
			set(berth.Node, frame.at(0, depth))
			set(berth.ID+"-departure", frame.at(g.part, depth))
		}
		if missing != nil {
			return missing
		}
		turn := g.turn[id] * math.Pi / 180
		if turn == 0 {
			continue
		}
		ids := []string{id + "-diverge", id + "-merge", station.Entry, station.Exit}
		for _, berth := range station.Berths {
			ids = append(ids, berth.ID+"-arrival", berth.Node, berth.ID+"-departure")
		}
		cos, sin := math.Cos(turn), math.Sin(turn)
		for _, node := range ids {
			p := n.Nodes[nodes[node]].Position
			dx, dy := p.X-frame.center.X, p.Y-frame.center.Y
			set(node, sim.Point{X: frame.center.X + dx*cos - dy*sin, Y: frame.center.Y + dx*sin + dy*cos})
		}
	}
	if g.curve != "" {
		index := slices.IndexFunc(n.Lanes, func(lane sim.Lane) bool { return lane.ID == g.curve })
		if index < 0 {
			return fmt.Errorf("unknown lane %q", g.curve)
		}
		lane := &n.Lanes[index]
		frame, ok := frames[lane.StationID]
		if !ok {
			return fmt.Errorf("lane %q is not at a hub", g.curve)
		}
		from, to := n.Nodes[nodes[lane.From]].Position, n.Nodes[nodes[lane.To]].Position
		middle := sim.Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
		chord := math.Hypot(to.X-from.X, to.Y-from.Y)
		normal := sim.Point{X: -(to.Y - from.Y) / chord, Y: (to.X - from.X) / chord}
		if normal.X*(middle.X-frame.center.X)+normal.Y*(middle.Y-frame.center.Y) < 0 {
			normal = sim.Point{X: -normal.X, Y: -normal.Y}
		}
		lane.Control = &sim.Point{X: middle.X + g.offset*normal.X, Y: middle.Y + g.offset*normal.Y}
	}
	for _, id := range g.remove {
		before := len(n.Lanes)
		n.Lanes = slices.DeleteFunc(n.Lanes, func(lane sim.Lane) bool { return lane.ID == id })
		if len(n.Lanes) == before {
			return fmt.Errorf("unknown lane %q", id)
		}
	}
	return nil
}
