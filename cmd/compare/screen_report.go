package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// The individual limits of docs/design/experimental-adoption.md: a wait
	// may grow by max(30 s, 5%), and a journey by max(60 s, 5%).
	screenWaitFloor    = 30 * sim.TicksPerSecond
	screenJourneyFloor = 60 * sim.TicksPerSecond
	screenShare        = 0.05
)

// screenGate is one gate of one pair.
type screenGate struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// screenPair compares a candidate arm with a reference arm of the same band
// and seed. Only the pair of D and LF decides the screen.
type screenPair struct {
	Band      string       `json:"band"`
	Seed      int64        `json:"seed"`
	Candidate string       `json:"candidate"`
	Reference string       `json:"reference"`
	Decides   bool         `json:"decides"`
	Gates     []screenGate `json:"gates"`
	// Matched has no gate.
	Matched screenMatched `json:"matched"`
}

// screenReferences gives the candidates of each reference. S0 isolates the
// Express pods of S. D and S differ in the network too.
var screenReferences = map[string][]string{
	screenLondonFull:  {screenGeometry, screenExpress, screenSharedPlain, screenShared},
	screenSharedPlain: {screenShared},
	screenExpress:     {screenShared},
}

// screenResult is the result of a screen.
type screenResult string

const (
	// screenPassed: the screen has a pair of D, and each gate of each pair
	// of D passes.
	screenPassed screenResult = "pass"
	// screenFailed: a gate of a pair of D fails.
	screenFailed screenResult = "fail"
	// screenUnevaluated: the screen has no pair of D, because it did not
	// run LF or D.
	screenUnevaluated screenResult = "unevaluated"
)

type screenReport struct {
	SchemaVersion int          `json:"schema_version"`
	Arms          []screenArm  `json:"arms"`
	Pairs         []screenPair `json:"pairs"`
	Result        screenResult `json:"result"`
}

// screenKey identifies the offered events of one band and seed.
type screenKey struct {
	band string
	seed int64
}

// screenCompare returns the gates of candidate against reference. Both arms
// ran the same events.
func screenCompare(candidate, reference screenArm, events []screenEvent) []screenGate {
	gates := []screenGate{{Name: "safety and accounting", Pass: candidate.Failure == "" && reference.Failure == "",
		Detail: fmt.Sprintf("candidate %q, reference %q", candidate.Failure, reference.Failure)}}
	gates = append(gates, screenGate{Name: "finite recovery", Pass: candidate.Outstanding == 0,
		Detail: fmt.Sprintf("%d outstanding orders at the end", candidate.Outstanding)})
	lost, waits, journeys, waitCensored, journeyCensored := 0, 0, 0, 0, 0
	candidateHub, referenceHub := 0, 0
	for index, event := range events {
		c, r := candidate.Outcomes[index], reference.Outcomes[index]
		if (c.Refused || c.Partial) && !r.Refused && !r.Partial {
			lost++
		}
		if event.Hub {
			if c.JourneyTicks >= 0 {
				candidateHub += event.Size
			}
			if r.JourneyTicks >= 0 {
				referenceHub += event.Size
			}
			continue
		}
		if c.Refused || c.Partial {
			continue
		}
		// A censored candidate cannot pass by omission: an event that
		// boarded or finished in the reference and not in the candidate
		// counts against the gate.
		switch {
		case r.WaitTicks >= 0 && c.WaitTicks < 0:
			waitCensored++
		case r.WaitTicks >= 0 && screenWaitOver(c.WaitTicks, r.WaitTicks):
			waits++
		}
		switch {
		case r.JourneyTicks >= 0 && c.JourneyTicks < 0:
			journeyCensored++
		case r.JourneyTicks >= 0 && float64(c.JourneyTicks-r.JourneyTicks) > max(screenJourneyFloor, screenShare*float64(r.JourneyTicks)):
			journeys++
		}
	}
	return append(gates,
		screenGate{Name: "live queue loss", Pass: lost == 0, Detail: fmt.Sprintf("%d events refused that the reference accepted", lost)},
		screenGate{Name: "background pickup wait", Pass: waits+waitCensored == 0, Detail: fmt.Sprintf("%d over the limit, %d not boarded", waits, waitCensored)},
		screenGate{Name: "background journey", Pass: journeys+journeyCensored == 0, Detail: fmt.Sprintf("%d over the limit, %d not finished", journeys, journeyCensored)},
		screenGate{Name: "hub benefit", Pass: candidateHub > referenceHub, Detail: fmt.Sprintf("%d hub passengers finished, reference %d", candidateHub, referenceHub)},
	)
}

