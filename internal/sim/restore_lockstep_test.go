package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// lockstepTicks is the time that a lockstep run steps the live and the
// restored simulations together.
const lockstepTicks = 60 * TicksPerSecond

// lockstepCase is a lockstep run. build returns the live simulation, and
// input returns the restore input of a save of it. The run restores the
// live simulation at the first tick where restoreAt is true, or fails when
// it finds none in limit ticks. With no restoreAt, it restores 30 s before
// the first event want. act, when set, gives both simulations the same
// command at a tick after the restore. want is the route change or the
// state that the run must pass through after the restore.
type lockstepCase struct {
	build     func(t *testing.T) *Simulation
	input     func(live *Simulation, state SavedState) RestoreStateInput
	restoreAt func(s *Simulation) bool
	limit     int64
	act       func(t *testing.T, s *Simulation, ticks int64)
	// settings, when set, gives the restored simulation the settings of
	// the live one that the save does not keep, as a session does.
	settings func(t *testing.T, s *Simulation)
	want     string
}

// TestRestoreLockstep restores a save of whole routes and steps the live
// and the restored simulations together for 60 s. At each tick it compares
// each pod bit for bit, and the owners, also at the restore tick. Each
// second it also compares the saved state. The runs pass through a
// diversion, a depot extension, a berth-suffix extension, a restore inside
// a reversal dwell, a restore of a pod that waits at a full depot entry, a
// restore of a follower in the tick of the grant of the end block of its
// link, and restores of a pod at an exact block end inside a lane, held
// there or with the next block (exact restore coordinates
// proposal, section 5.3, and EC-D8). Pickup swaps are
// off: the save does not keep the state of the swap controller (EC-D9).
func TestRestoreLockstep(t *testing.T) {
	t.Parallel()
	plain := func(network Network) func(*Simulation, SavedState) RestoreStateInput {
		return func(live *Simulation, state SavedState) RestoreStateInput {
			return RestoreStateInput{Network: network, Fleet: live.initial, State: state}
		}
	}
	cases := map[string]lockstepCase{
		"diversion": {
			build: func(t *testing.T) *Simulation {
				t.Helper()
				s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "market"}})
				if err != nil {
					t.Fatal(err)
				}
				if !s.park(s.findVehicle("01")) {
					t.Fatal("01 did not start a parking move")
				}
				return s
			},
			input: plain(Example()),
			restoreAt: func(s *Simulation) bool {
				v := s.findVehicle("01")
				return v.Pod.LaneID == "return-start" && v.Pod.LaneDistance > 20
			},
			limit: 90 * TicksPerSecond,
			act: func(t *testing.T, s *Simulation, ticks int64) {
				t.Helper()
				if ticks == 2*TicksPerSecond {
					if err := s.RequestTrip("harbor", "garden"); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: "diversion",
		},
		"depot extension": {
			build: func(t *testing.T) *Simulation {
				t.Helper()
				network := shortDepotNetwork(2, 3)
				s := newDepotFleet(t, network, depotPlacements(network, 3))
				sendToDepot(t, s, "harbor-pod")
				return s
			},
			input: func(live *Simulation, state SavedState) RestoreStateInput {
				return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: live.network, Fleet: live.initial, State: state}
			},
			limit: 90 * TicksPerSecond,
			want:  "depot",
		},
		"berth suffix": {
			build: func(t *testing.T) *Simulation {
				t.Helper()
				fleet := []Placement{{ID: "01", Class: ExpressClass, StationID: "harbor", BerthID: "harbor-1"}}
				s, err := expressPhysicalFleet(largeRestoreNetwork(), fleet)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 20, SharingConsent: PrivateConsent}); err != nil {
					t.Fatal(err)
				}
				return s
			},
			input: func(live *Simulation, state SavedState) RestoreStateInput {
				return RestoreStateInput{
					OrderContract: ExpressOrderContract, Network: live.network, Fleet: live.initial, State: state,
					ExpressServices: slices.Collect(maps.Values(live.expressServices)), OnboardPickups: live.onboardPickups,
				}
			},
			limit: 200 * TicksPerSecond,
			want:  "berth",
		},
		"reversal dwell": {
			build: func(t *testing.T) *Simulation {
				t.Helper()
				s, err := NewFleet(tallBayNetwork(2, 7), []Placement{{ID: "01", StationID: "bays", BerthID: "bay-1", Class: CompactClass}})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.RequestJourney("01", "stop"); err != nil {
					t.Fatal(err)
				}
				return s
			},
			input: plain(tallBayNetwork(2, 7)),
			restoreAt: func(s *Simulation) bool {
				return s.findVehicle("01").reversal.ticks == reversalDwellTicks/2
			},
			limit: 90 * TicksPerSecond,
			want:  "dwell",
		},
	}
	// corridor returns the merge corridor with its queues and no
	// platoons. A pod of the side queue waits at an interior block end
	// for the merge. With granted true, a test hook keeps a pod at rest
	// in the tick where it gets the block after the end, once, so live
	// motion moves it into that block at the same distance.
	corridor := func(granted bool) func(t *testing.T) *Simulation {
		return func(t *testing.T) *Simulation {
			t.Helper()
			s := restoreCorridor(t, mergeCorridor(true, 30), corridorQueues(corridorFeedLength-100))
			if granted {
				fired := false
				s.coastCheck = func(s *Simulation) {
					for index := range s.vehicles {
						if v := &s.vehicles[index]; !fired && blockTie(v) == "held at a block end" && v.reservedThrough > v.blockIndex {
							v.coast, fired = coastCap{ceiling: 0, tick: s.tick, set: true}, true
						}
					}
				}
			}
			return s
		}
	}
	tied := func(event string) func(s *Simulation) bool {
		return func(s *Simulation) bool {
			return slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return blockTie(&v) == event })
		}
	}
	// entryWait is the fleet of TestDepotEntryWait: the mover waits at a
	// full depot entry until the withdrawn pod at slot 2 returns to
	// service 2 s after the restore, and then binds that slot.
	entryWait := lockstepCase{
		build: func(t *testing.T) *Simulation {
			t.Helper()
			s := depotEntryWaitFleet(t, faultHold)
			sendToDepot(t, s, "pod-"+depotSlotID(2, 1))
			return s
		},
		input: func(live *Simulation, state SavedState) RestoreStateInput {
			return RestoreStateInput{Network: live.network, Fleet: live.initial, State: state}
		},
		restoreAt: func(s *Simulation) bool {
			return s.findVehicle("pod-"+depotSlotID(2, 1)).Pod.WaitReason == ParkingUnavailable
		},
		limit: 300 * TicksPerSecond,
		act: func(t *testing.T, s *Simulation, ticks int64) {
			t.Helper()
			if ticks == 2*TicksPerSecond {
				if err := s.restoreService(s.findVehicle("pod-"+depotSlotID(2, 2)), faultHold); err != nil {
					t.Fatal(err)
				}
			}
		},
		want: "depot",
	}
	cases["depot entry wait"] = entryWait
	// endGrant reports a follower whose grant took the end block of its
	// link after the link maintenance of the tick, so that its draining
	// flag is still false.
	endGrant := func(s *Simulation) bool {
		return slices.ContainsFunc(s.vehicles, func(v vehicle) bool {
			return v.link.leader != 0 && !v.link.draining && v.reservedThrough >= v.link.end
		})
	}
	cases["end grant"] = lockstepCase{
		build: func(t *testing.T) *Simulation {
			t.Helper()
			s := restoreCorridor(t, mergeCorridor(true, 30), corridorQueues(corridorFeedLength-100))
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			return s
		},
		input: plain(mergeCorridor(true, 30)), restoreAt: endGrant,
		settings: func(t *testing.T, s *Simulation) {
			t.Helper()
			if err := s.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
		},
		limit: 900 * TicksPerSecond, want: "restore at an end grant",
	}
	cases["held at a block end"] = lockstepCase{
		build: corridor(false), input: plain(mergeCorridor(true, 30)), restoreAt: tied("held at a block end"),
		limit: 60 * TicksPerSecond, want: "restore held at a block end",
	}
	cases["granted through a block end"] = lockstepCase{
		build: corridor(true), input: plain(mergeCorridor(true, 30)), restoreAt: tied("granted through a block end"),
		limit: 60 * TicksPerSecond, want: "restore granted through a block end",
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if test.restoreAt == nil {
				at := lockstepEventTick(t, test.build(t), test.want, test.limit) - lockstepTicks/2
				test.restoreAt = func(s *Simulation) bool { return s.tick >= at }
			}
			checkLockstep(t, test)
		})
	}
}

