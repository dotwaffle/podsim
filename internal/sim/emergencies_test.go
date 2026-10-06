package sim

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// emergencyFleet is incidentLegFleet with emergencies on. Pod 01 boards
// order 1 from s0 to s1 and order 2 from s0 to s2 at s0. Pod 02 is idle in
// the parking station p.
func emergencyFleet(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	s := incidentLegFleet(t)
	s.emergenciesOn = true
	return s, boardParties(t, s, "s1", "s2")
}

// startEmergency starts an emergency and fails the test on an error.
func startEmergency(t *testing.T, s *Simulation, v *vehicle, order int) string {
	t.Helper()
	id, err := s.Emergency(v.Pod.ID, order)
	if err != nil {
		t.Fatalf("emergency on pod %s: %v", v.Pod.ID, err)
	}
	return id
}

// emergencyPose is the state of a pod that the emergency transition rules
// compare.
type emergencyPose struct {
	hold, member bool
	// serial is the serial of the record that names the pod, or 0.
	serial uint64
}

// emergencyPoses returns the pose of each pod.
func emergencyPoses(s *Simulation) []emergencyPose {
	poses := make([]emergencyPose, len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		poses[index] = emergencyPose{hold: v.withdrawn&emergencyHold != 0, member: v.couplingID != "" || s.couplingApproachMember(v.Pod.ID)}
		if record := s.emergencyOf(v); record >= 0 {
			poses[index].serial = s.emergencies[record].serial
		}
	}
	return poses
}

// emergencyTransition checks the transition part of F11 for one pod, and
// the rule that a record pod outside a coupling or approach group in the
// previous observation has the hold (section 8 of the incident emergency
// contract).
func emergencyTransition(before, after emergencyPose) error {
	switch {
	case after.hold && !before.hold && after.serial == 0:
		return errors.New("F11: the pod gains the emergency hold with no record")
	case before.hold && !after.hold && after.serial != 0 && after.serial == before.serial:
		return errors.New("F11: the pod loses the emergency hold while its record stays")
	case after.serial != 0 && after.serial == before.serial && !before.member && !after.member && !after.hold:
		return errors.New("a record pod outside a group has no emergency hold")
	}
	return nil
}