// runScreen runs the screen command: compare screen [flags].
func runScreen(args []string, stdout, stderr io.Writer) int {
	opts, err := parseScreenOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	report, err := screen(opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return writeScreenResults(report, opts.output, stdout, stderr)
}

// writeScreenResults writes the summary to stdout and the JSON report to
// output, and returns the exit status: 0 for a pass, 3 for a fail, and 4
// for an unevaluated screen. A write error gives status 1, also when the
// screen did not pass. A screen can run for hours, so the JSON report is
// written also when the summary cannot be.
func writeScreenResults(report screenReport, output string, stdout, stderr io.Writer) int {
	code := 0
	if err := writeScreenSummary(stdout, report); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		code = 1
	}
	if output != "" {
		data, err := json.Marshal(report, jsontext.WithIndent("  "))
		if err == nil {
			err = os.WriteFile(output, append(data, '\n'), 0o600)
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "write screen report %s: %v\n", output, err)
			return 1
		}
	}
	switch {
	case code != 0 || report.Result == screenPassed:
		return code
	case report.Result == screenUnevaluated:
		return 4
	default:
		return 3
	}
}

// screen runs every arm and compares the pairs of screenReferences.
func screen(opts screenOptions) (screenReport, error) {
	configs, err := screenConfigs(opts.configs)
	if err != nil {
		return screenReport{}, err
	}
	source := scenarios.LondonFull()
	arrivalTicks, clearTicks := durationTicks(opts.arrivals), durationTicks(opts.clear)
	events := map[screenKey][]screenEvent{}
	var inputs []screenArmInput
	for _, name := range opts.bands {
		band, bandErr := screenBand(name)
		if bandErr != nil {
			return screenReport{}, bandErr
		}
		bandID := source.DemandProfiles[0].Bands[band].ID
		for _, seed := range opts.seeds {
			offered, eventsErr := screenEvents(screenEventsInput{seed: seed, band: band, arrivalTicks: arrivalTicks, intervalTicks: screenBackgroundInterval(opts.backgroundPerMin), source: source})
			if eventsErr != nil {
				return screenReport{}, eventsErr
			}
			events[screenKey{bandID, seed}] = offered
			for _, config := range configs {
				inputs = append(inputs, screenArmInput{config: config, band: band, bandID: bandID, seed: seed, events: offered,
					arrivalTicks: arrivalTicks, clearTicks: clearTicks, queueLimit: opts.queueLimit, checkTicks: int64(opts.checkEverySeconds) * sim.TicksPerSecond})
			}
		}
	}
	arms, err := runScreenArms(inputs, opts.workers)
	if err != nil {
		return screenReport{}, err
	}
	pairs, result := screenPairs(arms, events)
	return screenReport{SchemaVersion: 3, Arms: arms, Pairs: pairs, Result: result}, nil
}

// screenPairs compares the arms of the same band and seed that
// screenReferences pairs, and returns the pairs and the result. Only the pairs of D
// decide. A screen with no pair of D is unevaluated, so that a run of the
// diagnostic configurations alone cannot pass.
func screenPairs(arms []screenArm, events map[screenKey][]screenEvent) ([]screenPair, screenResult) {
	var pairs []screenPair
	decided, failed := false, false
	for _, reference := range arms {
		for _, candidate := range arms {
			if !slices.Contains(screenReferences[reference.Config], candidate.Config) || candidate.Band != reference.Band || candidate.Seed != reference.Seed {
				continue
			}
			offered := events[screenKey{candidate.Band, candidate.Seed}]
			pair := screenPair{Band: candidate.Band, Seed: candidate.Seed, Candidate: candidate.Config, Reference: reference.Config, Decides: candidate.Config == screenExpress,
				Gates: screenCompare(candidate, reference, offered), Matched: newScreenMatched(candidate, reference, offered)}
			if pair.Decides {
				decided = true
				failed = failed || slices.ContainsFunc(pair.Gates, func(gate screenGate) bool { return !gate.Pass })
			}
			pairs = append(pairs, pair)
		}
	}
	switch {
	case failed:
		return pairs, screenFailed
	case decided:
		return pairs, screenPassed
	default:
		return pairs, screenUnevaluated
	}
}

// writeScreenSummary writes a line for each arm, a line for each gate of
// each pair, and the result. It stops at the first write error and returns
// it.
func writeScreenSummary(w io.Writer, report screenReport) error {
	var err error
	printf := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format, args...)
		}
	}
	for _, arm := range report.Arms {
		printf("arm %s band=%s seed=%d ticks=%d wall=%.1fs wall_per_sim_hour=%.1fs checks=%d submitted=%d completed=%d interrupted=%d outstanding=%d refused=%d partial=%d refusal=%q failure=%q\n",
			arm.Config, arm.Band, arm.Seed, arm.Ticks, arm.WallSeconds, screenSecondsPerHour(arm), arm.Checks, arm.Submitted, arm.Completed, arm.Interrupted, arm.Outstanding, arm.Refused, arm.Partial, arm.Refusal, arm.Failure)
	}
	for _, pair := range report.Pairs {
		for _, gate := range pair.Gates {
			printf("pair %s vs %s band=%s seed=%d decides=%t gate=%q pass=%t %s\n", pair.Candidate, pair.Reference, pair.Band, pair.Seed, pair.Decides, gate.Name, gate.Pass, gate.Detail)
		}
	}
	printf("screen result=%s\n", report.Result)
	if err != nil {
		return fmt.Errorf("write screen summary: %w", err)
	}
	return nil
}
