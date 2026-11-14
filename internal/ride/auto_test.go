package ride

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestNextPod(t *testing.T) {
	aboard := []sim.Request{{ID: 1, From: "s", To: "t", PartySize: 1}}
	left := []sim.Request{{ID: 2, From: "s", To: "t", PartySize: 1, Completed: true}}
	at := func(id string, x float64, riders []sim.Request) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{ID: id, Position: sim.Point{X: x}}, Riders: riders}
	}
	tests := []struct {
		name     string
		current  string
		vehicles []sim.Vehicle
		pending  []sim.Request
		want     string
	}{
		{"nearest with riders", "01", []sim.Vehicle{at("01", 0, aboard), at("02", 50, aboard), at("03", -20, aboard), at("04", 5, nil)}, nil, "03"},
		{"tie goes to the lowest ID", "01", []sim.Vehicle{at("01", 0, nil), at("05", 10, aboard), at("03", -10, aboard)}, nil, "03"},
		{"riders that left do not count", "01", []sim.Vehicle{at("01", 0, nil), at("02", 5, left), at("03", 90, aboard)}, nil, "03"},
		{"riders come before a pickup", "01", []sim.Vehicle{at("01", 0, nil), at("02", 5, nil), at("03", 90, aboard)}, []sim.Request{{PodID: "02"}}, "03"},
		{"nearest pickup", "01", []sim.Vehicle{at("01", 0, nil), at("02", 50, nil), at("03", 20, nil), at("04", 1, nil)}, []sim.Request{{PodID: "02"}, {PodID: "03"}, {}}, "03"},
		{"the ridden pod is not a choice", "01", []sim.Vehicle{at("01", 0, aboard), at("02", 5, nil)}, []sim.Request{{PodID: "01"}}, ""},
		{"no pod with work", "01", []sim.Vehicle{at("01", 0, nil), at("02", 5, nil)}, nil, ""},
		{"unknown current pod", "99", []sim.Vehicle{at("01", 0, aboard)}, nil, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := pausedState(1)
			state.Simulation.Vehicles, state.Simulation.Pending = test.vehicles, test.pending
			if got := NextPod(state, test.current); got != test.want {
				t.Fatalf("NextPod = %q, want %q", got, test.want)
			}
		})
	}
}
