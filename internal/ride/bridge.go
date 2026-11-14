// Package ride gives the first-person ride page the poses of the pods and the
// network geometry. It has no browser code, so native tests cover it.
package ride

import (
	"math"
	"slices"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// PoseFields is the count of numbers for each pod in a frame: x, y,
// heading, speed, and class index.
const PoseFields = 5

// minMove is the smallest movement in meters that sets a new heading. A
// shorter movement keeps the earlier heading, or the lane tangent.
const minMove = 0.01

// Classes gives the pod classes in the order of the class index of a frame.
var Classes = []sim.VehicleClass{sim.LegacyClass, sim.CompactClass, sim.GroupClass, sim.ExpressClass}

// Bridge keeps the motion buffer and the previous pose of each pod. Use it
// on one goroutine.
type Bridge struct {
	motion   remote.Motion
	revision revisionKey
	lanes    laneTable
	// headings holds the pose of each pod in the last frame. spare is the
	// map of the frame before it, which Frame clears and uses again.
	headings, spare map[string]heading
	ids             []string
	version         uint64
}

type revisionKey struct {
	serverStart, epoch string
	generation, rev    uint64
}

type heading struct {
	at    sim.Point
	angle float64
}

// FrameInfo describes a frame that Frame wrote.
type FrameInfo struct {
	// Count is the number of pods in the frame.
	Count int
	// Version changes when the pod IDs or their order change.
	Version uint64
	// Generation is the session generation of the frame.
	Generation uint64
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
		b.headings, b.spare = nil, nil
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
		angle := b.heading(pod, previous)
		b.headings[pod.ID] = heading{at: pod.Position, angle: angle}
		out = append(out, pod.Position.X, pod.Position.Y, angle, pod.Speed, float64(classIndex(pod.Class)))
	}
	if changed {
		b.ids = b.ids[:0]
		for _, vehicle := range snapshot.Vehicles {
			b.ids = append(b.ids, vehicle.Pod.ID)
		}
		b.version++
	}
	return out, FrameInfo{Count: len(snapshot.Vehicles), Version: b.version, Generation: b.revision.generation}
}

// IDs returns the pod IDs in the order of the last frame.
func (b *Bridge) IDs() []string { return slices.Clone(b.ids) }

// heading returns the direction of travel of pod in radians, in world
// coordinates, where y points south.
func (b *Bridge) heading(pod sim.Pod, previous map[string]heading) float64 {
	last, known := previous[pod.ID]
	if known {
		dx, dy := pod.Position.X-last.at.X, pod.Position.Y-last.at.Y
		if math.Hypot(dx, dy) >= minMove {
			return math.Atan2(dy, dx)
		}
	}
	if angle, ok := b.lanes.tangent(pod.LaneID, pod.LaneDistance); ok {
		return angle
	}
	if known {
		return last.angle
	}
	return 0
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
	// lengths holds the arc length at each sample of a curve.
	lengths []float64
}

type laneTable map[string]laneShape

func newLaneTable(network sim.Network) laneTable {
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	table := make(laneTable, len(network.Lanes))
	for _, lane := range network.Lanes {
		shape := laneShape{from: nodes[lane.From], to: nodes[lane.To]}
		if lane.Control != nil {
			control := *lane.Control
			shape.control = &control
			shape.lengths = make([]float64, curveSamples)
			last := shape.from
			for i := 1; i < curveSamples; i++ {
				point := shape.point(float64(i) / (curveSamples - 1))
				shape.lengths[i] = shape.lengths[i-1] + math.Hypot(point.X-last.X, point.Y-last.Y)
				last = point
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
	// Find the curve parameter of distance in the arc length table.
	i, _ := slices.BinarySearch(shape.lengths, distance)
	param := 1.0
	switch {
	case i == 0:
		param = 0
	case i < len(shape.lengths):
		span := shape.lengths[i] - shape.lengths[i-1]
		fraction := 0.0
		if span > 0 {
			fraction = (distance - shape.lengths[i-1]) / span
		}
		param = (float64(i-1) + fraction) / (curveSamples - 1)
	}
	// The derivative of a quadratic Bezier curve.
	c := *shape.control
	u := 1 - param
	dx := 2*u*(c.X-shape.from.X) + 2*param*(shape.to.X-c.X)
	dy := 2*u*(c.Y-shape.from.Y) + 2*param*(shape.to.Y-c.Y)
	if dx == 0 && dy == 0 {
		return 0, false
	}
	return math.Atan2(dy, dx), true
}

// Topology is the geometry that the ride page draws.
type Topology struct {
	ServerStart string                 `json:"serverStart"`
	Epoch       string                 `json:"epoch"`
	Generation  uint64                 `json:"generation"`
	Nodes       []sim.Node             `json:"nodes"`
	Lanes       []TopologyLane         `json:"lanes"`
	Stations    []TopologyStation      `json:"stations"`
	Classes     []TopologyClass        `json:"classes"`
	Geo         *project.Geo           `json:"geo,omitzero"`
	Map         *project.MapBackground `json:"map,omitzero"`
}

// TopologyLane is one lane. Control is present on a curved lane.
type TopologyLane struct {
	ID         string     `json:"id"`
	From       string     `json:"from"`
	To         string     `json:"to"`
	Control    *sim.Point `json:"control,omitempty"`
	SpeedLimit float64    `json:"speedLimit"`
	StationID  string     `json:"stationID,omitempty"`
}

// TopologyStation is one station with the positions of its berths.
type TopologyStation struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	ParkingOnly bool        `json:"parkingOnly"`
	Berths      []sim.Point `json:"berths"`
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
	topology := Topology{
		ServerStart: state.ServerStart, Epoch: state.Epoch, Generation: state.Generation,
		Nodes: slices.Clone(network.Nodes), Geo: state.Geo, Map: state.Map,
		Lanes:    make([]TopologyLane, 0, len(network.Lanes)),
		Stations: make([]TopologyStation, 0, len(network.Stations)),
	}
	for _, lane := range network.Lanes {
		topology.Lanes = append(topology.Lanes, TopologyLane{ID: lane.ID, From: lane.From, To: lane.To, Control: lane.Control, SpeedLimit: lane.SpeedLimit, StationID: lane.StationID})
	}
	for _, station := range network.Stations {
		berths := make([]sim.Point, 0, len(station.Berths))
		for _, berth := range station.Berths {
			berths = append(berths, nodes[berth.Node])
		}
		topology.Stations = append(topology.Stations, TopologyStation{ID: station.ID, Name: station.Name, ParkingOnly: station.ParkingOnly, Berths: berths})
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

// PodSummary is one row of the pod list of the page.
type PodSummary struct {
	ID       string           `json:"id"`
	Class    sim.VehicleClass `json:"class"`
	Activity sim.Activity     `json:"activity"`
	Riders   int              `json:"riders"`
	From     string           `json:"from,omitempty"`
	To       string           `json:"to,omitempty"`
}

// Pods returns a summary of each pod of state, in the order of the state.
func Pods(state session.State) []PodSummary {
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
				summary.From, summary.To = rider.From, rider.To
			}
			summary.Riders++
		}
		pods = append(pods, summary)
	}
	return pods
}
