package sim

import (
	"errors"
	"fmt"
	"slices"
)

const (
	// MaxSavedWaitingTrips keeps the current supported-profile restore queue bound.
	MaxSavedWaitingTrips = 2600
	// maxSavedPods is the largest fleet that a saved state can hold. It is the
	// project fleet limit. TestMaximalRequeueRoundTrip checks both limits.
	maxSavedPods = 300
	// maxSavedText is the largest saved demo error or dispatch reason in bytes.
	maxSavedText = 1 << 10
)

// SavedState is the part of a running simulation that RestoreState needs. It
// does not hold the network, the fleet or the settings that the session
// applies again.
type SavedState struct {
	Tick                    int64   `json:"tick"`
	Paused                  bool    `json:"paused,omitzero"`
	Completed               int     `json:"completed"`
	RequestID               int     `json:"requestID"`
	Boarded                 int     `json:"boarded"`
	TotalWaitTicks          int64   `json:"totalWaitTicks"`
	MaxWaitTicks            int64   `json:"maxWaitTicks"`
	NextRedistributionTick  int64   `json:"nextRedistributionTick"`
	PassengerDistanceMeters float64 `json:"passengerDistanceMeters"`
	EmptyDistanceMeters     float64 `json:"emptyDistanceMeters"`
	RebalanceMoves          int     `json:"rebalanceMoves"`
	SharedParties           int     `json:"sharedParties"`
	SharedRidePartyLimit    int     `json:"sharedRidePartyLimit"`
	// SharedRideMode and SharedRideMaxStops are the settings of
	// SetSharedRideMode. An empty mode restores as DefaultSharedRideMode,
	// and a zero limit restores as DefaultSharedRideMaxStops.
	SharedRideMode     SharedRideMode `json:"sharedRideMode,omitempty"`
	SharedRideMaxStops int            `json:"sharedRideMaxStops,omitzero"`
	// SharedRideJoin is the setting of SetSharedRideJoin. An empty policy
	// restores as DefaultSharedRideJoin.
	SharedRideJoin SharedRideJoin `json:"sharedRideJoin,omitempty"`
	// Journeys counts the parties that left a pod at their destination.
	// The journey and distance totals count the same parties.
	Journeys             int     `json:"journeys,omitzero"`
	TotalJourneyTicks    int64   `json:"totalJourneyTicks,omitzero"`
	MaxJourneyTicks      int64   `json:"maxJourneyTicks,omitzero"`
	RiderDistanceMeters  float64 `json:"riderDistanceMeters,omitzero"`
	DirectDistanceMeters float64 `json:"directDistanceMeters,omitzero"`
	MaxDetourRatio       float64 `json:"maxDetourRatio,omitzero"`
	// Demo is nil when the traffic demo does not run.
	Demo      *SavedDemo `json:"demo,omitzero"`
	DemoError string     `json:"demoError,omitempty"`
	// Pods are in pod ID order.
	Pods    []SavedPod  `json:"pods"`
	Waiting []SavedTrip `json:"waiting,omitempty"`
}

// SavedDemo is the progress of the traffic demo.
type SavedDemo struct {
	SecondSent    bool `json:"secondSent,omitzero"`
	FollowupsSent bool `json:"followupsSent,omitzero"`
}

// SavedRequest is a saved passenger order. It has the same fields as Request.
type SavedRequest struct {
	SharingConsent  SharingConsent `json:"sharingConsent,omitempty"`
	Service         ServiceChoice  `json:"service,omitempty"`
	ServiceID       string         `json:"serviceID,omitempty"`
	LegacyPartySize bool           `json:"legacyPartySize,omitzero"`
	ID              int            `json:"id"`
	From            string         `json:"from"`
	To              string         `json:"to"`
	PartySize       int            `json:"partySize"`
	PodID           string         `json:"podID,omitempty"`
	Completed       bool           `json:"completed,omitzero"`
	RequestedTick   int64          `json:"requestedTick"`
	BoardedTick     int64          `json:"boardedTick,omitzero"`
	DispatchReason  string         `json:"dispatchReason,omitempty"`
}

