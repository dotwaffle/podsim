package sim

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestSafetyObservationCheck(t *testing.T) {
	t.Parallel()
	const tick = 42
	upper := []SafetyLocation{
		{SeparationGroup: "upper", From: "a", To: "b"},
		{SeparationGroup: "upper", From: "c", To: "d"},
	}
	tests := []struct {
		name      string
		pods      []Pod
		locations []SafetyLocation
		berths    []BerthState
		wantGap   float64
		// wantErr is a part of the error text. An empty value expects no error.
		wantErr        string
		wantSeparation *SeparationError
	}{
		{
			name: "valid",
			pods: []Pod{
				{ID: "one", BerthID: "berth-one"},
				{ID: "two", Position: Point{X: Clearance}},
			},
			berths:  []BerthState{{ID: "berth-one", Occupant: "one", ReservedBy: "one"}},
			wantGap: Clearance,
		},
		{
			name:    "no speed limit",
			pods:    []Pod{{ID: "one", Speed: 100}},
			wantGap: math.Inf(1),
		},
		{
			name: "smallest of three gaps",
			pods: []Pod{
				{ID: "one"},
				{ID: "two", Position: Point{X: 50}},
				{ID: "three", Position: Point{X: 30}},
			},
			wantGap: 20,
		},
		{
			name:    "NaN position",
			pods:    []Pod{{ID: "one", Position: Point{Y: math.NaN()}}},
			wantErr: "invalid pod",
		},
		{
			name:    "infinite position",
			pods:    []Pod{{ID: "one", Position: Point{X: math.Inf(1)}}},
			wantErr: "invalid pod",
		},
		{
			name:    "negative speed",
			pods:    []Pod{{ID: "one", Speed: -0.1}},
			wantErr: "invalid pod",
		},
		{
			name:    "NaN speed",
			pods:    []Pod{{ID: "one", Speed: math.NaN()}},
			wantErr: "invalid pod",
		},
		{
			name:           "gap 11.99 m in the same group",
			pods:           []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 11.99}}},
			locations:      upper,
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 11.99},
		},
		{
			name: "gap below Clearance in separated groups",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 1}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", From: "a", To: "b"},
				{SeparationGroup: "lower", From: "c", To: "d"},
			},
			wantGap: math.Inf(1),
		},
		{
			name: "separated groups with a shared node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", To: "junction"},
				{SeparationGroup: "lower", From: "junction"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "separated groups with a shared From node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", From: "junction", To: "b"},
				{SeparationGroup: "lower", From: "junction", To: "d"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "separated groups with a From node shared as a To node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", From: "junction", To: "b"},
				{SeparationGroup: "lower", From: "c", To: "junction"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "separated groups with a shared To node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", From: "a", To: "junction"},
				{SeparationGroup: "lower", From: "c", To: "junction"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "empty From nodes are not shared",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", To: "b"},
				{SeparationGroup: "lower", From: "c"},
			},
			wantGap: math.Inf(1),
		},
		{
			name: "empty To nodes are not shared",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", From: "a"},
				{SeparationGroup: "lower", To: "d"},
			},
			wantGap: math.Inf(1),
		},
		{
			name: "one pod without a group",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: []SafetyLocation{
				{SeparationGroup: "upper", From: "a", To: "b"},
				{From: "c", To: "d"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "two pods in one berth",
			pods: []Pod{
				{ID: "one", BerthID: "berth-one"},
				{ID: "two", Position: Point{X: Clearance}, BerthID: "berth-one"},
			},
			berths:  []BerthState{{ID: "berth-one", Occupant: "two", ReservedBy: "two"}},
			wantErr: "capacity exceeded",
		},
		{
			name:    "berth occupant mismatch",
			pods:    []Pod{{ID: "one", BerthID: "berth-one"}},
			berths:  []BerthState{{ID: "berth-one", Occupant: "other", ReservedBy: "one"}},
			wantErr: "invalid berth state",
		},
		{
			name:    "ReservedBy mismatch",
			pods:    []Pod{{ID: "one", BerthID: "berth-one"}},
			berths:  []BerthState{{ID: "berth-one", Occupant: "one", ReservedBy: "other"}},
			wantErr: "invalid berth state",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			observation := SafetyObservation{Tick: tick, Pods: test.pods, Locations: test.locations, Berths: test.berths}
			gap, err := observation.Check()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Check() error = %v", err)
				}
				if !sameGap(gap, test.wantGap) {
					t.Fatalf("Check() gap = %v, want %v", gap, test.wantGap)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Check() error = %v, want text %q", err, test.wantErr)
			}
			separation, ok := errors.AsType[*SeparationError](err)
			if test.wantSeparation == nil {
				if ok {
					t.Fatalf("Check() error = %v, want an error other than *SeparationError", err)
				}
				return
			}
			if !ok {
				t.Fatalf("Check() error = %v, want *SeparationError", err)
			}
			want := test.wantSeparation
			if separation.Tick != want.Tick || separation.First != want.First || separation.Second != want.Second || !sameGap(separation.Gap, want.Gap) {
				t.Fatalf("Check() error = %+v, want %+v", *separation, *want)
			}
		})
	}
}

