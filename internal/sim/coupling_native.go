package sim

import "errors"

// FleetContracts selects independent order, physical coupling and incident
// contracts. The descriptors are copied before the simulation retains them.
type FleetContracts struct {
	OrderContract     OrderContract
	CouplingContract  CouplingContract
	IncidentContract  IncidentContract
	FaultContract     FaultContract
	CouplingEnabled   bool
	CouplingSites     []CouplingSite
	CouplingCorridors []CouplingCorridor
}

// NewFleetWithContracts validates the contracts before it prepares geometry.
func NewFleetWithContracts(network Network, placements []Placement, contracts FleetContracts) (*Simulation, error) {
	if err := validateFleetContracts(network, placements, contracts); err != nil {
		return nil, err
	}
	if contracts.CouplingContract == "" {
		s, err := NewFleetWithOrderContract(network, placements, contracts.OrderContract)
		if err != nil {
			return nil, err
		}
		s.incidentContract, s.faultContract = contracts.IncidentContract, contracts.FaultContract
		return s, nil
	}
	p, err := PrepareNetwork(network)
	if err != nil {
		return nil, err
	}
	return p.NewFleetWithContracts(placements, contracts)
}

// NewFleetWithContracts creates fresh state with this prepared network.
func (p *PreparedNetwork) NewFleetWithContracts(placements []Placement, contracts FleetContracts) (*Simulation, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	if err := validateFleetContracts(p.network, placements, contracts); err != nil {
		return nil, err
	}
	if contracts.CouplingContract == "" {
		s, err := p.NewFleetWithOrderContract(placements, contracts.OrderContract)
		if err != nil {
			return nil, err
		}
		s.incidentContract, s.faultContract = contracts.IncidentContract, contracts.FaultContract
		return s, nil
	}
	if err := validatePlacementsWithOrderContract(p.network, placements, contracts.OrderContract); err != nil {
		return nil, err
	}
	n, err := prepareCouplingReservations(p, CouplingGeometryInput{
		Contract: contracts.CouplingContract, Network: p.network,
		Sites: contracts.CouplingSites, Corridors: contracts.CouplingCorridors,
	})
	if err != nil {
		return nil, err
	}
	s := p.newFleet(placements)
	s.orderContract = contracts.OrderContract
	s.incidentContract, s.faultContract = contracts.IncidentContract, contracts.FaultContract
	s.couplingNetwork = n
	s.couplingEnabled = contracts.CouplingEnabled
	return s, nil
}

func validateFleetContracts(network Network, placements []Placement, contracts FleetContracts) error {
	if err := ValidateOrderContract(contracts.OrderContract); err != nil {
		return err
	}
	if err := ValidateIncidentContract(contracts.IncidentContract); err != nil {
		return err
	}
	if err := ValidateFaultContracts(contracts.FaultContract, contracts.IncidentContract); err != nil {
		return err
	}
	if contracts.CouplingContract == "" {
		if contracts.CouplingEnabled || contracts.CouplingSites != nil || contracts.CouplingCorridors != nil {
			return errors.New("coupling settings require a coupling contract")
		}
		return nil
	}
	if _, ok := LookupCouplingProfile(contracts.CouplingContract); !ok {
		return ErrUnknownCouplingContract
	}
	// The coupling marker supplies bounds, not Express class or order semantics.
	if len(placements) < 1 || len(placements) > expressMaxPods ||
		len(network.Nodes) < 1 || len(network.Nodes) > expressMaxNodes ||
		len(network.Lanes) < 1 || len(network.Lanes) > expressMaxLanes ||
		len(network.Stations) < 2 || len(network.Stations) > expressMaxStations {
		return errors.New("the coupling fleet or network exceeds project bounds")
	}
	for _, placement := range placements {
		if !boundedContractID(placement.ID) || !boundedContractID(placement.StationID) ||
			placement.BerthID != "" && !boundedContractID(placement.BerthID) {
			return errors.New("the coupling placement ID exceeds bounds")
		}
	}
	if err := validateContractNetworkRecords(network); err != nil {
		return err
	}
	if err := validateContractGeometryBudget(network); err != nil {
		return err
	}
	return ValidateCouplingGeometry(CouplingGeometryInput{
		Contract: contracts.CouplingContract, Network: network,
		Sites: contracts.CouplingSites, Corridors: contracts.CouplingCorridors,
	})
}

// CouplingContract returns the simulation's immutable physical contract.
func (s *Simulation) CouplingContract() CouplingContract {
	if s.couplingNetwork == nil {
		return ""
	}
	return s.couplingNetwork.contract
}

// CouplingEnabled reports whether the policy permits new pair recruitment.
func (s *Simulation) CouplingEnabled() bool { return s.couplingEnabled }

// CouplingError reports an approach or committed motion invariant failure.
// A failure prevents the publication of the unproved tick.
func (s *Simulation) CouplingError() error { return s.couplingFault }

// SetCouplingEnabled stops new recruitment without changing committed groups.
func (s *Simulation) SetCouplingEnabled(enabled bool) error {
	if enabled && s.CouplingContract() == "" {
		return ErrUnknownCouplingContract
	}
	s.couplingEnabled = enabled
	return nil
}
