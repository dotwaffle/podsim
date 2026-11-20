// Package ride gives the first-person ride page the poses of the pods and the
// network geometry. It has no browser code, so native tests cover it.
package ride

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// PoseFields is the count of numbers for each pod in a frame: x, y,
// heading, speed, class index, and the beam top under the pod.
const PoseFields = 6

// headingStep is the movement in meters from the anchor of a pod that sets
// a new heading. A pod that moved less keeps its heading, so a stopped or a
// slow pod does not turn, and the result does not depend on the frame rate.
const headingStep = 0.1

// maxStep is the largest movement in meters between two frames that sets a
// heading. A longer jump, as after a restore, uses the lane tangent.
const maxStep = 50

// Classes gives the pod classes in the order of the class index of a frame.
var Classes = []sim.VehicleClass{sim.LegacyClass, sim.CompactClass, sim.GroupClass, sim.ExpressClass}

// Bridge keeps the motion buffer and the previous pose of each pod. Use it
// on one goroutine.
type Bridge struct {
	motion   remote.Motion
	revision revisionKey
	lanes    laneTable
	// pieces holds the round joins of the lanes. See addFillets.
	pieces map[pieceKey]piece
	// berths holds the direction of a pod at each berth.
	berths map[string]float64
	// distances holds the distance of each node from the mainline, and
	// berthNodes holds the node of each berth.
	distances  map[string]float64
	berthNodes map[string]string
	// headings holds the pose of each pod in the last frame. spare is the
	// map of the frame before it, which Frame clears and uses again.
	headings, spare map[string]heading
	ids             []string
	version         uint64
	session         string
}

type revisionKey struct {
	serverStart, epoch string
	generation, rev    uint64
}

// heading is the direction of a pod, and the position where the bridge
// last set it. lane is the lane of the pod in the frame, and previous is
// the lane before it, or empty when the bridge does not know them. sharp is
// true when the pod is in the trims of the join at the node position join
// and the bridge did not put it on a piece there.
type heading struct {
	anchor         sim.Point
	angle          float64
	lane, previous string
	sharp          bool
	join           sim.Point
}

// direction is a heading that the bridge knows when ok is true.
type direction struct {
	angle float64
	ok    bool
}

// shown is a pod as the page draws it, with the state that the bridge keeps
// for it. along is the direction of the piece under the pod.
type shown struct {
	pod      sim.Pod
	previous string
	sharp    bool
	join     sim.Point
	along    direction
}

// FrameInfo describes a frame that Frame wrote.
type FrameInfo struct {
	// Count is the number of pods in the frame.
	Count int
	// Version changes when the pod IDs or their order change.
	Version uint64
	// Generation is the session generation of the frame.
	Generation uint64
	// Session changes with the server start, the epoch, or the generation.
	// Then the network can be different.
	Session string
}

// Observe adds state to the motion buffer when it is a new revision.
func (b *Bridge) Observe(state session.State, at time.Time) {
	if state.Epoch == "" {
		return
	}
	key := revisionKey{state.ServerStart, state.Epoch, state.Generation, state.Revision}
	if key == b.revision {
		return
	}
	if key.serverStart != b.revision.serverStart || key.epoch != b.revision.epoch || key.generation != b.revision.generation {
		b.lanes = newLaneTable(state.Network)
		b.distances = b.lanes.addDistances(state.Network)
		b.pieces = pieceIndex(b.lanes.addFillets(state.Network))
		b.berths = b.lanes.berthHeadings(state.Network)
		b.berthNodes = make(map[string]string)
		for _, station := range state.Network.Stations {
			for _, berth := range station.Berths {
				b.berthNodes[berth.ID] = berth.Node
			}
		}
		b.headings, b.spare = nil, nil
		b.session = fmt.Sprintf("%s/%s/%d", state.ServerStart, state.Epoch, state.Generation)
	}
	b.revision = key
	b.motion.Observe(state, at)
}

