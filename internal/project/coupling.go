package project

import (
	"errors"

	"github.com/dotwaffle/podsim/internal/sim"
)

// CouplingVersion identifies projects with an explicit physical coupling contract.
const CouplingVersion = 5

func validateCouplingVersion(config Config) error {
	if config.Version != CouplingVersion {
		if config.CouplingContract != "" || config.CouplingEnabled || config.CouplingSites != nil || config.CouplingCorridors != nil {
			return errors.New("coupling fields require project version 5")
		}
		return nil
	}
	if config.CouplingContract != sim.CompactPairV1CouplingContract {
		return sim.ErrUnknownCouplingContract
	}
	return nil
}

func validateCouplingGeometry(config Config) error {
	if config.Version != CouplingVersion {
		return nil
	}
	return sim.ValidateCouplingGeometry(sim.CouplingGeometryInput{
		Contract:  config.CouplingContract,
		Network:   config.Network,
		Sites:     config.CouplingSites,
		Corridors: config.CouplingCorridors,
	})
}
