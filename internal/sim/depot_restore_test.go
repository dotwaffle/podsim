package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// depotContracts are the contracts of the depot restore tests, so that a
// save holds the faults.
// depotFaultSettings are the fault settings of the depot restore tests.
var depotFaultSettings = FaultSettings{EvacuationSeconds: 300}

// newDepotFleet returns a fleet on a depot network with the fault
// operations on.
func newDepotFleet(t *testing.T, network Network, placements []Placement) *Simulation {
	t.Helper()
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, depotFaultSettings); err != nil {
		t.Fatal(err)
	}
	return s
}

// restoreDepot restores the saved state of s, and checks that the restore
// keeps every pod and order in place and matches s. With no fault, the state
// takes a strict JSON round trip first. The session adapter saves the pod of
// a fault record, which the plain JSON of SavedFault omits.
func restoreDepot(t *testing.T, network Network, s *Simulation) *Simulation {
	t.Helper()
	state := s.ExportState()
	if len(s.faults) == 0 {
		state = roundTripState(t, state)
	}
	restored, result, err := RestoreState(RestoreStateInput{
		FaultsOn: true, Faults: depotFaultSettings,
		Network: network, Fleet: s.initial, State: state,
	})
	if err != nil || !cleanRestore(result) {
		t.Fatalf("tick %d: restore %+v, error %v", s.tick, result, err)
	}
	checkRestoredMatches(t, s, restored)
	for i := range s.vehicles {
		if live := &s.vehicles[i]; s.depotParked(live) && restored.vehicles[i].parkedSince != live.parkedSince {
			t.Fatalf("tick %d: pod %s has the parked tick %d after the restore, want %d", s.tick, live.Pod.ID, restored.vehicles[i].parkedSince, live.parkedSince)
		}
	}
	return restored
}

// depotPodKinds names the kinds of depot pods that a restore must keep.
var depotPodKinds = []string{
	"idle", "moving up", "bound with its slot", "bound without its slot", "unbound at the entry", "departing to the entry",
	"departing", "faulted",
}

// depotPodKind returns the kind of a pod of a depot, or "".
func (s *Simulation) depotPodKind(v *vehicle) string {
	_, bound := s.depots.slot(v.destination.ID)
	last := resource{kind: trackResource, id: s.depots.depots[0].incoming.ID, cell: s.laneCells[s.depots.depots[0].incoming.ID].count() - 1}
	switch {
	case v.faulted && (bound || s.depotParked(v)):
		return "faulted"
	case v.Pod.Activity == Idle && s.depotParked(v):
		return "idle"
	case s.depotParked(v):
		return "moving up"
	case bound && s.owners[resource{kind: berthResource, id: v.destination.ID}].isPod(v.Pod.ID):
		return "bound with its slot"
	case bound:
		return "bound without its slot"
	case s.depotUnbound(v, "parking") && s.owners[last].isPod(v.Pod.ID):
		return "unbound at the entry"
	case v.Pod.Activity == DepartingEmpty && s.depotUnbound(v, "parking"):
		return "departing to the entry"
	case v.Pod.Activity != Idle && s.depots.isDepot(v.origin.ID) || v.Pod.Activity != Idle && v.Pod.StationID == "parking":
		return "departing"
	}
	if _, ok := s.depots.slot(v.origin.ID); ok && v.Pod.Activity != Idle && !v.originReleased {
		return "departing"
	}
	return ""
}

// TestDepotRestore restores the state of a congested depot run at many
// ticks, from the tick at which three pods leave for the depot. Each
// restore must keep every pod in place and match the live simulation, and
// the samples must hold each kind of depot pod.
func TestDepotRestore(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 6))
	for _, id := range []string{"harbor-pod", "garden-pod", "market-pod"} {
		sendToDepot(t, s, id)
	}
	stations := []string{"harbor", "garden", "market"}
	const demandEnd = 6 * 60 * TicksPerSecond
	seconds := int64(10)
	seen := make(map[string]int)
	// sample restores the state when it holds a pod of a short-lived kind,
	// and at some other ticks.
	sample := func() {
		kinds := make(map[string]bool)
		for i := range s.vehicles {
			if kind := s.depotPodKind(&s.vehicles[i]); kind != "" {
				kinds[kind] = true
			}
		}
		if kinds["unbound at the entry"] || kinds["departing to the entry"] || kinds["moving up"] && s.tick%5 == 0 || s.tick%41 == 0 {
			restoreDepot(t, network, s)
			for kind := range kinds {
				seen[kind]++
			}
		}
	}
	sample()
	trips := 0
	stepDepot(t, s, 60*60*TicksPerSecond, func() bool {
		if s.tick < demandEnd && s.tick%(8*TicksPerSecond) == 0 {
			if _, err := s.SubmitTrip(stations[trips%3], stations[(trips+1+trips/3%2)%3]); err != nil {
				t.Fatal(err)
			}
			trips++
		}
		if s.tick < demandEnd && s.tick%(45*TicksPerSecond) == 0 {
			for i := range s.vehicles {
				if v := &s.vehicles[i]; !v.faulted && v.inService() && (s.depotParked(v) || s.depotPodKind(v) == "bound without its slot") {
					if _, err := s.Fault(FaultRequest{PodID: v.Pod.ID, DurationSeconds: &seconds}); err != nil {
						t.Fatal(err)
					}
					break
				}
			}
		}
		sample()
		return s.tick > demandEnd && len(s.faults) == 0 && s.completed == trips
	})
	for _, kind := range depotPodKinds {
		if seen[kind] == 0 {
			t.Errorf("no restore with a pod %s: %v", kind, seen)
		}
	}
}

// restoreDepotDeparture checks that v departs empty to the depot entry
// with no destination berth, and that both restore tiers accept the saved
// state. The physical restore must match s.
func restoreDepotDeparture(t *testing.T, network Network, s *Simulation, v *vehicle) {
	t.Helper()
	if v.Pod.Activity != DepartingEmpty || v.destination.ID != "" || v.RelocatingTo != "parking" || s.depotPodKind(v) != "departing to the entry" {
		t.Fatalf("pod %s is %s to %s %q", v.Pod.ID, v.Pod.Activity, v.RelocatingTo, v.destination.ID)
	}
	restoreDepot(t, network, s)
	_, result, err := RestoreState(RestoreStateInput{
		FaultsOn: true, Faults: depotFaultSettings,
		Network: network, Fleet: s.initial, State: s.ExportState(), LogicalOnly: true,
	})
	if err != nil || result.Tier != RestoreLogical {
		t.Fatalf("logical restore %+v, error %v", result, err)
	}
}

// TestDepotDepartureRestore restores a pod that departs empty to the entry
// of a depot station, for each path that starts such a move. The pod has
// no destination berth until it binds a slot at the entry.
func TestDepotDepartureRestore(t *testing.T) {
	t.Parallel()
	t.Run("empty move", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(2, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 6))
		restoreDepotDeparture(t, network, s, sendToDepot(t, s, "harbor-pod"))
	})
	t.Run("blocking pod", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(2, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 1))
		v := s.findVehicle("garden-pod")
		if !s.park(v) {
			t.Fatal("no parking move")
		}
		restoreDepotDeparture(t, network, s, v)
	})
	t.Run("guarded bump", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(2, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 1))
		v := s.findVehicle("market-pod")
		if !s.guardedBumpToParking(guardedBump{blocker: v, from: "market-berth"}) {
			t.Fatal("no bump")
		}
		restoreDepotDeparture(t, network, s, v)
	})
	t.Run("released pod", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(1, 3)
		s := newDepotFleet(t, network, depotPlacements(network, 0))
		// The pods at market and garden trade berths, so each passenger
		// berth is held or named (see TestDepotReleasedPark).
		v, other := s.findVehicle("market-pod"), s.findVehicle("garden-pod")
		if err := s.startEmptyMove(v, emptyDestination{station: "garden", berth: Berth{ID: "garden-1", Node: "garden-berth"}}); err != nil {
			t.Fatal(err)
		}
		if err := s.startEmptyMove(other, emptyDestination{station: "market", berth: Berth{ID: "market-1", Node: "market-berth"}}); err != nil {
			t.Fatal(err)
		}
		v.released = true
		s.parkUnclaimedReleased()
		restoreDepotDeparture(t, network, s, v)
	})
}

