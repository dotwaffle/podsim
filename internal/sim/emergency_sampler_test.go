package sim

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// startEmergencySampler starts the emergency sampler of s at 60 events an
// hour, with the seed words 7 and 8.
func startEmergencySampler(t *testing.T, s *Simulation) {
	t.Helper()
	if err := s.StartIncidentSampling(IncidentRates{EmergenciesPerHour: 60, EmergencySeed: [2]uint64{7, 8}}); err != nil {
		t.Fatal(err)
	}
}

// emergencyDue makes the emergency sampler due in the next tick, from the
// first stream whose pod and party draws meet accept.
func emergencyDue(t *testing.T, s *Simulation, accept func(pod, party float64) bool) {
	t.Helper()
	s.emergencySampler.nextTick = s.tick + 1
	s.emergencySampler.stream = streamWith(t, 2, func(draws []float64) bool { return accept(draws[0], draws[1]) })
}

// submitSharedTrips adds a shared trip every 30 ticks while the queue is
// short, in a fixed rotation of the passenger stations, so pods carry more
// than one party.
func submitSharedTrips(t *testing.T, s *Simulation) {
	t.Helper()
	if s.tick%(TicksPerSecond/2) != 0 || len(s.waiting) >= 8 {
		return
	}
	stations := []string{"harbor", "garden", "market"}
	from := int(s.tick/(TicksPerSecond/2)) % len(stations)
	if _, err := s.SubmitTripOptions(TripOptions{From: stations[from], To: stations[(from+1)%len(stations)], SharingConsent: SharedConsent}); err != nil {
		t.Fatal(err)
	}
}

// sharedTraceFleet returns traceFleet with shared rides.
func sharedTraceFleet(t *testing.T) *Simulation {
	t.Helper()
	s := traceFleet(t)
	if err := s.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
		t.Fatal(err)
	}
	return s
}

