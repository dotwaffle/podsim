package sim

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"
)

type cloneRule int

const (
	// cloneShare marks storage that code replaces whole and never writes in place.
	cloneShare cloneRule = iota + 1
	// cloneCopy marks storage that code writes in place. Clone gives it new storage.
	cloneCopy
	// cloneDrop marks a pure cache. Clone sets it to nil.
	cloneDrop
)

// cloneRules gives a rule for each field that holds references. Clone copies
// the other fields by value.
var cloneRules = map[reflect.Type]map[string]cloneRule{
	reflect.TypeFor[Simulation](): {
		"junctionConflicts": cloneShare, "lengths": cloneDrop, "routes": cloneDrop, "routeOrder": cloneDrop,
		"graph": cloneShare, "stationIndexes": cloneShare, "stationForbidden": cloneShare, "pickupBounds": cloneDrop, "routeWork": cloneDrop, "admissionWork": cloneDrop,
		"geometry": cloneShare, "network": cloneShare, "initial": cloneShare,
		"vehicles": cloneCopy, "owners": cloneCopy, "demo": cloneCopy, "waiting": cloneCopy,
		"demandWeights": cloneShare, "congestionRouteCosts": cloneShare, "congestionRoutes": cloneCopy,
		"laneSafety": cloneShare, "berthSafety": cloneShare, "vehicleIndexes": cloneShare,
		"berthResources": cloneShare, "laneCells": cloneShare, "approachStations": cloneShare, "routeStations": cloneDrop,
		"requestBoardings": cloneCopy, "requestCompletions": cloneCopy, "nodePasses": cloneCopy, "monitor": cloneShare,
		"pass": cloneDrop, "platoonData": cloneShare, "platoonOrder": cloneDrop, "platoonAhead": cloneDrop,
		"platoonLanes": cloneDrop, "pickupSwaps": cloneCopy,
	},
	reflect.TypeFor[vehicle](): {
		"Vehicle": cloneCopy, "blocks": cloneShare, "blockStarts": cloneShare, "routeReleases": cloneCopy,
		"routeLengths": cloneShare,
	},
	reflect.TypeFor[Vehicle]():              {"Riders": cloneCopy, "Stops": cloneCopy, "Route": cloneShare, "Presentation": cloneShare},
	reflect.TypeFor[waitingTrip]():          {"route": cloneShare},
	reflect.TypeFor[routeResult]():          {"lanes": cloneShare, "err": cloneShare},
	reflect.TypeFor[pickupSwapController](): {"cooldown": cloneCopy, "records": cloneCopy},
}

// clonePlainTypes hold only plain values, so a value copy of them is deep.
var clonePlainTypes = []reflect.Type{
	reflect.TypeFor[Request](), reflect.TypeFor[Pod](), reflect.TypeFor[Berth](),
	reflect.TypeFor[resource](), reflect.TypeFor[demoRun](), reflect.TypeFor[routeKey](),
	reflect.TypeFor[PickupReassignment](),
}

