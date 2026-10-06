package sim

import (
	"errors"
	"fmt"
	"slices"
)

const (
	// TicksPerSecond fixes simulation time independently of rendering.
	TicksPerSecond = 60
	boardingTicks  = 3 * TicksPerSecond
	unloadingTicks = 2 * TicksPerSecond
	acceleration   = 2.0

	defaultReservationLookaheadSeconds = 2.0 / TicksPerSecond
	maxReservationLookaheadSeconds     = 10.0
	// Clearance combines a four-meter pod length and an eight-meter gap.
	Clearance = 4.0 + 8.0
	// MaxSharedRideParties bounds the same-destination sharing experiment.
	MaxSharedRideParties = 8
)

// Activity is the pod's current operation.
type Activity string

const (
	// Idle means the pod can accept a journey at its current station.
	Idle Activity = "Ready"
	// DepartingEmpty waits for track admission without a passenger.
	DepartingEmpty Activity = "Departing empty"
	// Boarding means a party is entering the pod.
	Boarding Activity = "Boarding"
	// Traveling includes movement and waits for local traffic admission.
	Traveling Activity = "Traveling"
	// Unloading means a party is leaving the pod.
	Unloading Activity = "Unloading"
	// Continuing waits for track admission after an intermediate stop, with
	// parties aboard for a later stop.
	Continuing Activity = "Continuing"
)

// WaitReason identifies the local resource that prevents movement.
type WaitReason string

const (
	// NoWait means no resource currently blocks the pod.
	NoWait WaitReason = ""
	// TrackOccupied means a pod ahead owns the next track block.
	TrackOccupied WaitReason = "Pod ahead"
	// JunctionOccupied means another pod owns a conflicting movement.
	JunctionOccupied WaitReason = "Junction traffic"
	// BerthOccupied means the destination berth is occupied or reserved by an arriving pod.
	BerthOccupied WaitReason = "Berth occupied"
	// ParkingUnavailable means no free reachable parking berth can receive the blocker.
	ParkingUnavailable WaitReason = "No parking available"
)

var (
	// ErrBusy means the selected pod has an active journey.
	ErrBusy = errors.New("the selected pod must finish its current journey first")
	// ErrSameStation means the destination is the current station.
	ErrSameStation = errors.New("choose another station")
)

// Request describes a party's journey separately from the vehicle.
//
// From and To never change after acceptance. LegFrom is the station where
// the party boards its current pod. Only a transfer sets it, and then it
// stays until the next transfer of the order, also after completion. It is
// empty for every other order. See legOrigin. It needs the incident
// marker. The session save writes it as a station index.
type Request struct {
	SharingConsent SharingConsent `json:"sharingConsent"`
	Service        ServiceChoice  `json:"service"`
	ServiceID      string         `json:"serviceID,omitempty"`
	ID             int            `json:"id"`
	From           string         `json:"from"`
	LegFrom        string         `json:"legFrom,omitempty"`
	To             string         `json:"to"`
	PartySize      int            `json:"partySize"`
	PodID          string         `json:"podID"`
	Completed      bool           `json:"completed"`
	// RequestedTick marks submission, before any pickup travel.
	RequestedTick int64 `json:"requestedTick"`
	// BoardedTick is the tick at which the party boarded a pod. It is 0 for
	// a party that has not boarded.
	BoardedTick int64 `json:"boardedTick,omitzero"`
	// DispatchReason explains why a pending order has not started boarding.
	DispatchReason string `json:"dispatchReason"`
}

// Pod contains observable vehicle state. LaneDistance is measured from the lane start.
type Pod struct {
	Class             VehicleClass `json:"class,omitempty"`
	ID                string       `json:"id"`
	Position          Point        `json:"position"`
	Activity          Activity     `json:"activity"`
	StationID         string       `json:"stationID"`
	BerthID           string       `json:"berthID"`
	LaneID            string       `json:"laneID"`
	LaneDistance      float64      `json:"laneDistance"`
	Speed             float64      `json:"speed"`
	Occupied          bool         `json:"occupied"`
	WaitReason        WaitReason   `json:"waitReason"`
	BlockedBy         string       `json:"blockedBy"`
	StationPhase      StationPhase `json:"stationPhase,omitempty"`
	ManeuverStationID string       `json:"maneuverStationID,omitempty"`
}

