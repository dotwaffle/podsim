package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
)

type destination struct {
	Station string  `json:"station"`
	Weight  float64 `json:"weight"`
}

type dailyBand struct {
	StartMinute     int `json:"startMinute"`
	DurationMinutes int `json:"durationMinutes"`
	PerMinute       int `json:"perMinute"`
}

type parkRide struct {
	Name             string        `json:"name"`
	Hub              string        `json:"hub"`
	Destinations     []destination `json:"destinations"`
	Morning          dailyBand     `json:"morning"`
	Evening          dailyBand     `json:"evening"`
	DailyStartMinute int           `json:"dailyStartMinute"`
}

func makeParkRide(config project.Config, plan parkRide) (project.DemandProfile, project.DemandConfig, error) {
	var empty project.DemandProfile
	if len(config.DemandProfiles) >= project.MaxProfiles {
		return empty, config.Demand, errors.New("the project already has eight demand profiles")
	}
	if !utf8.ValidString(plan.Name) || strings.TrimSpace(plan.Name) == "" || len(plan.Name) > 80 {
		return empty, config.Demand, errors.New("the profile name must contain 1 to 80 UTF-8 bytes")
	}
	if len(config.Network.Stations) > project.MaxStations || len(plan.Destinations) < 1 || len(plan.Destinations) >= project.MaxStations {
		return empty, config.Demand, errors.New("select 1 to 299 destinations")
	}
	passengers := make(map[string]bool, len(config.Network.Stations))
	stationIDs := make(map[string]bool, len(config.Network.Stations))
	for _, station := range config.Network.Stations {
		if station.ID == "" || len(station.ID) > 64 || !utf8.ValidString(station.ID) || stationIDs[station.ID] {
			return empty, config.Demand, errors.New("station IDs must be unique and contain 1 to 64 bytes")
		}
		stationIDs[station.ID] = true
		passengers[station.ID] = !station.ParkingOnly
	}
	if !passengers[plan.Hub] {
		return empty, config.Demand, errors.New("select a passenger-service hub")
	}
	profile := project.DemandProfile{
		ID: nextProfileID(config.DemandProfiles), Name: plan.Name,
		Bands: []project.DemandBand{
			{ID: "morning", Name: "Morning", StartMinute: plan.Morning.StartMinute, DurationMinutes: plan.Morning.DurationMinutes, PerMinute: new(plan.Morning.PerMinute)},
			{ID: "evening", Name: "Evening", StartMinute: plan.Evening.StartMinute, DurationMinutes: plan.Evening.DurationMinutes, PerMinute: new(plan.Evening.PerMinute)},
		},
		Flows: make([]project.DemandFlow, 0, 2*len(plan.Destinations)),
	}
	seen := make(map[string]bool, len(plan.Destinations))
	for _, dest := range plan.Destinations {
		if !passengers[dest.Station] || dest.Station == plan.Hub || seen[dest.Station] {
			return empty, config.Demand, errors.New("destinations must be unique passenger stations other than the hub")
		}
		if dest.Weight <= 0 || math.IsNaN(dest.Weight) || math.IsInf(dest.Weight, 0) {
			return empty, config.Demand, errors.New("destination weights must be finite and positive")
		}
		seen[dest.Station] = true
		profile.Flows = append(profile.Flows,
			project.DemandFlow{From: plan.Hub, To: dest.Station, Weights: []float64{dest.Weight, 0}},
			project.DemandFlow{From: dest.Station, To: plan.Hub, Weights: []float64{0, dest.Weight}},
		)
	}
	demand := config.Demand
	demand.Pattern, demand.Profile, demand.Band = "profile-daily", profile.ID, ""
	demand.DailyStartMinute = plan.DailyStartMinute
	if _, err := project.NewDailyProfile(demand, []project.DemandProfile{profile}); err != nil {
		return empty, config.Demand, err
	}
	updated := config
	updated.Demand = demand
	updated.DemandProfiles = make([]project.DemandProfile, len(config.DemandProfiles), len(config.DemandProfiles)+1)
	copy(updated.DemandProfiles, config.DemandProfiles)
	updated.DemandProfiles = append(updated.DemandProfiles, profile)
	var size projectSizeCounter
	if err := json.MarshalWrite(&size, updated); err != nil {
		return empty, config.Demand, fmt.Errorf("check created project size: %w", err)
	}
	return profile, demand, nil
}

func nextProfileID(profiles []project.DemandProfile) string {
	used := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		used[profile.ID] = true
	}
	for index := 1; ; index++ {
		id := "park-ride-" + strconv.Itoa(index)
		if !used[id] {
			return id
		}
	}
}

func decodeParkRide(data jsontext.Value) (parkRide, error) {
	var plan parkRide
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(data, &fields); err != nil {
		return plan, fmt.Errorf("decode park-and-ride fields: %w", err)
	}
	for _, name := range []string{"name", "hub", "destinations", "morning", "evening", "dailyStartMinute"} {
		if len(fields[name]) == 0 {
			return plan, fmt.Errorf("park-and-ride plan needs %s", name)
		}
	}
	for _, name := range []string{"morning", "evening"} {
		var band map[string]jsontext.Value
		if err := json.Unmarshal(fields[name], &band); err != nil {
			return plan, fmt.Errorf("decode %s band: %w", name, err)
		}
		for _, field := range []string{"startMinute", "durationMinutes", "perMinute"} {
			if len(band[field]) == 0 {
				return plan, fmt.Errorf("%s band needs %s", name, field)
			}
		}
	}
	if err := json.Unmarshal(data, &plan, json.RejectUnknownMembers(true)); err != nil {
		return plan, fmt.Errorf("decode park-and-ride plan: %w", err)
	}
	return plan, nil
}
