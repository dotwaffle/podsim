package parkride

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

type fakePods struct {
	pending, nextID int
	options         []sim.TripOptions
	increasePending bool
	err             error
	badID           bool
}

func (p *fakePods) PendingCount() int { return p.pending }
func (p *fakePods) SubmitTripOptions(options sim.TripOptions) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	p.options = append(p.options, options)
	p.nextID++
	if p.increasePending {
		p.pending++
	}
	if p.badID {
		return 0, nil
	}
	return p.nextID, nil
}

func testLedger(t *testing.T, plan Plan, horizon int64) *ledger {
	t.Helper()
	normalized, err := normalizePlan(plan, sim.Example())
	if err != nil {
		t.Fatal(err)
	}
	return newLedger(normalized, 1, horizon)
}

func advanceLedger(t *testing.T, l *ledger, p *fakePods, tick int64, completions ...sim.StepCompletion) {
	t.Helper()
	for next := l.lastTick + 1; next <= tick; next++ {
		var receipts []sim.StepCompletion
		if next == tick {
			receipts = completions
		}
		if err := l.advance(next, receipts, p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLedgerCapacityAndRefusalRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		capacity     int64
		pending      int
		wantA, wantB string
		held         int64
	}{
		{"zero-lot", 0, 0, "full-lot", "full-lot", 0},
		{"one-lot", 1, 0, "", "full-lot", 1},
		{"refusal-release", 1, 1, "recovered-refusal", "recovered-refusal", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := testPlan()
			plan.Lots[0].Capacity = tc.capacity
			second := plan.Itineraries[0]
			second.ID, second.CarID = "b", "car-b"
			plan.Itineraries = []Itinerary{second, plan.Itineraries[0]}
			l := testLedger(t, plan, 60)
			p := &fakePods{pending: tc.pending}
			advanceLedger(t, l, p, 0)
			if l.records[0].Itinerary.ID != "a" || l.records[0].Outcome != tc.wantA || l.records[1].Outcome != tc.wantB || l.lots[0].Occupancy != tc.held {
				t.Fatalf("records %+v lots %+v", l.records, l.lots)
			}
			if tc.pending == 1 && (len(p.options) != 0 || l.records[0].HomeArrivalTick != 0 || l.records[0].CarReleaseTick != 0 || l.records[0].DoorToDoorTicks != nil) {
				t.Fatal("refusal invented orders, delayed zero transitions, or claimed success")
			}
			if tc.capacity == 0 && len(p.options) != 0 {
				t.Fatal("full lot generated pod offers")
			}
		})
	}
}

func TestLedgerReturnRequiresActualCompletion(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	plan.Itineraries[0].ReturnNotBeforeSeconds = 2
	plan.Itineraries[0].ActivitySeconds = 1
	l := testLedger(t, plan, 1000)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	advanceLedger(t, l, p, 180)
	if len(p.options) != 1 || l.records[0].Return.OfferedTick != -1 {
		t.Fatal("clock generated return before outward completion")
	}
	advanceLedger(t, l, p, 200, sim.StepCompletion{RequestID: 1, AlightedTick: 200})
	if l.records[0].ReturnEligibleTick != 260 || l.records[0].Stage != "activity" {
		t.Fatal("late outward completion lost minimum activity")
	}
	advanceLedger(t, l, p, 259)
	if len(p.options) != 1 {
		t.Fatal("early return")
	}
	advanceLedger(t, l, p, 260)
	if len(p.options) != 2 || p.options[1].From != "market" || p.options[1].To != "harbor" {
		t.Fatal("return missing or not paired")
	}
	advanceLedger(t, l, p, 261, sim.StepCompletion{RequestID: 2, AlightedTick: 261})
	if l.records[0].Outcome != "completed" || l.records[0].HomeArrivalTick != 261 || l.lots[0].Occupancy != 0 {
		t.Fatal("zero retrieval/home did not close at completion")
	}
	before := l.clone()
	if err := l.advance(261, []sim.StepCompletion{{RequestID: 1, AlightedTick: 200}, {RequestID: 2, AlightedTick: 261}}, p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l, before) {
		t.Fatal("duplicate completion changed ledger")
	}
}

