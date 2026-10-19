package scenarios

import (
	_ "embed"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
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
	// londonStationBerths and londonStationPods are the default berth and
	// pod counts of a passenger station.
	londonStationBerths = 2
	londonStationPods   = 1
	// londonParkingPods is the default pod count of a Parking facility.
	londonParkingPods = 6
	// londonBerthPitch is the default distance between two berth rows.
	londonBerthPitch = 75.0
	// customCapacitySuffix ends the name of a generated project with other
	// than the default capacity.
	customCapacitySuffix = " (custom capacity)"
	// londonDepotLength is the longest depot at a Parking facility, the
	// distance between the entry and the exit of an ordinary station. The
	// heading search uses the ordinary shape, and a depot of this length
	// or less stays inside its area.
	londonDepotLength = 200.0
)

// LondonParkingBerths is the berth count of an ordinary London Parking
// facility, which has no depot.
const LondonParkingBerths = 12

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

// londonParkingFacilities lists the Parking facilities in network order.
var londonParkingFacilities = []londonParkingSource{
	{ID: "parking-west", Name: "West London Parking", Gateway: "940GZZLUHSD", Direction: math.Pi},
	{ID: "parking-north", Name: "North London Parking", Gateway: "940GZZLUFPK", Direction: -math.Pi / 2},
	{ID: "parking-east", Name: "East London Parking", Gateway: "940GZZLUMED", Direction: 0},
}

var (
	londonOnce   sync.Once
	londonPreset project.Config
)

// LondonCentral returns a geographically scaled central-LondonCentral qualification preset.
func LondonCentral() project.Config {
	londonOnce.Do(func() {
		londonPreset = mustLondonConfig()
	})
	return project.Clone(londonPreset)
}

// LondonOptions sets the capacity of the London stations. The Berths and
// Pods maps override the counts of single stations. A key is a TfL station
// ID, or parking-west, parking-north, or parking-east.
type LondonOptions struct {
	// StationBerths is the berth count of each passenger station.
	StationBerths int
	// ParkingBerths is the berth count of each Parking facility, or with
	// depots the slot count of each depot lane.
	ParkingBerths int
	// StationPods is the initial pod count of each passenger station.
	StationPods int
	// ParkingPods is the initial pod count of each Parking facility.
	ParkingPods int
	// Berths holds the berth count of single stations.
	Berths map[string]int
	// Pods holds the initial pod count of single stations.
	Pods map[string]int
	// BerthPitch is the distance in meters between two berth rows. It must
	// be at least minimumBerthPitch.
	BerthPitch float64
	// ParkingLanes is the number of depot lanes of each Parking facility.
	// Zero gives the ordinary facilities. With depots, the berth count of
	// a Parking facility is the slot count of each of its lanes.
	ParkingLanes int
	// CompactStations lists the TfL IDs of at most sim.MaxManeuverGroups
	// passenger stations with the four bays of the compact station in
	// place of their berth rows. An empty list gives ordinary stations.
	// The berth count of such a station sets only its shape in the heading
	// search. With a Compact station, each pod is Compact.
	CompactStations []string
}

// DefaultLondonCentralOptions returns the options of the London preset.
func DefaultLondonCentralOptions() LondonOptions {
	return LondonOptions{
		StationBerths: londonStationBerths, ParkingBerths: LondonParkingBerths,
		StationPods: londonStationPods, ParkingPods: londonParkingPods,
		BerthPitch: londonBerthPitch,
	}
}

// LondonCentralWith returns the London preset with the given station capacity.
// With options other than the defaults, the project name ends with
// " (custom capacity)", and a layout audit checks the network. The error
// names the station of the first hard conflict.
func LondonCentralWith(options LondonOptions) (project.Config, error) {
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		return project.Config{}, err
	}
	capacity, err := options.resolve(source)
	if err != nil {
		return project.Config{}, err
	}
	defaults, err := DefaultLondonCentralOptions().resolve(source)
	if err != nil {
		return project.Config{}, err
	}
	if capacity.equal(defaults) {
		return LondonCentral(), nil
	}
	config, err := londonConfig(source, capacity, false)
	if err != nil {
		return project.Config{}, err
	}
	if err := layoutError(config.Network, auditLondonLayout(config.Network, newLondonAuditInput(source))); err != nil {
		return project.Config{}, err
	}
	return config, nil
}

