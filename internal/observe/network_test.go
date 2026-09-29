package observe

import (
	"compress/gzip"
	"encoding/json"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func checkNetworkSummaries(tb testing.TB, network sim.Network, state sim.Snapshot) {
	tb.Helper()
	batch := NewNetworkMonitor(network).Summarize(state)
	single := NewStationMonitor(network)
	if len(batch) != len(network.Stations) {
		tb.Fatalf("summary count = %d, want %d", len(batch), len(network.Stations))
	}
	for index, station := range network.Stations {
		if want := single.Summarize(station, state); batch[index] != want {
			tb.Fatalf("station %d (%s): batch %+v, single %+v", index, station.ID, batch[index], want)
		}
	}
}

func TestNetworkMonitorEmptyInputs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		network sim.Network
		state   sim.Snapshot
	}{
		{name: "nil network and snapshot"},
		{name: "empty slices", network: sim.Network{Stations: []sim.Station{}}, state: sim.Snapshot{Vehicles: []sim.Vehicle{}, Berths: []sim.BerthState{}}},
		{name: "empty snapshot", network: stationStatusNetwork()},
		{name: "unknown vehicles and berths", state: sim.Snapshot{Berths: []sim.BerthState{{ID: "unknown"}}, Vehicles: []sim.Vehicle{{Pod: sim.Pod{Activity: sim.Traveling, WaitReason: sim.TrackOccupied, LaneID: "unknown"}, Route: []sim.Lane{{ID: "unknown"}}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkNetworkSummaries(t, test.network, test.state)
		})
	}
	if got := (NetworkMonitor{}).Summarize(sim.Snapshot{}); len(got) != 0 {
		t.Fatalf("zero monitor returned %d summaries", len(got))
	}
}

func TestNetworkMonitorGeneratedEquivalence(t *testing.T) {
	t.Parallel()
	network := stationStatusNetwork()
	network.Stations = append(network.Stations,
		sim.Station{ID: "shared", Entry: "passenger-entry", Exit: "parking-exit", Berths: []sim.Berth{
			{ID: "passenger-1", Node: "passenger-berth"},
			{ID: "passenger-1", Node: "passenger-berth"},
			{ID: "parking-1", Node: "parking-berth"},
		}}, sim.Station{ID: "empty"},
	)
	activities := []sim.Activity{sim.Traveling, sim.DepartingEmpty, sim.Boarding, sim.Idle}
	waits := []sim.WaitReason{sim.NoWait, sim.TrackOccupied, sim.JunctionOccupied}
	speeds := []float64{-1, 0, 0.009, 0.01, 1}
	for sample := range 500 {
		t.Run(strconv.Itoa(sample), func(t *testing.T) {
			t.Parallel()
			random := rand.New(rand.NewPCG(17, uint64(sample)+29))
			state := sim.Snapshot{}
			for range 12 {
				station := network.Stations[random.IntN(3)]
				berth := station.Berths[random.IntN(len(station.Berths))]
				state.Berths = append(state.Berths, sim.BerthState{ID: berth.ID,
					Occupant: []string{"", "pod"}[random.IntN(2)], ReservedBy: []string{"", "pod"}[random.IntN(2)]})
			}
			state.Berths = append(state.Berths, sim.BerthState{ID: "unknown", Occupant: "pod", ReservedBy: "pod"})
			for range 20 {
				vehicle := sim.Vehicle{Pod: sim.Pod{Activity: activities[random.IntN(len(activities))],
					WaitReason: waits[random.IntN(len(waits))], Speed: speeds[random.IntN(len(speeds))]}}
				for range random.IntN(30) {
					vehicle.Route = append(vehicle.Route, network.Lanes[random.IntN(len(network.Lanes))])
				}
				if len(vehicle.Route) > 0 && random.IntN(5) != 0 {
					vehicle.Pod.LaneID = vehicle.Route[random.IntN(len(vehicle.Route))].ID
				}
				state.Vehicles = append(state.Vehicles, vehicle)
			}
			checkNetworkSummaries(t, network, state)
		})
	}
}

func TestNetworkMonitorRepeatedAndTruncatedRoutes(t *testing.T) {
	t.Parallel()
	network := stationStatusNetwork()
	departure := passengerDepartureRoute()
	repeated := append(slices.Clone(departure), departure...)
	interior := append(parkingRoute(), repeated...)
	for _, route := range [][]sim.Lane{nil, {}, departure, departure[1:], repeated, interior, interior[:len(interior)-1]} {
		for _, lane := range departure {
			state := sim.Snapshot{Vehicles: []sim.Vehicle{
				{Pod: sim.Pod{Activity: sim.Traveling, WaitReason: sim.TrackOccupied, LaneID: lane.ID}, Route: route},
				{Pod: sim.Pod{Activity: sim.DepartingEmpty, WaitReason: sim.TrackOccupied, LaneID: lane.ID}, Route: route},
			}}
			checkNetworkSummaries(t, network, state)
		}
	}
}

func TestNetworkMonitorOwnsIndexes(t *testing.T) {
	t.Parallel()
	network := stationStatusNetwork()
	state := sim.Snapshot{Berths: []sim.BerthState{{ID: "passenger-1", Occupant: "pod"}}, Vehicles: []sim.Vehicle{{
		Pod: sim.Pod{Activity: sim.Traveling, WaitReason: sim.TrackOccupied, LaneID: "departure-spine"}, Route: passengerDepartureRoute(),
	}}}
	monitor := NewNetworkMonitor(network)
	want := monitor.Summarize(state)
	network.Stations[0].Berths[0].ID, network.Stations[0].Berths[0].Node = "changed", "changed"
	network.Stations[0].Exit = "changed"
	network.Lanes[0].From, network.Lanes[0].To = "changed", "changed"
	for index := range 8 {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()
			got := monitor.Summarize(state)
			if !slices.Equal(got, want) {
				t.Fatalf("input mutation changed summaries: %+v, want %+v", got, want)
			}
			got[0] = StationMetrics{}
			if !slices.Equal(monitor.Summarize(state), want) {
				t.Fatal("returned counters share storage")
			}
		})
	}
}

