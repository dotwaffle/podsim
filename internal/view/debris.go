package view

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// debrisRingRadius and debrisRingWidth set the ring of a debris mark in
	// display units. The ring has a fixed size, because a segment of 2 m can
	// be below one pixel at Fit.
	debrisRingRadius = 7
	debrisRingWidth  = 2
)

// debrisMark is the map form of one active debris fault, in screen points.
// path is the stroke from from to to. On a curved lane, it holds the lane
// points between them.
type debrisMark struct {
	from, to, center sim.Point
	path             []sim.Point
}

// debrisMarks returns the marks of the active debris, in record order
// (section 5.6 of the incident stage 7 contract). A record with an unknown
// lane has no mark. A lane of a collapsed station keeps its marks, so a
// blocked lane is never invisible.
func (g *Game) debrisMarks(faults sim.FaultsView, detailed bool) []debrisMark {
	var marks []debrisMark
	for _, fault := range faults.Active {
		if fault.Kind != sim.FaultKindDebris || fault.FromMeters == nil || fault.ToMeters == nil {
			continue
		}
		lane, ok := g.displayIndex().lanes[fault.LaneID]
		length := g.network.Length(lane)
		if !ok || length <= 0 {
			continue
		}
		marks = append(marks, g.laneGeometry(lane, detailed).debrisMark(*fault.FromMeters/length, *fault.ToMeters/length))
	}
	return marks
}

// debrisMark returns the mark of the part of the lane between two fractions
// of its screen length.
func (geometry laneGeometry) debrisMark(from, to float64) debrisMark {
	mark := debrisMark{path: geometry.between(from, to)}
	mark.from, mark.to = mark.path[0], mark.path[len(mark.path)-1]
	mark.center, _ = geometry.along((from + to) / 2)
	return mark
}

// between returns the points of the lane between two fractions of its
// screen length: the two ends, and each lane point between them.
func (geometry laneGeometry) between(from, to float64) []sim.Point {
	start, _ := geometry.along(from)
	end, _ := geometry.along(to)
	total, distance := geometry.screenLength(), 0.0
	path := []sim.Point{start}
	for i := 1; i < geometry.count; i++ {
		a, b := geometry.points[i-1], geometry.points[i]
		distance += math.Hypot(b.X-a.X, b.Y-a.Y)
		if from*total < distance && distance < to*total {
			path = append(path, b)
		}
	}
	return append(path, end)
}

// drawDebrisMarks draws a stroke and a ring for each active debris fault.
func (g *Game) drawDebrisMarks(screen *ebiten.Image, faults sim.FaultsView, style networkStyle, scale spriteScale) {
	stroke := style.routeStroke(danger)
	ring := ringSprite(debrisRingRadius*g.layout.unit, debrisRingWidth*g.layout.unit, danger)
	for _, mark := range g.debrisMarks(faults, style.detailed) {
		for i := 1; i < len(mark.path); i++ {
			from, to := mark.path[i-1], mark.path[i]
			vector.StrokeLine(screen, float32(from.X), float32(from.Y), float32(to.X), float32(to.Y), stroke.width, rgb(stroke.color), stroke.antialias)
		}
		g.sprites.draw(screen, spriteDraw{center: mark.center, key: ring, scale: scale})
	}
}

// incidentPods returns the IDs of the pods with an active pod fault or an
// active emergency. Debris has no pod. The map is nil when no pod has one,
// so a frame with no incident allocates nothing.
func incidentPods(faults sim.FaultsView, emergencies sim.EmergenciesView) map[string]bool {
	var pods map[string]bool
	add := func(id string) {
		if pods == nil {
			pods = make(map[string]bool)
		}
		pods[id] = true
	}
	for _, fault := range faults.Active {
		if fault.Kind == sim.FaultKindPod {
			add(fault.PodID)
		}
	}
	for _, emergency := range emergencies.Active {
		add(emergency.PodID)
	}
	return pods
}

// podRingColor returns the color of the ring around a pod on the map. A pod
// with an incident has a danger ring, also before it waits. A pod that waits
// for any other reason has an amber ring. It reports false for no ring.
func podRingColor(pod sim.Pod, incident bool) (uint32, bool) {
	switch {
	case incident:
		return danger, true
	case pod.WaitReason != sim.NoWait:
		return amber, true
	}
	return 0, false
}

