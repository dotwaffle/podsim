package scenarios

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// londonChainSpacing is the distance between two nodes of a split or
	// join chain, the shortest lane for the large classes. On a short link
	// the spacing shrinks to londonShortestLane.
	londonChainSpacing = 40.0
	// londonShortestLane is the shortest lane of a ramps junction. It is a
	// little more than the project limit of 2*sim.Clearance, so that
	// rounding cannot make a lane too short.
	londonShortestLane = 2*sim.Clearance + 1e-6
	// londonRampAcceleration is the lateral acceleration in m/s² on the
	// smallest radius of a ramp at its speed limit.
	londonRampAcceleration = 2.0
	// londonRampParts is the number of straight parts of a curved lane in
	// the heading search.
	londonRampParts = 16
	// londonRampMinimumSpeed is the lowest speed limit of a ramp, at a
	// smallest radius of 12.5 m. A turn that is tighter gets no ramp, as a
	// U-turn: pods go through the station or take another route.
	londonRampMinimumSpeed = 5.0
)

// londonTurn is an exit of an arrival or an entry of a departure: a ramp
// to or from another link, or the slip road of a station.
type londonTurn struct {
	// angle orders the turns of one portal. The turn with the smallest
	// angle uses the node of the chain that is farthest from the station.
	angle float64
	// target is the other end of the turn: the join node of a ramp from an
	// arrival, or a nominal end for a ramp to a departure or a slip road. It
	// places the nodes of staged ramps.
	target sim.Point
	// other is the index of the other link of a ramp, or n plus the index
	// of the slip site of a slip road.
	other int
}

// londonEnd is the node where a turn leaves an arrival or joins a
// departure.
type londonEnd struct {
	node     string
	position sim.Point
}

// londonJunctionInput holds the values of one station for
// planLondonJunction.
type londonJunctionInput struct {
	id, name string
	center   sim.Point
	portals  londonStationPortals
	// directions holds the direction of each link from the station, in
	// portal order.
	directions []float64
	// slips holds the direction of each slip site: the station, then each
	// Parking facility at the station.
	slips []float64
	// lanes holds the lanes of the network. The plan reads the carriageway
	// lanes of the portals.
	lanes []sim.Lane
}

// londonJunction holds the ramps junction of one station.
type londonJunction struct {
	// nodes and lanes hold the new nodes and lanes: the carriageway lanes
	// of the chains, the staged lanes, and the ramps.
	nodes []sim.Node
	lanes []sim.Lane
	// cuts holds the carriageway lanes that a chain shortens. Each such
	// lane ends at the first node of its chain.
	cuts []londonCut
	// slips holds, for each slip site, the node where its off-slip leaves
	// each arrival and the node where its on-slip joins each departure.
	slips []londonStationPortals
	// segments holds the ramps and the staged lanes as straight parts for
	// the heading search.
	segments []londonSegment
	// slow holds the ramps that a staged lane slower than
	// londonRampMinimumSpeed carries. The plan drops them.
	slow []londonRamp
}

// londonCut gives a carriageway lane a new end node.
type londonCut struct {
	lane int
	to   string
}

// add adds the junction to the network.
func (junction *londonJunction) add(network *sim.Network) {
	for _, cut := range junction.cuts {
		network.Lanes[cut.lane].To = cut.to
	}
	network.Nodes = append(network.Nodes, junction.nodes...)
	network.Lanes = append(network.Lanes, junction.lanes...)
}

// londonSide tells whether a chain splits an arrival or joins a departure.
type londonSide bool

const (
	londonArrivalSide   londonSide = false
	londonDepartureSide londonSide = true
)

// londonRamp is a turn from an arrival to a departure, by link index.
type londonRamp struct{ arrival, departure int }

// planLondonJunction returns the ramps junction of a station with n links.
// Each arrival has a ramp to each other departure, and a slip road to each
// slip site. A ramp that is slower than londonRampMinimumSpeed is
// dropped, and the plan is made again without it, because the chains and
// thus the other ramps change. A ramp that a staged lane slower than
// londonRampMinimumSpeed carries is dropped too. The dropped turns only
// increase, so this stops.
func planLondonJunction(input londonJunctionInput) (londonJunction, error) {
	dropped := make(map[londonRamp]bool)
	for {
		junction, slow, err := planLondonTurns(input, dropped)
		if err != nil || len(slow) == 0 {
			return junction, err
		}
		for _, ramp := range slow {
			dropped[ramp] = true
		}
	}
}

