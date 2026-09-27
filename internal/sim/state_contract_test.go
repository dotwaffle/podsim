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
// state before the next step.
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
	for phase := range phaseRules {
		if _, ok := samples[podPhase(phase)]; !ok {
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
				ID: state.RequestID, From: "garden", To: "market", PartySize: 1, PodID: pod.ID, RequestedTick: 1, BoardedTick: 2,
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
	case routeStops, laterStops:
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
				state.Pods[1].Riders = []SavedRequest{{ID: 1, From: "garden", To: "market", PartySize: 1, PodID: "02", Completed: true}}
				return state
			},
			want: "order 1 is in pod 01 and in pod 02",
		},
		{
			name: "queued order that a pod carries",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				state.Waiting = append(state.Waiting, SavedTrip{Request: SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 1}})
				return state
			},
			want: "order 1 is in pod 01 and in the queue",
		},
		{
			name: "order queued two times",
			state: func(t *testing.T) SavedState {
				t.Helper()
				state := boardingState(t)
				trip := SavedTrip{Request: SavedRequest{ID: 2, From: "garden", To: "market", PartySize: 1}}
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
