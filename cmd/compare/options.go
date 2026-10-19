package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

type options struct {
	energyPath             string
	duration, arrivalsFor  time.Duration
	requestEvery           time.Duration
	seed                   int64
	seedsText              string
	pattern                string
	patternsText           string
	dailyStartMinute       int
	bandsText              string
	loadsText              string
	sharingConsentText     string
	sharingConsent         sim.SharingConsent
	sharingConsentColumn   bool
	sharingLimitsText      string
	sharingModesText       string
	routingPoliciesText    string
	redistributionText     string
	waitRulesText          string
	platoonPoliciesText    string
	sharingJoinsText       string
	onboardPickupsText     string
	pickupReassignmentText string
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
	sharingModes           []sim.SharedRideMode
	sharingMaxStops        int
	routingPolicies        []string
	redistributionPolicies []string
	// waitRules is nil when -wait-rules is not given. Then each arm uses the
	// default rule and the report has no wait rule column.
	waitRules []string
	// platoonPolicies is nil when -platoon-policies is not given. Then
	// each arm runs without platoons and the report has no platoon policy
	// column.
	platoonPolicies []string
	// sharingJoins is nil when -sharing-joins is not given. Then each arm
	// uses the default join policy and the report has no join policy
	// column.
	sharingJoins       []string
	onboardPickups     []string
	pickupReassignment []string
	stopWhenDrained    bool
	railForecast       bool
	// adaptiveLimit skips the rates of a group more than pastLimit rates
	// above the first rate at which a seed does not drain.
	adaptiveLimit bool
	pastLimit     int
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{}
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defineFlags(flags, &opts)
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("unexpected positional arguments")
	}
	given := make(map[string]bool)
	flags.Visit(func(set *flag.Flag) { given[set.Name] = true })
	opts.sharingConsentColumn = given["sharing-consent"]
	for _, step := range optionSteps {
		if err := step(&opts, given); err != nil {
			return options{}, err
		}
	}
	return opts, nil
}

// optionSteps check and parse the options after the flags. given holds the
// flags that the command line sets. The first error is the refusal, so the
// order is part of the result. A step can read the values that the steps
// before it set.
var optionSteps = [...]func(*options, map[string]bool) error{
	checkDurationOptions,
	checkRunLimitOptions,
	checkAdaptiveOptions,
	parseConsentOption,
	parseSeedOptions,
	parsePatternOptions,
	checkDailyOptions,
	checkRailOptions,
	parseLoadOptions,
	parseSharingOptions,
	parseRoutingOptions,
	parseGivenArmOptions,
	parseOnboardPickups,
	parseExperimentalOptions,
	checkMatrixSize,
}

