package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"
)

// entryRuns counts the ticks of entry runs. runs counts the ticks of
// followers whose run holds the entry lane "dest-access-in", shared the
// ticks of followers that hold a cell of that lane of another pod, and
// diverge the ticks of followers that hold the junction of the station
// diverge of another pod. approaches holds the approaches of the followers
// with an entry run.
type entryRuns struct {
	runs, shared, diverge int
	approaches            map[string]bool
}

// holdsEntryLane reports whether the run of the link of v holds the entry
// lane "dest-access-in".
func holdsEntryLane(v *vehicle) bool {
	return slices.ContainsFunc(v.Route[v.link.lane:v.link.lane+v.link.lanes], func(lane Lane) bool { return lane.ID == "dest-access-in" })
}

func (c *entryRuns) record(s *Simulation) {
	if c.approaches == nil {
		c.approaches = make(map[string]bool)
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.link.leader == 0 {
			continue
		}
		if holdsEntryLane(v) {
			c.runs++
			for _, lane := range v.Route {
				if lane.StationRole == StationApproachRole {
					c.approaches[lane.ID] = true
				}
			}
		}
		for r := range v.routeReleases {
			owner := s.owners[r]
			if owner.isZero() || owner.isPod(v.Pod.ID) {
				continue
			}
			switch {
			case r.kind == trackResource && r.id == "dest-access-in":
				c.shared++
			case r == divergeJunctionResource:
				c.diverge++
			}
		}
	}
}

// entryGrowth records the followers whose run grows onto the entry lane
// "dest-access-in" after their link forms without it.
type entryGrowth struct {
	// formed holds, for each follower, whether the run of its link held
	// the entry lane when the link formed.
	formed map[int]bool
	grown  map[int]bool
}

func (g *entryGrowth) record(s *Simulation) {
	if g.formed == nil {
		g.formed, g.grown = make(map[int]bool), make(map[int]bool)
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.link.leader == 0 {
			delete(g.formed, i)
			continue
		}
		entry := holdsEntryLane(v)
		if before, ok := g.formed[i]; !ok {
			g.formed[i] = entry
		} else if entry && !before {
			g.grown[i] = true
		}
	}
}

// divergeJunctionResource is the junction of the station diverge of
// entryNetwork.
var divergeJunctionResource = resource{kind: junctionResource, id: "dest-diverge"}

// freeGrants checks the free grants of the junction of the station
// diverge. Before each step, it records each waiting request whose span
// holds the junction and has no resource of another pod. When the step
// gives the free junction to a pod, no aged request of that record may
// precede the request of the pod in admission order. order holds the pods
// in the order of their free grants, and aged the pods whose request was
// aged at the grant.
type freeGrants struct {
	waiting []intent
	free    bool
	order   []string
	aged    map[string]bool
}

func (g *freeGrants) before(s *Simulation) {
	g.waiting = g.waiting[:0]
	g.free = s.owners[divergeJunctionResource].isZero()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Traveling || v.pending < 0 || v.pending >= v.blocks.len() {
			continue
		}
		holds, free := false, true
		for resources := range v.blocks.spanResources(v.pending, reservationEnd(&v.blocks, v.pending)+1) {
			for _, r := range resources {
				holds = holds || r == divergeJunctionResource
				if owner := s.owners[r]; !owner.isZero() && !owner.isPod(v.Pod.ID) {
					free = false
				}
			}
		}
		if holds && free {
			g.waiting = append(g.waiting, intent{index: i, since: v.waitSince, id: v.Pod.ID})
		}
	}
}

func (g *freeGrants) after(t *testing.T, s *Simulation) {
	t.Helper()
	if g.aged == nil {
		g.aged = make(map[string]bool)
	}
	owner := s.owners[divergeJunctionResource].podID()
	if !g.free || owner == "" {
		return
	}
	// A pod that did not wait requests at this tick.
	granted := intent{since: s.tick, id: owner}
	for _, in := range g.waiting {
		if in.id == owner {
			granted = in
		}
	}
	for _, in := range g.waiting {
		if in.id != owner && s.tick-in.since >= admissionAgeTicks && compareAdmission(in, granted, s.tick) < 0 {
			t.Fatalf("tick %d: %s takes the free diverge before the aged request of %s since tick %d", s.tick, owner, in.id, in.since)
		}
	}
	g.order = append(g.order, owner)
	g.aged[owner] = s.tick-granted.since >= admissionAgeTicks
}

