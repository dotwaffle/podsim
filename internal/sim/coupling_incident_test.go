package sim

import (
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
)

// couplingIncidentTrace is the record of one coupled incident run: each
// command result, a digest of the exported state every 1,000 ticks, and
// the observations of the run.
type couplingIncidentTrace struct {
	results []string
	digests [][32]byte
	// split is the tick at whose end the last coupling group split, or 0.
	split int64
	// recruited counts the ticks at which a pod with an emergency record
	// and no hold was an approach member after it had left each group or
	// approach with its record. Discovery runs before the emergency stage,
	// so such a pod has no hold in the tick after its group ends. The
	// discovery skip of a pod with a record (section 5.8 of the incident
	// emergency contract) keeps the count at 0.
	recruited int
}

// couplingIncidentCommand is a command of the run at a fixed tick.
type couplingIncidentCommand struct {
	tick int64
	name string
	// run starts the command. want is nil when the command must start, or
	// the error of its refusal.
	run  func(s *Simulation) (string, error)
	want error
}

func podFaultCommand(id string) func(*Simulation) (string, error) {
	return func(s *Simulation) (string, error) {
		return s.Fault(FaultRequest{PodID: id, DurationSeconds: new(int64(60))})
	}
}

func debrisCommand(lane string, from float64) func(*Simulation) (string, error) {
	return func(s *Simulation) (string, error) {
		return s.Fault(FaultRequest{LaneID: lane, FromMeters: new(from), ToMeters: new(from + 2), DurationSeconds: new(int64(60))})
	}
}

func emergencyCommand(id string) func(*Simulation) (string, error) {
	return func(s *Simulation) (string, error) { return s.Emergency(id, 0) }
}

// runCouplingIncidentJourney runs the coupling approach journey with
// faults and emergencies on until tick end, and starts each command at its
// tick before the step. The journey has a non-member, pod blocker, and the
// pods front and rear, which are approach members from tick 4407, a
// coupling group from tick 6728, and split at the end of tick 15374. The
// state contract and the emergency transition rules are checked after
// each command and each tick.
//
// observe, when not nil, checks the state after each tick.
func runCouplingIncidentJourney(t *testing.T, commands []couplingIncidentCommand, observe func(*Simulation), end int64) couplingIncidentTrace {
	t.Helper()
	s := newCouplingApproachJourney(t, true)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 3600}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	checkEmergenciesEachTick(t, s)
	var trace couplingIncidentTrace
	// left holds each pod that had an emergency record while it was not
	// a coupling or approach member.
	left := map[string]bool{}
	grouped := false
	for s.tick < end {
		for _, command := range commands {
			if command.tick != s.tick {
				continue
			}
			id, err := command.run(s)
			if !errors.Is(err, command.want) || (err == nil) == (id == "") {
				t.Fatalf("tick %d: %s: %q, %v, want %v", s.tick, command.name, id, err, command.want)
			}
			trace.results = append(trace.results, fmt.Sprintf("%d %s %s %v", s.tick, command.name, id, err))
		}
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		if observe != nil {
			observe(s)
		}
		wasGrouped := grouped
		grouped = false
		for index := range s.vehicles {
			v := &s.vehicles[index]
			grouped = grouped || v.couplingID != ""
			member := v.couplingID != "" || s.couplingApproachMember(v.Pod.ID)
			switch {
			case s.emergencyOf(v) < 0:
				delete(left, v.Pod.ID)
			case !member:
				left[v.Pod.ID] = true
			case left[v.Pod.ID] && v.withdrawn == 0:
				trace.recruited++
			}
		}
		if wasGrouped && !grouped {
			trace.split = s.tick
		}
		if s.tick%1000 == 0 {
			raw, err := json.Marshal(s.ExportState(), json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			trace.digests = append(trace.digests, sha256.Sum256(raw))
		}
	}
	return trace
}

