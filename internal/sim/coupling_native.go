package sim

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
