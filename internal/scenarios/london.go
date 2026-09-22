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
	londonPortalInset        = 60.0
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

type londonPortal struct {
	node string
}

type londonStationPortals struct {
	arrivals, departures []londonPortal
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
			Pattern:   "profile",
			Seed:      20260922,
			Profile:   londonDemandProfileID,
			Band:      "am-peak",
		},
		DemandProfiles: []project.DemandProfile{londonDemandProfile()},
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
	portals := make(map[string]*londonStationPortals, len(source.Stations))
	for _, station := range source.Stations {
		position := londonPoint(station.Latitude, station.Longitude)
		positions[station.ID] = position
		portals[station.ID] = &londonStationPortals{}
	}
	for index, link := range source.Links {
		a, aOK := positions[link.A]
		b, bOK := positions[link.B]
		if !aOK || !bOK || len(link.Lines) == 0 {
			panic(fmt.Sprintf("invalid London source link %q to %q", link.A, link.B))
		}
		addLondonCarriageway(&network, portals, index, link, a, b)
	}
	for index, station := range source.Stations {
		direction := londonStationDirection(positions[station.ID], index)
		addLondonMovements(&network, station.ID, positions[station.ID], *portals[station.ID])
		addLondonStation(&network, station.ID, station.Name, positions[station.ID], direction, *portals[station.ID], londonStationBerths, false)
	}
	for _, parking := range []londonParkingSource{
		{ID: "parking-west", Name: "West London Parking", Gateway: "940GZZLUHSD", Direction: math.Pi},
		{ID: "parking-north", Name: "North London Parking", Gateway: "940GZZLUFPK", Direction: -math.Pi / 2},
		{ID: "parking-east", Name: "East London Parking", Gateway: "940GZZLUMED", Direction: 0},
	} {
		if _, ok := positions[parking.Gateway]; !ok {
			panic(fmt.Sprintf("unknown London parking gateway %q", parking.Gateway))
		}
		addLondonStation(&network, parking.ID, parking.Name, positions[parking.Gateway], parking.Direction, *portals[parking.Gateway], londonParkingBerths, true)
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

func addLondonCarriageway(network *sim.Network, portals map[string]*londonStationPortals, index int, link londonSourceLink, a, b sim.Point) {
	dx, dy := b.X-a.X, b.Y-a.Y
	length := math.Hypot(dx, dy)
	if length == 0 {
		panic(fmt.Sprintf("London source link %q to %q has zero length", link.A, link.B))
	}
	direction := sim.Point{X: dx / length, Y: dy / length}
	perpendicular := sim.Point{X: -direction.Y * londonTrackOffset, Y: direction.X * londonTrackOffset}
	midpoint := sim.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
	inset := min(londonPortalInset, length/4)
	aPortalCenter := add(a, scale(direction, inset))
	bPortalCenter := add(b, scale(direction, -inset))
	abMidID := fmt.Sprintf("london-mid-%03d-a", index+1)
	baMidID := fmt.Sprintf("london-mid-%03d-b", index+1)
	prefix := fmt.Sprintf("london-link-%03d", index+1)
	aDepartureID, aArrivalID := prefix+"-a-departure", prefix+"-a-arrival"
	bDepartureID, bArrivalID := prefix+"-b-departure", prefix+"-b-arrival"
	network.Nodes = append(network.Nodes,
		sim.Node{ID: abMidID, Position: add(midpoint, perpendicular)},
		sim.Node{ID: baMidID, Position: add(midpoint, scale(perpendicular, -1))},
		sim.Node{ID: aDepartureID, Position: add(aPortalCenter, perpendicular)},
		sim.Node{ID: aArrivalID, Position: add(aPortalCenter, scale(perpendicular, -1))},
		sim.Node{ID: bDepartureID, Position: add(bPortalCenter, scale(perpendicular, -1))},
		sim.Node{ID: bArrivalID, Position: add(bPortalCenter, perpendicular)},
	)
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: prefix + "-ab-1", From: aDepartureID, To: abMidID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ab-2", From: abMidID, To: bArrivalID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ba-1", From: bDepartureID, To: baMidID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ba-2", From: baMidID, To: aArrivalID, SpeedLimit: speedLimit, SeparationGroup: prefix},
	)
	portals[link.A].departures = append(portals[link.A].departures, londonPortal{node: aDepartureID})
	portals[link.A].arrivals = append(portals[link.A].arrivals, londonPortal{node: aArrivalID})
	portals[link.B].departures = append(portals[link.B].departures, londonPortal{node: bDepartureID})
	portals[link.B].arrivals = append(portals[link.B].arrivals, londonPortal{node: bArrivalID})
}

