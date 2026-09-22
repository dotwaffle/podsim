// Command compare runs identical demand schedules with redistribution off and on.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	maxDuration    = 24 * time.Hour
	maxSeeds       = 100
	maxLoads       = 20
	maxComparisons = 1000
	maxQueueLimit  = 1_000_000
	maxBurstSize   = 1_000
)

var knownPatterns = []string{"balanced", "destination", "hotspot", "bursty-hotspot", "hub-burst"}

type options struct {
	duration, arrivalsFor time.Duration
	requestEvery          time.Duration
	seed                  int64
	seedsText             string
	pattern               string
	patternsText          string
	loadsText             string
	sharingLimitsText     string
	focus                 string
	format                string
	projectPath           string
	outputPath            string
	queueLimit            int
	burstSize             int
	seeds                 []int64
	patterns              []string
	loads                 []time.Duration
	sharingLimits         []int
}

type scheduledRequest struct {
	tick        int64
	origin      string
	destination string
}

type scenario struct {
	network    sim.Network
	fleet      []sim.Placement
	passengers []string
	focus      string
}

type result struct {
	Pattern                      string  `json:"pattern"`
	RequestEverySeconds          float64 `json:"request_every_seconds"`
	BurstSize                    int     `json:"burst_size"`
	Seed                         int64   `json:"seed"`
	Policy                       string  `json:"policy"`
	SharedRidePartyLimit         int     `json:"shared_ride_party_limit"`
	SharedParties                int     `json:"shared_parties"`
	FocusStation                 string  `json:"focus_station"`
	WindowStartSeconds           float64 `json:"window_start_seconds"`
	WindowEndSeconds             float64 `json:"window_end_seconds"`
	ArrivalEndSeconds            float64 `json:"arrival_end_seconds"`
	ScheduleID                   string  `json:"schedule_id"`
	Scheduled                    int     `json:"scheduled"`
	Served                       int     `json:"served"`
	Remaining                    int     `json:"remaining"`
	Skipped                      int     `json:"skipped"`
	PeakPending                  int     `json:"peak_pending"`
	PeakFocusApproaching         int     `json:"peak_focus_approaching"`
	PeakFocusEntranceStopped     int     `json:"peak_focus_entrance_stopped"`
	PeakFocusExitStopped         int     `json:"peak_focus_exit_stopped"`
	PeakFocusOccupiedBerths      int     `json:"peak_focus_occupied_berths"`
	PeakFocusReservedEmptyBerths int     `json:"peak_focus_reserved_empty_berths"`
	QueueCleared                 bool    `json:"queue_cleared"`
	QueueClearSeconds            float64 `json:"queue_clear_seconds"`
	WaitAverageSeconds           float64 `json:"wait_average_seconds"`
	WaitMaximumSeconds           float64 `json:"wait_maximum_seconds"`
	PassengerDistanceMeters      float64 `json:"passenger_distance_meters"`
	EmptyDistanceMeters          float64 `json:"empty_distance_meters"`
	LoadedDistancePercent        float64 `json:"loaded_distance_percent"`
	PositioningMoveCount         int     `json:"positioning_moves"`
}

type report struct {
	SchemaVersion int      `json:"schema_version"`
	Results       []result `json:"results"`
}

type cliInput struct {
	args           []string
	stdout, stderr io.Writer
}

func main() {
	os.Exit(runCLI(cliInput{args: os.Args[1:], stdout: os.Stdout, stderr: os.Stderr}))
}

