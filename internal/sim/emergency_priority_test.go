package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

// queueRoute returns the route of the queue tests: the lane link with
// blocks 0 to 6, where block 6 holds the junction merge, and then the lane
// down with blocks 7 to 9. From a block before 6, the queue spans of a pod
// end at block 7, the end of the reservation of the merge.
func queueRoute() blockList {
	var blocks []block
	for index := range 10 {
		lane, cell, laneStart := Lane{ID: "link", SpeedLimit: 14}, index, 0.0
		if index > 6 {
			lane, cell, laneStart = Lane{ID: "down", SpeedLimit: 14}, index-7, 210
		}
		resources := []resource{{kind: trackResource, id: lane.ID, cell: cell}}
		if index == 6 {
			resources = append(resources, resource{kind: junctionResource, id: "merge"})
		}
		blocks = append(blocks, block{lane: lane, cell: cell, start: float64(30 * index), end: float64(30 * (index + 1)), laneStart: laneStart, resources: resources, last: index == 6 || index == 9})
	}
	return blockListOf(blocks)
}

// queuePod is a traveling pod of the queue tests. It owns the blocks of
// owns on queueRoute, and its reservation ends at the last of them.
type queuePod struct {
	id   string
	owns []int
	// leader is the ID of the pod ahead in a platoon, or "".
	leader string
	// faulted faults the pod, and idle makes it Idle.
	faulted, idle bool
}

// queueSimulation returns a simulation with the record pod E, serial 1,
// at index 0. E owns block 0 of queueRoute. The other pods follow in the
// order of pods.
func queueSimulation(pods []queuePod) *Simulation {
	s := &Simulation{owners: make(map[resource]resourceOwner), emergenciesOn: true, networkIndexes: &networkIndexes{}}
	pods = append([]queuePod{{id: "E", owns: []int{0}}}, pods...)
	for _, pod := range pods {
		v := vehicle{Pod: Pod{ID: pod.id, Activity: Traveling}, reservedThrough: -1, blocks: queueRoute(), faulted: pod.faulted}
		if pod.idle {
			v.Pod.Activity = Idle
		}
		for _, index := range pod.owns {
			for _, r := range v.blocks.at(index).resources {
				s.owners[r] = podResourceOwner(pod.id)
			}
			v.reservedThrough = index
		}
		s.vehicles = append(s.vehicles, v)
	}
	for index, pod := range pods {
		if pod.leader == "" {
			continue
		}
		leader := slices.IndexFunc(pods, func(p queuePod) bool { return p.id == pod.leader })
		s.vehicles[index].link.leader, s.vehicles[leader].follower = leader+1, index+1
	}
	s.emergencies = []emergencyRecord{{serial: 1, pod: 0}}
	return s
}

