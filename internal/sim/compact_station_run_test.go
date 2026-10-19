package sim

import (
	"encoding/json/v2"
	"os"
	"slices"
	"testing"
)

// loadCompactStation returns the network and the fleet of the compact
// station preset, which package scenarios writes to the file.
func loadCompactStation(t *testing.T) (Network, []Placement) {
	t.Helper()
	data, err := os.ReadFile("testdata/compact_station.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Network Network     `json:"network"`
		Fleet   []Placement `json:"fleet"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Network, fixture.Fleet
}

// compactRun steps a compact station simulation and checks it at each
// tick.
type compactRun struct {
	t     *testing.T
	s     *Simulation
	group resource
	lanes map[string]bool
	// arrivals counts the arrivals at each berth, and dwells the ticks of
	// pods at rest in a reverse dwell.
	arrivals map[string]int
	dwells   int
}

func newCompactRun(t *testing.T, s *Simulation) *compactRun {
	t.Helper()
	group := s.network.ManeuverGroups[0]
	r := &compactRun{t: t, s: s, group: resource{kind: junctionResource, id: group.ID}, lanes: make(map[string]bool), arrivals: make(map[string]int)}
	for _, id := range group.LaneIDs {
		r.lanes[id] = true
	}
	return r
}

// newCompactSimulation returns the preset with the fault and emergency
// contracts.
func newCompactSimulation(t *testing.T) *Simulation {
	t.Helper()
	network, fleet := loadCompactStation(t)
	s, err := NewFleetWithContracts(network, fleet, FleetContracts{
		IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, EmergencyContract: EmergencyV1Contract,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// holders returns the pods that have a cell of the group resource ahead of
// them in their admitted span, or a retained release of it past their
// distance.
func (r *compactRun) holders() []string {
	var holders []string
	for i := range r.s.vehicles {
		v := &r.s.vehicles[i]
		held := false
		if releaseAt, retained := v.routeReleases[r.group]; retained && releaseAt > v.distance {
			held = true
		}
		// A block that the pod passed is not admitted, also when the pod
		// stopped at its end, as at an arrival.
		for b := max(v.blockIndex, 0); b <= v.reservedThrough && b < v.blocks.len(); b++ {
			lane := v.blocks.routeLane(b)
			cell := b - v.blocks.lanes[lane].first
			if _, end := v.blocks.cellBounds(lane, cell); end > v.distance && slices.Contains(v.blocks.lanes[lane].cells.cell(cell), r.group) {
				held = true
			}
		}
		if held {
			holders = append(holders, v.Pod.ID)
		}
	}
	return holders
}

// check checks the separation, the contract, that at most one pod is on
// the group lanes, and that the owner of the group resource is the one pod
// that holds it, or that no pod holds it when it has no owner.
func (r *compactRun) check() {
	r.t.Helper()
	s := r.s
	if _, err := s.SafetyObservation().Check(); err != nil {
		r.t.Fatalf("tick %d: %v", s.tick, err)
	}
	if err := s.CheckContract(); err != nil {
		r.t.Fatalf("tick %d: %v", s.tick, err)
	}
	var on []string
	for i := range s.vehicles {
		if pod := s.vehicles[i].Pod; r.lanes[pod.LaneID] {
			on = append(on, pod.ID)
		}
	}
	if len(on) > 1 {
		r.t.Fatalf("tick %d: pods %v on the group lanes", s.tick, on)
	}
	holders, owner := r.holders(), s.owners[r.group]
	switch {
	case owner.isZero() && len(holders) != 0:
		r.t.Fatalf("tick %d: the group resource has no owner, and pods %v hold it", s.tick, holders)
	case !owner.isZero() && (len(holders) != 1 || !owner.isPod(holders[0])):
		r.t.Fatalf("tick %d: the group resource has owner %s, and pods %v hold it", s.tick, owner, holders)
	}
	if len(on) == 1 && !owner.isPod(on[0]) {
		r.t.Fatalf("tick %d: pod %s is on a group lane without the group resource (owner %s)", s.tick, on[0], owner)
	}
}

// run steps until done reports true, checking each tick, and fails at the
// deadline in ticks.
func (r *compactRun) run(deadline int, done func() bool) {
	r.t.Helper()
	for range deadline {
		if done() {
			return
		}
		before := make([]string, len(r.s.vehicles))
		for i := range r.s.vehicles {
			before[i] = r.s.vehicles[i].Pod.BerthID
		}
		r.s.Step()
		r.check()
		for i := range r.s.vehicles {
			v := &r.s.vehicles[i]
			if berth := v.Pod.BerthID; berth != "" && berth != before[i] {
				r.arrivals[berth]++
			}
			if v.reversal.ticks > 0 && v.reversal.lane > 0 {
				r.dwells++
			}
		}
	}
	if !done() {
		r.t.Fatalf("tick %d: the run did not finish", r.s.tick)
	}
}

// TestCompactStationJourneys runs passenger trips both ways between the
// compact station and the town station until each bay has an arrival.
func TestCompactStationJourneys(t *testing.T) {
	t.Parallel()
	s := newCompactSimulation(t)
	r := newCompactRun(t, s)
	trips := 0
	for range 12 {
		for _, trip := range [][2]string{{"town", "compact"}, {"compact", "town"}} {
			if _, err := s.SubmitTrip(trip[0], trip[1]); err != nil {
				t.Fatal(err)
			}
			trips++
		}
	}
	r.run(60*60*TicksPerSecond, func() bool { return s.completed == trips })
	for _, bay := range []string{"bay-1", "bay-2", "bay-3", "bay-4"} {
		if r.arrivals[bay] == 0 {
			t.Errorf("no arrival at %s: %v", bay, r.arrivals)
		}
	}
	if r.dwells == 0 {
		t.Error("no pod dwelled")
	}
}

// drained reports whether no trip waits, no pod carries a party, and every
// pod is at rest at a berth.
func (r *compactRun) drained() bool {
	if len(r.s.waiting) != 0 {
		return false
	}
	for i := range r.s.vehicles {
		v := &r.s.vehicles[i]
		if v.carriesPassengers() || v.Pod.Activity == Traveling || v.Pod.BerthID == "" {
			return false
		}
	}
	return true
}

// finish runs until the station drains, and checks that the group resource
// is free.
func (r *compactRun) finish(deadline int) {
	r.t.Helper()
	r.run(deadline, r.drained)
	if owner := r.s.owners[r.group]; !owner.isZero() {
		r.t.Fatalf("tick %d: the drained station leaves the group resource with %s", r.s.tick, owner)
	}
}

// restore restores the saved state of the run in the physical tier, with
// no pod moved to a berth.
func (r *compactRun) restore() *compactRun {
	r.t.Helper()
	network, fleet := loadCompactStation(r.t)
	restored, result, err := RestoreState(RestoreStateInput{
		IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, EmergencyContract: EmergencyV1Contract,
		Network: network, Fleet: fleet, State: r.s.ExportState(),
	})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		r.t.Fatalf("tick %d: restore tier %s, demoted %v, error %v", r.s.tick, result.Tier, result.Demoted, err)
	}
	restoredRun := newCompactRun(r.t, restored)
	restoredRun.check()
	return restoredRun
}

// overloaded reports whether all four bays hold a pod at rest and a pod
// waits for a bay.
func (r *compactRun) overloaded() bool {
	parked, waiting := 0, false
	for i := range r.s.vehicles {
		pod := r.s.vehicles[i].Pod
		if pod.Activity != Traveling && pod.StationID == "compact" {
			parked++
		}
		waiting = waiting || pod.Activity == Traveling && pod.WaitReason == BerthOccupied
	}
	return parked == 4 && waiting
}

// TestCompactStationOverloadDrains sends trips to the compact station until
// its four bays are full and a pod waits for a bay. Then demand stops, and
// every trip completes by a deadline, also after a restore of the
// congested state.
func TestCompactStationOverloadDrains(t *testing.T) {
	t.Parallel()
	s := newCompactSimulation(t)
	r := newCompactRun(t, s)
	submitted := 0
	r.run(30*60*TicksPerSecond, func() bool {
		if r.overloaded() {
			return true
		}
		if s.tick%(10*TicksPerSecond) == 0 && submitted < 40 {
			if _, err := s.SubmitTrip("town", "compact"); err != nil {
				t.Fatal(err)
			}
			submitted++
		}
		return false
	})
	congested := r.restore()
	r.finish(30 * 60 * TicksPerSecond)
	if s.completed != submitted {
		t.Fatalf("%d of %d trips completed", s.completed, submitted)
	}
	congested.finish(30 * 60 * TicksPerSecond)
	if congested.s.completed != submitted {
		t.Fatalf("after the restore, %d of %d trips completed", congested.s.completed, submitted)
	}
}

// submitBothWays submits trips both ways between the compact station and
// the town station, and returns their number.
func submitBothWays(t *testing.T, s *Simulation, rounds int) int {
	t.Helper()
	trips := 0
	for range rounds {
		for _, trip := range [][2]string{{"town", "compact"}, {"compact", "town"}} {
			if _, err := s.SubmitTrip(trip[0], trip[1]); err != nil {
				t.Fatal(err)
			}
			trips++
		}
	}
	return trips
}

// compactPlace reports whether a pod is at a place of the compact station.
type compactPlace func(s *Simulation, pod Pod) bool

func laneEnd(s *Simulation, pod Pod) float64 {
	return s.network.Length(s.network.Lanes[s.graph.lanes[pod.LaneID]]) - pod.LaneDistance
}

var compactPlaces = map[string]compactPlace{
	"holding": func(s *Simulation, pod Pod) bool { return pod.LaneID == "approach" && laneEnd(s, pod) < 15 },
	"aisle":   func(_ *Simulation, pod Pod) bool { return pod.LaneID == "aisle-3" },
	"bay":     func(_ *Simulation, pod Pod) bool { return pod.LaneID == "bay-2-in-3" || pod.LaneID == "bay-2-out-3" },
	"receiving": func(s *Simulation, pod Pod) bool {
		return pod.LaneID == "aisle-out" && laneEnd(s, pod) < 3 || pod.LaneID == "leave" && pod.LaneDistance < 3
	},
}

// TestCompactStationFaults faults a pod for 5 seconds at the holding node,
// in the aisle, in a bay, and at the receiving node. Each fault clears,
// all trips complete, and the group resource ends free.
func TestCompactStationFaults(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"holding", "aisle", "bay", "receiving"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newCompactSimulation(t)
			if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
				t.Fatal(err)
			}
			r := newCompactRun(t, s)
			trips := submitBothWays(t, s, 6)
			seconds := int64(5)
			r.run(30*60*TicksPerSecond, func() bool {
				for i := range s.vehicles {
					if pod := s.vehicles[i].Pod; compactPlaces[name](s, pod) {
						if _, err := s.Fault(FaultRequest{PodID: pod.ID, DurationSeconds: &seconds}); err != nil {
							t.Fatal(err)
						}
						return true
					}
				}
				return false
			})
			r.finish(60 * 60 * TicksPerSecond)
			if s.completed != trips || len(s.faults) != 0 || s.faultCounters.started != 1 || s.faultCounters.cleared != 1 || s.faultCounters.evacuations != 0 {
				t.Fatalf("%d of %d trips completed, %d faults, counters %+v", s.completed, trips, len(s.faults), s.faultCounters)
			}
		})
	}
}

// TestCompactStationEmergency starts an emergency for a pod with a party
// in the aisle. The emergency ends with the party interrupted at a berth,
// and all other trips complete.
func TestCompactStationEmergency(t *testing.T) {
	t.Parallel()
	s := newCompactSimulation(t)
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
	r := newCompactRun(t, s)
	trips := submitBothWays(t, s, 4)
	r.run(30*60*TicksPerSecond, func() bool {
		for i := range s.vehicles {
			if v := &s.vehicles[i]; compactPlaces["aisle"](s, v.Pod) && v.carriesPassengers() {
				if _, err := s.Emergency(v.Pod.ID, 0); err != nil {
					t.Fatal(err)
				}
				return true
			}
		}
		return false
	})
	if len(s.emergencies) != 1 {
		t.Fatalf("%d emergencies", len(s.emergencies))
	}
	r.finish(60 * 60 * TicksPerSecond)
	if s.completed != trips-1 || s.interrupted != 1 || len(s.emergencies) != 0 || s.emergencyCounters.ended != 1 {
		t.Fatalf("%d of %d trips completed, %d interrupted, %d emergencies, counters %+v", s.completed, trips, s.interrupted, len(s.emergencies), s.emergencyCounters)
	}
}

// TestCompactStationRestore restores the station with a pod on a reverse
// lane, with a pod in a reverse dwell, and with a pod in the aisle. Each
// restored run completes all trips.
func TestCompactStationRestore(t *testing.T) {
	t.Parallel()
	reverse := func(s *Simulation, pod Pod) bool {
		return pod.Speed > 0 && slices.Contains(s.network.ManeuverGroups[0].ReverseLaneIDs, pod.LaneID)
	}
	for _, test := range []struct {
		name string
		at   func(s *Simulation, v *vehicle) bool
	}{
		{"reverse lane", func(s *Simulation, v *vehicle) bool { return reverse(s, v.Pod) }},
		{"dwell", func(_ *Simulation, v *vehicle) bool {
			return v.reversal.lane > 0 && v.reversal.ticks == reversalDwellTicks/2
		}},
		{"aisle", func(s *Simulation, v *vehicle) bool { return compactPlaces["aisle"](s, v.Pod) && v.Pod.Speed > 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newCompactSimulation(t)
			r := newCompactRun(t, s)
			trips := submitBothWays(t, s, 6)
			r.run(30*60*TicksPerSecond, func() bool {
				return slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return test.at(s, &v) })
			})
			restored := r.restore()
			restored.finish(60 * 60 * TicksPerSecond)
			if restored.s.completed != trips {
				t.Fatalf("%d of %d trips completed", restored.s.completed, trips)
			}
		})
	}
}