// SavedPod is a saved pod. A route holds indexes into Network.Lanes.
type SavedPod struct {
	Class        VehicleClass `json:"class,omitempty"`
	LegacyCohort bool         `json:"legacyCohort,omitzero"`
	ID           string       `json:"id"`
	// Activity is idle, departing, boarding, traveling, unloading or
	// continuing.
	Activity           string `json:"activity"`
	StationID          string `json:"stationID,omitempty"`
	BerthID            string `json:"berthID,omitempty"`
	Occupied           bool   `json:"occupied,omitzero"`
	RelocatingTo       string `json:"relocatingTo,omitempty"`
	Rebalancing        bool   `json:"rebalancing,omitzero"`
	RebalanceAfter     int64  `json:"rebalanceAfter,omitzero"`
	PhaseTicks         int    `json:"phaseTicks,omitzero"`
	Origin             string `json:"origin,omitempty"`
	Destination        string `json:"destination,omitempty"`
	DestinationStation string `json:"destinationStation,omitempty"`
	// Riders and Stops are the riders and the stops of the pod. See
	// Vehicle.
	Riders []SavedRequest `json:"riders,omitempty"`
	Stops  []string       `json:"stops,omitempty"`
	// RiddenMeters is the distance that the riders rode before Distance.
	RiddenMeters float64 `json:"riddenMeters,omitzero"`
	// JourneyOrigin is the berth where the riders boarded. It is empty when
	// the riders boarded at Origin.
	JourneyOrigin string `json:"journeyOrigin,omitempty"`
	// ClaimsDestination is true when a relocating pod holds its destination
	// berth.
	ClaimsDestination bool `json:"claimsDestination,omitzero"`
	// Released is true for an empty pod that dispatch released from a
	// pickup order. Such a pod can divert at once. A restore ignores it for
	// a pod that dispatch cannot release: an occupied pod, a pod with no
	// station to relocate to, a rebalancing pod, or a pod that is not
	// traveling or departing empty.
	Released bool `json:"released,omitzero"`
	// StationBuffered preserves a pending or physical buffer membership.
	// Versions 3 and 4 accept this flag.
	StationBuffered bool `json:"stationBuffered,omitzero"`
	// Route holds the lanes that the pod still needs. The route of a traveling
	// pod starts at the first lane that can still hold a resource, and
	// RouteIndex and Distance count from the start of that lane.
	Route        []int   `json:"route,omitempty"`
	RouteIndex   int     `json:"routeIndex,omitzero"`
	LaneID       string  `json:"laneID,omitempty"`
	LaneDistance float64 `json:"laneDistance,omitzero"`
	Distance     float64 `json:"distance,omitzero"`
	// Waiting is true when the pod waits for track admission since WaitSince.
	Waiting   bool  `json:"waiting,omitzero"`
	WaitSince int64 `json:"waitSince,omitzero"`
	// Platoon is the link of a traveling pod to its predecessor in a
	// platoon, or nil. The restore couples the pods again before it places
	// them, because a follower can hold cells of its predecessor.
	Platoon *SavedPlatoonLink `json:"platoon,omitzero"`
	// CompactQueue is the head-only retained compact-v1 physical certificate.
	CompactQueue *SavedCompactQueue `json:"compactQueue,omitzero"`
}

// SavedPlatoonLink is the saved link of a traveling pod to its predecessor
// in a platoon. The link certifies a run of lanes that both routes share.
// The restore checks the link against the network and the pods, and it
// does not plan the link again.
type SavedPlatoonLink struct {
	// Kind is empty for a complete-lane run, or buffer for a fixed entry run.
	Kind string `json:"kind,omitempty"`
	// TerminalCell is the fixed stopping-frontier cell of a buffer run.
	TerminalCell *int `json:"terminalCell,omitzero"`
	// Leader is the ID of the predecessor.
	Leader string `json:"leader"`
	// Lane and LeaderLane are the indexes of the first lane of the run in
	// the saved route of the pod and in the saved route of the predecessor.
	// Lanes is the number of lanes in the run.
	Lane       int `json:"lane"`
	LeaderLane int `json:"leaderLane"`
	Lanes      int `json:"lanes"`
	// Turn is the largest total turn in radians of the run from the lane of
	// the pod. All links of one platoon have the same turn. The clearance
	// of the link follows from it.
	Turn float64 `json:"turn"`
	// Draining is true when the pod reserved the end block of the run. The
	// pod then makes no more coupled grants.
	Draining bool `json:"draining,omitzero"`
}

