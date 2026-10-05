package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
)

// errIncidentSerialUnmarked refuses a saved incident serial in a state
// whose project has no incident marker.
var errIncidentSerialUnmarked = errors.New("saved state without the incident marker contains an incident serial")

// savedIncidentSerial reports whether data has the member
// /simulation/incidentSerial, with any value. The typed decode reads an
// explicit 0 or null as no serial, so only this scan sees such a member.
func savedIncidentSerial(data []byte) (bool, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() == jsontext.KindString && kind == jsontext.KindBeginObject && length%2 == 1 &&
			decoder.StackDepth() == 2 && string(decoder.StackPointer()) == "/simulation/incidentSerial" {
			return true, nil
		}
	}
}

// checkIncidentSerial refuses an incident serial member, also 0 or null,
// in a saved state whose project has no incident marker.
func checkIncidentSerial(data []byte, file stateFile) error {
	if file.Project.IncidentContract != "" {
		return nil
	}
	present, err := savedIncidentSerial(data)
	if err != nil {
		return err
	}
	if present {
		return errIncidentSerialUnmarked
	}
	return nil
}
