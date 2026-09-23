package sim

import "fmt"

const (
	// maxSavedPods is the largest fleet that a saved state can hold. It is the
	// project fleet limit.
	maxSavedPods = 200
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
	ID             int    `json:"id"`
	From           string `json:"from"`
	To             string `json:"to"`
	PartySize      int    `json:"partySize"`
	PodID          string `json:"podID,omitempty"`
	Completed      bool   `json:"completed,omitzero"`
	RequestedTick  int64  `json:"requestedTick"`
	DispatchReason string `json:"dispatchReason,omitempty"`
}

// SavedPod is a saved pod. A route holds indexes into Network.Lanes.
type SavedPod struct {
	ID string `json:"id"`
	// Activity is idle, departing, boarding, traveling or unloading.
	Activity           string        `json:"activity"`
	StationID          string        `json:"stationID,omitempty"`
	BerthID            string        `json:"berthID,omitempty"`
	Occupied           bool          `json:"occupied,omitzero"`
	Request            *SavedRequest `json:"request,omitzero"`
	Parties            int           `json:"parties,omitzero"`
	RelocatingTo       string        `json:"relocatingTo,omitempty"`
	Rebalancing        bool          `json:"rebalancing,omitzero"`
	RebalanceAfter     int64         `json:"rebalanceAfter,omitzero"`
	PhaseTicks         int           `json:"phaseTicks,omitzero"`
	Origin             string        `json:"origin,omitempty"`
	Destination        string        `json:"destination,omitempty"`
	DestinationStation string        `json:"destinationStation,omitempty"`
	// ClaimsDestination is true when a relocating pod holds its destination
	// berth.
	ClaimsDestination bool `json:"claimsDestination,omitzero"`
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
}

// SavedTrip is a saved queued order. A route holds indexes into
// Network.Lanes.
type SavedTrip struct {
	Request SavedRequest `json:"request"`
	Route   []int        `json:"route,omitempty"`
	// Parties is 0 for a new order, or the party count of a requeued trip.
	Parties    int    `json:"parties,omitzero"`
	DeferUntil int64  `json:"deferUntil,omitzero"`
	DeferCheck int64  `json:"deferCheck,omitzero"`
	DeferPodID string `json:"deferPodID,omitempty"`
}

// RestoreTier names the method that RestoreState used.
type RestoreTier string

// RestorePhysical keeps each pod where the saved state puts it.
const RestorePhysical RestoreTier = "physical"

// RestoreStateInput holds the network and the fleet of the saved simulation,
// and its saved state.
type RestoreStateInput struct {
	Network Network
	Fleet   []Placement
	State   SavedState
}

// RestoreResult tells how RestoreState rebuilt the simulation.
type RestoreResult struct {
	Tier RestoreTier
	// Demoted lists the pods that the physical tier moved to a berth.
	Demoted []string
	// Requeued lists the requests that went back to the queue.
	Requeued []int
	// Dropped lists the requests that the restore removed because they were
	// not valid.
	Dropped []int
	// DroppedParties counts the parties of the dropped requests.
	DroppedParties int
	// OverCap counts the routes that were longer than their limit.
	OverCap int
	// OverBudget counts the routes that did not fit in the block budget of the
	// restore. A pod that lost its route and then could not board again at a
	// berth counts two times.
	OverBudget int
	// PhysicalError tells why the physical tier failed. It is nil when the
	// tier is physical.
	PhysicalError error
}

// RestoreState rebuilds a running simulation from a saved state. The network
// and the fleet must be the ones that the saved simulation used. It returns
// an error when the physical tier fails.
func RestoreState(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	s, result, err := restorePhysical(input)
	if err != nil {
		return nil, RestoreResult{PhysicalError: err}, fmt.Errorf("restore the saved simulation: %w", err)
	}
	return s, result, nil
}

// ExportState copies the state that RestoreState needs. The result shares no
// storage with s. Call it only while no other goroutine uses s.
//
// A traveling pod saves only the part of its route that can still hold a
// resource. A route that is longer than its limit is not saved. RestoreState
// then moves the pod to a berth. A queued trip without its route also loses
// its pod bindings.
func (s *Simulation) ExportState() SavedState {
	state := SavedState{
		Tick: s.tick, Paused: s.paused, Completed: s.completed, RequestID: s.requestID, Boarded: s.boarded,
		TotalWaitTicks: s.totalWaitTicks, MaxWaitTicks: s.maxWaitTicks, NextRedistributionTick: s.nextRedistributionTick,
		PassengerDistanceMeters: s.passengerDistanceMeters, EmptyDistanceMeters: s.emptyDistanceMeters,
		RebalanceMoves: s.rebalanceMoves, SharedParties: s.sharedParties, SharedRidePartyLimit: s.sharedRidePartyLimit,
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
			Request: SavedRequest(trip.request), Route: s.laneIndexes(trip.route, limits.trip), Parties: trip.parties,
			DeferUntil: trip.deferUntil, DeferCheck: trip.deferCheck, DeferPodID: trip.deferPodID,
		}
		if saved.Route == nil && len(trip.route) > 0 {
			// A restore clears the pod bindings of a trip whose route is too
			// long, so the saved trip does not keep them.
			saved.Request.PodID, saved.DeferCheck, saved.DeferPodID = "", 0, ""
		}
		state.Waiting[index] = saved
	}
	return state
}

