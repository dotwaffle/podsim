package sim

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// restoreTick is the tick of the saved states that the restore tables build.
const restoreTick = 600

// restoreFleet has a pod at each berth of the example network.
func restoreFleet() []Placement {
	return []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
		{ID: "03", StationID: "parking", BerthID: "parking-1"},
		{ID: "04", StationID: "parking", BerthID: "parking-2"},
		{ID: "05", StationID: "market", BerthID: "market-1"},
	}
}

// restoreFixture builds saved states for a network and a fleet.
type restoreFixture struct {
	network Network
	fleet   []Placement
	s       *Simulation
}

func newRestoreFixture(t *testing.T, network Network) restoreFixture {
	t.Helper()
	return newRestoreFleetFixture(t, network, restoreFleet())
}

func newRestoreFleetFixture(t *testing.T, network Network, fleet []Placement) restoreFixture {
	t.Helper()
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	return restoreFixture{network: network, fleet: fleet, s: s}
}

// state returns a saved state at restoreTick with each fleet pod idle at its
// first berth. The given pods replace the fleet pods with the same IDs. Each
// party in a pod counts as one submitted and boarded order.
func (f restoreFixture) state(pods ...SavedPod) SavedState {
	state := f.s.ExportState()
	state.Tick = restoreTick
	for _, pod := range pods {
		state.Pods[slices.IndexFunc(state.Pods, func(saved SavedPod) bool { return saved.ID == pod.ID })] = pod
	}
	for _, pod := range state.Pods {
		if pod.carriesPassengers() {
			state.RequestID += max(1, pod.Parties)
		}
	}
	state.Boarded = state.RequestID
	return state
}

func (f restoreFixture) restore(state SavedState) (*Simulation, RestoreResult, error) {
	return RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state})
}

// restoreTiers are the tiers that a test can make RestoreState use.
var restoreTiers = []RestoreTier{RestorePhysical, RestoreLogical}

// restoreTier restores a saved state. For the logical tier, the restore
// skips the physical tier.
func (f restoreFixture) restoreTier(state SavedState, tier RestoreTier) (*Simulation, RestoreResult, error) {
	return RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, LogicalOnly: tier == RestoreLogical})
}

func (f restoreFixture) berth(t *testing.T, id string) berthRef {
	t.Helper()
	for _, station := range f.network.Stations {
		if berth, ok := station.berth(id); ok {
			return berthRef{station: station.ID, berth: berth}
		}
	}
	t.Fatalf("unknown berth %s", id)
	return berthRef{}
}

// idle returns a pod that waits empty at a berth.
func (f restoreFixture) idle(t *testing.T, id, berthID string) SavedPod {
	t.Helper()
	berth := f.berth(t, berthID)
	return SavedPod{
		ID: id, Activity: activityCode(Idle), StationID: berth.station, BerthID: berthID,
		Destination: berthID, DestinationStation: berth.station,
	}
}

// boarding returns a pod that boards one party at a berth, with a route to
// Market.
func (f restoreFixture) boarding(t *testing.T, id, berthID string) SavedPod {
	t.Helper()
	berth := f.berth(t, berthID)
	route, err := f.s.stationApproachRoute(berth.berth.Node, "market")
	if err != nil {
		t.Fatal(err)
	}
	return SavedPod{
		ID: id, Activity: activityCode(Boarding), StationID: berth.station, BerthID: berthID,
		Request: &SavedRequest{ID: 1, From: berth.station, To: "market", PartySize: 1, PodID: id, RequestedTick: 10},
		Parties: 1, PhaseTicks: boardingTicks / 2, Origin: berthID, DestinationStation: "market",
		Route: f.s.laneIndexes(route, len(route)),
	}
}

type travelInput struct {
	id string
	// from and to are the berths at the ends of the route.
	from, to string
	// lane is the lane of the route that holds the pod, and distance is the
	// distance of the pod from the start of that lane.
	lane     string
	distance float64
}

// traveling returns an empty pod that travels on the route between two
// berths.
func (f restoreFixture) traveling(t *testing.T, input travelInput) SavedPod {
	t.Helper()
	to := f.berth(t, input.to)
	route, err := f.s.route(f.berth(t, input.from).berth.Node, to.berth.Node)
	if err != nil {
		t.Fatal(err)
	}
	routeIndex := slices.IndexFunc(route, func(lane Lane) bool { return lane.ID == input.lane })
	if routeIndex < 0 {
		t.Fatalf("the route from %s to %s does not use %s", input.from, input.to, input.lane)
	}
	distance := input.distance
	for _, lane := range route[:routeIndex] {
		distance += f.s.laneLength(lane)
	}
	return SavedPod{
		ID: input.id, Activity: activityCode(Traveling), Origin: input.from, Destination: input.to,
		DestinationStation: to.station, Route: f.s.laneIndexes(route, len(route)), RouteIndex: routeIndex,
		LaneID: input.lane, LaneDistance: input.distance, Distance: distance,
	}
}

// carrying puts parties of the request with an ID in a traveling pod. The
// request joins the stations at the ends of the pod route.
func (f restoreFixture) carrying(t *testing.T, pod SavedPod, requestID, parties int) SavedPod {
	t.Helper()
	pod.Occupied, pod.Parties = true, parties
	pod.Request = &SavedRequest{
		ID: requestID, From: f.berth(t, pod.Origin).station, To: pod.DestinationStation, PartySize: parties,
		PodID: pod.ID, RequestedTick: 10,
	}
	return pod
}

// relocating makes an empty traveling pod relocate to the station of its
// destination.
func relocating(pod SavedPod) SavedPod {
	pod.RelocatingTo = pod.DestinationStation
	return pod
}

func findVehicle(t *testing.T, s *Simulation, id string) *vehicle {
	t.Helper()
	v := s.findVehicle(id)
	if v == nil {
		t.Fatalf("no pod %s", id)
	}
	return v
}

// checkAtBerth checks the activity and the berth of a pod.
func checkAtBerth(t *testing.T, s *Simulation, id string, activity Activity, berthID string) *vehicle {
	t.Helper()
	v := findVehicle(t, s, id)
	berth := s.network.Stations[slices.IndexFunc(s.network.Stations, func(station Station) bool {
		_, ok := station.berth(berthID)
		return ok
	})]
	if v.Pod.Activity != activity || v.Pod.BerthID != berthID || v.Pod.StationID != berth.ID || v.phaseTicks != 0 {
		t.Fatalf("pod %s is %q at berth %q of %q after %d ticks, want %q at %s",
			id, v.Pod.Activity, v.Pod.BerthID, v.Pod.StationID, v.phaseTicks, activity, berthID)
	}
	return v
}