// SavedTrip is a saved queued order. A route holds indexes into
// Network.Lanes.
type SavedTrip struct {
	Request SavedRequest `json:"request"`
	Route   []int        `json:"route,omitempty"`
	// Boarded is true for a rider that a restore queued again. Its wait is
	// already recorded.
	Boarded    bool   `json:"boarded,omitzero"`
	DeferUntil int64  `json:"deferUntil,omitzero"`
	DeferCheck int64  `json:"deferCheck,omitzero"`
	DeferPodID string `json:"deferPodID,omitempty"`
}

// RestoreTier names the method that RestoreState used.
type RestoreTier string

const (
	// RestorePhysical keeps each pod where the saved state puts it.
	RestorePhysical RestoreTier = "physical"
	// RestoreLogical puts each fleet pod at its initial berth. The riders
	// that were on their way go back to the queue.
	RestoreLogical RestoreTier = "logical"
)

// RestoreStateInput holds the network and the fleet of the saved simulation,
// and its saved state.
type RestoreStateInput struct {
	ExpressServices []ExpressService
	Network         Network
	Fleet           []Placement
	State           SavedState
	// LogicalOnly makes RestoreState skip the physical tier.
	LogicalOnly bool
	// StationBuffers selects the version 3 physical buffer contract.
	// It does not enable new buffer admissions after restoration.
	StationBuffers bool
	// BufferPlatoons selects the version 4 fixed entry certificate contract.
	// It does not enable formations or buffer admissions after restoration.
	BufferPlatoons bool
	// CompactQueues selects the save-6 compact certificate contract.
	CompactQueues bool
	// StationQueueSpacing selects the restored runtime policy, ordinary by default.
	StationQueueSpacing StationQueueSpacing
	// PlatoonLimit validates anticipatory capacity. Zero selects the default.
	PlatoonLimit int
}

// RestoreResult tells how RestoreState rebuilt the simulation.
type RestoreResult struct {
	Tier RestoreTier
	// Demoted lists the pods that the physical tier moved to a berth.
	Demoted []string
	// Requeued lists the requests that went back to the queue.
	Requeued []int
	// LogicalCompleted lists logical auto-completions without an actual
	// alighting tick. It does not include physical completions.
	LogicalCompleted []int
	// Dropped lists the requests that the restore removed because they were
	// not valid.
	Dropped []int
	// DroppedParties counts the dropped requests. Each request is one
	// party.
	DroppedParties int
	// Unaccounted counts the orders that the saved state submitted but that
	// are not complete, not in the queue and not aboard a pod. The restore
	// does not make up these orders. A state that the simulation saves after
	// the restore has them again, with the dropped orders.
	Unaccounted int
	// OverCap counts the routes that were longer than their limit.
	OverCap int
	// OverBudget counts the routes that did not fit in the block budget of the
	// restore. A pod that lost its route and then could not board again at a
	// berth counts two times.
	OverBudget int
	// PhysicalError tells why the physical tier failed. It is nil when the
	// tier is physical, and when LogicalOnly made RestoreState skip the
	// physical tier.
	PhysicalError error
}

// RestoreState rebuilds a running simulation from a saved state. The network
// and the fleet must be the ones that the saved simulation used. It tries the
// physical tier first. When that tier fails, or when input.LogicalOnly is
// set, it uses the logical tier. It returns an error only when the last tier
// that it tries fails. The error then wraps the error of each tier that it
// tried. Invalid version 4 buffer certificates return an error without a
// logical fallback. LogicalOnly still validates those certificates physically.
// Compact certificates require physical restore and reject logical conversion.
func RestoreState(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	return restoreState(input, func() (*Simulation, error) { return NewFleet(input.Network, input.Fleet) })
}

