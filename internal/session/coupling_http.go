package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// CouplingMediaType requires the independent compact-pair-v1 HTTP contract.
const CouplingMediaType = "application/vnd.podsim.compact-pair-v1+json"

// CouplingStateEnvelope binds checked cabins and train geometry to one topology.
type CouplingStateEnvelope struct {
	CouplingContract sim.CouplingContract `json:"couplingContract"`
	OrderContract    sim.OrderContract    `json:"orderContract,omitzero"`
	TextEncoding     string               `json:"textEncoding,omitzero"`
	Topology         TopologySnapshot     `json:"topology"`
	Frame            StreamFrame          `json:"frame"`
}

// EncodeCouplingStateJSON validates a same-source HTTP state before encoding.
func EncodeCouplingStateJSON(topology TopologySnapshot, frame StreamFrame) ([]byte, error) {
	assembler, err := NewStreamAssemblerVersion(topology, CouplingStreamVersion)
	if err != nil {
		return nil, err
	}
	if _, stateErr := assembler.State(frame); stateErr != nil {
		return nil, stateErr
	}
	topologyBytes, err := json.Marshal(topology)
	if err != nil {
		return nil, err
	}
	if len(topologyBytes) > project.MaxFileBytes+4096 {
		return nil, errors.New("HTTP topology exceeds supported limit")
	}
	envelope := CouplingStateEnvelope{CouplingContract: topology.CouplingContract, OrderContract: topology.OrderContract,
		TextEncoding: streamTextEncoding(topology.OrderContract), Topology: topology, Frame: frame}
	var data []byte
	if topology.OrderContract == sim.ExpressOrderContract {
		data, err = jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
	} else {
		data, err = json.Marshal(envelope)
	}
	if err == nil && len(data) > MaxStreamJSON {
		err = errors.New("HTTP state exceeds supported limit")
	}
	return data, err
}

// DecodeCouplingStateJSON validates both independent contracts before assembly.
func DecodeCouplingStateJSON(raw []byte) (State, error) {
	if len(raw) > MaxStreamJSON {
		return State{}, errors.New("HTTP state exceeds supported limit")
	}
	var header struct {
		OrderContract sim.OrderContract `json:"orderContract"`
	}
	if err := jsonv2.Unmarshal(raw, &header, json.DefaultOptionsV1()); err != nil {
		return State{}, err
	}
	packed := header.OrderContract == sim.ExpressOrderContract
	if err := prescanJSON(raw, couplingStreamLimits(packed)); err != nil {
		return State{}, err
	}
	if err := scanCouplingPublicJSON(raw, true); err != nil {
		return State{}, err
	}
	if err := scanContractMarkers(raw, packed, packed); err != nil {
		return State{}, err
	}
	orderVersion := CouplingStreamVersion
	if packed {
		orderVersion = ExpressStreamVersion
		if err := scanPackedOrders(raw); err != nil {
			return State{}, err
		}
	}
	if err := scanStreamBoardingMembers(raw, orderVersion); err != nil {
		return State{}, err
	}
	if err := scanStreamServiceMembersContract(raw, orderVersion, true); err != nil {
		return State{}, err
	}
	var envelope CouplingStateEnvelope
	var err error
	if packed {
		err = jsonv2.Unmarshal(raw, &envelope, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true), packedDecodeOptions())
	} else {
		err = decodeStreamJSON(raw, &envelope)
	}
	if err != nil {
		return State{}, err
	}
	if envelope.CouplingContract != envelope.Topology.CouplingContract || envelope.OrderContract != envelope.Topology.OrderContract ||
		envelope.TextEncoding != streamTextEncoding(envelope.OrderContract) {
		return State{}, errors.New("HTTP coupling or order contracts disagree")
	}
	assembler, err := NewStreamAssemblerVersion(envelope.Topology, CouplingStreamVersion)
	if err != nil {
		return State{}, err
	}
	return assembler.State(envelope.Frame)
}

// The caller holds the session lock. This method releases it before encoding.
func (s *Session) couplingStateHTTP(w http.ResponseWriter, r *http.Request) {
	accepted := false
	for _, value := range r.Header.Values("Accept") {
		for part := range strings.SplitSeq(value, ",") {
			accepted = accepted || strings.TrimSpace(part) == CouplingMediaType
		}
	}
	if !accepted {
		s.mu.Unlock()
		writeError(w, "use the coupling state media type", http.StatusNotAcceptable)
		return
	}
	topology := s.topologyLocked()
	frame, err := s.presentationFrameLocked()
	s.mu.Unlock()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := EncodeCouplingStateJSON(topology, frame)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", CouplingMediaType)
	w.Header().Set("Vary", "Accept")
	_, _ = w.Write(data)
}