func berthOwner(s *Simulation, berthID string) string {
	return s.owners[resource{kind: berthResource, id: berthID}]
}

func TestRestoreDemotesTravelingPods(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	// On the Market approach, pod 02 is in a block that pod 01 still holds.
	// Order 2 is in the queue, so pod 02 carries orders 3 to 5.
	ahead := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "market-approach", distance: 40}), 1, 1)
	behind := f.carrying(t, f.traveling(t, travelInput{id: "02", from: "garden-1", to: "market-1", lane: "market-approach", distance: 10}), 3, 3)
	queued := []SavedTrip{
		{Request: SavedRequest{ID: 2, From: "market", To: "garden", PartySize: 1, RequestedTick: 10}},
		{Request: SavedRequest{ID: 6, From: "market", To: "garden", PartySize: 1, RequestedTick: 20}},
	}
	// Pod 04 relocates to Parking. A wrong lane ID demotes it. Pod 05 carries
	// a party on the return line, so a berth can be free with no pod at it.
	parker := relocating(f.traveling(t, travelInput{id: "04", from: "garden-1", to: "parking-2", lane: "return-to-parking", distance: 100}))
	misplaced := parker
	misplaced.LaneID = "market-in"
	returning := f.carrying(t, f.traveling(t, travelInput{id: "05", from: "market-1", to: "harbor-1", lane: "return", distance: 150}), 1, 1)
	departed := f.carrying(t, f.traveling(t, travelInput{id: "02", from: "garden-1", to: "market-1", lane: "garden-out", distance: 0}), 1, 1)
	departed.LaneID = ""
	unknownLane := parker
	unknownLane.Route = slices.Clone(parker.Route)
	unknownLane.Route[1] = len(f.network.Lanes)
	claimant := parker
	claimant.ClaimsDestination = true
	admitted := relocating(f.traveling(t, travelInput{id: "04", from: "garden-1", to: "parking-2", lane: "parking-in-2", distance: 150}))
	admitted.ClaimsDestination = true
	phased := parker
	phased.PhaseTicks = 3
	// The saved route of pod 01 starts after its origin lane, but the pod is
	// still within Clearance of the route start.
	nearOrigin := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "approach-branch", distance: 5}), 1, 1)
	nearOrigin.Route, nearOrigin.RouteIndex, nearOrigin.Distance = nearOrigin.Route[1:], 0, 5
	// Pod 01 has no Market berth yet, so its route ends at the station entry.
	// In the last block of the lane before, it reserves the last lane.
	noBerth := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "bypass-merge", distance: 200}), 1, 1)
	noBerth.Route, noBerth.Destination = noBerth.Route[:len(noBerth.Route)-1], ""
	beforeLastLane := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "bypass-merge", distance: 100}), 1, 1)
	beforeLastLane.Route, beforeLastLane.Destination = noBerth.Route, ""
	for _, tc := range []struct {
		name  string
		pods  []SavedPod
		check func(*testing.T, SavedState, *Simulation, RestoreResult)
		// demoted lists the pods that the restore moves to a berth.
		demoted []string
		// waiting holds the saved queue. Each order in it counts as submitted.
		waiting []SavedTrip
	}{
		{
			name: "overlap boards again", demoted: []string{"02"},
			pods: []SavedPod{ahead, behind, f.idle(t, "04", "parking-2")},
			check: func(t *testing.T, state SavedState, s *Simulation, result RestoreResult) {
				t.Helper()
				v := checkAtBerth(t, s, "02", Boarding, "garden-1")
				route, err := s.stationApproachRoute("garden-berth", "market")
				if err != nil {
					t.Fatal(err)
				}
				if v.Parties != 3 || v.Request.PodID != "02" || v.destinationStation != "market" || !reflect.DeepEqual(v.Route, route) ||
					v.pending != -1 || v.Pod.Occupied {
					t.Fatalf("pod 02 boards %d parties of %+v to %s on %v", v.Parties, v.Request, v.destinationStation, v.Route)
				}
				if findVehicle(t, s, "01").Pod.Activity != Traveling || len(result.Requeued) > 0 || s.boarded != state.Boarded {
					t.Fatalf("pod 01 stopped, or the restore queued %v", result.Requeued)
				}
			},
		},
		{
			name: "overlap with a full origin queues the parties", demoted: []string{"02"},
			pods: []SavedPod{ahead, behind, f.idle(t, "04", "garden-1")}, waiting: queued,
			check: func(t *testing.T, state SavedState, s *Simulation, result RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "02", Idle, "harbor-1")
				// The requeued order goes before the first queued order with a
				// larger ID.
				ids := make([]int, len(s.waiting))
				for index, trip := range s.waiting {
					ids[index] = trip.request.ID
				}
				if !slices.Equal(result.Requeued, []int{3}) || !slices.Equal(ids, []int{2, 3, 6}) || s.waiting[1].parties != 3 ||
					s.waiting[1].request.PodID != "" || s.boarded != state.Boarded {
					t.Fatalf("requeued %v, queue %+v, boarded %d", result.Requeued, s.waiting, s.boarded)
				}
				for range 10 * TicksPerSecond {
					s.Step()
					if gap := s.ordersGap(); gap != 0 {
						t.Fatalf("tick %d: order gap %d", s.tick, gap)
					}
					if v := findVehicle(t, s, "04"); v.Request != nil && v.Request.ID == 3 {
						// Only the new orders that left the queue add to the
						// boarded count.
						boarded := state.Boarded + len(state.Waiting)
						for _, trip := range s.waiting {
							if trip.parties == 0 {
								boarded--
							}
						}
						if v.Parties != 3 || s.boarded != boarded {
							t.Fatalf("pod 04 took %d parties, boarded %d, want %d", v.Parties, s.boarded, boarded)
						}
						return
					}
				}
				t.Fatal("no pod took the queued parties")
			},
		},
		{
			name: "lane ID mismatch goes to the destination", demoted: []string{"04"},
			pods: []SavedPod{f.idle(t, "01", "harbor-1"), f.idle(t, "02", "market-1"), f.idle(t, "03", "parking-1"), misplaced, returning},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				v := checkAtBerth(t, s, "04", Idle, "parking-2")
				if v.RelocatingTo != "" || v.Route != nil || v.destination.ID != "parking-2" {
					t.Fatalf("pod 04 relocates to %q on %v", v.RelocatingTo, v.Route)
				}
			},
		},
		{
			name: "taken destination sends the pod to its origin", demoted: []string{"04"},
			pods: []SavedPod{f.idle(t, "01", "harbor-1"), f.idle(t, "02", "market-1"), f.idle(t, "03", "parking-2"), misplaced, returning},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "04", Idle, "garden-1")
			},
		},
		{
			name: "taken origin sends the pod to its station", demoted: []string{"04"},
			pods: []SavedPod{f.idle(t, "01", "harbor-1"), f.idle(t, "02", "garden-1"), f.idle(t, "03", "parking-2"), misplaced, returning},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "04", Idle, "parking-1")
			},
		},
		{
			name: "taken station sends the pod to the first free berth", demoted: []string{"04"},
			pods: []SavedPod{f.idle(t, "01", "parking-1"), f.idle(t, "02", "garden-1"), f.idle(t, "03", "parking-2"), misplaced, returning},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "04", Idle, "harbor-1")
			},
		},
		{
			name: "phase ticks on a traveling pod", demoted: []string{"04"},
			pods: []SavedPod{f.idle(t, "01", "harbor-1"), f.idle(t, "02", "market-1"), f.idle(t, "03", "parking-1"), phased, returning},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "04", Idle, "parking-2")
			},
		},
		{
			name: "trimmed route near the origin", demoted: []string{"01"}, pods: []SavedPod{nearOrigin},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "01", Boarding, "harbor-1")
			},
		},
		{
			name: "no berth but last lane reserved", demoted: []string{"01"}, pods: []SavedPod{noBerth},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "01", Boarding, "harbor-1")
			},
		},
		{
			name: "no berth before the last lane", pods: []SavedPod{beforeLastLane},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				if v := findVehicle(t, s, "01"); v.Pod.Activity != Traveling || v.destination.ID != "" {
					t.Fatalf("pod 01 is %q with berth %q", v.Pod.Activity, v.destination.ID)
				}
			},
		},
		{
			name: "unknown route lane", demoted: []string{"04"},
			pods: []SavedPod{f.idle(t, "01", "harbor-1"), f.idle(t, "02", "market-1"), f.idle(t, "03", "parking-1"), unknownLane, returning},
			check: func(t *testing.T, _ SavedState, s *Simulation, result RestoreResult) {
				t.Helper()
				checkAtBerth(t, s, "04", Idle, "parking-2")
				if result.OverCap != 0 || result.OverBudget != 0 {
					t.Fatalf("result %+v", result)
				}
			},
		},
		{
			name: "empty lane ID stays empty",
			pods: []SavedPod{departed, f.idle(t, "05", "market-1")},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				v := findVehicle(t, s, "02")
				if v.Pod.LaneID != "" || v.Pod.Position != (Point{480, 70}) || berthOwner(s, "garden-1") != "02" {
					t.Fatalf("pod 02 is at %+v on lane %q", v.Pod.Position, v.Pod.LaneID)
				}
				s.Step()
				if v.Pod.LaneID != "garden-out" {
					t.Fatalf("pod 02 moved to lane %q", v.Pod.LaneID)
				}
			},
		},
		{
			name: "claim of a taken berth is dropped",
			pods: []SavedPod{f.idle(t, "03", "parking-2"), claimant},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				if owner := berthOwner(s, "parking-2"); owner != "03" || s.ExportState().Pods[3].ClaimsDestination {
					t.Fatalf("parking-2 belongs to %q", owner)
				}
			},
		},
		{
			name: "claim of a free berth is kept",
			pods: []SavedPod{f.idle(t, "02", "market-1"), f.idle(t, "05", "harbor-1"), f.idle(t, "01", "garden-1"), claimant},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				if owner := berthOwner(s, "parking-2"); owner != "04" || !s.ExportState().Pods[3].ClaimsDestination {
					t.Fatalf("parking-2 belongs to %q", owner)
				}
			},
		},
		{
			name: "claim in the last block is kept",
			pods: []SavedPod{f.idle(t, "02", "market-1"), f.idle(t, "05", "harbor-1"), f.idle(t, "01", "garden-1"), admitted},
			check: func(t *testing.T, _ SavedState, s *Simulation, _ RestoreResult) {
				t.Helper()
				v := findVehicle(t, s, "04")
				if owner := berthOwner(s, "parking-2"); owner != "04" || !v.blocks[v.reservedThrough].last ||
					!s.ExportState().Pods[3].ClaimsDestination {
					t.Fatalf("parking-2 belongs to %q", owner)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := f.state(tc.pods...)
			state.Waiting = tc.waiting
			for _, trip := range tc.waiting {
				state.RequestID += max(1, trip.Parties)
			}
			state = roundTripState(t, state)
			s, result, err := f.restore(state)
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, tc.demoted) {
				t.Fatalf("result %+v, want demoted %v", result, tc.demoted)
			}
			if !maps.Equal(s.owners, s.retainedOwners()) {
				t.Fatal("the owners differ from the retention rules")
			}
			tc.check(t, state, s, result)
		})
	}
}

