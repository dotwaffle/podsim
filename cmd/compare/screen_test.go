package main

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestScreenParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		event   screenEvent
		express bool
		want    []int
	}{
		{"background", screenEvent{Size: 1}, false, []int{1}},
		{"hub at the split size", screenEvent{Size: 4, Hub: true}, false, []int{4}},
		{"hub above the split size", screenEvent{Size: 5, Hub: true}, false, []int{4, 1}},
		{"largest hub party", screenEvent{Size: 20, Hub: true}, false, []int{4, 4, 4, 4, 4}},
		{"Express hub party", screenEvent{Size: 20, Hub: true}, true, []int{20}},
	}
	for _, test := range tests {
		if got := screenParts(test.event, test.express, 4); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s: parts %v, want %v", test.name, got, test.want)
		}
	}
	// LondonFull runs Compact pods, so a hub party splits into orders of 4.
	if split, err := screenSplitSize(scenarios.LondonFull().Fleet); err != nil || split != 4 {
		t.Errorf("LondonFull split size %d, %v, want 4", split, err)
	}
	if split, err := screenSplitSize(scenarios.LondonFullExpress().Fleet); err != nil || split != 4 {
		t.Errorf("London Express split size %d, %v, want 4", split, err)
	}
	// A Legacy pod has 8 seats and takes a party of 1.
	if split, err := screenSplitSize([]sim.Placement{{ID: "l", Class: sim.LegacyClass}, {ID: "c", Class: sim.CompactClass}}); err != nil || split != 4 {
		t.Errorf("Legacy and Compact split size %d, %v, want 4", split, err)
	}
	if _, err := screenSplitSize([]sim.Placement{{ID: "x", Class: sim.ExpressClass}}); err == nil {
		t.Error("an Express-only fleet has a split size")
	}
}

// TestScreenEvents checks that the events of a seed repeat, that the hub
// stream leaves the background schedule unchanged, and the hub party shape.
func TestScreenEvents(t *testing.T) {
	t.Parallel()
	source := scenarios.LondonFull()
	input := screenEventsInput{seed: 2, band: 1, arrivalTicks: 20 * 60 * sim.TicksPerSecond, intervalTicks: 6 * sim.TicksPerSecond, source: source}
	events, err := screenEvents(input)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := screenEvents(input)
	if !reflect.DeepEqual(events, again) {
		t.Fatal("the events of a seed differ between two calls")
	}
	base, err := newScenario(source, "")
	if err != nil {
		t.Fatal(err)
	}
	background := demandSchedule(scheduleInput{seed: input.seed, durationTicks: input.arrivalTicks, intervalTicks: input.intervalTicks, pattern: "profile",
		passengers: base.passengers, focus: base.focus, profileFlows: weightedProfileFlows(source.DemandProfiles[0], input.band)})
	var got []scheduledRequest
	hubs, outbound := 0, 0
	for index, event := range events {
		if event.ID != index || index > 0 && event.Tick < events[index-1].Tick {
			t.Fatalf("event %d: ID %d at tick %d is out of order", index, event.ID, event.Tick)
		}
		if !event.Hub {
			got = append(got, scheduledRequest{tick: event.Tick, origin: event.From, destination: event.To})
			continue
		}
		hubs++
		if event.Outbound {
			outbound++
		}
		if event.Size < 1 || event.Size > sim.MaxExpressParties || !slices.Contains([]string{screenWaterloo, screenBank}, event.From) || event.From == event.To {
			t.Fatalf("hub event %+v", event)
		}
	}
	for index := range background {
		background[index] = scheduledRequest{tick: background[index].tick, origin: background[index].origin, destination: background[index].destination}
	}
	if !reflect.DeepEqual(got, background) {
		t.Fatal("the hub stream changed the background schedule")
	}
	// 20 minutes at a mean of 15 s gives about 80 hub events.
	if hubs < 50 || hubs > 110 || outbound*2 < hubs {
		t.Fatalf("%d hub events, %d outbound", hubs, outbound)
	}
}