// checkEmergenciesEachTick makes s check, after each tick and each public
// command, the state contract, the order balance, and the emergency
// transition rules of each pod.
func checkEmergenciesEachTick(t *testing.T, s *Simulation) {
	t.Helper()
	previous := emergencyPoses(s)
	s.monitor = func(s *Simulation) {
		if err := s.CheckContract(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		if err := checkOrderBalance(s); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		next := emergencyPoses(s)
		if s.emergenciesOn && len(next) == len(previous) {
			for index := range next {
				if err := emergencyTransition(previous[index], next[index]); err != nil {
					t.Fatalf("tick %d: pod %s: %v: %+v, then %+v", s.tick, s.vehicles[index].Pod.ID, err, previous[index], next[index])
				}
			}
		}
		previous = next
	}
}

// emergencyIDs returns the IDs of the active records, in record order.
func emergencyIDs(s *Simulation) []string {
	ids := make([]string, 0, len(s.emergencies))
	for _, record := range s.emergencies {
		ids = append(ids, record.id())
	}
	return ids
}

// refusalOracle checks that call refuses with want and changes nothing:
// the state after the call equals a clone from before it, and so do the
// free-flow route memo, which Clone drops (section 7 of the incident
// emergency contract).
func refusalOracle(t *testing.T, s *Simulation, want error, call func() error) {
	t.Helper()
	before := s.Clone()
	routes, order := cloneRoutes(s.routes), slices.Clone(s.routeOrder)
	if err := call(); !errors.Is(err, want) {
		t.Fatalf("error %v, want %v", err, want)
	}
	if !sameState(before, s) {
		t.Fatal("the refused call changed the state")
	}
	// Each side makes its route errors with the same deterministic code,
	// so a comparison by value is correct.
	if !reflect.DeepEqual(routes, s.routes) || !slices.Equal(order, s.routeOrder) { //nolint:govet // deepequalerrors: route errors compare by value on purpose.
		t.Fatal("the refused call changed the route memo")
	}
}

// cloneRoutes returns a copy of a route memo.
func cloneRoutes(routes map[routeKey]routeResult) map[routeKey]routeResult {
	if routes == nil {
		return nil
	}
	c := make(map[routeKey]routeResult, len(routes))
	for key, result := range routes {
		result.lanes = slices.Clone(result.lanes)
		c[key] = result
	}
	return c
}

// TestEmergencyRefusals checks each precondition of section 6.1 alone
// against the refusal oracle. Each case with an undo then removes its
// cause, and the same call succeeds.
func TestEmergencyRefusals(t *testing.T) {
	t.Parallel()
	type target struct {
		pod   string
		order int
	}
	tests := []struct {
		name string
		want error
		// cause sets up the refusal and returns the target of the call.
		// undo, when it is set, removes the cause and returns the target
		// of a call that succeeds.
		cause func(t *testing.T, s *Simulation, v *vehicle) target
		undo  func(s *Simulation, v *vehicle) target
	}{
		{"emergencies off", errEmergenciesOff,
			func(_ *testing.T, s *Simulation, _ *vehicle) target { s.emergenciesOn = false; return target{"01", 0} },
			func(s *Simulation, _ *vehicle) target { s.emergenciesOn = true; return target{"01", 0} }},
		{"unknown pod", errUnknownPod,
			func(*testing.T, *Simulation, *vehicle) target { return target{"09", 0} },
			func(*Simulation, *vehicle) target { return target{"01", 0} }},
		{"dispatch pass", errEmergencyDispatch,
			func(_ *testing.T, s *Simulation, _ *vehicle) target {
				s.pass = &dispatchPass{active: true}
				return target{"01", 0}
			},
			func(s *Simulation, _ *vehicle) target { s.pass = nil; return target{"01", 0} }},
		{"empty pod", errNoPassenger,
			func(*testing.T, *Simulation, *vehicle) target { return target{"02", 0} },
			func(*Simulation, *vehicle) target { return target{"01", 0} }},
		{"evacuated faulted pod", errNoPassenger,
			func(t *testing.T, s *Simulation, v *vehicle) target {
				t.Helper()
				s.faultsOn = true
				startFault(t, s, v, 0)
				if err := s.evacuate(v); err != nil {
					t.Fatal(err)
				}
				return target{"01", 0}
			}, nil},
		{"pod already has an emergency", errPodEmergency,
			func(t *testing.T, s *Simulation, v *vehicle) target {
				t.Helper()
				startEmergency(t, s, v, 1)
				return target{"01", 2}
			}, nil},
		{"emergency limit", errEmergencyLimit,
			func(_ *testing.T, s *Simulation, _ *vehicle) target {
				for serial := range uint64(maxEmergencies) {
					s.emergencies = append(s.emergencies, emergencyRecord{serial: serial + 1, pod: 1})
				}
				s.incidentSerial = maxEmergencies
				return target{"01", 0}
			},
			func(s *Simulation, _ *vehicle) target { s.emergencies = s.emergencies[1:]; return target{"01", 0} }},
		{"unknown order", errOrderNotAboard,
			func(*testing.T, *Simulation, *vehicle) target { return target{"01", 9} },
			func(*Simulation, *vehicle) target { return target{"01", 2} }},
		{"completed order", errOrderNotAboard,
			func(_ *testing.T, _ *Simulation, v *vehicle) target {
				v.Riders[0].Completed = true
				return target{"01", 1}
			},
			func(*Simulation, *vehicle) target { return target{"01", 0} }},
		{"negative order", errOrderNotAboard,
			func(*testing.T, *Simulation, *vehicle) target { return target{"01", -1} },
			func(*Simulation, *vehicle) target { return target{"01", 1} }},
		{"incident limit", errIncidentLimit,
			func(_ *testing.T, s *Simulation, _ *vehicle) target {
				s.incidentSerial = math.MaxUint64
				return target{"01", 0}
			},
			func(s *Simulation, _ *vehicle) target { s.incidentSerial--; return target{"01", 0} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := emergencyFleet(t)
			to := test.cause(t, s, v)
			refusalOracle(t, s, test.want, func() error {
				_, err := s.Emergency(to.pod, to.order)
				return err
			})
			if test.undo == nil {
				return
			}
			to = test.undo(s, v)
			if _, err := s.Emergency(to.pod, to.order); err != nil {
				t.Fatalf("after the undo: %v", err)
			}
		})
	}
}

// TestEmergencyPreconditionOrder checks the order of the preconditions:
// with several causes at once, the start returns the error of the first.
func TestEmergencyPreconditionOrder(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	s.emergenciesOn = false
	s.pass = &dispatchPass{active: true}
	for serial := range uint64(maxEmergencies) {
		s.emergencies = append(s.emergencies, emergencyRecord{serial: serial + 1, pod: 1})
	}
	s.incidentSerial = math.MaxUint64
	steps := []struct {
		pod   string
		order int
		want  error
		undo  func()
	}{
		{"09", 9, errEmergenciesOff, func() { s.emergenciesOn = true }},
		{"09", 9, errUnknownPod, nil},
		{"01", 9, errEmergencyDispatch, func() { s.pass = nil }},
		{"01", 9, errEmergencyLimit, func() { s.emergencies = nil }},
		{"01", 9, errOrderNotAboard, nil},
		{"01", 0, errIncidentLimit, func() { s.incidentSerial = 0 }},
	}
	for _, step := range steps {
		if _, err := s.Emergency(step.pod, step.order); !errors.Is(err, step.want) {
			t.Fatalf("pod %s, order %d: error %v, want %v", step.pod, step.order, err, step.want)
		}
		if step.undo != nil {
			step.undo()
		}
	}
	startEmergency(t, s, v, 0)
	if len(s.emergencies) != 1 || s.emergencies[0].pod != s.vehicleIndex(v) {
		t.Fatalf("records %v", emergencyIDs(s))
	}
}

// TestEmergencyStartRecord checks the ID, the record, the counter, and the
// hold of a start on a traveling pod, which stays deferred. orderID 0
// selects the first active rider, and a completed rider is skipped.
func TestEmergencyStartRecord(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		order, party  int
		completeFirst bool
	}{
		{name: "default party", order: 0, party: 1},
		{name: "named party", order: 2, party: 2},
		{name: "default after a completed rider", order: 0, party: 2, completeFirst: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := emergencyFleet(t)
			// After s1, order 1 is completed history. In its arrival
			// chain, the pod cannot divert, so it stays deferred.
			station := "s1"
			if test.completeFirst {
				station = "s2"
			}
			travelToArrivalChain(t, s, v, station)
			checkEmergenciesEachTick(t, s)
			s.SetIncidentGeneration(7)
			s.incidentSerial = 41
			route, destination := slices.Clone(v.Route), v.destination
			if id := startEmergency(t, s, v, test.order); id != "i7.42" {
				t.Fatalf("ID %s, want i7.42", id)
			}
			want := []emergencyRecord{{generation: 7, serial: 42, start: s.tick, pod: s.vehicleIndex(v), order: test.party}}
			if !slices.Equal(s.emergencies, want) || s.emergencyCounters != (emergencyCounters{started: 1}) {
				t.Fatalf("records %+v, counters %+v, want %+v", s.emergencies, s.emergencyCounters, want)
			}
			if v.withdrawn != emergencyHold || v.op != (operationalDestination{}) || !slices.Equal(v.Route, route) || v.destination != destination {
				t.Fatalf("holds %#x, purpose %+v, route changed %t", v.withdrawn, v.op, !slices.Equal(v.Route, route))
			}
			s.Step()
			if s.emergencyCounters.emergencyTicks != 1 {
				t.Fatalf("emergency ticks %d, want 1", s.emergencyCounters.emergencyTicks)
			}
		})
	}
}

