package project

import (
	"math/rand/v2"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// bankConfig is the default project with the bank example network.
func bankConfig() Config {
	config := Default()
	config.Name, config.Network = "banks", sim.BankExample()
	config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}, {ID: "02", StationID: "hub", BerthID: "bank-a-1"}}
	return config
}

// islandGapConfig makes each station of config a passenger station, and adds
// the parking station "island" with one berth. No lane enters the island, and
// one lane goes from its exit to the first station. Each berth then has a
// parking gap: a route from parking and no route to parking.
func islandGapConfig(config Config) Config {
	network := config.Network
	network.Nodes, network.Lanes, network.Stations = slices.Clone(network.Nodes), slices.Clone(network.Lanes), slices.Clone(network.Stations)
	for index := range network.Stations {
		network.Stations[index].ParkingOnly = false
		network.Stations[index].Berths = slices.Clone(network.Stations[index].Berths)
	}
	network.Nodes = append(network.Nodes, sim.Node{ID: "island-entry", Position: sim.Point{X: 7000, Y: 7000}}, sim.Node{ID: "island-berth", Position: sim.Point{X: 7100, Y: 6940}}, sim.Node{ID: "island-exit", Position: sim.Point{X: 7200, Y: 7000}})
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: "island-through", From: "island-entry", To: "island-exit", SpeedLimit: 14, StationID: "island", StationRole: sim.StationThroughRole},
		sim.Lane{ID: "island-in", From: "island-entry", To: "island-berth", SpeedLimit: 14, StationID: "island", StationRole: sim.StationBerthAccessRole},
		sim.Lane{ID: "island-out", From: "island-berth", To: "island-exit", SpeedLimit: 14, StationID: "island", StationRole: sim.StationDepartureRole},
		sim.Lane{ID: "island-link", From: "island-exit", To: network.Stations[0].Entry, SpeedLimit: 14})
	network.Stations = append(network.Stations, sim.Station{ID: "island", Name: "Island", Entry: "island-entry", Exit: "island-exit", ParkingOnly: true, Berths: []sim.Berth{{ID: "island-1", Node: "island-berth"}}})
	config.Network = network
	return config
}

// depotIslandConfig is islandGapConfig with a lane into the island and with
// no lane from the entry of the island to its berth. The island is a depot,
// so its entry is the parking goal, and no berth has a gap.
func depotIslandConfig() Config {
	config := islandGapConfig(Default())
	network := &config.Network
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane sim.Lane) bool { return lane.ID == "island-in" })
	network.Lanes = append(network.Lanes, sim.Lane{ID: "island-enter", From: network.Stations[0].Exit, To: "island-entry", SpeedLimit: 14})
	network.Depots = []sim.Depot{{Station: "island", Lanes: []sim.DepotLane{{BerthIDs: []string{"island-1"}}}}}
	return config
}

// laneIndex returns the index of the lane id of config.
func laneIndex(t *testing.T, config *Config, id string) int {
	t.Helper()
	index := slices.IndexFunc(config.Network.Lanes, func(lane sim.Lane) bool { return lane.ID == id })
	if index < 0 {
		t.Fatalf("the network has no lane %s", id)
	}
	return index
}

// jitter returns config with each node moved by up to 2 m on each axis, and
// with the control point of each lane added, moved or removed.
func jitter(config Config, random *rand.Rand) Config {
	network := config.Network
	network.Nodes, network.Lanes = slices.Clone(network.Nodes), slices.Clone(network.Lanes)
	positions := make(map[string]sim.Point, len(network.Nodes))
	for index := range network.Nodes {
		network.Nodes[index].Position.X += 4*random.Float64() - 2
		network.Nodes[index].Position.Y += 4*random.Float64() - 2
		positions[network.Nodes[index].ID] = network.Nodes[index].Position
	}
	for index, lane := range network.Lanes {
		network.Lanes[index].Control = nil
		if random.IntN(2) == 0 {
			from, to := positions[lane.From], positions[lane.To]
			network.Lanes[index].Control = &sim.Point{X: (from.X+to.X)/2 + 4*random.Float64() - 2, Y: (from.Y+to.Y)/2 + 4*random.Float64() - 2}
		}
	}
	config.Network = network
	return config
}

