// Command compare runs repeatable demand and policy experiments.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	randv2 "math/rand/v2"
	"os"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	maxDuration    = 24 * time.Hour
	maxSeeds       = 100
	maxLoads       = 20
	maxComparisons = 1000 // The cap does not count the redistribution policies.
	maxQueueLimit  = 1_000_000
	maxBurstSize   = 1_000
	maxWorkers     = 64
)

var syntheticPatterns = []string{"balanced", "destination", "hotspot", "bursty-hotspot", "hub-burst"}
var knownPatterns = append(append([]string(nil), syntheticPatterns...), "profile", "profile-daily", "rail-arrivals", "rail-services")

// waitRuleValues maps each -wait-rules name to its simulation rule.
var waitRuleValues = map[string]sim.FinishingPodWait{
	"current": sim.FinishingPodWaitCurrent,
	"strict":  sim.FinishingPodWaitStrict,
	"none":    sim.FinishingPodWaitNone,
}

// sharingJoinValues maps each -sharing-joins name to its shared ride join
// policy.
var sharingJoinValues = map[string]sim.SharedRideJoin{
	string(sim.SharedRideJoinUnassigned):       sim.SharedRideJoinUnassigned,
	string(sim.SharedRideJoinReassignExisting): sim.SharedRideJoinReassignExisting,
}

// platoonPolicyValues maps each -platoon-policies name to its platooning
// mode. The virtual policy links pods in queues with the simulation
// default platoon limit.
var platoonPolicyValues = map[string]sim.Platooning{
	"off":     sim.PlatooningOff,
	"virtual": sim.PlatooningVirtual,
}

// redistributionPolicyValues maps each -redistribution-policies name to
// its positioning mode. The on policy is guarded positioning, as for a
// project with redistribution.
var redistributionPolicyValues = map[string]sim.Positioning{
	"off": sim.PositioningOff,
	"on":  sim.PositioningGuarded,
}

// routingPolicyValues maps each -routing-policies name to its simulation
// policy.
var routingPolicyValues = map[string]sim.RoutingPolicy{
	"free-flow":  sim.FreeFlowRouting,
	"congestion": sim.CongestionRouting,
	"queue":      sim.QueueRouting,
	"predictive": sim.PredictiveRouting,
}

type options struct {
	energyPath              string
	duration, arrivalsFor   time.Duration
	requestEvery            time.Duration
	seed                    int64
	seedsText               string
	pattern                 string
	patternsText            string
	dailyStartMinute        int
	bandsText               string
	loadsText               string
	sharingConsentText      string
	sharingConsent          sim.SharingConsent
	sharingConsentColumn    bool
	sharingLimitsText       string
	sharingModesText        string
	routingPoliciesText     string
	redistributionText      string
	waitRulesText           string
	platoonPoliciesText     string
	sharingJoinsText        string
	onboardPickupsText      string
	stationBuffersText      string
	stationQueueSpacingText string
	pickupReassignmentText  string
	focus                   string
	format                  string
	projectPath             string
	outputPath              string
	queueLimit              int
	burstSize               int
	workers                 int
	seeds                   []int64
	patterns                []string
	loads                   []time.Duration
	sharingLimits           []int
	sharingModes            []sim.SharedRideMode
	sharingMaxStops         int
	routingPolicies         []string
	redistributionPolicies  []string
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
	sharingJoins        []string
	onboardPickups      []string
	stationBuffers      []string
	stationQueueSpacing []string
	pickupReassignment  []string
	stopWhenDrained     bool
	railForecast        bool
	// adaptiveLimit skips the rates of a group more than pastLimit rates
	// above the first rate at which a seed does not drain.
	adaptiveLimit bool
	pastLimit     int
}

type scheduledRequest struct {
	tick          int64
	origin        string
	destination   string
	event         string
	passenger     int
	kind          string
	departureTick int64
	walkingTicks  int64
}

type scenario struct {
	network        sim.Network
	fleet          []sim.Placement
	passengers     []string
	focus          string
	demand         project.DemandConfig
	demandProfiles []project.DemandProfile
	railArrivals   []project.RailArrival
	railDepartures []project.RailDeparture
}

