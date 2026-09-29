package sim

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"
)

func TestPreparedNetworkFields(t *testing.T) {
	t.Parallel()
	checkFieldRules(t, fieldRuleCheck{kind: "immutable prepared field", typ: reflect.TypeFor[PreparedNetwork](),
		names:     []string{"network", "graph", "stationIndexes", "stationForbidden", "geometry", "junctionConflicts", "berthResources", "laneCells", "laneSafety", "berthSafety"},
		needsRule: func(reflect.StructField) bool { return true },
	})
}

func TestPreparedNetworkOwnership(t *testing.T) {
	t.Parallel()
	network := curvedExample()
	original := network.clone()
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	expected := p.Network()
	network.Nodes[0].Position.X += 999
	network.Lanes[0].SpeedLimit++
	for i := range network.Lanes {
		if network.Lanes[i].Control != nil {
			network.Lanes[i].Control.X += 999
		}
	}
	network.Stations[0].Berths[0].Node = "mutated"
	detached := p.Network()
	detached.Nodes[0].ID = "changed"
	detached.Stations[0].Berths[0].ID = "changed"
	if !reflect.DeepEqual(p.Network(), expected) {
		t.Fatal("prepared network aliases caller storage")
	}
	fleet := []Placement{{ID: "01", StationID: "harbor"}}
	a, err := p.NewFleet(fleet)
	if err != nil {
		t.Fatal(err)
	}
	fleet[0].ID = "changed"
	b, err := NewFleet(original, []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.ExportState(), b.ExportState()) {
		t.Fatal("new fleet differs")
	}
	for _, s := range []*Simulation{a, b} {
		if requestErr := s.RequestTrip("harbor", "market"); requestErr != nil {
			t.Fatal(requestErr)
		}
	}
	for range 600 {
		a.Step()
		b.Step()
	}
	if !reflect.DeepEqual(a.ExportState(), b.ExportState()) {
		t.Fatal("simulation evolution differs")
	}
	if !reflect.DeepEqual(p.Network(), expected) {
		t.Fatal("simulation mutated prepared network")
	}
	fresh, err := p.NewFleet([]Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.tick != 0 || len(fresh.waiting) != 0 || fresh.requestID != 0 {
		t.Fatal("mutable simulation state leaked")
	}
}

func TestPreparedFleetValidation(t *testing.T) {
	t.Parallel()
	network := Example()
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	for _, fleet := range [][]Placement{nil, {{ID: "", StationID: "harbor"}}, {{ID: "01", StationID: "missing"}}, {{ID: "01", StationID: "harbor", BerthID: "missing"}}, {{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "harbor"}}} {
		_, ordinary := NewFleet(network, fleet)
		_, prepared := p.NewFleet(fleet)
		if ordinary == nil || prepared == nil || ordinary.Error() != prepared.Error() {
			t.Fatalf("validation differs: %v / %v", ordinary, prepared)
		}
	}
	for _, handle := range []*PreparedNetwork{nil, {}} {
		if _, err := handle.NewFleet([]Placement{{ID: "01", StationID: "harbor"}}); err == nil {
			t.Fatal("unprepared fleet accepted")
		}
		if _, _, err := handle.RestoreState(PreparedRestoreInput{}); err == nil {
			t.Fatal("unprepared restore accepted")
		}
	}
}

func TestPreparedRestoreEquivalence(t *testing.T) {
	t.Parallel()
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	base := logicalState(t, f)
	p, err := PrepareNetwork(f.network)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		logical   bool
		wantTier  RestoreTier
		wantError bool
		edit      func(*SavedState)
	}{
		{name: "physical", wantTier: RestorePhysical, edit: func(*SavedState) {}},
		{name: "logical", logical: true, wantTier: RestoreLogical, edit: func(*SavedState) {}},
		{name: "physical fallback", wantTier: RestoreLogical, edit: func(state *SavedState) {
			pod := &state.Pods[2]
			pod.StationID, pod.BerthID, pod.DestinationStation, pod.Destination = "garden", "garden-1", "garden", "garden-1"
			for i := range pod.Riders {
				pod.Riders[i].To = "garden"
			}
		}},
		{name: "invalid counter", wantError: true, edit: func(state *SavedState) { state.Tick = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := roundTripState(t, base)
			tc.edit(&state)
			ordinary, wantResult, wantErr := RestoreState(RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, LogicalOnly: tc.logical})
			got, result, err := p.RestoreState(PreparedRestoreInput{Fleet: f.fleet, State: state, LogicalOnly: tc.logical})
			if (err != nil) != tc.wantError || !tc.wantError && result.Tier != tc.wantTier {
				t.Fatalf("restore result: tier %q, error %v, want tier %q, error %v", result.Tier, err, tc.wantTier, tc.wantError)
			}
			if fmt.Sprint(wantErr) != fmt.Sprint(err) {
				t.Fatalf("errors differ: %v / %v", wantErr, err)
			}
			if fmt.Sprint(wantResult.PhysicalError) != fmt.Sprint(result.PhysicalError) {
				t.Fatal("physical error differs")
			}
			wantResult.PhysicalError, result.PhysicalError = nil, nil
			if !sameRestoreResult(wantResult, result) {
				t.Fatalf("results differ: %+v / %+v", wantResult, result)
			}
			if err == nil {
				if !reflect.DeepEqual(ordinary.ExportState(), got.ExportState()) || !reflect.DeepEqual(ordinary.owners, got.owners) {
					t.Fatal("restored state or ownership differs")
				}
				for range 120 {
					ordinary.Step()
					got.Step()
				}
				if !reflect.DeepEqual(ordinary.ExportState(), got.ExportState()) {
					t.Fatal("restored evolution differs")
				}
			}
			// A rejected or partially restored candidate must not affect later use.
			clean, cleanResult, cleanErr := p.RestoreState(PreparedRestoreInput{Fleet: f.fleet, State: roundTripState(t, base)})
			reference, referenceResult, referenceErr := f.restore(roundTripState(t, base))
			if cleanErr != nil || referenceErr != nil || !sameRestoreResult(cleanResult, referenceResult) || !reflect.DeepEqual(clean.ExportState(), reference.ExportState()) {
				t.Fatal("earlier restore contaminated later use")
			}
		})
	}
}

