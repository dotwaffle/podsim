package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

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

// screenPair compares a candidate arm with the LF arm of the same band and
// seed. Only the pair of D decides the screen. The pair of D0 explains it.
type screenPair struct {
	Band      string       `json:"band"`
	Seed      int64        `json:"seed"`
	Candidate string       `json:"candidate"`
	Decides   bool         `json:"decides"`
	Gates     []screenGate `json:"gates"`
}

type screenReport struct {
	SchemaVersion int          `json:"schema_version"`
	Arms          []screenArm  `json:"arms"`
	Pairs         []screenPair `json:"pairs"`
	Pass          bool         `json:"pass"`
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
		case r.WaitTicks >= 0 && float64(c.WaitTicks-r.WaitTicks) > max(screenWaitFloor, screenShare*float64(r.WaitTicks)):
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
	writeScreenSummary(stdout, report)
	if output := opts.output; output != "" {
		data, err := json.Marshal(report, jsontext.WithIndent("  "))
		if err == nil {
			err = os.WriteFile(output, append(data, '\n'), 0o600)
		}
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "write screen report %s: %v\n", output, err)
			return 1
		}
	}
	if !report.Pass {
		return 3
	}
	return 0
}

// screen runs every arm and compares each candidate with LF.
func screen(opts screenOptions) (screenReport, error) {
	configs, err := screenConfigs(opts.configs)
	if err != nil {
		return screenReport{}, err
	}
	source := scenarios.LondonFull()
	perMinute := opts.backgroundPerMin
	if perMinute == 0 {
		perMinute = source.Demand.PerMinute
	}
	arrivalTicks, clearTicks := durationTicks(opts.arrivals), durationTicks(opts.clear)
	type key struct {
		band string
		seed int64
	}
	events := map[key][]screenEvent{}
	var inputs []screenArmInput
	for _, name := range opts.bands {
		band, bandErr := screenBand(name)
		if bandErr != nil {
			return screenReport{}, bandErr
		}
		bandID := source.DemandProfiles[0].Bands[band].ID
		for _, seed := range opts.seeds {
			offered, eventsErr := screenEvents(screenEventsInput{seed: seed, band: band, arrivalTicks: arrivalTicks, intervalTicks: 60 * sim.TicksPerSecond / int64(perMinute), source: source})
			if eventsErr != nil {
				return screenReport{}, eventsErr
			}
			events[key{bandID, seed}] = offered
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
	report := screenReport{SchemaVersion: 1, Arms: arms, Pass: true}
	for _, reference := range arms {
		if reference.Config != screenLondonFull {
			continue
		}
		for _, candidate := range arms {
			if candidate.Config == screenLondonFull || candidate.Band != reference.Band || candidate.Seed != reference.Seed {
				continue
			}
			pair := screenPair{Band: candidate.Band, Seed: candidate.Seed, Candidate: candidate.Config, Decides: candidate.Config == screenExpress,
				Gates: screenCompare(candidate, reference, events[key{candidate.Band, candidate.Seed}])}
			for _, gate := range pair.Gates {
				if pair.Decides && !gate.Pass {
					report.Pass = false
				}
			}
			report.Pairs = append(report.Pairs, pair)
		}
	}
	return report, nil
}

func writeScreenSummary(w io.Writer, report screenReport) {
	for _, arm := range report.Arms {
		_, _ = fmt.Fprintf(w, "arm %s band=%s seed=%d ticks=%d wall=%.1fs wall_per_sim_hour=%.1fs checks=%d submitted=%d completed=%d interrupted=%d outstanding=%d refused=%d partial=%d refusal=%q failure=%q\n",
			arm.Config, arm.Band, arm.Seed, arm.Ticks, arm.WallSeconds, screenSecondsPerHour(arm), arm.Checks, arm.Submitted, arm.Completed, arm.Interrupted, arm.Outstanding, arm.Refused, arm.Partial, arm.Refusal, arm.Failure)
	}
	for _, pair := range report.Pairs {
		for _, gate := range pair.Gates {
			_, _ = fmt.Fprintf(w, "pair %s vs LF band=%s seed=%d decides=%t gate=%q pass=%t %s\n", pair.Candidate, pair.Band, pair.Seed, pair.Decides, gate.Name, gate.Pass, gate.Detail)
		}
	}
	_, _ = fmt.Fprintf(w, "screen pass=%t\n", report.Pass)
}
