// Package project defines the portable Podsim project format.
package project

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	currentVersion = 1
	maxIDLength    = 64
	maxNameLength  = 80
)

// These are the largest counts that Validate accepts. The saved session
// decoder and web/editor.js use the same limits. The maximum saved-state
// fixture includes the widest routes and a MaxFileBytes project member.
// TestStateFileWorstCaseSize must pass after any count or byte limit changes.
const (
	// MaxPods is the largest fleet.
	MaxPods = 300
	// MaxBerths is the largest number of berths in one station.
	MaxBerths = 200
	// MaxStations is the largest number of stations.
	MaxStations = 300
	// MaxNodes is the largest number of network nodes.
	MaxNodes = 5000
	// MaxLanes is the largest number of network lanes.
	MaxLanes = 8000
	// MaxNodeLanes is the largest number of lanes at one node. A lane
	// counts at its From node and at its To node.
	MaxNodeLanes = 64
	// MaxJunctionPairs is the largest total of the lane pairs that
	// sim.Network.JunctionPairs counts at the nodes. The simulator compares
	// each of these pairs when it starts, so the limit bounds the start
	// time. The largest total of a generated project in the tests is
	// 32,752, in a ring of 4 stations with 62 berths each. A network with
	// MaxNodes nodes and MaxLanes lanes has at least 48,000 pairs, so the
	// limit also leaves space for a network with the most lanes.
	MaxJunctionPairs = 100_000
	// MaxProfiles is the largest number of demand profiles.
	MaxProfiles = 8
	// MaxBands is the largest number of bands in one demand profile.
	MaxBands = 24
	// MaxFlows is the largest number of flows in one demand profile.
	MaxFlows = 65000
)

// These limits bound the network geometry. The coordinate limit keeps each
// lane length finite, so the block count of each lane is exact.
const (
	// MaxCoordinate is the largest absolute value in meters of each
	// coordinate of a node position or a lane control point. The largest
	// of a generated project in the tests is 36,120, in a ring of 200
	// stations.
	MaxCoordinate = 100_000
	// MaxNetworkBlocks is the largest total of the blocks that
	// sim.Network.LaneBlocks counts. A block is a track cell of about 30
	// meters. The simulator keeps the resources of each block one time,
	// and the routes of the pods share them, so this limit bounds their
	// memory. The restore of a saved state walks at most 32 times the
	// blocks of the network, plus 4 blocks for each lane, and at most
	// 256,000 blocks. The simulator compares lane pairs over their full
	// length when it starts, so this limit also bounds the start time. The
	// largest total of a generated project in the tests is 38,404, in a
	// ring of 4 stations with 62 berths each.
	MaxNetworkBlocks = 64_000
)

// These values limit the geographic reference of a project. GeoProjection
// is the one projection that Validate accepts. GeoRadius is the sphere
// radius in meters of that projection, the radius of the London preset.
// MaxGeoLatitude is the largest absolute latitude in degrees of the
// reference. web/editor.js has a copy of these values.
const (
	GeoProjection  = "equirectangular"
	GeoRadius      = 6_371_000
	MaxGeoLatitude = 80
)

// Geo is the geographic reference of a project. The projection maps a
// latitude and a longitude in degrees to world meters: x is R cos(lat0)
// (lon - lon0) and y is -R (lat - lat0), with the angles in radians, lat0
// and lon0 the reference, and R the radius. Thus x increases to the east
// and y increases to the south. The browser editor uses it to place a map
// image on the network. The simulation does not use it.
type Geo struct {
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Projection string  `json:"projection"`
	Radius     float64 `json:"radius"`
}

// MapBackground selects a live browser map. It does not change simulation geometry.
type MapBackground struct {
	Provider string  `json:"provider"`
	Opacity  float64 `json:"opacity"`
}

// MaxFileBytes is the largest project file accepted from local storage. It
// also limits the canonical encoding of a valid project. Validate measures
// the project with the widest demand settings that ValidateDemand accepts.
// Thus a session state file can hold each valid project, also after a
// change to its demand settings.
const MaxFileBytes = 10 << 20

// errTooLarge means that the canonical encoding of a project, with the
// widest demand settings, has more than MaxFileBytes.
var errTooLarge = fmt.Errorf("encoded project must have at most %d bytes with any demand settings", MaxFileBytes)

