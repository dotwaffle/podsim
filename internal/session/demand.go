package session

import (
	"math/rand/v2"
	"sort"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

// DemandConfig controls deterministic arrivals per simulated minute.
type DemandConfig = project.DemandConfig

// DemandState reports accepted and skipped generated orders for the current stream.
type DemandState struct {
	Config      DemandConfig `json:"config"`
	Generated   int          `json:"generated"`
	Skipped     int          `json:"skipped"`
	Connections rail.Counts  `json:"connections,omitzero"`
	Error       string       `json:"error,omitempty"`
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
	railOffers    []project.RailOffer
	railCursor    int
	serviceOffers []project.RailServiceOffer
	serviceCursor int
	connections   *rail.Connections
}

type demandInput struct {
	config     DemandConfig
	network    sim.Network
	profiles   []project.DemandProfile
	arrivals   []project.RailArrival
	departures []project.RailDeparture
	tick       int64
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
	if len(input.departures) > 0 {
		run.connections = rail.NewConnections(input.departures)
	}
	run.prepareLegacyWeights()
	if input.config.Pattern == "profile" {
		run.prepareProfile(input.profiles)
	}
	if input.config.Pattern == "rail-arrivals" {
		run.railOffers = project.RailSchedule(input.arrivals, input.config.Seed)
		run.railCursor = sort.Search(len(run.railOffers), func(index int) bool { return run.railOffers[index].Tick > input.tick })
		clear(run.pickupWeights)
		for _, arrival := range input.arrivals {
			run.pickupWeights[arrival.Station] += float64(arrival.Passengers)
		}
	}
	if input.config.Pattern == "rail-services" {
		run.serviceOffers = project.RailServicesSchedule(input.arrivals, input.departures, input.config.Seed)
		run.serviceCursor = sort.Search(len(run.serviceOffers), func(i int) bool { return run.serviceOffers[i].Tick > input.tick })
		clear(run.pickupWeights)
		for _, offer := range run.serviceOffers {
			run.pickupWeights[offer.From]++
		}
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
	c.connections = d.connections.Clone()
	if d.pcg != nil {
		c.pcg = new(*d.pcg)
		c.rng = rand.New(c.pcg)
	}
	return c
}

func (d *demandRun) configure(input demandInput) error {
	if err := project.ValidateDemand(input.config, project.DemandContext{Network: input.network, Profiles: input.profiles, RailArrivals: input.arrivals, RailDepartures: input.departures}); err != nil {
		return err
	}
	if input.config == d.state.Config {
		return nil
	}
	if input.config.Enabled {
		connections := d.connections
		*d = newDemand(input)
		d.connections = connections
		if connections != nil {
			d.state.Connections = connections.Counts()
		}
	} else {
		d.state.Config = input.config
	}
	return nil
}

func (d *demandRun) step(simulation *sim.Simulation) {
	if d.connections != nil {
		d.connections.Advance(simulation.Tick(), simulation.StepCompletions())
		d.state.Connections = d.connections.Counts()
	}
	if !d.state.Config.Enabled {
		return
	}
	if d.state.Config.Pattern == "rail-services" {
		d.releaseServices(simulation)
		return
	}
	if d.state.Config.Pattern == "rail-arrivals" {
		d.releaseRail(simulation)
		return
	}
	d.budget += d.state.Config.PerMinute
	if d.budget < 60*sim.TicksPerSecond {
		return
	}
	d.budget -= 60 * sim.TicksPerSecond
	from, to := d.nextPair()
	d.offer(simulation, from, to)
}

func (d *demandRun) offer(simulation *sim.Simulation, from, to string) {
	if simulation.PendingCount() >= QueueLimit {
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

// releaseRail consumes this tick once and skips past offers without catch-up.
func (d *demandRun) releaseRail(simulation *sim.Simulation) {
	tick := simulation.Tick()
	for d.railCursor < len(d.railOffers) && d.railOffers[d.railCursor].Tick <= tick {
		offer := d.railOffers[d.railCursor]
		d.railCursor++
		if offer.Tick == tick {
			d.offer(simulation, offer.From, offer.To)
		}
	}
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

// releaseServices issues offers after physics and retains rejected identities.
func (d *demandRun) releaseServices(simulation *sim.Simulation) {
	tick := simulation.Tick()
	for d.serviceCursor < len(d.serviceOffers) && d.serviceOffers[d.serviceCursor].Tick <= tick {
		offer := d.serviceOffers[d.serviceCursor]
		d.serviceCursor++
		if offer.Tick != tick {
			continue
		}
		if offer.Kind == "arrival" {
			d.offer(simulation, offer.From, offer.To)
			continue
		}
		if d.connections.Issued(offer.Event, offer.Passenger) {
			continue
		}
		requestID, reason := 0, ""
		if simulation.PendingCount() >= QueueLimit {
			d.state.Skipped++
			reason = "queue-limit"
		} else {
			var err error
			requestID, err = simulation.SubmitTrip(offer.From, offer.To)
			if err != nil {
				d.state.Skipped++
				d.state.Error = err.Error()
				reason = "request-error"
			} else {
				d.state.Generated++
			}
		}
		if err := d.connections.Add(offer, requestID, reason); err != nil {
			d.state.Error = err.Error()
		}
	}
	if d.connections != nil {
		d.state.Connections = d.connections.Counts()
	}
}

func (d *demandRun) connectionRecords() []rail.Connection {
	if d.connections == nil {
		return nil
	}
	return d.connections.Records()
}