// TestEmergencySamplerTrace pins 20 events of the emergency sampler: the role
// and the value of each draw, then the target, the outcome, and the next tick
// (section 5.1.8 of the incident stage 7 contract).
//
// Each pod target is checked against its draw on each CPU type. A run is
// deterministic on one architecture only (section 5.1.4), so the fixture
// shape and the pinned text hold on amd64.
func TestEmergencySamplerTrace(t *testing.T) {
	t.Parallel()
	s := sharedTraceFleet(t)
	checkEmergenciesEachTick(t, s)
	startEmergencySampler(t, s)
	var trace []string
	starts, skips, shared := 0, 0, 0
	for len(trace) < 20 {
		submitSharedTrips(t, s)
		before := s.emergencySampler
		started := s.emergencyCounters.started
		aboard := make([]int, len(s.vehicles))
		for index := range s.vehicles {
			aboard[index] = s.vehicles[index].RidersAboard()
		}
		s.Step()
		if !before.due(s.tick) {
			continue
		}
		draws := pcgDraws(before.stream, 3)
		outcome := "skip"
		if s.emergencyCounters.started > started {
			starts++
			record := s.emergencies[len(s.emergencies)-1]
			if want := int(draws[0] * float64(len(s.vehicles))); record.pod != want {
				t.Fatalf("tick %d: emergency on pod index %d, want %d for the draw %v", s.tick, record.pod, want, draws[0])
			}
			if aboard[record.pod] > 1 {
				shared++
			}
			outcome = fmt.Sprintf("pod %s order=%d start=%d", s.vehicles[record.pod].Pod.ID, record.order, record.start)
		} else {
			skips++
		}
		trace = append(trace, fmt.Sprintf("tick=%d aboard=%v pod=%.6f party=%.6f interval=%.6f %s next=%d",
			s.tick, aboard, draws[0], draws[1], draws[2], outcome, s.emergencySampler.nextTick))
	}
	if c := s.emergencyCounters; c.sampled != 20 || c.skipped != int64(skips) {
		t.Fatalf("counters %+v, want 20 sampled and %d skipped", c, skips)
	}
	if !incidentTraceGolden() {
		return
	}
	if starts == 0 || skips == 0 || shared == 0 {
		t.Fatalf("the fixture has %d starts, %d skips, and %d shared pods:\n%s", starts, skips, shared, strings.Join(trace, "\n"))
	}
	const want = `tick=1170 aboard=[2 2 2 0 0] pod=0.414406 party=0.910334 interval=0.541557 pod 03 order=6 start=1170 next=3978
tick=3978 aboard=[4 0 2 4 0] pod=0.165492 party=0.387480 interval=0.302576 pod 01 order=11 start=3978 next=5276
tick=5276 aboard=[4 0 2 4 3] pod=0.207323 party=0.477128 interval=0.557474 skip next=8211
tick=8211 aboard=[4 0 4 4 0] pod=0.409488 party=0.696741 interval=0.223999 pod 03 order=32 start=8211 next=9124
tick=9124 aboard=[4 0 4 4 0] pod=0.889645 party=0.773342 interval=0.399482 skip next=10960
tick=10960 aboard=[4 0 4 4 4] pod=0.930243 party=0.455586 interval=0.058149 pod 05 order=37 start=10960 next=11176
tick=11176 aboard=[4 0 4 4 4] pod=0.771602 party=0.474173 interval=0.647292 pod 04 order=25 start=11176 next=14928
tick=14928 aboard=[4 0 4 4 4] pod=0.947982 party=0.393937 interval=0.967787 skip next=27296
tick=27296 aboard=[4 4 1 4 0] pod=0.291686 party=0.164778 interval=0.638588 pod 02 order=83 start=27296 next=30960
tick=30960 aboard=[4 4 1 4 0] pod=0.549365 party=0.226138 interval=0.779091 pod 03 order=95 start=30960 next=36397
tick=36397 aboard=[4 4 2 4 0] pod=0.452771 party=0.119224 interval=0.987563 pod 03 order=125 start=36397 next=52191
tick=52191 aboard=[4 1 0 4 4] pod=0.628896 party=0.549990 interval=0.521900 pod 04 order=174 start=52191 next=54848
tick=54848 aboard=[4 1 0 0 4] pod=0.828857 party=0.556456 interval=0.115502 pod 05 order=185 start=54848 next=55290
tick=55290 aboard=[4 4 0 0 4] pod=0.357789 party=0.311535 interval=0.994159 pod 02 order=195 start=55290 next=73804
tick=73804 aboard=[4 4 0 4 0] pod=0.389343 party=0.267405 interval=0.190280 pod 02 order=247 start=73804 next=74564
tick=74564 aboard=[4 4 0 4 0] pod=0.648463 party=0.962999 interval=0.798308 pod 04 order=243 start=74564 next=80328
tick=80328 aboard=[4 3 0 4 0] pod=0.198137 party=0.437406 interval=0.273826 pod 01 order=262 start=80328 next=81480
tick=81480 aboard=[4 3 0 4 0] pod=0.014661 party=0.339359 interval=0.286171 skip next=82694
tick=82694 aboard=[4 3 0 4 0] pod=0.127582 party=0.215210 interval=0.254637 skip next=83752
tick=83752 aboard=[4 3 0 4 0] pod=0.222128 party=0.332889 interval=0.262765 pod 02 order=275 start=83752 next=84850`
	if got := strings.Join(trace, "\n"); got != want {
		t.Fatalf("the trace differs:\n%s", got)
	}
}

// TestSampledEmergencyPlatoon checks decision 3 of the incident stage 7
// contract. A sampled event accepts a linked platoon leader or follower with
// no skip. The record stays deferred while the link drains, and the pod then
// binds on its cadence. Faults refuse platoon members, and emergencies do not.
func TestSampledEmergencyPlatoon(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		end string
		pod func(leader, follower *vehicle) *vehicle
		// accept selects the pod draw of the end: index 0 is the leader.
		accept func(pod, party float64) bool
	}{
		{"leader", func(leader, _ *vehicle) *vehicle { return leader }, func(pod, _ float64) bool { return pod < 0.5 }},
		{"follower", func(_, follower *vehicle) *vehicle { return follower }, func(pod, _ float64) bool { return pod >= 0.5 }},
	} {
		t.Run(test.end, func(t *testing.T) {
			t.Parallel()
			s, leader, follower := emergencyPlatoon(t)
			checkEmergenciesEachTick(t, s)
			startEmergencySampler(t, s)
			s.emergencySampler.nextTick = MaxCounter
			s.Step()
			if follower.link.leader == 0 || follower.link.draining || leader.follower == 0 {
				t.Fatalf("the first step did not link the pods: %+v", follower.link)
			}
			lanes := follower.link.lanes
			v := test.pod(leader, follower)
			emergencyDue(t, s, test.accept)
			s.Step()
			start := s.tick
			// One event: later events would count in the checks below.
			s.emergencySampler.nextTick = MaxCounter
			if c := s.emergencyCounters; c.sampled != 1 || c.skipped != 0 || c.started != 1 {
				t.Fatalf("the sampled start has the counters %+v", c)
			}
			if len(s.emergencies) != 1 || s.vehicles[s.emergencies[0].pod].Pod.ID != v.Pod.ID {
				t.Fatalf("the records are %+v, want one on pod %s", s.emergencies, v.Pod.ID)
			}
			for follower.link.leader != 0 {
				if phase := emergencyPhase(v); phase != EmergencyPhaseDeferred || v.op.purpose != opService {
					t.Fatalf("tick %d: the record of the linked pod has the phase %s and the purpose %+v", s.tick, phase, v.op)
				}
				s.Step()
				if follower.link.leader != 0 && (!follower.link.draining || follower.link.lanes != lanes) {
					t.Fatalf("tick %d: the link does not drain: %+v", s.tick, follower.link)
				}
				if s.tick > start+10*TicksPerSecond {
					t.Fatal("the link did not end")
				}
			}
			stepUntil(t, s, "the bind", func() bool { return v.op.purpose == opEmergencyUnload })
			if c := s.emergencyCounters; c.sampled != 1 || c.skipped != 0 {
				t.Fatalf("the deferred record has the counters %+v", c)
			}
		})
	}
}