func runCLI(input cliInput) int {
	opts, err := parseOptions(input.args, input.stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(input.stderr, err)
		return 2
	}
	caseStudy, err := loadScenario(opts.projectPath, opts.focus)
	if err != nil {
		_, _ = fmt.Fprintln(input.stderr, err)
		return 1
	}
	results, err := compare(opts, caseStudy)
	if err != nil {
		_, _ = fmt.Fprintln(input.stderr, err)
		return 1
	}
	output := input.stdout
	closeOutput := func() error { return nil }
	if opts.outputPath != "" {
		file, err := os.Create(opts.outputPath) // #nosec G304 -- The operator selects this report file.
		if err != nil {
			_, _ = fmt.Fprintf(input.stderr, "create report %s: %v\n", opts.outputPath, err)
			return 1
		}
		output = file
		closeOutput = file.Close
	}
	if err := writeReport(writeReportInput{output: output, format: opts.format, results: results}); err != nil {
		_ = closeOutput()
		_, _ = fmt.Fprintln(input.stderr, err)
		return 1
	}
	if err := closeOutput(); err != nil {
		_, _ = fmt.Fprintf(input.stderr, "close report %s: %v\n", opts.outputPath, err)
		return 1
	}
	return 0
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{}
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.DurationVar(&opts.duration, "duration", 30*time.Minute, "simulated comparison duration")
	flags.DurationVar(&opts.arrivalsFor, "arrivals-for", 0, "simulated arrival window; default is the full duration")
	flags.DurationVar(&opts.requestEvery, "request-every", 45*time.Second, "simulated time between requests")
	flags.Int64Var(&opts.seed, "seed", 1, "demand schedule seed")
	flags.StringVar(&opts.seedsText, "seeds", "", "comma-separated demand schedule seeds")
	flags.StringVar(&opts.pattern, "pattern", "hotspot", "demand pattern")
	flags.StringVar(&opts.patternsText, "patterns", "", "comma-separated demand patterns or all")
	flags.StringVar(&opts.loadsText, "loads", "", "comma-separated request intervals")
	flags.StringVar(&opts.sharingLimitsText, "sharing-limits", "1", "comma-separated same-destination party limits")
	flags.StringVar(&opts.focus, "focus", "", "passenger station used by focused patterns")
	flags.StringVar(&opts.format, "format", "table", "output format: table, json, or csv")
	flags.StringVar(&opts.projectPath, "project", "", "raw project configuration path")
	flags.StringVar(&opts.outputPath, "output", "", "write the report to this path")
	flags.IntVar(&opts.queueLimit, "queue-limit", 200, "maximum pending requests before arrivals are skipped")
	flags.IntVar(&opts.burstSize, "burst-size", 3, "requests in each burst for burst patterns")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("unexpected positional arguments")
	}
	if opts.duration <= 0 || opts.duration > maxDuration {
		return options{}, fmt.Errorf("duration must be between one simulation tick and %s", maxDuration)
	}
	if durationTicks(opts.duration) < 1 {
		return options{}, errors.New("duration must be at least one simulation tick")
	}
	if opts.arrivalsFor == 0 {
		opts.arrivalsFor = opts.duration
	}
	if opts.arrivalsFor <= 0 || opts.arrivalsFor > opts.duration || durationTicks(opts.arrivalsFor) < 1 {
		return options{}, errors.New("arrivals-for must be at least one simulation tick and no longer than duration")
	}
	if opts.queueLimit < 1 || opts.queueLimit > maxQueueLimit {
		return options{}, fmt.Errorf("queue-limit must be between 1 and %d", maxQueueLimit)
	}
	if opts.burstSize < 1 || opts.burstSize > maxBurstSize {
		return options{}, fmt.Errorf("burst-size must be between 1 and %d", maxBurstSize)
	}
	if opts.format != "table" && opts.format != "json" && opts.format != "csv" {
		return options{}, errors.New("format must be table, json, or csv")
	}

	var err error
	opts.seeds, err = parseSeeds(opts.seed, opts.seedsText)
	if err != nil {
		return options{}, err
	}
	opts.patterns, err = parsePatterns(opts.pattern, opts.patternsText)
	if err != nil {
		return options{}, err
	}
	opts.loads, err = parseLoads(parseLoadsInput{single: opts.requestEvery, list: opts.loadsText, duration: opts.arrivalsFor})
	if err != nil {
		return options{}, err
	}
	opts.sharingLimits, err = parseSharingLimits(opts.sharingLimitsText)
	if err != nil {
		return options{}, err
	}
	if len(opts.seeds)*len(opts.patterns)*len(opts.loads)*len(opts.sharingLimits) > maxComparisons {
		return options{}, fmt.Errorf("the matrix must contain at most %d comparisons", maxComparisons)
	}
	return opts, nil
}