type result struct {
	Energy                         *energyReport      `json:"energy,omitempty"`
	SharingConsent                 sim.SharingConsent `json:"sharing_consent"`
	Pattern                        string             `json:"pattern"`
	DemandProfile                  string             `json:"demand_profile,omitempty"`
	DemandBand                     string             `json:"demand_band,omitempty"`
	DailyStartMinute               *int               `json:"daily_start_minute,omitempty"`
	RequestEverySeconds            float64            `json:"request_every_seconds"`
	OfferedPerMinute               float64            `json:"offered_per_minute"`
	BurstSize                      int                `json:"burst_size"`
	Seed                           int64              `json:"seed"`
	Policy                         string             `json:"policy"`
	SharedRidePartyLimit           int                `json:"shared_ride_party_limit"`
	SharingMode                    string             `json:"sharing_mode"`
	SharingJoin                    string             `json:"sharing_join,omitempty"`
	OnboardPickups                 string             `json:"onboard_pickups,omitempty"`
	SharedParties                  int                `json:"shared_parties"`
	FullPodRefusals                int                `json:"full_pod_refusals"`
	FullDepartures                 int                `json:"full_departures"`
	DepartureBacklog               int                `json:"departure_backlog"`
	DeparturesDemandOverFour       int                `json:"departures_demand_over_four"`
	DeparturesOverFourAboard       int                `json:"departures_over_four_aboard"`
	JoinEligibleAssigned           int                `json:"join_eligible_assigned"`
	JoinEligibleExistingStop       int                `json:"join_eligible_existing_stop"`
	JoinEligibleAddedStopOnly      int                `json:"join_eligible_added_stop_only"`
	ReassignedParties              int                `json:"reassigned_parties"`
	RoutingPolicy                  string             `json:"routing_policy"`
	WaitRule                       string             `json:"wait_rule,omitempty"`
	PlatoonPolicy                  string             `json:"platoon_policy,omitempty"`
	StationBuffers                 string             `json:"station_buffers,omitempty"`
	StationQueueSpacing            string             `json:"station_queue_spacing,omitempty"`
	PickupReassignment             string             `json:"pickup_reassignment,omitempty"`
	PickupReassignmentStats        *pickupPolicyStats `json:"pickup_reassignment_stats,omitempty"`
	FocusStation                   string             `json:"focus_station"`
	WindowStartSeconds             float64            `json:"window_start_seconds"`
	WindowEndSeconds               float64            `json:"window_end_seconds"`
	ActualEndSeconds               float64            `json:"actual_end_seconds"`
	ArrivalWindowSeconds           float64            `json:"arrival_window_seconds"`
	ArrivalEndSeconds              float64            `json:"arrival_end_seconds"`
	ScheduleID                     string             `json:"schedule_id"`
	Scheduled                      int                `json:"scheduled"`
	Served                         int                `json:"served"`
	Remaining                      int                `json:"remaining"`
	Skipped                        int                `json:"skipped"`
	RailConnections                *connectionReport  `json:"rail_connections,omitempty"`
	RailSkippedOffers              []int              `json:"rail_skipped_offers,omitempty"`
	CompletedAtArrivalEnd          int                `json:"completed_at_arrival_end"`
	BacklogAtArrivalEnd            int                `json:"backlog_at_arrival_end"`
	ArrivalThroughputPerMinute     float64            `json:"arrival_throughput_per_minute"`
	CompletedAtArrivalMidpoint     int                `json:"completed_at_arrival_midpoint"`
	BacklogAtArrivalMidpoint       int                `json:"backlog_at_arrival_midpoint"`
	LateArrivalThroughputPerMinute float64            `json:"late_arrival_throughput_per_minute"`
	LateBacklogChange              int                `json:"late_backlog_change"`
	Drained                        bool               `json:"drained"`
	DrainSeconds                   float64            `json:"drain_seconds"`
	PeakPending                    int                `json:"peak_pending"`
	PeakOutstanding                int                `json:"peak_outstanding"`
	PeakActiveVehicles             int                `json:"peak_active_vehicles"`
	PeakPassengerVehicles          int                `json:"peak_passenger_vehicles"`
	PeakStoppedVehicles            int                `json:"peak_stopped_vehicles"`
	PeakFocusApproaching           int                `json:"peak_focus_approaching"`
	PeakFocusEntranceStopped       int                `json:"peak_focus_entrance_stopped"`
	PeakFocusExitStopped           int                `json:"peak_focus_exit_stopped"`
	PeakFocusOccupiedBerths        int                `json:"peak_focus_occupied_berths"`
	PeakFocusReservedEmptyBerths   int                `json:"peak_focus_reserved_empty_berths"`
	StoppedPodSeconds              float64            `json:"stopped_pod_seconds"`
	JunctionWaitSeconds            float64            `json:"junction_wait_seconds"`
	TrackWaitSeconds               float64            `json:"track_wait_seconds"`
	PeakNodeThroughputPerMinute    int                `json:"peak_node_throughput_per_minute"`
	PeakNode                       string             `json:"peak_node"`
	QueueCleared                   bool               `json:"queue_cleared"`
	QueueClearSeconds              float64            `json:"queue_clear_seconds"`
	WaitAverageSeconds             float64            `json:"wait_average_seconds"`
	WaitMaximumSeconds             float64            `json:"wait_maximum_seconds"`
	WaitP95Seconds                 float64            `json:"wait_p95_seconds"`
	JourneyAverageSeconds          float64            `json:"journey_average_seconds"`
	JourneyP95Seconds              float64            `json:"journey_p95_seconds"`
	JourneyMaximumSeconds          float64            `json:"journey_maximum_seconds"`
	PassengerDistanceMeters        float64            `json:"passenger_distance_meters"`
	EmptyDistanceMeters            float64            `json:"empty_distance_meters"`
	LoadedDistancePercent          float64            `json:"loaded_distance_percent"`
	Occupancy                      float64            `json:"occupancy"`
	RiderDistanceMeters            float64            `json:"rider_distance_meters"`
	DirectDistanceMeters           float64            `json:"direct_distance_meters"`
	DetourRatioMean                float64            `json:"detour_ratio_mean"`
	DetourRatioMax                 float64            `json:"detour_ratio_max"`
	IntermediateStops              int                `json:"intermediate_stops"`
	PositioningMoveCount           int                `json:"positioning_moves"`
	PlatoonTimePercent             float64            `json:"platoon_time_percent"`
}

type report struct {
	SchemaVersion int      `json:"schema_version"`
	Results       []result `json:"results"`
}

type cliInput struct {
	args           []string
	stdout, stderr io.Writer
}

// compareGCPercent is the GC percent of compare runs when GOGC is not
// set. A London arm has a live heap of about 60 MB, and GC at the default
// 100 uses about 16 percent of its CPU. At 400, an arm uses about 14
// percent less CPU and about 120 MB more memory. The reports do not
// change.
const compareGCPercent = 400

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(compareGCPercent)
	}
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
	if err := writeReport(writeReportInput{
		output: output, format: opts.format, results: results, sharingConsentColumn: opts.sharingConsentColumn,
		waitRuleColumn: opts.waitRules != nil, platoonColumn: opts.platoonPolicies != nil, sharingJoinColumn: opts.sharingJoins != nil,
		seatColumns:         slices.ContainsFunc(opts.sharingLimits, func(limit int) bool { return limit > 1 }),
		stationBufferColumn: opts.stationBuffers != nil, pickupReassignmentColumn: opts.pickupReassignment != nil,
		stationQueueSpacingColumn: opts.stationQueueSpacing != nil, onboardPickupsColumn: opts.onboardPickups != nil,
	}); err != nil {
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
	checkStationQueueOptions,
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
	flags.StringVar(&opts.stationBuffersText, "station-buffers", "off", "comma-separated experimental station buffer policies: off, on")
	flags.StringVar(&opts.stationQueueSpacingText, "station-queue-spacing", "ordinary", "comma-separated station queue spacing policies: ordinary, compact-v1 (compact-v1 requires station-buffers on and platoon-policies virtual)")
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

// checkStationQueueOptions checks the compact arms. See
// validateStationQueueOptions.
func checkStationQueueOptions(opts *options, _ map[string]bool) error {
	return validateStationQueueOptions(*opts)
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
		demand: config.Demand, demandProfiles: config.DemandProfiles, railArrivals: config.RailArrivals, railDepartures: config.RailDepartures,
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
		return project.Config{}, fmt.Errorf("read project %s: file exceeds %d MiB", path, project.MaxFileBytes>>20)
	}
	// Member names match exactly. The other encoding/json rules stay.
	decoder := jsontext.NewDecoder(io.LimitReader(file, project.MaxFileBytes), json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true))
	var config project.Config
	if err := jsonv2.UnmarshalDecode(decoder, &config); err != nil {
		return project.Config{}, fmt.Errorf("read project %s: %w", path, err)
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return project.Config{}, fmt.Errorf("read project %s: expected one JSON value", path)
	}
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate project %s: %w", path, err)
	}
	// The comparison has no incident outcome yet.
	if config.IncidentContract != "" {
		return project.Config{}, fmt.Errorf("read project %s: comparisons do not support incidentContract", path)
	}
	return config, nil
}

type demandArm struct {
	pattern, profile, band string
	flows                  []weightedDemandFlow
	daily                  *project.DailyProfile
	dailyStartMinute       int
}

type weightedDemandFlow struct {
	from, to   string
	cumulative float64
}