// The route of harbor-pod in doublePassFleet passes the incoming lane of
// the depot at route index firstPass and at secondPass. Route index
// firstPass+1 is parking-through.
const (
	firstPass  = 7
	secondPass = 18
)

// doublePassFleet returns a fleet on a depot network with harbor-pod on its
// way to a depot slot, on a route that passes the incoming lane of the
// depot two times.
func doublePassFleet(t *testing.T) (Network, *Simulation, *vehicle) {
	t.Helper()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 2))
	lanes := func(ids ...string) []Lane {
		var route []Lane
		for _, id := range ids {
			route = append(route, s.network.Lanes[s.graph.lanes[id]])
		}
		return route
	}
	toDepot := lanes("approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-through", "return-start", "return-to-parking")
	loop := lanes("parking-through", "return", "harbor-approach", "harbor-through")
	v := s.findVehicle("harbor-pod")
	s.installEmptyMove(v, emptyDestination{station: "parking", depot: true}, slices.Concat(lanes("harbor-out"), toDepot, loop, toDepot))
	return network, s, v
}

// TestDepotDoublePassRestore restores a pod whose route passes the
// incoming lane return-to-parking of the depot two times: it goes on to
// parking-through, around the network, and back to the incoming lane. The
// pod binds its slot when it holds the last cell of the first pass,
// because its route then ends at the entry. In the live route, its entry
// is the second pass. The test saves the pod in the last cell of the first
// pass, with its grant on parking-through, first at speed and then stopped
// in place.
//
// A whole save restores with the live entry rule (see depotEntryBlock),
// and the restore keeps the live frontier and owners, at speed and at
// rest. With the entry of the first pass, the saved lead passes the entry,
// and the pod restores at rest short of its grant. A trimmed save and the
// restore before the saved motion use the entry of the first pass, as
// version 13 does. They restore the pod at rest at the end of the first
// pass, without the cells of parking-through, as version 13 restores the
// same live pod.
func TestDepotDoublePassRestore(t *testing.T) {
	t.Parallel()
	network, s, v := doublePassFleet(t)
	_, entry := routeLaneBlocks(&v.blocks, firstPass)
	for tick := 0; tick < 6000 && (v.blockIndex != entry || v.destination.ID == ""); tick++ {
		s.Step()
	}
	if v.blockIndex != entry || v.destination.ID == "" || v.blocks.lane(v.reservedThrough).ID != "parking-through" || v.Pod.Speed == 0 {
		t.Fatalf("the pod is in block %d with the slot %q, the frontier on %s, at %v m/s, want the bound pod moving in block %d with its grant on parking-through",
			v.blockIndex, v.destination.ID, v.blocks.lane(v.reservedThrough).ID, v.Pod.Speed, entry)
	}
	input := func(state SavedState) RestoreStateInput {
		return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: roundTripState(t, state)}
	}
	whole := func(t *testing.T) {
		t.Helper()
		state := s.ExportState()
		if pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })]; pod.RouteTrimmed || pod.ReservedAhead != 0 {
			t.Fatalf("the pod saves the marker %t and %d blocks ahead, want a whole route and no lead", pod.RouteTrimmed, pod.ReservedAhead)
		}
		restored, result, err := RestoreState(input(state))
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
			t.Fatalf("restore %v, %+v, want the pod in place", err, result)
		}
		if got := restored.findVehicle(v.Pod.ID); got.reservedThrough != v.reservedThrough || !maps.Equal(restored.owners, s.owners) {
			t.Fatalf("the restored frontier is %d, want the live frontier %d, and the owners must be the live owners", got.reservedThrough, v.reservedThrough)
		}
	}
	whole(t)
	v.Pod.Speed = 0
	whole(t)
	// Version 13 restores the pod at the end of the first pass, and the
	// pod holds no cell of parking-through.
	want := maps.Clone(s.owners)
	maps.DeleteFunc(want, func(r resource, owner resourceOwner) bool {
		return r.kind == trackResource && r.id == "parking-through" && owner.isPod(v.Pod.ID)
	})
	if len(want) != len(s.owners)-2 {
		t.Fatalf("the pod holds %d cells of parking-through, want 2", len(s.owners)-len(want))
	}
	earlier, _, err := headRestore(input(s.ExportState()))
	if err != nil {
		t.Fatal(err)
	}
	if got := earlier.findVehicle(v.Pod.ID); got.reservedThrough != entry || !maps.Equal(earlier.owners, want) {
		t.Fatalf("the restore before the saved motion gives the frontier %d, want the first entry %d, with the owners of version 13", got.reservedThrough, entry)
	}
	state := s.exportTrimmed()
	pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })]
	restored, result, err := RestoreState(input(state))
	if err != nil || !pod.RouteTrimmed || pod.ReservedAhead != 2 || result.Tier != RestorePhysical || len(result.Demoted) != 0 ||
		!slices.Contains(result.AtRest, v.Pod.ID) || result.EarlierRestore {
		t.Fatalf("the save with the marker %t and %d blocks ahead restores %v, %+v, want a trimmed route with 2 blocks ahead and the pod at rest in place",
			pod.RouteTrimmed, pod.ReservedAhead, err, result)
	}
	got := restored.findVehicle(v.Pod.ID)
	if start := len(v.Route) - len(got.Route); got.reservedThrough+v.blocks.laneFirst(start) != entry || !maps.Equal(restored.owners, want) {
		t.Fatalf("the trimmed save gives the frontier %d from route index %d, want the first entry %d, with the owners of version 13", got.reservedThrough, start, entry)
	}
	// Each restored pod holds its reservation up to the first entry. In the
	// whole frame, its route derives a frontier past that entry. The export
	// saves its trimmed route with the marker, and the next restore keeps
	// the capped frontier and owners.
	for name, capped := range map[string]*Simulation{"before the saved motion": earlier, "trimmed": restored} {
		pod := capped.findVehicle(v.Pod.ID)
		state := capped.ExportState()
		if saved := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })]; !saved.RouteTrimmed || saved.ReservedAhead != 0 {
			t.Fatalf("the restore %s saves the marker %t and %d blocks ahead, want a trimmed route and no lead", name, saved.RouteTrimmed, saved.ReservedAhead)
		}
		again, result, err := RestoreState(RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: roundTripState(t, state)})
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
			t.Fatalf("the save of the restore %s restores %v, %+v", name, err, result)
		}
		got := again.findVehicle(v.Pod.ID)
		if start := len(pod.Route) - len(got.Route); got.reservedThrough+pod.blocks.laneFirst(start) != pod.reservedThrough || !maps.Equal(again.owners, capped.owners) {
			t.Fatalf("the save of the restore %s gives the frontier %d from route index %d, want %d, with the capped owners", name, got.reservedThrough, start, pod.reservedThrough)
		}
	}
}

// resaveFixpoint saves source, restores the save, and then saves and
// restores that restore, with no Step. Each restore must give the pod id
// the block and the frontier of source, and the owners of source. The
// second save must equal the first, so the restore of a restore is the
// restore. input makes the restore input of a save.
func resaveFixpoint(t *testing.T, source *Simulation, id string, input func(SavedState) RestoreStateInput) (first SavedState) {
	t.Helper()
	want := source.findVehicle(id)
	var saves []SavedState
	from := source
	for range 2 {
		state := from.ExportState()
		saves = append(saves, state)
		restored, result, err := RestoreState(input(state))
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
			t.Fatalf("save %d restores %v, %+v, want the pod in place", len(saves), err, result)
		}
		got := restored.findVehicle(id)
		base := want.blocks.laneFirst(len(want.Route) - len(got.Route))
		if got.blockIndex+base != want.blockIndex || got.reservedThrough+base != want.reservedThrough || !maps.Equal(restored.owners, source.owners) {
			t.Fatalf("save %d restores the pod in block %d with the frontier %d, want %d and %d, with the owners of the source",
				len(saves), got.blockIndex+base, got.reservedThrough+base, want.blockIndex, want.reservedThrough)
		}
		from = restored
	}
	if !reflect.DeepEqual(saves[0], saves[1]) {
		t.Fatalf("the save of the restore differs from the save of the source")
	}
	return saves[0]
}