// entryWatch steps an entry test with its monitors.
type entryWatch struct {
	monitor *entryMonitor
	runs    entryRuns
	growth  entryGrowth
	grants  freeGrants
	// passed holds the pods in the order in which they reach the entry
	// lane.
	passed []string
	// leaders holds the predecessor of each pod after the last step.
	leaders map[int]int
}

func newEntryWatch(s *Simulation) *entryWatch {
	w := &entryWatch{monitor: newEntryMonitor(s), leaders: make(map[int]int)}
	for i := range s.vehicles {
		w.leaders[i] = s.vehicles[i].link.leader
	}
	return w
}

func (w *entryWatch) step(t *testing.T, s *Simulation) {
	t.Helper()
	w.grants.before(s)
	s.Step()
	w.monitor.check(t)
	w.runs.record(s)
	w.growth.record(s)
	w.grants.after(t, s)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.LaneID == "dest-access-in" && !slices.Contains(w.passed, v.Pod.ID) {
			w.passed = append(w.passed, v.Pod.ID)
		}
		// A new link never starts on an entry lane.
		if v.link.leader != 0 && v.link.leader != w.leaders[i] && v.blocks.currentLane(v.blockIndex).StationRole == StationEntryRole {
			t.Fatalf("tick %d: pod %s links to pod %d on the entry lane", s.tick, v.Pod.ID, v.link.leader)
		}
		w.leaders[i] = v.link.leader
	}
}

// until steps s until done, for up to 600 seconds.
func (w *entryWatch) until(t *testing.T, s *Simulation, what string, done func() bool) {
	t.Helper()
	for end := s.tick + 600*TicksPerSecond; !done(); {
		if s.tick >= end {
			t.Fatalf("tick %d: %s did not happen", s.tick, what)
		}
		w.step(t, s)
	}
}

// arrived reports whether no pod of s travels.
func arrived(s *Simulation) bool {
	return !slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.Pod.Activity == Traveling })
}

// entryQueues returns count pods at rest on the upstream lane of each
// approach, 45 m apart from 1250 m, with the berths 1, 2, 3, 1, and so on.
func entryQueues(count int, approaches ...int) []entryPod {
	var pods []entryPod
	for _, approach := range approaches {
		for index, pod := range approachQueue(approach, count, 1250, 45) {
			pod.berth = index%3 + 1
			pods = append(pods, pod)
		}
	}
	return pods
}

// TestPlatoonEntryLaneRun queues six pods with berths on each approach,
// with approaches of 150 m and of 36 m. Runs on both approaches grow onto
// the entry lane, and followers share the diverge zone and the cells of
// the entry lane before the station entry node. Heads on the other
// approach coast while a follower releases the zone. The monitors check
// each rule at each tick, and every trip completes.
func TestPlatoonEntryLaneRun(t *testing.T) {
	t.Parallel()
	for _, approach := range []float64{150, 36} {
		t.Run(fmt.Sprint(approach), func(t *testing.T) {
			t.Parallel()
			s := restoreEntry(t, entryNetwork(entryShape{join: 30, approach: approach}, 12), entryQueues(6, 1, 2))
			watch := newEntryWatch(s)
			watch.until(t, s, "the arrival of each pod", func() bool { return s.completed == len(s.vehicles) })
			if runs := watch.runs; runs.shared == 0 || runs.diverge == 0 || len(runs.approaches) != 2 {
				t.Fatalf("entry runs %+v", runs)
			}
			// Followers release the diverge zone, and heads coast for it.
			if monitor := watch.monitor; monitor.chained == 0 || monitor.pairs == 0 {
				t.Fatalf("%d ceilings with a follower as the releaser, %d ticks with two or more ceilings", monitor.chained, monitor.pairs)
			}
		})
	}
}

