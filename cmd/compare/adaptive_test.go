package main

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestRateGroupRule drives one group with fixed seed results. Each case
// finishes the started arms first in start order, then in reverse order.
// The rates that start must not depend on that order.
func TestRateGroupRule(t *testing.T) {
	t.Parallel()
	all, one, none := []bool{true, true, true}, []bool{true, false, true}, []bool{false, false, false}
	for _, tc := range []struct {
		name      string
		drained   [][]bool
		pastLimit int
		want      int
	}{
		{"stop one rate after the first failure", [][]bool{all, all, one, all, none, none}, 1, 4},
		{"stop at the first failure", [][]bool{all, all, one, all, none, none}, 0, 3},
		{"stop two rates after the first failure", [][]bool{all, all, one, all, none, none}, 2, 5},
		{"all rates drain", [][]bool{all, all, all, all, all}, 1, 5},
		{"a later rate drains after a failure", [][]bool{all, one, all, all, all}, 1, 3},
		{"failure at the lowest rate", [][]bool{none, all, all}, 0, 1},
		{"failure at the last rate", [][]bool{all, all, none}, 1, 3},
		{"past limit after the last rate", [][]bool{all, one, all}, 5, 3},
		{"largest past limit", [][]bool{all, one, all, all}, math.MaxInt, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, lastFirst := range []bool{false, true} {
				if got := startedRates(tc.drained, tc.pastLimit, lastFirst); got != tc.want {
					t.Errorf("last first = %t: %d rates started, want %d", lastFirst, got, tc.want)
				}
			}
		})
	}
}

// startedRates runs a group whose seed results are drained[rate][seed]. It
// finishes the started arms in start order, or in reverse order when
// lastFirst is set, and returns the number of rates that started.
func startedRates(drained [][]bool, pastLimit int, lastFirst bool) int {
	type arm struct{ rate, seed int }
	var arms []arm
	rates := make([][]int, len(drained))
	for rate, seeds := range drained {
		for seed := range seeds {
			rates[rate] = append(rates[rate], len(arms))
			arms = append(arms, arm{rate: rate, seed: seed})
		}
	}
	group := newRateGroup(rates, pastLimit)
	finish := func(index int) []int {
		group.finish(arms[index].rate, drained[arms[index].rate][arms[index].seed])
		return group.release()
	}
	driveQueue(group.release(), finish, lastFirst)
	return group.started
}

// driveQueue takes indices from queue until it is empty. It gives each
// index to finish and adds the indices that finish returns to the queue.
// It takes the oldest index first, or the newest when lastFirst is set.
func driveQueue(queue []int, finish func(int) []int, lastFirst bool) {
	for len(queue) > 0 {
		next := 0
		if lastFirst {
			next = len(queue) - 1
		}
		index := queue[next]
		queue = slices.Delete(queue, next, next+1)
		queue = append(queue, finish(index)...)
	}
}

// adaptiveFixture gives arms in the order that compare makes them: band,
// then load, then seed, then redistribution policy. The loads are not in
// rate order. The rate order is 60s, 30s, 20s, 15s, 10s.
func adaptiveFixture() []runInput {
	var inputs []runInput
	for _, band := range []string{"early", "late"} {
		for _, load := range []time.Duration{20 * time.Second, 60 * time.Second, 30 * time.Second, 10 * time.Second, 15 * time.Second} {
			for _, seed := range []int64{1, 2} {
				for _, enabled := range []bool{false, true} {
					inputs = append(inputs, runInput{band: band, requestEvery: load, seed: seed, enabled: enabled})
				}
			}
		}
	}
	return inputs
}

// fixtureRate names the group and the rate of a fixture arm.
func fixtureRate(input runInput) string {
	return fmt.Sprintf("%s/%t/%s", input.band, input.enabled, input.requestEvery)
}

// fixtureFailures lists the fixture arms that do not drain, by rate and
// seed. The early band with redistribution on drains at every rate. The
// late band with redistribution on drains again at 20s after it fails at
// 30s.
var fixtureFailures = map[string][]int64{
	"early/false/20s": {2},
	"early/false/10s": {1, 2},
	"late/false/1m0s": {1, 2},
	"late/false/30s":  {1},
	"late/true/30s":   {1},
	"late/true/15s":   {1, 2},
	"late/true/10s":   {2},
}

