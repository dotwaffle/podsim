package session

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func dailyDemandProject() project.Config {
	config := profileDemandProject()
	config.Demand.Pattern = "profile-daily"
	config.Demand.Band = ""
	config.Demand.DailyStartMinute = 420
	config.DemandProfiles[0].Bands = []project.DemandBand{
		{ID: "am", Name: "Morning", StartMinute: 420, DurationMinutes: 1, PerMinute: new(1)},
		{ID: "pm", Name: "Evening", StartMinute: 422, DurationMinutes: 1, PerMinute: new(2)},
	}
	config.DemandProfiles[0].Flows = []project.DemandFlow{
		{From: "harbor", To: "garden", Weights: []float64{1, 0}},
		{From: "garden", To: "harbor", Weights: []float64{0, 1}},
	}
	return config
}

func TestDailyDemandBoundariesAndQuietRedistribution(t *testing.T) {
	t.Parallel()
	config := dailyDemandProject()
	config.Redistribution = true
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for range 3600 {
		s.step()
	}
	if s.demand.state.Generated != 1 || s.demand.dailyBand != 0 || s.simulation.DemandRate() != 1 {
		t.Fatal("first minute did not produce one request")
	}
	s.step()
	if s.demand.dailyBand != -1 || s.simulation.Positioning() != sim.PositioningOff || s.demand.budget != 0 {
		t.Fatal("gap did not disable proactive moves")
	}
	for s.simulation.Tick() < 7201 {
		s.step()
	}
	if s.demand.dailyBand != 1 || s.simulation.Positioning() != sim.PositioningGuarded || s.simulation.DemandRate() != 2 || !reflect.DeepEqual(s.demand.pickupWeights, map[string]float64{"garden": 1}) {
		t.Fatal("evening band did not replace demand/positioning")
	}
	for s.simulation.Tick() < 10800 {
		s.step()
	}
	if s.demand.state.Generated != 3 {
		t.Fatalf("generated %d, want3", s.demand.state.Generated)
	}
	s.step()
	if s.simulation.Positioning() != sim.PositioningOff {
		t.Fatal("end of evening did not disable positioning")
	}
}

func TestDailyDemandFractionalRestoreAndClone(t *testing.T) {
	t.Parallel()
	for _, tick := range []int64{0, 3599, 3600, 3601, 7199, 7200, 7201, 86400 * sim.TicksPerSecond} {
		t.Run(strconv.FormatInt(tick, 10), func(t *testing.T) {
			t.Parallel()
			config := dailyDemandProject()
			s, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			state := s.simulation.ExportState()
			state.Tick = tick
			s.simulation, _, err = sim.RestoreState(sim.RestoreStateInput{Network: config.Network, Fleet: config.Fleet, State: state})
			if err != nil {
				t.Fatal(err)
			}
			s.demand = newDemand(demandInput{config: config.Demand, network: config.Network, profiles: config.DemandProfiles, tick: tick})
			s.demand.budget = 3599
			stored := sessionStateFile(t, s)
			stored.RestoreAttempts = 0
			restored := startFromStore(t, StoreInput{Store: &fakeStore{data: encodeTestState(t, stored)}})
			t.Cleanup(restored.Close)
			if restored.restore.Tier != "physical" || restored.demand.budget != 3599 || restored.demand.dailyBand != s.demand.dailyBand {
				t.Fatal("restore lost active band or fractional budget")
			}
			clone := s.demand.clone()
			if clone.daily != s.demand.daily {
				t.Fatal("clone copied immutable samplers")
			}
			for range 180 {
				s.step()
				restored.step()
				if s.demand.state != restored.demand.state || s.demand.budget != restored.demand.budget || s.demand.dailyBand != restored.demand.dailyBand {
					t.Fatal("restored demand diverged")
				}
				wantRandom, _ := s.demand.pcg.MarshalBinary()
				gotRandom, _ := restored.demand.pcg.MarshalBinary()
				if !reflect.DeepEqual(wantRandom, gotRandom) {
					t.Fatal("restored random stream diverged")
				}
			}
			if clone.budget != 3599 {
				t.Fatal("live boundary changed clone budget")
			}
		})
	}
}