// lockstepEventTick steps s until its route watch sees event, and returns
// the tick.
func lockstepEventTick(t *testing.T, s *Simulation, event string, limit int64) int64 {
	t.Helper()
	watch := newRouteWatch()
	for s.tick < limit {
		s.Step()
		if slices.Contains(watch.observe(s), event) {
			return s.tick
		}
	}
	t.Fatalf("no %s in %d ticks", event, limit)
	return 0
}

// checkLockstep runs test.
func checkLockstep(t *testing.T, test lockstepCase) {
	t.Helper()
	live := test.build(t)
	watch := newRouteWatch()
	for !test.restoreAt(live) {
		if live.tick >= test.limit {
			t.Fatalf("no restore tick in %d ticks", test.limit)
		}
		live.Step()
		watch.observe(live)
	}
	state := live.ExportState()
	dwell := false
	for index, pod := range state.Pods {
		if pod.RouteTrimmed {
			t.Fatalf("pod %s saves a trimmed route", pod.ID)
		}
		dwell = dwell || live.vehicles[index].reversal.lane > 0
	}
	restored, result, err := RestoreState(test.input(live, roundTripState(t, state)))
	if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
		t.Fatalf("restore at tick %d: %+v, %v", live.tick, result, err)
	}
	if test.settings != nil {
		test.settings(t, restored)
	}
	events := map[string]bool{}
	if dwell {
		events["restore in a dwell"] = true
	}
	for index := range live.vehicles {
		v := &live.vehicles[index]
		if tie := blockTie(v); tie != "" {
			events["restore "+tie] = true
		}
		if v.link.leader != 0 && !v.link.draining && v.reservedThrough >= v.link.end {
			events["restore at an end grant"] = true
		}
	}
	// The run compares the simulations at the restore tick, before the
	// first step, and then after each step.
	for ticks := int64(0); ticks <= lockstepTicks; ticks++ {
		if ticks > 0 {
			if test.act != nil {
				test.act(t, live, ticks)
				test.act(t, restored, ticks)
			}
			live.Step()
			restored.Step()
		}
		for _, event := range watch.observe(live) {
			events[event] = true
		}
		for index := range live.vehicles {
			if v := &live.vehicles[index]; dwell && v.reversal.lane < 0 {
				events["dwell"] = true
			}
		}
		if d := lockstepDiff(live, restored, ticks == 0, ticks%TicksPerSecond == 0); d != "" {
			t.Fatalf("tick %d, %d ticks after the restore: %s", live.tick, ticks, d)
		}
	}
	if !events[test.want] {
		t.Fatalf("the run after the restore at tick %d has the events %v, not %s", live.tick-lockstepTicks, slices.Sorted(maps.Keys(events)), test.want)
	}
}