// TestEmergencyAtBerth starts an emergency on a pod at a berth in each
// berth activity. The unload starts at once, also in a paused simulation,
// and an unloading pod keeps its phase count. At the end of the interval
// the party is interrupted, also at its own destination, an unmarked rider
// at its destination completes, and each other rider is transferred. The
// record ends in that tick, and the pod returns to service.
func TestEmergencyAtBerth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare brings pod 01 to its berth activity.
		prepare func(t *testing.T, s *Simulation, v *vehicle)
		// order is the party, interrupted the orders that end
		// interrupted, completed the orders that complete, and
		// transferred the orders that the queue gets.
		order                               int
		interrupted, completed, transferred []int
		paused                              bool
	}{
		{name: "boarding", prepare: func(*testing.T, *Simulation, *vehicle) {}, order: 0, interrupted: []int{1}, transferred: []int{2}},
		{name: "boarding paused", prepare: func(*testing.T, *Simulation, *vehicle) {}, order: 2, interrupted: []int{2}, transferred: []int{1}, paused: true},
		{name: "continuing", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading })
			s.alight(v)
			s.continueJourney(v)
			if v.Pod.Activity != Continuing {
				t.Fatal("the pod does not continue")
			}
		}, order: 0, interrupted: []int{2}},
		{name: "unloading", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks > 1 })
		}, order: 1, interrupted: []int{1}, transferred: []int{2}},
		{name: "unloading, other party", prepare: func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			stepUntil(t, s, "intermediate unload", func() bool { return v.Pod.Activity == Unloading && v.phaseTicks > 1 })
		}, order: 2, interrupted: []int{2}, completed: []int{1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := emergencyFleet(t)
			test.prepare(t, s, v)
			checkEmergenciesEachTick(t, s)
			s.SetPaused(test.paused)
			station, berth := v.Pod.StationID, v.Pod.BerthID
			phase := v.phaseTicks
			unloading := v.Pod.Activity == Unloading
			startEmergency(t, s, v, test.order)
			if v.Pod.Activity != Unloading || v.op.purpose != opEmergencyUnload || v.op.owner != emergencyHold || v.Pod.BerthID != berth {
				t.Fatalf("pod %s at %s, purpose %+v", v.Pod.Activity, v.Pod.BerthID, v.op)
			}
			if unloading && v.phaseTicks != phase || !unloading && v.phaseTicks != unloadingTicks {
				t.Fatalf("phase count %d, was %d", v.phaseTicks, phase)
			}
			if test.paused {
				s.Step()
				if v.phaseTicks != unloadingTicks || len(s.emergencies) != 1 {
					t.Fatal("a paused simulation ran a tick")
				}
				s.SetPaused(false)
			}
			completed, interrupted := s.completed, s.interrupted
			stepUntil(t, s, "the end of the unload", func() bool { return v.Pod.Activity != Unloading })
			if len(s.emergencies) != 0 || v.withdrawn != 0 || s.emergencyCounters.ended != 1 {
				t.Fatalf("records %v, holds %#x, counters %+v", emergencyIDs(s), v.withdrawn, s.emergencyCounters)
			}
			if !slices.Equal(s.undelivered, test.interrupted) || s.interrupted != interrupted+len(test.interrupted) {
				t.Fatalf("interrupted %v, want %v", s.undelivered, test.interrupted)
			}
			if s.completed != completed+len(test.completed) {
				t.Fatalf("completed %d more, want %v", s.completed-completed, test.completed)
			}
			for _, id := range test.transferred {
				index := slices.IndexFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.ID == id })
				aboard := slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID == id && !rider.Completed })
				if index < 0 && !aboard || index >= 0 && s.waiting[index].request.LegFrom != station {
					t.Fatalf("order %d is not transferred at %s: queue %v", id, station, queueIDs(s))
				}
			}
		})
	}
}