func addLondonMovements(network *sim.Network, stationID string, center sim.Point, portals londonStationPortals) {
	for arrivalIndex, arrival := range portals.arrivals {
		for departureIndex, departure := range portals.departures {
			id := fmt.Sprintf("%s-move-%02d-%02d", stationID, arrivalIndex+1, departureIndex+1)
			var control *sim.Point
			from, _ := network.Node(arrival.node)
			to, _ := network.Node(departure.node)
			if math.Hypot(to.Position.X-from.Position.X, to.Position.Y-from.Position.Y) < 2*sim.Clearance {
				point := center
				control = &point
			}
			network.Lanes = append(network.Lanes, sim.Lane{
				ID: id, From: arrival.node, To: departure.node,
				SpeedLimit: speedLimit, SeparationGroup: id, Control: control,
			})
		}
	}
}

func addLondonStation(network *sim.Network, id, name string, center sim.Point, direction float64, portals londonStationPortals, berths int, parking bool) {
	outward := sim.Point{X: math.Cos(direction), Y: math.Sin(direction)}
	tangent := sim.Point{X: -outward.Y, Y: outward.X}
	divergeID, mergeID := id+"-diverge", id+"-merge"
	entryID, exitID := id+"-entry", id+"-exit"
	separationGroup := id + "-station"
	diverge := add(center, add(scale(outward, 80), scale(tangent, -100)))
	merge := add(center, add(scale(outward, 80), scale(tangent, 100)))
	entry := add(center, add(scale(outward, 200), scale(tangent, -100)))
	exit := add(center, add(scale(outward, 200), scale(tangent, 100)))
	network.Nodes = append(network.Nodes,
		sim.Node{ID: divergeID, Position: diverge},
		sim.Node{ID: mergeID, Position: merge},
		sim.Node{ID: entryID, Position: entry},
		sim.Node{ID: exitID, Position: exit},
	)
	for index, portal := range portals.arrivals {
		laneID := fmt.Sprintf("%s-road-in-%02d", id, index+1)
		network.Lanes = append(network.Lanes, sim.Lane{
			ID: laneID, From: portal.node, To: divergeID,
			SpeedLimit: speedLimit, SeparationGroup: laneID, StationID: id, StationRole: sim.StationApproachRole,
		})
	}
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: id + "-access-in", From: divergeID, To: entryID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationEntryRole},
		sim.Lane{ID: id + "-through", From: entryID, To: exitID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationThroughRole},
		sim.Lane{ID: id + "-access-out", From: exitID, To: mergeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationExitRole},
	)
	for index, portal := range portals.departures {
		laneID := fmt.Sprintf("%s-road-out-%02d", id, index+1)
		network.Lanes = append(network.Lanes, sim.Lane{
			ID: laneID, From: mergeID, To: portal.node,
			SpeedLimit: speedLimit, SeparationGroup: laneID, StationID: id, StationRole: sim.StationExitRole,
		})
	}
	station := sim.Station{ID: id, Name: name, Entry: entryID, Exit: exitID, ParkingOnly: parking}
	previousArrival, previousDeparture := entryID, exitID
	for index := range berths {
		berthID := fmt.Sprintf("%s-%02d", id, index+1)
		berthNodeID := berthID + "-node"
		arrivalNodeID, departureNodeID := berthID+"-arrival", berthID+"-departure"
		depth := 290.0 + 75*float64(index)
		berthPosition := add(center, scale(outward, depth))
		network.Nodes = append(network.Nodes,
			sim.Node{ID: arrivalNodeID, Position: add(berthPosition, scale(tangent, -100))},
			sim.Node{ID: berthNodeID, Position: berthPosition},
			sim.Node{ID: departureNodeID, Position: add(berthPosition, scale(tangent, 100))},
		)
		station.Berths = append(station.Berths, sim.Berth{ID: berthID, Node: berthNodeID, SeparationGroup: separationGroup})
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: berthID + "-arrival-link", From: previousArrival, To: arrivalNodeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationBerthAccessRole},
			sim.Lane{ID: berthID + "-departure-link", From: departureNodeID, To: previousDeparture, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationDepartureRole},
			sim.Lane{ID: berthID + "-in", From: arrivalNodeID, To: berthNodeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationBerthAccessRole},
			sim.Lane{ID: berthID + "-out", From: berthNodeID, To: departureNodeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationDepartureRole},
		)
		previousArrival, previousDeparture = arrivalNodeID, departureNodeID
	}
	network.Stations = append(network.Stations, station)
}

func londonStationDirection(position sim.Point, index int) float64 {
	if math.Hypot(position.X, position.Y) >= 200 {
		return math.Atan2(position.Y, position.X)
	}
	return 2 * math.Pi * float64(index%12) / 12
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