// holdsReferences reports whether a value copy of t shares storage with the
// original. Strings cannot change, so they count as plain values.
func holdsReferences(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return holdsReferences(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			if holdsReferences(field.Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// clonedElements returns the types that Clone copies by value when it copies
// a field of type t.
func clonedElements(t reflect.Type) []reflect.Type {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		return []reflect.Type{t.Elem()}
	case reflect.Map:
		return []reflect.Type{t.Key(), t.Elem()}
	default:
		return []reflect.Type{t}
	}
}

// fieldRuleCheck describes a rule table for the fields of one type.
type fieldRuleCheck struct {
	// kind names the rule table in errors.
	kind string
	typ  reflect.Type
	// names holds the fields that have a rule.
	names []string
	// needsRule reports whether a field must have a rule.
	needsRule func(reflect.StructField) bool
}

// checkFieldRules reports each field that needs a rule and has none, and each
// rule for a field that the type does not have.
func checkFieldRules(t *testing.T, check fieldRuleCheck) {
	t.Helper()
	fields := make(map[string]bool)
	for field := range check.typ.Fields() {
		fields[field.Name] = true
		if !slices.Contains(check.names, field.Name) && check.needsRule(field) {
			t.Errorf("%s.%s has no %s rule", check.typ.Name(), field.Name, check.kind)
		}
	}
	for _, name := range check.names {
		if !fields[name] {
			t.Errorf("%s has a %s rule for the missing field %s", check.typ.Name(), check.kind, name)
		}
	}
}

func TestCloneRulesCoverReferenceFields(t *testing.T) {
	t.Parallel()
	for typ, rules := range cloneRules {
		checkFieldRules(t, fieldRuleCheck{
			kind: "clone", typ: typ, names: slices.Collect(maps.Keys(rules)),
			needsRule: func(field reflect.StructField) bool { return holdsReferences(field.Type) },
		})
		for field := range typ.Fields() {
			if rules[field.Name] != cloneCopy {
				continue
			}
			for _, element := range clonedElements(field.Type) {
				if _, ruled := cloneRules[element]; !ruled && holdsReferences(element) {
					t.Errorf("Clone copies %s.%s one level deep, but %s holds references and has no clone rules", typ.Name(), field.Name, element)
				}
			}
		}
	}
	for _, typ := range clonePlainTypes {
		if holdsReferences(typ) {
			t.Errorf("%s holds references, so a value copy is not deep", typ.Name())
		}
	}
}

type persistRule int

const (
	// persistSave marks a field that ExportState writes and RestoreState reads.
	persistSave persistRule = iota + 1
	// persistDerive marks a field that RestoreState computes from the saved
	// fields or from the network.
	persistDerive
	// persistReset marks a field that RestoreState sets to its start value.
	persistReset
	// persistSession marks a field that the session gives or sets again.
	persistSession
	// persistUnsupported marks a setting that a restore does not keep. Only
	// experiments change it, so a restore uses the NewFleet value.
	persistUnsupported
)

// persistRules gives a rule for each field of the types that ExportState
// reads. A new field needs a rule, so its author decides how a restore keeps
// it.
var persistRules = map[reflect.Type]map[string]persistRule{
	reflect.TypeFor[Simulation](): {
		"junctionConflicts": persistDerive, "lengths": persistReset, "routes": persistReset, "routeOrder": persistReset,
		"graph": persistDerive, "stationIndexes": persistDerive, "stationForbidden": persistDerive, "pickupBounds": persistReset, "routeWork": persistReset, "admissionWork": persistReset,
		"geometry": persistDerive, "network": persistSession, "initial": persistSession,
		"vehicles": persistSave, "owners": persistDerive, "tick": persistSave, "paused": persistSave,
		"completed": persistSave, "requestID": persistSave, "demo": persistSave, "demoError": persistSave,
		"waiting": persistSave, "boarded": persistSave, "totalWaitTicks": persistSave, "maxWaitTicks": persistSave,
		"positioning": persistSession, "demandRate": persistSession, "demandWeights": persistSession, "nextRedistributionTick": persistSave,
		"passengerDistanceMeters": persistSave, "emptyDistanceMeters": persistSave, "rebalanceMoves": persistSave,
		"sharedRidePartyLimit": persistSave, "sharedParties": persistSave, "unaccountedOrders": persistDerive, "monitor": persistReset,
		"sharedRideMode": persistSave, "sharedRideMaxStops": persistSave, "sharedRideJoin": persistSave,
		"approachStations": persistDerive, "routeStations": persistReset,
		"journeys": persistSave, "totalJourneyTicks": persistSave, "maxJourneyTicks": persistSave,
		"riderDistanceMeters": persistSave, "directDistanceMeters": persistSave, "maxDetourRatio": persistSave,
		"laneSafety": persistDerive, "berthSafety": persistDerive, "vehicleIndexes": persistDerive, "berthResources": persistDerive,
		"laneCells":     persistDerive,
		"routingPolicy": persistUnsupported, "congestionRouteCosts": persistUnsupported,
		"congestionRoutes": persistUnsupported, "nextCongestionRouteRefresh": persistUnsupported,
		"reservationLookaheadSeconds": persistUnsupported, "finishingPodWait": persistUnsupported,
		"requestBoardings": persistReset, "requestCompletions": persistReset, "nodePasses": persistReset, "seatScreen": persistReset,
		"recordExperiments": persistUnsupported, "pass": persistReset,
		"platooning": persistSession, "platoonLimit": persistSession, "platoonLinks": persistDerive,
		"platoonData": persistDerive, "platoonOrder": persistReset, "platoonAhead": persistReset,
		"platoonLanes": persistReset, "stationBuffers": persistUnsupported, "pickupSwaps": persistUnsupported,
	},
	reflect.TypeFor[vehicle](): {
		"Vehicle": persistSave, "phaseTicks": persistSave, "blocks": persistDerive, "blockStarts": persistDerive,
		"routeReleases": persistDerive, "nextRelease": persistReset, "blockIndex": persistDerive, "reservedThrough": persistDerive,
		"originReleased": persistDerive, "distance": persistSave, "riddenBase": persistSave, "journeyOrigin": persistSave, "pending": persistDerive,
		"waitSince":      persistSave,
		"rebalanceAfter": persistSave, "origin": persistSave, "destination": persistSave,
		"destinationStation": persistSave, "released": persistSave, "terminal": persistReset,
		"buffered": persistSave, "bufferBerth": persistReset,
		"routeVersion": persistReset, "routeLengths": persistDerive, "link": persistSave, "follower": persistDerive, "platoonCap": persistReset,
	},
	reflect.TypeFor[Vehicle](): {
		"Pod": persistSave, "Riders": persistSave, "Stops": persistSave, "Route": persistSave, "Presentation": persistReset,
		"RelocatingTo": persistSave, "Rebalancing": persistSave, "PlatoonID": persistDerive, "PlatoonIndex": persistDerive,
	},
	reflect.TypeFor[Pod](): {
		"ID": persistSave, "Position": persistDerive, "Activity": persistSave, "StationID": persistSave,
		"BerthID": persistSave, "LaneID": persistSave, "LaneDistance": persistSave, "Speed": persistReset,
		"Occupied": persistSave, "WaitReason": persistReset, "BlockedBy": persistReset,
		"StationPhase": persistDerive, "ManeuverStationID": persistDerive,
	},
	reflect.TypeFor[Request](): {
		"ID": persistSave, "From": persistSave, "To": persistSave, "PartySize": persistSave, "PodID": persistSave,
		"Completed": persistSave, "RequestedTick": persistSave, "BoardedTick": persistSave, "DispatchReason": persistSave,
	},
	reflect.TypeFor[waitingTrip](): {
		"request": persistSave, "route": persistSave, "destination": persistReset, "deferUntil": persistSave,
		"deferCheck": persistSave, "deferPodID": persistSave, "boarded": persistSave, "fullPodRefused": persistReset,
		"joinEligibleAssigned": persistReset, "joinEligibleExistingStop": persistReset,
	},
	reflect.TypeFor[demoRun](): {"secondSent": persistSave, "followupsSent": persistSave},
}

func TestSimulationFieldsHavePersistRules(t *testing.T) {
	t.Parallel()
	for typ, rules := range persistRules {
		checkFieldRules(t, fieldRuleCheck{
			kind: "persistence", typ: typ, names: slices.Collect(maps.Keys(rules)),
			needsRule: func(reflect.StructField) bool { return true },
		})
	}
}

// storage returns the addresses of the storage that v refers to.
func storage(v reflect.Value) []uintptr {
	switch v.Kind() {
	case reflect.Map, reflect.Pointer, reflect.Slice:
		return []uintptr{v.Pointer()}
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return storage(v.Elem())
	case reflect.Struct:
		var addresses []uintptr
		for _, field := range v.Fields() {
			addresses = append(addresses, storage(field)...)
		}
		return addresses
	default:
		return nil
	}
}

// holdsData reports whether v refers to storage that holds data.
func holdsData(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Map, reflect.Slice:
		return v.Len() > 0
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil()
	case reflect.Struct:
		for _, field := range v.Fields() {
			if holdsData(field) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

type cloneStorageCheck struct {
	path          string
	source, clone reflect.Value
	// checked records each rule, as "type.field", that saw data in the source.
	checked map[string]bool
}

// checkCloneStorage checks each ruled field of a struct against its clone.
func checkCloneStorage(t *testing.T, check cloneStorageCheck) {
	t.Helper()
	typ := check.source.Type()
	for name, rule := range cloneRules[typ] {
		source, clone := check.source.FieldByName(name), check.clone.FieldByName(name)
		path := check.path + "." + name
		if holdsData(source) {
			check.checked[typ.Name()+"."+name] = true
		}
		switch rule {
		case cloneShare:
			if !slices.Equal(storage(source), storage(clone)) {
				t.Errorf("%s does not share storage with the source", path)
			}
		case cloneDrop:
			if !clone.IsNil() {
				t.Errorf("%s is not dropped", path)
			}
		case cloneCopy:
			checkCopiedStorage(t, cloneStorageCheck{path: path, source: source, clone: clone, checked: check.checked})
		}
	}
}

// checkCopiedStorage checks that a copied field has its own storage, and that
// the elements in it follow their own rules.
func checkCopiedStorage(t *testing.T, check cloneStorageCheck) {
	t.Helper()
	source, clone := check.source, check.clone
	if source.Kind() == reflect.Struct {
		checkCloneStorage(t, check)
		return
	}
	if holdsData(source) && source.Pointer() == clone.Pointer() {
		t.Errorf("%s shares storage with the source", check.path)
	}
	if _, ruled := cloneRules[source.Type().Elem()]; !ruled || !holdsData(source) {
		return
	}
	element := func(path string, source, clone reflect.Value) {
		checkCloneStorage(t, cloneStorageCheck{path: path, source: source, clone: clone, checked: check.checked})
	}
	switch source.Kind() {
	case reflect.Pointer:
		element(check.path, source.Elem(), clone.Elem())
	case reflect.Slice:
		if clone.Len() != source.Len() {
			t.Errorf("%s has %d elements, want %d", check.path, clone.Len(), source.Len())
			return
		}
		for index := range source.Len() {
			element(fmt.Sprintf("%s[%d]", check.path, index), source.Index(index), clone.Index(index))
		}
	case reflect.Map:
		for key, value := range source.Seq2() {
			cloned := clone.MapIndex(key)
			if !cloned.IsValid() {
				t.Errorf("%s has no entry for %v", check.path, key)
				continue
			}
			element(fmt.Sprintf("%s[%v]", check.path, key), value, cloned)
		}
	default:
		t.Errorf("%s has kind %s, which Clone does not copy", check.path, source.Kind())
	}
}

// activeCloneSimulation returns a demo in progress. It has pods with requests
// and routes, waiting trips with routes, filled congestion routes, and the
// platoon work storage.
func activeCloneSimulation(t *testing.T) *Simulation {
	t.Helper()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	s.SetCongestionRouting(true)
	s.SetExperimentRecords(true)
	if err := s.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDemandWeights(map[string]float64{"harbor": 2, "garden": 1, "market": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPositioning(PositioningGuarded); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	advance(s, 35*TicksPerSecond)
	// The platoon work storage holds data only at a tick with a slow pod.
	for range 30 * TicksPerSecond {
		if len(s.platoonOrder) != 0 {
			break
		}
		s.Step()
	}
	// The demo does not use drop-offs, so the fixture fills the station
	// caches of drop-offs.
	s.stationsOnRoute("harbor-berth", "market")
	s.stationPickupBounds("market")
	s.SetPickupSwaps(true)
	s.pickupSwaps.cooldown["01"] = s.tick + pickupSwapCooldownTicks
	s.pickupSwaps.records = []PickupReassignment{{Tick: s.tick, RequestID: 1, OldPod: "01", NewPod: "02", OldSeconds: 30, NewSeconds: 10}}
	return s
}

func TestCloneFollowsRules(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		build func(*testing.T) *Simulation
		// covered requires data in the source for every rule except uncovered.
		covered   bool
		uncovered []string
	}{
		{name: "new fleet", build: newTraffic},
		{
			name: "active", build: activeCloneSimulation, covered: true,
			// No congestion route fails in the example network, no
			// journey ends in the first 35 seconds, and no test monitor
			// runs. Presentation exists only in remote snapshots.
			uncovered: []string{"routeResult.err", "Simulation.requestCompletions", "Simulation.monitor", "Vehicle.Presentation"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := tc.build(t)
			clone := source.Clone()
			if source.junctionConflicts == nil || reflect.ValueOf(clone.junctionConflicts).Pointer() != reflect.ValueOf(source.junctionConflicts).Pointer() {
				t.Fatal("the clone does not share the junction conflict table of its source")
			}
			checked := make(map[string]bool)
			checkCloneStorage(t, cloneStorageCheck{
				path: "Simulation", source: reflect.ValueOf(source).Elem(), clone: reflect.ValueOf(clone).Elem(), checked: checked,
			})
			if !tc.covered {
				return
			}
			for typ, rules := range cloneRules {
				for name := range rules {
					key := typ.Name() + "." + name
					if !checked[key] && !slices.Contains(tc.uncovered, key) {
						t.Errorf("the fixture has no data in %s", key)
					}
				}
			}
		})
	}
}

type cloneTrip struct {
	second   int
	from, to string
}

// cloneInputs steps a simulation and requests each trip at the start of its
// second.
type cloneInputs struct {
	seconds int
	trips   []cloneTrip
}

func (inputs cloneInputs) run(s *Simulation) error {
	for tick := range inputs.seconds * TicksPerSecond {
		for _, trip := range inputs.trips {
			if trip.second*TicksPerSecond != tick {
				continue
			}
			if err := s.RequestTrip(trip.from, trip.to); err != nil {
				return fmt.Errorf("request %s to %s at %d s: %w", trip.from, trip.to, trip.second, err)
			}
		}
		s.Step()
	}
	return nil
}

type cloneFixture struct {
	name string
	// network returns the network of the fixture. Nil selects Example.
	network    func() Network
	placements []Placement
	setup      func(*Simulation) error
	// warmup runs before the clone. continuation runs on the source and on
	// the clone.
	warmup, continuation cloneInputs
	// exercised returns an error when the continuation from the clone point
	// to the end state did not use the path that the fixture tests.
	exercised func(clonePoint, end *Simulation) error
}

func (fixture cloneFixture) build(t *testing.T) *Simulation {
	t.Helper()
	network := Example()
	if fixture.network != nil {
		network = fixture.network()
	}
	s, err := NewFleet(network, fixture.placements)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.setup(s); err != nil {
		t.Fatal(err)
	}
	if err := fixture.warmup.run(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// queueFixtureNetwork has a parking station with 8 berths and five passenger
// stations. Station s1 has one berth.
func queueFixtureNetwork() Network { return lineNetwork(lineStations(8, 2, 1, 2, 2, 2)) }

// queueTrips returns one trip each second from second first to second
// last, with most trips to station s1.
func queueTrips(first, last int) []cloneTrip {
	var trips []cloneTrip
	for second := first; second < last; second++ {
		trips = append(trips, cloneTrip{second: second, from: []string{"s0", "s4", "s3", "s0"}[second%4], to: []string{"s1", "s1", "s1", "s3"}[second%4]})
	}
	return trips
}

func cloneFixtures() []cloneFixture {
	var queueFleet []Placement
	for _, station := range queueFixtureNetwork().Stations {
		for _, berth := range station.Berths {
			queueFleet = append(queueFleet, Placement{ID: berth.ID, StationID: station.ID, BerthID: berth.ID})
		}
	}
	demoFleet := []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}}
	fleet := append(slices.Clone(demoFleet), Placement{ID: "03", StationID: "parking", BerthID: "parking-1"})
	return []cloneFixture{
		{
			name: "demo mid-run", placements: demoFleet,
			setup:        (*Simulation).StartDemo,
			warmup:       cloneInputs{seconds: 20},
			continuation: cloneInputs{seconds: 150},
			exercised: func(clonePoint, end *Simulation) error {
				if clonePoint.demo == nil || clonePoint.demo.secondSent {
					return errors.New("the clone point is not before the second demo journey")
				}
				if end.completed == 0 || (end.demo != nil && !end.demo.followupsSent) {
					return fmt.Errorf("the demo did not send its follow-up trips: completed=%d", end.completed)
				}
				return nil
			},
		},
		{
			// The gate of the example fleet is not active at this rate, so
			// the guarded positioning case covers the positioning moves.
			name: "sharing in guarded mode", placements: fleet,
			setup: func(s *Simulation) error {
				if err := s.SetPositioning(PositioningGuarded); err != nil {
					return fmt.Errorf("set positioning: %w", err)
				}
				if err := s.SetDemandWeights(map[string]float64{"harbor": 2, "garden": 1, "market": 1}); err != nil {
					return fmt.Errorf("set demand weights: %w", err)
				}
				return s.SetSharedRidePartyLimit(2)
			},
			warmup: cloneInputs{seconds: 20, trips: []cloneTrip{
				{0, "harbor", "market"}, {1, "harbor", "market"}, {2, "garden", "market"},
			}},
			continuation: cloneInputs{seconds: 200, trips: []cloneTrip{
				{40, "market", "garden"}, {41, "market", "garden"}, {60, "garden", "harbor"},
				{90, "harbor", "market"}, {91, "harbor", "market"},
			}},
			exercised: func(clonePoint, end *Simulation) error {
				if end.completed <= clonePoint.completed || end.sharedParties <= clonePoint.sharedParties {
					return fmt.Errorf("continuation completed %d and shared %d",
						end.completed-clonePoint.completed, end.sharedParties-clonePoint.sharedParties)
				}
				return nil
			},
		},
		{
			name: "requeued shared ride", placements: fleet,
			setup: func(s *Simulation) error {
				// A restore queues the three riders of a shared ride again.
				// The three orders boarded before the restore.
				s.requestID, s.boarded, s.sharedParties = 3, 3, 2
				for id := 1; id <= 3; id++ {
					s.waiting = append(s.waiting, waitingTrip{
						request: Request{ID: id, From: "market", To: "garden", PartySize: 1}, boarded: true,
					})
				}
				return s.SetSharedRidePartyLimit(4)
			},
			warmup: cloneInputs{seconds: 20},
			// The new order arrives while the pickup pod boards the requeued
			// parties.
			continuation: cloneInputs{seconds: 400, trips: []cloneTrip{{20, "market", "garden"}}},
			exercised: func(clonePoint, end *Simulation) error {
				if !slices.ContainsFunc(clonePoint.waiting, func(trip waitingTrip) bool { return trip.boarded }) {
					return errors.New("the clone point has no requeued trip")
				}
				// Only the new order boards for the first time.
				if end.completed-clonePoint.completed != 4 || end.boarded-clonePoint.boarded != 1 ||
					end.sharedParties-clonePoint.sharedParties != 1 {
					return fmt.Errorf("continuation completed %d, boarded %d, and shared %d",
						end.completed-clonePoint.completed, end.boarded-clonePoint.boarded,
						end.sharedParties-clonePoint.sharedParties)
				}
				return nil
			},
		},
		{
			// The second party for harbor has a pod on its way when the
			// first pod boards at market, so it joins that pod.
			name: "reassigned shared ride", placements: fleet,
			setup: func(s *Simulation) error {
				s.SetExperimentRecords(true)
				if err := s.SetSharedRideJoin(SharedRideJoinReassignExisting); err != nil {
					return err
				}
				return s.SetSharedRidePartyLimit(4)
			},
			warmup:       cloneInputs{seconds: 20},
			continuation: cloneInputs{seconds: 200, trips: []cloneTrip{{0, "market", "harbor"}, {0, "market", "harbor"}}},
			exercised: func(clonePoint, end *Simulation) error {
				if end.seatScreen.ReassignedParties <= clonePoint.seatScreen.ReassignedParties || end.completed-clonePoint.completed != 2 {
					return fmt.Errorf("continuation reassigned %d parties and completed %d",
						end.seatScreen.ReassignedParties-clonePoint.seatScreen.ReassignedParties, end.completed-clonePoint.completed)
				}
				return nil
			},
		},
		{
			name: "congestion routing", placements: fleet,
			setup: func(s *Simulation) error {
				s.SetCongestionRouting(true)
				return nil
			},
			warmup: cloneInputs{seconds: 20, trips: []cloneTrip{
				{0, "harbor", "market"}, {2, "garden", "market"}, {4, "market", "harbor"},
			}},
			continuation: cloneInputs{seconds: 150, trips: []cloneTrip{
				{5, "harbor", "garden"}, {15, "garden", "market"}, {30, "market", "harbor"}, {60, "harbor", "market"},
			}},
			exercised: func(clonePoint, end *Simulation) error {
				if len(clonePoint.congestionRoutes) == 0 {
					return errors.New("the clone point has no congestion routes")
				}
				if end.completed <= clonePoint.completed || end.nextCongestionRouteRefresh <= clonePoint.nextCongestionRouteRefresh {
					return fmt.Errorf("continuation completed %d and did not refresh congestion routes", end.completed-clonePoint.completed)
				}
				return nil
			},
		},
		{
			// Pods leave parking in a queue at the clone point, so the
			// assignments after it search with queue delays.
			name: "queue routing", placements: queueFleet,
			network:      queueFixtureNetwork,
			setup:        func(s *Simulation) error { return s.SetRoutingPolicy(QueueRouting) },
			warmup:       cloneInputs{seconds: 20, trips: queueTrips(0, 20)},
			continuation: cloneInputs{seconds: 150, trips: queueTrips(0, 60)},
			exercised: func(clonePoint, end *Simulation) error {
				if clonePoint.queueDischarge(nil) == nil || end.completed <= clonePoint.completed {
					return fmt.Errorf("the clone point has no queue, or the continuation completed %d", end.completed-clonePoint.completed)
				}
				return nil
			},
		},
		{
			// Request k comes after 200k s, so the gate is active for the
			// fleet of 6. The check after each boarding makes a refill.
			name: "guarded positioning", placements: place("p-1", "p-2", "p-3", "p-4", "p-5", "p-6"),
			network:      func() Network { return lineNetwork(lineStations(6, 2, 2, 2, 2, 2, 2)) },
			setup:        func(s *Simulation) error { return s.SetPositioning(PositioningGuarded) },
			warmup:       cloneInputs{seconds: 300, trips: []cloneTrip{{210, "s0", "s2"}}},
			continuation: cloneInputs{seconds: 200, trips: []cloneTrip{{150, "s1", "s4"}}},
			exercised: func(clonePoint, end *Simulation) error {
				if clonePoint.rebalanceMoves == 0 || end.rebalanceMoves <= clonePoint.rebalanceMoves || end.boarded != 2 {
					return fmt.Errorf("the run made %d refills before the clone point and %d after, with %d boardings",
						clonePoint.rebalanceMoves, end.rebalanceMoves-clonePoint.rebalanceMoves, end.boarded)
				}
				return nil
			},
		},
	}
}

// stripCaches returns a shallow copy of s without the pure caches that Clone
// drops.
func stripCaches(s *Simulation) *Simulation {
	c := *s
	c.lengths, c.routes, c.routeOrder = nil, nil, nil
	c.pickupBounds = nil
	c.routeWork = nil
	c.admissionWork = nil
	// The dispatch pass holds only buffers of the last dispatch.
	c.pass = nil
	// The cursors of a block list depend on the order of the lookups.
	c.vehicles = slices.Clone(s.vehicles)
	for index := range c.vehicles {
		c.vehicles[index].blocks.cursors, c.vehicles[index].blocks.scan = [2]blockCursor{}, 0
	}
	return &c
}

// sameState reports whether a and b hold the same state apart from the pure
// caches.
func sameState(a, b *Simulation) bool {
	// DeepEqual compares cached route errors by value. Each side makes its
	// errors with the same deterministic code, so this comparison is correct.
	return reflect.DeepEqual(stripCaches(a), stripCaches(b)) //nolint:govet // deepequalerrors: route errors compare by value on purpose.
}

func TestCloneIsIndependentAndExact(t *testing.T) {
	t.Parallel()
	for _, fixture := range cloneFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			source, twin := fixture.build(t), fixture.build(t)
			if !sameState(source, twin) {
				t.Fatal("identical inputs built different fixtures")
			}
			clone := source.Clone()
			if !sameState(clone, twin) {
				t.Fatal("the clone differs from its source")
			}
			if err := fixture.continuation.run(source); err != nil {
				t.Fatal(err)
			}
			if err := fixture.exercised(twin, source); err != nil {
				t.Fatal(err)
			}
			if !sameState(clone, twin) {
				t.Fatal("steps of the source changed the clone")
			}
			if err := fixture.continuation.run(clone); err != nil {
				t.Fatal(err)
			}
			if !sameState(clone, source) {
				t.Fatal("the clone diverged from its source for the same inputs")
			}
		})
	}
}

// TestCloneConcurrentStep finds in-place writes to shared storage with the
// race detector. The fixtures run with congestion routing off and on.
func TestCloneConcurrentStep(t *testing.T) {
	t.Parallel()
	for _, fixture := range cloneFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			source := fixture.build(t)
			clone := source.Clone()
			var group sync.WaitGroup
			var sourceErr, cloneErr error
			group.Go(func() { sourceErr = fixture.continuation.run(source) })
			group.Go(func() { cloneErr = fixture.continuation.run(clone) })
			group.Wait()
			if err := errors.Join(sourceErr, cloneErr); err != nil {
				t.Fatal(err)
			}
			if !sameState(clone, source) {
				t.Fatal("the clone diverged from its source for the same inputs")
			}
		})
	}
}
