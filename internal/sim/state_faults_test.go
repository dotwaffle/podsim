package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

// debrisSave returns a debris fleet with pod 02 on the return lane, one
// debris record on s3-link from 70 m to 80 m, and its saved state.
func debrisSave(t *testing.T) (*Simulation, SavedState) {
	t.Helper()
	s := debrisFleet(t)
	returnTraveler(t, s)
	startDebris(t, s, "s3-link", 70, 80, 0)
	state := s.ExportState()
	return s, state
}

// addSavedDebris appends a debris record with the next serial to state.
func addSavedDebris(state *SavedState, lane int, from, to float64) {
	state.Faults = &SavedFaults{Records: slices.Clone(state.Faults.Records), Counters: state.Faults.Counters}
	state.IncidentSerial++
	state.Faults.Records = append(state.Faults.Records, SavedFault{Serial: state.IncidentSerial, Debris: true, Start: state.Tick, Lane: lane, From: from, To: to})
}

// TestRestoreDebrisFirst checks the physical restore of debris (section
// 7.6 of the incident suspension contract): the debris owns its whole
// footprint, and the lane is blocked.
func TestRestoreDebrisFirst(t *testing.T) {
	t.Parallel()
	s, state := debrisSave(t)
	restored, result, err := RestoreState(faultRestoreInput(s, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	record := restored.faults[0]
	footprint := restored.debrisFootprint(record.lane, record.from, record.to)
	for _, r := range footprint {
		if restored.owners[r] != (resourceOwner{kind: faultOwnerKind, id: record.id()}) {
			t.Fatalf("%v has the owner %v", r, restored.owners[r])
		}
	}
	if len(footprint) == 0 || !laneBlocked(restored, "s3-link") {
		t.Fatal("the restored debris blocks nothing")
	}
}

// TestRestoreInvalidDebris checks that a saved debris record makes the
// whole save invalid when its segment fails precondition 5 of the debris
// start, or when its footprint meets another record or a restored pod
// (sections 7.6 and 13.5 of the incident suspension contract). Neither
// tier restores such a save: the logical tier does not recover the rest.
func TestRestoreInvalidDebris(t *testing.T) {
	t.Parallel()
	s, _ := debrisSave(t)
	lane := func(id string) int { return laneIndex(t, s, id) }
	length := func(id string) float64 { return s.graph.lengths[lane(id)] }
	edits := []struct {
		name     string
		physical bool
		edit     func(*SavedState)
	}{
		{"end past the lane", false, func(state *SavedState) { state.Faults.Records[0].To = math.Nextafter(length("s3-link"), math.Inf(1)) }},
		{"longer than the limit", false, func(state *SavedState) { state.Faults.Records[0].To = 120.5 }},
		{"empty segment", false, func(state *SavedState) { state.Faults.Records[0].To = 70 }},
		{"negative start", false, func(state *SavedState) { state.Faults.Records[0].From = -1 }},
		{"not a number", false, func(state *SavedState) { state.Faults.Records[0].From = math.NaN() }},
		{"unknown lane", false, func(state *SavedState) { state.Faults.Records[0].Lane = len(s.network.Lanes) }},
		{"berth in the footprint", false, func(state *SavedState) {
			state.Faults.Records[0].Lane, state.Faults.Records[0].From, state.Faults.Records[0].To = lane("s3-1-in"), length("s3-1-in")-10, length("s3-1-in")
		}},
		{"berth node in the footprint", false, func(state *SavedState) {
			state.Faults.Records[0].Lane, state.Faults.Records[0].From, state.Faults.Records[0].To = lane("s3-1-out"), 0, 5
		}},
		{"another debris footprint", false, func(state *SavedState) { addSavedDebris(state, lane("s3-link"), 85, 90) }},
		{"a pod in the footprint", true, func(state *SavedState) {
			index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })
			pod := state.Pods[index]
			addSavedDebris(state, pod.Route[pod.RouteIndex], pod.LaneDistance+1, pod.LaneDistance+2)
		}},
	}
	for _, test := range edits {
		for _, logical := range []bool{false, true} {
			if logical && test.physical {
				continue
			}
			live, state := debrisSave(t)
			test.edit(&state)
			input := faultRestoreInput(live, state)
			input.LogicalOnly = logical
			_, result, err := RestoreState(input)
			if !errors.Is(err, errInvalidFaults) || result.Tier != "" {
				t.Errorf("%s, logical %t: %v, tier %q", test.name, logical, err, result.Tier)
			}
		}
	}
	// The control restores a second debris record that meets nothing.
	s, state := debrisSave(t)
	addSavedDebris(&state, laneIndex(t, s, "s2-link"), 0, 2)
	if _, result, err := RestoreState(faultRestoreInput(s, state)); err != nil || !cleanRestore(result) {
		t.Fatalf("control: %v, %+v", err, result)
	}
}

