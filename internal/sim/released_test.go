package sim

import (
	"maps"
	"slices"
	"testing"
)

// twoBerthMarket returns the example network with a second Market berth.
// The two Market berths are at the same distance from the Market entry.
func twoBerthMarket() Network {
	network := Example()
	network.Nodes = append(network.Nodes, Node{ID: "market-berth-2", Position: Point{800, 170}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "market-in-2", From: "market-entry", To: "market-berth-2", SpeedLimit: 14, StationID: "market", StationRole: StationBerthAccessRole},
		Lane{ID: "market-out-2", From: "market-berth-2", To: "market-exit", SpeedLimit: 14, StationID: "market", StationRole: StationDepartureRole},
	)
	index := slices.IndexFunc(network.Stations, func(station Station) bool { return station.ID == "market" })
	network.Stations[index].Berths = append(slices.Clone(network.Stations[index].Berths), Berth{ID: "market-2", Node: "market-berth-2"})
	return network
}

// stepUntil steps s until done reports true, for at most 300 simulated
// seconds.
func stepUntil(t *testing.T, s *Simulation, what string, done func() bool) {
	t.Helper()
	for range 300 * TicksPerSecond {
		if done() {
			return
		}
		s.Step()
		checkTraffic(t, s.Snapshot())
	}
	t.Fatalf("%s did not happen: %+v", what, s.Snapshot())
}

// releaseFixture is a simulation where pod 01 travels from Parking to a
// pickup at Market for the pickup request. Pod 02 unloads request 1 at
// Market and becomes idle at the next step. Pod 03 is idle in Parking.
type releaseFixture struct {
	s                     *Simulation
	remote, local, parked *vehicle
	pickup                int
}

func newReleaseFixture(t *testing.T) releaseFixture {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "market"},
		{ID: "03", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := releaseFixture{s: s, remote: s.findVehicle("01"), local: s.findVehicle("02"), parked: s.findVehicle("03")}
	f.local.Pod.Activity, f.local.Pod.Occupied = Unloading, true
	f.local.Request = &Request{ID: 1, From: "garden", To: "market", PartySize: 1, PodID: f.local.Pod.ID}
	f.local.phaseTicks = 180 * TicksPerSecond
	s.requestID, s.boarded = 1, 1
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	f.pickup = s.requestID
	if s.waiting[0].request.PodID != "01" || f.remote.RelocatingTo != "market" {
		t.Fatalf("fixture did not send pod 01 to Market: %+v", s.Snapshot())
	}
	stepUntil(t, s, "pod 01 on the return lane", func() bool {
		return f.remote.Pod.LaneID == "return" && f.remote.Pod.LaneDistance > 20
	})
	f.local.phaseTicks = 1
	return f
}

// checkReleasedTo checks that a released pod goes to a berth and holds it.
func checkReleasedTo(t *testing.T, s *Simulation, v *vehicle, berthID string) {
	t.Helper()
	if !v.released || s.assigned(v.Pod.ID) || v.destination.ID != berthID || v.RelocatingTo != v.destinationStation ||
		v.Route[len(v.Route)-1].To != v.destination.Node {
		t.Fatalf("pod %s is not a released pod on its way to %s: %+v, released %v, destination %s",
			v.Pod.ID, berthID, v.Vehicle, v.released, v.destination.ID)
	}
	for _, r := range berthResources(v.destination) {
		if s.owners[r] != v.Pod.ID {
			t.Fatalf("pod %s does not hold %v", v.Pod.ID, r)
		}
	}
	if !maps.Equal(s.owners, s.retainedOwners()) {
		t.Fatal("the owners differ from the retention rules")
	}
}

func TestReleasedPickupPodTakesNextTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// samePass queues the next trip before the release.
		samePass bool
	}{
		{name: "same pass", samePass: true},
		{name: "next pass"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			if tc.samePass {
				f.s.requestID++
				f.s.waiting = append(f.s.waiting, waitingTrip{request: Request{
					ID: f.s.requestID, From: "garden", To: "market", PartySize: 1, RequestedTick: f.s.tick,
				}})
			}
			f.s.Step()
			if f.local.Pod.Activity != Boarding || f.local.Request.ID != f.pickup {
				t.Fatalf("the local pod did not take the pickup request: %+v", f.local.Vehicle)
			}
			if !tc.samePass {
				// Harbor is the next station on the route of pod 01.
				checkReleasedTo(t, f.s, f.remote, "harbor-1")
				if err := f.s.RequestTrip("garden", "market"); err != nil {
					t.Fatal(err)
				}
			}
			trip := f.s.waiting[len(f.s.waiting)-1]
			if trip.request.PodID != "01" || f.remote.RelocatingTo != "garden" || f.remote.released {
				t.Fatalf("the released pod did not take the next trip: %+v", f.s.Snapshot())
			}
			if berthOwner(f.s, "harbor-1") != "" {
				t.Fatal("the diverted pod kept its unused berth claim")
			}
			stepUntil(t, f.s, "the next trip boards", func() bool {
				return f.remote.Request != nil && f.remote.Request.ID == trip.request.ID
			})
			completeRequest(t, f.s, f.remote)
		})
	}
}

func TestPromotedPickupReleasesPod(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(twoBerthMarket(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "market", BerthID: "market-1"},
		{ID: "03", StationID: "market", BerthID: "market-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	remote := s.findVehicle("01")
	if err := s.sendPickup(remote, "harbor"); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 on the return lane", func() bool {
		return remote.Pod.LaneID == "return" && remote.Pod.LaneDistance > 20
	})
	// Request 1 waits for pod 01 and request 2 for pod 02. Pod 02 is ready,
	// so request 1 takes it. Request 2 then takes the idle pod 03 and
	// releases pod 01.
	s.requestID = 2
	s.waiting = []waitingTrip{
		{request: Request{ID: 1, From: "market", To: "garden", PartySize: 1, PodID: "01"}},
		{request: Request{ID: 2, From: "market", To: "harbor", PartySize: 1, PodID: "02"}},
	}
	s.dispatch()
	if len(s.waiting) != 0 {
		t.Fatalf("trips still wait: %+v", s.Snapshot().Pending)
	}
	for id, request := range map[string]int{"02": 1, "03": 2} {
		if v := s.findVehicle(id); v.Pod.Activity != Boarding || v.Request.ID != request {
			t.Fatalf("pod %s does not board request %d: %+v", id, request, v.Vehicle)
		}
	}
	checkReleasedTo(t, s, remote, "harbor-1")
}

