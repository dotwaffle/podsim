package view

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// stationPhases are all the station phases that a pod can show.
var stationPhases = []sim.StationPhase{
	sim.ApproachingStation, sim.EnteringStation, sim.AccessingBerth, sim.PassingStation,
	sim.AtBerth, sim.DepartingBerth, sim.ExitingStation,
}

func TestStationPhaseRows(t *testing.T) {
	t.Parallel()
	network := sim.Example()
	for _, test := range []struct {
		name string
		pod  sim.Pod
		want []inspectionRow
	}{
		{name: "main network", want: []inspectionRow{{"Station phase", "Main network"}}},
		{name: "known station", pod: sim.Pod{StationPhase: sim.AccessingBerth, ManeuverStationID: "market"}, want: []inspectionRow{{"Station phase", "Accessing berth"}, {"", "Market"}}},
		{name: "missing station", pod: sim.Pod{StationPhase: sim.ExitingStation, ManeuverStationID: "missing"}, want: []inspectionRow{{"Station phase", "Exiting station"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := stationPhaseRows(test.pod, network); !slices.Equal(got, test.want) {
				t.Fatalf("rows = %q, want %q", got, test.want)
			}
		})
	}
}

// TestWaitStatus checks that the inspector status of a waiting pod names the
// blocking pod by its fleet number and not by its pod ID.
func TestWaitStatus(t *testing.T) {
	t.Parallel()
	vehicles := []sim.Vehicle{
		{Pod: sim.Pod{ID: "london-pod-001"}},
		{Pod: sim.Pod{ID: "london-pod-008"}},
		{Pod: sim.Pod{ID: "london-pod-003"}},
	}
	tests := []struct {
		name string
		pod  sim.Pod
		want string
	}{
		{name: "pod ahead", pod: sim.Pod{WaitReason: sim.TrackOccupied, BlockedBy: "london-pod-008"}, want: "Pod ahead / pod 02"},
		{name: "junction traffic", pod: sim.Pod{WaitReason: sim.JunctionOccupied, BlockedBy: "london-pod-001"}, want: "Junction traffic / pod 01"},
		{name: "berth occupied", pod: sim.Pod{WaitReason: sim.BerthOccupied, BlockedBy: "london-pod-003"}, want: "Berth occupied / pod 03"},
		{name: "no parking available", pod: sim.Pod{WaitReason: sim.ParkingUnavailable, BlockedBy: "london-pod-003"}, want: "No parking available / pod 03"},
		{name: "unknown pod", pod: sim.Pod{WaitReason: sim.TrackOccupied, BlockedBy: "other"}, want: "Pod ahead / pod other"},
		{name: "no blocking pod", pod: sim.Pod{WaitReason: sim.JunctionOccupied}, want: "Junction traffic"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := waitStatus(test.pod, vehicles); got != test.want {
				t.Fatalf("waitStatus(%+v) = %q, want %q", test.pod, got, test.want)
			}
		})
	}
}