func TestScreenTimes(t *testing.T) {
	t.Parallel()
	events := []screenEvent{{Tick: 10}, {Tick: 20}, {Tick: 30}, {Tick: 40}}
	outcomes := []screenOutcome{
		{Orders: []int{1, 2}, WaitTicks: -1, JourneyTicks: -1},
		{Orders: []int{3, 4}, WaitTicks: -1, JourneyTicks: -1},
		{Orders: []int{5}, WaitTicks: -1, JourneyTicks: -1},
		{Refused: true, WaitTicks: -1, JourneyTicks: -1},
	}
	timings := []sim.RequestTiming{
		{RequestID: 1, BoardedTick: 50, CompletedTick: 300},
		{RequestID: 2, BoardedTick: 70, CompletedTick: 200},
		{RequestID: 3, BoardedTick: 90, CompletedTick: -1},
		{RequestID: 4, BoardedTick: 80, CompletedTick: 400},
	}
	screenTimes(outcomes, events, timings)
	want := [][2]int64{{60, 290}, {70, -1}, {-1, -1}, {-1, -1}}
	for index, outcome := range outcomes {
		if got := [2]int64{outcome.WaitTicks, outcome.JourneyTicks}; got != want[index] {
			t.Errorf("event %d: wait and journey %v, want %v", index, got, want[index])
		}
	}
}

// TestScreenCompare checks each gate at its limit and one tick past it.
func TestScreenCompare(t *testing.T) {
	t.Parallel()
	const second = sim.TicksPerSecond
	events := []screenEvent{{Size: 1}, {Size: 1}, {Size: 12, Hub: true}}
	reference := screenArm{Outcomes: []screenOutcome{
		{WaitTicks: 100 * second, JourneyTicks: 2000 * second},
		{WaitTicks: 1000 * second, JourneyTicks: 3000 * second},
		{WaitTicks: 10, JourneyTicks: 10},
	}}
	candidate := func(waits, journeys [2]int64, hub int64) screenArm {
		return screenArm{Outcomes: []screenOutcome{
			{WaitTicks: waits[0], JourneyTicks: journeys[0]},
			{WaitTicks: waits[1], JourneyTicks: journeys[1]},
			{WaitTicks: 10, JourneyTicks: hub},
		}}
	}
	gates := func(arm screenArm) map[string]bool {
		result := map[string]bool{}
		for _, gate := range screenCompare(arm, reference, events) {
			result[gate.Name] = gate.Pass
		}
		return result
	}
	// The limits: 30 s on a wait of 100 s, 5% (50 s) on a wait of 1000 s,
	// 100 s on a journey of 2000 s, and 150 s on a journey of 3000 s.
	atLimit := candidate([2]int64{130 * second, 1050 * second}, [2]int64{2100 * second, 3150 * second}, 10)
	if got := gates(atLimit); !got["background pickup wait"] || !got["background journey"] || !got["live queue loss"] || got["hub benefit"] {
		t.Fatalf("at the limits: %v", got)
	}
	past := candidate([2]int64{130*second + 1, 1050 * second}, [2]int64{2100 * second, 3150*second + 1}, 10)
	if got := gates(past); got["background pickup wait"] || got["background journey"] {
		t.Fatalf("one tick past the limits: %v", got)
	}
	censored := candidate([2]int64{-1, 1000 * second}, [2]int64{-1, 3000 * second}, 10)
	if got := gates(censored); got["background pickup wait"] || got["background journey"] {
		t.Fatalf("a censored candidate passes: %v", got)
	}
	refused := atLimit
	refused.Outcomes = slices.Clone(refused.Outcomes)
	refused.Outcomes[0] = screenOutcome{Refused: true, WaitTicks: -1, JourneyTicks: -1}
	if got := gates(refused); got["live queue loss"] || !got["background pickup wait"] {
		t.Fatalf("a refused event: %v", got)
	}
	reference.Outcomes[2].JourneyTicks = -1
	if got := gates(atLimit); !got["hub benefit"] {
		t.Fatalf("12 hub passengers against none: %v", got)
	}
	failed := atLimit
	failed.Failure, failed.Outstanding = "safety: x", 1
	if got := gates(failed); got["safety and accounting"] || got["finite recovery"] {
		t.Fatalf("a failed arm: %v", got)
	}
}