func compare(opts options, scenario scenario) ([]result, error) {
	var energy *energyModel
	if opts.energyPath != "" {
		var err error
		energy, err = readEnergyModel(opts.energyPath)
		if err != nil {
			return nil, err
		}
		if _, err := newEnergyMeter(energy, scenario.fleet, 0); err != nil {
			return nil, err
		}
	}
	if err := validateOnboardOptions(opts); err != nil {
		return nil, err
	}
	if err := validateStationQueueOptions(opts); err != nil {
		return nil, err
	}
	consent, err := comparisonConsent(opts.sharingConsent)
	if err != nil {
		return nil, err
	}
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
	sharing := sharingArms(opts)
	// An empty join policy selects the default policy and leaves the
	// result without a join policy. With a limit of 1, the arm keeps the
	// given policy, which has no effect.
	sharingJoins := opts.sharingJoins
	if sharingJoins == nil {
		sharingJoins = []string{""}
	}
	// An empty platoon policy runs without platoons and leaves the result
	// without a platoon policy.
	platoonPolicies := opts.platoonPolicies
	if platoonPolicies == nil {
		platoonPolicies = []string{""}
	}
	armsPerSchedule := len(sharing) * len(sharingJoins) * len(opts.routingPolicies) * len(waitRules) * len(platoonPolicies) * experimentalArmCount(opts) * onboardArmCount(opts)
	if len(opts.seeds)*len(arms)*len(opts.loads)*armsPerSchedule > maxComparisons {
		return nil, fmt.Errorf("the expanded matrix must contain at most %d comparisons", maxComparisons)
	}
	if err := validateRailMatrix(opts, arms, scenario, armsPerSchedule); err != nil {
		return nil, err
	}
	if err := validateDailyMatrix(opts, arms, armsPerSchedule); err != nil {
		return nil, err
	}
	inputs := make([]runInput, 0, len(arms)*len(opts.loads)*len(opts.seeds)*armsPerSchedule*2)
	for _, arm := range arms {
		for _, load := range opts.loads {
			if arm.daily != nil {
				load = 0
			}
			for _, seed := range opts.seeds {
				schedule := demandSchedule(scheduleInput{
					seed: seed, durationTicks: durationTicks(opts.arrivalsFor), intervalTicks: durationTicks(load), pattern: arm.pattern,
					burstSize:  opts.burstSize,
					passengers: scenario.passengers, focus: scenario.focus, profileFlows: arm.flows, daily: arm.daily, railArrivals: scenario.railArrivals, railDepartures: scenario.railDepartures,
				})
				id := scheduleID(schedule)
				for _, sharingArm := range sharing {
					for _, sharingJoin := range sharingJoins {
						for _, routingPolicy := range opts.routingPolicies {
							for _, waitRule := range waitRules {
								for _, platoonPolicy := range platoonPolicies {
									for _, policy := range opts.redistributionPolicies {
										inputs = append(inputs, runInput{
											energy: energy, railForecast: opts.railForecast, policy: policy, duration: opts.duration, requestEvery: load, seed: seed,
											pattern: arm.pattern, profile: arm.profile, band: arm.band, daily: arm.daily, dailyStartMinute: arm.dailyStartMinute,
											scheduleID: id, queueLimit: opts.queueLimit, arrivalsFor: opts.arrivalsFor,
											burstSize: opts.burstSize, sharingConsent: consent, sharingLimit: sharingArm.limit, sharingMode: sharingArm.mode,
											sharingMaxStops: opts.sharingMaxStops, sharingJoin: sharingJoin, routingPolicy: routingPolicy,
											waitRule: waitRule, platoonPolicy: platoonPolicy, schedule: schedule, scenario: scenario,
											stopWhenDrained: opts.stopWhenDrained,
										})
									}
								}
							}
						}
					}
				}
			}
		}
	}
	inputs = onboardInputs(experimentalInputs(inputs, opts), opts)
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
		if pattern == "profile-daily" {
			arm, err := dailyArm(opts, scenario)
			if err != nil {
				return nil, err
			}
			arms = append(arms, arm)
			continue
		}
		if pattern == "rail-arrivals" || pattern == "rail-services" {
			if err := project.ValidateDemand(project.DemandConfig{Pattern: pattern, PerMinute: 12}, project.DemandContext{Network: scenario.network, RailArrivals: scenario.railArrivals, RailDepartures: scenario.railDepartures}); err != nil {
				return nil, fmt.Errorf("rail demand: %w", err)
			}
		}
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
	daily                        *project.DailyProfile
	railArrivals                 []project.RailArrival
	railDepartures               []project.RailDeparture
}

func demandSchedule(input scheduleInput) []scheduledRequest {
	if input.pattern == "profile-daily" {
		return dailyDemandSchedule(input)
	}
	if input.pattern == "rail-services" {
		return railServiceSchedule(input)
	}
	if input.pattern == "rail-arrivals" || input.pattern == "rail-services" {
		return railDemandSchedule(input)
	}
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
		switch {
		case request.kind != "":
			_, _ = fmt.Fprintf(hash, "%s:%d:%q:%d:%q>%q:%d:%d\n", request.kind, request.tick, request.event, request.passenger, request.origin, request.destination, request.departureTick, request.walkingTicks)
		case request.event != "":
			_, _ = fmt.Fprintf(hash, "%d:%q:%d:%q>%q\n", request.tick, request.event, request.passenger, request.origin, request.destination)
		default:
			_, _ = fmt.Fprintf(hash, "%d:%s>%s\n", request.tick, request.origin, request.destination)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)[:8])
}

type runInput struct {
	energy *energyModel
	// policy names a redistributionPolicyValues key.
	policy                              string
	duration, arrivalsFor, requestEvery time.Duration
	seed                                int64
	pattern, profile, band, scheduleID  string
	queueLimit                          int
	burstSize                           int
	sharingLimit                        int
	sharingConsent                      sim.SharingConsent
	// sharingMode is the sharing mode. Empty selects
	// sim.DefaultSharedRideMode. sharingMaxStops is the stop limit of the drop-offs mode. Zero
	// selects the default limit.
	sharingMode     sim.SharedRideMode
	sharingMaxStops int
	// sharingJoin names a sharingJoinValues key. Empty selects the default
	// join policy.
	sharingJoin    string
	onboardPickups string
	routingPolicy  string
	// waitRule names a waitRuleValues key. Empty selects the default rule.
	waitRule string
	// platoonPolicy names a platoonPolicyValues key. Empty runs without
	// platoons.
	platoonPolicy       string
	stationBuffers      string
	stationQueueSpacing string
	pickupReassignment  string
	schedule            []scheduledRequest
	scenario            scenario
	stopWhenDrained     bool
	railForecast        bool
	daily               *project.DailyProfile
	dailyStartMinute    int
}

// sharingSettings returns the sharing mode and the stop limit of an arm,
// with the defaults for the zero values. With a limit of 1, no party joins
// a pod, so the mode has no effect. The arm then uses the destination mode,
// because the recorded rows of limit 1 use this mode name.
func (input *runInput) sharingSettings() (sim.SharedRideMode, int) {
	mode, maxStops := input.sharingMode, input.sharingMaxStops
	switch {
	case input.sharingLimit <= 1:
		mode = sim.SharedRideDestination
	case mode == "":
		mode = sim.DefaultSharedRideMode
	}
	if maxStops == 0 {
		maxStops = sim.DefaultSharedRideMaxStops
	}
	return mode, maxStops
}

