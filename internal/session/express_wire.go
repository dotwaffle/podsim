package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
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
// the publisher writes them. It does not parse the raw delta replacement
// groups. frameGroups builds them from the same frame, so the check trusts
// their members and examines only the coupling group key.
func validateEncodedContract(e StreamEnvelope) error {
	if e.CouplingContract != "" {
		if _, known := sim.LookupCouplingProfile(e.CouplingContract); !known {
			return sim.ErrUnknownCouplingContract
		}
	}
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
		if e.CouplingContract != e.Full.State.Simulation.CouplingContract {
			return errors.New("publication coupling contract mismatch")
		}
	}
	if e.CouplingContract != "" {
		return nil
	}
	if e.Full != nil && hasCouplingFrameFields(e.Full.State.Simulation) {
		return errors.New("unmarked publication contains coupling fields")
	}
	if e.Delta != nil && hasCouplingDeltaFields(*e.Delta) {
		return errors.New("unmarked publication contains coupling fields")
	}
	return nil
}

// hasCouplingFrameFields reports the fields that couplingFrameBinding
// rejects in an unmarked frame.
func hasCouplingFrameFields(frame SimulationFrame) bool {
	if frame.CouplingEnabled || frame.CouplingGroups != nil {
		return true
	}
	for _, cabin := range frame.Vehicles {
		if cabin.CouplingID != "" {
			return true
		}
	}
	return false
}

func hasCouplingDeltaFields(delta StreamDelta) bool {
	if _, replaced := delta.Groups["coupling"]; replaced {
		return true
	}
	for _, vehicle := range delta.Vehicles {
		if vehicle.Metadata != nil && vehicle.Metadata.Value.CouplingID != "" {
			return true
		}
	}
	return false
}

func validateEnvelopeContract(e StreamEnvelope, previous StreamFrame) error {
	contract := previous.State.Simulation.OrderContract
	coupling := previous.State.Simulation.CouplingContract
	incident := previous.State.Simulation.IncidentContract
	if e.Full != nil {
		contract = e.Full.State.Simulation.OrderContract
		coupling = e.Full.State.Simulation.CouplingContract
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
	if coupling != "" {
		if _, known := sim.LookupCouplingProfile(coupling); !known {
			return sim.ErrUnknownCouplingContract
		}
	}
	if e.CouplingContract != coupling {
		return errors.New("publication coupling contract mismatch")
	}
	return nil
}

func (file *stateFile) validateWireContract() error {
	if err := file.validateCouplingContract(); err != nil {
		return err
	}
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

func preflightExpressTopology(config project.Config, serverStart, epoch string, revision uint64) error {
	if !project.HasCouplingContract(config) && config.OrderContract != sim.ExpressOrderContract {
		return nil
	}
	topology := TopologySnapshot{CouplingContract: config.CouplingContract, CouplingEnabled: config.CouplingEnabled, CouplingSites: config.CouplingSites, CouplingCorridors: config.CouplingCorridors, ProjectVersion: config.Version, OrderContract: config.OrderContract, IncidentContract: config.IncidentContract, FaultContract: config.FaultContract, EmergencyContract: config.EmergencyContract, ExpressServices: config.ExpressServices, Network: config.Network, Geo: config.Geo, Map: config.Map, ServerStart: serverStart, Epoch: epoch, ProjectRevision: revision}
	data, err := json.Marshal(topology)
	if err != nil {
		return err
	}
	if len(data) > project.MaxFileBytes+4096 {
		return errors.New("express topology exceeds supported limit")
	}
	return nil
}

// StreamHello opens a connection. Its contract markers select the
// optional sections of every publication on the connection. The hello has
// no other members, so that a client of an earlier version can decode it,
// record the build and refuse the version.
type StreamHello struct {
	Kind             string               `json:"kind"`
	Version          int                  `json:"version"`
	Build            string               `json:"build"`
	ServerStart      string               `json:"serverStart"`
	OrderContract    sim.OrderContract    `json:"orderContract,omitzero"`
	CouplingContract sim.CouplingContract `json:"couplingContract,omitzero"`
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
	markers := contractMarkers{order: hello.OrderContract, coupling: hello.CouplingContract}
	if markers.coupling != "" {
		if err := scanCouplingStreamJSON(raw); err != nil {
			return hello, err
		}
	} else if err := scanStreamServiceMembers(raw, markers); err != nil {
		return hello, err
	}
	if err := scanContractMarkers(raw, markers.order == sim.ExpressOrderContract); err != nil {
		return hello, err
	}
	return hello, nil
}
