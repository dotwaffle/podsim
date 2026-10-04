package main

import (
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// validateStationQueueOptions checks every compact arm before schedules exist.
func validateStationQueueOptions(opts options) error {
	for _, spacing := range opts.stationQueueSpacing {
		if spacing != string(sim.StationQueueOrdinary) && spacing != string(sim.StationQueueCompactV1) {
			return fmt.Errorf("unknown station queue spacing %q", spacing)
		}
		if spacing != string(sim.StationQueueCompactV1) {
			continue
		}
		if len(opts.stationBuffers) != 1 || opts.stationBuffers[0] != "on" {
			return errors.New("compact-v1 requires station-buffers on for every arm")
		}
		if len(opts.platoonPolicies) != 1 || opts.platoonPolicies[0] != "virtual" {
			return errors.New("compact-v1 requires platoon-policies virtual for every arm")
		}
	}
	return nil
}

type comparisonStepper interface {
	Step()
	CompactQueueError() error
	CouplingError() error
}

// stepComparison reports a retained fault before the run reads the stopped tick.
func stepComparison(simulation comparisonStepper) error {
	if err := simulation.CouplingError(); err != nil {
		return fmt.Errorf("physical coupling controller: %w", err)
	}
	simulation.Step()
	if err := simulation.CouplingError(); err != nil {
		return fmt.Errorf("physical coupling controller: %w", err)
	}
	if err := simulation.CompactQueueError(); err != nil {
		return fmt.Errorf("compact station queue controller: %w", err)
	}
	return nil
}
