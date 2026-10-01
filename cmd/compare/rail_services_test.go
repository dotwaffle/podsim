package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

func serviceComparisonDepartures() []project.RailDeparture {
	return []project.RailDeparture{
		{ID: "early", Station: "harbor", AtSeconds: 10, Passengers: 2, Origins: []project.RailOrigin{{Station: "market", Weight: 1}}},
		{ID: "later", Station: "harbor", AtSeconds: 600, WalkingSeconds: 2, RequestFromSeconds: 1, RequestUntilSeconds: 2, Passengers: 3, Origins: []project.RailOrigin{{Station: "market", Weight: 1}, {Station: "garden", Weight: 1}}},
	}
}

func TestRailServiceScheduleAndHash(t *testing.T) {
	t.Parallel()
	plan := serviceComparisonDepartures()
	arrivals := []project.RailArrival{{ID: "early", Station: "garden", Passengers: 1, Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
	for _, end := range []int64{1, 2, 60, 61, 90, 91, 120, 121} {
		input := scheduleInput{pattern: "rail-services", seed: 7, durationTicks: end, railArrivals: arrivals, railDepartures: plan}
		got := demandSchedule(input)
		want := project.RailServicesSchedule(arrivals, plan, 7)
		want = slices.DeleteFunc(want, func(offer project.RailServiceOffer) bool { return offer.Tick >= end })
		if len(got) != len(want) || cap(got) != len(want) {
			t.Fatalf("window%d: %d/%d", end, len(got), len(want))
		}
		for i := range got {
			if got[i].serviceOffer() != want[i] {
				t.Fatal("schedule identity differs")
			}
		}
	}
	base := demandSchedule(scheduleInput{pattern: "rail-services", seed: 7, durationTicks: 180, railArrivals: arrivals, railDepartures: plan})
	for _, edit := range []func(*scheduledRequest){func(r *scheduledRequest) { r.kind = "departure" }, func(r *scheduledRequest) { r.departureTick++ }, func(r *scheduledRequest) { r.walkingTicks++ }} {
		changed := slices.Clone(base)
		edit(&changed[0])
		if scheduleID(changed) == scheduleID(base) {
			t.Fatal("service hash omitted deadline or kind")
		}
	}
}

func TestRailServiceReportsAndOriginalCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, duration string
		unresolved     int
	}{
		{"inclusive deadline", "10m", 0}, {"capped later deadline", "2m", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions([]string{"-pattern", "rail-services", "-duration", tc.duration, "-arrivals-for", "3s", "-stop-when-drained", "-seeds", "7,19", "-redistribution-policies", "off"}, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			caseStudy, err := loadScenario("", "harbor")
			if err != nil {
				t.Fatal(err)
			}
			caseStudy.railDepartures = serviceComparisonDepartures()
			caseStudy.railArrivals = []project.RailArrival{{ID: "inbound", Station: "garden", Passengers: 1, Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
			one, err := compare(opts, caseStudy)
			if err != nil {
				t.Fatal(err)
			}
			opts.workers = 4
			many, err := compare(opts, caseStudy)
			if err != nil || !reflect.DeepEqual(one, many) {
				t.Fatalf("parallel outcomes: %v", err)
			}
			for _, row := range one {
				c := row.RailConnections
				if c == nil || len(c.Offers) != 5 || c.Unresolved != tc.unresolved || c.Made+c.Missed+c.Unserved+c.Unresolved != 5 || row.Scheduled != 6 || row.Served+row.Remaining+row.Skipped != 6 {
					t.Fatalf("report: %+v", row)
				}
				if tc.unresolved == 0 && (c.Made == 0 || c.Missed != 2 || row.ActualEndSeconds != 600) {
					t.Fatalf("deadline drain: %+v", row)
				}
				if tc.unresolved > 0 && row.ActualEndSeconds > 120 {
					t.Fatal("extended original cap")
				}
				encoded, err := json.Marshal(row)
				if err != nil || !bytes.Contains(encoded, []byte(`"rail_connections"`)) {
					t.Fatal("missing connection JSON")
				}
			}
		})
	}
	encoded, err := json.Marshal(result{})
	if err != nil || bytes.Contains(encoded, []byte(`"rail_connections"`)) {
		t.Fatal("changed absent JSON")
	}
}

func TestRailServiceSkipsAndForecastTargets(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-pattern", "rail-services", "-duration", "3s", "-queue-limit", "1", "-redistribution-policies", "off"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.railDepartures = serviceComparisonDepartures()
	rows, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row.RailConnections == nil || row.RailConnections.Unserved == 0 || row.RailConnections.Unserved != row.Skipped || len(row.RailSkippedOffers) != row.Skipped {
		t.Fatalf("skips: %+v", row)
	}
	schedule := demandSchedule(scheduleInput{pattern: "rail-services", seed: 7, durationTicks: 200, railDepartures: caseStudy.railDepartures})
	ledger := rail.NewConnections(caseStudy.railDepartures)
	if err := ledger.Add(schedule[2].serviceOffer(), 0, "queue-limit"); err != nil {
		t.Fatal(err)
	}
	targets := futureRailTargets(schedule, 0, ledger, 1)
	total := 0
	for _, target := range targets {
		total += target.Passengers
		if target.ReleaseTick <= 1 {
			t.Fatal("included past/current offer")
		}
	}
	if total != 2 {
		t.Fatalf("issued/past forecast identities: %+v", targets)
	}
	boundary := []scheduledRequest{{tick: 1, origin: "market"}, {tick: 300 * sim.TicksPerSecond, origin: "garden"}, {tick: 300*sim.TicksPerSecond + 1, origin: "market"}}
	targets = futureRailTargets(boundary, 0, nil, 0)
	if len(targets) != 2 || targets[1].Passengers != 1 {
		t.Fatal("incorrect forecast horizon")
	}
	if _, err := parseOptions([]string{"-rail-forecast", "-pattern", "balanced"}, &bytes.Buffer{}); err == nil {
		t.Fatal("forecast accepted ordinary pattern")
	}
	for _, flag := range [][]string{{"-loads", "5s"}, {"-request-every", "5s"}, {"-burst-size", "2"}} {
		args := append([]string{"-pattern", "rail-services"}, flag...)
		if _, err := parseOptions(args, &bytes.Buffer{}); err == nil {
			t.Fatal("accepted service rate sweep")
		}
	}
}

func TestRailServiceFullCapacityPreflight(t *testing.T) {
	t.Parallel()
	caseStudy := scenario{}
	for i := range 15 {
		caseStudy.railDepartures = append(caseStudy.railDepartures, project.RailDeparture{ID: strconv.Itoa(i), Passengers: 200, RequestFromSeconds: 1000, RequestUntilSeconds: 1000})
	}
	opts := options{seeds: make([]int64, 87), loads: []time.Duration{0}, redistributionPolicies: []string{"off"}, arrivalsFor: time.Second}
	arms := []demandArm{{pattern: "rail-services"}}
	// No offer is in the window, but each result allocates the full live ledger.
	if err := validateRailMatrix(opts, arms, caseStudy, 1); err != nil {
		t.Fatal(err)
	}
	opts.seeds = make([]int64, 88)
	if err := validateRailMatrix(opts, arms, caseStudy, 1); err == nil {
		t.Fatal("unissued full ledger capacity escaped storage bound")
	}
	opts.seeds = make([]int64, 87)
	if err := validateRailMatrix(opts, arms, caseStudy, 2); err == nil {
		t.Fatal("sharing/routing result copies escaped storage bound")
	}
	opts.redistributionPolicies = []string{"off", "guarded"}
	if err := validateRailMatrix(opts, arms, caseStudy, 1); err == nil {
		t.Fatal("redistribution result copies escaped storage bound")
	}
}
