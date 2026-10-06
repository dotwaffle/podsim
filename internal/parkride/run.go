package parkride

import (
	"context"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// RunInput supplies authored inputs and bounded offline controls.
type RunInput struct {
	Project      project.Config
	Plan         Plan
	HorizonTicks int64
	QueueLimit   int
	Build        string
	// Continuation enables version-1 receipts and replay evidence from initialization.
	Continuation *Implementation
}

// Policies identifies the effective native policies of this run.
type Policies struct {
	PartyLimit          int                     `json:"partyLimit"`
	SharingMode         sim.SharedRideMode      `json:"sharingMode"`
	MaxStops            int                     `json:"maxStops"`
	SharingJoin         sim.SharedRideJoin      `json:"sharingJoin"`
	OnboardPickups      bool                    `json:"onboardPickups"`
	PlatoonLimit        int                     `json:"platoonLimit"`
	StationBuffers      bool                    `json:"stationBuffers"`
	StationQueueSpacing sim.StationQueueSpacing `json:"stationQueueSpacing"`
	PickupReassignment  bool                    `json:"pickupReassignment"`
	Positioning         sim.Positioning         `json:"positioning"`
	PositioningWeights  string                  `json:"positioningWeights"`
}

// PodMetrics keeps native measurements separate from car outcomes.
type PodMetrics struct {
	Submitted               int     `json:"submitted"`
	Completed               int     `json:"completed"`
	Pending                 int     `json:"pending"`
	PassengerDistanceMeters float64 `json:"passengerDistanceMeters"`
	EmptyDistanceMeters     float64 `json:"emptyDistanceMeters"`
	RiderDistanceMeters     float64 `json:"riderDistanceMeters"`
	DirectDistanceMeters    float64 `json:"directDistanceMeters"`
	SharedParties           int     `json:"sharedParties"`
}

// Report owns a snapshot of ledger results and source provenance.
type Report struct {
	Version      int        `json:"version"`
	ProjectHash  string     `json:"projectHash"`
	PlanHash     string     `json:"planHash"`
	Build        string     `json:"build"`
	HorizonTicks int64      `json:"horizonTicks"`
	EndpointTick int64      `json:"endpointTick"`
	QueueLimit   int        `json:"queueLimit"`
	Policies     Policies   `json:"policies"`
	Error        string     `json:"error,omitempty"`
	Itineraries  []Record   `json:"itineraries"`
	Lots         []LotState `json:"lots"`
	Pods         PodMetrics `json:"pods"`
}

// Run owns a simulation and car ledger. Use it from one goroutine at a time.
// Clone copies both states. Native saved-state restoration cannot restore cars.
type Run struct {
	pods         *sim.Simulation
	ledger       *ledger
	provenance   Report
	fault        string
	continuation *continuation
}

// NewRun validates and owns inputs before issuing any itinerary order.
// A runtime error can return a partial run for its report.
func NewRun(input RunInput) (*Run, error) {
	return NewRunContext(context.Background(), input)
}

// NewRunContext constructs a run and permits cancellation of checkpoint capture.
func NewRunContext(ctx context.Context, input RunInput) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateFoundationConfig(input.Project); err != nil {
		return nil, err
	}
	if input.Continuation != nil {
		if err := validateImplementation(*input.Continuation); err != nil {
			return nil, err
		}
		if !foundationProject(input.Project) {
			return nil, errFoundationProject
		}
	}
	if input.HorizonTicks < 1 || input.HorizonTicks > MaxHorizonTicks {
		return nil, errors.New("horizon must be 1 tick through 24 hours")
	}
	if input.QueueLimit < 1 || input.QueueLimit > MaxQueueLimit {
		return nil, errors.New("queue limit must be 1 to 1000000")
	}
	if err := project.Validate(input.Project); err != nil {
		return nil, fmt.Errorf("project: %w", err)
	}
	config := project.Clone(input.Project)
	plan, err := normalizePlan(input.Plan, config.Network)
	if err != nil {
		return nil, err
	}
	pods, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		return nil, err
	}
	if sharingErr := project.ConfigureSharedRides(pods, config); sharingErr != nil {
		return nil, sharingErr
	}
	if platoonErr := project.ConfigurePlatoons(pods, config); platoonErr != nil {
		return nil, platoonErr
	}
	if experimentErr := project.ConfigureExperiments(pods, config); experimentErr != nil {
		return nil, experimentErr
	}
	weights := make(map[string]float64)
	lots := make(map[string]Lot)
	for _, lot := range plan.Lots {
		lots[lot.ID] = lot
	}
	for _, itinerary := range plan.Itineraries {
		weights[lots[itinerary.Lot].Hub]++
		weights[itinerary.Destination]++
	}
	if weightErr := pods.SetDemandWeights(weights); weightErr != nil {
		return nil, weightErr
	}
	mode := sim.PositioningOff
	if config.Redistribution {
		mode = sim.PositioningGuarded
	}
	if positioningErr := pods.SetPositioning(mode); positioningErr != nil {
		return nil, positioningErr
	}
	// No generated stream sets a rate. Guarded positioning uses its observed-rate fallback.
	if rateErr := pods.SetDemandRate(0); rateErr != nil {
		return nil, rateErr
	}
	if preflightErr := preflight(pods, plan, lots); preflightErr != nil {
		return nil, preflightErr
	}
	projectHash, err := canonicalHash(config)
	if err != nil {
		return nil, err
	}
	planHash, err := canonicalHash(plan)
	if err != nil {
		return nil, err
	}
	run := &Run{pods: pods, ledger: newLedger(plan, input.QueueLimit, input.HorizonTicks), provenance: Report{Version: 1,
		ProjectHash: projectHash, PlanHash: planHash, Build: input.Build, HorizonTicks: input.HorizonTicks, QueueLimit: input.QueueLimit,
		Policies: Policies{PartyLimit: project.EffectiveSharedRidePartyLimit(config), SharingMode: project.EffectiveSharedRideMode(config),
			MaxStops: project.EffectiveSharedRideMaxStops(config), SharingJoin: project.EffectiveSharedRideJoin(config), OnboardPickups: config.OnboardPickups,
			PlatoonLimit: config.PlatoonLimit, StationBuffers: bool(config.StationBuffers), StationQueueSpacing: project.EffectiveStationQueueSpacing(config),
			PickupReassignment: bool(config.PickupReassignment), Positioning: mode, PositioningWeights: "one outward and one return origin per itinerary"}}}
	if input.Continuation != nil {
		if err := run.enableContinuation(ctx, config, plan, *input.Continuation); err != nil {
			return nil, err
		}
	}
	if err := run.ledger.advance(0, nil, run.service()); err != nil {
		run.fault = err.Error()
		return run, err
	}
	if err := run.captureBoundary(ctx); err != nil {
		return run, err
	}
	return run, nil
}

