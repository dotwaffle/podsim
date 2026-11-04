package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentProject returns the project members that the incident samplers of
// an arm read. The demand seed is the seed of the arm, as for the offered
// schedule, so arms with one seed get one event schedule.
func incidentProject(scenario scenario, seed int64) project.Config {
	config := project.Config{Faults: scenario.faults, Emergencies: scenario.emergencies}
	config.Demand.Seed = uint64(seed) // #nosec G115 -- Match the offered schedule.
	return config
}

// hasIncidentRate reports whether the project has a fault rate or an emergency
// rate above 0. No rate leaves the operations off and the report columns out.
func hasIncidentRate(scenario scenario) bool {
	rates := project.EffectiveIncidentRates(incidentProject(scenario, 0))
	return rates.Faults.PerHour > 0 || rates.EmergenciesPerHour > 0
}

// configureIncidents turns both operations on and starts the samplers of an
// arm whose project has a rate. Any other arm keeps its operations off.
func configureIncidents(simulation *sim.Simulation, scenario scenario, seed int64) error {
	if !hasIncidentRate(scenario) {
		return nil
	}
	config := incidentProject(scenario, seed)
	if err := simulation.SetFaults(true, project.EffectiveFaultSettings(config)); err != nil {
		return fmt.Errorf("turn faults on: %w", err)
	}
	if err := simulation.SetEmergencies(true); err != nil {
		return fmt.Errorf("turn emergencies on: %w", err)
	}
	if err := project.ConfigureIncidentSampling(simulation, config); err != nil {
		return fmt.Errorf("start incident sampling: %w", err)
	}
	return nil
}

// outstanding counts the accepted orders with no terminal state. A completed
// order and an interrupted order are terminal. Only a completed order is
// served.
func outstanding(state sim.Snapshot) int {
	return state.Submitted - state.Completed - state.Interrupted
}

// incidentColumns holds the incident columns of an arm whose project has a
// rate.
type incidentColumns struct {
	Interrupted      int   `json:"interrupted"`
	FaultStarted     int64 `json:"fault_started"`
	FaultSkipped     int64 `json:"fault_skipped"`
	EmergencyStarted int64 `json:"emergency_started"`
	EmergencySkipped int64 `json:"emergency_skipped"`
}

// incidentReport returns the incident columns of the state, or nil when the
// project has no rate.
func incidentReport(scenario scenario, state sim.Snapshot) *incidentColumns {
	if !hasIncidentRate(scenario) {
		return nil
	}
	faults, emergencies := state.Faults.Counters, state.Emergencies.Counters
	return &incidentColumns{
		Interrupted:  state.Interrupted,
		FaultStarted: faults.Started, FaultSkipped: faults.Skipped,
		EmergencyStarted: emergencies.Started, EmergencySkipped: emergencies.Skipped,
	}
}

var incidentCSVHeader = []string{"interrupted", "fault_started", "fault_skipped", "emergency_started", "emergency_skipped"}

// incidentCSVRow returns the cells of incidentCSVHeader. A result with no
// columns has empty cells.
func incidentCSVRow(columns *incidentColumns) []string {
	if columns == nil {
		return make([]string, len(incidentCSVHeader))
	}
	return []string{
		strconv.Itoa(columns.Interrupted),
		strconv.FormatInt(columns.FaultStarted, 10), strconv.FormatInt(columns.FaultSkipped, 10),
		strconv.FormatInt(columns.EmergencyStarted, 10), strconv.FormatInt(columns.EmergencySkipped, 10),
	}
}

// hasIncidentColumns reports whether a report has the incident columns.
func hasIncidentColumns(results []result) bool {
	return slices.ContainsFunc(results, func(outcome result) bool { return outcome.incidentColumns != nil })
}

// incidentTableHeader returns the end of the table header: nothing for a
// report with no incident columns.
func incidentTableHeader(results []result) string {
	if !hasIncidentColumns(results) {
		return ""
	}
	return "\t" + strings.ToUpper(strings.ReplaceAll(strings.Join(incidentCSVHeader, "\t"), "_", " "))
}

// incidentTableCells returns the end of the table row of outcome: nothing for
// a report with no incident columns.
func incidentTableCells(results []result, outcome result) string {
	if !hasIncidentColumns(results) {
		return ""
	}
	return "\t" + strings.Join(incidentCSVRow(outcome.incidentColumns), "\t")
}