// Vehicle is an independent display copy of a pod and its assigned journey.
type Vehicle struct {
	// CouplingID binds a checked cabin to its physical train registry record.
	CouplingID string `json:"couplingID,omitzero"`
	// Boardings aligns with Riders when the original journey fields are insufficient.
	Boardings []RiderBoarding `json:"boardings,omitempty"`
	// RiddenMeters is cumulative passenger distance when Boardings is present.
	RiddenMeters float64 `json:"riddenMeters,omitzero"`
	// Presentation is set only for bounded stream views.
	Presentation *RoutePresentation `json:"-"`
	Pod          Pod                `json:"pod"`
	// Riders has one request for each party of the current or last
	// passenger journey of the pod, in boarding order. The first rider
	// boarded the pod, and the other riders joined it. A rider with
	// Completed true has left the pod. Riders is empty for a pod that did
	// not carry passengers since the last reset.
	Riders []Request `json:"riders,omitempty"`
	// Stops holds the station IDs of the stops that the pod still makes
	// with its riders, in route order. The first stop is the destination of
	// the current route.
	Stops []string `json:"stops,omitempty"`
	Route []Lane   `json:"route"`
	// RelocatingTo identifies the destination station during an empty move.
	RelocatingTo string `json:"relocatingTo"`
	// Rebalancing reports whether an empty move was started by guarded
	// positioning.
	Rebalancing bool `json:"rebalancing"`
	// PlatoonID is the ID of the first pod of the platoon of the pod.
	// PlatoonIndex is the position of the pod in that platoon, 1 for the
	// first pod. Snapshot sets both for a pod with a link to a pod ahead or
	// behind. They are empty for a pod that is not coupled.
	PlatoonID    string `json:"platoonID,omitempty"`
	PlatoonIndex int    `json:"platoonIndex,omitzero"`
	// Withdrawn holds the service holds of the pod: 1 for a fault and 2
	// for an emergency. Operational is the purpose of an operational
	// destination: OperationalEmergencyUnload, OperationalRefuge, or
	// OperationalEmptyRecovery. It is empty for a pod in service.
	// Snapshot sets both. They need the incident marker.
	Withdrawn   uint8  `json:"withdrawn,omitzero"`
	Operational string `json:"operational,omitzero"`
}

// The operational purposes of Vehicle.Operational (incident contract,
// section 11.5).
const (
	OperationalEmergencyUnload = "emergency-unload"
	OperationalRefuge          = "refuge"
	OperationalEmptyRecovery   = "empty-recovery"
)

// BerthState separates physical occupancy from local arrival admission.
type BerthState struct {
	ID         string `json:"id"`
	Occupant   string `json:"occupant"`
	ReservedBy string `json:"reservedBy"`
}

// Snapshot is a copy of the fleet, clock, and station resources.
type Snapshot struct {
	CouplingContract CouplingContract    `json:"couplingContract,omitzero"`
	CouplingEnabled  bool                `json:"couplingEnabled,omitzero"`
	CouplingGroups   []CouplingGroupView `json:"couplingGroups,omitempty"`
	OrderContract    OrderContract       `json:"orderContract,omitzero"`
	IncidentContract IncidentContract    `json:"incidentContract,omitzero"`
	// FaultContract is the fault marker. Faults holds the active faults and
	// the fault counters. It is zero without the marker.
	FaultContract FaultContract `json:"faultContract,omitzero"`
	Faults        FaultsView    `json:"faults,omitzero"`

	// Submitted counts accepted passenger orders since reset.
	Submitted int          `json:"submitted"`
	Tick      int64        `json:"tick"`
	Paused    bool         `json:"paused"`
	Vehicles  []Vehicle    `json:"vehicles"`
	Berths    []BerthState `json:"berths"`
	Completed int          `json:"completed"`
	Demo      bool         `json:"demo"`
	DemoError string       `json:"demoError"`
	// Interrupted counts the orders that ended interrupted since reset, and
	// InterruptedPassengers is the sum of their party sizes. They need the
	// incident marker.
	Interrupted           int `json:"interrupted,omitzero"`
	InterruptedPassengers int `json:"interruptedPassengers,omitzero"`
	// Pending holds passenger requests that have not started boarding.
	Pending []Request `json:"pending"`
	// Wait summarizes request-to-boarding delay, including elapsed pending waits.
	Wait WaitStats `json:"wait"`
	// Journey summarizes the time from request to alighting of the parties
	// that left a pod at their destination.
	Journey JourneyStats `json:"journey"`
	// PassengerDistanceMeters is the distance traveled with a passenger.
	PassengerDistanceMeters float64 `json:"passengerDistanceMeters"`
	// RiderDistanceMeters is the sum of the distances that the parties of
	// Journey rode. DirectDistanceMeters is the sum of the free-flow
	// distances of the same parties from their boarding berth to their
	// alighting berth. MaxDetourRatio is the largest ratio of the two
	// distances for one party.
	RiderDistanceMeters  float64 `json:"riderDistanceMeters"`
	DirectDistanceMeters float64 `json:"directDistanceMeters"`
	MaxDetourRatio       float64 `json:"maxDetourRatio"`
	// SharedParties counts parties that joined another party's boarding pod.
	SharedParties        int `json:"sharedParties"`
	SharedRidePartyLimit int `json:"sharedRidePartyLimit"`
	// EmptyDistanceMeters is the distance traveled without a passenger.
	EmptyDistanceMeters float64 `json:"emptyDistanceMeters"`
	// RebalanceMoves counts proactive empty moves started since reset.
	RebalanceMoves int `json:"rebalanceMoves"`
}