// TestEmergencyQueuePromotion checks the members of the queue of a record
// (section 9.3.1 of the incident emergency contract). The queue holds the
// record pod and each pod that owns a resource of the queue spans of a
// pod of the queue. The spans of E end with the reservation of the merge
// at block 7.
func TestEmergencyQueuePromotion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pods []queuePod
		// junction is the owner of the junction merge, or "".
		junction string
		// branch owns a cell of another lane into the merge.
		branch string
		want   []string
	}{
		{name: "nobody ahead", pods: []queuePod{{id: "far", owns: []int{9}}}, want: []string{"E"}},
		{name: "queue ahead", pods: []queuePod{{id: "A", owns: []int{2, 3}}, {id: "B", owns: []int{5}}, {id: "far", owns: []int{9}}}, want: []string{"A", "B", "E"}},
		{name: "pod past the merge in the span", pods: []queuePod{{id: "C", owns: []int{7}}}, want: []string{"C", "E"}},
		{name: "pod in the merge", pods: []queuePod{{id: "Y"}}, junction: "Y", want: []string{"E", "Y"}},
		{name: "pod on the other branch", pods: []queuePod{{id: "X"}}, branch: "X", want: []string{"E"}},
		// A owns the merge, so its spans go on to block 9 and bring D.
		{name: "queue of a queue pod", pods: []queuePod{{id: "A", owns: []int{4, 5, 6, 7}}, {id: "D", owns: []int{9}}}, want: []string{"A", "D", "E"}},
		// A pod that requests no grant waits for no pod.
		{name: "faulted queue pod", pods: []queuePod{{id: "A", owns: []int{4, 5, 6, 7}, faulted: true}, {id: "D", owns: []int{9}}}, want: []string{"A", "E"}},
		{name: "idle queue pod", pods: []queuePod{{id: "A", owns: []int{4, 5, 6, 7}, idle: true}, {id: "D", owns: []int{9}}}, want: []string{"A", "E"}},
		// F holds block 2 that its leader L owns, so F owns nothing. G
		// follows E in its platoon. A pod of the queue brings the pods
		// behind it in its platoon.
		{name: "platoon", pods: []queuePod{{id: "L", owns: []int{2, 3}}, {id: "F", leader: "L"}, {id: "G", leader: "E"}}, want: []string{"E", "F", "G", "L"}},
		{name: "platoon behind E", pods: []queuePod{{id: "G", leader: "E"}}, want: []string{"E", "G"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := queueSimulation(tc.pods)
			if tc.junction != "" {
				s.owners[resource{kind: junctionResource, id: "merge"}] = podResourceOwner(tc.junction)
			}
			if tc.branch != "" {
				s.owners[resource{kind: trackResource, id: "branch"}] = podResourceOwner(tc.branch)
			}
			var tiers emergencyTiers
			tiers.build(s)
			var got []string
			for index, serial := range tiers.promoted {
				if serial != 1 {
					t.Fatalf("pod %s has the serial %d", s.vehicles[index].Pod.ID, serial)
				}
				got = append(got, s.vehicles[index].Pod.ID)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("queue %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEmergencyQueueSerial checks the serials of the queue. A record pod
// in the queue of another record, and each pod of its own queue, get the
// lower serial. With no record, the queue is empty.
func TestEmergencyQueueSerial(t *testing.T) {
	t.Parallel()
	s := queueSimulation([]queuePod{{id: "A", owns: []int{2}}, {id: "E2", owns: []int{4}}, {id: "B", owns: []int{6}}})
	s.emergencies = []emergencyRecord{{serial: 3, pod: 0}, {serial: 5, pod: 2}}
	var tiers emergencyTiers
	tiers.build(s)
	want := map[int]uint64{0: 3, 1: 3, 2: 3, 3: 3}
	if !maps.Equal(tiers.promoted, want) {
		t.Fatalf("serials %v, want %v", tiers.promoted, want)
	}
	s.emergencies = nil
	tiers.build(s)
	if len(tiers.promoted) != 0 {
		t.Fatalf("with no record, the queue is %v", tiers.promoted)
	}
}

// TestEmergencyPromotedOrder checks compareAdmission with the promoted
// tier: every emergency intent goes first, then the promoted intents by
// serial, and then in the order of the other intents, which keeps the age
// rule. Each promoted intent goes before each ordinary intent.
func TestEmergencyPromotedOrder(t *testing.T) {
	t.Parallel()
	tick := int64(admissionAgeTicks + 1)
	emergency := intent{since: tick, priority: 2, id: "09", emergency: 9}
	promoted := intent{since: tick, priority: 2, id: "01", promoted: 1}
	later := intent{since: 0, priority: 0, id: "00", promoted: 2}
	agedPromoted := intent{since: 0, priority: 2, id: "03", promoted: 1}
	youngPromoted := intent{since: tick, priority: 0, id: "02", promoted: 1}
	aged := intent{since: 0, priority: 0, id: "00"}
	for _, pair := range [][2]intent{
		{emergency, promoted}, {promoted, later}, {agedPromoted, youngPromoted},
		{youngPromoted, promoted}, {later, aged}, {promoted, aged},
	} {
		if compareAdmission(pair[0], pair[1], tick) >= 0 || compareAdmission(pair[1], pair[0], tick) <= 0 {
			t.Fatalf("%+v does not go before %+v", pair[0], pair[1])
		}
	}
}

// TestEmergencyPromotedAdmission checks the promoted tier in admit. Pod Q
// waits at the merge in front of E, and pod O on the other branch has an
// aged request for the merge. With no record, O gets the merge. With a
// record on E, Q is in the queue of E and gets the merge first.
func TestEmergencyPromotedAdmission(t *testing.T) {
	t.Parallel()
	for _, record := range []bool{false, true} {
		s := queueSimulation([]queuePod{{id: "Q", owns: []int{2, 3, 4, 5}}, {id: "O"}})
		s.tick = admissionAgeTicks
		q, o := &s.vehicles[1], &s.vehicles[2]
		q.blockIndex, q.distance = 5, 179
		o.blocks = blockListOf([]block{{lane: Lane{ID: "branch", SpeedLimit: 14}, end: 30, resources: []resource{{kind: junctionResource, id: "merge"}}, last: true}})
		o.waitSince = 0
		q.waitSince = s.tick
		if !record {
			s.emergencies = nil
		}
		s.admit()
		want := podResourceOwner("O")
		if record {
			want = podResourceOwner("Q")
		}
		if got := s.owners[resource{kind: junctionResource, id: "merge"}]; got != want {
			t.Fatalf("record %v: merge owner %v, want %v", record, got, want)
		}
	}
}

// buildQueue returns the queue of s by pod ID and serial.
func buildQueue(s *Simulation) map[string]uint64 {
	var tiers emergencyTiers
	tiers.build(s)
	queue := make(map[string]uint64, len(tiers.promoted))
	for index, serial := range tiers.promoted {
		queue[s.vehicles[index].Pod.ID] = serial
	}
	return queue
}

// TestEmergencyQueueRecords checks the queues of more than one record. Two
// records with disjoint queues each get their own serial. When the older
// record of two overlapping queues ends, its queue pods move to the
// serial of the record that stays. A faulted record pod requests no grant,
// so its queue holds only the pod and its platoon.
func TestEmergencyQueueRecords(t *testing.T) {
	t.Parallel()
	// E2 owns block 8, past the queue spans of E, and B owns block 9.
	s := queueSimulation([]queuePod{{id: "A", owns: []int{2}}, {id: "E2", owns: []int{8}}, {id: "B", owns: []int{9}}})
	s.emergencies = []emergencyRecord{{serial: 1, pod: 0}, {serial: 2, pod: 2}}
	if got, want := buildQueue(s), map[string]uint64{"E": 1, "A": 1, "E2": 2, "B": 2}; !maps.Equal(got, want) {
		t.Fatalf("disjoint queues %v, want %v", got, want)
	}
	// E2 owns block 4, in the queue spans of E.
	s = queueSimulation([]queuePod{{id: "A", owns: []int{2}}, {id: "E2", owns: []int{4}}, {id: "B", owns: []int{6}}})
	s.emergencies = []emergencyRecord{{serial: 3, pod: 0}, {serial: 5, pod: 2}}
	if got, want := buildQueue(s), map[string]uint64{"E": 3, "A": 3, "E2": 3, "B": 3}; !maps.Equal(got, want) {
		t.Fatalf("overlapping queues %v, want %v", got, want)
	}
	s.emergencies = s.emergencies[1:]
	if got, want := buildQueue(s), map[string]uint64{"E2": 5, "B": 5}; !maps.Equal(got, want) {
		t.Fatalf("after the end of the older record %v, want %v", got, want)
	}
	s = queueSimulation([]queuePod{{id: "A", owns: []int{2}}})
	s.vehicles[0].faulted = true
	if got, want := buildQueue(s), map[string]uint64{"E": 1}; !maps.Equal(got, want) {
		t.Fatalf("faulted record pod %v, want %v", got, want)
	}
}

// TestEmergencyQueueRetained checks the retained resources of a linked
// follower. E follows P, and the route of P forked away, so the queue
// spans of E are free. E still retains a shared cell that P owns, and
// grant refuses a request of E outside its linked span until P releases
// the cell (see holdsPending), so P is in the queue. The cells that an
// unlinked pod retains do not count.
func TestEmergencyQueueRetained(t *testing.T) {
	t.Parallel()
	s := queueSimulation([]queuePod{{id: "P"}, {id: "X", owns: []int{2}}, {id: "Y"}})
	shared := resource{kind: trackResource, id: "shared"}
	s.owners[shared] = podResourceOwner("P")
	e := &s.vehicles[0]
	e.routeReleases = map[resource]float64{shared: 40}
	e.link.leader, s.vehicles[1].follower = 2, 1
	// X is in the queue, but it is not linked, so the owner Y of a cell
	// that X retains is not.
	other := resource{kind: trackResource, id: "other"}
	s.vehicles[2].routeReleases = map[resource]float64{other: 40}
	s.owners[other] = podResourceOwner("Y")
	if got, want := buildQueue(s), map[string]uint64{"E": 1, "P": 1, "X": 1}; !maps.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}
}

// TestEmergencyQueueNoRequest checks that a pod of the queue that requests
// no grant in this tick still brings the pods ahead of it. A's next block
// is on the final lane of its passenger route, and its destination station
// does not exist, so its terminal berth choice fails, and admit makes no
// request for it. D owns a block of the queue spans of A, so D is in the
// queue.
func TestEmergencyQueueNoRequest(t *testing.T) {
	t.Parallel()
	s := queueSimulation([]queuePod{{id: "A", owns: []int{7}}, {id: "D", owns: []int{9}}})
	a := &s.vehicles[1]
	a.Pod.Occupied, a.Riders, a.destinationStation = true, []Request{{ID: 1, PartySize: 1}}, "nowhere"
	a.Route = slices.Clone(a.blocks.route)
	// A is at the end of its reservation, so only the failed choice stops
	// its request.
	a.blockIndex, a.distance = 7, 239
	if next, ok := s.admissionRequest(a); !ok || next != 8 || s.assignTerminalBerth(a) {
		t.Fatalf("A requests %d %v, or its berth choice does not fail", next, ok)
	}
	s.admit()
	if a.pending != 0 || a.reservedThrough != 7 {
		t.Fatalf("A requested a grant: pending %d, reserved %d", a.pending, a.reservedThrough)
	}
	if got, want := buildQueue(s), map[string]uint64{"E": 1, "A": 1, "D": 1}; !maps.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}
}

// TestEmergencyRecordInQueue checks that a record pod in the queue of an
// older record keeps its own emergency tier in admit. E2, serial 2, is in
// the queue of E and requests the merge. O is also in the queue of E, by
// a cell that it owns on the route of E, and its request for the merge is
// aged. E2 gets the merge, because an emergency intent goes before each
// promoted intent.
func TestEmergencyRecordInQueue(t *testing.T) {
	t.Parallel()
	s := queueSimulation([]queuePod{{id: "E2", owns: []int{2, 3, 4, 5}}, {id: "O"}})
	s.tick = admissionAgeTicks
	e2, o := &s.vehicles[1], &s.vehicles[2]
	e2.blockIndex, e2.distance, e2.waitSince = 5, 179, s.tick
	e2.Pod.Occupied, e2.Riders = true, []Request{{ID: 1, PartySize: 1}}
	o.blocks = blockListOf([]block{{lane: Lane{ID: "branch", SpeedLimit: 14}, end: 30, resources: []resource{{kind: junctionResource, id: "merge"}}, last: true}})
	s.owners[resource{kind: trackResource, id: "link", cell: 1}] = podResourceOwner("O")
	s.emergencies = append(s.emergencies, emergencyRecord{serial: 2, pod: 1, order: 1})
	if got, want := buildQueue(s), map[string]uint64{"E": 1, "E2": 1, "O": 1}; !maps.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}
	s.admit()
	if got := s.owners[resource{kind: junctionResource, id: "merge"}]; got != podResourceOwner("E2") {
		t.Fatalf("merge owner %v, want E2", got)
	}
}

// TestEmergencyQueueRestore checks that the queue, which no save holds,
// gives the same run after a restore. Four pods drive in one platoon to a
// station with one berth, and p03 has a record. Its queue has the pods
// ahead of it and its follower p04. Two restores of a save of that tick
// have the queue of the live run, and they run in lockstep for 20
// seconds, with the same queue and the same saved state on each tick. A
// restore sets each speed to zero, so the live run is not a reference
// after the restore tick.
func TestEmergencyQueueRestore(t *testing.T) {
	t.Parallel()
	lanes := []geometryLane{{to: Point{X: 300}}, {to: Point{X: 600}}, {to: Point{X: 900}}, {to: Point{X: 1200}}}
	network := geometryNetwork(Point{}, lanes, 4)
	s := restoreGeometry(t, network, len(lanes), 4, 200, 30)
	s.emergenciesOn = true
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	for range 5 * TicksPerSecond {
		s.Step()
	}
	startEmergency(t, s, &s.vehicles[2], 0)
	s.Step()
	want := map[string]uint64{"p01": 1, "p02": 1, "p03": 1, "p04": 1}
	if got := buildQueue(s); !maps.Equal(got, want) || s.vehicles[3].link.leader == 0 {
		t.Fatalf("queue %v, want %v, p04 link %+v", got, want, s.vehicles[3].link)
	}
	restore := func() *Simulation {
		restored, result, err := RestoreState(emergencyRestoreInput(s, s.ExportState()))
		if err != nil || !cleanRestore(result) {
			t.Fatalf("restore %v, %+v", err, result)
		}
		if err := restored.SetPlatooning(PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
		return restored
	}
	first, second := restore(), restore()
	if got := buildQueue(first); !maps.Equal(got, want) {
		t.Fatalf("restored queue %v, want %v", got, want)
	}
	for range 20 * TicksPerSecond {
		first.Step()
		second.Step()
		if got, want := buildQueue(second), buildQueue(first); !maps.Equal(got, want) {
			t.Fatalf("tick %d: the second restore has the queue %v, the first %v", first.tick, got, want)
		}
		if !reflect.DeepEqual(second.ExportState(), first.ExportState()) {
			t.Fatalf("tick %d: the restores differ", first.tick)
		}
	}
}
