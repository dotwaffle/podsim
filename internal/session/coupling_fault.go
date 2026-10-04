package session

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The caller holds mu when checking a native publication boundary.
func (s *Session) couplingError() error {
	if err := s.simulation.CouplingError(); err != nil {
		return fmt.Errorf("physical coupling controller: %w", err)
	}
	if s.couplingViewError != nil {
		return fmt.Errorf("physical coupling observation: %w", s.couplingViewError)
	}
	return nil
}

// CouplingError reports a retained fault without publishing the failed tick.
func (s *Session) CouplingError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.couplingError()
}

// refreshCouplingObservation records a valid command or installation boundary.
// Ordinary projects retain no extra fleet snapshot. The caller holds mu.
func (s *Session) refreshCouplingObservation() {
	if s.project.Version != project.CouplingVersion {
		s.couplingObservation = nil
		return
	}
	if s.couplingError() == nil {
		s.stateWithoutNetwork()
	}
}

func cloneCouplingObservation(state State) State {
	state = ownAssemblerState(state)
	state.Simulation.Berths = slices.Clone(state.Simulation.Berths)
	state.Simulation.CouplingGroups = slices.Clone(state.Simulation.CouplingGroups)
	for i := range state.Simulation.CouplingGroups {
		group := &state.Simulation.CouplingGroups[i]
		if group.CommonSpeed != nil {
			group.CommonSpeed = new(*group.CommonSpeed)
		}
		if group.Connector != nil {
			group.Connector = new(*group.Connector)
		}
		if group.ManeuverEnvelope != nil {
			group.ManeuverEnvelope = new(*group.ManeuverEnvelope)
		}
	}
	for i := range state.Simulation.Vehicles {
		vehicle := &state.Simulation.Vehicles[i]
		vehicle.Route = cloneObservationLanes(vehicle.Route)
		if vehicle.Presentation != nil {
			route := *vehicle.Presentation
			route.Display = slices.Clone(route.Display)
			route.Lanes = slices.Clone(route.Lanes)
			route.Motion = cloneObservationLanes(route.Motion)
			vehicle.Presentation = &route
		}
	}
	return state
}

func cloneObservationLanes(lanes []sim.Lane) []sim.Lane {
	lanes = slices.Clone(lanes)
	for i := range lanes {
		if lanes[i].Control != nil {
			lanes[i].Control = new(*lanes[i].Control)
		}
	}
	return lanes
}

func (s *Session) lastCouplingObservation() State {
	if s.couplingObservation == nil {
		return State{}
	}
	return cloneCouplingObservation(*s.couplingObservation)
}

// A refused mechanical observation cannot become a successful save or report.
// Recovery commands install or reset the controller before clearing this error.
func (s *Session) retainCouplingViewError(err error) error {
	if s.simulation.CouplingError() == nil && s.couplingViewError == nil {
		s.couplingViewError = err
		s.simulation.SetPaused(true)
		s.logger.Error("Physical coupling observation failed", slog.Any("error", err), slog.Int64("tick", s.simulation.Tick()))
	}
	return s.couplingError()
}
