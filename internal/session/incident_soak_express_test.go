package session

import (
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// soakExpressPreset is the name of the Express project of the soak. No preset
// can run an Express pod, so the test builds this project. It is not a product
// preset, and only an explicit PODSIM_SOAK_PRESETS selects it.
const soakExpressPreset = "rail-hub-express"

// soakExpressPartiesPreset is the Express project with the party orders of the
// driver. Generated demand submits parties of one, so it never loads a pod past
// its seats, and the load dwell and the standees stay out of the run.
const soakExpressPartiesPreset = "rail-hub-express-parties"

// soakLondonExpressPreset is the opt-in LondonFull Express preset, project D
// of the Express qualification proposal. Only an explicit PODSIM_SOAK_PRESETS
// selects it.
const soakLondonExpressPreset = "london-full:dedicated"

// soakLondonSharedExpressPreset is the opt-in LondonFull Shared Express
// preset of decision D11 of the shape clearance design. Rows SX10 and SX11
// of the shared-track qualification run it as D runs, and the checks of the
// soak also run the D6 oracle at each tick. Only an explicit
// PODSIM_SOAK_PRESETS selects it.
const soakLondonSharedExpressPreset = "london-full:shared"

// soakLondonWharfExpressPreset is the opt-in LondonFull Shared Express Wharf
// preset. Rows HX10 and HX11 of the wharf qualification run it as the
// shared-track preset runs, with a second stream of hub parties (decision
// HX-D3). Only an explicit PODSIM_SOAK_PRESETS selects it.
const soakLondonWharfExpressPreset = "london-full:wharf"

// soakLondonHubPreset reports whether preset is a London Express preset
// with the hub party orders and the soak shape of the London soak row.
func soakLondonHubPreset(preset string) bool {
	return preset == soakLondonExpressPreset || preset == soakLondonSharedExpressPreset || preset == soakLondonWharfExpressPreset
}

// soakSharedExpress generates the shared-track preset one time for each
// test process. soakPreset returns a copy for each run.
var soakSharedExpress = sync.OnceValues(scenarios.LondonFullShared.Generate)

// soakWharfExpress is soakSharedExpress for the wharf preset.
var soakWharfExpress = sync.OnceValues(scenarios.LondonFullWharf.Generate)

// soakLondonExpressArms lists the arms of the London Express row of the
// qualification proposal, with the low-demand group. LondonFull sets the
// platoon limit that the platoon arms set, so those arms would repeat the
// incidents and reference arms.
var soakLondonExpressArms = []string{"incidents", "emergencies", "incidents-low-demand", "reference", "reference-low-demand"}

// soakExpressArms lists the arms of the Express project. A large class forms no
// platoon link, so the platoon arms would test nothing on an all-Express fleet.
var soakExpressArms = []string{"incidents", "emergencies", "reference"}

// soakExpressProject returns rail-hub with the express-v1 order contract, each
// class on each lane, station, and berth, and an Express pod for each place in
// the fleet. Each other setting stays as the preset has it.
func soakExpressProject(t *testing.T) project.Config {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	config := scenarios.RailHub()
	config.OrderContract = sim.ExpressOrderContract
	for index := range config.Network.Lanes {
		config.Network.Lanes[index].VehicleClasses = classes
	}
	for index := range config.Network.Stations {
		station := &config.Network.Stations[index]
		station.VehicleClasses = classes
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = classes
		}
	}
	for index := range config.Fleet {
		config.Fleet[index].Class = sim.ExpressClass
	}
	return config
}

