package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

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
	platoonPolicy      string
	pickupReassignment string
	schedule           []scheduledRequest
	scenario           scenario
	stopWhenDrained    bool
	railForecast       bool
	daily              *project.DailyProfile
	dailyStartMinute   int
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
		tick = arm.stepService()
	}
	if err := arm.submitDue(tick); err != nil {
		return false, err
	}
	if !service {
		arm.simulation.Step()
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
func (arm *armRun) stepService() int64 {
	arm.simulation.Step()
	tick := arm.simulation.Tick()
	if arm.connections != nil {
		// Interruptions reach rail before Advance scores the
		// departures of the tick, as in the session.
		arm.connections.Interrupt(tick, arm.simulation.DrainInterruptions())
		arm.connections.Advance(tick, arm.simulation.StepCompletions())
	}
	return tick
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
		PickupReassignment:      input.pickupReassignment,
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
