package sim

import (
	"math"
	"slices"
	"strings"
	"testing"
)

// phaseSample is a saved state from a live run, with the index of a pod in
// a phase of the contract.
type phaseSample struct {
	state SavedState
	pod   int
}

// livePhaseSamples runs the two stop ride and an empty pickup move, and
// returns the first saved state for each phase of the contract. The
// continuing phase lasts only while a pod waits for track, so the run makes
// pod 01 continue at once when it starts to unload at garden, and saves the
// state before the next step. The phases of an operational purpose need a
// service hold. TestOperationalPhaseMutations covers them.
func livePhaseSamples(t *testing.T) map[podPhase]phaseSample {
	t.Helper()
	samples := make(map[podPhase]phaseSample)
	record := func(s *Simulation) {
		state := s.ExportState()
		for index, pod := range state.Pods {
			phase, err := phaseOf(pod)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := samples[phase]; !ok {
				samples[phase] = phaseSample{state: roundTripState(t, state), pod: index}
			}
		}
	}
	ride := newTwoStopRide(t)
	continued := false
	for range 600 * TicksPerSecond {
		record(ride)
		if v := ride.findVehicle("01"); !continued && v.Pod.Activity == Unloading && v.Pod.StationID == "garden" {
			record(ride)
			ride.alight(v)
			ride.continueJourney(v)
			record(ride)
			continued = true
		}
		if ride.Snapshot().Completed == 2 && ride.findVehicle("01").Pod.Activity == Idle {
			record(ride)
			break
		}
		ride.Step()
	}
	pickup := newSharingSimulation(t)
	if err := pickup.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	for range 60 * TicksPerSecond {
		record(pickup)
		pickup.Step()
	}
	for phase := range phaseContinuing + 1 {
		if _, ok := samples[phase]; !ok {
			t.Fatalf("the live runs did not reach phase %d", phase)
		}
	}
	return samples
}

// podMutation changes the pod of a phase sample so that it breaks the
// phase contract.
type podMutation struct {
	name string
	edit func(state *SavedState, pod *SavedPod)
}

// otherStation returns a passenger station that is not the given station.
func otherStation(station string) string {
	if station == "harbor" {
		return "market"
	}
	return "harbor"
}

