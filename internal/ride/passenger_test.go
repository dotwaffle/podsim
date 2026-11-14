package ride

import (
	"errors"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPassengerReply(t *testing.T) {
	requested := Request(pausedState(1), "s", "t")
	tests := []struct {
		name  string
		start Passenger
		reply session.Reply
		err   error
		want  Passenger
	}{
		{"accepted", requested, session.Reply{OrderID: 7, Revision: 9}, nil, Passenger{Phase: Requested, OrderID: 7, Origin: "s", Destination: "t", Station: "s", revision: 9}},
		{"rejected", requested, session.Reply{Error: "the queue is full"}, nil, Passenger{Phase: Stopped, Origin: "s", Destination: "t", Station: "s", Message: "the queue is full"}},
		{"not sent", requested, session.Reply{}, errors.New("no connection"), Passenger{Phase: Stopped, Origin: "s", Destination: "t", Station: "s", Message: "no connection"}},
		// A reply of another command does not change an accepted order.
		{"second reply", Passenger{Phase: Requested, OrderID: 7}, session.Reply{OrderID: 8}, nil, Passenger{Phase: Requested, OrderID: 7}},
		{"no trip", Passenger{}, session.Reply{OrderID: 8}, nil, Passenger{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.start.Reply(test.reply, test.err)
			test.want.session = got.session
			if got != test.want {
				t.Fatalf("Reply = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPassengerStep(t *testing.T) {
	order := func(mutate func(*sim.Request)) sim.Request {
		request := sim.Request{ID: 7, From: "s", To: "t", PartySize: 1}
		if mutate != nil {
			mutate(&request)
		}
		return request
	}
	pod := func(id string, riders ...sim.Request) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{ID: id}, Riders: riders}
	}
	other := sim.Request{ID: 3, From: "s", To: "t", BoardedTick: 5}
	tests := []struct {
		name     string
		phase    PassengerPhase
		revision uint64
		pending  []sim.Request
		vehicles []sim.Vehicle
		session  string
		want     Passenger
	}{
		{"pending with no pod", Requested, 1, []sim.Request{order(nil)}, nil, "", Passenger{Phase: Waiting, Station: "s"}},
		{"pending with a pickup pod", Waiting, 1, []sim.Request{order(func(r *sim.Request) { r.PodID = "p2" })}, nil, "", Passenger{Phase: Waiting, Station: "s", PodID: "p2"}},
		{"pending at a transfer", Riding, 1, []sim.Request{order(func(r *sim.Request) { r.LegFrom = "x" })}, []sim.Vehicle{pod("p1", order(func(r *sim.Request) { r.Completed = true }))}, "", Passenger{Phase: Waiting, Station: "x"}},
		{"assigned to a shared ride", Waiting, 1, nil, []sim.Vehicle{pod("p1", other, order(nil))}, "", Passenger{Phase: Waiting, Station: "s", PodID: "p1"}},
		{"assigned at a transfer", Waiting, 1, nil, []sim.Vehicle{pod("p3", order(func(r *sim.Request) { r.LegFrom = "x" }))}, "", Passenger{Phase: Waiting, Station: "x", PodID: "p3"}},
		{"aboard", Waiting, 1, nil, []sim.Vehicle{pod("p0"), pod("p1", other, order(func(r *sim.Request) { r.BoardedTick = 9 }))}, "", Passenger{Phase: Riding, Station: "s", PodID: "p1"}},
		{"aboard the second pod", Riding, 1, nil, []sim.Vehicle{
			pod("p1", order(func(r *sim.Request) { r.Completed, r.BoardedTick = true, 2 })),
			pod("p2", order(func(r *sim.Request) { r.LegFrom, r.BoardedTick = "x", 9 })),
		}, "", Passenger{Phase: Riding, Station: "s", PodID: "p2"}},
		{"arrived", Riding, 1, nil, []sim.Vehicle{pod("p1", order(func(r *sim.Request) { r.Completed, r.BoardedTick = true, 2 }))}, "", Passenger{Phase: Arrived, Station: "t", PodID: "p1"}},
		{"state before the reply", Requested, 2, nil, nil, "", Passenger{Phase: Requested, Station: "s"}},
		{"order gone", Waiting, 1, nil, []sim.Vehicle{pod("p1", other)}, "", Passenger{Phase: Stopped, Station: "s", Message: orderGoneMessage}},
		{"session changed", Riding, 1, []sim.Request{order(nil)}, nil, "e2", Passenger{Phase: Stopped, Station: "s", Message: sessionChangedMessage}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := pausedState(1)
			passenger := Request(state, "s", "t")
			passenger.Phase, passenger.OrderID, passenger.revision = test.phase, 7, test.revision
			if test.session != "" {
				state.Epoch = test.session
			}
			state.Simulation.Pending, state.Simulation.Vehicles = test.pending, test.vehicles
			got := passenger.Step(state)
			test.want.OrderID, test.want.Origin, test.want.Destination = 7, "s", "t"
			test.want.session, test.want.revision = passenger.session, passenger.revision
			if got != test.want {
				t.Fatalf("Step = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPassengerStepKeepsLastPhases(t *testing.T) {
	state := pausedState(1)
	state.Epoch = "other"
	for _, phase := range []PassengerPhase{NoTrip, Arrived, Stopped} {
		passenger := Passenger{Phase: phase, OrderID: 7}
		if got := passenger.Step(state); got != passenger {
			t.Fatalf("Step of %q = %+v", phase, got)
		}
	}
	// Before the reply, the flow has no order to find.
	requested := Request(pausedState(1), "s", "t")
	if got := requested.Step(pausedState(1)); got != requested {
		t.Fatalf("Step before the reply = %+v", got)
	}
}