// SafetyObservation is the state needed to check fleet separation and berth use.
type SafetyObservation struct {
	OrderContract OrderContract `json:"orderContract,omitzero"`
	Tick          int64
	Completed     int
	Pending       int
	Pods          []Pod
	Berths        []BerthState
	Locations     map[string]SafetyLocation
	compactPairs  map[[2]string]compactSafetyPair
	compactError  error
	couplingPairs map[[2]string]couplingSafetyPair
	couplingError error
	envelopes     map[string]safetyEnvelope
}

// SafetyLocation identifies the physical plane and endpoints occupied by a pod.
type SafetyLocation struct {
	SeparationGroup string
	From            string
	To              string
}

// Placement starts a pod at an empty station berth.
type Placement struct {
	Class     VehicleClass `json:"class,omitempty"`
	ID        string       `json:"id"`
	StationID string       `json:"stationID"`
	BerthID   string       `json:"berthID"`
}

type vehicle struct {
	couplingID string
	// buffered keeps pending buffer admissions until berth commitment.
	buffered     bool
	bufferBerth  string
	routeVersion uint64
	stationPhase stationPhaseCheck
	Vehicle
	phaseTicks                  int
	blockStarts                 map[string]int
	routeReleases               map[resource]float64
	blockIndex, reservedThrough int
	originReleased              bool
	distance                    float64
	// riddenBase is the distance that the riders rode before the start of
	// distance: the earlier legs of the journey, and the start of the route
	// that a restore cut.
	riddenBase float64
	// journeyOrigin is the common boarding berth of an old-form journey.
	// Recorded parties use Boardings. origin starts the current leg.
	journeyOrigin       Berth
	pending             int
	waitSince           int64
	rebalanceAfter      int64
	origin, destination Berth
	destinationStation  string
	// released is true for an empty pod that dispatch sent to a pickup and
	// then released with no claim. It is also true for a guarded
	// rebalancing pod that yielded its claim. Such a pod can divert at once,
	// as a pod on its way to parking can.
	released bool
	// withdrawn holds the causes that withdraw the pod from service. The
	// pod is in service when it has no hold. See inService.
	withdrawn serviceHold
	// op is the purpose of the physical destination of the pod. It is zero
	// for a pod in service. See operationalDestination.
	op operationalDestination
	// nextRelease is 0, or it is at most each release distance in
	// routeReleases and the pod or a pod ahead of it in its platoon owns
	// each resource in routeReleases that the pod has not passed. In the
	// second case, releaseVehicleResources has no entry to change while
	// distance is less than nextRelease. With the value 0, an entry
	// can name a resource that the pod does not own. releaseOwned sets 0,
	// and retainRouteResource keeps 0. Code that removes the owner of a
	// resource in routeReleases must call releaseOwned or clear
	// routeReleases.
	nextRelease float64
	// terminal keeps the last result of terminalLane. Each write of the
	// route resets it.
	terminal terminalCheck
	// routeLengths holds the value of laneLength for each lane of Route.
	// Each write of the route also writes it.
	routeLengths []float64
	// blocks holds the blocks of Route. Each write of the route also writes
	// it.
	blocks blockList
	// link couples the pod to its predecessor in a platoon. follower is one
	// plus the index in Simulation.vehicles of the pod that couples to this
	// pod, or 0. platoonCap is the route distance within which a pod with a
	// predecessor must stop in this tick. platoonCaps sets it.
	link       platoonLink
	follower   int
	platoonCap float64
	// faulted is true while a fault record names the pod. faultCap is the
	// route distance at which a faulted traveling pod stops. Fault start
	// sets it, and the clear sets it to 0. No other code writes it.
	faulted  bool
	faultCap float64
}