func TestReleasedPodFallbackBerth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// start is the berth where pod 01 starts.
		start string
		// destination is the berth that the pickup of pod 01 uses.
		destination string
		// lane is where pod 01 is at the release.
		lane string
		// busy holds the berths that pod 02 and pod 03 hold.
		busy []string
		// congestion turns on congestion routing.
		congestion bool
		// setup changes s just before the fallback choice. The returned
		// function undoes the change.
		setup func(s *Simulation) (undo func())
		want  string
	}{
		{name: "next station", start: "market-2", destination: "garden-1", lane: "return", busy: []string{"parking-1", "parking-2"}, want: "harbor-1"},
		{name: "keeps nearest destination", start: "harbor-1", destination: "market-2", lane: "bypass-merge", busy: []string{"parking-1", "parking-2"}, want: "market-2"},
		{name: "tie breaks by berth order", start: "harbor-1", destination: "parking-1", lane: "bypass-merge", busy: []string{"garden-1", "parking-2"}, want: "market-1"},
		{name: "skips a berth in use", start: "harbor-1", destination: "parking-1", lane: "bypass-merge", busy: []string{"market-1", "parking-2"}, want: "market-2"},
		{
			name: "skips a berth that a pod goes to", start: "harbor-1", destination: "parking-1", lane: "bypass-merge",
			busy: []string{"garden-1", "parking-2"}, want: "market-2",
			setup: func(s *Simulation) func() {
				// Pod 02 goes to Market 1 but does not hold it.
				other := s.findVehicle("02")
				activity, destination := other.Pod.Activity, other.destination
				other.Pod.Activity, other.destination = Boarding, Berth{ID: "market-1", Node: "market-berth"}
				return func() { other.Pod.Activity, other.destination = activity, destination }
			},
		},
		{
			name: "skips a berth that a trip goes to", start: "harbor-1", destination: "parking-1", lane: "bypass-merge",
			busy: []string{"garden-1", "parking-2"}, want: "market-2",
			setup: func(s *Simulation) func() {
				s.waiting = append(s.waiting, waitingTrip{
					request:     Request{ID: 99, From: "garden", To: "market", PartySize: 1},
					destination: Berth{ID: "market-1", Node: "market-berth"},
				})
				return func() { s.waiting = nil }
			},
		},
		{
			name: "congestion changes the nearest berth", start: "harbor-1", destination: "parking-1", lane: "bypass-merge",
			busy: []string{"garden-1", "parking-2"}, congestion: true, want: "market-2",
			setup: func(s *Simulation) func() {
				// Pod 02 seems to hold the inlet of Market 1.
				track := resource{kind: trackResource, id: "market-in"}
				s.owners[track], s.congestionRouteCosts = "02", nil
				return func() { delete(s.owners, track) }
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := twoBerthMarket()
			s, err := NewFleet(network, []Placement{
				{ID: "01", StationID: stationOfBerth(t, network, tc.start), BerthID: tc.start},
				{ID: "02", StationID: stationOfBerth(t, network, tc.busy[0]), BerthID: tc.busy[0]},
				{ID: "03", StationID: stationOfBerth(t, network, tc.busy[1]), BerthID: tc.busy[1]},
			})
			if err != nil {
				t.Fatal(err)
			}
			s.SetCongestionRouting(tc.congestion)
			v := s.findVehicle("01")
			berth := Berth{ID: tc.destination}
			for _, station := range network.Stations {
				if found, ok := station.berth(tc.destination); ok {
					berth = found
				}
			}
			if err := s.startEmptyMove(v, emptyDestination{station: stationOfBerth(t, network, tc.destination), berth: berth}); err != nil {
				t.Fatal(err)
			}
			stepUntil(t, s, "pod 01 on lane "+tc.lane, func() bool {
				return v.Pod.LaneID == tc.lane && v.Pod.LaneDistance > 10
			})
			if !s.releasePickup(v) {
				t.Fatal("pod 01 was not released")
			}
			reserved := slices.Clone(v.blocks[:v.reservedThrough+1])
			undo := func() {}
			if tc.setup != nil {
				undo = tc.setup(s)
			}
			s.parkReleased(v)
			undo()
			checkReleasedTo(t, s, v, tc.want)
			if !slices.EqualFunc(reserved, v.blocks[:v.reservedThrough+1], func(a, b block) bool {
				return a.lane.ID == b.lane.ID && a.laneStart == b.laneStart
			}) {
				t.Fatal("the release changed the reserved track")
			}
			stepUntil(t, s, "pod 01 arrives", func() bool { return v.Pod.Activity == Idle })
			if v.Pod.BerthID != tc.want || v.released || v.RelocatingTo != "" {
				t.Fatalf("pod 01 stopped at %s, released %v: %+v", v.Pod.BerthID, v.released, v.Vehicle)
			}
		})
	}
}

// stationOfBerth returns the station of a berth.
func stationOfBerth(t *testing.T, network Network, berthID string) string {
	t.Helper()
	for _, station := range network.Stations {
		if _, ok := station.berth(berthID); ok {
			return station.ID
		}
	}
	t.Fatalf("unknown berth %s", berthID)
	return ""
}