func restoreState(input RestoreStateInput, newFleet func() (*Simulation, error)) (*Simulation, RestoreResult, error) {
	registry, serviceErr := validatedExpressServices(input.Network, newRouteGraph(input.Network), input.ExpressServices)
	if serviceErr != nil {
		return nil, RestoreResult{}, serviceErr
	}
	for _, trip := range input.State.Waiting {
		if err := serviceMatches(registry, Request(trip.Request).options()); err != nil {
			return nil, RestoreResult{}, err
		}
	}
	for _, pod := range input.State.Pods {
		for _, rider := range pod.Riders {
			if err := serviceMatches(registry, Request(rider).options()); err != nil {
				return nil, RestoreResult{}, err
			}
		}
	}
	if err := checkSavedBankRoutes(input); err != nil {
		return nil, RestoreResult{}, err
	}
	if err := checkCompactFields(input); err != nil {
		return nil, RestoreResult{}, err
	}
	if err := checkBufferLinkFields(input); err != nil {
		return nil, RestoreResult{}, err
	}
	var physicalErr error
	bufferCertificate := hasBufferCertificate(input.State)
	if !input.LogicalOnly || bufferCertificate {
		s, result, err := restorePhysical(input, newFleet)
		if err == nil && bufferCertificate {
			err = checkRestoredBufferMembers(input.State, result)
		}
		if err == nil && input.LogicalOnly && hasCompactCertificate(input.State) {
			err = errors.New("compact certificate cannot preserve physical recovery in a logical-only conversion")
		}
		if err == nil && !input.LogicalOnly {
			return s, result, nil
		}
		physicalErr = err
		if err != nil && bufferCertificate {
			err = fmt.Errorf("%w: %w", errBufferCertificate, err)
			return nil, RestoreResult{PhysicalError: err}, err
		}
	}
	s, result, err := restoreLogical(input, newFleet)
	switch {
	case err == nil:
		result.PhysicalError = physicalErr
		return s, result, nil
	case physicalErr != nil:
		return nil, RestoreResult{PhysicalError: physicalErr},
			fmt.Errorf("restore the saved simulation: physical tier: %w, logical tier: %w", physicalErr, err)
	default:
		return nil, RestoreResult{}, fmt.Errorf("restore the saved simulation: logical tier: %w", err)
	}
}

// ExportState copies the state that RestoreState needs. The result shares no
// storage with s. Call it only while no other goroutine uses s.
//
// A traveling pod saves only the part of its route that can still hold a
// resource. A route that is longer than its limit is not saved. RestoreState
// then moves the pod to a berth. A queued trip without its route also loses
// its pod bindings, and an empty pod on its way to that pickup is saved as
// released.
func (s *Simulation) ExportState() SavedState {
	state := SavedState{
		Tick: s.tick, Paused: s.paused, Completed: s.completed, RequestID: s.requestID, Boarded: s.boarded,
		TotalWaitTicks: s.totalWaitTicks, MaxWaitTicks: s.maxWaitTicks, NextRedistributionTick: s.nextRedistributionTick,
		PassengerDistanceMeters: s.passengerDistanceMeters, EmptyDistanceMeters: s.emptyDistanceMeters,
		RebalanceMoves: s.rebalanceMoves, SharedParties: s.sharedParties, SharedRidePartyLimit: s.sharedRidePartyLimit,
		SharedRideMode: s.sharedRideMode, SharedRideMaxStops: s.sharedRideMaxStops, SharedRideJoin: s.sharedRideJoin,
		Journeys: s.journeys, TotalJourneyTicks: s.totalJourneyTicks, MaxJourneyTicks: s.maxJourneyTicks,
		RiderDistanceMeters: s.riderDistanceMeters, DirectDistanceMeters: s.directDistanceMeters, MaxDetourRatio: s.maxDetourRatio,
		DemoError: s.demoError, Pods: make([]SavedPod, len(s.vehicles)),
	}
	if s.demo != nil {
		state.Demo = &SavedDemo{SecondSent: s.demo.secondSent, FollowupsSent: s.demo.followupsSent}
	}
	limits := newRouteLimits(s.network)
	for index := range s.vehicles {
		state.Pods[index] = s.exportPod(&s.vehicles[index], limits)
	}
	if len(s.waiting) > 0 {
		state.Waiting = make([]SavedTrip, len(s.waiting))
	}
	for index, trip := range s.waiting {
		saved := SavedTrip{
			Request: SavedRequest(trip.request), Route: s.laneIndexes(trip.route, limits.trip), Boarded: trip.boarded,
			DeferUntil: trip.deferUntil, DeferCheck: trip.deferCheck, DeferPodID: trip.deferPodID,
		}
		if saved.Route == nil && len(trip.route) > 0 {
			// A restore clears the pod bindings of a trip whose route is too
			// long, so the saved trip does not keep them. The pod on its way
			// to the pickup is saved as released, so that it can take new
			// work after the restore.
			for index := range s.vehicles {
				if v := &s.vehicles[index]; v.Pod.ID == trip.request.PodID && releasable(v) {
					state.Pods[index].Released = true
				}
			}
			saved.Request.PodID, saved.DeferCheck, saved.DeferPodID = "", 0, ""
		}
		state.Waiting[index] = saved
	}
	return state
}

