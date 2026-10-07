package view

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// legRider returns an active rider from station-01 to station-03 that a
// transfer moved to the leg origin station-02.
func legRider() sim.Request {
	return sim.Request{ID: 1, From: "station-01", LegFrom: "station-02", To: "station-03", PartySize: 1, PodID: "01"}
}

// TestLegOriginOrderRow checks that the order row of a transferred party
// keeps its order origin and names its leg origin.
func TestLegOriginOrderRow(t *testing.T) {
	t.Parallel()
	game := ordersTestGame(t, controlLayouts[0].input, 0)
	game.state.Simulation.Pending = []sim.Request{legRider()}
	game.state.Simulation.Pending[0].DispatchReason = "Waiting for an available pod"
	game.network.Stations[1].Name = "Bank"
	var values []string
	for _, value := range game.orderLabels(game.state.Simulation) {
		values = append(values, value.value)
	}
	// The ordersTestGame station names are long, so the row text ends
	// early.
	if len(values) != 4 || !strings.HasPrefix(values[2], "#1  Battersea") || values[3] != "Transfer at Bank / Waiting for an available pod" {
		t.Fatalf("order labels %q", values)
	}
}

// TestLegOriginPodStations checks that the stations of a pod start at the
// leg origin of its first rider, and that the label preference uses the
// leg origin.
func TestLegOriginPodStations(t *testing.T) {
	t.Parallel()
	vehicle := sim.Vehicle{Riders: []sim.Request{legRider()}, Stops: []string{"station-03"}}
	if got := journeyStations(vehicle); !slices.Equal(got, []string{"station-02", "station-03"}) {
		t.Errorf("journey stations %q", got)
	}
	stations := map[string]bool{}
	addRiderStations(stations, vehicle)
	if got := slices.Sorted(maps.Keys(stations)); !slices.Equal(got, []string{"station-02", "station-03"}) {
		t.Errorf("rider stations %q", got)
	}
}

func TestVehicleRouteOriginWithClippedDisplay(t *testing.T) {
	t.Parallel()
	v := sim.Vehicle{
		Route:        []sim.Lane{{From: "window-start", To: "window-end"}},
		Presentation: &sim.RoutePresentation{Start: sim.MotionRouteLimit, Before: true, OriginNode: "original-origin"},
	}
	if got := vehicleRouteOrigin(v); got != "original-origin" {
		t.Fatalf("route origin %q, want original-origin", got)
	}
}
