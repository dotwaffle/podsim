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