func TestRestoreSeparatesTravelingPods(t *testing.T) {
	t.Parallel()
	// Move the bypass node next to the second parking berth. The lanes to the
	// two nodes then share no resource.
	network := Example()
	network.Nodes[slices.IndexFunc(network.Nodes, func(node Node) bool { return node.ID == "bypass" })].Position = Point{480, 372}
	f := newRestoreFixture(t, network)
	parker := relocating(f.traveling(t, travelInput{id: "03", from: "market-1", to: "parking-2", lane: "parking-in-2", distance: 164}))
	bypass := f.carrying(t, f.traveling(t, travelInput{id: "05", from: "harbor-1", to: "market-1", lane: "bypass-in", distance: 207}), 1, 1)
	idle := []SavedPod{f.idle(t, "01", "garden-1"), f.idle(t, "02", "market-1"), f.idle(t, "04", "parking-1")}
	// Restore each pod with the other pod at Harbor to find the resources and
	// the position that it needs.
	alone := make(map[string]*Simulation)
	for _, pods := range [][2]SavedPod{{parker, f.idle(t, "05", "harbor-1")}, {bypass, f.idle(t, "03", "harbor-1")}} {
		s, result, err := f.restore(f.state(append(slices.Clone(idle), pods[:]...)...))
		if err != nil || len(result.Demoted) > 0 {
			t.Fatalf("restore pod %s alone: %v, %+v", pods[0].ID, err, result)
		}
		alone[pods[0].ID] = s
	}
	for r, owner := range alone["03"].owners {
		if owner == "03" && alone["05"].owners[r] == "05" {
			t.Fatalf("pods 03 and 05 both need %+v", r)
		}
	}
	first, second := findVehicle(t, alone["03"], "03").Pod.Position, findVehicle(t, alone["05"], "05").Pod.Position
	if gap := math.Hypot(first.X-second.X, first.Y-second.Y); gap >= Clearance {
		t.Fatalf("the pods are %v meters apart", gap)
	}
	s, result, err := f.restore(f.state(append(slices.Clone(idle), parker, bypass)...))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Demoted, []string{"05"}) {
		t.Fatalf("demoted %v, want [05]", result.Demoted)
	}
	checkAtBerth(t, s, "05", Boarding, "harbor-1")
	if v := findVehicle(t, s, "03"); v.Pod.Activity != Traveling || v.Pod.LaneID != "parking-in-2" {
		t.Fatalf("pod 03 is %q on %q", v.Pod.Activity, v.Pod.LaneID)
	}
}

