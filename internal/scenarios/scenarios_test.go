package scenarios

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
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
		{name: "rail hub", config: RailHub, stations: 7, pods: 30},
		{name: "scale 100", config: Scale100, stations: 20, pods: 100},
		{name: "London", config: London, stations: 99, pods: 114},
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

func TestLondonUsesRealScaleAndDirectedGuideways(t *testing.T) {
	t.Parallel()
	config := London()
	stations := make(map[string]sim.Station, len(config.Network.Stations))
	for _, station := range config.Network.Stations {
		stations[station.Name] = station
	}
	for _, name := range []string{"Holborn", "Goodge Street", "Euston", "Bank and Monument", "Willesden Green"} {
		if _, ok := stations[name]; !ok {
			t.Fatalf("missing named London station %q", name)
		}
	}
	holborn := stations["Holborn"]
	chancery := stations["Chancery Lane"]
	holbornJunction, _ := config.Network.Node(londonJunctionID(holborn.ID))
	chanceryJunction, _ := config.Network.Node(londonJunctionID(chancery.ID))
	distance := math.Hypot(
		holbornJunction.Position.X-chanceryJunction.Position.X,
		holbornJunction.Position.Y-chanceryJunction.Position.Y,
	)
	if distance < 500 || distance > 800 {
		t.Fatalf("Holborn to Chancery Lane projection = %.1f meters", distance)
	}
	if holborn.Entry == londonJunctionID(holborn.ID) || holborn.Exit == londonJunctionID(holborn.ID) {
		t.Fatal("Holborn berths are not off the through guideway")
	}
	if _, err := config.Network.Route(stations["Brixton"].Berths[0].Node, stations["Willesden Green"].Berths[0].Node); err != nil {
		t.Fatalf("route across London extent: %v", err)
	}

	pairedSegments := 0
	for _, lane := range config.Network.Lanes {
		if strings.HasPrefix(lane.ID, "london-link-") {
			pairedSegments++
		}
	}
	if pairedSegments != 4*127 {
		t.Fatalf("got %d directed guideway segments, want %d", pairedSegments, 4*127)
	}
}

func TestLondonHasDistributedParking(t *testing.T) {
	t.Parallel()
	config := London()
	parking, parkingPods := 0, 0
	for _, station := range config.Network.Stations {
		if station.ParkingOnly {
			parking++
			if len(station.Berths) != londonParkingBerths {
				t.Fatalf("parking %q has %d berths", station.Name, len(station.Berths))
			}
		}
	}
	for _, placement := range config.Fleet {
		station, _ := config.Network.Station(placement.StationID)
		if station.ParkingOnly {
			parkingPods++
		}
	}
	if parking != 3 || parkingPods != 18 {
		t.Fatalf("got %d Parking stations and %d parked pods", parking, parkingPods)
	}
}

