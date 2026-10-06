package sim

import (
	"cmp"
	"maps"
	"slices"
	"testing"
)

// TestEmergencyAdmissionTier checks the emergency tier of admission
// (section 9.3 of the incident emergency contract). Two pods request one
// free junction in one tick. An emergency pod gets it before an ordinary
// pod, also before an aged request and a request with passengers. Of two
// emergency pods, the pod of the lower serial gets it. A junction that a
// pod already owns stays with its owner.
func TestEmergencyAdmissionTier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// serials holds the record serial of pods 01 and 02, or 0.
		serials [2]uint64
		// aged makes the request of pod 01 aged, and passengers puts a
		// party in pod 01.
		aged, passengers, existing bool
		want                       string
	}{
		{name: "ordinary order", want: "01"},
		{name: "emergency before ordinary", serials: [2]uint64{0, 1}, want: "02"},
		{name: "emergency before aged", serials: [2]uint64{0, 1}, aged: true, want: "02"},
		{name: "emergency before passengers", serials: [2]uint64{0, 1}, passengers: true, aged: true, want: "02"},
		{name: "aged without emergency", aged: true, passengers: true, want: "01"},
		{name: "lower serial first", serials: [2]uint64{5, 3}, aged: true, want: "02"},
		{name: "lower serial first, other pod", serials: [2]uint64{3, 5}, want: "01"},
		{name: "owner keeps its resource", serials: [2]uint64{0, 1}, existing: true, want: "01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			junction := resource{kind: junctionResource, id: "merge"}
			s := &Simulation{tick: admissionAgeTicks, owners: make(map[resource]resourceOwner), emergenciesOn: true}
			for index, id := range []string{"01", "02"} {
				v := vehicle{Pod: Pod{ID: id, Activity: Traveling}, reservedThrough: -1}
				v.blocks = blockListOf([]block{{lane: Lane{ID: id, SpeedLimit: 14}, end: 30, resources: []resource{junction}}})
				v.waitSince = s.tick
				if id == "01" && tc.aged {
					v.waitSince -= admissionAgeTicks
				}
				if id == "01" && tc.passengers {
					v.Pod.Occupied = true
					v.Riders = []Request{{PartySize: 1}}
				}
				s.vehicles = append(s.vehicles, v)
				if tc.serials[index] != 0 {
					s.emergencies = append(s.emergencies, emergencyRecord{serial: tc.serials[index], pod: index})
				}
			}
			slices.SortFunc(s.emergencies, func(a, b emergencyRecord) int { return cmp.Compare(a.serial, b.serial) })
			if tc.existing {
				s.owners[junction] = podResourceOwner("01")
				s.vehicles[0].reservedThrough = 0
			}
			owners := maps.Clone(s.owners)
			s.admit()
			if got := s.owners[junction]; got != podResourceOwner(tc.want) {
				t.Fatalf("junction owner = %q, want %q", got, tc.want)
			}
			if tc.existing && (!maps.Equal(owners, s.owners) || s.vehicles[1].reservedThrough != -1 || s.vehicles[1].Pod.BlockedBy != "01") {
				t.Fatalf("the emergency grant changed the owners %v, or reserved %d", s.owners, s.vehicles[1].reservedThrough)
			}
		})
	}
}

// TestEmergencyAdmissionOrder checks compareAdmission directly: the tier
// goes before the age rule, then the serial, then the pod ID. Without an
// emergency intent, the order is the order of the age rule.
func TestEmergencyAdmissionOrder(t *testing.T) {
	t.Parallel()
	tick := int64(admissionAgeTicks + 1)
	aged := intent{since: 0, priority: 2, id: "01"}
	young := intent{since: tick, priority: 0, id: "02"}
	emergency := intent{since: tick, priority: 2, id: "03", emergency: 7}
	earlier := intent{since: tick, priority: 2, id: "04", emergency: 6}
	same := intent{since: 0, priority: 0, id: "00", emergency: 7}
	// With equal serials, the lower pod ID wins although its request is
	// younger and its ordinary priority is worse, so the comparison does
	// not fall through to the age rule.
	lowerID := intent{since: tick, priority: 2, id: "05", emergency: 9}
	higherID := intent{since: 0, priority: 0, id: "06", emergency: 9}
	for _, pair := range [][2]intent{{emergency, aged}, {emergency, young}, {earlier, emergency}, {same, emergency}, {lowerID, higherID}, {aged, young}} {
		if compareAdmission(pair[0], pair[1], tick) >= 0 || compareAdmission(pair[1], pair[0], tick) <= 0 {
			t.Fatalf("%+v does not go before %+v", pair[0], pair[1])
		}
	}
}

