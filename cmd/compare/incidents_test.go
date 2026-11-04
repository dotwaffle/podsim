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
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentRing returns the small ring with both rates. A pod fault evacuates
// after 30 seconds, and each fault lasts 60 seconds.
func incidentRing(faults, emergencies float64) project.Config {
	config := scenarios.Small()
	config.Faults = &project.FaultConfig{
		PerHour: new(faults), DebrisShare: new(0.25), DebrisMeters: new(2.0), EvacuationSeconds: new(30),
		Duration: &project.FaultDuration{Kind: "fixed", Seconds: new(60)},
	}
	config.Emergencies = &project.EmergencyConfig{PerHour: new(emergencies)}
	return config
}

// ringScenario loads config from a project file, as a comparison does.
func ringScenario(t *testing.T, config project.Config) scenario {
	t.Helper()
	caseStudy, err := loadScenario(writeProjectFile(t, config), "")
	if err != nil {
		t.Fatal(err)
	}
	return caseStudy
}

// balancedInput returns an arm of steady demand for 8 minutes that stops when
// it drains. It uses the step path of a run with no service.
func balancedInput(caseStudy scenario, seed int64) runInput {
	const arrivalsFor, every = 8 * time.Minute, 10 * time.Second
	schedule := demandSchedule(scheduleInput{
		seed: seed, durationTicks: durationTicks(arrivalsFor), intervalTicks: durationTicks(every),
		pattern: "balanced", passengers: caseStudy.passengers, focus: caseStudy.focus,
	})
	return runInput{
		policy: "off", duration: time.Hour, arrivalsFor: arrivalsFor, requestEvery: every, seed: seed,
		pattern: "balanced", queueLimit: len(schedule) + 1, sharingLimit: 1, routingPolicy: "free-flow",
		schedule: schedule, scenario: caseStudy, stopWhenDrained: true,
	}
}

// simulated runs the arm of input to its end.
func simulated(t *testing.T, input runInput) *armRun {
	t.Helper()
	arm, err := startArm(&input)
	if err != nil {
		t.Fatal(err)
	}
	if err := arm.simulate(); err != nil {
		t.Fatal(err)
	}
	return arm
}

// TestLoadScenarioKeepsIncidentSettings checks that a project with each rate
// loads with both members. The project validation still runs first.
func TestLoadScenarioKeepsIncidentSettings(t *testing.T) {
	t.Parallel()
	duration := &project.FaultDuration{Kind: "fixed", Seconds: new(600)}
	for name, test := range map[string]struct {
		faults      *project.FaultConfig
		emergencies *project.EmergencyConfig
		name        string
		want        string
	}{
		"fault rate":     {faults: &project.FaultConfig{PerHour: new(1.0), Duration: duration}},
		"emergency rate": {emergencies: &project.EmergencyConfig{PerHour: new(1.0)}},
		"both rates":     {faults: &project.FaultConfig{PerHour: new(1.0), Duration: duration}, emergencies: &project.EmergencyConfig{PerHour: new(2.0)}},
		"rates of 0":     {faults: &project.FaultConfig{PerHour: new(0.0)}, emergencies: &project.EmergencyConfig{PerHour: new(0.0)}},
		"validate first": {emergencies: &project.EmergencyConfig{PerHour: new(61.0)}, want: "emergencies perHour must be 0 to 60"},
		"validate name":  {emergencies: &project.EmergencyConfig{PerHour: new(1.0)}, name: " ", want: "project name"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := scenarios.Small()
			config.Faults, config.Emergencies = test.faults, test.emergencies
			if test.name != "" {
				config.Name = test.name
			}
			caseStudy, err := loadScenario(writeProjectFile(t, config), "")
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("loadScenario: %v, want %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(caseStudy.faults, test.faults) || !reflect.DeepEqual(caseStudy.emergencies, test.emergencies) {
				t.Fatalf("the scenario has %+v and %+v, want %+v and %+v", caseStudy.faults, caseStudy.emergencies, test.faults, test.emergencies)
			}
		})
	}
}