// planLondonTurns returns the ramps junction of a station without the
// dropped ramps, or the ramps that are slower than londonRampMinimumSpeed
// or that a slower staged lane carries.
// The first turns of a portal use the nodes that are farthest from the
// station (see londonTurnKey). The slip roads come last, so on a chain
// without staged ramps they leave from the portal or join at it, as with
// chords. Each ramp curves toward the station position (see
// londonCenteredControl).
//
// A chain of 2-way nodes along the carriageway gives each turn its own node,
// and the portal is the last node. The chain spacing is
// londonChainSpacing, or less on a short link, at least
// londonShortestLane. Where the chain does not fit in the half of the link
// at the station, the turns stage: a group of consecutive turns shares a
// staged lane, and splits in two at the end of it (see
// londonChainBuilder.emit). Where a lane is shorter than
// londonShortestLane, the plan returns a layout conflict error.
func planLondonTurns(input londonJunctionInput, dropped map[londonRamp]bool) (londonJunction, []londonRamp, error) {
	n := len(input.portals.arrivals)
	junction := londonJunction{slips: make([]londonStationPortals, len(input.slips))}
	for index := range junction.slips {
		junction.slips[index] = londonStationPortals{arrivals: make([]londonPortal, n), departures: make([]londonPortal, n)}
	}
	slipTarget := func(direction float64) sim.Point {
		return add(input.center, scale(sim.Point{X: math.Cos(direction), Y: math.Sin(direction)}, londonStationThroatDepth))
	}
	// The turns of a portal are the ramps to or from each other link that
	// are not dropped, in link order, and then the slip roads.
	turns := func(side londonSide, own int, angle func(other float64) float64, targets []sim.Point) []londonTurn {
		list := make([]londonTurn, 0, n-1+len(input.slips))
		for index, target := range targets {
			ramp := londonRamp{arrival: own, departure: index}
			if side == londonDepartureSide {
				ramp = londonRamp{arrival: index, departure: own}
			}
			if index != own && !dropped[ramp] {
				list = append(list, londonTurn{angle: angle(input.directions[index]), target: target, other: index})
			}
		}
		// The slip roads come after the ramps, so that they use the
		// portal, as with chords.
		for site, direction := range input.slips {
			list = append(list, londonTurn{angle: 2*math.Pi + angle(direction), target: slipTarget(direction), other: n + site})
		}
		return list
	}
	// chain places the chain of a portal and returns the end of each turn
	// by the index of its other link or slip site.
	chain := func(side londonSide, index int, portal londonPortal, list []londonTurn) (map[int]londonEnd, error) {
		ends, err := junction.chain(input, side, index, portal, list)
		if err != nil {
			return nil, err
		}
		byOther := make(map[int]londonEnd, len(list))
		for turn, end := range ends {
			byOther[list[turn].other] = end
		}
		return byOther, nil
	}
	narrow := londonFreeAngle(input.slips[0], input.directions) < londonNarrowAngle
	// The departures come first, so that the ramps of each arrival know
	// their join nodes.
	entries := make([]map[int]londonEnd, n)
	for index, portal := range input.portals.departures {
		key := londonTurnKey(input.directions[index], input.slips[0], input.directions, londonDepartureSide, narrow)
		ends, err := chain(londonDepartureSide, index, portal, turns(londonDepartureSide, index, key, londonPortalPositions(input.portals.arrivals)))
		if err != nil {
			return londonJunction{}, nil, err
		}
		entries[index] = ends
	}
	exits := make([]map[int]londonEnd, n)
	for index, portal := range input.portals.arrivals {
		joins := make([]sim.Point, n)
		for departure := range n {
			if end, ok := entries[departure][index]; ok {
				joins[departure] = end.position
			}
		}
		key := londonTurnKey(input.directions[index], input.slips[0], input.directions, londonArrivalSide, narrow)
		ends, err := chain(londonArrivalSide, index, portal, turns(londonArrivalSide, index, key, joins))
		if err != nil {
			return londonJunction{}, nil, err
		}
		exits[index] = ends
	}
	type plannedRamp struct {
		id       string
		from, to londonEnd
		control  sim.Point
		portal   londonPortal
	}
	var ramps []plannedRamp
	slow := junction.slow
	for arrival := range n {
		for departure := range n {
			if arrival == departure || dropped[londonRamp{arrival: arrival, departure: departure}] {
				continue
			}
			from, to := exits[arrival][departure], entries[departure][arrival]
			control := londonCenteredControl(from.position, to.position, input.center)
			if londonRampSpeed(from.position, control, to.position) < londonRampMinimumSpeed {
				slow = append(slow, londonRamp{arrival: arrival, departure: departure})
				continue
			}
			id := fmt.Sprintf("%s-ramp-%02d-%02d", input.id, arrival+1, departure+1)
			ramps = append(ramps, plannedRamp{id: id, from: from, to: to, control: control, portal: input.portals.arrivals[arrival]})
		}
	}
	if len(slow) != 0 {
		return londonJunction{}, slow, nil
	}
	for _, ramp := range ramps {
		if err := junction.addCurve(input, ramp.id, ramp.from, ramp.to, ramp.control, ramp.portal); err != nil {
			return londonJunction{}, nil, err
		}
	}
	for site := range input.slips {
		for index := range n {
			exit, entry := exits[index][n+site], entries[index][n+site]
			junction.slips[site].arrivals[index] = londonPortal{node: exit.node, position: exit.position}
			junction.slips[site].departures[index] = londonPortal{node: entry.node, position: entry.position}
		}
	}
	return junction, nil, nil
}

