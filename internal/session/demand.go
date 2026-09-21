package session

import (
	"errors"
	"math/rand/v2"

	"github.com/dotwaffle/podsim/internal/sim"
)

// DemandConfig controls deterministic arrivals per simulated minute.
type DemandConfig struct {
	Enabled   bool   `json:"enabled"`
	PerMinute int    `json:"perMinute"`
	Pattern   string `json:"pattern"`
	Seed      uint64 `json:"seed"`
}

// DemandState reports accepted and skipped generated orders for the current stream.
type DemandState struct {
	Config    DemandConfig `json:"config"`
	Generated int          `json:"generated"`
	Skipped   int          `json:"skipped"`
	Error     string       `json:"error,omitempty"`
}

type demandRun struct {
	state  DemandState
	rng    *rand.Rand
	budget int
}

func newDemand() demandRun {
	return demandRun{state: DemandState{Config: DemandConfig{PerMinute: 2, Pattern: "balanced", Seed: 1}}}
}

func (d *demandRun) configure(config DemandConfig) error {
	if config.PerMinute < 1 || config.PerMinute > 12 {
		return errors.New("demand rate must be 1 to 12 orders per simulated minute")
	}
	if config.Pattern != "balanced" && config.Pattern != "market" {
		return errors.New("demand pattern must be balanced or market")
	}
	if config == d.state.Config {
		return nil
	}
	if config.Enabled {
		d.state = DemandState{Config: config}
		d.rng = rand.New(rand.NewPCG(config.Seed, ^config.Seed))
		d.budget = 0
	} else {
		d.state.Config = config
	}
	return nil
}

func (d *demandRun) step(simulation *sim.Simulation) {
	if !d.state.Config.Enabled {
		return
	}
	d.budget += d.state.Config.PerMinute
	if d.budget < 60*sim.TicksPerSecond {
		return
	}
	d.budget -= 60 * sim.TicksPerSecond
	stations := []string{"harbor", "garden", "market"}
	from, to := 0, 2
	if d.state.Config.Pattern == "market" {
		from = d.rng.IntN(2)
	} else {
		from = d.rng.IntN(3)
		to = d.rng.IntN(2)
		if to >= from {
			to++
		}
	}
	if len(simulation.Snapshot().Pending) >= QueueLimit {
		d.state.Skipped++
		return
	}
	if err := simulation.RequestTrip(stations[from], stations[to]); err != nil {
		d.state.Skipped++
		d.state.Error = err.Error()
		return
	}
	d.state.Generated++
}
