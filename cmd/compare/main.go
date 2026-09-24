// Command compare runs repeatable demand and policy experiments.
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
	randv2 "math/rand/v2"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	maxWorkers     = 64
)

var syntheticPatterns = []string{"balanced", "destination", "hotspot", "bursty-hotspot", "hub-burst"}
var knownPatterns = append(append([]string(nil), syntheticPatterns...), "profile")

// waitRuleValues maps each -wait-rules name to its simulation rule.
var waitRuleValues = map[string]sim.FinishingPodWait{
	"current": sim.FinishingPodWaitCurrent,
	"strict":  sim.FinishingPodWaitStrict,
	"none":    sim.FinishingPodWaitNone,
}

type options struct {
	duration, arrivalsFor  time.Duration
	requestEvery           time.Duration
	seed                   int64
	seedsText              string
	pattern                string
	patternsText           string
	bandsText              string
	loadsText              string
	sharingLimitsText      string
	routingPoliciesText    string
	redistributionText     string
	waitRulesText          string
	focus                  string
	format                 string
	projectPath            string
	outputPath             string
	queueLimit             int
	burstSize              int
	workers                int
	seeds                  []int64
	patterns               []string
	loads                  []time.Duration
	sharingLimits          []int
	routingPolicies        []string
	redistributionPolicies []bool
	// waitRules is nil when -wait-rules is not given. Then each arm uses the
	// default rule and the report has no wait rule column.
	waitRules       []string
	stopWhenDrained bool
	// adaptiveLimit skips the rates of a group more than pastLimit rates
	// above the first rate at which a seed does not drain.
	adaptiveLimit bool
	pastLimit     int
}

type scheduledRequest struct {
	tick        int64
	origin      string
	destination string
}

type scenario struct {
	network        sim.Network
	fleet          []sim.Placement
	passengers     []string
	focus          string
	demand         project.DemandConfig
	demandProfiles []project.DemandProfile
}

