package view

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCollapsedStationAnchorsUseLondonJunctions(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	sources := londonSourcePositions(t)
	anchors := collapsedStationAnchors(network)
	passengers := 0
	for _, station := range network.Stations {
		anchor, ok := anchors[station.ID]
		if !ok {
			t.Fatalf("station %q has no anchor", station.ID)
		}
		if station.ParkingOnly {
			if want := berthCentroid(t, network, station); !closePoint(anchor, want) {
				t.Errorf("%s anchor = %v, want the berth centroid %v", station.Name, anchor, want)
			}
			continue
		}
		passengers++
		source, ok := sources[station.ID]
		if !ok {
			t.Fatalf("station %q has no source position", station.ID)
		}
		if distance := math.Hypot(anchor.X-source.X, anchor.Y-source.Y); distance > 70 {
			t.Errorf("%s anchor is %.1f m from its source position, want at most 70 m", station.Name, distance)
		}
	}
	if passengers != 96 {
		t.Fatalf("checked %d passenger stations, want 96", passengers)
	}
}

func TestCollapsedStationAnchorsKeepBerthCentroid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		network sim.Network
	}{
		// The example has approach lanes, but it is a detailed network.
		{name: "example", network: sim.Example()},
		{name: "scale100 mesh", network: scenarios.Scale100().Network},
		{name: "small ring", network: scenarios.Small().Network},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			anchors := collapsedStationAnchors(test.network)
			if len(anchors) != len(test.network.Stations) {
				t.Fatalf("got %d anchors, want %d", len(anchors), len(test.network.Stations))
			}
			for _, station := range test.network.Stations {
				if got, want := anchors[station.ID], berthCentroid(t, test.network, station); !closePoint(got, want) {
					t.Errorf("%s anchor = %v, want the berth centroid %v", station.ID, got, want)
				}
			}
		})
	}
}

func TestCollapsedStationAnchorRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		lanes   int
		parking bool
		role    sim.StationLaneRole
		starts  []string
		want    sim.Point
	}{
		{name: "mean of approach starts", lanes: detailedLanes + 1, role: sim.StationApproachRole, starts: []string{"a1", "a2"}, want: sim.Point{X: 50, Y: 0}},
		{name: "detailed network", lanes: detailedLanes, role: sim.StationApproachRole, starts: []string{"a1", "a2"}, want: sim.Point{X: 50, Y: 300}},
		{name: "parking", lanes: detailedLanes + 1, parking: true, role: sim.StationApproachRole, starts: []string{"a1", "a2"}, want: sim.Point{X: 50, Y: 300}},
		{name: "unknown approach start", lanes: detailedLanes + 1, role: sim.StationApproachRole, starts: []string{"missing", "a2"}, want: sim.Point{X: 100, Y: 0}},
		{name: "entry lanes only", lanes: detailedLanes + 1, role: sim.StationEntryRole, starts: []string{"a1", "a2"}, want: sim.Point{X: 50, Y: 300}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := sim.Network{
				Nodes: []sim.Node{
					{ID: "a1", Position: sim.Point{X: 0, Y: 0}},
					{ID: "a2", Position: sim.Point{X: 100, Y: 0}},
					{ID: "b1", Position: sim.Point{X: 0, Y: 300}},
					{ID: "b2", Position: sim.Point{X: 100, Y: 300}},
				},
				Stations: []sim.Station{{ID: "s", ParkingOnly: test.parking, Berths: []sim.Berth{{ID: "s-1", Node: "b1"}, {ID: "s-2", Node: "b2"}}}},
			}
			for index, start := range test.starts {
				network.Lanes = append(network.Lanes, sim.Lane{ID: fmt.Sprintf("in-%d", index), From: start, To: "b1", StationID: "s", StationRole: test.role})
			}
			for len(network.Lanes) < test.lanes {
				network.Lanes = append(network.Lanes, sim.Lane{ID: fmt.Sprintf("pad-%d", len(network.Lanes)), From: "b1", To: "b2"})
			}
			if got := collapsedStationAnchors(network)["s"]; !closePoint(got, test.want) {
				t.Fatalf("anchor = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCollapsedStationAnchorsSkipUnknownBerthNodes(t *testing.T) {
	t.Parallel()
	network := sim.Network{Stations: []sim.Station{{ID: "lost", Berths: []sim.Berth{{ID: "lost-1", Node: "missing"}}}}}
	if anchor, ok := collapsedStationAnchors(network)["lost"]; ok {
		t.Fatalf("anchor = %v, want no anchor", anchor)
	}
}

func TestStationAnchorsBuildOncePerNetwork(t *testing.T) {
	t.Parallel()
	first := session.State{Epoch: "first", ProjectRevision: 1, Generation: 1}
	tests := []struct {
		name    string
		state   session.State
		rebuild bool
	}{
		{name: "same network", state: first, rebuild: false},
		{name: "new generation", state: session.State{Epoch: "first", ProjectRevision: 1, Generation: 2}, rebuild: true},
		{name: "new project revision", state: session.State{Epoch: "first", ProjectRevision: 2, Generation: 1}, rebuild: true},
		{name: "new epoch", state: session.State{Epoch: "second", ProjectRevision: 1, Generation: 1}, rebuild: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{network: sim.Example(), state: first}
			before := game.stationAnchors()["harbor"]
			moved := sim.Example()
			for index := range moved.Nodes {
				moved.Nodes[index].Position.X += 1000
			}
			game.network, game.state = moved, test.state
			want := before
			if test.rebuild {
				want = collapsedStationAnchors(moved)["harbor"]
			}
			if got := game.stationAnchors()["harbor"]; got != want {
				t.Fatalf("harbor anchor = %v, want %v", got, want)
			}
		})
	}
}

// londonSourcePositions returns the source position of each London station
// by station ID. It reads the checked-in TfL source and uses the same local
// meter projection as the London generator.
func londonSourcePositions(t *testing.T) map[string]sim.Point {
	t.Helper()
	data, err := os.ReadFile("../scenarios/data/london-tube.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Stations []struct {
			ID        string  `json:"id"`
			Latitude  float64 `json:"lat"`
			Longitude float64 `json:"lon"`
		} `json:"stations"`
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	const (
		earthRadiusMeters  = 6_371_000.0
		referenceLatitude  = 51.5074
		referenceLongitude = -0.1278
	)
	positions := make(map[string]sim.Point, len(source.Stations))
	for _, station := range source.Stations {
		positions[station.ID] = sim.Point{
			X: (station.Longitude - referenceLongitude) * math.Pi / 180 * earthRadiusMeters * math.Cos(referenceLatitude*math.Pi/180),
			Y: -(station.Latitude - referenceLatitude) * math.Pi / 180 * earthRadiusMeters,
		}
	}
	return positions
}

// berthCentroid returns the mean position of the berth nodes of the station.
func berthCentroid(t *testing.T, network sim.Network, station sim.Station) sim.Point {
	t.Helper()
	var sum sim.Point
	for _, berth := range station.Berths {
		node, ok := network.Node(berth.Node)
		if !ok {
			t.Fatalf("station %q berth %q has no node %q", station.ID, berth.ID, berth.Node)
		}
		sum.X += node.Position.X
		sum.Y += node.Position.Y
	}
	return sim.Point{X: sum.X / float64(len(station.Berths)), Y: sum.Y / float64(len(station.Berths))}
}