func TestRestorePicksTheSavedLaneAtABoundary(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	start := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "bypass-in", distance: 0}), 1, 1)
	end := start
	end.RouteIndex, end.LaneID = start.RouteIndex-1, "approach-branch"
	end.LaneDistance = f.s.laneLength(f.network.Lanes[start.Route[end.RouteIndex]])
	for _, tc := range []struct {
		name string
		pod  SavedPod
		// last tells whether the pod is in the last block of its lane.
		last bool
	}{
		{name: "start of the next lane", pod: start},
		{name: "end of the lane", pod: end, last: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, result, err := f.restore(f.state(tc.pod))
			if err != nil || len(result.Demoted) > 0 {
				t.Fatalf("%v, %+v", err, result)
			}
			v := findVehicle(t, s, "01")
			b := v.blocks[v.blockIndex]
			if b.lane.ID != tc.pod.LaneID || b.last != tc.last || b.cell != 0 && !tc.last {
				t.Fatalf("pod 01 is in cell %d of %s", b.cell, b.lane.ID)
			}
			// The export trims the lanes that the pod has passed, so it can
			// give a smaller route index for the same lane.
			saved := s.ExportState().Pods[0]
			if lane := f.network.Lanes[saved.Route[saved.RouteIndex]]; lane.ID != tc.pod.LaneID ||
				saved.LaneID != tc.pod.LaneID || saved.LaneDistance != tc.pod.LaneDistance {
				t.Fatalf("saved again as %+v", saved)
			}
		})
	}
}

// TestRestoreSnapsToBlockEnds restores a pod at positions that differ from a
// block end or from the saved distance in the last bits. The pod must hold
// the resources that it holds at the exact position.
func TestRestoreSnapsToBlockEnds(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	base := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "bypass-in"}), 1, 1)
	route := make([]Lane, len(base.Route))
	for position, index := range base.Route {
		route[position] = f.network.Lanes[index]
	}
	blocks := f.s.routeBlocks(route)
	first, _ := routeLaneBlocks(blocks, base.RouteIndex)
	laneStart, interior := blocks[first].laneStart, blocks[first+1]
	middle := (interior.start + interior.end) / 2
	for _, tc := range []struct {
		name string
		// laneDistance and distance are the saved position of the pod.
		laneDistance, distance float64
		// want is the restored route distance, in the block at index.
		want  float64
		index int
	}{
		{name: "block end", laneDistance: interior.end - laneStart, distance: interior.end, want: interior.end, index: first + 1},
		{
			name: "just before a block end", laneDistance: interior.end - laneStart - 1e-9, distance: interior.end - 1e-9,
			want: interior.end, index: first + 1,
		},
		{
			name: "just after a block end", laneDistance: interior.end - laneStart + 1e-9, distance: interior.end + 1e-9,
			want: interior.end, index: first + 1,
		},
		{name: "just after the lane start", laneDistance: 1e-9, distance: laneStart + 1e-9, want: blocks[first-1].end, index: first},
		{
			name: "lane distance off the saved distance", laneDistance: middle - laneStart + restoreTolerance/2, distance: middle,
			want: middle, index: first + 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pod := base
			pod.LaneDistance, pod.Distance = tc.laneDistance, tc.distance
			s, result, err := f.restore(f.state(pod))
			if err != nil || !cleanRestore(result) {
				t.Fatalf("%v, %+v", err, result)
			}
			v := findVehicle(t, s, "01")
			if v.distance != tc.want || v.blockIndex != tc.index || v.reservedThrough != reservationEnd(v.blocks, tc.index) {
				t.Fatalf("pod 01 at %v in block %d through %d, want %v in block %d", v.distance, v.blockIndex, v.reservedThrough, tc.want, tc.index)
			}
			held := make(map[resource]string)
			for _, claimed := range v.footprint(v.reservedThrough, tc.want) {
				held[claimed] = "01"
			}
			owned := maps.Clone(s.owners)
			maps.DeleteFunc(owned, func(_ resource, owner string) bool { return owner != "01" })
			if !maps.Equal(owned, held) {
				t.Fatalf("pod 01 holds %v, want %v", owned, held)
			}
		})
	}
}

// TestRestoreKeepsBerthWaits restores a pod at Harbor that waits to depart
// while pod 01 is on the Harbor departure lane.
func TestRestoreKeepsBerthWaits(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	ahead := relocating(f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "garden-1", lane: "harbor-out", distance: 20}))
	// The fixture caches routes, so build the pod before the parallel subtests.
	boarding := f.boarding(t, "02", "harbor-1")
	for _, tc := range []struct {
		name       string
		phaseTicks int
		// waits tells whether the restored pod waits for admission.
		waits bool
	}{
		{name: "ready pod keeps its wait", waits: true},
		{name: "pod that still boards does not wait", phaseTicks: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			waiting := boarding
			waiting.PhaseTicks, waiting.Waiting, waiting.WaitSince = tc.phaseTicks, true, restoreTick-50
			s, result, err := f.restore(roundTripState(t, f.state(ahead, waiting)))
			if err != nil || !cleanRestore(result) {
				t.Fatalf("%v, %+v", err, result)
			}
			v := findVehicle(t, s, "02")
			saved := s.ExportState().Pods[1]
			switch {
			case !tc.waits && (v.pending != -1 || saved.Waiting || saved.WaitSince != 0):
				t.Fatalf("pod 02 waits for block %d since %d", v.pending, v.waitSince)
			case tc.waits && (v.pending != 0 || v.waitSince != restoreTick-50 || !saved.Waiting || saved.WaitSince != restoreTick-50):
				t.Fatalf("pod 02 waits for block %d since %d, saved again as %+v", v.pending, v.waitSince, saved)
			}
			// Pod 01 still holds the departure lane, so the wait goes on.
			s.Step()
			if tc.waits && (v.pending != 0 || v.waitSince != restoreTick-50 || v.Pod.BlockedBy != "01") {
				t.Fatalf("after a step, pod 02 waits for block %d since %d behind %q", v.pending, v.waitSince, v.Pod.BlockedBy)
			}
		})
	}
}

