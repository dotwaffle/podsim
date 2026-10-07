package sim

import (
	"maps"
	"testing"
)

// entryQueueNetwork returns network with each position scaled by scale and
// with market-approach as the station entry lane of Market, so that a
// queue of pods can wait on it.
func entryQueueNetwork(network Network, scale float64) Network {
	for index := range network.Nodes {
		network.Nodes[index].Position.X *= scale
		network.Nodes[index].Position.Y *= scale
	}
	for index := range network.Lanes {
		if network.Lanes[index].ID == "market-approach" {
			network.Lanes[index].StationRole = StationEntryRole
		}
	}
	return network
}

// surrenderQueue returns a simulation with faults on and a queue on the
// entry lane of Market. Pod 02 carries riders and is the head. Pod 01 is
// an empty pickup pod behind it. An external owner holds the only berth
// and the first cell of market-in. The caller releases both with release.
func surrenderQueue(t *testing.T) (s *Simulation, head, behind *vehicle, release func()) {
	t.Helper()
	s, err := NewFleet(entryQueueNetwork(Example(), 1), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	s.faultsOn = true
	barriers := []resource{{kind: berthResource, id: "market-1"}, {kind: trackResource, id: "market-in"}}
	for _, barrier := range barriers {
		s.owners[barrier] = podResourceOwner("external")
	}
	if err := s.RequestJourney("02", "market"); err != nil {
		t.Fatal(err)
	}
	head, behind = s.findVehicle("02"), s.findVehicle("01")
	stepUntil(t, s, "the head waits at the entry", func() bool { return head.Pod.LaneID == "market-approach" && head.Pod.Speed == 0 })
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "a queue of two at the frontier", func() bool {
		return head.Pod.Speed == 0 && behind.Pod.Speed == 0 && behind.Pod.BlockedBy == "02"
	})
	return s, head, behind, func() {
		for _, barrier := range barriers {
			delete(s.owners, barrier)
		}
	}
}

// releaseQueuedPickup takes the pickup of the pod behind away, as dispatch
// does, so that the pod is a released empty pod that parks again.
func releaseQueuedPickup(t *testing.T, s *Simulation, behind *vehicle) {
	t.Helper()
	if s.releasePickups(behind) != 1 || !behind.released {
		t.Fatal("the pod behind is not released")
	}
}

// ownsDestination reports whether v owns each resource of its destination
// berth.
func ownsDestination(s *Simulation, v *vehicle) bool {
	claims := berthResources(v.destination)
	return v.destination.ID != "" && s.owners[claims[0]].isPod(v.Pod.ID) && s.owners[claims[1]].isPod(v.Pod.ID)
}

// TestClaimSurrenderTwoPodsOneBerth checks the end of the deadlock of
// finding N1 (section 9.5 of the incident suspension contract). A waiting
// head faults before it claims a berth, is evacuated, and clears. The
// fault unbinds the pickup of the empty pod behind it. That released pod
// parks again at the only berth in each
// dispatch, and gives up that claim in the first phase of admit while it
// waits. The gate stays on through the recovery, so the head takes the
// berth after the clear. A replay from a checkpoint after a surrender is
// identical, and the state at the end of a tick with a surrender saves and
// restores.
func TestClaimSurrenderTwoPodsOneBerth(t *testing.T) {
	t.Parallel()
	s, head, behind, release := surrenderQueue(t)
	id := startFault(t, s, head, 0)
	s.Step()
	if head.op.purpose != opEmptyRecovery || head.op.owner != faultHold || head.Pod.Occupied {
		t.Fatalf("the head is not in its fault recovery: %+v", head.op)
	}
	// The fault cuts the pickup pod off from Market, so dispatch unbinds
	// its trip, and the pod parks again.
	if !behind.released || len(s.waiting) != 1 || s.waiting[0].request.PodID != "" || s.waiting[0].excludedPod != "" {
		t.Fatalf("dispatch kept the pickup: released %v, %+v", behind.released, s.waiting)
	}
	release()
	for range 3 {
		s.Step()
		checkNow(t, s)
		if behind.destination.ID != "market-1" || ownsDestination(s, behind) || behind.RelocatingTo != "market" {
			t.Fatalf("tick %d: the pod behind goes to %q and owns it %v", s.tick, behind.destination.ID, ownsDestination(s, behind))
		}
	}
	physicalSave(t, s, "end of a tick with a claim surrender")
	checkpoint := s.Clone()
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	if !s.incidentOutstanding() {
		t.Fatal("the gate is off during the fault recovery")
	}
	s.faultsOn = false
	if s.incidentOutstanding() {
		t.Fatal("the gate is on without the fault marker")
	}
	s.faultsOn = true
	stepUntil(t, s, "the head at the berth", func() bool { return head.Pod.BerthID == "market-1" })
	stepUntil(t, s, "the hold release", func() bool { return head.withdrawn == 0 })
	if s.incidentOutstanding() {
		t.Fatal("the gate is on after the hold release")
	}
	if err := checkpoint.clearFault(id); err != nil {
		t.Fatal(err)
	}
	for checkpoint.tick < s.tick {
		checkpoint.Step()
	}
	if !sameState(checkpoint, s) {
		t.Fatal("the replay differs from the source")
	}
}

