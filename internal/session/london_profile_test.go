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

// londonRewindWarmupSeconds is short, because the queue grows and each
// simulated second on London then costs more. With the race detector, each
// 60 s window takes about 4 s and the test takes about 14 s.
const londonRewindWarmupSeconds = 5

func TestLondonRewindReplaysExactly(t *testing.T) {
	t.Parallel()
	config := scenarios.London()
	config.Demand.Enabled = true
	config.Redistribution = true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(shared, "test")
	client.mustApply(t, Command{Action: "speed", Speed: 8})
	advanceTicks(shared, londonRewindWarmupSeconds*sim.TicksPerSecond/8)
	result := checkReplays(t, replayCheck{client: client, seconds: 60})
	start, end := result.start.state, result.end.state
	if len(result.start.demand.profileFlows) == 0 || end.Demand.Generated <= start.Demand.Generated {
		t.Fatalf("London profile demand did not generate in the replay window: %+v", end.Demand)
	}
	if end.Simulation.RebalanceMoves <= start.Simulation.RebalanceMoves {
		t.Fatal("London redistribution did not move a pod in the replay window")
	}
	t.Logf("ticks=%d..%d generated=%d..%d submitted=%d..%d completed=%d..%d rebalance=%d..%d",
		start.Simulation.Tick, end.Simulation.Tick, start.Demand.Generated, end.Demand.Generated,
		start.Simulation.Submitted, end.Simulation.Submitted, start.Simulation.Completed, end.Simulation.Completed,
		start.Simulation.RebalanceMoves, end.Simulation.RebalanceMoves)
}