// londonCapacity holds the berth and pod counts of each London site, in
// network order: the passenger stations in source order, then the Parking
// facilities. With depots, the berth count of a Parking facility is the
// slot count of each of its lanes.
type londonCapacity struct {
	berths, pods []int
	pitch        float64
	// lanes is the depot lane count of each Parking facility, or 0 for
	// the ordinary facilities.
	lanes int
	// compact holds, for each site, whether it is a Compact station.
	compact []bool
}

// hasCompact reports whether a site is a Compact station.
func (capacity londonCapacity) hasCompact() bool {
	return slices.Contains(capacity.compact, true)
}

// depot returns the depot layout of the site at index, or false when the
// site is not a depot.
func (capacity londonCapacity) depot(index int) (depotLayout, bool) {
	if capacity.lanes == 0 || index < len(capacity.berths)-len(londonParkingFacilities) {
		return depotLayout{}, false
	}
	return depotLayout{lanes: capacity.lanes, slots: capacity.berths[index]}, true
}

// search returns the berth count of the site at index for the heading
// search. A depot uses the ordinary shape of a Parking facility, so the
// headings of all sites are those of the ordinary facilities.
func (capacity londonCapacity) search(index int) int {
	if _, ok := capacity.depot(index); ok {
		return LondonParkingBerths
	}
	return capacity.berths[index]
}

// sameLayout reports whether the capacity has the station layouts of the
// other capacity: the same depot lanes and the same Compact stations.
func (capacity londonCapacity) sameLayout(other londonCapacity) bool {
	compact := func(capacity londonCapacity) []int {
		var indexes []int
		for index, ok := range capacity.compact {
			if ok {
				indexes = append(indexes, index)
			}
		}
		return indexes
	}
	return capacity.lanes == other.lanes && slices.Equal(compact(capacity), compact(other))
}

// equal reports whether the capacity has the pitch, the berth and pod
// counts, and the station layouts of the other capacity.
func (capacity londonCapacity) equal(other londonCapacity) bool {
	return capacity.pitch == other.pitch && slices.Equal(capacity.berths, other.berths) && slices.Equal(capacity.pods, other.pods) && capacity.sameLayout(other)
}

