package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

func parseExperimentalOptions(opts *options, _ map[string]bool) error {
	var err error
	opts.pickupReassignment, err = parseOffOn("pickup-reassignment", opts.pickupReassignmentText)
	return err
}

// parseOffOn parses the comma-separated off and on policies of the flag
// name, in order. It refuses another word and a repeated policy.
func parseOffOn(name, text string) ([]string, error) {
	var policies []string
	for part := range strings.SplitSeq(text, ",") {
		policy := strings.TrimSpace(part)
		if policy != "off" && policy != "on" {
			return nil, fmt.Errorf("%s must contain only off or on, got %q", name, policy)
		}
		if slices.Contains(policies, policy) {
			return nil, fmt.Errorf("%s policy %q appears more than once", name, policy)
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func experimentalArmCount(opts options) int {
	return max(1, len(opts.pickupReassignment))
}

// experimentalInputs expands each schedule across the pickup reassignment
// policies. The default policy runs without a table or CSV column.
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
	ScannedPairs          int     `json:"scanned_pairs"`
	RoutePairs            int     `json:"route_pairs"`
	Swaps                 int     `json:"swaps"`
	Transfers             int     `json:"transfers"`
	AssignmentChecks      int     `json:"assignment_checks"`
	IneligiblePairs       int     `json:"ineligible_pairs"`
	CooldownPairs         int     `json:"cooldown_pairs"`
	SameOriginPairs       int     `json:"same_origin_pairs"`
	RouteFailures         int     `json:"route_failures"`
	NoBenefitPairs        int     `json:"no_benefit_pairs"`
	PredictedSecondsSaved float64 `json:"predicted_seconds_saved"`
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
		NoBenefitPairs:        stats.NoBenefitPairs,
		PredictedSecondsSaved: stats.PredictedSecondsSaved,
	}
}