// londonTurnKey returns the order key of the turns of the portal of a link
// with the direction own. The key of an arrival is the counterclockwise
// angle from its link to the other link, and the key of a departure is the
// clockwise angle. In this order, the ramps of one arrival, which all curve
// toward the station position, nest: a ramp from a farther node curves
// inside a ramp from a nearer node. Then most of them do not cross each
// other. Ramps between links in almost the same direction, and ramps from
// nodes far out, can still cross.
//
// The order starts at the link itself. When the free angle of the station
// (see londonFreeAngle) is narrow, the ramps across it pass near the
// station core. Then a portal next to the free angle starts the order at
// the opposite direction, so that its ramps across the free angle use the
// nodes nearest to the station. station is the direction of the station.
func londonTurnKey(own, station float64, directions []float64, side londonSide, narrow bool) func(other float64) float64 {
	angle := func(other float64) float64 { return counterclockwise(own, other) }
	if side == londonDepartureSide {
		angle = func(other float64) float64 { return counterclockwise(other, own) }
	}
	toStation := angle(station)
	between := slices.ContainsFunc(directions, func(direction float64) bool {
		other := angle(direction)
		return other > 0 && other < toStation
	})
	cut := 0.0
	if narrow && toStation < math.Pi && !between {
		cut = math.Pi
	}
	return func(other float64) float64 { return counterclockwise(cut, angle(other)) }
}

// londonNarrowAngle is the largest free angle of a station that
// londonTurnKey takes as narrow.
const londonNarrowAngle = math.Pi / 2

// londonFreeAngle returns the angle between the two links on each side of
// the station direction.
func londonFreeAngle(station float64, directions []float64) float64 {
	ahead, behind := 2*math.Pi, 2*math.Pi
	for _, direction := range directions {
		if angle := counterclockwise(station, direction); angle > 0 {
			ahead = min(ahead, angle)
		}
		if angle := counterclockwise(direction, station); angle > 0 {
			behind = min(behind, angle)
		}
	}
	return ahead + behind
}

// counterclockwise returns the counterclockwise angle from one direction
// to another, from 0 to 2*pi.
func counterclockwise(from, to float64) float64 {
	return math.Mod(math.Mod(to-from, 2*math.Pi)+2*math.Pi, 2*math.Pi)
}