func preflight(pods *sim.Simulation, plan Plan, lots map[string]Lot) error {
	checked := make(map[sim.TripOptions]bool)
	for _, itinerary := range plan.Itineraries {
		options := sim.TripOptions{From: lots[itinerary.Lot].Hub, To: itinerary.Destination, PartySize: itinerary.PartySize, SharingConsent: itinerary.SharingConsent}
		for range 2 {
			if !checked[options] {
				validation := pods.Clone()
				if _, err := validation.SubmitTripOptions(options); err != nil {
					return fmt.Errorf("itinerary %q preflight %s to %s: %w", itinerary.ID, options.From, options.To, err)
				}
				checked[options] = true
			}
			options.From, options.To = options.To, options.From
		}
	}
	return nil
}

// Clone returns independent native and ledger state with the same continuation.
func (r *Run) Clone() *Run {
	clone := *r
	clone.pods, clone.ledger = r.pods.Clone(), r.ledger.clone()
	clone.continuation = r.continuation.clone()
	return &clone
}

// Done includes terminal refusals and stranded cars. It never calls them success.
func (r *Run) Done() bool {
	return r.fault != "" || r.ledger.terminal == len(r.ledger.records) || r.pods.Tick() >= r.provenance.HorizonTicks
}

// Step advances one native tick and then applies ordered car events.
func (r *Run) Step() error {
	return r.StepContext(context.Background())
}

// StepContext advances a tick and permits cancellation of checkpoint hashing.
func (r *Run) StepContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.fault != "" {
		return errors.New(r.fault)
	}
	if r.Done() {
		return nil
	}
	before := r.pods.Tick()
	r.pods.Step()
	if err := r.pods.CompactQueueError(); err != nil {
		r.fault = err.Error()
		return err
	}
	if r.pods.Tick() != before+1 {
		r.fault = "native simulation did not advance one tick"
		return errors.New(r.fault)
	}
	if r.continuation != nil {
		r.continuation.observeBoardings(r.pods)
	}
	if err := r.ledger.advance(r.pods.Tick(), r.pods.StepCompletions(), r.service()); err != nil {
		r.fault = err.Error()
		return err
	}
	return r.captureBoundary(ctx)
}

// Report returns owned partial or final evidence. It does not change the run.
func (r *Run) Report() Report {
	report := r.provenance
	report.EndpointTick, report.Error = r.pods.Tick(), r.fault
	report.Itineraries, report.Lots = r.ledger.reports()
	state := r.pods.MetricsSnapshot()
	report.Pods = PodMetrics{Submitted: state.Submitted, Completed: state.Completed, Pending: len(state.Pending),
		PassengerDistanceMeters: state.PassengerDistanceMeters, EmptyDistanceMeters: state.EmptyDistanceMeters,
		RiderDistanceMeters: state.RiderDistanceMeters, DirectDistanceMeters: state.DirectDistanceMeters, SharedParties: state.SharedParties}
	return report
}