// Simulation owns a fixed fleet and local track, junction, and berth resources.
//
// Clone shares some fields with its source. They are the network, the route
// graph, and the station, berth, pod, geometry, junction, and safety
// indexes. They also include the initial fleet, the demand weights, the
// congestion costs, the routes of pods and waiting trips, the lane cells,
// and the block tables and route lengths of pods. Code must replace a
// shared field whole. It must not write into a shared field in place,
// because that change also changes the clones.
//
// The embedded networkIndexes holds the network and its indexes. The
// prepared network, other simulations and clones share it, so code that
// changes the network or an index installs a new value. See
// ensureNetworkIndexes.
type Simulation struct {
	*networkIndexes
	couplingState
	compactBufferState
	platooningState
	predictiveState
	experimentRecords
	metricCounters
	orderContract    OrderContract
	incidentContract IncidentContract
	// faultContract is the fault marker of the project. The frames copy
	// it. faultsOn is the switch of the operations: the traffic demo keeps
	// the marker and turns the operations off.
	faultContract FaultContract
	// incidentSerial counts the incident records. It is saved, and a reset
	// keeps it. incidentGeneration is the session generation of the IDs.
	incidentSerial     uint64
	incidentGeneration uint64
	// faultsOn enables the fault operations and the fault stage, with the
	// faultSettings. Reset keeps both.
	faultsOn      bool
	faultSettings faultSettings
	// faults holds the active fault records in serial order. faultCounters
	// counts the fault events. Reset clears both. See faults.go.
	faults        []faultRecord
	faultCounters faultCounters
	// faultReleased holds the debris resources that a clear in the fault
	// stage releases. Step releases them at each exit of the tick, so it
	// is empty at each boundary.
	faultReleased          []resource
	motion                 *motionRecorder
	expressServices        map[string]ExpressService
	lengths                map[string]float64
	routes                 map[routeKey]routeResult
	routeOrder             []routeKey
	pickupBounds           map[string][]float64
	routeWork              *routeSearchWork
	admissionWork          *admissionWork
	initial                []Placement
	vehicles               []vehicle
	owners                 map[resource]resourceOwner
	tick                   int64
	paused                 bool
	requestID              int
	demo                   *demoRun
	demoError              string
	waiting                []waitingTrip
	positioning            Positioning
	demandRate             int
	demandWeights          map[string]float64
	nextRedistributionTick int64
	sharedRidePartyLimit   int
	sharedRideMode         SharedRideMode
	sharedRideMaxStops     int
	sharedRideJoin         SharedRideJoin
	onboardPickups         bool
	// approachStations and routeStations belong to stationsOnRoute.
	approachStations            map[string][]string
	routeStations               map[stopKey][]string
	seatScreen                  SeatScreen
	routingPolicy               RoutingPolicy
	finishingPodWait            FinishingPodWait
	stationBuffers              bool
	stationQueueSpacing         StationQueueSpacing
	pickupSwaps                 *pickupSwapController
	congestionRouteCosts        []float64
	congestionRoutes            map[routeKey]routeResult
	nextCongestionRouteRefresh  int64
	reservationLookaheadSeconds float64
	// unaccountedOrders counts the orders that the simulation submitted but
	// that are not complete, not interrupted, not queued and not aboard a
	// pod. It is 0 until a restore finds such orders in a saved state or
	// drops orders.
	unaccountedOrders int
	// undelivered holds the IDs of the orders interrupted since the last
	// DrainInterruptions call. Step does not clear it, and it is not saved.
	undelivered []int
	// monitor runs after each tick and after each public command that
	// changes the pods, the orders or the sharing settings. Tests use it to
	// check the contract. See observe.
	monitor func(*Simulation)
	// blocked is the blocked set of the active faults. setBlocked sets
	// rerouteDue at each new epoch of the set. See blocked_routes.go.
	blocked    blockedSet
	rerouteDue bool
	// staticConnected and staticRoutes are caches of the network. See
	// blocked_routes.go.
	staticConnected map[routeKey]bool
	staticRoutes    map[routeKey]routeResult
	// routeView is the routing view of one query. It is nil outside the
	// query. See route_view.go.
	routeView *routeView
	// vehicleIndexes gives the position in vehicles of each pod ID. Reset
	// and restorePhysical replace it whole after they replace vehicles. No
	// code writes to it in place. findVehicle checks each entry, so an entry
	// that is missing or stale makes the lookup slower but not wrong.
	vehicleIndexes map[string]int
	// pass holds the buffers of dispatch, which makes it at the first call
	// and reuses it at each later call. It is not part of the state. Clone
	// drops it, so two simulations never share the buffers.
	pass *dispatchPass
}

// couplingState holds the physical coupling state of a simulation.
type couplingState struct {
	couplingNetwork    *couplingReservationNetwork
	couplingEnabled    bool
	couplingGroups     []couplingNativeGroup
	couplingFault      error
	couplingFleet      *nativeForeignFleet
	couplingApproaches []couplingNativeApproach
	couplingAttempts   map[string]couplingApproachAttempt
}