func TestRestoreClearsInvalidTripBindings(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	route, err := f.s.stationApproachRoute("harbor-berth", "market")
	if err != nil {
		t.Fatal(err)
	}
	valid := SavedTrip{
		Request: SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 1, PodID: "03", RequestedTick: 500},
		Route:   f.s.laneIndexes(route, len(route)), DeferUntil: restoreTick + 100, DeferCheck: restoreTick + 30, DeferPodID: "04",
	}
	unbound := valid
	unbound.Request.PodID, unbound.Route, unbound.DeferCheck, unbound.DeferPodID = "", nil, 0, ""
	expired := unbound
	expired.DeferUntil = 0
	demoted := relocating(f.traveling(t, travelInput{id: "04", from: "garden-1", to: "parking-2", lane: "return-to-parking", distance: 100}))
	demoted.LaneID = "market-in"
	for _, tc := range []struct {
		name    string
		edit    func(*SavedTrip)
		pods    []SavedPod
		want    SavedTrip
		overCap int
	}{
		{name: "valid", edit: func(*SavedTrip) {}, want: valid},
		{name: "unknown pod", edit: func(trip *SavedTrip) { trip.Request.PodID = "09" }, want: unbound},
		{name: "demoted pod", edit: func(*SavedTrip) {}, pods: []SavedPod{f.idle(t, "02", "parking-2"), demoted}, want: unbound},
		{name: "unknown lane", edit: func(trip *SavedTrip) { trip.Route = []int{trip.Route[0], len(f.network.Lanes)} }, want: unbound},
		{name: "disconnected route", edit: func(trip *SavedTrip) { slices.Reverse(trip.Route) }, want: unbound},
		{
			name: "route over the limit", overCap: 1, want: unbound,
			edit: func(trip *SavedTrip) { trip.Route = slices.Repeat(trip.Route[:1], len(f.network.Nodes)+1) },
		},
		{name: "unknown deferral pod", edit: func(trip *SavedTrip) { trip.DeferPodID = "09" }, want: unbound},
		{name: "late deferral check", edit: func(trip *SavedTrip) { trip.DeferCheck = restoreTick + TicksPerSecond + 1 }, want: unbound},
		{name: "late deferral deadline", edit: func(trip *SavedTrip) { trip.DeferUntil = restoreTick + maxDispatchDeferral + 1 }, want: expired},
		{name: "negative deferral deadline", edit: func(trip *SavedTrip) { trip.DeferUntil = -1 }, want: expired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			trip := valid
			trip.Route = slices.Clone(valid.Route)
			tc.edit(&trip)
			state := f.state(tc.pods...)
			state.RequestID, state.Waiting = state.RequestID+1, []SavedTrip{trip}
			for _, tier := range restoreTiers {
				s, result, err := f.restoreTier(state, tier)
				if err != nil || result.Tier != tier {
					t.Fatalf("%s tier: %v, %+v", tier, err, result)
				}
				// The logical tier clears each binding and builds no route.
				want, overCap := tc.want, tc.overCap
				if tier == RestoreLogical {
					want.Request.PodID, want.Route, want.DeferCheck, want.DeferPodID, overCap = "", nil, 0, "", 0
				}
				if result.OverCap != overCap {
					t.Fatalf("%s tier: result %+v", tier, result)
				}
				if got := s.ExportState().Waiting; !reflect.DeepEqual(got, []SavedTrip{want}) {
					t.Fatalf("%s tier: queue\n got %+v\nwant %+v", tier, got, want)
				}
			}
		})
	}
}

func TestRestoreDropsInvalidTrips(t *testing.T) {
	t.Parallel()
	f := newRestoreFixture(t, Example())
	unloading := SavedPod{
		ID: "05", Activity: activityCode(Unloading), StationID: "market", BerthID: "market-1", Occupied: true,
		Request: &SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 1, PodID: "05", RequestedTick: 10},
		Parties: 1, PhaseTicks: unloadingTicks / 2, Origin: "harbor-1", Destination: "market-1", DestinationStation: "market",
	}
	for _, tc := range []struct {
		name string
		pods []SavedPod
		trip SavedTrip
		// parties counts the orders of the dropped trip.
		parties int
		// duplicate gives the dropped trip the ID of the trip before it.
		duplicate bool
		// requeued tells whether the logical tier puts the request of the
		// first pod back in the queue.
		requeued bool
	}{
		{
			name: "three parties from a parking station", parties: 3,
			trip: SavedTrip{Request: SavedRequest{ID: 1, From: "parking", To: "market", PartySize: 3, RequestedTick: 10}, Parties: 3},
		},
		{
			name: "request that a pod carries", parties: 1, pods: []SavedPod{f.boarding(t, "01", "harbor-1")}, requeued: true,
			trip: SavedTrip{Request: SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 1, RequestedTick: 10}},
		},
		{
			name: "request that an unloading pod carries", parties: 1, pods: []SavedPod{unloading},
			trip: SavedTrip{Request: SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 1, RequestedTick: 10}},
		},
		{
			name: "duplicate queued ID", parties: 1, duplicate: true,
			trip: SavedTrip{Request: SavedRequest{From: "harbor", To: "market", PartySize: 1, RequestedTick: 10}},
		},
		{
			name: "party of size zero", parties: 1,
			trip: SavedTrip{Request: SavedRequest{ID: 1, From: "harbor", To: "market", RequestedTick: 10}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := f.state(tc.pods...)
			kept := SavedTrip{Request: SavedRequest{ID: state.RequestID + tc.parties + 1, From: "garden", To: "harbor", PartySize: 1, RequestedTick: 20}}
			state.RequestID = kept.Request.ID
			if tc.duplicate {
				tc.trip.Request.ID = kept.Request.ID
			}
			state.Waiting = []SavedTrip{kept, tc.trip}
			if gap := state.ordersGap(); gap != 0 {
				t.Fatalf("the saved order gap is %d", gap)
			}
			for _, tier := range restoreTiers {
				s, result, err := f.restoreTier(state, tier)
				if err != nil || result.Tier != tier {
					t.Fatalf("%s tier: %v, %+v", tier, err, result)
				}
				// The request of a boarding pod goes back to the queue in front
				// of the saved trip with the same ID.
				var requeued []int
				want := []SavedTrip{kept}
				if tier == RestoreLogical && tc.requeued {
					trip := SavedTrip{Request: *tc.pods[0].Request, Parties: tc.pods[0].Parties}
					trip.Request.PodID = ""
					requeued, want = []int{trip.Request.ID}, []SavedTrip{trip, kept}
				}
				if !slices.Equal(result.Dropped, []int{tc.trip.Request.ID}) || result.DroppedParties != tc.parties ||
					!slices.Equal(result.Requeued, requeued) {
					t.Fatalf("%s tier: result %+v", tier, result)
				}
				if gap := s.ordersGap(); gap != tc.parties {
					t.Fatalf("%s tier: the order gap is %d, want %d", tier, gap, tc.parties)
				}
				if got := s.ExportState().Waiting; !reflect.DeepEqual(got, want) {
					t.Fatalf("%s tier: queue %+v", tier, got)
				}
			}
		})
	}
}