type summaryVehicle struct {
	Activity sim.Activity   `json:"Activity"`
	Speed    float64        `json:"Speed"`
	Wait     sim.WaitReason `json:"Wait"`
	Lane     string         `json:"Lane"`
	Route    []string       `json:"Route"`
}

type summaryFixture struct {
	Vehicles []summaryVehicle `json:"Vehicles"`
	Berths   []sim.BerthState `json:"Berths"`
}

func loadSummaryFixture(tb testing.TB, name string) (sim.Network, sim.Snapshot) {
	tb.Helper()
	var network sim.Network
	switch name {
	case "central":
		network = scenarios.LondonCentral().Network
	case "full":
		network = scenarios.LondonFull().Network
	default:
		tb.Fatalf("unknown fixture %q", name)
	}
	file, err := os.Open("testdata/" + name + "-station-summary.json.gz")
	if err != nil {
		tb.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		tb.Fatal(err)
	}
	defer reader.Close()
	var fixture summaryFixture
	if err := json.NewDecoder(reader).Decode(&fixture); err != nil {
		tb.Fatal(err)
	}
	lanes := make(map[string]sim.Lane, len(network.Lanes))
	for _, lane := range network.Lanes {
		lanes[lane.ID] = lane
	}
	state := sim.Snapshot{Berths: fixture.Berths}
	for _, saved := range fixture.Vehicles {
		vehicle := sim.Vehicle{Pod: sim.Pod{Activity: saved.Activity, Speed: saved.Speed, WaitReason: saved.Wait, LaneID: saved.Lane}}
		for _, id := range saved.Route {
			lane, ok := lanes[id]
			if !ok {
				tb.Fatalf("unknown saved route lane %q", id)
			}
			vehicle.Route = append(vehicle.Route, lane)
		}
		state.Vehicles = append(state.Vehicles, vehicle)
	}
	return network, state
}

func TestNetworkMonitorSavedLondonSnapshots(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"central", "full"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network, state := loadSummaryFixture(t, name)
			checkNetworkSummaries(t, network, state)
			for index := range state.Vehicles {
				vehicle := &state.Vehicles[index]
				if len(vehicle.Route) == 0 {
					continue
				}
				vehicle.Pod.Speed, vehicle.Pod.WaitReason = 0, sim.TrackOccupied
				vehicle.Pod.LaneID = vehicle.Route[len(vehicle.Route)/2].ID
			}
			checkNetworkSummaries(t, network, state)
		})
	}
}

func BenchmarkStationSummaryNetwork(b *testing.B) {
	for _, name := range []string{"central", "full"} {
		network, state := loadSummaryFixture(b, name)
		b.Run(name+"/old-frame", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				monitor := NewStationMonitor(network)
				for _, station := range network.Stations {
					_ = monitor.Summarize(station, state)
				}
			}
		})
		b.Run(name+"/cached-single", func(b *testing.B) {
			monitor := NewStationMonitor(network)
			b.ReportAllocs()
			for b.Loop() {
				for _, station := range network.Stations {
					_ = monitor.Summarize(station, state)
				}
			}
		})
		b.Run(name+"/batch", func(b *testing.B) {
			monitor := NewNetworkMonitor(network)
			b.ReportAllocs()
			for b.Loop() {
				_ = monitor.Summarize(state)
			}
		})
	}
}