// resolve returns the berth and pod counts of each site. It checks the
// station IDs, the counts, and the pitch.
func (options LondonOptions) resolve(source londonSource) (londonCapacity, error) {
	if err := checkBerthPitch(options.BerthPitch); err != nil {
		return londonCapacity{}, err
	}
	ids := make([]string, 0, len(source.Stations)+len(londonParkingFacilities))
	names := make([]string, 0, cap(ids))
	for _, station := range source.Stations {
		ids, names = append(ids, station.ID), append(names, station.Name)
	}
	for _, parking := range londonParkingFacilities {
		ids, names = append(ids, parking.ID), append(names, parking.Name)
	}
	for _, overrides := range []map[string]int{options.Berths, options.Pods} {
		for _, id := range slices.Sorted(maps.Keys(overrides)) {
			if !slices.Contains(ids, id) {
				return londonCapacity{}, fmt.Errorf("unknown London station ID %q", id)
			}
		}
	}
	if options.ParkingLanes < 0 {
		return londonCapacity{}, errors.New("parking lanes must not be negative")
	}
	capacity := londonCapacity{
		berths: make([]int, len(ids)), pods: make([]int, len(ids)), pitch: options.BerthPitch, lanes: options.ParkingLanes,
		compact: make([]bool, len(ids)),
	}
	for _, id := range options.CompactStations {
		index := slices.Index(ids, id)
		switch {
		case index < 0:
			return londonCapacity{}, fmt.Errorf("unknown London station ID %q", id)
		case index >= len(source.Stations):
			return londonCapacity{}, fmt.Errorf("%s (%s) is a Parking facility, not a passenger station for Compact bays", names[index], id)
		case capacity.compact[index]:
			return londonCapacity{}, fmt.Errorf("%s (%s) is listed more than once as a Compact station", names[index], id)
		}
		capacity.compact[index] = true
	}
	if len(options.CompactStations) > sim.MaxManeuverGroups {
		return londonCapacity{}, fmt.Errorf("%d Compact stations, want at most %d", len(options.CompactStations), sim.MaxManeuverGroups)
	}
	total := 0
	for index, id := range ids {
		berths, pods := options.StationBerths, options.StationPods
		if index >= len(source.Stations) {
			berths, pods = options.ParkingBerths, options.ParkingPods
		}
		if value, ok := options.Berths[id]; ok {
			berths = value
		}
		if value, ok := options.Pods[id]; ok {
			pods = value
		}
		capacity.berths[index], capacity.pods[index] = berths, pods
		limit, err := capacity.berthLimit(index)
		if err != nil {
			return londonCapacity{}, fmt.Errorf("%s (%s): %w", names[index], id, err)
		}
		if limit == 0 {
			return londonCapacity{}, fmt.Errorf("%s (%s) has %d berths, want 1 to %d", names[index], id, berths, project.MaxBerths)
		}
		if pods < 0 || pods > limit {
			return londonCapacity{}, fmt.Errorf("%s (%s) has %d pods, want 0 to its %d berths", names[index], id, pods, limit)
		}
		total += pods
	}
	if total < 1 || total > project.MaxPods {
		return londonCapacity{}, fmt.Errorf("fleet has %d pods, want 1 to %d", total, project.MaxPods)
	}
	return capacity, nil
}

// berthLimit returns the number of berths of the site at index, or 0 when
// its berth count is out of range. For a depot, it checks the layout and
// then the length, and the limit is the slot count of the depot. A Compact
// station has the bays of the compact station.
func (capacity londonCapacity) berthLimit(index int) (int, error) {
	berths := capacity.berths[index]
	if layout, ok := capacity.depot(index); ok {
		if err := checkDepotLayout(layout); err != nil {
			return 0, err
		}
		if length := layout.length(); length > londonDepotLength {
			return 0, fmt.Errorf("the depot of %d lanes of %d slots is %.2f meters long, more than %.0f meters", layout.lanes, layout.slots, length, londonDepotLength)
		}
		return layout.lanes * layout.slots, nil
	}
	if berths < 1 || berths > project.MaxBerths {
		return 0, nil
	}
	if capacity.compact[index] {
		return len(compactBays), nil
	}
	return berths, nil
}

func mustLondonConfig() project.Config {
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		panic(err)
	}
	capacity, err := DefaultLondonCentralOptions().resolve(source)
	if err != nil {
		panic(err)
	}
	config, err := londonConfig(source, capacity, true)
	if err != nil {
		panic(err)
	}
	return config
}

// londonConfig builds and validates the London project with the capacity.
// Without defaultCapacity, the project name ends with customCapacitySuffix.
func londonConfig(source londonSource, capacity londonCapacity, defaultCapacity bool) (project.Config, error) {
	network, err := londonNetwork(source, capacity)
	if err != nil {
		return project.Config{}, err
	}
	name := "LondonCentral"
	if !defaultCapacity {
		name += customCapacitySuffix
	}
	config := project.Config{
		Version: 1,
		Name:    name,
		Network: network,
		Demand: project.DemandConfig{
			PerMinute: 20,
			Pattern:   "profile",
			Seed:      20260922,
			Profile:   londonDemandProfileID,
			Band:      "am-peak",
		},
		DemandProfiles: []project.DemandProfile{londonDemandProfile()},
		// Virtual platoons raise the Early limit, where queues on the
		// track set the limit. See the London capacity envelope in the
		// qualification record.
		PlatoonLimit: sim.MaxPlatoonLimit,
		// The editor places a map image with this reference, which is the
		// reference of londonPoint.
		Geo: &project.Geo{Latitude: londonReferenceLatitude, Longitude: londonReferenceLongitude, Projection: project.GeoProjection, Radius: project.GeoRadius},
	}
	config.Fleet = londonFleet(config.Network, capacity)
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate London scenario: %w", err)
	}
	return config, nil
}