// widestDemand has the longest canonical encoding of all demand settings
// that ValidateDemand accepts. Enabled is false, because false is longer
// than true. Rail-arrivals is the longest pattern name. Each reference has
// the largest length, and each byte is a control character, which JSON
// writes as a 6-byte escape. Keep this value in step with ValidateDemand.
var widestDemand = DemandConfig{
	PerMinute:        120,
	Pattern:          "rail-arrivals",
	Seed:             math.MaxUint64,
	Destination:      strings.Repeat("\x01", maxIDLength),
	Profile:          strings.Repeat("\x01", maxIDLength),
	Band:             strings.Repeat("\x01", maxIDLength),
	DailyStartMinute: 1439,
}

// DemandConfig controls deterministic arrivals per simulated minute.
type DemandConfig struct {
	Enabled          bool   `json:"enabled"`
	PerMinute        int    `json:"perMinute"`
	Pattern          string `json:"pattern"`
	Seed             uint64 `json:"seed"`
	Destination      string `json:"destination,omitempty"`
	Profile          string `json:"profile,omitempty"`
	Band             string `json:"band,omitempty"`
	DailyStartMinute int    `json:"dailyStartMinute,omitzero"`
}

// DemandProfile contains a portable origin-destination demand matrix.
type DemandProfile struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Bands []DemandBand `json:"bands"`
	Flows []DemandFlow `json:"flows"`
}

// DemandBand identifies one set of weights in a demand profile.
type DemandBand struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	StartMinute     int    `json:"startMinute"`
	DurationMinutes int    `json:"durationMinutes"`
	PerMinute       *int   `json:"perMinute,omitempty"`
}

// DemandFlow contains one origin-destination pair and one weight per band.
type DemandFlow struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	Weights []float64 `json:"weights"`
}

// DemandContext supplies the network and profiles used to validate demand settings.
type DemandContext struct {
	Network        sim.Network
	Profiles       []DemandProfile
	RailArrivals   []RailArrival
	RailDepartures []RailDeparture
}

// Config is the versioned, portable scenario configuration.
type Config struct {
	Version        int             `json:"version"`
	Name           string          `json:"name"`
	Network        sim.Network     `json:"network"`
	Fleet          []sim.Placement `json:"fleet"`
	Demand         DemandConfig    `json:"demand"`
	DemandProfiles []DemandProfile `json:"demandProfiles,omitempty"`
	RailArrivals   []RailArrival   `json:"railArrivals,omitempty"`
	RailDepartures []RailDeparture `json:"railDepartures,omitempty"`
	// SharedRidePartyLimit caps the parties per pod. Zero loads as one.
	SharedRidePartyLimit int `json:"sharedRidePartyLimit,omitempty"`
	// SharedRideMode selects the parties that can join a pod: "destination"
	// or "drop-offs". An empty mode loads as sim.DefaultSharedRideMode,
	// which is "drop-offs".
	SharedRideMode sim.SharedRideMode `json:"sharedRideMode,omitempty"`
	// SharedRideMaxStops caps the intermediate stops of a pod in drop-offs
	// mode, from 1 to sim.MaxSharedRideStops. Zero loads as
	// sim.DefaultSharedRideMaxStops.
	SharedRideMaxStops int `json:"sharedRideMaxStops,omitempty"`
	// SharedRideJoin selects the parties that can join a boarding pod by
	// their pickup state: "unassigned" or "reassign-existing". An empty
	// policy loads as sim.DefaultSharedRideJoin, which is "unassigned".
	SharedRideJoin sim.SharedRideJoin `json:"sharedRideJoin,omitempty"`
	// StationBuffers enables experimental berthless station queues.
	StationBuffers PolicyFlag `json:"stationBuffers,omitzero"`
	// PickupReassignment enables experimental empty-pod pickup replacement.
	PickupReassignment PolicyFlag `json:"pickupReassignment,omitzero"`
	// PlatoonLimit is the largest number of pods in one virtual platoon,
	// from sim.MinPlatoonLimit to sim.MaxPlatoonLimit. Zero turns platoons
	// off.
	PlatoonLimit   int  `json:"platoonLimit,omitzero"`
	Redistribution bool `json:"redistribution"`
	// Geo is the geographic reference, or nil for a project with no
	// reference. A reference at latitude 0 and longitude 0 is valid.
	Geo *Geo `json:"geo,omitzero"`
	// Map selects an optional live map behind the network.
	Map *MapBackground `json:"map,omitzero"`
}