func parseSharingLimits(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	limits := make([]int, 0, len(parts))
	seen := make(map[int]bool, len(parts))
	for _, part := range parts {
		limit, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || limit < 1 || limit > sim.MaxSharedRideParties {
			return nil, fmt.Errorf("sharing limits must be integers from 1 to %d", sim.MaxSharedRideParties)
		}
		if seen[limit] {
			return nil, fmt.Errorf("sharing limit %d appears more than once", limit)
		}
		seen[limit] = true
		limits = append(limits, limit)
	}
	return limits, nil
}

func parseSeeds(single int64, list string) ([]int64, error) {
	if list == "" {
		return []int64{single}, nil
	}
	parts := strings.Split(list, ",")
	if len(parts) > maxSeeds {
		return nil, fmt.Errorf("seeds must contain at most %d values", maxSeeds)
	}
	seeds := make([]int64, 0, len(parts))
	seen := make(map[int64]bool, len(parts))
	for _, part := range parts {
		seed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse seed %q: %w", part, err)
		}
		if seen[seed] {
			return nil, fmt.Errorf("seed %d appears more than once", seed)
		}
		seen[seed] = true
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		return nil, errors.New("seeds must contain at least one value")
	}
	return seeds, nil
}

func parsePatterns(single, list string) ([]string, error) {
	if list == "" {
		list = single
	}
	if strings.TrimSpace(list) == "all" {
		return append([]string(nil), knownPatterns...), nil
	}
	parts := strings.Split(list, ",")
	patterns := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		pattern := strings.TrimSpace(part)
		if !isKnownPattern(pattern) {
			return nil, fmt.Errorf("unknown demand pattern %q", pattern)
		}
		if seen[pattern] {
			return nil, fmt.Errorf("demand pattern %q appears more than once", pattern)
		}
		seen[pattern] = true
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

type parseLoadsInput struct {
	single, duration time.Duration
	list             string
}

func parseLoads(input parseLoadsInput) ([]time.Duration, error) {
	if input.list == "" {
		return validateLoads([]time.Duration{input.single}, input.duration)
	}
	parts := strings.Split(input.list, ",")
	if len(parts) > maxLoads {
		return nil, fmt.Errorf("loads must contain at most %d values", maxLoads)
	}
	loads := make([]time.Duration, 0, len(parts))
	seen := make(map[time.Duration]bool, len(parts))
	for _, part := range parts {
		load, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parse load %q: %w", part, err)
		}
		if seen[load] {
			return nil, fmt.Errorf("load %s appears more than once", load)
		}
		seen[load] = true
		loads = append(loads, load)
	}
	return validateLoads(loads, input.duration)
}

func validateLoads(loads []time.Duration, duration time.Duration) ([]time.Duration, error) {
	for _, load := range loads {
		if load <= 0 || load >= duration {
			return nil, errors.New("each load must be positive and shorter than the arrival window")
		}
		if durationTicks(load) < 1 {
			return nil, errors.New("each load must be at least one simulation tick")
		}
	}
	return loads, nil
}

func isKnownPattern(pattern string) bool {
	return slices.Contains(knownPatterns, pattern)
}

func loadScenario(path, focus string) (scenario, error) {
	config := project.Config{
		Network: sim.Example(),
		Fleet: []sim.Placement{
			{ID: "01", StationID: "parking", BerthID: "parking-1"},
			{ID: "02", StationID: "garden", BerthID: "garden-1"},
		},
	}
	if path != "" {
		var err error
		config, err = readProject(path)
		if err != nil {
			return scenario{}, err
		}
	}
	stations := project.PassengerStations(config.Network)
	passengers := make([]string, len(stations))
	for i, station := range stations {
		passengers[i] = station.ID
	}
	if focus == "" {
		focus = config.Demand.Destination
	}
	if focus == "" {
		for _, station := range passengers {
			if station == "market" {
				focus = station
				break
			}
		}
	}
	if focus == "" && len(passengers) > 0 {
		focus = passengers[len(passengers)-1]
	}
	found := false
	for _, station := range passengers {
		found = found || station == focus
	}
	if !found {
		return scenario{}, fmt.Errorf("focus %q must name a passenger station", focus)
	}
	return scenario{network: config.Network, fleet: config.Fleet, passengers: passengers, focus: focus}, nil
}