func (s *Simulation) exportPod(v *vehicle, limits routeLimits) SavedPod {
	pod := SavedPod{
		ID: v.Pod.ID, Class: v.Pod.Class, LegacyCohort: v.LegacyCohort, Activity: activityCode(v.Pod.Activity), StationID: v.Pod.StationID, BerthID: v.Pod.BerthID,
		Occupied: v.Pod.Occupied, RelocatingTo: v.RelocatingTo, Rebalancing: v.Rebalancing,
		RebalanceAfter: v.rebalanceAfter, PhaseTicks: v.phaseTicks, Origin: v.origin.ID,
		Destination: v.destination.ID, DestinationStation: v.destinationStation,
		ClaimsDestination: v.RelocatingTo != "" && v.destination.ID != "" &&
			s.owners[resource{kind: berthResource, id: v.destination.ID}] == v.Pod.ID,
		LaneID: v.Pod.LaneID, LaneDistance: v.Pod.LaneDistance, Waiting: v.pending >= 0, Released: v.released, StationBuffered: v.buffered,
	}
	for _, rider := range v.Riders {
		pod.Riders = append(pod.Riders, SavedRequest(rider))
	}
	pod.Stops = slices.Clone(v.Stops)
	if v.carriesPassengers() {
		pod.RiddenMeters = v.riddenMeters()
		if v.journeyOrigin.ID != v.origin.ID {
			pod.JourneyOrigin = v.journeyOrigin.ID
		}
	}
	if pod.Waiting {
		pod.WaitSince = v.waitSince
	}
	switch v.Pod.Activity {
	case Boarding, DepartingEmpty, Continuing:
		pod.Route = s.laneIndexes(v.Route, limits.pod)
	case Traveling:
		if v.blocks.len() == 0 {
			break
		}
		start, offset, current := s.savedStart(v)
		if pod.Route = s.laneIndexes(v.Route[start:], limits.pod); pod.Route != nil {
			pod.RouteIndex = current - start
			pod.Distance = v.distance - offset
			if s.compactGroup(v) != nil {
				pod.Distance = v.Pod.LaneDistance
			}
			if v.carriesPassengers() {
				pod.RiddenMeters = v.riddenBase + offset
			}
			pod.Platoon = s.savedLink(v, start)
			pod.CompactQueue = s.savedCompactQueue(v)
		}
	default:
		// An idle or unloading pod keeps the route of its last journey only
		// for display.
	}
	return pod
}

// savedLink returns the saved link of the traveling pod v, whose saved
// route starts at route index start, or nil. The saved route of v can start
// after the first lane of the run, so the saved run starts at the first
// lane of the run that the saved route holds. savedStart keeps that lane
// in the saved route of the predecessor. The end of the run does not
// change. When the predecessor stopped traveling or no lane of the run is
// left, the follower holds no resource of a pod ahead, and its link ends
// at the next tick. Then savedLink returns nil.
func (s *Simulation) savedLink(v *vehicle, start int) *SavedPlatoonLink {
	leader, skip, ok := s.savedRun(v, start)
	if !ok {
		return nil
	}
	leaderStart, _, _ := s.savedStart(leader)
	saved := &SavedPlatoonLink{
		Leader: leader.Pod.ID, Lane: v.link.lane + skip - start, LeaderLane: v.link.leaderLane + skip - leaderStart,
		Lanes: v.link.lanes - skip, Turn: v.link.turn, Draining: v.link.draining || v.reservedThrough >= v.link.end,
	}
	if v.link.buffer {
		saved.Kind = "buffer"
		if v.link.compact {
			saved.Kind, saved.Draining = "compact-buffer-v1", false
		}
		saved.TerminalCell = new(v.link.terminalCell)
	}
	return saved
}