// TestRestoreFailsForInvalidState restores states that the physical tier
// does not accept. The logical tier restores some of them.
func TestRestoreFailsForInvalidState(t *testing.T) {
	t.Parallel()
	example := newRestoreFixture(t, Example())
	// Move the second parking berth within Clearance of the first.
	near := Example()
	near.Nodes[slices.IndexFunc(near.Nodes, func(node Node) bool { return node.ID == "parking-berth-2" })].Position = Point{480, 412}
	crowded := newRestoreFixture(t, near)
	// Pod 01 has just left Harbor and claims the second parking berth. With
	// the other berths taken, the demoted pod 04 finds no berth.
	departing := relocating(example.traveling(t, travelInput{id: "01", from: "harbor-1", to: "parking-2", lane: "harbor-out", distance: 5}))
	departing.ClaimsDestination = true
	misplaced := relocating(example.traveling(t, travelInput{id: "04", from: "garden-1", to: "parking-1", lane: "return-to-parking", distance: 100}))
	misplaced.LaneID = "market-in"
	boarding := example.boarding(t, "02", "garden-1")
	withBoarding := func(edit func(*SavedPod)) func(*SavedState) {
		return func(state *SavedState) {
			pod := boarding
			pod.Request = new(*boarding.Request)
			edit(&pod)
			state.Pods[1] = pod
			state.RequestID, state.Boarded = 1, 1
		}
	}
	for _, tc := range []struct {
		name    string
		fixture restoreFixture
		edit    func(*SavedState)
		// want is a part of the error of the physical tier.
		want string
		// logical tells whether the logical tier restores the state.
		logical bool
	}{
		{
			name: "two pods at one berth", fixture: example, want: "are at berth", logical: true,
			edit: func(state *SavedState) { state.Pods[0] = example.idle(t, "01", "garden-1") },
		},
		{name: "two pods at berths within clearance", fixture: crowded, edit: func(*SavedState) {}, want: "restored pods at berths"},
		{
			name: "unknown berth", fixture: example, want: "unknown berth", logical: true,
			edit: func(state *SavedState) { state.Pods[2].BerthID = "parking-9" },
		},
		{
			name: "unloading without a request", fixture: example, want: "no active request", logical: true,
			edit: func(state *SavedState) { state.Pods[4].Activity = activityCode(Unloading) },
		},
		{
			name: "missing fleet pod", fixture: example, want: "not in the saved state", logical: true,
			edit: func(state *SavedState) { state.Pods = state.Pods[:4] },
		},
		{
			name: "pods out of order", fixture: example, want: "out of order",
			edit: func(state *SavedState) { state.Pods[0], state.Pods[1] = state.Pods[1], state.Pods[0] },
		},
		{
			name: "no free berth for a demoted pod", fixture: example, want: "no berth is free", logical: true,
			edit: func(state *SavedState) {
				state.Pods[0], state.Pods[1] = departing, example.idle(t, "02", "garden-1")
				state.Pods[3], state.Pods[4] = misplaced, example.idle(t, "05", "market-1")
			},
		},
		{
			name: "negative phase", fixture: example, want: "out of range", logical: true,
			edit: withBoarding(func(pod *SavedPod) { pod.PhaseTicks = -1 }),
		},
		{
			name: "phase longer than boarding", fixture: example, want: "out of range", logical: true,
			edit: withBoarding(func(pod *SavedPod) { pod.PhaseTicks = boardingTicks + 1 }),
		},
		{
			name: "boarding route with an unknown lane", fixture: example, want: "unknown lane", logical: true,
			edit: withBoarding(func(pod *SavedPod) { pod.Route = []int{len(example.network.Lanes)} }),
		},
		{
			name: "boarding route to another station", fixture: example, want: "does not connect", logical: true,
			edit: withBoarding(func(pod *SavedPod) { pod.Route = pod.Route[:len(pod.Route)-1] }),
		},
		{name: "request with no party", fixture: example, edit: withBoarding(func(pod *SavedPod) { pod.Request.PartySize = 0 }), want: "not valid"},
		{
			name: "two pods carry one request", fixture: example, want: "pod 02 carries request 1",
			edit: func(state *SavedState) {
				state.Pods[0], state.Pods[1] = example.boarding(t, "01", "harbor-1"), boarding
				state.RequestID, state.Boarded = 2, 2
			},
		},
		{
			name: "empty departure without a station", fixture: example, want: "no station to relocate to", logical: true,
			edit: func(state *SavedState) { state.Pods[2].Activity = activityCode(DepartingEmpty) },
		},
		{
			name: "wait after the saved tick", fixture: example, want: "out of range", logical: true,
			edit: withBoarding(func(pod *SavedPod) { pod.Waiting, pod.WaitSince = true, restoreTick+1 }),
		},
		{name: "more parties than orders", fixture: example, edit: withBoarding(func(pod *SavedPod) { pod.Parties = 2 }), want: "more parties"},
		{
			name: "more completed than submitted", fixture: example, want: "completed or boarded more orders",
			edit: func(state *SavedState) { state.Completed = state.RequestID + 1 },
		},
		{
			name: "too many pods", fixture: example, want: "201 pods",
			edit: func(state *SavedState) {
				state.Pods = make([]SavedPod, maxSavedPods+1)
				for index := range state.Pods {
					state.Pods[index] = SavedPod{ID: fmt.Sprintf("%03d", index), Activity: activityCode(Idle)}
				}
			},
		},
		{
			name: "pod not in the fleet", fixture: example, want: "not in the fleet",
			edit: func(state *SavedState) { state.Pods = append(state.Pods, example.idle(t, "06", "market-1")) },
		},
		{name: "demo without the demo fleet", fixture: example, edit: func(state *SavedState) { state.Demo = &SavedDemo{} }, want: "demo fleet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := tc.fixture.state()
			tc.edit(&state)
			s, result, err := tc.fixture.restore(state)
			if result.PhysicalError == nil || !strings.Contains(result.PhysicalError.Error(), tc.want) {
				t.Fatalf("physical tier error %v, want %q", result.PhysicalError, tc.want)
			}
			if tc.logical {
				if err != nil || s == nil || result.Tier != RestoreLogical {
					t.Fatalf("the logical tier failed: %v, %+v", err, result)
				}
				return
			}
			if err == nil || s != nil || result.Tier != "" || !errors.Is(err, result.PhysicalError) {
				t.Fatalf("error %v with %+v", err, result)
			}
		})
	}
}

