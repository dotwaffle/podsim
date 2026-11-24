package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	input := screenEventsInput{seed: 2, band: 1, arrivalTicks: 20 * 60 * sim.TicksPerSecond, intervalTicks: 6 * sim.TicksPerSecond, lines: screenKnownLines(screenHubMean)[:1], source: source}
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
	// A longer mean thins the hub stream and keeps the background.
	input.lines = screenKnownLines(120 * sim.TicksPerSecond)[:1]
	light, err := screenEvents(input)
	if err != nil {
		t.Fatal(err)
	}
	lightHubs := 0
	for _, event := range light {
		if event.Hub {
			lightHubs++
		}
	}
	if lightHubs < 3 || lightHubs > 20 || len(light)-lightHubs != len(background) {
		t.Fatalf("%d hub events of %d at a mean of 120 s", lightHubs, len(light))
	}
}

// TestScreenLines checks that the hub lines come from the Express services
// of the configurations, that a configuration without its Express keeps its
// lines, and that an unknown service is an error.
func TestScreenLines(t *testing.T) {
	t.Parallel()
	known := screenKnownLines(screenHubMean)
	configs, err := screenConfigs([]string{screenLondonFull, screenExpress, screenGeometry, screenShared, screenWharf, screenWharfPlain}, known)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range [][]screenLine{nil, known[:1], known[:1], known[:1], known, known} {
		if !slices.Equal(configs[index].lines, want) {
			t.Errorf("%s: lines %+v, want %+v", configs[index].name, configs[index].lines, want)
		}
	}
	plain := configs[5].config
	if plain.OrderContract != "" || len(plain.ExpressServices) != 0 || len(plain.Fleet) != len(configs[0].config.Fleet) || !reflect.DeepEqual(plain.Network, configs[4].config.Network) {
		t.Error("W0 does not have the fleet of LF, the network of W, and no Express")
	}
	for _, test := range []struct {
		configs []screenConfig
		want    []screenLine
	}{
		{configs[:1], known[:1]}, {configs[:4], known[:1]}, {configs[5:], known},
	} {
		if got := screenLines(test.configs, known); !slices.Equal(got, test.want) {
			t.Errorf("screen lines %+v, want %+v", got, test.want)
		}
	}
	if _, err := screenServiceLines([]sim.ExpressService{{ID: "x", From: screenWaterloo, To: screenCanaryWharf}}, known); err == nil || err.Error() != "service x has no hub line" {
		t.Errorf("an unknown service: %v", err)
	}
}

// TestScreenWharfEvents checks that the events of the wharf line change no
// other event, and the rate and the share of the line.
func TestScreenWharfEvents(t *testing.T) {
	t.Parallel()
	known := screenKnownLines(screenHubMean)
	input := screenEventsInput{seed: 2, band: 1, arrivalTicks: 2 * 3600 * sim.TicksPerSecond, intervalTicks: 6 * sim.TicksPerSecond, lines: known[:1], source: scenarios.LondonFull()}
	one, err := screenEvents(input)
	if err != nil {
		t.Fatal(err)
	}
	input.lines = known
	both, err := screenEvents(input)
	if err != nil {
		t.Fatal(err)
	}
	var rest []screenEvent
	wharf, outbound := 0, 0
	for _, event := range both {
		if event.Line == 0 {
			event.ID = len(rest)
			rest = append(rest, event)
			continue
		}
		wharf++
		from, to := screenCanaryWharf, screenLondonBridge
		if event.Outbound {
			outbound++
			from, to = to, from
		}
		if !event.Hub || event.From != from || event.To != to {
			t.Fatalf("wharf event %+v", event)
		}
	}
	if !reflect.DeepEqual(rest, one) {
		t.Fatal("the wharf line changed the other events")
	}
	// The lines have independent streams.
	line := known[1]
	line.stream = known[0].stream
	input.lines = []screenLine{known[0], line}
	if same, err := screenEvents(input); err != nil || reflect.DeepEqual(same, both) {
		t.Fatalf("the stream of the wharf line has no effect: %v", err)
	}
	// Two hours at a mean of 28 s give about 257 events, 17 in 20 outbound.
	if wharf < 210 || wharf > 305 || outbound*100 < wharf*78 || outbound*100 > wharf*92 {
		t.Fatalf("%d wharf events, %d outbound", wharf, outbound)
	}
}