// TestRestoreSavedFaultRules checks each rule of section 13.5 of the
// incident suspension contract that a restore checks without the network.
// Each failure refuses the restore before either tier.
func TestRestoreSavedFaultRules(t *testing.T) {
	t.Parallel()
	build := func(t *testing.T) (*Simulation, SavedState) {
		t.Helper()
		s := faultLegFleet(t)
		v := boardParties(t, s, "s2")
		cruiseOn(t, s, v, "s0-link")
		startFault(t, s, v, 10)
		startFault(t, s, s.findVehicle("02"), 0)
		state := s.ExportState()
		return s, state
	}
	pod := func(state *SavedState) *SavedFault { return &state.Faults.Records[0] }
	parked := func(state *SavedState) *SavedFault { return &state.Faults.Records[1] }
	edits := map[string]func(*SavedState){
		"serials out of order":       func(state *SavedState) { parked(state).Serial = pod(state).Serial },
		"serial above the saved one": func(state *SavedState) { parked(state).Serial = state.IncidentSerial + 1 },
		"negative start":             func(state *SavedState) { pod(state).Start = -1 },
		"start after the tick":       func(state *SavedState) { pod(state).Start = state.Tick + 1 },
		"negative end":               func(state *SavedState) { pod(state).End = -1 },
		"end at the start":           func(state *SavedState) { pod(state).End = pod(state).Start },
		"evacuation tick overflow": func(state *SavedState) {
			state.Tick = math.MaxInt64
			pod(state).Start, pod(state).End = math.MaxInt64-300*TicksPerSecond+1, 0
		},
		"negative counter":     func(state *SavedState) { state.Faults.Counters.FaultWaitTicks = -1 },
		"negative pod":         func(state *SavedState) { pod(state).Pod = -1 },
		"pod out of range":     func(state *SavedState) { pod(state).Pod = len(state.Pods) },
		"two records of a pod": func(state *SavedState) { parked(state).Pod = pod(state).Pod },
		"pod without the hold": func(state *SavedState) { state.Pods[1].Withdrawn = 0 },
		"platoon follower": func(state *SavedState) {
			state.Pods[1].Platoon = &SavedPlatoonLink{Leader: "01"}
		},
		"platoon leader": func(state *SavedState) {
			state.Faults.Records = state.Faults.Records[1:]
			state.Pods[0].Withdrawn, state.Pods[0].Platoon = 0, &SavedPlatoonLink{Leader: "02"}
		},
		"traffic demo with records": func(state *SavedState) { state.Demo = &SavedDemo{} },
		"too many debris": func(state *SavedState) {
			for range maxDebrisFaults + 1 {
				state.IncidentSerial++
				state.Faults.Records = append(state.Faults.Records, SavedFault{Serial: state.IncidentSerial, Debris: true})
			}
		},
	}
	for name, edit := range edits {
		s, state := build(t)
		state.Faults = &SavedFaults{Records: slices.Clone(state.Faults.Records), Counters: state.Faults.Counters}
		state.Pods = slices.Clone(state.Pods)
		edit(&state)
		for _, logical := range []bool{false, true} {
			input := faultRestoreInput(s, state)
			input.LogicalOnly = logical
			fleet := func() (*Simulation, error) { t.Fatalf("%s: the restore reached a tier", name); return nil, nil }
			if _, _, err := restoreState(input, fleet); !errors.Is(err, errInvalidFaults) {
				t.Errorf("%s, logical %t: the restore accepts the save", name, logical)
			}
		}
	}
	s, state := build(t)
	for name, edit := range map[string]func(*RestoreStateInput){
		"faults off":          func(input *RestoreStateInput) { input.FaultsOn = false },
		"evacuation too long": func(input *RestoreStateInput) { input.Faults.EvacuationSeconds = 3601 },
	} {
		input := faultRestoreInput(s, state)
		edit(&input)
		if _, _, err := RestoreState(input); err == nil {
			t.Errorf("%s: the restore accepts the save", name)
		}
	}
	if _, result, err := RestoreState(faultRestoreInput(s, state)); err != nil || !cleanRestore(result) {
		t.Fatalf("control: %v, %+v", err, result)
	}
}