// TestDepotFirstEntryLead restores a trimmed save of the pod of
// doublePassFleet, stopped on its first pass of the incoming lane, with a
// lead that ends at the end of that lane. The trimmed frame takes that end
// as the entry, so the restore accepts the frontier. The whole frame takes
// the last pass as the entry, and the restore refuses a frontier at the
// end of the first pass, which is not the end of a grant. So the save of
// the restore keeps the trimmed route with the marker.
func TestDepotFirstEntryLead(t *testing.T) {
	t.Parallel()
	network, s, v := doublePassFleet(t)
	for tick := 0; tick < 6000 && (v.blocks.routeLane(v.blockIndex) != firstPass || v.destination.ID == ""); tick++ {
		s.Step()
	}
	_, entry := routeLaneBlocks(&v.blocks, firstPass)
	if v.blocks.routeLane(v.blockIndex) != firstPass || v.destination.ID == "" || v.reservedThrough <= entry {
		t.Fatalf("the pod is on route lane %d with the slot %q and the frontier %d, want it bound on route lane %d with its grant past %d",
			v.blocks.routeLane(v.blockIndex), v.destination.ID, v.reservedThrough, firstPass, entry)
	}
	input := func(state SavedState) RestoreStateInput {
		return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: roundTripState(t, state)}
	}
	state := s.exportTrimmed()
	index := slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })
	lead := state.Pods[index].ReservedAhead - (v.reservedThrough - entry)
	if lead <= 0 {
		t.Fatalf("the trimmed save has %d blocks ahead, want the derived frontier before the entry %d", state.Pods[index].ReservedAhead, entry)
	}
	state = withPod(state, index, func(pod *SavedPod) { pod.Speed, pod.ReservedAhead = 0, lead })
	source, result, err := RestoreState(input(state))
	if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 {
		t.Fatalf("the trimmed save restores %v, %+v", err, result)
	}
	got := source.findVehicle(v.Pod.ID)
	if start := len(v.Route) - len(got.Route); got.reservedThrough+v.blocks.laneFirst(start) != entry {
		t.Fatalf("the trimmed save restores the frontier %d, want the first entry %d", got.reservedThrough+v.blocks.laneFirst(start), entry)
	}
	saved := resaveFixpoint(t, source, v.Pod.ID, input)
	if pod := saved.Pods[index]; !pod.RouteTrimmed || pod.ReservedAhead != lead {
		t.Fatalf("the save of the restore has the marker %t and %d blocks ahead, want a trimmed route with %d", pod.RouteTrimmed, pod.ReservedAhead, lead)
	}
}

// TestDepotEntryRules pins each depot entry rule on the route of
// doublePassFleet, with the pod bound on parking-through after its first
// pass.
func TestDepotEntryRules(t *testing.T) {
	t.Parallel()
	_, s, v := doublePassFleet(t)
	for tick := 0; tick < 6000 && (v.blocks.routeLane(v.blockIndex) != firstPass+1 || v.destination.ID == ""); tick++ {
		s.Step()
	}
	if v.blocks.routeLane(v.blockIndex) != firstPass+1 || v.destination.ID == "" {
		t.Fatalf("the pod is on route lane %d with the slot %q, want it bound on route lane %d", v.blocks.routeLane(v.blockIndex), v.destination.ID, firstPass+1)
	}
	_, first := routeLaneBlocks(&v.blocks, firstPass)
	_, second := routeLaneBlocks(&v.blocks, secondPass)
	for _, test := range []struct {
		from, current int
		rule          depotEntryRule
		want          int
	}{
		{0, v.blockIndex, lastEntry, second},
		{secondPass + 1, v.blockIndex, lastEntry, -1},
		{0, v.blockIndex, firstEntry, first},
		{firstPass + 1, v.blockIndex, firstEntry, second},
		{0, v.blockIndex, aheadEntry, second},
		{0, first, aheadEntry, first},
		{secondPass + 1, first, aheadEntry, -1},
	} {
		got, ok := s.depotEntryBlock(v, test.from, test.current, test.rule)
		if !ok {
			got = -1
		}
		if got != test.want {
			t.Fatalf("the rule %d from route index %d at block %d gives the entry %d, want %d", test.rule, test.from, test.current, got, test.want)
		}
	}
	// A whole route keeps the entry of its last pass in each frame. A
	// trimmed route keeps the first entry of the saved route, as version
	// 13 did, so the pod past that entry has no entry.
	whole, trimmed := SavedPod{}, SavedPod{RouteTrimmed: true}
	for _, test := range []struct {
		name  string
		frame positionFrame
		want  int
	}{
		{"whole", primaryPolicy(whole).frame(whole), second},
		{"whole before the saved motion", earlierPlacement.frame(whole), second},
		{"trimmed", primaryPolicy(trimmed).frame(trimmed), -1},
		{"trimmed before the saved motion", earlierPlacement.frame(trimmed), -1},
	} {
		if through, entry := s.derivedFrontier(v, firstPass, v.blockIndex, false, test.frame); through != reservationEnd(&v.blocks, v.blockIndex) || entry != test.want {
			t.Fatalf("the %s frame derives the frontier %d and the entry %d, want %d and %d", test.name, through, entry, reservationEnd(&v.blocks, v.blockIndex), test.want)
		}
	}
}

// TestDepotLeftFirstPass saves the pod of doublePassFleet bound on
// parking-through after its first pass, with no lead and then with a lead
// of 3 blocks. The trimmed save restores the live frontier and owners, as
// version 13 did. The restore before the saved motion of the whole save
// reads no lead, so it restores the derived frontier.
func TestDepotLeftFirstPass(t *testing.T) {
	t.Parallel()
	network, s, v := doublePassFleet(t)
	derived := -1
	for _, lead := range []int{0, 3} {
		for tick := 0; tick < 6000 && (v.blocks.routeLane(v.blockIndex) != firstPass+1 || v.destination.ID == "" || v.reservedThrough-derived != lead); tick++ {
			s.Step()
			if derived < 0 && v.destination.ID != "" && v.blocks.routeLane(v.blockIndex) == firstPass+1 {
				derived = v.reservedThrough
			}
		}
		if v.blocks.routeLane(v.blockIndex) != firstPass+1 || v.destination.ID == "" || v.reservedThrough-derived != lead {
			t.Fatalf("the pod is on route lane %d with the slot %q and the frontier %d, want it bound on route lane %d with %d blocks ahead of %d",
				v.blocks.routeLane(v.blockIndex), v.destination.ID, v.reservedThrough, firstPass+1, lead, derived)
		}
		input := func(state SavedState) RestoreStateInput {
			return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: roundTripState(t, state)}
		}
		state := s.exportTrimmed()
		if pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })]; !pod.RouteTrimmed || pod.RouteIndex != 1 || pod.ReservedAhead != lead {
			t.Fatalf("the trimmed save has the marker %t, route index %d, and %d blocks ahead, want a trimmed route from the first pass with %d blocks ahead",
				pod.RouteTrimmed, pod.RouteIndex, pod.ReservedAhead, lead)
		}
		restored, result, err := RestoreState(input(state))
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
			t.Fatalf("lead %d: the trimmed save restores %v, %+v", lead, err, result)
		}
		got := restored.findVehicle(v.Pod.ID)
		if through := got.reservedThrough + v.blocks.laneFirst(firstPass); through != derived+lead || !maps.Equal(restored.owners, s.owners) {
			t.Fatalf("lead %d: the trimmed save gives the frontier %d, want %d, with the live owners", lead, through, derived+lead)
		}
		earlier, _, err := headRestore(input(s.ExportState()))
		if err != nil {
			t.Fatal(err)
		}
		if got := earlier.findVehicle(v.Pod.ID); got.reservedThrough != derived {
			t.Fatalf("lead %d: the restore before the saved motion gives the frontier %d, want %d", lead, got.reservedThrough, derived)
		}
		for r, owner := range earlier.owners {
			if s.owners[r] != owner {
				t.Fatalf("lead %d: the restore before the saved motion gives %v to %v, not a live owner", lead, r, owner)
			}
		}
	}
	if derived != 46 {
		t.Fatalf("the derived frontier is %d, want 46", derived)
	}
}

