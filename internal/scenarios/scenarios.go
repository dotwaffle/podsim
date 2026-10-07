// Package scenarios builds deterministic Podsim qualification scenarios.
package scenarios

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	minimumStations = 3
	// maximumStations is the largest ring. The ring radius is 180 meters
	// for each station, so a larger ring passes project.MaxCoordinate.
	maximumStations      = 500
	maximumPods          = project.MaxPods
	maximumStationBerths = project.MaxNodeLanes - 2 // A ring entry or exit node has a lane for each berth and 2 more.
	maximumNodes         = project.MaxNodes
	maximumLanes         = project.MaxLanes
	speedLimit           = 14.0
	stationHalf          = 80.0
	berthOffset          = 120.0
	berthSpacing         = 30.0
)

// Parameters controls a ring scenario with one parking station. The
// Scale100 mesh uses the same parameters with 20 stations.
type Parameters struct {
	Name               string
	Stations           int
	Pods               int
	PassengerBerths    int
	ParkingBerths      int
	InitialParkingPods int
	DemandPerMinute    int
	DemandSeed         uint64
	Redistribution     bool
	// StationBerths holds the berth count of single stations by station ID:
	// station-01 and on, or parking.
	StationBerths map[string]int
	// BerthPitch is the distance in meters between two berths of a station.
	// Zero keeps the pitch of the layout: berthSpacing for a ring station and
	// meshBerthPitch for a mesh station.
	BerthPitch float64
}

// stationID returns the ID of the station at index. The last station is
// the Parking station.
func (parameters Parameters) stationID(index int) string {
	if index == parameters.Stations-1 {
		return "parking"
	}
	return fmt.Sprintf("station-%02d", index+1)
}

// berths returns the berth count of the station at index.
func (parameters Parameters) berths(index int) int {
	if count, ok := parameters.StationBerths[parameters.stationID(index)]; ok {
		return count
	}
	if index == parameters.Stations-1 {
		return parameters.ParkingBerths
	}
	return parameters.PassengerBerths
}

// pitch returns BerthPitch, or layout when BerthPitch is zero.
func (parameters Parameters) pitch(layout float64) float64 {
	if parameters.BerthPitch == 0 {
		return layout
	}
	return parameters.BerthPitch
}

// checkParameters checks the station, berth, and pod counts, the station
// IDs of StationBerths, and the berth pitch. It returns the number of
// berths.
func checkParameters(parameters Parameters) (int, error) {
	if parameters.Stations < minimumStations {
		return 0, fmt.Errorf("stations must be at least %d", minimumStations)
	}
	if parameters.Stations > maximumStations {
		return 0, fmt.Errorf("stations must be at most %d", maximumStations)
	}
	if parameters.BerthPitch != 0 {
		if err := checkBerthPitch(parameters.BerthPitch); err != nil {
			return 0, err
		}
	}
	ids := make([]string, 0, parameters.Stations)
	for index := range parameters.Stations {
		ids = append(ids, parameters.stationID(index))
	}
	for _, id := range slices.Sorted(maps.Keys(parameters.StationBerths)) {
		if !slices.Contains(ids, id) {
			return 0, fmt.Errorf("unknown station ID %q", id)
		}
	}
	passengerBerths, berthCount := 0, 0
	for index, id := range ids {
		berths := parameters.berths(index)
		if berths < 1 || berths > maximumStationBerths {
			return 0, fmt.Errorf("station %s has %d berths, want 1 to %d", id, berths, maximumStationBerths)
		}
		berthCount += berths
		if index < parameters.Stations-1 {
			passengerBerths += berths
		}
	}
	if parameters.Pods > maximumPods {
		return 0, fmt.Errorf("pods must be at most %d", maximumPods)
	}
	passengerPods := parameters.Pods - parameters.InitialParkingPods
	if parameters.Pods < 1 || parameters.InitialParkingPods < 0 || passengerPods < 0 {
		return 0, errors.New("pod counts must be positive and internally consistent")
	}
	if passengerPods > passengerBerths || parameters.InitialParkingPods > parameters.berths(parameters.Stations-1) {
		return 0, errors.New("initial pods exceed berth capacity")
	}
	return berthCount, nil
}

