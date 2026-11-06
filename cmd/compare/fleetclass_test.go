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

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestFleetClassOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want []string
		err  string
	}{
		{name: "absent"},
		{name: "plain", args: []string{"-fleet-classes", "plain"}, want: []string{"plain"}},
		{name: "list order", args: []string{"-project", "p", "-fleet-classes", "express, plain"}, want: []string{"express", "plain"}},
		{name: "empty", args: []string{"-fleet-classes", ""}, err: `unknown fleet class ""`},
		{name: "unknown", args: []string{"-fleet-classes", "group"}, err: `unknown fleet class "group"`},
		{name: "repeated", args: []string{"-fleet-classes", "plain,plain"}, err: `fleet class "plain" appears more than once`},
		{name: "express without project", args: []string{"-fleet-classes", "express"}, err: "fleet class express requires -project"},
		{name: "express with energy", args: []string{"-project", "p", "-energy-file", "e", "-fleet-classes", "plain,express"}, err: "fleet class express does not support energy-file"},
		{name: "express with virtual platoons", args: []string{"-project", "p", "-platoon-policies", "off,virtual", "-fleet-classes", "express"}, err: "fleet class express does not support platoon policy virtual"},
		{name: "express with platoons off", args: []string{"-project", "p", "-platoon-policies", "off", "-fleet-classes", "express"}, want: []string{"express"}},
		{name: "plain with energy", args: []string{"-energy-file", "e", "-fleet-classes", "plain"}, want: []string{"plain"}},
		{name: "plain with virtual platoons", args: []string{"-platoon-policies", "virtual", "-fleet-classes", "plain"}, want: []string{"plain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(tc.args, &bytes.Buffer{})
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("error %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(opts.fleetClasses, tc.want) {
				t.Fatalf("classes %q, want %q", opts.fleetClasses, tc.want)
			}
		})
	}
}