// TestReleasedPodSkipsHeldOrigin checks that a pod that is not yet
// Clearance from its origin berth does not choose that berth. The origin
// release at Clearance would remove the new claim.
func TestReleasedPodSkipsHeldOrigin(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "market", BerthID: "market-1"},
		{ID: "03", StationID: "garden", BerthID: "garden-1"},
		{ID: "04", StationID: "parking", BerthID: "parking-1"},
		{ID: "05", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	remote := s.findVehicle("01")
	if err := s.sendPickup(remote, "market"); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 leaves its berth", func() bool {
		return remote.Pod.Activity == Traveling && remote.distance > 0
	})
	if remote.originReleased {
		t.Fatal("pod 01 released its origin too early for this test")
	}
	s.requestID = 1
	s.waiting = []waitingTrip{{request: Request{ID: 1, From: "market", To: "garden", PartySize: 1, PodID: "01"}}}
	s.dispatch()
	if !remote.released || remote.destination.ID == "harbor-1" {
		t.Fatalf("pod 01 is not released or goes to its origin: released %v, destination %s",
			remote.released, remote.destination.ID)
	}
	stepUntil(t, s, "pod 01 passes Clearance", func() bool {
		if !maps.Equal(s.owners, s.retainedOwners()) {
			t.Fatal("the owners differ from the retention rules")
		}
		return remote.originReleased
	})
}

func TestReleasedPodFinishesCommittedInlet(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(twoBerthMarket(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "market", BerthID: "market-2"},
		{ID: "03", StationID: "parking", BerthID: "parking-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	remote := s.findVehicle("01")
	if err := s.startEmptyMove(remote, emptyDestination{station: "market", berth: Berth{ID: "market-1", Node: "market-berth"}}); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 commits to the Market inlet", func() bool {
		_, _, ok := s.divertStart(remote)
		return remote.Pod.Activity == Traveling && !ok
	})
	route := slices.Clone(remote.Route)
	s.requestID = 1
	s.waiting = []waitingTrip{{request: Request{ID: 1, From: "market", To: "garden", PartySize: 1, PodID: "01"}}}
	s.dispatch()
	local := s.findVehicle("02")
	if local.Pod.Activity != Boarding || local.Request.ID != 1 {
		t.Fatalf("the local pod did not take request 1: %+v", local.Vehicle)
	}
	if !remote.released || remote.destination.ID != "market-1" || !slices.Equal(remote.Route, route) {
		t.Fatalf("the released pod left its committed inlet: %+v", remote.Vehicle)
	}
	if err := s.RequestTrip("harbor", "garden"); err != nil {
		t.Fatal(err)
	}
	if s.waiting[0].request.PodID != "03" {
		t.Fatalf("request 2 did not take the parked pod: %+v", s.Snapshot().Pending)
	}
	stepUntil(t, s, "pod 01 arrives", func() bool { return remote.Pod.Activity == Idle })
	if remote.Pod.BerthID != "market-1" || remote.released {
		t.Fatalf("pod 01 stopped at %s, released %v", remote.Pod.BerthID, remote.released)
	}
}

func TestRestoreKeepsReleasedPod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// clear removes the released flag from the saved state, as in a
		// state from an earlier build.
		clear bool
		// want is the pod that takes the next trip after the restore.
		want string
	}{
		{name: "released", want: "01"},
		{name: "saved without the flag", clear: true, want: "03"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			f.s.Step()
			checkReleasedTo(t, f.s, f.remote, "harbor-1")
			state := roundTripState(t, f.s.ExportState())
			index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == "01" })
			if !state.Pods[index].Released || !state.Pods[index].ClaimsDestination {
				t.Fatalf("the saved pod is not released: %+v", state.Pods[index])
			}
			if tc.clear {
				state.Pods[index].Released = false
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: f.s.network, Fleet: f.s.initial, State: state})
			if err != nil || !cleanRestore(result) {
				t.Fatalf("restore: %v, %+v", err, result)
			}
			if !tc.clear {
				checkRestoredMatches(t, f.s, restored)
			}
			if err := restored.RequestTrip("garden", "market"); err != nil {
				t.Fatal(err)
			}
			if got := restored.waiting[len(restored.waiting)-1].request.PodID; got != tc.want {
				t.Fatalf("pod %s took the next trip, want %s", got, tc.want)
			}
			if tc.clear {
				return
			}
			if err := f.s.RequestTrip("garden", "market"); err != nil {
				t.Fatal(err)
			}
			checkRestoredMatches(t, f.s, restored)
		})
	}
}

