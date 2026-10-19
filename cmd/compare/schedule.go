package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math/rand"
	randv2 "math/rand/v2"
	"os"
	"sort"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type scheduledRequest struct {
	tick          int64
	origin        string
	destination   string
	event         string
	passenger     int
	kind          string
	departureTick int64
	walkingTicks  int64
}

type scenario struct {
	network        sim.Network
	fleet          []sim.Placement
	passengers     []string
	focus          string
	demand         project.DemandConfig
	demandProfiles []project.DemandProfile
	railArrivals   []project.RailArrival
	railDepartures []project.RailDeparture
}

func loadScenario(path, focus string) (scenario, error) {
	config := project.Config{
		Network: sim.Example(),
		Fleet: []sim.Placement{
			{ID: "01", StationID: "parking", BerthID: "parking-1"},
			{ID: "02", StationID: "garden", BerthID: "garden-1"},
		},
	}
	if path != "" {
		var err error
		config, err = readProject(path)
		if err != nil {
			return scenario{}, err
		}
	}
	stations := project.PassengerStations(config.Network)
	passengers := make([]string, len(stations))
	for i, station := range stations {
		passengers[i] = station.ID
	}
	if focus == "" {
		focus = config.Demand.Destination
	}
	if focus == "" {
		for _, station := range passengers {
			if station == "market" {
				focus = station
				break
			}
		}
	}
	if focus == "" && len(passengers) > 0 {
		focus = passengers[len(passengers)-1]
	}
	found := false
	for _, station := range passengers {
		found = found || station == focus
	}
	if !found {
		return scenario{}, fmt.Errorf("focus %q must name a passenger station", focus)
	}
	return scenario{
		network: config.Network, fleet: config.Fleet, passengers: passengers, focus: focus,
		demand: config.Demand, demandProfiles: config.DemandProfiles, railArrivals: config.RailArrivals, railDepartures: config.RailDepartures,
	}, nil
}

func readProject(path string) (project.Config, error) {
	file, err := os.Open(path) // #nosec G304 -- The operator selects this local file.
	if err != nil {
		return project.Config{}, fmt.Errorf("open project %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return project.Config{}, fmt.Errorf("stat project %s: %w", path, err)
	}
	if info.Size() > project.MaxFileBytes {
		return project.Config{}, fmt.Errorf("read project %s: file exceeds %d MiB", path, project.MaxFileBytes>>20)
	}
	// Member names match exactly. The other encoding/json rules stay.
	decoder := jsontext.NewDecoder(io.LimitReader(file, project.MaxFileBytes), json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true))
	var config project.Config
	if err := jsonv2.UnmarshalDecode(decoder, &config); err != nil {
		return project.Config{}, fmt.Errorf("read project %s: %w", path, err)
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return project.Config{}, fmt.Errorf("read project %s: expected one JSON value", path)
	}
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate project %s: %w", path, err)
	}
	// The comparison has no incident outcome yet.
	if config.IncidentContract != "" {
		return project.Config{}, fmt.Errorf("read project %s: comparisons do not support incidentContract", path)
	}
	return config, nil
}

type demandArm struct {
	pattern, profile, band string
	flows                  []weightedDemandFlow
	daily                  *project.DailyProfile
	dailyStartMinute       int
}

type weightedDemandFlow struct {
	from, to   string
	cumulative float64
}

