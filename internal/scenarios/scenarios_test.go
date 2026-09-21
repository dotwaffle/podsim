package scenarios

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPresetsValidateAndRemainStable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		config   func() project.Config
		stations int
		pods     int
	}{
		{name: "small", config: Small, stations: 5, pods: 12},
		{name: "busy", config: Busy, stations: 8, pods: 32},
		{name: "parking constrained", config: ParkingConstrained, stations: 6, pods: 20},
		{name: "scale 100", config: Scale100, stations: 20, pods: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			first := test.config()
			second := test.config()
			if err := project.Validate(first); err != nil {
				t.Fatal(err)
			}
			if len(first.Network.Stations) != test.stations || len(first.Fleet) != test.pods {
				t.Fatalf("got %d stations and %d pods", len(first.Network.Stations), len(first.Fleet))
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatal("preset changed between calls")
			}
			firstJSON, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			secondJSON, err := json.Marshal(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(firstJSON, secondJSON) {
				t.Fatal("preset JSON is not repeatable")
			}
			for _, lane := range first.Network.Lanes {
				if length := first.Network.Length(lane); length < 2*sim.Clearance {
					t.Fatalf("lane %q is only %.2f meters", lane.ID, length)
				}
			}
		})
	}
}

func TestConfigRejectsInvalidCapacity(t *testing.T) {
	t.Parallel()
	tests := []Parameters{
		{Name: "too few stations", Stations: 2, Pods: 1, PassengerBerths: 1, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "too many stations", Stations: 101, Pods: 1, PassengerBerths: 1, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "no passenger berths", Stations: 3, Pods: 1, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "too many passenger berths", Stations: 3, Pods: 1, PassengerBerths: 201, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "too many parking berths", Stations: 3, Pods: 1, PassengerBerths: 1, ParkingBerths: 201, DemandPerMinute: 1},
		{Name: "no pods", Stations: 3, PassengerBerths: 1, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "too many pods", Stations: 3, Pods: 201, PassengerBerths: 200, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "passenger overflow", Stations: 3, Pods: 4, PassengerBerths: 1, ParkingBerths: 1, DemandPerMinute: 1},
		{Name: "parking overflow", Stations: 3, Pods: 2, PassengerBerths: 1, ParkingBerths: 1, InitialParkingPods: 2, DemandPerMinute: 1},
		{Name: "network resource overflow", Stations: 100, Pods: 1, PassengerBerths: 20, ParkingBerths: 200, DemandPerMinute: 1},
		{Name: "invalid demand", Stations: 3, Pods: 1, PassengerBerths: 1, ParkingBerths: 1},
	}
	for _, parameters := range tests {
		if _, err := Config(parameters); err == nil {
			t.Fatalf("accepted invalid parameters: %+v", parameters)
		}
	}
}

func TestScale100Capacity(t *testing.T) {
	t.Parallel()
	config := Scale100()
	parkingBerths, freeBerths := 0, 0
	occupied := make(map[string]bool, len(config.Fleet))
	for _, placement := range config.Fleet {
		occupied[placement.BerthID] = true
	}
	for _, station := range config.Network.Stations {
		if station.ParkingOnly {
			parkingBerths += len(station.Berths)
		}
		for _, berth := range station.Berths {
			if !occupied[berth.ID] {
				freeBerths++
			}
		}
	}
	if parkingBerths != 24 || freeBerths != 38 {
		t.Fatalf("got %d parking berths and %d free berths", parkingBerths, freeBerths)
	}
}