// Config builds and validates a deterministic directed-ring scenario.
func Config(parameters Parameters) (project.Config, error) {
	berthCount, err := checkParameters(parameters)
	if err != nil {
		return project.Config{}, err
	}
	nodes, lanes := 2*parameters.Stations+berthCount, 2*(parameters.Stations+berthCount)
	if nodes > maximumNodes || lanes > maximumLanes {
		return project.Config{}, fmt.Errorf("generated network needs %d nodes and %d lanes, more than the limits of %d and %d", nodes, lanes, maximumNodes, maximumLanes)
	}
	config := project.Config{
		Version: 1,
		Name:    parameters.Name,
		Network: ring(parameters),
		Demand: project.DemandConfig{
			PerMinute: parameters.DemandPerMinute,
			Pattern:   "balanced",
			Seed:      parameters.DemandSeed,
		},
		Redistribution: parameters.Redistribution,
	}
	config.Fleet = placements(parameters)
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate generated scenario: %w", err)
	}
	return config, nil
}

// ringPreset is a ring or mesh preset. build makes the preset from its
// parameters, and pitch is the berth pitch of its layout.
type ringPreset struct {
	parameters func() Parameters
	build      func(Parameters) (project.Config, error)
	pitch      float64
}

// ringPresets holds the ring and mesh presets by their cmd/scenario name.
var ringPresets = map[string]ringPreset{
	"small":               {parameters: smallParameters, build: Config, pitch: berthSpacing},
	"busy":                {parameters: busyParameters, build: Config, pitch: berthSpacing},
	"parking-constrained": {parameters: parkingConstrainedParameters, build: Config, pitch: berthSpacing},
	"rail-hub":            {parameters: railHubParameters, build: railHubConfig, pitch: berthSpacing},
	"scale100":            {parameters: scale100Parameters, build: scale100Config, pitch: meshBerthPitch},
}

// PresetWith returns the ring or mesh preset with the given name, after
// change changes its parameters. When the parameters give a layout other
// than that of the preset, the project name ends with " (custom
// capacity)", and a layout audit checks the network. The error names the
// station of the first hard conflict.
func PresetWith(name string, change func(*Parameters)) (project.Config, error) {
	preset, ok := ringPresets[name]
	if !ok {
		return project.Config{}, fmt.Errorf("unknown ring or mesh preset %q", name)
	}
	parameters := preset.parameters()
	change(&parameters)
	if _, err := checkParameters(parameters); err != nil {
		return project.Config{}, err
	}
	custom := !sameParameters(preset.parameters(), parameters, preset.pitch)
	if custom {
		parameters.Name += customCapacitySuffix
	}
	config, err := preset.build(parameters)
	if err != nil {
		return project.Config{}, err
	}
	if custom {
		if err := layoutError(config.Network, auditPlanarLayout(config.Network)); err != nil {
			return project.Config{}, err
		}
	}
	return config, nil
}

// sameParameters reports whether a and b give the same project. A station
// override with the uniform count, and a zero pitch or the pitch of the
// layout, do not change the project.
func sameParameters(a, b Parameters, layout float64) bool {
	if a.Stations != b.Stations || a.pitch(layout) != b.pitch(layout) {
		return false
	}
	for index := range a.Stations {
		if a.berths(index) != b.berths(index) {
			return false
		}
	}
	for _, parameters := range []*Parameters{&a, &b} {
		parameters.PassengerBerths, parameters.ParkingBerths = 0, 0
		parameters.StationBerths, parameters.BerthPitch = nil, 0
	}
	return reflect.DeepEqual(a, b)
}

func smallParameters() Parameters {
	return Parameters{
		Name: "Small qualification ring", Stations: 5, Pods: 12,
		PassengerBerths: 4, ParkingBerths: 4, DemandPerMinute: 8, DemandSeed: 11,
	}
}

// Small returns a compact scenario for smoke tests and visual checks.
func Small() project.Config {
	return mustConfig(smallParameters())
}

func busyParameters() Parameters {
	return Parameters{
		Name: "Busy qualification ring", Stations: 8, Pods: 32,
		PassengerBerths: 5, ParkingBerths: 10, InitialParkingPods: 4,
		DemandPerMinute: 30, DemandSeed: 23,
	}
}

