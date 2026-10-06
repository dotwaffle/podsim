package view

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

func (g *Game) drawInspection(screen *ebiten.Image, state sim.Snapshot) {
	if _, ok := selectedVehicle(state, g.selected); !ok {
		return
	}
	podID := state.Vehicles[g.selected].Pod.ID
	podLabel := fleetPodLabel(g.selected)
	if podID != podLabel {
		podLabel += " / " + podID
	}
	heading := g.fitText("POD "+podLabel, 12, 140)
	g.label(screen, label{x: inspectionLeft, y: 83, size: 12, value: heading, color: muted})
	g.label(screen, activityLine(activityLabel(state.Vehicles[g.selected].Pod, g.podPurpose(state.Vehicles[g.selected], state)), g.podPurpose(state.Vehicles[g.selected], state).color()))
	status := "Available for passenger orders."
	station, _ := g.network.Station(state.Vehicles[g.selected].Pod.StationID)
	if station.ParkingOnly {
		status = "Available for pickup requests."
	}
	switch state.Vehicles[g.selected].Pod.Activity {
	case sim.Idle:
	// The station type determines the idle status above.
	case sim.DepartingEmpty:
		status = "Waits to depart without a passenger."
	case sim.Boarding:
		status = "Party enters the pod."
	case sim.Traveling:
		status = "Follows the highlighted route."
	case sim.Unloading:
		status = "Party leaves at its destination."
	case sim.Continuing:
		status = "Waits to leave for its next stop."
	}
	for _, request := range state.Pending {
		if request.PodID == state.Vehicles[g.selected].Pod.ID && state.Vehicles[g.selected].Pod.Activity == sim.Idle {
			status = "Waiting for destination access."
		}
	}
	if state.Vehicles[g.selected].Pod.WaitReason != sim.NoWait {
		status = waitStatus(state.Vehicles[g.selected].Pod, state.Vehicles)
	}
	if state.Paused {
		status = "Paused. Resume to advance."
	}
	status = g.fitText(status, 13, inspectionRight-inspectionLeft)
	g.label(screen, label{x: inspectionLeft, y: 151, size: 13, value: status, color: muted})
	journey := "No active journey"
	if stations := journeyStations(state.Vehicles[g.selected]); len(stations) > 0 {
		names := make([]string, len(stations))
		for index, id := range stations {
			stop, _ := g.network.Station(id)
			names[index] = stop.Name
		}
		journey = strings.Join(names, " > ")
	}
	if station.ParkingOnly {
		journey = "Parked at " + station.Name
	}
	if to := state.Vehicles[g.selected].RelocatingTo; to != "" {
		station, _ := g.network.Station(to)
		journey = "Empty > " + station.Name
	}
	journey = g.fitText(journey, 17, inspectionRight-inspectionLeft)
	g.label(screen, label{x: inspectionLeft, y: 192, size: 17, value: journey, color: foreground})
	for i, row := range g.inspectionRows(state.Vehicles[g.selected]) {
		y := inspectionRowsTop + float64(i)*inspectionRowSpacing
		if row.name != "" {
			g.label(screen, label{x: inspectionLeft, y: y, size: 14, value: row.name, color: muted})
		}
		g.label(screen, label{x: row.valueLeft(), y: y, size: 14, value: g.fitInspectionValue(row), color: foreground})
	}
}

// waitStatus returns the inspector status of a pod that waits for a local
// resource. The status gives the wait reason and the pod that holds the
// resource. It names that pod by its fleet number, which is the label of its
// pod button. A pod ID that is not in vehicles does not change. A fault
// reason names the fault instead. Without a blocker, the status is the wait
// reason only.
func waitStatus(pod sim.Pod, vehicles []sim.Vehicle) string {
	status := string(pod.WaitReason)
	if pod.BlockedBy == "" {
		return status
	}
	if pod.WaitReason == sim.FaultBraking || pod.WaitReason == sim.FaultStopped || pod.WaitReason == sim.BlockedByIncident {
		return status + " / fault " + pod.BlockedBy
	}
	blocker := pod.BlockedBy
	if i := slices.IndexFunc(vehicles, func(v sim.Vehicle) bool { return v.Pod.ID == pod.BlockedBy }); i >= 0 {
		blocker = fleetPodLabel(i)
	}
	return status + " / pod " + blocker
}