// defineFlags defines the command-line flags of compare, with opts as their
// destination.
func defineFlags(flags *flag.FlagSet, opts *options) {
	flags.DurationVar(&opts.duration, "duration", 30*time.Minute, "simulated comparison duration")
	flags.DurationVar(&opts.arrivalsFor, "arrivals-for", 0, "simulated arrival window; default is the full duration")
	flags.DurationVar(&opts.requestEvery, "request-every", 45*time.Second, "simulated time between requests")
	flags.Int64Var(&opts.seed, "seed", 1, "demand schedule seed")
	flags.StringVar(&opts.seedsText, "seeds", "", "comma-separated demand schedule seeds")
	flags.StringVar(&opts.pattern, "pattern", "hotspot", "demand pattern")
	flags.StringVar(&opts.patternsText, "patterns", "", "comma-separated demand patterns or all")
	flags.IntVar(&opts.dailyStartMinute, "daily-start-minute", -1, "initial daily profile clock minute; default is the project value")
	flags.StringVar(&opts.bandsText, "bands", "", "comma-separated profile bands or all")
	flags.StringVar(&opts.loadsText, "loads", "", "comma-separated request intervals")
	flags.StringVar(&opts.sharingConsentText, "sharing-consent", string(sim.PrivateConsent), "consent for every offered party: private or shared (default private; adds a sharing_consent column when given)")
	flags.StringVar(&opts.sharingLimitsText, "sharing-limits", "1", "comma-separated shared ride party limits")
	flags.StringVar(&opts.sharingModesText, "sharing-modes", string(sim.DefaultSharedRideMode), "comma-separated shared ride modes: drop-offs, destination")
	flags.IntVar(&opts.sharingMaxStops, "sharing-max-stops", sim.DefaultSharedRideMaxStops, "intermediate stops of a pod in drop-offs mode")
	flags.StringVar(&opts.sharingJoinsText, "sharing-joins", string(sim.DefaultSharedRideJoin), "comma-separated shared ride join policies: unassigned, reassign-existing (adds a sharing_join column)")
	flags.StringVar(&opts.onboardPickupsText, "onboard-pickups", "off", "comma-separated occupied pickup policies: off, on (on requires sharing above one party and drop-offs mode; adds an onboard_pickups column)")
	flags.StringVar(&opts.routingPoliciesText, "routing-policies", "free-flow", "comma-separated routing policies: free-flow, congestion, queue, predictive")
	flags.StringVar(&opts.redistributionText, "redistribution-policies", "off,on", "comma-separated redistribution policies: off, on")
	flags.StringVar(&opts.waitRulesText, "wait-rules", "current", "comma-separated finishing-pod wait rules: current, strict, none (adds a wait_rule column)")
	flags.StringVar(&opts.platoonPoliciesText, "platoon-policies", "off", "comma-separated platoon policies: off, virtual (adds a platoon_policy column)")
	flags.StringVar(&opts.pickupReassignmentText, "pickup-reassignment", "off", "comma-separated experimental pickup reassignment policies: off, on")
	flags.StringVar(&opts.focus, "focus", "", "passenger station used by focused patterns")
	flags.StringVar(&opts.format, "format", "table", "output format: table, json, or csv")
	flags.StringVar(&opts.energyPath, "energy-file", "", "authored flat-v1 energy model file (adds energy estimates)")
	flags.StringVar(&opts.projectPath, "project", "", "raw project configuration path")
	flags.StringVar(&opts.outputPath, "output", "", "write the report to this path")
	flags.IntVar(&opts.queueLimit, "queue-limit", 200, "maximum pending requests before arrivals are skipped")
	flags.IntVar(&opts.burstSize, "burst-size", 3, "requests in each burst for burst patterns")
	flags.IntVar(&opts.workers, "workers", 1, "independent simulation arms to run concurrently")
	flags.BoolVar(&opts.railForecast, "rail-forecast", false, "position empty pods for known rail offers (comparison experiment)")
	flags.BoolVar(&opts.stopWhenDrained, "stop-when-drained", false, "stop after the arrival window when all accepted requests complete")
	flags.BoolVar(&opts.adaptiveLimit, "adaptive-limit", false, "run each arm group from its lowest offered rate, and skip its rates more than -past-limit rates above the first rate at which a seed does not drain (requires -stop-when-drained)")
	flags.IntVar(&opts.pastLimit, "past-limit", 1, "with -adaptive-limit, the number of rates to run after the first rate at which a seed does not drain")
}

// checkDurationOptions checks the duration, and then the arrival window. An
// omitted arrival window is the full duration.
func checkDurationOptions(opts *options, _ map[string]bool) error {
	if opts.duration <= 0 || opts.duration > maxDuration {
		return fmt.Errorf("duration must be between one simulation tick and %s", maxDuration)
	}
	if durationTicks(opts.duration) < 1 {
		return errors.New("duration must be at least one simulation tick")
	}
	if opts.arrivalsFor == 0 {
		opts.arrivalsFor = opts.duration
	}
	if opts.arrivalsFor <= 0 || opts.arrivalsFor > opts.duration || durationTicks(opts.arrivalsFor) < 1 {
		return errors.New("arrivals-for must be at least one simulation tick and no longer than duration")
	}
	return nil
}

// checkRunLimitOptions checks the queue limit, the burst size, the worker
// count, and then the report format.
func checkRunLimitOptions(opts *options, _ map[string]bool) error {
	if opts.queueLimit < 1 || opts.queueLimit > maxQueueLimit {
		return fmt.Errorf("queue-limit must be between 1 and %d", maxQueueLimit)
	}
	if opts.burstSize < 1 || opts.burstSize > maxBurstSize {
		return fmt.Errorf("burst-size must be between 1 and %d", maxBurstSize)
	}
	if opts.workers < 1 || opts.workers > maxWorkers {
		return fmt.Errorf("workers must be between 1 and %d", maxWorkers)
	}
	if opts.format != "table" && opts.format != "json" && opts.format != "csv" {
		return errors.New("format must be table, json, or csv")
	}
	return nil
}