func demandArms(opts options, scenario scenario) ([]demandArm, error) {
	arms := make([]demandArm, 0, len(opts.patterns))
	for _, pattern := range opts.patterns {
		if pattern == "profile-daily" {
			arm, err := dailyArm(opts, scenario)
			if err != nil {
				return nil, err
			}
			arms = append(arms, arm)
			continue
		}
		if pattern == "rail-arrivals" || pattern == "rail-services" {
			if err := project.ValidateDemand(project.DemandConfig{Pattern: pattern, PerMinute: 12}, project.DemandContext{Network: scenario.network, RailArrivals: scenario.railArrivals, RailDepartures: scenario.railDepartures}); err != nil {
				return nil, fmt.Errorf("rail demand: %w", err)
			}
		}
		if pattern != "profile" {
			if opts.bandsText != "" {
				return nil, errors.New("bands require the profile demand pattern")
			}
			arms = append(arms, demandArm{pattern: pattern})
			continue
		}
		profile, err := selectedDemandProfile(scenario)
		if err != nil {
			return nil, err
		}
		bands, err := selectedDemandBands(profile, scenario.demand.Band, opts.bandsText)
		if err != nil {
			return nil, err
		}
		for _, band := range bands {
			flows := weightedProfileFlows(profile, band)
			arms = append(arms, demandArm{pattern: pattern, profile: profile.ID, band: profile.Bands[band].ID, flows: flows})
		}
	}
	return arms, nil
}

func selectedDemandProfile(scenario scenario) (project.DemandProfile, error) {
	profileID := scenario.demand.Profile
	if profileID == "" && len(scenario.demandProfiles) == 1 {
		profileID = scenario.demandProfiles[0].ID
	}
	for _, profile := range scenario.demandProfiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return project.DemandProfile{}, errors.New("profile demand requires a selected project demand profile")
}