// Default returns the supplied example project.
func Default() Config {
	return Config{
		Version: currentVersion,
		Name:    "Podsim example",
		Network: sim.Example(),
		Fleet: []sim.Placement{
			{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
			{ID: "02", StationID: "garden", BerthID: "garden-1"},
		},
		Demand:               DemandConfig{PerMinute: 2, Pattern: "balanced", Seed: 1},
		SharedRidePartyLimit: 1,
	}
}

// Validate checks format bounds and confirms that the scenario can start. It
// also checks that the canonical encoding of config has at most MaxFileBytes
// with any demand settings that ValidateDemand accepts.
func Validate(config Config) error {
	if config.Version != currentVersion {
		return fmt.Errorf("project version must be %d", currentVersion)
	}
	if strings.TrimSpace(config.Name) == "" || len(config.Name) > maxNameLength {
		return fmt.Errorf("project name must contain 1 to %d characters", maxNameLength)
	}
	if len(config.Network.Nodes) == 0 || len(config.Network.Nodes) > MaxNodes {
		return fmt.Errorf("network must contain 1 to %d nodes", MaxNodes)
	}
	if len(config.Network.Lanes) == 0 || len(config.Network.Lanes) > MaxLanes {
		return fmt.Errorf("network must contain 1 to %d lanes", MaxLanes)
	}
	if len(config.Network.Stations) < 2 || len(config.Network.Stations) > MaxStations {
		return fmt.Errorf("network must contain 2 to %d stations", MaxStations)
	}
	if len(config.Fleet) == 0 || len(config.Fleet) > MaxPods {
		return fmt.Errorf("fleet must contain 1 to %d pods", MaxPods)
	}
	if config.SharedRidePartyLimit < 0 || config.SharedRidePartyLimit > sim.MaxSharedRideParties {
		return fmt.Errorf("shared ride party limit must be 1 to %d", sim.MaxSharedRideParties)
	}
	if mode := config.SharedRideMode; mode != "" && mode != sim.SharedRideDestination && mode != sim.SharedRideDropOffs {
		return fmt.Errorf("shared ride mode must be %q or %q", sim.SharedRideDestination, sim.SharedRideDropOffs)
	}
	if config.SharedRideMaxStops < 0 || config.SharedRideMaxStops > sim.MaxSharedRideStops {
		return fmt.Errorf("shared ride stop limit must be 1 to %d", sim.MaxSharedRideStops)
	}
	if join := config.SharedRideJoin; join != "" && join != sim.SharedRideJoinUnassigned && join != sim.SharedRideJoinReassignExisting {
		return fmt.Errorf("shared ride join policy must be %q or %q", sim.SharedRideJoinUnassigned, sim.SharedRideJoinReassignExisting)
	}
	if limit := config.PlatoonLimit; limit != 0 && (limit < sim.MinPlatoonLimit || limit > sim.MaxPlatoonLimit) {
		return fmt.Errorf("platoon limit must be %d to %d, or 0 for no platoons", sim.MinPlatoonLimit, sim.MaxPlatoonLimit)
	}
	if err := validateGeo(config.Geo); err != nil {
		return err
	}
	if err := validateMap(config.Map, config.Geo); err != nil {
		return err
	}
	if err := validateNames(config); err != nil {
		return err
	}
	if err := validateLanes(config.Network); err != nil {
		return err
	}
	if err := validateGeometry(config.Network); err != nil {
		return err
	}
	if err := validateDemandProfiles(config.DemandProfiles, config.Network); err != nil {
		return err
	}
	if err := validateRailServices(config.RailArrivals, config.RailDepartures, config.Network); err != nil {
		return err
	}
	if err := ValidateDemand(config.Demand, DemandContext{Network: config.Network, Profiles: config.DemandProfiles, RailArrivals: config.RailArrivals, RailDepartures: config.RailDepartures}); err != nil {
		return err
	}
	if err := sim.ValidateFleet(config.Network, config.Fleet); err != nil {
		return fmt.Errorf("invalid project scenario: %w", err)
	}
	passenger := PassengerStations(config.Network)
	if len(passenger) < 2 {
		return errors.New("network needs at least two passenger stations")
	}
	if err := validatePassengerRoutes(config.Network.Lanes, passenger); err != nil {
		return err
	}
	// The size check runs last, because it encodes the full project. It
	// measures the project with the widest demand settings, because the
	// demand command checks new settings with ValidateDemand only.
	measured := config
	measured.Demand = widestDemand
	_, err := encodedSize(measured)
	return err
}

// EffectiveSharedRidePartyLimit returns one for legacy projects that omit the setting.
func EffectiveSharedRidePartyLimit(config Config) int {
	return max(1, config.SharedRidePartyLimit)
}

// EffectiveSharedRideMode returns the shared ride mode, with
// sim.DefaultSharedRideMode for an empty mode.
func EffectiveSharedRideMode(config Config) sim.SharedRideMode {
	if config.SharedRideMode == "" {
		return sim.DefaultSharedRideMode
	}
	return config.SharedRideMode
}

// EffectiveSharedRideMaxStops returns the stop limit, with
// sim.DefaultSharedRideMaxStops for zero.
func EffectiveSharedRideMaxStops(config Config) int {
	if config.SharedRideMaxStops == 0 {
		return sim.DefaultSharedRideMaxStops
	}
	return config.SharedRideMaxStops
}

// EffectiveSharedRideJoin returns the join policy, with
// sim.DefaultSharedRideJoin for an empty policy.
func EffectiveSharedRideJoin(config Config) sim.SharedRideJoin {
	if config.SharedRideJoin == "" {
		return sim.DefaultSharedRideJoin
	}
	return config.SharedRideJoin
}

// ConfigureSharedRides applies the shared ride settings of a project to a
// simulation.
func ConfigureSharedRides(simulation *sim.Simulation, config Config) error {
	if err := simulation.SetSharedRidePartyLimit(EffectiveSharedRidePartyLimit(config)); err != nil {
		return err
	}
	if err := simulation.SetSharedRideMode(EffectiveSharedRideMode(config), EffectiveSharedRideMaxStops(config)); err != nil {
		return err
	}
	return simulation.SetSharedRideJoin(EffectiveSharedRideJoin(config))
}

// ConfigurePlatoons applies the platoon limit of a project to a
// simulation. A limit of 0 turns platoons off and keeps the limit of the
// simulation. The saved state does not keep the platooning mode, so the
// session also calls this function after a restore.
func ConfigurePlatoons(simulation *sim.Simulation, config Config) error {
	if config.PlatoonLimit == 0 {
		return simulation.SetPlatooning(sim.PlatooningOff)
	}
	if err := simulation.SetPlatoonLimit(config.PlatoonLimit); err != nil {
		return err
	}
	return simulation.SetPlatooning(sim.PlatooningVirtual)
}

// validateGeo checks the geographic reference of a project. A nil
// reference is valid. The latitude must be from -MaxGeoLatitude to
// MaxGeoLatitude degrees and the longitude from -180 to 180 degrees. The
// projection must be GeoProjection and the radius must be GeoRadius, so
// the formula of Geo needs no hidden value.
func validateGeo(geo *Geo) error {
	switch {
	case geo == nil:
		return nil
	case !(math.Abs(geo.Latitude) <= MaxGeoLatitude):
		return fmt.Errorf("geo latitude must be from -%d to %d degrees", MaxGeoLatitude, MaxGeoLatitude)
	case !(math.Abs(geo.Longitude) <= 180):
		return errors.New("geo longitude must be from -180 to 180 degrees")
	case geo.Projection != GeoProjection:
		return fmt.Errorf("geo projection must be %q", GeoProjection)
	case geo.Radius != GeoRadius:
		return fmt.Errorf("geo radius must be %d meters", GeoRadius)
	}
	return nil
}

// validatePassengerRoutes returns an error for the first pair of passenger
// berths at different stations where no lane path goes from the origin berth
// to the destination berth. The order is the station order of passenger, then
// the berth order, first for the origin and then for the destination.
//
// When each passenger berth can reach the first berth and the first berth can
// reach each passenger berth, all the pairs have a path. Two searches from the
// first berth find this case in O(nodes + lanes). In the other case, the
// function checks each origin in order to find the first pair without a path.
// An origin that reaches the first berth, and that the first berth reaches,
// reaches the same nodes as the first berth, so it uses the first search.
func validatePassengerRoutes(lanes []sim.Lane, passenger []sim.Station) error {
	graph := newLaneGraph(lanes)
	// berthNodes has the node index of each berth of each station.
	berthNodes := make([][]int, len(passenger))
	var all []int
	for index, station := range passenger {
		for _, berth := range station.Berths {
			berthNodes[index] = append(berthNodes[index], graph.node(berth.Node))
		}
		all = append(all, berthNodes[index]...)
	}
	if len(all) == 0 {
		return nil
	}
	fromRoot := reachable(graph.forward, all[0])
	toRoot := reachable(graph.reverse, all[0])
	withRoot := func(node int) bool { return fromRoot[node] && toRoot[node] }
	if !slices.ContainsFunc(all, func(node int) bool { return !withRoot(node) }) {
		return nil
	}
	for fromIndex, from := range passenger {
		for originIndex, origin := range from.Berths {
			reached := fromRoot
			if node := berthNodes[fromIndex][originIndex]; !withRoot(node) {
				reached = reachable(graph.forward, node)
			}
			for toIndex, to := range passenger {
				if from.ID == to.ID {
					continue
				}
				for destinationIndex, destination := range to.Berths {
					if !reached[berthNodes[toIndex][destinationIndex]] {
						return fmt.Errorf("passenger route %s berth %s to %s berth %s: %w", quoteID(from.ID), quoteID(origin.ID), quoteID(to.ID), quoteID(destination.ID), sim.ErrUnreachable)
					}
				}
			}
		}
	}
	return nil
}

// laneGraph gives each node ID an index and holds the lanes as adjacency
// lists in both directions.
type laneGraph struct {
	indexes          map[string]int
	forward, reverse [][]int
}

func newLaneGraph(lanes []sim.Lane) *laneGraph {
	graph := &laneGraph{indexes: make(map[string]int, len(lanes))}
	for _, lane := range lanes {
		from, to := graph.node(lane.From), graph.node(lane.To)
		graph.forward[from] = append(graph.forward[from], to)
		graph.reverse[to] = append(graph.reverse[to], from)
	}
	return graph
}

// node returns the index of id. It adds id when the graph does not have it.
func (g *laneGraph) node(id string) int {
	index, ok := g.indexes[id]
	if !ok {
		index = len(g.indexes)
		g.indexes[id] = index
		g.forward = append(g.forward, nil)
		g.reverse = append(g.reverse, nil)
	}
	return index
}

// reachable returns the nodes that a breadth-first search over adjacent
// finds from start, start included.
func reachable(adjacent [][]int, start int) []bool {
	reached := make([]bool, len(adjacent))
	reached[start] = true
	queue := []int{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[current] {
			if !reached[next] {
				reached[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reached
}

// encodedSize returns the length of the canonical encoding of config. The
// session state file writes its project member with the same options (see
// stateEncoder.encodeProject in internal/session). Keep the options the
// same. When the length passes MaxFileBytes, encodedSize stops and returns
// errTooLarge. Thus a large project does not use memory for a full encoding.
// A project that the session cannot encode, for example with a string that
// is not valid UTF-8, also gets an error.
func encodedSize(config Config) (int, error) {
	counter := sizeCounter{limit: MaxFileBytes}
	if err := json.MarshalWrite(&counter, config, json.Deterministic(true)); err != nil {
		if errors.Is(err, errTooLarge) {
			return 0, errTooLarge
		}
		return 0, fmt.Errorf("encode project: %w", err)
	}
	return counter.size, nil
}

// sizeCounter counts the bytes written to it and keeps no data. A write
// that takes the count past limit fails with errTooLarge.
type sizeCounter struct {
	size, limit int
}

func (c *sizeCounter) Write(data []byte) (int, error) {
	if len(data) > c.limit-c.size {
		return 0, errTooLarge
	}
	c.size += len(data)
	return len(data), nil
}

// quoteID returns id as a quoted Go string for an error message. It keeps
// at most maxIDLength bytes of id and adds "..." after the quotes when it
// removes bytes. Thus an error message does not grow with an ID that is not
// valid, for example an ID of some megabytes in a command.
func quoteID(id string) string {
	if len(id) <= maxIDLength {
		return strconv.Quote(id)
	}
	// The cut goes back to the start of a rune, but by less than one rune,
	// so that invalid UTF-8 cannot remove all of the bytes.
	end := maxIDLength
	for end > maxIDLength-utf8.UTFMax+1 && !utf8.RuneStart(id[end]) {
		end--
	}
	return strconv.Quote(id[:end]) + "..."
}

func validateNames(config Config) error {
	validID := func(id string) bool { return id != "" && len(id) <= maxIDLength }
	for _, node := range config.Network.Nodes {
		if !validID(node.ID) {
			return fmt.Errorf("node ID must contain 1 to %d characters", maxIDLength)
		}
	}
	for _, lane := range config.Network.Lanes {
		if !validID(lane.ID) || !validID(lane.From) || !validID(lane.To) {
			return fmt.Errorf("lane IDs must contain 1 to %d characters", maxIDLength)
		}
		if lane.StationID != "" && !validID(lane.StationID) {
			return fmt.Errorf("lane station IDs must contain 1 to %d characters", maxIDLength)
		}
		if len(lane.SeparationGroup) > maxIDLength {
			return fmt.Errorf("lane separation groups must contain at most %d characters", maxIDLength)
		}
	}
	for _, station := range config.Network.Stations {
		if !validID(station.ID) || !validID(station.Entry) || !validID(station.Exit) {
			return fmt.Errorf("station IDs must contain 1 to %d characters", maxIDLength)
		}
		if strings.TrimSpace(station.Name) == "" || len(station.Name) > maxNameLength {
			return fmt.Errorf("station name must contain 1 to %d characters", maxNameLength)
		}
		if len(station.Berths) == 0 || len(station.Berths) > MaxBerths {
			return fmt.Errorf("station %s must contain 1 to %d berths", quoteID(station.ID), MaxBerths)
		}
		for _, berth := range station.Berths {
			if !validID(berth.ID) || !validID(berth.Node) {
				return fmt.Errorf("berth IDs must contain 1 to %d characters", maxIDLength)
			}
			if len(berth.SeparationGroup) > maxIDLength {
				return fmt.Errorf("berth separation groups must contain at most %d characters", maxIDLength)
			}
		}
	}
	for _, placement := range config.Fleet {
		if !validID(placement.ID) || !validID(placement.StationID) || placement.BerthID != "" && !validID(placement.BerthID) {
			return fmt.Errorf("fleet IDs must contain 1 to %d characters", maxIDLength)
		}
	}
	return nil
}

// validateLanes checks that each node has at most MaxNodeLanes lanes, that
// the nodes have at most MaxJunctionPairs lane pairs, and that no two lanes
// have the same nodes and the same path.
func validateLanes(network sim.Network) error {
	counts := make(map[string]int, len(network.Nodes))
	for _, lane := range network.Lanes {
		counts[lane.From]++
		counts[lane.To]++
	}
	for _, node := range network.Nodes {
		if counts[node.ID] > MaxNodeLanes {
			return fmt.Errorf("node %s has %d lanes, more than %d", quoteID(node.ID), counts[node.ID], MaxNodeLanes)
		}
	}
	pairs := network.JunctionPairs()
	total, most := 0, 0
	for index, count := range pairs {
		total += count
		if count > pairs[most] {
			most = index
		}
	}
	if total > MaxJunctionPairs {
		return fmt.Errorf("network has %d lane pairs at nodes, more than %d, and node %s has the most, %d", total, MaxJunctionPairs, quoteID(network.Nodes[most].ID), pairs[most])
	}
	type path struct {
		from, to string
		curved   bool
		control  sim.Point
	}
	paths := make(map[path]string, len(network.Lanes))
	for _, lane := range network.Lanes {
		key := path{from: lane.From, to: lane.To}
		if lane.Control != nil {
			key.curved, key.control = true, *lane.Control
		}
		if other, ok := paths[key]; ok {
			return fmt.Errorf("lanes %s and %s have the same nodes and path", quoteID(other), quoteID(lane.ID))
		}
		paths[key] = lane.ID
	}
	return nil
}

// validateGeometry checks that each node position and lane control point
// is in the square of MaxCoordinate meters about the origin, and that the
// lanes have at most MaxNetworkBlocks blocks. It checks the coordinates
// first, because a lane with a coordinate outside the square can have a
// length that is too large for a block count.
func validateGeometry(network sim.Network) error {
	inside := func(point sim.Point) bool {
		return math.Abs(point.X) <= MaxCoordinate && math.Abs(point.Y) <= MaxCoordinate
	}
	for _, node := range network.Nodes {
		if !inside(node.Position) {
			return fmt.Errorf("node %s must have coordinates from -%d to %d meters", quoteID(node.ID), MaxCoordinate, MaxCoordinate)
		}
	}
	for _, lane := range network.Lanes {
		if lane.Control != nil && !inside(*lane.Control) {
			return fmt.Errorf("lane %s must have a control point with coordinates from -%d to %d meters", quoteID(lane.ID), MaxCoordinate, MaxCoordinate)
		}
	}
	blocks := network.LaneBlocks()
	total, most := 0, 0
	for index, count := range blocks {
		total += count
		if count > blocks[most] {
			most = index
		}
	}
	if total > MaxNetworkBlocks {
		return fmt.Errorf("network lanes have %d blocks, more than %d, and lane %s has the most, %d", total, MaxNetworkBlocks, quoteID(network.Lanes[most].ID), blocks[most])
	}
	return nil
}

// ValidateDemand checks demand settings against the active passenger stations and profiles.
func ValidateDemand(config DemandConfig, context DemandContext) error {
	if config.PerMinute < 1 || config.PerMinute > 120 {
		return errors.New("demand rate must be 1 to 120 orders per simulated minute")
	}
	if config.Pattern != "balanced" && config.Pattern != "market" && config.Pattern != "destination" && config.Pattern != "profile" && config.Pattern != "profile-daily" && config.Pattern != "rail-arrivals" && config.Pattern != "rail-services" {
		return errors.New("demand pattern must be balanced, market, destination, profile, profile-daily, rail-arrivals, or rail-services")
	}
	if config.DailyStartMinute < 0 || config.DailyStartMinute >= 1440 || config.Pattern != "profile-daily" && config.DailyStartMinute != 0 {
		return errors.New("daily start minute must be 0 to 1439 and requires profile-daily")
	}
	if len(config.Destination) > maxIDLength || len(config.Profile) > maxIDLength || len(config.Band) > maxIDLength {
		return fmt.Errorf("demand references must contain at most %d characters", maxIDLength)
	}
	if config.Pattern == "rail-services" {
		if len(context.RailArrivals)+len(context.RailDepartures) == 0 {
			return errors.New("rail-services demand needs a nonempty rail plan")
		}
		return validateRailServices(context.RailArrivals, context.RailDepartures, context.Network)
	}
	if config.Pattern == "rail-arrivals" {
		if len(context.RailArrivals) == 0 {
			return errors.New("rail-arrivals demand needs a nonempty arrival plan")
		}
		return validateRailArrivals(context.RailArrivals, context.Network)
	}
	if config.Pattern == "profile" || config.Pattern == "profile-daily" {
		profile, ok := demandProfile(context.Profiles, config.Profile)
		if !ok {
			return fmt.Errorf("unknown demand profile %s", quoteID(config.Profile))
		}
		if config.Pattern == "profile-daily" {
			return validateDailyBands(profile)
		}
		if _, ok := demandBand(profile, config.Band); !ok {
			return fmt.Errorf("unknown demand band %s", quoteID(config.Band))
		}
		return nil
	}
	if config.Pattern != "destination" {
		return nil
	}
	station, ok := context.Network.Station(config.Destination)
	if !ok {
		return fmt.Errorf("unknown demand destination %s", quoteID(config.Destination))
	}
	if station.ParkingOnly {
		return errors.New("demand destination must be a passenger station")
	}
	return nil
}

func validateDemandProfiles(profiles []DemandProfile, network sim.Network) error {
	if len(profiles) > MaxProfiles {
		return fmt.Errorf("project must contain at most %d demand profiles", MaxProfiles)
	}
	passenger := make(map[string]bool)
	for _, station := range PassengerStations(network) {
		passenger[station.ID] = true
	}
	profileIDs := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if profile.ID == "" || len(profile.ID) > maxIDLength || profileIDs[profile.ID] {
			return fmt.Errorf("invalid or duplicate demand profile %s", quoteID(profile.ID))
		}
		if strings.TrimSpace(profile.Name) == "" || len(profile.Name) > maxNameLength {
			return fmt.Errorf("demand profile name must contain 1 to %d characters", maxNameLength)
		}
		if len(profile.Bands) == 0 || len(profile.Bands) > MaxBands {
			return fmt.Errorf("demand profile %s must contain 1 to %d bands", quoteID(profile.ID), MaxBands)
		}
		if len(profile.Flows) == 0 || len(profile.Flows) > MaxFlows {
			return fmt.Errorf("demand profile %s must contain 1 to %d flows", quoteID(profile.ID), MaxFlows)
		}
		profileIDs[profile.ID] = true
		if err := validateDemandProfile(profile, passenger); err != nil {
			return err
		}
	}
	return nil
}

func validateDemandProfile(profile DemandProfile, passenger map[string]bool) error {
	bandIDs := make(map[string]bool, len(profile.Bands))
	for _, band := range profile.Bands {
		if band.ID == "" || len(band.ID) > maxIDLength || bandIDs[band.ID] {
			return fmt.Errorf("demand profile %s has an invalid or duplicate band", quoteID(profile.ID))
		}
		if strings.TrimSpace(band.Name) == "" || len(band.Name) > maxNameLength || band.StartMinute < 0 || band.StartMinute >= 24*60 || band.DurationMinutes < 1 || band.DurationMinutes > 24*60 {
			return fmt.Errorf("demand profile %s has an invalid band %s", quoteID(profile.ID), quoteID(band.ID))
		}
		if band.PerMinute != nil && (*band.PerMinute < 0 || *band.PerMinute > 120) {
			return fmt.Errorf("demand profile %s band %s rate must be 0 to 120", quoteID(profile.ID), quoteID(band.ID))
		}
		bandIDs[band.ID] = true
	}
	totals := make([]float64, len(profile.Bands))
	pairs := make(map[[2]string]bool, len(profile.Flows))
	for _, flow := range profile.Flows {
		pair := [2]string{flow.From, flow.To}
		if !passenger[flow.From] || !passenger[flow.To] || flow.From == flow.To || pairs[pair] || len(flow.Weights) != len(profile.Bands) {
			return fmt.Errorf("demand profile %s has an invalid flow from %s to %s", quoteID(profile.ID), quoteID(flow.From), quoteID(flow.To))
		}
		pairs[pair] = true
		for index, weight := range flow.Weights {
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
				return fmt.Errorf("demand profile %s has an invalid weight", quoteID(profile.ID))
			}
			totals[index] += weight
		}
	}
	for index, total := range totals {
		if total <= 0 || math.IsInf(total, 0) {
			return fmt.Errorf("demand profile %s band %s needs a finite positive weight total", quoteID(profile.ID), quoteID(profile.Bands[index].ID))
		}
	}
	return nil
}

func demandProfile(profiles []DemandProfile, id string) (DemandProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return DemandProfile{}, false
}

func demandBand(profile DemandProfile, id string) (int, bool) {
	for index, band := range profile.Bands {
		if band.ID == id {
			return index, true
		}
	}
	return 0, false
}

// PassengerStations returns the passenger stations in network order.
func PassengerStations(network sim.Network) []sim.Station {
	stations := make([]sim.Station, 0, len(network.Stations))
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			stations = append(stations, station)
		}
	}
	return stations
}

func validateMap(background *MapBackground, geo *Geo) error {
	if background == nil {
		return nil
	}
	if geo == nil {
		return errors.New("a map background needs a geographic reference")
	}
	if background.Provider != "osm" {
		return errors.New("map provider must be osm")
	}
	if math.IsNaN(background.Opacity) || math.IsInf(background.Opacity, 0) || background.Opacity < 0 || background.Opacity > 1 {
		return errors.New("map opacity must be a finite number from 0 to 1")
	}
	return nil
}

// Clone returns a detached project configuration.
func Clone(config Config) Config {
	clone := config
	clone.Network = CloneNetwork(config.Network)
	clone.Fleet = append([]sim.Placement(nil), config.Fleet...)
	clone.DemandProfiles = cloneDemandProfiles(config.DemandProfiles)
	clone.RailArrivals = cloneRailArrivals(config.RailArrivals)
	clone.RailDepartures = cloneRailDepartures(config.RailDepartures)
	if config.Geo != nil {
		clone.Geo = new(*config.Geo)
	}
	if config.Map != nil {
		clone.Map = new(*config.Map)
	}
	return clone
}

func cloneDemandProfiles(profiles []DemandProfile) []DemandProfile {
	cloned := append([]DemandProfile(nil), profiles...)
	for index := range cloned {
		cloned[index].Bands = append([]DemandBand(nil), profiles[index].Bands...)
		for bandIndex := range cloned[index].Bands {
			if rate := cloned[index].Bands[bandIndex].PerMinute; rate != nil {
				cloned[index].Bands[bandIndex].PerMinute = new(*rate)
			}
		}
		cloned[index].Flows = append([]DemandFlow(nil), profiles[index].Flows...)
		for flowIndex := range cloned[index].Flows {
			cloned[index].Flows[flowIndex].Weights = append([]float64(nil), profiles[index].Flows[flowIndex].Weights...)
		}
	}
	return cloned
}

// CloneNetwork returns detached network slices.
func CloneNetwork(network sim.Network) sim.Network {
	clone := network
	clone.Nodes = append([]sim.Node(nil), network.Nodes...)
	clone.Lanes = append([]sim.Lane(nil), network.Lanes...)
	for i := range clone.Lanes {
		if clone.Lanes[i].Control != nil {
			clone.Lanes[i].Control = new(*clone.Lanes[i].Control)
		}
	}
	clone.Stations = append([]sim.Station(nil), network.Stations...)
	for i := range clone.Stations {
		clone.Stations[i].Berths = append([]sim.Berth(nil), network.Stations[i].Berths...)
	}
	return clone
}
