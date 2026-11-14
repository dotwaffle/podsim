package ride

import "github.com/dotwaffle/podsim/internal/sim"

// Levels gives the heights of the guideway. A station lane goes down from
// the mainline height to the station height over the ramp length.
type Levels struct {
	// MainTop is the beam top of the mainline in meters.
	MainTop float64 `json:"mainTop"`
	// StationTop is the beam top at the station level in meters.
	StationTop float64 `json:"stationTop"`
	// RampMeters is the distance from the mainline at which a station lane
	// reaches the station level.
	RampMeters float64 `json:"rampMeters"`
}

// levels are the heights of the ride page. MainTop is BEAM_TOP of
// web/ride.js.
var levels = Levels{MainTop: 5, StationTop: 1, RampMeters: 120}

// height returns the beam top at distance meters from the mainline. The
// height follows a smoothstep curve, so a ramp starts and ends level.
func (l Levels) height(distance float64) float64 {
	f := min(max(distance/l.RampMeters, 0), 1)
	return l.MainTop - (l.MainTop-l.StationTop)*f*f*(3-2*f)
}

// nodeDistances returns the distance of each node of network to the nearest
// mainline node, over station lanes in either direction, at most
// limit. A mainline node is a node of a lane with no station. A node that no
// mainline node reaches within limit has the distance limit.
//
// A node goes back on the queue each time its distance gets shorter. Lane
// lengths are not negative, so the distances stop changing. The search stops
// at limit, so it reads only the station lanes near the mainline.
func nodeDistances(network sim.Network, lanes laneTable, limit float64) map[string]float64 {
	distances := make(map[string]float64, len(network.Nodes))
	for _, node := range network.Nodes {
		distances[node.ID] = limit
	}
	type edge struct {
		to     string
		length float64
	}
	edges := make(map[string][]edge)
	var queue []string
	for _, lane := range network.Lanes {
		if lane.StationID == "" {
			for _, id := range []string{lane.From, lane.To} {
				if distances[id] != 0 {
					distances[id] = 0
					queue = append(queue, id)
				}
			}
			continue
		}
		length := lanes.length(lane.ID)
		edges[lane.From] = append(edges[lane.From], edge{lane.To, length})
		edges[lane.To] = append(edges[lane.To], edge{lane.From, length})
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, e := range edges[id] {
			if distance := distances[id] + e.length; distance < distances[e.to] {
				distances[e.to] = distance
				queue = append(queue, e.to)
			}
		}
	}
	return distances
}

// addDistances sets the node distances of each lane of t, and returns the
// distance of each node. See nodeDistances.
func (t laneTable) addDistances(network sim.Network) map[string]float64 {
	distances := nodeDistances(network, t, levels.RampMeters)
	for _, lane := range network.Lanes {
		shape := t[lane.ID]
		shape.fromDistance, shape.toDistance = distanceOf(distances, lane.From), distanceOf(distances, lane.To)
		t[lane.ID] = shape
	}
	return distances
}

// podHeight returns the beam top under pod. A pod on a lane interpolates the
// distances of the two lane nodes by the fraction of the lane behind it. A
// pod at a berth has the distance of the berth node. Other pods are at the
// mainline height.
func (b *Bridge) podHeight(pod sim.Pod) float64 {
	if shape, ok := b.lanes[pod.LaneID]; ok {
		fraction := 0.0
		if length := b.lanes.length(pod.LaneID); length > 0 {
			fraction = min(max(pod.LaneDistance/length, 0), 1)
		}
		return levels.height(shape.fromDistance + (shape.toDistance-shape.fromDistance)*fraction)
	}
	if node, ok := b.berthNodes[pod.BerthID]; ok {
		return levels.height(b.distances[node])
	}
	return levels.MainTop
}

// stationHeight returns the mean height of the berths of station.
func stationHeight(station sim.Station, distances map[string]float64) float64 {
	if len(station.Berths) == 0 {
		return levels.MainTop
	}
	sum := 0.0
	for _, berth := range station.Berths {
		sum += levels.height(distanceOf(distances, berth.Node))
	}
	return sum / float64(len(station.Berths))
}

// distanceOf returns the distance of node, or 0 for an unknown node, which
// draws at the mainline height.
func distanceOf(distances map[string]float64, node string) float64 {
	if distance, ok := distances[node]; ok {
		return distance
	}
	return 0
}