// cycleNetwork joins station s, with a berth every 20 meters, to station t
// through two long lanes. The lanes form a cycle of about 250 blocks.
func cycleNetwork(berths int) Network {
	network := Network{
		Nodes: []Node{
			{ID: "s-entry", Position: Point{0, 0}}, {ID: "s-exit", Position: Point{400, 0}},
			{ID: "t-entry", Position: Point{3400, 0}}, {ID: "t-berth", Position: Point{3450, 100}},
			{ID: "t-exit", Position: Point{3500, 0}},
		},
		Lanes: []Lane{
			{ID: "s-through", From: "s-entry", To: "s-exit", SpeedLimit: 14, StationID: "s", StationRole: StationThroughRole},
			{ID: "outbound", From: "s-exit", To: "t-entry", SpeedLimit: 14},
			{ID: "t-through", From: "t-entry", To: "t-exit", SpeedLimit: 14, StationID: "t", StationRole: StationThroughRole},
			{ID: "t-in", From: "t-entry", To: "t-berth", SpeedLimit: 14, StationID: "t", StationRole: StationBerthAccessRole},
			{ID: "t-out", From: "t-berth", To: "t-exit", SpeedLimit: 14, StationID: "t", StationRole: StationDepartureRole},
			{ID: "inbound", From: "t-exit", To: "s-entry", SpeedLimit: 14, Control: &Point{1750, -1500}},
		},
		Stations: []Station{
			{ID: "s", Name: "S", Entry: "s-entry", Exit: "s-exit"},
			{ID: "t", Name: "T", Entry: "t-entry", Exit: "t-exit", Berths: []Berth{{ID: "t-1", Node: "t-berth"}}},
		},
	}
	for index := range berths {
		node := fmt.Sprintf("s-berth-%d", index)
		network.Nodes = append(network.Nodes, Node{ID: node, Position: Point{float64(10 + 20*index), 100}})
		network.Lanes = append(network.Lanes,
			Lane{ID: "s-in-" + node, From: "s-entry", To: node, SpeedLimit: 14, StationID: "s", StationRole: StationBerthAccessRole},
			Lane{ID: "s-out-" + node, From: node, To: "s-exit", SpeedLimit: 14, StationID: "s", StationRole: StationDepartureRole},
		)
		network.Stations[0].Berths = append(network.Stations[0].Berths, Berth{ID: fmt.Sprintf("s-%d", index), Node: node})
	}
	return network
}

// TestRestoreLimitsRouteBlocks restores pods whose routes repeat a long
// cycle up to the route limit. Their blocks are many times the budget.
func TestRestoreLimitsRouteBlocks(t *testing.T) {
	t.Parallel()
	const pods = 20
	network := cycleNetwork(pods)
	fleet := make([]Placement, pods)
	for index := range fleet {
		fleet[index] = Placement{ID: fmt.Sprintf("%02d", index+1), StationID: "s", BerthID: fmt.Sprintf("s-%d", index)}
	}
	f := newRestoreFleetFixture(t, network, fleet)
	lane := func(id string) int { return f.s.graph.lanes[id] }
	cycle := []int{lane("outbound"), lane("t-through"), lane("inbound"), lane("s-through")}
	limit := newRouteLimits(network).pod
	var saved []SavedPod
	costs := make(map[string]int)
	// With these route lengths, the kept routes leave room in the budget for
	// only some of the demoted passenger pods to board again.
	for index, placement := range fleet {
		cycles := (limit-2)/len(cycle) - index%9
		route := append(slices.Repeat(cycle, cycles), lane("outbound"), lane("t-in"))
		pod := SavedPod{
			ID: placement.ID, Activity: activityCode(Traveling), Origin: placement.BerthID, Destination: "t-1",
			DestinationStation: "t", Route: route, LaneID: "outbound", LaneDistance: float64(100 + 140*index),
			Distance: float64(100 + 140*index),
		}
		if index%2 == 0 {
			pod = f.carrying(t, pod, index/2+1, 1)
		} else {
			pod = relocating(pod)
		}
		saved = append(saved, pod)
		costs[pod.ID] = savedStateCost(network, SavedState{Pods: []SavedPod{pod}})
	}
	state := f.state(saved...)
	budget := blockBudget(network)
	if cost := savedStateCost(network, state); cost <= 2*budget {
		t.Fatalf("the saved routes have %d blocks, want more than twice the budget of %d", cost, budget)
	}
	s, result, err := f.restore(state)
	if err != nil {
		t.Fatal(err)
	}
	blocks := 0
	var kept []string
	for index := range s.vehicles {
		v := &s.vehicles[index]
		blocks += len(v.blocks)
		if v.Pod.Activity == Traveling {
			kept = append(kept, v.Pod.ID)
		}
	}
	t.Logf("blocks %d of %d, kept %v, demoted %v, requeued %v, over budget %d",
		blocks, budget, kept, result.Demoted, result.Requeued, result.OverBudget)
	if blocks > budget || len(kept) == 0 || len(result.Requeued) == 0 {
		t.Fatalf("the restore built %d blocks for the budget of %d, kept %v and requeued %v", blocks, budget, kept, result.Requeued)
	}
	// Each refused boarding counts as over budget, as does each demotion.
	if result.OverBudget != len(result.Demoted)+len(result.Requeued) {
		t.Fatalf("over budget %d, demoted %d, requeued %d", result.OverBudget, len(result.Demoted), len(result.Requeued))
	}
	// The restore demotes the pods with the most blocks first, with pod ID
	// order for equal costs.
	order := func(a, b string) int { return cmp.Or(cmp.Compare(costs[b], costs[a]), cmp.Compare(a, b)) }
	for _, demoted := range result.Demoted {
		for _, id := range kept {
			if order(demoted, id) > 0 {
				t.Fatalf("the restore demoted pod %s with %d blocks and kept pod %s with %d", demoted, costs[demoted], id, costs[id])
			}
		}
	}
}

