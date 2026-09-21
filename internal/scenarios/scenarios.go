// Package scenarios builds deterministic Podsim qualification scenarios.
package scenarios

import (
	"errors"
	"fmt"
	"math"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	minimumStations      = 3
	maximumStations      = 100
	maximumPods          = 200
	maximumStationBerths = 200
	maximumNodes         = 2000
	maximumLanes         = 4000
	speedLimit           = 14.0
	stationHalf          = 80.0
	berthOffset          = 120.0
	berthSpacing         = 30.0
)

// Parameters controls a ring scenario with one parking station.
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
}

// Config builds and validates a deterministic directed-ring scenario.
func Config(parameters Parameters) (project.Config, error) {
	if parameters.Stations < minimumStations {
		return project.Config{}, fmt.Errorf("stations must be at least %d", minimumStations)
	}
	if parameters.Stations > maximumStations {
		return project.Config{}, fmt.Errorf("stations must be at most %d", maximumStations)
	}
	if parameters.PassengerBerths < 1 || parameters.ParkingBerths < 1 {
		return project.Config{}, errors.New("passenger and parking berth counts must be positive")
	}
	if parameters.PassengerBerths > maximumStationBerths || parameters.ParkingBerths > maximumStationBerths {
		return project.Config{}, fmt.Errorf("station berth counts must be at most %d", maximumStationBerths)
	}
	if parameters.Pods > maximumPods {
		return project.Config{}, fmt.Errorf("pods must be at most %d", maximumPods)
	}
	passengerStations := parameters.Stations - 1
	passengerPods := parameters.Pods - parameters.InitialParkingPods
	if parameters.Pods < 1 || parameters.InitialParkingPods < 0 || passengerPods < 0 {
		return project.Config{}, errors.New("pod counts must be positive and internally consistent")
	}
	if passengerPods > passengerStations*parameters.PassengerBerths || parameters.InitialParkingPods > parameters.ParkingBerths {
		return project.Config{}, errors.New("initial pods exceed berth capacity")
	}
	berthCount := passengerStations*parameters.PassengerBerths + parameters.ParkingBerths
	if 2*parameters.Stations+berthCount > maximumNodes || 2*(parameters.Stations+berthCount) > maximumLanes {
		return project.Config{}, errors.New("generated network exceeds project node or lane limits")
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
	config.Fleet = placements(parameters, passengerPods)
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate generated scenario: %w", err)
	}
	return config, nil
}

// Small returns a compact scenario for smoke tests and visual checks.
func Small() project.Config {
	return mustConfig(Parameters{
		Name: "Small qualification ring", Stations: 5, Pods: 12,
		PassengerBerths: 4, ParkingBerths: 4, DemandPerMinute: 8, DemandSeed: 11,
	})
}

// Busy returns a scenario with sustained feasible demand and merge contention.
func Busy() project.Config {
	return mustConfig(Parameters{
		Name: "Busy qualification ring", Stations: 8, Pods: 32,
		PassengerBerths: 5, ParkingBerths: 10, InitialParkingPods: 4,
		DemandPerMinute: 30, DemandSeed: 23,
	})
}

// ParkingConstrained returns a saturated case with no free parking berth.
func ParkingConstrained() project.Config {
	return mustConfig(Parameters{
		Name: "Parking-constrained qualification ring", Stations: 6, Pods: 20,
		PassengerBerths: 4, ParkingBerths: 1, InitialParkingPods: 1,
		DemandPerMinute: 30, DemandSeed: 37,
	})
}

// RailHub returns a station-capacity experiment with a large parked reserve.
func RailHub() project.Config {
	config := mustConfig(Parameters{
		Name: "Rail-hub burst experiment", Stations: 7, Pods: 30,
		PassengerBerths: 6, ParkingBerths: 12, InitialParkingPods: 12,
		DemandPerMinute: 12, DemandSeed: 41,
	})
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
		panic(fmt.Errorf("validate rail-hub scenario: %w", err))
	}
	return config
}

// Scale100 returns the 20-station, 100-pod browser qualification scenario.
func Scale100() project.Config {
	parameters := scale100Parameters()
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
	config.Fleet = placements(parameters, parameters.Pods-parameters.InitialParkingPods)
	if err := project.Validate(config); err != nil {
		panic(fmt.Errorf("validate scale scenario: %w", err))
	}
	return config
}

