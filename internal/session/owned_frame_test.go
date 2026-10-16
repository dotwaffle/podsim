package session

import (
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestOwnedStateFrameMatchesCopy(t *testing.T) {
	t.Parallel()
	for _, fixture := range frameFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			shared := newFrameFixture(t, fixture)
			t.Cleanup(shared.Close)
			state := shared.State()
			state.Simulation.Vehicles[0].Boardings = []sim.RiderBoarding{{BerthID: "harbor-1", MetersAtBoarding: 1}}
			copied := stateFrame(state)
			owned := ownedStateFrame(state)
			if !reflect.DeepEqual(owned, copied) {
				t.Fatal("owned frame changed content")
			}
			input := &state.Simulation.Vehicles[0]
			got := &owned.Simulation.Vehicles[0]
			if &got.Boardings[0] != &input.Boardings[0] || &got.Riders[0] != &input.Riders[0] || &got.Stops[0] != &input.Stops[0] {
				t.Fatal("owned frame copied already detached slices")
			}
			got.Boardings[0].BerthID = "owned changed"
			got.Riders[0].From = "owned changed"
			got.Stops[0] = "owned changed"
			if copied.Simulation.Vehicles[0].Boardings[0].BerthID == "owned changed" || copied.Simulation.Vehicles[0].Riders[0].From == "owned changed" || copied.Simulation.Vehicles[0].Stops[0] == "owned changed" {
				t.Fatal("copying stateFrame lost its ownership contract")
			}
		})
	}
}

func TestPresentationCaptureRemainsDetached(t *testing.T) {
	t.Parallel()
	shared := newFrameFixture(t, frameFixtures[1])
	t.Cleanup(shared.Close)
	first, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	before := string(mustJSON(t, first))
	for range 120 {
		shared.step()
	}
	if string(mustJSON(t, first)) != before {
		t.Fatal("stepping changed a retained frame")
	}
	second, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	first.State.Simulation.Vehicles[0].Riders[0].From = "caller changed"
	first.State.Simulation.Vehicles[0].Stops[0] = "caller changed"
	first.State.Checkpoints[0].ID = 999
	third, err := shared.presentationFrame()
	if err != nil || !reflect.DeepEqual(second, third) {
		t.Fatalf("caller mutation changed a future capture: %v", err)
	}
}
