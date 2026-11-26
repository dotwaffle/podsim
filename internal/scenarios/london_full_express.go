package scenarios

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The stations of the LondonFull Express services.
const (
	expressWaterloo = "940GZZLUWLO"
	expressBank     = "940GZZLUBNK"
	expressParking  = "express-parking"
)

// expressPods is the number of Express pods in Express parking.
const expressPods = 16

// expressTrunkStep is the largest length in meters of one lane of an Express
// trunk. A trunk of length L has int(L/expressTrunkStep) lanes.
const expressTrunkStep = 80

// expressGatePitch is the length in meters of one lane of the Express gate
// stubs at the entry and the exit of a station.
const expressGatePitch = 41

// LondonFullExpress returns an owned copy of the opt-in LondonFull Express
// preset. It is LondonFull with Waterloo and Bank in two banks each,
// dedicated Express lanes between them, an Express-only parking station that
// is not a depot, 16 Express pods, and an Express service in each direction.
// It is project D of docs/studies/express-hub-fixture.md. Its approved
// geometry is that of LondonFull before the smaller and tight stations, so
// it starts from LondonFull with wide stations at station scale 1.
func LondonFullExpress() project.Config {
	expressBaseOnce.Do(func() {
		var err error
		if expressBase, err = generateLondonFull(expressBaseOptions); err != nil {
			panic(err)
		}
	})
	config, err := londonFullExpress(project.Clone(expressBase))
	if err != nil {
		panic(err)
	}
	return config
}

var (
	expressBaseOnce sync.Once
	expressBase     project.Config
)

// expressBaseOptions changes the LondonFull options to those of the
// approved Express geometry: wide stations at station scale 1, and roads
// that keep their chords.
func expressBaseOptions(options *LondonOptions) {
	options.StationScale, options.WideStations, options.RoadFillets = 1, true, false
}

// generateLondonFullExpress returns the LondonFull Express preset without
// a cache.
func generateLondonFullExpress() (project.Config, error) {
	config, err := generateLondonFull(expressBaseOptions)
	if err != nil {
		return project.Config{}, err
	}
	return londonFullExpress(config)
}

// londonFullExpress adds the Express layout, fleet, and services to c, which
// is a LondonFull project that the caller owns.
func londonFullExpress(c project.Config) (project.Config, error) {
	n := &c.Network
	addExpressBanks(n, expressWaterloo, 3)
	addExpressBanks(n, expressBank, 2)
	mixed, err := sim.NewClassSet("legacy", "compact", "express")
	if err != nil {
		return project.Config{}, err
	}
	express, err := sim.NewClassSet("express")
	if err != nil {
		return project.Config{}, err
	}
	for i := range n.Stations {
		s := &n.Stations[i]
		if s.ID != expressWaterloo && s.ID != expressBank {
			continue
		}
		s.VehicleClasses = mixed
		for j := range s.Berths {
			s.Berths[j].VehicleClasses = mixed
		}
	}
	for i := range n.Lanes {
		l := &n.Lanes[i]
		if l.StationID != expressWaterloo && l.StationID != expressBank {
			continue
		}
		if strings.Contains(l.ID, "-road-") || strings.Contains(l.ID, "-access-") || strings.HasSuffix(l.ID, "-through") {
			continue
		}
		l.VehicleClasses = mixed
	}
	addExpressParking(n, express)
	for _, pair := range [][2]string{{expressWaterloo, expressBank}, {expressBank, expressWaterloo}} {
		c.ExpressServices = append(c.ExpressServices, sim.ExpressService{
			ID: "xs-" + pair[0][8:] + "-" + pair[1][8:], From: pair[0], To: pair[1], Class: sim.ExpressClass, PartyLimit: 20,
		})
	}
	c.OrderContract = sim.ExpressOrderContract
	for i := range expressPods {
		c.Fleet = append(c.Fleet, sim.Placement{ID: fmt.Sprintf("express-%02d", i+1), Class: sim.ExpressClass, StationID: expressParking, BerthID: fmt.Sprintf("xp-%02d", i+1)})
	}
	for _, id := range []string{expressWaterloo, expressBank, expressParking} {
		s, _ := n.Station(id)
		extendExpressGates(n, id, s.Entry, s.Exit, express)
	}
	// The trunks: 0 Waterloo to Bank, 1 Bank to Waterloo, 2 parking to
	// Waterloo, and 3 Waterloo to parking.
	for i, pair := range [][2]string{{expressWaterloo, expressBank}, {expressBank, expressWaterloo}, {expressParking, expressWaterloo}, {expressWaterloo, expressParking}} {
		a, _ := n.Station(pair[0])
		b, _ := n.Station(pair[1])
		from, to := a.Exit+"-xp", b.Entry+"-xp"
		if pair[0] == expressBank {
			p, _ := n.Node(from)
			n.Nodes = append(n.Nodes, sim.Node{ID: "xp-bank-bend", Position: sim.Point{X: p.Position.X + 120, Y: p.Position.Y + 165}})
			connectExpress(n, "xp-bank-lead", from, "xp-bank-bend", express)
			from = "xp-bank-bend"
		}
		connectExpress(n, fmt.Sprintf("xp-route-%d", i), from, to, express)
	}
	for _, id := range []string{expressWaterloo, expressBank} {
		s, _ := n.Station(id)
		extendExpressGates(n, id, s.Banks[1].Entry, s.Banks[1].Exit, express)
		branchExpressBank(n, id, express)
	}
	return c, nil
}