// soakExpressParties returns the project of the party orders: pods can pool 4
// parties, as in the mixed fleet test, and two service pairs join the hub to one
// district. Demand falls to one order a minute, because at the 12 of the preset
// the queue is full after 25 minutes, and a party waits most of a branch to board.
func soakExpressParties(t *testing.T) project.Config {
	t.Helper()
	config := soakExpressProject(t)
	config.SharedRidePartyLimit = 4
	config.Demand.PerMinute = 1
	const hub = "station-01"
	var district string
	for index := range config.Network.Stations {
		if station := &config.Network.Stations[index]; !station.ParkingOnly && station.ID != hub {
			district = station.ID
			break
		}
	}
	config.ExpressServices = []sim.ExpressService{
		{ID: "hub-out", From: hub, To: district, Class: sim.ExpressClass, PartyLimit: sim.MaxExpressParties},
		{ID: "hub-in", From: district, To: hub, Class: sim.ExpressClass, PartyLimit: sim.MaxExpressParties},
	}
	return config
}

const (
	// soakPartyInterval is the mean time between two party orders.
	soakPartyInterval = 60 * sim.TicksPerSecond
	// soakHubPartyInterval is the mean time between two hub party orders on
	// the London Express preset. With a mean party of 10.5, it gives about
	// 42 passengers a minute, the hub rate of the screen of project D.
	soakHubPartyInterval = 15 * sim.TicksPerSecond
	// soakWharfPartyInterval and soakWharfOutbound are the mean time and the
	// outbound share of the hub party orders between London Bridge and Canary
	// Wharf (decision HX-D3). With a mean party of 10.5, they give about 19.1
	// passengers a minute out and 3.4 back, near the AM peak.
	soakWharfPartyInterval = 28 * sim.TicksPerSecond
	soakWharfOutbound      = 0.85
	// soakPartySeats is the seats of an Express pod. A load above it has standees.
	soakPartySeats = 12
)

// soakParties sends shared party orders of 1 to 20 passengers, and one in four
// uses a service pair. It has its own stream, so the driver draws the same
// incident events with it or without it. An order waits for a place in the queue,
// because the session refuses an order at the queue limit and generated demand
// can fill the queue. The orders are demand and not an incident event, so a
// reference arm has them too. It reads each second what the orders reach.
type soakParties struct {
	rng      *rand.Rand
	stations []string
	services []sim.ExpressService
	next     int64
	// mean is the mean time between two orders. On the London Express preset,
	// hub is true: each order uses a service, and outbound is the share of
	// the first service.
	mean     int64
	hub      bool
	outbound float64
	// second holds the orders of the second line of the wharf preset. Its
	// stream is its own, and the first line has the stream of D.
	second *soakParties
	// accepted and refused count the orders. passengers counts the passengers of
	// the accepted orders.
	accepted, refused, passengers int
	// peak is the largest load of a pod, and peakParties the most parties
	// aboard a pod. standeeSeconds counts the pod readings above the seats.
	peak, peakParties, standeeSeconds int
	// faults and emergencies hold the records that a reading found on a pod above
	// the seats. A replay of a rewind can add the same record with a new ID.
	faults, emergencies map[string]bool
}

// newSoakParties returns nil for each project that has no party order. On the
// London Express preset, the orders go between the hubs, three in four from
// the first service, and the low-demand arms send one order a minute, half in
// each direction. On the wharf preset, the second line has the same orders at
// its own rate.
func newSoakParties(input soakInput, config project.Config, tick int64) *soakParties {
	if input.preset != soakExpressPartiesPreset && !soakLondonHubPreset(input.preset) {
		return nil
	}
	p := &soakParties{rng: rand.New(rand.NewPCG(input.seed, 0x50415254+input.branch)), services: config.ExpressServices, faults: map[string]bool{}, emergencies: map[string]bool{},
		mean: soakPartyInterval}
	if soakLondonHubPreset(input.preset) {
		p.hub, p.mean, p.outbound = true, soakHubPartyInterval, 0.75
		if input.arm.perMinute != 0 {
			p.mean, p.outbound = soakPartyInterval, 0.5
		}
	}
	for index := range config.Network.Stations {
		if station := &config.Network.Stations[index]; !station.ParkingOnly {
			p.stations = append(p.stations, station.ID)
		}
	}
	p.next = tick + p.interval()
	if input.preset == soakLondonWharfExpressPreset {
		p.second = &soakParties{rng: rand.New(rand.NewPCG(input.seed, 0x57484152+input.branch)), services: config.ExpressServices[2:], hub: true, mean: soakWharfPartyInterval, outbound: soakWharfOutbound}
		if input.arm.perMinute != 0 {
			p.second.mean, p.second.outbound = soakPartyInterval, 0.5
		}
		p.second.next = tick + p.second.interval()
	}
	return p
}

