package observe

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStationMonitorSummarize(t *testing.T) {
	t.Parallel()
	network := stationStatusNetwork()
	monitor := NewStationMonitor(network)
	station, _ := network.Station("passenger")
	parking, _ := network.Station("parking")
	state := sim.Snapshot{
		Berths: []sim.BerthState{
			{ID: "passenger-1", Occupant: "idle", ReservedBy: "arrival"},
			{ID: "passenger-2", ReservedBy: "second-arrival"},
			{ID: "parking-1", ReservedBy: "parking-arrival"},
		},
		Vehicles: []sim.Vehicle{
			stationStatusVehicle(stationStatusVehicleInput{id: "arrival", activity: sim.Traveling, laneID: "arrival-spine", wait: sim.BerthOccupied, route: passengerRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "approaching", activity: sim.Traveling, laneID: "road-in", route: passengerRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "access-stop", activity: sim.Traveling, laneID: "passenger-access", wait: sim.TrackOccupied, route: passengerRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "access-unblocked", activity: sim.Traveling, laneID: "passenger-access", route: passengerRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "road-stop", activity: sim.Traveling, laneID: "road-in", wait: sim.TrackOccupied, route: passengerRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "moving-in", activity: sim.Traveling, laneID: "arrival-spine", speed: 2, wait: sim.TrackOccupied, route: passengerRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "departure", activity: sim.Traveling, laneID: "departure-spine", wait: sim.TrackOccupied, route: passengerDepartureRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "exit-access", activity: sim.Traveling, laneID: "passenger-exit-access", wait: sim.TrackOccupied, route: passengerDepartureRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "exit-road", activity: sim.Traveling, laneID: "road-out", wait: sim.TrackOccupied, route: passengerDepartureRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "parking-arrival", activity: sim.Traveling, laneID: "parking-in", wait: sim.TrackOccupied, route: parkingRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "parking-departure", activity: sim.Traveling, laneID: "parking-out", wait: sim.JunctionOccupied, route: parkingDepartureRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "boarding", activity: sim.Boarding, laneID: "passenger-out", wait: sim.TrackOccupied, route: passengerDepartureRoute()}),
			stationStatusVehicle(stationStatusVehicleInput{id: "idle", activity: sim.Idle, laneID: "passenger-in", wait: sim.BerthOccupied, route: passengerRoute()}),
		},
	}

	tests := []struct {
		name    string
		station sim.Station
		want    StationMetrics
	}{
		{name: "passenger station", station: station, want: StationMetrics{Occupied: 1, ReservedEmpty: 1, TotalReserved: 2, Free: 1, Approaching: 6, EntranceStopped: 2, ExitStopped: 2}},
		{name: "parking station", station: parking, want: StationMetrics{ReservedEmpty: 1, TotalReserved: 1, Free: 1, Approaching: 1, EntranceStopped: 1, ExitStopped: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := monitor.Summarize(test.station, state); got != test.want {
				t.Fatalf("Summarize() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func stationStatusNetwork() sim.Network {
	return sim.Network{
		Lanes: stationStatusLanes(),
		Stations: []sim.Station{
			{ID: "passenger", Entry: "passenger-entry", Exit: "passenger-exit", Berths: []sim.Berth{{ID: "passenger-1", Node: "passenger-berth"}, {ID: "passenger-2", Node: "passenger-berth-2"}, {ID: "passenger-3", Node: "passenger-berth-3"}}},
			{ID: "parking", Entry: "parking-entry", Exit: "parking-exit", Berths: []sim.Berth{{ID: "parking-1", Node: "parking-berth"}, {ID: "parking-2", Node: "parking-berth-2"}}, ParkingOnly: true},
		},
	}
}

func stationStatusLanes() []sim.Lane {
	lanes := append(passengerRoute(), passengerDepartureRoute()...)
	lanes = append(lanes, parkingRoute()...)
	lanes = append(lanes, parkingDepartureRoute()...)
	return append(lanes,
		sim.Lane{ID: "other-out", From: "diverge", To: "other"},
		sim.Lane{ID: "other-in", From: "other", To: "merge"},
	)
}

type stationStatusVehicleInput struct {
	id       string
	activity sim.Activity
	laneID   string
	speed    float64
	wait     sim.WaitReason
	route    []sim.Lane
}

func stationStatusVehicle(input stationStatusVehicleInput) sim.Vehicle {
	return sim.Vehicle{Pod: sim.Pod{ID: input.id, Activity: input.activity, LaneID: input.laneID, Speed: input.speed, WaitReason: input.wait}, Route: input.route}
}

func passengerRoute() []sim.Lane {
	return []sim.Lane{
		{ID: "road-in", From: "road", To: "diverge"},
		{ID: "passenger-access", From: "diverge", To: "passenger-entry"},
		{ID: "passenger-in", From: "passenger-entry", To: "arrival-junction"},
		{ID: "arrival-spine", From: "arrival-junction", To: "passenger-berth"},
	}
}

func passengerDepartureRoute() []sim.Lane {
	return []sim.Lane{
		{ID: "passenger-out", From: "passenger-berth", To: "departure-junction"},
		{ID: "departure-spine", From: "departure-junction", To: "passenger-exit"},
		{ID: "passenger-exit-access", From: "passenger-exit", To: "merge"},
		{ID: "road-out", From: "merge", To: "road"},
	}
}

func parkingRoute() []sim.Lane {
	return []sim.Lane{{ID: "parking-in", From: "parking-entry", To: "parking-berth"}}
}

func parkingDepartureRoute() []sim.Lane {
	return []sim.Lane{{ID: "parking-out", From: "parking-berth", To: "parking-exit"}}
}
