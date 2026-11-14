package view

import (
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// rideActionPrefix is the action prefix of the Ride button of the pod
// inspector. The pod ID follows the prefix.
const rideActionPrefix = "ride/"

// WithRide sets the func that opens the first-person ride page on the pod
// with an ID. It returns an error when it cannot open the page. With a nil
// func, the pod inspector does not show Ride. The desktop client has no
// ride page.
func WithRide(open func(podID string) error) Option {
	return func(g *Game) { g.openRide = open }
}

// rideButton returns the Ride button of the pod inspector. It shows only
// while the inspector shows the selected pod. It sends no command, so it
// stays enabled without a connection. The button is below the inspector
// rows and above the pod selector.
func (g *Game) rideButton(state sim.Snapshot) (button, bool) {
	if g.openRide == nil || g.showOrders || g.showDemand || g.incidentsOpen(state) {
		return button{}, false
	}
	vehicle, ok := selectedVehicle(state, g.selected)
	if !ok {
		return button{}, false
	}
	return button{x: 986, y: 331, w: 74, h: 24, label: "Ride", action: rideActionPrefix + vehicle.Pod.ID, fontSize: 11}, true
}

// pressRide opens the ride page for a Ride button action, and shows an
// error in the message line. It returns false for any other action.
func (g *Game) pressRide(action string) bool {
	podID, ok := strings.CutPrefix(action, rideActionPrefix)
	if !ok || g.openRide == nil {
		return false
	}
	g.message = ""
	if err := g.openRide(podID); err != nil {
		g.message = err.Error()
	}
	return true
}
