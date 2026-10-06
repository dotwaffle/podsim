package parkride

import (
	"encoding/json/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func runInput() RunInput {
	plan := testPlan()
	plan.Itineraries[0].PartySize = 1
	return RunInput{Project: project.Default(), Plan: plan, HorizonTicks: 1200 * sim.TicksPerSecond, QueueLimit: 200, Build: "test-build"}
}

func finishRun(t *testing.T, run *Run) {
	t.Helper()
	for !run.Done() {
		if err := run.Step(); err != nil {
			t.Fatal(err)
		}
		if _, err := run.pods.SafetyObservation().Check(); err != nil {
			t.Fatalf("physical check at %d: %v", run.pods.Tick(), err)
		}
	}
}

func TestNativePairedJourneyAndClone(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		class   sim.VehicleClass
		size    int
		consent sim.SharingConsent
	}{
		{"legacy-private", sim.LegacyClass, 1, sim.PrivateConsent}, {"legacy-shared", sim.LegacyClass, 1, sim.SharedConsent},
		{"compact-private-party2", sim.CompactClass, 2, sim.PrivateConsent}, {"compact-shared-party2", sim.CompactClass, 2, sim.SharedConsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := runInput()
			consent := tc.consent
			input.Plan.Itineraries[0].PartySize = tc.size
			for index := range input.Project.Fleet {
				input.Project.Fleet[index].Class = tc.class
			}
			input.Project.SharedRidePartyLimit = 2
			input.Plan.Itineraries[0].SharingConsent = consent
			input.Plan.Itineraries[0].ActivitySeconds = 2
			input.Plan.Itineraries[0].RetrievalSeconds = 1
			input.Plan.Itineraries[0].HomeboundSeconds = 2
			run, err := NewRun(input)
			if err != nil {
				t.Fatal(err)
			}
			if run.pods.Snapshot().Submitted != 1 {
				t.Fatal("preflight retained native orders")
			}
			// Change every caller-owned reference after construction.
			input.Plan.Itineraries[0].CarID = "changed"
			input.Plan.Lots[0].Capacity = 0
			input.Project.Network.Stations[0].ID = "changed"
			seen := make(map[int]bool)
			for run.ledger.records[0].Outward.AlightedTick < 0 && !run.Done() {
				state := run.pods.ExportState()
				for _, pod := range state.Pods {
					for _, request := range pod.Riders {
						if request.SharingConsent != consent || request.PartySize != tc.size {
							t.Fatal("outward native consent or party changed")
						}
						seen[request.ID] = true
					}
				}
				if err = run.Step(); err != nil {
					t.Fatal(err)
				}
				if _, err = run.pods.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
			}
			if run.ledger.records[0].Outward.AlightedTick < 0 || !run.ledger.records[0].Held {
				t.Fatal("outward did not complete with car retained")
			}
			clone := run.Clone()
			for !run.Done() {
				if err = run.Step(); err != nil {
					t.Fatal(err)
				}
				if _, err = run.pods.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				state := run.pods.ExportState()
				for _, pod := range state.Pods {
					for _, request := range pod.Riders {
						if request.SharingConsent != consent || request.PartySize != tc.size {
							t.Fatal("return consent or party changed")
						}
						seen[request.ID] = true
					}
				}
			}
			finishRun(t, clone)
			if !reflect.DeepEqual(run.Report(), clone.Report()) || !reflect.DeepEqual(run.pods.ExportState(), clone.pods.ExportState()) {
				t.Fatal("linked clone continuation changed results")
			}
			report := run.Report()
			record := report.Itineraries[0]
			if record.Outcome != "completed" || record.Itinerary.CarID != "car-a" || record.Itinerary.SharingConsent != consent || record.Outward.RequestID == record.Return.RequestID || record.Return.AlightedTick < 0 || record.DoorToDoorTicks == nil || report.Lots[0].Occupancy != 0 || report.Pods.Completed != 2 || len(seen) != 2 {
				t.Fatalf("paired native journey %+v, seen %v", report, seen)
			}
			if run.pods.RequestTimings() != nil || run.pods.NodePasses() != nil {
				t.Fatal("run enabled unbounded experiment history")
			}
			*record.DoorToDoorTicks = -1
			if *run.Report().Itineraries[0].DoorToDoorTicks < 0 {
				t.Fatal("report aliases completed duration")
			}
			saved, err := json.Marshal(run.pods.ExportState())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(saved), "car-a") {
				t.Fatal("native save unexpectedly contains external car ledger")
			}
		})
	}
}