// TestRestoreFaultDemotion checks a physical restore that would demote a
// faulted pod (section 12.7 of the incident suspension contract). The
// physical tier fails, and the logical tier ends every record, releases
// the fault hold, and keeps the counters. A logical restore does the same
// for a save that the physical tier accepts.
func TestRestoreFaultDemotion(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2")
	cruiseOn(t, s, v, "s0-link")
	startFault(t, s, v, 0)
	startFault(t, s, s.findVehicle("02"), 0)
	state := s.ExportState()
	logical := faultRestoreInput(s, state)
	logical.LogicalOnly = true
	state.Pods = slices.Clone(state.Pods)
	// The physical tier demotes a traveling pod without a route.
	state.Pods[0].Route = nil
	for name, input := range map[string]RestoreStateInput{"demotion": faultRestoreInput(s, state), "logical only": logical} {
		restored, result, err := RestoreState(input)
		if err != nil || result.Tier != RestoreLogical || result.DroppedFaults != 2 {
			t.Fatalf("%s: %v, %+v", name, err, result)
		}
		if name == "demotion" && (result.PhysicalError == nil || errors.Is(result.PhysicalError, errInvalidFaults)) {
			t.Fatalf("the physical tier failed with %v", result.PhysicalError)
		}
		if len(restored.faults) != 0 || restored.faultCounters != s.faultCounters || !restored.faultsOn {
			t.Fatalf("%s: records %d, counters %+v", name, len(restored.faults), restored.faultCounters)
		}
		for index := range restored.vehicles {
			if v := &restored.vehicles[index]; v.withdrawn != 0 || v.faulted {
				t.Fatalf("%s: pod %s keeps the hold %d", name, v.Pod.ID, v.withdrawn)
			}
		}
	}
}