func fixtureDrained(input runInput) bool {
	return !slices.Contains(fixtureFailures[fixtureRate(input)], input.seed)
}

// fixtureSkipped lists the rates that each past limit skips.
var fixtureSkipped = map[int][]string{
	0: {
		"early/false/15s", "early/false/10s",
		"late/false/30s", "late/false/20s", "late/false/15s", "late/false/10s",
		"late/true/20s", "late/true/15s", "late/true/10s",
	},
	1: {"early/false/10s", "late/false/20s", "late/false/15s", "late/false/10s", "late/true/15s", "late/true/10s"},
}

// TestArmSchedulerInterleavesGroups finishes the arms of four groups in two
// orders. In reverse order the groups interleave, and later rates finish
// before earlier rates. Both orders must run the same arms.
func TestArmSchedulerInterleavesGroups(t *testing.T) {
	t.Parallel()
	inputs := adaptiveFixture()
	for pastLimit, skipped := range fixtureSkipped {
		for _, lastFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("past limit %d last first %t", pastLimit, lastFirst), func(t *testing.T) {
				t.Parallel()
				scheduler := newArmScheduler(inputs, pastLimit)
				ran := make([]bool, len(inputs))
				finish := func(index int) []int {
					if ran[index] {
						t.Fatalf("arm %d started twice", index)
					}
					ran[index] = true
					return scheduler.finish(index, fixtureDrained(inputs[index]))
				}
				driveQueue(scheduler.start(), finish, lastFirst)
				for index, input := range inputs {
					if want := !slices.Contains(skipped, fixtureRate(input)); ran[index] != want {
						t.Errorf("arm %s seed %d ran = %t, want %t", fixtureRate(input), input.seed, ran[index], want)
					}
				}
			})
		}
	}
}

// TestArmSchedulerRateOrder checks that a group starts its lowest offered
// rate first, whatever the order of the loads.
func TestArmSchedulerRateOrder(t *testing.T) {
	t.Parallel()
	inputs := adaptiveFixture()
	scheduler := newArmScheduler(inputs, 0)
	if len(scheduler.groups) != 4 {
		t.Fatalf("got %d groups, want 4", len(scheduler.groups))
	}
	for _, group := range scheduler.groups {
		var loads []time.Duration
		for _, indices := range group.rates {
			if len(indices) != 2 {
				t.Fatalf("rate has %d seeds, want 2", len(indices))
			}
			loads = append(loads, inputs[indices[0]].requestEvery)
		}
		want := []time.Duration{60 * time.Second, 30 * time.Second, 20 * time.Second, 15 * time.Second, 10 * time.Second}
		if !slices.Equal(loads, want) {
			t.Errorf("rate order = %v, want %v", loads, want)
		}
	}
}

// TestRunAdaptiveKeepsInputOrder runs the fixture on worker pools of
// different sizes. The results must be the arms that the rule keeps, in
// input order.
func TestRunAdaptiveKeepsInputOrder(t *testing.T) {
	t.Parallel()
	inputs := adaptiveFixture()
	fakeRun := func(input runInput) (result, error) {
		return result{DemandBand: input.band, RequestEverySeconds: input.requestEvery.Seconds(), Seed: input.seed, Policy: strconv.FormatBool(input.enabled), Drained: fixtureDrained(input)}, nil
	}
	for pastLimit, skipped := range fixtureSkipped {
		var want []result
		for _, input := range inputs {
			if !slices.Contains(skipped, fixtureRate(input)) {
				outcome, _ := fakeRun(input)
				want = append(want, outcome)
			}
		}
		for _, workers := range []int{1, 3, 64} {
			t.Run(fmt.Sprintf("past limit %d workers %d", pastLimit, workers), func(t *testing.T) {
				t.Parallel()
				got, err := runAdaptive(adaptiveRun{inputs: inputs, workers: workers, pastLimit: pastLimit, run: fakeRun})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %d results, want %d:\n%+v\n%+v", len(got), len(want), got, want)
				}
			})
		}
	}
	t.Run("error", func(t *testing.T) {
		t.Parallel()
		failure := errors.New("arm failed")
		failingRun := func(input runInput) (result, error) {
			if input.band == "late" && input.requestEvery == 30*time.Second {
				return result{}, failure
			}
			return fakeRun(input)
		}
		if _, err := runAdaptive(adaptiveRun{inputs: inputs, workers: 3, pastLimit: 1, run: failingRun}); !errors.Is(err, failure) {
			t.Fatalf("runAdaptive() error = %v, want %v", err, failure)
		}
	})
}

