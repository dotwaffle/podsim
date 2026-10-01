package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func railComparisonPlan() []project.RailArrival {
	var plan []project.RailArrival
	for index, passengers := range []int{3, 5, 7} {
		plan = append(plan, project.RailArrival{ID: "train" + strconv.Itoa(index), Station: "harbor", AtSeconds: index,
			Passengers: passengers, Destinations: []project.RailDestination{{Station: "market", Weight: 3}, {Station: "garden", Weight: 1}}})
	}
	return plan
}

func TestRailComparisonOptions(t *testing.T) {
	t.Parallel()
	for _, flag := range [][]string{{"-request-every", "5s"}, {"-loads", "5s"}, {"-burst-size", "3"}, {"-adaptive-limit=false"}, {"-past-limit", "1"}} {
		args := append([]string{"-pattern", "rail-arrivals"}, flag...)
		if _, err := parseOptions(args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted fixed-volume rate flag %v", flag)
		}
	}
	opts, err := parseOptions([]string{"-pattern", "rail-arrivals", "-duration", "100ms"}, &bytes.Buffer{})
	if err != nil || !slices.Equal(opts.loads, []time.Duration{0}) {
		t.Fatalf("short rail window: %+v %v", opts.loads, err)
	}
	all, err := parsePatterns("rail-arrivals", "all")
	if err != nil || !slices.Equal(all, syntheticPatterns) {
		t.Fatal("changed synthetic all expansion")
	}
	caseStudy, err := loadScenario("", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compare(opts, caseStudy); err == nil || !strings.Contains(err.Error(), "nonempty arrival plan") {
		t.Fatalf("accepted missing rail plan: %v", err)
	}
}

func TestRailScheduleBoundariesAndSharedIdentity(t *testing.T) {
	t.Parallel()
	plan := railComparisonPlan()
	for _, window := range []int64{1, 2, 60, 61, 120, 121} {
		input := scheduleInput{pattern: "rail-arrivals", seed: -7, durationTicks: window, railArrivals: plan}
		schedule := demandSchedule(input)
		seed := uint64(input.seed) // #nosec G115 -- Match the signed comparison seed's bits.
		want := project.RailSchedule(plan, seed)
		want = slices.DeleteFunc(want, func(offer project.RailOffer) bool { return offer.Tick >= window })
		if len(schedule) != len(want) || len(schedule) != railOfferCount(plan, window) {
			t.Fatalf("window %d: %d offers, want %d", window, len(schedule), len(want))
		}
		for index, request := range schedule {
			offer := want[index]
			if request.tick != offer.Tick || request.event != offer.Event || request.passenger != offer.Passenger || request.origin != offer.From || request.destination != offer.To {
				t.Fatalf("window %d offer %d: %+v, want %+v", window, index, request, offer)
			}
		}
	}
	plan[0].WalkingSeconds = 3
	schedule := demandSchedule(scheduleInput{pattern: "rail-arrivals", seed: 7, durationTicks: 181, railArrivals: plan})
	if schedule[len(schedule)-1].tick != 180 || schedule[len(schedule)-1].event != "train0" {
		t.Fatal("walking delay did not change release order")
	}
}

func TestRailScheduleHashIncludesEventAndPassenger(t *testing.T) {
	t.Parallel()
	base := []scheduledRequest{{tick: 1, event: "train", passenger: 1, origin: "harbor", destination: "market"}}
	for _, change := range []func(*scheduledRequest){
		func(request *scheduledRequest) { request.event = "other" },
		func(request *scheduledRequest) { request.passenger++ },
		func(request *scheduledRequest) { request.tick++ },
		func(request *scheduledRequest) { request.origin = "garden" },
		func(request *scheduledRequest) { request.destination = "garden" },
	} {
		changed := slices.Clone(base)
		change(&changed[0])
		if scheduleID(base) == scheduleID(changed) {
			t.Fatal("rail identity was absent from hash")
		}
	}
}

func TestRailMatrixMemoryPreflight(t *testing.T) {
	t.Parallel()
	opts := options{seeds: make([]int64, 100), loads: []time.Duration{0}, redistributionPolicies: []string{"off"}, arrivalsFor: time.Hour}
	caseStudy := scenario{}
	for index := range 50 {
		caseStudy.railArrivals = append(caseStudy.railArrivals, project.RailArrival{AtSeconds: index, Passengers: 200})
	}
	arms := []demandArm{{pattern: "rail-arrivals"}}
	if err := validateRailMatrix(opts, arms, caseStudy, 1); err != nil {
		t.Fatal(err)
	}
	opts.redistributionPolicies = []string{"off", "on"}
	if err := validateRailMatrix(opts, arms, caseStudy, 1); err == nil {
		t.Fatal("redistribution copies escaped the memory bound")
	}
	opts.arrivalsFor = time.Second
	if err := validateRailMatrix(opts, arms, caseStudy, 1); err != nil {
		t.Fatal("preflight counted releases outside the arrival window")
	}
}

func TestRailComparisonWorkersAndSkippedIdentity(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-pattern", "rail-arrivals", "-duration", "4s", "-queue-limit", "1", "-seeds", "7,19"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	caseStudy, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.railArrivals = railComparisonPlan()
	first, err := compare(opts, caseStudy)
	if err != nil {
		t.Fatal(err)
	}
	opts.workers = 4
	second, err := compare(opts, caseStudy)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("worker-dependent rail results: %v", err)
	}
	for _, row := range first {
		schedule := demandSchedule(scheduleInput{pattern: row.Pattern, seed: row.Seed, durationTicks: durationTicks(opts.arrivalsFor), railArrivals: caseStudy.railArrivals})
		if row.Scheduled != 15 || row.Skipped == 0 || row.Skipped != len(row.RailSkippedOffers) || cap(row.RailSkippedOffers) > row.Scheduled || scheduleID(schedule) != row.ScheduleID {
			t.Fatalf("incorrect skipped evidence: %+v", row)
		}
		if row.BurstSize != 0 || row.RequestEverySeconds != 0 || row.Served+row.Remaining+row.Skipped != row.Scheduled {
			t.Fatal("rail metadata or conservation mismatch")
		}
		previous := -1
		for _, index := range row.RailSkippedOffers {
			if index <= previous || index >= len(schedule) || schedule[index].event == "" {
				t.Fatal("invalid skipped event index")
			}
			previous = index
		}
		encoded, encodeErr := json.Marshal(row)
		if encodeErr != nil || !bytes.Contains(encoded, []byte(`"rail_skipped_offers"`)) {
			t.Fatal("missing JSON evidence")
		}
	}
	legacy, err := json.Marshal(result{})
	if err != nil || bytes.Contains(legacy, []byte(`"rail_skipped_offers"`)) {
		t.Fatal("changed legacy JSON member set")
	}
}

