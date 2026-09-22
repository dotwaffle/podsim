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
type Request struct {
	ID        int    `json:"ID"`
	From      string `json:"From"`
	To        string `json:"To"`
	PartySize int    `json:"PartySize"`
	PodID     string `json:"PodID"`
	Completed bool   `json:"Completed"`
	// RequestedTick marks submission, before any pickup travel.
	RequestedTick int64 `json:"RequestedTick"`
	// DispatchReason explains why a pending order has not started boarding.
	DispatchReason string `json:"DispatchReason"`
}

// Pod contains observable vehicle state. LaneDistance is measured from the lane start.
type Pod struct {
	ID           string     `json:"ID"`
	Position     Point      `json:"Position"`
	Activity     Activity   `json:"Activity"`
	StationID    string     `json:"StationID"`
	BerthID      string     `json:"BerthID"`
	LaneID       string     `json:"LaneID"`
	LaneDistance float64    `json:"LaneDistance"`
	Speed        float64    `json:"Speed"`
	Occupied     bool       `json:"Occupied"`
	WaitReason   WaitReason `json:"WaitReason"`
	BlockedBy    string     `json:"BlockedBy"`
}

// Vehicle is an independent display copy of a pod and its assigned journey.
type Vehicle struct {
	Pod     Pod      `json:"Pod"`
	Request *Request `json:"Request"`
	Route   []Lane   `json:"Route"`
	// Parties counts separately submitted passenger groups aboard this pod.
	Parties int `json:"Parties,omitempty"`
	// RelocatingTo identifies the destination station during an empty move.
	RelocatingTo string `json:"RelocatingTo"`
	// Rebalancing reports whether an empty move was started by redistribution.
	Rebalancing bool `json:"Rebalancing"`
}

// BerthState separates physical occupancy from local arrival admission.
type BerthState struct {
	ID         string `json:"ID"`
	Occupant   string `json:"Occupant"`
	ReservedBy string `json:"ReservedBy"`
}

// Snapshot is a copy of the fleet, clock, and station resources.
type Snapshot struct {

	// Submitted counts accepted passenger orders since reset.
	Submitted int          `json:"Submitted"`
	Tick      int64        `json:"Tick"`
	Paused    bool         `json:"Paused"`
	Vehicles  []Vehicle    `json:"Vehicles"`
	Berths    []BerthState `json:"Berths"`
	Completed int          `json:"Completed"`
	Demo      bool         `json:"Demo"`
	DemoError string       `json:"DemoError"`
	// Pending holds passenger requests that have not started boarding.
	Pending []Request `json:"Pending"`
	// Wait summarizes request-to-boarding delay, including elapsed pending waits.
	Wait WaitStats `json:"Wait"`
	// PassengerDistanceMeters is the distance traveled with a passenger.
	PassengerDistanceMeters float64 `json:"PassengerDistanceMeters"`
	// SharedParties counts parties that joined another party's boarding pod.
	SharedParties        int `json:"SharedParties"`
	SharedRidePartyLimit int `json:"SharedRidePartyLimit"`
	// EmptyDistanceMeters is the distance traveled without a passenger.
	EmptyDistanceMeters float64 `json:"EmptyDistanceMeters"`
	// RebalanceMoves counts proactive empty moves started since reset.
	RebalanceMoves int `json:"RebalanceMoves"`
}

// SafetyObservation is the state needed to check fleet separation and berth use.
type SafetyObservation struct {
	Tick      int64
	Completed int
	Pending   int
	Pods      []Pod
	Berths    []BerthState
	Locations map[string]SafetyLocation
}

// SafetyLocation identifies the physical plane and endpoints occupied by a pod.
type SafetyLocation struct {
	SeparationGroup string
	From            string
	To              string
}

// Placement starts a pod at an empty station berth.
type Placement struct {
	ID        string `json:"ID"`
	StationID string `json:"StationID"`
	BerthID   string `json:"BerthID"`
}

type vehicle struct {
	Vehicle
	phaseTicks                  int
	blocks                      []block
	blockStarts                 map[string]int
	routeReleases               map[resource]float64
	blockIndex, reservedThrough int
	originReleased              bool
	distance                    float64
	pending                     int
	waitSince                   int64
	rebalanceAfter              int64
	origin, destination         Berth
	destinationStation          string
}