// addExpressBanks moves all but the first keep berths of station id into a
// second bank beside the station, with two berths, and moves the roads
// on that side to the second bank.
func addExpressBanks(n *sim.Network, id string, keep int) {
	index := slices.IndexFunc(n.Stations, func(s sim.Station) bool { return s.ID == id })
	s := &n.Stations[index]
	entry, _ := n.Node(s.Entry)
	exit, _ := n.Node(s.Exit)
	diverge, _ := n.Node(id + "-diverge")
	merge, _ := n.Node(id + "-merge")
	ax, ay := (exit.Position.X-entry.Position.X)/200, (exit.Position.Y-entry.Position.Y)/200
	mx, my := (entry.Position.X+exit.Position.X)/2, (entry.Position.Y+exit.Position.Y)/2
	ox, oy := mx-(diverge.Position.X+merge.Position.X)/2, my-(diverge.Position.Y+merge.Position.Y)/2
	length := math.Hypot(ox, oy)
	ox, oy = ox/length, oy/length
	cx, cy := mx-ox*200, my-oy*200
	position := func(a, o float64) sim.Point { return sim.Point{X: cx + ax*a + ox*o, Y: cy + ay*a + oy*o} }
	removed := map[string]bool{}
	for _, b := range s.Berths[keep:] {
		for _, suffix := range []string{"-node", "-arrival", "-departure"} {
			removed[b.ID+suffix] = true
		}
	}
	n.Nodes = slices.DeleteFunc(n.Nodes, func(p sim.Node) bool { return removed[p.ID] })
	n.Lanes = slices.DeleteFunc(n.Lanes, func(l sim.Lane) bool { return removed[l.From] || removed[l.To] })
	s.Berths = s.Berths[:keep]
	for _, node := range []struct {
		name string
		a, o float64
	}{{"diverge", 85, 130}, {"merge", 130, 140}, {"entry", 160, 280}, {"exit", 360, 280}} {
		n.Nodes = append(n.Nodes, sim.Node{ID: id + "-b-" + node.name, Position: position(node.a, node.o)})
	}
	plane := id + "-station-b"
	addExpressLane(n, id+"-b-access-in", id+"-b-diverge", id+"-b-entry", id, sim.StationEntryRole, plane, 0)
	addExpressLane(n, id+"-b-through", id+"-b-entry", id+"-b-exit", id, sim.StationThroughRole, plane, 0)
	addExpressLane(n, id+"-b-access-out", id+"-b-exit", id+"-b-merge", id, sim.StationExitRole, plane, 0)
	arrival, departure := id+"-b-entry", id+"-b-exit"
	for i := range 2 {
		berth := fmt.Sprintf("%s-b%02d", id, i+1)
		for j, suffix := range []string{"-arrival", "-node", "-departure"} {
			n.Nodes = append(n.Nodes, sim.Node{ID: berth + suffix, Position: position(160+100*float64(j), 350+75*float64(i))})
		}
		s.Berths = append(s.Berths, sim.Berth{ID: berth, Node: berth + "-node", SeparationGroup: plane})
		addExpressLane(n, berth+"-arrival-link", arrival, berth+"-arrival", id, sim.StationBerthAccessRole, plane, 0)
		addExpressLane(n, berth+"-in", berth+"-arrival", berth+"-node", id, sim.StationBerthAccessRole, plane, 0)
		addExpressLane(n, berth+"-out", berth+"-node", berth+"-departure", id, sim.StationDepartureRole, plane, 0)
		addExpressLane(n, berth+"-departure-link", berth+"-departure", departure, id, sim.StationDepartureRole, plane, 0)
		arrival, departure = berth+"-arrival", berth+"-departure"
	}
	moved := map[string]bool{}
	for i := range n.Lanes {
		l := &n.Lanes[i]
		if strings.HasPrefix(l.ID, id+"-road-in-") {
			p, _ := n.Node(l.From)
			if (p.Position.X-cx)*ax+(p.Position.Y-cy)*ay > 0 {
				l.To = id + "-b-diverge"
				moved[l.ID[len(l.ID)-2:]] = true
			}
		}
	}
	for i := range n.Lanes {
		l := &n.Lanes[i]
		if strings.HasPrefix(l.ID, id+"-road-out-") && moved[l.ID[len(l.ID)-2:]] {
			l.From = id + "-b-merge"
		}
	}
	s.Banks = []sim.StationBank{{ID: "a", Entry: s.Entry, Exit: s.Exit}, {ID: "b", Entry: id + "-b-entry", Exit: id + "-b-exit"}}
	for i, b := range s.Berths {
		which := 0
		if i >= keep {
			which = 1
		}
		s.Banks[which].BerthIDs = append(s.Banks[which].BerthIDs, b.ID)
	}
}