// TestRestoreFaultedServiceClaim checks the claim surrender of a restored
// faulted pod (section 12.7 of the incident suspension contract). A save
// that has the destination claim of a faulted empty pod gets the claim
// back from claimDestinations, and the pod gives it up again. A stopping
// claim of a faulted pod at rest stays.
func TestRestoreFaultedServiceClaim(t *testing.T) {
	t.Parallel()
	market := berthResources(Berth{ID: "market-1", Node: "market-berth"})
	t.Run("service claim", func(t *testing.T) {
		t.Parallel()
		s, relocating, _ := faultFixture(t)
		startFault(t, s, relocating, 0)
		state := s.ExportState()
		state.Pods = slices.Clone(state.Pods)
		index := s.vehicleIndex(relocating)
		if state.Pods[index].ClaimsDestination {
			t.Fatal("the faulted pod saves its service claim")
		}
		state.Pods[index].ClaimsDestination = true
		restored, result, err := RestoreState(faultRestoreInput(s, state))
		if err != nil || result.Tier != RestorePhysical {
			t.Fatalf("restore: %v, %+v", err, result)
		}
		for _, r := range market {
			if !restored.owners[r].isZero() {
				t.Fatalf("the restored faulted pod keeps %v", r)
			}
		}
	})
	t.Run("stopping claim", func(t *testing.T) {
		t.Parallel()
		s, relocating, _ := faultFixture(t)
		// A pod at rest keeps the reservation of its current block, so the
		// fault starts where that reservation reaches the berth.
		stepUntil(t, s, "a stopping grant at Market at rest", func() bool {
			if relocating.Pod.Activity != Traveling {
				return false
			}
			through := reservationEnd(&relocating.blocks, relocating.blockIndex)
			held := make(map[resource]bool)
			for resources := range relocating.blocks.spanResources(0, through+1) {
				for _, r := range market {
					held[r] = held[r] || slices.Contains(resources, r)
				}
			}
			return held[market[0]] && held[market[1]]
		})
		startFault(t, s, relocating, 0)
		restored := physicalSave(t, s, "a faulted pod with its stopping grant")
		for _, r := range market {
			if !restored.owners[r].isPod(relocating.Pod.ID) {
				t.Fatalf("the restored faulted pod gave up %v", r)
			}
		}
	})
}

// TestRestoreFaultCountersAtLimit restores each fault counter at
// math.MaxInt64, then starts and clears a fault. Each counter stays at its
// limit, and each transition completes.
func TestRestoreFaultCountersAtLimit(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	state := s.ExportState()
	state.Faults = &SavedFaults{Counters: FaultCounters{
		Started: math.MaxInt64, Cleared: math.MaxInt64, Evacuations: math.MaxInt64, Reroutes: math.MaxInt64, FaultWaitTicks: math.MaxInt64,
	}}
	restored, result, err := RestoreState(faultRestoreInput(s, state))
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	id, err := restored.startPodFault(restored.findVehicle("02"), 1)
	if err != nil {
		t.Fatal(err)
	}
	stepUntil(t, restored, "the timed clear", func() bool { return len(restored.faults) == 0 })
	if restored.faultCounters.exported() != state.Faults.Counters || id == "" {
		t.Fatalf("counters %+v", restored.faultCounters)
	}
}