// TestEmergencySamplerDrawCount checks that an event uses three draws for
// each outcome, and that a refused start changes no state and counts as a
// skip (sections 5.1.1 and 5.1.7 of the incident stage 7 contract).
func TestEmergencySamplerDrawCount(t *testing.T) {
	t.Parallel()
	first := func(pod, party float64) bool { return pod < 0.5 && party < 0.5 }
	second := func(pod, _ float64) bool { return pod >= 0.5 }
	for _, test := range []struct {
		name    string
		accept  func(pod, party float64) bool
		prepare func(t *testing.T, s *Simulation, v *vehicle)
		want    error
	}{
		{"accepted", first, nil, nil},
		{"no active rider", second, nil, errNoPassenger},
		{"pod has a record", first, func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			startEmergency(t, s, v, 0)
		}, errPodEmergency},
		{"not carrying", first, func(_ *testing.T, _ *Simulation, v *vehicle) {
			v.Pod.Activity, v.Pod.Occupied = Idle, false
		}, errNoPassenger},
		{"emergency limit", first, func(_ *testing.T, s *Simulation, _ *vehicle) {
			for serial := range uint64(MaxEmergencies) {
				s.emergencies = append(s.emergencies, emergencyRecord{serial: serial + 1, pod: 1})
			}
			s.incidentSerial = MaxEmergencies
		}, errEmergencyLimit},
		{"incident limit", first, func(_ *testing.T, s *Simulation, _ *vehicle) { s.incidentSerial = MaxCounter }, errIncidentLimit},
		{"no pod", first, func(_ *testing.T, s *Simulation, _ *vehicle) { s.vehicles = nil }, errUnknownPod},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := emergencyFleet(t)
			startEmergencySampler(t, s)
			emergencyDue(t, s, test.accept)
			s.emergencySampler.nextTick = s.tick
			if test.prepare != nil {
				test.prepare(t, s, v)
			}
			before := s.emergencySampler.stream
			draws := pcgDraws(before, 3)
			if err := s.Clone().startSampledEmergency(draws[0], draws[1]); !errors.Is(err, test.want) {
				t.Fatalf("the start gives %v, want %v", err, test.want)
			}
			state, records, serial, started := s.ExportState(), len(s.emergencies), s.incidentSerial, s.emergencyCounters.started
			s.sampleEmergencies()
			after := before
			for range 3 {
				after.Uint64()
			}
			if s.emergencySampler.stream != after {
				t.Fatal("the event did not use three draws")
			}
			if want := nextIncidentTick(s.tick, draws[2], 60); s.emergencySampler.nextTick != want {
				t.Fatalf("next tick %d, want %d", s.emergencySampler.nextTick, want)
			}
			if s.emergencyCounters.sampled != 1 {
				t.Fatalf("counters %+v", s.emergencyCounters)
			}
			if test.want == nil {
				if s.emergencyCounters.skipped != 0 || s.emergencyCounters.started != started+1 || len(s.emergencies) != records+1 {
					t.Fatalf("an accepted event has the counters %+v and %d records", s.emergencyCounters, len(s.emergencies))
				}
				return
			}
			if s.emergencyCounters.skipped != 1 || s.emergencyCounters.started != started {
				t.Fatalf("a refused event has the counters %+v", s.emergencyCounters)
			}
			if !reflect.DeepEqual(withoutSamplerMoves(s.ExportState()), withoutSamplerMoves(state)) || len(s.emergencies) != records || s.incidentSerial != serial {
				t.Fatal("a refused start changed the state")
			}
		})
	}
}

