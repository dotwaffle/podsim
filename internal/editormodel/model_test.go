package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func validPlan() parkRide {
	return parkRide{
		Name: "Park and ride", Hub: "harbor", Destinations: []destination{{Station: "garden", Weight: 2}},
		Morning:          dailyBand{StartMinute: 420, DurationMinutes: 120, PerMinute: 12},
		Evening:          dailyBand{StartMinute: 1020, DurationMinutes: 120, PerMinute: 0},
		DailyStartMinute: 420,
	}
}

func TestParkRideConstruction(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Demand.Enabled, config.Demand.Seed = true, math.MaxUint64
	config.DemandProfiles = []project.DemandProfile{{ID: "park-ride-1"}, {ID: "park-ride-3"}}
	before := project.Clone(config)
	plan := validPlan()
	profile, demand, err := makeParkRide(config, plan)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "park-ride-2" || demand.Profile != profile.ID || demand.Pattern != "profile-daily" || demand.Band != "" || demand.DailyStartMinute != 420 || demand.Seed != math.MaxUint64 || !demand.Enabled || demand.PerMinute != config.Demand.PerMinute {
		t.Fatalf("profile/demand = %+v / %+v", profile, demand)
	}
	want := []project.DemandFlow{{From: "harbor", To: "garden", Weights: []float64{2, 0}}, {From: "garden", To: "harbor", Weights: []float64{0, 2}}}
	if !reflect.DeepEqual(profile.Flows, want) || *profile.Bands[1].PerMinute != 0 {
		t.Fatalf("flows/bands = %+v / %+v", profile.Flows, profile.Bands)
	}
	plan.Destinations[0].Weight = 100
	profile.Flows[0].Weights[0] = 200
	*profile.Bands[0].PerMinute = 99
	if !reflect.DeepEqual(config, before) || plan.Morning.PerMinute != 12 {
		t.Fatal("constructor or returned data aliases the input")
	}
}

func TestParkRideInvalidInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*project.Config, *parkRide)
	}{
		{"empty name", func(_ *project.Config, p *parkRide) { p.Name = " " }},
		{"long UTF8 name", func(_ *project.Config, p *parkRide) { p.Name = strings.Repeat("é", 41) }},
		{"unknown hub", func(_ *project.Config, p *parkRide) { p.Hub = "missing" }},
		{"parking hub", func(c *project.Config, _ *parkRide) { c.Network.Stations[0].ParkingOnly = true }},
		{"parking destination", func(c *project.Config, _ *parkRide) { c.Network.Stations[1].ParkingOnly = true }},
		{"duplicate parking ID", func(c *project.Config, _ *parkRide) {
			c.Network.Stations[0].ParkingOnly = true
			c.Network.Stations = append(c.Network.Stations, c.Network.Stations[0])
		}},
		{"same station", func(_ *project.Config, p *parkRide) { p.Destinations[0].Station = p.Hub }},
		{"no destinations", func(_ *project.Config, p *parkRide) { p.Destinations = nil }},
		{"duplicate destination", func(_ *project.Config, p *parkRide) { p.Destinations = append(p.Destinations, p.Destinations[0]) }},
		{"zero weight", func(_ *project.Config, p *parkRide) { p.Destinations[0].Weight = 0 }},
		{"negative weight", func(_ *project.Config, p *parkRide) { p.Destinations[0].Weight = -1 }},
		{"infinite weight", func(_ *project.Config, p *parkRide) { p.Destinations[0].Weight = math.Inf(1) }},
		{"nan weight", func(_ *project.Config, p *parkRide) { p.Destinations[0].Weight = math.NaN() }},
		{"overlap", func(_ *project.Config, p *parkRide) { p.Evening.StartMinute = 500 }},
		{"wrap overlap", func(_ *project.Config, p *parkRide) {
			p.Morning.StartMinute = 1380
			p.Morning.DurationMinutes = 120
			p.Evening.StartMinute = 30
		}},
		{"invalid duration", func(_ *project.Config, p *parkRide) { p.Morning.DurationMinutes = 0 }},
		{"invalid start", func(_ *project.Config, p *parkRide) { p.Morning.StartMinute = 1440 }},
		{"invalid clock", func(_ *project.Config, p *parkRide) { p.DailyStartMinute = -1 }},
		{"invalid rate", func(_ *project.Config, p *parkRide) { p.Morning.PerMinute = 121 }},
		{"profiles full", func(c *project.Config, _ *parkRide) {
			c.DemandProfiles = make([]project.DemandProfile, project.MaxProfiles)
		}},
	}
	for _, run := range cases {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			config, plan := project.Default(), validPlan()
			run.change(&config, &plan)
			if _, _, err := makeParkRide(config, plan); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
}

