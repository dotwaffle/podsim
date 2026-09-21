package session

import (
	"math/rand/v2"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// DemandConfig controls deterministic arrivals per simulated minute.
type DemandConfig = project.DemandConfig

// DemandState reports accepted and skipped generated orders for the current stream.
type DemandState struct {
	Config    DemandConfig `json:"config"`
	Generated int          `json:"generated"`
	Skipped   int          `json:"skipped"`
	Error     string       `json:"error,omitempty"`
}

type demandRun struct {
	state       DemandState
	rng         *rand.Rand
	budget      int
	passenger   []string
	destination int
}

func newDemand(config DemandConfig, network sim.Network) demandRun {
	stations := project.PassengerStations(network)
	passenger := make([]string, len(stations))
	for i, station := range stations {
		passenger[i] = station.ID
	}
	destination := len(passenger) - 1
	for i, id := range passenger {
		if config.Pattern == "destination" && id == config.Destination || config.Pattern == "market" && id == "market" {
			destination = i
			break
		}
	}
	return demandRun{
		state:       DemandState{Config: config},
		rng:         rand.New(rand.NewPCG(config.Seed, ^config.Seed)),
		passenger:   passenger,
		destination: destination,
	}
}

func (d *demandRun) configure(config DemandConfig, network sim.Network) error {
	if err := project.ValidateDemand(config, network); err != nil {
		return err
	}
	if config == d.state.Config {
		return nil
	}
	if config.Enabled {
		*d = newDemand(config, network)
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
	var from int
	to := d.destination
	if d.state.Config.Pattern == "balanced" {
		from = d.rng.IntN(len(d.passenger))
		to = d.rng.IntN(len(d.passenger) - 1)
		if to >= from {
			to++
		}
	} else {
		from = d.rng.IntN(len(d.passenger) - 1)
		if from >= to {
			from++
		}
	}
	if len(simulation.Snapshot().Pending) >= QueueLimit {
		d.state.Skipped++
		return
	}
	if err := simulation.RequestTrip(d.passenger[from], d.passenger[to]); err != nil {
		d.state.Skipped++
		d.state.Error = err.Error()
		return
	}
	d.state.Generated++
}