// TestEmergencySamplerParty checks the target of an event: the party draw is
// an index over the active riders, in Riders order, so a completed rider has
// no index.
func TestEmergencySamplerParty(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		completed bool
		party     float64
		want      int
	}{
		{"first of two", false, 0.25, 0},
		{"second of two", false, 0.75, 1},
		{"one active rider", true, 0.75, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := emergencyFleet(t)
			startEmergencySampler(t, s)
			if test.completed {
				v.Riders[0].Completed = true
			}
			orders := []int{v.Riders[0].ID, v.Riders[1].ID}
			if err := s.startSampledEmergency(0, test.party); err != nil {
				t.Fatal(err)
			}
			if got := s.emergencies[0].order; got != orders[test.want] {
				t.Fatalf("order %d, want %d", got, orders[test.want])
			}
		})
	}
}

// TestSampledEmergencyMonitor checks that a sampled start does not run the
// monitor inside the tick, and that the command still runs it once.
func TestSampledEmergencyMonitor(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	startEmergencySampler(t, s)
	emergencyDue(t, s, func(pod, party float64) bool { return pod < 0.5 && party < 0.5 })
	runs := 0
	s.monitor = func(*Simulation) { runs++ }
	s.Step()
	if s.emergencyCounters.started != 1 || runs != 1 {
		t.Fatalf("%d starts and %d monitor runs in a tick, want one of each", s.emergencyCounters.started, runs)
	}
	runs = 0
	if _, err := s.Emergency(v.Pod.ID, 0); !errors.Is(err, errPodEmergency) || runs != 0 {
		t.Fatalf("a refused command: %v, %d monitor runs", err, runs)
	}
	if _, err := s.Emergency("02", 0); !errors.Is(err, errNoPassenger) || runs != 0 {
		t.Fatalf("a refused command: %v, %d monitor runs", err, runs)
	}
	s, v = emergencyFleet(t)
	s.monitor = func(*Simulation) { runs++ }
	startEmergency(t, s, v, 0)
	if runs != 1 {
		t.Fatalf("%d monitor runs in a command, want 1", runs)
	}
}

// TestIncidentSamplersBoundary checks the place of both samplers in one tick
// (section 5.1.8 of the incident stage 7 contract). In the tick, pod 01 ends
// its unload, the fault on pod 02 ends, and both samplers come due, each with
// the target pod 01. The fault sampler sees the state after the unloading loop
// and before the clears. The emergency sampler also sees the state after the
// unloading loop, so pod 01 has no rider, and the event is a skip.
func TestIncidentSamplersBoundary(t *testing.T) {
	t.Parallel()
	s, unloading, cleared, due := faultBoundaryFixture(t)
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	startEmergencySampler(t, s)
	s.emergencySampler.nextTick = due
	s.emergencySampler.stream = streamWith(t, 2, func(draws []float64) bool { return draws[0] < 0.5 })
	checkFaultsEachTick(t, s)
	for s.tick < due {
		s.Step()
	}
	if c := s.faultCounters; c.sampled != 1 || c.skipped != 0 || !unloading.faulted || cleared.faulted {
		t.Fatalf("counters %+v, pod 01 faulted %v, pod 02 faulted %v", c, unloading.faulted, cleared.faulted)
	}
	if unloading.RidersAboard() != 0 || unloading.Pod.Activity != Idle {
		t.Fatalf("the unload did not end before the events: %d riders, activity %s", unloading.RidersAboard(), unloading.Pod.Activity)
	}
	if c := s.emergencyCounters; c.sampled != 1 || c.skipped != 1 || c.started != 0 || len(s.emergencies) != 0 {
		t.Fatalf("emergency counters %+v, %d records", c, len(s.emergencies))
	}
}