func decodeLondonSource(source *londonSource) error {
	if err := json.Unmarshal(londonSourceJSON, source); err != nil {
		return fmt.Errorf("decode London source: %w", err)
	}
	return nil
}

// londonNetwork builds the guideways first. Then it gives each station a
// heading from searchLondonHeadings and builds the stations. Before the
// search, it checks that the network stays in the project node and lane
// limits.
func londonNetwork(source londonSource, capacity londonCapacity) (sim.Network, error) {
	return londonNetworkWithHeadings(source, capacity, nil, projectLondonLimits)
}

// londonLimits holds the node and lane limits of the London size check.
type londonLimits struct {
	nodes, lanes int
}

// projectLondonLimits holds the project node and lane limits.
var projectLondonLimits = londonLimits{nodes: project.MaxNodes, lanes: project.MaxLanes}

// londonNetworkWithHeadings applies preset-specific headings after the shared search.
// The size checks and station construction are the same for both presets.
// The presets check the size with projectLondonLimits.
func londonNetworkWithHeadings(source londonSource, capacity londonCapacity, overrides map[string]float64, limits londonLimits) (sim.Network, error) {
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
	facilities := londonParkingFacilities
	if err := checkLondonSize(network, source, portals, capacity, limits); err != nil {
		return sim.Network{}, err
	}
	input := londonHeadingInput{links: londonLaneSegments(network)}
	indexes := make(map[string]int, len(source.Stations))
	for index, station := range source.Stations {
		center := positions[station.ID]
		indexes[station.ID] = index
		input.links = append(input.links, londonMovementSegments(center, *portals[station.ID])...)
		input.centers = append(input.centers, center)
		input.sites = append(input.sites, londonSite{
			center: center, berths: capacity.berths[index], pitch: capacity.pitch, own: index,
			arrivals:   londonPortalPositions(portals[station.ID].arrivals),
			departures: londonPortalPositions(portals[station.ID].departures),
			preferred:  londonStationPreferences(center, neighbors[station.ID]),
		})
	}
	for index, parking := range facilities {
		gateway, ok := indexes[parking.Gateway]
		if !ok {
			panic(fmt.Sprintf("unknown London parking gateway %q", parking.Gateway))
		}
		input.sites = append(input.sites, londonSite{
			center: positions[parking.Gateway], berths: capacity.search(len(source.Stations) + index), pitch: capacity.pitch, own: gateway,
			arrivals:   londonPortalPositions(portals[parking.Gateway].arrivals),
			departures: londonPortalPositions(portals[parking.Gateway].departures),
			preferred:  []londonPreference{{direction: parking.Direction}},
		})
	}
	headings := searchLondonHeadings(input)
	for id, direction := range overrides {
		headings[indexes[id]] = direction
	}
	shapes := mirrorLondonStations(input, headings)
	for index, station := range source.Stations {
		addLondonMovements(&network, station.ID, positions[station.ID], *portals[station.ID])
		addLondonStation(&network, londonStationInput{
			id: station.ID, name: station.Name, portals: *portals[station.ID],
			shape: shapes[index], compact: capacity.compact[index],
		})
	}
	for index, parking := range facilities {
		site := len(source.Stations) + index
		depot, _ := capacity.depot(site)
		addLondonStation(&network, londonStationInput{
			id: parking.ID, name: parking.Name, portals: *portals[parking.Gateway], parking: true,
			shape: shapes[site], depot: depot,
		})
	}
	return network, nil
}

