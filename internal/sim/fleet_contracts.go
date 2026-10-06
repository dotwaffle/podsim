package sim

// FleetContracts selects independent order and incident contracts. The
// descriptors are copied before the simulation retains them.
type FleetContracts struct {
	OrderContract     OrderContract
	IncidentContract  IncidentContract
	FaultContract     FaultContract
	EmergencyContract EmergencyContract
}

// NewFleetWithContracts validates the contracts before it prepares geometry.
func NewFleetWithContracts(network Network, placements []Placement, contracts FleetContracts) (*Simulation, error) {
	if err := validateFleetContracts(contracts); err != nil {
		return nil, err
	}
	s, err := NewFleetWithOrderContract(network, placements, contracts.OrderContract)
	if err != nil {
		return nil, err
	}
	s.setFeatureContracts(contracts)
	return s, nil
}

// NewFleetWithContracts creates fresh state with this prepared network.
func (p *PreparedNetwork) NewFleetWithContracts(placements []Placement, contracts FleetContracts) (*Simulation, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	if err := validateFleetContracts(contracts); err != nil {
		return nil, err
	}
	s, err := p.NewFleetWithOrderContract(placements, contracts.OrderContract)
	if err != nil {
		return nil, err
	}
	s.setFeatureContracts(contracts)
	return s, nil
}

// setFeatureContracts gives s the incident, fault, and emergency markers
// of contracts. The frames copy them. The fault and emergency switches stay
// the gates of the operations.
func (s *Simulation) setFeatureContracts(contracts FleetContracts) {
	s.incidentContract, s.faultContract, s.emergencyContract = contracts.IncidentContract, contracts.FaultContract, contracts.EmergencyContract
}

func validateFleetContracts(contracts FleetContracts) error {
	if err := ValidateOrderContract(contracts.OrderContract); err != nil {
		return err
	}
	if err := ValidateIncidentContract(contracts.IncidentContract); err != nil {
		return err
	}
	if err := ValidateFaultContracts(contracts.FaultContract, contracts.IncidentContract); err != nil {
		return err
	}
	return ValidateEmergencyContracts(contracts.EmergencyContract, contracts.IncidentContract)
}

func (input RestoreStateInput) fleetContracts() FleetContracts {
	return FleetContracts{
		OrderContract: input.OrderContract, IncidentContract: input.IncidentContract,
		FaultContract: input.FaultContract, EmergencyContract: input.EmergencyContract,
	}
}
