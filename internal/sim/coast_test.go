package sim

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestCoastMoveStep checks the motion kernel of a coasting pod. With an
// infinite ceiling it is ordinaryMoveStep bit for bit. With the ceiling
// keepSpeed(speed), the stop point does not move. A ceiling stopSpeed(x)
// that binds puts the stop point x ahead of the pod.
func TestCoastMoveStep(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(londonEntry, 1), approachQueue(1, 1, 200, 0))
	blocks := &s.vehicles[0].blocks
	for _, test := range []struct{ distance, speed, limit float64 }{
		{100, 0, 1000}, {100, 3.7, 1000}, {100, 14, 1000}, {100, 9.25, 131.7}, {100, 14, 100.2}, {1349.5, 6, 1400},
	} {
		t.Run(fmt.Sprint(test), func(t *testing.T) {
			t.Parallel()
			if got, want := coastMoveStep(blocks, 0, test.distance, test.speed, test.limit, math.Inf(1)), ordinaryMoveStep(blocks, 0, test.distance, test.speed, test.limit); got != want {
				t.Fatalf("infinite ceiling: %+v, want %+v", got, want)
			}
			stop := test.distance + stoppingDistance(test.speed)
			if stop+1 < test.limit {
				got := coastMoveStep(blocks, 0, test.distance, test.speed, test.limit, keepSpeed(test.speed))
				if after := got.distance + stoppingDistance(got.speed); math.Abs(after-stop) > 1e-9 || test.speed-got.speed > maxSpeedStep {
					t.Fatalf("keep: stop point %v at %v m/s, want %v", after, got.speed, stop)
				}
			}
			// The ceiling binds when it is less than speed + acceleration ×
			// dt, as for a pod that holds back to a hold point.
			if gap := stoppingDistance(test.speed) + 0.05; stopSpeed(gap) < test.speed+maxSpeedStep && test.distance+gap+1 < test.limit {
				got := coastMoveStep(blocks, 0, test.distance, test.speed, test.limit, stopSpeed(gap))
				if after := got.distance + stoppingDistance(got.speed); math.Abs(after-(test.distance+gap)) > 1e-9 {
					t.Fatalf("stop: stop point %v, want %v", after, test.distance+gap)
				}
			}
		})
	}
}

// runEntry steps s with the entry monitor for up to 600 seconds, until
// the trip of each pod completes. It returns the order in which the pods
// took the diverge junction.
func runEntry(t *testing.T, s *Simulation, monitor *entryMonitor) []string {
	t.Helper()
	count := len(s.vehicles)
	var order divergeOrder
	for range 600 * TicksPerSecond {
		s.Step()
		monitor.check(t)
		order.record(s)
		if s.completed == count {
			return order.order
		}
	}
	t.Fatalf("tick %d: %d trips complete, want %d", s.tick, s.completed, count)
	return nil
}

// TestCoastTwoApproaches queues four pods 100 m apart on each approach.
// The pods choose their berths early and link, and the platoons on the
// two approaches meet at the diverge. Heads that
// wait for the diverge coast, also two or more at one tick, and the
// monitor checks each coast rule at each tick. Coast changes no grant:
// the pods take the diverge junction in the order of a run with no coast.
func TestCoastTwoApproaches(t *testing.T) {
	t.Parallel()
	run := func(coast bool) ([]string, *entryMonitor) {
		s := twoApproaches(t)
		monitor := newEntryMonitor(s)
		monitor.off = !coast
		return runEntry(t, s, monitor), monitor
	}
	order, monitor := run(true)
	if monitor.coasted == 0 || monitor.pairs == 0 {
		t.Fatalf("%d ticks of coasting pods, %d ticks with two or more", monitor.coasted, monitor.pairs)
	}
	if base, _ := run(false); !reflect.DeepEqual(order, base) {
		t.Fatalf("diverge order %v, want %v of a run with no coast", order, base)
	}
}

