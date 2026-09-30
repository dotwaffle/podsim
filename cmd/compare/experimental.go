package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

func parseExperimentalOptions(opts *options, given map[string]bool) error {
	for _, option := range []struct {
		name, value string
		policies    *[]string
	}{
		{"station-buffers", opts.stationBuffersText, &opts.stationBuffers},
		{"pickup-reassignment", opts.pickupReassignmentText, &opts.pickupReassignment},
	} {
		if !given[option.name] {
			continue
		}
		for part := range strings.SplitSeq(option.value, ",") {
			policy := strings.TrimSpace(part)
			if policy != "off" && policy != "on" {
				return fmt.Errorf("%s must contain only off or on, got %q", option.name, policy)
			}
			if slices.Contains(*option.policies, policy) {
				return fmt.Errorf("%s policy %q appears more than once", option.name, policy)
			}
			*option.policies = append(*option.policies, policy)
		}
	}
	return nil
}

func experimentalArmCount(opts options) int {
	return max(1, len(opts.stationBuffers)) * max(1, len(opts.pickupReassignment))
}

// experimentalInputs expands each schedule across the independent policies.
// An omitted option keeps the policy off and leaves its report field absent.
func experimentalInputs(inputs []runInput, opts options) []runInput {
	if opts.stationBuffers == nil && opts.pickupReassignment == nil {
		return inputs
	}
	buffers, reassignment := opts.stationBuffers, opts.pickupReassignment
	if buffers == nil {
		buffers = []string{""}
	}
	if reassignment == nil {
		reassignment = []string{""}
	}
	expanded := make([]runInput, 0, len(inputs)*experimentalArmCount(opts))
	for _, input := range inputs {
		for _, bufferPolicy := range buffers {
			for _, pickupPolicy := range reassignment {
				arm := input
				arm.stationBuffers, arm.pickupReassignment = bufferPolicy, pickupPolicy
				expanded = append(expanded, arm)
			}
		}
	}
	return expanded
}

func configureExperimentalPolicies(simulation *sim.Simulation, input runInput) error {
	for _, policy := range []string{input.stationBuffers, input.pickupReassignment} {
		if policy != "" && policy != "off" && policy != "on" {
			return fmt.Errorf("unknown experimental policy %q", policy)
		}
	}
	simulation.SetStationBuffers(input.stationBuffers == "on")
	simulation.SetPickupSwaps(input.pickupReassignment == "on")
	return nil
}

type pickupPolicyStats struct {
	ScannedPairs            int     `json:"scanned_pairs"`
	RoutePairs              int     `json:"route_pairs"`
	Swaps                   int     `json:"swaps"`
	Transfers               int     `json:"transfers"`
	AssignmentChecks        int     `json:"assignment_checks"`
	IneligiblePairs         int     `json:"ineligible_pairs"`
	CooldownPairs           int     `json:"cooldown_pairs"`
	SameOriginPairs         int     `json:"same_origin_pairs"`
	RouteFailures           int     `json:"route_failures"`
	NoBenefitPairs          int     `json:"no_benefit_pairs"`
	UnsupportedPolicyChecks int     `json:"unsupported_policy_checks"`
	PredictedSecondsSaved   float64 `json:"predicted_seconds_saved"`
}

func pickupStatsForReport(simulation *sim.Simulation, policy string) *pickupPolicyStats {
	if policy == "" {
		return nil
	}
	stats := simulation.PickupSwapStats()
	return &pickupPolicyStats{
		ScannedPairs: stats.ScannedPairs, RoutePairs: stats.RoutePairs,
		Swaps: stats.Swaps, Transfers: stats.Transfers, AssignmentChecks: stats.AssignmentChecks,
		IneligiblePairs: stats.IneligiblePairs, CooldownPairs: stats.CooldownPairs,
		SameOriginPairs: stats.SameOriginPairs, RouteFailures: stats.RouteFailures,
		NoBenefitPairs: stats.NoBenefitPairs, UnsupportedPolicyChecks: stats.UnsupportedPolicyChecks,
		PredictedSecondsSaved: stats.PredictedSecondsSaved,
	}
}