func (p *soakParties) interval() int64 {
	return max(1, int64(p.rng.ExpFloat64()*float64(p.mean)))
}

// order returns the next party order.
func (p *soakParties) order() Command {
	command := Command{Action: "trip", PartySize: 1 + p.rng.IntN(sim.MaxExpressParties), SharingConsent: sim.SharedConsent, OrderContract: sim.ExpressOrderContract}
	if p.hub {
		service := p.services[0]
		if p.rng.Float64() >= p.outbound {
			service = p.services[1]
		}
		command.Origin, command.Destination, command.Service, command.ServiceID = service.From, service.To, sim.ExpressServiceChoice, service.ID
		return command
	}
	if p.rng.IntN(4) == 0 {
		service := p.services[p.rng.IntN(len(p.services))]
		command.Origin, command.Destination, command.Service, command.ServiceID = service.From, service.To, sim.ExpressServiceChoice, service.ID
		return command
	}
	origin := p.rng.IntN(len(p.stations))
	destination := (origin + 1 + p.rng.IntN(len(p.stations)-1)) % len(p.stations)
	command.Origin, command.Destination = p.stations[origin], p.stations[destination]
	return command
}

// run sends the order that is due, and reads the state at each second. A nil p
// does nothing.
func (p *soakParties) run(d *soakDriver, tick int64) {
	if p == nil {
		return
	}
	p.send(d, tick)
	if p.second != nil {
		p.second.send(d, tick)
	}
	if tick%soakViewTicks == 0 {
		p.read(d.s.simulation.Snapshot())
	}
}

// send sends the order that is due.
func (p *soakParties) send(d *soakDriver, tick int64) {
	if tick >= p.next && d.s.simulation.PendingCount() < QueueLimit {
		p.next = tick + p.interval()
		command := p.order()
		if reply := d.apply(command); reply.Error != "" {
			p.refused++
			d.t.Logf("the party order %+v is refused: %s", command, reply.Error)
		} else {
			p.accepted++
			p.passengers += command.PartySize
		}
	}
}

// read adds one reading of the state.
func (p *soakParties) read(state sim.Snapshot) {
	standing := map[string]bool{}
	for index := range state.Vehicles {
		vehicle := &state.Vehicles[index]
		load, parties := vehicle.PassengersAboard(), 0
		for _, rider := range vehicle.Riders {
			if !rider.Completed {
				parties++
			}
		}
		p.peak, p.peakParties = max(p.peak, load), max(p.peakParties, parties)
		if load > soakPartySeats {
			p.standeeSeconds++
			standing[vehicle.Pod.ID] = true
		}
	}
	for _, fault := range state.Faults.Active {
		if standing[fault.PodID] {
			p.faults[fault.ID] = true
		}
	}
	for _, emergency := range state.Emergencies.Active {
		if standing[emergency.PodID] {
			p.emergencies[emergency.ID] = true
		}
	}
}

// log reports what the orders reached. A nil p does nothing.
func (p *soakParties) log(t *testing.T, input soakInput) {
	t.Helper()
	if p == nil {
		return
	}
	t.Logf("parties preset=%s arm=%s seed=%d branch=%d accepted=%d refused=%d passengers=%d peakLoad=%d peakParties=%d standeeSeconds=%d standeeFaults=%d standeeEmergencies=%d", input.preset, input.arm.name, input.seed, input.branch, p.accepted, p.refused, p.passengers, p.peak, p.peakParties, p.standeeSeconds, len(p.faults), len(p.emergencies))
	if q := p.second; q != nil {
		t.Logf("second line parties preset=%s arm=%s seed=%d branch=%d accepted=%d refused=%d passengers=%d", input.preset, input.arm.name, input.seed, input.branch, q.accepted, q.refused, q.passengers)
	}
}

