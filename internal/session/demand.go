package session

import (
	"math/rand/v2"
	"sort"

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

// demandRun is the live demand stream. clone copies pcg and rng, because
// each draw changes them. Clones share the other reference fields. newDemand
// and its helpers fill them. After newDemand returns, code replaces them
// whole and never writes to them in place.
type demandRun struct {
	state         DemandState
	pcg           *rand.PCG
	rng           *rand.Rand
	budget        int
	passenger     []string
	destination   int
	profileFlows  []weightedDemandFlow
	profileTotal  float64
	pickupWeights map[string]float64
}

type demandInput struct {
	config   DemandConfig
	network  sim.Network
	profiles []project.DemandProfile
}

type weightedDemandFlow struct {
	from       string
	to         string
	cumulative float64
}

func newDemand(input demandInput) demandRun {
	stations := project.PassengerStations(input.network)
	passenger := make([]string, len(stations))
	for i, station := range stations {
		passenger[i] = station.ID
	}
	destination := len(passenger) - 1
	for i, id := range passenger {
		if input.config.Pattern == "destination" && id == input.config.Destination || input.config.Pattern == "market" && id == "market" {
			destination = i
			break
		}
	}
	pcg := rand.NewPCG(input.config.Seed, ^input.config.Seed)
	run := demandRun{
		state:         DemandState{Config: input.config},
		pcg:           pcg,
		rng:           rand.New(pcg),
		passenger:     passenger,
		destination:   destination,
		pickupWeights: make(map[string]float64, len(passenger)),
	}
	run.prepareLegacyWeights()
	if input.config.Pattern == "profile" {
		run.prepareProfile(input.profiles)
	}
	return run
}

func (d *demandRun) prepareLegacyWeights() {
	for index, id := range d.passenger {
		d.pickupWeights[id] = 1
		if d.state.Config.Pattern != "balanced" && index == d.destination {
			d.pickupWeights[id] = 0
		}
	}
}

// prepareProfile writes to pickupWeights and profileFlows in place. Only
// newDemand calls it. After newDemand returns, code replaces these fields
// whole, because clones share them.
func (d *demandRun) prepareProfile(profiles []project.DemandProfile) {
	var selected project.DemandProfile
	for _, profile := range profiles {
		if profile.ID == d.state.Config.Profile {
			selected = profile
			break
		}
	}
	bandIndex := 0
	for index, band := range selected.Bands {
		if band.ID == d.state.Config.Band {
			bandIndex = index
			break
		}
	}
	clear(d.pickupWeights)
	for _, flow := range selected.Flows {
		weight := flow.Weights[bandIndex]
		if weight == 0 {
			continue
		}
		d.profileTotal += weight
		d.pickupWeights[flow.From] += weight
		d.profileFlows = append(d.profileFlows, weightedDemandFlow{from: flow.From, to: flow.To, cumulative: d.profileTotal})
	}
}

// clone returns an independent stream that draws the same sequence as d. It
// shares the fields that code never writes to in place.
func (d *demandRun) clone() demandRun {
	c := *d
	if d.pcg != nil {
		c.pcg = new(*d.pcg)
		c.rng = rand.New(c.pcg)
	}
	return c
}

func (d *demandRun) configure(input demandInput) error {
	if err := project.ValidateDemand(input.config, project.DemandContext{Network: input.network, Profiles: input.profiles}); err != nil {
		return err
	}
	if input.config == d.state.Config {
		return nil
	}
	if input.config.Enabled {
		*d = newDemand(input)
	} else {
		d.state.Config = input.config
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
	from, to := d.nextPair()
	if len(simulation.Snapshot().Pending) >= QueueLimit {
		d.state.Skipped++
		return
	}
	if err := simulation.RequestTrip(from, to); err != nil {
		d.state.Skipped++
		d.state.Error = err.Error()
		return
	}
	d.state.Generated++
}

func (d *demandRun) nextPair() (string, string) {
	if d.state.Config.Pattern == "profile" {
		target := d.rng.Float64() * d.profileTotal
		index := sort.Search(len(d.profileFlows), func(index int) bool {
			return d.profileFlows[index].cumulative > target
		})
		flow := d.profileFlows[index]
		return flow.from, flow.to
	}
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
	return d.passenger[from], d.passenger[to]
}
