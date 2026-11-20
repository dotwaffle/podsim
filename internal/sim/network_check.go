package sim

// NetworkCheck holds the results of the checks of PrepareNetwork for one
// network, so that a caller that checks the same network more than one time
// runs each check one time. It runs a check when a method first needs it.
// No simulation reads a NetworkCheck. Only NewNetworkCheck makes one. Use it
// from one goroutine at a time.
type NetworkCheck struct {
	network Network
	tails   map[string]geometryTails
	// prepared and err hold the result of prepareNetworkTails, after
	// checked is true.
	prepared networkPreparation
	err      error
	checked  bool
	// banksErr holds the result of ValidateStationBanks, after banksChecked
	// is true.
	banksErr     error
	banksChecked bool
}

// NewNetworkCheck returns the checks of network, with the tails of an index
// of the same network, as ValidateNetworkClearance reads them. The zero
// index has no tails, and the checks then build them. The checks keep
// network and do not copy it, so the caller must not change it later.
func NewNetworkCheck(network Network, clearance LaneClearance) *NetworkCheck {
	return &NetworkCheck{network: network, tails: clearance.tails}
}

// preparation returns the result of prepareNetworkTails for the network.
func (c *NetworkCheck) preparation() (networkPreparation, error) {
	if !c.checked {
		c.prepared, c.err = prepareNetworkTails(c.network, c.tails)
		c.checked = true
	}
	return c.prepared, c.err
}

// Err returns the error of ValidateNetworkClearance for the network.
func (c *NetworkCheck) Err() error {
	prepared, err := c.preparation()
	if err != nil {
		return err
	}
	if prepared.reach != nil {
		return prepared.reach
	}
	return nil
}

// StationBanksErr returns the error of Network.ValidateStationBanks for the
// network.
func (c *NetworkCheck) StationBanksErr() error {
	if !c.banksChecked {
		c.banksErr, c.banksChecked = c.network.ValidateStationBanks(), true
	}
	return c.banksErr
}

// ValidateFleet returns the error of ValidateFleetWithOrderContract for the
// network and placements under contract.
func (c *NetworkCheck) ValidateFleet(placements []Placement, contract OrderContract) error {
	if contract != "" {
		if err := validateContractFleetBounds(c.network, placements, contract); err != nil {
			return err
		}
	}
	prepared, err := c.preparation()
	_, err = checkPreparedFleet(prepared, err, placements, contract)
	return err
}

// NewFleetWithOrderContract is the package function NewFleetWithOrderContract
// for the network of the check. It takes the preparation of the check, so
// that a caller that validated the network with the check prepares it one
// time. The refusals and their order are those of the package function.
// The simulation shares the indexes of the preparation, which do not change.
func (c *NetworkCheck) NewFleetWithOrderContract(placements []Placement, contract OrderContract) (*Simulation, error) {
	prepared, err := c.prepareFleet(placements, contract)
	if err != nil {
		return nil, err
	}
	s := newPreparedNetwork(prepared).newFleet(placements)
	s.orderContract = contract
	return s, nil
}

// RestoreState is the package function RestoreState for the network of the
// check. It always restores on that network and ignores input.Network, so
// the preparation of the check cannot meet another network. It takes that
// preparation, so that a caller that validated the network with the check
// prepares it one time. The refusals and their order are those of the
// package function.
func (c *NetworkCheck) RestoreState(input RestoreStateInput) (*Simulation, RestoreResult, error) {
	input.Network = c.network
	return restoreState(input, restoreFleets(input, func() (*PreparedNetwork, error) {
		prepared, err := c.prepareFleet(input.Fleet, input.OrderContract)
		if err != nil {
			return nil, err
		}
		return newPreparedNetwork(prepared), nil
	}))
}

// prepareFleet returns the preparation of the network for placements under
// contract, or the refusal of prepareContractNetwork for them, in the same
// order.
func (c *NetworkCheck) prepareFleet(placements []Placement, contract OrderContract) (networkPreparation, error) {
	if contract != "" {
		if err := validateContractFleetBounds(c.network, placements, contract); err != nil {
			return networkPreparation{}, err
		}
	}
	prepared, err := c.preparation()
	return checkPreparedFleet(prepared, err, placements, contract)
}