// Simulation owns a fixed fleet and local track, junction, and berth resources.
type Simulation struct {
	lengths                      map[string]float64
	routes                       map[routeKey]routeResult
	routeOrder                   []routeKey
	graph                        routeGraph
	stationIndexes               map[string]int
	stationForbidden             map[string]bool
	geometry                     map[string]laneGeometry
	network                      Network
	initial                      []Placement
	vehicles                     []vehicle
	owners                       map[resource]string
	junctionConflicts            map[string][]laneConflict
	tick                         int64
	paused                       bool
	completed, requestID         int
	demo                         *demoRun
	demoError                    string
	waiting                      []waitingTrip
	boarded                      int
	totalWaitTicks, maxWaitTicks int64
	redistribution               bool
	demandWeights                map[string]float64
	nextRedistributionTick       int64
	passengerDistanceMeters      float64
	emptyDistanceMeters          float64
	rebalanceMoves               int
	sharedRidePartyLimit         int
	sharedParties                int
	reservationLookaheadSeconds  float64
	laneSafety                   map[string]SafetyLocation
	berthSafety                  map[string]SafetyLocation
}

// New creates a one-pod scenario for focused experiments.
func New(network Network, startStation string) (*Simulation, error) {
	return NewFleet(network, []Placement{{ID: "01", StationID: startStation}})
}

// NewFleet validates and copies a fixed fleet. An omitted berth ID selects the first berth.
func NewFleet(network Network, placements []Placement) (*Simulation, error) {
	if err := network.validate(); err != nil {
		return nil, err
	}
	graph := newRouteGraph(network)
	for index, lane := range network.Lanes {
		if graph.lengths[index] < 2*Clearance {
			return nil, fmt.Errorf("lane %q must be at least %.0f meters long", lane.ID, 2*Clearance)
		}
	}
	if len(placements) == 0 {
		return nil, errors.New("the fleet needs at least one pod")
	}
	ids, berths := make(map[string]bool), make(map[string]bool)
	for _, p := range placements {
		if p.ID == "" || ids[p.ID] {
			return nil, fmt.Errorf("invalid or duplicate pod %q", p.ID)
		}
		station, ok := network.Station(p.StationID)
		if !ok {
			return nil, fmt.Errorf("unknown start station %q", p.StationID)
		}
		berth, ok := station.berth(p.BerthID)
		if !ok || berths[berth.ID] {
			return nil, fmt.Errorf("invalid or occupied initial berth at %q", p.StationID)
		}
		ids[p.ID], berths[berth.ID] = true, true
	}
	initial := slices.Clone(placements)
	slices.SortFunc(initial, func(a, b Placement) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	owned := network.clone()
	s := &Simulation{
		network: owned, initial: initial, graph: graph,
		stationIndexes:              indexStations(owned),
		stationForbidden:            owned.stationForbidden(),
		geometry:                    buildLaneGeometry(owned),
		sharedRidePartyLimit:        1,
		reservationLookaheadSeconds: defaultReservationLookaheadSeconds,
		laneSafety:                  make(map[string]SafetyLocation, len(network.Lanes)),
		berthSafety:                 make(map[string]SafetyLocation),
	}
	for _, lane := range network.Lanes {
		s.laneSafety[lane.ID] = SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}
	}
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			s.berthSafety[berth.ID] = SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}
		}
	}
	s.Reset()
	return s, nil
}

// Reset restores the initial fleet, clock, and resources. It clears supplied demo requests.
func (s *Simulation) Reset() {
	s.tick, s.completed, s.requestID = 0, 0, 0
	s.paused, s.demo, s.demoError = false, nil, ""
	s.waiting = nil
	s.boarded, s.totalWaitTicks, s.maxWaitTicks = 0, 0, 0
	s.redistribution, s.demandWeights = false, nil
	s.nextRedistributionTick = 0
	s.passengerDistanceMeters, s.emptyDistanceMeters, s.rebalanceMoves, s.sharedParties = 0, 0, 0, 0
	s.owners = make(map[resource]string)
	s.vehicles = nil
	for _, p := range s.initial {
		station, _ := s.station(p.StationID)
		berth, _ := station.berth(p.BerthID)
		node, _ := s.network.Node(berth.Node)
		pod := Pod{ID: p.ID, Position: node.Position, Activity: Idle, StationID: station.ID, BerthID: berth.ID}
		s.vehicles = append(s.vehicles, vehicle{Pod: pod, pending: -1, reservedThrough: -1})
		s.owners[resource{kind: berthResource, id: berth.ID}] = p.ID
		s.owners[resource{kind: nodeResource, id: berth.Node}] = p.ID
	}
}