// TestAdaptiveLimitMatchesFullGrid runs a small grid on the example project
// with and without -adaptive-limit. The adaptive report must hold the full
// grid rows that the rule keeps, in the same order.
func TestAdaptiveLimitMatchesFullGrid(t *testing.T) {
	t.Parallel()
	args := []string{
		"-duration", "8m", "-arrivals-for", "3m", "-loads", "20s,40s,25s,10s,30s,15s", "-seeds", "1,2",
		"-patterns", "balanced,hotspot", "-stop-when-drained", "-format", "csv", "-workers", "4",
	}
	full := csvReport(t, args)
	adaptive := csvReport(t, append(slices.Clone(args), "-adaptive-limit"))
	want := keptRows(t, full, 1)
	if len(want) == len(full) {
		t.Fatal("the rule skips no arm, so the test does not cover skipped arms")
	}
	if !reflect.DeepEqual(adaptive, want) {
		t.Fatalf("adaptive report has %d rows, want %d:\n%v\nwant:\n%v", len(adaptive), len(want), adaptive, want)
	}
}

func csvReport(t *testing.T, args []string) [][]string {
	t.Helper()
	var output, stderr bytes.Buffer
	if code := runCLI(cliInput{args: args, stdout: &output, stderr: &stderr}); code != 0 {
		t.Fatalf("exit = %d, error = %s", code, stderr.String())
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

// keptRows applies the adaptive rule to the rows of a full-grid CSV report.
// It returns the header and the rows that the rule keeps, in report order.
func keptRows(t *testing.T, records [][]string, pastLimit int) [][]string {
	t.Helper()
	type rate struct {
		group string
		load  float64
	}
	rateOf := func(row []string) rate {
		var group []string
		for _, name := range []string{"pattern", "demand_profile", "demand_band", "policy", "routing_policy", "shared_ride_party_limit"} {
			group = append(group, row[csvColumn(t, records[0], name)])
		}
		load, err := strconv.ParseFloat(row[csvColumn(t, records[0], "request_every_seconds")], 64)
		if err != nil {
			t.Fatal(err)
		}
		return rate{group: strings.Join(group, "/"), load: load}
	}
	loads := make(map[string][]float64)
	failed := make(map[rate]bool)
	for _, row := range records[1:] {
		key := rateOf(row)
		if !slices.Contains(loads[key.group], key.load) {
			loads[key.group] = append(loads[key.group], key.load)
		}
		failed[key] = failed[key] || row[csvColumn(t, records[0], "drained")] != "true"
	}
	// lowestLoad holds the shortest request interval, so the highest rate,
	// that the rule keeps in each group.
	lowestLoad := make(map[string]float64)
	for group, values := range loads {
		slices.SortFunc(values, func(left, right float64) int { return cmp.Compare(right, left) })
		last := len(values) - 1
		for position, load := range values {
			if failed[rate{group: group, load: load}] {
				last = min(last, position+pastLimit)
				break
			}
		}
		lowestLoad[group] = values[last]
	}
	kept := [][]string{records[0]}
	for _, row := range records[1:] {
		if key := rateOf(row); key.load >= lowestLoad[key.group] {
			kept = append(kept, row)
		}
	}
	return kept
}

func csvColumn(t *testing.T, header []string, name string) int {
	t.Helper()
	index := slices.Index(header, name)
	if index < 0 {
		t.Fatalf("report has no %s column", name)
	}
	return index
}

func TestParseAdaptiveLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		args      []string
		adaptive  bool
		pastLimit int
	}{
		{"flags not given", nil, false, 1},
		{"default past limit", []string{"-adaptive-limit", "-stop-when-drained"}, true, 1},
		{"no rate past the limit", []string{"-adaptive-limit", "-stop-when-drained", "-past-limit", "0"}, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(tc.args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			if opts.adaptiveLimit != tc.adaptive || opts.pastLimit != tc.pastLimit {
				t.Fatalf("adaptive limit = %t, past limit = %d, want %t and %d", opts.adaptiveLimit, opts.pastLimit, tc.adaptive, tc.pastLimit)
			}
		})
	}
}
