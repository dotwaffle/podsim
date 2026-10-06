package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestComparisonConsentOptions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
		want sim.SharingConsent
	}{
		{"omitted", nil, sim.PrivateConsent},
		{"private", []string{"-sharing-consent", "private"}, sim.PrivateConsent},
		{"shared", []string{"-sharing-consent", "shared"}, sim.SharedConsent},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions(test.args, &bytes.Buffer{})
			if err != nil || opts.sharingConsent != test.want {
				t.Fatalf("consent %q, want %q: %v", opts.sharingConsent, test.want, err)
			}
		})
	}
	for _, value := range []string{"", "legacy-unknown", "shared,private", "Shared", " private", "future"} {
		t.Run("invalid-"+value, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions([]string{"-sharing-consent", value}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "sharing-consent") {
				t.Fatalf("accepted invalid consent %q: %v", value, err)
			}
		})
	}
}

func TestComparisonConsentRejectsRawBeforeRun(t *testing.T) {
	t.Parallel()
	for _, consent := range []sim.SharingConsent{sim.SharingConsent("legacy-unknown"), "future", " shared"} {
		t.Run(string(consent), func(t *testing.T) {
			t.Parallel()
			// The empty scenario would fail construction if validation ran later.
			if _, err := run(runInput{sharingConsent: consent}); err == nil || !strings.Contains(err.Error(), "sharing-consent") {
				t.Fatalf("run accepted invalid consent: %v", err)
			}
			if _, err := compare(options{sharingConsent: consent}, scenario{}); err == nil || !strings.Contains(err.Error(), "sharing-consent") {
				t.Fatalf("matrix accepted invalid consent: %v", err)
			}
		})
	}
}

func TestComparisonConsentPrivateCannotPool(t *testing.T) {
	t.Parallel()
	input := smallBurstInput(t)
	input.sharingLimit = 8
	input.sharingMode = sim.SharedRideDropOffs
	input.sharingJoin = "reassign-existing"
	before := slices.Clone(input.schedule)
	private, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	input.sharingConsent = sim.PrivateConsent
	explicit, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(private, explicit) || private.SharedParties != 0 || private.Occupancy != 1 {
		t.Fatalf("private pooled with limit eight: %+v", private)
	}
	input.sharingConsent = sim.SharedConsent
	shared, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	if shared.SharedParties == 0 || shared.Occupancy <= 1 || shared.Occupancy > 8 {
		t.Fatalf("explicit shared control did not pool: %+v", shared)
	}
	if !reflect.DeepEqual(before, input.schedule) || private.Scheduled != shared.Scheduled || private.ScheduleID != shared.ScheduleID {
		t.Fatal("consent changed offer identity")
	}
	for _, outcome := range []result{private, shared} {
		if outcome.Scheduled != outcome.Served+outcome.Remaining+outcome.Skipped {
			t.Fatal("consent changed request conservation")
		}
	}
}

func TestComparisonConsentCommonAcrossArms(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, consent := range []string{"private", "shared"} {
		t.Run(consent, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions([]string{"-duration", "2m", "-request-every", "5s", "-pattern", "hub-burst", "-burst-size", "6",
				"-sharing-consent", consent, "-sharing-limits", "1,8", "-sharing-modes", "destination,drop-offs",
				"-sharing-joins", "unassigned,reassign-existing", "-redistribution-policies", "off"}, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			caseStudy, err := loadScenario("", "market")
			if err != nil {
				t.Fatal(err)
			}
			rows, err := compare(opts, caseStudy)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 6 {
				t.Fatal("consent unexpectedly added comparison arms", len(rows))
			}
			pooled := false
			for _, row := range rows {
				if row.SharingConsent != sim.SharingConsent(consent) {
					t.Fatal("matched arm changed offer consent")
				}
				if row.ScheduleID != rows[0].ScheduleID || row.Scheduled != rows[0].Scheduled {
					t.Fatal("matched arms changed offered demand")
				}
				if row.SharedRidePartyLimit == 1 && row.SharedParties != 0 || consent == "private" && row.SharedParties != 0 {
					t.Fatal("arm policy invented party consent")
				}
				pooled = pooled || row.SharedParties > 0
			}
			if (consent == "shared") != pooled {
				t.Fatal("common shared control did not pool independently of its arm policies")
			}
		})
	}
}

func TestComparisonConsentSeparatesAdaptiveGroups(t *testing.T) {
	t.Parallel()
	inputs := []runInput{{}, {sharingConsent: sim.PrivateConsent}, {sharingConsent: sim.SharedConsent}}
	want := [][]int{{0, 1}, {2}}
	if got := groupMembers(inputs); !reflect.DeepEqual(got, want) {
		t.Fatalf("adaptive consent groups %v, want %v", got, want)
	}
}

func TestComparisonConsentPreservesOfflineQueue(t *testing.T) {
	t.Parallel()
	skipLong(t)
	opts, err := parseOptions([]string{"-queue-limit", "1000000"}, &bytes.Buffer{})
	if err != nil || opts.queueLimit != maxQueueLimit {
		t.Fatalf("offline queue option changed: %v", err)
	}
	input := smallBurstInput(t)
	input.duration, input.arrivalsFor = 100*time.Millisecond, 100*time.Millisecond
	input.queueLimit = maxQueueLimit
	input.schedule = make([]scheduledRequest, sim.MaxSavedWaitingTrips+2)
	for i := range input.schedule {
		input.schedule[i] = scheduledRequest{origin: input.scenario.passengers[0], destination: input.scenario.passengers[1]}
	}
	outcome, err := run(input)
	if err != nil || outcome.Skipped != 0 || outcome.Remaining != len(input.schedule) {
		t.Fatalf("saved-state bound became offline admission: %+v %v", outcome, err)
	}
}