func TestRailHubHasExplicitCapacityAndReserveFleet(t *testing.T) {
	t.Parallel()
	config := RailHub()
	hub, ok := config.Network.Station(config.Demand.Destination)
	if !ok || hub.Name != "Rail Hub" || len(hub.Berths) != 6 {
		t.Fatalf("rail hub = %+v, found = %t", hub, ok)
	}
	parked := 0
	for _, placement := range config.Fleet {
		if placement.StationID == "parking" {
			parked++
		}
	}
	if parked != 12 {
		t.Fatalf("parked pods = %d, want 12", parked)
	}
	for _, berth := range hub.Berths {
		arrival, err := config.Network.Route(hub.Entry, berth.Node)
		if err != nil || len(arrival) == 0 {
			t.Fatalf("route to berth %q = %v, %v", berth.ID, arrival, err)
		}
		departure, err := config.Network.Route(berth.Node, hub.Exit)
		if err != nil || len(departure) == 0 {
			t.Fatalf("route from berth %q = %v, %v", berth.ID, departure, err)
		}
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

func TestScale100UsesConnectedMeshWithRouteChoices(t *testing.T) {
	t.Parallel()
	network := Scale100().Network
	junctions := make(map[string]bool)
	for _, node := range network.Nodes {
		if strings.HasPrefix(node.ID, "junction-") {
			junctions[node.ID] = true
		}
	}
	if len(junctions) != 20 {
		t.Fatalf("got %d explicit mesh junctions, want 20", len(junctions))
	}
	conflicts := 0
	for junction := range junctions {
		incoming, outgoing := 0, 0
		for _, lane := range network.Lanes {
			if lane.To == junction {
				incoming++
			}
			if lane.From == junction {
				outgoing++
			}
		}
		if incoming >= 2 && outgoing >= 2 {
			conflicts++
		}
	}
	if conflicts < 6 {
		t.Fatalf("got %d junctions with at least two incoming and outgoing lanes, want at least 6", conflicts)
	}

	from, to := "junction-2-2", "junction-3-4"
	route, err := network.Route(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(route) < 6 {
		t.Fatalf("interior route uses only %d lanes", len(route))
	}
	removed := route[0]
	alternate := network
	alternate.Lanes = make([]sim.Lane, 0, len(network.Lanes)-1)
	for _, lane := range network.Lanes {
		if lane.ID != removed.ID {
			alternate.Lanes = append(alternate.Lanes, lane)
		}
	}
	if alternateRoute, err := alternate.Route(from, to); err != nil {
		t.Fatalf("removing %q eliminated the alternate interior route: %v", removed.ID, err)
	} else if reflect.DeepEqual(route, alternateRoute) {
		t.Fatal("alternate route did not change")
	}
}

func TestScale100MeshLanesCrossOnlyAtJunctions(t *testing.T) {
	t.Parallel()
	network := Scale100().Network
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	var mesh []sim.Lane
	for _, lane := range network.Lanes {
		if strings.HasPrefix(lane.ID, "mesh-junction-") {
			mesh = append(mesh, lane)
		}
	}
	for index, first := range mesh {
		for _, second := range mesh[index+1:] {
			if first.From == second.From || first.From == second.To || first.To == second.From || first.To == second.To {
				continue
			}
			if segmentsCross(lineSegment{from: nodes[first.From], to: nodes[first.To]}, lineSegment{from: nodes[second.From], to: nodes[second.To]}) {
				t.Fatalf("mesh lanes %q and %q cross without a shared junction", first.ID, second.ID)
			}
		}
	}
}

func TestScale100UnrelatedLanesDoNotCross(t *testing.T) {
	t.Parallel()
	network := Scale100().Network
	segments := make(map[string][]lineSegment, len(network.Lanes))
	for _, lane := range network.Lanes {
		length := network.Length(lane)
		for segmentIndex := range 12 {
			segments[lane.ID] = append(segments[lane.ID], lineSegment{
				from: network.Position(lane, length*float64(segmentIndex)/12),
				to:   network.Position(lane, length*float64(segmentIndex+1)/12),
			})
		}
	}
	for firstIndex, first := range network.Lanes {
		for _, second := range network.Lanes[firstIndex+1:] {
			if first.From == second.From || first.From == second.To || first.To == second.From || first.To == second.To {
				continue
			}
			for _, firstSegment := range segments[first.ID] {
				for _, secondSegment := range segments[second.ID] {
					if segmentsCross(firstSegment, secondSegment) {
						t.Fatalf("lanes %q and %q cross without a shared node", first.ID, second.ID)
					}
				}
			}
		}
	}
}

type lineSegment struct {
	from, to sim.Point
}

func segmentsCross(first, second lineSegment) bool {
	return orientation(second.from, first)*orientation(second.to, first) < 0 &&
		orientation(first.from, second)*orientation(first.to, second) < 0
}

func orientation(point sim.Point, segment lineSegment) float64 {
	return (segment.to.X-segment.from.X)*(point.Y-segment.from.Y) -
		(segment.to.Y-segment.from.Y)*(point.X-segment.from.X)
}

func TestScale100StationAccessUsesSeparateRoadNodes(t *testing.T) {
	t.Parallel()
	network := Scale100().Network
	lanes := make(map[string]sim.Lane, len(network.Lanes))
	incoming, outgoing := make(map[string]int), make(map[string]int)
	for _, lane := range network.Lanes {
		lanes[lane.ID] = lane
		incoming[lane.To]++
		outgoing[lane.From]++
	}
	for index := range 20 {
		in := lanes[fmt.Sprintf("mesh-in-%02d", index+1)]
		out := lanes[fmt.Sprintf("mesh-out-%02d", index+1)]
		if in.From == out.To || in.To != stationNodeID(index, "entry") || out.From != stationNodeID(index, "exit") {
			t.Fatalf("station %d access is not separate: in=%+v out=%+v", index+1, in, out)
		}
		if incoming[in.From] != 1 || outgoing[in.From] != 2 || incoming[out.To] != 2 || outgoing[out.To] != 1 {
			t.Fatalf("station %d needs a road diverge and merge", index+1)
		}
		entry, _ := network.Node(in.From)
		exit, _ := network.Node(out.To)
		if math.Hypot(entry.Position.X-exit.Position.X, entry.Position.Y-exit.Position.Y) < 20*sim.Clearance {
			t.Fatalf("station %d road connections are too close", index+1)
		}
	}
}

func TestScale100UnrelatedLanesKeepClearance(t *testing.T) {
	t.Parallel()
	network := Scale100().Network
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	for index, first := range network.Lanes {
		if first.Control != nil {
			t.Fatalf("update clearance check for curved lane %q", first.ID)
		}
		a := lineSegment{from: nodes[first.From], to: nodes[first.To]}
		for _, second := range network.Lanes[index+1:] {
			if first.From == second.From || first.From == second.To || first.To == second.From || first.To == second.To {
				continue
			}
			b := lineSegment{from: nodes[second.From], to: nodes[second.To]}
			gap := min(distanceToSegment(a.from, b), distanceToSegment(a.to, b), distanceToSegment(b.from, a), distanceToSegment(b.to, a))
			if segmentsCross(a, b) || gap < sim.Clearance {
				t.Fatalf("unrelated lanes %q and %q lack clearance: %.3fm", first.ID, second.ID, gap)
			}
		}
	}
}

func distanceToSegment(point sim.Point, segment lineSegment) float64 {
	dx, dy := segment.to.X-segment.from.X, segment.to.Y-segment.from.Y
	fraction := max(0, min(1, ((point.X-segment.from.X)*dx+(point.Y-segment.from.Y)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(point.X-segment.from.X-fraction*dx, point.Y-segment.from.Y-fraction*dy)
}

func TestScale100StationAccessPreservesRoadTravel(t *testing.T) {
	t.Parallel()
	network := Scale100().Network
	road := sim.Network{Nodes: network.Nodes}
	for _, lane := range network.Lanes {
		if lane.SpeedLimit != speedLimit {
			t.Fatalf("lane %q changes the scenario speed limit", lane.ID)
		}
		if strings.HasPrefix(lane.ID, "mesh-junction-") {
			road.Lanes = append(road.Lanes, lane)
		}
	}
	for row := range 4 {
		for column := range 5 {
			var edges []meshEdge
			if column < 4 {
				edges = append(edges, meshEdge{rowA: row, columnA: column, rowB: row, columnB: column + 1})
			}
			if row < 3 {
				edges = append(edges, meshEdge{rowA: row, columnA: column, rowB: row + 1, columnB: column})
			}
			for _, edge := range edges {
				_, original := meshCarriageways(edge)
				for _, lane := range original {
					route, err := road.Route(lane.From, lane.To)
					if err != nil {
						t.Fatalf("station access disconnects road %q: %v", lane.ID, err)
					}
					length := 0.0
					for _, part := range route {
						length += road.Length(part)
					}
					if math.Abs(length-road.Length(lane)) > 1e-6 {
						t.Fatalf("station access changes road %q length: %.3f", lane.ID, length)
					}
				}
			}
		}
	}
	parking, _ := network.Station("parking")
	for _, berth := range parking.Berths {
		node, _ := network.Node(berth.Node)
		if node.Position.Y <= 3*1200+sim.Clearance {
			t.Fatalf("parking berth %q must remain outside the mesh", berth.ID)
		}
	}
}
