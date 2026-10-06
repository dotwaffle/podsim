package sim

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// missFleet returns a simulation on choiceNetwork with the stations sA and
// sB, with pod 01 on approach on its way to sB, and the lanes into both
// stations blocked, so that no station has a candidate.
func missFleet(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1100, y: -300, berths: 1}}, 1)
	s, v := choiceFleet(t, network, 1, "sB")
	travelApproach(t, s, v, 100)
	blockLanes(t, s, "to-sA", "to-sB")
	return s, v
}

// stepToCadence steps s to the next cadence tick of the record.
func stepToCadence(s *Simulation) {
	s.Step()
	for (s.tick-s.emergencies[0].start)%emergencyChoiceTicks != 0 {
		s.Step()
	}
}

// TestEmergencyNoCandidate starts an emergency when no station has a
// candidate. The pod keeps its route and stays deferred, and the memo
// holds the result. The next cadence tick with the same divert node makes
// no search. A refused installation writes no memo entry, so the next
// choice searches again. A blocked-set change clears the memo, and the
// next cadence tick binds the pod.
func TestEmergencyNoCandidate(t *testing.T) {
	t.Parallel()
	s, v := missFleet(t)
	checkEmergenciesEachTick(t, s)
	route := slices.Clone(v.Route)
	startEmergency(t, s, v, 0)
	_, from, _ := s.divertStart(v)
	want := []emergencyMiss{{serial: s.emergencies[0].serial, from: s.graph.nodes[from], class: v.Pod.Class}}
	if v.op.purpose != opService || !sameLanes(v.Route, route) || !slices.Equal(s.emergencyMisses, want) {
		t.Fatalf("purpose %+v, route %v, memo %+v, want %+v", v.op, laneIDs(v.Route), s.emergencyMisses, want)
	}
	before := s.searchCounters
	stepToCadence(s)
	if _, next, _ := s.divertStart(v); next != from {
		t.Fatalf("the divert node moved from %s to %s", from, next)
	}
	after := s.searchCounters
	if after.memoHits != before.memoHits+1 || after.choices != before.choices || after.trees != before.trees || after.routes != before.routes {
		t.Fatalf("the memo did not answer the cadence tick: %+v, then %+v", before, after)
	}
	// A refused installation leaves no entry: the mask names no rider.
	blockLanes(t, s, "to-sA")
	s.chooseEmergencyStation(s.emergencies[0], 1<<7)
	if v.op.purpose != opService || len(s.emergencyMisses) != 0 {
		t.Fatalf("after a refused installation: purpose %+v, memo %+v", v.op, s.emergencyMisses)
	}
	choices := s.searchCounters.choices
	s.chooseEmergencyStation(s.emergencies[0], 1<<7)
	if s.searchCounters.choices != choices+1 {
		t.Fatal("the choice after a refused installation did not search")
	}
	stepToCadence(s)
	if v.op.purpose != opEmergencyUnload || v.destinationStation != "sB" {
		t.Fatalf("after the clear of to-sB, the pod has %+v at %s, want sB", v.op, v.destinationStation)
	}
}

// TestEmergencyMissKey checks the key and the clears of the memo. A choice
// from a new divert node searches and binds the pod, and it removes the
// entry of the old node. The end of a record removes its entry. A
// rebuild of the network indexes, Reset, and a restore each clear the
// memo.
func TestEmergencyMissKey(t *testing.T) {
	t.Parallel()
	t.Run("divert node", func(t *testing.T) {
		t.Parallel()
		s, v := missFleet(t)
		checkEmergenciesEachTick(t, s)
		startEmergency(t, s, v, 0)
		// to-sB is blocked only for the searches. Past fork, the divert
		// node is the entry of sB, and its berths have routes.
		memo := s.emergencyMisses[0]
		for v.op.purpose == opService {
			memo = s.emergencyMisses[0]
			stepToCadence(s)
			if v.Pod.Activity != Traveling {
				t.Fatalf("the pod did not bind on its way: %s", v.Pod.Activity)
			}
		}
		if v.destinationStation != "sB" || memo.from == s.graph.nodes["sB-entry"] || len(s.emergencyMisses) != 0 {
			t.Fatalf("the pod binds to %s, the memo had the node %s, memo %+v", v.destinationStation, s.network.Nodes[memo.from].ID, s.emergencyMisses)
		}
	})
	t.Run("record end", func(t *testing.T) {
		t.Parallel()
		s, v := missFleet(t)
		checkEmergenciesEachTick(t, s)
		startEmergency(t, s, v, 0)
		// The record of the deferred pod ends with an entry in the memo.
		if len(s.emergencyMisses) != 1 || v.op.purpose != opService {
			t.Fatalf("purpose %+v, memo %+v", v.op, s.emergencyMisses)
		}
		s.endEmergency(0)
		if len(s.emergencies) != 0 || len(s.emergencyMisses) != 0 {
			t.Fatalf("records %v, memo %+v", emergencyIDs(s), s.emergencyMisses)
		}
	})
	t.Run("clears", func(t *testing.T) {
		t.Parallel()
		for _, clearMemo := range []struct {
			name  string
			clear func(*Simulation) *Simulation
		}{
			{"network indexes", func(s *Simulation) *Simulation {
				p := *s.networkIndexes
				p.graph = routeGraph{}
				s.networkIndexes = &p
				s.ensureNetworkIndexes()
				return s
			}},
			{"reset", func(s *Simulation) *Simulation {
				// A blocked-set change clears the memo, so Reset gets a
				// memo without a blocked set.
				misses := s.emergencyMisses
				s.setBlocked(nil)
				s.emergencyMisses = misses
				s.Reset()
				return s
			}},
			{"restore", func(s *Simulation) *Simulation {
				restored, _, err := RestoreState(RestoreStateInput{
					IncidentContract: IncidentV1Contract, EmergencyContract: EmergencyV1Contract, Network: s.network, Fleet: s.initial, State: s.ExportState(),
				})
				if err != nil {
					t.Fatal(err)
				}
				return restored
			}},
		} {
			s, v := missFleet(t)
			startEmergency(t, s, v, 0)
			if len(s.emergencyMisses) != 1 {
				t.Fatalf("%s: memo %+v", clearMemo.name, s.emergencyMisses)
			}
			if next := clearMemo.clear(s); next.emergencyMisses != nil {
				t.Fatalf("%s keeps the memo %+v", clearMemo.name, next.emergencyMisses)
			}
		}
	})
}