// TestIncidentArmSetup checks the arm setup of section 5.3.2. An arm with a
// rate has both operations on and the samplers of its own seed, and an arm
// with no rate keeps its operations off.
func TestIncidentArmSetup(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                string
		faults, emergencies float64
	}{
		{"both rates", 60, 30},
		{"fault rate", 60, 0},
		{"emergency rate", 0, 30},
		{"no rate", 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := incidentRing(test.faults, test.emergencies)
			caseStudy := ringScenario(t, config)
			want := make(map[int64]sim.SavedState, 2)
			for _, seed := range []int64{7, 19} {
				input := balancedInput(caseStudy, seed)
				arm, err := startArm(&input)
				if err != nil {
					t.Fatal(err)
				}
				if got := arm.simulation.Snapshot().OperationsOff; got != (test.faults == 0 && test.emergencies == 0) {
					t.Fatalf("operations off is %t", got)
				}
				want[seed] = arm.simulation.ExportState()
				config.Demand.Seed = uint64(seed)
				reference, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
				if err != nil {
					t.Fatal(err)
				}
				if test.faults > 0 || test.emergencies > 0 {
					if err := project.ConfigureExperiments(reference, config, true); err != nil {
						t.Fatal(err)
					}
				}
				if err := project.ConfigureIncidentSampling(reference, config); err != nil {
					t.Fatal(err)
				}
				state := reference.ExportState()
				if !reflect.DeepEqual(want[seed].Faults, state.Faults) || !reflect.DeepEqual(want[seed].Emergencies, state.Emergencies) {
					t.Fatalf("seed %d: the samplers are %+v and %+v, want %+v and %+v", seed, want[seed].Faults, want[seed].Emergencies, state.Faults, state.Emergencies)
				}
				if test.faults == 0 && test.emergencies == 0 {
					continue
				}
				id := caseStudy.fleet[0].ID
				if _, err := arm.simulation.Fault(sim.FaultRequest{PodID: id}); err != nil {
					t.Fatal(err)
				}
				active := arm.simulation.Snapshot().Faults.Active
				if len(active) != 1 || active[0].EvacuateTick == nil || *active[0].EvacuateTick != 30*sim.TicksPerSecond {
					t.Fatalf("the fault is %+v, want an evacuation after 30 seconds", active)
				}
			}
			if test.faults > 0 && reflect.DeepEqual(want[7].Faults, want[19].Faults) {
				t.Error("two seeds have one fault stream")
			}
			if test.emergencies > 0 && reflect.DeepEqual(want[7].Emergencies, want[19].Emergencies) {
				t.Error("two seeds have one emergency stream")
			}
		})
	}
}

// TestIncidentRunsEachRate checks that each rate alone runs its own kind of
// event, and that the columns report the counters of the simulation.
func TestIncidentRunsEachRate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                string
		faults, emergencies float64
	}{
		{"fault rate", 60, 0},
		{"emergency rate", 0, 60},
		{"both rates", 60, 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			arm := simulated(t, balancedInput(ringScenario(t, incidentRing(test.faults, test.emergencies)), 1))
			state := arm.simulation.Snapshot()
			faults, emergencies := state.Faults.Counters, state.Emergencies.Counters
			if (faults.Sampled > 0) != (test.faults > 0) || (emergencies.Sampled > 0) != (test.emergencies > 0) {
				t.Fatalf("sampled %d faults and %d emergencies", faults.Sampled, emergencies.Sampled)
			}
			want := incidentColumns{
				Interrupted:  state.Interrupted,
				FaultStarted: faults.Started, FaultSkipped: faults.Skipped,
				EmergencyStarted: emergencies.Started, EmergencySkipped: emergencies.Skipped,
			}
			row := arm.result()
			if row.incidentColumns == nil || *row.incidentColumns != want {
				t.Fatalf("the columns are %+v, want %+v", row.incidentColumns, want)
			}
			if test.faults > 0 && (want.FaultStarted == 0 || faults.Sampled-faults.Skipped != want.FaultStarted) {
				t.Errorf("fault counters %+v", faults)
			}
			if test.emergencies > 0 && (want.EmergencyStarted == 0 || emergencies.Sampled-emergencies.Skipped != want.EmergencyStarted) {
				t.Errorf("emergency counters %+v", emergencies)
			}
		})
	}
}