// Busy returns a scenario with sustained feasible demand and merge contention.
func Busy() project.Config {
	return mustConfig(busyParameters())
}

func parkingConstrainedParameters() Parameters {
	return Parameters{
		Name: "Parking-constrained qualification ring", Stations: 6, Pods: 20,
		PassengerBerths: 4, ParkingBerths: 1, InitialParkingPods: 1,
		DemandPerMinute: 30, DemandSeed: 37,
	}
}

// ParkingConstrained returns a saturated case with no free parking berth.
func ParkingConstrained() project.Config {
	return mustConfig(parkingConstrainedParameters())
}

func railHubParameters() Parameters {
	return Parameters{
		Name: "Rail-hub burst experiment", Stations: 7, Pods: 30,
		PassengerBerths: 6, ParkingBerths: 12, InitialParkingPods: 12,
		DemandPerMinute: 12, DemandSeed: 41,
	}
}

// RailHub returns a station-capacity experiment with a large parked reserve.
func RailHub() project.Config {
	config, err := railHubConfig(railHubParameters())
	if err != nil {
		panic(err)
	}
	return config
}

// railHubConfig builds the rail-hub ring. Station 01 is the Rail Hub and
// the demand destination.
func railHubConfig(parameters Parameters) (project.Config, error) {
	config, err := Config(parameters)
	if err != nil {
		return project.Config{}, err
	}
	for index := range config.Network.Stations {
		station := &config.Network.Stations[index]
		switch {
		case station.ID == "station-01":
			station.Name = "Rail Hub"
		case !station.ParkingOnly:
			station.Name = fmt.Sprintf("District %d", index)
		}
	}
	config.Demand.Destination = "station-01"
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate rail-hub scenario: %w", err)
	}
	return config, nil
}

// Scale100 returns the 20-station, 100-pod browser qualification scenario.
func Scale100() project.Config {
	config, err := scale100Config(scale100Parameters())
	if err != nil {
		panic(err)
	}
	return config
}

// scale100Config builds the Scale100 mesh. The mesh has 20 stations.
func scale100Config(parameters Parameters) (project.Config, error) {
	if parameters.Stations != meshStations {
		return project.Config{}, fmt.Errorf("the mesh has %d stations, not %d", meshStations, parameters.Stations)
	}
	if _, err := checkParameters(parameters); err != nil {
		return project.Config{}, err
	}
	config := project.Config{
		Version: 1,
		Name:    parameters.Name,
		Network: scaleMesh(parameters),
		Demand: project.DemandConfig{
			PerMinute: parameters.DemandPerMinute,
			Pattern:   "balanced",
			Seed:      parameters.DemandSeed,
		},
	}
	if nodes, lanes := len(config.Network.Nodes), len(config.Network.Lanes); nodes > maximumNodes || lanes > maximumLanes {
		return project.Config{}, fmt.Errorf("generated network needs %d nodes and %d lanes, more than the limits of %d and %d", nodes, lanes, maximumNodes, maximumLanes)
	}
	config.Fleet = placements(parameters)
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate scale scenario: %w", err)
	}
	return config, nil
}

func scale100Parameters() Parameters {
	return Parameters{
		Name: "Scale qualification: 20 stations and 100 pods", Stations: meshStations, Pods: 100,
		PassengerBerths: 6, ParkingBerths: 24, InitialParkingPods: 5,
		DemandPerMinute: 20, DemandSeed: 100,
	}
}

func scale100Ring() project.Config {
	return mustConfig(scale100Parameters())
}

func scaleMesh(parameters Parameters) sim.Network {
	const (
		columns = 5
		rows    = meshStations / columns
		spacing = 1200.0
	)
	network := sim.Network{}
	for row := range rows {
		for column := range columns {
			junction := meshJunctionID(row, column)
			network.Nodes = append(network.Nodes, sim.Node{
				ID: junction, Position: sim.Point{X: float64(column) * spacing, Y: float64(row) * spacing},
			})

		}
	}
	for row := range rows {
		for column := range columns {
			if column+1 < columns {
				nodes, lanes := meshCarriageways(meshEdge{rowA: row, columnA: column, rowB: row, columnB: column + 1})
				network.Nodes = append(network.Nodes, nodes...)
				network.Lanes = append(network.Lanes, lanes...)
			}
			if row+1 < rows {
				nodes, lanes := meshCarriageways(meshEdge{rowA: row, columnA: column, rowB: row + 1, columnB: column})
				network.Nodes = append(network.Nodes, nodes...)
				network.Lanes = append(network.Lanes, lanes...)
			}
		}
	}
	for row := range rows {
		for column := range columns {
			index := row*columns + column
			addMeshStation(&network, meshStationParameters{
				index: index, row: row, column: column, berths: parameters.berths(index),
				parking: index == parameters.Stations-1, pitch: parameters.pitch(meshBerthPitch),
			})
		}
	}
	return network
}