// twoApproaches restores four pods 100 m apart on each approach of the
// London shape.
func twoApproaches(t *testing.T) *Simulation {
	t.Helper()
	return restoreEntry(t, entryNetwork(londonEntry, 8), append(approachQueue(1, 4, 1250, 100), approachQueue(2, 4, 1250, 100)...))
}

// hasCeiling reports whether results has a ceiling for the pod at index.
func hasCeiling(results []coastResult, index int) bool {
	for _, result := range results {
		if result.index == index {
			return true
		}
	}
	return false
}

// TestCoastExemptions checks the pods and the settings with no coast. With
// platoons off or with one approach, no pod coasts. A pod of a large class
// and a pod with an emergency record do not coast, also when the same pod
// coasts as a small pod with no record.
func TestCoastExemptions(t *testing.T) {
	t.Parallel()
	t.Run("platoons off", func(t *testing.T) {
		t.Parallel()
		s := twoApproaches(t)
		if err := s.SetPlatooning(PlatooningOff); err != nil {
			t.Fatal(err)
		}
		monitor := newEntryMonitor(s)
		runEntry(t, s, monitor)
		if monitor.coasted != 0 {
			t.Fatalf("%d ticks of coasting pods with platoons off", monitor.coasted)
		}
	})
	t.Run("one approach", func(t *testing.T) {
		t.Parallel()
		s := restoreEntry(t, entryNetwork(londonEntry, 8), approachQueue(1, 8, 1250, 45))
		monitor := newEntryMonitor(s)
		runEntry(t, s, monitor)
		if monitor.coasted != 0 {
			t.Fatalf("%d ticks of coasting pods with one approach", monitor.coasted)
		}
	})
	t.Run("large class and emergency", func(t *testing.T) {
		t.Parallel()
		s := twoApproaches(t)
		monitor := newEntryMonitor(s)
		checked := 0
		monitor.extra = func(s *Simulation, denied []deniedRequest, results []coastResult) {
			for _, result := range results {
				v := &s.vehicles[result.index]
				class := v.Pod.Class
				v.Pod.Class = GroupClass
				large := hasCeiling(s.coastCeilings(denied), result.index)
				v.Pod.Class = class
				s.emergencies = append(s.emergencies, emergencyRecord{pod: result.index})
				emergency := hasCeiling(s.coastCeilings(denied), result.index)
				s.emergencies = s.emergencies[:len(s.emergencies)-1]
				if large || emergency {
					panic(fmt.Sprintf("tick %d: pod %s coasts as a large pod (%v) or with an emergency record (%v)", s.tick, v.Pod.ID, large, emergency))
				}
				checked++
			}
		}
		runEntry(t, s, monitor)
		if checked == 0 {
			t.Fatal("no pod coasted")
		}
	})
}

// TestCoastReleaser checks the predicted releaser of a resource. The owner
// releases it unless a pod behind it in its platoon retains it past its
// position. Then the last such pod releases it. An owner that does not
// retain the resource, and an owner that is not a pod, give no releaser.
func TestCoastReleaser(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(londonEntry, 3), approachQueue(1, 3, 1250, 45))
	owner, second, third := &s.vehicles[0], &s.vehicles[1], &s.vehicles[2]
	owner.follower, second.follower = 2, 3
	r := resource{kind: trackResource, id: "dest-access-in"}
	s.owners[r] = podResourceOwner(owner.Pod.ID)
	if p := s.coastReleaser(s.owners[r], r); p != nil {
		t.Fatalf("an owner that does not retain the resource gives releaser %s", p.Pod.ID)
	}
	owner.retainRouteResource(r, owner.distance+400)
	second.retainRouteResource(r, second.distance+300)
	third.retainRouteResource(r, third.distance)
	if p := s.coastReleaser(s.owners[r], r); p != second {
		t.Fatalf("releaser %v, want the second pod", p)
	}
	third.routeReleases[r] = third.distance + 200
	if p := s.coastReleaser(s.owners[r], r); p != third {
		t.Fatalf("releaser %v, want the third pod", p)
	}
	if p := s.coastReleaser(resourceOwner{kind: faultOwnerKind, id: "fault"}, r); p != nil {
		t.Fatalf("a fault owner gives releaser %s", p.Pod.ID)
	}
}