// checkAdaptiveOptions checks -adaptive-limit and -past-limit. See
// validateAdaptiveLimit.
func checkAdaptiveOptions(opts *options, given map[string]bool) error {
	return validateAdaptiveLimit(*opts, given["past-limit"])
}

// parseConsentOption parses -sharing-consent. An empty value is not valid.
func parseConsentOption(opts *options, _ map[string]bool) error {
	if opts.sharingConsentText == "" {
		return errors.New("sharing-consent must be private or shared")
	}
	consent, err := comparisonConsent(sim.SharingConsent(opts.sharingConsentText))
	opts.sharingConsent = consent
	return err
}

// parseSeedOptions parses -seeds, or uses -seed.
func parseSeedOptions(opts *options, _ map[string]bool) error {
	seeds, err := parseSeeds(opts.seed, opts.seedsText)
	opts.seeds = seeds
	return err
}

// parsePatternOptions parses -patterns, or uses -pattern. Then it checks
// that -rail-forecast has only rail patterns.
func parsePatternOptions(opts *options, _ map[string]bool) error {
	patterns, err := parsePatterns(opts.pattern, opts.patternsText)
	opts.patterns = patterns
	if err != nil {
		return err
	}
	if opts.railForecast && slices.ContainsFunc(opts.patterns, func(pattern string) bool {
		return pattern != "rail-arrivals" && pattern != "rail-services"
	}) {
		return errors.New("rail-forecast requires only rail-arrivals or rail-services patterns")
	}
	return nil
}

// checkDailyOptions checks the options of the profile-daily pattern. See
// validateDailyOptions.
func checkDailyOptions(opts *options, given map[string]bool) error {
	return validateDailyOptions(*opts, given)
}

// checkRailOptions checks the options of the rail patterns. See
// validateRailOptions.
func checkRailOptions(opts *options, given map[string]bool) error {
	return validateRailOptions(*opts, given)
}

// parseLoadOptions parses the request intervals. A single rail or daily
// pattern takes its timing from the project, and has one zero load.
func parseLoadOptions(opts *options, _ map[string]bool) error {
	if len(opts.patterns) == 1 && (opts.patterns[0] == "rail-arrivals" || opts.patterns[0] == "rail-services" || opts.patterns[0] == "profile-daily") {
		opts.loads = []time.Duration{0}
		return nil
	}
	loads, err := parseLoads(parseLoadsInput{single: opts.requestEvery, list: opts.loadsText, duration: opts.arrivalsFor})
	opts.loads = loads
	return err
}

// parseSharingOptions parses the shared ride limits and modes, and then
// checks the stop limit.
func parseSharingOptions(opts *options, _ map[string]bool) error {
	var err error
	if opts.sharingLimits, err = parseSharingLimits(opts.sharingLimitsText); err != nil {
		return err
	}
	if opts.sharingModes, err = parseSharingModes(opts.sharingModesText); err != nil {
		return err
	}
	if opts.sharingMaxStops < 1 || opts.sharingMaxStops > sim.MaxSharedRideStops {
		return fmt.Errorf("sharing-max-stops must be between 1 and %d", sim.MaxSharedRideStops)
	}
	return nil
}

// parseRoutingOptions parses the routing policies, and then the
// redistribution policies.
func parseRoutingOptions(opts *options, _ map[string]bool) error {
	var err error
	if opts.routingPolicies, err = parseRoutingPolicies(opts.routingPoliciesText); err != nil {
		return err
	}
	opts.redistributionPolicies, err = parseRedistributionPolicies(opts.redistributionText)
	return err
}

// parseGivenArmOptions parses the wait rules, the platoon policies, and then
// the sharing joins. It parses only the lists that the command line sets,
// because each list adds a report column.
func parseGivenArmOptions(opts *options, given map[string]bool) error {
	var err error
	if given["wait-rules"] {
		if opts.waitRules, err = parseWaitRules(opts.waitRulesText); err != nil {
			return err
		}
	}
	if given["platoon-policies"] {
		if opts.platoonPolicies, err = parsePlatoonPolicies(opts.platoonPoliciesText); err != nil {
			return err
		}
	}
	if given["sharing-joins"] {
		if opts.sharingJoins, err = parseSharingJoins(opts.sharingJoinsText); err != nil {
			return err
		}
	}
	return nil
}