// TestCouplingIncidentJourneyDeterminism runs the coupling approach
// journey twice for each command schedule, with faults and emergencies
// on, and compares the command results and the state digests of the two
// runs.
//
// The member schedule sends a pod fault to each approach member and each
// group member, and debris on the remaining route of the members. Each is
// refused with errFaultTarget. A pod fault on the non-member, debris on a
// lane of no route, and a pod fault on the front after the split start.
// An emergency on the rear group member starts. The member gets no hold
// until the split, and the emergency stage after the split withdraws it.
//
// The approach schedule starts an emergency on the rear approach member.
// The approach aborts, the member gets its hold after the approach ends,
// and no group forms (Q7).
func TestCouplingIncidentJourneyDeterminism(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the coupled incident journey runs without -short")
	}
	const approach, group, split = 5000, 10000, 15374
	for _, test := range []struct {
		name     string
		commands []couplingIncidentCommand
		// observe returns the check of each tick of one run.
		observe func(t *testing.T) func(*Simulation)
		check   func(t *testing.T, trace couplingIncidentTrace)
	}{
		{"member", []couplingIncidentCommand{
			{approach, "fault on the approach front", podFaultCommand("front"), errFaultTarget},
			{approach, "fault on the approach rear", podFaultCommand("rear"), errFaultTarget},
			{approach, "debris on the approach route", debrisCommand("rear-road", 100), errFaultTarget},
			{group, "fault on the group front", podFaultCommand("front"), errFaultTarget},
			{group, "fault on the group rear", podFaultCommand("rear"), errFaultTarget},
			{group, "debris on the group route", debrisCommand("rear-road", 100), errFaultTarget},
			{group, "fault on the non-member", podFaultCommand("blocker"), nil},
			{group, "debris on a free lane", debrisCommand("block-through", 100), nil},
			{group, "emergency on the group rear", emergencyCommand("rear"), nil},
			{split + 1000, "fault on the front after the split", podFaultCommand("front"), nil},
		}, func(t *testing.T) func(*Simulation) {
			t.Helper()
			return func(s *Simulation) {
				rear := s.findVehicle("rear")
				switch {
				case s.tick > group && s.tick < split && (rear.couplingID == "" || rear.withdrawn != 0 || s.emergencyOf(rear) < 0):
					t.Fatalf("tick %d: the deferred member has the coupling %q and the holds %#x", s.tick, rear.couplingID, rear.withdrawn)
				case s.tick == split && (rear.couplingID != "" || rear.withdrawn != 0):
					t.Fatalf("tick %d: the split member has the coupling %q and the holds %#x", s.tick, rear.couplingID, rear.withdrawn)
				case s.tick == split+1 && (rear.couplingID != "" || rear.withdrawn != emergencyHold):
					t.Fatalf("tick %d: the stage after the split did not withdraw the pod: holds %#x", s.tick, rear.withdrawn)
				}
			}
		}, func(t *testing.T, trace couplingIncidentTrace) {
			t.Helper()
			if trace.split != split {
				t.Fatalf("the group ends at tick %d, want %d", trace.split, split)
			}
		}},
		{"approach", []couplingIncidentCommand{
			{approach, "emergency on the approach rear", emergencyCommand("rear"), nil},
		}, func(t *testing.T) func(*Simulation) {
			t.Helper()
			ended := int64(0)
			return func(s *Simulation) {
				rear := s.findVehicle("rear")
				member := s.couplingApproachMember("rear")
				switch {
				case s.tick == approach+1 && (!member || s.couplingApproaches[0].state.Phase != couplingApproachAborting):
					t.Fatalf("tick %d: the approach does not abort", s.tick)
				case s.tick > approach && member && rear.withdrawn != 0:
					t.Fatalf("tick %d: the approach member has the holds %#x", s.tick, rear.withdrawn)
				case s.tick > approach && !member && ended == 0:
					ended = s.tick
				case ended != 0 && s.tick == ended+1 && rear.withdrawn != emergencyHold && s.emergencyOf(rear) >= 0:
					t.Fatalf("tick %d: the stage after the approach did not withdraw the pod: holds %#x", s.tick, rear.withdrawn)
				}
			}
		}, func(t *testing.T, trace couplingIncidentTrace) {
			t.Helper()
			if trace.split != 0 {
				t.Fatalf("a group formed at tick %d", trace.split)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			first := runCouplingIncidentJourney(t, test.commands, test.observe(t), 20000)
			second := runCouplingIncidentJourney(t, test.commands, test.observe(t), 20000)
			if !slices.Equal(first.results, second.results) || !slices.Equal(first.digests, second.digests) || first.split != second.split || first.recruited != second.recruited {
				t.Fatalf("the runs differ:\n%v\n%v", first.results, second.results)
			}
			test.check(t, first)
			t.Logf("final digest %x, split %d, recruited %d", first.digests[len(first.digests)-1], first.split, first.recruited)
		})
	}
}

// TestCouplingTickWorkMatchesFreshBuffers runs the coupling approach
// journey twice: once with the tick buffers that the simulation keeps,
// and once with new buffers for each tick. The command results and the
// state digests must be the same.
func TestCouplingTickWorkMatchesFreshBuffers(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the coupled incident journey runs without -short")
	}
	const group = 10000
	commands := []couplingIncidentCommand{
		{group, "fault on the non-member", podFaultCommand("blocker"), nil},
		{group, "emergency on the group rear", emergencyCommand("rear"), nil},
	}
	// The group splits at tick 15374, so the runs end soon after.
	reused := runCouplingIncidentJourney(t, commands, nil, 16000)
	fresh := runCouplingIncidentJourney(t, commands, func(s *Simulation) { s.couplingWork = nil }, 16000)
	if !slices.Equal(reused.results, fresh.results) || !slices.Equal(reused.digests, fresh.digests) || reused.split != fresh.split {
		t.Fatalf("reused and fresh buffers differ:\n%v\n%v", reused.results, fresh.results)
	}
	if reused.split == 0 {
		t.Fatal("no group formed and split, so the buffers were not exercised")
	}
}

// TestCouplingTickWorkClearsReusedMaps checks that a reused retention map
// and owner view hold only the values of the current build. A stale entry
// would let the retained grant check pass for a resource that the pod no
// longer retains.
func TestCouplingTickWorkClearsReusedMaps(t *testing.T) {
	t.Parallel()
	a, b := resource{kind: junctionResource, id: "a"}, resource{kind: junctionResource, id: "b"}
	work := &couplingTickWork{retained: make([]map[resource]float64, 1), views: make([]map[resource]resourceOwner, 1)}
	v := &vehicle{routeReleases: map[resource]float64{a: 1, b: 2}}
	nativeForeignRetained(v, work, 0)
	v.routeReleases = map[resource]float64{a: 3}
	if got := nativeForeignRetained(v, work, 0); !maps.Equal(got, v.routeReleases) {
		t.Fatalf("reused retention map = %v, want %v", got, v.routeReleases)
	}
	frame := &nativeForeignTick{work: work}
	frame.ownerView(0, 1)[a] = resourceOwner{}
	if got := frame.ownerView(0, 1); len(got) != 0 {
		t.Fatalf("reused owner view = %v, want empty", got)
	}
}
