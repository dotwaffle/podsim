package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestComparisonOnboardOptions(t *testing.T) {
	for _, test := range []struct {
		value, limits, mode string
		valid               bool
	}{
		{"off", "1", "destination", true}, {"on", "2", "drop-offs", true}, {"off,on", "2", "drop-offs", true},
		{"on", "1", "drop-offs", false}, {"on", "2", "destination", false}, {"off,on", "1,2", "drop-offs", false},
		{"true", "2", "drop-offs", false}, {"", "2", "drop-offs", false}, {"off,off", "2", "drop-offs", false}, {"on,", "2", "drop-offs", false},
	} {
		opts, err := parseOptions([]string{"-onboard-pickups", test.value, "-sharing-limits", test.limits, "-sharing-modes", test.mode}, &bytes.Buffer{})
		if (err == nil) != test.valid {
			t.Fatalf("policy=%q limits=%q mode=%q: %v", test.value, test.limits, test.mode, err)
		}
		if test.valid && !slices.Equal(opts.onboardPickups, strings.Split(test.value, ",")) {
			t.Fatal("policy order changed")
		}
	}
	opts, err := parseOptions(nil, &bytes.Buffer{})
	if err != nil || opts.onboardPickups != nil {
		t.Fatal("default enabled policy dimension", err)
	}
}

func TestComparisonOnboardPairsSchedulesAndAdaptiveGroups(t *testing.T) {
	args := []string{"-duration", "10s", "-request-every", "5s", "-redistribution-policies", "off", "-sharing-limits", "2", "-sharing-modes", "drop-offs", "-sharing-consent", "shared"}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	base, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	opts, err = parseOptions(append(slices.Clone(args), "-onboard-pickups", "off,on"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].OnboardPickups != "off" || rows[1].OnboardPickups != "on" {
		t.Fatal("policy arms missing")
	}
	for _, row := range rows {
		if row.ScheduleID != base[0].ScheduleID || row.Scheduled != base[0].Scheduled {
			t.Fatal("policy arms changed offered schedule")
		}
	}
	unchanged := rows[0]
	unchanged.OnboardPickups = ""
	if !reflect.DeepEqual(unchanged, base[0]) {
		t.Fatal("explicit off changed default outcome")
	}
	scheduler := newArmScheduler(onboardInputs([]runInput{{requestEvery: time.Second}}, opts), 0)
	if len(scheduler.groups) != 2 || len(scheduler.start()) != 2 {
		t.Fatal("onboard policies share an adaptive rate group")
	}
}

func TestComparisonOnboardRunRejectsUnsupported(t *testing.T) {
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		limit  int
		mode   sim.SharedRideMode
		policy string
	}{
		{1, sim.SharedRideDropOffs, "on"}, {2, sim.SharedRideDestination, "on"}, {2, sim.SharedRideDropOffs, "unknown"},
	} {
		input := runInput{scenario: caseStudy, policy: "off", duration: time.Second, arrivalsFor: time.Second, requestEvery: time.Second, pattern: "balanced", queueLimit: 200, routingPolicy: "free-flow", sharingLimit: test.limit, sharingMode: test.mode, onboardPickups: test.policy}
		if _, runErr := run(input); runErr == nil {
			t.Fatal("unsupported onboard runtime policy accepted")
		}
	}
}

func TestComparisonOnboardReportProvenance(t *testing.T) {
	args := []string{"-duration", "10s", "-request-every", "5s", "-redistribution-policies", "off", "-sharing-limits", "2"}
	for _, format := range []string{"json", "csv", "table"} {
		baseArgs := append(slices.Clone(args), "-format", format)
		var base, out bytes.Buffer
		if runCLI(cliInput{args: baseArgs, stdout: &base, stderr: &bytes.Buffer{}}) != 0 {
			t.Fatal("default CLI failed")
		}
		if runCLI(cliInput{args: append(slices.Clone(baseArgs), "-onboard-pickups", "off,on"), stdout: &out, stderr: &bytes.Buffer{}}) != 0 {
			t.Fatal("onboard CLI failed")
		}
		switch format {
		case "json":
			var baseline, decoded report
			if err := jsonv2.Unmarshal(base.Bytes(), &baseline, json.DefaultOptionsV1()); err != nil {
				t.Fatal(err)
			}
			if err := jsonv2.Unmarshal(out.Bytes(), &decoded, json.DefaultOptionsV1()); err != nil {
				t.Fatal(err)
			}
			if baseline.SchemaVersion != 12 || bytes.Contains(base.Bytes(), []byte("onboard_pickups")) || decoded.SchemaVersion != 14 || len(decoded.Results) != 2 || decoded.Results[0].OnboardPickups != "off" || decoded.Results[1].OnboardPickups != "on" {
				t.Fatal("conditional JSON provenance changed")
			}
		case "csv":
			baseline, err := csv.NewReader(bytes.NewReader(base.Bytes())).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := csv.NewReader(bytes.NewReader(out.Bytes())).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			column := slices.Index(decoded[0], "onboard_pickups")
			if slices.Contains(baseline[0], "onboard_pickups") || column < 0 || len(decoded) != 3 || decoded[1][column] != "off" || decoded[2][column] != "on" {
				t.Fatal("conditional CSV provenance changed")
			}
			for _, row := range decoded {
				if len(row) != len(decoded[0]) {
					t.Fatal("CSV row alignment changed")
				}
			}
			if !slices.Equal(slices.Delete(slices.Clone(decoded[0]), column, column+1), baseline[0]) || !slices.Equal(slices.Delete(slices.Clone(decoded[1]), column, column+1), baseline[1]) {
				t.Fatal("onboard column changed default headers or off values")
			}
		case "table":
			if strings.Contains(base.String(), "ONBOARD PICKUPS") || !strings.Contains(out.String(), "ONBOARD PICKUPS") {
				t.Fatal("conditional table policy column changed")
			}
		}
	}
}

func TestComparisonOnboardMatrixBound(t *testing.T) {
	seeds := make([]string, 100)
	for i := range seeds {
		seeds[i] = floatText(float64(i + 1))
	}
	_, err := parseOptions([]string{"-seeds", strings.Join(seeds, ","), "-loads", "5s,10s,15s,20s,25s,30s", "-sharing-limits", "2", "-onboard-pickups", "off,on"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("policy dimension escaped matrix bound: %v", err)
	}
}