// TestCoastUpstreamGate checks the bounds of the upstream gate on the
// frozen state of a coasting pod. A node or junction resource that the pod
// retains to a release distance after its position and at most its
// reservation end stops coast. A release distance at the position, which
// the pod passed, and one after the reservation end do not.
func TestCoastUpstreamGate(t *testing.T) {
	t.Parallel()
	s := twoApproaches(t)
	monitor := newEntryMonitor(s)
	checked := 0
	monitor.extra = func(s *Simulation, denied []deniedRequest, results []coastResult) {
		for _, result := range results {
			v := &s.vehicles[result.index]
			frontier := v.blocks.end(v.reservedThrough)
			if frontier-v.distance < 1e-3 {
				continue
			}
			for _, test := range []struct {
				at    float64
				coast bool
			}{{v.distance, true}, {math.Nextafter(v.distance, frontier), false}, {frontier, false}, {math.Nextafter(frontier, math.Inf(1)), true}} {
				for _, kind := range []resourceKind{nodeResource, junctionResource} {
					r := resource{kind: kind, id: "gate"}
					v.routeReleases[r] = test.at
					got := hasCeiling(s.coastCeilings(denied), result.index)
					delete(v.routeReleases, r)
					if got != test.coast {
						panic(fmt.Sprintf("tick %d: pod %s at %v with reservation end %v retains %v to %v: coast %v, want %v",
							s.tick, v.Pod.ID, v.distance, frontier, r, test.at, got, test.coast))
					}
				}
			}
			checked++
		}
	}
	runEntry(t, s, monitor)
	if checked == 0 {
		t.Fatal("no coasting pod had room before its reservation end")
	}
}

// TestCoastQueuedFollowerCrossing queues sixteen pods on approach 2, so
// the queue behind a coasting head reaches back over the node p2, where a
// crossing lane passes. Pods on the crossing lane wait for the queued pod
// that holds p2. Coast does not bound that wait. The test checks only
// that each pod makes progress: every trip completes, and every crossing
// pod passes p2. The approaches are 150 m long, or 36 m as the approach
// "road-in-02" of KSX, where the diverge zone covers the whole approach.
func TestCoastQueuedFollowerCrossing(t *testing.T) {
	t.Parallel()
	for _, approach := range []float64{150, 36} {
		t.Run(fmt.Sprint(approach), func(t *testing.T) {
			t.Parallel()
			const queued = 20
			pods := append(approachQueue(2, 16, 1200, 45), approachQueue(1, 4, 1200, 45)...)
			pods = append(pods, entryPod{distance: 260}, entryPod{distance: 200}, entryPod{distance: 140})
			s := restoreEntry(t, entryNetwork(entryShape{join: 30, approach: approach, crossing: true}, len(pods)), pods)
			monitor := newEntryMonitor(s)
			blocked := 0
			for range 1200 * TicksPerSecond {
				s.Step()
				monitor.check(t)
				crossed := true
				for _, v := range s.vehicles[queued:] {
					if v.Pod.LaneID == "x-in" {
						crossed = false
						if v.Pod.WaitReason != NoWait && slices.ContainsFunc(s.vehicles[:queued], func(u vehicle) bool { return u.Pod.ID == v.Pod.BlockedBy }) {
							blocked++
						}
					}
				}
				if s.completed == queued && crossed {
					// On the short approach, the crossing pods need not meet
					// a queued pod at p2, so only the long approach must make
					// them wait.
					if monitor.coasted == 0 || approach == 150 && blocked == 0 {
						t.Fatalf("%d ticks of coasting pods, %d ticks of crossing pods that wait for a queued pod", monitor.coasted, blocked)
					}
					return
				}
			}
			t.Fatalf("%d trips complete", s.completed)
		})
	}
}