// inspectionRow is a row in the pod inspector. A row with a name shows the
// name and the value in two columns. A row without a name shows the value
// across the full width of the inspector.
type inspectionRow struct{ name, value string }

// valueLeft returns the left edge of the value of row in the pod inspector.
func (row inspectionRow) valueLeft() float64 {
	if row.name == "" {
		return inspectionLeft
	}
	return inspectionValueLeft
}

// fitInspectionValue returns the value of row. It cuts the value when the
// value does not fit between its left edge and the right edge of the
// inspector.
func (g *Game) fitInspectionValue(row inspectionRow) string {
	return g.fitText(row.value, 14, inspectionRight-row.valueLeft())
}

// inspectionRows returns the rows of the pod inspector for vehicle. The
// rows show only values of the pod. The run status in the header shows the
// simulated time and the completed journeys. The station phase rows are
// last, because the station name row shows only during a station maneuver.
// So the other rows do not move when the pod enters or leaves a station.
func (g *Game) inspectionRows(vehicle sim.Vehicle) []inspectionRow {
	passengers := "Empty"
	if vehicle.Pod.Occupied && vehicle.RidersAboard() > 0 {
		passengers = passengerCount(vehicle.PassengersAboard())
	}
	rows := []inspectionRow{
		{"Speed", fmt.Sprintf("%.0f km/h", vehicle.Pod.Speed*3.6)},
		{"On board", passengers},
	}
	return append(rows, stationPhaseRows(vehicle.Pod, g.network)...)
}

// journeyStations returns the stations of the current or last passenger
// journey of a pod: the leg origin of its first rider, then the stops that
// the pod still makes. When no stop remains, it gives the destinations of
// the riders in rider order. It returns nil for a pod with no riders.
func journeyStations(vehicle sim.Vehicle) []string {
	if len(vehicle.Riders) == 0 {
		return nil
	}
	stations := []string{cmp.Or(vehicle.Riders[0].LegFrom, vehicle.Riders[0].From)}
	if len(vehicle.Stops) > 0 {
		return append(stations, vehicle.Stops...)
	}
	for _, rider := range vehicle.Riders {
		if !slices.Contains(stations[1:], rider.To) {
			stations = append(stations, rider.To)
		}
	}
	return stations
}

// passengerCount returns the On board value for count passengers, such as
// "1 passenger".
func passengerCount(count int) string {
	if count == 1 {
		return "1 passenger"
	}
	return fmt.Sprintf("%d passengers", count)
}

// stationPhaseRows returns the Station phase row of the pod inspector for
// pod. During a station maneuver at a known station, a second row shows the
// station name across the full width of the inspector, so that long names
// such as "Edgware Road (Circle Line)" show in full.
func stationPhaseRows(pod sim.Pod, network sim.Network) []inspectionRow {
	if pod.StationPhase == "" {
		return []inspectionRow{{"Station phase", "Main network"}}
	}
	rows := []inspectionRow{{"Station phase", string(pod.StationPhase)}}
	if station, ok := network.Station(pod.ManeuverStationID); ok {
		rows = append(rows, inspectionRow{value: station.Name})
	}
	return rows
}

func fleetPodLabel(index int) string {
	return fmt.Sprintf("%02d", index+1)
}

// activityLine returns the large activity label of the pod inspector. The
// fault button is to its right.
func activityLine(value string, shade uint32) label {
	return label{x: 816, y: 111, size: 26, value: value, color: shade}
}

func activityLabel(pod sim.Pod, purpose podPurpose) string {
	if pod.WaitReason != sim.NoWait && pod.Speed < 0.01 {
		return "Waiting"
	}
	if pod.Activity == sim.Traveling || pod.Activity == sim.DepartingEmpty || pod.Activity == sim.Idle {
		return purpose.label()
	}
	return string(pod.Activity)
}