// TestPlatoonEntryLaneGrowth queues four pods on the second approach. The
// leader has no berth yet, so its route ends on the entry lane, and the
// runs of the new links end before it. Each link takes the turn to the end
// of the entry lane (see entryRunTurn). After the leader chooses its
// berth, the run of its follower grows onto the entry lane. With an
// approach of 36 m, the diverge zone covers the approach, and the next
// request of the follower goes from the upstream lane onto the entry lane
// in one grant (see entryRequest).
func TestPlatoonEntryLaneGrowth(t *testing.T) {
	t.Parallel()
	for _, approach := range []float64{150, 36} {
		t.Run(fmt.Sprint(approach), func(t *testing.T) {
			t.Parallel()
			pods := approachQueue(2, 4, 1250, 45)
			for index := range pods[1:] {
				pods[index+1].berth = index%3 + 1
			}
			s := restoreEntry(t, entryNetwork(entryShape{join: 30, approach: approach}, 4), pods)
			watch := newEntryWatch(s)
			watch.step(t, s)
			for _, v := range s.vehicles[1:] {
				if v.link.leader == 0 {
					t.Fatalf("pod %s did not link", v.Pod.ID)
				}
				if want := s.runTurn(v.Route, v.link.lane, 2); v.link.turn != want || want < math.Pi/6-1e-9 {
					t.Fatalf("pod %s has the link turn %v, want the turn %v to the end of the entry lane", v.Pod.ID, v.link.turn, want)
				}
			}
			watch.until(t, s, "the arrival of each pod", func() bool { return s.completed == len(s.vehicles) })
			if !watch.growth.grown[1] {
				t.Fatalf("the run of the follower of the leader did not grow onto the entry lane: %v", watch.growth.grown)
			}
		})
	}
}

// TestPlatoonEntryLaneEnds checks that no run continues past the station
// entry node of its pods. The pods wait on the approaches, so the lane
// after the entry lane is within platoonHorizon when the links form. In
// the first network, that lane has the through role, so only the rule on
// the entry node ends the run. In the second network, the pods go to
// their berth on the lane "dest-alt", which has no station role.
func TestPlatoonEntryLaneEnds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		shape entryShape
		path  []string
		role  StationLaneRole
	}{
		{name: "explicit role", shape: entryShape{join: 30, approach: 150, explicit: true}, role: StationThroughRole},
		{name: "unmarked path", shape: entryShape{join: 30, approach: 150, alt: true}, path: []string{"dest-alt"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var pods []entryPod
			for _, approach := range []int{1, 2} {
				for index, distance := range []float64{120, 75, 30} {
					pod := entryPod{approach: approach, lane: "road", distance: distance, berth: index + 1}
					if test.path != nil {
						pod.berth, pod.path = 2, test.path
					}
					pods = append(pods, pod)
				}
			}
			s := restoreEntry(t, entryNetwork(test.shape, 8), pods)
			if lane := s.vehicles[0].Route[2]; lane.StationRole != test.role {
				t.Fatalf("the lane %s after the entry lane has the role %q, want %q", lane.ID, lane.StationRole, test.role)
			}
			watch := newEntryWatch(s)
			watch.until(t, s, "the arrival of each pod", func() bool { return s.completed == len(s.vehicles) })
			if watch.runs.shared == 0 {
				t.Fatalf("entry runs %+v", watch.runs)
			}
		})
	}
}

// TestPlatoonEntryLanePassThrough queues four pods on each approach that
// pass the station "dest" to the origin. Their runs never hold the entry
// lane of "dest", which is not their destination station. The test ends
// when the pods leave the station, before the berth lanes of the origin,
// which are closer than Clearance at their start.
func TestPlatoonEntryLanePassThrough(t *testing.T) {
	t.Parallel()
	pods := entryQueues(4, 1, 2)
	for index := range pods {
		pods[index].through, pods[index].berth = true, 0
	}
	s := restoreEntry(t, entryNetwork(londonEntry, 8), pods)
	watch := newEntryWatch(s)
	watch.until(t, s, "the exit of each pod", func() bool {
		return !slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.Pod.LaneID != "return" })
	})
	if len(watch.monitor.platoon.linked) == 0 || watch.runs.runs != 0 {
		t.Fatalf("%d linked pods, entry runs %+v", len(watch.monitor.platoon.linked), watch.runs)
	}
}

