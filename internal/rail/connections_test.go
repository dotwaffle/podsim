package rail

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func connectionPlan() []project.RailDeparture {
	return []project.RailDeparture{{ID: "outbound", Station: "harbor", AtSeconds: 10, WalkingSeconds: 1,
		Passengers: 4, Origins: []project.RailOrigin{{Station: "market", Weight: 1}, {Station: "garden", Weight: 1}}}}
}

func addAccepted(t *testing.T, c *Connections, count int) []project.RailServiceOffer {
	t.Helper()
	offers := project.RailServicesSchedule(nil, connectionPlan(), 7)
	for i, offer := range offers[:count] {
		if err := c.Add(offer, i+1, ""); err != nil {
			t.Fatal(err)
		}
	}
	return offers
}

func activeState(records []Connection, tick int64) sim.SavedState {
	state := sim.SavedState{Tick: tick}
	for _, record := range records {
		state.RequestID = max(state.RequestID, record.RequestID)
		if record.RequestID > 0 && record.AlightedTick == -1 && record.Outcome != "unserved" {
			state.Waiting = append(state.Waiting, sim.SavedTrip{Request: sim.SavedRequest{ID: record.RequestID,
				From: record.From, To: record.To, RequestedTick: record.RequestedTick, PartySize: 1}})
		}
	}
	return state
}

func TestConnectionDeadlineBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		alight  int64
		walking int
		outcome string
	}{
		{"early", 539, 1, "made"}, {"exact readiness", 540, 1, "made"},
		{"late readiness", 541, 1, "missed"}, {"zero walk exact", 600, 0, "made"},
		{"not arrived", -1, 1, "missed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := connectionPlan()
			plan[0].WalkingSeconds = tc.walking
			c := NewConnections(plan)
			offer := project.RailServicesSchedule(nil, plan, 7)[0]
			if err := c.Add(offer, 1, ""); err != nil {
				t.Fatal(err)
			}
			c.Advance(599, nil)
			if c.Counts() != (Counts{Unresolved: 1}) {
				t.Fatal("scored before departure")
			}
			var completion []sim.StepCompletion
			if tc.alight >= 0 {
				completion = []sim.StepCompletion{{RequestID: 1, AlightedTick: tc.alight}}
			}
			c.Advance(600, completion)
			c.Advance(600, completion)
			if got := c.Records()[0]; got.Outcome != tc.outcome || got.AlightedTick != tc.alight || c.Counts().Unresolved != 0 {
				t.Fatalf("outcome: %+v, counts %+v", got, c.Counts())
			}
		})
	}
}

func TestConnectionIssueAndCloneOwnership(t *testing.T) {
	t.Parallel()
	plan := connectionPlan()
	c := NewConnections(plan)
	offers := addAccepted(t, c, 2)
	if err := c.Add(offers[2], 0, "queue-limit"); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(offers[3], 0, "request-error"); err != nil {
		t.Fatal(err)
	}
	if c.Counts() != (Counts{Unresolved: 2, Unserved: 2}) || len(c.Records()) != 4 || cap(c.records) != 4 || cap(c.deadlines) != 4 {
		t.Fatalf("issued ledger bounds: %+v", c)
	}
	before, counts := c.Records(), c.Counts()
	if err := c.Add(offers[0], 9, ""); err == nil || !slices.Equal(c.Records(), before) || c.Counts() != counts {
		t.Fatal("duplicate offer changed ledger")
	}
	read := c.Records()
	read[0].From = "changed"
	clone := c.Clone()
	clone.Advance(540, []sim.StepCompletion{{RequestID: 1, AlightedTick: 540}})
	clone.Advance(600, nil)
	if !slices.Equal(c.Records(), before) || c.Counts() != counts || clone.Counts() != (Counts{Made: 1, Missed: 1, Unserved: 2}) {
		t.Fatal("clone or read changed source ledger")
	}
	clone.Advance(650, []sim.StepCompletion{{RequestID: 2, AlightedTick: 650}})
	if clone.Records()[1].Outcome != "missed" || clone.Records()[1].AlightedTick != 650 {
		t.Fatal("late completion erased miss or lost actual time")
	}
}

func TestConnectionRestoreRetainsAlightedAndActive(t *testing.T) {
	t.Parallel()
	c := NewConnections(connectionPlan())
	addAccepted(t, c, 2)
	c.Advance(540, []sim.StepCompletion{{RequestID: 1, AlightedTick: 540}})
	state := activeState(c.Records(), 550)
	restored, err := RestoreConnections(connectionPlan(), c.Records(), state, c.Counts())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.ReconcileRestore(state, sim.RestoreResult{Tier: sim.RestorePhysical}); err != nil {
		t.Fatal(err)
	}
	c.Advance(600, nil)
	restored.Advance(600, nil)
	if !slices.Equal(restored.Records(), c.Records()) || restored.Counts() != c.Counts() || c.Counts() != (Counts{Made: 1, Missed: 1}) {
		t.Fatal("restart lost actual readiness or accepted pending journey")
	}
}