type meshStationParameters struct {
	index, row, column, berths int
	parking                    bool
	pitch                      float64
}

type meshEdge struct {
	rowA, columnA, rowB, columnB int
}

func meshCarriageways(edge meshEdge) ([]sim.Node, []sim.Lane) {
	const (
		spacing = 1200.0
		offset  = 90.0
	)
	a, b := meshJunctionID(edge.rowA, edge.columnA), meshJunctionID(edge.rowB, edge.columnB)
	ax, ay := float64(edge.columnA)*spacing, float64(edge.rowA)*spacing
	bx, by := float64(edge.columnB)*spacing, float64(edge.rowB)*spacing
	if edge.rowA == edge.rowB && edge.rowA%2 == 1 || edge.columnA == edge.columnB && meshColumnRunsBackward(edge.columnA) {
		a, b = b, a
		ax, ay, bx, by = bx, by, ax, ay
	}
	perpendicularX, perpendicularY := -(by - ay), bx-ax
	length := math.Hypot(perpendicularX, perpendicularY)
	perpendicularX, perpendicularY = offset*perpendicularX/length, offset*perpendicularY/length
	abMid := "mid-" + a + "-" + b
	nodes := []sim.Node{{ID: abMid, Position: sim.Point{X: (ax+bx)/2 + perpendicularX, Y: (ay+by)/2 + perpendicularY}}}
	lanes := []sim.Lane{
		{ID: fmt.Sprintf("mesh-%s-%s-a", a, b), From: a, To: abMid, SpeedLimit: speedLimit},
		{ID: fmt.Sprintf("mesh-%s-%s-b", a, b), From: abMid, To: b, SpeedLimit: speedLimit},
	}
	return nodes, lanes
}

func meshColumnRunsBackward(column int) bool {
	return column == 0 || column == 2
}

func meshJunctionID(row, column int) string {
	return fmt.Sprintf("junction-%d-%d", row+1, column+1)
}

func mustConfig(parameters Parameters) project.Config {
	config, err := Config(parameters)
	if err != nil {
		panic(err)
	}
	return config
}

func ring(parameters Parameters) sim.Network {
	stations := make([]sim.Station, 0, parameters.Stations)
	var nodes []sim.Node
	var lanes []sim.Lane
	for index := range parameters.Stations {
		station, stationNodes, stationLanes := stationGeometry(stationParameters{
			index: index, count: parameters.Stations, berths: parameters.berths(index),
			parking: index == parameters.Stations-1, pitch: parameters.pitch(berthSpacing),
		})
		stations = append(stations, station)
		nodes = append(nodes, stationNodes...)
		lanes = append(lanes, stationLanes...)
	}
	for index := range parameters.Stations {
		next := (index + 1) % parameters.Stations
		lanes = append(lanes, sim.Lane{
			ID:   fmt.Sprintf("link-%02d-%02d", index+1, next+1),
			From: stationNodeID(index, "exit"), To: stationNodeID(next, "entry"), SpeedLimit: speedLimit,
		})
	}
	return sim.Network{Nodes: nodes, Lanes: lanes, Stations: stations}
}

type stationParameters struct {
	index, count, berths int
	parking              bool
	pitch                float64
}