type result struct {
	Pattern                        string  `json:"pattern"`
	DemandProfile                  string  `json:"demand_profile,omitempty"`
	DemandBand                     string  `json:"demand_band,omitempty"`
	RequestEverySeconds            float64 `json:"request_every_seconds"`
	OfferedPerMinute               float64 `json:"offered_per_minute"`
	BurstSize                      int     `json:"burst_size"`
	Seed                           int64   `json:"seed"`
	Policy                         string  `json:"policy"`
	SharedRidePartyLimit           int     `json:"shared_ride_party_limit"`
	SharedParties                  int     `json:"shared_parties"`
	RoutingPolicy                  string  `json:"routing_policy"`
	WaitRule                       string  `json:"wait_rule,omitempty"`
	FocusStation                   string  `json:"focus_station"`
	WindowStartSeconds             float64 `json:"window_start_seconds"`
	WindowEndSeconds               float64 `json:"window_end_seconds"`
	ActualEndSeconds               float64 `json:"actual_end_seconds"`
	ArrivalWindowSeconds           float64 `json:"arrival_window_seconds"`
	ArrivalEndSeconds              float64 `json:"arrival_end_seconds"`
	ScheduleID                     string  `json:"schedule_id"`
	Scheduled                      int     `json:"scheduled"`
	Served                         int     `json:"served"`
	Remaining                      int     `json:"remaining"`
	Skipped                        int     `json:"skipped"`
	CompletedAtArrivalEnd          int     `json:"completed_at_arrival_end"`
	BacklogAtArrivalEnd            int     `json:"backlog_at_arrival_end"`
	ArrivalThroughputPerMinute     float64 `json:"arrival_throughput_per_minute"`
	CompletedAtArrivalMidpoint     int     `json:"completed_at_arrival_midpoint"`
	BacklogAtArrivalMidpoint       int     `json:"backlog_at_arrival_midpoint"`
	LateArrivalThroughputPerMinute float64 `json:"late_arrival_throughput_per_minute"`
	LateBacklogChange              int     `json:"late_backlog_change"`
	Drained                        bool    `json:"drained"`
	DrainSeconds                   float64 `json:"drain_seconds"`
	PeakPending                    int     `json:"peak_pending"`
	PeakOutstanding                int     `json:"peak_outstanding"`
	PeakActiveVehicles             int     `json:"peak_active_vehicles"`
	PeakPassengerVehicles          int     `json:"peak_passenger_vehicles"`
	PeakStoppedVehicles            int     `json:"peak_stopped_vehicles"`
	PeakFocusApproaching           int     `json:"peak_focus_approaching"`
	PeakFocusEntranceStopped       int     `json:"peak_focus_entrance_stopped"`
	PeakFocusExitStopped           int     `json:"peak_focus_exit_stopped"`
	PeakFocusOccupiedBerths        int     `json:"peak_focus_occupied_berths"`
	PeakFocusReservedEmptyBerths   int     `json:"peak_focus_reserved_empty_berths"`
	QueueCleared                   bool    `json:"queue_cleared"`
	QueueClearSeconds              float64 `json:"queue_clear_seconds"`
	WaitAverageSeconds             float64 `json:"wait_average_seconds"`
	WaitMaximumSeconds             float64 `json:"wait_maximum_seconds"`
	PassengerDistanceMeters        float64 `json:"passenger_distance_meters"`
	EmptyDistanceMeters            float64 `json:"empty_distance_meters"`
	LoadedDistancePercent          float64 `json:"loaded_distance_percent"`
	PositioningMoveCount           int     `json:"positioning_moves"`
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
	if err := writeReport(writeReportInput{output: output, format: opts.format, results: results, waitRuleColumn: opts.waitRules != nil}); err != nil {
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
	flags.StringVar(&opts.bandsText, "bands", "", "comma-separated profile bands or all")
	flags.StringVar(&opts.loadsText, "loads", "", "comma-separated request intervals")
	flags.StringVar(&opts.sharingLimitsText, "sharing-limits", "1", "comma-separated same-destination party limits")
	flags.StringVar(&opts.routingPoliciesText, "routing-policies", "free-flow", "comma-separated routing policies: free-flow, congestion")
	flags.StringVar(&opts.redistributionText, "redistribution-policies", "off,on", "comma-separated redistribution policies: off, on")
	flags.StringVar(&opts.waitRulesText, "wait-rules", "current", "comma-separated finishing-pod wait rules: current, strict, none (adds a wait_rule column)")
	flags.StringVar(&opts.focus, "focus", "", "passenger station used by focused patterns")
	flags.StringVar(&opts.format, "format", "table", "output format: table, json, or csv")
	flags.StringVar(&opts.projectPath, "project", "", "raw project configuration path")
	flags.StringVar(&opts.outputPath, "output", "", "write the report to this path")
	flags.IntVar(&opts.queueLimit, "queue-limit", 200, "maximum pending requests before arrivals are skipped")
	flags.IntVar(&opts.burstSize, "burst-size", 3, "requests in each burst for burst patterns")
	flags.IntVar(&opts.workers, "workers", 1, "independent simulation arms to run concurrently")
	flags.BoolVar(&opts.stopWhenDrained, "stop-when-drained", false, "stop after the arrival window when all accepted requests complete")
	flags.BoolVar(&opts.adaptiveLimit, "adaptive-limit", false, "run each arm group from its lowest offered rate, and skip its rates more than -past-limit rates above the first rate at which a seed does not drain (requires -stop-when-drained)")
	flags.IntVar(&opts.pastLimit, "past-limit", 1, "with -adaptive-limit, the number of rates to run after the first rate at which a seed does not drain")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("unexpected positional arguments")
	}
	given := make(map[string]bool)
	flags.Visit(func(set *flag.Flag) { given[set.Name] = true })
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
	if opts.workers < 1 || opts.workers > maxWorkers {
		return options{}, fmt.Errorf("workers must be between 1 and %d", maxWorkers)
	}
	if opts.format != "table" && opts.format != "json" && opts.format != "csv" {
		return options{}, errors.New("format must be table, json, or csv")
	}
	if err := validateAdaptiveLimit(opts, given["past-limit"]); err != nil {
		return options{}, err
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
	opts.routingPolicies, err = parseRoutingPolicies(opts.routingPoliciesText)
	if err != nil {
		return options{}, err
	}
	opts.redistributionPolicies, err = parseRedistributionPolicies(opts.redistributionText)
	if err != nil {
		return options{}, err
	}
	if given["wait-rules"] {
		opts.waitRules, err = parseWaitRules(opts.waitRulesText)
		if err != nil {
			return options{}, err
		}
	}
	if len(opts.seeds)*len(opts.patterns)*len(opts.loads)*len(opts.sharingLimits)*len(opts.routingPolicies)*max(1, len(opts.waitRules)) > maxComparisons {
		return options{}, fmt.Errorf("the matrix must contain at most %d comparisons", maxComparisons)
	}
	return opts, nil
}

// validateAdaptiveLimit checks -adaptive-limit and -past-limit.
// pastLimitGiven reports whether the command line sets -past-limit.
func validateAdaptiveLimit(opts options, pastLimitGiven bool) error {
	if opts.pastLimit < 0 {
		return errors.New("past-limit must be at least 0")
	}
	if pastLimitGiven && !opts.adaptiveLimit {
		return errors.New("past-limit requires -adaptive-limit")
	}
	if opts.adaptiveLimit && !opts.stopWhenDrained {
		return errors.New("adaptive-limit requires -stop-when-drained")
	}
	return nil
}

func parseRedistributionPolicies(value string) ([]bool, error) {
	parts := strings.Split(value, ",")
	policies := make([]bool, 0, len(parts))
	seen := make(map[bool]bool, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "off" && name != "on" {
			return nil, fmt.Errorf("unknown redistribution policy %q", name)
		}
		enabled := name == "on"
		if seen[enabled] {
			return nil, fmt.Errorf("redistribution policy %q appears more than once", name)
		}
		seen[enabled] = true
		policies = append(policies, enabled)
	}
	return policies, nil
}

// parseWaitRules reads the -wait-rules list. Each name must be a key of
// waitRuleValues and can occur only once. The order of the list is the order
// of the arms in the report.
func parseWaitRules(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	rules := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		rule := strings.TrimSpace(part)
		if _, ok := waitRuleValues[rule]; !ok {
			return nil, fmt.Errorf("unknown wait rule %q", rule)
		}
		if seen[rule] {
			return nil, fmt.Errorf("wait rule %q appears more than once", rule)
		}
		seen[rule] = true
		rules = append(rules, rule)
	}
	return rules, nil
}