// blockTie returns "held at a block end" for a traveling pod at the exact
// end of its block, before the last block of its lane, and "granted
// through a block end" for a traveling pod at the exact end of the block
// before its block in the same lane. Otherwise it returns "".
func blockTie(v *vehicle) string {
	if v.Pod.Activity != Traveling || v.blocks.len() == 0 {
		return ""
	}
	b := v.blockIndex
	lane := v.blocks.locate(b, v.blocks.scan)
	switch first, last := v.blocks.lanes[lane].first, v.blocks.lanes[lane+1].first-1; {
	case b < last && v.distance == v.blocks.peekEnd(b):
		return "held at a block end"
	case b > first && v.distance == v.blocks.peekEnd(b-1):
		return "granted through a block end"
	}
	return ""
}

// lockstepDiff returns the first difference of two simulations, or "".
// With restore true, the simulations are at the restore tick, and the
// wait reason and the blocking pod of a pod do not count: the save does
// not keep them, and the next admission sets them. With saved true, it
// also compares their saved states.
func lockstepDiff(a, b *Simulation, restore, saved bool) string {
	if a.tick != b.tick {
		return fmt.Sprintf("tick %d and %d", a.tick, b.tick)
	}
	for index := range a.vehicles {
		x, y := a.vehicles[index], b.vehicles[index]
		if restore {
			x.Pod.WaitReason, x.Pod.BlockedBy = y.Pod.WaitReason, y.Pod.BlockedBy
		}
		if d := lockstepVehicleDiff(&x, &y); d != "" {
			return fmt.Sprintf("pod %s: %s", a.vehicles[index].Pod.ID, d)
		}
	}
	if !maps.Equal(a.owners, b.owners) {
		return "owners"
	}
	if saved && !reflect.DeepEqual(a.ExportState(), b.ExportState()) {
		return "saved state"
	}
	return ""
}