// TestEmergencyCadence starts an emergency on a faulted pod, which stays
// deferred, and clears the fault 10 ticks later. The pod can divert from
// then on, but it chooses only on the next tick with (tick - start) % 60
// == 0.
func TestEmergencyCadence(t *testing.T) {
	t.Parallel()
	network := choiceNetwork([]choiceStation{{id: "sA", x: 1000, y: 300, berths: 1}, {id: "sB", x: 1100, y: -300, berths: 1}}, 1)
	s, v := choiceFleet(t, network, 1, "sB")
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	travelApproach(t, s, v, 100)
	checkEmergenciesEachTick(t, s)
	id := startFault(t, s, v, 0)
	startEmergency(t, s, v, 0)
	start := s.tick
	advance(s, 10)
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	choices := s.searchCounters.choices
	for s.tick < start+emergencyChoiceTicks-1 {
		s.Step()
		if v.op.purpose != opService || s.searchCounters.choices != choices {
			t.Fatalf("tick %d: the pod chose before its cadence tick: %+v", s.tick-start, v.op)
		}
	}
	if _, _, ok := s.divertStart(v); !ok {
		t.Fatal("the pod cannot divert")
	}
	s.Step()
	if v.op.purpose != opEmergencyUnload || v.destinationStation != "sA" {
		t.Fatalf("on the cadence tick, the pod has %+v at %s", v.op, v.destinationStation)
	}
}

// TestEmergencyMissParity runs two simulations from one state with lane
// blocks that change at random ticks. One keeps the memo, and the other
// clears it before each tick. Both make the same choices, and their states
// stay equal apart from the memo and the search counters. A clone with a
// filled memo starts without it, and it also stays equal to its source.
func TestEmergencyMissParity(t *testing.T) {
	t.Parallel()
	for seed := range uint64(5) {
		t.Run(strconv.FormatUint(seed, 10), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewPCG(seed, 3))
			s, v := missFleet(t)
			startEmergency(t, s, v, 0)
			if len(s.emergencyMisses) != 1 {
				t.Fatalf("memo %+v", s.emergencyMisses)
			}
			plain, clone := s.Clone(), s.Clone()
			if clone.emergencyMisses != nil || !sameState(clone, s) {
				t.Fatal("the clone keeps the memo, or differs from its source")
			}
			blocks := [][]string{{"to-sA", "to-sB"}, {"to-sA"}, {"to-sB"}, nil}
			hits := s.searchCounters.memoHits
			for tick := range 300 * TicksPerSecond {
				if tick%97 == 0 {
					lanes := blocks[rng.IntN(len(blocks))]
					for _, sim := range []*Simulation{s, plain, clone} {
						blockLanes(t, sim, lanes...)
					}
				}
				plain.emergencyMisses = nil
				for _, sim := range []*Simulation{s, plain, clone} {
					sim.Step()
				}
				if !sameState(s, plain) || !sameState(s, clone) {
					t.Fatalf("tick %d: the runs differ", s.tick)
				}
				if !reflect.DeepEqual(s.Snapshot(), clone.Snapshot()) {
					t.Fatalf("tick %d: the snapshots differ", s.tick)
				}
				if len(s.emergencies) == 0 {
					break
				}
			}
			if s.emergencyCounters.ended != 1 || s.searchCounters.memoHits == hits && seed == 0 {
				t.Fatalf("ended %d, memo hits %d", s.emergencyCounters.ended, s.searchCounters.memoHits-hits)
			}
			_ = v
		})
	}
}