// TestInspectionRows checks the rows of the pod inspector. The station
// phase rows are last, so the other rows keep their position when the
// station name row shows.
func TestInspectionRows(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	tests := []struct {
		name    string
		vehicle sim.Vehicle
		want    []inspectionRow
	}{
		{
			name:    "empty on the main network",
			vehicle: sim.Vehicle{Pod: sim.Pod{Speed: 10}},
			want:    []inspectionRow{{"Speed", "36 km/h"}, {"On board", "Empty"}, {"Station phase", "Main network"}},
		},
		{
			name:    "one party at a berth",
			vehicle: sim.Vehicle{Pod: sim.Pod{Occupied: true, StationPhase: sim.AtBerth, ManeuverStationID: "harbor"}, Request: &sim.Request{PartySize: 1}, Parties: 1},
			want:    []inspectionRow{{"Speed", "0 km/h"}, {"On board", "1 passenger"}, {"Station phase", "At berth"}, {"", "Harbor"}},
		},
		{
			name:    "shared ride leaves a station",
			vehicle: sim.Vehicle{Pod: sim.Pod{Occupied: true, Speed: 5, StationPhase: sim.ExitingStation, ManeuverStationID: "garden"}, Request: &sim.Request{PartySize: 3}, Parties: 3},
			want:    []inspectionRow{{"Speed", "18 km/h"}, {"On board", "3 passengers"}, {"Station phase", "Exiting station"}, {"", "Garden"}},
		},
		{
			name:    "passenger count comes from the request",
			vehicle: sim.Vehicle{Pod: sim.Pod{Occupied: true}, Request: &sim.Request{PartySize: 2}},
			want:    []inspectionRow{{"Speed", "0 km/h"}, {"On board", "2 passengers"}, {"Station phase", "Main network"}},
		},
		{
			name:    "occupied pod without a request",
			vehicle: sim.Vehicle{Pod: sim.Pod{Occupied: true}},
			want:    []inspectionRow{{"Speed", "0 km/h"}, {"On board", "Empty"}, {"Station phase", "Main network"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := game.inspectionRows(test.vehicle); !slices.Equal(got, test.want) {
				t.Errorf("rows = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPassengerCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		count int
		want  string
	}{
		{count: 1, want: "1 passenger"},
		{count: 2, want: "2 passengers"},
		{count: sim.MaxSharedRideParties, want: "8 passengers"},
	}
	for _, test := range tests {
		if got := passengerCount(test.count); got != test.want {
			t.Errorf("passengerCount(%d) = %q, want %q", test.count, got, test.want)
		}
	}
}

// TestFitInspectionValueCutsLongValues checks that the inspector cuts a
// value that is too long. The cut value must end before the right edge of
// the inspector. A row with a name starts its value in the value column. A
// row without a name starts its value at the left edge of the inspector.
func TestFitInspectionValueCutsLongValues(t *testing.T) {
	t.Parallel()
	rows := map[string]inspectionRow{
		"named row":        {"Station phase", "Approaching station / Tottenham Court Road"},
		"station name row": {value: "Heathrow Terminal 5 and King's Cross St Pancras Interchange"},
	}
	for _, layout := range controlLayouts {
		for name, row := range rows {
			t.Run(layout.name+" "+name, func(t *testing.T) {
				t.Parallel()
				game := controlTestGame(t, layout.input)
				got := game.fitInspectionValue(row)
				if got == row.value {
					t.Fatalf("long value %q was not cut", row.value)
				}
				width, _ := text.Measure(got, game.textFace(14), 0)
				if limit := game.layout.x(inspectionRight - row.valueLeft()); width > limit {
					t.Fatalf("cut value %q width %g exceeds %g", got, width, limit)
				}
			})
		}
	}
}

// TestInspectionRowsFitLondonNames checks that the inspector shows each
// London station name, each station phase, and each passenger count in
// full, and that each row name ends before its value. The layouts include
// the minimum window size and smaller windows. Small text is wider in
// proportion to the unit, so "Approaching station" needs more than 130
// units in a window of 1366x617.
func TestInspectionRowsFitLondonNames(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	var vehicles []sim.Vehicle
	for _, station := range network.Stations {
		for _, phase := range stationPhases {
			vehicles = append(vehicles, sim.Vehicle{Pod: sim.Pod{StationPhase: phase, ManeuverStationID: station.ID}})
		}
	}
	for count := range sim.MaxSharedRideParties {
		vehicles = append(vehicles, sim.Vehicle{Pod: sim.Pod{Occupied: true}, Request: &sim.Request{PartySize: count + 1}, Parties: count + 1})
	}
	inputs := map[string]layoutInput{"laptop 1366x617": {outsideWidth: 1366, outsideHeight: 617, deviceScale: 1}}
	for _, layout := range controlLayouts {
		inputs[layout.name] = layout.input
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, input)
			game.network = network
			for _, vehicle := range vehicles {
				for _, row := range game.inspectionRows(vehicle) {
					if got := game.fitInspectionValue(row); got != row.value {
						t.Errorf("row %q shows %q, want %q", row.name, got, row.value)
					}
					if row.name == "" {
						continue
					}
					nameWidth, _ := text.Measure(row.name, game.textFace(14), 0)
					if right, valueLeft := game.layout.x(inspectionLeft)+nameWidth, game.layout.x(row.valueLeft()); right >= valueLeft {
						t.Errorf("row name %q ends at %g, value starts at %g", row.name, right, valueLeft)
					}
				}
			}
		})
	}
}
