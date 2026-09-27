package project

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// passengerRoutesReference is the passenger route check that Validate used
// before validatePassengerRoutes. It searches from each passenger berth.
// validatePassengerRoutes must return the same error.
func passengerRoutesReference(lanes []sim.Lane, passenger []sim.Station) error {
	adjacent := make(map[string][]string)
	for _, lane := range lanes {
		adjacent[lane.From] = append(adjacent[lane.From], lane.To)
	}
	for _, from := range passenger {
		for _, origin := range from.Berths {
			reachable := directedReachable(adjacent, origin.Node)
			for _, to := range passenger {
				if from.ID == to.ID {
					continue
				}
				for _, destination := range to.Berths {
					if !reachable[destination.Node] {
						return fmt.Errorf("passenger route %q berth %q to %q berth %q: %w", from.ID, origin.ID, to.ID, destination.ID, sim.ErrUnreachable)
					}
				}
			}
		}
	}
	return nil
}

func directedReachable(adjacent map[string][]string, start string) map[string]bool {
	reachable := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[current] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reachable
}

// ComparePassengerRoutes fails t when validatePassengerRoutes and the
// reference give different results for network. It returns the error. It
// is exported for the preset tests in package project_test.
func ComparePassengerRoutes(t *testing.T, network sim.Network) error {
	t.Helper()
	passenger := PassengerStations(network)
	got := validatePassengerRoutes(network.Lanes, passenger)
	want := passengerRoutesReference(network.Lanes, passenger)
	if (got == nil) != (want == nil) || (got != nil && (got.Error() != want.Error() || !errors.Is(got, sim.ErrUnreachable))) {
		t.Fatalf("validatePassengerRoutes = %v, want %v", got, want)
	}
	return got
}

// berthRingNetwork returns a one-way ring of passenger stations. Each
// station has berths berths.
func berthRingNetwork(stations, berths int) sim.Network {
	var network sim.Network
	for index := range stations {
		id := fmt.Sprintf("s%03d", index)
		entry, exit := id+"-entry", id+"-exit"
		x := 400 * float64(index)
		network.Nodes = append(network.Nodes,
			sim.Node{ID: entry, Position: sim.Point{X: x}},
			sim.Node{ID: exit, Position: sim.Point{X: x + 200}},
		)
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: id + "-through", From: entry, To: exit, SpeedLimit: 14, StationID: id, StationRole: sim.StationThroughRole},
			sim.Lane{ID: id + "-next", From: exit, To: fmt.Sprintf("s%03d-entry", (index+1)%stations), SpeedLimit: 14},
		)
		station := sim.Station{ID: id, Name: "Station " + id, Entry: entry, Exit: exit}
		for berth := range berths {
			node := fmt.Sprintf("%s-berth-%d", id, berth)
			network.Nodes = append(network.Nodes, sim.Node{ID: node, Position: sim.Point{X: x + 100, Y: 60 + 30*float64(berth)}})
			network.Lanes = append(network.Lanes,
				sim.Lane{ID: node + "-in", From: entry, To: node, SpeedLimit: 14, StationID: id, StationRole: sim.StationBerthAccessRole},
				sim.Lane{ID: node + "-out", From: node, To: exit, SpeedLimit: 14, StationID: id, StationRole: sim.StationDepartureRole},
			)
			station.Berths = append(station.Berths, sim.Berth{ID: fmt.Sprintf("%s-%d", id, berth+1), Node: node})
		}
		network.Stations = append(network.Stations, station)
	}
	return network
}

// withoutLane returns network without the lane with the ID id.
func withoutLane(network sim.Network, id string) sim.Network {
	network.Lanes = slices.DeleteFunc(slices.Clone(network.Lanes), func(lane sim.Lane) bool { return lane.ID == id })
	return network
}