func stationGeometry(parameters stationParameters) (sim.Station, []sim.Node, []sim.Lane) {
	theta := 2 * math.Pi * float64(parameters.index) / float64(parameters.count)
	radius := 180 * float64(parameters.count)
	center := sim.Point{X: radius * math.Cos(theta), Y: radius * math.Sin(theta)}
	tangent := sim.Point{X: -math.Sin(theta), Y: math.Cos(theta)}
	radial := sim.Point{X: math.Cos(theta), Y: math.Sin(theta)}
	entryID, exitID := stationNodeID(parameters.index, "entry"), stationNodeID(parameters.index, "exit")
	stationID := fmt.Sprintf("station-%02d", parameters.index+1)
	name := fmt.Sprintf("Station %02d", parameters.index+1)
	if parameters.parking {
		stationID, name = "parking", "Parking"
	}
	nodes := []sim.Node{
		{ID: entryID, Position: add(center, scale(tangent, -stationHalf))},
		{ID: exitID, Position: add(center, scale(tangent, stationHalf))},
	}
	lanes := []sim.Lane{{
		ID: stationLaneID(parameters.index, "through"), From: entryID, To: exitID, SpeedLimit: speedLimit,
		StationID: stationID, StationRole: sim.StationThroughRole,
	}}
	station := sim.Station{ID: stationID, Name: name, Entry: entryID, Exit: exitID, ParkingOnly: parameters.parking}
	for berthIndex := range parameters.berths {
		berthNodeID := fmt.Sprintf("s%02d-berth-%02d", parameters.index+1, berthIndex+1)
		depth := berthOffset + parameters.pitch*float64(berthIndex)
		approach := stationHalf + 80 + 60*float64(berthIndex)
		berthID := fmt.Sprintf("%s-%02d", stationID, berthIndex+1)
		nodes = append(nodes, sim.Node{ID: berthNodeID, Position: add(center, scale(radial, depth))})
		station.Berths = append(station.Berths, sim.Berth{ID: berthID, Node: berthNodeID})
		lanes = append(lanes,
			sim.Lane{
				ID: fmt.Sprintf("s%02d-in-%02d", parameters.index+1, berthIndex+1), From: entryID, To: berthNodeID,
				SpeedLimit: speedLimit, Control: new(add(add(center, scale(tangent, -approach)), scale(radial, depth/2))),
				StationID: stationID, StationRole: sim.StationBerthAccessRole,
			},
			sim.Lane{
				ID: fmt.Sprintf("s%02d-out-%02d", parameters.index+1, berthIndex+1), From: berthNodeID, To: exitID,
				SpeedLimit: speedLimit, Control: new(add(add(center, scale(tangent, approach)), scale(radial, depth/2))),
				StationID: stationID, StationRole: sim.StationDepartureRole,
			},
		)
	}
	return station, nodes, lanes
}

// placements puts the passenger pods round robin on the passenger
// stations, and a full station gets no more pods. Each station fills its
// berths in order. The other pods go to the Parking station.
func placements(parameters Parameters) []sim.Placement {
	passengerPods := parameters.Pods - parameters.InitialParkingPods
	placements := make([]sim.Placement, 0, parameters.Pods)
	for round := 0; len(placements) < passengerPods && round < maximumStationBerths; round++ {
		for index := range parameters.Stations - 1 {
			if len(placements) == passengerPods {
				break
			}
			if round >= parameters.berths(index) {
				continue
			}
			stationID := parameters.stationID(index)
			placements = append(placements, sim.Placement{
				ID: fmt.Sprintf("pod-%03d", len(placements)+1), StationID: stationID,
				BerthID: fmt.Sprintf("%s-%02d", stationID, round+1),
			})
		}
	}
	for parkingIndex := range parameters.InitialParkingPods {
		placements = append(placements, sim.Placement{
			ID: fmt.Sprintf("pod-%03d", passengerPods+parkingIndex+1), StationID: "parking",
			BerthID: fmt.Sprintf("parking-%02d", parkingIndex+1),
		})
	}
	return placements
}

func stationNodeID(index int, part string) string {
	return fmt.Sprintf("s%02d-%s", index+1, part)
}

func stationLaneID(index int, part string) string {
	return fmt.Sprintf("s%02d-%s", index+1, part)
}

func add(a, b sim.Point) sim.Point {
	return sim.Point{X: a.X + b.X, Y: a.Y + b.Y}
}

func scale(point sim.Point, multiplier float64) sim.Point {
	return sim.Point{X: point.X * multiplier, Y: point.Y * multiplier}
}