// Frame writes the pose of each pod at the time at into out, PoseFields
// numbers for each pod, and returns out with the new length.
func (b *Bridge) Frame(at time.Time, out []float64) ([]float64, FrameInfo) {
	snapshot := b.motion.Sample(at)
	out = out[:0]
	previous := b.headings
	if b.spare == nil {
		b.spare = make(map[string]heading, len(snapshot.Vehicles))
	}
	clear(b.spare)
	b.headings, b.spare = b.spare, previous
	changed := len(b.ids) != len(snapshot.Vehicles)
	for i, vehicle := range snapshot.Vehicles {
		pod := vehicle.Pod
		if !changed && b.ids[i] != pod.ID {
			changed = true
		}
		view := b.show(vehicle, previous[pod.ID])
		next := b.heading(view.pod, view.along, previous)
		next.lane, next.previous, next.sharp, next.join = view.pod.LaneID, view.previous, view.sharp, view.join
		b.headings[pod.ID] = next
		out = append(out, view.pod.Position.X, view.pod.Position.Y, next.angle, pod.Speed, float64(classIndex(pod.Class)), b.podHeight(view.pod))
	}
	if changed {
		b.ids = b.ids[:0]
		for _, vehicle := range snapshot.Vehicles {
			b.ids = append(b.ids, vehicle.Pod.ID)
		}
		b.version++
	}
	return out, FrameInfo{Count: len(snapshot.Vehicles), Version: b.version, Generation: b.revision.generation, Session: b.session}
}

// show returns the pod of vehicle as the page draws it. The lane and the
// lane distance match the sampled position. A pod in the trim of a round
// join moves to its piece. A pod that is not on its route, or that has no
// known lane on the other side of the join, keeps its sampled position.
// last is the state of the pod in the previous frame. The previous lane is
// the lane before the pod lane on the route, or else the lane that the
// bridge remembers.
//
// A pod that keeps its sampled position in the trims of a join keeps it
// until it leaves the trims of that join, also when the lane on the other
// side becomes known. Thus the pod does not jump onto the piece.
func (b *Bridge) show(vehicle sim.Vehicle, last heading) shown {
	pod := vehicle.Pod
	at, ok := b.lanes.locate(vehicle)
	if !ok {
		return shown{pod: pod, previous: rememberedLane(pod.LaneID, last)}
	}
	pod.LaneID, pod.LaneDistance = at.lane, at.distance
	if at.previous == "" {
		at.previous = rememberedLane(at.lane, last)
	}
	view := shown{pod: pod, previous: at.previous}
	join, inTrim := b.lanes.trimJoin(at)
	if !inTrim {
		return view
	}
	if !last.sharp || last.join != join {
		if position, angle, ok := b.filletPosition(at); ok {
			view.pod.Position, view.along = position, direction{angle, true}
			return view
		}
	}
	view.sharp, view.join = true, join
	return view
}

// rememberedLane returns the lane before lane from the state last of the
// previous frame: the lane of that frame when the pod changed lanes since,
// or else the lane before it.
func rememberedLane(lane string, last heading) string {
	if lane == "" {
		return ""
	}
	if last.lane != "" && last.lane != lane {
		return last.lane
	}
	if last.lane == lane {
		return last.previous
	}
	return ""
}

// IDs returns the pod IDs in the order of the last frame.
func (b *Bridge) IDs() []string { return slices.Clone(b.ids) }