// Snapshot does not expose mutable simulation storage.
func (s *Simulation) Snapshot() Snapshot {
	state := Snapshot{
		Submitted: s.requestID, Tick: s.tick, Paused: s.paused,
		Completed: s.completed, Demo: s.demo != nil, DemoError: s.demoError,
		Wait: s.waitStats(), PassengerDistanceMeters: s.passengerDistanceMeters,
		EmptyDistanceMeters: s.emptyDistanceMeters, RebalanceMoves: s.rebalanceMoves,
		SharedParties: s.sharedParties, SharedRidePartyLimit: s.sharedRidePartyLimit,
	}
	for _, trip := range s.waiting {
		state.Pending = append(state.Pending, trip.request)
	}
	for _, v := range s.vehicles {
		cloned := v.Vehicle
		cloned.Route = cloneLanes(cloned.Route)
		if cloned.Request != nil {
			cloned.Request = new(*cloned.Request)
		}
		state.Vehicles = append(state.Vehicles, cloned)
	}
	state.Berths = s.berthStates()
	return state
}

// SafetyObservation does not expose mutable simulation storage.
func (s *Simulation) SafetyObservation() SafetyObservation {
	state := SafetyObservation{
		Tick: s.tick, Completed: s.completed, Pending: len(s.waiting),
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
				ReservedBy: s.owners[resource{kind: berthResource, id: berth.ID}],
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
	if v.Pod.Activity != Idle || s.assigned(v.Pod.ID) {
		return ErrBusy
	}
	from, _ := s.station(v.Pod.StationID)
	to, ok := s.station(destination)
	if !ok {
		return fmt.Errorf("unknown destination %q", destination)
	}
	if from.ParkingOnly || to.ParkingOnly {
		return errors.New("parking stations do not serve passenger requests")
	}
	if from.ID == to.ID {
		return ErrSameStation
	}
	origin, _ := from.berth(v.Pod.BerthID)
	route, err := s.stationApproachRoute(origin.Node, to.ID)
	if err != nil {
		return fmt.Errorf("route %s to %s: %w", from.Name, to.Name, err)
	}
	s.requestID++
	return s.board(v, waitingTrip{request: Request{ID: s.requestID, From: from.ID, To: to.ID, PartySize: 1, RequestedTick: s.tick}, route: route})
}

func (s *Simulation) findVehicle(id string) *vehicle {
	for i := range s.vehicles {
		if s.vehicles[i].Pod.ID == id {
			return &s.vehicles[i]
		}
	}
	return nil
}

// Step plans admission from pre-movement state, arbitrates, then moves every pod.
func (s *Simulation) Step() {
	if s.paused {
		return
	}
	s.tick++
	s.stepDemo()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.phaseTicks > 0 {
			v.phaseTicks--
		}
		if v.Pod.Activity == Unloading && v.phaseTicks == 0 {
			v.Pod.Activity, v.Pod.Occupied = Idle, false
			v.Request.Completed = true
			s.completed += max(1, v.Parties)
			v.Parties = 0
		}
	}
	s.dispatch()
	s.redistribute()
	s.admit()
	s.clearBlockedBerths()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if (v.Pod.Activity == Boarding || v.Pod.Activity == DepartingEmpty) && v.phaseTicks == 0 && v.reservedThrough >= 0 {
			v.Pod.Occupied = v.Pod.Activity == Boarding
			v.Pod.Activity = Traveling
			v.Pod.StationID, v.Pod.BerthID = "", ""
			continue
		}
		if v.Pod.Activity == Traveling {
			s.moveAndMeasure(v)
		}
	}
	// No pod can reuse resources released during this tick until the next tick.
	s.releaseCleared()
}

func (s *Simulation) arrive(v *vehicle) {
	station, _ := s.station(v.destinationStation)
	berth := v.destination
	node, _ := s.network.Node(berth.Node)
	v.Pod = Pod{ID: v.Pod.ID, Position: node.Position, Activity: Unloading, StationID: station.ID, BerthID: berth.ID, Occupied: true}
	v.phaseTicks = unloadingTicks
	if v.RelocatingTo != "" {
		v.Pod.Activity, v.Pod.Occupied = Idle, false
		v.phaseTicks, v.RelocatingTo = 0, ""
		if v.Rebalancing {
			v.rebalanceAfter = s.tick + redistributionCooldownTicks
			v.Rebalancing = false
		}
	}
}