// TestEmergencyEndSupply checks that a pod that the record end releases
// is supply in the dispatch of the same tick: the party that the unload
// transferred boards the emergency pod again in the tick of the end.
func TestEmergencyEndSupply(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	checkEmergenciesEachTick(t, s)
	startEmergency(t, s, v, 1)
	for len(s.emergencies) > 0 {
		s.Step()
		if s.tick > 10*TicksPerSecond {
			t.Fatal("the record does not end")
		}
	}
	if v.withdrawn != 0 || !slices.Equal(s.undelivered, []int{1}) {
		t.Fatalf("holds %#x, interrupted %v", v.withdrawn, s.undelivered)
	}
	if v.Pod.Activity != Boarding || !slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID == 2 && !rider.Completed }) {
		t.Fatalf("the transferred order 2 does not board pod 01 in the tick of the end: pod %s, queue %v", v.Pod.Activity, queueIDs(s))
	}
}

// TestEmergencyArrivalChain checks a deferred traveling pod. It keeps its
// route and arrives at its next stop by the ordinary arrival, and the next
// emergency stage starts the unload there with the phase count of the
// arrival. The party is interrupted, the other rider at its destination
// completes, and the record ends in the tick of the unload.
func TestEmergencyArrivalChain(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	travelToArrivalChain(t, s, v, "s1")
	checkEmergenciesEachTick(t, s)
	route := slices.Clone(v.Route)
	startEmergency(t, s, v, 2)
	stepUntil(t, s, "the arrival", func() bool { return v.Pod.Activity == Unloading })
	if v.op.purpose != opService || !slices.Equal(v.Route, route) || v.Pod.StationID != "s1" {
		t.Fatalf("the deferred pod changed its route or purpose: %+v at %s", v.op, v.Pod.StationID)
	}
	phase := v.phaseTicks
	s.Step()
	if v.op.purpose != opEmergencyUnload || v.op.interrupt != 2 || v.phaseTicks != phase-1 {
		t.Fatalf("purpose %+v, phase count %d after %d", v.op, v.phaseTicks, phase)
	}
	completed := s.completed
	stepUntil(t, s, "the end of the unload", func() bool { return len(s.emergencies) == 0 })
	if !slices.Equal(s.undelivered, []int{2}) || s.completed != completed+1 || v.RidersAboard() != 0 {
		t.Fatalf("interrupted %v, completed %d more", s.undelivered, s.completed-completed)
	}
	if v.withdrawn != 0 || s.emergencyCounters.started != 1 || s.emergencyCounters.ended != 1 || s.emergencyCounters.emergencyTicks == 0 {
		t.Fatalf("holds %#x, counters %+v", v.withdrawn, s.emergencyCounters)
	}
}

// TestEmergencyPartyIndex checks that the record names the party by its
// order ID. A rider before the party leaves the pod while the pod is
// deferred, so the index of the party changes. The unload still
// interrupts the party, and not the rider at its old index.
func TestEmergencyPartyIndex(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	s.emergenciesOn = true
	v := boardParties(t, s, "s1", "s1", "s1")
	travelToArrivalChain(t, s, v, "s1")
	checkEmergenciesEachTick(t, s)
	startEmergency(t, s, v, 2)
	if err := s.InterruptRider("01", 1); err != nil {
		t.Fatal(err)
	}
	completed := s.completed
	stepUntil(t, s, "the end of the unload", func() bool { return len(s.emergencies) == 0 })
	if !slices.Equal(s.undelivered, []int{1, 2}) || s.completed != completed+1 {
		t.Fatalf("interrupted %v, completed %d more, want orders 1 and 2 interrupted and order 3 complete", s.undelivered, s.completed-completed)
	}
}

