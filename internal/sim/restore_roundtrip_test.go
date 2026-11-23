package sim

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

// roundTripStats records the largest speed of a traveling pod above the
// speed limit of its lane and the largest change of a position that the
// restores of a fixture saw.
type roundTripStats struct {
	ticks            int
	excess, residual float64
	// lanes holds the lanes where a pod moved, and trimmed counts the
	// saves of a pod whose saved route starts after its live route.
	lanes   map[string]bool
	trimmed int
	// crossings holds each move "a->b" of a moving pod from lane a onto
	// lane b with a lower speed limit, and braking counts the ticks in
	// which a traveling pod slowed. last holds the lane, the speed limit,
	// and the speed of each pod at the previous save.
	crossings map[string]bool
	braking   int
	last      map[string]roundTripPod
	// second counts the second round trips.
	second int
}

// roundTripPod is the lane, the speed limit of the lane, and the speed of a
// pod at a save.
type roundTripPod struct {
	lane         string
	limit, speed float64
}

func newRoundTripStats() roundTripStats {
	return roundTripStats{lanes: make(map[string]bool), crossings: make(map[string]bool), last: make(map[string]roundTripPod)}
}

// roundTripCase is one fixture of TestRestoreMotionEveryTick: a fleet, the
// restore input of a save of the fleet, and the number of ticks to run.
// crossing names a move between lanes with a descending speed limit that a
// moving pod must make, and the fixture must also save a pod with a later
// route start. braking tells that a traveling pod must slow.
type roundTripCase struct {
	name     string
	seed     uint64
	s        *Simulation
	input    func(SavedState) RestoreStateInput
	ticks    int
	crossing string
	braking  bool
	// noJSON skips the JSON round trip of the save.
	noJSON bool
}

// worseThanPlain returns the way in which a restore, which gives result
// and err, is worse than the restore of the same save before the saved
// motion (see headRestore), which gives plain and plainErr, or "". The
// restore must not refuse a save that the plain restore accepts, must
// keep the physical tier where the plain restore keeps it, and must demote
// no pod that the plain restore keeps (restore speed proposal, section
// 4.2).
func worseThanPlain(result RestoreResult, err error, plain RestoreResult, plainErr error) string {
	switch {
	case plainErr == nil && err != nil:
		return fmt.Sprintf("the restore refuses the save with %v, which the restore without the saved motion accepts: %+v", err, plain)
	case plainErr == nil && plain.Tier == RestorePhysical && result.Tier != RestorePhysical:
		return fmt.Sprintf("the restore gives %v, %+v, worse than %+v without the saved motion", err, result, plain)
	case plainErr == nil && plain.Tier == RestorePhysical:
		for _, id := range result.Demoted {
			if !slices.Contains(plain.Demoted, id) {
				return fmt.Sprintf("the restore demotes %s, which the restore without the saved motion keeps: %+v, %+v", id, result, plain)
			}
		}
	}
	return ""
}

// roundTripTick saves c.s and restores the save, and fails as
// roundTripCheck finds. The failure names the fixture, the seed, and the
// tick.
func roundTripTick(t *testing.T, c roundTripCase, stats *roundTripStats) {
	t.Helper()
	got := roundTripCheck(t, c, stats)
	if got.failure != "" || len(got.dropped)+len(got.extra) != 0 {
		t.Fatalf("%s, seed %d, tick %d: %s; the restore drops the owners of %v and adds owners of %v", c.name, c.seed, c.s.tick, got.failure, got.dropped, got.extra)
	}
	if c.s.tick%secondRoundTripTicks == 0 {
		if failure := secondRoundTrip(t, c, got.restored); failure != "" {
			t.Fatalf("%s, seed %d, tick %d: %s", c.name, c.seed, c.s.tick, failure)
		}
		stats.second++
	}
}

// secondRoundTripTicks is the number of ticks from one second round trip
// of roundTripTick to the next.
const secondRoundTripTicks = 4

