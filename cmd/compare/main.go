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

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	maxDuration    = 24 * time.Hour
	maxSeeds       = 100
	maxLoads       = 20
	maxComparisons = 1000
	maxQueueLimit  = 1_000_000
	maxProjectSize = 2 << 20
)

var knownPatterns = []string{"balanced", "destination", "hotspot", "bursty-hotspot"}

type options struct {
	duration     time.Duration
	requestEvery time.Duration
	seed         int64
	seedsText    string
	pattern      string
	patternsText string
	loadsText    string
	focus        string
	format       string
	projectPath  string
	outputPath   string
	queueLimit   int
	seeds        []int64
	patterns     []string
	loads        []time.Duration
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
	Pattern              string  `json:"pattern"`
	RequestEverySeconds  float64 `json:"request_every_seconds"`
	Seed                 int64   `json:"seed"`
	Policy               string  `json:"policy"`
	WindowStartSeconds   float64 `json:"window_start_seconds"`
	WindowEndSeconds     float64 `json:"window_end_seconds"`
	ScheduleID           string  `json:"schedule_id"`
	Scheduled            int     `json:"scheduled"`
	Served               int     `json:"served"`
	Remaining            int     `json:"remaining"`
	Skipped              int     `json:"skipped"`
	WaitAverageSeconds   float64 `json:"wait_average_seconds"`
	WaitMaximumSeconds   float64 `json:"wait_maximum_seconds"`
	EmptyDistanceMeters  float64 `json:"empty_distance_meters"`
	PositioningMoveCount int     `json:"positioning_moves"`
}

type report struct {
	SchemaVersion int      `json:"schema_version"`
	Results       []result `json:"results"`
}

func main() { os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr)) }