// TestEmergencyFixedRiders checks that InterruptRider refuses a pod whose
// record is bound or unloading, for the party and for an earlier or a
// later rider, and changes nothing. The interrupt set names the party by
// its index, so a removed rider would move the set onto another rider.
func TestEmergencyFixedRiders(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"bound", "unloading"} {
		s := incidentLegFleet(t)
		s.emergenciesOn = true
		v := boardParties(t, s, "s1", "s1", "s1")
		if phase == "bound" {
			travelOn(t, s, v, "s0-link")
			boundEmergency(t, s, v)
		} else {
			startEmergency(t, s, v, 2)
		}
		if v.op.purpose == opService {
			t.Fatalf("%s: the pod has no operational destination", phase)
		}
		for _, order := range []int{1, 2, 3} {
			before := s.Clone()
			if err := s.InterruptRider(v.Pod.ID, order); err == nil {
				t.Fatalf("%s: the interruption of order %d succeeded", phase, order)
			}
			if !sameState(before, s) {
				t.Fatalf("%s: the refused interruption of order %d changed the state", phase, order)
			}
		}
		if err := s.CheckContract(); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
	}
}

// TestEmergencyPartyLeaves checks a faulted pod with a deferred record.
// The pod keeps both holds while it is faulted. When an interruption ends
// the party, the next emergency stage ends the record before the faulted
// skip, and the pod keeps only its fault hold.
func TestEmergencyPartyLeaves(t *testing.T) {
	t.Parallel()
	s := incidentLegFleet(t)
	s.emergenciesOn, s.faultsOn = true, true
	s.faultSettings.evacuationSeconds = 300
	v := boardParties(t, s, "s1", "s1")
	travelOn(t, s, v, "s0-link")
	checkEmergenciesEachTick(t, s)
	startFault(t, s, v, 0)
	waiting := slices.Clone(s.waiting)
	startEmergency(t, s, v, 1)
	if v.withdrawn != faultHold|emergencyHold || v.op.purpose != opService || !reflect.DeepEqual(s.waiting, waiting) {
		t.Fatalf("holds %#x, purpose %+v", v.withdrawn, v.op)
	}
	if err := s.InterruptRider("01", 1); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if len(s.emergencies) != 0 || v.withdrawn != faultHold || s.emergencyCounters.ended != 1 {
		t.Fatalf("records %v, holds %#x", emergencyIDs(s), v.withdrawn)
	}
}

// TestEmergencyPickups checks that a start releases each pending pickup of
// the pod with the exclusion, and that the released trip is served by
// another pod and never by the emergency pod before it boards.
func TestEmergencyPickups(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	travelOn(t, s, v, "s0-link")
	trip := newTrip(s, "s0", "s2")
	trip.deferUntil, trip.deferCheck, trip.deferPodID = s.tick+maxDispatchDeferral, s.tick+TicksPerSecond, "01"
	trip.request.DispatchReason = "Waiting for pod 01 to finish"
	s.waiting = append(s.waiting, trip)
	withdrawn := s.Clone()
	if err := withdrawn.withdrawService(withdrawn.findVehicle("01"), emergencyHold); err != nil {
		t.Fatal(err)
	}
	checkEmergenciesEachTick(t, s)
	startEmergency(t, s, v, 0)
	if !reflect.DeepEqual(s.waiting, withdrawn.waiting) || s.waiting[0].excludedPod != "01" {
		t.Fatalf("queue after the start %+v, want %+v", s.waiting, withdrawn.waiting)
	}
	id := trip.request.ID
	check := s.monitor
	s.monitor = func(s *Simulation) {
		check(s)
		if err := checkExclusions(s); err != nil {
			t.Fatal(err)
		}
		for _, waiting := range s.waiting {
			if waiting.request.ID == id && (waiting.excludedPod != "01" || waiting.request.PodID == "01") {
				t.Fatalf("tick %d: the trip %+v", s.tick, waiting)
			}
		}
	}
	stepUntil(t, s, "the trip boards", func() bool {
		return !slices.ContainsFunc(s.waiting, func(waiting waitingTrip) bool { return waiting.request.ID == id })
	})
	if slices.ContainsFunc(v.Riders, func(rider Request) bool { return rider.ID == id }) || len(s.emergencies) != 0 {
		t.Fatalf("pod 01 carries the trip, or its record stays: %v", emergencyIDs(s))
	}
}