func run(input runInput) (result, error) {
	meter, err := newEnergyMeter(input.energy, input.scenario.fleet, 0)
	if err != nil {
		return result{}, err
	}
	consent, err := comparisonConsent(input.sharingConsent)
	if err != nil {
		return result{}, err
	}
	simulation, err := sim.NewFleet(input.scenario.network, input.scenario.fleet)
	if err != nil {
		return result{}, fmt.Errorf("create comparison: %w", err)
	}
	mode, err := configureRun(simulation, &input, meter != nil)
	if err != nil {
		return result{}, err
	}
	metrics, err := newRunMetrics(input.scenario, input.schedule)
	if err != nil {
		return result{}, err
	}
	arm := newArmRun(simulation, &input, armSettings{mode: mode, consent: consent, meter: meter}, metrics)
	if err := arm.simulate(); err != nil {
		return result{}, err
	}
	return arm.result(), nil
}

// configureRun applies the policies of input to simulation in a fixed
// order and returns the redistribution mode. motion turns on the motion
// records that the energy meter reads.
func configureRun(simulation *sim.Simulation, input *runInput, motion bool) (sim.Positioning, error) {
	if err := configureDemandWeights(simulation, input); err != nil {
		return 0, err
	}
	if err := configureSharing(simulation, input); err != nil {
		return 0, err
	}
	if err := configureOnboardPickups(simulation, input.onboardPickups); err != nil {
		return 0, err
	}
	if err := simulation.SetRoutingPolicy(routingPolicyValues[input.routingPolicy]); err != nil {
		return 0, fmt.Errorf("set routing policy: %w", err)
	}
	simulation.SetExperimentRecords(true)
	if motion {
		simulation.SetMotionRecording(true)
	}
	if err := configureWaitAndPlatoon(simulation, input); err != nil {
		return 0, err
	}
	if err := configureExperimentalPolicies(simulation, *input); err != nil {
		return 0, err
	}
	mode, ok := redistributionPolicyValues[input.policy]
	if !ok {
		return 0, fmt.Errorf("unknown redistribution policy %q", input.policy)
	}
	if err := simulation.SetPositioning(mode); err != nil {
		return 0, fmt.Errorf("set redistribution policy: %w", err)
	}
	return mode, nil
}

// configureDemandWeights sets the demand weights of the pattern. A daily
// profile sets its own weights for each band, so it starts with none.
func configureDemandWeights(simulation *sim.Simulation, input *runInput) error {
	weightScenario := input.scenario
	weightScenario.demand.Seed = uint64(input.seed) // #nosec G115 -- Match the offered schedule.
	var weights map[string]float64
	if input.daily == nil {
		var err error
		weights, err = demandWeights(input.pattern, input.band, weightScenario)
		if err != nil {
			return err
		}
	}
	if err := simulation.SetDemandWeights(weights); err != nil {
		return fmt.Errorf("set demand weights: %w", err)
	}
	return nil
}

// configureSharing sets the party limit, the sharing mode, and the join
// policy, in this order.
func configureSharing(simulation *sim.Simulation, input *runInput) error {
	if err := simulation.SetSharedRidePartyLimit(input.sharingLimit); err != nil {
		return fmt.Errorf("set sharing limit: %w", err)
	}
	sharingMode, sharingStops := input.sharingSettings()
	if err := simulation.SetSharedRideMode(sharingMode, sharingStops); err != nil {
		return fmt.Errorf("set sharing mode: %w", err)
	}
	if input.sharingJoin == "" {
		return nil
	}
	join, ok := sharingJoinValues[input.sharingJoin]
	if !ok {
		return fmt.Errorf("unknown sharing join policy %q", input.sharingJoin)
	}
	if err := simulation.SetSharedRideJoin(join); err != nil {
		return fmt.Errorf("set sharing join policy: %w", err)
	}
	return nil
}

// configureWaitAndPlatoon sets the wait rule and then the platoon policy.
// An empty name keeps the simulation default.
func configureWaitAndPlatoon(simulation *sim.Simulation, input *runInput) error {
	if input.waitRule != "" {
		rule, ok := waitRuleValues[input.waitRule]
		if !ok {
			return fmt.Errorf("unknown wait rule %q", input.waitRule)
		}
		if err := simulation.SetFinishingPodWait(rule); err != nil {
			return fmt.Errorf("set wait rule: %w", err)
		}
	}
	if input.platoonPolicy == "" {
		return nil
	}
	platooning, ok := platoonPolicyValues[input.platoonPolicy]
	if !ok {
		return fmt.Errorf("unknown platoon policy %q", input.platoonPolicy)
	}
	if err := simulation.SetPlatooning(platooning); err != nil {
		return fmt.Errorf("set platoon policy: %w", err)
	}
	return nil
}

// isRailPattern reports whether pattern offers the requests of a rail plan.
func isRailPattern(pattern string) bool {
	return pattern == "rail-arrivals" || pattern == "rail-services"
}

// armSettings holds the values that run gives to an arm before its first
// tick.
type armSettings struct {
	mode    sim.Positioning
	consent sim.SharingConsent
	meter   *energyMeter
}

// armRun carries the state of one configured arm from its first tick to
// its result.
type armRun struct {
	armSettings
	simulation  *sim.Simulation
	input       *runInput
	metrics     runMetrics
	connections *rail.Connections
	// next is the index of the next schedule request. skipped counts the
	// requests that the arm did not submit, and railSkipped holds their
	// schedule indices for a rail pattern.
	next, skipped int
	railSkipped   []int
	// The arm keeps the snapshots at the arrival midpoint and at the end of
	// the arrival window.
	arrivalWindowTicks, arrivalMidpointTicks int64
	midpointState, arrivalState              sim.Snapshot
	// previousBand and previousDay identify the last daily band occurrence
	// that set the positioning.
	previousBand int
	previousDay  int64
}

func newArmRun(simulation *sim.Simulation, input *runInput, settings armSettings, metrics runMetrics) *armRun {
	arm := &armRun{armSettings: settings, simulation: simulation, input: input, metrics: metrics}
	if isRailPattern(input.pattern) {
		arm.railSkipped = make([]int, 0, len(input.schedule))
	}
	if input.pattern == "rail-services" && len(input.scenario.railDepartures) > 0 {
		arm.connections = rail.NewConnections(input.scenario.railDepartures)
	}
	arm.arrivalWindowTicks = durationTicks(input.arrivalsFor)
	arm.arrivalMidpointTicks = arm.arrivalWindowTicks / 2
	arm.midpointState = simulation.MetricsSnapshot()
	arm.arrivalState = simulation.MetricsSnapshot()
	arm.previousBand, arm.previousDay = -2, -2
	return arm
}

// simulate runs the ticks of the arm. It stops early when the arm stops
// when drained and has drained.
func (arm *armRun) simulate() error {
	for tick := range durationTicks(arm.input.duration) {
		stop, err := arm.step(tick)
		if err != nil || stop {
			return err
		}
	}
	return nil
}

