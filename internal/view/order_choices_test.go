package view

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestOrderChoicesDefaultAndBounds(t *testing.T) {
	t.Parallel()
	game := controlTestGame(t, controlLayouts[0].input)
	game.connected = true
	command := game.orderCommand()
	if command.PartySize != 1 || command.SharingConsent != sim.PrivateConsent || command.Service != sim.OnDemandService {
		t.Fatalf("initial order is not private singleton: %+v", command)
	}
	if !findButton(t, game.buttons(), "party-less").disabled {
		t.Fatal("party can shrink below one")
	}
	for range 10 {
		game.click(centerOfButton(findButton(t, game.buttons(), "party-more")))
	}
	if game.selectedPartySize() != 8 || !findButton(t, game.buttons(), "party-more").disabled {
		t.Fatal("party exceeds approved bound")
	}
	game.click(centerOfButton(findButton(t, game.buttons(), "order-sharing")))
	shared := game.orderCommand()
	if shared.SharingConsent != sim.SharedConsent || shared.PartySize != 8 {
		t.Fatalf("choice not stored as whole party: %+v", shared)
	}
	game.click(centerOfButton(findButton(t, game.buttons(), "party-less")))
	game.click(centerOfButton(findButton(t, game.buttons(), "order-sharing")))
	if game.orderCommand().SharingConsent != sim.PrivateConsent || shared.SharingConsent != sim.SharedConsent || shared.PartySize != 8 {
		t.Fatal("later choices changed captured order")
	}
	for _, action := range []string{"party-less", "party-more", "order-sharing"} {
		game.connected = false
		if !findButton(t, game.buttons(), action).disabled {
			t.Errorf("offline choice enabled: %s", action)
		}
	}
}

func TestOrderSubmissionCarriesPrivateConsent(t *testing.T) {
	t.Parallel()
	game := sharedTestGame(t)
	game.request()
	result := commandResult(t, game)
	if result.Err != nil || result.Reply.Error != "" || result.Command.PartySize != 1 || result.Command.SharingConsent != sim.PrivateConsent || result.Command.Service != sim.OnDemandService {
		t.Fatalf("submission lost explicit private consent: %+v", result)
	}
}

func TestOrderChoiceLayoutDoesNotOverlap(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		game := controlTestGame(t, layout.input)
		controls := game.buttons()
		for _, action := range []string{"party-less", "party-count", "party-more", "order-sharing"} {
			choice := findButton(t, controls, action)
			for _, other := range controls {
				if other.action == choice.action {
					continue
				}
				if choice.x < other.x+other.w && choice.x+choice.w > other.x && choice.y < other.y+other.h && choice.y+choice.h > other.y {
					t.Errorf("%s overlaps %s at %+v", action, other.action, layout.input)
				}
			}
		}
	}
}