// extendExpressGates adds an Express stub of three lanes before entry and
// after exit, along the line from entry to exit. The stub before the entry
// of Bank points west. The stubs of a station with banks have their first
// lane in the station.
func extendExpressGates(n *sim.Network, id, entry, exit string, classes sim.ClassSet) {
	a, _ := n.Node(entry)
	b, _ := n.Node(exit)
	dx, dy := b.Position.X-a.Position.X, b.Position.Y-a.Position.Y
	length := math.Hypot(dx, dy)
	dx, dy = dx/length, dy/length
	for _, end := range []struct {
		id   string
		p    sim.Point
		sign float64
	}{{entry, a.Position, -1}, {exit, b.Position, 1}} {
		ux, uy := end.sign*dx, end.sign*dy
		if id == expressBank && end.sign < 0 {
			ux, uy = -1, 0
		}
		n.Nodes = append(n.Nodes, sim.Node{ID: end.id + "-xp", Position: sim.Point{X: end.p.X + 3*expressGatePitch*ux, Y: end.p.Y + 3*expressGatePitch*uy}})
		for i := 1; i < 3; i++ {
			n.Nodes = append(n.Nodes, sim.Node{ID: fmt.Sprintf("%s-xp-s%d", end.id, i), Position: sim.Point{X: end.p.X + expressGatePitch*float64(i)*ux, Y: end.p.Y + expressGatePitch*float64(i)*uy}})
		}
		ids := []string{end.id, end.id + "-xp-s1", end.id + "-xp-s2", end.id + "-xp"}
		for i := range 3 {
			from, to := ids[i], ids[i+1]
			if end.sign < 0 {
				from, to = to, from
			}
			addExpressLane(n, fmt.Sprintf("%s-xp-stub-%d", end.id, i), from, to, "", "", end.id+"-xp-plane", classes)
		}
		station, _ := n.Station(id)
		if len(station.Banks) == 0 {
			continue
		}
		for i := range n.Lanes {
			l := &n.Lanes[i]
			if l.ID == end.id+"-xp-stub-0" {
				l.StationID = id
				l.StationRole = sim.StationExitRole
				if end.sign < 0 {
					l.StationRole = sim.StationEntryRole
				}
			}
		}
	}
}