// secondRoundTrip saves the restored simulation first of c and restores
// the save. The second restore must keep the speed bits, the frontiers,
// and the owners of the first restore exactly (see roundTripCheck), and
// the route distance of each traveling pod bit for bit, as the save gives
// it. It returns the failure, or "".
func secondRoundTrip(t *testing.T, c roundTripCase, first *Simulation) string {
	t.Helper()
	c.s = first
	stats := newRoundTripStats()
	got := roundTripCheck(t, c, &stats)
	if got.failure != "" || len(got.dropped)+len(got.extra) != 0 {
		return fmt.Sprintf("second round trip: %s; the restore drops the owners of %v and adds owners of %v", got.failure, got.dropped, got.extra)
	}
	for index := range first.vehicles {
		v := &first.vehicles[index]
		if v.Pod.Activity != Traveling || v.blocks.len() == 0 {
			continue
		}
		_, offset, _ := first.savedStart(v)
		if want, distance := v.distance-offset, got.restored.vehicles[index].distance; math.Float64bits(distance) != math.Float64bits(want) {
			return fmt.Sprintf("second round trip, pod %s: restored at %v, want %v", v.Pod.ID, distance, want)
		}
	}
	return ""
}

// roundTripResult is the result of roundTripCheck: the first failure or
// "", the resources that the live simulation owns and the restore does
// not (dropped), the resources that the restore owns and the live
// simulation does not (extra), the save, and the restored simulation.
type roundTripResult struct {
	failure        string
	dropped, extra []resource
	state          SavedState
	restored       *Simulation
}

// roundTripCheck saves c.s and restores the save with c.input. A save
// without an incident record takes a strict JSON round trip first, unless
// c.noJSON, because the plain JSON omits the members that only the session
// adapter saves. The restore must accept the save, keep every pod in the
// physical tier, keep the speed bits of each pod (0 for a faulted pod),
// and keep the frontier of each traveling pod at the same route occurrence
// and cell. The failure names the pod. For a refusal or a demotion, it
// also tells whether the save with no saved motion fails in the same way.
// A resource that both simulations own with different owners is a
// failure. The result lists the other differences of the owners.
func roundTripCheck(t *testing.T, c roundTripCase, stats *roundTripStats) roundTripResult {
	t.Helper()
	s := c.s
	state := s.ExportState()
	if !c.noJSON && len(s.faults)+len(s.emergencies) == 0 {
		state = roundTripState(t, state)
	}
	restored, result, err := RestoreState(c.input(state))
	_, plain, plainErr := headRestore(c.input(state))
	if failure := worseThanPlain(result, err, plain, plainErr); failure != "" {
		return roundTripResult{failure: failure, state: state}
	}
	if err != nil || !cleanRestore(result) || len(result.Interrupted)+len(result.LogicalCompleted)+result.DroppedFaults+result.DroppedEmergencies+result.Unaccounted != 0 {
		return roundTripResult{failure: fmt.Sprintf("restore %v, %+v; without the saved motion: %v, %+v", err, result, plainErr, plain), state: state}
	}
	got := roundTripResult{state: state, restored: restored}
	stats.ticks++
	for index := range s.vehicles {
		v, copied := &s.vehicles[index], &restored.vehicles[index]
		want := v.Pod.Speed
		if v.faulted {
			want = 0
		}
		if math.Float64bits(copied.Pod.Speed) != math.Float64bits(want) {
			got.failure = fmt.Sprintf("pod %s: restored speed %v, want %v", v.Pod.ID, copied.Pod.Speed, want)
			return got
		}
		if v.Pod.Activity != Traveling || v.blocks.len() == 0 {
			delete(stats.last, v.Pod.ID)
			continue
		}
		start, offset, current := s.savedStart(v)
		if wantThrough := v.reservedThrough - v.blocks.laneFirst(start); copied.reservedThrough != wantThrough {
			got.failure = fmt.Sprintf("pod %s: restored frontier %d, want %d", v.Pod.ID, copied.reservedThrough, wantThrough)
			return got
		}
		limit := v.blocks.route[current].SpeedLimit
		stats.excess = max(stats.excess, v.Pod.Speed-limit)
		if v.Pod.Speed > 0 {
			stats.lanes[v.Pod.LaneID] = true
		}
		if last, ok := stats.last[v.Pod.ID]; ok {
			if v.Pod.Speed > 0 && last.lane != v.Pod.LaneID && limit < last.limit {
				stats.crossings[last.lane+"->"+v.Pod.LaneID] = true
			}
			if v.Pod.Speed > 0 && v.Pod.Speed < last.speed {
				stats.braking++
			}
		}
		stats.last[v.Pod.ID] = roundTripPod{lane: v.Pod.LaneID, limit: limit, speed: v.Pod.Speed}
		if start > 0 {
			stats.trimmed++
		}
		stats.residual = max(stats.residual, math.Abs(copied.distance-(v.distance-offset)))
	}
	for _, held := range slices.SortedFunc(maps.Keys(s.owners), compareResources) {
		switch owner := restored.owners[held]; {
		case owner.isZero():
			got.dropped = append(got.dropped, held)
		case owner != s.owners[held]:
			got.failure = fmt.Sprintf("resource %+v has the restored owner %+v, want %+v", held, owner, s.owners[held])
			return got
		}
	}
	for _, held := range slices.SortedFunc(maps.Keys(restored.owners), compareResources) {
		if _, ok := s.owners[held]; !ok {
			got.extra = append(got.extra, held)
		}
	}
	return got
}