func parseRoutingPolicies(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	policies := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		policy := strings.TrimSpace(part)
		if policy != "free-flow" && policy != "congestion" {
			return nil, fmt.Errorf("unknown routing policy %q", policy)
		}
		if seen[policy] {
			return nil, fmt.Errorf("routing policy %q appears more than once", policy)
		}
		seen[policy] = true
		policies = append(policies, policy)
	}
	return policies, nil
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
		return append([]string(nil), syntheticPatterns...), nil
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
	return scenario{
		network: config.Network, fleet: config.Fleet, passengers: passengers, focus: focus,
		demand: config.Demand, demandProfiles: config.DemandProfiles,
	}, nil
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

type demandArm struct {
	pattern, profile, band string
	flows                  []weightedDemandFlow
}

type weightedDemandFlow struct {
	from, to   string
	cumulative float64
}

func compare(opts options, scenario scenario) ([]result, error) {
	arms, err := demandArms(opts, scenario)
	if err != nil {
		return nil, err
	}
	// An empty wait rule selects the default rule and leaves the result
	// without a wait rule.
	waitRules := opts.waitRules
	if waitRules == nil {
		waitRules = []string{""}
	}
	if len(opts.seeds)*len(arms)*len(opts.loads)*len(opts.sharingLimits)*len(opts.routingPolicies)*len(waitRules) > maxComparisons {
		return nil, fmt.Errorf("the expanded matrix must contain at most %d comparisons", maxComparisons)
	}
	inputs := make([]runInput, 0, len(arms)*len(opts.loads)*len(opts.seeds)*len(opts.sharingLimits)*len(opts.routingPolicies)*len(waitRules)*2)
	for _, arm := range arms {
		for _, load := range opts.loads {
			for _, seed := range opts.seeds {
				schedule := demandSchedule(scheduleInput{
					seed: seed, durationTicks: durationTicks(opts.arrivalsFor), intervalTicks: durationTicks(load), pattern: arm.pattern,
					burstSize:  opts.burstSize,
					passengers: scenario.passengers, focus: scenario.focus, profileFlows: arm.flows,
				})
				id := scheduleID(schedule)
				for _, sharingLimit := range opts.sharingLimits {
					for _, routingPolicy := range opts.routingPolicies {
						for _, waitRule := range waitRules {
							for _, enabled := range opts.redistributionPolicies {
								inputs = append(inputs, runInput{
									enabled: enabled, duration: opts.duration, requestEvery: load, seed: seed,
									pattern: arm.pattern, profile: arm.profile, band: arm.band,
									scheduleID: id, queueLimit: opts.queueLimit, arrivalsFor: opts.arrivalsFor,
									burstSize: opts.burstSize, sharingLimit: sharingLimit, routingPolicy: routingPolicy,
									waitRule: waitRule, schedule: schedule, scenario: scenario, stopWhenDrained: opts.stopWhenDrained,
								})
							}
						}
					}
				}
			}
		}
	}
	if opts.adaptiveLimit {
		return runAdaptive(adaptiveRun{inputs: inputs, workers: opts.workers, pastLimit: opts.pastLimit, run: run})
	}
	return runComparisons(inputs, opts.workers)
}