// TestValidateReportsFirstUnreachablePassengerRoute checks the Validate
// error for networks where a berth is cut off, is at a one-way dead end, or
// can be reached in only one direction. The error must be the same as the
// error of the reference.
func TestValidateReportsFirstUnreachablePassengerRoute(t *testing.T) {
	t.Parallel()
	ring := berthRingNetwork(4, 2)
	// A second ring of two stations, with no lane to or from the first.
	island := berthRingNetwork(2, 1)
	for index := range island.Nodes {
		island.Nodes[index].ID = "i" + island.Nodes[index].ID
		island.Nodes[index].Position.Y += 2000
	}
	for index := range island.Lanes {
		lane := &island.Lanes[index]
		lane.ID, lane.From, lane.To = "i"+lane.ID, "i"+lane.From, "i"+lane.To
		if lane.StationID != "" {
			lane.StationID = "i" + lane.StationID
		}
	}
	for index := range island.Stations {
		station := &island.Stations[index]
		station.ID, station.Entry, station.Exit = "i"+station.ID, "i"+station.Entry, "i"+station.Exit
		for berth := range station.Berths {
			station.Berths[berth].ID, station.Berths[berth].Node = "i"+station.Berths[berth].ID, "i"+station.Berths[berth].Node
		}
	}
	disconnected := ring
	disconnected.Nodes = append(append([]sim.Node(nil), ring.Nodes...), island.Nodes...)
	disconnected.Lanes = append(append([]sim.Lane(nil), ring.Lanes...), island.Lanes...)
	disconnected.Stations = append(append([]sim.Station(nil), ring.Stations...), island.Stations...)
	// s003 feeds s000, but no lane goes to s003.
	source := withoutLane(ring, "s002-next")
	source.Lanes = append(source.Lanes, sim.Lane{ID: "s002-back", From: "s002-exit", To: "s000-entry", SpeedLimit: 14})
	for _, test := range []struct {
		name    string
		network sim.Network
		want    string
	}{
		{
			name:    "disconnected stations",
			network: disconnected,
			want:    `passenger route "s000" berth "s000-1" to "is000" berth "is000-1": destination is unreachable`,
		},
		{
			name:    "one-way dead end",
			network: withoutLane(ring, "s001-next"),
			want:    `passenger route "s000" berth "s000-1" to "s002" berth "s002-1": destination is unreachable`,
		},
		{
			name:    "reachable one way only",
			network: source,
			want:    `passenger route "s000" berth "s000-1" to "s003" berth "s003-1": destination is unreachable`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			config.Network = test.network
			config.Fleet = []sim.Placement{{ID: "01", StationID: "s000", BerthID: "s000-1"}}
			ComparePassengerRoutes(t, config.Network)
			err := Validate(config)
			if err == nil || err.Error() != test.want || !errors.Is(err, sim.ErrUnreachable) {
				t.Fatalf("Validate = %v, want %s", err, test.want)
			}
		})
	}
}

// TestPassengerRoutesMatchReferenceWithoutEachLane removes each lane of
// ring networks in turn. Most removals leave a berth that cannot reach, or
// cannot be reached from, the other berths.
func TestPassengerRoutesMatchReferenceWithoutEachLane(t *testing.T) {
	t.Parallel()
	for _, network := range []sim.Network{berthRingNetwork(3, 1), berthRingNetwork(4, 3), Default().Network} {
		ComparePassengerRoutes(t, network)
		for _, lane := range network.Lanes {
			ComparePassengerRoutes(t, withoutLane(network, lane.ID))
		}
	}
}

// TestPassengerRoutesMatchReferenceOnLargeRing compares the check on a ring
// of 100 stations with 18 berths each, the node limit of Validate. It also
// removes the lane out of the last berth, and the lane after the first
// station.
func TestPassengerRoutesMatchReferenceOnLargeRing(t *testing.T) {
	t.Parallel()
	ring := berthRingNetwork(100, 18)
	if err := ComparePassengerRoutes(t, ring); err != nil {
		t.Fatal(err)
	}
	for _, lane := range []string{"s099-berth-17-out", "s000-next"} {
		if ComparePassengerRoutes(t, withoutLane(ring, lane)) == nil {
			t.Fatalf("accepted the ring without lane %s", lane)
		}
	}
}