func TestFleetClassMatrixBound(t *testing.T) {
	t.Parallel()
	seeds := make([]string, 100)
	for index := range seeds {
		seeds[index] = strconv.Itoa(index)
	}
	args := []string{"-project", "p", "-seeds", strings.Join(seeds, ","), "-loads", "10s,20s,30s,40s,50s,60s"}
	if _, err := parseOptions(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	_, err := parseOptions(append(args, "-fleet-classes", "plain,express"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "the matrix must contain at most") {
		t.Fatalf("error %v", err)
	}
}

// TestFleetClassesExpandedMatrixBound checks that the fleet classes count in
// the limit after the bands of a profile expand the arms.
func TestFleetClassesExpandedMatrixBound(t *testing.T) {
	t.Parallel()
	seeds := make([]string, 100)
	for index := range seeds {
		seeds[index] = strconv.Itoa(index)
	}
	opts, err := parseOptions([]string{
		"-project", "p", "-duration", "1s", "-pattern", "profile", "-bands", "all", "-seeds", strings.Join(seeds, ","),
		"-loads", "100ms,200ms,300ms", "-redistribution-policies", "off", "-fleet-classes", "plain,express",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	config := scenarios.Small()
	caseStudy := ringScenario(t, config)
	stations := project.PassengerStations(config.Network)
	caseStudy.demand = project.DemandConfig{Pattern: "profile", Profile: "test", Band: "peak"}
	caseStudy.demandProfiles = []project.DemandProfile{{
		ID: "test", Name: "Test profile",
		Bands: []project.DemandBand{{ID: "peak", Name: "Peak"}, {ID: "quiet", Name: "Quiet"}},
		Flows: []project.DemandFlow{{From: stations[0].ID, To: stations[1].ID, Weights: []float64{1, 1}}},
	}}
	if _, err := compare(opts, caseStudy); err == nil || !strings.Contains(err.Error(), "expanded matrix") {
		t.Fatalf("error %v, want the expanded matrix limit", err)
	}
}

// TestFleetClassProjectRefusals runs the refusals that read the project, and
// checks that each writes no report.
func TestFleetClassProjectRefusals(t *testing.T) {
	t.Parallel()
	compact, err := scenarios.CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		config  project.Config
		classes string
		err     string
	}{
		{"compact bays", compact, "plain,express", "fleet class express: invalid project scenario: maneuver group"},
		{"plain arm of an Express project", expressConfig(t, scenarios.Small()), "plain,express", "fleet class plain needs a project with no Express pod"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := runCLI(cliInput{args: []string{"-project", writeProjectFile(t, tc.config), "-fleet-classes", tc.classes, "-duration", "1s", "-request-every", "500ms"}, stdout: &stdout, stderr: &stderr})
			if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tc.err) {
				t.Fatalf("code %d, stdout %q, stderr %q", code, stdout.String(), stderr.String())
			}
		})
	}
}

// expressConfig returns config in the form that expressScenario gives it.
func expressConfig(t *testing.T, config project.Config) project.Config {
	t.Helper()
	express, err := expressScenario(scenario{source: config, focus: firstPassengerStation(t, config)})
	if err != nil {
		t.Fatal(err)
	}
	return express.source
}

func firstPassengerStation(t *testing.T, config project.Config) string {
	t.Helper()
	stations := project.PassengerStations(config.Network)
	if len(stations) == 0 {
		t.Fatal("project has no passenger station")
	}
	return stations[0].ID
}

// TestExpressArmRunsExpress checks that the arm starts Express pods under the
// contract, on all classes, with no change to the plain scenario. It also runs
// an Express project with an Express arm only.
func TestExpressArmRunsExpress(t *testing.T) {
	t.Parallel()
	base := ringScenario(t, scenarios.Small())
	opts := options{fleetClasses: []string{plainFleetClass, expressFleetClass}}
	fleets, err := fleetArms(opts, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(fleets) != 2 || fleets[0].class != plainFleetClass || fleets[1].class != expressFleetClass {
		t.Fatalf("fleets %+v", fleets)
	}
	if !reflect.DeepEqual(fleets[0].scenario, base) {
		t.Fatal("the plain arm differs from the project")
	}
	if base.orderContract != "" || slices.ContainsFunc(base.fleet, func(pod sim.Placement) bool { return pod.Class == sim.ExpressClass }) {
		t.Fatal("the Express arm changed the plain scenario")
	}
	if !slices.ContainsFunc(base.network.Lanes, func(lane sim.Lane) bool { return lane.VehicleClasses == 0 }) {
		t.Fatal("the Express arm changed the plain lanes")
	}
	express := fleets[1].scenario
	input := runInput{policy: "off", pattern: "hotspot", duration: sim.TicksPerSecond, arrivalsFor: sim.TicksPerSecond, sharingLimit: 1, queueLimit: 1, scenario: express}
	arm, err := startArm(&input)
	if err != nil {
		t.Fatal(err)
	}
	state := arm.simulation.ExportState()
	if state.OrderContract != sim.ExpressOrderContract || len(state.Pods) != len(base.fleet) {
		t.Fatalf("contract %q, %d pods", state.OrderContract, len(state.Pods))
	}
	for _, pod := range state.Pods {
		if pod.Class != sim.ExpressClass {
			t.Fatalf("pod %s has class %q", pod.ID, pod.Class)
		}
	}
	if express.focus != base.focus || !slices.Equal(express.passengers, base.passengers) {
		t.Fatal("the Express arm changed the stations of the schedule")
	}
	again, err := fleetArms(options{fleetClasses: []string{expressFleetClass}}, express)
	if err != nil || len(again) != 1 || again[0].scenario.orderContract != sim.ExpressOrderContract {
		t.Fatalf("Express project: %v", err)
	}
}

// TestProjectMarkerStaysOutOfPlainRuns pins the default path: a marker in the
// project does not reach a run with no Express arm.
func TestProjectMarkerStaysOutOfPlainRuns(t *testing.T) {
	t.Parallel()
	config := scenarios.Small()
	config.OrderContract = sim.ExpressOrderContract
	caseStudy, err := loadScenario(writeProjectFile(t, config), "")
	if err != nil {
		t.Fatal(err)
	}
	if caseStudy.orderContract != "" {
		t.Fatalf("contract %q", caseStudy.orderContract)
	}
}

func decodeReport(t *testing.T, data []byte) report {
	t.Helper()
	var decoded report
	if err := jsonv2.Unmarshal(data, &decoded, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func cliReport(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := runCLI(cliInput{args: args, stdout: &stdout, stderr: &stderr}); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	return stdout.Bytes()
}

// TestFleetClassesPairArms checks the rows of a run: each seed has a plain and
// an Express row on one schedule, the plain row equals a run with no flag, and
// the report does not depend on the worker count.
func TestFleetClassesPairArms(t *testing.T) {
	t.Parallel()
	path := writeProjectFile(t, scenarios.Small())
	base := []string{"-project", path, "-pattern", "hotspot", "-request-every", "5s", "-duration", "5m", "-redistribution-policies", "off", "-seeds", "1,2", "-format", "json"}
	data := cliReport(t, append(slices.Clone(base), "-fleet-classes", "plain,express")...)
	if !bytes.Equal(data, cliReport(t, append(slices.Clone(base), "-fleet-classes", "plain,express", "-workers", "4")...)) {
		t.Fatal("the report depends on the worker count")
	}
	paired := decodeReport(t, data)
	baseline := decodeReport(t, cliReport(t, base...))
	if paired.SchemaVersion != 19 || baseline.SchemaVersion != 15 || len(paired.Results) != 4 || len(baseline.Results) != 2 {
		t.Fatalf("schema %d and %d, %d and %d rows", paired.SchemaVersion, baseline.SchemaVersion, len(paired.Results), len(baseline.Results))
	}
	if bytes.Contains(cliReport(t, base...), []byte("fleet_class")) {
		t.Fatal("a run with no flag reports a fleet class")
	}
	for index := range baseline.Results {
		plain, express := paired.Results[2*index], paired.Results[2*index+1]
		if plain.FleetClass != plainFleetClass || express.FleetClass != expressFleetClass {
			t.Fatalf("seed row %d: classes %q and %q", index, plain.FleetClass, express.FleetClass)
		}
		if plain.ScheduleID != express.ScheduleID || plain.Seed != express.Seed {
			t.Fatalf("seed row %d: schedules %s and %s", index, plain.ScheduleID, express.ScheduleID)
		}
		if plain.ServedPercentOfPlain != nil || express.ServedPercentOfPlain == nil {
			t.Fatalf("seed row %d: percents %v and %v", index, plain.ServedPercentOfPlain, express.ServedPercentOfPlain)
		}
		if want := 100 * float64(express.Served) / float64(plain.Served); *express.ServedPercentOfPlain != want {
			t.Fatalf("seed row %d: percent %v, want %v", index, *express.ServedPercentOfPlain, want)
		}
		plain.FleetClass = ""
		if !reflect.DeepEqual(plain, baseline.Results[index]) {
			t.Fatalf("seed row %d: the plain arm differs from a run with no flag", index)
		}
		express.FleetClass, express.ServedPercentOfPlain = "", nil
		if reflect.DeepEqual(plain, express) {
			t.Fatalf("seed row %d: the Express arm repeats the plain arm", index)
		}
	}
}

// TestFleetClassColumns checks that each format has the two columns with the
// flag, and has neither of them without it.
func TestFleetClassColumns(t *testing.T) {
	t.Parallel()
	path := writeProjectFile(t, scenarios.Small())
	base := []string{"-project", path, "-request-every", "5s", "-duration", "2m", "-redistribution-policies", "off"}
	for _, tc := range []struct {
		format string
		names  []string
	}{
		{"table", []string{"FLEET", "SERVED % PLAIN"}},
		{"csv", []string{"fleet_class", "served_percent_of_plain"}},
		{"json", []string{"fleet_class", "served_percent_of_plain"}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			t.Parallel()
			args := append(slices.Clone(base), "-format", tc.format)
			without := cliReport(t, args...)
			with := cliReport(t, append(slices.Clone(args), "-fleet-classes", "plain,express")...)
			for _, name := range tc.names {
				if !bytes.Contains(with, []byte(name)) {
					t.Errorf("%s is missing with the flag", name)
				}
				if bytes.Contains(without, []byte(name)) {
					t.Errorf("%s is present without the flag", name)
				}
			}
		})
	}
	rows, err := csv.NewReader(bytes.NewReader(cliReport(t, append(slices.Clone(base), "-format", "csv", "-fleet-classes", "plain,express")...))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	column := slices.Index(rows[0], "served_percent_of_plain")
	class := slices.Index(rows[0], "fleet_class")
	if len(rows) != 3 || class < 0 || column != class+1 || rows[1][class] != "plain" || rows[1][column] != "" || rows[2][class] != "express" || rows[2][column] == "" {
		t.Fatalf("CSV rows %q", rows)
	}
}

func TestAddServedPercents(t *testing.T) {
	t.Parallel()
	row := func(class string, served int, change func(*result)) result {
		outcome := result{FleetClass: class, Served: served, Seed: 1, ScheduleID: "a", Policy: "off", SharingConsent: sim.PrivateConsent}
		if change != nil {
			change(&outcome)
		}
		return outcome
	}
	otherSchedule := func(outcome *result) { outcome.ScheduleID = "b" }
	otherSeed := func(outcome *result) { outcome.Seed = 2 }
	otherPolicy := func(outcome *result) { outcome.Policy = "on" }
	otherWait := func(outcome *result) { outcome.WaitRule = "strict" }
	for _, tc := range []struct {
		name string
		rows []result
		want *float64
	}{
		{"pair", []result{row("plain", 100, nil), row("express", 91, nil)}, new(91.0)},
		{"Express first", []result{row("express", 50, nil), row("plain", 100, nil)}, new(50.0)},
		{"no plain row", []result{row("express", 91, nil)}, nil},
		{"plain served nothing", []result{row("plain", 0, nil), row("express", 91, nil)}, nil},
		{"other schedule", []result{row("plain", 100, otherSchedule), row("express", 91, nil)}, nil},
		{"other seed", []result{row("plain", 100, otherSeed), row("express", 91, nil)}, nil},
		{"other policy", []result{row("plain", 100, otherPolicy), row("express", 91, nil)}, nil},
		{"other wait rule", []result{row("plain", 100, otherWait), row("express", 91, nil)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			addServedPercents(tc.rows)
			for _, outcome := range tc.rows {
				if outcome.FleetClass == plainFleetClass {
					if outcome.ServedPercentOfPlain != nil {
						t.Fatal("a plain row has a percent")
					}
					continue
				}
				if !reflect.DeepEqual(outcome.ServedPercentOfPlain, tc.want) {
					t.Fatalf("percent %v, want %v", outcome.ServedPercentOfPlain, tc.want)
				}
			}
		})
	}
}

// TestFleetClassesAdaptiveGroups checks that each fleet has its own rate
// group, so a plain limit does not skip an Express rate.
func TestFleetClassesAdaptiveGroups(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-project", "p", "-fleet-classes", "plain,express", "-loads", "10s,5s", "-redistribution-policies", "off", "-stop-when-drained", "-adaptive-limit"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	inputs := fleetInputs([]runInput{{requestEvery: 10}, {requestEvery: 5}}, []fleetArm{{class: opts.fleetClasses[0]}, {class: opts.fleetClasses[1]}})
	groups := groupMembers(inputs)
	if len(groups) != 2 || !slices.Equal(groups[0], []int{0, 2}) || !slices.Equal(groups[1], []int{1, 3}) {
		t.Fatalf("groups %v", groups)
	}
}

// TestExpressArmWithOtherOptions runs an Express arm with each option that
// reaches the simulation, on a schedule that has rail departures.
func TestExpressArmWithOtherOptions(t *testing.T) {
	t.Parallel()
	plain := scenarios.Small()
	path := writeProjectFile(t, plain)
	stations := project.PassengerStations(plain.Network)
	railProject := scenarios.Small()
	railProject.RailDepartures = []project.RailDeparture{{
		ID: "train", Station: stations[0].ID, AtSeconds: 120, Passengers: 2,
		Origins: []project.RailOrigin{{Station: stations[1].ID, Weight: 1}},
	}}
	railPath := writeProjectFile(t, railProject)
	incidents := writeProjectFile(t, incidentRing(60, 60))
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"sharing", []string{"-project", path, "-sharing-limits", "1,4", "-sharing-consent", "shared"}},
		{"occupied pickups", []string{"-project", path, "-sharing-limits", "4", "-sharing-consent", "shared", "-onboard-pickups", "off,on"}},
		{"pickup reassignment", []string{"-project", path, "-pickup-reassignment", "off,on"}},
		{"wait rules", []string{"-project", path, "-wait-rules", "current,strict,none"}},
		{"platoons off", []string{"-project", path, "-platoon-policies", "off"}},
		{"guarded redistribution", []string{"-project", path, "-redistribution-policies", "on"}},
		{"bursts", []string{"-project", path, "-pattern", "bursty-hotspot"}},
		{"rail services", []string{"-project", railPath, "-pattern", "rail-services"}},
		{"incidents", []string{"-project", incidents}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := append(slices.Clone(tc.args), "-fleet-classes", "express,plain", "-duration", "3m", "-format", "json")
			if !slices.Contains(tc.args, "-pattern") {
				args = append(args, "-request-every", "10s")
			}
			if !slices.Contains(tc.args, "-redistribution-policies") {
				args = append(args, "-redistribution-policies", "off")
			}
			// An incident row has embedded columns that a result cannot decode.
			var decoded struct {
				SchemaVersion int `json:"schema_version"`
				Results       []struct {
					FleetClass string `json:"fleet_class"`
				} `json:"results"`
			}
			if err := jsonv2.Unmarshal(cliReport(t, args...), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.SchemaVersion != 19 || len(decoded.Results) == 0 || len(decoded.Results)%2 != 0 {
				t.Fatalf("schema %d, %d rows", decoded.SchemaVersion, len(decoded.Results))
			}
			for index, outcome := range decoded.Results {
				if want := []string{expressFleetClass, plainFleetClass}[index%2]; outcome.FleetClass != want {
					t.Fatalf("row %d has class %q", index, outcome.FleetClass)
				}
			}
		})
	}
}

// TestExpressArmSeparation runs the Express arm of the study on rail-hub with
// the separation oracle at each tick. The oracle does not change the row. The
// test detects a violation, and it is not a capacity claim.
func TestExpressArmSeparation(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long test")
	}
	express, err := expressScenario(ringScenario(t, scenarios.RailHub()))
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []int64{1, 2, 3} {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			t.Parallel()
			want, err := run(hotspotInput(express, seed))
			if err != nil {
				t.Fatal(err)
			}
			input := hotspotInput(express, seed)
			arm, err := startArm(&input)
			if err != nil {
				t.Fatal(err)
			}
			for tick := range durationTicks(input.duration) {
				if _, err := arm.step(tick); err != nil {
					t.Fatal(err)
				}
				observation := arm.simulation.SafetyObservation()
				if _, err := observation.Check(); err != nil {
					t.Fatalf("tick %d: %v", tick, err)
				}
			}
			if got := arm.result(); got.Served != want.Served || got.StoppedPodSeconds != want.StoppedPodSeconds {
				t.Fatalf("with the oracle: served %d and %v, without: %d and %v", got.Served, got.StoppedPodSeconds, want.Served, want.StoppedPodSeconds)
			}
			t.Logf("served %d", want.Served)
		})
	}
}
