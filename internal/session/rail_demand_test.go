package session

import (
	"bytes"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func railDemandProject() project.Config {
	config := project.Default()
	config.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: "rail-arrivals", Seed: 7}
	for index, count := range []int{3, 5, 7} {
		config.RailArrivals = append(config.RailArrivals, project.RailArrival{ID: strconv.Itoa(index), Station: "harbor", AtSeconds: index,
			Passengers: count, Destinations: []project.RailDestination{{Station: "market", Weight: 3}, {Station: "garden", Weight: 1}}})
	}
	return config
}

func newRailSession(t *testing.T, config project.Config) *Session {
	t.Helper()
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestRailDemandTimingAndQueueLoss(t *testing.T) {
	t.Parallel()
	s := newRailSession(t, railDemandProject())
	random, err := s.demand.pcg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for tick := int64(1); tick <= 130; tick++ {
		s.step()
		want := 3
		if tick >= sim.TicksPerSecond {
			want += 5
		}
		if tick >= 2*sim.TicksPerSecond {
			want += 7
		}
		if s.demand.state.Generated != want || s.simulation.Tick() != tick || s.simulation.Snapshot().Tick != tick {
			t.Fatalf("tick %d: generated %d, want %d", tick, s.demand.state.Generated, want)
		}
		// The release cannot repeat if demand is called twice at one tick.
		s.demand.step(s.simulation)
		if s.demand.state.Generated != want {
			t.Fatal("repeated a release")
		}
	}
	after, err := s.demand.pcg.MarshalBinary()
	if err != nil || !bytes.Equal(random, after) {
		t.Fatalf("rail releases changed the legacy random stream: %v", err)
	}
	config := railDemandProject()
	config.RailArrivals = config.RailArrivals[:1]
	config.RailArrivals[0].Passengers = 200
	full := newRailSession(t, config)
	for full.simulation.PendingCount() < QueueLimit {
		if err := full.simulation.RequestTrip("garden", "market"); err != nil {
			t.Fatal(err)
		}
	}
	full.step()
	if full.demand.state.Skipped != 200 || full.demand.state.Generated != 0 || full.demand.railCursor != 200 {
		t.Fatalf("full queue: %+v, cursor %d", full.demand.state, full.demand.railCursor)
	}
	for range 3 * sim.TicksPerSecond {
		full.step()
	}
	if full.demand.state.Skipped != 200 || full.demand.state.Generated != 0 {
		t.Fatal("retried skipped rail offers")
	}
}

func TestRailDemandEnableResetPauseAndSpeed(t *testing.T) {
	t.Parallel()
	for _, speed := range []int{1, 15, 60} {
		t.Run(strconv.Itoa(speed), func(t *testing.T) {
			t.Parallel()
			config := railDemandProject()
			config.Demand.Enabled = false
			s := newRailSession(t, config)
			s.speed = speed
			for s.simulation.Tick() < 60 {
				s.advance()
			}
			config.Demand.Enabled = true
			if err := s.applyDemand(config.Demand, nil); err != nil {
				t.Fatal(err)
			}
			if s.demand.railCursor != 8 || s.demand.state.Generated != 0 {
				t.Fatal("enable caught up past offers")
			}
			s.simulation.SetPaused(true)
			for range 10 {
				s.advance()
			}
			if s.simulation.Tick() != 60 || s.demand.state.Generated != 0 {
				t.Fatal("pause advanced rail time")
			}
			s.simulation.SetPaused(false)
			for s.simulation.Tick() < 120 {
				s.advance()
			}
			if s.demand.state.Generated != 7 {
				t.Fatalf("future offers: %+v", s.demand.state)
			}
			config.Demand.Enabled = false
			if err := s.applyDemand(config.Demand, nil); err != nil || s.demand.state.Generated != 7 {
				t.Fatalf("disable lost counters: %v", err)
			}
			client := newTestClient(s, "rail")
			client.mustApply(t, Command{Action: "reset"})
			if s.simulation.Tick() != 0 || s.demand.railCursor != 0 || s.demand.state.Generated != 0 {
				t.Fatal("reset retained rail progress")
			}
			config.Demand.Enabled = true
			if err := s.applyDemand(config.Demand, nil); err != nil {
				t.Fatal(err)
			}
			s.step()
			if s.demand.state.Generated != 3 {
				t.Fatal("reset did not restart rail plan")
			}
		})
	}
}

func TestRailDemandCloneAndPhysicalRestart(t *testing.T) {
	t.Parallel()
	for _, savedTick := range []int{0, 1, 59, 60, 61, 120} {
		t.Run(strconv.Itoa(savedTick), func(t *testing.T) {
			t.Parallel()
			s := newRailSession(t, railDemandProject())
			for range savedTick {
				s.step()
			}
			saved := sessionStateFile(t, s)
			saved.RestoreAttempts = 0
			restored := startFromStore(t, StoreInput{Store: &fakeStore{data: encodeTestState(t, saved)}})
			t.Cleanup(restored.Close)
			if restored.restore.Tier != "physical" {
				t.Fatalf("restore tier %s", restored.restore.Tier)
			}
			wantSaved, gotSaved := s.simulation.ExportState(), restored.simulation.ExportState()
			for index := range gotSaved.Pods {
				if math.Abs(gotSaved.Pods[index].Distance-wantSaved.Pods[index].Distance) > 1e-7 {
					t.Fatal("physical restore changed distance")
				}
				gotSaved.Pods[index].Distance = wantSaved.Pods[index].Distance
			}
			if !reflect.DeepEqual(wantSaved, gotSaved) {
				t.Fatal("physical restore changed saved state")
			}
			clonedSimulation, clonedDemand := s.simulation.Clone(), s.demand.clone()
			if &clonedDemand.railOffers[0] != &s.demand.railOffers[0] {
				t.Fatal("clone does not share immutable offers")
			}
			for tick := savedTick; tick < 600; tick++ {
				s.step()
				restored.step()
				clonedSimulation.Step()
				clonedDemand.step(clonedSimulation)
				want := s.simulation.Snapshot()
				if !reflect.DeepEqual(want, clonedSimulation.Snapshot()) {
					t.Fatalf("clone divergence after saved tick %d at tick %d", savedTick, tick+1)
				}
				// Physical restore resets speeds under the existing save contract.
				// Offers must retain their IDs, endpoints, and request timestamps.
				if !reflect.DeepEqual(railRequestIdentity(want), railRequestIdentity(restored.simulation.Snapshot())) {
					t.Fatal("physical restart changed offered request identity")
				}
				if _, err := restored.simulation.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}

				if s.demand.state != restored.demand.state || s.demand.state != clonedDemand.state || s.demand.railCursor != restored.demand.railCursor {
					t.Fatal("demand restore or clone divergence")
				}
			}
			if s.demand.state.Generated != 15 || clonedDemand.railCursor != 15 {
				t.Fatal("lost or repeated rail offers")
			}
		})
	}
}

