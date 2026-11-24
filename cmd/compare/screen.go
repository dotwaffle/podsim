package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The screen of docs/contracts/express-london-adoption-screen-proposal.md
// runs LondonFull (LF), the London Express project without its Express pods
// (D0), and the London Express project (D) on the same offered events.

const (
	screenLondonFull = "LF"
	screenGeometry   = "D0"
	screenExpress    = "D"
	// screenShared and screenSharedPlain are the shared-track Express
	// project with and without its Express pods.
	screenShared      = "S"
	screenSharedPlain = "S0"
	// screenWharf and screenWharfPlain are the shared-track Express project
	// with the London Bridge and Canary Wharf line, with and without its
	// Express pods.
	screenWharf      = "W"
	screenWharfPlain = "W0"
	// screenHubMean is the default and the shortest mean time between two
	// hub events, and screenHubOutbound the share of the events from Waterloo. The values are
	// the hub stream of the D soak.
	screenHubMean     = 15 * sim.TicksPerSecond
	screenHubOutbound = 0.75
	screenWaterloo    = "940GZZLUWLO"
	screenBank        = "940GZZLUBNK"
	// The wharf line has the AM peak rates of decision HX-D3 of the Express
	// wharf qualification.
	screenLondonBridge  = "940GZZLULNB"
	screenCanaryWharf   = "940GZZLUCYF"
	screenWharfMean     = 28 * sim.TicksPerSecond
	screenWharfOutbound = 0.85
	// screenMaxLines is the largest number of hub lines of a screen.
	screenMaxLines = 2
	// screenHubStream separates the stream of the hub events from the stream
	// of the background schedule.
	screenHubStream = 0x48554245
	// screenWharfStream separates the hub events of the wharf line.
	screenWharfStream = 0x57484152
	// screenMaxBackgroundPerMinute is the highest background rate with an
	// interval of at least one tick between two requests.
	screenMaxBackgroundPerMinute = 60 * sim.TicksPerSecond
)

// screenOptions holds the options of the screen command.
type screenOptions struct {
	seeds             []int64
	bands             []string
	configs           []string
	arrivals, clear   time.Duration
	hubMean           time.Duration
	backgroundPerMin  int
	queueLimit        int
	workers           int
	checkEverySeconds int
	output            string
}