const (
	// placeDebrisAction is the action of the button that arms a debris click.
	placeDebrisAction = "place-debris"
	// lanePickRadius is the largest distance in design units from a click to
	// a lane that the click can pick.
	lanePickRadius = 12.0
	// placedDebrisMeters is the length of the debris that a click places.
	placedDebrisMeters = 2.0
	// noLaneMessage tells the user that an armed click found no lane.
	noLaneMessage = "No lane at that point. Click a lane, or press the button again to stop."
)

// laneTarget is a drawn lane that a debris click can pick.
type laneTarget struct {
	lane sim.Lane
	path lanePath
}

// lanePick is the lane that a click picks, and the place of the click on
// the lane as a fraction of its screen length.
type lanePick struct {
	lane     sim.Lane
	fraction float64
}

// pickLane returns the lane nearest to point, within radius. The first lane
// in order wins a tie. It reports false when no lane is in reach.
func pickLane(point sim.Point, lanes []laneTarget, radius float64) (lanePick, bool) {
	var found lanePick
	best := math.Inf(1)
	for _, target := range lanes {
		if distance, fraction := nearestOnPath(point, target.path); distance < best {
			found, best = lanePick{lane: target.lane, fraction: fraction}, distance
		}
	}
	return found, best <= radius
}

// nearestOnPath returns the distance from point to path, and the place of
// the nearest point as a fraction of the path length. A path with no length
// has fraction 0. An empty path is at an infinite distance.
func nearestOnPath(point sim.Point, path lanePath) (distance, fraction float64) {
	distance = math.Inf(1)
	walked, nearest := 0.0, 0.0
	for i := 1; i < len(path); i++ {
		a, b := path[i-1], path[i]
		dx, dy := b.X-a.X, b.Y-a.Y
		length := math.Hypot(dx, dy)
		t := 0.0
		if length > 0 {
			t = min(1, max(0, ((point.X-a.X)*dx+(point.Y-a.Y)*dy)/(length*length)))
		}
		if d := math.Hypot(a.X+dx*t-point.X, a.Y+dy*t-point.Y); d < distance {
			distance, nearest = d, walked+length*t
		}
		walked += length
	}
	if walked > 0 {
		fraction = nearest / walked
	}
	return distance, fraction
}

// debrisSegment returns the segment of placedDebrisMeters centered on the
// place center of a lane, clamped into the lane. A shorter lane gets a
// segment of its whole length.
func debrisSegment(center, length float64) (from, to float64) {
	from = max(0, center-placedDebrisMeters/2)
	to = min(length, from+placedDebrisMeters)
	return max(0, to-placedDebrisMeters), to
}

// laneTargets returns the lanes that a debris click can pick: the lanes that
// the map draws in the viewport, in network order. The thin lanes of a
// collapsed station are hidden under its marker, so they are not targets.
func (g *Game) laneTargets() []laneTarget {
	style := g.currentNetworkStyle(g.collapsedStationMarkers())
	targets := make([]laneTarget, 0, len(g.network.Lanes))
	for _, lane := range g.network.Lanes {
		if style.dimLane(lane) {
			continue
		}
		if _, path, ok := g.baseLane(lane, style.detailed, g.layout.mapViewport); ok {
			targets = append(targets, laneTarget{lane: lane, path: path})
		}
	}
	return targets
}

// placeDebris handles an armed click at point on the map. It sends a fault
// with no duration, so the debris lasts until a clear, and it disarms. The
// view checks nothing else: the server refuses a segment that fails a
// precondition, and the message line shows the error. A click that finds no
// lane sends nothing and stays armed.
func (g *Game) placeDebris(point sim.Point) {
	pick, ok := pickLane(point, g.laneTargets(), lanePickRadius*g.layout.unit)
	if !ok {
		g.message = noLaneMessage
		return
	}
	g.debrisArmed = false
	length := g.network.Length(pick.lane)
	from, to := debrisSegment(pick.fraction*length, length)
	g.submit(session.Command{Action: "fault", LaneID: pick.lane.ID, FromMeters: new(from), ToMeters: new(to)})
}

// placeDebrisButton returns the button that arms and disarms a debris click.
// While a command waits, the button can still disarm.
func (g *Game) placeDebrisButton() button {
	control := button{x: 810, y: 100, w: 250, h: 22, label: "Place debris", action: placeDebrisAction, fontSize: 11, disabled: !g.connected || g.pending}
	if g.debrisArmed {
		control.label, control.selected, control.disabled = "Click a lane", true, false
	}
	return control
}
