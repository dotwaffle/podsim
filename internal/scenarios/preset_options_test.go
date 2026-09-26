package scenarios

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestPresetWithNoChangeIsPreset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config func() project.Config
		// change gives the preset values again.
		change func(*Parameters)
	}{
		{name: "small", config: Small, change: func(*Parameters) {}},
		{name: "busy", config: Busy, change: func(parameters *Parameters) { parameters.BerthPitch = berthSpacing }},
		{name: "parking-constrained", config: ParkingConstrained, change: func(parameters *Parameters) {
			parameters.StationBerths = map[string]int{"station-01": 4, "parking": 1}
		}},
		{name: "rail-hub", config: RailHub, change: func(*Parameters) {}},
		{name: "scale100", config: Scale100, change: func(parameters *Parameters) { parameters.BerthPitch = meshBerthPitch }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, err := PresetWith(test.name, test.change)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(config, test.config()) {
				t.Fatal("PresetWith with the preset values is not the preset")
			}
		})
	}
}

func TestPresetWithCapacity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, preset string
		change       func(*Parameters)
		// berths holds the berth count of some stations.
		berths map[string]int
		// pods holds the pod count of some stations.
		pods map[string]int
		// err is a part of the error text, or empty for no error.
		err string
	}{
		{
			// A 7th berth row at the 75-meter pitch puts the departure chain
			// of Station 19 within the clearance of the arrival chain of
			// Station 13.
			name: "Station 19 with 7 berths", preset: "scale100",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"station-19": 7} },
			err:    `Station 19 lane "s19-departure-link-07"`,
		},
		{
			// 68 meters is the largest pitch, in steps of 0.5 meters, that
			// passes the audit with 8 berths at Station 19.
			name: "Station 19 with 8 berths at 68 meters", preset: "scale100",
			change: func(parameters *Parameters) {
				parameters.StationBerths, parameters.BerthPitch = map[string]int{"station-19": 8}, 68
			},
			berths: map[string]int{"station-19": 8, "station-18": 6, "parking": 24},
		},
		{
			name: "Station 19 with 8 berths at 68.5 meters", preset: "scale100",
			change: func(parameters *Parameters) {
				parameters.StationBerths, parameters.BerthPitch = map[string]int{"station-19": 8}, 68.5
			},
			err: `Station 19 lane "s19-departure-link-08"`,
		},
		{
			// The curves of these lanes are 12.3 meters apart as 12 parts,
			// but 7.5 meters apart on the 64-part paths of the pods.
			name: "curves nearer on the pod paths", preset: "busy",
			change: func(parameters *Parameters) {
				parameters.StationBerths = map[string]int{"station-01": 30, "station-02": 30}
				parameters.BerthPitch = 69
			},
			err: `layout conflict at station Station 01 (station-01): lane "s01-out-30" is nearer than 12 meters to Station 02 lane "s02-in-30"`,
		},
		{
			name: "rail hub with 8 berths", preset: "rail-hub",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"station-01": 8} },
			berths: map[string]int{"station-01": 8, "station-02": 6, "parking": 12},
			pods:   map[string]int{"station-01": 3, "parking": 12},
		},
		{
			// Pods go round robin, and a full station gets no more pods.
			name: "small with one berth at Station 02", preset: "small",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"station-02": 1} },
			berths: map[string]int{"station-01": 4, "station-02": 1},
			pods:   map[string]int{"station-01": 4, "station-02": 1, "station-03": 4, "station-04": 3},
		},
		{
			name: "busy with more berths", preset: "busy",
			change: func(parameters *Parameters) { parameters.PassengerBerths, parameters.ParkingBerths = 8, 16 },
			berths: map[string]int{"station-07": 8, "parking": 16},
		},
		{
			name: "parking-constrained at the pitch floor", preset: "parking-constrained",
			change: func(parameters *Parameters) { parameters.BerthPitch = minimumBerthPitch },
			berths: map[string]int{"station-01": 4, "parking": 1},
		},
		{
			name: "unknown station", preset: "small",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"station-05": 2} },
			err:    `unknown station ID "station-05"`,
		},
		{
			name: "no berth", preset: "small",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"parking": 0} },
			err:    "station parking has 0 berths, want 1 to 62",
		},
		{
			name: "too many berths", preset: "scale100",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"station-01": 63} },
			err:    "station station-01 has 63 berths, want 1 to 62",
		},
		{
			name: "pods past the berths", preset: "small",
			change: func(parameters *Parameters) { parameters.PassengerBerths = 2 },
			err:    "initial pods exceed berth capacity",
		},
		{
			name: "Parking pods past the berths", preset: "rail-hub",
			change: func(parameters *Parameters) { parameters.StationBerths = map[string]int{"parking": 11} },
			err:    "initial pods exceed berth capacity",
		},
		{
			name: "pitch below the floor", preset: "busy",
			change: func(parameters *Parameters) { parameters.BerthPitch = 24.5 },
			err:    "berth pitch must be a finite distance of at least 25 meters",
		},
		{
			name: "mesh with 21 stations", preset: "scale100",
			change: func(parameters *Parameters) { parameters.Stations = 21 },
			err:    "the mesh has 20 stations, not 21",
		},
		{
			name: "network past the node limit", preset: "small",
			change: func(parameters *Parameters) { parameters.Stations, parameters.PassengerBerths = 200, 20 },
			err:    "more than the limits of 4000 and 8000",
		},
		{
			name: "unknown preset", preset: "london",
			change: func(*Parameters) {},
			err:    `unknown ring or mesh preset "london"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config, err := PresetWith(test.preset, test.change)
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("PresetWith error = %v, want an error with %q", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := project.Validate(config); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(config.Name, " (custom capacity)") {
				t.Fatalf("project name %q has no custom capacity suffix", config.Name)
			}
			for id, want := range test.berths {
				station, _ := config.Network.Station(id)
				if len(station.Berths) != want {
					t.Errorf("station %s has %d berths, want %d", id, len(station.Berths), want)
				}
			}
			count := make(map[string]int)
			for _, placement := range config.Fleet {
				count[placement.StationID]++
			}
			for id, want := range test.pods {
				if count[id] != want {
					t.Errorf("station %s has %d pods, want %d", id, count[id], want)
				}
			}
		})
	}
}