// contractMutations returns the mutations of a pod that each break the rule
// of its phase: one for each field that the rule needs or forbids.
func contractMutations(rule phaseRule, pod SavedPod) []podMutation {
	mutations := []podMutation{
		{"occupied flag flipped", func(_ *SavedState, pod *SavedPod) { pod.Occupied = !pod.Occupied }},
		{"phase above its range", func(_ *SavedState, pod *SavedPod) { pod.PhaseTicks = rule.maxPhase + 1 }},
		{"phase below its range", func(_ *SavedState, pod *SavedPod) { pod.PhaseTicks = rule.minPhase - 1 }},
		{"ridden distance not finite", func(_ *SavedState, pod *SavedPod) { pod.RiddenMeters = math.NaN() }},
		{"negative ridden distance", func(_ *SavedState, pod *SavedPod) { pod.RiddenMeters = -1 }},
		{"unknown activity", func(_ *SavedState, pod *SavedPod) { pod.Activity = "waiting" }},
	}
	add := func(name string, edit func(state *SavedState, pod *SavedPod)) {
		mutations = append(mutations, podMutation{name, edit})
	}
	if rule.active {
		add("no active rider", func(_ *SavedState, pod *SavedPod) {
			for index := range pod.Riders {
				pod.Riders[index].Completed = true
			}
		})
	} else {
		add("an active rider", func(state *SavedState, pod *SavedPod) {
			state.RequestID++
			pod.Riders = append(pod.Riders, SavedRequest{
				SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "garden", To: "market", PartySize: 1, PodID: pod.ID, RequestedTick: 1, BoardedTick: 2,
			})
		})
	}
	if !rule.history {
		add("a completed rider", func(_ *SavedState, pod *SavedPod) { pod.Riders[0].Completed = true })
	}
	if len(pod.Riders) > 0 {
		add("rider of another pod", func(_ *SavedState, pod *SavedPod) { pod.Riders[0].PodID = "99" })
		add("rider to its own station", func(_ *SavedState, pod *SavedPod) { pod.Riders[0].To = pod.Riders[0].From })
		add("boarding before the request", func(_ *SavedState, pod *SavedPod) {
			pod.Riders[0].BoardedTick = pod.Riders[0].RequestedTick - 1
		})
	}
	if len(pod.Riders) > 1 {
		add("riders from two stations", func(_ *SavedState, pod *SavedPod) {
			pod.Riders[1].From = otherStation(pod.Riders[0].From)
		})
	}
	if rule.atBerth {
		add("no station", func(_ *SavedState, pod *SavedPod) { pod.StationID = "" })
		add("no berth", func(_ *SavedState, pod *SavedPod) { pod.BerthID = "" })
	} else {
		add("a berth", func(_ *SavedState, pod *SavedPod) { pod.StationID, pod.BerthID = "harbor", "harbor-1" })
	}
	if rule.relocating {
		add("no relocation", func(_ *SavedState, pod *SavedPod) { pod.RelocatingTo = "" })
		add("relocation to another station", func(_ *SavedState, pod *SavedPod) {
			pod.RelocatingTo = otherStation(pod.DestinationStation)
		})
		add("claim without a berth", func(_ *SavedState, pod *SavedPod) { pod.ClaimsDestination, pod.Destination = true, "" })
	} else {
		add("relocation", func(_ *SavedState, pod *SavedPod) { pod.RelocatingTo = "market" })
		add("rebalancing", func(_ *SavedState, pod *SavedPod) { pod.Rebalancing = true })
		add("destination claim", func(_ *SavedState, pod *SavedPod) { pod.ClaimsDestination = true })
	}
	switch rule.stops {
	case noStops, finalStop:
		add("a stop", func(_ *SavedState, pod *SavedPod) { pod.Stops = append(pod.Stops, "garden") })
	case routeStops, laterStops, serviceStops:
		add("no stops", func(_ *SavedState, pod *SavedPod) { pod.Stops = nil })
		add("a repeated stop", func(_ *SavedState, pod *SavedPod) { pod.Stops = append(pod.Stops, pod.Stops[0]) })
		add("a stop for no rider", func(_ *SavedState, pod *SavedPod) { pod.Stops = append(pod.Stops, "parking") })
	}
	if rule.stops == laterStops {
		add("a stop at its station", func(_ *SavedState, pod *SavedPod) { pod.Stops = append(pod.Stops, pod.StationID) })
	}
	if rule.stops == routeStops {
		add("first stop not the destination", func(_ *SavedState, pod *SavedPod) {
			pod.DestinationStation = otherStation(pod.Stops[0])
		})
	}
	if rule.startsAtBerth {
		add("origin at another berth", func(_ *SavedState, pod *SavedPod) { pod.Origin = "parking-1" })
	}
	if rule.hasOrigin {
		add("no origin", func(_ *SavedState, pod *SavedPod) { pod.Origin, pod.JourneyOrigin = "", "" })
	}
	if rule.boardsHere {
		add("journey from another berth", func(_ *SavedState, pod *SavedPod) { pod.JourneyOrigin = "parking-1" })
	}
	if rule.active {
		add("no journey origin", func(_ *SavedState, pod *SavedPod) { pod.Origin, pod.JourneyOrigin = "", "" })
	}
	if rule.atDestination {
		add("no destination berth", func(_ *SavedState, pod *SavedPod) { pod.Destination = "" })
		add("destination at another station", func(_ *SavedState, pod *SavedPod) {
			pod.DestinationStation = otherStation(pod.StationID)
		})
	}
	if rule.hasDestination {
		add("no destination berth", func(_ *SavedState, pod *SavedPod) { pod.Destination, pod.ClaimsDestination = "", false })
	}
	return mutations
}