// addCurve adds a ramp or a staged lane with its own separation group and
// the speed limit of londonRampSpeed. It returns a layout conflict error,
// which names the link of the portal, when the lane is shorter than
// londonShortestLane.
func (junction *londonJunction) addCurve(input londonJunctionInput, id string, from, to londonEnd, control sim.Point, portal londonPortal) error {
	points := londonCurvePoints(from.position, control, to.position, 64)
	length := 0.0
	for index := 1; index < len(points); index++ {
		length += math.Hypot(points[index].X-points[index-1].X, points[index].Y-points[index-1].Y)
	}
	if length < londonShortestLane {
		return londonJunctionError(input, portal)
	}
	junction.lanes = append(junction.lanes, sim.Lane{
		ID: id, From: from.node, To: to.node, SpeedLimit: londonRampSpeed(from.position, control, to.position),
		SeparationGroup: id, Control: new(control),
	})
	coarse := londonCurvePoints(from.position, control, to.position, londonRampParts)
	for index := 1; index < len(coarse); index++ {
		junction.segments = append(junction.segments, londonSegment{from: coarse[index-1], to: coarse[index]})
	}
	return nil
}

// londonJunctionError returns the layout conflict error of a junction that
// does not fit at the link of the portal.
func londonJunctionError(input londonJunctionInput, portal londonPortal) error {
	return fmt.Errorf("layout conflict at station %s (%s): the ramps at link %q do not fit, even when they stage", input.name, input.id, input.lanes[portal.half].SeparationGroup)
}

// londonCurvePoints returns parts+1 points of a quadratic curve, as
// sim.Network.Polyline does for 64 parts.
func londonCurvePoints(from, control, to sim.Point, parts int) []sim.Point {
	points := make([]sim.Point, parts+1)
	for index := range points {
		points[index] = londonCurvePoint(from, control, to, float64(index)/float64(parts))
	}
	return points
}

// londonRampSpeed returns the speed limit of a quadratic curve: the speed
// at which the lateral acceleration on its smallest radius is
// londonRampAcceleration, and at most speedLimit. For the curve B with
// B'(t) = 2(a + tc), the radius at t is 2|a + tc|³ / |a × c|, and it is
// smallest where |a + tc| is smallest.
func londonRampSpeed(from, control, to sim.Point) float64 {
	a := sim.Point{X: control.X - from.X, Y: control.Y - from.Y}
	c := sim.Point{X: to.X - 2*control.X + from.X, Y: to.Y - 2*control.Y + from.Y}
	cross := math.Abs(a.X*c.Y - a.Y*c.X)
	lengthSquared := c.X*c.X + c.Y*c.Y
	if cross == 0 || lengthSquared == 0 {
		return speedLimit
	}
	t := min(1, max(0, -(a.X*c.X+a.Y*c.Y)/lengthSquared))
	velocity := math.Hypot(a.X+t*c.X, a.Y+t*c.Y)
	radius := 2 * velocity * velocity * velocity / cross
	return min(speedLimit, math.Sqrt(londonRampAcceleration*radius))
}

// londonChainBuilder places the nodes of one chain and its staged lanes.
type londonChainBuilder struct {
	junction *londonJunction
	input    londonJunctionInput
	side     londonSide
	// index is the index of the portal, and portal the portal.
	index  int
	portal londonPortal
	turns  []londonTurn
	// ends holds the end of each turn.
	ends []londonEnd
	// nodes counts the new nodes of the chain.
	nodes int
}

// chain places the chain of a portal and returns the end of each turn, in
// the order of turns. It adds the nodes and the lanes of the chain to the
// junction.
func (junction *londonJunction) chain(input londonJunctionInput, side londonSide, index int, portal londonPortal, turns []londonTurn) ([]londonEnd, error) {
	builder := &londonChainBuilder{junction: junction, input: input, side: side, index: index, portal: portal, turns: turns, ends: make([]londonEnd, len(turns))}
	order := make([]int, len(turns))
	for turn := range order {
		order[turn] = turn
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(turns[a].angle, turns[b].angle) })
	end := londonEnd{node: portal.node, position: portal.position}
	if len(turns) == 1 {
		builder.ends[0] = end
		return builder.ends, nil
	}
	line, spacing := londonChainLine(math.Hypot(portal.mid.X-portal.position.X, portal.mid.Y-portal.position.Y), len(turns)-2)
	slots := builder.line(line, spacing)
	// Each node of the line gives one group of turns, and the portal two.
	groups := londonGroups(order, line+2)
	for slot, group := range groups {
		if err := builder.emit(slots[min(slot, line)], group); err != nil {
			return nil, err
		}
	}
	return builder.ends, nil
}