func sameGap(got, want float64) bool {
	return got == want || math.Abs(got-want) <= 1e-9
}

// TestSafetyObservationMovedPods checks that a pod keeps its own location,
// envelope, and key when a caller removes or moves pods of the
// observation, as the restore does. The pods a and c are close on one
// plane, and b is on a different plane between them in the fleet order.
// An oracle that reads the entries by position gives c the plane of b, and
// misses the pair.
func TestSafetyObservationMovedPods(t *testing.T) {
	t.Parallel()
	upper := SafetyLocation{SeparationGroup: "upper", From: "a", To: "b"}
	lower := SafetyLocation{SeparationGroup: "lower", From: "c", To: "d"}
	for _, class := range []VehicleClass{CompactClass, GroupClass} {
		fleet := []Pod{
			{ID: "a", Class: class},
			{ID: "b", Class: class, Position: Point{X: 1}},
			{ID: "c", Class: class, Position: Point{X: 2}},
		}
		observation := func(pods ...Pod) SafetyObservation {
			return SafetyObservation{
				Pods: pods, bound: slices.Clone(fleet),
				Locations: []SafetyLocation{upper, lower, upper},
				envelopes: [][]SafetyLocation{{upper}, {lower}, {upper}},
				keys:      []separationKey{laneKey(0), laneKey(1), laneKey(2)},
				certified: certifiedPairs{keyPair(laneKey(0), laneKey(1)): true, keyPair(laneKey(1), laneKey(2)): true},
			}
		}
		a, b, c := fleet[0], fleet[1], fleet[2]
		for _, test := range []struct {
			name          string
			pods          []Pod
			first, second string
		}{
			{name: "fleet order", pods: []Pod{a, b, c}, first: "a", second: "c"},
			{name: "b removed", pods: []Pod{a, c}, first: "a", second: "c"},
			{name: "moved", pods: []Pod{c, b, a}, first: "c", second: "a"},
			{name: "a removed", pods: []Pod{b, c}},
			{name: "c removed", pods: []Pod{a, b}},
			{name: "c removed and moved", pods: []Pod{b, a}},
		} {
			_, err := observation(test.pods...).Check()
			separation, ok := errors.AsType[*SeparationError](err)
			switch {
			case test.first == "" && err != nil:
				t.Errorf("class %q, %s: %v", class, test.name, err)
			case test.first != "" && (!ok || separation.First != test.first || separation.Second != test.second):
				t.Errorf("class %q, %s: error %v, want the pods %s and %s", class, test.name, err, test.first, test.second)
			}
		}
		// A pod that is not in the fleet has no entries, and a pod that
		// differs from its bound pod has no key.
		stranger := Pod{ID: "x", Class: class, Position: Point{X: 1}}
		if _, err := observation(stranger, b).Check(); err == nil {
			t.Errorf("class %q: a pod with no entries is separated", class)
		}
		// The location is not bound, so the pods share one plane here.
		changed := c
		changed.Position.X = 1.5
		stale := observation(changed, b)
		stale.Locations = []SafetyLocation{upper, upper, upper}
		if _, err := stale.Check(); err == nil {
			t.Errorf("class %q: a changed pod kept its key", class)
		}
	}
}
