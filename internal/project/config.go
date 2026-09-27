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
// decoder uses the same limits.
const (
	// MaxPods is the largest fleet.
	MaxPods = 200
	// MaxBerths is the largest number of berths in one station.
	MaxBerths = 200
	// MaxStations is the largest number of stations.
	MaxStations = 100
	// MaxNodes is the largest number of network nodes.
	MaxNodes = 2000
	// MaxLanes is the largest number of network lanes.
	MaxLanes = 4000
	// MaxNodeLanes is the largest number of lanes at one node. A lane
	// counts at its From node and at its To node. The simulator compares
	// each pair of lanes at a node when it starts, so the limit keeps
	// that work below 2 * MaxLanes * MaxNodeLanes pairs.
	MaxNodeLanes = 64
	// MaxProfiles is the largest number of demand profiles.
	MaxProfiles = 8
	// MaxBands is the largest number of bands in one demand profile.
	MaxBands = 24
	// MaxFlows is the largest number of flows in one demand profile.
	MaxFlows = 20000
)

// MaxFileBytes is the largest project file accepted from local storage. It
// also limits the canonical encoding of a valid project. Validate measures
// the project with the widest demand settings that ValidateDemand accepts.
// Thus a session state file can hold each valid project, also after a
// change to its demand settings.
const MaxFileBytes = 4 << 20

// errTooLarge means that the canonical encoding of a project, with the
// widest demand settings, has more than MaxFileBytes.
var errTooLarge = fmt.Errorf("encoded project must have at most %d bytes with any demand settings", MaxFileBytes)

// widestDemand has the longest canonical encoding of all demand settings
// that ValidateDemand accepts. Enabled is false, because false is longer
// than true. Destination is the longest pattern name. Each reference has
// the largest length, and each byte is a control character, which JSON
// writes as a 6-byte escape. Keep this value in step with ValidateDemand.
var widestDemand = DemandConfig{
	PerMinute:   120,
	Pattern:     "destination",
	Seed:        math.MaxUint64,
	Destination: strings.Repeat("\x01", maxIDLength),
	Profile:     strings.Repeat("\x01", maxIDLength),
	Band:        strings.Repeat("\x01", maxIDLength),
}

// DemandConfig controls deterministic arrivals per simulated minute.
type DemandConfig struct {
	Enabled     bool   `json:"enabled"`
	PerMinute   int    `json:"perMinute"`
	Pattern     string `json:"pattern"`
	Seed        uint64 `json:"seed"`
	Destination string `json:"destination,omitempty"`
	Profile     string `json:"profile,omitempty"`
	Band        string `json:"band,omitempty"`
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
}

// DemandFlow contains one origin-destination pair and one weight per band.
type DemandFlow struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	Weights []float64 `json:"weights"`
}

// DemandContext supplies the network and profiles used to validate demand settings.
type DemandContext struct {
	Network  sim.Network
	Profiles []DemandProfile
}

// Config is the versioned, portable scenario configuration.
type Config struct {
	Version        int             `json:"version"`
	Name           string          `json:"name"`
	Network        sim.Network     `json:"network"`
	Fleet          []sim.Placement `json:"fleet"`
	Demand         DemandConfig    `json:"demand"`
	DemandProfiles []DemandProfile `json:"demandProfiles,omitempty"`
	// SharedRidePartyLimit caps same-destination parties per pod. Zero loads as one.
	SharedRidePartyLimit int  `json:"sharedRidePartyLimit,omitempty"`
	Redistribution       bool `json:"redistribution"`
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
	if err := validateNames(config); err != nil {
		return err
	}
	if err := validateLanes(config.Network); err != nil {
		return err
	}
	if err := validateDemandProfiles(config.DemandProfiles, config.Network); err != nil {
		return err
	}
	if err := ValidateDemand(config.Demand, DemandContext{Network: config.Network, Profiles: config.DemandProfiles}); err != nil {
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

// validateLanes checks that each node has at most MaxNodeLanes lanes, and
// that no two lanes have the same nodes and the same path.
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

// ValidateDemand checks demand settings against the active passenger stations and profiles.
func ValidateDemand(config DemandConfig, context DemandContext) error {
	if config.PerMinute < 1 || config.PerMinute > 120 {
		return errors.New("demand rate must be 1 to 120 orders per simulated minute")
	}
	if config.Pattern != "balanced" && config.Pattern != "market" && config.Pattern != "destination" && config.Pattern != "profile" {
		return errors.New("demand pattern must be balanced, market, destination, or profile")
	}
	if len(config.Destination) > maxIDLength || len(config.Profile) > maxIDLength || len(config.Band) > maxIDLength {
		return fmt.Errorf("demand references must contain at most %d characters", maxIDLength)
	}
	if config.Pattern == "profile" {
		profile, ok := demandProfile(context.Profiles, config.Profile)
		if !ok {
			return fmt.Errorf("unknown demand profile %s", quoteID(config.Profile))
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

// Clone returns a detached project configuration.
func Clone(config Config) Config {
	clone := config
	clone.Network = CloneNetwork(config.Network)
	clone.Fleet = append([]sim.Placement(nil), config.Fleet...)
	clone.DemandProfiles = cloneDemandProfiles(config.DemandProfiles)
	return clone
}

func cloneDemandProfiles(profiles []DemandProfile) []DemandProfile {
	cloned := append([]DemandProfile(nil), profiles...)
	for index := range cloned {
		cloned[index].Bands = append([]DemandBand(nil), profiles[index].Bands...)
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