// TestSoakExpressProject checks that the project of the Express soak has only
// Express pods, and that the soak presets of the other arms have none.
func TestSoakExpressProject(t *testing.T) {
	t.Parallel()
	config := soakProject(t, soakExpressPreset, soakArms["incidents"], 1)
	if config.OrderContract != sim.ExpressOrderContract {
		t.Errorf("the order contract is %q", config.OrderContract)
	}
	for _, pod := range config.Fleet {
		if pod.Class != sim.ExpressClass {
			t.Errorf("pod %s has class %q", pod.ID, pod.Class)
		}
	}
	plain := soakPreset(t, "rail-hub")
	if plain.OrderContract != "" {
		t.Errorf("rail-hub has the order contract %q", plain.OrderContract)
	}
	for _, pod := range plain.Fleet {
		if pod.Class == sim.ExpressClass {
			t.Errorf("rail-hub has the Express pod %s", pod.ID)
		}
	}
	for _, name := range []string{soakExpressPreset, soakExpressPartiesPreset, soakLondonExpressPreset, soakLondonSharedExpressPreset, soakLondonWharfExpressPreset} {
		if slices.Contains(strings.Split(soakDefaultPresets, ","), name) {
			t.Errorf("the default presets %q hold the Express project %s", soakDefaultPresets, name)
		}
	}
}

// TestSoakExpressDriver runs a short soak of the Express project in the ordinary
// test run: a pod fault on an Express pod, a command fault, a command emergency,
// a save that restores in the physical tier, a rewind, and the clear.
func TestSoakExpressDriver(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing")
	}
	input := soakInput{preset: soakExpressPreset, arm: soakArms["incidents"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = 5*sim.TicksPerSecond/3, 100*sim.TicksPerSecond/3, 125*sim.TicksPerSecond/3, 50*sim.TicksPerSecond
	reply := d.client.mustApply(t, Command{Action: "fault", PodID: d.config.Fleet[0].ID})
	d.clears = append(d.clears, soakClear{reply.FaultID, 50 * sim.TicksPerSecond / 3})
	d.runHours()
	d.clearAndSettle()
	if r := d.result; r.commands == 0 || r.refused != 0 || r.saves != 1 || r.demoted != 0 || r.rewinds != 1 || r.emergencies == 0 || r.settleTicks == 0 || len(r.misses) != 0 {
		t.Fatalf("the driver result is %+v", r)
	}
	state := d.s.simulation.Snapshot()
	if state.Faults.Counters.Started == 0 || state.Emergencies.Counters.Started == 0 {
		t.Fatalf("the faults are %+v, and the emergencies are %+v", state.Faults, state.Emergencies)
	}
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.Class != sim.ExpressClass {
			t.Fatalf("pod %s has class %q", vehicle.Pod.ID, vehicle.Pod.Class)
		}
	}
}

// TestSoakExpressParties gives the queue a party of 20 first, so a pod has
// standees soon, and then the driver sends its own orders. The other projects
// get no party order.
func TestSoakExpressParties(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing")
	}
	input := soakInput{preset: soakExpressPartiesPreset, arm: soakArms["incidents"], seed: 1, hours: 0.05, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	p := d.parties
	if p == nil {
		t.Fatal("the Express parties project has no party order")
	}
	service := d.config.ExpressServices[0]
	d.mustApply(Command{Action: "trip", Origin: service.From, Destination: service.To, PartySize: sim.MaxExpressParties, SharingConsent: sim.SharedConsent, OrderContract: sim.ExpressOrderContract})
	p.next = 1
	d.runHours()
	d.clearAndSettle()
	if p.accepted == 0 || p.refused != 0 || p.passengers < p.accepted || p.peak <= soakPartySeats || p.standeeSeconds == 0 {
		t.Fatalf("the parties reach %+v", p)
	}
	for _, off := range []soakInput{
		{preset: soakExpressPreset, arm: soakArms["incidents"]},
		{preset: "rail-hub", arm: soakArms["incidents"]},
	} {
		if newSoakParties(off, d.config, 0) != nil {
			t.Errorf("preset %s arm %s has party orders", off.preset, off.arm.name)
		}
	}
}

