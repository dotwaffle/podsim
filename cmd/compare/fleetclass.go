package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The names of -fleet-classes. The plain arm runs the project as it is. The
// express arm runs the project of expressScenario.
const (
	plainFleetClass   = "plain"
	expressFleetClass = "express"
)

// parseFleetClassOptions parses -fleet-classes when the command line sets it,
// because the list adds report columns. It then refuses the options that
// cannot describe an Express arm. The built-in network is not a project, so it
// has no project validation. An energy file cannot hold an Express profile,
// and an Express pod never links, so a virtual platoon arm would be the off arm
// under another name.
func parseFleetClassOptions(opts *options, given map[string]bool) error {
	if !given["fleet-classes"] {
		return nil
	}
	for part := range strings.SplitSeq(opts.fleetClassesText, ",") {
		class := strings.TrimSpace(part)
		if class != plainFleetClass && class != expressFleetClass {
			return fmt.Errorf("unknown fleet class %q", class)
		}
		if slices.Contains(opts.fleetClasses, class) {
			return fmt.Errorf("fleet class %q appears more than once", class)
		}
		opts.fleetClasses = append(opts.fleetClasses, class)
	}
	if !slices.Contains(opts.fleetClasses, expressFleetClass) {
		return nil
	}
	if opts.projectPath == "" {
		return errors.New("fleet class express requires -project")
	}
	if opts.energyPath != "" {
		return errors.New("fleet class express does not support energy-file")
	}
	if slices.Contains(opts.platoonPolicies, "virtual") {
		return errors.New("fleet class express does not support platoon policy virtual")
	}
	return nil
}

func fleetClassArmCount(opts options) int { return max(1, len(opts.fleetClasses)) }

// fleetArm is one -fleet-classes name and the scenario of its arms.
type fleetArm struct {
	class    string
	scenario scenario
}

// fleetArms returns the arm of each -fleet-classes name, in list order. It
// returns nil when the option is not given. A project with an Express pod has
// no plain arm, because a plain run cannot start that pod.
func fleetArms(opts options, base scenario) ([]fleetArm, error) {
	arms := make([]fleetArm, 0, len(opts.fleetClasses))
	for _, class := range opts.fleetClasses {
		switch class {
		case plainFleetClass:
			if slices.ContainsFunc(base.fleet, func(pod sim.Placement) bool { return pod.Class == sim.ExpressClass }) {
				return nil, errors.New("fleet class plain needs a project with no Express pod")
			}
			arms = append(arms, fleetArm{class: class, scenario: base})
		case expressFleetClass:
			express, err := expressScenario(base)
			if err != nil {
				return nil, fmt.Errorf("fleet class express: %w", err)
			}
			arms = append(arms, fleetArm{class: class, scenario: express})
		default:
			return nil, fmt.Errorf("unknown fleet class %q", class)
		}
	}
	return arms, nil
}

// expressScenario returns base with all classes on each lane, station, and
// berth, Express pods in each placement, and the express-v1 order contract. The
// project validation of that contract decides whether Express can run on the
// project. The stations and the focus stay, so the schedules stay.
func expressScenario(base scenario) (scenario, error) {
	classes, err := sim.NewClassSet(string(sim.LegacyClass), string(sim.CompactClass), string(sim.GroupClass), string(sim.ExpressClass))
	if err != nil {
		return scenario{}, err
	}
	config := project.Clone(base.source)
	for index := range config.Network.Lanes {
		config.Network.Lanes[index].VehicleClasses = classes
	}
	for index := range config.Network.Stations {
		station := &config.Network.Stations[index]
		station.VehicleClasses = classes
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = classes
		}
	}
	for index := range config.Fleet {
		config.Fleet[index].Class = sim.ExpressClass
	}
	config.OrderContract = sim.ExpressOrderContract
	if invalid := project.Validate(config); invalid != nil {
		return scenario{}, invalid
	}
	express, err := newScenario(config, base.focus)
	if err != nil {
		return scenario{}, err
	}
	express.orderContract = sim.ExpressOrderContract
	return express, nil
}

// fleetInputs expands each input across the fleet arms. Each fleet arm keeps
// the schedule of its input. With no fleet arm, the inputs stay as they are.
func fleetInputs(inputs []runInput, fleets []fleetArm) []runInput {
	if len(fleets) == 0 {
		return inputs
	}
	expanded := make([]runInput, 0, len(inputs)*len(fleets))
	for _, input := range inputs {
		for _, fleet := range fleets {
			arm := input
			arm.fleetClass = fleet.class
			arm.scenario = fleet.scenario
			expanded = append(expanded, arm)
		}
	}
	return expanded
}

// plainPairKey holds every dimension of a result except the fleet class. A
// new dimension in compare must also go here. The schedule ID is part of the
// key, so a pair always has identical orders.
type plainPairKey struct {
	pattern, profile, band string
	requestEverySeconds    float64
	seed                   int64
	scheduleID             string
	sharingConsent         sim.SharingConsent
	sharingLimit           int
	sharingMode            string
	onboardPickups         string
	waitRule               string
	platoonPolicy          string
	policy                 string
	pickupReassignment     string
}

func plainPairKeyOf(outcome *result) plainPairKey {
	return plainPairKey{
		pattern: outcome.Pattern, profile: outcome.DemandProfile, band: outcome.DemandBand,
		requestEverySeconds: outcome.RequestEverySeconds, seed: outcome.Seed, scheduleID: outcome.ScheduleID,
		sharingConsent: outcome.SharingConsent, sharingLimit: outcome.SharedRidePartyLimit, sharingMode: outcome.SharingMode,
		onboardPickups: outcome.OnboardPickups, waitRule: outcome.WaitRule, platoonPolicy: outcome.PlatoonPolicy,
		policy: outcome.Policy, pickupReassignment: outcome.PickupReassignment,
	}
}

// addServedPercents gives each Express result the percent of the served orders
// of its plain pair. A result keeps no value when it has no plain pair, as
// after -adaptive-limit skips the plain arm, or when the pair served nothing.
func addServedPercents(results []result) {
	plainServed := make(map[plainPairKey]int)
	for index := range results {
		if results[index].FleetClass == plainFleetClass {
			plainServed[plainPairKeyOf(&results[index])] = results[index].Served
		}
	}
	for index := range results {
		outcome := &results[index]
		if outcome.FleetClass != expressFleetClass {
			continue
		}
		if served := plainServed[plainPairKeyOf(outcome)]; served > 0 {
			outcome.ServedPercentOfPlain = new(100 * float64(outcome.Served) / float64(served))
		}
	}
}

func servedPercentTableText(outcome result) string {
	if outcome.ServedPercentOfPlain == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *outcome.ServedPercentOfPlain)
}

func servedPercentCSVText(outcome result) string {
	if outcome.ServedPercentOfPlain == nil {
		return ""
	}
	return floatText(*outcome.ServedPercentOfPlain)
}