// boundEmergency starts an emergency for order 1 on the traveling pod v,
// and binds it to s1-2 with the stage 1 operation. Patch 3 of the incident
// emergency contract adds the station choice that binds a pod in Step.
func boundEmergency(t *testing.T, s *Simulation, v *vehicle) {
	t.Helper()
	startEmergency(t, s, v, 1)
	if err := s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: 1, station: "s1", berth: "s1-2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestEmergencyFaults checks each phase of section 5.7 of the incident
// emergency contract with a fault. While the fault lasts, the pod keeps
// its phase. A clear before the evacuation continues the emergency, and an
// evacuation ends the record in the tick of the evacuation.
func TestEmergencyFaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare starts the emergency on pod 01 and returns the purpose
		// that the pod keeps while the fault lasts.
		prepare func(t *testing.T, s *Simulation, v *vehicle) opPurpose
	}{
		{"deferred", func(t *testing.T, s *Simulation, v *vehicle) opPurpose {
			t.Helper()
			travelToArrivalChain(t, s, v, "s1")
			startEmergency(t, s, v, 1)
			return opService
		}},
		{"bound", func(t *testing.T, s *Simulation, v *vehicle) opPurpose {
			t.Helper()
			travelOn(t, s, v, "s0-link")
			boundEmergency(t, s, v)
			return opEmergencyUnload
		}},
		{"unloading", func(t *testing.T, s *Simulation, v *vehicle) opPurpose {
			t.Helper()
			startEmergency(t, s, v, 1)
			s.Step()
			return opEmergencyUnload
		}},
	}
	for _, test := range tests {
		for _, evacuate := range []bool{false, true} {
			name := test.name + map[bool]string{false: ", clear", true: ", evacuation"}[evacuate]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				s, v := emergencyFleet(t)
				s.faultsOn = true
				s.faultSettings.evacuationSeconds = 300
				checkEmergenciesEachTick(t, s)
				purpose := test.prepare(t, s, v)
				phase := v.phaseTicks
				id := startFault(t, s, v, 0)
				for range 30 * TicksPerSecond {
					s.Step()
					if v.op.purpose != purpose || len(s.emergencies) != 1 || v.withdrawn != faultHold|emergencyHold {
						t.Fatalf("tick %d: purpose %+v, records %v, holds %#x", s.tick, v.op, emergencyIDs(s), v.withdrawn)
					}
				}
				if v.Pod.Speed != 0 || v.Pod.Activity == Unloading && v.phaseTicks != phase {
					t.Fatalf("speed %g, phase count %d, was %d", v.Pod.Speed, v.phaseTicks, phase)
				}
				if evacuate {
					s.faultSettings.evacuationSeconds = 0
					s.Step()
					if v.RidersAboard() != 0 || len(s.emergencies) != 0 || v.withdrawn != faultHold {
						t.Fatalf("after the evacuation: riders %d, records %v, holds %#x", v.RidersAboard(), emergencyIDs(s), v.withdrawn)
					}
					return
				}
				if err := s.clearFault(id); err != nil {
					t.Fatal(err)
				}
				stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
				if !slices.Contains(s.undelivered, 1) || v.withdrawn != 0 {
					t.Fatalf("interrupted %v, holds %#x", s.undelivered, v.withdrawn)
				}
			})
		}
	}
}

// TestEmergencyOnFaultedPod checks a start on a faulted pod at a berth
// with riders. It is accepted and deferred: the pod gets the second hold,
// no pickup is released again, and it does not unload while the fault
// lasts. After the clear, the next stage starts the unload.
func TestEmergencyOnFaultedPod(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	s.faultsOn = true
	s.faultSettings.evacuationSeconds = 300
	checkEmergenciesEachTick(t, s)
	id := startFault(t, s, v, 0)
	waiting := slices.Clone(s.waiting)
	startEmergency(t, s, v, 1)
	if v.withdrawn != faultHold|emergencyHold || !reflect.DeepEqual(s.waiting, waiting) {
		t.Fatalf("holds %#x, queue %v", v.withdrawn, queueIDs(s))
	}
	for range 3 * TicksPerSecond {
		s.Step()
		if v.op.purpose != opService || v.Pod.Activity != Boarding {
			t.Fatalf("tick %d: the faulted pod acts: %s with %+v", s.tick, v.Pod.Activity, v.op)
		}
	}
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if v.op.purpose != opEmergencyUnload || v.Pod.Activity != Unloading {
		t.Fatalf("after the clear: %s with %+v", v.Pod.Activity, v.op)
	}
}