// metricCounters holds the service counters of a simulation.
type metricCounters struct {
	completed                    int
	boarded                      int
	totalWaitTicks, maxWaitTicks int64
	// journeys counts the parties that left a pod at their destination.
	// The journey and distance totals below count the same parties.
	journeys                                  int
	totalJourneyTicks, maxJourneyTicks        int64
	riderDistanceMeters, directDistanceMeters float64
	maxDetourRatio                            float64
	passengerDistanceMeters                   float64
	emptyDistanceMeters                       float64
	rebalanceMoves                            int
	sharedParties                             int
	// interrupted counts the orders that ended interrupted, and
	// interruptedPassengers is the sum of their party sizes.
	interrupted, interruptedPassengers int
}

// predictiveState holds the queue history of predictive routing.
type predictiveState struct {
	predictiveQueues    []float64
	predictivePodQueues map[string]podQueueHistory
	predictiveQueueTick int64
}

// compactBufferState holds the compact station queues.
type compactBufferState struct {
	compactGroups     []*compactBufferGroup
	compactMotions    []compactBufferMotion
	compactNextGroups []*compactBufferGroup
	compactFault      error
}

// experimentRecords holds the experiment records.
// recordExperiments turns them on. requestBoardings has one entry for each
// boarding, and requestCompletions has one entry for each pod journey with
// passengers that ends. RequestTimings reads them. nodePasses has one entry
// for each lane that a pod enters, and NodePasses reads it. Only append
// writes to them. Reset clears them, and a restore starts without them.
type experimentRecords struct {
	recordExperiments  bool
	requestBoardings   []RequestTiming
	requestCompletions []requestCompletion
	stepCompletions    []StepCompletion
	nodePasses         []NodePass
}

// platooningState holds the platooning settings and links.
type platooningState struct {
	// platooning is the platooning mode and platoonLimit is the largest
	// platoon. platoonLinks counts the pods with a predecessor. Reset
	// keeps the mode and the limit.
	platooning   Platooning
	platoonLimit int
	platoonLinks int
	// platoonData holds the network data that links read. platoonIndexes
	// builds it, and no code writes to it in place.
	platoonData *platoonIndexData
	// platoonOrder, platoonAhead and platoonLanes are work storage of
	// formPlatoons. Clone does not share them.
	platoonOrder, platoonAhead []int
	platoonLanes               map[string]bool
}

// New creates a one-pod scenario for focused experiments.
func New(network Network, startStation string) (*Simulation, error) {
	return NewFleet(network, []Placement{{ID: "01", StationID: startStation}})
}

// NewFleet validates and copies a fixed fleet. An omitted berth ID selects the first berth.
func NewFleet(network Network, placements []Placement) (*Simulation, error) {
	owned, graph, err := prepareFleet(network, placements)
	if err != nil {
		return nil, err
	}
	return newPreparedNetwork(owned, graph).newFleet(placements), nil
}

// ValidateFleet returns the error that NewFleet returns for the same network and fleet.
// It does not build the simulation, so it costs less than NewFleet.
func ValidateFleet(network Network, placements []Placement) error {
	_, _, err := prepareFleet(network, placements)
	return err
}

// prepareFleet validates a network and a fleet. It returns an owned copy of the
// network with inferred station lane roles, and the route graph of that copy.
func prepareFleet(network Network, placements []Placement) (Network, routeGraph, error) {
	owned, graph, err := prepareNetwork(network)
	if err != nil {
		return Network{}, routeGraph{}, err
	}
	if err := validatePlacements(owned, placements); err != nil {
		return Network{}, routeGraph{}, err
	}
	return owned, graph, nil
}