// savedRun returns the predecessor of the traveling pod v, whose saved route
// starts at route index start, and the number of lanes at the start of the
// run that the saved route does not hold. It reports false when v has no
// saved link.
func (s *Simulation) savedRun(v *vehicle, start int) (leader *vehicle, skip int, ok bool) {
	if v.link.leader == 0 {
		return nil, 0, false
	}
	leader = &s.vehicles[v.link.leader-1]
	skip = max(0, start-v.link.lane)
	return leader, skip, leader.Pod.Activity == Traveling && leader.blocks.len() > 0 && skip < v.link.lanes
}

// savedStart returns the values of savedRouteStart for the traveling pod v,
// with an earlier start when the saved link of its follower needs it. The
// saved run of the follower starts at the first lane of the run that the
// saved route of the follower holds. The predecessor can have passed that
// lane, but a junction section of the lane can be in its next lane too,
// and the follower can share it. So the saved route of the predecessor
// starts at that lane at the latest.
func (s *Simulation) savedStart(v *vehicle) (start int, offset float64, current int) {
	start, offset, current = v.savedRouteStart()
	if v.link.leader != 0 && v.link.buffer && v.link.lane < start {
		start = v.link.lane
		offset = v.blocks.lanes[start].start
	}
	if v.follower == 0 {
		return start, offset, current
	}
	follower := &s.vehicles[v.follower-1]
	if follower.Pod.Activity != Traveling || follower.blocks.len() == 0 {
		return start, offset, current
	}
	followerStart, _, _ := s.savedStart(follower)
	if _, skip, ok := s.savedRun(follower, followerStart); ok && follower.link.leaderLane+skip < start {
		start = follower.link.leaderLane + skip
		offset = v.blocks.lanes[start].start
	}
	return start, offset, current
}

// savedRouteStart returns the first route index that a restore of a traveling
// pod needs, the route distance at the start of that lane, and the route
// index of the current block. A pod releases each resource of a lane at most
// Clearance past the lane end, so an earlier lane holds no resource.
func (v *vehicle) savedRouteStart() (start int, offset float64, current int) {
	start = -1
	for index, b := range v.blocks.span(0, v.blockIndex+1) {
		if index > 0 && b.cell == 0 {
			current++
		}
		if start < 0 && b.last && b.end+Clearance > v.distance {
			start, offset = current, b.laneStart
		}
	}
	if start < 0 {
		start, offset = current, v.blocks.at(v.blockIndex).laneStart
	}
	return start, offset, current
}

// routeLimits bounds the length of saved routes. A waiting trip holds one
// shortest path, which visits each node at most once, so a live trip route
// is always within its limit. A pod route can grow when a pod diverts or
// circles a full station, so its limit is larger.
type routeLimits struct{ pod, trip int }

func newRouteLimits(network Network) routeLimits {
	return routeLimits{pod: len(network.Lanes) + len(network.Nodes), trip: len(network.Nodes)}
}

// laneIndexes returns the network index of each lane of a route. It returns
// nil for an empty route and for a route with more than limit lanes.
func (s *Simulation) laneIndexes(route []Lane, limit int) []int {
	if len(route) == 0 || len(route) > limit {
		return nil
	}
	indexes := make([]int, len(route))
	for index, lane := range route {
		indexes[index] = s.graph.lanes[lane.ID]
	}
	return indexes
}

// savedActivities lists the activities that a saved state can hold.
var savedActivities = [...]Activity{Idle, DepartingEmpty, Boarding, Traveling, Unloading, Continuing}

// activityCode returns the stable code of an activity in a saved state. The
// code does not change when the display text of the activity changes.
func activityCode(activity Activity) string {
	switch activity {
	case Idle:
		return "idle"
	case DepartingEmpty:
		return "departing"
	case Boarding:
		return "boarding"
	case Traveling:
		return "traveling"
	case Unloading:
		return "unloading"
	case Continuing:
		return "continuing"
	default:
		return ""
	}
}

func activityOfCode(code string) (Activity, bool) {
	for _, activity := range savedActivities {
		if activityCode(activity) == code {
			return activity, true
		}
	}
	return "", false
}
