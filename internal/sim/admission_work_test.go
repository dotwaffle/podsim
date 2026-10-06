package sim

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
)

func TestAdmissionWorkMatchesOriginalTicks(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, tc := range []struct {
		name              string
		buffers, platoons bool
	}{
		{name: "ordinary"}, {name: "buffers", buffers: true},
		{name: "platoons", platoons: true}, {name: "buffers and platoons", buffers: true, platoons: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := Example()
			if tc.buffers {
				network = stationBufferNetwork(twoBerthMarket(), 4)
			}
			got, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			got.SetStationBuffers(tc.buffers)
			if tc.platoons {
				if err := got.SetPlatooning(PlatooningVirtual); err != nil {
					t.Fatal(err)
				}
			}
			want := got.Clone()
			stops := []string{"harbor", "market", "garden"}
			used := false
			for tick := range 600 * TicksPerSecond {
				if tick%(12*TicksPerSecond) == 0 {
					n := tick / (12 * TicksPerSecond)
					a := got.RequestTrip(stops[n%3], stops[(n+1)%3])
					b := want.RequestTrip(stops[n%3], stops[(n+1)%3])
					if fmt.Sprint(a) != fmt.Sprint(b) {
						t.Fatalf("request errors differ: %v / %v", a, b)
					}
				}
				got.Step()
				want.stepBeforeAdmissionWork()
				if !sameState(got, want) {
					t.Fatalf("original admission diverged at tick%d", tick)
				}
				if got.admissionWork != nil {
					used = used || cap(got.admissionWork.intents) > 0
					checkAdmissionWorkCleared(t, got.admissionWork)
				}
			}
			if !used || got.completed == 0 || got.requestID < 2 {
				t.Fatalf("fixture lacks completed demand: used%v completed%d requests%d", used, got.completed, got.requestID)
			}
		})
	}
}

func TestAdmissionWorkMatchesTerminalAndBufferGrants(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		make      func(*testing.T) *Simulation
		wantBerth string
	}{
		{name: "terminal reroute", make: func(t *testing.T) *Simulation {
			t.Helper()
			s := berthChoiceSimulation(t)
			addMarketBerth(s)
			s.owners[resource{kind: berthResource, id: "market-1"}] = podResourceOwner("02")
			s.owners[resource{kind: nodeResource, id: "market-berth"}] = podResourceOwner("02")
			positionBeforeTerminalInlet(t, terminalInletPosition{simulation: s, vehicle: s.findVehicle("01")})
			return s
		}, wantBerth: "market-2"},
		{name: "recursive buffer head", make: bufferedHeadWithPickup, wantBerth: "market-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.make(t)
			want := got.Clone()
			got.admit()
			want.admitBeforeWork()
			if !sameState(got, want) {
				t.Fatal("terminal or recursive grant diverged")
			}
			if got.findVehicle("01").destination.ID != tc.wantBerth {
				t.Fatal("fixture did not commit the expected berth")
			}
			checkAdmissionWorkCleared(t, got.admissionWork)
		})
	}
}

func TestAdmissionWorkMatchesLinkedCorridor(t *testing.T) {
	t.Parallel()
	skipLong(t)
	got := restoreCorridor(t, mergeCorridor(true, 30), corridorQueues(corridorFeedLength-100))
	if err := got.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	want := got.Clone()
	peakLinked := 0
	for range 120 * TicksPerSecond {
		got.Step()
		want.stepBeforeAdmissionWork()
		if !sameState(got, want) {
			t.Fatalf("linked admission diverged at tick%d", got.tick)
		}
		peakLinked = max(peakLinked, got.LinkedPods())
	}
	if peakLinked == 0 {
		t.Fatal("corridor did not exercise active linking")
	}
}

func admissionWorkContention() *Simulation {
	s := &Simulation{owners: make(map[resource]resourceOwner)}
	for _, id := range []string{"02", "01"} {
		v := vehicle{Pod: Pod{ID: id, Activity: Traveling}, reservedThrough: -1, pending: -1}
		v.blocks = blockListOf([]block{{lane: Lane{ID: id, SpeedLimit: 14}, end: 30, resources: []resource{{kind: junctionResource, id: "merge"}}}})
		s.vehicles = append(s.vehicles, v)
	}
	return s
}