// TestParkingRoutesKey pins the key of the kept parking route result. A
// change of the node positions and the control points keeps the result, with
// and without gaps. A change of a member that the check reads gives a new
// result. Each case changes the gaps, so a key without that member gives the
// gaps of the project before the change.
func TestParkingRoutesKey(t *testing.T) {
	t.Parallel()
	group, err := sim.NewClassSet("group")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		base   Config
		change func(*testing.T, *Config)
	}{
		{"lane removed", Default(), func(_ *testing.T, config *Config) {
			for _, station := range config.Network.Stations {
				if station.ParkingOnly {
					config.Network.Lanes = slices.DeleteFunc(slices.Clone(config.Network.Lanes), func(lane sim.Lane) bool { return lane.To == station.Entry })
				}
			}
		}},
		{"lane to", islandGapConfig(Default()), func(t *testing.T, config *Config) {
			t.Helper()
			lanes := slices.Clone(config.Network.Lanes)
			lanes[laneIndex(t, config, "island-link")].To = "island-entry"
			config.Network.Lanes = lanes
		}},
		{"lane from", depotIslandConfig(), func(t *testing.T, config *Config) {
			t.Helper()
			lanes := slices.Clone(config.Network.Lanes)
			lanes[laneIndex(t, config, "island-enter")].From = "island-exit"
			config.Network.Lanes = lanes
		}},
		{"lane station", bankConfig(), func(t *testing.T, config *Config) {
			t.Helper()
			lanes := slices.Clone(config.Network.Lanes)
			index := slices.IndexFunc(lanes, func(lane sim.Lane) bool { return lane.StationID == "hub" && lane.StationRole == sim.StationThroughRole })
			if index < 0 {
				t.Fatal("the hub has no through lane")
			}
			lanes[index].StationID = "origin"
			config.Network.Lanes = lanes
		}},
		{"station entry", depotIslandConfig(), func(_ *testing.T, config *Config) {
			stations := slices.Clone(config.Network.Stations)
			stations[len(stations)-1].Entry = "island-berth"
			config.Network.Stations = stations
		}},
		{"station classes", islandGapConfig(Default()), func(_ *testing.T, config *Config) {
			stations := slices.Clone(config.Network.Stations)
			stations[len(stations)-1].VehicleClasses = group
			config.Network.Stations = stations
		}},
		{"lane classes", Default(), func(_ *testing.T, config *Config) {
			lanes := slices.Clone(config.Network.Lanes)
			for _, station := range config.Network.Stations {
				for _, berth := range station.Berths {
					for index := range lanes {
						if station.ParkingOnly && lanes[index].To == berth.Node {
							lanes[index].VehicleClasses = group
						}
					}
				}
			}
			config.Network.Lanes = lanes
		}},
		{"parking flag", islandGapConfig(Default()), func(_ *testing.T, config *Config) {
			stations := slices.Clone(config.Network.Stations)
			stations[len(stations)-1].ParkingOnly = false
			config.Network.Stations = stations
		}},
		{"berth order", islandGapConfig(Default()), func(t *testing.T, config *Config) {
			t.Helper()
			stations := slices.Clone(config.Network.Stations)
			index := slices.IndexFunc(stations, func(station sim.Station) bool { return len(station.Berths) > 1 })
			if index < 0 {
				t.Fatal("no station has two berths")
			}
			stations[index].Berths = slices.Clone(stations[index].Berths)
			slices.Reverse(stations[index].Berths)
			config.Network.Stations = stations
		}},
		{"fleet class added", islandGapConfig(Default()), func(_ *testing.T, config *Config) {
			config.Fleet = append(slices.Clone(config.Fleet), sim.Placement{ID: "compact", StationID: config.Fleet[0].StationID, Class: sim.CompactClass})
		}},
		{"fleet class order", func() Config {
			config := islandGapConfig(Default())
			config.Fleet = append(slices.Clone(config.Fleet), sim.Placement{ID: "compact", StationID: config.Fleet[0].StationID, Class: sim.CompactClass})
			return config
		}(), func(_ *testing.T, config *Config) {
			config.Fleet = slices.Clone(config.Fleet)
			slices.Reverse(config.Fleet)
		}},
		{"station role", bankConfig(), func(t *testing.T, config *Config) {
			t.Helper()
			lanes := slices.Clone(config.Network.Lanes)
			index := slices.IndexFunc(lanes, func(lane sim.Lane) bool { return lane.StationID == "hub" && lane.StationRole == sim.StationThroughRole })
			if index < 0 {
				t.Fatal("the hub has no through lane")
			}
			lanes[index].StationRole = sim.StationBerthAccessRole
			config.Network.Lanes = lanes
		}},
		{"bank list", bankConfig(), func(t *testing.T, config *Config) {
			t.Helper()
			stations := slices.Clone(config.Network.Stations)
			index := slices.IndexFunc(stations, func(station sim.Station) bool { return len(station.Banks) > 1 })
			if index < 0 {
				t.Fatal("no station has two banks")
			}
			stations[index].Banks = slices.Clone(stations[index].Banks)
			stations[index].Banks[0].BerthIDs = nil
			config.Network.Stations = stations
		}},
		{"depots", depotIslandConfig(), func(_ *testing.T, config *Config) { config.Network.Depots = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			random := rand.New(rand.NewPCG(1, 2))
			var kept ParkingRoutes
			before := ParkingGaps(test.base)
			if got := kept.Gaps(test.base); !reflect.DeepEqual(got, before) {
				t.Fatalf("gaps = %v, want %v", got, before)
			}
			key := &kept.topology.Nodes[0]
			changed := test.base
			test.change(t, &changed)
			after := ParkingGaps(changed)
			if reflect.DeepEqual(before, after) {
				t.Fatalf("the change keeps the gaps %v, so the case tests no key", before)
			}
			// Moves keep the result of each of the two projects, and
			// the change between them gives a new result.
			for index, state := range []struct {
				config Config
				want   []sim.ParkingGap
			}{{test.base, before}, {changed, after}, {test.base, before}} {
				if got := kept.Gaps(state.config); !reflect.DeepEqual(got, state.want) {
					t.Fatalf("state %d: gaps = %v, want %v", index, got, state.want)
				}
				if hit := &kept.topology.Nodes[0] == key; hit != (index == 0) {
					t.Fatalf("state %d: the result was kept: %t", index, hit)
				}
				key = &kept.topology.Nodes[0]
				for range 5 {
					moved := jitter(state.config, random)
					if got, want := kept.Gaps(moved), ParkingGaps(moved); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got, state.want) {
						t.Fatalf("after a move: gaps = %v, a new check gives %v, before the move %v", got, want, state.want)
					}
					if &kept.topology.Nodes[0] != key {
						t.Fatal("a move ran the check again")
					}
				}
			}
		})
	}
}

