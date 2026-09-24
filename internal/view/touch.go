package view

import (
	"image"
	"math"
	"slices"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

// touchTapSlop is the distance in design units that a touch can move and
// still be a tap. A longer move is a drag.
const touchTapSlop = 10

// touchPoint is one touch on the screen in one frame.
type touchPoint struct {
	id    ebiten.TouchID
	point sim.Point
}

// touchFrame is the touch input of one frame.
type touchFrame struct {
	// touches are the touches on the screen in this frame.
	touches []touchPoint
	// mapArea is the map viewport. Only touches that start in it pan and
	// zoom the map.
	mapArea image.Rectangle
	// tapSlop is the distance in screen pixels that a touch can move and
	// still be a tap.
	tapSlop float64
}

// touchActions are the results of one touch frame.
type touchActions struct {
	// mapPressed is true when a touch starts on the map.
	mapPressed bool
	// tap is true when a tap ends in this frame. The game then acts as for
	// a left click at tapPoint.
	tap      bool
	tapPoint sim.Point
	// pan is the distance to move the map.
	pan sim.Point
	// zoom is the zoom factor around zoomCenter. It is 0 when the touches
	// do not zoom.
	zoom       float64
	zoomCenter sim.Point
}

// touchTrack follows one touch from its start to its end.
type touchTrack struct {
	id          ebiten.TouchID
	start, last sim.Point
	onMap       bool
	// moved is true when the touch moved tapSlop or more from its start.
	moved bool
	// shared is true when a different touch was on the screen at the same
	// time. Such a touch is not a tap.
	shared bool
}

// touchGestures turns touch frames into taps, pans, and zooms.
//
// A touch that stays near its start is a tap when it ends, if no other touch
// was on the screen during its life. One touch that starts on the map and
// moves pans the map. Two touches on the map zoom around their midpoint, and
// a move of the midpoint pans the map. When one of two touches ends, the
// other touch continues to pan the map. A touch that starts outside the map
// does not pan or zoom.
type touchGestures struct {
	tracks []touchTrack
}

// update reads the touches of one frame and returns the actions.
func (g *touchGestures) update(frame touchFrame) touchActions {
	var actions touchActions
	var moves []touchMove
	kept := g.tracks[:0]
	for _, track := range g.tracks {
		index := slices.IndexFunc(frame.touches, func(touch touchPoint) bool { return touch.id == track.id })
		if index < 0 {
			if !track.moved && !track.shared {
				actions.tap, actions.tapPoint = true, track.last
			}
			continue
		}
		point := frame.touches[index].point
		if math.Hypot(point.X-track.start.X, point.Y-track.start.Y) >= frame.tapSlop {
			track.moved = true
		}
		if track.onMap {
			moves = append(moves, touchMove{from: track.last, to: point, pans: track.moved || track.shared})
		}
		track.last = point
		kept = append(kept, track)
	}
	g.tracks = kept
	actions.mapPressed = g.start(frame)
	if len(g.tracks) > 1 {
		for index := range g.tracks {
			g.tracks[index].shared = true
		}
	}
	switch {
	case len(moves) >= 2:
		actions.pan, actions.zoom, actions.zoomCenter = pinch(moves[0], moves[1])
	case len(moves) == 1 && moves[0].pans:
		actions.pan = sim.Point{X: moves[0].to.X - moves[0].from.X, Y: moves[0].to.Y - moves[0].from.Y}
	}
	return actions
}

// start adds a track for each new touch. It reports whether a new touch
// is on the map.
func (g *touchGestures) start(frame touchFrame) (onMap bool) {
	for _, touch := range frame.touches {
		if slices.ContainsFunc(g.tracks, func(track touchTrack) bool { return track.id == touch.id }) {
			continue
		}
		track := touchTrack{id: touch.id, start: touch.point, last: touch.point}
		track.onMap = image.Pt(int(touch.point.X), int(touch.point.Y)).In(frame.mapArea)
		onMap = onMap || track.onMap
		g.tracks = append(g.tracks, track)
	}
	return onMap
}

// touchMove is the move of one map touch from the last frame to this frame.
// pans is false while the touch can still be a tap.
type touchMove struct {
	from, to sim.Point
	pans     bool
}

// pinch returns the pan and the zoom of two map touches. The pan is the
// move of the midpoint. The zoom factor is the change of the distance
// between the touches, around the current midpoint. The factor is 0 when
// the touches were at the same point.
func pinch(a, b touchMove) (pan sim.Point, factor float64, center sim.Point) {
	before := sim.Point{X: (a.from.X + b.from.X) / 2, Y: (a.from.Y + b.from.Y) / 2}
	center = sim.Point{X: (a.to.X + b.to.X) / 2, Y: (a.to.Y + b.to.Y) / 2}
	pan = sim.Point{X: center.X - before.X, Y: center.Y - before.Y}
	if distance := math.Hypot(a.from.X-b.from.X, a.from.Y-b.from.Y); distance >= 1 {
		factor = math.Hypot(a.to.X-b.to.X, a.to.Y-b.to.Y) / distance
	}
	return pan, factor, center
}

// updateTouchInput reads the touches of this frame from Ebitengine and
// applies the gestures.
func (g *Game) updateTouchInput() {
	ids := ebiten.AppendTouchIDs(nil)
	touches := make([]touchPoint, 0, len(ids))
	for _, id := range ids {
		x, y := ebiten.TouchPosition(id)
		touches = append(touches, touchPoint{id: id, point: sim.Point{X: float64(x), Y: float64(y)}})
	}
	actions := g.touch.update(touchFrame{touches: touches, mapArea: g.camera.viewport, tapSlop: touchTapSlop * g.layout.unit})
	g.applyTouch(actions, ebiten.IsKeyPressed(ebiten.KeyShift) || pointerShift())
}

// applyTouch applies the actions of one touch frame. A touch on the map
// stops following the selected pod, as a mouse press does. A tap acts as a
// left click. With shift true, a tap on a station sets To.
func (g *Game) applyTouch(actions touchActions, shift bool) {
	if actions.mapPressed {
		g.followSelected = false
	}
	changed := g.camera.pan(actions.pan)
	if actions.zoom > 0 {
		changed = g.camera.zoomAt(actions.zoomCenter, actions.zoom) || changed
	}
	if changed {
		g.syncCamera()
	}
	if !actions.tap {
		return
	}
	if g.camera.contains(actions.tapPoint) {
		g.pickOnMap(actions.tapPoint, shift)
		return
	}
	g.click(actions.tapPoint)
}
