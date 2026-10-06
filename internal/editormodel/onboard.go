package editormodel

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

func onboardSettingError(value any) string {
	if !has(value, "onboardPickups") {
		return ""
	}
	enabled, ok := member(value, "onboardPickups").(bool)
	if !ok {
		return "The onboard pickup setting must be true or false."
	}
	if !enabled {
		return ""
	}
	limit := member(value, "sharedRidePartyLimit")
	mode, validMode := member(value, "sharedRideMode").(string)
	if !has(value, "sharedRideMode") {
		validMode = true
	}
	if !integer(limit) || number(limit) < 2 || number(limit) > sim.MaxSharedRideParties || !validMode || mode != "" && mode != "drop-offs" {
		return fmt.Sprintf("Onboard pickups require drop-offs sharing and a party limit from 2 to %d.", sim.MaxSharedRideParties)
	}
	return ""
}
