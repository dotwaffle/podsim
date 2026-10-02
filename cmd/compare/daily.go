package main

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func validateDailyOptions(opts options, given map[string]bool) error {
	daily := slices.Contains(opts.patterns, "profile-daily")
	if given["daily-start-minute"] && (!daily || opts.dailyStartMinute < 0 || opts.dailyStartMinute >= 1440) {
		return errors.New("daily-start-minute requires profile-daily and a value from 0 to 1439")
	}
	if !daily {
		return nil
	}
	for _, name := range []string{"request-every", "loads", "burst-size", "adaptive-limit", "past-limit", "bands"} {
		if given[name] {
			return fmt.Errorf("daily demand does not accept -%s: timing and rates come from the profile", name)
		}
	}
	return nil
}

func dailyArm(opts options, scenario scenario) (demandArm, error) {
	profile, err := selectedDemandProfile(scenario)
	if err != nil {
		return demandArm{}, err
	}
	config := scenario.demand
	config.Pattern, config.Profile = "profile-daily", profile.ID
	if opts.dailyStartMinute >= 0 {
		config.DailyStartMinute = opts.dailyStartMinute
	}
	if validationErr := project.ValidateDemand(config, project.DemandContext{Network: scenario.network, Profiles: scenario.demandProfiles}); validationErr != nil {
		return demandArm{}, fmt.Errorf("daily demand: %w", validationErr)
	}
	daily, err := project.NewDailyProfile(config, scenario.demandProfiles)
	if err != nil {
		return demandArm{}, err
	}
	return demandArm{pattern: "profile-daily", profile: profile.ID, daily: daily, dailyStartMinute: config.DailyStartMinute}, nil
}

const ticksPerMinute = 60 * sim.TicksPerSecond

// Each complete minute has an integer number of offers and ends with zero
// budget. Jump between those offers without scanning every physics tick.
func dailyOfferTicks(daily *project.DailyProfile, end int64, offer func(int64, int)) int {
	count := 0
	for base := int64(0); base+1 < end; base += ticksPerMinute {
		band := daily.Band(base + 1)
		rate := int64(daily.Rate(band))
		if rate == 0 {
			continue
		}
		available := min(ticksPerMinute, end-1-base)
		volume := available * rate / ticksPerMinute
		count += int(volume)
		if offer == nil {
			continue
		}
		for passenger := int64(1); passenger <= volume; passenger++ {
			offer(base+(passenger*ticksPerMinute+rate-1)/rate, band)
		}
	}
	return count
}

func dailyDemandSchedule(input scheduleInput) []scheduledRequest {
	count := dailyOfferTicks(input.daily, input.durationTicks, nil)
	requests := make([]scheduledRequest, 0, count)
	seed := uint64(input.seed) // #nosec G115 -- Preserve the signed seed's bits for the session PCG stream.
	rng := rand.New(rand.NewPCG(seed, ^seed))
	dailyOfferTicks(input.daily, input.durationTicks, func(tick int64, band int) {
		from, to, _ := input.daily.Pair(band, rng.Float64())
		requests = append(requests, scheduledRequest{tick: tick, origin: from, destination: to})
	})
	return requests
}

func validateDailyMatrix(opts options, arms []demandArm, copiesPerSchedule int) error {
	var storage int64
	for _, arm := range arms {
		if arm.daily == nil {
			continue
		}
		offers := int64(dailyOfferTicks(arm.daily, durationTicks(opts.arrivalsFor), nil))
		copies := int64(copiesPerSchedule) * int64(len(opts.seeds)) * int64(len(opts.loads)) * int64(len(opts.redistributionPolicies))
		storage += offers * copies * 512
		if storage > 256<<20 {
			return errors.New("daily matrix exceeds its 256 MiB offer storage bound")
		}
	}
	return nil
}

// configureDailyPositioning disables positioning during gaps and after the
// guarded run exceeds its queue limit. It never changes the chosen policy.
func configureDailyPositioning(simulation *sim.Simulation, daily *project.DailyProfile, tick int64, mode sim.Positioning, skipped int) error {
	band := daily.Band(tick)
	if err := simulation.SetDemandWeights(daily.Weights(band)); err != nil {
		return err
	}
	if err := simulation.SetDemandRate(daily.Rate(band)); err != nil {
		return err
	}
	if daily.Rate(band) == 0 || mode == sim.PositioningGuarded && skipped > 0 {
		mode = sim.PositioningOff
	}
	return simulation.SetPositioning(mode)
}