// TestPassengerRoutesMatchReferenceOnRandomGraphs compares the check with
// the reference on small random graphs. As after network validation, the
// node IDs are unique, each station has one or more berths, and each berth
// has its own node.
func TestPassengerRoutesMatchReferenceOnRandomGraphs(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(1, 2))
	counts := make(map[bool]int)
	for range 20000 {
		nodes := 3 + random.IntN(10)
		var network sim.Network
		for index := range nodes {
			network.Nodes = append(network.Nodes, sim.Node{ID: fmt.Sprintf("n%d", index)})
		}
		for index := range random.IntN(3 * nodes) {
			network.Lanes = append(network.Lanes, sim.Lane{
				ID:   fmt.Sprintf("l%d", index),
				From: network.Nodes[random.IntN(nodes)].ID,
				To:   network.Nodes[random.IntN(nodes)].ID,
			})
		}
		free := random.Perm(nodes)
		for station := 0; station < 2+random.IntN(3) && len(free) > 0; station++ {
			value := sim.Station{ID: fmt.Sprintf("s%d", station), ParkingOnly: random.IntN(5) == 0}
			for berth := 0; berth < 1+random.IntN(3) && len(free) > 0; berth++ {
				value.Berths = append(value.Berths, sim.Berth{ID: fmt.Sprintf("s%d-%d", station, berth), Node: network.Nodes[free[0]].ID})
				free = free[1:]
			}
			network.Stations = append(network.Stations, value)
		}
		counts[ComparePassengerRoutes(t, network) == nil]++
	}
	// Both results must occur often, or the test does not test much.
	if counts[true] < 1000 || counts[false] < 1000 {
		t.Fatalf("results: %d accepted, %d rejected", counts[true], counts[false])
	}
}

// TestPassengerRoutesFindFirstPairInOrder has a first berth that is not in
// the strongly connected component of the other berths. The first berth
// with no path is not the origin of the first pair with no path.
func TestPassengerRoutesFindFirstPairInOrder(t *testing.T) {
	t.Parallel()
	// a and a2 are at station a, c is at station c. a and a2 reach c, c
	// reaches a, and nothing reaches a2.
	network := sim.Network{
		Lanes: []sim.Lane{{ID: "ac", From: "a", To: "c"}, {ID: "a2c", From: "a2", To: "c"}, {ID: "ca", From: "c", To: "a"}},
		Stations: []sim.Station{
			{ID: "a", Berths: []sim.Berth{{ID: "a-1", Node: "a"}, {ID: "a-2", Node: "a2"}}},
			{ID: "c", Berths: []sim.Berth{{ID: "c-1", Node: "c"}}},
		},
	}
	err := ComparePassengerRoutes(t, network)
	if want := `passenger route "c" berth "c-1" to "a" berth "a-2": destination is unreachable`; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

// BenchmarkPassengerRoutes checks a ring of 200 stations with 18 berths
// each: 4,000 nodes and 7,600 lanes, more than Validate accepts. In the
// dead-end case, the last berth has no lane out, so the check fails at the
// last origin.
func BenchmarkPassengerRoutes(b *testing.B) {
	ring := berthRingNetwork(200, 18)
	for _, network := range []struct {
		name    string
		network sim.Network
		fails   bool
	}{
		{name: "ring", network: ring},
		{name: "dead-end", network: withoutLane(ring, "s199-berth-17-out"), fails: true},
	} {
		passenger := PassengerStations(network.network)
		for _, check := range []struct {
			name  string
			check func([]sim.Lane, []sim.Station) error
		}{
			{name: "new", check: validatePassengerRoutes},
			{name: "reference", check: passengerRoutesReference},
		} {
			b.Run(network.name+"/"+check.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := check.check(network.network.Lanes, passenger); (err != nil) != network.fails {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
