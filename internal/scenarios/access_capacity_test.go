package scenarios

import (
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// Include empty departures after a concentrated burst of passenger arrivals.
func TestScale100Station19BurstDrainsSafely(t *testing.T) {
	t.Parallel()
	config := Scale100()
	simulation := newSimulation(t, config)
	var origins []string
	for _, station := range config.Network.Stations {
		if !station.ParkingOnly && station.ID != "station-19" {
			origins = append(origins, station.ID)
		}
	}
	const orders = 100
	submitted, previousCompleted := 0, 0
	var firstComplete, lastComplete int64
	peakStopped, peakStationExit, peakParking := 0, 0, 0
	for tick := range 3600 * sim.TicksPerSecond {
		if submitted < orders && tick%(5*sim.TicksPerSecond) == 0 {
			if err := simulation.RequestTrip(origins[submitted%len(origins)], "station-19"); err != nil {
				t.Fatal(err)
			}
			submitted++
		}
		simulation.Step()
		state := simulation.SafetyObservation()
		checkScaleSafety(t, state)
		active, stopped, stationExit, parking := 0, 0, 0, 0
		for _, pod := range state.Pods {
			if pod.Activity != sim.Idle {
				active++
			}
			if pod.Activity != sim.Traveling || pod.Speed >= 0.01 {
				continue
			}
			stopped++
			if pod.LaneID == "mesh-out-19" || strings.HasPrefix(pod.LaneID, "s19-out-") || strings.HasPrefix(pod.LaneID, "s19-departure-") {
				stationExit++
			}
			if strings.HasPrefix(pod.LaneID, "s20-") || pod.LaneID == "mesh-in-20" || pod.LaneID == "mesh-out-20" {
				parking++
			}
		}
		peakStopped = max(peakStopped, stopped)
		peakStationExit = max(peakStationExit, stationExit)
		peakParking = max(peakParking, parking)
		if state.Completed != previousCompleted {
			if firstComplete == 0 {
				firstComplete = state.Tick
			}
			lastComplete = state.Tick
			previousCompleted = state.Completed
		}
		if state.Completed == orders && state.Pending == 0 && active == 0 {
			t.Logf("orders=%d rate_per_minute=12 first_delivery_s=%.1f last_delivery_s=%.1f all_idle_s=%.1f peak_stopped=%d peak_station19_exit=%d peak_parking=%d", orders, float64(firstComplete)/sim.TicksPerSecond, float64(lastComplete)/sim.TicksPerSecond, float64(state.Tick)/sim.TicksPerSecond, peakStopped, peakStationExit, peakParking)
			return
		}
	}
	final := simulation.Snapshot()
	t.Fatalf("Station 19 burst did not drain: completed=%d submitted=%d pending=%d", final.Completed, submitted, len(final.Pending))
}
