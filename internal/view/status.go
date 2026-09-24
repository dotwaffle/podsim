package view

import (
	"github.com/hajimehoshi/ebiten/v2"

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

type fleetUse struct {
	active, passenger, total int
}

func summarizeFleet(state sim.Snapshot) fleetUse {
	assigned := make(map[string]bool, len(state.Pending))
	for _, request := range state.Pending {
		if request.PodID != "" {
			assigned[request.PodID] = true
		}
	}
	use := fleetUse{total: len(state.Vehicles)}
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.Activity != sim.Idle || assigned[vehicle.Pod.ID] {
			use.active++
		}
		if vehicle.Pod.Occupied || vehicle.Pod.Activity == sim.Boarding || vehicle.Pod.Activity == sim.Unloading {
			use.passenger++
		}
	}
	return use
}

func (use fleetUse) activePercent() int { return percent(use.active, use.total) }

func (use fleetUse) passengerPercent() int { return percent(use.passenger, use.total) }

func percent(count, total int) int {
	if total == 0 {
		return 0
	}
	return (100*count + total/2) / total
}

// color returns the purpose color. With protanopia, deuteranopia and
// tritanopia, each pair of colors keeps a CIEDE2000 difference of 4.5 or
// more. Each color also keeps that difference from muted, the color of the
// node dots and the empty berth rings, so a pod cannot look like a node dot.
// Each color has a contrast of 4.5:1 or more on the panel and on the
// background.
func (purpose podPurpose) color() uint32 {
	switch purpose {
	case purposePickup:
		return 0x56b4e9
	case purposePassengers:
		return 0x5fe3a1
	case purposeParking:
		return 0xd08fd0
	case purposeRedistribution:
		return 0xf28c28
	case purposeEmpty:
		return 0xf0e442
	default:
		return foreground
	}
}

// emptyMove reports whether a pod with this purpose moves without
// passengers.
func (purpose podPurpose) emptyMove() bool {
	switch purpose {
	case purposePickup, purposeParking, purposeRedistribution, purposeEmpty:
		return true
	default:
		return false
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

const (
	// podRadius is the radius of a pod on the map in display units.
	podRadius = 4.0
	// podRingWidth is the line width of the ring of an empty move in
	// display units. The ring has the same outer radius as a disc.
	podRingWidth = 2.0
	// podButtonFill is the fill of a pod button that is not selected. Each
	// purpose color has a contrast of 4.5:1 or more on it.
	podButtonFill = 0x243645
)

// podMarkSprite returns the sprite of a pod in its purpose color. unit is
// the display unit in device pixels. A pod with passengers and an idle pod
// are filled discs. A pod that moves empty is a ring, so the shape also shows
// the purpose.
func podMarkSprite(purpose podPurpose, unit float64) spriteKey {
	key := discSprite(podRadius*unit, purpose.color())
	if purpose.emptyMove() {
		// Fill the hole in the lane color. A node dot or a route line under
		// the pod then cannot make the ring look like a disc.
		key.layers[1] = circleLayer{radius: float32((podRadius - podRingWidth) * unit), color: track}
	}
	return key
}

// drawPodLegend draws the pod purposes and the pod rings below the map. It
// uses the sprites of the map at scale.
func (g *Game) drawPodLegend(screen *ebiten.Image, scale spriteScale) {
	for index, purpose := range []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty} {
		x, y := 220+float64(index%4)*130, 530+float64(index/4)*18
		g.sprites.draw(screen, spriteDraw{center: sim.Point{X: g.layout.x(x), Y: g.layout.bottom(y + 6)}, key: podMarkSprite(purpose, g.layout.unit), scale: scale})
		g.label(screen, label{x: g.layout.x(x + 9), y: g.layout.bottom(y), size: 10, value: purpose.label(), color: purpose.color(), physical: true})
	}
	// The map draws the Waiting and Selected rings around a pod. The legend
	// draws them around an idle pod, so a ring of pod size means only an
	// empty move.
	for _, marker := range []struct {
		x     float64
		value string
		color uint32
	}{{x: 480, value: "Waiting", color: amber}, {x: 610, value: "Selected", color: foreground}} {
		x, y := g.layout.x(marker.x-3), g.layout.bottom(554)
		center := sim.Point{X: x, Y: y}
		g.sprites.draw(screen, spriteDraw{center: center, key: podMarkSprite(purposeIdle, g.layout.unit), scale: scale})
		g.sprites.draw(screen, spriteDraw{center: center, key: ringSprite(7*g.layout.unit, 1.5*g.layout.unit, marker.color), scale: scale})
		g.label(screen, label{x: g.layout.x(marker.x + 9), y: g.layout.bottom(548), size: 10, value: marker.value, color: marker.color, physical: true})
	}
}
