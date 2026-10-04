package project

import (
	"errors"

	"github.com/dotwaffle/podsim/internal/sim"
)

// HasCouplingContract reports whether config carries the physical coupling
// marker. The marker selects saved state 8 and stream hello 5.
func HasCouplingContract(config Config) bool {
	return config.CouplingContract != ""
}

// validateCouplingContract permits the coupling fields only with the
// compact-pair-v1 marker. The marker forbids no other project field.
func validateCouplingContract(config Config) error {
	if !HasCouplingContract(config) {
		if config.CouplingEnabled || config.CouplingSites != nil || config.CouplingCorridors != nil {
			return errors.New("coupling fields require couplingContract compact-pair-v1")
		}
		return nil
	}
	if config.CouplingContract != sim.CompactPairV1CouplingContract {
		return sim.ErrUnknownCouplingContract
	}
	return nil
}

func validateCouplingGeometry(config Config) error {
	if !HasCouplingContract(config) {
		return nil
	}
	return sim.ValidateCouplingGeometry(sim.CouplingGeometryInput{
		Contract:  config.CouplingContract,
		Network:   config.Network,
		Sites:     config.CouplingSites,
		Corridors: config.CouplingCorridors,
	})
}
