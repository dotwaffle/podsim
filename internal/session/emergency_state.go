package session

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// emergencyTupleLength is the number of elements of a saved emergency
// tuple: [generation, serial, start, pod, order] (incident emergency
// contract, section 11.3).
const emergencyTupleLength = 5

// errEmergencyMemberUnmarked refuses a stage 3 member in a saved state
// whose project has no emergency marker.
var errEmergencyMemberUnmarked = errors.New("saved state without the emergency marker contains an emergency member")

// savedEmergencyPath is the path of the stage 3 member of a save. Each
// member below it is also a stage 3 member.
const savedEmergencyPath = "/simulation/emergencies"

// scanSavedEmergencyMembers reads the tokens of a saved state. Without the
// emergency marker, it refuses the stage 3 member with any value, also
// null, an empty object, or an empty array: the typed decode reads such a
// value as no member, so only this scan sees it. With the marker, it
// refuses null for the member and for each member below it, which no
// encoder writes.
func scanSavedEmergencyMembers(data []byte, marked bool) error {
	return scanSavedMarkedMember(data, savedEmergencyPath, marked, errEmergencyMemberUnmarked)
}

// checkEmergencyMembers checks the raw stage 3 members of a decoded saved
// state against the emergency marker of its project.
func checkEmergencyMembers(data []byte, file stateFile) error {
	return scanSavedEmergencyMembers(data, file.Project.EmergencyContract != "")
}

// validateEmergencyValues refuses the emergency records and counters of a
// state file whose project has no emergency marker, and an empty
// emergencies member, which the save omits. The encoder calls it.
func (file *stateFile) validateEmergencyValues() error {
	emergencies := file.Simulation.Emergencies
	switch {
	case emergencies == nil:
		return nil
	case file.Project.EmergencyContract == "":
		return errEmergencyMemberUnmarked
	case len(emergencies.Records) == 0 && emergencies.Counters == (sim.EmergencyCounters{}):
		return errors.New("saved emergencies member is empty")
	}
	return nil
}

// encodeSavedEmergency writes a saved emergency record as one tuple:
// [generation, serial, start, pod, order]. pod is an index into the saved
// pods, and order is the order ID of the party.
func encodeSavedEmergency(encoder *jsontext.Encoder, record sim.SavedEmergency) error {
	return json.MarshalEncode(encoder, []any{record.Generation, record.Serial, record.Start, record.Pod, record.Order})
}

// decodeSavedEmergency reads one saved emergency tuple. It has exactly 5
// elements, and each element is a number that fits its field. The restore
// checks the values. A value that is not an array fails the decode into
// numbers, and null gives no numbers.
func decodeSavedEmergency(decoder *jsontext.Decoder, record *sim.SavedEmergency) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var numbers []jsontext.Value
	if err := json.Unmarshal(value, &numbers); err != nil {
		return err
	}
	if len(numbers) != emergencyTupleLength {
		return fmt.Errorf("saved emergency tuple has %d elements", len(numbers))
	}
	var next sim.SavedEmergency
	for index, target := range []any{&next.Generation, &next.Serial, &next.Start, &next.Pod, &next.Order} {
		if numbers[index].Kind() != jsontext.KindNumber {
			return errors.New("saved emergency tuple contains a nonnumber")
		}
		if err := json.Unmarshal(numbers[index], target); err != nil {
			return err
		}
	}
	*record = next
	return nil
}