func TestLedgerNotBeforeAndRetrieval(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	plan.Itineraries[0].ReturnNotBeforeSeconds = 3
	plan.Itineraries[0].RetrievalSeconds = 1
	plan.Itineraries[0].HomeboundSeconds = 1
	l := testLedger(t, plan, 1000)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	advanceLedger(t, l, p, 1, sim.StepCompletion{RequestID: 1, AlightedTick: 1})
	if l.records[0].ReturnEligibleTick != 180 {
		t.Fatal("return not-before ignored")
	}
	advanceLedger(t, l, p, 180)
	advanceLedger(t, l, p, 181, sim.StepCompletion{RequestID: 2, AlightedTick: 181})
	advanceLedger(t, l, p, 240)
	if l.lots[0].Occupancy != 1 {
		t.Fatal("slot released before retrieval")
	}
	advanceLedger(t, l, p, 241)
	if l.lots[0].Occupancy != 0 || l.records[0].Outcome != "" {
		t.Fatal("home travel did not follow release")
	}
	advanceLedger(t, l, p, 301)
	if l.records[0].Outcome != "completed" || *l.records[0].DoorToDoorTicks != 301 {
		t.Fatal("door-to-door completion mismatch")
	}
}

func TestLedgerReturnBeforeArrivalAndStranding(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	plan.Lots[0].Capacity = 2
	second := plan.Itineraries[0]
	second.ID, second.CarID, second.DepartureSeconds = "b", "car-b", 1
	plan.Itineraries = append(plan.Itineraries, second)
	l := testLedger(t, plan, 1000)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	p.increasePending = true
	advanceLedger(t, l, p, 60, sim.StepCompletion{RequestID: 1, AlightedTick: 60})
	if len(p.options) != 2 || p.options[1].From != "market" || l.records[1].Outward.Reason != "queue-limit" || l.records[1].Outcome != "recovered-refusal" {
		t.Fatal("arrival took queue priority over return")
	}
	advanceLedger(t, l, p, 61, sim.StepCompletion{RequestID: 2, AlightedTick: 61})
	if l.lots[0].Occupancy != 0 {
		t.Fatal("completed return retained slot")
	}
	stranded := testLedger(t, testPlan(), 1000)
	q := &fakePods{}
	advanceLedger(t, stranded, q, 0)
	q.pending = 1
	advanceLedger(t, stranded, q, 1, sim.StepCompletion{RequestID: 1, AlightedTick: 1})
	if stranded.records[0].Outcome != "stranded" || !stranded.records[0].Held || stranded.terminal != 1 || len(q.options) != 1 {
		t.Fatal("return refusal released or retried")
	}
	advanceLedger(t, stranded, q, 1000)
	if stranded.lots[0].Occupancy != 1 {
		t.Fatal("horizon released stranded car")
	}
}

func TestLedgerReleaseBeforeSimultaneousArrival(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	other := plan.Itineraries[0]
	other.ID, other.CarID, other.DepartureSeconds = "b", "car-b", 1
	plan.Itineraries = append(plan.Itineraries, other)
	l := testLedger(t, plan, 1000)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	advanceLedger(t, l, p, 1, sim.StepCompletion{RequestID: 1, AlightedTick: 1})
	advanceLedger(t, l, p, 60, sim.StepCompletion{RequestID: 2, AlightedTick: 60})
	if l.records[0].Outcome != "completed" || l.records[1].Outward.RequestID != 3 || l.lots[0].Occupancy != 1 || l.lots[0].Peak != 1 {
		t.Fatal("release did not precede arrival")
	}
}

func TestLedgerHorizonTransitions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		capacity int64
		want     string
		held     bool
	}{
		{"admitted-at-cap", 1, "censored", true}, {"full-at-cap", 0, "full-lot", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := testPlan()
			plan.Lots[0].Capacity = tc.capacity
			plan.Itineraries[0].DepartureSeconds = 1
			l := testLedger(t, plan, 60)
			p := &fakePods{}
			advanceLedger(t, l, p, 60)
			records, _ := l.reports()
			if len(p.options) != 0 || records[0].Outcome != tc.want || records[0].Held != tc.held || records[0].CarArrivalTick != 60 || records[0].Outward.OfferedTick != -1 {
				t.Fatalf("horizon arrival %+v", records[0])
			}
		})
	}
	returning := testLedger(t, testPlan(), 60)
	p := &fakePods{}
	advanceLedger(t, returning, p, 0)
	advanceLedger(t, returning, p, 60, sim.StepCompletion{RequestID: 1, AlightedTick: 60})
	if returning.records[0].Return.OfferedTick != -1 || !returning.records[0].Held {
		t.Fatal("return offered at cap")
	}
	finishing := testLedger(t, testPlan(), 60)
	q := &fakePods{}
	advanceLedger(t, finishing, q, 0)
	advanceLedger(t, finishing, q, 1, sim.StepCompletion{RequestID: 1, AlightedTick: 1})
	advanceLedger(t, finishing, q, 60, sim.StepCompletion{RequestID: 2, AlightedTick: 60})
	if finishing.records[0].Outcome != "completed" || finishing.records[0].HomeArrivalTick != 60 {
		t.Fatal("completion at horizon was censored")
	}
}