// compareResources orders resources by kind, ID, and cell.
func compareResources(a, b resource) int {
	return cmp.Or(cmp.Compare(a.kind, b.kind), strings.Compare(a.id, b.id), cmp.Compare(a.cell, b.cell))
}

// boundaryLanes is a straight path with a short lane, descending speed
// limits, a slow lane, a lane of 122 m with five cells, and enough lanes
// that the saved route of a pod starts later as it moves.
var boundaryLanes = []struct {
	length, limit float64
}{
	{300, 14}, {17, 3}, {100, 5}, {50, 14}, {30, 0.5}, {200, 14}, {122, 14}, {300, 5}, {60, 14}, {300, 14},
}

// boundaryCase returns a fleet of pods on boundaryLanes. The seed sets the
// number of pods, their positions on the first lane, and the gap between
// them.
func boundaryCase(t *testing.T, seed uint64) roundTripCase {
	t.Helper()
	lanes := make([]geometryLane, len(boundaryLanes))
	x := 0.0
	for index, lane := range boundaryLanes {
		x += lane.length
		lanes[index] = geometryLane{to: Point{X: x}}
	}
	pods := 1 + int(seed%3)
	network := geometryNetwork(Point{}, lanes, pods)
	for index := range network.Lanes {
		var lane int
		if _, err := fmt.Sscanf(network.Lanes[index].ID, "l%d", &lane); err == nil {
			network.Lanes[index].SpeedLimit = boundaryLanes[lane].limit
		}
	}
	s := restoreGeometry(t, network, len(lanes), pods, 120+13.7*float64(seed), 31+3.1*float64(seed))
	return roundTripCase{
		name: "boundary", seed: seed, s: s, ticks: 180 * TicksPerSecond, crossing: "l3->l4",
		input: func(state SavedState) RestoreStateInput {
			return RestoreStateInput{Network: network, Fleet: s.initial, State: state}
		},
	}
}

