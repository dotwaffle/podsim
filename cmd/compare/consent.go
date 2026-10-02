package main

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// comparisonConsent defaults old inputs to private without reading arm policies.
func comparisonConsent(consent sim.SharingConsent) (sim.SharingConsent, error) {
	switch consent {
	case "", sim.PrivateConsent:
		return sim.PrivateConsent, nil
	case sim.SharedConsent:
		return consent, nil
	default:
		return "", fmt.Errorf("sharing-consent must be private or shared, got %q", consent)
	}
}
