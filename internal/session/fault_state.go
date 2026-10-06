package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The bounds of the fault records (incident suspension contract, section
// 13.5): one record for each pod and 64 debris records. A pod tuple has 6
// numbers, and a debris tuple 8.
const (
	maxDebrisRecords  = 64
	maxFaultRecords   = project.MaxPods + maxDebrisRecords
	podTupleLength    = 6
	debrisTupleLength = 8
)

// The kinds of a saved fault tuple.
const (
	savedPodFault    = 0
	savedDebrisFault = 1
)

// errFaultMemberUnmarked refuses a stage 2 member in a saved state whose
// project has no fault marker.
var errFaultMemberUnmarked = errors.New("saved state without the fault marker contains a fault member")

// savedFaultPath is the path of the stage 2 member of a save. Each member
// below it is also a stage 2 member.
const savedFaultPath = "/simulation/faults"

// scanSavedFaultMembers reads the tokens of a saved state. Without the
// fault marker, it refuses the stage 2 member with any value, also null or
// an empty object: the typed decode reads such a value as no member, so
// only this scan sees it. With the marker, it refuses null for the member
// and for each member below it, which no encoder writes.
func scanSavedFaultMembers(data []byte, marked bool) error {
	return scanSavedMarkedMember(data, savedFaultPath, marked, errFaultMemberUnmarked)
}

// scanSavedMarkedMember reads the tokens of a saved state for the member
// at the path member, which needs a marker. Without the marker, it refuses
// the member with any value, with the error unmarked. With the marker, it
// refuses null for the member and for each member below it.
func scanSavedMarkedMember(data []byte, member string, marked bool, unmarked error) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		if path := arrayPath(decoder); path != member && !strings.HasPrefix(path, member+"/") {
			continue
		}
		if !marked {
			return fmt.Errorf("%w: %s", unmarked, decoder.StackPointer())
		}
		if decoder.PeekKind() == 'n' {
			return fmt.Errorf("saved member %s is null", decoder.StackPointer())
		}
	}
}

// checkFaultMembers checks the raw stage 2 members of a decoded saved
// state against the fault marker of its project.
func checkFaultMembers(data []byte, file stateFile) error {
	return scanSavedFaultMembers(data, file.Project.FaultContract != "")
}

// validateFaultValues refuses the fault records and counters of a state
// file whose project has no fault marker, and an empty faults member,
// which the save omits. The encoder calls it.
func (file *stateFile) validateFaultValues() error {
	faults := file.Simulation.Faults
	switch {
	case faults == nil:
		return nil
	case file.Project.FaultContract == "":
		return errFaultMemberUnmarked
	case len(faults.Records) == 0 && faults.Counters == (sim.FaultCounters{}):
		return errors.New("saved faults member is empty")
	}
	return nil
}

// encodeSavedFault writes a saved fault record as one tuple: [generation,
// serial, start, end, 0, pod] for a pod fault, and [generation, serial,
// start, end, 1, lane, from, to] for debris.
func encodeSavedFault(encoder *jsontext.Encoder, fault sim.SavedFault) error {
	values := []any{fault.Generation, fault.Serial, fault.Start, fault.End, savedPodFault, fault.Pod}
	if fault.Debris {
		values = []any{fault.Generation, fault.Serial, fault.Start, fault.End, savedDebrisFault, fault.Lane, fault.From, fault.To}
	}
	return json.MarshalEncode(encoder, values)
}

// decodeSavedFault reads one saved fault tuple. Each element is a number,
// the kind is 0 or 1, and the length matches the kind. The restore checks
// the values. A value that is not an array fails the decode into numbers,
// and null gives no numbers.
func decodeSavedFault(decoder *jsontext.Decoder, fault *sim.SavedFault) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var numbers []jsontext.Value
	if err := json.Unmarshal(value, &numbers); err != nil {
		return err
	}
	for _, number := range numbers {
		if number.Kind() != jsontext.KindNumber {
			return errors.New("saved fault tuple contains a nonnumber")
		}
	}
	if len(numbers) != podTupleLength && len(numbers) != debrisTupleLength {
		return fmt.Errorf("saved fault tuple has %d numbers", len(numbers))
	}
	var kind uint8
	if err := json.Unmarshal(numbers[4], &kind); err != nil {
		return err
	}
	var next sim.SavedFault
	targets := []any{&next.Generation, &next.Serial, &next.Start, &next.End, &kind}
	switch {
	case kind == savedPodFault && len(numbers) == podTupleLength:
		targets = append(targets, &next.Pod)
	case kind == savedDebrisFault && len(numbers) == debrisTupleLength:
		next.Debris = true
		targets = append(targets, &next.Lane, &next.From, &next.To)
	default:
		return fmt.Errorf("saved fault tuple has %d numbers for the kind %d", len(numbers), kind)
	}
	for index, target := range targets {
		if err := json.Unmarshal(numbers[index], target); err != nil {
			return err
		}
	}
	*fault = next
	return nil
}