// heading returns the direction that the front of pod faces, in radians,
// in world coordinates, where y points south. A pod on a reverse lane of a
// bay faces against its direction of travel. along is the direction of the
// piece under the pod, which a new pod, or a pod after a jump, takes in
// place of the lane direction.
func (b *Bridge) heading(pod sim.Pod, along direction, previous map[string]heading) heading {
	turn := 0.0
	if b.lanes[pod.LaneID].reverse {
		turn = math.Pi
	}
	last, known := previous[pod.ID]
	if known {
		dx, dy := pod.Position.X-last.anchor.X, pod.Position.Y-last.anchor.Y
		moved := math.Hypot(dx, dy)
		if moved < headingStep {
			return last
		}
		if moved <= maxStep {
			return heading{anchor: pod.Position, angle: wrap(math.Atan2(dy, dx) + turn)}
		}
	}
	if along.ok {
		return heading{anchor: pod.Position, angle: wrap(along.angle + turn)}
	}
	if angle, ok := b.lanes.tangent(pod.LaneID, pod.LaneDistance); ok {
		return heading{anchor: pod.Position, angle: wrap(angle + turn)}
	}
	// A pod at a berth has no lane.
	if angle, ok := b.berths[pod.BerthID]; ok {
		return heading{anchor: pod.Position, angle: angle}
	}
	return heading{anchor: pod.Position, angle: last.angle}
}

// wrap returns angle in the range (-pi, pi].
func wrap(angle float64) float64 {
	angle = math.Remainder(angle, 2*math.Pi)
	if angle <= -math.Pi {
		angle += 2 * math.Pi
	}
	return angle
}

func classIndex(class sim.VehicleClass) int {
	if class == "" {
		return 0
	}
	if index := slices.Index(Classes, class); index >= 0 {
		return index
	}
	return 0
}

// curveSamples is the number of points of the arc length table of a curved
// lane. remote.Motion uses the same number.
const curveSamples = 65

type laneShape struct {
	from, to sim.Point
	control  *sim.Point
	// points holds the samples of a curve, and lengths holds the arc length
	// at each sample. The simulation moves a pod on the same samples.
	points  []sim.Point
	lengths []float64
	// reverse is true for a reverse lane of a maneuver group.
	reverse bool
	// fromDistance and toDistance are the distances of the two lane nodes
	// from the mainline. See nodeDistances.
	fromDistance, toDistance float64
	// trimStart and trimEnd are the lengths at the two ends that a round
	// join replaces. See addFillets.
	trimStart, trimEnd float64
}

type laneTable map[string]laneShape

// berthHeadings returns the direction of a pod at each berth: the end
// direction of a lane that arrives at the berth node and is not a reverse
// lane. A lane of the station of the berth comes first. A berth with no
// such lane is not in the result.
func (t laneTable) berthHeadings(network sim.Network) map[string]float64 {
	type arrival struct {
		angle   float64
		station string
	}
	arriving := make(map[string][]arrival)
	for _, lane := range network.Lanes {
		if t[lane.ID].reverse {
			continue
		}
		if angle, ok := t.tangent(lane.ID, math.Inf(1)); ok {
			arriving[lane.To] = append(arriving[lane.To], arrival{angle, lane.StationID})
		}
	}
	headings := make(map[string]float64)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			choices := arriving[berth.Node]
			if len(choices) == 0 {
				continue
			}
			choice := choices[0]
			if i := slices.IndexFunc(choices, func(a arrival) bool { return a.station == station.ID }); i >= 0 {
				choice = choices[i]
			}
			headings[berth.ID] = choice.angle
		}
	}
	return headings
}

func newLaneTable(network sim.Network) laneTable {
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	reverse := make(map[string]bool)
	for _, group := range network.ManeuverGroups {
		for _, id := range group.ReverseLaneIDs {
			reverse[id] = true
		}
	}
	table := make(laneTable, len(network.Lanes))
	for _, lane := range network.Lanes {
		shape := laneShape{from: nodes[lane.From], to: nodes[lane.To], reverse: reverse[lane.ID]}
		if lane.Control != nil {
			control := *lane.Control
			shape.control = &control
			shape.points = make([]sim.Point, curveSamples)
			shape.lengths = make([]float64, curveSamples)
			shape.points[0] = shape.from
			for i := 1; i < curveSamples; i++ {
				point := shape.point(float64(i) / (curveSamples - 1))
				last := shape.points[i-1]
				shape.points[i] = point
				shape.lengths[i] = shape.lengths[i-1] + math.Hypot(point.X-last.X, point.Y-last.Y)
			}
		}
		table[lane.ID] = shape
	}
	return table
}

