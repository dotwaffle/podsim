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
		"graph": cloneShare, "stationIndexes": cloneShare, "stationForbidden": cloneShare,
		"geometry": cloneShare, "network": cloneShare, "initial": cloneShare,
		"vehicles": cloneCopy, "owners": cloneCopy, "demo": cloneCopy, "waiting": cloneCopy,
		"demandWeights": cloneShare, "congestionRouteCosts": cloneShare, "congestionRoutes": cloneCopy,
		"laneSafety": cloneShare, "berthSafety": cloneShare,
	},
	reflect.TypeFor[vehicle](): {
		"Vehicle": cloneCopy, "blocks": cloneShare, "blockStarts": cloneShare, "routeReleases": cloneCopy,
	},
	reflect.TypeFor[Vehicle]():     {"Request": cloneCopy, "Route": cloneShare},
	reflect.TypeFor[waitingTrip](): {"route": cloneShare},
	reflect.TypeFor[routeResult](): {"lanes": cloneShare, "err": cloneShare},
}

// clonePlainTypes hold only plain values, so a value copy of them is deep.
var clonePlainTypes = []reflect.Type{
	reflect.TypeFor[Request](), reflect.TypeFor[Pod](), reflect.TypeFor[Berth](),
	reflect.TypeFor[resource](), reflect.TypeFor[demoRun](), reflect.TypeFor[routeKey](),
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
		"graph": persistDerive, "stationIndexes": persistDerive, "stationForbidden": persistDerive,
		"geometry": persistDerive, "network": persistSession, "initial": persistSession,
		"vehicles": persistSave, "owners": persistDerive, "tick": persistSave, "paused": persistSave,
		"completed": persistSave, "requestID": persistSave, "demo": persistSave, "demoError": persistSave,
		"waiting": persistSave, "boarded": persistSave, "totalWaitTicks": persistSave, "maxWaitTicks": persistSave,
		"redistribution": persistSession, "demandWeights": persistSession, "nextRedistributionTick": persistSave,
		"passengerDistanceMeters": persistSave, "emptyDistanceMeters": persistSave, "rebalanceMoves": persistSave,
		"sharedRidePartyLimit": persistSave, "sharedParties": persistSave,
		"laneSafety": persistDerive, "berthSafety": persistDerive,
		"congestionRouting": persistUnsupported, "congestionRouteCosts": persistUnsupported,
		"congestionRoutes": persistUnsupported, "nextCongestionRouteRefresh": persistUnsupported,
		"reservationLookaheadSeconds": persistUnsupported,
	},
	reflect.TypeFor[vehicle](): {
		"Vehicle": persistSave, "phaseTicks": persistSave, "blocks": persistDerive, "blockStarts": persistDerive,
		"routeReleases": persistDerive, "blockIndex": persistDerive, "reservedThrough": persistDerive,
		"originReleased": persistDerive, "distance": persistSave, "pending": persistDerive, "waitSince": persistSave,
		"rebalanceAfter": persistSave, "origin": persistSave, "destination": persistSave,
		"destinationStation": persistSave,
	},
	reflect.TypeFor[Vehicle](): {
		"Pod": persistSave, "Request": persistSave, "Route": persistSave, "Parties": persistSave,
		"RelocatingTo": persistSave, "Rebalancing": persistSave,
	},
	reflect.TypeFor[Pod](): {
		"ID": persistSave, "Position": persistDerive, "Activity": persistSave, "StationID": persistSave,
		"BerthID": persistSave, "LaneID": persistSave, "LaneDistance": persistSave, "Speed": persistReset,
		"Occupied": persistSave, "WaitReason": persistReset, "BlockedBy": persistReset,
		"StationPhase": persistDerive, "ManeuverStationID": persistDerive,
	},
	reflect.TypeFor[Request](): {
		"ID": persistSave, "From": persistSave, "To": persistSave, "PartySize": persistSave, "PodID": persistSave,
		"Completed": persistSave, "RequestedTick": persistSave, "DispatchReason": persistSave,
	},
	reflect.TypeFor[waitingTrip](): {
		"request": persistSave, "route": persistSave, "destination": persistReset, "deferUntil": persistSave,
		"deferCheck": persistSave, "deferPodID": persistSave, "parties": persistSave,
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
// and routes, waiting trips with routes, and filled congestion routes.
func activeCloneSimulation(t *testing.T) *Simulation {
	t.Helper()
	s := newTraffic(t)
	if err := s.StartDemo(); err != nil {
		t.Fatal(err)
	}
	s.SetCongestionRouting(true)
	if err := s.SetSharedRidePartyLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDemandWeights(map[string]float64{"harbor": 2, "garden": 1, "market": 1}); err != nil {
		t.Fatal(err)
	}
	s.SetRedistribution(true)
	advance(s, 35*TicksPerSecond)
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
			// No congestion route fails in the example network.
			uncovered: []string{"routeResult.err"},
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
	name       string
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
	s, err := NewFleet(Example(), fixture.placements)
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

func cloneFixtures() []cloneFixture {
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
			name: "sharing and redistribution", placements: fleet,
			setup: func(s *Simulation) error {
				s.SetRedistribution(true)
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
				if end.completed <= clonePoint.completed || end.sharedParties <= clonePoint.sharedParties ||
					end.rebalanceMoves <= clonePoint.rebalanceMoves {
					return fmt.Errorf("continuation completed %d, shared %d, and rebalanced %d",
						end.completed-clonePoint.completed, end.sharedParties-clonePoint.sharedParties,
						end.rebalanceMoves-clonePoint.rebalanceMoves)
				}
				return nil
			},
		},
		{
			name: "requeued shared ride", placements: fleet,
			setup: func(s *Simulation) error {
				// A restore queues a shared ride of three orders again. The
				// three orders boarded before the restore.
				s.requestID, s.boarded, s.sharedParties = 3, 3, 2
				s.waiting = append(s.waiting, waitingTrip{
					request: Request{ID: 1, From: "market", To: "garden", PartySize: 3}, parties: 3,
				})
				return s.SetSharedRidePartyLimit(4)
			},
			warmup: cloneInputs{seconds: 20},
			// The new order arrives while the pickup pod boards the requeued
			// parties.
			continuation: cloneInputs{seconds: 200, trips: []cloneTrip{{20, "market", "garden"}}},
			exercised: func(clonePoint, end *Simulation) error {
				if !slices.ContainsFunc(clonePoint.waiting, func(trip waitingTrip) bool { return trip.parties == 3 }) {
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
	}
}

// stripCaches returns a shallow copy of s without the pure caches that Clone
// drops.
func stripCaches(s *Simulation) *Simulation {
	c := *s
	c.lengths, c.routes, c.routeOrder = nil, nil, nil
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