// TestParkingRoutesKeepRefusal pins that ValidateWith gives the parking
// refusal of Validate, text included, from a kept result after a node move,
// and that it reads the result that it gets.
func TestParkingRoutesKeepRefusal(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(3, 4))
	config := islandGapConfig(Default())
	const refusal = "parking routes: 5 berths have a gap, the first is berth harbor-1 of station harbor, with no legacy route to parking"
	if err := Validate(config); err == nil || err.Error() != refusal {
		t.Fatalf("refusal = %v, want %s", err, refusal)
	}
	var kept ParkingRoutes
	for index := range 6 {
		if err := ValidateWith(config, Checked{Parking: &kept}); err == nil || err.Error() != refusal {
			t.Fatalf("move %d: refusal = %v, want %s", index, err, refusal)
		}
		if got, want := validationText(ValidateWith(config, Checked{Parking: &kept})), validationText(Validate(config)); got != want {
			t.Fatalf("move %d: refusal = %s, want %s", index, got, want)
		}
		config = jitter(config, random)
		// Keep the controls out: a curved lane can fail an earlier check.
		for lane := range config.Network.Lanes {
			config.Network.Lanes[lane].Control = nil
		}
	}
	// A valid project gets the refusal of the gaps that the caller gives.
	stale := ParkingRoutes{topology: routeTopology(Default().Network), classes: fleetClasses(Default().Fleet), gaps: kept.gaps, kept: true}
	if err := ValidateWith(Default(), Checked{Parking: &stale}); err == nil || err.Error() != refusal {
		t.Fatalf("refusal = %v, want the refusal %s of the kept gaps", err, refusal)
	}
	name := runtime.FuncForPC(reflect.ValueOf(projectChecks[parkingCheck]).Pointer()).Name()
	if !strings.HasSuffix(name, ".validateParkingRoutes") {
		t.Fatalf("projectChecks[parkingCheck] is %s", name)
	}
}