func TestRailMatrixRejectsBeforeCreatingSimulation(t *testing.T) {
	t.Parallel()
	opts, err := parseOptions([]string{"-pattern", "rail-arrivals", "-duration", "2m"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	opts.seeds = make([]int64, 100)
	caseStudy, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.fleet = []sim.Placement{{ID: "invalid", BerthID: "missing"}}
	for index := range 50 {
		caseStudy.railArrivals = append(caseStudy.railArrivals, project.RailArrival{ID: strconv.Itoa(index), AtSeconds: index, Station: "harbor", Passengers: 200,
			Destinations: []project.RailDestination{{Station: "market", Weight: 1}}})
	}
	if _, err := compare(opts, caseStudy); err == nil || !strings.Contains(err.Error(), "rail matrix") {
		t.Fatalf("did not reject before invalid fleet creation: %v", err)
	}
}

func TestRailComparisonMatchesLiveReleaseTimestamps(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.RailArrivals = railComparisonPlan()
	config.Demand = project.DemandConfig{Enabled: true, Pattern: "rail-arrivals", PerMinute: 12, Seed: 7}
	shared, err := session.NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); shared.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done; shared.Close() })
	if reply := shared.Apply(session.Command{Client: "rail-comparison", Sequence: 1, Epoch: shared.State().Epoch, Action: "speed", Speed: 60}); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for shared.State().Simulation.Tick < 120 {
		select {
		case <-poll.C:
		case <-deadline.C:
			t.Fatal("live session did not reach the last release")
		}
	}
	cancel()
	<-done
	state := shared.State()
	want := demandSchedule(scheduleInput{pattern: "rail-arrivals", seed: 7, durationTicks: 121, railArrivals: config.RailArrivals})
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
		t.Fatalf("live offers missing: generated=%d skipped=%d seen=%d", state.Demand.Generated, state.Demand.Skipped, len(seen))
	}
	for index, offer := range want {
		request := seen[index+1]
		if request.RequestedTick != offer.tick || request.From != offer.origin || request.To != offer.destination {
			t.Fatalf("offer %d: live %+v, comparison %+v", index, request, offer)
		}
	}
}