// Reset restores the initial fleet, clock, and resources. It clears supplied
// demo requests and the fault records, counters and blocked set. It keeps
// the incident serial and the fault settings.
func (s *Simulation) Reset() {
	defer s.observe()
	s.admissionWork = nil
	s.faults, s.faultCounters, s.faultReleased = nil, faultCounters{}, nil
	if s.blockedActive() {
		s.setBlocked(nil)
	}
	s.rerouteDue = false
	s.tick, s.completed, s.requestID, s.unaccountedOrders = 0, 0, 0, 0
	s.interrupted, s.interruptedPassengers, s.undelivered = 0, 0, nil
	if s.motion != nil {
		s.motion = &motionRecorder{}
	}
	s.paused, s.demo, s.demoError = false, nil, ""
	s.waiting = nil
	s.boarded, s.totalWaitTicks, s.maxWaitTicks = 0, 0, 0
	s.journeys, s.totalJourneyTicks, s.maxJourneyTicks = 0, 0, 0
	s.riderDistanceMeters, s.directDistanceMeters, s.maxDetourRatio = 0, 0, 0
	s.positioning, s.demandRate, s.demandWeights = PositioningOff, 0, nil
	s.nextRedistributionTick = 0
	s.nextCongestionRouteRefresh, s.congestionRouteCosts, s.congestionRoutes = 0, nil, nil
	s.predictiveQueues, s.predictiveQueueTick, s.predictivePodQueues = nil, 0, nil
	s.passengerDistanceMeters, s.emptyDistanceMeters, s.rebalanceMoves, s.sharedParties = 0, 0, 0, 0
	s.seatScreen = SeatScreen{}
	s.requestBoardings, s.requestCompletions, s.nodePasses = nil, nil, nil
	s.stepCompletions = nil
	if s.pickupSwaps != nil {
		s.pickupSwaps = &pickupSwapController{enabled: s.pickupSwaps.enabled, right: 1, cooldown: make(map[string]int64)}
	}
	s.platoonLinks = 0
	s.couplingGroups = nil
	s.couplingApproaches = nil
	s.couplingAttempts = nil
	s.couplingFleet = nil
	s.couplingFault = nil
	s.compactGroups, s.compactMotions, s.compactNextGroups, s.compactFault = nil, nil, nil, nil
	s.owners = make(map[resource]resourceOwner)
	s.vehicles = nil
	for _, p := range s.initial {
		station, _ := s.station(p.StationID)
		berth, _ := station.berth(p.BerthID)
		node, _ := s.network.Node(berth.Node)
		pod := Pod{
			ID: p.ID, Class: p.Class, Position: node.Position, Activity: Idle,
			StationID: station.ID, BerthID: berth.ID,
			StationPhase: AtBerth, ManeuverStationID: station.ID,
		}
		s.vehicles = append(s.vehicles, vehicle{Pod: pod, pending: -1, reservedThrough: -1})
		s.owners[resource{kind: berthResource, id: berth.ID}] = podResourceOwner(p.ID)
		s.owners[resource{kind: nodeResource, id: berth.Node}] = podResourceOwner(p.ID)
	}
	s.vehicleIndexes = indexVehicles(s.vehicles)
}

// indexVehicles returns the position of each pod ID in vehicles. Pod IDs are
// unique, because fleet validation and restore validation reject duplicates.
func indexVehicles(vehicles []vehicle) map[string]int {
	indexes := make(map[string]int, len(vehicles))
	for index := range vehicles {
		indexes[vehicles[index].Pod.ID] = index
	}
	return indexes
}

// Snapshot does not expose mutable simulation storage.
func (s *Simulation) Snapshot() Snapshot { return s.snapshot(true) }

func (s *Simulation) snapshot(routes bool) Snapshot {
	state := Snapshot{
		OrderContract: s.orderContract, IncidentContract: s.incidentContract, FaultContract: s.faultContract,
		Submitted: s.requestID, Tick: s.tick, Paused: s.paused,
		Completed: s.completed, Demo: s.demo != nil, DemoError: s.demoError,
		Interrupted: s.interrupted, InterruptedPassengers: s.interruptedPassengers,
		Wait: s.waitStats(), Journey: s.journeyStats(), PassengerDistanceMeters: s.passengerDistanceMeters,
		RiderDistanceMeters: s.riderDistanceMeters, DirectDistanceMeters: s.directDistanceMeters,
		MaxDetourRatio: s.maxDetourRatio, EmptyDistanceMeters: s.emptyDistanceMeters, RebalanceMoves: s.rebalanceMoves,
		SharedParties: s.sharedParties, SharedRidePartyLimit: s.sharedRidePartyLimit,
	}
	if len(s.waiting) > 0 {
		state.Pending = make([]Request, 0, len(s.waiting))
	}
	if len(s.vehicles) > 0 {
		state.Vehicles = make([]Vehicle, 0, len(s.vehicles))
	}
	for _, trip := range s.waiting {
		state.Pending = append(state.Pending, trip.request)
	}
	for _, v := range s.vehicles {
		cloned := v.Vehicle
		cloned.Route = nil
		if routes {
			cloned.Route = cloneLanes(v.Route)
		}
		cloned.Riders, cloned.Stops = slices.Clone(cloned.Riders), slices.Clone(cloned.Stops)
		cloned.Withdrawn, cloned.Operational = uint8(v.withdrawn), v.op.purpose.name()
		cloned.Boardings, cloned.RiddenMeters = nil, 0
		if !v.legacyBoardingRecords() {
			cloned.Boardings, cloned.RiddenMeters = slices.Clone(v.Boardings), v.riddenMeters()
		}
		state.Vehicles = append(state.Vehicles, cloned)
	}
	if s.platoonLinks > 0 {
		for index := range s.vehicles {
			if s.vehicles[index].coupled() {
				first, position := s.platoonPosition(index)
				state.Vehicles[index].PlatoonID, state.Vehicles[index].PlatoonIndex = s.vehicles[first].Pod.ID, position
			}
		}
	}
	state.Berths = s.berthStates()
	if s.faultContract != "" {
		state.Faults = s.faultsView()
	}
	return state
}