func TestLedgerOvernightAndCloneOwnership(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	plan.Itineraries[0].ReturnNotBeforeSeconds = 86401
	l := testLedger(t, plan, MaxHorizonTicks)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	advanceLedger(t, l, p, 1, sim.StepCompletion{RequestID: 1, AlightedTick: 1})
	clone := l.clone()
	clone.records[0].Itinerary.CarID = "changed"
	clone.lots[0].Occupancy = 0
	delete(clone.lotIndexes, "lot")
	if l.records[0].Itinerary.CarID != "car-a" || l.lots[0].Occupancy != 1 || len(l.lotIndexes) != 1 {
		t.Fatal("clone aliases ledger")
	}
	advanceLedger(t, l, p, MaxHorizonTicks)
	records, lots := l.reports()
	if records[0].Outcome != "censored" || !records[0].Held || lots[0].Occupancy != 1 || len(p.options) != 1 {
		t.Fatal("midnight/horizon reset occupancy or issued future return")
	}
	records[0].Itinerary.CarID = "changed"
	if l.records[0].Itinerary.CarID != "car-a" {
		t.Fatal("report aliases ledger")
	}
}

func TestLedgerRuntimeGuardsAndPartialError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		tick int64
	}{{"negative", -1}, {"initial-gap", 1}, {"beyond-horizon", 61}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := testLedger(t, testPlan(), 60)
			if err := l.advance(tc.tick, nil, &fakePods{}); err == nil {
				t.Fatal("invalid transition tick accepted")
			}
		})
	}
	l := testLedger(t, testPlan(), 60)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	advanceLedger(t, l, p, 1)
	if err := l.advance(0, nil, p); err == nil {
		t.Fatal("tick regression accepted")
	}
	if err := l.advance(2, []sim.StepCompletion{{RequestID: 1, AlightedTick: -1}}, p); err == nil {
		t.Fatal("negative completion accepted")
	}
	if err := l.advance(2, []sim.StepCompletion{{RequestID: 1, AlightedTick: 3}}, p); err == nil {
		t.Fatal("future completion accepted")
	}
	for _, tc := range []struct {
		name string
		pods *fakePods
	}{{"submission-error", &fakePods{err: errors.New("native fault")}}, {"invalid-receipt", &fakePods{badID: true}}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ledger := testLedger(t, testPlan(), 60)
			if err := ledger.advance(0, nil, tc.pods); err == nil {
				t.Fatal("runtime error accepted")
			}
			record := ledger.records[0]
			if record.Outcome != "error" || !record.Held || record.CarReleaseTick != -1 || record.HomeArrivalTick != -1 {
				t.Fatal("native error invented recovery")
			}
		})
	}
}

func TestLedgerDueReturnsUseIdentityOrder(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	plan.Lots[0].Capacity = 2
	second := plan.Itineraries[0]
	second.ID, second.CarID = "b", "car-b"
	plan.Itineraries = append(plan.Itineraries, second)
	l := testLedger(t, plan, 1000)
	p := &fakePods{}
	advanceLedger(t, l, p, 0)
	p.increasePending = true
	// Deliver the older receipt first. Due offers still use itinerary ID order.
	advanceLedger(t, l, p, 60, sim.StepCompletion{RequestID: 2, AlightedTick: 1}, sim.StepCompletion{RequestID: 1, AlightedTick: 60})
	if l.records[0].Return.RequestID != 3 || l.records[1].Outcome != "stranded" || p.options[2].From != "market" {
		t.Fatal("due returns used receipt or deadline order")
	}
}

func TestLedgerAuthoredCarLegs(t *testing.T) {
	t.Parallel()
	plan := testPlan()
	plan.Itineraries[0].DepartureSeconds, plan.Itineraries[0].OutwardSeconds = 1, 2
	l := testLedger(t, plan, 1000)
	p := &fakePods{}
	advanceLedger(t, l, p, 59)
	if l.records[0].Stage != "planned" {
		t.Fatal("car departed early")
	}
	advanceLedger(t, l, p, 60)
	if l.records[0].Stage != "car-out" || len(p.options) != 0 {
		t.Fatal("car travel omitted")
	}
	advanceLedger(t, l, p, 179)
	if l.records[0].Held {
		t.Fatal("car parked before authored arrival")
	}
	advanceLedger(t, l, p, 180)
	if !l.records[0].Held || l.records[0].Outward.OfferedTick != 180 || l.records[0].CarArrivalTick != 180 {
		t.Fatal("authored car arrival changed")
	}
}