// costConfig is the default project with the speed limits of the lanes
// return-start and return-to-parking, the only route from the market to
// parking, and with the node return-east between the two lanes at
// position. The cost of a lane is its length divided by its speed limit.
func costConfig(t *testing.T, start, toParking float64, position sim.Point) Config {
	t.Helper()
	config := Default()
	config.Network.Nodes, config.Network.Lanes = slices.Clone(config.Network.Nodes), slices.Clone(config.Network.Lanes)
	config.Network.Lanes[laneIndex(t, &config, "return-start")].SpeedLimit = start
	config.Network.Lanes[laneIndex(t, &config, "return-to-parking")].SpeedLimit = toParking
	node := slices.IndexFunc(config.Network.Nodes, func(node sim.Node) bool { return node.ID == "return-east" })
	if node < 0 {
		t.Fatal("the network has no node return-east")
	}
	config.Network.Nodes[node].Position = position
	return config
}

// TestParkingRoutesCostBound pins the cost bound of the kept parking route
// result. A node move that makes the cost of a route infinite removes the
// route, so it changes the gaps. The cost of one lane can become infinite,
// or the sum of the finite costs of the lanes of a route can. Each case goes
// from the project with no gap to the project with gaps, and back, and the
// kept result must be the result of a new check at each step.
func TestParkingRoutesCostBound(t *testing.T) {
	t.Parallel()
	east := sim.Point{X: 900, Y: 460}
	for _, test := range []struct {
		name          string
		before, after Config
	}{
		// return-to-parking is 280 m long, then 380 m long.
		{"lane cost", costConfig(t, 14, 2e-306, east), costConfig(t, 14, 2e-306, sim.Point{X: 1000, Y: 460})},
		// Each of the two lanes costs about 8e307 s, then 1.2e308 s and
		// 8.5e307 s, which are finite and have no finite sum.
		{"route cost", costConfig(t, 201/8e307, 280/8e307, east), costConfig(t, 201/8e307, 280/8e307, sim.Point{X: 900, Y: 560})},
		// return-to-parking costs 4e307 s, in the bound, then 1280 m
		// gives no finite cost. The result of the first project is kept,
		// and the result of the second must not be.
		{"bound crossed", costConfig(t, 14, 7e-306, east), costConfig(t, 14, 7e-306, sim.Point{X: 1900, Y: 460})},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before, after := ParkingGaps(test.before), ParkingGaps(test.after)
			if len(before) != 0 || len(after) == 0 {
				t.Fatalf("gaps before the move %v, after %v, want none and some", before, after)
			}
			if test.name == "bound crossed" && (!test.before.Network.RouteCostsBounded() || test.after.Network.RouteCostsBounded()) {
				t.Fatal("the move does not take the costs out of the bound")
			}
			for _, order := range [][]Config{{test.before, test.after, test.before}, {test.after, test.before, test.after}} {
				var kept ParkingRoutes
				for index, config := range order {
					if got, want := kept.Gaps(config), ParkingGaps(config); !reflect.DeepEqual(got, want) {
						t.Fatalf("step %d: gaps = %v, a new check gives %v", index, got, want)
					}
				}
			}
		})
	}
}

// TestParkingRoutesGapsCopy pins that a caller that changes the gaps that
// it gets does not change the kept result.
func TestParkingRoutesGapsCopy(t *testing.T) {
	t.Parallel()
	config := islandGapConfig(Default())
	var kept ParkingRoutes
	gaps := kept.Gaps(config)
	want := slices.Clone(gaps)
	gaps[0].BerthID = "changed"
	if got := kept.Gaps(config); !reflect.DeepEqual(got, want) {
		t.Fatalf("gaps = %v, want %v", got, want)
	}
}