// TestIncidentTerminalAccounting checks each row of section 5.3.2 on one run.
// The order is terminal when it is interrupted, so the arm stops early. The
// report shows it as interrupted, and not as remaining or as served.
func TestIncidentTerminalAccounting(t *testing.T) {
	t.Parallel()
	input := balancedInput(ringScenario(t, incidentRing(60, 60)), 1)
	arm := simulated(t, input)
	state, midpoint, arrival := arm.simulation.MetricsSnapshot(), arm.midpointState, arm.arrivalState
	if midpoint.Interrupted == 0 || arrival.Interrupted == 0 {
		t.Fatalf("the fixture has %d and %d interrupted orders at the midpoint and the arrival end", midpoint.Interrupted, arrival.Interrupted)
	}
	row := arm.result()
	if !row.Drained || row.Remaining != 0 || row.ActualEndSeconds >= row.WindowEndSeconds || row.DrainSeconds <= 0 {
		t.Fatalf("the arm did not stop early: drained %t, remaining %d, end %.0f of %.0f, drain %.1f", row.Drained, row.Remaining, row.ActualEndSeconds, row.WindowEndSeconds, row.DrainSeconds)
	}
	if state.Completed+state.Interrupted != state.Submitted || state.Interrupted != row.Interrupted || row.Served != state.Completed {
		t.Fatalf("the state has %d, %d, and %d orders; the row serves %d and interrupts %d", state.Submitted, state.Completed, state.Interrupted, row.Served, row.Interrupted)
	}
	if row.Scheduled != row.Served+row.Interrupted+row.Remaining+row.Skipped {
		t.Fatalf("%d scheduled, %d served, %d interrupted, %d remaining, %d skipped", row.Scheduled, row.Served, row.Interrupted, row.Remaining, row.Skipped)
	}
	wantMidpoint := midpoint.Submitted - midpoint.Completed - midpoint.Interrupted
	wantArrival := arrival.Submitted - arrival.Completed - arrival.Interrupted
	if row.BacklogAtArrivalMidpoint != wantMidpoint || row.BacklogAtArrivalEnd != wantArrival || row.LateBacklogChange != wantArrival-wantMidpoint {
		t.Fatalf("the backlogs are %d, %d, and %d, want %d, %d, and %d", row.BacklogAtArrivalMidpoint, row.BacklogAtArrivalEnd, row.LateBacklogChange, wantMidpoint, wantArrival, wantArrival-wantMidpoint)
	}
	if row.CompletedAtArrivalEnd != arrival.Completed || row.ArrivalThroughputPerMinute != float64(arrival.Completed)/input.arrivalsFor.Minutes() {
		t.Fatalf("the throughput counts more than completed orders: %d, %v", row.CompletedAtArrivalEnd, row.ArrivalThroughputPerMinute)
	}
}