// checkRejected checks that both restore tiers reject a saved state. The
// physical tier runs first and the logical tier runs after its failure,
// and the logical tier also runs alone.
func checkRejected(t *testing.T, network Network, fleet []Placement, state SavedState, want string) {
	t.Helper()
	for _, logicalOnly := range []bool{false, true} {
		s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state, LogicalOnly: logicalOnly})
		if err == nil || s != nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("logical only %t: restore %v, %+v, want an error with %q", logicalOnly, err, result, want)
		}
	}
}

// TestPhaseContractMutations takes a saved state from a live run for each
// phase of the contract. The live state must restore. Each change to a
// field that the phase needs or forbids must make both restore tiers
// reject the state.
func TestPhaseContractMutations(t *testing.T) {
	t.Parallel()
	fleet := newSharingSimulation(t).initial
	for phase, sample := range livePhaseSamples(t) {
		if _, result, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: fleet, State: sample.state}); err != nil ||
			result.Tier != RestorePhysical {
			t.Fatalf("phase %d: the live state does not restore: %v, %+v", phase, err, result)
		}
		pod := sample.state.Pods[sample.pod]
		for _, mutation := range contractMutations(phaseRules[phase], pod) {
			state := sample.state
			state.Pods = slices.Clone(state.Pods)
			mutated := state.Pods[sample.pod]
			mutated.Riders, mutated.Stops = slices.Clone(mutated.Riders), slices.Clone(mutated.Stops)
			mutation.edit(&state, &mutated)
			state.Pods[sample.pod] = mutated
			if _, err := state.checkContract(); err == nil {
				t.Errorf("phase %d, %s: the contract accepts %+v", phase, mutation.name, mutated)
				continue
			}
			checkRejected(t, Example(), fleet, state, "pod "+pod.ID)
		}
	}
}

// finishedRide returns the saved state after the two stop ride. Pod 01 is
// idle at market and keeps riders 1 and 2 as completed history.
func finishedRide(t *testing.T) SavedState {
	t.Helper()
	s := newTwoStopRide(t)
	runTwoStopRide(t, s, nil)
	stepUntil(t, s, "pod 01 idle at market", func() bool { return s.findVehicle("01").Pod.Activity == Idle })
	return roundTripState(t, s.ExportState())
}

// boardingState returns the saved state after an order from harbor to
// market. Pod 01 boards the order at harbor.
func boardingState(t *testing.T) SavedState {
	t.Helper()
	s := newSharingSimulation(t)
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	return roundTripState(t, s.ExportState())
}