// TestEmergencyClaimSurrender checks the claim surrender gate (section 9.4
// of the incident emergency contract). An empty relocation waits with an
// unused service claim behind a buffered head that carries a rider, with
// faults off. Without an emergency, the gate is off, and the pod keeps its
// claim. An emergency on the head binds the head to the berth of the
// buffer, and the gate is on: the pod behind gives up its claim in the
// first phase of admit, and it keeps its destination and its relocation.
// The head then unloads at the berth, and the gate goes off at the end.
func TestEmergencyClaimSurrender(t *testing.T) {
	t.Parallel()
	for _, emergency := range []bool{false, true} {
		s, head, behind, release := surrenderQueue(t)
		s.faultsOn, s.emergenciesOn = false, true
		releaseQueuedPickup(t, s, behind)
		stepUntil(t, s, "a claim of the pod behind", func() bool { return ownsDestination(s, behind) })
		claims := berthResources(behind.destination)
		if !s.admissionWaits(behind) || s.claimKind(behind, claims[0]) != claimService {
			t.Fatalf("the pod behind does not wait with a service claim: %+v", behind.Pod)
		}
		checkEmergenciesEachTick(t, s)
		if emergency {
			startEmergency(t, s, head, 0)
			if head.op.purpose != opEmergencyUnload || head.destination.ID != "market-1" {
				t.Fatalf("the head is not bound to market-1: %+v, %q", head.op, head.destination.ID)
			}
		}
		if s.incidentOutstanding() != emergency {
			t.Fatalf("emergency %v: the gate is %v", emergency, !emergency)
		}
		destination, relocation := behind.destination, behind.RelocatingTo
		s.admit()
		if behind.destination != destination || behind.RelocatingTo != relocation || behind.Pod.BlockedBy != head.Pod.ID {
			t.Fatalf("emergency %v: the pod behind goes to %q, blocked by %q", emergency, behind.destination.ID, behind.Pod.BlockedBy)
		}
		for _, r := range claims {
			if owned := s.owners[r].isPod(behind.Pod.ID); owned == emergency {
				t.Fatalf("emergency %v: the pod behind owns %v: %v", emergency, r, owned)
			}
		}
		if !emergency {
			continue
		}
		release()
		stepUntil(t, s, "the end of the emergency", func() bool { return len(s.emergencies) == 0 })
		if head.Pod.BerthID != "market-1" || head.withdrawn != 0 || s.incidentOutstanding() {
			t.Fatalf("the head ends at %q with the holds %#x, gate %v", head.Pod.BerthID, head.withdrawn, s.incidentOutstanding())
		}
	}
}

// TestEmergencyIncidentOutstanding checks each clause of the claim
// surrender gate. An emergency record or the emergency hold turns the gate
// on, also without the emergency switch, because the stage 1 test entry
// can set the hold without it (section 9.4 of the incident emergency
// contract). A fault record or the fault hold turns the gate on only with
// faults on.
func TestEmergencyIncidentOutstanding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                string
		faults, emergencies bool
		faultRecord, record bool
		hold                serviceHold
		want                bool
	}{
		{name: "nothing", faults: true, emergencies: true},
		{name: "emergency record", emergencies: true, record: true, want: true},
		{name: "emergency hold", emergencies: true, hold: emergencyHold, want: true},
		{name: "emergency hold, faults on", faults: true, hold: emergencyHold, want: true},
		{name: "emergency hold, both off", hold: emergencyHold, want: true},
		{name: "fault hold, both off", hold: faultHold},
		{name: "fault record, faults off", faultRecord: true},
		{name: "fault hold, emergencies on", emergencies: true, hold: faultHold},
		{name: "fault hold", faults: true, hold: faultHold, want: true},
		{name: "fault record", faults: true, faultRecord: true, want: true},
		{name: "both holds, both on", faults: true, emergencies: true, hold: faultHold | emergencyHold, want: true},
	} {
		s := &Simulation{vehicles: make([]vehicle, 2), faultsOn: tc.faults, emergenciesOn: tc.emergencies}
		s.vehicles[1].withdrawn = tc.hold
		if tc.record {
			s.emergencies = []emergencyRecord{{serial: 1, pod: 1}}
		}
		if tc.faultRecord {
			s.faults = []faultRecord{{serial: 1, pod: 1}}
		}
		if got := s.incidentOutstanding(); got != tc.want {
			t.Errorf("%s: the gate is %v, want %v", tc.name, got, tc.want)
		}
	}
}