// londonChainLine returns the number of new nodes of a chain along the
// carriageway, and their spacing, for a chain that needs the given number
// of new nodes. room is the distance from the portal to the middle of the
// link. The lane to the middle of the link is at least
// londonShortestLane. When the nodes do not fit at that spacing, the chain
// gets as many nodes as fit.
func londonChainLine(room float64, needed int) (int, float64) {
	if needed <= 0 {
		return 0, 0
	}
	if spacing := min(londonChainSpacing, (room-londonShortestLane)/float64(needed)); spacing >= londonShortestLane {
		return needed, spacing
	}
	line := max(0, int((room-londonShortestLane)/londonShortestLane))
	if line == 0 {
		return 0, 0
	}
	return line, min(londonChainSpacing, (room-londonShortestLane)/float64(line))
}

// londonGroups divides the ordered turns into count groups of consecutive
// turns. The sizes differ by at most one, and the larger groups come
// first.
func londonGroups(order []int, count int) [][]int {
	groups := make([][]int, 0, count)
	size, extra := len(order)/count, len(order)%count
	for index := range count {
		length := size
		if index < extra {
			length++
		}
		groups = append(groups, order[:length])
		order = order[length:]
	}
	return groups
}

// line adds the new nodes of the chain along the carriageway and the
// carriageway lanes between them, at the spacing from the portal. It
// returns the nodes from the farthest to the portal, with the portal. The
// new nodes are numbered in the direction of travel: those of an arrival
// from the farthest, and those of a departure from the portal. The lane of
// the node with number K has the ID of the carriageway lane with the
// suffix -K.
func (builder *londonChainBuilder) line(count int, spacing float64) []londonEnd {
	portal := builder.portal
	end := londonEnd{node: portal.node, position: portal.position}
	if count == 0 {
		return []londonEnd{end}
	}
	room := math.Hypot(portal.mid.X-portal.position.X, portal.mid.Y-portal.position.Y)
	outward := scale(sim.Point{X: portal.mid.X - portal.position.X, Y: portal.mid.Y - portal.position.Y}, 1/room)
	half := builder.input.lanes[portal.half]
	// The last lane of an arrival chain ends at the portal, and the last
	// lane of a departure chain at the middle of the link.
	last := end
	if builder.side == londonDepartureSide {
		last = londonEnd{node: half.To}
	}
	travel := make([]londonEnd, count)
	for index := range count {
		distance := float64(count-index) * spacing
		if builder.side == londonDepartureSide {
			distance = float64(index+1) * spacing
		}
		travel[index] = londonEnd{node: builder.next(), position: add(portal.position, scale(outward, distance))}
		builder.junction.nodes = append(builder.junction.nodes, sim.Node{ID: travel[index].node, Position: travel[index].position})
	}
	builder.junction.cuts = append(builder.junction.cuts, londonCut{lane: portal.half, to: travel[0].node})
	for index, node := range travel {
		to := last
		if index+1 < count {
			to = travel[index+1]
		}
		builder.junction.lanes = append(builder.junction.lanes, sim.Lane{
			ID: fmt.Sprintf("%s-%d", half.ID, index+1), From: node.node, To: to.node,
			SpeedLimit: speedLimit, SeparationGroup: half.SeparationGroup,
		})
	}
	slots := slices.Clone(travel)
	if builder.side == londonDepartureSide {
		slices.Reverse(slots)
	}
	return append(slots, end)
}