// cycleRoute returns the last entries of a route that goes around the cycle
// of cycleNetwork and then to berth t-1.
func cycleRoute(s *Simulation, entries int) []int {
	lane := func(id string) int { return s.graph.lanes[id] }
	cycle := []int{lane("outbound"), lane("t-through"), lane("inbound"), lane("s-through")}
	route := append(slices.Repeat(cycle, entries/len(cycle)+1), lane("outbound"), lane("t-in"))
	return route[len(route)-entries:]
}

// TestRestoreDemotesRoutesOverTheLimit restores saved pod routes at and over
// the route limit. TestExportStateLimitsRoutes restores the routes that
// ExportState leaves out.
func TestRestoreDemotesRoutesOverTheLimit(t *testing.T) {
	t.Parallel()
	network := cycleNetwork(2)
	f := newRestoreFleetFixture(t, network, []Placement{{ID: "01", StationID: "s", BerthID: "s-0"}, {ID: "02", StationID: "s", BerthID: "s-1"}})
	limit := newRouteLimits(network).pod
	// travel returns pod 01 on the first outbound lane of a route.
	travel := func(route []int) SavedPod {
		routeIndex := slices.Index(route, f.s.graph.lanes["outbound"])
		distance := 100.0
		for _, lane := range route[:routeIndex] {
			distance += f.s.laneLength(network.Lanes[lane])
		}
		return relocating(SavedPod{
			ID: "01", Activity: activityCode(Traveling), Origin: "s-0", Destination: "t-1", DestinationStation: "t",
			Route: route, RouteIndex: routeIndex, LaneID: "outbound", LaneDistance: 100, Distance: distance,
		})
	}
	for _, tc := range []struct {
		name    string
		entries int
		// overCap tells whether the restore demotes the pod.
		overCap bool
	}{
		{name: "route at the limit", entries: limit},
		{name: "route over the limit", entries: limit + 1, overCap: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, result, err := f.restore(roundTripState(t, f.state(travel(cycleRoute(f.s, tc.entries)))))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.overCap {
				if v := findVehicle(t, s, "01"); !cleanRestore(result) || v.Pod.Activity != Traveling || len(v.Route) != limit {
					t.Fatalf("pod 01 is %q with %d lanes, result %+v", v.Pod.Activity, len(v.Route), result)
				}
				return
			}
			if !slices.Equal(result.Demoted, []string{"01"}) || result.OverCap != 1 || result.OverBudget != 0 {
				t.Fatalf("result %+v", result)
			}
			checkAtBerth(t, s, "01", Idle, "t-1")
		})
	}
}

// TestRestoreLimitsTripRoutes restores a queue whose routes alone are over
// the block budget. The restore demotes each pod with a route, and then
// clears the longest trip routes first.
func TestRestoreLimitsTripRoutes(t *testing.T) {
	t.Parallel()
	network := Example()
	f := newRestoreFixture(t, network)
	budget := blockBudget(network)
	// The main line of the example network is a cycle of 11 lanes.
	var cycle []int
	for _, id := range []string{
		"harbor-through", "approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-through",
		"return-start", "return-to-parking", "parking-through", "return", "harbor-approach",
	} {
		cycle = append(cycle, f.s.graph.lanes[id])
	}
	limit := newRouteLimits(network).trip
	var trips []SavedTrip
	for cost := 0; cost <= budget+4*limit; {
		entries := 1 + len(trips)%limit
		trips = append(trips, SavedTrip{
			Request: SavedRequest{ID: len(trips) + 1, From: "harbor", To: "market", PartySize: 1, RequestedTick: 10},
			Route:   slices.Repeat(cycle, 2)[:entries],
		})
		cost += entries
	}
	// A trip with a short route and the first trip with the longest route
	// have pods. A trip keeps its pod when the restore clears its route.
	trips[0].Request.PodID, trips[limit-1].Request.PodID = "05", "02"
	state := f.state(relocating(f.traveling(t, travelInput{id: "04", from: "garden-1", to: "parking-2", lane: "return-to-parking", distance: 100})))
	state.RequestID, state.Waiting = len(trips), trips
	s, result, err := f.restore(roundTripState(t, state))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Demoted, []string{"04"}) {
		t.Fatalf("demoted %v, want [04]", result.Demoted)
	}
	var cleared, kept []int
	cost := 0
	for index, trip := range s.waiting {
		if trip.request.ID != trips[index].Request.ID || trip.request.PodID != trips[index].Request.PodID {
			t.Fatalf("queued trip %d is %+v, want %+v", index, trip.request, trips[index].Request)
		}
		if trip.route == nil {
			cleared = append(cleared, index)
			continue
		}
		kept = append(kept, index)
		cost += len(trip.route)
	}
	t.Logf("%d trips, cleared %d, cost %d of %d", len(trips), len(cleared), cost, budget)
	if cost > budget || len(cleared) < 2 || !slices.Contains(cleared, limit-1) || result.OverBudget != 1+len(cleared) {
		t.Fatalf("cost %d of %d, cleared %v, over budget %d", cost, budget, cleared, result.OverBudget)
	}
	// Each cleared route is longer than each kept route, or as long and
	// earlier in the queue. The restore clears no more routes than it must.
	for _, c := range cleared {
		for _, k := range kept {
			if order := cmp.Or(cmp.Compare(len(trips[k].Route), len(trips[c].Route)), cmp.Compare(c, k)); order > 0 {
				t.Fatalf("the restore cleared trip %d with %d lanes and kept trip %d with %d", c, len(trips[c].Route), k, len(trips[k].Route))
			}
		}
	}
	shortest := slices.MinFunc(cleared, func(a, b int) int { return cmp.Compare(len(trips[a].Route), len(trips[b].Route)) })
	if cost+len(trips[shortest].Route) <= budget {
		t.Fatalf("the restore cleared trip %d, but the cost fits without that", shortest)
	}
}