// TestDepotSecondPassFault saves the pod of doublePassFleet faulted at the
// end of its second pass on the incoming lane, bound to its slot, with its
// reservation up to the entry, and debris on parking-through. The check of
// the fault footprints places each faulted pod before the saved motion. On
// a whole route, that placement takes the next entry ahead of the pod, so
// the pod holds nothing past the entry. Debris on parking-through clear of
// the entry node is then legal, and debris that meets it is not.
func TestDepotSecondPassFault(t *testing.T) {
	t.Parallel()
	network, s, v := doublePassFleet(t)
	for tick := 0; tick < 12000 && (v.blocks.routeLane(v.blockIndex) != secondPass || v.destination.ID == ""); tick++ {
		s.Step()
	}
	if v.blocks.routeLane(v.blockIndex) != secondPass || v.destination.ID == "" {
		t.Fatalf("the pod is on route lane %d with the slot %q, want it bound on route lane %d", v.blocks.routeLane(v.blockIndex), v.destination.ID, secondPass)
	}
	if _, err := s.startPodFaultTicks(v, 100*TicksPerSecond); err != nil {
		t.Fatal(err)
	}
	_, entry := routeLaneBlocks(&v.blocks, secondPass)
	if reservationEnd(&v.blocks, entry) <= entry {
		t.Fatalf("the reservation of the entry block %d ends at %d, want past the entry", entry, reservationEnd(&v.blocks, entry))
	}
	base := s.ExportState()
	index := slices.IndexFunc(base.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })
	base = withPod(base, index, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneDistance, pod.Distance = secondPass, s.laneLength(v.Route[secondPass]), v.blocks.end(entry)
		pod.Speed, pod.ReservedAhead = 0, 0
	})
	if pod := base.Pods[index]; pod.RouteTrimmed || pod.Destination == "" || pod.ClaimsDestination {
		t.Fatalf("saved pod %+v, want a whole route bound to a slot that it does not claim", pod)
	}
	for _, test := range []struct {
		from, to float64
		legal    bool
	}{{39, 40, true}, {37, 38, false}} {
		input := RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: withDebris(t, network, base, "parking-through", test.from, test.to)}
		if !test.legal {
			// The logical tier places no pod, so only the check of the fault
			// footprints before both tiers refuses the save there.
			for _, logicalOnly := range []bool{false, true} {
				input.LogicalOnly = logicalOnly
				if _, result, err := RestoreState(input); err == nil || !strings.Contains(err.Error(), "the pod holds a resource of a debris footprint") {
					t.Fatalf("debris from %v m restores %v, %+v with LogicalOnly %t, want the debris footprint error", test.from, err, result, logicalOnly)
				}
			}
			continue
		}
		restored, result, err := RestoreState(input)
		if err != nil || !cleanRestore(result) || result.EarlierRestore {
			t.Fatalf("debris from %v m restores %v, %+v, want the pod in place", test.from, err, result)
		}
		if got := restored.findVehicle(v.Pod.ID); got.blockIndex != entry || got.reservedThrough != entry {
			t.Fatalf("debris from %v m restores the pod in block %d with the frontier %d, want both at the entry %d", test.from, got.blockIndex, got.reservedThrough, entry)
		}
	}
}

// TestDepotDepartureOverLimit departs a pod to the depot entry on a route
// that circles the network until it is longer than the saved route limit.
// The saved state leaves out the route. The physical tier must demote the
// pod to a berth, and the logical tier must accept the state.
func TestDepotDepartureOverLimit(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 6))
	lanes := func(ids ...string) []Lane {
		var route []Lane
		for _, id := range ids {
			route = append(route, s.network.Lanes[s.graph.lanes[id]])
		}
		return route
	}
	// toDepot runs from the exit of harbor to the depot entry, and loop
	// goes on from there back to the exit of harbor.
	toDepot := lanes("approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-through", "return-start", "return-to-parking")
	loop := lanes("parking-through", "return", "harbor-approach", "harbor-through")
	route := lanes("harbor-out")
	for len(route) <= newRouteLimits(s.network).pod {
		route = append(append(route, toDepot...), loop...)
	}
	route = append(route, toDepot...)
	v := s.findVehicle("harbor-pod")
	s.installEmptyMove(v, emptyDestination{station: "parking", depot: true}, route)
	state := roundTripState(t, s.ExportState())
	if pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "harbor-pod" })]; pod.Route != nil ||
		pod.Activity != activityCode(DepartingEmpty) || pod.Destination != "" || pod.RelocatingTo != "parking" {
		t.Fatalf("saved pod %+v", pod)
	}
	for _, logicalOnly := range []bool{false, true} {
		restored, result, err := RestoreState(RestoreStateInput{
			FaultsOn: true, Faults: depotFaultSettings,
			Network: network, Fleet: s.initial, State: state, LogicalOnly: logicalOnly,
		})
		if err != nil {
			t.Fatalf("logical only %t: restore %+v, error %v", logicalOnly, result, err)
		}
		if logicalOnly {
			continue
		}
		if result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"harbor-pod"}) || result.OverCap != 1 {
			t.Fatalf("restore %+v", result)
		}
		checkAtBerth(t, restored, "harbor-pod", Idle, "harbor-1")
	}
}

// TestDepotDepartureRefusals checks that a pod that departs empty with no
// destination berth is refused, with the text and at the place of the
// refusal of any such pod, unless it goes to the entry of a depot station.
// A traveling pod with a refuge needs a destination berth, also when its
// route ends at a depot entry.
func TestDepotDepartureRefusals(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(2, 3)
	s := newDepotFleet(t, network, depotPlacements(network, 6))
	sendToDepot(t, s, "harbor-pod")
	saved := s.ExportState()
	index := slices.IndexFunc(saved.Pods, func(p SavedPod) bool { return p.ID == "harbor-pod" })
	lane := func(id string) int { return s.graph.lanes[id] }
	noDepot := network
	noDepot.Depots = nil
	const noBerth = "pod harbor-pod: the pod has no destination berth"
	tests := []struct {
		name    string
		network Network
		change  func(*SavedPod)
		want    string
	}{
		{"no depot in the network", noDepot, func(*SavedPod) {}, noBerth},
		{"a passenger station", network, func(p *SavedPod) {
			p.RelocatingTo, p.DestinationStation = "garden", "garden"
			p.Route = []int{lane("harbor-out"), lane("approach-branch"), lane("garden-approach")}
		}, noBerth},
		{"a route short of the entry", network, func(p *SavedPod) { p.Route = p.Route[:len(p.Route)-1] }, noBerth},
		{"a route lane out of range", network, func(p *SavedPod) { p.Route[len(p.Route)-1] = len(network.Lanes) }, noBerth},
		{"no relocation", network, func(p *SavedPod) { p.RelocatingTo = "" }, "pod harbor-pod: the empty move does not go to the destination station"},
	}
	for _, test := range tests {
		state := saved
		state.Pods = slices.Clone(saved.Pods)
		state.Pods[index].Route = slices.Clone(state.Pods[index].Route)
		test.change(&state.Pods[index])
		if _, err := state.checkContract(test.network); err == nil || err.Error() != test.want {
			t.Errorf("%s: contract error %v, want %q", test.name, err, test.want)
		}
		for _, logicalOnly := range []bool{false, true} {
			_, result, err := RestoreState(RestoreStateInput{
				FaultsOn: true, Faults: depotFaultSettings,
				Network: test.network, Fleet: s.initial, State: state, LogicalOnly: logicalOnly,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("%s, logical only %t: restore %+v, error %v, want %q", test.name, logicalOnly, result, err, test.want)
			}
		}
	}
	refuge := saved.Pods[index]
	refuge.Activity, refuge.StationID, refuge.BerthID, refuge.Occupied = activityCode(Traveling), "", "", true
	refuge.RelocatingTo, refuge.Purpose = "", uint8(opRefuge)
	if !ruleForPod(refuge, phaseTravelingOccupied, network).hasDestination {
		t.Error("a refuge at a depot entry needs no destination berth")
	}
}

// TestDepotRestoreDemotion checks the slot of a demoted pod B. B takes the
// slot behind the rear of a lane where no pod moves, with the restore tick
// as its parked tick, and no slot in a lane where pod A moves, because A
// would have to pass B to reach its slot.
func TestDepotRestoreDemotion(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, []Placement{
		{ID: "harbor-pod", StationID: "harbor"}, {ID: "garden-pod", StationID: "garden"},
		{ID: "a-pod", StationID: "parking", BerthID: depotSlotID(1, 1)}, {ID: "b-pod", StationID: "parking", BerthID: depotSlotID(1, 2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		s.Step()
	}
	lanes := func(ids ...string) []int {
		var indexes []int
		for _, id := range ids {
			indexes = append(indexes, s.graph.lanes[id])
		}
		return indexes
	}
	// bind makes a saved pod travel on the incoming lane, bound to a slot
	// and with no grant after the entry. Its origin is the berth at garden.
	bind := func(p *SavedPod, slot int, route []int) {
		p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", "return-to-parking", 50, 50
		p.RelocatingTo, p.DestinationStation, p.Destination, p.Origin, p.Route, p.RouteIndex = "parking", "parking", depotSlotID(1, slot), "garden-1", route, 0
	}
	restore := func(t *testing.T, state SavedState) *vehicle {
		t.Helper()
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 1 || result.Demoted[0] != "b-pod" {
			t.Fatalf("restore %+v, error %v", result, err)
		}
		return restored.findVehicle("b-pod")
	}
	t.Run("open lane", func(t *testing.T) {
		t.Parallel()
		// A is idle at slot 1, and B names the tail slot with a route that
		// demotes.
		state := s.ExportState()
		b := &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "b-pod" })]
		bind(b, 3, lanes("return-to-parking", "parking-fan-1", "parking-in-1"))
		b.RouteIndex = -1
		v := restore(t, state)
		if v.Pod.Activity != Idle || v.Pod.BerthID != depotSlotID(1, 2) || v.parkedSince != state.Tick {
			t.Fatalf("B is at %s %s with the parked tick %d", v.Pod.StationID, v.Pod.BerthID, v.parkedSince)
		}
	})
	t.Run("moving lane", func(t *testing.T) {
		t.Parallel()
		// A travels to slot 1, and B names slot 2 with a route that
		// demotes.
		state := s.ExportState()
		a := &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "a-pod" })]
		b := &state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == "b-pod" })]
		bind(a, 1, lanes("return-to-parking", "parking-fan-1", "parking-in-1", "parking-chain-1-2", "parking-chain-1-1"))
		bind(b, 2, lanes("return-to-parking", "parking-fan-1", "parking-in-1", "parking-chain-1-2"))
		b.LaneID, b.RouteIndex = "return-start", -1
		if v := restore(t, state); v.Pod.BerthID != "market-1" {
			t.Fatalf("B is at %s %s", v.Pod.StationID, v.Pod.BerthID)
		}
	})
}