// checkLondonSize returns an error when the London network with the
// capacity has more nodes or lanes than the project limits. The network
// holds the guideways only. Each station adds its diverge, entry, exit,
// and merge nodes, and 3 nodes for each berth. Each passenger station adds
// a movement lane from each arrival portal to each departure portal. Each
// station adds a road lane to or from each portal, 3 lanes from the
// diverge node to the merge node, and 4 lanes for each berth. A depot has
// the nodes and lanes of its layout in place of the berths, and a Compact
// station has those of the compact station in place of the berths and the
// through lane.
func checkLondonSize(network sim.Network, source londonSource, portals map[string]*londonStationPortals, capacity londonCapacity, limits londonLimits) error {
	nodes, lanes := len(network.Nodes), len(network.Lanes)
	compact := compactStation(nil, "", "")
	for index, berths := range capacity.berths {
		var station londonStationPortals
		if index < len(source.Stations) {
			station = *portals[source.Stations[index].ID]
			lanes += len(station.arrivals) * len(station.departures)
		} else {
			station = *portals[londonParkingFacilities[index-len(source.Stations)].Gateway]
		}
		nodes, lanes = nodes+4, lanes+len(station.arrivals)+len(station.departures)+3
		if layout, ok := capacity.depot(index); ok {
			nodes, lanes = nodes+layout.nodes(), lanes+layout.laneCount()
			continue
		}
		if capacity.compact[index] {
			// The compact layout has the entry and exit nodes and the
			// lanes between them.
			nodes, lanes = nodes+len(compact.nodes)-2, lanes+len(compact.lanes)-1
			continue
		}
		nodes += 3 * berths
		lanes += 4 * berths
	}
	if nodes > limits.nodes {
		return fmt.Errorf("generated London network needs %d nodes, more than the limit of %d", nodes, limits.nodes)
	}
	if lanes > limits.lanes {
		return fmt.Errorf("generated London network needs %d lanes, more than the limit of %d", lanes, limits.lanes)
	}
	return nil
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

// londonPoint gives the world position of a latitude and a longitude with
// the projection of project.Geo at the London reference.
func londonPoint(latitude, longitude float64) sim.Point {
	const earthRadiusMeters = project.GeoRadius
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
	// depot is the depot layout of a Parking facility, or zero for berth
	// rows.
	depot depotLayout
	// compact gives a passenger station the bays of the compact station.
	compact bool
}

// addLondonStation adds a station with its roads. A station has berth
// rows, or a depot or Compact bays in place of them. The depot lies
// outward from its through lane, and its entry and exit are half its length
// to each side of the station axis, 200 meters out. The compact station
// replaces the through lane as a rigid body: its aisle is 200 meters out,
// its entry and exit are equally far to each side of the station axis, its
// bays lie outward of the aisle, and its bypass curves toward the throat.
func addLondonStation(network *sim.Network, input londonStationInput) {
	id := input.id
	positions := input.shape.nodes()
	var at func(along, across float64) sim.Point
	if input.depot.lanes != 0 {
		frame, half := input.shape.frame(), input.depot.length()/2
		at = func(along, across float64) sim.Point { return frame.position(along-half, 200+across) }
		positions.entry, positions.exit, positions.berths = at(0, 0), at(input.depot.length(), 0), nil
	}
	var compact compactLayout
	if input.compact {
		frame, middle := input.shape.frame(), (compactHoldX+compactReceiveX)/2
		compact = compactStation(func(point sim.Point) sim.Point {
			return frame.position(point.X-middle, 200+point.Y-compactAisleY)
		}, id+"-", id)
		// The first four nodes are the entry, holding, receiving, and
		// exit nodes.
		positions.entry, positions.exit, positions.berths = compact.nodes[0].Position, compact.nodes[3].Position, nil
	}
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
	access := []sim.Lane{
		{ID: id + "-access-in", From: divergeID, To: entryID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationEntryRole},
		{ID: id + "-through", From: entryID, To: exitID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationThroughRole},
		{ID: id + "-access-out", From: exitID, To: mergeID, SpeedLimit: speedLimit, SeparationGroup: separationGroup, StationID: id, StationRole: sim.StationExitRole},
	}
	if input.compact {
		access = slices.Delete(access, 1, 2)
	}
	network.Lanes = append(network.Lanes, access...)
	for index, portal := range input.portals.departures {
		laneID := fmt.Sprintf("%s-road-out-%02d", id, index+1)
		network.Lanes = append(network.Lanes, sim.Lane{
			ID: laneID, From: mergeID, To: portal.node,
			SpeedLimit: speedLimit, SeparationGroup: laneID, StationID: id, StationRole: sim.StationExitRole,
		})
	}
	station := sim.Station{ID: id, Name: input.name, Entry: entryID, Exit: exitID, ParkingOnly: input.parking}
	if at != nil {
		depotStation, nodes, lanes, depot := depotGeometry(input.depot, depotPlacement{at: at, station: station, prefix: id})
		network.Nodes, network.Lanes = append(network.Nodes, nodes...), append(network.Lanes, lanes...)
		network.Stations, network.Depots = append(network.Stations, depotStation), append(network.Depots, depot)
		return
	}
	if input.compact {
		addLondonCompact(network, station, compact)
		return
	}
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

// addLondonCompact adds the nodes, the lanes, the bays, and the maneuver
// group of a Compact station. The entry and exit nodes are already in the
// network. The approach, the bypass, and the leave lane get the station
// roles of berth access, through, and departure. The group lanes have no
// station and no role.
func addLondonCompact(network *sim.Network, station sim.Station, compact compactLayout) {
	roles := map[string]sim.StationLaneRole{
		station.ID + "-approach": sim.StationBerthAccessRole, station.ID + "-bypass": sim.StationThroughRole, station.ID + "-leave": sim.StationDepartureRole,
	}
	network.Nodes = append(network.Nodes, compact.nodes[1], compact.nodes[2])
	network.Nodes = append(network.Nodes, compact.nodes[4:]...)
	for _, lane := range compact.lanes {
		if role, ok := roles[lane.ID]; ok {
			lane.StationID, lane.StationRole = station.ID, role
		}
		network.Lanes = append(network.Lanes, lane)
	}
	station.Berths = compact.berths
	network.Stations = append(network.Stations, station)
	network.ManeuverGroups = append(network.ManeuverGroups, compact.group)
}

// londonFleet places pods on the first berths of each station, and on the
// heads of the lanes of a depot first (see depotPlacements). The capacity
// holds the pod count of each station, in network order. With a Compact
// station, each pod is Compact.
func londonFleet(network sim.Network, capacity londonCapacity) []sim.Placement {
	pods := capacity.pods
	total := 0
	for _, count := range pods {
		total += count
	}
	placements := make([]sim.Placement, 0, total)
	for stationIndex, station := range network.Stations {
		if layout, ok := capacity.depot(stationIndex); ok {
			first := len(placements)
			placements = append(placements, depotPlacements(layout, station.ID, pods[stationIndex], func(index int) string {
				return fmt.Sprintf("london-pod-%03d", first+index+1)
			})...)
			continue
		}
		for index := range pods[stationIndex] {
			placements = append(placements, sim.Placement{
				ID:        fmt.Sprintf("london-pod-%03d", len(placements)+1),
				StationID: station.ID,
				BerthID:   station.Berths[index].ID,
			})
		}
	}
	if capacity.hasCompact() {
		for index := range placements {
			placements[index].Class = sim.CompactClass
		}
	}
	return placements
}
