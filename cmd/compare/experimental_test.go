package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestExperimentalPolicyOptions(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "true", "ON", "on,", "off,off"} {
		if _, err := parseOptions([]string{"-pickup-reassignment", value}, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted -pickup-reassignment %q", value)
		}
	}
	opts, err := parseOptions([]string{"-pickup-reassignment", "on, off"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opts.pickupReassignment, []string{"on", "off"}) {
		t.Fatalf("policy order changed: %+v", opts)
	}
}

func TestExperimentalArmsPairSchedulesAndPreserveDefault(t *testing.T) {
	t.Parallel()
	args := []string{"-duration", "2m", "-request-every", "10s", "-redistribution-policies", "off", "-seed", "7"}
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
	opts, err = parseOptions(append(slices.Clone(args), "-pickup-reassignment", "off,on"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	arms, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 2 {
		t.Fatalf("got %d arms, want 2", len(arms))
	}
	seen := make(map[string]bool)
	for _, arm := range arms {
		seen[arm.PickupReassignment] = true
		if arm.ScheduleID != base[0].ScheduleID || arm.Scheduled != base[0].Scheduled {
			t.Fatalf("unmatched demand schedule: %+v", arm)
		}
		if arm.Served+arm.Remaining+arm.Skipped != arm.Scheduled || arm.PickupReassignmentStats == nil {
			t.Fatalf("incomplete accounting or metadata: %+v", arm)
		}
		stats := arm.PickupReassignmentStats
		if arm.PickupReassignment == "off" && *stats != (pickupPolicyStats{}) {
			t.Fatalf("disabled policy did work: %+v", stats)
		}
		if arm.PickupReassignment == "on" && stats.AssignmentChecks == 0 {
			t.Fatal("enabled policy did not run assignment checks")
		}
	}
	if len(seen) != 2 {
		t.Fatalf("missing policy arms: %+v", seen)
	}
	unchanged := arms[1]
	if !reflect.DeepEqual(unchanged, base[0]) {
		t.Fatalf("explicit on changed default outcome: %+v != %+v", unchanged, base[0])
	}
	opts.workers = 4
	parallel, err := compare(opts, caseStudy)
	if err != nil || !reflect.DeepEqual(parallel, arms) {
		t.Fatalf("worker count changed outcomes: %v", err)
	}
}

func TestExperimentalAdaptiveGroupsStayIndependent(t *testing.T) {
	t.Parallel()
	inputs := experimentalInputs([]runInput{{requestEvery: time.Second}}, options{
		pickupReassignment: []string{"off", "on"},
	})
	scheduler := newArmScheduler(inputs, 0)
	if len(scheduler.groups) != 2 || len(scheduler.start()) != 2 {
		t.Fatal("experimental policies shared an adaptive rate group")
	}
}

func TestExperimentalPoliciesCountTowardMatrixLimit(t *testing.T) {
	t.Parallel()
	seeds := make([]string, 100)
	for i := range seeds {
		seeds[i] = strconv.Itoa(i + 1)
	}
	_, err := parseOptions([]string{
		"-seeds", strings.Join(seeds, ","), "-loads", "5s,10s,15s,20s,25s,30s",
		"-pickup-reassignment", "off,on",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("experimental arms escaped matrix limit: %v", err)
	}
	// Profile expansion must also count the policy arms.
	opts, err := parseOptions([]string{
		"-seeds", strings.Join(seeds, ","), "-pattern", "profile", "-bands", "all", "-loads", "10s,20s,30s",
		"-pickup-reassignment", "off,on",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.demand = project.DemandConfig{Pattern: "profile", Profile: "test", Band: "peak"}
	caseStudy.demandProfiles = []project.DemandProfile{{
		ID: "test", Name: "Test profile",
		Bands: []project.DemandBand{{ID: "peak", Name: "Peak"}, {ID: "quiet", Name: "Quiet"}},
		Flows: []project.DemandFlow{{From: "harbor", To: "market", Weights: []float64{1, 1}}},
	}}
	if _, err = compare(opts, caseStudy); err == nil || !strings.Contains(err.Error(), "expanded matrix") {
		t.Fatalf("expanded arms escaped matrix limit: %v", err)
	}
}

func TestExperimentalReportMetadata(t *testing.T) {
	t.Parallel()
	results := []result{{SharingConsent: sim.PrivateConsent, PickupReassignment: "off", PickupReassignmentStats: &pickupPolicyStats{}}}
	var output bytes.Buffer
	if err := writeReport(writeReportInput{output: &output, format: "json", results: results}); err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := jsonv2.Unmarshal(output.Bytes(), &decoded, json.DefaultOptionsV1()); err != nil || decoded.SchemaVersion != 16 || !reflect.DeepEqual(decoded.Results, results) {
		t.Fatalf("experimental JSON metadata lost: %v, %+v", err, decoded)
	}
	output.Reset()
	if err := writeReport(writeReportInput{output: &output, format: "csv", results: results, pickupReassignmentColumn: true}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for column, value := range map[string]string{"pickup_reassignment": "off"} {
		index := slices.Index(rows[0], column)
		if index < 0 || rows[1][index] != value {
			t.Fatalf("CSV lost %s: %+v", column, rows)
		}
	}
	output.Reset()
	if err := writeReport(writeReportInput{output: &output, format: "table", results: results, pickupReassignmentColumn: true}); err != nil || !strings.Contains(output.String(), "REASSIGN") {
		t.Fatalf("table lost policy columns: %v", err)
	}
}

func TestExperimentalArmConfiguration(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureExperimentalPolicies(simulation, runInput{pickupReassignment: "on"}); err != nil {
		t.Fatalf("enabled policy did not configure simulation: %v", err)
	}
	if err := configureExperimentalPolicies(simulation, runInput{pickupReassignment: "invalid"}); err == nil {
		t.Fatal("invalid direct arm accepted")
	}
}

func TestComparePolicyDefaults(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		flags  []string
		policy string
		column bool
	}{
		{"omitted", nil, "on", false},
		{"explicit on", []string{"-redistribution-policies", "on", "-pickup-reassignment", "on"}, "on", true},
		{"explicit off", []string{"-redistribution-policies", "off", "-pickup-reassignment", "off"}, "off", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"-duration", "2m", "-loads", "10s,20s", "-seeds", "7,19"}, test.flags...)
			opts, err := parseOptions(args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.redistributionPolicies, []string{test.policy}) || !slices.Equal(opts.pickupReassignment, []string{test.policy}) || opts.pickupReassignmentColumn != test.column {
				t.Fatalf("policy options: %+v", opts)
			}
			rows, err := compare(opts, caseStudy)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 4 {
				t.Fatalf("got %d arms, want 4", len(rows))
			}
			for _, row := range rows {
				if row.Policy != test.policy || row.PickupReassignment != test.policy || row.PickupReassignmentStats == nil {
					t.Fatalf("effective policies: %+v", row)
				}
				stats := *row.PickupReassignmentStats
				if test.policy == "on" && stats.AssignmentChecks == 0 || test.policy == "off" && stats != (pickupPolicyStats{}) {
					t.Fatalf("pickup configuration: %+v", stats)
				}
			}
			opts.workers = 4
			parallel, err := compare(opts, caseStudy)
			if err != nil || !reflect.DeepEqual(rows, parallel) {
				t.Fatalf("worker count changed outcomes: %v", err)
			}
			for _, format := range []string{"json", "csv", "table"} {
				var output, stderr bytes.Buffer
				if code := runCLI(cliInput{args: append(slices.Clone(args), "-format", format), stdout: &output, stderr: &stderr}); code != 0 {
					t.Fatalf("%s report: %s", format, &stderr)
				}
				switch format {
				case "json":
					var decoded report
					if err := jsonv2.Unmarshal(output.Bytes(), &decoded, json.DefaultOptionsV1()); err != nil || decoded.SchemaVersion != 16 || !reflect.DeepEqual(decoded.Results, rows) {
						t.Fatalf("JSON lost effective policies: %v", err)
					}
				case "csv":
					header, err := csv.NewReader(&output).Read()
					if err != nil || slices.Contains(header, "pickup_reassignment") != test.column {
						t.Fatalf("CSV column: %v, %v", header, err)
					}
				case "table":
					if strings.Contains(output.String(), "REASSIGN") != test.column {
						t.Fatalf("table column: %s", &output)
					}
				}
			}
		})
	}
}

func TestComparePolicyDefaultsWithOtherOptions(t *testing.T) {
	t.Parallel()
	base, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	base.railArrivals = railComparisonPlan()
	base.railDepartures = serviceComparisonDepartures()
	path := writeProjectFile(t, scenarios.Small())
	for _, test := range []struct {
		name     string
		flags    []string
		scenario scenario
	}{
		{"rail arrivals", []string{"-pattern", "rail-arrivals"}, base},
		{"rail services", []string{"-pattern", "rail-services"}, base},
		{"daily", []string{"-pattern", "profile-daily"}, dailyComparisonScenario(t)},
		{"fleet classes", []string{"-project", path, "-fleet-classes", "plain,express"}, ringScenario(t, scenarios.Small())},
		{"consent", []string{"-sharing-consent", "shared", "-sharing-limits", "2"}, base},
		{"onboard", []string{"-sharing-consent", "shared", "-sharing-limits", "2", "-onboard-pickups", "on"}, base},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"-duration", "1m"}, test.flags...)
			opts, err := parseOptions(args, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := compare(opts, test.scenario)
			if err != nil || len(rows) == 0 {
				t.Fatalf("default policies refused: %v", err)
			}
			for _, row := range rows {
				if row.Policy != "on" || row.PickupReassignment != "on" {
					t.Fatalf("supported policy disabled: %+v", row)
				}
			}
		})
	}
}