// TestClaimSurrenderBehindHealthyPod checks the wait derivation of the
// claim surrender: an empty pod with an unused claim behind a healthy pod
// that waits gives up its claim, with no incident owner. Debris on a lane
// that neither pod uses turns the gate on. Without it, the gate is off,
// and the pod keeps its claim, as with faults off.
func TestClaimSurrenderBehindHealthyPod(t *testing.T) {
	t.Parallel()
	for _, debris := range []bool{false, true} {
		s, head, behind, _ := surrenderQueue(t)
		releaseQueuedPickup(t, s, behind)
		stepUntil(t, s, "a claim of the pod behind", func() bool { return ownsDestination(s, behind) })
		if !s.admissionWaits(behind) || s.claimKind(behind, berthResources(behind.destination)[0]) != claimService {
			t.Fatalf("the pod behind does not wait with a service claim: %+v", behind.Pod)
		}
		if debris {
			startDebris(t, s, "harbor-approach", 40, 44, 0)
		}
		if s.incidentOutstanding() != debris {
			t.Fatalf("the gate is %v", !debris)
		}
		destination := behind.destination
		owners := maps.Clone(s.owners)
		s.admit()
		if behind.destination != destination || behind.Pod.BlockedBy != head.Pod.ID {
			t.Fatalf("the pod behind goes to %q, blocked by %q", behind.destination.ID, behind.Pod.BlockedBy)
		}
		for _, r := range berthResources(destination) {
			if debris {
				delete(owners, r)
			}
		}
		if !maps.Equal(owners, s.owners) {
			t.Fatalf("debris %v: owners %v, want %v", debris, s.owners, owners)
		}
	}
}

// TestClaimSurrenderMarkerParity runs the queue of
// TestClaimSurrenderBehindHealthyPod with faults on and with faults off. With
// no fault, the gate is off, so the states stay equal at each tick.
func TestClaimSurrenderMarkerParity(t *testing.T) {
	t.Parallel()
	on, _, onBehind, _ := surrenderQueue(t)
	off, _, offBehind, _ := surrenderQueue(t)
	off.faultsOn = false
	releaseQueuedPickup(t, on, onBehind)
	releaseQueuedPickup(t, off, offBehind)
	waited := false
	for range 20 * TicksPerSecond {
		waited = waited || on.ownsServiceClaim(onBehind) && on.admissionWaits(onBehind)
		on.Step()
		off.Step()
		off.faultsOn = true
		same := sameState(on, off)
		off.faultsOn = false
		if !same {
			t.Fatalf("tick %d: the states differ", on.tick)
		}
	}
	if !waited {
		t.Fatal("no pod waits with a service claim")
	}
}

// TestClaimSurrenderKinds checks that the claim surrender gives up only a
// claimService resource of a healthy pod. The pod behind waits with a
// claim on its destination berth, and debris turns the gate on. An
// occupied pod has claimOther on the same berth. A faulted pod gave up its
// service claims at fault start, and the surrender skips it, so a claim
// that the test gives it again stays.
func TestClaimSurrenderKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		prepare  func(t *testing.T, s *Simulation, v *vehicle)
		kind     claimKind
		released bool
	}{
		{"service", func(*testing.T, *Simulation, *vehicle) {}, claimService, true},
		{"occupied", func(_ *testing.T, _ *Simulation, v *vehicle) { v.Pod.Occupied = true }, claimOther, false},
		{"faulted", func(t *testing.T, s *Simulation, v *vehicle) {
			t.Helper()
			startFault(t, s, v, 0)
			for _, r := range berthResources(v.destination) {
				s.owners[r] = podResourceOwner(v.Pod.ID)
			}
		}, claimService, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, _, behind, _ := surrenderQueue(t)
			releaseQueuedPickup(t, s, behind)
			stepUntil(t, s, "a claim of the pod behind", func() bool { return ownsDestination(s, behind) })
			startDebris(t, s, "harbor-approach", 40, 44, 0)
			test.prepare(t, s, behind)
			if kind := s.claimKind(behind, berthResources(behind.destination)[0]); kind != test.kind || !s.admissionWaits(behind) {
				t.Fatalf("claim kind %v, waits %v", kind, s.admissionWaits(behind))
			}
			s.surrenderWaitingClaims()
			if released := !ownsDestination(s, behind); released != test.released {
				t.Fatalf("released %v", released)
			}
		})
	}
}

// TestClaimSurrenderReadyPods checks that a pod waits only when admit
// would build a request for it: a pod that departs waits only when its
// phase timer has ended.
func TestClaimSurrenderReadyPods(t *testing.T) {
	t.Parallel()
	s, _, behind, _ := surrenderQueue(t)
	releaseQueuedPickup(t, s, behind)
	stepUntil(t, s, "a claim of the pod behind", func() bool { return ownsDestination(s, behind) })
	behind.Pod.Activity, behind.phaseTicks = DepartingEmpty, 10
	if s.admissionWaits(behind) {
		t.Fatal("a pod with a running phase timer waits")
	}
	behind.phaseTicks = 0
	if !s.admissionWaits(behind) {
		t.Fatal("a ready pod does not wait")
	}
}

// TestClaimSurrenderWritesNothing checks that the wait derivation of the
// claim surrender writes nothing.
func TestClaimSurrenderWritesNothing(t *testing.T) {
	t.Parallel()
	s, _, behind, _ := surrenderQueue(t)
	releaseQueuedPickup(t, s, behind)
	stepUntil(t, s, "a claim of the pod behind", func() bool { return ownsDestination(s, behind) })
	before := s.Clone()
	if !s.ownsServiceClaim(behind) || !s.admissionWaits(behind) || !sameState(before, s) {
		t.Fatal("the wait derivation wrote state")
	}
}
