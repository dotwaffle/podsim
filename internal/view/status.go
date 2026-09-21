package view

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/dotwaffle/podsim/internal/sim"
)

type podPurpose uint8

const (
	purposeIdle podPurpose = iota
	purposePickup
	purposePassengers
	purposeParking
	purposeRedistribution
	purposeEmpty
)

func (purpose podPurpose) color() uint32 {
	switch purpose {
	case purposePickup:
		return 0x62cce6
	case purposePassengers:
		return accent
	case purposeParking:
		return 0xb6a0ff
	case purposeRedistribution:
		return 0xffa879
	case purposeEmpty:
		return 0xe6d889
	default:
		return muted
	}
}

func (purpose podPurpose) label() string {
	switch purpose {
	case purposePickup:
		return "Pickup"
	case purposePassengers:
		return "Passengers"
	case purposeParking:
		return "Parking"
	case purposeRedistribution:
		return "Redistributing"
	case purposeEmpty:
		return "Empty travel"
	default:
		return "Idle"
	}
}

func (g *Game) podPurpose(vehicle sim.Vehicle, state sim.Snapshot) podPurpose {
	// Boarding and unloading belong to passenger service, even before occupancy changes.
	if vehicle.Pod.Occupied || vehicle.Pod.Activity == sim.Boarding || vehicle.Pod.Activity == sim.Unloading {
		return purposePassengers
	}
	// Pickup assignments live in Pending. Vehicle.Request can retain a completed trip.
	for _, request := range state.Pending {
		if request.PodID == vehicle.Pod.ID && !request.Completed {
			return purposePickup
		}
	}
	if vehicle.Pod.Activity == sim.Idle {
		return purposeIdle
	}
	if vehicle.Rebalancing {
		return purposeRedistribution
	}
	if station, ok := g.network.Station(vehicle.RelocatingTo); ok && station.ParkingOnly {
		return purposeParking
	}
	return purposeEmpty
}

func (g *Game) podButtonColor(id string) uint32 {
	for _, vehicle := range g.state.Simulation.Vehicles {
		if vehicle.Pod.ID == id {
			return g.podPurpose(vehicle, g.state.Simulation).color()
		}
	}
	return muted
}

func (g *Game) drawPodLegend(screen *ebiten.Image) {
	for index, purpose := range []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty} {
		x, y := 220+float64(index%4)*130, 530+float64(index/4)*18
		vector.FillCircle(screen, float32(g.layout.x(x)), float32(g.layout.bottom(y+6)), float32(3*g.layout.unit), rgb(purpose.color()), false)
		g.label(screen, label{x: g.layout.x(x + 9), y: g.layout.bottom(y), size: 10, value: purpose.label(), color: purpose.color(), physical: true})
	}
	vector.StrokeCircle(screen, float32(g.layout.x(480)), float32(g.layout.bottom(554)), float32(4*g.layout.unit), float32(g.layout.unit), rgb(amber), false)
	g.label(screen, label{x: g.layout.x(489), y: g.layout.bottom(548), size: 10, value: "Waiting", color: amber, physical: true})
	vector.StrokeCircle(screen, float32(g.layout.x(610)), float32(g.layout.bottom(554)), float32(4*g.layout.unit), float32(g.layout.unit), rgb(foreground), false)
	g.label(screen, label{x: g.layout.x(619), y: g.layout.bottom(548), size: 10, value: "Selected", color: foreground, physical: true})
}