// TestEmergencySamplerBeforeRecords checks that the emergency sampler runs
// before the records. Each of the four records ends in this tick, because its
// pod carries no passenger. The sampler still counts them, so the limit
// refuses its event.
func TestEmergencySamplerBeforeRecords(t *testing.T) {
	t.Parallel()
	s, v := emergencyFleet(t)
	startEmergencySampler(t, s)
	emergencyDue(t, s, func(pod, _ float64) bool { return pod < 0.5 })
	for serial := range uint64(MaxEmergencies) {
		s.emergencies = append(s.emergencies, emergencyRecord{serial: serial + 1, start: s.tick, pod: 1, order: 9})
	}
	s.incidentSerial = MaxEmergencies
	s.Step()
	if c := s.emergencyCounters; c.sampled != 1 || c.skipped != 1 || c.ended != MaxEmergencies || len(s.emergencies) != 0 {
		t.Fatalf("counters %+v, %d records", c, len(s.emergencies))
	}
	if v.withdrawn != 0 {
		t.Fatalf("pod 01 has the holds %#x", v.withdrawn)
	}
}

// TestEmergencySamplerTerminal runs a tick to the limit with the terminal
// value: no event starts, and E9 holds.
func TestEmergencySamplerTerminal(t *testing.T) {
	t.Parallel()
	s, _ := emergencyFleet(t)
	checkEmergenciesEachTick(t, s)
	startEmergencySampler(t, s)
	s.tick, s.emergencySampler.nextTick = MaxCounter-1, MaxCounter
	stream := s.emergencySampler.stream
	s.Step()
	if s.tick != MaxCounter || s.emergencyCounters.sampled != 0 || s.emergencySampler.stream != stream {
		t.Fatalf("tick %d, counters %+v", s.tick, s.emergencyCounters)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	checkTerminalRestore(t, s, IncidentRates{EmergenciesPerHour: 60})
}

// TestIncidentStreamsSeparate runs one rate at a time, each way. The state of
// a sampler at each tick does not depend on the other sampler, on its rate,
// or on the events of the run, because each sampler has its own stream and
// its draws do not depend on outcomes.
func TestIncidentStreamsSeparate(t *testing.T) {
	t.Parallel()
	const ticks = 40_000
	faults := FaultRate{PerHour: 60, DebrisShare: 0.5, DebrisMeters: 3, Duration: FaultDurationRule{Kind: FaultDurationFixed, Seconds: 30}}
	seeds := IncidentRates{FaultSeed: [2]uint64{5, 6}, EmergencySeed: [2]uint64{7, 8}}
	run := func(t *testing.T, faultRate, emergencyRate float64) ([]incidentSampler, []incidentSampler) {
		t.Helper()
		s := sharedTraceFleet(t)
		rates := seeds
		rates.Faults, rates.EmergenciesPerHour = faults, emergencyRate
		rates.Faults.PerHour = faultRate
		if err := s.StartIncidentSampling(rates); err != nil {
			t.Fatal(err)
		}
		fault, emergency := make([]incidentSampler, ticks), make([]incidentSampler, ticks)
		for index := range ticks {
			submitSharedTrips(t, s)
			s.Step()
			fault[index], emergency[index] = s.faultSampler, s.emergencySampler
		}
		return fault, emergency
	}
	bothFaults, bothEmergencies := run(t, 60, 60)
	faultOnly, _ := run(t, 60, 0)
	_, emergencyOnly := run(t, 0, 60)
	otherFaults, _ := run(t, 60, 10)
	_, otherEmergencies := run(t, 10, 60)
	if !reflect.DeepEqual(bothFaults, faultOnly) || !reflect.DeepEqual(bothFaults, otherFaults) {
		t.Fatal("the fault sampler depends on the emergency sampler")
	}
	if !reflect.DeepEqual(bothEmergencies, emergencyOnly) || !reflect.DeepEqual(bothEmergencies, otherEmergencies) {
		t.Fatal("the emergency sampler depends on the fault sampler")
	}
	if bothFaults[0].stream == bothFaults[ticks-1].stream || bothEmergencies[0].stream == bothEmergencies[ticks-1].stream {
		t.Fatal("a sampler had no event")
	}
}

// TestSampledEmergencySearch checks rule G15 of the incident stage 7
// contract: a sampled emergency advances twice in its start tick, and it
// makes one station search. The second call does nothing for a bound pod, and
// the no-candidate memo answers it for a failed search.
func TestSampledEmergencySearch(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, s *Simulation, v *vehicle, bound bool) {
		t.Helper()
		startEmergencySampler(t, s)
		emergencyDue(t, s, func(pod, _ float64) bool { return pod < 0.5 })
		choices := s.searchCounters.choices
		s.Step()
		if s.emergencyCounters.started != 1 {
			t.Fatalf("counters %+v", s.emergencyCounters)
		}
		if got := s.searchCounters.choices - choices; got != 1 {
			t.Fatalf("%d station searches in the start tick, want 1", got)
		}
		if got := v.op.purpose == opEmergencyUnload; got != bound {
			t.Fatalf("bound %v, want %v", got, bound)
		}
	}
	t.Run("candidate", func(t *testing.T) {
		t.Parallel()
		network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1100, y: -300, berths: 1}}, 1)
		s, v := choiceFleet(t, network, 1, "sB")
		travelApproach(t, s, v, 100)
		check(t, s, v, true)
	})
	t.Run("no candidate", func(t *testing.T) {
		t.Parallel()
		s, v := missFleet(t)
		check(t, s, v, false)
	})
}