func parseScreenOptions(args []string, stderr io.Writer) (screenOptions, error) {
	flags := flag.NewFlagSet("compare screen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	seeds := flags.String("seeds", "1,2,3", "comma-separated seeds")
	bands := flags.String("bands", "busy,quiet", "comma-separated bands: busy, quiet, or LondonFull band IDs")
	configs := flags.String("configs", "LF,D0,D", "comma-separated configurations: LF, D0, D, S0, S, W0, W")
	opts := screenOptions{}
	flags.DurationVar(&opts.arrivals, "arrivals", 2*time.Hour, "simulated time with offered events")
	flags.DurationVar(&opts.clear, "clear", 2*time.Hour, "longest simulated time after the arrivals")
	flags.DurationVar(&opts.hubMean, "hub-mean", screenHubMean/sim.TicksPerSecond*time.Second, "mean time between two hub events of Waterloo and Bank, at least the default")
	flags.IntVar(&opts.backgroundPerMin, "background-per-minute", 0, "background requests a minute; 0 uses the LondonFull preset rate")
	flags.IntVar(&opts.queueLimit, "queue-limit", 200, "live queue limit")
	flags.IntVar(&opts.workers, "workers", 3, "arms that run at the same time")
	flags.IntVar(&opts.checkEverySeconds, "check-every", 1, "simulated seconds between two oracle checks")
	flags.StringVar(&opts.output, "output", "", "file of the JSON report")
	if err := flags.Parse(args); err != nil {
		return screenOptions{}, err
	}
	if flags.NArg() != 0 {
		return screenOptions{}, fmt.Errorf("unexpected screen argument %q", flags.Arg(0))
	}
	if strings.Count(*seeds, ",") >= maxSeeds {
		return screenOptions{}, fmt.Errorf("seeds must contain at most %d values", maxSeeds)
	}
	for part := range strings.SplitSeq(*seeds, ",") {
		seed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || seed < 1 || slices.Contains(opts.seeds, seed) {
			return screenOptions{}, fmt.Errorf("seeds must be distinct positive integers, got %q", part)
		}
		opts.seeds = append(opts.seeds, seed)
	}
	opts.bands = strings.Split(*bands, ",")
	opts.configs = strings.Split(*configs, ",")
	for _, name := range opts.configs {
		switch name {
		case screenLondonFull, screenGeometry, screenExpress, screenShared, screenSharedPlain, screenWharf, screenWharfPlain:
		default:
			return screenOptions{}, fmt.Errorf("unknown screen configuration %q", name)
		}
	}
	switch {
	case opts.arrivals <= 0 || opts.arrivals > maxDuration || opts.clear < 0 || opts.clear > maxDuration:
		return screenOptions{}, errors.New("arrivals must be positive, and arrivals and clear at most 24h")
	case opts.backgroundPerMin < 0 || opts.backgroundPerMin > screenMaxBackgroundPerMinute || opts.queueLimit < 1 || opts.queueLimit > maxQueueLimit:
		return screenOptions{}, fmt.Errorf("background-per-minute must be from 0 to %d and queue-limit from 1 to the queue cap", screenMaxBackgroundPerMinute)
	case opts.hubMean < screenHubMean/sim.TicksPerSecond*time.Second || opts.hubMean > maxDuration:
		return screenOptions{}, errors.New("hub-mean must be from 15s to 24h")
	case opts.workers < 1 || opts.workers > maxWorkers || opts.checkEverySeconds < 1:
		return screenOptions{}, errors.New("workers must be from 1 to 64 and check-every at least 1")
	}
	if opts.backgroundPerMin == 0 {
		opts.backgroundPerMin = scenarios.LondonFull().Demand.PerMinute
	}
	if err := checkScreenBytes(opts); err != nil {
		return screenOptions{}, err
	}
	return opts, nil
}

// The sizes of the screen storage on a 64-bit platform.
// TestScreenStorageBytes checks them.
const (
	screenEventBytes   = 64
	screenOutcomeBytes = 72
	// screenOrderBytes is the order list of a background event, which has
	// one order.
	screenOrderBytes = 8
	// screenHubOrderBytes is the largest order list of a hub event. A hub
	// party splits into at most sim.MaxExpressParties orders, and append
	// gives a list of 32 orders for them.
	screenHubOrderBytes = 32 * 8
)

// screenBackgroundInterval returns the ticks between two background
// requests at perMinute requests a minute.
func screenBackgroundInterval(perMinute int) int64 {
	return 60 * sim.TicksPerSecond / int64(perMinute)
}

// screenHubLimit returns the largest number of hub events of a line in an
// arrival window. It is twice the mean number at screenHubMean and 32 more, so a seed does not get
// near it. screenHubEvents refuses a stream with more events, so the
// storage of a screen cannot be larger than checkScreenBytes counts.
func screenHubLimit(arrivalTicks int64) int64 {
	return 2*(arrivalTicks/screenHubMean) + 32
}

// maxScreenBytes bounds the offered events, the outcomes, and one
// background schedule of a screen. It holds one band and seed of 24 hours
// at one request each tick in the three configurations, which need about
// 1000 MiB.
const maxScreenBytes = 1 << 30

// checkScreenBytes refuses a screen whose offered events, outcomes, and
// background schedule need more than maxScreenBytes. The events of each
// band and seed and the outcomes of each arm stay in memory until the
// report. One background schedule is in memory while the screen builds the
// events of a band and seed. The bound does not count the memory of a
// simulation. opts.backgroundPerMin must be the rate that the screen uses.
func checkScreenBytes(opts screenOptions) error {
	arrivalTicks := durationTicks(opts.arrivals)
	background := intervalRequestCount(arrivalTicks, screenBackgroundInterval(opts.backgroundPerMin))
	hub := screenHubLimit(arrivalTicks)
	if slices.Contains(opts.configs, screenWharf) || slices.Contains(opts.configs, screenWharfPlain) {
		hub *= screenMaxLines
	}
	configs := float64(len(opts.configs))
	events := float64(background)*(screenEventBytes+configs*(screenOutcomeBytes+screenOrderBytes)) +
		float64(hub)*(screenEventBytes+configs*(screenOutcomeBytes+screenHubOrderBytes))
	// A float64 holds each total below 2^53 bytes exactly, so the result is
	// exact near the bound, and a larger total cannot overflow.
	total := float64(len(opts.bands))*float64(len(opts.seeds))*events + float64(background*scheduleRequestBytes)
	if total > maxScreenBytes {
		return fmt.Errorf("the screen storage must fit in %d MiB, and this screen needs %.0f MiB; use fewer seeds, bands, or configurations, a lower background rate, or a shorter arrival window",
			maxScreenBytes>>20, math.Ceil(total/(1<<20)))
	}
	return nil
}

// screenConfig is one configuration of the screen.
type screenConfig struct {
	name   string
	config project.Config
	// lines holds the hub lines of the Express services of the preset.
	// A configuration without its Express services keeps them.
	lines []screenLine
}

// screenLine is a pair of hubs with a hub stream. Outbound is from From to
// To.
type screenLine struct {
	from, to  string
	meanTicks int64
	outbound  float64
	stream    uint64
}

// screenKnownLines returns the lines that have rates, in the order of the
// events of one tick. hubMeanTicks is the mean of the first line.
func screenKnownLines(hubMeanTicks int64) []screenLine {
	return []screenLine{
		{from: screenWaterloo, to: screenBank, meanTicks: hubMeanTicks, outbound: screenHubOutbound, stream: screenHubStream},
		{from: screenLondonBridge, to: screenCanaryWharf, meanTicks: screenWharfMean, outbound: screenWharfOutbound, stream: screenWharfStream},
	}
}

// screenServiceLines returns the known lines that the Express services
// serve. It refuses a service with no known line.
func screenServiceLines(services []sim.ExpressService, known []screenLine) ([]screenLine, error) {
	served := make([]bool, len(known))
	for _, service := range services {
		index := slices.IndexFunc(known, func(line screenLine) bool {
			return line.from == service.From && line.to == service.To || line.from == service.To && line.to == service.From
		})
		if index < 0 {
			return nil, fmt.Errorf("service %s has no hub line", service.ID)
		}
		served[index] = true
	}
	var lines []screenLine
	for index, line := range known {
		if served[index] {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// screenLines returns the known lines of a screen. The first line is always
// one, and the others are there if a configuration serves them.
func screenLines(configs []screenConfig, known []screenLine) []screenLine {
	lines := slices.Clone(known[:1])
	for _, line := range known[1:] {
		if slices.ContainsFunc(configs, func(config screenConfig) bool { return slices.Contains(config.lines, line) }) {
			lines = append(lines, line)
		}
	}
	return lines
}

// screenWithoutExpress returns config with no Express pod, no Express
// service, and the plain order contract.
func screenWithoutExpress(config project.Config) project.Config {
	config.Fleet = slices.DeleteFunc(config.Fleet, func(pod sim.Placement) bool { return pod.Class == sim.ExpressClass })
	config.ExpressServices, config.OrderContract = nil, ""
	return config
}

// screenConfigs returns the named configurations. D0 is D, S0 is S, and W0
// is W, without Express.
func screenConfigs(names []string, known []screenLine) ([]screenConfig, error) {
	configs := make([]screenConfig, 0, len(names))
	for _, name := range names {
		var config project.Config
		switch name {
		case screenLondonFull:
			config = scenarios.LondonFull()
		case screenExpress, screenGeometry:
			config = scenarios.LondonFullExpress()
		case screenShared, screenSharedPlain, screenWharf, screenWharfPlain:
			generate := scenarios.GenerateLondonFullSharedExpress
			if name == screenWharf || name == screenWharfPlain {
				generate = scenarios.GenerateLondonFullSharedExpressWharf
			}
			var err error
			if config, err = generate(); err != nil {
				return nil, fmt.Errorf("screen configuration %s: %w", name, err)
			}
		}
		lines, err := screenServiceLines(config.ExpressServices, known)
		if err != nil {
			return nil, fmt.Errorf("screen configuration %s: %w", name, err)
		}
		if name == screenGeometry || name == screenSharedPlain || name == screenWharfPlain {
			config = screenWithoutExpress(config)
		}
		config.Demand.Enabled = false
		if err := project.Validate(config); err != nil {
			return nil, fmt.Errorf("screen configuration %s: %w", name, err)
		}
		configs = append(configs, screenConfig{name: name, config: config, lines: lines})
	}
	return configs, nil
}

// screenBand returns the index of a band in the LondonFull demand profile.
// busy and quiet select the bands with the most and the fewest observed
// journeys a minute.
func screenBand(name string) (int, error) {
	bands := scenarios.LondonFullDemand()
	if len(bands) == 0 {
		return 0, errors.New("LondonFull has no demand band")
	}
	if name == "busy" || name == "quiet" {
		chosen := 0
		for index, band := range bands {
			rate, chosenRate := band.ObservedJourneys/float64(band.DurationMinutes), bands[chosen].ObservedJourneys/float64(bands[chosen].DurationMinutes)
			if name == "busy" && rate > chosenRate || name == "quiet" && rate < chosenRate {
				chosen = index
			}
		}
		return chosen, nil
	}
	profile := scenarios.LondonFull().DemandProfiles[0]
	index := slices.IndexFunc(profile.Bands, func(band project.DemandBand) bool { return band.ID == name })
	if index < 0 {
		return 0, fmt.Errorf("unknown band %q", name)
	}
	return index, nil
}

// screenEvent is one offered event. A background event has one passenger. A
// hub event is a shared party between the two hubs of a line.
type screenEvent struct {
	ID       int    `json:"id"`
	Tick     int64  `json:"tick"`
	From     string `json:"from"`
	To       string `json:"to"`
	Size     int    `json:"size"`
	Hub      bool   `json:"hub"`
	Outbound bool   `json:"outbound,omitzero"`
	// Line is the index of the hub line of a hub event.
	Line uint8 `json:"line,omitzero"`
}

// screenEventsInput selects the offered events of one seed and band.
type screenEventsInput struct {
	seed          int64
	band          int
	arrivalTicks  int64
	intervalTicks int64
	lines         []screenLine
	source        project.Config
}

// screenEvents returns the background requests of the band and the hub
// events, ordered by tick and then by stream, background first. Each hub
// line has its own random source, so it does not change the background or
// another line.
func screenEvents(input screenEventsInput) ([]screenEvent, error) {
	base, err := newScenario(input.source, "")
	if err != nil {
		return nil, err
	}
	profile := input.source.DemandProfiles[0]
	background := demandSchedule(scheduleInput{
		seed: input.seed, durationTicks: input.arrivalTicks, intervalTicks: input.intervalTicks, pattern: "profile",
		passengers: base.passengers, focus: base.focus, profileFlows: weightedProfileFlows(profile, input.band),
	})
	limit := screenHubLimit(input.arrivalTicks)
	events := make([]screenEvent, 0, int64(len(background))+int64(len(input.lines))*limit)
	for _, request := range background {
		events = append(events, screenEvent{Tick: request.tick, From: request.origin, To: request.destination, Size: 1})
	}
	for index, line := range input.lines {
		events, err = screenHubEvents(events, screenHubInput{seed: input.seed, arrivalTicks: input.arrivalTicks, limit: limit, line: line, index: index})
		if err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(events, func(a, b screenEvent) int { return cmp.Compare(a.Tick, b.Tick) })
	for index := range events {
		events[index].ID = index
	}
	return events, nil
}

type screenHubInput struct {
	seed, arrivalTicks, limit int64
	line                      screenLine
	index                     int
}

// screenHubEvents appends the hub events of a line in the arrival window
// to events. It refuses a stream with more than limit events.
func screenHubEvents(events []screenEvent, input screenHubInput) ([]screenEvent, error) {
	seed, arrivalTicks, limit, mean := input.seed, input.arrivalTicks, input.limit, float64(input.line.meanTicks)
	rng := rand.New(rand.NewPCG(uint64(seed), input.line.stream)) // #nosec G115 G404 -- Seeds are positive, and the stream is a reproducible schedule.
	count := int64(0)
	for tick := int64(rng.ExpFloat64() * mean); tick < arrivalTicks; tick += 1 + int64(rng.ExpFloat64()*mean) {
		if count == limit {
			return nil, fmt.Errorf("seed %d: the hub stream has more than %d events", seed, limit)
		}
		count++
		outbound := rng.Float64() < input.line.outbound
		from, to := input.line.to, input.line.from
		if outbound {
			from, to = input.line.from, input.line.to
		}
		events = append(events, screenEvent{Tick: tick, From: from, To: to, Size: 1 + rng.IntN(sim.MaxExpressParties), Hub: true, Outbound: outbound, Line: uint8(input.index)}) // #nosec G115 -- A screen has at most screenMaxLines lines.
	}
	return events, nil
}

// screenSplitSize returns the largest new party of a pod of the fleet that
// is not Express. A larger order has no pod that can take it.
func screenSplitSize(fleet []sim.Placement) (int, error) {
	size := 0
	for _, pod := range fleet {
		if pod.Class == sim.ExpressClass {
			continue
		}
		spec, ok := sim.LookupVehicleClass(pod.Class)
		if !ok {
			return 0, fmt.Errorf("pod %s: %w", pod.ID, sim.ErrUnknownVehicleClass)
		}
		size = max(size, spec.MaxNewPartySize)
	}
	if size < 1 {
		return 0, errors.New("the fleet has no pod for a party")
	}
	return size, nil
}

// screenParts returns the party sizes of the orders of an event. A hub
// party with no Express service in the arm, and of more than split
// passengers, splits before admission into orders of at most split
// passengers.
func screenParts(event screenEvent, express bool, split int) []int {
	if express && event.Hub || event.Size <= split {
		return []int{event.Size}
	}
	parts := make([]int, 0, (event.Size+split-1)/split)
	for left := event.Size; left > 0; left -= split {
		parts = append(parts, min(left, split))
	}
	return parts
}

// screenArmInput is one run of the screen.
type screenArmInput struct {
	config       screenConfig
	band         int
	bandID       string
	seed         int64
	events       []screenEvent
	lines        []screenLine
	arrivalTicks int64
	clearTicks   int64
	queueLimit   int
	checkTicks   int64
}

// screenOutcome is the outcome of one event in one arm. Refused means that
// the queue limit refused the whole event. Partial means that the
// simulation refused a part after it accepted another part. WaitTicks and
// JourneyTicks are -1 for an event with a part that did not board or did not
// finish.
type screenOutcome struct {
	Refused bool `json:"refused,omitzero"`
	// Error is the error of the simulation for a refused order, or "" for
	// a refusal of the queue limit.
	Error        string `json:"error,omitempty"`
	Partial      bool   `json:"partial,omitzero"`
	Orders       []int  `json:"orders,omitempty"`
	WaitTicks    int64  `json:"wait_ticks"`
	JourneyTicks int64  `json:"journey_ticks"`
}

// screenArm is the result of one run.
type screenArm struct {
	Config      string  `json:"config"`
	Band        string  `json:"band"`
	Seed        int64   `json:"seed"`
	Ticks       int64   `json:"ticks"`
	WallSeconds float64 `json:"wall_seconds"`
	Checks      int     `json:"checks"`
	Failure     string  `json:"failure,omitempty"`
	Submitted   int     `json:"submitted"`
	Completed   int     `json:"completed"`
	Interrupted int     `json:"interrupted"`
	Outstanding int     `json:"outstanding"`
	Refused     int     `json:"refused"`
	Partial     int     `json:"partial"`
	// Refusal is the first error of the simulation for a refused order.
	Refusal  string          `json:"refusal,omitempty"`
	Measures screenMeasures  `json:"measures"`
	Outcomes []screenOutcome `json:"-"`
}

// runScreenArm runs one arm. A safety, contract, or accounting failure
// stops the arm and sets Failure. Other errors stop the screen.
func runScreenArm(input screenArmInput) (screenArm, error) {
	started := time.Now()
	config := input.config.config
	simulation, err := sim.NewFleetWithOrderContract(config.Network, config.Fleet, config.OrderContract)
	if err != nil {
		return screenArm{}, fmt.Errorf("%s: %w", input.config.name, err)
	}
	if err = screenConfigure(simulation, input); err != nil {
		return screenArm{}, fmt.Errorf("%s: %w", input.config.name, err)
	}
	services := make(map[string]string, len(config.ExpressServices))
	for _, service := range config.ExpressServices {
		services[service.From+" "+service.To] = service.ID
	}
	consent, err := comparisonConsent(config.GeneratedOrderConsent)
	if err != nil {
		return screenArm{}, err
	}
	split, err := screenSplitSize(config.Fleet)
	if err != nil {
		return screenArm{}, fmt.Errorf("%s: %w", input.config.name, err)
	}
	arm := screenArm{Config: input.config.name, Band: input.bandID, Seed: input.seed, Outcomes: make([]screenOutcome, len(input.events))}
	classes := make(map[string]sim.VehicleClass, len(config.Fleet))
	for _, pod := range config.Fleet {
		classes[pod.ID] = pod.Class
	}
	next := 0
	for tick := range input.arrivalTicks + input.clearTicks {
		for ; next < len(input.events) && input.events[next].Tick == tick; next++ {
			arm.Outcomes[next] = screenOffer(simulation, input.events[next], screenOfferInput{split: split, services: services, consent: consent, queueLimit: input.queueLimit})
		}
		simulation.Step()
		arm.Interrupted += len(simulation.DrainInterruptions())
		if simulation.Tick()%input.checkTicks != 0 {
			continue
		}
		arm.Checks++
		if failure := screenCheck(simulation); failure != "" {
			arm.Failure = failure
			break
		}
		arm.Measures.sample(simulation.MetricsSnapshot(), screenSample{arrivalTicks: input.arrivalTicks, ticks: input.checkTicks, classes: classes})
		if tick >= input.arrivalTicks && next == len(input.events) && screenOutstanding(simulation.MetricsSnapshot()) == 0 {
			break
		}
	}
	state := simulation.MetricsSnapshot()
	arm.Ticks, arm.Submitted, arm.Completed, arm.Outstanding = state.Tick, state.Submitted, state.Completed, screenOutstanding(state)
	screenTimes(arm.Outcomes, input.events, simulation.RequestTimings())
	arm.Measures.finish(arm.Outcomes, input.events, screenFinish{arrivalTicks: input.arrivalTicks, lines: input.lines})
	for _, outcome := range arm.Outcomes {
		if outcome.Refused {
			arm.Refused++
		}
		if outcome.Partial {
			arm.Partial++
		}
		if outcome.Error != "" && arm.Refusal == "" {
			arm.Refusal = outcome.Error
		}
	}
	arm.WallSeconds = time.Since(started).Seconds()
	return arm, nil
}

// screenConfigure applies the policies of the project, as a session does,
// with the demand weights of the band and the incidents off.
func screenConfigure(simulation *sim.Simulation, input screenArmInput) error {
	config := input.config.config
	if err := project.ConfigureSharedRides(simulation, config); err != nil {
		return err
	}
	if err := project.ConfigurePlatoons(simulation, config); err != nil {
		return err
	}
	if err := project.ConfigureExperiments(simulation, config, false); err != nil {
		return err
	}
	base, err := newScenario(config, "")
	if err != nil {
		return err
	}
	weights, err := demandWeights("profile", input.bandID, base)
	if err != nil {
		return err
	}
	if err := simulation.SetDemandWeights(weights); err != nil {
		return err
	}
	simulation.SetExperimentRecords(true)
	mode := sim.PositioningOff
	if config.Redistribution {
		mode = sim.PositioningGuarded
	}
	return simulation.SetPositioning(mode)
}

type screenOfferInput struct {
	split int
	// services gives the Express service of "from to".
	services   map[string]string
	consent    sim.SharingConsent
	queueLimit int
}

// screenOffer submits the orders of an event. The queue limit admits the
// whole event or refuses it before the first order.
func screenOffer(simulation *sim.Simulation, event screenEvent, input screenOfferInput) screenOutcome {
	service := ""
	if event.Hub {
		service = input.services[event.From+" "+event.To]
	}
	parts := screenParts(event, service != "", input.split)
	outcome := screenOutcome{WaitTicks: -1, JourneyTicks: -1}
	if simulation.PendingCount()+len(parts) > input.queueLimit {
		outcome.Refused = true
		return outcome
	}
	for _, size := range parts {
		options := sim.TripOptions{From: event.From, To: event.To, PartySize: size, SharingConsent: input.consent}
		if event.Hub {
			options.SharingConsent = sim.SharedConsent
			if service != "" {
				options.Service, options.ServiceID = sim.ExpressServiceChoice, service
			}
		}
		id, err := simulation.SubmitTripOptions(options)
		if err != nil {
			outcome.Refused = len(outcome.Orders) == 0
			outcome.Partial = len(outcome.Orders) > 0
			outcome.Error = err.Error()
			return outcome
		}
		outcome.Orders = append(outcome.Orders, id)
	}
	return outcome
}

// screenCheck returns the first failure of the safety oracle, the state
// contract, or the order accounting, or "".
func screenCheck(simulation *sim.Simulation) string {
	if _, err := simulation.SafetyObservation().Check(); err != nil {
		return "safety: " + err.Error()
	}
	if err := simulation.CheckContract(); err != nil {
		return "contract: " + err.Error()
	}
	if state := simulation.MetricsSnapshot(); screenOutstanding(state) < 0 {
		return fmt.Sprintf("accounting: %d submitted, %d completed, %d interrupted", state.Submitted, state.Completed, state.Interrupted)
	}
	return ""
}

func screenOutstanding(state sim.Snapshot) int {
	return state.Submitted - state.Completed - state.Interrupted
}

// screenTimes sets the wait and the journey of each event whose orders all
// boarded or all finished. The wait of an event ends when its last order
// boards, and its journey when its last order finishes.
func screenTimes(outcomes []screenOutcome, events []screenEvent, timings []sim.RequestTiming) {
	byID := make(map[int]sim.RequestTiming, len(timings))
	for _, timing := range timings {
		byID[timing.RequestID] = timing
	}
	for index := range outcomes {
		outcome := &outcomes[index]
		if outcome.Refused || outcome.Partial || len(outcome.Orders) == 0 {
			continue
		}
		boarded, finished := int64(0), int64(0)
		for _, id := range outcome.Orders {
			timing, ok := byID[id]
			if !ok {
				boarded, finished = -1, -1
				break
			}
			boarded = max(boarded, timing.BoardedTick)
			if timing.CompletedTick < 0 || finished < 0 {
				finished = -1
			} else {
				finished = max(finished, timing.CompletedTick)
			}
		}
		if boarded >= 0 {
			outcome.WaitTicks = boarded - events[index].Tick
		}
		if finished >= 0 {
			outcome.JourneyTicks = finished - events[index].Tick
		}
	}
}

// runScreenArms runs the arms with at most workers at a time, and returns
// the results in the order of inputs.
func runScreenArms(inputs []screenArmInput, workers int) ([]screenArm, error) {
	results, errs := runIndexed(inputs, workers, runScreenArm)
	return results, errors.Join(errs...)
}

// screenSecondsPerHour gives the wall seconds of an arm for each simulated
// hour.
func screenSecondsPerHour(arm screenArm) float64 {
	if arm.Ticks == 0 {
		return math.NaN()
	}
	return arm.WallSeconds / (float64(arm.Ticks) / (3600 * sim.TicksPerSecond))
}