// addExpressParking adds the Express parking station 300 m east of the
// entry of parking-north, with 16 berths in a comb.
func addExpressParking(n *sim.Network, classes sim.ClassSet) {
	depot, _ := n.Station("parking-north")
	anchor, _ := n.Node(depot.Entry)
	x, y := anchor.Position.X+300, anchor.Position.Y
	at := func(along, outward float64) sim.Point { return sim.Point{X: x + along, Y: y - outward} }
	addExpressComb(n, expressParkingComb(), classes, at, 100, 60)
}

// expressComb holds the IDs of an Express parking comb: the station, its
// name, the prefix of its nodes, lanes, and berths, and its plane.
type expressComb struct {
	id, name, prefix, plane string
}

// expressParkingComb returns the IDs of the Express parking.
func expressParkingComb() expressComb {
	return expressComb{id: expressParking, name: "Express Parking", prefix: "xp", plane: "xp-parking"}
}

// addExpressComb adds the Express parking station with 16 berths in a
// comb. at gives the position of a point from the entry of the station: a
// distance along the through lane, and a distance outward to the rows. The
// through lane is two berth lanes long, each berth lane is length meters
// long, and the rows are pitch meters apart.
func addExpressComb(n *sim.Network, comb expressComb, classes sim.ClassSet, at func(along, outward float64) sim.Point, length, pitch float64) {
	station := sim.Station{ID: comb.id, Name: comb.name, Entry: comb.prefix + "-entry", Exit: comb.prefix + "-exit", ParkingOnly: true, VehicleClasses: classes}
	n.Nodes = append(n.Nodes, sim.Node{ID: station.Entry, Position: at(0, 0)}, sim.Node{ID: station.Exit, Position: at(2*length, 0)})
	addExpressLane(n, comb.prefix+"-through", station.Entry, station.Exit, comb.id, sim.StationThroughRole, comb.plane, classes)
	arrival, departure := station.Entry, station.Exit
	for i := range expressPods {
		id := fmt.Sprintf("%s-%02d", comb.prefix, i+1)
		for j, suffix := range []string{"-arrival", "-node", "-departure"} {
			n.Nodes = append(n.Nodes, sim.Node{ID: id + suffix, Position: at(float64(j)*length, float64(i+1)*pitch)})
		}
		station.Berths = append(station.Berths, sim.Berth{ID: id, Node: id + "-node", SeparationGroup: comb.plane, VehicleClasses: classes})
		addExpressLane(n, id+"-arrival-link", arrival, id+"-arrival", comb.id, sim.StationBerthAccessRole, comb.plane, classes)
		addExpressLane(n, id+"-in", id+"-arrival", id+"-node", comb.id, sim.StationBerthAccessRole, comb.plane, classes)
		addExpressLane(n, id+"-out", id+"-node", id+"-departure", comb.id, sim.StationDepartureRole, comb.plane, classes)
		addExpressLane(n, id+"-departure-link", id+"-departure", departure, comb.id, sim.StationDepartureRole, comb.plane, classes)
		arrival, departure = id+"-arrival", id+"-departure"
	}
	n.Stations = append(n.Stations, station)
}