// TestDepotSavedRefusals checks the refusals of the depot fields of a saved
// state, their text, and their order, and that the lane order check accepts
// a rear pod that names a slot which the pod ahead still crosses.
func TestDepotSavedRefusals(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	s, err := NewFleet(network, depotPlacements(network, 1))
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		s.Step()
	}
	// pod returns the saved pod with an ID.
	pod := func(state *SavedState, id string) *SavedPod {
		for i := range state.Pods {
			if state.Pods[i].ID == id {
				return &state.Pods[i]
			}
		}
		t.Fatalf("no pod %s", id)
		return nil
	}
	lane := func(id string) int { return s.graph.lanes[id] }
	// bind makes a saved pod travel on a lane of the depot lane, bound to a
	// slot.
	bind := func(p *SavedPod, laneID string, distance float64, slot int) {
		route := []int{lane(laneID)}
		for _, next := range []string{"parking-in-1", "parking-chain-1-2", "parking-chain-1-1"} {
			if lane(next) > 0 && len(route) > 0 && s.network.Lanes[route[len(route)-1]].To == s.network.Lanes[lane(next)].From {
				route = append(route, lane(next))
			}
		}
		p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", laneID, distance, distance
		p.RelocatingTo, p.DestinationStation, p.Destination, p.Route, p.RouteIndex = "parking", "parking", depotSlotID(1, slot), route, 0
	}
	tests := []struct {
		name   string
		change func(*SavedState)
		want   string
	}{
		{"negative", func(state *SavedState) { pod(state, "depot-pod-01").ParkedSince = -1 }, `pod "depot-pod-01" has the negative parked tick -1`},
		{"after the saved tick", func(state *SavedState) { pod(state, "depot-pod-01").ParkedSince = 11 }, `pod "depot-pod-01" has the parked tick 11 after the saved tick 10`},
		{"first pod first", func(state *SavedState) {
			pod(state, "depot-pod-01").ParkedSince = 11
			pod(state, "harbor-pod").ParkedSince = -1
		}, `pod "depot-pod-01" has the parked tick 11 after the saved tick 10`},
		{"not parked", func(state *SavedState) { pod(state, "harbor-pod").ParkedSince = 5 }, `pod "harbor-pod" has a parked tick and is not parked in a depot lane`},
		{"range before place", func(state *SavedState) {
			pod(state, "depot-pod-01").ParkedSince = 11
			pod(state, "harbor-pod").ParkedSince = 5
		}, `pod "depot-pod-01" has the parked tick 11 after the saved tick 10`},
		{"one slot", func(state *SavedState) { bind(pod(state, "market-pod"), "parking-in-1", 1, 1) }, `depot station "parking" lane 1 has pods out of slot order`},
		{"reverse order", func(state *SavedState) {
			bind(pod(state, "market-pod"), "parking-in-1", 1, 3)
			bind(pod(state, "garden-pod"), "parking-fan-1", 1, 2)
		}, `depot station "parking" lane 1 has pods out of slot order`},
		{"place before order", func(state *SavedState) {
			bind(pod(state, "market-pod"), "parking-in-1", 1, 1)
			pod(state, "harbor-pod").ParkedSince = 5
		}, `pod "harbor-pod" has a parked tick and is not parked in a depot lane`},
		{"crossed slot", func(state *SavedState) {
			bind(pod(state, "market-pod"), "parking-chain-1-2", 1, 2)
			bind(pod(state, "garden-pod"), "parking-in-1", 1, 3)
		}, ""},
	}
	for _, test := range tests {
		state := s.ExportState()
		test.change(&state)
		got := checkSavedDepotFields(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		switch {
		case test.want == "" && got != nil:
			t.Errorf("%s: %v", test.name, got)
		case test.want != "" && (got == nil || got.Error() != test.want):
			t.Errorf("%s: got %v, want %q", test.name, got, test.want)
		}
		if test.want == "" {
			continue
		}
		if _, _, got := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state}); got == nil || got.Error() != test.want {
			t.Errorf("%s: restore got %v, want %q", test.name, got, test.want)
		}
	}
	// A network without a depot refuses each parked tick.
	plain, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	plain.Step()
	state := plain.ExportState()
	state.Pods[0].ParkedSince = state.Tick
	want := `pod "01" has a parked tick and is not parked in a depot lane`
	if _, _, err := RestoreState(RestoreStateInput{Network: Example(), Fleet: plain.initial, State: state}); err == nil || err.Error() != want {
		t.Errorf("no depot: restore got %v, want %q", err, want)
	}
}

// TestDepotSavedPlatoonLink checks that a saved platoon run that holds a
// depot lane fails the physical tier after the other link checks, and that
// the logical tier restores the state.
func TestDepotSavedPlatoonLink(t *testing.T) {
	t.Parallel()
	network := shortDepotNetwork(1, 3)
	// One speed limit lets the run reach the depot check.
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = depotSpeed
	}
	s, err := NewFleet(network, depotPlacements(network, 2))
	if err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	route := func(ids ...string) []int {
		var indexes []int
		for _, id := range ids {
			indexes = append(indexes, s.graph.lanes[id])
		}
		return indexes
	}
	out := route("parking-out-1", "parking-throat-1", "return", "harbor-approach", "harbor-in")
	for i := range state.Pods {
		p := &state.Pods[i]
		var distance float64
		switch p.ID {
		case "depot-pod-01":
			p.Origin, distance = depotSlotID(1, 1), 20
		case "depot-pod-02":
			p.Origin, distance = depotSlotID(1, 2), 5
			p.Platoon = &SavedPlatoonLink{Leader: "depot-pod-01", Lane: 0, LeaderLane: 0, Lanes: 1}
		default:
			continue
		}
		p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", "parking-out-1", distance, distance
		p.RelocatingTo, p.DestinationStation, p.Destination, p.Route, p.RouteIndex = "harbor", "harbor", "harbor-1", out, 0
	}
	for i := range state.Pods {
		if state.Pods[i].ID == "harbor-pod" {
			state.Pods[i].StationID, state.Pods[i].BerthID = "garden", "garden-1"
		}
		if state.Pods[i].ID == "garden-pod" {
			state.Pods[i].StationID, state.Pods[i].BerthID = "parking", depotSlotID(1, 1)
		}
	}
	_, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
	if err != nil || result.Tier != RestoreLogical || result.PhysicalError == nil || !strings.HasSuffix(result.PhysicalError.Error(), "pod depot-pod-02: the platoon run holds a lane of a depot") {
		t.Fatalf("restore %+v, error %v", result, err)
	}
}

