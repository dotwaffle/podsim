package sim

import (
	"fmt"
	"reflect"
	"testing"
)

// TestNetworkCheckRestoreEquivalence checks that the restore of a network
// check gives the result, the refusal, and the evolution of the package
// function RestoreState, and that it does not prepare the network again.
// The input names another network, which the check ignores. It does not
// run in parallel, because the count is for the process.
func TestNetworkCheckRestoreEquivalence(t *testing.T) {
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	base := logicalState(t, f)
	for _, tc := range []struct {
		name    string
		logical bool
		fleet   []Placement
		edit    func(*SavedState)
	}{
		{name: "physical", fleet: f.fleet, edit: func(*SavedState) {}},
		{name: "logical", logical: true, fleet: f.fleet, edit: func(*SavedState) {}},
		{name: "physical fallback", fleet: f.fleet, edit: func(state *SavedState) {
			pod := &state.Pods[2]
			pod.StationID, pod.BerthID, pod.DestinationStation, pod.Destination = "garden", "garden-1", "garden", "garden-1"
			for i := range pod.Riders {
				pod.Riders[i].To = "garden"
			}
		}},
		{name: "invalid counter", fleet: f.fleet, edit: func(state *SavedState) { state.Tick = -1 }},
		{name: "invalid fleet", fleet: []Placement{{ID: "01", StationID: "no-such-station"}}, edit: func(*SavedState) {}},
	} {
		state := roundTripState(t, base)
		tc.edit(&state)
		input := RestoreStateInput{Network: f.network, Fleet: tc.fleet, State: state, LogicalOnly: tc.logical}
		want, wantResult, wantErr := RestoreState(input)
		check := NewNetworkCheck(f.network, LaneClearance{})
		if err := check.Err(); err != nil {
			t.Fatal(err)
		}
		before := preparations.Load()
		input.Network = Network{}
		got, result, err := check.RestoreState(input)
		if runs := preparations.Load() - before; runs != 0 {
			t.Errorf("%s: the restore of the check prepares the network %d times, want 0", tc.name, runs)
		}
		if fmt.Sprint(err) != fmt.Sprint(wantErr) || !sameRestoreResult(result, wantResult) {
			t.Fatalf("%s: result %+v, error %v, want %+v, error %v", tc.name, result, err, wantResult, wantErr)
		}
		if err != nil {
			continue
		}
		if !reflect.DeepEqual(got.ExportState(), want.ExportState()) || !reflect.DeepEqual(got.owners, want.owners) {
			t.Fatalf("%s: the restored state or ownership differs", tc.name)
		}
		for range 120 {
			want.Step()
			got.Step()
		}
		if !reflect.DeepEqual(got.ExportState(), want.ExportState()) {
			t.Fatalf("%s: the restored evolution differs", tc.name)
		}
	}
}

// TestNetworkCheckRestoreColdFailure checks the restore of a network check
// that did not run its checks, on a network whose preparation fails. The
// refusal and the preparation count over the tiers that run are those of
// the package function RestoreState. With valid saved counters, the tiers
// prepare the network one time and both give its error. With an invalid
// counter, the counter check refuses first, and the restore does not
// prepare the network. It does not run in parallel, because the count is
// for the process.
func TestNetworkCheckRestoreColdFailure(t *testing.T) {
	f := newRestoreFleetFixture(t, Example(), demoFleet())
	base := logicalState(t, f)
	network := Example()
	network.Nodes = append(network.Nodes, network.Nodes[0])
	for _, tc := range []struct {
		name     string
		edit     func(*SavedState)
		wantErr  string
		wantRuns int64
	}{
		{
			name: "valid counters", edit: func(*SavedState) {}, wantRuns: 1,
			wantErr: `restore the saved simulation: physical tier: create the fleet: invalid or duplicate node "harbor-entry", ` +
				`logical tier: create the fleet: invalid or duplicate node "harbor-entry"`,
		},
		{
			name: "invalid counter", edit: func(state *SavedState) { state.Tick = -1 }, wantRuns: 0,
			wantErr: "pod 01 boarding records: rider 8 is not valid",
		},
	} {
		state := roundTripState(t, base)
		tc.edit(&state)
		input := RestoreStateInput{Network: network, Fleet: f.fleet, State: state}
		before := preparations.Load()
		_, _, wantErr := RestoreState(input)
		wantRuns := preparations.Load() - before
		check := NewNetworkCheck(network, LaneClearance{})
		before = preparations.Load()
		_, _, err := check.RestoreState(input)
		runs := preparations.Load() - before
		if wantErr == nil || wantErr.Error() != tc.wantErr || wantRuns != tc.wantRuns {
			t.Fatalf("%s: the package restore gives %v with %d preparations, want %s with %d", tc.name, wantErr, wantRuns, tc.wantErr, tc.wantRuns)
		}
		if err == nil || err.Error() != tc.wantErr || runs != tc.wantRuns {
			t.Errorf("%s: the restore of the check gives %v with %d preparations, want %s with %d", tc.name, err, runs, tc.wantErr, tc.wantRuns)
		}
	}
}
