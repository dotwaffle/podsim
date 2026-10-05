package sim

import "errors"

// IncidentContract selects the incident service transitions. Without it,
// no saved state or stream message has an incident member.
type IncidentContract string

// IncidentV1Contract permits the stage 1 incident members.
const IncidentV1Contract IncidentContract = "incident-v1"

// ErrUnknownIncidentContract means that the incident marker is not
// IncidentV1Contract.
var ErrUnknownIncidentContract = errors.New("unknown incident contract")

// ValidateIncidentContract accepts no marker and IncidentV1Contract.
func ValidateIncidentContract(contract IncidentContract) error {
	if contract != "" && contract != IncidentV1Contract {
		return ErrUnknownIncidentContract
	}
	return nil
}