// SafetyObservation does not expose mutable simulation storage.
func (s *Simulation) SafetyObservation() SafetyObservation {
	state := SafetyObservation{
		OrderContract: s.orderContract,
		Tick:          s.tick, Completed: s.completed, Pending: len(s.waiting),
		Pods: make([]Pod, len(s.vehicles)), Berths: s.berthStates(),
		Locations: make(map[string]SafetyLocation, len(s.vehicles)),
	}
	for index := range s.vehicles {
		pod := s.vehicles[index].Pod
		state.Pods[index] = pod
		if pod.LaneID != "" {
			state.Locations[pod.ID] = s.laneSafety[pod.LaneID]
		} else if pod.BerthID != "" {
			state.Locations[pod.ID] = s.berthSafety[pod.BerthID]
		}
	}
	s.compactSafety(&state)
	s.largeSafety(&state)
	s.couplingSafety(&state)
	return state
}

func (s *Simulation) berthStates() []BerthState {
	occupants := make(map[string]string, len(s.vehicles))
	for _, v := range s.vehicles {
		if v.Pod.BerthID != "" {
			occupants[v.Pod.BerthID] = v.Pod.ID
		}
	}
	berthCount := 0
	for _, station := range s.network.Stations {
		berthCount += len(station.Berths)
	}
	states := make([]BerthState, 0, berthCount)
	for _, station := range s.network.Stations {
		for _, berth := range station.Berths {
			states = append(states, BerthState{
				ID:         berth.ID,
				Occupant:   occupants[berth.ID],
				ReservedBy: s.owners[resource{kind: berthResource, id: berth.ID}].podID(),
			})
		}
	}
	return states
}

// SetPaused controls whether Step advances the simulation clock.
func (s *Simulation) SetPaused(paused bool) { s.paused = paused }

// RequestJourney assigns a party at the selected pod's station.
func (s *Simulation) RequestJourney(podID, destination string) error {
	v := s.findVehicle(podID)
	if v == nil {
		return fmt.Errorf("unknown pod %q", podID)
	}
	return s.RequestJourneyOptions(podID, TripOptions{From: v.Pod.StationID, To: destination})
}

// RequestJourneyOptions assigns a whole party to the selected idle pod. A
// withdrawn pod is busy.
func (s *Simulation) RequestJourneyOptions(podID string, options TripOptions) error {
	defer s.observe()
	v := s.findVehicle(podID)
	if v == nil {
		return fmt.Errorf("unknown pod %q", podID)
	}
	if v.Pod.Activity != Idle || !v.inService() || s.assigned(v.Pod.ID) {
		return ErrBusy
	}
	if options.From == "" {
		options.From = v.Pod.StationID
	}
	if options.From != v.Pod.StationID {
		return errors.New("the selected pod must be at the pickup station")
	}
	options, err := s.validateTripOptions(options)
	if err != nil {
		return err
	}
	request := requestFromOptions(options, s.requestID+1, s.tick)
	if !s.podFitsRequest(v, request) {
		return ErrPartyAdmission
	}
	if err := s.board(v, waitingTrip{request: request}); err != nil {
		return err
	}
	s.requestID++
	return nil
}

// findVehicle returns the pod with the given ID, or nil when no pod has it.
// It returns nil at once for an empty ID, because fleet validation and
// restore validation reject an empty pod ID. It uses vehicleIndexes when the
// entry names a pod with that ID. Pod IDs in vehicles are unique, so that
// pod is the pod that a scan finds. When the entry is missing or stale,
// findVehicle scans vehicles.
func (s *Simulation) findVehicle(id string) *vehicle {
	if id == "" {
		return nil
	}
	if index, ok := s.vehicleIndexes[id]; ok && index < len(s.vehicles) && s.vehicles[index].Pod.ID == id {
		return &s.vehicles[index]
	}
	for i := range s.vehicles {
		if s.vehicles[i].Pod.ID == id {
			return &s.vehicles[i]
		}
	}
	return nil
}

