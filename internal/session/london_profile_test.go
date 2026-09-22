package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestLondonProfileDrivesLiveDemand(t *testing.T) {
	t.Parallel()
	config := scenarios.London()
	config.Demand.Enabled = true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 * sim.TicksPerSecond {
		shared.advance()
	}
	state := shared.State()
	if state.Demand.Generated != 1 || state.Simulation.Submitted != 1 {
		t.Fatalf("London demand did not generate: %+v", state.Demand)
	}
	request, ok := activeRequest(state.Simulation)
	if !ok {
		t.Fatal("London demand generated no observable request")
	}
	profile := config.DemandProfiles[0]
	bandIndex := 0
	for index, band := range profile.Bands {
		if band.ID == config.Demand.Band {
			bandIndex = index
		}
	}
	for _, flow := range profile.Flows {
		if flow.From == request.From && flow.To == request.To && flow.Weights[bandIndex] > 0 {
			return
		}
	}
	t.Fatalf("generated request is absent from the selected London band: %+v", request)
}

func activeRequest(snapshot sim.Snapshot) (sim.Request, bool) {
	if len(snapshot.Pending) > 0 {
		return snapshot.Pending[0], true
	}
	for _, vehicle := range snapshot.Vehicles {
		if vehicle.Request != nil && !vehicle.Request.Completed {
			return *vehicle.Request, true
		}
	}
	return sim.Request{}, false
}