// TestRestoreIgnoresUnreleasablePod checks that a restore ignores the saved
// released flag of a pod that dispatch cannot release.
func TestRestoreIgnoresUnreleasablePod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pod  string
		// saved changes the saved pod.
		saved func(pod *SavedPod)
	}{
		{name: "idle pod with no station to relocate to", pod: "03"},
		{name: "boarding pod", pod: "02"},
		{name: "rebalancing pod", pod: "01", saved: func(pod *SavedPod) { pod.Rebalancing = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			f.s.Step()
			state := roundTripState(t, f.s.ExportState())
			index := slices.IndexFunc(state.Pods, func(pod SavedPod) bool { return pod.ID == tc.pod })
			state.Pods[index].Released = true
			if tc.saved != nil {
				tc.saved(&state.Pods[index])
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: f.s.network, Fleet: f.s.initial, State: state})
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("restore: %v, %+v", err, result)
			}
			if v := restored.findVehicle(tc.pod); v.released {
				t.Fatalf("pod %s is released after the restore: %+v", tc.pod, v.Vehicle)
			}
		})
	}
}

// TestRestoreReleasesUnboundPickupPod checks that a restore that removes the
// pod binding of a trip releases the empty pod on its way to that pickup.
func TestRestoreReleasesUnboundPickupPod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// live changes the live simulation before the export.
		live func(f releaseFixture)
		// saved changes the saved trip.
		saved func(trip *SavedTrip)
	}{
		{
			name: "trip route over the limit",
			live: func(f releaseFixture) {
				lane := f.s.network.Lanes[0]
				f.s.waiting[0].route = slices.Repeat([]Lane{lane}, len(f.s.network.Nodes)+1)
			},
		},
		{
			name:  "bad deferral check",
			saved: func(trip *SavedTrip) { trip.DeferCheck = -1 },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			f.local.Parties = 1
			if tc.live != nil {
				tc.live(f)
			}
			state := roundTripState(t, f.s.ExportState())
			if tc.saved != nil {
				tc.saved(&state.Waiting[0])
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: f.s.network, Fleet: f.s.initial, State: state})
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("restore: %v, %+v", err, result)
			}
			v := restored.findVehicle("01")
			if restored.waiting[0].request.PodID != "" || !v.released || v.RelocatingTo != "market" {
				t.Fatalf("pod 01 is not released: released %v, trip %+v, pod %+v",
					v.released, restored.waiting[0].request, v.Vehicle)
			}
			// Pod 02 becomes idle at Market and takes the trip. Pod 01 must
			// not keep its route to Market, where pod 02 boards.
			restored.Step()
			local := restored.findVehicle("02")
			if local.Pod.Activity != Boarding || local.Request.ID != f.pickup {
				t.Fatalf("the local pod did not take the pickup request: %+v", local.Vehicle)
			}
			checkReleasedTo(t, restored, v, "harbor-1")
		})
	}
}