// Step plans admission from pre-movement state, arbitrates, then moves every pod.
func (s *Simulation) Step() {
	defer s.observe()
	if s.couplingFault != nil {
		s.paused = true
		return
	}
	if s.paused {
		return
	}
	s.discoverCouplingApproaches()
	s.stepCompletions = s.stepCompletions[:0]
	s.tick++
	s.stepDemo()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		// A faulted pod stops where it is: its phase timer does not run,
		// and no rider alights.
		if v.faulted {
			continue
		}
		if v.phaseTicks > 0 {
			v.phaseTicks--
		}
		if v.Pod.Activity == Unloading && v.phaseTicks == 0 {
			// A refuge holds until resumeFromRefuge. An emergency unload
			// ends with its own outcomes.
			switch v.op.purpose {
			case opRefuge:
				continue
			case opEmergencyUnload:
				s.finishOperationalUnload(v)
				continue
			default:
			}
			s.alight(v)
			if len(v.Stops) > 0 && v.RidersAboard() > 0 {
				s.continueJourney(v)
			} else {
				v.Pod.Activity, v.Pod.Occupied, v.Stops = Idle, false, nil
			}
		}
	}
	if s.faultsOn {
		s.faultStage()
		// A clear in the fault stage defers the release of its debris
		// resources to the end of the tick. Each exit of the tick, also an
		// exit on a planning error, finishes the release before observe.
		defer s.releaseFaultResources()
	}
	s.dispatch()
	s.swapPickups()
	s.redistribute()
	s.formPlatoons()
	s.admit()
	s.clearBlockedBerths()
	s.formCompactQueues()
	if err := s.planCompactQueues(); err != nil {
		s.compactFault, s.paused = err, true
		return
	}
	s.platoonCaps()
	coupling, err := s.planNativeCouplingTick()
	if err != nil {
		s.couplingFault, s.paused = err, true
		return
	}
	s.beginMotionFrame()
	if coupling != nil {
		for i := range s.couplingGroups {
			s.moveNativeCoupling(coupling, i)
		}
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.couplingID != "" {
			continue
		}
		if departs(v.Pod.Activity) && v.phaseTicks == 0 && v.reservedThrough >= 0 && !v.faulted {
			if v.Pod.Activity == Boarding && s.screensSeats() {
				s.recordDeparture(v)
			}
			v.Pod.Occupied = v.Pod.Activity == Boarding || v.Pod.Activity == Continuing
			v.Pod.Activity = Traveling
			v.Pod.StationID, v.Pod.BerthID = "", ""
			continue
		}
		if v.Pod.Activity == Traveling {
			if s.moveCouplingApproach(coupling, v) {
				continue
			}
			s.moveAndMeasure(v)
		}
	}
	if err := s.finishNativeCoupling(coupling); err != nil {
		s.couplingFault, s.paused = err, true
		return
	}
	s.compactGroups = s.compactNextGroups
	s.compactNextGroups = nil
	s.finishCompactQueues()
	// No pod can reuse resources released during this tick until the next tick.
	s.releaseCleared()
	s.finishCouplingApproachLinks()
	for i := range s.vehicles {
		s.updateStationPhase(&s.vehicles[i])
	}
	s.publishMotionFrame()
}

func (s *Simulation) arrive(v *vehicle) {
	v.buffered, v.bufferBerth = false, ""
	station, _ := s.station(v.destinationStation)
	berth := v.destination
	node, _ := s.network.Node(berth.Node)
	v.Pod = Pod{
		ID: v.Pod.ID, Class: v.Pod.Class, Position: node.Position, Activity: Unloading,
		StationID: station.ID, BerthID: berth.ID, Occupied: true,
		StationPhase: AtBerth, ManeuverStationID: station.ID,
	}
	v.phaseTicks = unloadingTicks
	switch {
	case v.op.purpose == opEmergencyUnload:
		v.Stops = withoutStop(v.Stops, station.ID)
	case v.op.purpose == opRefuge:
		v.phaseTicks, v.Pod.WaitReason = 0, refugeHolding
	case len(v.Stops) > 0 && v.Stops[0] == station.ID:
		v.Stops = slices.Clip(v.Stops[1:])
		if len(v.Stops) == 0 {
			v.Stops = nil
		}
	}
	if v.RelocatingTo != "" {
		v.Pod.Activity, v.Pod.Occupied = Idle, false
		v.phaseTicks, v.RelocatingTo, v.released = 0, "", false
		if v.Rebalancing {
			v.rebalanceAfter = s.tick + redistributionCooldownTicks
			v.Rebalancing = false
		}
		// An empty recovery ends at its berth. The holds stay.
		v.op = operationalDestination{}
	}
	// The fault stays, and the gates hold the pod at the berth. Its
	// footprint is now the berth. The new pod has no report, so the
	// fault report starts again, at rest.
	if v.faulted {
		s.rebuildBlocked()
		s.reportFault(v)
	}
}