func TestPreparedConcurrentIsolation(t *testing.T) {
	t.Parallel()
	network := Example()
	fleet := []Placement{{ID: "01", StationID: "harbor"}}
	p, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if requestErr := reference.RequestTrip("harbor", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	for range 120 {
		reference.Step()
	}
	saved := reference.ExportState()
	ordinary, _, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: saved})
	if err != nil {
		t.Fatal(err)
	}
	for range 120 {
		ordinary.Step()
	}
	want := ordinary.ExportState()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			s, _, err := p.RestoreState(PreparedRestoreInput{Fleet: slices.Clone(fleet), State: saved})
			if err != nil {
				t.Error(err)
				return
			}
			for range 120 {
				s.Step()
			}
			if !reflect.DeepEqual(s.ExportState(), want) {
				t.Error("concurrent restore differs")
			}
			s.Reset()
		})
	}
	wg.Wait()
}

func BenchmarkPreparedRestore(b *testing.B) {
	network := forkNetwork(14)
	fleet := []Placement{{ID: "p01", StationID: "origin"}}
	p, err := PrepareNetwork(network)
	if err != nil {
		b.Fatal(err)
	}
	s, err := p.NewFleet(fleet)
	if err != nil {
		b.Fatal(err)
	}
	state := s.ExportState()
	for _, prepared := range []bool{false, true} {
		name := "ordinary"
		if prepared {
			name = "prepared"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if prepared {
					_, _, err = p.RestoreState(PreparedRestoreInput{Fleet: fleet, State: state})
				} else {
					_, _, err = RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// sameRestoreResult compares all result fields, with errors compared by message.
func sameRestoreResult(a, b RestoreResult) bool {
	if fmt.Sprint(a.PhysicalError) != fmt.Sprint(b.PhysicalError) {
		return false
	}
	left, right := reflect.ValueOf(a), reflect.ValueOf(b)
	for index := range left.NumField() {
		if left.Type().Field(index).Name == "PhysicalError" {
			continue
		}
		if !reflect.DeepEqual(left.Field(index).Interface(), right.Field(index).Interface()) {
			return false
		}
	}
	return true
}