// step runs one tick. A service run steps the simulation before it
// submits the requests of the new tick. Other runs submit the requests of
// tick and then step.
func (arm *armRun) step(tick int64) (bool, error) {
	if err := arm.followDailyBand(); err != nil {
		return false, err
	}
	service := arm.input.pattern == "rail-services" || arm.input.daily != nil
	if service {
		var err error
		if tick, err = arm.stepService(); err != nil {
			return false, err
		}
	}
	if err := arm.submitDue(tick); err != nil {
		return false, err
	}
	if !service {
		if err := stepComparison(arm.simulation); err != nil {
			return false, err
		}
	}
	if err := consumeEnergy(arm.meter, arm.simulation); err != nil {
		return false, err
	}
	advancedTick := arm.simulation.Tick()
	if err := arm.forecastRail(advancedTick); err != nil {
		return false, err
	}
	return arm.sample(advancedTick), nil
}

// followDailyBand sets the positioning of a daily profile run when the
// next tick starts a new band occurrence.
func (arm *armRun) followDailyBand() error {
	if arm.input.daily == nil {
		return nil
	}
	band, day := arm.input.daily.BandOccurrence(arm.simulation.Tick() + 1)
	if band == arm.previousBand && day == arm.previousDay {
		return nil
	}
	if err := configureDailyPositioning(arm.simulation, arm.input.daily, arm.simulation.Tick()+1, arm.mode, arm.skipped); err != nil {
		return fmt.Errorf("daily positioning: %w", err)
	}
	arm.previousBand, arm.previousDay = band, day
	return nil
}

// stepService steps a service run and returns its new tick.
func (arm *armRun) stepService() (int64, error) {
	if err := stepComparison(arm.simulation); err != nil {
		return 0, err
	}
	tick := arm.simulation.Tick()
	if arm.connections != nil {
		// Interruptions reach rail before Advance scores the
		// departures of the tick, as in the session.
		arm.connections.Interrupt(tick, arm.simulation.DrainInterruptions())
		arm.connections.Advance(tick, arm.simulation.StepCompletions())
	}
	return tick, nil
}

// submitDue offers each schedule request of tick. It skips a request when
// the pending queue is at the queue limit. When it submitted at least one
// request, it observes the metrics once, after the whole batch.
func (arm *armRun) submitDue(tick int64) error {
	schedule := arm.input.schedule
	injected := false
	for arm.next < len(schedule) && schedule[arm.next].tick == tick {
		request := schedule[arm.next]
		if arm.simulation.PendingCount() >= arm.input.queueLimit {
			if err := arm.skip(request); err != nil {
				return err
			}
		} else {
			if err := arm.submit(request); err != nil {
				return err
			}
			injected = true
		}
		arm.next++
	}
	if injected {
		arm.metrics.observe(arm.simulation.MetricsSnapshot())
	}
	return nil
}

// skip records a request that the queue limit refuses.
func (arm *armRun) skip(request scheduledRequest) error {
	// The guarded gate reads the rate of the accepted requests.
	// After a skipped arrival, that rate is lower than the
	// offered rate, and the gate can open above its limit. The
	// run is then over its queue limit, so the guarded policy
	// stops for the rest of the run.
	if arm.skipped == 0 && arm.mode == sim.PositioningGuarded {
		if err := arm.simulation.SetPositioning(sim.PositioningOff); err != nil {
			return fmt.Errorf("stop the guarded policy: %w", err)
		}
	}
	arm.skipped++
	if isRailPattern(arm.input.pattern) {
		arm.railSkipped = append(arm.railSkipped, arm.next)
	}
	if arm.connections != nil && request.kind == "departure" {
		return arm.connections.Add(request.serviceOffer(), 0, "queue-limit")
	}
	return nil
}

// submit submits a request. A departure offer of a service run records a
// refused request as skipped, and gives the result to rail.
func (arm *armRun) submit(request scheduledRequest) error {
	options := sim.TripOptions{From: request.origin, To: request.destination, SharingConsent: arm.consent}
	if arm.connections == nil || request.kind != "departure" {
		if _, err := arm.simulation.SubmitTripOptions(options); err != nil {
			return fmt.Errorf("request %s to %s: %w", request.origin, request.destination, err)
		}
		return nil
	}
	id, err := arm.simulation.SubmitTripOptions(options)
	reason := ""
	if err != nil {
		reason = "request-error"
		arm.skipped++
		arm.railSkipped = append(arm.railSkipped, arm.next)
	}
	return arm.connections.Add(request.serviceOffer(), id, reason)
}

// forecastRail positions pods for the future rail offers every five
// seconds when the arm uses the rail forecast.
func (arm *armRun) forecastRail(advancedTick int64) error {
	if !arm.input.railForecast || advancedTick%(5*sim.TicksPerSecond) != 0 {
		return nil
	}
	if _, err := arm.simulation.PositionForForecast(futureRailTargets(arm.input.schedule, arm.next, arm.connections, advancedTick)); err != nil {
		return fmt.Errorf("rail forecast: %w", err)
	}
	return nil
}

// sample observes the metrics each second and at the arrival midpoint and
// end. It reports whether the arm stops because it has drained.
func (arm *armRun) sample(advancedTick int64) bool {
	if advancedTick%sim.TicksPerSecond != 0 && advancedTick != arm.arrivalMidpointTicks && advancedTick != arm.arrivalWindowTicks {
		return false
	}
	state := arm.simulation.MetricsSnapshot()
	if state.Tick == arm.arrivalMidpointTicks {
		arm.midpointState = state
	}
	if state.Tick == arm.arrivalWindowTicks {
		arm.arrivalState = state
	}
	arm.metrics.observe(state)
	if advancedTick%sim.TicksPerSecond == 0 {
		arm.metrics.waits.sampleWaits(state.Vehicles)
		arm.metrics.platoon.sample(state.Vehicles, arm.simulation.LinkedPods())
	}
	return arm.input.stopWhenDrained && arm.drainedAfterArrivals(state)
}

// drainedAfterArrivals reports whether the arrival window is over, every
// request was offered and completed, and no rail connection can still
// become due.
func (arm *armRun) drainedAfterArrivals(state sim.Snapshot) bool {
	return state.Tick >= arm.arrivalWindowTicks && arm.next == len(arm.input.schedule) && state.Completed == state.Submitted &&
		!connectionPendingWithin(arm.connections, arm.input.scenario.railDepartures, durationTicks(arm.input.duration))
}

