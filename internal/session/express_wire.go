package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// ExpressStateEnvelope binds packed dynamic state to its topology.
type ExpressStateEnvelope struct {
	OrderContract sim.OrderContract `json:"orderContract"`
	TextEncoding  string            `json:"textEncoding"`
	Topology      TopologySnapshot  `json:"topology"`
	Frame         StreamFrame       `json:"frame"`
}

// EncodeStreamJSON encodes a publication under its explicit field contract.
func EncodeStreamJSON(e StreamEnvelope) ([]byte, error) {
	if e.OrderContract == "" {
		if e.TextEncoding != "" {
			return nil, errors.New("foundation envelope contains text encoding")
		}
	} else if e.OrderContract != sim.ExpressOrderContract || e.TextEncoding != ExpressTextEncoding {
		return nil, errors.New("invalid Express envelope markers")
	}
	if err := validateEncodedContract(e); err != nil {
		return nil, err
	}
	var data []byte
	var err error
	if e.OrderContract == "" {
		data, err = json.Marshal(e)
	} else {
		data, err = jsonv2.Marshal(e, json.DefaultOptionsV1(), packedRequestOptions())
	}
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
	if e.Full != nil {
		contract = e.Full.State.Simulation.OrderContract
		coupling = e.Full.State.Simulation.CouplingContract
	}
	if err := sim.ValidateOrderContract(contract); err != nil {
		return err
	}
	if e.OrderContract != contract || (contract == sim.ExpressOrderContract && e.TextEncoding != ExpressTextEncoding) || (contract == "" && e.TextEncoding != "") {
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

// expressSavedLimits bound a version 7 state file, which has the Express
// marker. The table contains every other saved table, so the decoder also
// uses it before it reads the version.
func expressSavedLimits() jsonLimits {
	return savedLimits(contractMarkers{order: sim.ExpressOrderContract})
}

// expressStreamLimits bound hello 4 documents and the Express HTTP state.
func expressStreamLimits() jsonLimits {
	return streamLimits(contractMarkers{order: sim.ExpressOrderContract})
}

func (file *stateFile) validateWireContract() error {
	if err := file.validateCouplingContract(); err != nil {
		return err
	}
	if file.packedOrders() {
		if file.OrderContract != sim.ExpressOrderContract || file.TextEncoding != ExpressTextEncoding || file.Simulation.OrderContract != file.OrderContract || file.Project.OrderContract != file.OrderContract {
			return errors.New("saved Express contract markers disagree")
		}
	} else if file.OrderContract != "" || file.TextEncoding != "" || file.Simulation.OrderContract != "" || file.Project.OrderContract != "" {
		return errors.New("legacy saved version contains Express contract")
	}
	return nil
}

func preflightExpressTopology(config project.Config, serverStart, epoch string, revision uint64) error {
	if !project.HasCouplingContract(config) && config.OrderContract != sim.ExpressOrderContract {
		return nil
	}
	topology := TopologySnapshot{CouplingContract: config.CouplingContract, CouplingEnabled: config.CouplingEnabled, CouplingSites: config.CouplingSites, CouplingCorridors: config.CouplingCorridors, ProjectVersion: config.Version, OrderContract: config.OrderContract, ExpressServices: config.ExpressServices, Network: config.Network, Geo: config.Geo, Map: config.Map, ServerStart: serverStart, Epoch: epoch, ProjectRevision: revision}
	data, err := json.Marshal(topology)
	if err != nil {
		return err
	}
	if len(data) > project.MaxFileBytes+4096 {
		return errors.New("express topology exceeds supported limit")
	}
	return nil
}

// EncodeExpressStateJSON encodes a compact same-source HTTP state.
func EncodeExpressStateJSON(topology TopologySnapshot, frame StreamFrame) ([]byte, error) {
	if topology.OrderContract != sim.ExpressOrderContract || frame.State.Simulation.OrderContract != topology.OrderContract {
		return nil, errors.New("HTTP state requires Express contract")
	}
	assembler, err := NewStreamAssemblerVersion(topology, ExpressStreamVersion)
	if err != nil {
		return nil, err
	}
	if _, err = assembler.State(frame); err != nil {
		return nil, err
	}
	topologyBytes, err := json.Marshal(topology)
	if err != nil {
		return nil, err
	}
	if len(topologyBytes) > project.MaxFileBytes+4096 {
		return nil, errors.New("HTTP topology exceeds supported limit")
	}
	data, err := jsonv2.Marshal(ExpressStateEnvelope{sim.ExpressOrderContract, ExpressTextEncoding, topology, frame}, json.DefaultOptionsV1(), packedRequestOptions())
	if err == nil && len(data) > MaxStreamJSON {
		err = errors.New("HTTP state exceeds supported limit")
	}
	return data, err
}

// DecodeExpressStateJSON validates packed HTTP state before publishing native data.
func DecodeExpressStateJSON(raw []byte) (State, error) {
	if len(raw) > MaxStreamJSON {
		return State{}, errors.New("HTTP state exceeds supported limit")
	}
	if err := prescanJSON(raw, expressStreamLimits()); err != nil {
		return State{}, err
	}
	if err := scanContractMarkers(raw, true, true); err != nil {
		return State{}, err
	}
	if err := scanStreamBoardingMembers(raw, ExpressStreamVersion); err != nil {
		return State{}, err
	}
	if err := scanStreamServiceMembers(raw, ExpressStreamVersion); err != nil {
		return State{}, err
	}
	if err := scanPackedOrders(raw); err != nil {
		return State{}, err
	}
	var envelope ExpressStateEnvelope
	if err := jsonv2.Unmarshal(raw, &envelope, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true), packedDecodeOptions()); err != nil {
		return State{}, err
	}
	assembler, err := NewStreamAssemblerVersion(envelope.Topology, ExpressStreamVersion)
	if err != nil {
		return State{}, err
	}
	return assembler.State(envelope.Frame)
}

func (s *Session) stateHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if err := s.couplingError(); err != nil {
		s.mu.Unlock()
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if project.HasCouplingContract(s.project) {
		s.couplingStateHTTP(w, r)
		return
	}
	if s.project.OrderContract != sim.ExpressOrderContract {
		frame := stateFrame(s.state())
		err := s.couplingError()
		s.mu.Unlock()
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, frame)
		return
	}
	accepted := false
	for part := range strings.SplitSeq(r.Header.Get("Accept"), ",") {
		if strings.TrimSpace(part) == ExpressMediaType {
			accepted = true
		}
	}
	if !accepted {
		s.mu.Unlock()
		writeError(w, "use the Express state media type", http.StatusNotAcceptable)
		return
	}
	topology := s.topologyLocked()
	frame, err := s.presentationFrameLocked()
	s.mu.Unlock()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := EncodeExpressStateJSON(topology, frame)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ExpressMediaType)
	w.Header().Set("Vary", "Accept")
	_, _ = w.Write(data)
}