// TestRestoreFaultedPodDoesNotWait restores a faulted pod at a berth whose
// saved state says that it waits for its departure. A faulted pod waits
// for no grant, so the restore drops the wait.
func TestRestoreFaultedPodDoesNotWait(t *testing.T) {
	t.Parallel()
	s := faultLegFleet(t)
	v := boardParties(t, s, "s2")
	startFault(t, s, v, 0)
	state := s.ExportState()
	state.Pods = slices.Clone(state.Pods)
	index := s.vehicleIndex(v)
	// The saved pod has boarded and waits for its departure grant.
	state.Pods[index].PhaseTicks, state.Pods[index].Waiting = 0, true
	restored, result, err := RestoreState(faultRestoreInput(s, state))
	if err != nil || result.Tier != RestorePhysical {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	if restored.vehicles[index].pending != -1 {
		t.Fatalf("the restored faulted pod waits at %d", restored.vehicles[index].pending)
	}
}

// TestRestoreFaultFootprints checks F6 of the incident suspension contract
// before either tier (sections 7.6 and 13.5): a save whose debris meets
// the footprint of a pod fault is invalid, also in a logical restore and
// after an early failure of the physical tier. A faulted pod that the
// physical tier cannot place has no footprint, so a save with such a pod
// and valid debris restores in the logical tier.
func TestRestoreFaultFootprints(t *testing.T) {
	t.Parallel()
	// build returns a save with a fault on pod 02, which travels on the
	// return lane, and debris on the lane of pod 02 when overlap is true,
	// or on s2-link.
	build := func(t *testing.T, overlap bool) (*Simulation, SavedState) {
		t.Helper()
		s := debrisFleet(t)
		startFault(t, s, returnTraveler(t, s), 0)
		state := s.ExportState()
		state.Pods = slices.Clone(state.Pods)
		pod := state.Pods[slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })]
		if overlap {
			addSavedDebris(&state, pod.Route[pod.RouteIndex], pod.LaneDistance+1, pod.LaneDistance+2)
		} else {
			addSavedDebris(&state, laneIndex(t, s, "s2-link"), 0, 2)
		}
		return s, state
	}
	// early makes the physical tier fail before it places the debris: pod
	// 01 at its berth waits from a tick after the saved tick. The logical
	// tier does not read the value.
	early := func(state *SavedState) {
		state.Pods[slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "01" })].WaitSince = state.Tick + 1
	}
	// underivable gives pod 02 a value out of range, no route, or a route
	// that does not end at its destination, so the physical tier would
	// demote it.
	underivable := map[string]func(*Simulation, *SavedPod){
		"value out of range": func(_ *Simulation, pod *SavedPod) { pod.WaitSince = math.MaxInt64 },
		"no route":           func(_ *Simulation, pod *SavedPod) { pod.Route = nil },
		// Four turns of the loop before the route make it longer than the
		// route limit of the restore.
		"route over the limit": func(s *Simulation, pod *SavedPod) {
			var loop []int
			for _, id := range []string{"return-down", "return", "return-up", "p-through", "p-link", "s0-through", "s0-link", "s1-through", "s1-link",
				"s2-through", "s2-link", "s3-through", "s3-link", "s4-through", "s4-link", "s5-through"} {
				loop = append(loop, laneIndex(t, s, id))
			}
			prefix := slices.Repeat(loop, 4)
			pod.Route, pod.RouteIndex = append(prefix, pod.Route...), pod.RouteIndex+len(prefix)
		},
		"route to elsewhere": func(s *Simulation, pod *SavedPod) { pod.Route, pod.RouteIndex = []int{laneIndex(t, s, "s2-link")}, 0 },
	}
	for _, test := range []struct {
		name    string
		logical bool
		edit    func(*SavedState)
	}{
		{"logical only", true, func(*SavedState) {}},
		{"early physical failure", false, early},
	} {
		s, state := build(t, true)
		test.edit(&state)
		input := faultRestoreInput(s, state)
		input.LogicalOnly = test.logical
		if _, result, err := RestoreState(input); !errors.Is(err, errInvalidFaults) || result.Tier != "" {
			t.Errorf("%s: %v, tier %q", test.name, err, result.Tier)
		}
		// The same save with debris that meets nothing restores in the
		// logical tier.
		s, state = build(t, false)
		test.edit(&state)
		input = faultRestoreInput(s, state)
		input.LogicalOnly = test.logical
		_, result, err := RestoreState(input)
		if err != nil || result.Tier != RestoreLogical || result.DroppedFaults != 2 || test.logical == (result.PhysicalError != nil) {
			t.Errorf("%s, control: %v, %+v", test.name, err, result)
		}
	}
	for name, edit := range underivable {
		s, state := build(t, true)
		edit(s, &state.Pods[slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "02" })])
		_, result, err := RestoreState(faultRestoreInput(s, state))
		if err != nil || result.Tier != RestoreLogical || result.DroppedFaults != 2 || result.PhysicalError == nil {
			t.Errorf("%s: %v, %+v", name, err, result)
		}
	}
	// A faulted pod at a berth holds only the berth and its node, also
	// with a route. Debris at the start of its next lane meets nothing.
	s := debrisFleet(t)
	v := s.findVehicle("01")
	if err := s.board(v, newTrip(s, "s0", "s5")); err != nil {
		t.Fatal(err)
	}
	startFault(t, s, v, 0)
	startDebris(t, s, "s0-link", 0, 1, 0)
	if _, result, err := RestoreState(faultRestoreInput(s, s.ExportState())); err != nil || !cleanRestore(result) {
		t.Fatalf("faulted pod at a berth: %v, %+v", err, result)
	}
}