// result gives the report row of the arm after its last tick.
func (arm *armRun) result() result {
	input, simulation, metrics := arm.input, arm.simulation, &arm.metrics
	state := simulation.MetricsSnapshot()
	arrivalState, midpointState := arm.arrivalState, arm.midpointState
	if arrivalState.Tick != arm.arrivalWindowTicks {
		arrivalState = state
	}
	if midpointState.Tick != arm.arrivalMidpointTicks {
		midpointState = arrivalState
	}
	metrics.observe(state)
	requests := requestTimeStats(simulation.RequestTimings(), state)
	seats := seatScreenStats(simulation.SeatScreen())
	peakNodePasses, peakNode := peakNodeFlow(simulation.NodePasses())
	burstSize := 1
	if isBurstPattern(input.pattern) {
		burstSize = input.burstSize
	}
	requestEvery := input.requestEvery.Seconds()
	if isRailPattern(input.pattern) || input.daily != nil {
		burstSize, requestEvery = 0, 0
	}
	arrivalEnd := 0.0
	if len(input.schedule) > 0 {
		arrivalEnd = float64(input.schedule[len(input.schedule)-1].tick) / sim.TicksPerSecond
	}
	drained := arm.next == len(input.schedule) && state.Completed == state.Submitted
	drainSeconds := 0.0
	if drained && state.Tick > arm.arrivalWindowTicks {
		drainSeconds = float64(state.Tick-arm.arrivalWindowTicks) / sim.TicksPerSecond
	}
	arrivalMinutes := input.arrivalsFor.Minutes()
	lateArrivalMinutes := float64(arm.arrivalWindowTicks-arm.arrivalMidpointTicks) / sim.TicksPerSecond / 60
	midpointBacklog := midpointState.Submitted - midpointState.Completed
	arrivalBacklog := arrivalState.Submitted - arrivalState.Completed
	var dailyStart *int
	if input.daily != nil {
		dailyStart = new(input.dailyStartMinute)
	}
	sharingMode, _ := input.sharingSettings()
	return result{
		Energy:         arm.meter.snapshot(),
		SharingConsent: arm.consent, DailyStartMinute: dailyStart, OnboardPickups: input.onboardPickups,
		Pattern: input.pattern, DemandProfile: input.profile, DemandBand: input.band,
		RequestEverySeconds: requestEvery, OfferedPerMinute: float64(len(input.schedule)) / arrivalMinutes, Seed: input.seed,
		BurstSize: burstSize, Policy: input.policy, SharedRidePartyLimit: input.sharingLimit, SharingMode: string(sharingMode),
		SharingJoin:   input.sharingJoin,
		SharedParties: state.SharedParties, FullPodRefusals: seats.FullPodRefusals, FullDepartures: seats.FullDepartures,
		DepartureBacklog: seats.DepartureBacklog, DeparturesDemandOverFour: seats.demandOverFour, DeparturesOverFourAboard: seats.overFourAboard,
		JoinEligibleAssigned: seats.JoinEligibleAssigned, JoinEligibleExistingStop: seats.JoinEligibleExistingStop, JoinEligibleAddedStopOnly: seats.addedStopOnly,
		ReassignedParties: seats.ReassignedParties,
		RoutingPolicy:     input.routingPolicy, WaitRule: input.waitRule, PlatoonPolicy: input.platoonPolicy,
		StationBuffers: input.stationBuffers, StationQueueSpacing: input.stationQueueSpacing, PickupReassignment: input.pickupReassignment,
		PickupReassignmentStats: pickupStatsForReport(simulation, input.pickupReassignment),
		FocusStation:            input.scenario.focus,
		WindowStartSeconds:      0, WindowEndSeconds: input.duration.Seconds(), ActualEndSeconds: float64(state.Tick) / sim.TicksPerSecond,
		ArrivalWindowSeconds: input.arrivalsFor.Seconds(), ArrivalEndSeconds: arrivalEnd, ScheduleID: input.scheduleID,
		Scheduled: len(input.schedule), Served: state.Completed, Remaining: state.Submitted - state.Completed, Skipped: arm.skipped, RailSkippedOffers: arm.railSkipped, RailConnections: reportConnections(arm.connections),
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
		StoppedPodSeconds: float64(metrics.waits.stopped), JunctionWaitSeconds: float64(metrics.waits.junction), TrackWaitSeconds: float64(metrics.waits.track),
		PeakNodeThroughputPerMinute: peakNodePasses, PeakNode: peakNode,
		QueueCleared: metrics.queueCleared, QueueClearSeconds: metrics.queueClearSeconds,
		WaitAverageSeconds: state.Wait.AverageSeconds, WaitMaximumSeconds: state.Wait.MaxSeconds,
		WaitP95Seconds: requests.waitP95, JourneyAverageSeconds: requests.journeyAverage,
		JourneyP95Seconds: requests.journeyP95, JourneyMaximumSeconds: requests.journeyMaximum,
		PassengerDistanceMeters: state.PassengerDistanceMeters, EmptyDistanceMeters: state.EmptyDistanceMeters,
		LoadedDistancePercent: loadedDistancePercent(state.PassengerDistanceMeters, state.EmptyDistanceMeters),
		Occupancy:             requests.occupancy,
		RiderDistanceMeters:   state.RiderDistanceMeters, DirectDistanceMeters: state.DirectDistanceMeters,
		DetourRatioMean: requests.detourMean, DetourRatioMax: state.MaxDetourRatio, IntermediateStops: requests.intermediateStops,
		PositioningMoveCount: state.RebalanceMoves,
		PlatoonTimePercent:   metrics.platoon.percent(),
	}
}

// screenSeats is the seat count that the seat screen columns compare with.
// The screen asks whether demand exceeds the four seats of the London
// sharing measurements.
const screenSeats = 4

// seatStats holds the seat screen counters of one arm, the departures
// above screenSeats, and the join census parties that need a new stop.
type seatStats struct {
	sim.SeatScreen
	// demandOverFour counts the departures with more than screenSeats
	// parties aboard plus backlog, and overFourAboard the departures with
	// more than screenSeats parties aboard.
	demandOverFour, overFourAboard int
	// addedStopOnly counts the parties of JoinEligibleAssigned that are
	// not in JoinEligibleExistingStop. A boarding pod could take each of
	// them only with a new stop.
	addedStopOnly int
}

// seatScreenStats gives the seat screen columns of an arm.
func seatScreenStats(screen sim.SeatScreen) seatStats {
	stats := seatStats{SeatScreen: screen, addedStopOnly: screen.JoinEligibleAssigned - screen.JoinEligibleExistingStop}
	for parties := screenSeats + 1; parties < len(screen.Demand); parties++ {
		stats.demandOverFour += screen.Demand[parties]
		stats.overFourAboard += screen.Aboard[parties]
	}
	return stats
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
	if pattern == "rail-arrivals" {
		for _, arrival := range scenario.railArrivals {
			weights[arrival.Station] += float64(arrival.Passengers)
		}
		return weights, nil
	}
	if pattern == "rail-services" {
		for _, offer := range project.RailServicesSchedule(scenario.railArrivals, scenario.railDepartures, scenario.demand.Seed) {
			weights[offer.From]++
		}
		return weights, nil
	}
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
	sharingConsentColumn bool
	output               io.Writer
	format               string
	results              []result
	// waitRuleColumn adds the wait rule to table and CSV output. JSON output
	// has the wait_rule field only when a result has a wait rule.
	waitRuleColumn bool
	// platoonColumn adds the platoon policy to table and CSV output. JSON
	// output has the platoon_policy field only when a result has a platoon
	// policy.
	platoonColumn bool
	// sharingJoinColumn adds the join policy to table and CSV output. JSON
	// output has the sharing_join field only when a result has a join
	// policy.
	sharingJoinColumn    bool
	onboardPickupsColumn bool
	// seatColumns adds the seat screen columns to CSV output. JSON output
	// always has them.
	seatColumns               bool
	stationBufferColumn       bool
	pickupReassignmentColumn  bool
	stationQueueSpacingColumn bool
}

