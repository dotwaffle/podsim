package ride

import (
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestHUD(t *testing.T) {
	network := testNetwork()
	east, south := network.Lanes[0], network.Lanes[1]
	pod := sim.Pod{ID: "01", Activity: sim.Traveling, LaneID: "east", LaneDistance: 40, Speed: 9, WaitReason: sim.TrackOccupied, BlockedBy: "02"}
	state := pausedState(1, pod)
	state.Speed = 15
	vehicle := &state.Simulation.Vehicles[0]
	vehicle.Riders = []sim.Request{
		{ID: 1, From: "s", To: "t", PartySize: 9, Completed: true},
		{ID: 2, From: "s", To: "x", PartySize: 3},
		{ID: 3, From: "y", To: "s", PartySize: 2},
	}
	// The window starts one lane before the pod lane, and the route goes on
	// after it.
	vehicle.Presentation = &sim.RoutePresentation{Start: 4, Current: 5, After: true, Motion: []sim.Lane{south, east, south}}

	var bridge Bridge
	bridge.Observe(state, time.Unix(0, 0))
	hud := bridge.HUD(state, "01", HUDSession{Connected: true, Message: "done"})
	if !hud.Found || hud.PodID != "01" || hud.Class != sim.LegacyClass || hud.Activity != sim.Traveling || hud.Speed != 9 || hud.SpeedLimit != 10 {
		t.Fatalf("pod fields = %+v", hud)
	}
	if hud.WaitReason != sim.TrackOccupied || hud.BlockedBy != "02" {
		t.Fatalf("wait = %q %q", hud.WaitReason, hud.BlockedBy)
	}
	wantRiders := []HUDRider{{PartySize: 3, From: "Stop", To: "x"}, {PartySize: 2, From: "y", To: "Stop"}}
	if len(hud.Riders) != len(wantRiders) || hud.Riders[0] != wantRiders[0] || hud.Riders[1] != wantRiders[1] {
		t.Fatalf("riders = %+v", hud.Riders)
	}
	// With no stops, the next stop is the destination of the first rider aboard.
	if hud.NextStop != "x" {
		t.Fatalf("next stop = %q", hud.NextStop)
	}
	// 60 m left on east at 10 m/s, then 100 m of south at 10 m/s.
	// 100 m of south before the pod lane and 40 m of east are behind the pod.
	if hud.Route == nil || !near(hud.Route.TraveledMeters, 140) || !near(hud.Route.RemainingMeters, 160) || !near(hud.Route.Seconds, 16) || hud.Route.Before || !hud.Route.After {
		t.Fatalf("route = %+v", hud.Route)
	}
	if hud.Session != (HUDSession{Speed: 15, Paused: true, Connected: true, Message: "done"}) {
		t.Fatalf("session = %+v", hud.Session)
	}

	// A stop comes before the destination of the first rider.
	vehicle.Stops = []string{"s"}
	if hud := bridge.HUD(state, "01", HUDSession{}); hud.NextStop != "Stop" {
		t.Fatalf("next stop with stops = %q", hud.NextStop)
	}
}

func TestHUDRoute(t *testing.T) {
	network := testNetwork()
	east, south := network.Lanes[0], network.Lanes[1]
	var bridge Bridge
	bridge.Observe(pausedState(1), time.Unix(0, 0))
	tests := []struct {
		name    string
		vehicle sim.Vehicle
		want    *HUDRoute
	}{
		{"no route", sim.Vehicle{}, nil},
		{"empty window", sim.Vehicle{Presentation: &sim.RoutePresentation{}}, nil},
		{"past the window", sim.Vehicle{Presentation: &sim.RoutePresentation{Current: 2, Motion: []sim.Lane{east, south}}}, nil},
		{"current before start", sim.Vehicle{Presentation: &sim.RoutePresentation{Start: 3, Current: 2, Motion: []sim.Lane{east, south}}}, nil},
		// A pod at a berth has no lane, so the first lane counts in full.
		{"at a berth", sim.Vehicle{Presentation: &sim.RoutePresentation{Motion: []sim.Lane{east, south}}}, &HUDRoute{RemainingMeters: 200, Seconds: 20}},
		{"lane distance past the end", sim.Vehicle{Pod: sim.Pod{LaneID: "east", LaneDistance: 150}, Presentation: &sim.RoutePresentation{Motion: []sim.Lane{east}}}, &HUDRoute{TraveledMeters: 100}},
		{"negative lane distance", sim.Vehicle{Pod: sim.Pod{LaneID: "east", LaneDistance: -5}, Presentation: &sim.RoutePresentation{Motion: []sim.Lane{east}}}, &HUDRoute{RemainingMeters: 100, Seconds: 10}},
		{"lane with no limit", sim.Vehicle{Presentation: &sim.RoutePresentation{Motion: []sim.Lane{{ID: "east"}}}}, &HUDRoute{RemainingMeters: 100}},
		{"window cut at both ends", sim.Vehicle{Pod: sim.Pod{LaneID: "south", LaneDistance: 30}, Presentation: &sim.RoutePresentation{Start: 7, Current: 8, Before: true, After: true, Motion: []sim.Lane{east, south, east}}}, &HUDRoute{TraveledMeters: 130, RemainingMeters: 170, Seconds: 17, Before: true, After: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := bridge.routeProgress(test.vehicle)
			if (got == nil) != (test.want == nil) || got != nil && (!near(got.TraveledMeters, test.want.TraveledMeters) || !near(got.RemainingMeters, test.want.RemainingMeters) || !near(got.Seconds, test.want.Seconds) || got.Before != test.want.Before || got.After != test.want.After) {
				t.Fatalf("route = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestHUDMissingPod(t *testing.T) {
	var bridge Bridge
	state := pausedState(1, sim.Pod{ID: "01"})
	state.Speed = 2
	hud := bridge.HUD(state, "99", HUDSession{Error: "lost"})
	if hud.Found || hud.PodID != "" || hud.Riders == nil || hud.Session != (HUDSession{Speed: 2, Paused: true, Error: "lost"}) {
		t.Fatalf("hud = %+v", hud)
	}
}

func TestLaneLength(t *testing.T) {
	table := newLaneTable(testNetwork())
	if got := table.length("east"); !near(got, 100) {
		t.Fatalf("east length = %v", got)
	}
	// The curve from (0,0) by (100,0) to (100,100) is longer than its chord
	// and shorter than its control polygon.
	if got := table.length("curve"); got <= 141.42 || got >= 200 {
		t.Fatalf("curve length = %v", got)
	}
	if got := table.length("none"); got != 0 {
		t.Fatalf("unknown lane length = %v", got)
	}
}