// entryRunStart steps s until a follower holds a cell of the entry lane of
// another pod, and returns the follower.
func entryRunStart(t *testing.T, s *Simulation, watch *entryWatch) *vehicle {
	t.Helper()
	var follower *vehicle
	watch.until(t, s, "an entry run", func() bool {
		for i := range s.vehicles {
			v := &s.vehicles[i]
			for r := range v.routeReleases {
				if owner := s.owners[r]; v.link.leader != 0 && r.kind == trackResource && r.id == "dest-access-in" && !owner.isZero() && !owner.isPod(v.Pod.ID) {
					follower = v
					return true
				}
			}
		}
		return false
	})
	return follower
}

// TestPlatoonEntryLaneFault checks that a fault on a member of an entry
// run stays refused and changes nothing.
func TestPlatoonEntryLaneFault(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(londonEntry, 4), entryQueues(4, 1))
	s.faultsOn = true
	watch := newEntryWatch(s)
	follower := entryRunStart(t, s, watch)
	leader := &s.vehicles[follower.link.leader-1]
	// sameState cannot compare the hook of the monitor, and Clone does not
	// copy the work storage of formPlatoons.
	hook := s.coastCheck
	s.coastCheck = nil
	for _, v := range []*vehicle{follower, leader} {
		before := s.Clone()
		before.platoonOrder, before.platoonAhead, before.platoonLanes = s.platoonOrder, s.platoonAhead, s.platoonLanes
		if _, err := s.startPodFault(v, 0); !errors.Is(err, errFaultTarget) {
			t.Fatalf("fault on %s: error %v, want %v", v.Pod.ID, err, errFaultTarget)
		}
		if !sameState(before, s) {
			t.Fatal("the refused fault changed the state")
		}
	}
	s.coastCheck = hook
	watch.until(t, s, "the arrival of each pod", func() bool { return s.completed == len(s.vehicles) })
}

// TestPlatoonEntryLaneEmergency starts an emergency on the follower of an
// entry run. Each link of the pod drains at each tick and does not grow,
// no link with the pod forms again, and every pod arrives.
func TestPlatoonEntryLaneEmergency(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(londonEntry, 4), entryQueues(4, 1))
	s.emergenciesOn = true
	watch := newEntryWatch(s)
	v := entryRunStart(t, s, watch)
	lanes, leader, follower := v.link.lanes, v.link.leader, v.follower
	startEmergency(t, s, v, 0)
	watch.step(t, s)
	watch.until(t, s, "the arrival of each pod", func() bool {
		if v.link.leader != 0 && (v.link.leader != leader || !v.link.draining || v.link.lanes != lanes) {
			t.Fatalf("tick %d: the link of the emergency pod does not drain: %+v", s.tick, v.link)
		}
		if v.follower != 0 && (v.follower != follower || !s.vehicles[v.follower-1].link.draining) {
			t.Fatalf("tick %d: the link of the follower of the emergency pod does not drain", s.tick)
		}
		if v.link.leader == 0 {
			leader = -1
		}
		if v.follower == 0 {
			follower = -1
		}
		return arrived(s)
	})
}

