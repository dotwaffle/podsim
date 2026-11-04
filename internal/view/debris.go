package view

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

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