// divertedAtEntry returns a fleet of a depot network in which a pod waits
// unbound at the full depot entry, and a trip command then diverts it to
// the harbor pickup. Each other pod is withdrawn, so only the waiting pod
// can take the trip. The redirect keeps the
// grant that ends at the old route end, and on the new route the derived
// frontier is past it (restore speed proposal, amendment of RL-D2).
func divertedAtEntry(t *testing.T) (Network, *Simulation, *vehicle) {
	t.Helper()
	network := shortDepotNetwork(2, 3)
	placements := depotPlacements(network, 0)
	for _, slot := range []string{depotSlotID(1, 1), depotSlotID(1, 2), depotSlotID(1, 3), depotSlotID(2, 1), depotSlotID(2, 2)} {
		placements = append(placements, Placement{ID: "pod-" + slot, StationID: "parking", BerthID: slot})
	}
	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.withdrawService(s.findVehicle("pod-"+depotSlotID(2, 2)), faultHold); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("pod-" + depotSlotID(2, 1))
	to := emptyDestination{station: "parking", depot: true}
	route, err := s.prepareEmptyMove(v, to)
	if err != nil {
		t.Fatal(err)
	}
	s.installEmptyMove(v, to, route)
	for tick := 0; tick < 3000*TicksPerSecond && v.Pod.WaitReason != ParkingUnavailable; tick++ {
		s.Step()
	}
	if v.Pod.WaitReason != ParkingUnavailable || v.destination.ID != "" || v.Pod.Speed != 0 {
		t.Fatalf("the pod waits for %q with the slot %q at %v m/s, want it unbound at rest at the entry", v.Pod.WaitReason, v.destination.ID, v.Pod.Speed)
	}
	for _, id := range []string{"pod-" + depotSlotID(1, 1), "pod-" + depotSlotID(1, 2), "pod-" + depotSlotID(1, 3), "harbor-pod", "garden-pod", "market-pod"} {
		if err := s.withdrawService(s.findVehicle(id), faultHold); err != nil {
			t.Fatal(err)
		}
	}
	through, end := v.reservedThrough, v.blocks.len()-1
	if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "garden", PartySize: 1}); err != nil {
		t.Fatal(err)
	}
	if v.destinationStation != "harbor" || v.reservedThrough != through || through != end || reservationEnd(&v.blocks, through) <= through || v.pending >= 0 {
		t.Fatalf("the trip sends the pod to %q with the frontier %d of %d, want it diverted to harbor with its grant at the old route end %d", v.destinationStation, v.reservedThrough, v.blocks.len(), end)
	}
	return network, s, v
}

// sameRestore restores state and checks that the pod v of live restores
// with the live block, frontier, and pending request, and that the
// restore has the owners owners. With exact, the pod also has its live
// route distance bit for bit. It returns the restored pod.
func sameRestore(t *testing.T, network Network, live *Simulation, v *vehicle, state SavedState, owners map[resource]resourceOwner, exact bool) *vehicle {
	t.Helper()
	restored, result, err := RestoreState(RestoreStateInput{FaultsOn: live.faultsOn, Faults: depotFaultSettings, Network: network, Fleet: live.initial, State: state})
	if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
		t.Fatalf("restore %v, %+v, want each pod in place", err, result)
	}
	got := restored.findVehicle(v.Pod.ID)
	base := v.blocks.laneFirst(len(v.Route) - len(got.Route))
	pending := got.pending
	if pending >= 0 {
		pending += base
	}
	if got.blockIndex+base != v.blockIndex || got.reservedThrough+base != v.reservedThrough || pending != v.pending || !maps.Equal(restored.owners, owners) {
		t.Fatalf("the pod restores in block %d with the frontier %d and the request %d, want %d, %d, and %d, with the owners that the test gives",
			got.blockIndex+base, got.reservedThrough+base, pending, v.blockIndex, v.reservedThrough, v.pending)
	}
	if exact && got.distance+v.blocks.lanes[len(v.Route)-len(got.Route)].start != v.distance {
		t.Fatalf("the pod restores at %v, want %v", got.distance, v.distance)
	}
	return got
}

// TestDepotDivertedGrant saves the pod of divertedAtEntry. After a grant
// past the old route end, the save has a whole route, and the restore
// gives the pod its live position bit for bit. Right after the trip
// command, the published lane distance of the pod is short of its route
// distance by more than restoreTolerance. A whole route that the test
// saves itself then restores the pod at its lane distance, behind its
// live route distance. That is why the export saves the whole route of a
// pod only when the restore decodes its live route distance (exact restore
// coordinates proposal, EC-D11). The export saves that pod with its
// trimmed route (see fitFrames and TestDepotDivertedFollowUp).
func TestDepotDivertedGrant(t *testing.T) {
	t.Parallel()
	t.Run("after the grant", func(t *testing.T) {
		t.Parallel()
		network, s, v := divertedAtEntry(t)
		through := v.reservedThrough
		for tick := 0; tick < 10*TicksPerSecond && v.reservedThrough == through; tick++ {
			s.Step()
		}
		if v.reservedThrough <= through {
			t.Fatalf("the pod holds the frontier %d, want a grant past %d", v.reservedThrough, through)
		}
		state := s.ExportState()
		if pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })]; pod.RouteTrimmed {
			t.Fatal("the save has the marker, want a whole route")
		}
		sameRestore(t, network, s, v, state, s.owners, true)
	})
	t.Run("a whole route after the trip command", func(t *testing.T) {
		t.Parallel()
		network, s, v := divertedAtEntry(t)
		lane := v.blocks.routeLane(v.blockIndex)
		if gap := v.distance - v.blocks.lanes[lane].start - v.Pod.LaneDistance; v.distance != 2011.7876799221262 || gap <= restoreTolerance {
			t.Fatalf("the pod is at %v with the lane distance %v, want 2011.7876799221262 and a gap of more than %v", v.distance, v.Pod.LaneDistance, restoreTolerance)
		}
		state := s.ExportState()
		index := slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })
		state = withPod(state, index, func(pod *SavedPod) {
			pod.Route, pod.RouteIndex, pod.RouteTrimmed = s.laneIndexes(v.Route, len(v.Route)), lane, false
			pod.Distance, pod.ReservedAhead = v.distance-v.blocks.lanes[0].start, 0
		})
		restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
			t.Fatalf("restore %v, %+v, want each pod in place", err, result)
		}
		if got := restored.findVehicle(v.Pod.ID); got.distance >= v.distance || got.blockIndex != v.blockIndex {
			t.Fatalf("the pod restores at %v in block %d, want behind %v in block %d", got.distance, got.blockIndex, v.distance, v.blockIndex)
		}
	})
}

