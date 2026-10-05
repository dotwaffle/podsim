package sim

import (
	"errors"
	"strconv"
)

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

// SetIncidentGeneration sets the generation of the session. Each incident
// record that the simulation makes after the call gets an ID with this
// generation. The session calls it after each change of its generation:
// each project apply that makes a new fleet, each reset, demo, rewind and
// restart. It does not reset the serial.
func (s *Simulation) SetIncidentGeneration(generation uint64) {
	s.incidentGeneration = generation
}

// nextIncidentID returns a new incident record ID, i<generation>.<serial>.
// The serial only increases, so an ID is unique in its generation. A
// rewind restores the serial of the save point, and the session gives the
// rewound simulation a new generation, so a record of the abandoned
// timeline keeps an ID that no new record gets. Event order uses the
// serial only, so a run does not depend on the generation.
func (s *Simulation) nextIncidentID() string {
	s.incidentSerial++
	return "i" + strconv.FormatUint(s.incidentGeneration, 10) + "." + strconv.FormatUint(s.incidentSerial, 10)
}
