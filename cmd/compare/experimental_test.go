package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestExperimentalPolicyOptions(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"-station-buffers", "-pickup-reassignment"} {
		for _, value := range []string{"", "true", "ON", "on,", "off,off"} {
			if _, err := parseOptions([]string{flag, value}, &bytes.Buffer{}); err == nil {
				t.Fatalf("accepted %s %q", flag, value)
			}
		}
	}
	opts, err := parseOptions([]string{"-station-buffers", "on, off", "-pickup-reassignment", "off,on"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opts.stationBuffers, []string{"on", "off"}) || !slices.Equal(opts.pickupReassignment, []string{"off", "on"}) {
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
	opts, err = parseOptions(append(slices.Clone(args), "-station-buffers", "off,on", "-pickup-reassignment", "off,on"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	arms, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 4 {
		t.Fatalf("got %d arms, want 4", len(arms))
	}
	seen := make(map[[2]string]bool)
	for _, arm := range arms {
		seen[[2]string{arm.StationBuffers, arm.PickupReassignment}] = true
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
	if len(seen) != 4 {
		t.Fatalf("missing independent policy combinations: %+v", seen)
	}
	unchanged := arms[0]
	unchanged.StationBuffers, unchanged.PickupReassignment, unchanged.PickupReassignmentStats = "", "", nil
	if !reflect.DeepEqual(unchanged, base[0]) {
		t.Fatalf("explicit off changed baseline outcome: %+v != %+v", unchanged, base[0])
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
		stationBuffers: []string{"off", "on"}, pickupReassignment: []string{"off", "on"},
	})
	scheduler := newArmScheduler(inputs, 0)
	if len(scheduler.groups) != 4 || len(scheduler.start()) != 4 {
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
		"-seeds", strings.Join(seeds, ","), "-loads", "5s,10s,15s",
		"-station-buffers", "off,on", "-pickup-reassignment", "off,on",
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "matrix") {
		t.Fatalf("experimental arms escaped matrix limit: %v", err)
	}
	// Profile expansion must also count both new dimensions.
	opts, err := parseOptions([]string{
		"-seeds", strings.Join(seeds, ","), "-pattern", "profile", "-bands", "all", "-loads", "10s,20s",
		"-station-buffers", "off,on", "-pickup-reassignment", "off,on",
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
	results := []result{{SharingConsent: sim.PrivateConsent, StationBuffers: "on", PickupReassignment: "off", PickupReassignmentStats: &pickupPolicyStats{}}}
	var output bytes.Buffer
	if err := writeReport(writeReportInput{output: &output, format: "json", results: results}); err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded.SchemaVersion != 13 || !reflect.DeepEqual(decoded.Results, results) {
		t.Fatalf("experimental JSON metadata lost: %v, %+v", err, decoded)
	}
	output.Reset()
	if err := writeReport(writeReportInput{output: &output, format: "csv", results: results, stationBufferColumn: true, pickupReassignmentColumn: true}); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for column, value := range map[string]string{"station_buffers": "on", "pickup_reassignment": "off"} {
		index := slices.Index(rows[0], column)
		if index < 0 || rows[1][index] != value {
			t.Fatalf("CSV lost %s: %+v", column, rows)
		}
	}
	output.Reset()
	if err := writeReport(writeReportInput{output: &output, format: "table", results: results, stationBufferColumn: true, pickupReassignmentColumn: true}); err != nil || !strings.Contains(output.String(), "BUFFERS") || !strings.Contains(output.String(), "REASSIGN") {
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
	if err := configureExperimentalPolicies(simulation, runInput{stationBuffers: "on", pickupReassignment: "on"}); err != nil || !simulation.NeedsBufferState() {
		t.Fatalf("enabled policies did not configure simulation: %v", err)
	}
	if err := configureExperimentalPolicies(simulation, runInput{stationBuffers: "invalid"}); err == nil {
		t.Fatal("invalid direct arm accepted")
	}
}
