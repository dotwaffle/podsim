package view

import (
	"math"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// podPickRadius is the largest distance in design units from a click to
	// a pod that selects the pod.
	podPickRadius = 18.0
	// stationPickRadius is the largest distance in design units from a click
	// to a station point that selects the station.
	stationPickRadius = 14.0
)

// mapCandidate is a pod or a station that a map click can select. pod is the
// index of the pod, or -1 for a station. station is the station ID, or empty
// for a pod.
type mapCandidate struct {
	pod     int
	station string
	center  sim.Point
}

// mapPickInput holds a map click and the objects that the click can select.
// All positions and radii are in physical pixels.
type mapPickInput struct {
	point                    sim.Point
	pods, stations           []mapCandidate
	podRadius, stationRadius float64
}

// pickMapTarget returns the object that a map click selects. It returns the
// nearest pod in reach. When no pod is in reach, it returns the nearest
// station point in reach. A pod wins over a station, because an idle pod can
// stay at the station. It returns false when no object is in reach.
func pickMapTarget(input mapPickInput) (mapCandidate, bool) {
	if pod, ok := nearestCandidate(input.point, input.pods, input.podRadius); ok {
		return pod, true
	}
	return nearestCandidate(input.point, input.stations, input.stationRadius)
}

// nearestCandidate returns the candidate nearest to point that is less than
// radius from point. It returns false when no candidate is in reach.
func nearestCandidate(point sim.Point, candidates []mapCandidate, radius float64) (mapCandidate, bool) {
	var found mapCandidate
	best := radius
	ok := false
	for _, candidate := range candidates {
		if distance := math.Hypot(point.X-candidate.center.X, point.Y-candidate.center.Y); distance < best {
			found, best, ok = candidate, distance, true
		}
	}
	return found, ok
}

// mapPickTargets returns the objects that a click at point can select. The
// pods are the pods on the map. A pod that the map does not show is not a
// target. The stations are the passenger stations. A station point is its
// marker anchor. When the map shows the berths of a station, each berth is
// also a point of the station. There are no station points while the station
// chips are disabled.
func (g *Game) mapPickTargets(point sim.Point) mapPickInput {
	input := mapPickInput{point: point, podRadius: podPickRadius * g.layout.unit, stationRadius: stationPickRadius * g.layout.unit}
	for i, v := range g.mapSnapshot().Vehicles {
		if g.podHiddenInCluster(v, i) {
			continue
		}
		input.pods = append(input.pods, mapCandidate{pod: i, center: g.mapPoint(v.Pod.Position)})
	}
	if g.state.Simulation.Demo || !g.connected {
		return input
	}
	anchors := g.stationAnchors()
	for _, station := range g.passengerStations() {
		if anchor, ok := anchors[station.ID]; ok {
			input.stations = append(input.stations, mapCandidate{pod: -1, station: station.ID, center: g.mapPoint(anchor)})
		}
		if !g.showStationBerths(station) {
			continue
		}
		for _, berth := range station.Berths {
			if node, ok := g.network.Node(berth.Node); ok {
				input.stations = append(input.stations, mapCandidate{pod: -1, station: station.ID, center: g.mapPoint(node.Position)})
			}
		}
	}
	return input
}

// pickOnMap handles a click at point on the map. A click on a pod selects
// the pod. A click on a station sets From. With toStation true, for example
// with Shift pressed, it sets To. The station page then shows the chip of
// the station.
func (g *Game) pickOnMap(point sim.Point, toStation bool) {
	target, ok := pickMapTarget(g.mapPickTargets(point))
	if !ok {
		return
	}
	if target.station == "" {
		g.selectPod(target.pod)
		return
	}
	if toStation {
		g.destination = target.station
	} else {
		g.origin = target.station
	}
	g.stationPage = stationPageOf(g.stationPages(), target.station, g.stationPage)
	g.message = ""
}