// TestScreenOfferService checks that a hub party is one Express order on a
// line that the arm serves, and splits on another line.
func TestScreenOfferService(t *testing.T) {
	t.Parallel()
	configs, err := screenConfigs([]string{screenShared}, screenKnownLines(screenHubMean))
	if err != nil {
		t.Fatal(err)
	}
	config := configs[0].config
	simulation, err := sim.NewFleetWithOrderContract(config.Network, config.Fleet, config.OrderContract)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.ConfigureSharedRides(simulation, config); err != nil {
		t.Fatal(err)
	}
	if err := project.ConfigureExperiments(simulation, config, false); err != nil {
		t.Fatal(err)
	}
	input := screenOfferInput{split: 4, services: map[string]string{}, consent: sim.SharedConsent, queueLimit: 200}
	for _, service := range config.ExpressServices {
		input.services[service.From+" "+service.To] = service.ID
	}
	if outcome := screenOffer(simulation, screenEvent{From: screenWaterloo, To: screenBank, Size: 10, Hub: true}, input); outcome.Error != "" || len(outcome.Orders) != 1 {
		t.Errorf("a served line: %+v", outcome)
	}
	if outcome := screenOffer(simulation, screenEvent{From: screenLondonBridge, To: screenCanaryWharf, Size: 10, Hub: true}, input); outcome.Error != "" || len(outcome.Orders) != 3 {
		t.Errorf("a line without a service: %+v", outcome)
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
	configs, err := screenConfigs([]string{screenLondonFull, screenGeometry, screenExpress, screenSharedPlain, screenShared}, screenKnownLines(screenHubMean))
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
	if express(3) != 0 || express(4) != 16 || configs[3].config.OrderContract != "" || len(configs[3].config.ExpressServices) != 0 || configs[4].config.OrderContract != sim.ExpressOrderContract {
		t.Fatal("S0 keeps an Express member, or S has none")
	}
	if len(configs[3].config.Fleet) != len(configs[0].config.Fleet) || len(configs[3].config.Network.Lanes) != len(configs[4].config.Network.Lanes) || len(configs[4].config.Network.Lanes) == len(configs[2].config.Network.Lanes) {
		t.Fatal("S0 does not have the fleet of LF and the network of S")
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
	for _, want := range []string{"arm LF band=", "arm D band=", `pair D vs LF`, `gate="safety and accounting" pass=true`, "screen result="} {
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
	if err != nil || !reflect.DeepEqual(opts.seeds, []int64{1, 2, 3}) || opts.arrivals != 2*time.Hour || opts.clear != 2*time.Hour || opts.queueLimit != 200 || opts.hubMean != 15*time.Second || len(opts.configs) != 3 {
		t.Fatalf("defaults %+v, %v", opts, err)
	}
	for _, args := range [][]string{{"-seeds", "1,1"}, {"-seeds", "0"}, {"-configs", "X"}, {"-arrivals", "0s"}, {"-workers", "0"}, {"-hub-mean", "14s"}, {"-hub-mean", "25h"}, {"extra"}} {
		if _, err := parseScreenOptions(args, &bytes.Buffer{}); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
	// At 1800 a minute, the interval is one tick. A higher rate has an
	// interval of 0 ticks.
	if opts, err := parseScreenOptions([]string{"-background-per-minute", "1800"}, &bytes.Buffer{}); err != nil || opts.backgroundPerMin != 1800 {
		t.Errorf("1800 a minute: %+v, %v", opts, err)
	}
	const want = "background-per-minute must be from 0 to 1800 and queue-limit from 1 to the queue cap"
	if _, err := parseScreenOptions([]string{"-background-per-minute", "1801"}, &bytes.Buffer{}); err == nil || err.Error() != want {
		t.Errorf("1801 a minute: got %v, want %q", err, want)
	}
}

// TestParseScreenSeedCount checks the cap on the number of seeds. The count
// comes before the values, as in the -seeds flag of a comparison.
func TestParseScreenSeedCount(t *testing.T) {
	t.Parallel()
	if opts, err := parseScreenOptions([]string{"-seeds", seedList(maxSeeds)}, &bytes.Buffer{}); err != nil || len(opts.seeds) != maxSeeds {
		t.Fatalf("%d seeds: %d seeds, %v", maxSeeds, len(opts.seeds), err)
	}
	const want = "seeds must contain at most 100 values"
	for _, list := range []string{seedList(maxSeeds + 1), seedList(maxSeeds) + ",x", strings.Repeat(",", maxSeeds)} {
		if _, err := parseScreenOptions([]string{"-seeds", list}, &bytes.Buffer{}); err == nil || err.Error() != want {
			t.Errorf("%.20q: got %v, want %q", list, err, want)
		}
	}
}

// failingWriter fails only the write after the first ok writes, so that a
// write error that one line drops does not show again on the next line.
type failingWriter struct{ ok, writes int }

var errWriteFailed = errors.New("write failed")

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.ok+1 {
		return 0, errWriteFailed
	}
	return len(p), nil
}

// TestScreenSummaryWriteError checks that the screen reports a failed write
// of each summary line, still writes the JSON report, and exits with
// status 1, also when a deciding gate failed.
func TestScreenSummaryWriteError(t *testing.T) {
	t.Parallel()
	report := screenReport{SchemaVersion: 5, Arms: []screenArm{{Config: screenLondonFull, Band: "b", Seed: 1}},
		Pairs: []screenPair{{Band: "b", Seed: 1, Candidate: screenExpress, Reference: screenLondonFull, Decides: true, Gates: []screenGate{{Name: "g"}}}}, Result: screenFailed}
	// The summary has an arm line, a gate line, and the result line.
	for ok := range 3 {
		err := writeScreenSummary(&failingWriter{ok: ok}, report)
		if !errors.Is(err, errWriteFailed) || !strings.HasPrefix(err.Error(), "write screen summary: ") {
			t.Errorf("failure at write %d: %v", ok+1, err)
		}
	}
	if err := writeScreenSummary(&failingWriter{ok: 3}, report); err != nil {
		t.Fatalf("three writes: %v", err)
	}
	output := filepath.Join(t.TempDir(), "screen.json")
	var stderr bytes.Buffer
	if code := writeScreenResults(report, output, &failingWriter{}, &stderr); code != 1 {
		t.Errorf("exit %d, want 1: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "write screen summary: write failed") {
		t.Errorf("stderr %q", stderr.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Errorf("no JSON report: %v", err)
	}
	if code := writeScreenResults(report, "", &bytes.Buffer{}, &bytes.Buffer{}); code != 3 {
		t.Errorf("a failed deciding gate: exit %d, want 3", code)
	}
}

// TestScreenResult checks that only a pair of D decides the screen, and
// that a screen with no pair of D is unevaluated: it does not pass, and it
// exits with status 4.
func TestScreenResult(t *testing.T) {
	t.Parallel()
	events := map[screenKey][]screenEvent{{"b", 1}: {{Size: 1, Hub: true}}}
	arm := func(config string, outstanding int, finished bool) screenArm {
		outcome := screenOutcome{WaitTicks: -1, JourneyTicks: -1}
		if finished {
			outcome = screenOutcome{WaitTicks: 10, JourneyTicks: 10}
		}
		return screenArm{Config: config, Band: "b", Seed: 1, Outstanding: outstanding, Outcomes: []screenOutcome{outcome}}
	}
	reference := arm(screenLondonFull, 0, false)
	passD, failD := arm(screenExpress, 0, true), arm(screenExpress, 1, true)
	passD0, failD0 := arm(screenGeometry, 0, true), arm(screenGeometry, 1, true)
	for _, test := range []struct {
		name  string
		arms  []screenArm
		pairs int
		want  screenResult
		code  int
	}{
		{"LF and D", []screenArm{reference, passD}, 1, screenPassed, 0},
		{"a failed D0 does not decide", []screenArm{reference, failD0, passD}, 2, screenPassed, 0},
		{"D with orders still waiting", []screenArm{reference, passD0, failD}, 2, screenFailed, 3},
		{"D alone", []screenArm{failD}, 0, screenUnevaluated, 4},
		{"LF and D0", []screenArm{reference, passD0}, 1, screenUnevaluated, 4},
		{"D0 and D", []screenArm{passD0, passD}, 0, screenUnevaluated, 4},
		{"LF, S0, and S do not decide", []screenArm{reference, arm(screenSharedPlain, 0, true), arm(screenShared, 1, true)}, 3, screenUnevaluated, 4},
		{"a failed S does not decide", []screenArm{reference, passD, arm(screenShared, 1, true)}, 3, screenPassed, 0},
	} {
		pairs, result := screenPairs(test.arms, events)
		if len(pairs) != test.pairs || result != test.want {
			t.Errorf("%s: %d pairs, result %q; want %d, %q", test.name, len(pairs), result, test.pairs, test.want)
			continue
		}
		var stdout bytes.Buffer
		output := filepath.Join(t.TempDir(), "screen.json")
		report := screenReport{SchemaVersion: 5, Arms: test.arms, Pairs: pairs, Result: result}
		if code := writeScreenResults(report, output, &stdout, &bytes.Buffer{}); code != test.code {
			t.Errorf("%s: exit %d, want %d", test.name, code, test.code)
		}
		if line := "screen result=" + string(test.want) + "\n"; !strings.HasSuffix(stdout.String(), line) {
			t.Errorf("%s: summary does not end with %q:\n%s", test.name, line, stdout.String())
		}
		data, err := os.ReadFile(output)
		if err != nil || !strings.Contains(string(data), `"result": "`+string(test.want)+`"`) {
			t.Errorf("%s: JSON report %s, %v", test.name, data, err)
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

// TestScreenStorageBytes checks the sizes that the storage bound of a
// screen charges.
func TestScreenStorageBytes(t *testing.T) {
	t.Parallel()
	if size := reflect.TypeFor[screenEvent]().Size(); size != screenEventBytes {
		t.Errorf("an event has %d bytes, and the bound charges %d", size, screenEventBytes)
	}
	if size := reflect.TypeFor[screenOutcome]().Size(); size != screenOutcomeBytes {
		t.Errorf("an outcome has %d bytes, and the bound charges %d", size, screenOutcomeBytes)
	}
	var orders []int
	for range sim.MaxExpressParties {
		orders = append(orders, 0)
	}
	if size := cap(orders) * 8; size > screenHubOrderBytes {
		t.Errorf("the orders of a hub party use %d bytes, and the bound charges %d", size, screenHubOrderBytes)
	}
}

// TestScreenHubLimit checks that the hub stream stops at its limit, and
// that the limit is far from the hub streams of the screen.
func TestScreenHubLimit(t *testing.T) {
	t.Parallel()
	const arrivalTicks = 20 * 60 * sim.TicksPerSecond
	events, err := screenHubEvents(nil, screenHubInput{seed: 2, arrivalTicks: arrivalTicks, line: screenKnownLines(screenHubMean)[0], limit: screenHubLimit(arrivalTicks)})
	if err != nil {
		t.Fatal(err)
	}
	count := int64(len(events))
	if _, err := screenHubEvents(nil, screenHubInput{seed: 2, arrivalTicks: arrivalTicks, line: screenKnownLines(screenHubMean)[0], limit: count}); err != nil {
		t.Fatalf("a limit of exactly %d events: %v", count, err)
	}
	want := fmt.Sprintf("seed 2: the hub stream has more than %d events", count-1)
	if _, err := screenHubEvents(nil, screenHubInput{seed: 2, arrivalTicks: arrivalTicks, line: screenKnownLines(screenHubMean)[0], limit: count - 1}); err == nil || err.Error() != want {
		t.Fatalf("a limit one event under the stream: got %v, want %q", err, want)
	}
	for seed := range int64(100) {
		for _, window := range []int64{1, 450, 2 * 3600 * sim.TicksPerSecond} {
			if _, err := screenHubEvents(nil, screenHubInput{seed: seed + 1, arrivalTicks: window, line: screenKnownLines(screenHubMean)[0], limit: screenHubLimit(window)}); err != nil {
				t.Errorf("seed %d, %d ticks: %v", seed+1, window, err)
			}
		}
	}
}

// TestParseScreenStorage checks the storage bound of a screen. The bound
// holds one band and seed of 24 hours at one request each tick in the three
// configurations. The default rate is the LondonFull rate.
func TestParseScreenStorage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-arrivals", "24h", "-background-per-minute", "1800", "-seeds", "1", "-bands", "busy"},
		{"-arrivals", "24h", "-seeds", "1", "-bands", "busy"},
	} {
		if _, err := parseScreenOptions(args, &bytes.Buffer{}); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"-arrivals", "24h", "-background-per-minute", "1800", "-seeds", "1,2", "-bands", "busy"}, "the screen storage must fit in 1024 MiB, and this screen needs 1764 MiB"},
		{[]string{"-arrivals", "24h", "-seeds", seedList(100)}, "the screen storage must fit in 1024 MiB, and this screen needs 3146 MiB"},
		{[]string{"-arrivals", "24h", "-seeds", seedList(100), "-configs", "LF,W0,W"}, "the screen storage must fit in 1024 MiB, and this screen needs 5455 MiB"},
	} {
		const advice = "; use fewer seeds, bands, or configurations, a lower background rate, or a shorter arrival window"
		_, err := parseScreenOptions(test.args, &bytes.Buffer{})
		if err == nil || err.Error() != test.want+advice {
			t.Errorf("%.80v: got %v, want %q", test.args, err, test.want+advice)
		}
	}
}