// TestEmergencyCloneAndReset checks that a clone copies the records and
// the counters and replays exactly, that the source does not change the
// clone, and that Reset ends every record with its hold and clears the
// counters. Reset keeps the serial and the switch.
func TestEmergencyCloneAndReset(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	travelOn(t, s, v, "s0-link")
	startEmergency(t, s, v, 2)
	s.Step()
	checkpoint, twin := s.Clone(), s.Clone()
	stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
	if len(checkpoint.emergencies) != 1 || !sameState(checkpoint, twin) {
		t.Fatalf("the source changed the clone: records %v", emergencyIDs(checkpoint))
	}
	for checkpoint.tick < s.tick {
		checkpoint.Step()
	}
	if !sameState(checkpoint, s) {
		t.Fatal("the replay differs from the source")
	}
	twin.Reset()
	if twin.emergencies != nil || twin.emergencyCounters != (emergencyCounters{}) || !twin.emergenciesOn || twin.incidentSerial != 1 {
		t.Fatalf("records %v, counters %+v, on %t, serial %d", emergencyIDs(twin), twin.emergencyCounters, twin.emergenciesOn, twin.incidentSerial)
	}
	for index := range twin.vehicles {
		if twin.vehicles[index].withdrawn != 0 {
			t.Fatalf("pod %s keeps the holds %#x", twin.vehicles[index].Pod.ID, twin.vehicles[index].withdrawn)
		}
	}
	if err := twin.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestEmergencyCountersSaturate checks that each counter stops at
// math.MaxInt64, and that a start and an end complete with a counter at
// the maximum.
func TestEmergencyCountersSaturate(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	maximum := int64(math.MaxInt64)
	s.emergencyCounters = emergencyCounters{started: maximum, ended: maximum, emergencyTicks: maximum - 1}
	startEmergency(t, s, v, 0)
	stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
	want := emergencyCounters{started: maximum, ended: maximum, emergencyTicks: maximum}
	if s.emergencyCounters != want {
		t.Fatalf("counters %+v, want each at the maximum", s.emergencyCounters)
	}
	counter := maximum - 2
	addCount(&counter, maximum)
	if counter != maximum {
		t.Fatalf("a large addition gives %d", counter)
	}
}

// TestEmergenciesOnWithoutEmergencies checks that emergencies on with no
// record changes no state: a run with emergencies on equals a run with
// emergencies off, apart from the switch.
func TestEmergenciesOnWithoutEmergencies(t *testing.T) {
	t.Parallel()
	off, on := newTraffic(t), newTraffic(t)
	for _, s := range []*Simulation{off, on} {
		if err := s.StartDemo(); err != nil {
			t.Fatal(err)
		}
	}
	on.emergenciesOn = true
	for range 120 * TicksPerSecond {
		off.Step()
		on.Step()
	}
	on.emergenciesOn = false
	if off.completed == 0 || !sameState(off, on) {
		t.Fatalf("the runs differ: completed %d and %d", off.completed, on.completed)
	}
}

// TestEmergencyInvariants checks that CheckContract finds each damage of
// the mutation table of section 8 of the incident emergency contract. Pod
// 02 has a fault with serial 1, and pod 01 unloads for the emergency with
// serial 2.
func TestEmergencyInvariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		damage func(s *Simulation, v *vehicle)
		want   string
	}{
		{"second record for one pod", func(s *Simulation, _ *vehicle) {
			s.incidentSerial++
			record := s.emergencies[0]
			record.serial = s.incidentSerial
			s.emergencies = append(s.emergencies, record)
		}, "E1: pod 01 has two emergency records"},
		{"record out of serial order", func(s *Simulation, _ *vehicle) {
			s.incidentSerial++
			s.emergencies = slices.Insert(s.emergencies, 0, emergencyRecord{serial: s.incidentSerial, pod: 1})
		}, "is not after emergency"},
		{"serial 0", func(s *Simulation, _ *vehicle) { s.emergencies[0].serial = 0 }, "E1: emergency i0.0 has a serial outside"},
		{"serial above the incident serial", func(s *Simulation, _ *vehicle) { s.emergencies[0].serial = 3 }, "has a serial outside"},
		{"serial of a fault record", func(s *Simulation, _ *vehicle) { s.emergencies[0].serial = 1 }, "has the serial of a fault record"},
		{"too many records", func(s *Simulation, _ *vehicle) {
			for range maxEmergencies {
				s.emergencies = append(s.emergencies, s.emergencies[0])
			}
		}, "E1: 5 emergency records"},
		{"start after the tick", func(s *Simulation, _ *vehicle) { s.emergencies[0].start = s.tick + 1 }, "starts at tick"},
		{"pod outside the fleet", func(s *Simulation, _ *vehicle) { s.emergencies[0].pod = 2 }, "outside the fleet"},
		{"orphan hold", func(s *Simulation, _ *vehicle) { s.emergencies = nil }, "E2: pod 01 has the emergency hold"},
		{"interrupt bit of another rider", func(_ *Simulation, v *vehicle) { v.op.interrupt = 2 }, "E3: pod 01 interrupts the riders 0x2"},
		{"party not aboard", func(s *Simulation, _ *vehicle) { s.emergencies[0].order = 9 }, "E3: the party 9"},
		{"owner of another hold", func(_ *Simulation, v *vehicle) {
			v.withdrawn |= faultHold
			v.op.owner = faultHold
		}, "E4: the emergency unload of pod 01 has the owner 0x1"},
		{"emergency unload without record", func(s *Simulation, v *vehicle) {
			s.emergencies, v.withdrawn = nil, 0
		}, "E4: pod 01 has an emergency unload and no emergency record"},
		{"no passenger", func(_ *Simulation, v *vehicle) { v.Pod.Occupied = false }, "E5: pod 01"},
		{"emergencies off", func(s *Simulation, _ *vehicle) { s.emergenciesOn, s.faultsOn = false, false }, "E7: 1 emergency records"},
		// checkPodOperational accepts this pod: it holds at a refuge that
		// is not a stop of its riders.
		{"refuge with an active rider", func(_ *Simulation, v *vehicle) {
			v.op = operationalDestination{purpose: opRefuge, owner: emergencyHold}
			v.phaseTicks, v.Pod.WaitReason = 0, refugeHolding
		}, "E8: pod 01"},
		{"third hold", func(_ *Simulation, v *vehicle) { v.withdrawn |= 4 }, "not only the fault hold and the emergency hold"},
		{"approach member with the hold", func(s *Simulation, v *vehicle) {
			s.couplingApproaches = []couplingNativeApproach{{context: &couplingApproachContext{members: [2]couplingApproachMember{{id: "01"}, {id: "03"}}}}}
		}, "E6: coupling member 01"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := emergencyFleet(t)
			s.faultsOn = true
			s.faultSettings.evacuationSeconds = 300
			startFault(t, s, s.findVehicle("02"), 0)
			startEmergency(t, s, v, 1)
			if err := s.CheckContract(); err != nil {
				t.Fatalf("before the damage: %v", err)
			}
			test.damage(s, v)
			if err := s.CheckContract(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %v, want one with %q", err, test.want)
			}
		})
	}
}