func selectedDemandBands(profile project.DemandProfile, defaultBand, bandsText string) ([]int, error) {
	if bandsText == "" {
		bandsText = defaultBand
	}
	if strings.TrimSpace(bandsText) == "all" {
		bands := make([]int, len(profile.Bands))
		for index := range bands {
			bands[index] = index
		}
		return bands, nil
	}
	if strings.TrimSpace(bandsText) == "" {
		return nil, errors.New("profile demand requires a demand band")
	}
	parts := strings.Split(bandsText, ",")
	bands := make([]int, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if seen[id] {
			return nil, fmt.Errorf("demand band %q appears more than once", id)
		}
		found := -1
		for index, band := range profile.Bands {
			if band.ID == id {
				found = index
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("unknown demand band %q", id)
		}
		seen[id] = true
		bands = append(bands, found)
	}
	return bands, nil
}

func weightedProfileFlows(profile project.DemandProfile, band int) []weightedDemandFlow {
	flows := make([]weightedDemandFlow, 0, len(profile.Flows))
	total := 0.0
	for _, flow := range profile.Flows {
		weight := flow.Weights[band]
		if weight <= 0 {
			continue
		}
		total += weight
		flows = append(flows, weightedDemandFlow{from: flow.From, to: flow.To, cumulative: total})
	}
	return flows
}

type scheduleInput struct {
	seed                         int64
	durationTicks, intervalTicks int64
	burstSize                    int
	pattern, focus               string
	passengers                   []string
	profileFlows                 []weightedDemandFlow
	daily                        *project.DailyProfile
	railArrivals                 []project.RailArrival
	railDepartures               []project.RailDeparture
}

func demandSchedule(input scheduleInput) []scheduledRequest {
	if input.pattern == "profile-daily" {
		return dailyDemandSchedule(input)
	}
	if input.pattern == "rail-services" {
		return railServiceSchedule(input)
	}
	if input.pattern == "rail-arrivals" || input.pattern == "rail-services" {
		return railDemandSchedule(input)
	}
	if input.pattern == "profile" {
		return profileDemandSchedule(input)
	}
	rng := rand.New(rand.NewSource(input.seed))
	count := int((input.durationTicks - 1) / input.intervalTicks)
	requests := make([]scheduledRequest, 0, count)
	for i := range count {
		tick := int64(i+1) * input.intervalTicks
		if isBurstPattern(input.pattern) {
			tick = input.intervalTicks + int64(i/input.burstSize)*int64(input.burstSize)*input.intervalTicks
		}
		origin, destination := demandPair(rng, input)
		requests = append(requests, scheduledRequest{tick: tick, origin: origin, destination: destination})
	}
	return requests
}

func profileDemandSchedule(input scheduleInput) []scheduledRequest {
	count := int((input.durationTicks - 1) / input.intervalTicks)
	requests := make([]scheduledRequest, 0, count)
	seed := uint64(input.seed) // #nosec G115 -- Preserve the signed seed's bits for deterministic PCG input.
	rng := randv2.New(randv2.NewPCG(seed, ^seed))
	total := input.profileFlows[len(input.profileFlows)-1].cumulative
	for index := range count {
		target := rng.Float64() * total
		selected := sort.Search(len(input.profileFlows), func(flowIndex int) bool {
			return input.profileFlows[flowIndex].cumulative > target
		})
		flow := input.profileFlows[selected]
		requests = append(requests, scheduledRequest{
			tick: int64(index+1) * input.intervalTicks, origin: flow.from, destination: flow.to,
		})
	}
	return requests
}

func demandPair(rng *rand.Rand, input scheduleInput) (string, string) {
	switch input.pattern {
	case "destination":
		origins := without(input.passengers, input.focus)
		return origins[rng.Intn(len(origins))], input.focus
	case "hotspot", "bursty-hotspot":
		origins := make([]string, 0, 2*len(input.passengers)+4)
		for range 6 {
			origins = append(origins, input.focus)
		}
		for _, station := range input.passengers {
			if station != input.focus {
				origins = append(origins, station, station)
			}
		}
		origin := origins[rng.Intn(len(origins))]
		destinations := without(input.passengers, origin)
		return origin, destinations[rng.Intn(len(destinations))]
	case "hub-burst":
		destinations := without(input.passengers, input.focus)
		return input.focus, destinations[rng.Intn(len(destinations))]
	default:
		origin := input.passengers[rng.Intn(len(input.passengers))]
		destinations := without(input.passengers, origin)
		return origin, destinations[rng.Intn(len(destinations))]
	}
}

func isBurstPattern(pattern string) bool {
	return pattern == "bursty-hotspot" || pattern == "hub-burst"
}

func without(stations []string, excluded string) []string {
	result := make([]string, 0, len(stations)-1)
	for _, station := range stations {
		if station != excluded {
			result = append(result, station)
		}
	}
	return result
}

func scheduleID(schedule []scheduledRequest) string {
	hash := sha256.New()
	for _, request := range schedule {
		switch {
		case request.kind != "":
			_, _ = fmt.Fprintf(hash, "%s:%d:%q:%d:%q>%q:%d:%d\n", request.kind, request.tick, request.event, request.passenger, request.origin, request.destination, request.departureTick, request.walkingTicks)
		case request.event != "":
			_, _ = fmt.Fprintf(hash, "%d:%q:%d:%q>%q\n", request.tick, request.event, request.passenger, request.origin, request.destination)
		default:
			_, _ = fmt.Fprintf(hash, "%d:%s>%s\n", request.tick, request.origin, request.destination)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)[:8])
}

func demandWeights(pattern, band string, scenario scenario) (map[string]float64, error) {
	if pattern == "balanced" {
		return nil, nil
	}
	weights := make(map[string]float64, len(scenario.passengers))
	if pattern == "rail-arrivals" {
		for _, arrival := range scenario.railArrivals {
			weights[arrival.Station] += float64(arrival.Passengers)
		}
		return weights, nil
	}
	if pattern == "rail-services" {
		for _, offer := range project.RailServicesSchedule(scenario.railArrivals, scenario.railDepartures, scenario.demand.Seed) {
			weights[offer.From]++
		}
		return weights, nil
	}
	if pattern == "profile" {
		profile, err := selectedDemandProfile(scenario)
		if err != nil {
			return nil, err
		}
		bands, err := selectedDemandBands(profile, "", band)
		if err != nil {
			return nil, err
		}
		for _, flow := range profile.Flows {
			weights[flow.From] += flow.Weights[bands[0]]
		}
		return weights, nil
	}
	for _, station := range scenario.passengers {
		switch pattern {
		case "destination":
			if station != scenario.focus {
				weights[station] = 1
			}
		default:
			weights[station] = 2
			if station == scenario.focus {
				weights[station] = 6
			}
		}
	}
	return weights, nil
}
