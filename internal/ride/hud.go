package ride

import (
	"math"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// HUD is the head-up display data of one pod and of the session. Speeds use
// meters per second, and station fields hold station names.
type HUD struct {
	// Found is false when the session has no pod with the requested ID. Then
	// only the session fields are set.
	Found      bool             `json:"found"`
	PodID      string           `json:"podID,omitempty"`
	Class      sim.VehicleClass `json:"class,omitempty"`
	Activity   sim.Activity     `json:"activity,omitempty"`
	Speed      float64          `json:"speed"`
	SpeedLimit float64          `json:"speedLimit"`
	WaitReason sim.WaitReason   `json:"waitReason,omitempty"`
	BlockedBy  string           `json:"blockedBy,omitempty"`
	Riders     []HUDRider       `json:"riders"`
	// NextStop is the next station where riders leave the pod, or empty.
	NextStop string     `json:"nextStop,omitempty"`
	Route    *HUDRoute  `json:"route,omitempty"`
	Session  HUDSession `json:"session"`
}

// HUDRider is one party aboard the pod.
type HUDRider struct {
	PartySize int    `json:"partySize"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// HUDRoute is the progress of the pod along its route. The stream sends a
// window of the route. When the route starts before the window, Before is
// true, and TraveledMeters is the part in the window. When the route
// continues past the window, After is true, and the remaining distance and
// the time are the part in the window.
type HUDRoute struct {
	TraveledMeters  float64 `json:"traveledMeters"`
	RemainingMeters float64 `json:"remainingMeters"`
	// Seconds is the time to drive the remaining distance at the speed
	// limits of the lanes, with no waits.
	Seconds float64 `json:"seconds"`
	Before  bool    `json:"before"`
	After   bool    `json:"after"`
}

// HUDSession is the state of the shared session.
type HUDSession struct {
	Speed     int    `json:"speed"`
	Paused    bool   `json:"paused"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
	// Message is the outcome of the last command of this page, or empty.
	Message string `json:"message,omitempty"`
}

// HUD returns the display data of the pod podID in state. It uses the lane
// geometry of the last Observe, which has the network of state.
func (b *Bridge) HUD(state session.State, podID string, connection HUDSession) HUD {
	connection.Speed, connection.Paused = state.Speed, state.Simulation.Paused
	hud := HUD{Riders: []HUDRider{}, Session: connection}
	index := -1
	for i := range state.Simulation.Vehicles {
		if state.Simulation.Vehicles[i].Pod.ID == podID {
			index = i
			break
		}
	}
	if index < 0 {
		return hud
	}
	vehicle := state.Simulation.Vehicles[index]
	pod := vehicle.Pod
	names := stationNames(state.Network)
	hud.Found, hud.PodID, hud.Activity, hud.Speed = true, pod.ID, pod.Activity, pod.Speed
	hud.Class = pod.Class
	if hud.Class == "" {
		hud.Class = sim.LegacyClass
	}
	hud.WaitReason, hud.BlockedBy = pod.WaitReason, pod.BlockedBy
	for _, lane := range state.Network.Lanes {
		if lane.ID == pod.LaneID {
			hud.SpeedLimit = lane.SpeedLimit
			break
		}
	}
	nextStop := ""
	for _, rider := range vehicle.Riders {
		if rider.Completed {
			continue
		}
		hud.Riders = append(hud.Riders, HUDRider{PartySize: rider.PartySize, From: names.of(rider.From), To: names.of(rider.To)})
		if nextStop == "" {
			nextStop = rider.To
		}
	}
	if len(vehicle.Stops) > 0 {
		nextStop = vehicle.Stops[0]
	}
	if nextStop != "" {
		hud.NextStop = names.of(nextStop)
	}
	hud.Route = b.routeProgress(vehicle)
	return hud
}

// routeProgress returns the progress of vehicle along its route window, or
// nil when the vehicle has no route.
func (b *Bridge) routeProgress(vehicle sim.Vehicle) *HUDRoute {
	window := vehicle.Presentation
	if window == nil || len(window.Motion) == 0 {
		return nil
	}
	// The stream decoder checks that Start <= Current <= Start + the window
	// length, so the offset is small.
	if window.Current < window.Start || window.Current-window.Start >= uint64(len(window.Motion)) {
		return nil
	}
	first := int(window.Current - window.Start) // #nosec G115 -- checked above.
	route := &HUDRoute{Before: window.Before, After: window.After}
	for _, lane := range window.Motion[:first] {
		route.TraveledMeters += b.lanes.length(lane.ID)
	}
	for i, lane := range window.Motion[first:] {
		length := b.lanes.length(lane.ID)
		if i == 0 && vehicle.Pod.LaneID == lane.ID {
			done := min(length, max(0, vehicle.Pod.LaneDistance))
			route.TraveledMeters += done
			length -= done
		}
		route.RemainingMeters += length
		if lane.SpeedLimit > 0 {
			route.Seconds += length / lane.SpeedLimit
		}
	}
	return route
}

// length returns the length of lane id in meters, or 0 for an unknown lane.
func (t laneTable) length(id string) float64 {
	shape, ok := t[id]
	if !ok {
		return 0
	}
	if shape.control != nil {
		return shape.lengths[len(shape.lengths)-1]
	}
	return math.Hypot(shape.to.X-shape.from.X, shape.to.Y-shape.from.Y)
}

// stationNameTable gives the name of each station that has one.
type stationNameTable map[string]string

func stationNames(network sim.Network) stationNameTable {
	names := make(stationNameTable, len(network.Stations))
	for _, station := range network.Stations {
		if station.Name != "" {
			names[station.ID] = station.Name
		}
	}
	return names
}

// of returns the name of station id, or id when the station has no name.
func (t stationNameTable) of(id string) string {
	if name, ok := t[id]; ok {
		return name
	}
	return id
}
