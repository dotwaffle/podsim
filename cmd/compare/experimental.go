package main

import (
	"errors"
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
	if given["station-queue-spacing"] {
		for part := range strings.SplitSeq(opts.stationQueueSpacingText, ",") {
			policy := strings.TrimSpace(part)
			if policy != string(sim.StationQueueOrdinary) && policy != string(sim.StationQueueCompactV1) {
				return fmt.Errorf("unknown station queue spacing %q", policy)
			}
			if slices.Contains(opts.stationQueueSpacing, policy) {
				return fmt.Errorf("station queue spacing %q appears more than once", policy)
			}
			opts.stationQueueSpacing = append(opts.stationQueueSpacing, policy)
		}
	}
	return nil
}

func experimentalArmCount(opts options) int {
	return max(1, len(opts.stationBuffers)) * max(1, len(opts.pickupReassignment)) * max(1, len(opts.stationQueueSpacing))
}

// experimentalInputs expands each schedule across the independent policies.
// An omitted option keeps ordinary spacing or disables the other policies.
// Its report field stays absent.
func experimentalInputs(inputs []runInput, opts options) []runInput {
	if opts.stationBuffers == nil && opts.pickupReassignment == nil && opts.stationQueueSpacing == nil {
		return inputs
	}
	buffers, reassignment := opts.stationBuffers, opts.pickupReassignment
	if buffers == nil {
		buffers = []string{""}
	}
	if reassignment == nil {
		reassignment = []string{""}
	}
	spacing := opts.stationQueueSpacing
	if spacing == nil {
		spacing = []string{""}
	}
	expanded := make([]runInput, 0, len(inputs)*experimentalArmCount(opts))
	for _, input := range inputs {
		for _, bufferPolicy := range buffers {
			for _, pickupPolicy := range reassignment {
				for _, queueSpacing := range spacing {
					arm := input
					arm.stationBuffers, arm.pickupReassignment = bufferPolicy, pickupPolicy
					arm.stationQueueSpacing = queueSpacing
					expanded = append(expanded, arm)
				}
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
	spacing := sim.StationQueueSpacing(input.stationQueueSpacing)
	if spacing == "" {
		spacing = sim.StationQueueOrdinary
	}
	if spacing == sim.StationQueueCompactV1 && (input.stationBuffers != "on" || input.platoonPolicy != "virtual" || simulation.Platooning() != sim.PlatooningVirtual || simulation.PlatoonLimit() < 2 || simulation.PlatoonLimit() > 4) {
		return errors.New("compact-v1 requires station buffers on and virtual platoons with a limit from 2 to 4")
	}
	simulation.SetStationBuffers(input.stationBuffers == "on")
	simulation.SetPickupSwaps(input.pickupReassignment == "on")
	if err := simulation.SetStationQueueSpacing(spacing); err != nil {
		return fmt.Errorf("set station queue spacing: %w", err)
	}
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