func TestDailyDemandDisableResetAndCheckpoint(t *testing.T) {
	t.Parallel()
	config := dailyDemandProject()
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	client := newTestClient(s, "daily")
	for range 3540 {
		s.step()
	}
	checkReplays(t, replayCheck{client: client, seconds: 62})
	disabled := config.Demand
	disabled.Enabled = false
	if err := s.applyDemand(disabled, nil); err != nil {
		t.Fatal(err)
	}
	generated := s.demand.state.Generated
	for range 10800 {
		s.step()
	}
	if s.demand.state.Generated != generated {
		t.Fatal("disabled demand generated offers")
	}
	if err := s.applyDemand(config.Demand, nil); err != nil {
		t.Fatal(err)
	}
	if s.demand.state.Generated != 0 || s.demand.budget != 0 || s.demand.dailyBand != -1 {
		t.Fatal("re-enable caught up past bands")
	}
	client.mustApply(t, Command{Action: "reset"})
	if s.simulation.Tick() != 0 || s.demand.dailyBand != 0 || s.demand.budget != 0 {
		t.Fatal("reset did not restart daily clock")
	}
	legacy := project.Default().Demand
	if err := s.applyDemand(legacy, nil); err != nil {
		t.Fatal(err)
	}
	if s.demand.activateDaily(7201) {
		t.Fatal("disabled legacy pattern activated a stale daily sampler")
	}
}

func TestDailyDemandFullDayOccurrence(t *testing.T) {
	t.Parallel()
	config := dailyDemandProject()
	config.Demand.DailyStartMinute = 0
	config.DemandProfiles[0].Bands = []project.DemandBand{{ID: "day", Name: "All day", DurationMinutes: 1440, PerMinute: new(1)}}
	config.DemandProfiles[0].Flows = []project.DemandFlow{{From: "harbor", To: "garden", Weights: []float64{1}}}
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	// Re-enable at tick one, then retain the partial budget at the next day boundary.
	state := s.simulation.ExportState()
	state.Tick = 86400 * sim.TicksPerSecond
	s.simulation, _, err = sim.RestoreState(sim.RestoreStateInput{Network: config.Network, Fleet: config.Fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	s.demand = newDemand(demandInput{config: config.Demand, network: config.Network, profiles: config.DemandProfiles, tick: 1})
	s.demand.budget = 3599
	s.step()
	if s.demand.budget != 1 || s.demand.state.Generated != 0 || s.demand.dailyDay != 1 {
		t.Fatalf("day boundary retained budget: budget=%d generated=%d occurrence=%d", s.demand.budget, s.demand.state.Generated, s.demand.dailyDay)
	}
	// Calling activation twice at the same boundary cannot reset an issued budget.
	if s.demand.activateDaily(state.Tick+1) || s.demand.budget != 1 {
		t.Fatal("same occurrence reset the budget twice")
	}
}

func TestDailyDemandWrappingOccurrence(t *testing.T) {
	t.Parallel()
	config := dailyDemandProject()
	config.Demand.DailyStartMinute = 1439
	config.DemandProfiles[0].Bands = []project.DemandBand{{ID: "night", Name: "Night", StartMinute: 1380, DurationMinutes: 120, PerMinute: new(1)}}
	config.DemandProfiles[0].Flows = []project.DemandFlow{{From: "harbor", To: "garden", Weights: []float64{1}}}
	run := newDemand(demandInput{config: config.Demand, network: config.Network, profiles: config.DemandProfiles})
	run.budget = 3599
	if run.activateDaily(3601) || run.budget != 3599 {
		t.Fatal("midnight reset the wrapping band's budget")
	}
	if !run.activateDaily(1381*3600+1) || run.budget != 0 || run.dailyDay != 1 {
		t.Fatal("next band occurrence did not reset the budget")
	}
}