type comparisonJob struct {
	index int
	input runInput
}

func runComparisons(inputs []runInput, workers int) ([]result, error) {
	results := make([]result, len(inputs))
	errorsByIndex := make([]error, len(inputs))
	jobs := make(chan comparisonJob)
	var group sync.WaitGroup
	for range min(workers, len(inputs)) {
		group.Go(func() {
			for job := range jobs {
				results[job.index], errorsByIndex[job.index] = run(job.input)
			}
		})
	}
	for index, input := range inputs {
		jobs <- comparisonJob{index: index, input: input}
	}
	close(jobs)
	group.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func demandArms(opts options, scenario scenario) ([]demandArm, error) {
	arms := make([]demandArm, 0, len(opts.patterns))
	for _, pattern := range opts.patterns {
		if pattern != "profile" {
			if opts.bandsText != "" {
				return nil, errors.New("bands require the profile demand pattern")
			}
			arms = append(arms, demandArm{pattern: pattern})
			continue
		}
		profile, err := selectedDemandProfile(scenario)
		if err != nil {
			return nil, err
		}
		bands, err := selectedDemandBands(profile, scenario.demand.Band, opts.bandsText)
		if err != nil {
			return nil, err
		}
		for _, band := range bands {
			flows := weightedProfileFlows(profile, band)
			arms = append(arms, demandArm{pattern: pattern, profile: profile.ID, band: profile.Bands[band].ID, flows: flows})
		}
	}
	return arms, nil
}

func selectedDemandProfile(scenario scenario) (project.DemandProfile, error) {
	profileID := scenario.demand.Profile
	if profileID == "" && len(scenario.demandProfiles) == 1 {
		profileID = scenario.demandProfiles[0].ID
	}
	for _, profile := range scenario.demandProfiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return project.DemandProfile{}, errors.New("profile demand requires a selected project demand profile")
}

func selectedDemandBands(profile project.DemandProfile, defaultBand, bandsText string) ([]int, error) {
	if bandsText == "" {
		bandsText = defaultBand
	}
	if strings.TrimSpace(bandsText) == "all" {
		bands := make([]int, len(profile.Bands))
		for index := range bands {
			bands[index] = index
		}
		return bands, nil
	}
	if strings.TrimSpace(bandsText) == "" {
		return nil, errors.New("profile demand requires a demand band")
	}
	parts := strings.Split(bandsText, ",")
	bands := make([]int, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if seen[id] {
			return nil, fmt.Errorf("demand band %q appears more than once", id)
		}
		found := -1
		for index, band := range profile.Bands {
			if band.ID == id {
				found = index
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("unknown demand band %q", id)
		}
		seen[id] = true
		bands = append(bands, found)
	}
	return bands, nil
}

func weightedProfileFlows(profile project.DemandProfile, band int) []weightedDemandFlow {
	flows := make([]weightedDemandFlow, 0, len(profile.Flows))
	total := 0.0
	for _, flow := range profile.Flows {
		weight := flow.Weights[band]
		if weight <= 0 {
			continue
		}
		total += weight
		flows = append(flows, weightedDemandFlow{from: flow.From, to: flow.To, cumulative: total})
	}
	return flows
}

type scheduleInput struct {
	seed                         int64
	durationTicks, intervalTicks int64
	burstSize                    int
	pattern, focus               string
	passengers                   []string
	profileFlows                 []weightedDemandFlow
}

func demandSchedule(input scheduleInput) []scheduledRequest {
	if input.pattern == "profile" {
		return profileDemandSchedule(input)
	}
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

func profileDemandSchedule(input scheduleInput) []scheduledRequest {
	count := int((input.durationTicks - 1) / input.intervalTicks)
	requests := make([]scheduledRequest, 0, count)
	seed := uint64(input.seed) // #nosec G115 -- Preserve the signed seed's bits for deterministic PCG input.
	rng := randv2.New(randv2.NewPCG(seed, ^seed))
	total := input.profileFlows[len(input.profileFlows)-1].cumulative
	for index := range count {
		target := rng.Float64() * total
		selected := sort.Search(len(input.profileFlows), func(flowIndex int) bool {
			return input.profileFlows[flowIndex].cumulative > target
		})
		flow := input.profileFlows[selected]
		requests = append(requests, scheduledRequest{
			tick: int64(index+1) * input.intervalTicks, origin: flow.from, destination: flow.to,
		})
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
	enabled                             bool
	duration, arrivalsFor, requestEvery time.Duration
	seed                                int64
	pattern, profile, band, scheduleID  string
	queueLimit                          int
	burstSize                           int
	sharingLimit                        int
	routingPolicy                       string
	// waitRule names a waitRuleValues key. Empty selects the default rule.
	waitRule        string
	schedule        []scheduledRequest
	scenario        scenario
	stopWhenDrained bool
}

func run(input runInput) (result, error) {
	simulation, err := sim.NewFleet(input.scenario.network, input.scenario.fleet)
	if err != nil {
		return result{}, fmt.Errorf("create comparison: %w", err)
	}
	weights, err := demandWeights(input.pattern, input.band, input.scenario)
	if err != nil {
		return result{}, err
	}
	if demandErr := simulation.SetDemandWeights(weights); demandErr != nil {
		return result{}, fmt.Errorf("set demand weights: %w", demandErr)
	}
	if sharingErr := simulation.SetSharedRidePartyLimit(input.sharingLimit); sharingErr != nil {
		return result{}, fmt.Errorf("set sharing limit: %w", sharingErr)
	}
	simulation.SetCongestionRouting(input.routingPolicy == "congestion")
	if input.waitRule != "" {
		rule, ok := waitRuleValues[input.waitRule]
		if !ok {
			return result{}, fmt.Errorf("unknown wait rule %q", input.waitRule)
		}
		if waitErr := simulation.SetFinishingPodWait(rule); waitErr != nil {
			return result{}, fmt.Errorf("set wait rule: %w", waitErr)
		}
	}
	simulation.SetRedistribution(input.enabled)
	metrics, err := newRunMetrics(input.scenario, input.schedule)
	if err != nil {
		return result{}, err
	}
	next, skipped := 0, 0
	arrivalWindowTicks := durationTicks(input.arrivalsFor)
	arrivalMidpointTicks := arrivalWindowTicks / 2
	midpointState := simulation.Snapshot()
	arrivalState := simulation.Snapshot()
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
		if (tick+1)%sim.TicksPerSecond == 0 || tick+1 == arrivalMidpointTicks || tick+1 == arrivalWindowTicks {
			state := simulation.Snapshot()
			if state.Tick == arrivalMidpointTicks {
				midpointState = state
			}
			if state.Tick == arrivalWindowTicks {
				arrivalState = state
			}
			metrics.observe(state)
			if input.stopWhenDrained && state.Tick >= arrivalWindowTicks && next == len(input.schedule) && state.Completed == state.Submitted {
				break
			}
		}
	}
	state := simulation.Snapshot()
	if arrivalState.Tick != arrivalWindowTicks {
		arrivalState = state
	}
	if midpointState.Tick != arrivalMidpointTicks {
		midpointState = arrivalState
	}
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
	drained := next == len(input.schedule) && state.Completed == state.Submitted
	drainSeconds := 0.0
	if drained && state.Tick > arrivalWindowTicks {
		drainSeconds = float64(state.Tick-arrivalWindowTicks) / sim.TicksPerSecond
	}
	arrivalMinutes := input.arrivalsFor.Minutes()
	lateArrivalMinutes := float64(arrivalWindowTicks-arrivalMidpointTicks) / sim.TicksPerSecond / 60
	midpointBacklog := midpointState.Submitted - midpointState.Completed
	arrivalBacklog := arrivalState.Submitted - arrivalState.Completed
	return result{
		Pattern: input.pattern, DemandProfile: input.profile, DemandBand: input.band,
		RequestEverySeconds: input.requestEvery.Seconds(), OfferedPerMinute: float64(len(input.schedule)) / arrivalMinutes, Seed: input.seed,
		BurstSize: burstSize, Policy: policy, SharedRidePartyLimit: input.sharingLimit,
		SharedParties: state.SharedParties, RoutingPolicy: input.routingPolicy, WaitRule: input.waitRule, FocusStation: input.scenario.focus,
		WindowStartSeconds: 0, WindowEndSeconds: input.duration.Seconds(), ActualEndSeconds: float64(state.Tick) / sim.TicksPerSecond,
		ArrivalWindowSeconds: input.arrivalsFor.Seconds(), ArrivalEndSeconds: arrivalEnd, ScheduleID: input.scheduleID,
		Scheduled: len(input.schedule), Served: state.Completed, Remaining: state.Submitted - state.Completed, Skipped: skipped,
		CompletedAtArrivalEnd: arrivalState.Completed, BacklogAtArrivalEnd: arrivalState.Submitted - arrivalState.Completed,
		ArrivalThroughputPerMinute: float64(arrivalState.Completed) / arrivalMinutes,
		CompletedAtArrivalMidpoint: midpointState.Completed, BacklogAtArrivalMidpoint: midpointBacklog,
		LateArrivalThroughputPerMinute: float64(arrivalState.Completed-midpointState.Completed) / lateArrivalMinutes,
		LateBacklogChange:              arrivalBacklog - midpointBacklog,
		Drained:                        drained, DrainSeconds: drainSeconds,
		PeakPending: metrics.peakPending, PeakOutstanding: metrics.peakOutstanding,
		PeakActiveVehicles: metrics.peakActiveVehicles, PeakPassengerVehicles: metrics.peakPassengerVehicles,
		PeakStoppedVehicles: metrics.peakStoppedVehicles, PeakFocusApproaching: metrics.peakApproaching,
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

func demandWeights(pattern, band string, scenario scenario) (map[string]float64, error) {
	if pattern == "balanced" {
		return nil, nil
	}
	weights := make(map[string]float64, len(scenario.passengers))
	if pattern == "profile" {
		profile, err := selectedDemandProfile(scenario)
		if err != nil {
			return nil, err
		}
		bands, err := selectedDemandBands(profile, "", band)
		if err != nil {
			return nil, err
		}
		for _, flow := range profile.Flows {
			weights[flow.From] += flow.Weights[bands[0]]
		}
		return weights, nil
	}
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
	return weights, nil
}

type writeReportInput struct {
	output  io.Writer
	format  string
	results []result
	// waitRuleColumn adds the wait rule to table and CSV output. JSON output
	// has the wait_rule field only when a result has a wait rule.
	waitRuleColumn bool
}

func writeReport(input writeReportInput) error {
	switch input.format {
	case "json":
		encoder := json.NewEncoder(input.output)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report{SchemaVersion: 5, Results: input.results}); err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
		return nil
	case "csv":
		return writeCSV(input)
	default:
		return writeTable(input)
	}
}

// writeTable writes one aligned row for each result. When
// input.waitRuleColumn is set, a WAIT RULE column follows POLICY.
func writeTable(input writeReportInput) error {
	output, results := input.output, input.results
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "window: %.2fs to %.2fs / arrivals end: %.2fs / focus: %s\n", results[0].WindowStartSeconds, results[0].WindowEndSeconds, results[0].ArrivalEndSeconds, results[0].FocusStation); err != nil {
		return fmt.Errorf("write table window: %w", err)
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	policyHeader := "POLICY"
	if input.waitRuleColumn {
		policyHeader += "\tWAIT RULE"
	}
	if _, err := fmt.Fprintln(w, "PATTERN\tBAND\tLOAD (S)\tOFFERED/M\tARRIVAL/M\tLATE/M\tBACKLOG\tLATE DELTA\tDRAIN (S)\tSEED\t"+policyHeader+"\tWAIT AVG\tWAIT MAX\tSERVED\tLEFT\tSKIPPED\tPEAK OUT\tPEAK ACTIVE\tPEAK PAX\tPEAK STOPPED\tHUB IN\tHUB OUT\tHUB OCC\tHUB RSV\tPASSENGER (M)\tEMPTY (M)\tLOADED %\tMOVES"); err != nil {
		return fmt.Errorf("write table header: %w", err)
	}
	for _, outcome := range results {
		policy := outcome.Policy
		if input.waitRuleColumn {
			policy += "\t" + outcome.WaitRule
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%d\t%d\t%s\t%d\t%s\t%.2f\t%.2f\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%.1f\t%.1f\t%.2f\t%d\n",
			outcome.Pattern, outcome.DemandBand, outcome.RequestEverySeconds, outcome.OfferedPerMinute,
			outcome.ArrivalThroughputPerMinute, outcome.LateArrivalThroughputPerMinute,
			outcome.BacklogAtArrivalEnd, outcome.LateBacklogChange, drainText(outcome), outcome.Seed, policy,
			outcome.WaitAverageSeconds, outcome.WaitMaximumSeconds, outcome.Served, outcome.Remaining, outcome.Skipped,
			outcome.PeakOutstanding, outcome.PeakActiveVehicles, outcome.PeakPassengerVehicles, outcome.PeakStoppedVehicles,
			outcome.PeakFocusEntranceStopped, outcome.PeakFocusExitStopped,
			outcome.PeakFocusOccupiedBerths, outcome.PeakFocusReservedEmptyBerths,
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

// writeCSV writes a header and one row for each result. When
// input.waitRuleColumn is set, a wait_rule column follows routing_policy.
// Without it, the columns are the same as before the wait rule option.
func writeCSV(input writeReportInput) error {
	w := csv.NewWriter(input.output)
	header := []string{
		"pattern", "demand_profile", "demand_band", "request_every_seconds", "offered_per_minute", "burst_size", "seed", "policy", "routing_policy",
	}
	if input.waitRuleColumn {
		header = append(header, "wait_rule")
	}
	header = append(header,
		"shared_ride_party_limit", "shared_parties", "focus_station", "window_start_seconds", "window_end_seconds", "actual_end_seconds", "arrival_window_seconds", "arrival_end_seconds", "schedule_id",
		"scheduled", "served", "remaining", "skipped", "completed_at_arrival_end", "backlog_at_arrival_end", "arrival_throughput_per_minute",
		"completed_at_arrival_midpoint", "backlog_at_arrival_midpoint", "late_arrival_throughput_per_minute", "late_backlog_change", "drained", "drain_seconds",
		"peak_pending", "peak_outstanding", "peak_active_vehicles", "peak_passenger_vehicles", "peak_stopped_vehicles", "peak_focus_approaching", "peak_focus_entrance_stopped", "peak_focus_exit_stopped",
		"peak_focus_occupied_berths", "peak_focus_reserved_empty_berths", "queue_cleared", "queue_clear_seconds",
		"wait_average_seconds", "wait_maximum_seconds", "passenger_distance_meters", "empty_distance_meters", "loaded_distance_percent", "positioning_moves",
	)
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, outcome := range input.results {
		row := []string{
			outcome.Pattern, outcome.DemandProfile, outcome.DemandBand, floatText(outcome.RequestEverySeconds), floatText(outcome.OfferedPerMinute), strconv.Itoa(outcome.BurstSize), strconv.FormatInt(outcome.Seed, 10), outcome.Policy, outcome.RoutingPolicy,
		}
		if input.waitRuleColumn {
			row = append(row, outcome.WaitRule)
		}
		row = append(row,
			strconv.Itoa(outcome.SharedRidePartyLimit), strconv.Itoa(outcome.SharedParties), outcome.FocusStation,
			floatText(outcome.WindowStartSeconds), floatText(outcome.WindowEndSeconds), floatText(outcome.ActualEndSeconds), floatText(outcome.ArrivalWindowSeconds), floatText(outcome.ArrivalEndSeconds), outcome.ScheduleID,
			strconv.Itoa(outcome.Scheduled), strconv.Itoa(outcome.Served), strconv.Itoa(outcome.Remaining), strconv.Itoa(outcome.Skipped),
			strconv.Itoa(outcome.CompletedAtArrivalEnd), strconv.Itoa(outcome.BacklogAtArrivalEnd), floatText(outcome.ArrivalThroughputPerMinute),
			strconv.Itoa(outcome.CompletedAtArrivalMidpoint), strconv.Itoa(outcome.BacklogAtArrivalMidpoint), floatText(outcome.LateArrivalThroughputPerMinute), strconv.Itoa(outcome.LateBacklogChange),
			strconv.FormatBool(outcome.Drained), floatText(outcome.DrainSeconds),
			strconv.Itoa(outcome.PeakPending), strconv.Itoa(outcome.PeakOutstanding), strconv.Itoa(outcome.PeakActiveVehicles), strconv.Itoa(outcome.PeakPassengerVehicles), strconv.Itoa(outcome.PeakStoppedVehicles),
			strconv.Itoa(outcome.PeakFocusApproaching), strconv.Itoa(outcome.PeakFocusEntranceStopped), strconv.Itoa(outcome.PeakFocusExitStopped),
			strconv.Itoa(outcome.PeakFocusOccupiedBerths), strconv.Itoa(outcome.PeakFocusReservedEmptyBerths), strconv.FormatBool(outcome.QueueCleared), floatText(outcome.QueueClearSeconds),
			floatText(outcome.WaitAverageSeconds), floatText(outcome.WaitMaximumSeconds), floatText(outcome.PassengerDistanceMeters),
			floatText(outcome.EmptyDistanceMeters), floatText(outcome.LoadedDistancePercent),
			strconv.Itoa(outcome.PositioningMoveCount),
		)
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

func drainText(outcome result) string {
	if !outcome.Drained {
		return "-"
	}
	return fmt.Sprintf("%.1f", outcome.DrainSeconds)
}

func durationTicks(duration time.Duration) int64 {
	return int64(duration * sim.TicksPerSecond / time.Second)
}

type runMetrics struct {
	monitor                 observe.StationMonitor
	station                 sim.Station
	lastArrivalTick         int64
	peakPending             int
	peakOutstanding         int
	peakActiveVehicles      int
	peakPassengerVehicles   int
	peakStoppedVehicles     int
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

// workingVehicles counts the pods with assigned work in state. A pod has
// assigned work when it holds a trip that is not complete, or when a pending
// request names it as the pickup pod. The first case includes boarding,
// travel with passengers, and unloading. The second case includes a pod on
// its way to the pickup station and a pod that waits there to board.
//
// An empty move without a pending request, such as redistribution or a move
// to parking, is not work. A pod keeps its last Request after the trip, so
// the count also ignores a completed Request. Every pod with passengers
// aboard has a trip that is not complete, so peak_passenger_vehicles is
// never more than peak_active_vehicles.
func workingVehicles(state sim.Snapshot) int {
	pickups := make(map[string]bool, len(state.Pending))
	for _, request := range state.Pending {
		if request.PodID != "" {
			pickups[request.PodID] = true
		}
	}
	working := 0
	for _, vehicle := range state.Vehicles {
		if vehicle.Request != nil && !vehicle.Request.Completed || pickups[vehicle.Pod.ID] {
			working++
		}
	}
	return working
}

func (metrics *runMetrics) observe(state sim.Snapshot) {
	station := metrics.monitor.Summarize(metrics.station, state)
	metrics.peakPending = max(metrics.peakPending, len(state.Pending))
	metrics.peakOutstanding = max(metrics.peakOutstanding, state.Submitted-state.Completed)
	active, passenger, stopped := workingVehicles(state), 0, 0
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.Occupied {
			passenger++
		}
		if vehicle.Pod.WaitReason != sim.NoWait && vehicle.Pod.Speed < 0.01 {
			stopped++
		}
	}
	metrics.peakActiveVehicles = max(metrics.peakActiveVehicles, active)
	metrics.peakPassengerVehicles = max(metrics.peakPassengerVehicles, passenger)
	metrics.peakStoppedVehicles = max(metrics.peakStoppedVehicles, stopped)
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
