package view

import (
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// emergencyActionPrefix is the action prefix of the emergency button of
// the pod inspector. The pod ID follows the prefix, so a click sends the
// pod that the button showed.
const emergencyActionPrefix = "emergency/"

// carriesParty reports whether vehicle carries a party that has not
// arrived. It is the rule of the emergency command (precondition 4 of the
// incident emergency contract): a boarding pod carries its party before it
// is occupied, and a pod keeps its riders after a journey.
func carriesParty(vehicle sim.Vehicle) bool {
	return (vehicle.Pod.Activity == sim.Boarding || vehicle.Pod.Occupied) && vehicle.RidersAboard() > 0
}

// hasEmergency reports whether an active emergency names the pod with
// podID.
func hasEmergency(emergencies sim.EmergenciesView, podID string) bool {
	return slices.ContainsFunc(emergencies.Active, func(emergency sim.EmergencyView) bool {
		return emergency.PodID == podID
	})
}

// emergencyButton returns the emergency control of the pod inspector
// (section 10.6 of the incident emergency contract). It shows only with
// the emergency marker, only while the inspector shows the selected pod,
// and only on a pod that carries a party and has no emergency. The command
// has no order ID, so the party is the first active rider. The view does
// not check the other preconditions: the server refuses a command that
// fails one, and the message line shows the error. The button is below the
// fault button, between the status line and the journey line.
func (g *Game) emergencyButton(state sim.Snapshot) (button, bool) {
	if state.EmergencyContract == "" || g.showOrders || g.showDemand {
		return button{}, false
	}
	vehicle, ok := selectedVehicle(state, g.selected)
	if !ok || !carriesParty(vehicle) || hasEmergency(state.Emergencies, vehicle.Pod.ID) {
		return button{}, false
	}
	return button{x: 986, y: 168, w: 74, h: 22, label: "Emergency", action: emergencyActionPrefix + vehicle.Pod.ID, fontSize: 11, disabled: !g.connected || g.pending}, true
}

// emergencyCommand returns the command of an emergency button action. It
// reports false for any other action.
func emergencyCommand(action string) (session.Command, bool) {
	podID, ok := strings.CutPrefix(action, emergencyActionPrefix)
	if !ok {
		return session.Command{}, false
	}
	return session.Command{Action: "emergency", PodID: podID}, true
}

// isEmergencyButton reports whether control is the emergency button.
func isEmergencyButton(control button) bool {
	_, ok := emergencyCommand(control.action)
	return ok
}