// TestDepotDivertedFollowUp records the restores of the pod of
// divertedAtEntry that behave as in version 13 (exact restore coordinates
// proposal, EC-D10 and the note of 2026-11-23 in the restore speed
// proposal). The live pod keeps the grant that ends at the old route end,
// block 71, and the derived frontier of the new route is block 73. The
// export saves no negative lead, and fitFrames gives the pod its trimmed
// route. Each test asserts the current result and then skips.
//
//   - Right after the trip command, and over the block budget, the restore
//     gives the pod the frontier 73, with two cells of parking-through and
//     the parking-entry junction that the live pod does not hold. The pod
//     restores at its lane distance, behind its live route distance.
//   - With debris on parking-through that denies the grant past block 71,
//     the restored footprint meets the debris. The physical tier refuses
//     the valid save, and so does the check of the fault footprints when
//     the pod is faulted, before or after a tick.
func TestDepotDivertedFollowUp(t *testing.T) {
	t.Parallel()
	extra := []resource{
		{kind: trackResource, id: "parking-through", cell: 0}, {kind: trackResource, id: "parking-through", cell: 1},
		{kind: junctionResource, id: "parking-entry"},
	}
	for _, test := range []struct {
		name string
		save func(*testing.T, *Simulation) SavedState
	}{{"after the trip command", func(_ *testing.T, s *Simulation) SavedState { return s.ExportState() }}, {"over the block budget", func(t *testing.T, s *Simulation) SavedState {
		t.Helper()
		// A positive budget that the whole routes exceed.
		live, _ := s.WholeRouteBlocksForTest()
		if live <= 1 {
			t.Fatalf("the whole routes have %d blocks", live)
		}
		return s.exportState(live - 1)
	}}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network, s, v := divertedAtEntry(t)
			state := test.save(t, s)
			pod := state.Pods[slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })]
			restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
			if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || !pod.RouteTrimmed || pod.ReservedAhead != 0 {
				t.Fatalf("the save with the marker %t and %d blocks ahead restores %v, %+v, want a trimmed route with no lead in place", pod.RouteTrimmed, pod.ReservedAhead, err, result)
			}
			got := restored.findVehicle(v.Pod.ID)
			start := len(v.Route) - len(got.Route)
			base := v.blocks.laneFirst(start)
			want := maps.Clone(s.owners)
			for _, r := range extra {
				want[r] = podResourceOwner(v.Pod.ID)
			}
			if got.blockIndex+base != 71 || got.reservedThrough+base != 73 || v.reservedThrough != 71 || !maps.Equal(restored.owners, want) ||
				got.distance+v.blocks.lanes[start].start >= v.distance {
				t.Fatalf("the pod restores in block %d with the frontier %d at %v, want block 71 with the frontier 73 behind %v, and the live owners with %v",
					got.blockIndex+base, got.reservedThrough+base, got.distance, v.distance, extra)
			}
			t.Skip("follow-up: the restore gives the pod the frontier 73 past its live frontier 71")
		})
	}
	const refusal = "the pod holds a resource of a debris footprint"
	for _, test := range []struct {
		name         string
		fault, ticks bool
	}{{"with the grant denied", false, true}, {"faulted after the denial", true, true}, {"faulted before a tick", true, false}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network, s, v := divertedAtEntry(t)
			if err := s.SetFaults(true, depotFaultSettings); err != nil {
				t.Fatal(err)
			}
			if _, err := s.startDebris(laneIndex(t, s, "parking-through"), 39, 40, 600*TicksPerSecond); err != nil {
				t.Fatal(err)
			}
			if test.ticks {
				s.Step()
			}
			if test.fault {
				if _, err := s.startPodFaultTicks(v, 100*TicksPerSecond); err != nil {
					t.Fatal(err)
				}
			}
			if v.reservedThrough != 71 || test.ticks && !test.fault && v.pending < 0 {
				t.Fatalf("the pod holds the frontier %d with the request %d, want 71 and, with no fault, a denied request", v.reservedThrough, v.pending)
			}
			input := RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: s.ExportState()}
			for _, logicalOnly := range []bool{false, true} {
				input.LogicalOnly = logicalOnly
				_, result, err := RestoreState(input)
				// Without a pod fault, only the physical tier places the pod, so
				// the logical tier restores the save.
				if !test.fault && logicalOnly {
					if err != nil || result.Tier != RestoreLogical {
						t.Fatalf("the logical tier restores %v, %+v, want the save", err, result)
					}
					continue
				}
				if err == nil || !strings.Contains(err.Error(), refusal) {
					t.Fatalf("the save restores %v, %+v with LogicalOnly %t, want the debris footprint error", err, result, logicalOnly)
				}
			}
			t.Skip("follow-up: the restore refuses the valid save, because the restored frontier 73 meets the debris")
		})
	}
}