func TestConnectionRestoreExplicitReceipts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		result sim.RestoreResult
		reason string
	}{
		{"drop", sim.RestoreResult{Dropped: []int{1}}, "restore-drop"},
		{"untimed unloading", sim.RestoreResult{LogicalCompleted: []int{1}}, "restore-degraded"},
		{"unaccounted alone", sim.RestoreResult{Unaccounted: 1}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := NewConnections(connectionPlan())
			addAccepted(t, c, 1)
			state := activeState(c.Records(), 100)
			state.Waiting = nil
			before := c.Records()
			err := c.ReconcileRestore(state, tc.result)
			if tc.reason == "" {
				if err == nil || !slices.Equal(c.Records(), before) {
					t.Fatal("unidentified missing request was accepted or changed ledger")
				}
				return
			}
			if err != nil || c.Records()[0].Outcome != "unserved" || c.Records()[0].Reason != tc.reason || c.Counts() != (Counts{Unserved: 1}) {
				t.Fatalf("explicit reconciliation: %+v, %v", c.Records(), err)
			}
			c.Advance(600, nil)
			if c.Counts() != (Counts{Unserved: 1}) {
				t.Fatal("departure rescored an unserved connection")
			}
		})
	}
	c := NewConnections(connectionPlan())
	addAccepted(t, c, 1)
	c.Advance(600, nil)
	before := c.Records()
	if err := c.ReconcileRestore(sim.SavedState{Tick: 600, RequestID: 1}, sim.RestoreResult{Dropped: []int{1}}); err != nil || !slices.Equal(before, c.Records()) {
		t.Fatal("later drop changed a terminal miss")
	}
}

func TestConnectionRestoreRejectsMalformedLedger(t *testing.T) {
	t.Parallel()
	base := NewConnections(connectionPlan())
	addAccepted(t, base, 2)
	for _, tc := range []struct {
		name string
		edit func([]Connection, *sim.SavedState, *Counts)
	}{
		{"missing binding", func(_ []Connection, s *sim.SavedState, _ *Counts) { s.Waiting = nil }},
		{"duplicate offer", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[1] = r[0] }},
		{"duplicate request", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[1].RequestID = r[0].RequestID }},
		{"missing event", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].Event = "missing" }},
		{"bad ordinal", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].Passenger = 0 }},
		{"bad origin", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].From = "harbor" }},
		{"bad hub", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].To = "market" }},
		{"bad release", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].RequestedTick++ }},
		{"future alighting", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].AlightedTick = 101 }},
		{"unknown outcome", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].Outcome = "other" }},
		{"pending reason", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].Reason = "restore-drop" }},
		{"early made", func(r []Connection, _ *sim.SavedState, _ *Counts) { r[0].Outcome, r[0].AlightedTick = "made", 1 }},
		{"wrong counts", func(_ []Connection, _ *sim.SavedState, c *Counts) { c.Unserved++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			records := base.Records()
			state, counts := activeState(records, 100), base.Counts()
			tc.edit(records, &state, &counts)
			if _, err := RestoreConnections(connectionPlan(), records, state, counts); err == nil {
				t.Fatal("accepted malformed ledger")
			}
		})
	}
	if !reflect.DeepEqual(base.Counts(), Counts{Unresolved: 2}) {
		t.Fatal("failed restores changed their source")
	}
}

func TestConnectionRestoreCompletedBindings(t *testing.T) {
	t.Parallel()
	c := NewConnections(connectionPlan())
	addAccepted(t, c, 1)
	state := activeState(c.Records(), 100)
	state.Waiting[0].Request.Completed = true
	restored, err := RestoreConnections(connectionPlan(), c.Records(), state, c.Counts())
	if err != nil {
		t.Fatal(err)
	}
	state.Waiting = nil
	if err := restored.ReconcileRestore(state, sim.RestoreResult{Dropped: []int{1}}); err != nil || restored.Counts() != (Counts{Unserved: 1}) {
		t.Fatalf("completed queue reconciliation: %v, %+v", err, restored.Counts())
	}
	state = activeState(c.Records(), 550)
	rider := state.Waiting[0].Request
	rider.Completed = true
	state.Waiting = nil
	state.Pods = []sim.SavedPod{{Riders: []sim.SavedRequest{rider}}}
	if _, err := RestoreConnections(connectionPlan(), c.Records(), state, c.Counts()); err == nil {
		t.Fatal("inferred completion from retained history")
	}
	c.Advance(540, []sim.StepCompletion{{RequestID: 1, AlightedTick: 540}})
	for _, tick := range []int64{550, 600} {
		c.Advance(tick, nil)
		state.Tick = tick
		if _, err := RestoreConnections(connectionPlan(), c.Records(), state, c.Counts()); err != nil {
			t.Fatal(err)
		}
		records := c.Records()
		for _, origin := range connectionPlan()[0].Origins {
			if origin.Station != rider.From {
				records[0].From = origin.Station
			}
		}
		if _, err := RestoreConnections(connectionPlan(), records, state, c.Counts()); err == nil {
			t.Fatal("accepted conflicting completed rider history")
		}
	}
}
