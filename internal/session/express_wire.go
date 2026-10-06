package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// EncodeStreamJSON encodes a publication with packed order text. The
// envelope markers must agree with the frame.
func EncodeStreamJSON(e StreamEnvelope) ([]byte, error) {
	if err := sim.ValidateOrderContract(e.OrderContract); err != nil {
		return nil, err
	}
	if err := validateEncodedContract(e); err != nil {
		return nil, err
	}
	data, err := jsonv2.Marshal(e, json.DefaultOptionsV1(), packedRequestOptions())
	if err == nil && len(data) > MaxStreamJSON {
		err = errors.New("state JSON exceeds supported limit")
	}
	return data, err
}

// validateEncodedContract binds the envelope markers to the frame before
// the publisher writes them.
func validateEncodedContract(e StreamEnvelope) error {
	if e.Full != nil {
		if err := sim.ValidateIncidentContract(e.Full.State.Simulation.IncidentContract); err != nil {
			return err
		}
		if err := checkIncidentFrame(e.Full.State.Simulation); err != nil {
			return err
		}
		if err := checkFaultFrame(e.Full.State.Simulation); err != nil {
			return err
		}
		if err := checkEmergencyFrame(e.Full.State.Simulation); err != nil {
			return err
		}
		if e.OrderContract != e.Full.State.Simulation.OrderContract {
			return errors.New("publication order contract mismatch")
		}
	}
	return nil
}

func validateEnvelopeContract(e StreamEnvelope, previous StreamFrame) error {
	contract := previous.State.Simulation.OrderContract
	incident := previous.State.Simulation.IncidentContract
	if e.Full != nil {
		contract = e.Full.State.Simulation.OrderContract
		incident = e.Full.State.Simulation.IncidentContract
	}
	if err := sim.ValidateOrderContract(contract); err != nil {
		return err
	}
	if err := sim.ValidateIncidentContract(incident); err != nil {
		return err
	}
	if e.OrderContract != contract {
		return errors.New("publication order contract mismatch")
	}
	return nil
}

func (file *stateFile) validateWireContract() error {
	if file.OrderContract != "" && file.OrderContract != sim.ExpressOrderContract ||
		file.Simulation.OrderContract != file.OrderContract || file.Project.OrderContract != file.OrderContract {
		return errors.New("saved order contract markers disagree")
	}
	if err := file.validateIncidentValues(); err != nil {
		return err
	}
	if err := file.validateFaultValues(); err != nil {
		return err
	}
	return file.validateEmergencyValues()
}

// StreamHello opens a connection. Its contract markers select the
// optional sections of every publication on the connection. The hello has
// no other members, so that a client of an earlier version can decode it,
// record the build and refuse the version.
type StreamHello struct {
	Kind          string            `json:"kind"`
	Version       int               `json:"version"`
	Build         string            `json:"build"`
	ServerStart   string            `json:"serverStart"`
	OrderContract sim.OrderContract `json:"orderContract,omitzero"`
}

// DecodeStreamHello rejects unknown, duplicate, and contradictory
// negotiation, and every version other than StreamVersion. On an error it
// returns the members that it decoded, so that the client can record the
// build of a server of another version.
func DecodeStreamHello(raw []byte) (StreamHello, error) {
	var hello StreamHello
	if len(raw) > 4096 {
		return hello, errors.New("state hello exceeds supported limit")
	}
	if err := decodeStreamJSON(raw, &hello); err != nil {
		return hello, err
	}
	if hello.Kind != "hello" || hello.ServerStart == "" {
		return hello, errors.New("unsupported state stream protocol")
	}
	if hello.Version != StreamVersion {
		return hello, fmt.Errorf("unsupported state stream version %d", hello.Version)
	}
	if err := scanStreamServiceMembers(raw); err != nil {
		return hello, err
	}
	if err := scanContractMarkers(raw, hello.OrderContract == sim.ExpressOrderContract); err != nil {
		return hello, err
	}
	return hello, nil
}