func readProject(path string) (project.Config, error) {
	file, err := os.Open(path) // #nosec G304 -- The operator selects this local file.
	if err != nil {
		return project.Config{}, fmt.Errorf("open project %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return project.Config{}, fmt.Errorf("stat project %s: %w", path, err)
	}
	if info.Size() > project.MaxFileBytes {
		return project.Config{}, fmt.Errorf("read project %s: file exceeds 4 MiB", path)
	}
	decoder := json.NewDecoder(io.LimitReader(file, project.MaxFileBytes))
	decoder.DisallowUnknownFields()
	var config project.Config
	if err := decoder.Decode(&config); err != nil {
		return project.Config{}, fmt.Errorf("read project %s: %w", path, err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return project.Config{}, fmt.Errorf("read project %s: expected one JSON value", path)
	}
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate project %s: %w", path, err)
	}
	return config, nil
}

func compare(opts options, scenario scenario) ([]result, error) {
	results := make([]result, 0, len(opts.patterns)*len(opts.loads)*len(opts.seeds)*len(opts.sharingLimits)*2)
	for _, pattern := range opts.patterns {
		for _, load := range opts.loads {
			for _, seed := range opts.seeds {
				schedule := demandSchedule(scheduleInput{
					seed: seed, durationTicks: durationTicks(opts.arrivalsFor), intervalTicks: durationTicks(load), pattern: pattern,
					burstSize:  opts.burstSize,
					passengers: scenario.passengers, focus: scenario.focus,
				})
				id := scheduleID(schedule)
				for _, sharingLimit := range opts.sharingLimits {
					for _, enabled := range []bool{false, true} {
						outcome, err := run(runInput{
							enabled: enabled, duration: opts.duration, requestEvery: load, seed: seed,
							pattern: pattern, scheduleID: id, queueLimit: opts.queueLimit,
							burstSize: opts.burstSize, sharingLimit: sharingLimit,
							schedule: schedule, scenario: scenario,
						})
						if err != nil {
							return nil, err
						}
						results = append(results, outcome)
					}
				}
			}
		}
	}
	return results, nil
}

type scheduleInput struct {
	seed                         int64
	durationTicks, intervalTicks int64
	burstSize                    int
	pattern, focus               string
	passengers                   []string
}

func demandSchedule(input scheduleInput) []scheduledRequest {
	rng := rand.New(rand.NewSource(input.seed))
	count := int((input.durationTicks - 1) / input.intervalTicks)
	requests := make([]scheduledRequest, 0, count)
	for i := range count {
		tick := int64(i+1) * input.intervalTicks
		if isBurstPattern(input.pattern) {
			tick = input.intervalTicks + int64(i/input.burstSize)*int64(input.burstSize)*input.intervalTicks
		}
		origin, destination := demandPair(rng, input)
		requests = append(requests, scheduledRequest{tick: tick, origin: origin, destination: destination})
	}
	return requests
}

func demandPair(rng *rand.Rand, input scheduleInput) (string, string) {
	switch input.pattern {
	case "destination":
		origins := without(input.passengers, input.focus)
		return origins[rng.Intn(len(origins))], input.focus
	case "hotspot", "bursty-hotspot":
		origins := make([]string, 0, 2*len(input.passengers)+4)
		for range 6 {
			origins = append(origins, input.focus)
		}
		for _, station := range input.passengers {
			if station != input.focus {
				origins = append(origins, station, station)
			}
		}
		origin := origins[rng.Intn(len(origins))]
		destinations := without(input.passengers, origin)
		return origin, destinations[rng.Intn(len(destinations))]
	case "hub-burst":
		destinations := without(input.passengers, input.focus)
		return input.focus, destinations[rng.Intn(len(destinations))]
	default:
		origin := input.passengers[rng.Intn(len(input.passengers))]
		destinations := without(input.passengers, origin)
		return origin, destinations[rng.Intn(len(destinations))]
	}
}

func isBurstPattern(pattern string) bool {
	return pattern == "bursty-hotspot" || pattern == "hub-burst"
}

func without(stations []string, excluded string) []string {
	result := make([]string, 0, len(stations)-1)
	for _, station := range stations {
		if station != excluded {
			result = append(result, station)
		}
	}
	return result
}

func scheduleID(schedule []scheduledRequest) string {
	hash := sha256.New()
	for _, request := range schedule {
		_, _ = fmt.Fprintf(hash, "%d:%s>%s\n", request.tick, request.origin, request.destination)
	}
	return hex.EncodeToString(hash.Sum(nil)[:8])
}

type runInput struct {
	enabled                bool
	duration, requestEvery time.Duration
	seed                   int64
	pattern, scheduleID    string
	queueLimit             int
	burstSize              int
	sharingLimit           int
	schedule               []scheduledRequest
	scenario               scenario
}

func run(input runInput) (result, error) {
	simulation, err := sim.NewFleet(input.scenario.network, input.scenario.fleet)
	if err != nil {
		return result{}, fmt.Errorf("create comparison: %w", err)
	}
	if demandErr := simulation.SetDemandWeights(demandWeights(input.pattern, input.scenario)); demandErr != nil {
		return result{}, fmt.Errorf("set demand weights: %w", demandErr)
	}
	if sharingErr := simulation.SetSharedRidePartyLimit(input.sharingLimit); sharingErr != nil {
		return result{}, fmt.Errorf("set sharing limit: %w", sharingErr)
	}
	simulation.SetRedistribution(input.enabled)
	metrics, err := newRunMetrics(input.scenario, input.schedule)
	if err != nil {
		return result{}, err
	}
	next, skipped := 0, 0
	for tick := range durationTicks(input.duration) {
		injected := false
		for next < len(input.schedule) && input.schedule[next].tick == tick {
			request := input.schedule[next]
			if len(simulation.Snapshot().Pending) >= input.queueLimit {
				skipped++
				next++
				continue
			}
			if err := simulation.RequestTrip(request.origin, request.destination); err != nil {
				return result{}, fmt.Errorf("request %s to %s: %w", request.origin, request.destination, err)
			}
			next++
			injected = true
		}
		if injected {
			metrics.observe(simulation.Snapshot())
		}
		simulation.Step()
		if (tick+1)%sim.TicksPerSecond == 0 {
			metrics.observe(simulation.Snapshot())
		}
	}
	state := simulation.Snapshot()
	metrics.observe(state)
	policy := "off"
	if input.enabled {
		policy = "on"
	}
	burstSize := 1
	if isBurstPattern(input.pattern) {
		burstSize = input.burstSize
	}
	arrivalEnd := 0.0
	if len(input.schedule) > 0 {
		arrivalEnd = float64(input.schedule[len(input.schedule)-1].tick) / sim.TicksPerSecond
	}
	return result{
		Pattern: input.pattern, RequestEverySeconds: input.requestEvery.Seconds(), Seed: input.seed,
		BurstSize: burstSize, Policy: policy, SharedRidePartyLimit: input.sharingLimit,
		SharedParties: state.SharedParties, FocusStation: input.scenario.focus,
		WindowStartSeconds: 0, WindowEndSeconds: input.duration.Seconds(), ArrivalEndSeconds: arrivalEnd, ScheduleID: input.scheduleID,
		Scheduled: len(input.schedule), Served: state.Completed, Remaining: state.Submitted - state.Completed, Skipped: skipped,
		PeakPending: metrics.peakPending, PeakFocusApproaching: metrics.peakApproaching,
		PeakFocusEntranceStopped: metrics.peakEntranceStopped, PeakFocusExitStopped: metrics.peakExitStopped,
		PeakFocusOccupiedBerths: metrics.peakOccupiedBerths, PeakFocusReservedEmptyBerths: metrics.peakReservedEmptyBerths,
		QueueCleared: metrics.queueCleared, QueueClearSeconds: metrics.queueClearSeconds,
		WaitAverageSeconds: state.Wait.AverageSeconds, WaitMaximumSeconds: state.Wait.MaxSeconds,
		PassengerDistanceMeters: state.PassengerDistanceMeters, EmptyDistanceMeters: state.EmptyDistanceMeters,
		LoadedDistancePercent: loadedDistancePercent(state.PassengerDistanceMeters, state.EmptyDistanceMeters),
		PositioningMoveCount:  state.RebalanceMoves,
	}, nil
}

func loadedDistancePercent(passenger, empty float64) float64 {
	total := passenger + empty
	if total == 0 {
		return 0
	}
	return 100 * passenger / total
}

func demandWeights(pattern string, scenario scenario) map[string]float64 {
	if pattern == "balanced" {
		return nil
	}
	weights := make(map[string]float64, len(scenario.passengers))
	for _, station := range scenario.passengers {
		switch pattern {
		case "destination":
			if station != scenario.focus {
				weights[station] = 1
			}
		default:
			weights[station] = 2
			if station == scenario.focus {
				weights[station] = 6
			}
		}
	}
	return weights
}

type writeReportInput struct {
	output  io.Writer
	format  string
	results []result
}

func writeReport(input writeReportInput) error {
	switch input.format {
	case "json":
		encoder := json.NewEncoder(input.output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report{SchemaVersion: 3, Results: input.results}); err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
		return nil
	case "csv":
		return writeCSV(input.output, input.results)
	default:
		return writeTable(input.output, input.results)
	}
}

func writeTable(output io.Writer, results []result) error {
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "window: %.2fs to %.2fs / arrivals end: %.2fs / focus: %s\n", results[0].WindowStartSeconds, results[0].WindowEndSeconds, results[0].ArrivalEndSeconds, results[0].FocusStation); err != nil {
		return fmt.Errorf("write table window: %w", err)
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "PATTERN\tLOAD (S)\tBURST\tSEED\tPOLICY\tSHARE LIMIT\tSHARED\tWAIT AVG\tWAIT MAX\tSERVED\tLEFT\tSKIPPED\tPEAK WAIT\tHUB IN\tHUB OUT\tHUB OCC\tHUB RSV\tCLEAR (S)\tPASSENGER (M)\tEMPTY (M)\tLOADED %\tMOVES"); err != nil {
		return fmt.Errorf("write table header: %w", err)
	}
	for _, outcome := range results {
		if _, err := fmt.Fprintf(w, "%s\t%.2f\t%d\t%d\t%s\t%d\t%d\t%.2f\t%.2f\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%.1f\t%.1f\t%.2f\t%d\n",
			outcome.Pattern, outcome.RequestEverySeconds, outcome.BurstSize, outcome.Seed, outcome.Policy, outcome.SharedRidePartyLimit, outcome.SharedParties,
			outcome.WaitAverageSeconds, outcome.WaitMaximumSeconds, outcome.Served, outcome.Remaining, outcome.Skipped,
			outcome.PeakPending, outcome.PeakFocusEntranceStopped, outcome.PeakFocusExitStopped,
			outcome.PeakFocusOccupiedBerths, outcome.PeakFocusReservedEmptyBerths, queueClearText(outcome),
			outcome.PassengerDistanceMeters, outcome.EmptyDistanceMeters, outcome.LoadedDistancePercent,
			outcome.PositioningMoveCount); err != nil {
			return fmt.Errorf("write table row: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table report: %w", err)
	}
	return nil
}

func writeCSV(output io.Writer, results []result) error {
	w := csv.NewWriter(output)
	header := []string{
		"pattern", "request_every_seconds", "burst_size", "seed", "policy", "shared_ride_party_limit", "shared_parties", "focus_station", "window_start_seconds", "window_end_seconds", "arrival_end_seconds", "schedule_id",
		"scheduled", "served", "remaining", "skipped", "peak_pending", "peak_focus_approaching", "peak_focus_entrance_stopped", "peak_focus_exit_stopped",
		"peak_focus_occupied_berths", "peak_focus_reserved_empty_berths", "queue_cleared", "queue_clear_seconds",
		"wait_average_seconds", "wait_maximum_seconds", "passenger_distance_meters", "empty_distance_meters", "loaded_distance_percent", "positioning_moves",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, outcome := range results {
		row := []string{
			outcome.Pattern, floatText(outcome.RequestEverySeconds), strconv.Itoa(outcome.BurstSize), strconv.FormatInt(outcome.Seed, 10), outcome.Policy,
			strconv.Itoa(outcome.SharedRidePartyLimit), strconv.Itoa(outcome.SharedParties), outcome.FocusStation,
			floatText(outcome.WindowStartSeconds), floatText(outcome.WindowEndSeconds), floatText(outcome.ArrivalEndSeconds), outcome.ScheduleID,
			strconv.Itoa(outcome.Scheduled), strconv.Itoa(outcome.Served), strconv.Itoa(outcome.Remaining), strconv.Itoa(outcome.Skipped),
			strconv.Itoa(outcome.PeakPending), strconv.Itoa(outcome.PeakFocusApproaching), strconv.Itoa(outcome.PeakFocusEntranceStopped), strconv.Itoa(outcome.PeakFocusExitStopped),
			strconv.Itoa(outcome.PeakFocusOccupiedBerths), strconv.Itoa(outcome.PeakFocusReservedEmptyBerths), strconv.FormatBool(outcome.QueueCleared), floatText(outcome.QueueClearSeconds),
			floatText(outcome.WaitAverageSeconds), floatText(outcome.WaitMaximumSeconds), floatText(outcome.PassengerDistanceMeters),
			floatText(outcome.EmptyDistanceMeters), floatText(outcome.LoadedDistancePercent),
			strconv.Itoa(outcome.PositioningMoveCount),
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush CSV report: %w", err)
	}
	return nil
}

func floatText(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func queueClearText(outcome result) string {
	if !outcome.QueueCleared {
		return "-"
	}
	return fmt.Sprintf("%.1f", outcome.QueueClearSeconds)
}

func durationTicks(duration time.Duration) int64 {
	return int64(duration * sim.TicksPerSecond / time.Second)
}

type runMetrics struct {
	monitor                 observe.StationMonitor
	station                 sim.Station
	lastArrivalTick         int64
	peakPending             int
	peakApproaching         int
	peakEntranceStopped     int
	peakExitStopped         int
	peakOccupiedBerths      int
	peakReservedEmptyBerths int
	queueCleared            bool
	queueClearSeconds       float64
}

func newRunMetrics(caseStudy scenario, schedule []scheduledRequest) (runMetrics, error) {
	station, ok := caseStudy.network.Station(caseStudy.focus)
	if !ok {
		return runMetrics{}, fmt.Errorf("observe focus station %q: station not found", caseStudy.focus)
	}
	metrics := runMetrics{
		monitor: observe.NewStationMonitor(caseStudy.network),
		station: station,
	}
	if len(schedule) > 0 {
		metrics.lastArrivalTick = schedule[len(schedule)-1].tick
	}
	return metrics, nil
}

func (metrics *runMetrics) observe(state sim.Snapshot) {
	station := metrics.monitor.Summarize(metrics.station, state)
	metrics.peakPending = max(metrics.peakPending, len(state.Pending))
	metrics.peakApproaching = max(metrics.peakApproaching, station.Approaching)
	metrics.peakEntranceStopped = max(metrics.peakEntranceStopped, station.EntranceStopped)
	metrics.peakExitStopped = max(metrics.peakExitStopped, station.ExitStopped)
	metrics.peakOccupiedBerths = max(metrics.peakOccupiedBerths, station.Occupied)
	metrics.peakReservedEmptyBerths = max(metrics.peakReservedEmptyBerths, station.ReservedEmpty)
	if !metrics.queueCleared && metrics.lastArrivalTick > 0 && state.Tick >= metrics.lastArrivalTick && len(state.Pending) == 0 {
		metrics.queueCleared = true
		metrics.queueClearSeconds = float64(state.Tick-metrics.lastArrivalTick) / sim.TicksPerSecond
	}
}
