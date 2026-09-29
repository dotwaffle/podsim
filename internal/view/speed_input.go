package view

import (
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// rightClick decreases speed only when the enabled Speed button is pressed.
func (g *Game) rightClick(point sim.Point) {
	for _, button := range g.buttons() {
		if button.action != "speed" || button.disabled || point.X < button.x || point.X >= button.x+button.w || point.Y < button.y || point.Y >= button.y+button.h {
			continue
		}
		g.journeySearch.focus = 0
		g.submit(session.Command{Action: "speed", Speed: previousSpeed(g.state.Speed)})
		return
	}
}

// previousSpeed wraps the visible choices and maps legacy speeds downward.
func previousSpeed(speed int) int {
	for _, previous := range [...]int{60, 15, 5, 2, 1} {
		if previous < speed {
			return previous
		}
	}
	return 60
}
