package main

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestScreenMeasuresSample(t *testing.T) {
	t.Parallel()
	pod := func(id, blockedBy string, reason sim.WaitReason) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{ID: id, BlockedBy: blockedBy, WaitReason: reason}}
	}
	input := screenSample{arrivalTicks: 60, ticks: 30, classes: map[string]sim.VehicleClass{"c1": sim.CompactClass, "c2": sim.CompactClass, "e1": sim.ExpressClass}}
	state := sim.Snapshot{Tick: 60, EmptyDistanceMeters: 10, Pending: make([]sim.Request, 3), Vehicles: []sim.Vehicle{
		pod("c1", "e1", sim.JunctionOccupied), pod("c2", "c1", sim.TrackOccupied), pod("e1", "e1", sim.TrackOccupied),
	}}
	m := screenMeasures{}
	m.sample(state, input)
	// A report that names no pod, and a stale holder with no wait report,
	// count for neither class.
	m.sample(sim.Snapshot{Tick: 90, EmptyDistanceMeters: 25, Pending: make([]sim.Request, 1), Vehicles: []sim.Vehicle{
		pod("c1", "e1", sim.NoWait), pod("c2", "a berth", sim.BerthOccupied),
	}}, input)
	state.Tick = 120
	m.sample(state, input)
	want := screenMeasures{ArrivalBlockedByExpressSeconds: 1, ArrivalBlockedByCompactSeconds: 1, TotalBlockedByExpressSeconds: 2, TotalBlockedByCompactSeconds: 2,
		ArrivalEmptyMeters: 10, TotalEmptyMeters: 10, QueuePeak: 3, ArrivalQueue: 3}
	if m != want {
		t.Fatalf("got %+v, want %+v", m, want)
	}
}

func TestScreenMeasuresFinish(t *testing.T) {
	t.Parallel()
	events := []screenEvent{
		{Tick: 0, Size: 1},
		{Tick: 0, Size: 1},
		{Tick: 100, Size: 10, Hub: true, Outbound: true},
		{Tick: 100, Size: 7, Hub: true, Outbound: true},
		{Tick: 0, Size: 4, Hub: true},
	}
	outcomes := []screenOutcome{
		{WaitTicks: 30, JourneyTicks: 60}, {WaitTicks: -1, JourneyTicks: -1},
		{WaitTicks: 60, JourneyTicks: 200}, {WaitTicks: 90, JourneyTicks: 201}, {WaitTicks: 30, JourneyTicks: -1},
	}
	m := screenMeasures{}
	m.finish(outcomes, events, 300)
	if m.BackgroundWait.Count != 1 || m.BackgroundWait.Mean != 1 {
		t.Errorf("background %+v", m.BackgroundWait)
	}
	want := screenHubMeasure{Offered: 17, ArrivalPassengers: 10, ArrivalParties: 1, TotalPassengers: 17, TotalParties: 2,
		Wait: screenWaitMeasure{Count: 2, Mean: 2.5, Median: 2, P95: 3, Longest: 3}}
	if m.Outbound != want {
		t.Errorf("outbound %+v, want %+v", m.Outbound, want)
	}
	if m.Inbound.Offered != 4 || m.Inbound.TotalParties != 0 || m.Inbound.Wait.Count != 1 {
		t.Errorf("inbound %+v", m.Inbound)
	}
}

func TestScreenMatched(t *testing.T) {
	t.Parallel()
	events := []screenEvent{{}, {}, {}, {}, {Hub: true}}
	arm := func(waits ...int64) screenArm {
		a := screenArm{}
		for _, wait := range waits {
			a.Outcomes = append(a.Outcomes, screenOutcome{WaitTicks: wait})
		}
		return a
	}
	got := newScreenMatched(arm(901, 0, 900, 5, 9000), arm(0, 901, 0, -1, 0), events)
	want := screenMatched{Count: 3, MeanChange: 10, MedianChange: 30, P95Change: 901.0 / 30, LargestChange: 901.0 / 30, Longer: 1, Shorter: 1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
