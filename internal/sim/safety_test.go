package sim

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestSafetyObservationCheck(t *testing.T) {
	t.Parallel()
	const tick = 42
	upper := map[string]SafetyLocation{
		"one": {SeparationGroup: "upper", From: "a", To: "b"},
		"two": {SeparationGroup: "upper", From: "c", To: "d"},
	}
	tests := []struct {
		name      string
		pods      []Pod
		locations map[string]SafetyLocation
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
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", From: "a", To: "b"},
				"two": {SeparationGroup: "lower", From: "c", To: "d"},
			},
			wantGap: math.Inf(1),
		},
		{
			name: "separated groups with a shared node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", To: "junction"},
				"two": {SeparationGroup: "lower", From: "junction"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "separated groups with a shared From node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", From: "junction", To: "b"},
				"two": {SeparationGroup: "lower", From: "junction", To: "d"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "separated groups with a From node shared as a To node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", From: "junction", To: "b"},
				"two": {SeparationGroup: "lower", From: "c", To: "junction"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "separated groups with a shared To node",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", From: "a", To: "junction"},
				"two": {SeparationGroup: "lower", From: "c", To: "junction"},
			},
			wantErr:        "meters apart",
			wantSeparation: &SeparationError{Tick: tick, First: "one", Second: "two", Gap: 5},
		},
		{
			name: "empty From nodes are not shared",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", To: "b"},
				"two": {SeparationGroup: "lower", From: "c"},
			},
			wantGap: math.Inf(1),
		},
		{
			name: "empty To nodes are not shared",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", From: "a"},
				"two": {SeparationGroup: "lower", To: "d"},
			},
			wantGap: math.Inf(1),
		},
		{
			name: "one pod without a group",
			pods: []Pod{{ID: "one"}, {ID: "two", Position: Point{X: 5}}},
			locations: map[string]SafetyLocation{
				"one": {SeparationGroup: "upper", From: "a", To: "b"},
				"two": {From: "c", To: "d"},
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