// TestRestoreRejectsBrokenOrders checks the saved states of the review
// findings. Each state holds an order that a restore could lose without a
// report, so both tiers must reject it.
func TestRestoreRejectsBrokenOrders(t *testing.T) {
	t.Parallel()
	fleet := newSharingSimulation(t).initial
	for _, test := range []struct {
		name  string
		state func(t *testing.T) SavedState
		want  string
	}{
		{
			// An idle pod does not unload, so its rider would never arrive.
			name: "idle pod with an active rider",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				pod := &state.Pods[0]
				pod.Activity, pod.Occupied, pod.PhaseTicks, pod.Route = activityCode(Idle), false, 0, nil
				return state
			},
			want: "pod 01: the pod has active rider 1",
		},
		{
			// arrive makes a relocating pod idle without alight.
			name: "boarding pod with an empty move",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				state.Pods[0].RelocatingTo = "market"
				return state
			},
			want: "pod 01: the pod has an empty move",
		},
		{
			name: "traveling pod with riders and no occupied flag",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := livePhaseSamples(t)[phaseTravelingOccupied].state
				state.Pods[0].Occupied = false
				return state
			},
			want: "pod 01: the pod has active rider 1",
		},
		{
			name: "unloading pod without the occupied flag",
			state: func(t *testing.T) SavedState {
				t.Helper()
				sample := livePhaseSamples(t)[phaseUnloadingFinal]
				sample.state.Pods[sample.pod].Occupied = false
				return sample.state
			},
			want: "occupied is false",
		},
		{
			name: "completed order in the queue again",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := finishedRide(t)
				request := state.Pods[0].Riders[0]
				request.PodID, request.Completed, request.BoardedTick = "", false, 0
				state.Waiting = append(state.Waiting, SavedTrip{Request: request})
				state.Completed, state.Journeys = state.Completed-1, state.Journeys-1
				return state
			},
			want: "order 1 is in pod 01 and in the queue",
		},
		{
			// Pod 02 keeps order 1 as completed history, and pod 01 boards it.
			name: "completed order aboard another pod",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				state.Pods[1].Riders = []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "garden", To: "market", PartySize: 1, PodID: "02", Completed: true}}
				return state
			},
			want: "order 1 is in pod 01 and in pod 02",
		},
		{
			name: "queued order that a pod carries",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				state.Waiting = append(state.Waiting, SavedTrip{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "harbor", To: "market", PartySize: 1}})
				return state
			},
			want: "order 1 is in pod 01 and in the queue",
		},
		{
			name: "order queued two times",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				trip := SavedTrip{Request: SavedRequest{SharingConsent: SharedConsent, Service: OnDemandService, ID: 2, From: "garden", To: "market", PartySize: 1}}
				state.Waiting, state.RequestID = []SavedTrip{trip, trip}, 3
				return state
			},
			want: "order 2 is in the queue and in the queue",
		},
		{
			name: "negative journey count",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := finishedRide(t)
				state.Journeys = -1
				return state
			},
			want: "negative or not finite",
		},
		{
			name: "more journeys than completed orders",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := finishedRide(t)
				state.Journeys = state.Completed + 1
				return state
			},
			want: "journey totals",
		},
		{
			name: "rider distance not finite",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := finishedRide(t)
				state.RiderDistanceMeters = math.Inf(1)
				return state
			},
			want: "negative or not finite",
		},
		{
			name: "negative detour ratio",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := finishedRide(t)
				state.MaxDetourRatio = -1
				return state
			},
			want: "negative or not finite",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkRejected(t, Example(), fleet, test.state(t), test.want)
		})
	}
}

// countUnaccounted is a test oracle for the orders of a simulation. It
// returns the submitted orders that are not complete, not in the queue and
// not aboard a pod. It fails the test when an order is in two places.
func countUnaccounted(t *testing.T, s *Simulation) int {
	t.Helper()
	var ids []int
	for _, trip := range s.waiting {
		ids = append(ids, trip.request.ID)
	}
	for index := range s.vehicles {
		for _, rider := range s.vehicles[index].Riders {
			if !rider.Completed {
				ids = append(ids, rider.ID)
			}
		}
	}
	return s.requestID - s.completed - countHeld(t, ids)
}

// countSavedUnaccounted is countUnaccounted for a saved state.
func countSavedUnaccounted(t *testing.T, state SavedState) int {
	t.Helper()
	var ids []int
	for _, trip := range state.Waiting {
		ids = append(ids, trip.Request.ID)
	}
	for _, pod := range state.Pods {
		for _, rider := range pod.Riders {
			if !rider.Completed {
				ids = append(ids, rider.ID)
			}
		}
	}
	return state.RequestID - state.Completed - countHeld(t, ids)
}

func countHeld(t *testing.T, ids []int) int {
	t.Helper()
	seen := make(map[int]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("order %d is in two places", id)
		}
		seen[id] = true
	}
	return len(ids)
}