func TestAdmissionWorkDropsStalePickupPriority(t *testing.T) {
	t.Parallel()
	got := admissionWorkContention()
	for _, tc := range []struct{ pickup, winner string }{{"02", "02"}, {"01", "01"}, {"", "01"}} {
		clear(got.owners)
		for i := range got.vehicles {
			got.vehicles[i].reservedThrough = -1
			got.vehicles[i].pending = -1
			got.vehicles[i].waitSince = got.tick
		}
		got.waiting = []waitingTrip{{request: Request{PodID: tc.pickup}}, {request: Request{PodID: "unknown"}}}
		want := got.Clone()
		got.admit()
		want.admitBeforeWork()
		if !sameState(got, want) || got.owners[resource{kind: junctionResource, id: "merge"}] != podResourceOwner(tc.winner) {
			t.Fatal("a previous pickup assignment changed priority")
		}
		checkAdmissionWorkCleared(t, got.admissionWork)
	}
}

func TestAdmissionWorkClearsLargeThenEmptyPass(t *testing.T) {
	t.Parallel()
	s := &Simulation{owners: make(map[resource]resourceOwner)}
	for i := range 64 {
		id := strconv.Itoa(i)
		v := vehicle{Pod: Pod{ID: id, Activity: Traveling}, reservedThrough: -1, pending: -1}
		v.blocks = blockListOf([]block{{lane: Lane{ID: id, SpeedLimit: 14}, end: 30}})
		s.vehicles = append(s.vehicles, v)
		s.waiting = append(s.waiting, waitingTrip{request: Request{PodID: id}})
	}
	s.admit()
	work := s.admissionWork
	if cap(work.intents) < 64 {
		t.Fatal("fixture did not grow the intent buffer")
	}
	storage := &work.intents[:cap(work.intents)][0]
	checkAdmissionWorkCleared(t, work)
	for i := range s.vehicles {
		s.vehicles[i].Pod.Activity = Idle
	}
	s.waiting = nil
	s.admit()
	if s.admissionWork != work || &work.intents[:cap(work.intents)][0] != storage {
		t.Fatal("empty pass replaced retained storage")
	}
	checkAdmissionWorkCleared(t, work)
}

func checkAdmissionWorkCleared(t *testing.T, work *admissionWork) {
	t.Helper()
	if work == nil || len(work.intents) != 0 || len(work.pickups) != 0 {
		t.Fatal("admission retained active scratch entries")
	}
	for _, in := range work.intents[:cap(work.intents)] {
		if in != (intent{}) {
			t.Fatal("admission retained a pod ID or intent")
		}
	}
}

func TestAdmissionWorkCloneResetAndRestore(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	for range 30 * TicksPerSecond {
		s.Step()
	}
	work := s.admissionWork
	checkAdmissionWorkCleared(t, work)
	clone := s.Clone()
	if clone.admissionWork != nil {
		t.Fatal("Clone retained mutable admission work")
	}
	clone.admit()
	if clone.admissionWork == work {
		t.Fatal("Clone reused source admission work")
	}
	work.pickups["source-only"] = true
	clone.admissionWork.pickups["clone-only"] = true
	if work.pickups["clone-only"] || clone.admissionWork.pickups["source-only"] {
		t.Fatal("clone pickup map aliases source")
	}
	clear(work.pickups)
	clear(clone.admissionWork.pickups)
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
	if err != nil || result.Tier != RestorePhysical {
		t.Fatalf("restore: %+v, %v", result, err)
	}
	if restored.admissionWork != nil {
		t.Fatal("restore retained admission work")
	}
	control := restored.Clone()
	for range 120 {
		restored.Step()
		control.stepBeforeAdmissionWork()
	}
	if !sameState(restored, control) {
		t.Fatal("restored admission differs from the original loop")
	}
	s.Reset()
	if s.admissionWork != nil {
		t.Fatal("Reset retained prior admission work")
	}
}