// TestEmergencyCountersGate checks the counter gate of section 5.1.5 of the
// incident stage 7 contract and the meaning of each gated counter. With no
// sampler, a manual emergency changes neither counter. With a live sampler, a
// manual record counts as a sampled one does.
func TestEmergencyCountersGate(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, live bool, counters emergencyCounters) (*Simulation, int64) {
		t.Helper()
		s, v := emergencyFleet(t)
		if live {
			startEmergencySampler(t, s)
			s.emergencySampler.nextTick = MaxCounter
		}
		s.emergencyCounters = counters
		startEmergency(t, s, v, 0)
		start := s.tick
		stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
		return s, s.tick - start
	}
	t.Run("no sampler", func(t *testing.T) {
		t.Parallel()
		s, _ := run(t, false, emergencyCounters{})
		if c := s.emergencyCounters; c.endedTicks != 0 || c.maxTicks != 0 || c.sampled != 0 || c.skipped != 0 || c.ended != 1 {
			t.Fatalf("counters %+v", c)
		}
	})
	t.Run("live sampler", func(t *testing.T) {
		t.Parallel()
		s, length := run(t, true, emergencyCounters{})
		if c := s.emergencyCounters; c.endedTicks != length || c.maxTicks != length || c.sampled != 0 || length == 0 {
			t.Fatalf("counters %+v, length %d", c, length)
		}
	})
	t.Run("sum and longest", func(t *testing.T) {
		t.Parallel()
		s, length := run(t, true, emergencyCounters{endedTicks: 5, maxTicks: 1 << 40})
		if c := s.emergencyCounters; c.endedTicks != 5+length || c.maxTicks != 1<<40 {
			t.Fatalf("counters %+v, length %d", c, length)
		}
	})
	t.Run("saturation", func(t *testing.T) {
		t.Parallel()
		s, _ := run(t, true, emergencyCounters{endedTicks: MaxCounter - 1})
		if c := s.emergencyCounters; c.endedTicks != MaxCounter {
			t.Fatalf("counters %+v", c)
		}
	})
}

// TestEmergencySamplerCounterInvariant checks the counter clause of E9.
func TestEmergencySamplerCounterInvariant(t *testing.T) {
	t.Parallel()
	s := samplerFleet(t)
	s.emergencyCounters.skipped = 1
	if err := s.CheckContract(); err == nil || !strings.HasPrefix(err.Error(), "E9: 1 events skipped") {
		t.Fatalf("error %v", err)
	}
	s.emergencyCounters.sampled = 1
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
}

// TestEmergencySamplerReplay checks that a clone replays the emergency events
// of the original (section 5.1.4 of the incident stage 7 contract).
func TestEmergencySamplerReplay(t *testing.T) {
	t.Parallel()
	s := sharedTraceFleet(t)
	startEmergencySampler(t, s)
	for s.emergencyCounters.sampled < 2 {
		submitSharedTrips(t, s)
		s.Step()
	}
	clone := s.Clone()
	sampled := s.emergencyCounters.sampled
	for range 10_000 {
		for _, run := range []*Simulation{s, clone} {
			submitSharedTrips(t, run)
			run.Step()
		}
		if s.emergencySampler != clone.emergencySampler || s.emergencyCounters != clone.emergencyCounters || !reflect.DeepEqual(emergencyIDs(s), emergencyIDs(clone)) {
			t.Fatalf("tick %d: sampler %+v, clone %+v", s.tick, s.emergencySampler, clone.emergencySampler)
		}
	}
	if s.emergencyCounters.sampled == sampled {
		t.Fatal("no event came due after the clone")
	}
}