func addExpressLane(n *sim.Network, id, from, to, station string, role sim.StationLaneRole, plane string, classes sim.ClassSet) {
	n.Lanes = append(n.Lanes, sim.Lane{ID: id, From: from, To: to, SpeedLimit: 14, StationID: station, StationRole: role, SeparationGroup: plane, VehicleClasses: classes})
}

// connectExpress adds a straight trunk from one node to another in
// int(length/expressTrunkStep) lanes, in the separation plane id.
func connectExpress(n *sim.Network, id, from, to string, classes sim.ClassSet) {
	a, _ := n.Node(from)
	b, _ := n.Node(to)
	steps := int(math.Hypot(b.Position.X-a.Position.X, b.Position.Y-a.Position.Y) / expressTrunkStep)
	previous := from
	for i := 1; i <= steps; i++ {
		next := to
		if i < steps {
			next = fmt.Sprintf("%s-n-%03d", id, i)
			t := float64(i) / float64(steps)
			n.Nodes = append(n.Nodes, sim.Node{ID: next, Position: sim.Point{X: a.Position.X + t*(b.Position.X-a.Position.X), Y: a.Position.Y + t*(b.Position.Y-a.Position.Y)}})
		}
		addExpressLane(n, fmt.Sprintf("%s-%03d", id, i), previous, next, "", "", id, classes)
		previous = next
	}
}

// branchExpressBank joins the second bank of station id to the Express
// trunks. At Waterloo it also joins the parking trunks to the trunks between
// the hubs, in both directions, and the second bank to the parking trunks,
// so that an Express pod can park from each berth of Waterloo.
func branchExpressBank(n *sim.Network, id string, classes sim.ClassSet) {
	incoming, outgoing := "xp-route-0", "xp-route-1"
	if id == expressWaterloo {
		incoming, outgoing = "xp-route-1", "xp-route-0"
	}
	entry, _ := n.Node(id + "-b-entry-xp")
	exit, _ := n.Node(id + "-b-exit-xp")
	from := nearestTrunkNode(n, incoming, entry.Position)
	if id == expressBank {
		p, _ := n.Node(from)
		bend := id + "-branch-bend"
		n.Nodes = append(n.Nodes, sim.Node{ID: bend, Position: sim.Point{X: p.Position.X, Y: p.Position.Y - 240}})
		connectExpress(n, id+"-branch-lead", from, bend, classes)
		from = bend
	}
	connectExpress(n, id+"-branch-in", from, entry.ID, classes)
	connectExpress(n, id+"-branch-out", exit.ID, nearestTrunkNode(n, outgoing, exit.Position), classes)
	if id == expressWaterloo {
		connectExpress(n, id+"-parking-link", nearestTrunkNode(n, "xp-route-2", entry.Position), nearestTrunkNode(n, "xp-route-0", entry.Position), classes)
		connectExpress(n, id+"-parking-return", nearestTrunkNode(n, "xp-route-1", entry.Position), nearestTrunkNode(n, "xp-route-3", entry.Position), classes)
		connectExpress(n, id+"-b-parking-out", exit.ID, nearestTrunkNode(n, "xp-route-3", exit.Position), classes)
		connectExpress(n, id+"-b-parking-in", nearestTrunkNode(n, "xp-route-2", entry.Position), entry.ID, classes)
	}
}

// nearestTrunkNode returns the interior node of trunk whose distance from p
// is closest to 500 m. The first such node in network order wins a tie.
func nearestTrunkNode(n *sim.Network, trunk string, p sim.Point) string {
	best, result := math.Inf(1), ""
	for _, node := range n.Nodes {
		if !strings.HasPrefix(node.ID, trunk+"-n-") {
			continue
		}
		if d := math.Abs(math.Hypot(node.Position.X-p.X, node.Position.Y-p.Y) - 500); d < best {
			best, result = d, node.ID
		}
	}
	return result
}