func runCLI(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	caseStudy, err := loadScenario(opts.projectPath, opts.focus)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	results, err := compare(opts, caseStudy)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	output := stdout
	closeOutput := func() error { return nil }
	if opts.outputPath != "" {
		file, err := os.Create(opts.outputPath) // #nosec G304 -- The operator selects this report file.
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "create report %s: %v\n", opts.outputPath, err)
			return 1
		}
		output = file
		closeOutput = file.Close
	}
	if err := writeReport(output, opts.format, results); err != nil {
		_ = closeOutput()
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if err := closeOutput(); err != nil {
		_, _ = fmt.Fprintf(stderr, "close report %s: %v\n", opts.outputPath, err)
		return 1
	}
	return 0
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{}
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.DurationVar(&opts.duration, "duration", 30*time.Minute, "simulated comparison duration")
	flags.DurationVar(&opts.requestEvery, "request-every", 45*time.Second, "simulated time between requests")
	flags.Int64Var(&opts.seed, "seed", 1, "demand schedule seed")
	flags.StringVar(&opts.seedsText, "seeds", "", "comma-separated demand schedule seeds")
	flags.StringVar(&opts.pattern, "pattern", "hotspot", "demand pattern")
	flags.StringVar(&opts.patternsText, "patterns", "", "comma-separated demand patterns or all")
	flags.StringVar(&opts.loadsText, "loads", "", "comma-separated request intervals")
	flags.StringVar(&opts.focus, "focus", "", "passenger station used by focused patterns")
	flags.StringVar(&opts.format, "format", "table", "output format: table, json, or csv")
	flags.StringVar(&opts.projectPath, "project", "", "raw project configuration path")
	flags.StringVar(&opts.outputPath, "output", "", "write the report to this path")
	flags.IntVar(&opts.queueLimit, "queue-limit", 200, "maximum pending requests before arrivals are skipped")
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
	if opts.queueLimit < 1 || opts.queueLimit > maxQueueLimit {
		return options{}, fmt.Errorf("queue-limit must be between 1 and %d", maxQueueLimit)
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
	opts.loads, err = parseLoads(opts.requestEvery, opts.loadsText, opts.duration)
	if err != nil {
		return options{}, err
	}
	if len(opts.seeds)*len(opts.patterns)*len(opts.loads) > maxComparisons {
		return options{}, fmt.Errorf("the matrix must contain at most %d comparisons", maxComparisons)
	}
	return opts, nil
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

func parseLoads(single time.Duration, list string, duration time.Duration) ([]time.Duration, error) {
	if list == "" {
		return validateLoads([]time.Duration{single}, duration)
	}
	parts := strings.Split(list, ",")
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
	return validateLoads(loads, duration)
}

func validateLoads(loads []time.Duration, duration time.Duration) ([]time.Duration, error) {
	for _, load := range loads {
		if load <= 0 || load > duration {
			return nil, errors.New("each load must be positive and no longer than duration")
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
	if info.Size() > maxProjectSize {
		return project.Config{}, fmt.Errorf("read project %s: file exceeds 2 MiB", path)
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxProjectSize))
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
	ticks := durationTicks(opts.duration)
	results := make([]result, 0, len(opts.patterns)*len(opts.loads)*len(opts.seeds)*2)
	for _, pattern := range opts.patterns {
		for _, load := range opts.loads {
			for _, seed := range opts.seeds {
				schedule := demandSchedule(scheduleInput{
					seed: seed, durationTicks: ticks, intervalTicks: durationTicks(load), pattern: pattern,
					passengers: scenario.passengers, focus: scenario.focus,
				})
				id := scheduleID(schedule)
				for _, enabled := range []bool{false, true} {
					outcome, err := run(runInput{
						enabled: enabled, duration: opts.duration, requestEvery: load, seed: seed,
						pattern: pattern, scheduleID: id, queueLimit: opts.queueLimit,
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
	return results, nil
}

type scheduleInput struct {
	seed                         int64
	durationTicks, intervalTicks int64
	pattern, focus               string
	passengers                   []string
}

func demandSchedule(input scheduleInput) []scheduledRequest {
	rng := rand.New(rand.NewSource(input.seed))
	count := int((input.durationTicks - 1) / input.intervalTicks)
	requests := make([]scheduledRequest, 0, count)
	for i := range count {
		tick := int64(i+1) * input.intervalTicks
		if input.pattern == "bursty-hotspot" {
			tick = input.intervalTicks + int64(i/3)*3*input.intervalTicks
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
	default:
		origin := input.passengers[rng.Intn(len(input.passengers))]
		destinations := without(input.passengers, origin)
		return origin, destinations[rng.Intn(len(destinations))]
	}
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
	schedule               []scheduledRequest
	scenario               scenario
}

func run(input runInput) (result, error) {
	simulation, err := sim.NewFleet(input.scenario.network, input.scenario.fleet)
	if err != nil {
		return result{}, fmt.Errorf("create comparison: %w", err)
	}
	if err := simulation.SetDemandWeights(demandWeights(input.pattern, input.scenario)); err != nil {
		return result{}, fmt.Errorf("set demand weights: %w", err)
	}
	simulation.SetRedistribution(input.enabled)
	next, skipped := 0, 0
	for tick := range durationTicks(input.duration) {
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
		}
		simulation.Step()
	}
	state := simulation.Snapshot()
	policy := "off"
	if input.enabled {
		policy = "on"
	}
	return result{
		Pattern: input.pattern, RequestEverySeconds: input.requestEvery.Seconds(), Seed: input.seed,
		Policy: policy, WindowStartSeconds: 0, WindowEndSeconds: input.duration.Seconds(), ScheduleID: input.scheduleID,
		Scheduled: len(input.schedule), Served: state.Completed, Remaining: state.Submitted - state.Completed, Skipped: skipped,
		WaitAverageSeconds: state.Wait.AverageSeconds, WaitMaximumSeconds: state.Wait.MaxSeconds,
		EmptyDistanceMeters: state.EmptyDistanceMeters, PositioningMoveCount: state.RebalanceMoves,
	}, nil
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

func writeReport(output io.Writer, format string, results []result) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report{SchemaVersion: 1, Results: results}); err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
		return nil
	case "csv":
		return writeCSV(output, results)
	default:
		return writeTable(output, results)
	}
}

func writeTable(output io.Writer, results []result) error {
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "window: %.2fs to %.2fs\n", results[0].WindowStartSeconds, results[0].WindowEndSeconds); err != nil {
		return fmt.Errorf("write table window: %w", err)
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "PATTERN\tLOAD (S)\tSEED\tPOLICY\tWAIT AVG (S)\tWAIT MAX (S)\tSERVED\tREMAINING\tSKIPPED\tEMPTY (M)\tMOVES"); err != nil {
		return fmt.Errorf("write table header: %w", err)
	}
	for _, outcome := range results {
		if _, err := fmt.Fprintf(w, "%s\t%.2f\t%d\t%s\t%.2f\t%.2f\t%d\t%d\t%d\t%.1f\t%d\n",
			outcome.Pattern, outcome.RequestEverySeconds, outcome.Seed, outcome.Policy,
			outcome.WaitAverageSeconds, outcome.WaitMaximumSeconds, outcome.Served,
			outcome.Remaining, outcome.Skipped, outcome.EmptyDistanceMeters, outcome.PositioningMoveCount); err != nil {
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
		"pattern", "request_every_seconds", "seed", "policy", "window_start_seconds", "window_end_seconds", "schedule_id",
		"scheduled", "served", "remaining", "skipped", "wait_average_seconds", "wait_maximum_seconds", "empty_distance_meters", "positioning_moves",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, outcome := range results {
		row := []string{
			outcome.Pattern, floatText(outcome.RequestEverySeconds), strconv.FormatInt(outcome.Seed, 10), outcome.Policy,
			floatText(outcome.WindowStartSeconds), floatText(outcome.WindowEndSeconds), outcome.ScheduleID,
			strconv.Itoa(outcome.Scheduled), strconv.Itoa(outcome.Served), strconv.Itoa(outcome.Remaining), strconv.Itoa(outcome.Skipped),
			floatText(outcome.WaitAverageSeconds), floatText(outcome.WaitMaximumSeconds), floatText(outcome.EmptyDistanceMeters),
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

func durationTicks(duration time.Duration) int64 {
	return int64(duration * sim.TicksPerSecond / time.Second)
}