// lockstepVehicleDiff returns the first field of two vehicles that
// differs, or "". The motion fields count for a traveling pod, and the
// route for a pod that uses it.
func lockstepVehicleDiff(a, b *vehicle) string {
	traveling := a.Pod.Activity == Traveling
	bits := math.Float64bits
	switch {
	case a.Pod != b.Pod:
		return fmt.Sprintf("pod %+v and %+v", a.Pod, b.Pod)
	case traveling && bits(a.distance) != bits(b.distance):
		return fmt.Sprintf("distance %v and %v", a.distance, b.distance)
	case traveling && (a.blockIndex != b.blockIndex || a.reservedThrough != b.reservedThrough):
		return fmt.Sprintf("block %d and %d, frontier %d and %d", a.blockIndex, b.blockIndex, a.reservedThrough, b.reservedThrough)
	case a.pending != b.pending || a.pending >= 0 && a.waitSince != b.waitSince:
		return fmt.Sprintf("wait %d at %d and %d at %d", a.pending, a.waitSince, b.pending, b.waitSince)
	case a.link != b.link || a.follower != b.follower:
		return fmt.Sprintf("link %+v and %+v", a.link, b.link)
	case traveling && a.reversal != b.reversal:
		return fmt.Sprintf("reversal %+v and %+v", a.reversal, b.reversal)
	case len(a.Boardings) > 0 && bits(a.riddenMeters()) != bits(b.riddenMeters()):
		return fmt.Sprintf("ridden meters %v and %v", a.riddenMeters(), b.riddenMeters())
	case !routed(a.Pod.Activity):
		return ""
	case !slices.EqualFunc(a.Route, b.Route, func(x, y Lane) bool { return x.ID == y.ID }):
		return "route"
	case !slices.EqualFunc(a.blocks.lanes, b.blocks.lanes, func(x, y routeLaneCells) bool {
		return bits(x.start) == bits(y.start) && x.first == y.first
	}):
		return "lane starts"
	}
	return ""
}

// routeWatch classifies the route changes of traveling pods. A route that
// keeps its lanes and adds lanes after them is an extension: "depot" when
// an added lane is a depot lane, and "berth" otherwise. Each other change
// is a "diversion".
type routeWatch struct {
	versions map[string]uint64
	routes   map[string][]string
	active   map[string]bool
}

func newRouteWatch() *routeWatch {
	return &routeWatch{versions: map[string]uint64{}, routes: map[string][]string{}, active: map[string]bool{}}
}

// observe returns the route changes of s since the last call.
func (w *routeWatch) observe(s *Simulation) []string {
	var events []string
	for index := range s.vehicles {
		v := &s.vehicles[index]
		id := v.Pod.ID
		traveling := v.Pod.Activity == Traveling
		if traveling && w.active[id] && v.routeVersion != w.versions[id] {
			old := w.routes[id]
			now := laneIDs(v.Route)
			switch {
			case len(now) > len(old) && slices.Equal(now[:len(old)], old):
				kind := "berth"
				if slices.ContainsFunc(v.Route[len(old):], func(lane Lane) bool { return s.depots.depotLane(lane.ID) }) {
					kind = "depot"
				}
				events = append(events, kind)
			default:
				events = append(events, "diversion")
			}
		}
		if traveling && (!w.active[id] || v.routeVersion != w.versions[id]) {
			w.routes[id] = laneIDs(v.Route)
		}
		w.active[id], w.versions[id] = traveling, v.routeVersion
	}
	return events
}