// TestPeakOutstandingIsTerminal checks that an interrupted order leaves the
// peak of the outstanding orders.
func TestPeakOutstandingIsTerminal(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := newRunMetrics(caseStudy, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics.observe(sim.Snapshot{Submitted: 10, Completed: 3, Interrupted: 4})
	if metrics.peakOutstanding != 3 {
		t.Fatalf("the peak is %d, want 3", metrics.peakOutstanding)
	}
}

// railRingInput returns a rail-services arm of the ring. Each order of the
// departure at second 900 waits for its deadline, so the run reaches it.
func railRingInput(caseStudy scenario, seed int64) runInput {
	const arrivalsFor = 10 * time.Minute
	caseStudy.railDepartures = []project.RailDeparture{{
		ID: "outbound", Station: "station-01", AtSeconds: 900, Passengers: 60, RequestFromSeconds: 1, RequestUntilSeconds: 590,
		Origins: []project.RailOrigin{{Station: "station-02", Weight: 1}, {Station: "station-03", Weight: 1}, {Station: "station-04", Weight: 1}},
	}}
	schedule := demandSchedule(scheduleInput{seed: seed, durationTicks: durationTicks(arrivalsFor), pattern: "rail-services", railDepartures: caseStudy.railDepartures})
	return runInput{
		policy: "off", duration: time.Hour, arrivalsFor: arrivalsFor, seed: seed, pattern: "rail-services",
		queueLimit: len(schedule) + 1, sharingLimit: 1, routingPolicy: "free-flow",
		schedule: schedule, scenario: caseStudy, stopWhenDrained: true,
	}
}

// dailyRingInput returns a profile-daily arm of the ring. It uses the service
// path with no rail ledger.
func dailyRingInput(t *testing.T, caseStudy scenario, seed int64) runInput {
	t.Helper()
	const arrivalsFor = 15 * time.Minute
	caseStudy.demand = project.DemandConfig{Pattern: "profile-daily", Profile: "daily", PerMinute: 6}
	caseStudy.demandProfiles = []project.DemandProfile{{
		ID: "daily", Name: "Daily", Bands: []project.DemandBand{{ID: "day", Name: "Day", DurationMinutes: 1440}},
		Flows: []project.DemandFlow{
			{From: "station-01", To: "station-03", Weights: []float64{1}},
			{From: "station-03", To: "station-02", Weights: []float64{1}},
			{From: "station-02", To: "station-04", Weights: []float64{1}},
		},
	}}
	arm, err := dailyArm(options{dailyStartMinute: -1}, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	schedule := demandSchedule(scheduleInput{seed: seed, durationTicks: durationTicks(arrivalsFor), pattern: arm.pattern, daily: arm.daily})
	return runInput{
		policy: "off", duration: time.Hour, arrivalsFor: arrivalsFor, seed: seed, pattern: arm.pattern, profile: arm.profile,
		daily: arm.daily, dailyStartMinute: arm.dailyStartMinute, queueLimit: len(schedule) + 1, sharingLimit: 1, routingPolicy: "free-flow",
		schedule: schedule, scenario: caseStudy, stopWhenDrained: true,
	}
}

// TestIncidentDelivery checks each Step path. The delivered count equals the
// interrupted orders, and the simulation holds no more of them. Conservation
// cannot show this, because an interruption updates the counters at once.
func TestIncidentDelivery(t *testing.T) {
	t.Parallel()
	caseStudy := ringScenario(t, incidentRing(60, 60))
	for name, input := range map[string]runInput{
		"no service":    balancedInput(caseStudy, 1),
		"rail services": railRingInput(caseStudy, 1),
		"daily":         dailyRingInput(t, caseStudy, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			arm := simulated(t, input)
			interrupted := arm.simulation.MetricsSnapshot().Interrupted
			if interrupted == 0 {
				t.Fatal("the fixture has no interrupted order")
			}
			if arm.delivered != interrupted {
				t.Fatalf("delivered %d orders of %d", arm.delivered, interrupted)
			}
			if left := arm.simulation.DrainInterruptions(); len(left) != 0 {
				t.Fatalf("the simulation holds the orders %v", left)
			}
			if arm.connections != nil {
				checkInterruptedRecords(t, arm.connections)
			}
		})
	}
}

// checkInterruptedRecords checks that some record ended interrupted.
func checkInterruptedRecords(t *testing.T, connections *rail.Connections) {
	t.Helper()
	if !slices.ContainsFunc(connections.Records(), func(record rail.Connection) bool {
		return record.Outcome == "unserved" && record.Reason == "interrupted"
	}) {
		t.Fatalf("no interrupted record in %+v", connections.Counts())
	}
}

// TestRailInterruptionOnDepartureTick checks an evacuation in the departure
// tick of the connection of its order. The drain comes before Advance, so the
// order ends unserved with reason interrupted, and not missed.
func TestRailInterruptionOnDepartureTick(t *testing.T) {
	t.Parallel()
	const departureSeconds = 120
	caseStudy, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.railDepartures = []project.RailDeparture{{ID: "outbound", Station: "harbor", AtSeconds: departureSeconds, Passengers: 1, Origins: []project.RailOrigin{{Station: "market", Weight: 1}}}}
	schedule := demandSchedule(scheduleInput{seed: 1, durationTicks: durationTicks(time.Minute), pattern: "rail-services", railDepartures: caseStudy.railDepartures})
	arm, err := startArm(&runInput{
		policy: "off", duration: 5 * time.Minute, arrivalsFor: time.Minute, seed: 1, pattern: "rail-services",
		queueLimit: 10, sharingLimit: 1, routingPolicy: "free-flow", schedule: schedule, scenario: caseStudy,
	})
	if err != nil {
		t.Fatal(err)
	}
	departure := int64(departureSeconds * sim.TicksPerSecond)
	faulted := false
	for tick := range departure {
		if _, err := arm.step(tick); err != nil {
			t.Fatal(err)
		}
		state := arm.simulation.MetricsSnapshot()
		if tick < departure-1 && state.Interrupted != 0 {
			t.Fatalf("tick %d: the fixture interrupts before the departure", state.Tick)
		}
		if faulted || state.Tick%sim.TicksPerSecond != 0 {
			continue
		}
		index := slices.IndexFunc(state.Vehicles, func(vehicle sim.Vehicle) bool { return vehicle.Pod.Occupied })
		if index < 0 {
			continue
		}
		delay := int((departure - state.Tick) / sim.TicksPerSecond)
		if err := arm.simulation.SetFaults(true, sim.FaultSettings{EvacuationSeconds: delay}); err != nil {
			t.Fatal(err)
		}
		if _, err := arm.simulation.Fault(sim.FaultRequest{PodID: state.Vehicles[index].Pod.ID}); err != nil {
			t.Fatal(err)
		}
		faulted = true
	}
	state := arm.simulation.MetricsSnapshot()
	if !faulted || state.Tick != departure || state.Interrupted != 1 {
		t.Fatalf("the fixture has fault %t, tick %d, and %d interrupted orders", faulted, state.Tick, state.Interrupted)
	}
	records := arm.connections.Records()
	if len(records) != 1 || records[0].Outcome != "unserved" || records[0].Reason != "interrupted" {
		t.Fatalf("the records are %+v", records)
	}
	if counts := arm.connections.Counts(); counts != (rail.Counts{Unserved: 1}) {
		t.Fatalf("the counts are %+v", counts)
	}
	if arm.delivered != 1 {
		t.Fatalf("delivered %d orders", arm.delivered)
	}
}

// TestIncidentRunsAreRepeatable checks that two runs of one arm give one
// report, also with parallel workers, so the arms share no sampler.
func TestIncidentRunsAreRepeatable(t *testing.T) {
	t.Parallel()
	path := writeProjectFile(t, incidentRing(60, 60))
	args := []string{"-project", path, "-duration", "6m", "-arrivals-for", "4m", "-request-every", "10s", "-seeds", "1,2", "-routing-policies", "free-flow,queue"}
	first := runComparison(t, args)
	second := runComparison(t, slices.Concat(args, []string{"-workers", "4"}))
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two runs give other reports:\n%+v\n%+v", first, second)
	}
	if first[0].incidentColumns == nil || first[0].EmergencyStarted+first[0].FaultStarted == 0 {
		t.Fatalf("the fixture has no incident: %+v", first[0].incidentColumns)
	}
}