// TestPlatoonEntryLaneRestore saves the run of TestPlatoonEntryLaneRun when
// a follower first holds a cell of the entry lane of another pod. The
// restore keeps each link and its end block. Two restores continue
// identically for 600 ticks, and a clone at the save tick continues
// identically to the live run. A restored run then passes every monitor at
// each tick, and every trip completes.
func TestPlatoonEntryLaneRestore(t *testing.T) {
	t.Parallel()
	network := entryNetwork(londonEntry, 8)
	s := restoreEntry(t, network, entryQueues(4, 1, 2))
	watch := newEntryWatch(s)
	entryRunStart(t, s, watch)
	clone := s.Clone()
	clone.coastCheck = nil
	restore := func() *Simulation {
		restored := roundTrip(t, network, s)
		if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
		for i := range s.vehicles {
			live, v := &s.vehicles[i], &restored.vehicles[i]
			if live.link.leader == 0 {
				continue
			}
			before, after := live.blocks.at(live.link.end), v.blocks.at(v.link.end)
			if before.lane.ID != after.lane.ID || before.cell != after.cell {
				t.Fatalf("pod %s ends its run at cell %d of %s after the restore, want cell %d of %s", v.Pod.ID, after.cell, after.lane.ID, before.cell, before.lane.ID)
			}
		}
		return restored
	}
	first, second := restore(), restore()
	for range 600 {
		watch.step(t, s)
		clone.Step()
		first.Step()
		second.Step()
		if got, want := entryState(clone), entryState(s); got != want {
			t.Fatalf("tick %d: the clone has %s, the live run %s", s.tick, got, want)
		}
		if got, want := entryState(second), entryState(first); got != want {
			t.Fatalf("tick %d: the second restore has %s, the first %s", first.tick, got, want)
		}
	}
	restored := restore()
	runs := newEntryWatch(restored)
	runs.until(t, restored, "the arrival of each pod", func() bool { return restored.completed == len(restored.vehicles) })
}

// holdEntry puts debris on the meter of the entry lane from the meter
// from for seconds, and steps s with the monitors of watch until the last tick of
// the debris. Debris at the start of the entry lane blocks each request
// for the diverge zone.
func holdEntry(t *testing.T, s *Simulation, watch *entryWatch, from float64, seconds int64) {
	t.Helper()
	s.faultsOn = true
	to := from + 1
	if _, err := s.Fault(FaultRequest{LaneID: "dest-access-in", FromMeters: &from, ToMeters: &to, DurationSeconds: &seconds}); err != nil {
		t.Fatal(err)
	}
	for end := s.tick + seconds*TicksPerSecond - 1; s.tick < end; {
		watch.step(t, s)
	}
	if len(s.faults) != 1 {
		t.Fatalf("tick %d: %d faults, want the debris", s.tick, len(s.faults))
	}
}

// waitOrder returns the IDs of the pods that wait for the diverge junction
// in admission order of aged requests: by WaitSince, then by ID.
func waitOrder(s *Simulation) []string {
	var waiting []intent
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Traveling && v.pending >= 0 && v.Pod.WaitReason == BlockedByIncident {
			waiting = append(waiting, intent{since: v.waitSince, id: v.Pod.ID})
		}
	}
	slices.SortFunc(waiting, func(a, b intent) int { return compareAdmission(a, b, math.MaxInt64) })
	ids := make([]string, len(waiting))
	for index, in := range waiting {
		ids[index] = in.id
	}
	return ids
}

// TestPlatoonEntryFairness checks the free grants of the diverge junction
// after debris blocks the diverge zone for 60 seconds. One pod waits on each of
// three approaches, and the pods arrive in the reverse order of their IDs.
// After the hold, each head is aged, and the heads take the junction in
// the order of their WaitSince. Two pods at the same position on the two
// mirrored approaches wait from the same tick, and the pod with the lower
// ID takes the junction first.
func TestPlatoonEntryFairness(t *testing.T) {
	t.Parallel()
	shape := entryShape{join: 30, approach: 150, third: true}
	for _, test := range []struct {
		name string
		pods []entryPod
		want []string
	}{
		{name: "three approaches", pods: []entryPod{{approach: 1, distance: 1100}, {approach: 2, distance: 1200}, {approach: 3, distance: 1250}}, want: []string{"p03", "p02", "p01"}},
		{name: "tie", pods: []entryPod{{approach: 3, distance: 1200}, {approach: 2, distance: 1200}}, want: []string{"p01", "p02"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := restoreEntry(t, entryNetwork(shape, len(test.pods)), test.pods)
			watch := newEntryWatch(s)
			holdEntry(t, s, watch, 1, 60)
			order := waitOrder(s)
			since := make(map[int64]bool)
			for i := range s.vehicles {
				since[s.vehicles[i].waitSince] = true
			}
			if !slices.Equal(order, test.want) || test.name == "tie" && len(since) != 1 {
				t.Fatalf("the pods wait in the order %v, want %v, with WaitSince %v", order, test.want, since)
			}
			watch.until(t, s, "the arrival of each pod", func() bool { return s.completed == len(s.vehicles) })
			if !slices.Equal(watch.grants.order, test.want) || len(watch.grants.aged) != len(test.want) || slices.Contains(slices.Collect(maps.Values(watch.grants.aged)), false) {
				t.Fatalf("free grants %v, aged %v, want %v, all aged", watch.grants.order, watch.grants.aged, test.want)
			}
		})
	}
}