func TestRunConstructorControlsAndPreflight(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*RunInput)
	}{
		{"negative-queue", func(i *RunInput) { i.QueueLimit = -1 }}, {"zero-queue", func(i *RunInput) { i.QueueLimit = 0 }}, {"large-queue", func(i *RunInput) { i.QueueLimit = MaxQueueLimit + 1 }},
		{"negative-horizon", func(i *RunInput) { i.HorizonTicks = -1 }}, {"large-horizon", func(i *RunInput) { i.HorizonTicks = MaxHorizonTicks + 1 }},
		{"bad-direct-plan", func(i *RunInput) { i.Plan.Itineraries[0].PartySize = 0 }},
		{"native-party-capacity", func(i *RunInput) { i.Plan.Itineraries[0].CarSeats = 8; i.Plan.Itineraries[0].PartySize = 8 }},
		{"legacy-party2", func(i *RunInput) { i.Plan.Itineraries[0].PartySize = 2 }},
		{"invalid-project", func(i *RunInput) { i.Project.Fleet = nil }},
		{"incident-marker", func(i *RunInput) { i.Project.IncidentContract = sim.IncidentV1Contract }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := runInput()
			tc.change(&input)
			if run, err := NewRun(input); err == nil || run != nil {
				t.Fatal("invalid constructor/preflight accepted")
			}
		})
	}
	input := runInput()
	input.Plan = Plan{Lots: []Lot{}, Itineraries: []Itinerary{}}
	run, err := NewRun(input)
	if err != nil || !run.Done() || run.Report().EndpointTick != 0 {
		t.Fatal("empty batch did not finish")
	}
	input = runInput()
	input.Plan.Itineraries[0].DepartureSeconds = 2000
	run, err = NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if run.Done() {
		t.Fatal("empty native queue stopped future itinerary")
	}
	input = runInput()
	input.Project.Redistribution = true
	input.Project.Demand.Enabled = true
	input.Project.Demand.Pattern = "profile-daily"
	input.Project.Demand.Profile = "daily"
	input.Project.DemandProfiles = []project.DemandProfile{{ID: "daily", Name: "daily", Bands: []project.DemandBand{{ID: "all", Name: "all", DurationMinutes: 1440}}, Flows: []project.DemandFlow{{From: "harbor", To: "market", Weights: []float64{1}}}}}
	input.Plan.Itineraries[0].DepartureSeconds = 1
	input.HorizonTicks = 120
	run, err = NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	finishRun(t, run)
	if run.Report().Pods.Submitted != 1 || run.Report().Policies.Positioning != sim.PositioningGuarded || run.pods.DemandRate() != 0 {
		t.Fatal("daily demand injected offers or changed observed-rate positioning")
	}
}

func TestRunSourcesAndPausedFault(t *testing.T) {
	t.Parallel()
	input := runInput()
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	other := runInput()
	other.Project.Name = "changed source"
	second, err := NewRun(other)
	if err != nil {
		t.Fatal(err)
	}
	if run.Report().ProjectHash == second.Report().ProjectHash || run.Report().PlanHash != second.Report().PlanHash {
		t.Fatal("source hashes conflated project and plan")
	}
	run.pods.SetPaused(true)
	if err = run.Step(); err == nil || run.Report().Error == "" || !run.Done() {
		t.Fatal("paused native clock did not stop with partial evidence")
	}
}

func TestPreflightChecksReturnDirection(t *testing.T) {
	t.Parallel()
	input := runInput()
	input.Plan.Itineraries[0].PartySize = 2
	for index := range input.Project.Fleet {
		input.Project.Fleet[index].Class = sim.CompactClass
	}
	legacy, err := sim.NewClassSet("legacy")
	if err != nil {
		t.Fatal(err)
	}
	for index := range input.Project.Network.Lanes {
		if input.Project.Network.Lanes[index].ID == "harbor-in" {
			input.Project.Network.Lanes[index].VehicleClasses = legacy
		}
	}
	if err = project.Validate(input.Project); err != nil {
		t.Fatal(err)
	}
	pods, err := sim.NewFleet(input.Project.Network, input.Project.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pods.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", PartySize: 2}); err != nil {
		t.Fatal("outward fixture must be admissible", err)
	}
	if run, err := NewRun(input); err == nil || run != nil || !strings.Contains(err.Error(), "market to harbor") {
		t.Fatalf("return direction bypassed preflight: %v", err)
	}
}