// TestIncidentRateWithOtherPolicies runs a rate with each policy that reads
// the fleet in its own way. The arm reports an error if a policy refuses a
// state with an incident.
func TestIncidentRateWithOtherPolicies(t *testing.T) {
	t.Parallel()
	skipLong(t)
	caseStudy := ringScenario(t, incidentRing(60, 60))
	for name, change := range map[string]func(*runInput){
		"energy":   func(input *runInput) { input.energy = energyTestModel() },
		"platoons": func(input *runInput) { input.platoonPolicy = "virtual" },
		"sharing":  func(input *runInput) { input.sharingLimit, input.sharingConsent = 2, sim.SharedConsent },
		"positioning": func(input *runInput) {
			input.policy = "on"
		},
		"pickups": func(input *runInput) { input.pickupReassignment = "on" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := balancedInput(caseStudy, 1)
			change(&input)
			row, err := run(input)
			if err != nil {
				t.Fatal(err)
			}
			if row.incidentColumns == nil || row.EmergencyStarted+row.FaultStarted == 0 {
				t.Fatalf("no incident: %+v", row.incidentColumns)
			}
		})
	}
	t.Run("rail forecast", func(t *testing.T) {
		t.Parallel()
		input := railRingInput(caseStudy, 1)
		input.railForecast = true
		if _, err := run(input); err != nil {
			t.Fatal(err)
		}
	})
}

