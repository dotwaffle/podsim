package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func dailyComparisonScenario(t *testing.T) scenario {
	t.Helper()
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.demand = project.DemandConfig{Pattern: "profile-daily", Profile: "daily", PerMinute: 7, DailyStartMinute: 1439}
	caseStudy.demandProfiles = []project.DemandProfile{{
		ID: "daily", Name: "Daily",
		Bands: []project.DemandBand{
			{ID: "night", Name: "Night", StartMinute: 1439, DurationMinutes: 2},
			{ID: "idle", Name: "Idle", StartMinute: 1, DurationMinutes: 1, PerMinute: new(0)},
			{ID: "morning", Name: "Morning", StartMinute: 3, DurationMinutes: 1, PerMinute: new(11)},
		},
		Flows: []project.DemandFlow{
			{From: "harbor", To: "market", Weights: []float64{3, 1, 1}},
			{From: "garden", To: "harbor", Weights: []float64{1, 3, 2}},
		},
	}}
	return caseStudy
}

func TestDailyOptions(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"request-every", "loads", "burst-size", "adaptive-limit", "past-limit", "bands"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{"request-every": "45s", "loads": "45s", "burst-size": "3", "adaptive-limit": "false", "past-limit": "1", "bands": ""}
			if _, err := parseOptions([]string{"-pattern", "profile-daily", "-" + flag, values[flag]}, &bytes.Buffer{}); err == nil {
				t.Fatal("accepted an explicit timing override")
			}
		})
	}
	for _, args := range [][]string{
		{"-daily-start-minute", "0"},
		{"-pattern", "profile-daily", "-daily-start-minute", "-1"},
		{"-pattern", "profile-daily", "-daily-start-minute", "1440"},
	} {
		if _, err := parseOptions(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	opts, err := parseOptions([]string{"-pattern", "profile-daily", "-duration", "100ms", "-daily-start-minute", "0"}, &bytes.Buffer{})
	if err != nil || !reflect.DeepEqual(opts.loads, []time.Duration{0}) || opts.dailyStartMinute != 0 {
		t.Fatalf("daily options: %+v, %v", opts, err)
	}
}

// The reference increments the offer budget at every physics tick. The
// production schedule jumps between offers within each whole minute.
func tickBudgetDailySchedule(input scheduleInput) []scheduledRequest {
	seed := uint64(input.seed) // #nosec G115 -- Match the signed schedule seed.
	rng := rand.New(rand.NewPCG(seed, ^seed))
	var result []scheduledRequest
	budget, previousBand, previousDay := 0, -2, int64(-2)
	for tick := int64(1); tick < input.durationTicks; tick++ {
		band, day := input.daily.BandOccurrence(tick)
		if band != previousBand || day != previousDay {
			budget, previousBand, previousDay = 0, band, day
		}
		budget += input.daily.Rate(band)
		if budget >= ticksPerMinute {
			budget -= ticksPerMinute
			from, to, _ := input.daily.Pair(band, rng.Float64())
			result = append(result, scheduledRequest{tick: tick, origin: from, destination: to})
		}
	}
	return result
}

func TestDailyScheduleMatchesTickBudget(t *testing.T) {
	t.Parallel()
	caseStudy := dailyComparisonScenario(t)
	for _, allDay := range []bool{false, true} {
		profiles := project.Clone(project.Config{DemandProfiles: caseStudy.demandProfiles}).DemandProfiles
		if allDay {
			profiles[0].Bands = profiles[0].Bands[:1]
			profiles[0].Bands[0].StartMinute, profiles[0].Bands[0].DurationMinutes = 0, 1440
			for index := range profiles[0].Flows {
				profiles[0].Flows[index].Weights = profiles[0].Flows[index].Weights[:1]
			}
		}
		daily, err := project.NewDailyProfile(caseStudy.demand, profiles)
		if err != nil {
			t.Fatal(err)
		}
		for _, end := range []int64{1, 2, 514, 515, 516, 3599, 3600, 3601, 7201, 1440*ticksPerMinute + 1} {
			input := scheduleInput{pattern: "profile-daily", daily: daily, seed: -7, durationTicks: end}
			got, want := demandSchedule(input), tickBudgetDailySchedule(input)
			if !slices.Equal(got, want) || len(got) != dailyOfferTicks(daily, end, nil) {
				t.Fatalf("all day %t, end %d: %d offers differ from %d tick offers", allDay, end, len(got), len(want))
			}
		}
	}
}

func TestDailyArmAndMemoryBound(t *testing.T) {
	t.Parallel()
	caseStudy := dailyComparisonScenario(t)
	for _, start := range []int{-1, 0, 420} {
		arm, err := dailyArm(options{dailyStartMinute: start}, caseStudy)
		if err != nil {
			t.Fatal(err)
		}
		want := start
		if start == -1 {
			want = caseStudy.demand.DailyStartMinute
		}
		if arm.dailyStartMinute != want || arm.profile != "daily" || arm.band != "" {
			t.Fatalf("daily arm: %+v", arm)
		}
	}
	caseStudy.demandProfiles[0].Bands = []project.DemandBand{{ID: "all", Name: "All", DurationMinutes: 1440, PerMinute: new(120)}}
	for index := range caseStudy.demandProfiles[0].Flows {
		caseStudy.demandProfiles[0].Flows[index].Weights = []float64{1}
	}
	arm, err := dailyArm(options{dailyStartMinute: -1}, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	opts := options{arrivalsFor: 24 * time.Hour, loads: []time.Duration{0}, seeds: []int64{1, 2}, redistributionPolicies: []string{"off"}}
	if err := validateDailyMatrix(opts, []demandArm{arm}, 1); err != nil {
		t.Fatal(err)
	}
	if err := validateDailyMatrix(opts, []demandArm{arm}, 2); err == nil {
		t.Fatal("accepted an oversized daily matrix")
	}
}

func TestDailyPositioningBoundary(t *testing.T) {
	t.Parallel()
	caseStudy := dailyComparisonScenario(t)
	arm, err := dailyArm(options{dailyStartMinute: -1}, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	simulation, err := sim.NewFleet(caseStudy.network, caseStudy.fleet)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		tick    int64
		skipped int
		rate    int
		mode    sim.Positioning
	}{
		{1, 0, 7, sim.PositioningGuarded},
		{7201, 0, 0, sim.PositioningOff},
		{10801, 0, 0, sim.PositioningOff},
		{14401, 0, 11, sim.PositioningGuarded},
		{14401, 1, 11, sim.PositioningOff},
	} {
		if err := configureDailyPositioning(simulation, arm.daily, row.tick, sim.PositioningGuarded, row.skipped); err != nil {
			t.Fatal(err)
		}
		if simulation.DemandRate() != row.rate || simulation.Positioning() != row.mode {
			t.Fatalf("tick %d: rate %d, mode %d", row.tick, simulation.DemandRate(), simulation.Positioning())
		}
	}
}

func TestDailyComparisonAndReports(t *testing.T) {
	t.Parallel()
	caseStudy := dailyComparisonScenario(t)
	opts, err := parseOptions([]string{"-pattern", "profile-daily", "-duration", "3m", "-arrivals-for", "2m", "-seeds", "7,19"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	one, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	opts.workers = 2
	many, err := compare(opts, caseStudy)
	if err != nil || !reflect.DeepEqual(one, many) {
		t.Fatalf("daily parallel parity: %v", err)
	}
	for _, row := range one {
		if row.DailyStartMinute == nil || *row.DailyStartMinute != 1439 || row.RequestEverySeconds != 0 || row.BurstSize != 0 || row.Scheduled != 13 || row.Served+row.Remaining+row.Skipped != row.Scheduled {
			t.Fatalf("daily report: %+v", row)
		}
	}
	for _, format := range []string{"json", "csv", "table"} {
		var output bytes.Buffer
		if reportErr := writeReport(writeReportInput{output: &output, format: format, results: one}); reportErr != nil {
			t.Fatal(reportErr)
		}
		if format == "csv" {
			rows, readErr := csv.NewReader(&output).ReadAll()
			if readErr != nil || rows[0][len(rows[0])-1] != "daily_start_minute" || rows[1][len(rows[1])-1] != "1439" {
				t.Fatalf("daily CSV: %v", readErr)
			}
		}
	}
	legacy, err := json.Marshal(result{})
	if err != nil || bytes.Contains(legacy, []byte("daily_start_minute")) {
		t.Fatal("daily field changed a legacy JSON report")
	}
	var output bytes.Buffer
	if err := writeCSV(writeReportInput{output: &output, results: []result{{}}}); err != nil || strings.Contains(output.String(), "daily_start_minute") {
		t.Fatal("daily column changed a legacy CSV report")
	}
}

func TestDailyComparisonMatchesLiveOffers(t *testing.T) {
	t.Parallel()
	skipLong(t)
	caseStudy := dailyComparisonScenario(t)
	config := project.Default()
	config.Fleet = config.Fleet[:1]
	for index := range config.Network.Nodes {
		config.Network.Nodes[index].Position.X *= 10
		config.Network.Nodes[index].Position.Y *= 10
	}
	for index := range config.Network.Lanes {
		lane := &config.Network.Lanes[index]
		lane.SpeedLimit = 1
		if lane.Control != nil {
			lane.Control.X *= 10
			lane.Control.Y *= 10
		}
	}
	config.Demand, config.DemandProfiles = caseStudy.demand, caseStudy.demandProfiles
	config.Demand.Enabled, config.Demand.Seed = true, 7
	shared, err := session.NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); shared.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done; shared.Close() })
	if reply := shared.Apply(session.Command{Client: "daily-comparison", Sequence: 1, Epoch: shared.State().Epoch, Action: "speed", Speed: 60}); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for shared.State().Simulation.Tick < 7201 {
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatal("live session did not cross the daily boundary")
		}
	}
	cancel()
	<-done
	state := shared.State()
	daily, err := project.NewDailyProfile(config.Demand, config.DemandProfiles)
	if err != nil {
		t.Fatal(err)
	}
	want := demandSchedule(scheduleInput{pattern: "profile-daily", daily: daily, seed: 7, durationTicks: state.Simulation.Tick + 1})
	seen := make(map[int]sim.Request)
	for _, request := range state.Simulation.Pending {
		seen[request.ID] = request
	}
	for _, vehicle := range state.Simulation.Vehicles {
		for _, request := range vehicle.Riders {
			seen[request.ID] = request
		}
	}
	if state.Demand.Generated != len(want) || state.Demand.Skipped != 0 || len(seen) != len(want) {
		t.Fatalf("live offers: generated%d skipped%d seen%d want%d", state.Demand.Generated, state.Demand.Skipped, len(seen), len(want))
	}
	for index, offer := range want {
		request := seen[index+1]
		if request.RequestedTick != offer.tick || request.From != offer.origin || request.To != offer.destination {
			t.Fatalf("offer%d: live%+v compare%+v", index, request, offer)
		}
	}
}
