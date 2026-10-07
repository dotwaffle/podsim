package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

func parseExperimentalOptions(opts *options, given map[string]bool) error {
	if !given["pickup-reassignment"] {
		return nil
	}
	for part := range strings.SplitSeq(opts.pickupReassignmentText, ",") {
		policy := strings.TrimSpace(part)
		if policy != "off" && policy != "on" {
			return fmt.Errorf("pickup-reassignment must contain only off or on, got %q", policy)
		}
		if slices.Contains(opts.pickupReassignment, policy) {
			return fmt.Errorf("pickup-reassignment policy %q appears more than once", policy)
		}
		opts.pickupReassignment = append(opts.pickupReassignment, policy)
	}
	return nil
}

func experimentalArmCount(opts options) int {
	return max(1, len(opts.pickupReassignment))
}

// experimentalInputs expands each schedule across the pickup reassignment
// policies. An omitted option disables the policy, and its report field
// stays absent.
func experimentalInputs(inputs []runInput, opts options) []runInput {
	if opts.pickupReassignment == nil {
		return inputs
	}
	expanded := make([]runInput, 0, len(inputs)*experimentalArmCount(opts))
	for _, input := range inputs {
		for _, pickupPolicy := range opts.pickupReassignment {
			arm := input
			arm.pickupReassignment = pickupPolicy
			expanded = append(expanded, arm)
		}
	}
	return expanded
}

func configureExperimentalPolicies(simulation *sim.Simulation, input runInput) error {
	if policy := input.pickupReassignment; policy != "" && policy != "off" && policy != "on" {
		return fmt.Errorf("unknown experimental policy %q", policy)
	}
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