// TestSoakExpressWarmBranch checks that a branch of the warm save still has the
// project of the warm run: the order contract and an Express class for each pod.
func TestSoakExpressWarmBranch(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing")
	}
	input := soakInput{preset: soakExpressPreset, arm: soakArms["incidents"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	input.warm = soakWarmUp(t, input, 30*sim.TicksPerSecond)
	input.branch = 1
	d := newSoakDriver(t, input)
	if d.s.project.OrderContract != sim.ExpressOrderContract {
		t.Errorf("the branch has the order contract %q", d.s.project.OrderContract)
	}
	state := d.s.simulation.Snapshot()
	if len(state.Vehicles) != len(d.config.Fleet) {
		t.Errorf("the branch has %d pods, and the fleet has %d", len(state.Vehicles), len(d.config.Fleet))
	}
	for index := range state.Vehicles {
		if pod := state.Vehicles[index].Pod; pod.Class != sim.ExpressClass {
			t.Errorf("pod %s has class %q in the branch", pod.ID, pod.Class)
		}
	}
}

// TestSoakLondonExpressParties checks the hub orders of the London Express
// preset: every order uses a service between the hubs, three in four go
// out at preset demand, and the low-demand arms send one a minute, half in
// each direction.
func TestSoakLondonExpressParties(t *testing.T) {
	t.Parallel()
	config := scenarios.LondonFullExpress()
	for _, test := range []struct {
		arm      string
		mean     int64
		outbound float64
	}{
		{"incidents", soakHubPartyInterval, 0.75},
		{"reference-low-demand", soakPartyInterval, 0.5},
	} {
		p := newSoakParties(soakInput{preset: soakLondonExpressPreset, arm: soakArms[test.arm], seed: 1}, config, 0)
		if p == nil || !p.hub || p.mean != test.mean || p.outbound != test.outbound {
			t.Fatalf("%s: parties %+v", test.arm, p)
		}
		out := 0
		const orders = 4000
		for range orders {
			command := p.order()
			service := slices.IndexFunc(config.ExpressServices, func(s sim.ExpressService) bool { return s.ID == command.ServiceID })
			if service < 0 || command.Service != sim.ExpressServiceChoice || command.Origin != config.ExpressServices[service].From || command.PartySize < 1 || command.PartySize > sim.MaxExpressParties {
				t.Fatalf("%s: order %+v", test.arm, command)
			}
			if service == 0 {
				out++
			}
		}
		if share := float64(out) / orders; share < test.outbound-0.03 || share > test.outbound+0.03 {
			t.Errorf("%s: outbound share %.3f, want about %.2f", test.arm, share, test.outbound)
		}
	}
}

// TestSoakLondonExpressDriver runs a short soak of the London Express preset
// with the checks: hub orders, a save that restores in the physical tier, and
// the clear. It uses the reference arm, because an incident on a London
// journey does not end in the short settle window. The soak runs the
// incident arms.
func TestSoakLondonExpressDriver(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("a LondonFull session, and one simulation on one goroutine")
	}
	input := soakInput{preset: soakLondonExpressPreset, arm: soakArms["reference"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	d.nextSave = 40 * sim.TicksPerSecond
	d.parties.next = 1
	d.runHours()
	d.clearAndSettle()
	if r := d.result; d.parties.accepted == 0 || r.refused != 0 || r.saves != 1 || r.demoted != 0 || len(r.misses) != 0 {
		t.Fatalf("the driver result is %+v, parties %+v", r, d.parties)
	}
}

// TestSoakLondonSharedExpressDriver runs a short soak of the LondonFull
// Shared Express preset with the checks, as TestSoakLondonExpressDriver does
// for D. The driver must run the D6 oracle at each tick. The
// test:express-shared task runs this test.
func TestSoakLondonSharedExpressDriver(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("a LondonFull session, and one simulation on one goroutine")
	}
	input := soakInput{preset: soakLondonSharedExpressPreset, arm: soakArms["reference"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	d.nextSave = 40 * sim.TicksPerSecond
	d.parties.next = 1
	d.runHours()
	d.clearAndSettle()
	if r := d.result; d.parties.accepted == 0 || r.refused != 0 || r.saves != 1 || r.demoted != 0 || len(r.misses) != 0 {
		t.Fatalf("the driver result is %+v, parties %+v", r, d.parties)
	}
	if ticks := d.s.simulation.Tick(); int64(d.lateralChecks) < ticks {
		t.Fatalf("%d ticks have %d runs of the D6 oracle", ticks, d.lateralChecks)
	}
}

// TestSoakLondonWharfExpressParties checks the two party streams of the wharf
// preset (decision HX-D3): the first line has the orders of the shared-track
// preset, and the second line has its own rate and share.
func TestSoakLondonWharfExpressParties(t *testing.T) {
	t.Parallel()
	config, err := soakWharfExpress()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		arm      string
		mean     int64
		outbound float64
	}{
		{"incidents", 28 * sim.TicksPerSecond, 0.85},
		{"reference-low-demand", 60 * sim.TicksPerSecond, 0.5},
	} {
		input := soakInput{preset: soakLondonWharfExpressPreset, arm: soakArms[test.arm], seed: 1}
		p := newSoakParties(input, config, 0)
		input.preset = soakLondonSharedExpressPreset
		shared := newSoakParties(input, config, 0)
		if p == nil || p.second == nil || p.second.mean != test.mean || p.second.outbound != test.outbound || shared.second != nil || p.next != shared.next {
			t.Fatalf("%s: parties %+v, want the first stream of %+v", test.arm, p, shared)
		}
		for range 100 {
			if got, want := p.order(), shared.order(); got != want {
				t.Fatalf("%s: first line order %+v, want %+v", test.arm, got, want)
			}
		}
		out := 0
		const orders = 4000
		for range orders {
			command := p.second.order()
			switch [2]string{command.Origin, command.Destination} {
			case [2]string{"940GZZLULNB", "940GZZLUCYF"}:
				out++
			case [2]string{"940GZZLUCYF", "940GZZLULNB"}:
			default:
				t.Fatalf("%s: order %+v", test.arm, command)
			}
		}
		if share := float64(out) / orders; share < test.outbound-0.03 || share > test.outbound+0.03 {
			t.Errorf("%s: outbound share %.3f, want about %.2f", test.arm, share, test.outbound)
		}
	}
}

// TestSoakLondonWharfExpressDriver is TestSoakLondonSharedExpressDriver on
// the wharf preset, with orders on both lines. The test:express-wharf task
// runs this test.
func TestSoakLondonWharfExpressDriver(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("a LondonFull session, and one simulation on one goroutine")
	}
	input := soakInput{preset: soakLondonWharfExpressPreset, arm: soakArms["reference"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	d.nextSave = 40 * sim.TicksPerSecond
	d.parties.next, d.parties.second.next = 1, 1
	d.runHours()
	d.clearAndSettle()
	if r := d.result; d.parties.accepted == 0 || d.parties.second.accepted == 0 || r.refused != 0 || r.saves != 1 || r.demoted != 0 || len(r.misses) != 0 {
		t.Fatalf("the driver result is %+v, parties %+v", r, d.parties)
	}
	if ticks := d.s.simulation.Tick(); int64(d.lateralChecks) < ticks {
		t.Fatalf("%d ticks have %d runs of the D6 oracle", ticks, d.lateralChecks)
	}
}