// TestIncidentColumnsOnlyWithRate checks the columns of each format. A row with
// a rate has each column, also at 0, and a row with no rate has none. The
// schema version does not change.
func TestIncidentColumnsOnlyWithRate(t *testing.T) {
	t.Parallel()
	plain := result{SharingConsent: sim.PrivateConsent, Pattern: "balanced", Policy: "off", ScheduleID: "abc", Scheduled: 1}
	values := plain
	values.incidentColumns = &incidentColumns{Interrupted: 1, FaultStarted: 2, FaultSkipped: 3, EmergencyStarted: 4, EmergencySkipped: 5}
	zero := plain
	zero.incidentColumns = &incidentColumns{}
	names := []string{"interrupted", "fault_started", "fault_skipped", "emergency_started", "emergency_skipped"}
	write := func(t *testing.T, format string, row result) string {
		t.Helper()
		var output bytes.Buffer
		if err := writeReport(writeReportInput{output: &output, format: format, results: []result{row}}); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	t.Run("csv", func(t *testing.T) {
		t.Parallel()
		records, err := csv.NewReader(strings.NewReader(write(t, "csv", values))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		for index, name := range names {
			at := slices.Index(records[0], name)
			if at < 0 || records[1][at] != strconv.Itoa(index+1) {
				t.Errorf("%s: at %d in %v", name, at, records[1])
			}
		}
		for _, name := range names {
			if strings.Contains(write(t, "csv", plain), name) {
				t.Errorf("no rate has the column %s", name)
			}
		}
	})
	t.Run("json", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			row  result
			want map[string]int
		}{
			{values, map[string]int{"interrupted": 1, "fault_started": 2, "fault_skipped": 3, "emergency_started": 4, "emergency_skipped": 5}},
			{zero, map[string]int{"interrupted": 0, "fault_started": 0, "fault_skipped": 0, "emergency_started": 0, "emergency_skipped": 0}},
			{plain, nil},
		} {
			var decoded struct {
				SchemaVersion int              `json:"schema_version"`
				Results       []map[string]any `json:"results"`
			}
			if err := jsonv2.Unmarshal([]byte(write(t, "json", test.row)), &decoded, json.DefaultOptionsV1()); err != nil {
				t.Fatal(err)
			}
			if decoded.SchemaVersion != 12 {
				t.Errorf("schema version %d", decoded.SchemaVersion)
			}
			for _, name := range names {
				got, ok := decoded.Results[0][name]
				want, wanted := test.want[name]
				if ok != wanted || wanted && got != float64(want) {
					t.Errorf("%s: %v, want %d", name, got, want)
				}
			}
		}
	})
	t.Run("table", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split(strings.TrimSpace(write(t, "table", values)), "\n")
		header, row := lines[len(lines)-2], lines[len(lines)-1]
		if !strings.HasSuffix(header, "INTERRUPTED  FAULT STARTED  FAULT SKIPPED  EMERGENCY STARTED  EMERGENCY SKIPPED") {
			t.Errorf("the header is %q", header)
		}
		if fields := strings.Fields(row); !slices.Equal(fields[len(fields)-5:], []string{"1", "2", "3", "4", "5"}) {
			t.Errorf("the row is %q", row)
		}
		if strings.Contains(write(t, "table", plain), "INTERRUPTED") {
			t.Error("no rate has the column INTERRUPTED")
		}
	})
}