// TestRestoreReportsUnaccountedOrders restores a state that lost orders 11
// and 12. Each tier restores it and reports the two orders. The report stays
// when the restored simulation saves and restores again. The restore does
// not make up the lost orders.
func TestRestoreReportsUnaccountedOrders(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	base := logicalState(t, f)
	base.RequestID += 2
	for _, tier := range restoreTiers {
		state := roundTripState(t, base)
		// The first restore drops order 10 from the parking station, which
		// adds to the report of each later restore.
		for round, want := range []int{2, 3, 3} {
			s, result, err := f.restoreTier(state, tier)
			if err != nil || result.Tier != tier || result.Unaccounted != want {
				t.Fatalf("%s tier, round %d: %v, %+v, want %d unaccounted orders", tier, round, err, result, want)
			}
			if n := countUnaccounted(t, s); n != 3 || s.unaccountedOrders != n {
				t.Fatalf("%s tier, round %d: %d unaccounted orders, counted %d", tier, round, n, s.unaccountedOrders)
			}
			monitorContractEachTick(t, s)
			advance(s, 30*TicksPerSecond)
			state = roundTripState(t, s.ExportState())
		}
		s, _, err := f.restoreTier(state, tier)
		if err != nil {
			t.Fatal(err)
		}
		submitted := s.requestID
		if err := s.RequestTrip("harbor", "market"); err != nil {
			t.Fatal(err)
		}
		if s.requestID != submitted+1 || countUnaccounted(t, s) != 3 {
			t.Fatalf("%s tier: %d orders, %d unaccounted", tier, s.requestID, countUnaccounted(t, s))
		}
	}
}