// TestRestoreMotionEveryTick saves and restores fixtures at every tick (see
// roundTripTick). The fixtures cover a short lane, a slow lane, descending
// speed limits, saved routes that start later, Express braking, a depot
// claim, a reversal, a pod fault, and draining platoon links. The session
// test TestSavedMotionCodecEveryTick takes saves through the codec of the
// state file.
func TestRestoreMotionEveryTick(t *testing.T) {
	t.Parallel()
	skipLong(t)
	builders := map[string]func(t *testing.T) roundTripCase{
		"boundary 1": func(t *testing.T) roundTripCase {
			t.Helper()
			return boundaryCase(t, 1)
		},
		"boundary 2": func(t *testing.T) roundTripCase {
			t.Helper()
			return boundaryCase(t, 2)
		},
		"boundary 3": func(t *testing.T) roundTripCase {
			t.Helper()
			return boundaryCase(t, 3)
		},
		"express": func(t *testing.T) roundTripCase {
			t.Helper()
			network := largeRestoreNetwork()
			fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
			s, err := expressPhysicalFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
			return roundTripCase{
				name: "express", s: s, ticks: 200 * TicksPerSecond, braking: true,
				input: func(state SavedState) RestoreStateInput {
					services := slices.Collect(maps.Values(s.expressServices))
					return RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, State: state, ExpressServices: services, OnboardPickups: s.onboardPickups}
				},
			}
		},
		"depot": func(t *testing.T) roundTripCase {
			t.Helper()
			network := shortDepotNetwork(2, 3)
			s := newDepotFleet(t, network, depotPlacements(network, 3))
			sendToDepot(t, s, "harbor-pod")
			return roundTripCase{
				name: "depot", s: s, ticks: 90 * TicksPerSecond,
				input: func(state SavedState) RestoreStateInput {
					return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: state}
				},
			}
		},
		"reversal": func(t *testing.T) roundTripCase {
			t.Helper()
			network := tallBayNetwork(2, 7)
			fleet := []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}}
			s, err := NewFleet(network, fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.RequestJourney("01", "stop"); err != nil {
				t.Fatal(err)
			}
			return roundTripCase{
				name: "reversal", s: s, ticks: 90 * TicksPerSecond,
				input: func(state SavedState) RestoreStateInput {
					return RestoreStateInput{Network: network, Fleet: fleet, State: state}
				},
			}
		},
		"fault": func(t *testing.T) roundTripCase {
			t.Helper()
			s := faultLegFleet(t)
			v := boardParties(t, s, "s2", "s2")
			cruiseOn(t, s, v, "s0-link")
			startFault(t, s, v, 20)
			return roundTripCase{
				name: "fault", s: s, ticks: 40 * TicksPerSecond,
				input: func(state SavedState) RestoreStateInput { return faultRestoreInput(s, state) },
			}
		},
		"draining link": func(t *testing.T) roundTripCase {
			t.Helper()
			network := geometryNetwork(Point{}, motionLanes, 4)
			s := restoreGeometry(t, network, len(motionLanes), 4, 200, 30)
			s.emergenciesOn = true
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			for range 5 * TicksPerSecond {
				s.Step()
			}
			startEmergency(t, s, &s.vehicles[2], 0)
			return roundTripCase{
				name: "draining link", s: s, ticks: 30 * TicksPerSecond,
				input: func(state SavedState) RestoreStateInput { return emergencyRestoreInput(s, state) },
			}
		},
	}
	for _, name := range slices.Sorted(maps.Keys(builders)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := builders[name](t)
			stats := newRoundTripStats()
			for range c.ticks {
				roundTripTick(t, c, &stats)
				c.s.Step()
			}
			t.Logf("%s: %d restores, %d second round trips; largest speed over a lane limit %v m/s; largest position residual %v m; %d lanes with motion; %d saves of a pod with a later route start",
				c.name, stats.ticks, stats.second, stats.excess, stats.residual, len(stats.lanes), stats.trimmed)
			if stats.second == 0 {
				t.Fatalf("%s: no second round trip", c.name)
			}
			if c.crossing != "" && (!stats.crossings[c.crossing] || stats.trimmed == 0) {
				t.Fatalf("%s: the moves onto a lower limit %v and %d saves with a later route start, want %s and more than 0", c.name, slices.Sorted(maps.Keys(stats.crossings)), stats.trimmed, c.crossing)
			}
			if c.braking && stats.braking == 0 {
				t.Fatalf("%s: no tick in which a traveling pod slowed", c.name)
			}
		})
	}
}