func TestRailDemandSeedAndFixedVolume(t *testing.T) {
	t.Parallel()
	config := railDemandProject()
	s := newRailSession(t, config)
	s.step()
	config.Demand.Seed++
	if err := s.applyDemand(config.Demand, nil); err != nil {
		t.Fatal(err)
	}
	want := project.RailSchedule(config.RailArrivals, config.Demand.Seed)
	if s.demand.state.Generated != 0 || s.demand.railCursor != 3 || !slices.Equal(want, s.demand.railOffers) {
		t.Fatal("enabled reconfiguration did not reset counters and prepare future offers")
	}
	config.Demand.PerMinute = 1
	one := newRailSession(t, config)
	config.Demand.PerMinute = 120
	many := newRailSession(t, config)
	for range 180 {
		one.step()
		many.step()
		if !reflect.DeepEqual(one.simulation.Snapshot(), many.simulation.Snapshot()) || one.demand.state.Generated != many.demand.state.Generated {
			t.Fatal("rail demand volume depends on perMinute")
		}
	}
	if one.demand.state.Generated != 15 {
		t.Fatal("fixed plan volume changed")
	}
}

func TestRailDemandCheckpointReplay(t *testing.T) {
	t.Parallel()
	s := newRailSession(t, railDemandProject())
	client := newTestClient(s, "rail")
	for range 60 {
		s.step()
	}
	checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	for range 180 {
		s.step()
	}
	wantSimulation, wantDemand := s.simulation.Snapshot(), s.demand.state
	for range 2 {
		client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
		client.mustApply(t, Command{Action: "pause", Paused: false})
		for range 180 {
			s.step()
		}
		if !reflect.DeepEqual(wantSimulation, s.simulation.Snapshot()) || s.demand.state != wantDemand {
			t.Fatal("checkpoint replay changed rail demand or physics")
		}
	}
}

func railRequestIdentity(state sim.Snapshot) map[int]project.RailOffer {
	identities := make(map[int]project.RailOffer)
	add := func(request sim.Request) {
		identities[request.ID] = project.RailOffer{Tick: request.RequestedTick, From: request.From, To: request.To}
	}
	for _, request := range state.Pending {
		add(request)
	}
	for _, vehicle := range state.Vehicles {
		for _, request := range vehicle.Riders {
			add(request)
		}
	}
	return identities
}