func TestWorkerCalls(t *testing.T) {
	t.Parallel()
	config := project.Default()
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, input string
		valid       bool
	}{
		{"valid", `{"op":"validate","project":` + string(encoded) + `}`, true},
		{"unknown operation", `{"op":"unknown","project":` + string(encoded) + `}`, false},
		{"unknown member", `{"op":"validate","project":` + string(encoded) + `,"extra":1}`, false},
		{"missing project", `{"op":"validate"}`, false},
		{"null project", `{"op":"validate","project":null}`, false},
		{"array project", `{"op":"validate","project":[]}`, false},
		{"invalid project", `{"op":"validate","project":{}}`, false},
		{"missing plan", `{"op":"park-ride","project":` + string(encoded) + `}`, false},
		{"invalid JSON", `{"op":`, false},
		{"oversized request", strings.Repeat(" ", MaxRequestBytes+1), false},
	}
	for _, run := range cases {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			var result response
			if decodeErr := json.Unmarshal([]byte(Call(run.input)), &result); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if result.Valid != run.valid || (result.Error == "") != run.valid {
				t.Fatalf("result = %+v", result)
			}
		})
	}
	plan := validPlan()
	input, err := json.Marshal(request{Op: "park-ride", Project: jsontext.Value(encoded), ParkRide: encodedParkRide(t, plan)})
	if err != nil {
		t.Fatal(err)
	}
	var result response
	if err := json.Unmarshal([]byte(Call(string(input))), &result); err != nil || result.Error != "" || result.Profile == nil || result.Demand == nil {
		t.Fatalf("constructor result %+v, decode error %v", result, err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{*result.Profile}, *result.Demand
	if err := project.Validate(config); err != nil {
		t.Fatalf("constructed project invalid: %v", err)
	}
}

func TestWorkerStructureBounds(t *testing.T) {
	t.Parallel()
	for _, run := range []struct{ name, text string }{
		{"nodes", `{"op":"validate","project":{"network":{"nodes":[` + strings.Repeat(`{},`, project.MaxNodes) + `{}]}}}`},
		{"weights", `{"op":"validate","project":{"demandProfiles":[{"flows":[{"weights":[` + strings.Repeat(`0,`, project.MaxBands) + `0]}]}]}}`},
		{"destinations", `{"op":"park-ride","project":{},"parkRide":{"destinations":[` + strings.Repeat(`{},`, project.MaxStations-1) + `{}]}}`},
		{"unknown array", `{"op":"validate","project":{"name":[{}]}}`},
		{"deep", strings.Repeat(`{"name":`, 65) + `0` + strings.Repeat(`}`, 65)},
		{"long string", `{"op":"validate","project":{"name":"` + strings.Repeat("x", 1024) + `"}}`},
	} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			if err := scanRequest([]byte(run.text)); err == nil {
				t.Fatal("oversized structure passed its prescan")
			}
		})
	}
}

// TestParkRideAllLongDestinations sends a plan with the largest number of
// destinations, each with an ID of the largest length. JSON writes each
// ID character as 1 byte.
func TestParkRideAllLongDestinations(t *testing.T) {
	t.Parallel()
	config, plan := project.Default(), validPlan()
	config.Network.Stations = []sim.Station{{ID: "hub", Name: "Hub"}}
	plan.Hub, plan.Destinations = "hub", nil
	for index := range project.MaxStations - 1 {
		id := strings.Repeat("d", project.MaxIDLength-3) + strconv.Itoa(index+100)
		config.Network.Stations = append(config.Network.Stations, sim.Station{ID: id, Name: "Destination"})
		plan.Destinations = append(plan.Destinations, destination{Station: id, Weight: 1})
	}
	encodedPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(encodedPlan) > MaxRequestBytes-project.MaxFileBytes {
		t.Fatalf("constructor envelope %d does not fit its allowance", len(encodedPlan))
	}
	encodedProject, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(request{Op: "park-ride", Project: jsontext.Value(encodedProject), ParkRide: encodedParkRide(t, plan)})
	if err != nil {
		t.Fatal(err)
	}
	var result response
	if err := json.Unmarshal([]byte(Call(string(input))), &result); err != nil || result.Error != "" || result.Profile == nil || len(result.Profile.Flows) != 2*(project.MaxStations-1) {
		t.Fatalf("full constructor failed: error=%s decode=%v", result.Error, err)
	}
}

func encodedParkRide(t *testing.T, plan parkRide) jsontext.Value {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return jsontext.Value(data)
}

func TestParkRideRequiredFields(t *testing.T) {
	t.Parallel()
	data := encodedParkRide(t, validPlan())
	var original map[string]jsontext.Value
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"name", "hub", "destinations", "morning", "evening", "dailyStartMinute"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields := make(map[string]jsontext.Value, len(original))
			for key, value := range original {
				if key != name {
					fields[key] = value
				}
			}
			encoded, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeParkRide(encoded); err == nil {
				t.Fatal("missing required field accepted")
			}
		})
	}
	for _, name := range []string{"morning", "evening"} {
		for _, field := range []string{"startMinute", "durationMinutes", "perMinute"} {
			t.Run(name+"/"+field, func(t *testing.T) {
				t.Parallel()
				fields := maps.Clone(original)
				var band map[string]jsontext.Value
				if err := json.Unmarshal(fields[name], &band); err != nil {
					t.Fatal(err)
				}
				delete(band, field)
				encoded, err := json.Marshal(band)
				if err != nil {
					t.Fatal(err)
				}
				fields[name] = encoded
				encoded, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := decodeParkRide(encoded); err == nil {
					t.Fatal("missing required band field accepted")
				}
			})
		}
	}
}

// TestCallDecodeErrorWording checks that a decode error in the reply has
// fixed wording, whatever modal verb encoding/json/v2 chose.
func TestCallDecodeErrorWording(t *testing.T) {
	t.Parallel()
	var result response
	reply := Call(`{"op":"validate","project":{"name":1}}`)
	if err := json.Unmarshal([]byte(reply), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "json: invalid JSON number at /name: expected Go string") {
		t.Fatalf("reply %s has no fixed wording", reply)
	}
}