// TestPlatoonEntryFairnessHeldEntry queues three pods with berths on each
// of three approaches, and debris before the station entry node blocks
// the entry lane for 60 seconds. The queues and their links wait, and
// the monitor of free grants checks each free grant of the diverge
// junction. After the debris clears, the first free grant goes to the
// oldest aged head.
func TestPlatoonEntryFairnessHeldEntry(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(entryShape{join: 30, approach: 150, third: true}, 9), entryQueues(3, 1, 2, 3))
	watch := newEntryWatch(s)
	holdEntry(t, s, watch, 130, 60)
	aged := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.pending < 0 || s.tick+1-v.waitSince < admissionAgeTicks {
			continue
		}
		for resources := range v.blocks.spanResources(v.pending, reservationEnd(&v.blocks, v.pending)+1) {
			if slices.Contains(resources, divergeJunctionResource) {
				aged++
			}
		}
	}
	held := len(watch.grants.order)
	watch.until(t, s, "the arrival of each pod", func() bool { return s.completed == len(s.vehicles) })
	if after := watch.grants.order[held:]; aged < 2 || len(after) < 2 || !watch.grants.aged[after[0]] {
		t.Fatalf("%d aged heads at the end of the debris, then the free grants %v, aged %v", aged, after, watch.grants.aged)
	}
}

// TestPlatoonEntryBatch checks the passages of a platoon before an aged
// head on another approach. Eight pods with berths go to the station on
// the first approach, and pod p09 waits on the second approach. Debris on
// the entry lane holds the diverge for 60 seconds, so the heads are aged.
// The first pod of the first approach waits from an earlier tick or has
// the lower ID, so it takes the free diverge first, and its platoon follows with linked
// grants. In "batch", the pods of the first approach queue from the start
// and link in two platoons of platoonLimit pods. In "stream", two pods
// wait on a long approach, and the other six start further back. Platoons
// are off for the first 20 seconds, so the six do not link at rest. In
// both, p09 takes the diverge after platoonLimit
// passages. A new pod links only to a pod ahead on its lane, and a member
// counts in the platoon size until its link drains on the entry lane, so
// the platoon has no room for a new pod on the approach while it owns the
// diverge. This fixture thus does not show a finite stream that passes
// more than platoonLimit pods before p09.
func TestPlatoonEntryBatch(t *testing.T) {
	t.Parallel()
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "batch", true: "stream"}[stream], func(t *testing.T) {
			t.Parallel()
			shape, pods := londonEntry, append(entryQueues(8, 1), entryPod{approach: 2, distance: 900, berth: 3})
			if stream {
				shape = entryShape{join: 30, approach: 600}
				pods = []entryPod{{approach: 1, lane: "road", distance: 560, berth: 1}, {approach: 1, lane: "road", distance: 510, berth: 2}}
				for k := range 6 {
					pods = append(pods, entryPod{approach: 1, distance: 1120 - 70*float64(k), berth: k%3 + 1})
				}
				pods = append(pods, entryPod{approach: 2, lane: "road", distance: 560, berth: 3})
			}
			s := restoreEntry(t, entryNetwork(shape, len(pods)), pods)
			if stream {
				if err := s.SetPlatooning(PlatooningOff); err != nil {
					t.Fatal(err)
				}
			}
			watch := newEntryWatch(s)
			watch.until(t, s, "the start of the hold", func() bool { return true })
			s.faultsOn = true
			from, to, seconds := 1.0, 2.0, int64(60)
			if _, err := s.Fault(FaultRequest{LaneID: "dest-access-in", FromMeters: &from, ToMeters: &to, DurationSeconds: &seconds}); err != nil {
				t.Fatal(err)
			}
			watch.until(t, s, "the arrival of each pod", func() bool {
				if stream && s.tick == 20*TicksPerSecond {
					if err := s.SetPlatooning(PlatooningVirtual); err != nil {
						t.Fatal(err)
					}
				}
				return s.completed == len(s.vehicles)
			})
			if index := slices.Index(watch.passed, "p09"); index != s.platoonLimit || !watch.grants.aged["p09"] || !slices.Equal(watch.grants.order[:2], []string{"p01", "p09"}) {
				t.Fatalf("passages %v, free grants %v, aged %v, want p09 after %d passages, aged, at the second free grant", watch.passed, watch.grants.order, watch.grants.aged, s.platoonLimit)
			}
		})
	}
}

