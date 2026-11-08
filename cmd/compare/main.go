// Command compare runs repeatable demand and policy experiments.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	maxDuration    = 24 * time.Hour
	maxSeeds       = 100
	maxLoads       = 20
	maxComparisons = 1000 // The cap does not count the redistribution policies.
	maxQueueLimit  = 1_000_000
	maxBurstSize   = 1_000
	maxWorkers     = 64
)

var syntheticPatterns = []string{"balanced", "destination", "hotspot", "bursty-hotspot", "hub-burst"}
var knownPatterns = append(append([]string(nil), syntheticPatterns...), "profile", "profile-daily", "rail-arrivals", "rail-services")

// waitRuleValues maps each -wait-rules name to its simulation rule.
var waitRuleValues = map[string]sim.FinishingPodWait{
	"current": sim.FinishingPodWaitCurrent,
	"strict":  sim.FinishingPodWaitStrict,
	"none":    sim.FinishingPodWaitNone,
}

// platoonPolicyValues maps each -platoon-policies name to its platooning
// mode. The virtual policy links pods in queues with the simulation
// default platoon limit.
var platoonPolicyValues = map[string]sim.Platooning{
	"off":     sim.PlatooningOff,
	"virtual": sim.PlatooningVirtual,
}

// redistributionPolicyValues maps each -redistribution-policies name to
// its positioning mode. The on policy is guarded positioning, as for a
// project with redistribution.
var redistributionPolicyValues = map[string]sim.Positioning{
	"off": sim.PositioningOff,
	"on":  sim.PositioningGuarded,
}

type report struct {
	SchemaVersion int      `json:"schema_version"`
	Results       []result `json:"results"`
}

type cliInput struct {
	args           []string
	stdout, stderr io.Writer
}

// compareGCPercent is the GC percent of compare runs when GOGC is not
// set. A London arm has a live heap of about 60 MB, and GC at the default
// 100 uses about 16 percent of its CPU. At 400, an arm uses about 14
// percent less CPU and about 120 MB more memory. The reports do not
// change.
const compareGCPercent = 400

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(compareGCPercent)
	}
	os.Exit(runCLI(cliInput{args: os.Args[1:], stdout: os.Stdout, stderr: os.Stderr}))
}

func runCLI(input cliInput) int {
	if len(input.args) > 0 && input.args[0] == "screen" {
		return runScreen(input.args[1:], input.stdout, input.stderr)
	}
	opts, err := parseOptions(input.args, input.stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(input.stderr, err)
		return 2
	}
	caseStudy, err := loadScenario(opts.projectPath, opts.focus)
	if err != nil {
		_, _ = fmt.Fprintln(input.stderr, err)
		return 1
	}
	results, err := compare(opts, caseStudy)
	if err != nil {
		_, _ = fmt.Fprintln(input.stderr, err)
		return 1
	}
	output := input.stdout
	closeOutput := func() error { return nil }
	if opts.outputPath != "" {
		file, err := os.Create(opts.outputPath) // #nosec G304 -- The operator selects this report file.
		if err != nil {
			_, _ = fmt.Fprintf(input.stderr, "create report %s: %v\n", opts.outputPath, err)
			return 1
		}
		output = file
		closeOutput = file.Close
	}
	if err := writeReport(writeReportInput{
		output: output, format: opts.format, results: results, sharingConsentColumn: opts.sharingConsentColumn,
		waitRuleColumn: opts.waitRules != nil, platoonColumn: opts.platoonPolicies != nil,
		pickupReassignmentColumn: opts.pickupReassignmentColumn, onboardPickupsColumn: opts.onboardPickups != nil,
		fleetClassColumn: opts.fleetClasses != nil,
	}); err != nil {
		_ = closeOutput()
		_, _ = fmt.Fprintln(input.stderr, err)
		return 1
	}
	if err := closeOutput(); err != nil {
		_, _ = fmt.Fprintf(input.stderr, "close report %s: %v\n", opts.outputPath, err)
		return 1
	}
	return 0
}
