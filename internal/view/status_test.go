package view

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPodPurpose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		vehicle sim.Vehicle
		pending []sim.Request
		want    podPurpose
	}{
		{name: "idle", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Idle}}, want: purposeIdle},
		{name: "old completed trip", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Idle}, Request: &sim.Request{Completed: true}}, want: purposeIdle},
		{name: "pickup pending", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}, RelocatingTo: "garden"}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "pickup blocked", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling, WaitReason: sim.JunctionOccupied}, RelocatingTo: "garden"}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "assigned at station", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Idle}}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "passenger before next pickup", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling, Occupied: true}}, pending: []sim.Request{{PodID: "01"}}, want: purposePassengers},
		{name: "boarding before occupancy", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Boarding}}, want: purposePassengers},
		{name: "unloading", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Unloading, Occupied: true}}, want: purposePassengers},
		{name: "parking with previous trip", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.DepartingEmpty}, RelocatingTo: "parking", Request: &sim.Request{Completed: true}}, want: purposeParking},
		{name: "redistribution", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}, RelocatingTo: "garden", Rebalancing: true}, want: purposeRedistribution},
		{name: "diverted pickup wins", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}, RelocatingTo: "parking", Rebalancing: true}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "unassigned empty", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}}, pending: []sim.Request{{PodID: "02"}}, want: purposeEmpty},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{network: sim.Example()}
			game.state.Simulation = sim.Snapshot{Vehicles: []sim.Vehicle{test.vehicle}, Pending: test.pending}
			got := game.podPurpose(test.vehicle, game.state.Simulation)
			if got != test.want {
				t.Fatalf("purpose %s, want %s", got.label(), test.want.label())
			}
			if color := game.podButtonColor(test.vehicle.Pod.ID); color != got.color() {
				t.Fatalf("button color %x differs from purpose color %x", color, got.color())
			}
		})
	}
}

func TestPodPurposeFollowsActualPickup(t *testing.T) {
	t.Parallel()
	network := sim.Example()
	simulation, err := sim.NewFleet(network, []sim.Placement{{ID: "01", StationID: "parking"}})
	if err != nil {
		t.Fatal(err)
	}
	game := &Game{network: network}
	if err := simulation.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	state := simulation.Snapshot()
	if got := game.podPurpose(state.Vehicles[0], state); got != purposePickup {
		t.Fatalf("dispatched pod is %s", got.label())
	}
	sawPassengers := false
	for range 5 * 60 * sim.TicksPerSecond {
		simulation.Step()
		state = simulation.Snapshot()
		purpose := game.podPurpose(state.Vehicles[0], state)
		sawPassengers = sawPassengers || purpose == purposePassengers
		if state.Completed == 1 {
			if !sawPassengers || purpose != purposeIdle {
				t.Fatalf("completed trip: passenger phase %t, final purpose %s", sawPassengers, purpose.label())
			}
			return
		}
	}
	t.Fatal("pickup journey did not complete")
}

func TestPurposeColorsRemainDistinct(t *testing.T) {
	t.Parallel()
	colors := make(map[uint32]podPurpose)
	for _, purpose := range []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty} {
		if previous, ok := colors[purpose.color()]; ok {
			t.Fatalf("%s and %s share a color", previous.label(), purpose.label())
		}
		colors[purpose.color()] = purpose
	}
}