func (s *Simulation) exportPod(v *vehicle, limits routeLimits) SavedPod {
	pod := SavedPod{
		ID: v.Pod.ID, Activity: activityCode(v.Pod.Activity), StationID: v.Pod.StationID, BerthID: v.Pod.BerthID,
		Occupied: v.Pod.Occupied, Parties: v.Parties, RelocatingTo: v.RelocatingTo, Rebalancing: v.Rebalancing,
		RebalanceAfter: v.rebalanceAfter, PhaseTicks: v.phaseTicks, Origin: v.origin.ID,
		Destination: v.destination.ID, DestinationStation: v.destinationStation,
		ClaimsDestination: v.RelocatingTo != "" && v.destination.ID != "" &&
			s.owners[resource{kind: berthResource, id: v.destination.ID}] == v.Pod.ID,
		LaneID: v.Pod.LaneID, LaneDistance: v.Pod.LaneDistance, Waiting: v.pending >= 0,
	}
	if v.Request != nil {
		pod.Request = new(SavedRequest(*v.Request))
	}
	if pod.Waiting {
		pod.WaitSince = v.waitSince
	}
	switch v.Pod.Activity {
	case Boarding, DepartingEmpty:
		pod.Route = s.laneIndexes(v.Route, limits.pod)
	case Traveling:
		if len(v.blocks) == 0 {
			break
		}
		start, offset, current := v.savedRouteStart()
		if pod.Route = s.laneIndexes(v.Route[start:], limits.pod); pod.Route != nil {
			pod.RouteIndex = current - start
			pod.Distance = v.distance - offset
		}
	default:
		// An idle or unloading pod keeps the route of its last journey only
		// for display.
	}
	return pod
}

// savedRouteStart returns the first route index that a restore of a traveling
// pod needs, the route distance at the start of that lane, and the route
// index of the current block. A pod releases each resource of a lane at most
// Clearance past the lane end, so an earlier lane holds no resource.
func (v *vehicle) savedRouteStart() (start int, offset float64, current int) {
	start = -1
	for index, b := range v.blocks[:v.blockIndex+1] {
		if index > 0 && b.cell == 0 {
			current++
		}
		if start < 0 && b.last && b.end+Clearance > v.distance {
			start, offset = current, b.laneStart
		}
	}
	if start < 0 {
		start, offset = current, v.blocks[v.blockIndex].laneStart
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
var savedActivities = [...]Activity{Idle, DepartingEmpty, Boarding, Traveling, Unloading}

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

// carriesPassengers reports whether a pod carries parties that have not
// arrived. After a journey, a pod keeps its completed request, so the request
// alone does not tell.
func (v *vehicle) carriesPassengers() bool {
	return v.Request != nil && !v.Request.Completed && (v.Pod.Activity == Boarding || v.Pod.Occupied)
}

// carriesPassengers is the same rule for a saved pod.
func (pod SavedPod) carriesPassengers() bool {
	return pod.Request != nil && !pod.Request.Completed && (pod.Activity == activityCode(Boarding) || pod.Occupied)
}

// ordersGap returns the parties that are submitted but not completed, queued
// or in a pod. It counts parties, because a shared ride puts several orders
// into one request. It is 0 in a live simulation.
func (s *Simulation) ordersGap() int {
	gap := s.requestID - s.completed
	for _, trip := range s.waiting {
		gap -= trip.partyCount()
	}
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.carriesPassengers() {
			gap -= max(1, v.Parties)
		}
	}
	return gap
}

// ordersGap is the same count for a saved state.
func (state SavedState) ordersGap() int {
	gap := state.RequestID - state.Completed
	for _, trip := range state.Waiting {
		gap -= max(1, trip.Parties)
	}
	for _, pod := range state.Pods {
		if pod.carriesPassengers() {
			gap -= max(1, pod.Parties)
		}
	}
	return gap
}