// TestRestoreReleasesDroppedPickupPod checks that a restore that drops a
// trip releases the empty pod on its way to that pickup. The restore keeps
// the pod bound when a trip that it keeps names the pod.
func TestRestoreReleasesDroppedPickupPod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// saved changes the saved state. The trip of pod 01 is first.
		saved func(state *SavedState)
		// released is true when pod 01 must be released.
		released bool
	}{
		{
			name:     "invalid trip",
			saved:    func(state *SavedState) { state.Waiting[0].Request.PartySize = 0 },
			released: true,
		},
		{
			name:     "trip for a carried request",
			saved:    func(state *SavedState) { state.Waiting[0].Request.ID = 1 },
			released: true,
		},
		{
			name: "duplicate of an unbound trip",
			saved: func(state *SavedState) {
				first := state.Waiting[0]
				first.Route, first.Request.PodID = nil, ""
				state.Waiting = slices.Insert(state.Waiting, 0, first)
				state.RequestID++
			},
			released: true,
		},
		{
			name: "duplicate of a trip that keeps the pod",
			saved: func(state *SavedState) {
				state.Waiting = append(state.Waiting, state.Waiting[0])
				state.RequestID++
			},
		},
		{
			name: "invalid trip before a trip that keeps the pod",
			saved: func(state *SavedState) {
				kept := state.Waiting[0]
				state.RequestID++
				kept.Request.ID = state.RequestID
				state.Waiting[0].Request.PartySize = 0
				state.Waiting = append(state.Waiting, kept)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			f.local.Parties = 1
			state := roundTripState(t, f.s.ExportState())
			tc.saved(&state)
			restored, result, err := RestoreState(RestoreStateInput{Network: f.s.network, Fleet: f.s.initial, State: state})
			if err != nil || result.Tier != RestorePhysical || len(result.Dropped) != 1 {
				t.Fatalf("restore: %v, %+v", err, result)
			}
			v := restored.findVehicle("01")
			if v.released != tc.released || restored.assigned("01") == tc.released || v.RelocatingTo != "market" {
				t.Fatalf("pod 01: released %v, assigned %v, want released %v: %+v",
					v.released, restored.assigned("01"), tc.released, v.Vehicle)
			}
		})
	}
}

// TestReleasedPodYieldsBerth checks that a released pod that gives its berth
// to a passenger pod goes to another free berth. A relocating pod that is
// not released keeps its route.
func TestReleasedPodYieldsBerth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// prepare changes pod 01 after the release.
		prepare func(v *vehicle)
		// parks is true when pod 01 must go to another berth.
		parks bool
	}{
		{name: "released", parks: true},
		{name: "parking move", prepare: func(v *vehicle) { v.released = false }},
		{name: "rebalancing move", prepare: func(v *vehicle) { v.released, v.Rebalancing = false, true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			f.s.Step()
			checkReleasedTo(t, f.s, f.remote, "harbor-1")
			if tc.prepare != nil {
				tc.prepare(f.remote)
			}
			before := f.remote.Vehicle
			route := slices.Clone(f.remote.Route)
			// Pod 02 carries the pickup request to Harbor 1.
			harbor, _ := f.s.station("harbor")
			berth, _ := harbor.berth("harbor-1")
			f.local.destination, f.local.destinationStation = berth, "harbor"
			f.s.redistribute()
			if berthOwner(f.s, "harbor-1") == f.remote.Pod.ID {
				t.Fatal("pod 01 kept its claim on Harbor 1")
			}
			if tc.parks {
				if f.remote.destination.ID == "harbor-1" {
					t.Fatalf("pod 01 still goes to Harbor 1: %+v", f.remote.Vehicle)
				}
				checkReleasedTo(t, f.s, f.remote, f.remote.destination.ID)
				return
			}
			if f.remote.destination.ID != "harbor-1" || f.remote.RelocatingTo != before.RelocatingTo ||
				f.remote.Rebalancing != before.Rebalancing || !slices.Equal(f.remote.Route, route) {
				t.Fatalf("pod 01 changed its move: %+v, want %+v", f.remote.Vehicle, before)
			}
		})
	}
}

// TestNearestFreeBerthSkipsStartNode checks that a moving pod does not choose
// a berth at the node where its new route starts. A departing pod can choose
// it.
func TestNearestFreeBerthSkipsStartNode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		activity Activity
		want     bool
	}{
		{name: "traveling", activity: Traveling},
		{name: "departing empty", activity: DepartingEmpty, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newReleaseFixture(t)
			f.s.Step()
			checkReleasedTo(t, f.s, f.remote, "harbor-1")
			f.remote.Pod.Activity = tc.activity
			harbor, _ := f.s.station("harbor")
			start, _ := harbor.berth("harbor-1")
			berth, _, ok := f.s.nearestFreeBerth(f.remote, start.Node)
			if !ok {
				t.Fatal("no free berth")
			}
			if got := berth.ID == start.ID; got != tc.want {
				t.Fatalf("pod 01 chose %s from the node of %s", berth.ID, start.ID)
			}
		})
	}
}