// TestRestartFollowUp records two restarts with no tick that behave as in
// version 13 (exact restore coordinates proposal, EC-D12). A restart with
// no tick is exact for a pod that a whole-route restore placed exactly,
// not for every placement.
//
//   - M1: the pod of doublePassFleet on a route that passes the depot entry
//     three times, stopped at the end of a block on its second pass, with a
//     grant on the parking-through after it. A save of its route from the
//     first lane, with the marker, restores in place: the first saved
//     entry is behind the pod. The pod is at the exact end of a block, so
//     the whole frame puts it in the next block, and the next save trims
//     the first pass away. The second pass is then the first saved entry,
//     and the next restore places the pod at rest, without the cells past
//     the entry.
//   - M3: a pod at rest at 59.99999999925 m on a lane of 120 m with four
//     cells, with the lane distance 59.9999989995 m, with the marker. The
//     first restore snaps the pod to the cell end at 60 m, and keeps the
//     lane distance. With no lead and with a lead of 1, the whole frame
//     decodes the lane distance, so the next save keeps the marker
//     (EC-D11), and its restore takes the lane distance, about 1e-6 m back.
//   - Depot order with no lane IDs: two pods bound to slots 2 and 3 of one
//     depot lane, stopped on its incoming lane at 60 m and 180 m, in a file
//     with no lane IDs (Astra review, round 14). The check of the input
//     finds no lane to check, so the first restore places both pods. The
//     next save has the lane IDs, and the next restore refuses the order.
//   - A negative lane distance at the route start: an empty pod on the
//     first lane out of its origin berth, at rest, with the route distance
//     2e-6 m and the lane distance -5e-7 m (Astra review, round 14). The
//     first restore places the pod at -5e-7 m, the next save has that route
//     distance, and the next restore moves the pod to a berth.
func TestRestartFollowUp(t *testing.T) {
	t.Parallel()
	t.Run("M1", func(t *testing.T) {
		t.Parallel()
		network, s, v := doublePassFleet(t)
		lanes := func(ids ...string) []Lane {
			var route []Lane
			for _, id := range ids {
				route = append(route, *findLane(&s.network, id))
			}
			return route
		}
		toDepot := lanes("approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-through", "return-start", "return-to-parking")
		loop := lanes("parking-through", "return", "harbor-approach", "harbor-through")
		s.installEmptyMove(v, emptyDestination{station: "parking", depot: true}, slices.Concat(lanes("harbor-out"), toDepot, loop, toDepot, loop, toDepot))
		_, last := routeLaneBlocks(&v.blocks, secondPass)
		for tick := 0; tick < 20000 && (v.blocks.routeLane(v.blockIndex) != secondPass || v.blocks.routeLane(v.reservedThrough) != secondPass+1 || v.blockIndex == last); tick++ {
			s.Step()
		}
		block, through := v.blockIndex, v.reservedThrough
		if v.blocks.routeLane(block) != secondPass || v.blocks.routeLane(through) != secondPass+1 || block == last || v.destination.ID == "" {
			t.Fatalf("the pod is in block %d with the frontier %d and the slot %q, want it bound in the second pass with its grant on parking-through", block, through, v.destination.ID)
		}
		state := s.ExportState()
		index := slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == v.Pod.ID })
		state = withPod(state, index, func(pod *SavedPod) {
			pod.RouteTrimmed, pod.Distance, pod.Speed, pod.ReservedAhead = true, v.blocks.end(block), 0, through-reservationEnd(&v.blocks, block)
			pod.LaneDistance = pod.Distance - v.blocks.lanes[secondPass].start
		})
		input := func(state SavedState) RestoreStateInput {
			return RestoreStateInput{FaultsOn: true, Faults: depotFaultSettings, Network: network, Fleet: s.initial, State: roundTripState(t, state)}
		}
		first, result, err := RestoreState(input(state))
		if got := first.findVehicle(v.Pod.ID); err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || got.blockIndex != block || got.reservedThrough != through {
			t.Fatalf("the save restores %v, %+v, want the pod in block %d with the frontier %d", err, result, block, through)
		}
		again := first.ExportState()
		pod := again.Pods[index]
		second, result, err := RestoreState(input(again))
		got := second.findVehicle(v.Pod.ID)
		base := v.blocks.laneFirst(secondPass)
		if err != nil || !pod.RouteTrimmed || len(v.Route)-len(pod.Route) != secondPass || !slices.Equal(result.AtRest, []string{v.Pod.ID}) || got.blockIndex+base != block || got.reservedThrough+base != block {
			t.Fatalf("the save of the restore from route index %d restores %v, %+v, with the frontier %d, want the pod at rest in block %d", len(v.Route)-len(pod.Route), err, result, got.reservedThrough+base, block)
		}
		t.Skipf("follow-up: the second restart releases the grant of blocks %d to %d past the depot entry", block+1, through)
	})
	t.Run("depot order with no lane IDs", func(t *testing.T) {
		t.Parallel()
		network := shortDepotNetwork(1, 3)
		s, err := NewFleet(network, depotPlacements(network, 1))
		if err != nil {
			t.Fatal(err)
		}
		for range 10 {
			s.Step()
		}
		state := s.ExportState()
		lane := func(id string) int { return s.graph.lanes[id] }
		bind := func(id string, slot int, distance float64) {
			index := slices.IndexFunc(state.Pods, func(p SavedPod) bool { return p.ID == id })
			route := []int{lane("return-to-parking"), lane("parking-fan-1"), lane("parking-in-1")}
			if slot == 2 {
				route = append(route, lane("parking-chain-1-2"))
			}
			state = withPod(state, index, func(p *SavedPod) {
				p.Origin = p.BerthID
				p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance = "traveling", "", "", "", distance, distance
				p.RelocatingTo, p.DestinationStation, p.Destination, p.Route, p.RouteIndex = "parking", "parking", depotSlotID(1, slot), route, 0
			})
		}
		bind("market-pod", 2, 60)
		bind("garden-pod", 3, 180)
		first, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil || !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore {
			t.Fatalf("the first restore %v, %+v, want each pod in place", err, result)
		}
		again := first.ExportState()
		for _, pod := range again.Pods {
			if (pod.ID == "market-pod" || pod.ID == "garden-pod") && pod.LaneID != "return-to-parking" {
				t.Fatalf("the next save has %s on the lane %q, want return-to-parking", pod.ID, pod.LaneID)
			}
		}
		const refusal = `depot station "parking" lane 1 has pods out of slot order`
		if _, _, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: again}); err == nil || !strings.Contains(err.Error(), refusal) {
			t.Fatalf("the second restore gives %v, want %q", err, refusal)
		}
		t.Skip("follow-up: the first restore places depot pods with no lane IDs out of slot order, and the next restore refuses the save")
	})
	t.Run("a negative lane distance at the route start", func(t *testing.T) {
		t.Parallel()
		network := Example()
		s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}})
		if err != nil {
			t.Fatal(err)
		}
		route, err := s.route("harbor-berth", "market-berth")
		if err != nil || route[0].ID != "harbor-out" {
			t.Fatalf("the route %v, %v, want a route from harbor-out", route, err)
		}
		state := withPod(s.ExportState(), 0, func(p *SavedPod) {
			p.Origin = p.BerthID
			p.Activity, p.StationID, p.BerthID, p.LaneID, p.LaneDistance, p.Distance, p.Speed = "traveling", "", "", route[0].ID, -5e-7, 2e-6, 0
			p.RelocatingTo, p.DestinationStation, p.Destination, p.Route, p.RouteIndex = "market", "market", "market-1", s.laneIndexes(route, len(route)), 0
		})
		first, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: state})
		if err != nil {
			t.Fatalf("the first restore %v", err)
		}
		if v := &first.vehicles[0]; !cleanRestore(result) || len(result.AtRest) != 0 || result.EarlierRestore || v.distance != -5e-7 {
			t.Fatalf("the first restore %+v places the pod at %v, want the saved motion at -5e-7 m", result, v.distance)
		}
		again := first.ExportState()
		if pod := again.Pods[0]; pod.Distance != -5e-7 || pod.RouteTrimmed {
			t.Fatalf("the next save has the distance %v and the marker %t, want a whole route at -5e-7 m", pod.Distance, pod.RouteTrimmed)
		}
		second, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: again})
		if err != nil {
			t.Fatalf("the second restore %v", err)
		}
		if v := &second.vehicles[0]; !slices.Equal(result.Demoted, []string{"01"}) || v.Pod.Activity == Traveling {
			t.Fatalf("the second restore %+v leaves the pod %v, want it demoted to a berth", result, v.Pod.Activity)
		}
		t.Skip("follow-up: the first restore places the pod at a negative route distance, and the next restore moves it to a berth")
	})
	for _, lead := range []int{0, 1} {
		t.Run(fmt.Sprintf("M3 with the lead %d", lead), func(t *testing.T) {
			t.Parallel()
			lanes := []geometryLane{{to: Point{X: 120}}, {to: Point{X: 240}}, {to: Point{X: 360}}}
			network := geometryNetwork(Point{}, lanes, 1)
			fleet, state := geometrySave(network, len(lanes), 1, 60, 0)
			state = withPod(state, 0, func(pod *SavedPod) {
				pod.RouteTrimmed, pod.Distance, pod.LaneDistance, pod.ReservedAhead = true, 59.99999999925, 59.9999989995, lead
			})
			first, _ := restoreMotion(t, network, fleet, state)
			if v := &first.vehicles[0]; v.distance != 60 || v.blockIndex != 1 || v.reservedThrough != 1+lead {
				t.Fatalf("the first restore is at %v in block %d with the frontier %d, want 60 m, block 1, and %d", v.distance, v.blockIndex, v.reservedThrough, 1+lead)
			}
			again := first.ExportState()
			second, result := restoreMotion(t, network, fleet, again)
			v := &second.vehicles[0]
			if !cleanRestore(result) || v.blockIndex != 1 || v.reservedThrough != 1+lead || !maps.Equal(second.owners, first.owners) {
				t.Fatalf("the second restore %+v is in block %d with the frontier %d, want block 1 with %d and the owners of the first", result, v.blockIndex, v.reservedThrough, 1+lead)
			}
			if !again.Pods[0].RouteTrimmed || v.distance != 59.9999989995 {
				t.Fatalf("the second save has the marker %t, and its restore is at %v, want a trimmed route at 59.9999989995 m", again.Pods[0].RouteTrimmed, v.distance)
			}
			t.Skip("follow-up: the second restart moves the pod from 60 m back to 59.9999989995 m")
		})
	}
}

// TestBudgetKeepsMarker saves the pod of M3 in TestRestartFollowUp after
// its first restore, and a second pod at 125 m with a lead of 1, with a
// positive block budget that their whole routes exceed. The whole frame
// does not decode the live route distance of the first pod, so fitFrames
// marks it. The second pod fits the whole frame, and fitFrames keeps its
// lead. The
// trimmed start of each pod is its start, so the budget pass keeps the
// marker and the lead, and does not fit either pod again.
func TestBudgetKeepsMarker(t *testing.T) {
	t.Parallel()
	lanes := []geometryLane{{to: Point{X: 120}}, {to: Point{X: 240}}, {to: Point{X: 360}}}
	network := geometryNetwork(Point{}, lanes, 2)
	fleet, state := geometrySave(network, len(lanes), 2, 60, 0)
	state = withPod(state, 0, func(pod *SavedPod) {
		pod.RouteTrimmed, pod.Distance, pod.LaneDistance = true, 59.99999999925, 59.9999989995
	})
	state = withPod(state, 1, func(pod *SavedPod) {
		pod.RouteIndex, pod.LaneID, pod.Distance, pod.LaneDistance, pod.ReservedAhead = 1, "l1", 125, 5, 1
	})
	s, _ := restoreMotion(t, network, fleet, state)
	limits := newRouteLimits(s.network)
	plan := s.planSavedRoutes(limits, math.MaxInt, nil)
	const budget = 1
	known := plan.pods[1]
	if route := plan.pods[0]; !route.marked || route.start != 0 || route.known || known.marked || !known.known || known.lead != 1 || known.start != 0 || plan.planBlocks(nil) <= budget {
		t.Fatalf("the whole plan %+v, %+v has %d blocks, want the first pod marked, the second with the lead 1, both from the start 0, and more than %d blocks",
			route, known, plan.planBlocks(nil), budget)
	}
	plan.trimRoutes()
	if route := plan.pods[0]; !route.marked || route.start != 0 || plan.pods[1] != known || plan.pods[1].lead != 1 {
		t.Fatalf("the trimmed plan %+v, %+v, want the first pod marked from the start 0, and the second plan %+v with the lead 1", route, plan.pods[1], known)
	}
	if got := s.exportState(budget); !got.Pods[0].RouteTrimmed || got.Pods[1].RouteTrimmed || got.Pods[1].ReservedAhead != 1 {
		t.Fatalf("the save over the budget has the markers %t and %t, and the second lead %d, want true, false, and 1",
			got.Pods[0].RouteTrimmed, got.Pods[1].RouteTrimmed, got.Pods[1].ReservedAhead)
	}
}