// TestReconcileOrdersFindsEachMismatch breaks the result of a physical
// restore of logicalState, which drops order 10.
func TestReconcileOrdersFindsEachMismatch(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	state := roundTripState(t, logicalState(t, f))
	for _, tc := range []struct {
		name string
		// edit breaks the restored simulation and returns the completed,
		// the interrupted, and the dropped orders.
		edit func(s *Simulation) (completed, interrupted, dropped []int)
		want string
	}{
		{
			name: "no change",
			edit: func(*Simulation) ([]int, []int, []int) { return nil, nil, []int{10} },
		},
		{
			name: "lost order", want: "the restore lost order 9",
			edit: func(s *Simulation) ([]int, []int, []int) {
				s.waiting = nil
				return nil, nil, []int{10}
			},
		},
		{
			name: "queued order that is also dropped", want: "order 9 is in the queue and dropped",
			edit: func(*Simulation) ([]int, []int, []int) { return nil, nil, []int{9, 10} },
		},
		{
			name: "order that the state does not hold", want: "order 11 is in the queue, but the saved state does not hold it",
			edit: func(s *Simulation) ([]int, []int, []int) {
				s.waiting = append(s.waiting, waitingTrip{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: 11, From: "harbor", To: "market", PartySize: 1}})
				return nil, nil, []int{10}
			},
		},
		{
			name: "rider aboard and complete", want: "order 2 is in pod 03 and complete",
			edit: func(*Simulation) ([]int, []int, []int) { return []int{2}, nil, []int{10} },
		},
		{
			name: "completed order of the history", want: "order 1 is complete, but the saved state does not hold it",
			edit: func(*Simulation) ([]int, []int, []int) { return []int{1}, nil, []int{10} },
		},
		{
			name: "interrupted rider",
			edit: func(s *Simulation) ([]int, []int, []int) {
				s.interruptRider(s.findVehicle("03"), 0)
				return nil, []int{2}, []int{10}
			},
		},
		{
			name: "rider aboard and interrupted", want: "order 2 is in pod 03 and interrupted",
			edit: func(*Simulation) ([]int, []int, []int) { return nil, []int{2}, []int{10} },
		},
		{
			name: "interrupted order of the history", want: "order 1 is interrupted, but the saved state does not hold it",
			edit: func(*Simulation) ([]int, []int, []int) { return nil, []int{1}, []int{10} },
		},
		{
			name: "interrupted count", want: "the restore has 1 interrupted orders, want 0",
			edit: func(s *Simulation) ([]int, []int, []int) {
				s.interrupted++
				return nil, nil, []int{10}
			},
		},
		{
			name: "completed count", want: "the restore has 10 submitted and 3 completed orders, want 10 and 2",
			edit: func(s *Simulation) ([]int, []int, []int) {
				s.completed++
				return nil, nil, []int{10}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, result, err := f.restore(state)
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("%v, %+v", err, result)
			}
			completed, interrupted, dropped := tc.edit(s)
			err = s.reconcileOrders(state, completed, interrupted, dropped)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || err.Error() != tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
}

// contractCheckTicks is the interval of the sampled contract check, one
// simulated second. The check exports the whole state, so a check after
// each tick took about a third of the time of the long monitored tests.
// Intervals of 30 to 120 ticks took about the same time.
const contractCheckTicks = TicksPerSecond

// contractSampler decides when a monitor checks the contract. It checks at
// the first observation, every contractCheckTicks ticks, and at each event
// boundary: an observation that is not the next tick (a command, a reset,
// a restore, or a Step of a paused simulation), or a tick in which a count
// of the simulation or the phase of a pod changes.
type contractSampler struct {
	every    int64
	observed bool
	tick     int64
	counts   contractCounts
	pods     []contractPhase
}

// contractCounts holds the counts of a simulation that an event changes.
type contractCounts struct {
	pods, waiting, faults                   int
	completed, requestID, boarded, journeys int
	interrupted, unaccounted                int
	paused                                  bool
}

// contractPhase holds the fields of a pod that an event changes.
type contractPhase struct {
	activity                     Activity
	stationPhase                 StationPhase
	station, berth, relocatingTo string
	platoon                      string
	riders, stops                int
	occupied, rebalancing        bool
	withdrawn                    serviceHold
	faulted                      bool
}

// due reports whether the monitor checks the contract at this observation
// of s.
func (c *contractSampler) due(s *Simulation) bool {
	if c.every <= 1 {
		return true
	}
	counts := contractCounts{
		pods: len(s.vehicles), waiting: len(s.waiting), faults: len(s.faults),
		completed: s.completed, requestID: s.requestID, boarded: s.boarded, journeys: s.journeys,
		interrupted: s.interrupted, unaccounted: s.unaccountedOrders, paused: s.paused,
	}
	pods := make([]contractPhase, len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		pods[index] = contractPhase{
			activity: v.Pod.Activity, stationPhase: v.Pod.StationPhase,
			station: v.Pod.StationID, berth: v.Pod.BerthID, relocatingTo: v.RelocatingTo,
			platoon: v.PlatoonID, riders: len(v.Riders), stops: len(v.Stops),
			occupied: v.Pod.Occupied, rebalancing: v.Rebalancing, withdrawn: v.withdrawn, faulted: v.faulted,
		}
	}
	due := !c.observed || s.tick != c.tick+1 || s.tick%c.every == 0 || counts != c.counts || !slices.Equal(pods, c.pods)
	c.observed, c.tick, c.counts, c.pods = true, s.tick, counts, pods
	return due
}

// monitorContract makes a simulation check the contract at the
// observations that a contractSampler selects. The test fails at the first
// break. The monitor proves that the contract is not stricter than the
// live code.
func monitorContract(tb testing.TB, s *Simulation) {
	tb.Helper()
	monitorContractEvery(tb, s, contractCheckTicks)
}

// monitorContractEachTick makes a simulation check the contract after each
// tick and each command that changes its pods, its orders or its sharing
// settings. A few targeted tests use it to find a break that lasts less
// than contractCheckTicks ticks between two events.
func monitorContractEachTick(tb testing.TB, s *Simulation) {
	tb.Helper()
	monitorContractEvery(tb, s, 1)
}

func monitorContractEvery(tb testing.TB, s *Simulation, every int64) {
	tb.Helper()
	sampler := &contractSampler{every: every}
	s.monitor = func(s *Simulation) {
		tb.Helper()
		sampler.check(tb, s)
	}
	s.observe()
}

// check checks the contract of s when the sampler selects this
// observation. The test fails at a break.
func (c *contractSampler) check(tb testing.TB, s *Simulation) {
	tb.Helper()
	if !c.due(s) {
		return
	}
	if err := s.CheckContract(); err != nil {
		tb.Fatalf("tick %d: the live state breaks the contract: %v", s.tick, err)
	}
}