func scale100Parameters() Parameters {
	return Parameters{
		Name: "Scale qualification: 20 stations and 100 pods", Stations: 20, Pods: 100,
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
		rows    = 4
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
			parking := index == parameters.Stations-1
			berths := parameters.PassengerBerths
			if parking {
				berths = parameters.ParkingBerths
			}
			addMeshStation(&network, meshStationParameters{index: index, row: row, column: column, berths: berths, parking: parking})
		}
	}
	return network
}

type meshStationParameters struct {
	index, row, column, berths int
	parking                    bool
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
		parking := index == parameters.Stations-1
		berthCount := parameters.PassengerBerths
		if parking {
			berthCount = parameters.ParkingBerths
		}
		station, stationNodes, stationLanes := stationGeometry(stationParameters{index: index, count: parameters.Stations, berths: berthCount, parking: parking})
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
}

func stationGeometry(parameters stationParameters) (sim.Station, []sim.Node, []sim.Lane) {
	theta := 2 * math.Pi * float64(parameters.index) / float64(parameters.count)
	radius := 180 * float64(parameters.count)
	center := sim.Point{X: radius * math.Cos(theta), Y: radius * math.Sin(theta)}
	tangent := sim.Point{X: -math.Sin(theta), Y: math.Cos(theta)}
	radial := sim.Point{X: math.Cos(theta), Y: math.Sin(theta)}
	entryID, exitID := stationNodeID(parameters.index, "entry"), stationNodeID(parameters.index, "exit")
	nodes := []sim.Node{
		{ID: entryID, Position: add(center, scale(tangent, -stationHalf))},
		{ID: exitID, Position: add(center, scale(tangent, stationHalf))},
	}
	lanes := []sim.Lane{{
		ID: stationLaneID(parameters.index, "through"), From: entryID, To: exitID, SpeedLimit: speedLimit,
	}}
	stationID := fmt.Sprintf("station-%02d", parameters.index+1)
	name := fmt.Sprintf("Station %02d", parameters.index+1)
	if parameters.parking {
		stationID, name = "parking", "Parking"
	}
	station := sim.Station{ID: stationID, Name: name, Entry: entryID, Exit: exitID, ParkingOnly: parameters.parking}
	for berthIndex := range parameters.berths {
		berthNodeID := fmt.Sprintf("s%02d-berth-%02d", parameters.index+1, berthIndex+1)
		depth := berthOffset + berthSpacing*float64(berthIndex)
		approach := stationHalf + 80 + 60*float64(berthIndex)
		berthID := fmt.Sprintf("%s-%02d", stationID, berthIndex+1)
		nodes = append(nodes, sim.Node{ID: berthNodeID, Position: add(center, scale(radial, depth))})
		station.Berths = append(station.Berths, sim.Berth{ID: berthID, Node: berthNodeID})
		lanes = append(lanes,
			sim.Lane{
				ID: fmt.Sprintf("s%02d-in-%02d", parameters.index+1, berthIndex+1), From: entryID, To: berthNodeID,
				SpeedLimit: speedLimit, Control: new(add(add(center, scale(tangent, -approach)), scale(radial, depth/2))),
			},
			sim.Lane{
				ID: fmt.Sprintf("s%02d-out-%02d", parameters.index+1, berthIndex+1), From: berthNodeID, To: exitID,
				SpeedLimit: speedLimit, Control: new(add(add(center, scale(tangent, approach)), scale(radial, depth/2))),
			},
		)
	}
	return station, nodes, lanes
}

func placements(parameters Parameters, passengerPods int) []sim.Placement {
	placements := make([]sim.Placement, 0, parameters.Pods)
	passengerStations := parameters.Stations - 1
	for podIndex := range passengerPods {
		stationIndex := podIndex % passengerStations
		berthIndex := podIndex/passengerStations + 1
		stationID := fmt.Sprintf("station-%02d", stationIndex+1)
		placements = append(placements, sim.Placement{
			ID: fmt.Sprintf("pod-%03d", podIndex+1), StationID: stationID,
			BerthID: fmt.Sprintf("%s-%02d", stationID, berthIndex),
		})
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