// TestRestoreFailedBerthChoice saves a pod that rests in the first cell of its
// final lane, because debris makes its berth choice fail. The pod has no berth,
// and it can hold that cell. Each case keeps the physical tier and the place of
// the pod, with or without a record on the pod. After the clear, the pod chooses
// its berth and arrives.
func TestRestoreFailedBerthChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		prepare       func(t *testing.T, s *Simulation, v *vehicle)
		faults        int
		emergencyPods int
	}{
		{name: "no record", prepare: func(*testing.T, *Simulation, *vehicle) {}, faults: 1},
		{name: "emergency", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			s.emergenciesOn = true
			startEmergency(t, s, v, 0)
		}, faults: 1, emergencyPods: 1},
		{name: "fault", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			startFault(t, s, v, 0)
		}, faults: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := faultLegFleet(t)
			v := boardParties(t, s, "s1")
			startDebris(t, s, "s1-1-in", 0, 5, 0)
			stepUntil(t, s, "the rest with a failed berth choice", func() bool {
				return v.Pod.WaitReason == NoForwardRoute && v.Pod.Speed == 0
			})
			if first, _ := routeLaneBlocks(&v.blocks, len(v.Route)-1); v.destination.ID != "" || v.reservedThrough != first || v.blockIndex != first {
				t.Fatalf("pod 01 has the berth %q, block %d, and a reservation through %d, want block %d", v.destination.ID, v.blockIndex, v.reservedThrough, first)
			}
			test.prepare(t, s, v)
			s.Step()
			input := emergencyRestoreInput(s, s.ExportState())
			input.EmergenciesOn = s.emergenciesOn
			restored, result, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != RestorePhysical || len(result.Demoted) != 0 || result.PhysicalError != nil {
				t.Fatalf("the result is %+v", result)
			}
			got := restored.findVehicle(v.Pod.ID)
			if got.Pod.Activity != Traveling || got.Pod.LaneID != v.Pod.LaneID || got.destination.ID != "" || len(restored.faults) != test.faults || len(restored.emergencies) != test.emergencyPods {
				t.Fatalf("pod 01 is %s on %s with the berth %q, and %d faults and %d emergencies", got.Pod.Activity, got.Pod.LaneID, got.destination.ID, len(restored.faults), len(restored.emergencies))
			}
			checkFaultsEachTick(t, restored)
			for _, fault := range slices.Clone(restored.faults) {
				if err := restored.clearFault(fault.id()); err != nil {
					t.Fatal(err)
				}
			}
			stepUntil(t, restored, "the arrival at s1", func() bool { return got.Pod.Activity == Unloading && got.Pod.StationID == "s1" })
		})
	}
}

// TestRestoreShortFinalLane saves a pod with a failed berth choice before a
// final lane so short that the junction zone of the station entry covers all of
// it. The next request of the pod reaches the end of the route, so the pod tries
// its choice one block before that lane, and it rests with blocks left to
// reserve. The reservation that the restore derives from the current block ends
// at or before the live one, so the pod keeps its place, and it arrives after
// the clear. The lane of 30 m is a control with a later request on the final
// lane.
func TestRestoreShortFinalLane(t *testing.T) {
	t.Parallel()
	for _, length := range []float64{24, 25, 30} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			t.Parallel()
			lanes := []geometryLane{{to: Point{}}, {to: Point{X: length}}}
			network := geometryNetwork(Point{X: -500}, lanes, 1)
			s := restoreGeometry(t, network, len(lanes), 1, 400, corridorQueueGap)
			if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
				t.Fatal(err)
			}
			id := startDebris(t, s, "dest-in", 0, 5, 0)
			v := &s.vehicles[0]
			stepUntil(t, s, "the rest with a failed berth choice", func() bool {
				return v.Pod.WaitReason == NoForwardRoute && v.Pod.Speed == 0
			})
			if v.destination.ID != "" || v.reservedThrough >= v.blocks.len()-1 {
				t.Fatalf("pod p01 has the berth %q and a reservation through %d of %d blocks", v.destination.ID, v.reservedThrough, v.blocks.len())
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: s.ExportState(), FaultsOn: true, Faults: FaultSettings{EvacuationSeconds: 300}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
				t.Fatalf("the result is %+v", result)
			}
			got := &restored.vehicles[0]
			if got.reservedThrough > v.reservedThrough {
				t.Fatalf("the restored reservation ends at block %d, after the live block %d", got.reservedThrough, v.reservedThrough)
			}
			if err := restored.clearFault(id); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, restored, "the arrival", func() bool { return got.Pod.Activity == Unloading && got.Pod.BerthID == "dest-1" })
		})
	}
}