func writeReport(input writeReportInput) error {
	input.results = slices.Clone(input.results)
	for i := range input.results {
		consent, err := comparisonConsent(input.results[i].SharingConsent)
		if err != nil {
			return err
		}
		input.results[i].SharingConsent = consent
	}
	switch input.format {
	case "json":
		encoder := json.NewEncoder(input.output)
		encoder.SetIndent("", "  ")
		version := 12
		if slices.ContainsFunc(input.results, func(outcome result) bool {
			return outcome.StationBuffers != "" || outcome.PickupReassignment != "" || outcome.StationQueueSpacing != ""
		}) {
			version = 13
		}
		if slices.ContainsFunc(input.results, func(outcome result) bool { return outcome.OnboardPickups != "" }) {
			version = 14
		}
		if slices.ContainsFunc(input.results, func(outcome result) bool { return outcome.Energy != nil }) {
			version = 15
		}
		if err := encoder.Encode(report{SchemaVersion: version, Results: input.results}); err != nil {
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
// input.waitRuleColumn is set, a WAIT RULE column follows POLICY. When
// input.platoonColumn is set, a PLATOON column follows them. When
// input.sharingJoinColumn is set, a JOIN column follows them.
func writeTable(input writeReportInput) error {
	output, results := input.output, input.results
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "window: %.2fs to %.2fs / arrivals end: %.2fs / focus: %s\n", results[0].WindowStartSeconds, results[0].WindowEndSeconds, results[0].ArrivalEndSeconds, results[0].FocusStation); err != nil {
		return fmt.Errorf("write table window: %w", err)
	}
	for _, outcome := range results {
		if outcome.DailyStartMinute != nil {
			if _, err := fmt.Fprintf(output, "daily profile: %s / start minute: %d / seed: %d / policy: %s\n", outcome.DemandProfile, *outcome.DailyStartMinute, outcome.Seed, outcome.Policy); err != nil {
				return fmt.Errorf("write daily clock: %w", err)
			}
		}
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	policyHeader := "POLICY"
	if input.sharingConsentColumn {
		policyHeader += "\tSHARING CONSENT"
	}
	if input.waitRuleColumn {
		policyHeader += "\tWAIT RULE"
	}
	if input.platoonColumn {
		policyHeader += "\tPLATOON"
	}
	if input.sharingJoinColumn {
		policyHeader += "\tJOIN"
	}
	if input.onboardPickupsColumn {
		policyHeader += "\tONBOARD PICKUPS"
	}
	if input.stationBufferColumn {
		policyHeader += "\tBUFFERS"
	}
	if input.stationQueueSpacingColumn {
		policyHeader += "\tQUEUE SPACING"
	}
	if input.pickupReassignmentColumn {
		policyHeader += "\tREASSIGN"
	}
	if _, err := fmt.Fprintln(w, "PATTERN\tBAND\tLOAD (S)\tOFFERED/M\tARRIVAL/M\tLATE/M\tBACKLOG\tLATE DELTA\tDRAIN (S)\tSEED\t"+policyHeader+"\tWAIT AVG\tWAIT MAX\tWAIT P95\tJOURNEY AVG\tJOURNEY P95\tSERVED\tLEFT\tSKIPPED\tPEAK OUT\tPEAK ACTIVE\tPEAK PAX\tPEAK STOPPED\tHUB IN\tHUB OUT\tHUB OCC\tHUB RSV\tPASSENGER (M)\tEMPTY (M)\tLOADED %\tMOVES"); err != nil {
		return fmt.Errorf("write table header: %w", err)
	}
	for _, outcome := range results {
		policy := outcome.Policy
		if input.sharingConsentColumn {
			policy += "\t" + string(outcome.SharingConsent)
		}
		if input.waitRuleColumn {
			policy += "\t" + outcome.WaitRule
		}
		if input.platoonColumn {
			policy += "\t" + outcome.PlatoonPolicy
		}
		if input.sharingJoinColumn {
			policy += "\t" + outcome.SharingJoin
		}
		if input.onboardPickupsColumn {
			policy += "\t" + outcome.OnboardPickups
		}
		if input.stationBufferColumn {
			policy += "\t" + outcome.StationBuffers
		}
		if input.stationQueueSpacingColumn {
			policy += "\t" + outcome.StationQueueSpacing
		}
		if input.pickupReassignmentColumn {
			policy += "\t" + outcome.PickupReassignment
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%d\t%d\t%s\t%d\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%.1f\t%.1f\t%.2f\t%d\n",
			outcome.Pattern, outcome.DemandBand, outcome.RequestEverySeconds, outcome.OfferedPerMinute,
			outcome.ArrivalThroughputPerMinute, outcome.LateArrivalThroughputPerMinute,
			outcome.BacklogAtArrivalEnd, outcome.LateBacklogChange, drainText(outcome), outcome.Seed, policy,
			outcome.WaitAverageSeconds, outcome.WaitMaximumSeconds, outcome.WaitP95Seconds,
			outcome.JourneyAverageSeconds, outcome.JourneyP95Seconds, outcome.Served, outcome.Remaining, outcome.Skipped,
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
	return writeTableEnergy(output, results)
}

// writeCSV writes a header and one row for each result. When
// input.waitRuleColumn is set, a wait_rule column follows routing_policy.
// When input.platoonColumn is set, a platoon_policy column follows them.
// When input.sharingJoinColumn is set, a sharing_join column follows
// sharing_mode. When input.seatColumns is set, the seat screen columns
// follow shared_parties. Without these options, the columns are the same
// as before them.
func writeCSV(input writeReportInput) error {
	w := csv.NewWriter(input.output)
	energyColumn := slices.ContainsFunc(input.results, func(r result) bool { return r.Energy != nil })
	dailyColumn := slices.ContainsFunc(input.results, func(r result) bool { return r.DailyStartMinute != nil })
	header := []string{
		"pattern", "demand_profile", "demand_band", "request_every_seconds", "offered_per_minute", "burst_size", "seed", "policy", "routing_policy",
	}
	if input.sharingConsentColumn {
		header = append(header, "sharing_consent")
	}
	if input.waitRuleColumn {
		header = append(header, "wait_rule")
	}
	if input.platoonColumn {
		header = append(header, "platoon_policy")
	}
	if input.stationBufferColumn {
		header = append(header, "station_buffers")
	}
	if input.stationQueueSpacingColumn {
		header = append(header, "station_queue_spacing")
	}
	if input.pickupReassignmentColumn {
		header = append(header, "pickup_reassignment")
	}
	header = append(header, "shared_ride_party_limit", "sharing_mode")
	if input.sharingJoinColumn {
		header = append(header, "sharing_join")
	}
	if input.onboardPickupsColumn {
		header = append(header, "onboard_pickups")
	}
	header = append(header, "shared_parties")
	if input.seatColumns {
		header = append(header, "full_pod_refusals", "full_departures", "departure_backlog", "departures_demand_over_four", "departures_over_four_aboard",
			"join_eligible_assigned", "join_eligible_existing_stop", "join_eligible_added_stop_only", "reassigned_parties")
	}
	header = append(header,
		"focus_station", "window_start_seconds", "window_end_seconds", "actual_end_seconds", "arrival_window_seconds", "arrival_end_seconds", "schedule_id",
		"scheduled", "served", "remaining", "skipped", "completed_at_arrival_end", "backlog_at_arrival_end", "arrival_throughput_per_minute",
		"completed_at_arrival_midpoint", "backlog_at_arrival_midpoint", "late_arrival_throughput_per_minute", "late_backlog_change", "drained", "drain_seconds",
		"peak_pending", "peak_outstanding", "peak_active_vehicles", "peak_passenger_vehicles", "peak_stopped_vehicles", "peak_focus_approaching", "peak_focus_entrance_stopped", "peak_focus_exit_stopped",
		"peak_focus_occupied_berths", "peak_focus_reserved_empty_berths", "stopped_pod_seconds", "junction_wait_seconds", "track_wait_seconds",
		"peak_node_throughput_per_minute", "peak_node",
		"queue_cleared", "queue_clear_seconds",
		"wait_average_seconds", "wait_maximum_seconds", "wait_p95_seconds",
		"journey_average_seconds", "journey_p95_seconds", "journey_maximum_seconds", "passenger_distance_meters", "empty_distance_meters", "loaded_distance_percent", "occupancy",
		"rider_distance_meters", "direct_distance_meters", "detour_ratio_mean", "detour_ratio_max", "intermediate_stops", "positioning_moves",
		"platoon_time_percent",
	)
	if dailyColumn {
		header = append(header, "daily_start_minute")
	}
	if energyColumn {
		header = append(header, energyCSVHeader...)
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, outcome := range input.results {
		row := []string{
			outcome.Pattern, outcome.DemandProfile, outcome.DemandBand, floatText(outcome.RequestEverySeconds), floatText(outcome.OfferedPerMinute), strconv.Itoa(outcome.BurstSize), strconv.FormatInt(outcome.Seed, 10), outcome.Policy, outcome.RoutingPolicy,
		}
		if input.sharingConsentColumn {
			row = append(row, string(outcome.SharingConsent))
		}
		if input.waitRuleColumn {
			row = append(row, outcome.WaitRule)
		}
		if input.platoonColumn {
			row = append(row, outcome.PlatoonPolicy)
		}
		if input.stationBufferColumn {
			row = append(row, outcome.StationBuffers)
		}
		if input.stationQueueSpacingColumn {
			row = append(row, outcome.StationQueueSpacing)
		}
		if input.pickupReassignmentColumn {
			row = append(row, outcome.PickupReassignment)
		}
		row = append(row, strconv.Itoa(outcome.SharedRidePartyLimit), outcome.SharingMode)
		if input.sharingJoinColumn {
			row = append(row, outcome.SharingJoin)
		}
		if input.onboardPickupsColumn {
			row = append(row, outcome.OnboardPickups)
		}
		row = append(row, strconv.Itoa(outcome.SharedParties))
		if input.seatColumns {
			row = append(row, strconv.Itoa(outcome.FullPodRefusals), strconv.Itoa(outcome.FullDepartures), strconv.Itoa(outcome.DepartureBacklog),
				strconv.Itoa(outcome.DeparturesDemandOverFour), strconv.Itoa(outcome.DeparturesOverFourAboard),
				strconv.Itoa(outcome.JoinEligibleAssigned), strconv.Itoa(outcome.JoinEligibleExistingStop), strconv.Itoa(outcome.JoinEligibleAddedStopOnly),
				strconv.Itoa(outcome.ReassignedParties))
		}
		row = append(row,
			outcome.FocusStation,
			floatText(outcome.WindowStartSeconds), floatText(outcome.WindowEndSeconds), floatText(outcome.ActualEndSeconds), floatText(outcome.ArrivalWindowSeconds), floatText(outcome.ArrivalEndSeconds), outcome.ScheduleID,
			strconv.Itoa(outcome.Scheduled), strconv.Itoa(outcome.Served), strconv.Itoa(outcome.Remaining), strconv.Itoa(outcome.Skipped),
			strconv.Itoa(outcome.CompletedAtArrivalEnd), strconv.Itoa(outcome.BacklogAtArrivalEnd), floatText(outcome.ArrivalThroughputPerMinute),
			strconv.Itoa(outcome.CompletedAtArrivalMidpoint), strconv.Itoa(outcome.BacklogAtArrivalMidpoint), floatText(outcome.LateArrivalThroughputPerMinute), strconv.Itoa(outcome.LateBacklogChange),
			strconv.FormatBool(outcome.Drained), floatText(outcome.DrainSeconds),
			strconv.Itoa(outcome.PeakPending), strconv.Itoa(outcome.PeakOutstanding), strconv.Itoa(outcome.PeakActiveVehicles), strconv.Itoa(outcome.PeakPassengerVehicles), strconv.Itoa(outcome.PeakStoppedVehicles),
			strconv.Itoa(outcome.PeakFocusApproaching), strconv.Itoa(outcome.PeakFocusEntranceStopped), strconv.Itoa(outcome.PeakFocusExitStopped),
			strconv.Itoa(outcome.PeakFocusOccupiedBerths), strconv.Itoa(outcome.PeakFocusReservedEmptyBerths),
			floatText(outcome.StoppedPodSeconds), floatText(outcome.JunctionWaitSeconds), floatText(outcome.TrackWaitSeconds),
			strconv.Itoa(outcome.PeakNodeThroughputPerMinute), outcome.PeakNode,
			strconv.FormatBool(outcome.QueueCleared), floatText(outcome.QueueClearSeconds),
			floatText(outcome.WaitAverageSeconds), floatText(outcome.WaitMaximumSeconds), floatText(outcome.WaitP95Seconds),
			floatText(outcome.JourneyAverageSeconds), floatText(outcome.JourneyP95Seconds), floatText(outcome.JourneyMaximumSeconds),
			floatText(outcome.PassengerDistanceMeters),
			floatText(outcome.EmptyDistanceMeters), floatText(outcome.LoadedDistancePercent), floatText(outcome.Occupancy),
			floatText(outcome.RiderDistanceMeters), floatText(outcome.DirectDistanceMeters),
			floatText(outcome.DetourRatioMean), floatText(outcome.DetourRatioMax), strconv.Itoa(outcome.IntermediateStops),
			strconv.Itoa(outcome.PositioningMoveCount), floatText(outcome.PlatoonTimePercent),
		)
		if dailyColumn {
			value := ""
			if outcome.DailyStartMinute != nil {
				value = strconv.Itoa(*outcome.DailyStartMinute)
			}
			row = append(row, value)
		}
		if energyColumn {
			values, err := energyCSVRow(outcome.Energy)
			if err != nil {
				return err
			}
			row = append(row, values...)
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
	// waits holds the stopped pod-seconds, and platoon holds the
	// traveling and linked pod-seconds. run samples them once per
	// simulated second.
	waits   trafficWaits
	platoon platoonTime
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
	metrics.peakOutstanding = max(metrics.peakOutstanding, state.Submitted-state.Completed)
	active, passenger, stopped := state.WorkingVehicles(), 0, 0
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