// next returns the ID of the next new node of the chain: the node
// <station>-split-AA-K of arrival AA, or <station>-join-DD-K of departure
// DD, where K counts the new nodes of the chain from 1.
func (builder *londonChainBuilder) next() string {
	builder.nodes++
	kind := "split"
	if builder.side == londonDepartureSide {
		kind = "join"
	}
	return fmt.Sprintf("%s-%s-%02d-%d", builder.input.id, kind, builder.index+1, builder.nodes)
}

// emit gives a group of turns the node at end. A single turn uses the
// node. A larger group stages: a staged lane goes from the node to a new
// node, or for a departure from a new node to the node, and the group
// splits in two at the new node. The first half has the extra turn. The
// staged lane follows the curve from the node toward the farthest target
// of the group, and it is londonShortestLane long as a chord. Each curve
// toward the station position starts in the same direction, so the
// staged lane starts in the direction of the ramps of the group.
func (builder *londonChainBuilder) emit(end londonEnd, group []int) error {
	if len(group) == 1 {
		builder.ends[group[0]] = end
		return nil
	}
	distance := func(turn int) float64 {
		target := builder.turns[turn].target
		return math.Hypot(target.X-end.position.X, target.Y-end.position.Y)
	}
	// The carrier is the turn of the group with the farthest target.
	carrier := builder.turns[slices.MaxFunc(group, func(a, b int) int { return cmp.Compare(distance(a), distance(b)) })]
	target := carrier.target
	control := londonCenteredControl(end.position, target, builder.input.center)
	t, ok := londonChordParameter(end.position, control, target, londonShortestLane)
	if !ok {
		return londonJunctionError(builder.input, builder.portal)
	}
	node := londonEnd{node: builder.next(), position: londonCurvePoint(end.position, control, target, t)}
	number := builder.nodes
	builder.junction.nodes = append(builder.junction.nodes, sim.Node{ID: node.node, Position: node.position})
	// The part of the curve up to t has this control point.
	part := add(scale(end.position, 1-t), scale(control, t))
	// A staged lane that is too slow drops the ramp that it carries. A
	// slip road does not drop.
	if carrier.other < len(builder.input.directions) && londonRampSpeed(end.position, part, node.position) < londonRampMinimumSpeed {
		ramp := londonRamp{arrival: builder.index, departure: carrier.other}
		if builder.side == londonDepartureSide {
			ramp = londonRamp{arrival: carrier.other, departure: builder.index}
		}
		builder.junction.slow = append(builder.junction.slow, ramp)
	}
	if builder.side == londonArrivalSide {
		id := fmt.Sprintf("%s-stage-in-%02d-%d", builder.input.id, builder.index+1, number)
		if err := builder.junction.addCurve(builder.input, id, end, node, part, builder.portal); err != nil {
			return err
		}
	} else {
		id := fmt.Sprintf("%s-stage-out-%02d-%d", builder.input.id, builder.index+1, number)
		if err := builder.junction.addCurve(builder.input, id, node, end, part, builder.portal); err != nil {
			return err
		}
	}
	half := (len(group) + 1) / 2
	if err := builder.emit(node, group[:half]); err != nil {
		return err
	}
	return builder.emit(node, group[half:])
}

// londonCurvePoint returns the point of a quadratic curve at t, with the
// arithmetic of sim.Network.Polyline.
func londonCurvePoint(from, control, to sim.Point, t float64) sim.Point {
	u := 1 - t
	return sim.Point{X: u*u*from.X + 2*u*t*control.X + t*t*to.X, Y: u*u*from.Y + 2*u*t*control.Y + t*t*to.Y}
}

// londonChordParameter returns the smallest t found by bisection at which
// the point of a quadratic curve is the distance from its start. It reports
// false when the end of the curve is nearer than the distance.
func londonChordParameter(from, control, to sim.Point, distance float64) (float64, bool) {
	far := func(t float64) bool {
		point := londonCurvePoint(from, control, to, t)
		return math.Hypot(point.X-from.X, point.Y-from.Y) >= distance
	}
	if !far(1) {
		return 0, false
	}
	low, high := 0.0, 1.0
	for range 60 {
		middle := (low + high) / 2
		if far(middle) {
			high = middle
		} else {
			low = middle
		}
	}
	return high, true
}
