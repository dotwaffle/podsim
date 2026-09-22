package scenarios

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	londonReferenceLatitude  = 51.5074
	londonReferenceLongitude = -0.1278
	londonTrackOffset        = 18.0
	londonStationBerths      = 2
	londonParkingBerths      = 12
)

//go:embed data/london-tube.json
var londonSourceJSON []byte

type londonSource struct {
	Source struct {
		Retrieved   string `json:"retrieved"`
		URL         string `json:"url"`
		Attribution string `json:"attribution"`
	} `json:"source"`
	Stations []londonSourceStation `json:"stations"`
	Links    []londonSourceLink    `json:"links"`
}

type londonSourceStation struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Zone      string  `json:"zone"`
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
}

type londonSourceLink struct {
	A     string   `json:"a"`
	B     string   `json:"b"`
	Lines []string `json:"lines"`
}

type londonParkingSource struct {
	ID, Name, Gateway string
	Direction         float64
}

var (
	londonOnce   sync.Once
	londonPreset project.Config
)

// London returns a geographically scaled central-London qualification preset.
func London() project.Config {
	londonOnce.Do(func() {
		londonPreset = mustLondonConfig()
	})
	return project.Clone(londonPreset)
}

func mustLondonConfig() project.Config {
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		panic(err)
	}
	config := project.Config{
		Version: 1,
		Name:    "Central London Underground-derived PRT",
		Network: londonNetwork(source),
		Demand: project.DemandConfig{
			PerMinute: 20,
			Pattern:   "balanced",
			Seed:      20260922,
		},
		Redistribution: true,
	}
	config.Fleet = londonFleet(config.Network)
	if err := project.Validate(config); err != nil {
		panic(fmt.Errorf("validate London scenario: %w", err))
	}
	return config
}

func decodeLondonSource(source *londonSource) error {
	if err := json.Unmarshal(londonSourceJSON, source); err != nil {
		return fmt.Errorf("decode London source: %w", err)
	}
	return nil
}

func londonNetwork(source londonSource) sim.Network {
	network := sim.Network{}
	positions := make(map[string]sim.Point, len(source.Stations))
	for _, station := range source.Stations {
		position := londonPoint(station.Latitude, station.Longitude)
		positions[station.ID] = position
		network.Nodes = append(network.Nodes, sim.Node{ID: londonJunctionID(station.ID), Position: position})
	}
	for index, link := range source.Links {
		a, aOK := positions[link.A]
		b, bOK := positions[link.B]
		if !aOK || !bOK || len(link.Lines) == 0 {
			panic(fmt.Sprintf("invalid London source link %q to %q", link.A, link.B))
		}
		addLondonCarriageway(&network, index, link, a, b)
	}
	for index, station := range source.Stations {
		direction := londonStationDirection(positions[station.ID], index)
		addLondonStation(&network, station.ID, station.Name, londonJunctionID(station.ID), direction, londonStationBerths, false)
	}
	for _, parking := range []londonParkingSource{
		{ID: "parking-west", Name: "West London Parking", Gateway: "940GZZLUHSD", Direction: math.Pi},
		{ID: "parking-north", Name: "North London Parking", Gateway: "940GZZLUFPK", Direction: -math.Pi / 2},
		{ID: "parking-east", Name: "East London Parking", Gateway: "940GZZLUMED", Direction: 0},
	} {
		if _, ok := positions[parking.Gateway]; !ok {
			panic(fmt.Sprintf("unknown London parking gateway %q", parking.Gateway))
		}
		addLondonStation(&network, parking.ID, parking.Name, londonJunctionID(parking.Gateway), parking.Direction, londonParkingBerths, true)
	}
	return network
}

func londonPoint(latitude, longitude float64) sim.Point {
	const earthRadiusMeters = 6_371_000.0
	referenceLatitude := londonReferenceLatitude * math.Pi / 180
	x := (longitude - londonReferenceLongitude) * math.Pi / 180 * earthRadiusMeters * math.Cos(referenceLatitude)
	y := -(latitude - londonReferenceLatitude) * math.Pi / 180 * earthRadiusMeters
	return sim.Point{X: x, Y: y}
}