// TestEmergencyHoldCallers checks the code part of F11: only Emergency
// and advanceEmergency call withdrawService with the emergency hold.
func TestEmergencyHoldCallers(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var callers []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if hold, isIdent := call.Args[1].(*ast.Ident); ok && isIdent && selector.Sel.Name == "withdrawService" && hold.Name == "emergencyHold" {
					callers = append(callers, function.Name.Name)
				}
				return true
			})
		}
	}
	slices.Sort(callers)
	if want := []string{"Emergency", "advanceEmergency"}; !slices.Equal(callers, want) {
		t.Fatalf("callers of withdrawService with the emergency hold %v, want %v", callers, want)
	}
}

// TestEmergencyCouplingMember starts an emergency on the occupied rear
// member of a committed train. The member gets no hold, and the train
// keeps its plan with no rider change until it retires at its split site.
// The next emergency stage withdraws the pod, which then binds on its
// cadence and unloads.
func TestEmergencyCouplingMember(t *testing.T) {
	t.Parallel()
	s := newCouplingApproachJourney(t, true)
	s.emergenciesOn = true
	rear := s.findVehicle("rear")
	for range 18000 {
		if len(s.couplingGroups) != 0 && rear.couplingID != "" {
			break
		}
		s.Step()
	}
	if rear.couplingID == "" {
		t.Fatal("the train did not form")
	}
	checkEmergenciesEachTick(t, s)
	riders := slices.Clone(rear.Riders)
	party := riders[0].ID
	startEmergency(t, s, rear, 0)
	start := s.tick
	if rear.withdrawn != 0 || rear.op.purpose != opService || len(s.emergencies) != 1 {
		t.Fatalf("the member has the holds %#x and the purpose %+v", rear.withdrawn, rear.op)
	}
	for rear.couplingID != "" {
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatal(err)
		}
		if rear.withdrawn != 0 || !slices.Equal(rear.Riders, riders) {
			t.Fatalf("tick %d: the member has the holds %#x, riders %+v", s.tick, rear.withdrawn, rear.Riders)
		}
		if s.tick > 30000 {
			t.Fatal("the train did not retire")
		}
	}
	s.Step()
	if rear.withdrawn != emergencyHold && len(s.emergencies) == 1 {
		t.Fatalf("the stage after the split did not withdraw the pod: holds %#x", rear.withdrawn)
	}
	// The retired member can divert, so it binds on its cadence.
	if _, _, ok := s.divertStart(rear); !ok {
		t.Fatal("the retired member cannot divert")
	}
	stepUntil(t, s, "the bind", func() bool { return rear.op.purpose == opEmergencyUnload })
	if (s.tick-start)%60 != 0 {
		t.Fatalf("the pod binds at tick %d, %d ticks after the start", s.tick, s.tick-start)
	}
	stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
	if !slices.Contains(s.undelivered, party) || rear.withdrawn != 0 {
		t.Fatalf("interrupted %v, holds %#x", s.undelivered, rear.withdrawn)
	}
}