// TestRestoredFaultTransition restores a faulted pod at rest whose saved lane
// distance differs from its route distance by a rounding error. The check for a
// restored simulation accepts the first publish, and the ordinary check does
// not. A real move fails in the first tick, and in a later tick.
func TestRestoredFaultTransition(t *testing.T) {
	t.Parallel()
	restore := func(t *testing.T) (*Simulation, *vehicle) {
		t.Helper()
		s := faultLegFleet(t)
		v := boardParties(t, s, "s1")
		cruiseOn(t, s, v, "s0-link")
		startFault(t, s, v, 0)
		stepUntil(t, s, "the rest", func() bool { return v.Pod.Speed == 0 })
		state := s.ExportState()
		state.Pods = slices.Clone(state.Pods)
		state.Pods[s.vehicleIndex(v)].LaneDistance += 2e-9
		restored, result, err := RestoreState(faultRestoreInput(s, state))
		if err != nil {
			t.Fatal(err)
		}
		if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
			t.Fatalf("the result is %+v", result)
		}
		got := restored.findVehicle(v.Pod.ID)
		return restored, got
	}
	const moved = "the pod at rest moves"
	tests := []struct {
		name  string
		check func(*Simulation) func() error
		// prepare runs after the check starts, and before the first tick.
		prepare, later func(*vehicle)
		want           string
	}{
		{name: "ordinary check", check: (*Simulation).FaultTransitionForTest, want: moved},
		{name: "restored check", check: (*Simulation).RestoredFaultTransitionForTest},
		{name: "command before the first tick", check: (*Simulation).RestoredFaultTransitionForTest, prepare: func(*vehicle) {}},
		{name: "move in the first tick", check: (*Simulation).RestoredFaultTransitionForTest, later: func(v *vehicle) { v.Pod.LaneDistance += 1e-9 }, want: moved},
		{name: "route distance before the first tick", check: (*Simulation).RestoredFaultTransitionForTest, prepare: func(v *vehicle) { v.distance -= 1e-9 }, want: moved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := restore(t)
			check := test.check(s)
			if test.prepare != nil {
				test.prepare(v)
				if err := check(); err != nil {
					if test.want == "" || !strings.Contains(err.Error(), test.want) {
						t.Fatalf("before the first tick: %v", err)
					}
					return
				}
			}
			s.Step()
			if test.later != nil {
				test.later(v)
			}
			err := check()
			switch {
			case test.want == "" && err != nil:
				t.Fatalf("after the first tick: %v", err)
			case test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)):
				t.Fatalf("after the first tick: %v, want %q", err, test.want)
			}
			if test.want != "" {
				return
			}
			s.Step()
			if err := check(); err != nil {
				t.Fatalf("after the second tick: %v", err)
			}
			v.Pod.LaneDistance += 1e-12
			if err := check(); err == nil || !strings.Contains(err.Error(), moved) {
				t.Fatalf("a later move gives %v", err)
			}
		})
	}
}