// TestPlatoonEntryRestorePins restores a saved link of pod p02 to pod p01
// on the first approach, or on the second approach with a turn of 30
// degrees at the diverge. The restore checks the lanes of the run in two
// passes. The first pass refuses a lane that is not on both routes, a
// berth access lane, and a lane from an entry node of the destination
// station of either pod. Only then does the second pass refuse the entry
// lane of a station that is not the destination station of both pods, and
// only then does the restore check the turn. A refused link sends the
// restore to the logical tier with the text of the first refusal.
func TestPlatoonEntryRestorePins(t *testing.T) {
	t.Parallel()
	const (
		route   = "pod p02: the platoon run is not on both routes"
		station = "pod p02: the platoon run holds the entry lane of another station"
		turn    = "pod p02: the platoon run turns more than 0"
	)
	dest := entryPod{berth: 1}
	through := entryPod{through: true}
	// loop has no berth and goes past the station entry and around to the
	// entry lane again.
	loop := entryPod{path: []string{"dest-through", "dest-access-out", "return", "origin-through", "feed-1", "u1", "dest-road-in-01", "dest-access-in"}}
	for _, test := range []struct {
		name             string
		leader, follower entryPod
		approach, lanes  int
		// zero saves the turn 0 in place of the turn of the run.
		zero bool
		want string
	}{
		{name: "accept", leader: dest, follower: entryPod{berth: 2}, lanes: 3},
		{name: "foreign for the leader", leader: through, follower: dest, lanes: 3, want: station},
		{name: "foreign for the follower", leader: dest, follower: through, lanes: 3, want: station},
		{name: "other routes after a foreign entry lane", leader: through, follower: dest, lanes: 4, want: route},
		{name: "entry node after a foreign entry lane", leader: through, follower: loop, lanes: 4, want: route},
		{name: "turn", leader: dest, follower: entryPod{berth: 2}, approach: 1, lanes: 3, zero: true, want: turn},
		{name: "foreign entry lane before the turn", leader: through, follower: dest, approach: 1, lanes: 3, zero: true, want: station},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := entryNetwork(londonEntry, 2)
			leader, follower := test.leader, test.follower
			leader.approach, leader.distance = 1+test.approach, 1250
			follower.approach, follower.distance = 1+test.approach, 1205
			state, fleet := savedEntry(network, 0, []entryPod{leader, follower})
			var ids []string
			for _, index := range state.Pods[1].Route {
				ids = append(ids, network.Lanes[index].ID)
			}
			link := savedRunLink(network, ids, ids, "p01", test.lanes)
			if test.zero {
				link.Turn = 0
			}
			state.Pods[1].Platoon = link
			s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if result.Tier != RestorePhysical || len(result.Demoted) != 0 || s.vehicles[1].link.leader != 1 || !holdsEntryLane(&s.vehicles[1]) {
					t.Fatalf("restore %+v, link %+v", result, s.vehicles[1].link)
				}
				return
			}
			if result.Tier != RestoreLogical || result.PhysicalError == nil || result.PhysicalError.Error() != test.want {
				t.Fatalf("tier %s, physical error %q, want %q", result.Tier, result.PhysicalError, test.want)
			}
		})
	}
}