// parseOnboardPickups parses -onboard-pickups. See parseOnboardOptions.
func parseOnboardPickups(opts *options, given map[string]bool) error {
	return parseOnboardOptions(opts, given["onboard-pickups"])
}

// checkMatrixSize checks the number of comparisons in the matrix.
func checkMatrixSize(opts *options, _ map[string]bool) error {
	if len(opts.seeds)*len(opts.patterns)*len(opts.loads)*len(sharingArms(*opts))*max(1, len(opts.sharingJoins))*len(opts.routingPolicies)*
		max(1, len(opts.waitRules))*max(1, len(opts.platoonPolicies))*experimentalArmCount(*opts)*onboardArmCount(*opts) > maxComparisons {
		return fmt.Errorf("the matrix must contain at most %d comparisons", maxComparisons)
	}
	return nil
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

// parseRedistributionPolicies reads the -redistribution-policies list.
// Each name must be a key of redistributionPolicyValues and can occur only
// once. The order of the list is the order of the arms in the report.
func parseRedistributionPolicies(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	policies := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if _, ok := redistributionPolicyValues[name]; !ok {
			return nil, fmt.Errorf("unknown redistribution policy %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("redistribution policy %q appears more than once", name)
		}
		seen[name] = true
		policies = append(policies, name)
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

// parseSharingJoins reads the -sharing-joins list. Each name must be a key
// of sharingJoinValues and can occur only once. The order of the list is
// the order of the arms in the report.
func parseSharingJoins(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	joins := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		join := strings.TrimSpace(part)
		if _, ok := sharingJoinValues[join]; !ok {
			return nil, fmt.Errorf("unknown sharing join policy %q", join)
		}
		if seen[join] {
			return nil, fmt.Errorf("sharing join policy %q appears more than once", join)
		}
		seen[join] = true
		joins = append(joins, join)
	}
	return joins, nil
}

// parsePlatoonPolicies reads the -platoon-policies list. Each name must be
// a key of platoonPolicyValues and can occur only once. The order of the
// list is the order of the arms in the report.
func parsePlatoonPolicies(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	policies := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		policy := strings.TrimSpace(part)
		if _, ok := platoonPolicyValues[policy]; !ok {
			return nil, fmt.Errorf("unknown platoon policy %q", policy)
		}
		if seen[policy] {
			return nil, fmt.Errorf("platoon policy %q appears more than once", policy)
		}
		seen[policy] = true
		policies = append(policies, policy)
	}
	return policies, nil
}

func parseRoutingPolicies(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	policies := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		policy := strings.TrimSpace(part)
		if _, ok := routingPolicyValues[policy]; !ok {
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

// parseSharingModes parses -sharing-modes.
func parseSharingModes(value string) ([]sim.SharedRideMode, error) {
	parts := strings.Split(value, ",")
	modes := make([]sim.SharedRideMode, 0, len(parts))
	for _, part := range parts {
		mode := sim.SharedRideMode(strings.TrimSpace(part))
		if mode != sim.SharedRideDestination && mode != sim.SharedRideDropOffs {
			return nil, fmt.Errorf("unknown sharing mode %q", mode)
		}
		if slices.Contains(modes, mode) {
			return nil, fmt.Errorf("sharing mode %q appears more than once", mode)
		}
		modes = append(modes, mode)
	}
	return modes, nil
}

// sharingArm is one combination of a party limit and a sharing mode.
type sharingArm struct {
	limit int
	mode  sim.SharedRideMode
}

// sharingArms returns the party limits and the sharing modes that compare
// runs. With a limit of 1, no party joins a pod, so each mode gives the same
// run. The limit then runs one time. See sharingSettings.
func sharingArms(opts options) []sharingArm {
	var arms []sharingArm
	for _, limit := range opts.sharingLimits {
		if limit == 1 {
			arms = append(arms, sharingArm{limit: 1, mode: sim.SharedRideDestination})
			continue
		}
		for _, mode := range opts.sharingModes {
			arms = append(arms, sharingArm{limit: limit, mode: mode})
		}
	}
	return arms
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
