package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

func parseOnboardOptions(opts *options, given bool) error {
	if !given {
		return nil
	}
	for part := range strings.SplitSeq(opts.onboardPickupsText, ",") {
		policy := strings.TrimSpace(part)
		if policy != "off" && policy != "on" {
			return fmt.Errorf("onboard-pickups must contain only off or on, got %q", policy)
		}
		if slices.Contains(opts.onboardPickups, policy) {
			return fmt.Errorf("onboard-pickups policy %q appears more than once", policy)
		}
		opts.onboardPickups = append(opts.onboardPickups, policy)
	}
	return validateOnboardOptions(*opts)
}

func validateOnboardOptions(opts options) error {
	for _, policy := range opts.onboardPickups {
		if policy != "off" && policy != "on" {
			return fmt.Errorf("unknown onboard pickup policy %q", policy)
		}
		if policy != "on" {
			continue
		}
		for _, arm := range sharingArms(opts) {
			if arm.limit <= 1 || arm.mode != sim.SharedRideDropOffs {
				return errors.New("onboard-pickups on requires sharing limits above one and drop-offs mode")
			}
		}
	}
	return nil
}

func onboardArmCount(opts options) int { return max(1, len(opts.onboardPickups)) }

func onboardInputs(inputs []runInput, opts options) []runInput {
	if opts.onboardPickups == nil {
		return inputs
	}
	expanded := make([]runInput, 0, len(inputs)*onboardArmCount(opts))
	for _, input := range inputs {
		for _, policy := range opts.onboardPickups {
			arm := input
			arm.onboardPickups = policy
			expanded = append(expanded, arm)
		}
	}
	return expanded
}

func configureOnboardPickups(simulation *sim.Simulation, policy string) error {
	if policy != "" && policy != "off" && policy != "on" {
		return fmt.Errorf("unknown onboard pickup policy %q", policy)
	}
	if err := simulation.SetOnboardPickups(policy == "on"); err != nil {
		return fmt.Errorf("set onboard pickups: %w", err)
	}
	return nil
}