func TestComparisonConsentRailDepartureOffers(t *testing.T) {
	t.Parallel()
	caseStudy, err := loadScenario("", "harbor")
	if err != nil {
		t.Fatal(err)
	}
	caseStudy.fleet = []sim.Placement{{ID: "01", StationID: "market", BerthID: "market-1"}}
	caseStudy.railDepartures = []project.RailDeparture{{ID: "outbound", Station: "harbor", AtSeconds: 600, Passengers: 3,
		Origins: []project.RailOrigin{{Station: "market", Weight: 1}}}}
	schedule := demandSchedule(scheduleInput{seed: 1, durationTicks: 2, pattern: "rail-services", railDepartures: caseStudy.railDepartures})
	for _, consent := range []sim.SharingConsent{sim.PrivateConsent, sim.SharedConsent} {
		t.Run(string(consent), func(t *testing.T) {
			t.Parallel()
			outcome, err := run(runInput{policy: "off", pattern: "rail-services", duration: 100 * time.Millisecond, arrivalsFor: 100 * time.Millisecond,
				sharingConsent: consent, sharingLimit: 8, routingPolicy: "free-flow", queueLimit: 200, scenario: caseStudy, schedule: schedule})
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Scheduled != 3 || outcome.Skipped != 0 || outcome.RailConnections == nil || len(outcome.RailConnections.Offers) != 3 {
				t.Fatalf("consent changed the connection offers: %+v", outcome)
			}
			if (consent == sim.SharedConsent) != (outcome.SharedParties > 0) || outcome.SharingConsent != consent {
				t.Fatal("departure offer ignored common consent")
			}
		})
	}
}

func TestComparisonConsentCLIProvenance(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"json", "csv", "table"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			args := []string{"-duration", "2m", "-request-every", "30s", "-redistribution-policies", "off", "-format", format}
			var baseline bytes.Buffer
			if code := runCLI(cliInput{args: args, stdout: &baseline, stderr: &bytes.Buffer{}}); code != 0 {
				t.Fatal("default consent run failed")
			}
			for _, consent := range []string{"private", "shared"} {
				var output bytes.Buffer
				if code := runCLI(cliInput{args: append(slices.Clone(args), "-sharing-consent", consent), stdout: &output, stderr: &bytes.Buffer{}}); code != 0 {
					t.Fatal("explicit consent run failed")
				}
				switch format {
				case "json":
					for _, source := range []struct {
						data []byte
						want sim.SharingConsent
					}{{baseline.Bytes(), sim.PrivateConsent}, {output.Bytes(), sim.SharingConsent(consent)}} {
						var decoded report
						if err := json.Unmarshal(source.data, &decoded); err != nil || decoded.SchemaVersion != 12 || len(decoded.Results) != 1 || decoded.Results[0].SharingConsent != source.want || !bytes.Contains(source.data, []byte(`"sharing_consent"`)) {
							t.Fatalf("missing effective JSON consent: %s", source.data)
						}
					}
				case "csv":
					base, err := csv.NewReader(bytes.NewReader(baseline.Bytes())).ReadAll()
					if err != nil {
						t.Fatal(err)
					}
					rows, err := csv.NewReader(bytes.NewReader(output.Bytes())).ReadAll()
					if err != nil {
						t.Fatal(err)
					}
					column := slices.Index(rows[0], "sharing_consent")
					if slices.Contains(base[0], "sharing_consent") || column < 0 || rows[1][column] != consent || len(rows[0]) != len(base[0])+1 || len(rows[1]) != len(rows[0]) {
						t.Fatal("conditional CSV consent column changed")
					}
					if !slices.Equal(slices.Delete(slices.Clone(rows[0]), column, column+1), base[0]) {
						t.Fatal("consent changed another CSV column")
					}
					if consent == "private" && !slices.Equal(slices.Delete(slices.Clone(rows[1]), column, column+1), base[1]) {
						t.Fatal("explicit private changed default report values")
					}
				case "table":
					if strings.Contains(baseline.String(), "SHARING CONSENT") || !strings.Contains(output.String(), "SHARING CONSENT") || !strings.Contains(output.String(), consent) {
						t.Fatal("conditional table consent column changed")
					}
				}
			}
		})
	}
}

func TestComparisonConsentReportDefaultsAreOwned(t *testing.T) {
	t.Parallel()
	rows := []result{{Pattern: "balanced", Scheduled: 1}}
	var output bytes.Buffer
	if err := writeReport(writeReportInput{output: &output, format: "json", results: rows}); err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded.Results[0].SharingConsent != sim.PrivateConsent || rows[0].SharingConsent != "" {
		t.Fatal("private synthetic fixture default changed its caller")
	}
	output.Reset()
	if err := writeReport(writeReportInput{output: &output, format: "json", results: []result{{SharingConsent: sim.SharingConsent("legacy-unknown")}}}); err == nil || output.Len() != 0 {
		t.Fatal("report published invalid consent")
	}
}