// entryState returns the motion state of each pod and the owners of s.
func entryState(s *Simulation) string {
	var state strings.Builder
	for i := range s.vehicles {
		v := &s.vehicles[i]
		fmt.Fprintf(&state, "%s %s %v %v %v %d %v|", v.Pod.ID, v.Pod.Activity, v.distance, v.Pod.Speed, v.Pod.Position, v.reservedThrough, v.link)
	}
	keys := slices.SortedFunc(maps.Keys(s.owners), func(a, b resource) int {
		return cmp.Or(cmp.Compare(a.kind, b.kind), cmp.Compare(a.id, b.id), cmp.Compare(a.cell, b.cell))
	})
	for _, r := range keys {
		fmt.Fprintf(&state, "%v=%v ", r, s.owners[r])
	}
	return state.String()
}

// TestCoastRestoreAndClone saves the two-approach run at a tick when two
// heads coast. Two restores of the save continue identically for 600
// ticks. A clone at the save tick continues identically to the live run.
// One restored run then passes every monitor at each tick, and every trip
// completes.
func TestCoastRestoreAndClone(t *testing.T) {
	t.Parallel()
	network := entryNetwork(londonEntry, 8)
	s := twoApproaches(t)
	monitor := newEntryMonitor(s)
	for monitor.pairs == 0 {
		s.Step()
		monitor.check(t)
		if s.tick > 300*TicksPerSecond {
			t.Fatal("no two heads coasted")
		}
	}
	saved := s.ExportState()
	clone := s.Clone()
	clone.coastCheck = nil
	restore := func() *Simulation {
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: saved})
		if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
			t.Fatalf("restore %+v: %v", result, err)
		}
		if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
		return restored
	}
	first, second := restore(), restore()
	for range 600 {
		s.Step()
		monitor.check(t)
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
	restored := newEntryMonitor(first)
	runEntry(t, first, restored)
}

// TestCoastHeldFollower queues six linked pods 45 m apart on each of two
// approaches. A platoon member that holdsPending refuses at the diverge
// waits for the diverge zone as a head does, so it coasts against the
// heads of the other approach. At least one such pod stops at its hold
// point, 10 m or more before the end of its reservation, and then takes
// the diverge junction at 2 m/s or more.
func TestCoastHeldFollower(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(londonEntry, 12), append(approachQueue(1, 6, 1250, 45), approachQueue(2, 6, 1250, 45)...))
	monitor := newEntryMonitor(s)
	diverge := resource{kind: junctionResource, id: "dest-diverge"}
	// hold is the largest hold at rest of each held follower, and grant
	// is its speed when it takes the diverge after that hold.
	hold := make(map[string]float64)
	grant := make(map[string]float64)
	var owner resourceOwner
	monitor.extra = func(s *Simulation, _ []deniedRequest, results []coastResult) {
		if next := s.owners[diverge]; next != owner {
			owner = next
			if v := s.ownerVehicle(next); v != nil {
				if _, ok := hold[v.Pod.ID]; ok {
					grant[v.Pod.ID] = v.Pod.Speed
				}
			}
		}
		for _, result := range results {
			if v := &s.vehicles[result.index]; v.link.leader != 0 && s.holdsPending(v) && v.Pod.Speed == 0 {
				hold[v.Pod.ID] = max(hold[v.Pod.ID], v.blocks.end(v.reservedThrough)-v.distance)
			}
		}
	}
	runEntry(t, s, monitor)
	t.Logf("held followers: holds %v, diverge grant speeds %v", hold, grant)
	for id, meters := range hold {
		if speed, ok := grant[id]; ok && meters >= 10 && speed >= 2 {
			return
		}
	}
	t.Fatalf("no held follower held 10 m or more and then took the diverge at 2 m/s or more: holds %v, grant speeds %v", hold, grant)
}