func addLondonCarriageway(network *sim.Network, index int, link londonSourceLink, a, b sim.Point) {
	dx, dy := b.X-a.X, b.Y-a.Y
	length := math.Hypot(dx, dy)
	if length == 0 {
		panic(fmt.Sprintf("London source link %q to %q has zero length", link.A, link.B))
	}
	perpendicular := sim.Point{X: -dy * londonTrackOffset / length, Y: dx * londonTrackOffset / length}
	midpoint := sim.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
	abMidID := fmt.Sprintf("london-mid-%03d-a", index+1)
	baMidID := fmt.Sprintf("london-mid-%03d-b", index+1)
	network.Nodes = append(network.Nodes,
		sim.Node{ID: abMidID, Position: add(midpoint, perpendicular)},
		sim.Node{ID: baMidID, Position: add(midpoint, scale(perpendicular, -1))},
	)
	aID, bID := londonJunctionID(link.A), londonJunctionID(link.B)
	prefix := fmt.Sprintf("london-link-%03d", index+1)
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: prefix + "-ab-1", From: aID, To: abMidID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ab-2", From: abMidID, To: bID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ba-1", From: bID, To: baMidID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ba-2", From: baMidID, To: aID, SpeedLimit: speedLimit, SeparationGroup: prefix},
	)
}

func addLondonStation(network *sim.Network, id, name, junction string, direction float64, berths int, parking bool) {
	center, ok := network.Node(junction)
	if !ok {
		panic(fmt.Sprintf("unknown London station junction %q", junction))
	}
	outward := sim.Point{X: math.Cos(direction), Y: math.Sin(direction)}
	tangent := sim.Point{X: -outward.Y, Y: outward.X}
	divergeID, mergeID := id+"-diverge", id+"-merge"
	entryID, exitID := id+"-entry", id+"-exit"
	separationGroup := id + "-station"
	diverge := add(center.Position, add(scale(outward, 80), scale(tangent, -100)))
	merge := add(center.Position, add(scale(outward, 80), scale(tangent, 100)))
	entry := add(center.Position, add(scale(outward, 200), scale(tangent, -100)))
	exit := add(center.Position, add(scale(outward, 200), scale(tangent, 100)))
	network.Nodes = append(network.Nodes,
		sim.Node{ID: divergeID, Position: diverge},
		sim.Node{ID: mergeID, Position: merge},
		sim.Node{ID: entryID, Position: entry},
		sim.Node{ID: exitID, Position: exit},
	)
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: id + "-road-in", From: junction, To: divergeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
		sim.Lane{ID: id + "-access-in", From: divergeID, To: entryID, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
		sim.Lane{ID: id + "-through", From: entryID, To: exitID, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
		sim.Lane{ID: id + "-access-out", From: exitID, To: mergeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
		sim.Lane{ID: id + "-road-out", From: mergeID, To: junction, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
	)
	station := sim.Station{ID: id, Name: name, Entry: entryID, Exit: exitID, ParkingOnly: parking}
	for index := range berths {
		berthID := fmt.Sprintf("%s-%02d", id, index+1)
		berthNodeID := berthID + "-node"
		depth := 290.0 + 32*float64(index)
		berthPosition := add(center.Position, scale(outward, depth))
		network.Nodes = append(network.Nodes, sim.Node{ID: berthNodeID, Position: berthPosition})
		station.Berths = append(station.Berths, sim.Berth{ID: berthID, Node: berthNodeID, SeparationGroup: separationGroup})
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: berthID + "-in", From: entryID, To: berthNodeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
			sim.Lane{ID: berthID + "-out", From: berthNodeID, To: exitID, SpeedLimit: speedLimit, SeparationGroup: separationGroup},
		)
	}
	network.Stations = append(network.Stations, station)
}

func londonStationDirection(position sim.Point, index int) float64 {
	if math.Hypot(position.X, position.Y) >= 200 {
		return math.Atan2(position.Y, position.X)
	}
	return 2 * math.Pi * float64(index%12) / 12
}

func londonJunctionID(stationID string) string {
	return "london-junction-" + stationID
}

func londonFleet(network sim.Network) []sim.Placement {
	placements := make([]sim.Placement, 0, len(network.Stations)+18)
	for _, station := range network.Stations {
		count := 1
		if station.ParkingOnly {
			count = 6
		}
		for index := range count {
			placements = append(placements, sim.Placement{
				ID:        fmt.Sprintf("london-pod-%03d", len(placements)+1),
				StationID: station.ID,
				BerthID:   station.Berths[index].ID,
			})
		}
	}
	return placements
}