// TestPlatoonEntryLanePickupSwap restores pods p01 and p02 empty on the
// first approach, on their way to parties at the station "dest". Pod p03
// boards a party for the origin at the berth "dest-03". After the first
// step, the run of the link of p02 to p01 holds the entry lane. Then both
// parties join p03, and dispatch releases p01 and p02. The released pods
// keep their routes and their link, p02 shares cells of the entry lane,
// and the pods stop at their berths.
func TestPlatoonEntryLanePickupSwap(t *testing.T) {
	t.Parallel()
	network := entryNetwork(londonEntry, 3)
	lanes := entryLaneIndexes(network)
	state, fleet := savedEntry(network, restoreTick, []entryPod{{approach: 1, distance: 1250, berth: 1}, {approach: 1, distance: 1205, berth: 2}})
	state.RequestID, state.Boarded, state.SharedRidePartyLimit = 3, 1, 4
	state.Waiting = nil
	for index := range 2 {
		pod := &state.Pods[index]
		pod.Occupied, pod.Riders, pod.Stops, pod.RelocatingTo = false, nil, nil, "dest"
		state.Waiting = append(state.Waiting, SavedTrip{Request: SavedRequest{
			SharingConsent: SharedConsent, Service: OnDemandService, ID: index + 2, From: "dest", To: "origin", PartySize: 1, PodID: pod.ID, RequestedTick: restoreTick - 10,
		}})
	}
	fleet = append(fleet, Placement{ID: "p03", StationID: "dest", BerthID: "dest-03"})
	var route []int
	for _, id := range []string{"dest-03-out", "dest-03-departure-link", "dest-02-departure-link", "dest-01-departure-link", "dest-access-out", "return"} {
		route = append(route, lanes[id])
	}
	state.Pods = append(state.Pods, SavedPod{
		ID: "p03", Activity: activityCode(Boarding), StationID: "dest", BerthID: "dest-03", PhaseTicks: boardingTicks,
		Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "dest", To: "origin", PartySize: 1, PodID: "p03", RequestedTick: 10, BoardedTick: 20}},
		Stops:  []string{"origin"}, Origin: "dest-03", DestinationStation: "origin", Route: route,
	})
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: roundTripState(t, state)})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("restore: %v, %+v", err, result)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	watch := newEntryWatch(s)
	watch.step(t, s)
	leader, follower, host := s.findVehicle("p01"), s.findVehicle("p02"), s.findVehicle("p03")
	if follower.link.leader != 1 || !holdsEntryLane(follower) || len(s.waiting) != 2 || host.Pod.Activity != Boarding {
		t.Fatalf("link %+v, waiting %+v, p03 %+v", follower.link, s.waiting, host.Vehicle)
	}
	routes := [][]Lane{slices.Clone(leader.Route), slices.Clone(follower.Route)}
	if err := s.SetSharedRideJoin(SharedRideJoinReassignExisting); err != nil {
		t.Fatal(err)
	}
	watch.until(t, s, "the join", func() bool { return len(s.waiting) == 0 || host.Pod.Activity != Boarding })
	if len(s.waiting) != 0 || len(host.Riders) != 3 || follower.link.leader != 1 || !holdsEntryLane(follower) {
		t.Fatalf("the parties did not join p03: waiting %+v, p03 %+v, link %+v", s.waiting, host.Vehicle, follower.link)
	}
	for index, v := range []*vehicle{leader, follower} {
		if !v.released || !slices.Equal(v.Route, routes[index]) {
			t.Fatalf("pod %s changed its route or is not released: %+v, released %v", v.Pod.ID, v.Vehicle, v.released)
		}
	}
	watch.until(t, s, "the stop of the released pods", func() bool { return leader.Pod.Activity == Idle && follower.Pod.Activity == Idle })
	if leader.Pod.BerthID != "dest-01" || follower.Pod.BerthID != "dest-02" || watch.runs.shared == 0 {
		t.Fatalf("the released pods stopped at %s and %s, entry runs %+v", leader.Pod.BerthID, follower.Pod.BerthID, watch.runs)
	}
}
