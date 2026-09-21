package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOptionsRejectsInvalidBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "zero duration", args: []string{"-duration", "0s"}, want: "duration must"},
		{name: "long duration", args: []string{"-duration", "25h"}, want: "duration must"},
		{name: "subtick duration", args: []string{"-duration", "1ms"}, want: "simulation tick"},
		{name: "zero load", args: []string{"-request-every", "0s"}, want: "each load"},
		{name: "load after window", args: []string{"-duration", "1m", "-request-every", "2m"}, want: "each load"},
		{name: "subtick load", args: []string{"-request-every", "1ms"}, want: "simulation tick"},
		{name: "duplicate seed", args: []string{"-seeds", "1,1"}, want: "more than once"},
		{name: "invalid seed", args: []string{"-seeds", "one"}, want: "parse seed"},
		{name: "duplicate load", args: []string{"-loads", "30s,30s"}, want: "more than once"},
		{name: "unknown pattern", args: []string{"-pattern", "future"}, want: "unknown demand pattern"},
		{name: "duplicate pattern", args: []string{"-patterns", "balanced,balanced"}, want: "more than once"},
		{name: "unknown format", args: []string{"-format", "yaml"}, want: "format must"},
		{name: "zero queue", args: []string{"-queue-limit", "0"}, want: "queue-limit"},
		{name: "positional argument", args: []string{"extra"}, want: "unexpected positional"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(test.args, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseOptions() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestCompareIsRepeatableAndPairsSchedules(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "2m", "-loads", "20s,30s", "-seeds", "7,19", "-patterns", "balanced,bursty-hotspot",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := compare(opts, scenario)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compare(opts, scenario)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical comparisons produced different results")
	}
	if len(first) != 16 {
		t.Fatalf("result count = %d, want 16", len(first))
	}
	for i := 0; i < len(first); i += 2 {
		off, on := first[i], first[i+1]
		if off.Policy != "off" || on.Policy != "on" || off.ScheduleID != on.ScheduleID || off.Scheduled != on.Scheduled {
			t.Fatalf("comparison pair does not share a schedule: off=%+v on=%+v", off, on)
		}
	}
}

func TestCLIOutputIsRepeatable(t *testing.T) {
	t.Parallel()
	args := []string{
		"-duration", "2m", "-request-every", "30s", "-seeds", "7,19", "-patterns", "balanced,hotspot", "-format", "json",
	}
	var firstOutput, secondOutput bytes.Buffer
	var firstError, secondError bytes.Buffer
	if code := runCLI(args, &firstOutput, &firstError); code != 0 {
		t.Fatalf("first run exit = %d, error = %s", code, firstError.String())
	}
	if code := runCLI(args, &secondOutput, &secondError); code != 0 {
		t.Fatalf("second run exit = %d, error = %s", code, secondError.String())
	}
	if !bytes.Equal(firstOutput.Bytes(), secondOutput.Bytes()) {
		t.Fatal("identical CLI runs produced different reports")
	}
}

func TestDemandSchedulePatterns(t *testing.T) {
	t.Parallel()
	base := scheduleInput{
		seed: 7, durationTicks: durationTicks(10 * time.Minute), intervalTicks: durationTicks(time.Minute),
		passengers: []string{"harbor", "garden", "market"}, focus: "market",
	}
	for _, pattern := range knownPatterns {
		input := base
		input.pattern = pattern
		first, second := demandSchedule(input), demandSchedule(input)
		if !reflect.DeepEqual(first, second) || len(first) != 9 {
			t.Fatalf("pattern %s is not repeatable or has wrong count: %d", pattern, len(first))
		}
		for _, request := range first {
			if request.origin == request.destination {
				t.Fatalf("pattern %s generated same-station request: %+v", pattern, request)
			}
			if pattern == "destination" && request.destination != "market" {
				t.Fatalf("destination pattern generated %+v", request)
			}
		}
		if pattern == "bursty-hotspot" && (first[0].tick != first[1].tick || first[1].tick != first[2].tick) {
			t.Fatalf("bursty pattern did not group arrivals: %+v", first[:3])
		}
	}
}

func TestReportFormatsAreMachineReadable(t *testing.T) {
	t.Parallel()
	results := []result{{Pattern: "balanced", Policy: "off", ScheduleID: "abc", Scheduled: 1}}
	var jsonOutput bytes.Buffer
	if err := writeReport(&jsonOutput, "json", results); err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 1 || !reflect.DeepEqual(decoded.Results, results) {
		t.Fatalf("JSON report changed values: %+v", decoded)
	}

	var csvOutput bytes.Buffer
	if err := writeReport(&csvOutput, "csv", results); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&csvOutput).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0][0] != "pattern" || records[1][0] != "balanced" {
		t.Fatalf("unexpected CSV report: %+v", records)
	}
}

func TestQueueLimitCountsSkippedArrivals(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{
		"-duration", "3m", "-request-every", "5s", "-seed", "3", "-pattern", "bursty-hotspot", "-queue-limit", "1",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	results, err := compare(opts, scenario)
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range results {
		if outcome.Skipped == 0 || outcome.Scheduled != outcome.Served+outcome.Remaining+outcome.Skipped {
			t.Fatalf("incorrect queue accounting: %+v", outcome)
		}
	}
}