func (s laneShape) point(t float64) sim.Point {
	u := 1 - t
	c := *s.control
	return sim.Point{X: u*u*s.from.X + 2*u*t*c.X + t*t*s.to.X, Y: u*u*s.from.Y + 2*u*t*c.Y + t*t*s.to.Y}
}

// tangent returns the direction of lane id at distance along it.
func (t laneTable) tangent(id string, distance float64) (float64, bool) {
	shape, ok := t[id]
	if !ok {
		return 0, false
	}
	if shape.control == nil {
		dx, dy := shape.to.X-shape.from.X, shape.to.Y-shape.from.Y
		if dx == 0 && dy == 0 {
			return 0, false
		}
		return math.Atan2(dy, dx), true
	}
	d := shape.direction(arcParam(shape.lengths, distance))
	if d.X == 0 && d.Y == 0 {
		return 0, false
	}
	return math.Atan2(d.Y, d.X), true
}

// Topology is the geometry that the ride page draws.
type Topology struct {
	ServerStart string            `json:"serverStart"`
	Epoch       string            `json:"epoch"`
	Generation  uint64            `json:"generation"`
	Nodes       []sim.Node        `json:"nodes"`
	Lanes       []TopologyLane    `json:"lanes"`
	Pieces      []TopologyPiece   `json:"pieces"`
	Stations    []TopologyStation `json:"stations"`
	Classes     []TopologyClass   `json:"classes"`
	Levels      Levels            `json:"levels"`
	// PartyLimit is the largest party size of a new order.
	PartyLimit int                    `json:"partyLimit"`
	Geo        *project.Geo           `json:"geo,omitzero"`
	Map        *project.MapBackground `json:"map,omitzero"`
}

// TopologyLane is one lane. Control is present on a curved lane.
// FromDistance and ToDistance are the distances of the two lane nodes from
// the mainline, for Levels. TrimStart and TrimEnd are the lengths at the two
// ends that a piece replaces, or zero.
type TopologyLane struct {
	ID           string     `json:"id"`
	From         string     `json:"from"`
	To           string     `json:"to"`
	Control      *sim.Point `json:"control,omitempty"`
	SpeedLimit   float64    `json:"speedLimit"`
	StationID    string     `json:"stationID,omitempty"`
	FromDistance float64    `json:"fromDistance"`
	ToDistance   float64    `json:"toDistance"`
	TrimStart    float64    `json:"trimStart,omitzero"`
	TrimEnd      float64    `json:"trimEnd,omitzero"`
}

// TopologyPiece is a round join from the end of lane In to the start of
// lane Out, as indexes of Topology.Lanes. It is a quadratic Bezier curve
// from the point at TrimEnd before the end of In to the point at TrimStart
// after the start of Out, on the sample lines of the lanes, with the control
// point Control, x and y, rounded to 1 mm.
type TopologyPiece struct {
	In      int        `json:"in"`
	Out     int        `json:"out"`
	Control [2]float64 `json:"control"`
}

// TopologyStation is one station with the positions of its berths.
type TopologyStation struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ParkingOnly bool        `json:"parkingOnly"`
	Berths      []sim.Point `json:"berths"`
	// Height is the mean beam top at the berths.
	Height float64 `json:"height"`
}

// TopologyClass gives the body length of a class index.
type TopologyClass struct {
	Name         sim.VehicleClass `json:"name"`
	LengthMeters float64          `json:"lengthMeters"`
}

// expressLengthMeters is the body length of an Express pod. Its class
// profile has no physical length, and the standee design sets 6 m.
const expressLengthMeters = 6

