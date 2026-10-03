package view

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func (g *Game) selectedPartySize() int {
	return max(1, min(g.orderPartySize, g.orderPartyLimit()))
}

func (g *Game) orderPartyLimit() int {
	if g.state.Simulation.OrderContract == sim.ExpressOrderContract {
		profile, _ := sim.LookupVehicleClassWithOrderContract(sim.ExpressClass, sim.ExpressOrderContract)
		return profile.MaxNewPartySize
	}
	return sim.MaxNewPartySize
}

func (g *Game) orderCommand() session.Command {
	consent := g.orderSharingConsent
	if consent == "" {
		consent = sim.PrivateConsent
	}
	var contract sim.OrderContract
	if g.state.Simulation.OrderContract == sim.ExpressOrderContract {
		contract = sim.ExpressOrderContract
	}
	return session.Command{Action: "trip", Origin: g.origin, Destination: g.destination,
		PartySize: g.selectedPartySize(), SharingConsent: consent, Service: sim.OnDemandService,
		OrderContract: contract}
}

func (g *Game) orderChoiceButtons(disabled bool) []button {
	size := g.selectedPartySize()
	label := "Private party"
	shared := g.orderSharingConsent == sim.SharedConsent
	if shared {
		label = "Share allowed"
	}
	return []button{
		{x: 810, y: 565, w: 24, h: 28, label: "−", action: "party-less", disabled: disabled || size <= 1},
		{x: 836, y: 565, w: 58, h: 28, label: fmt.Sprintf("Party %d", size), action: "party-count", disabled: true, fontSize: 11},
		{x: 896, y: 565, w: 24, h: 28, label: "+", action: "party-more", disabled: disabled || size >= g.orderPartyLimit()},
		{x: 930, y: 565, w: 130, h: 28, label: label, action: "order-sharing", selected: shared, disabled: disabled, fontSize: 11},
	}
}