func TestScreenConfigs(t *testing.T) {
	t.Parallel()
	configs, err := screenConfigs([]string{screenLondonFull, screenGeometry, screenExpress})
	if err != nil {
		t.Fatal(err)
	}
	express := func(index int) int {
		count := 0
		for _, pod := range configs[index].config.Fleet {
			if pod.Class == sim.ExpressClass {
				count++
			}
		}
		return count
	}
	if express(0) != 0 || express(1) != 0 || express(2) != 16 {
		t.Fatalf("Express pods %d, %d, %d", express(0), express(1), express(2))
	}
	if configs[1].config.OrderContract != "" || len(configs[1].config.ExpressServices) != 0 || configs[2].config.OrderContract != sim.ExpressOrderContract {
		t.Fatal("D0 keeps an Express order member, or D has none")
	}
	if len(configs[1].config.Fleet) != len(configs[0].config.Fleet) || len(configs[1].config.Network.Lanes) != len(configs[2].config.Network.Lanes) {
		t.Fatal("D0 does not have the fleet of LF and the network of D")
	}
}

// TestScreenRun runs a short screen of LF and D, and checks that the hub
// parties ride Express in D and split in LF.
func TestScreenRun(t *testing.T) {
	t.Parallel()
	skipLong(t)
	var stdout, stderr bytes.Buffer
	code := runCLI(cliInput{args: []string{"screen", "-seeds", "1", "-bands", "busy", "-configs", "LF,D", "-arrivals", "3m", "-clear", "20m", "-workers", "2"}, stdout: &stdout, stderr: &stderr})
	if code != 0 && code != 3 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"arm LF band=", "arm D band=", `pair D vs LF`, `gate="safety and accounting" pass=true`, "screen pass="} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `failure="`+"s") {
		t.Fatalf("an arm failed:\n%s", out)
	}
}

func TestParseScreenOptions(t *testing.T) {
	t.Parallel()
	opts, err := parseScreenOptions(nil, &bytes.Buffer{})
	if err != nil || !reflect.DeepEqual(opts.seeds, []int64{1, 2, 3}) || opts.arrivals != 2*time.Hour || opts.clear != 2*time.Hour || opts.queueLimit != 200 || len(opts.configs) != 3 {
		t.Fatalf("defaults %+v, %v", opts, err)
	}
	for _, args := range [][]string{{"-seeds", "1,1"}, {"-seeds", "0"}, {"-configs", "X"}, {"-arrivals", "0s"}, {"-workers", "0"}, {"extra"}} {
		if _, err := parseScreenOptions(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

// TestScreenOfferQueueLimit checks that the queue limit admits a whole event
// or refuses it before its first order.
func TestScreenOfferQueueLimit(t *testing.T) {
	t.Parallel()
	config := project.Default()
	base, err := newScenario(config, "")
	if err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.NewFleetWithOrderContract(config.Network, config.Fleet, config.OrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := simulation.SetSharedRidePartyLimit(4); err != nil {
		t.Fatal(err)
	}
	event := screenEvent{From: base.passengers[0], To: base.passengers[1], Size: 2, Hub: true}
	input := screenOfferInput{split: 1, consent: sim.SharedConsent, queueLimit: 1}
	if outcome := screenOffer(simulation, event, input); !outcome.Refused || len(outcome.Orders) != 0 || simulation.PendingCount() != 0 {
		t.Fatalf("two orders over a queue limit of 1: %+v, %d pending", outcome, simulation.PendingCount())
	}
	input.queueLimit = 2
	if outcome := screenOffer(simulation, event, input); outcome.Refused || outcome.Partial || len(outcome.Orders) != 2 {
		t.Fatalf("two orders at a queue limit of 2: %+v, %d pending", outcome, simulation.PendingCount())
	}
}