// NewTopology returns the geometry of state.
func NewTopology(state session.State) Topology {
	network := state.Network
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	lanes := newLaneTable(network)
	distances := lanes.addDistances(network)
	pieces := lanes.addFillets(network)
	topology := Topology{
		ServerStart: state.ServerStart, Epoch: state.Epoch, Generation: state.Generation, PartyLimit: sim.MaxNewPartySize, Levels: levels,
		Nodes: slices.Clone(network.Nodes), Geo: state.Geo, Map: state.Map,
		Lanes:    make([]TopologyLane, 0, len(network.Lanes)),
		Pieces:   make([]TopologyPiece, 0, len(pieces)),
		Stations: make([]TopologyStation, 0, len(network.Stations)),
	}
	indexes := make(map[string]int, len(network.Lanes))
	for i, lane := range network.Lanes {
		shape := lanes[lane.ID]
		indexes[lane.ID] = i
		topology.Lanes = append(topology.Lanes, TopologyLane{ID: lane.ID, From: lane.From, To: lane.To, Control: lane.Control, SpeedLimit: lane.SpeedLimit, StationID: lane.StationID, FromDistance: shape.fromDistance, ToDistance: shape.toDistance, TrimStart: shape.trimStart, TrimEnd: shape.trimEnd})
	}
	for _, p := range pieces {
		topology.Pieces = append(topology.Pieces, TopologyPiece{In: indexes[p.key.in], Out: indexes[p.key.out], Control: [2]float64{millimeters(p.control.X), millimeters(p.control.Y)}})
	}
	for _, station := range network.Stations {
		berths := make([]sim.Point, 0, len(station.Berths))
		for _, berth := range station.Berths {
			berths = append(berths, nodes[berth.Node])
		}
		topology.Stations = append(topology.Stations, TopologyStation{ID: station.ID, Name: station.Name, ParkingOnly: station.ParkingOnly, Berths: berths, Height: stationHeight(station, distances)})
	}
	if state.Simulation.OrderContract == sim.ExpressOrderContract {
		profile, _ := sim.LookupVehicleClassWithOrderContract(sim.ExpressClass, sim.ExpressOrderContract)
		topology.PartyLimit = profile.MaxNewPartySize
	}
	for _, class := range Classes {
		spec, _ := sim.LookupVehicleClass(class)
		length := spec.BodyLengthMeters
		if length == 0 {
			length = expressLengthMeters
		}
		topology.Classes = append(topology.Classes, TopologyClass{Name: class, LengthMeters: length})
	}
	return topology
}

// millimeters rounds meters to whole millimeters. The trims are whole
// millimeters, and the control points of the pieces of a topology are
// rounded, so the JSON text stays short. A rounded control point moves a
// piece by at most half of its 0.7 mm rounding.
func millimeters(meters float64) float64 { return math.Round(meters*1000) / 1000 }

// PodSummary is one row of the pod list of the page. From and To are the
// station names of the trip of the first rider aboard.
type PodSummary struct {
	ID       string           `json:"id"`
	Class    sim.VehicleClass `json:"class"`
	Activity sim.Activity     `json:"activity"`
	Riders   int              `json:"riders"`
	From     string           `json:"from,omitempty"`
	To       string           `json:"to,omitempty"`
}

// Pods returns a summary of each pod of state, in the order of the state.
// A station with no name shows its ID.
func Pods(state session.State) []PodSummary {
	names := stationNames(state.Network)
	pods := make([]PodSummary, 0, len(state.Simulation.Vehicles))
	for _, vehicle := range state.Simulation.Vehicles {
		class := vehicle.Pod.Class
		if class == "" {
			class = sim.LegacyClass
		}
		summary := PodSummary{ID: vehicle.Pod.ID, Class: class, Activity: vehicle.Pod.Activity}
		for _, rider := range vehicle.Riders {
			if rider.Completed {
				continue
			}
			if summary.Riders == 0 {
				summary.From, summary.To = names.of(rider.From), names.of(rider.To)
			}
			summary.Riders++
		}
		pods = append(pods, summary)
	}
	return pods
}