// StreamHello selects one field contract for the connection.
type StreamHello struct {
	Kind             string               `json:"kind"`
	Version          int                  `json:"version"`
	Build            string               `json:"build"`
	ServerStart      string               `json:"serverStart"`
	OrderContract    sim.OrderContract    `json:"orderContract,omitzero"`
	TextEncoding     string               `json:"textEncoding,omitzero"`
	CouplingContract sim.CouplingContract `json:"couplingContract,omitzero"`
}

// DecodeStreamHello rejects unknown, duplicate, and contradictory negotiation.
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
	if hello.Version < FoundationStreamVersion || hello.Version > StreamVersion {
		return hello, fmt.Errorf("unsupported state stream version %d", hello.Version)
	}
	if hello.Version == CouplingStreamVersion {
		if err := scanCouplingStreamJSON(raw); err != nil {
			return hello, err
		}
	} else if err := scanStreamServiceMembers(raw, hello.Version); err != nil {
		return hello, err
	}
	packed := hello.Version == ExpressStreamVersion || hello.Version == CouplingStreamVersion && hello.OrderContract == sim.ExpressOrderContract
	if err := scanContractMarkers(raw, packed, packed); err != nil {
		return hello, err
	}
	return hello, nil
}

func streamTextEncoding(contract sim.OrderContract) string {
	if contract == sim.ExpressOrderContract {
		return ExpressTextEncoding
	}
	return ""
}
