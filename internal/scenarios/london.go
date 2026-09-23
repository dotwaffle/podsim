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
	// londonStationThroatOffset is the distance of the diverge and merge
	// nodes from the station axis. It is less than londonPortalInset, so pods
	// do not make a hairpin turn between a guideway and the station.
	londonStationThroatOffset = 30.0
	londonStationBerths       = 2
	londonParkingBerths       = 12
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

// londonParkingSource is a Parking facility at a gateway station. Direction
// is the preferred heading of its berth rows.
type londonParkingSource struct {
	ID, Name, Gateway string
	Direction         float64
}

type londonPortal struct {
	node     string
	position sim.Point
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

// londonNetwork builds the guideways first. Then it gives each station a
// heading from searchLondonHeadings and builds the stations.
func londonNetwork(source londonSource) sim.Network {
	network := sim.Network{}
	positions := make(map[string]sim.Point, len(source.Stations))
	portals := make(map[string]*londonStationPortals, len(source.Stations))
	for _, station := range source.Stations {
		position := londonPoint(station.Latitude, station.Longitude)
		positions[station.ID] = position
		portals[station.ID] = &londonStationPortals{}
	}
	neighbors := make(map[string][]sim.Point, len(source.Stations))
	for index, link := range source.Links {
		a, aOK := positions[link.A]
		b, bOK := positions[link.B]
		if !aOK || !bOK || len(link.Lines) == 0 {
			panic(fmt.Sprintf("invalid London source link %q to %q", link.A, link.B))
		}
		addLondonCarriageway(&network, portals, index, link, a, b)
		neighbors[link.A] = append(neighbors[link.A], b)
		neighbors[link.B] = append(neighbors[link.B], a)
	}
	facilities := []londonParkingSource{
		{ID: "parking-west", Name: "West London Parking", Gateway: "940GZZLUHSD", Direction: math.Pi},
		{ID: "parking-north", Name: "North London Parking", Gateway: "940GZZLUFPK", Direction: -math.Pi / 2},
		{ID: "parking-east", Name: "East London Parking", Gateway: "940GZZLUMED", Direction: 0},
	}
	input := londonHeadingInput{links: londonLaneSegments(network)}
	indexes := make(map[string]int, len(source.Stations))
	for index, station := range source.Stations {
		center := positions[station.ID]
		indexes[station.ID] = index
		input.links = append(input.links, londonMovementSegments(center, *portals[station.ID])...)
		input.centers = append(input.centers, center)
		input.sites = append(input.sites, londonSite{
			center: center, berths: londonStationBerths, own: index,
			arrivals:   londonPortalPositions(portals[station.ID].arrivals),
			departures: londonPortalPositions(portals[station.ID].departures),
			preferred:  londonStationPreferences(center, neighbors[station.ID]),
		})
	}
	for _, parking := range facilities {
		gateway, ok := indexes[parking.Gateway]
		if !ok {
			panic(fmt.Sprintf("unknown London parking gateway %q", parking.Gateway))
		}
		input.sites = append(input.sites, londonSite{
			center: positions[parking.Gateway], berths: londonParkingBerths, own: gateway,
			arrivals:   londonPortalPositions(portals[parking.Gateway].arrivals),
			departures: londonPortalPositions(portals[parking.Gateway].departures),
			preferred:  []londonPreference{{direction: parking.Direction}},
		})
	}
	headings := searchLondonHeadings(input)
	for index, station := range source.Stations {
		addLondonMovements(&network, station.ID, positions[station.ID], *portals[station.ID])
		addLondonStation(&network, londonStationInput{
			id: station.ID, name: station.Name, portals: *portals[station.ID],
			shape: input.sites[index].shape(headings[index]),
		})
	}
	for index, parking := range facilities {
		site := len(source.Stations) + index
		addLondonStation(&network, londonStationInput{
			id: parking.ID, name: parking.Name, portals: *portals[parking.Gateway], parking: true,
			shape: input.sites[site].shape(headings[site]),
		})
	}
	return network
}

// londonLaneSegments returns each lane of the network as a straight
// segment. It ignores control points, so use it only for straight lanes.
func londonLaneSegments(network sim.Network) []londonSegment {
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	segments := make([]londonSegment, 0, len(network.Lanes))
	for _, lane := range network.Lanes {
		segments = append(segments, londonSegment{from: nodes[lane.From], to: nodes[lane.To]})
	}
	return segments
}

func londonPortalPositions(portals []londonPortal) []sim.Point {
	positions := make([]sim.Point, 0, len(portals))
	for _, portal := range portals {
		positions = append(positions, portal.position)
	}
	return positions
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
	aDeparture := londonPortal{node: aDepartureID, position: add(aPortalCenter, perpendicular)}
	aArrival := londonPortal{node: aArrivalID, position: add(aPortalCenter, scale(perpendicular, -1))}
	bDeparture := londonPortal{node: bDepartureID, position: add(bPortalCenter, scale(perpendicular, -1))}
	bArrival := londonPortal{node: bArrivalID, position: add(bPortalCenter, perpendicular)}
	network.Nodes = append(network.Nodes,
		sim.Node{ID: abMidID, Position: add(midpoint, perpendicular)},
		sim.Node{ID: baMidID, Position: add(midpoint, scale(perpendicular, -1))},
		sim.Node{ID: aDepartureID, Position: aDeparture.position},
		sim.Node{ID: aArrivalID, Position: aArrival.position},
		sim.Node{ID: bDepartureID, Position: bDeparture.position},
		sim.Node{ID: bArrivalID, Position: bArrival.position},
	)
	network.Lanes = append(network.Lanes,
		sim.Lane{ID: prefix + "-ab-1", From: aDepartureID, To: abMidID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ab-2", From: abMidID, To: bArrivalID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ba-1", From: bDepartureID, To: baMidID, SpeedLimit: speedLimit, SeparationGroup: prefix},
		sim.Lane{ID: prefix + "-ba-2", From: baMidID, To: aArrivalID, SpeedLimit: speedLimit, SeparationGroup: prefix},
	)
	portals[link.A].departures = append(portals[link.A].departures, aDeparture)
	portals[link.A].arrivals = append(portals[link.A].arrivals, aArrival)
	portals[link.B].departures = append(portals[link.B].departures, bDeparture)
	portals[link.B].arrivals = append(portals[link.B].arrivals, bArrival)
}

func addLondonMovements(network *sim.Network, stationID string, center sim.Point, portals londonStationPortals) {
	for arrivalIndex, arrival := range portals.arrivals {
		for departureIndex, departure := range portals.departures {
			id := fmt.Sprintf("%s-move-%02d-%02d", stationID, arrivalIndex+1, departureIndex+1)
			network.Lanes = append(network.Lanes, sim.Lane{
				ID: id, From: arrival.node, To: departure.node,
				SpeedLimit: speedLimit, SeparationGroup: id,
				Control: londonMovementControl(arrival.position, departure.position, center),
			})
		}
	}
}

// londonMovementControl returns the control point of a movement lane
// between two portals. When the portals are nearer than 2*sim.Clearance, the
// lane curves through the station position. Otherwise it is straight and the
// control point is nil.
func londonMovementControl(arrival, departure, center sim.Point) *sim.Point {
	if math.Hypot(departure.X-arrival.X, departure.Y-arrival.Y) < 2*sim.Clearance {
		return new(center)
	}
	return nil
}

// londonMovementSegments returns the movement lanes of a station as
// segments, before addLondonMovements adds the lanes. A curved lane gets two
// segments through its control point. The curve lies between these
// segments and the straight line from its start to its end.
func londonMovementSegments(center sim.Point, portals londonStationPortals) []londonSegment {
	var segments []londonSegment
	for _, arrival := range portals.arrivals {
		for _, departure := range portals.departures {
			if control := londonMovementControl(arrival.position, departure.position, center); control != nil {
				segments = append(segments, londonSegment{from: arrival.position, to: *control}, londonSegment{from: *control, to: departure.position})
				continue
			}
			segments = append(segments, londonSegment{from: arrival.position, to: departure.position})
		}
	}
	return segments
}

// londonStationInput holds the values of one station for addLondonStation.
type londonStationInput struct {
	id, name string
	shape    londonStationShape
	portals  londonStationPortals
	parking  bool
}

func addLondonStation(network *sim.Network, input londonStationInput) {
	id := input.id
	positions := input.shape.nodes()
	divergeID, mergeID := id+"-diverge", id+"-merge"
	entryID, exitID := id+"-entry", id+"-exit"
	separationGroup := id + "-station"
	network.Nodes = append(network.Nodes,
		sim.Node{ID: divergeID, Position: positions.diverge},
		sim.Node{ID: mergeID, Position: positions.merge},
		sim.Node{ID: entryID, Position: positions.entry},
		sim.Node{ID: exitID, Position: positions.exit},
	)
	for index, portal := range input.portals.arrivals {
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
	for index, portal := range input.portals.departures {
		laneID := fmt.Sprintf("%s-road-out-%02d", id, index+1)
		network.Lanes = append(network.Lanes, sim.Lane{
			ID: laneID, From: mergeID, To: portal.node,
			SpeedLimit: speedLimit, SeparationGroup: laneID, StationID: id, StationRole: sim.StationExitRole,
		})
	}
	station := sim.Station{ID: id, Name: input.name, Entry: entryID, Exit: exitID, ParkingOnly: input.parking}
	previousArrival, previousDeparture := entryID, exitID
	for index, berth := range positions.berths {
		berthID := fmt.Sprintf("%s-%02d", id, index+1)
		berthNodeID := berthID + "-node"
		arrivalNodeID, departureNodeID := berthID+"-arrival", berthID+"-departure"
		network.Nodes = append(network.Nodes,
			sim.Node{ID: arrivalNodeID, Position: berth.arrival},
			sim.Node{ID: berthNodeID, Position: berth.berth},
			sim.Node{ID: departureNodeID, Position: berth.departure},
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