func TestAdmissionWorkPreparedFleetIsolation(t *testing.T) {
	t.Parallel()
	prepared, err := PrepareNetwork(Example())
	if err != nil {
		t.Fatal(err)
	}
	var fleet []*Simulation
	for _, id := range []string{"first", "second"} {
		s, err := prepared.NewFleet([]Placement{{ID: id, StationID: "harbor"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RequestTrip("harbor", "garden"); err != nil {
			t.Fatal(err)
		}
		fleet = append(fleet, s)
	}
	var workers sync.WaitGroup
	for _, s := range fleet {
		workers.Go(func() {
			for range 120 {
				s.Step()
			}
		})
	}
	workers.Wait()
	if fleet[0].admissionWork == nil || fleet[0].admissionWork == fleet[1].admissionWork {
		t.Fatal("prepared fleets shared admission work")
	}
	if !reflect.DeepEqual(fleet[0].Snapshot().Vehicles[0].Pod.Position, fleet[1].Snapshot().Vehicles[0].Pod.Position) {
		t.Fatal("equivalent prepared fleets evolved differently")
	}
	for _, s := range fleet {
		checkAdmissionWorkCleared(t, s.admissionWork)
	}
}

// These functions retain the admission and tick loops from 42747d5.
func (s *Simulation) admitBeforeWork() {
	var intents []intent
	for i := range s.vehicles {
		v := &s.vehicles[i]
		ready := departs(v.Pod.Activity) && v.phaseTicks == 0
		if !ready && v.Pod.Activity != Traveling {
			continue
		}
		if !s.assignTerminalBerth(v) {
			continue
		}
		s.reevaluateTerminalBerth(v)
		v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
		next := v.reservedThrough + 1
		if next >= v.blocks.len() {
			continue
		}
		// Reserve enough track for cruising speed plus the configured lookahead.
		// A denied extension leaves the existing stopping boundary intact.
		speed := math.Max(v.Pod.Speed, v.blocks.currentLane(v.blockIndex).SpeedLimit)
		horizon := speed*speed/(2*acceleration) + speed*s.reservationLookaheadSeconds
		if v.reservedThrough >= 0 && v.blocks.end(v.reservedThrough)-v.distance >= horizon {
			continue
		}
		if v.pending != next {
			v.pending, v.waitSince = next, s.tick
		}
		intents = append(intents, intent{index: i, block: next, since: v.waitSince, id: v.Pod.ID})
	}
	pickups := make(map[string]bool)
	for _, trip := range s.waiting {
		if trip.request.PodID != "" {
			pickups[trip.request.PodID] = true
		}
	}
	for i := range intents {
		v := &s.vehicles[intents[i].index]
		intents[i].priority = admissionPriority(v, pickups[v.Pod.ID])
	}
	slices.SortFunc(intents, func(a, b intent) int {
		return compareAdmission(a, b, s.tick)
	})
	for _, in := range intents {
		s.grant(in)
	}
}

func (s *Simulation) stepBeforeAdmissionWork() {
	defer s.observe()
	if s.paused {
		return
	}
	s.stepCompletions = s.stepCompletions[:0]
	s.tick++
	s.stepDemo()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.phaseTicks > 0 {
			v.phaseTicks--
		}
		if v.Pod.Activity == Unloading && v.phaseTicks == 0 {
			s.alight(v)
			if len(v.Stops) > 0 && v.RidersAboard() > 0 {
				s.continueJourney(v)
			} else {
				v.Pod.Activity, v.Pod.Occupied, v.Stops = Idle, false, nil
			}
		}
	}
	s.dispatch()
	s.swapPickups()
	s.redistribute()
	s.formPlatoons()
	s.admitBeforeWork()
	s.clearBlockedBerths()
	s.platoonCaps()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if departs(v.Pod.Activity) && v.phaseTicks == 0 && v.reservedThrough >= 0 {
			if v.Pod.Activity == Boarding && s.screensSeats() {
				s.recordDeparture(v)
			}
			v.Pod.Occupied = v.Pod.Activity == Boarding || v.Pod.Activity == Continuing
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
	for i := range s.vehicles {
		s.updateStationPhase(&s.vehicles[i])
	}
}
