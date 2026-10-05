package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"mime"
	"net/http"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// StateMediaType is the media type of the HTTP state of every project.
// The state endpoint replies only to a request that accepts it.
const StateMediaType = "application/vnd.podsim.state-6+json"

// StateEnvelope binds the stream frame of the HTTP state to its topology.
// The contract markers are the markers of the topology and of the frame.
type StateEnvelope struct {
	CouplingContract sim.CouplingContract `json:"couplingContract,omitzero"`
	OrderContract    sim.OrderContract    `json:"orderContract,omitzero"`
	Topology         TopologySnapshot     `json:"topology"`
	Frame            StreamFrame          `json:"frame"`
}

// EncodeStateJSON validates a same-source HTTP state before encoding. It
// packs the order text.
func EncodeStateJSON(topology TopologySnapshot, frame StreamFrame) ([]byte, error) {
	assembler, err := NewStreamAssembler(topology)
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
	envelope := StateEnvelope{CouplingContract: topology.CouplingContract, OrderContract: topology.OrderContract, Topology: topology, Frame: frame}
	data, err := jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
	if err == nil && len(data) > MaxStreamJSON {
		err = errors.New("HTTP state exceeds supported limit")
	}
	return data, err
}

// DecodeStateJSON validates an HTTP state before assembly. The root
// contract markers select the rules, and they must be the markers of the
// topology.
func DecodeStateJSON(raw []byte) (State, error) {
	if len(raw) > MaxStreamJSON {
		return State{}, errors.New("HTTP state exceeds supported limit")
	}
	var envelope StateEnvelope
	members, err := decodeMarkedJSON(raw, true, &envelope)
	if err != nil {
		return State{}, err
	}
	if members && envelope.Frame.State.Simulation.IncidentContract == "" {
		return State{}, errIncidentStreamUnmarked
	}
	if envelope.CouplingContract != envelope.Topology.CouplingContract || envelope.OrderContract != envelope.Topology.OrderContract {
		return State{}, errors.New("HTTP coupling or order contracts disagree")
	}
	assembler, err := NewStreamAssembler(envelope.Topology)
	if err != nil {
		return State{}, err
	}
	return assembler.State(envelope.Frame)
}

// acceptsMedia reports whether an Accept header of r names media with a
// quality above zero. A media range with a wildcard does not name media,
// and a media range with a bad quality value does not accept it.
func acceptsMedia(r *http.Request, media string) bool {
	for _, value := range r.Header.Values("Accept") {
		for part := range strings.SplitSeq(value, ",") {
			name, params, err := mime.ParseMediaType(part)
			if err != nil || name != media {
				continue
			}
			if q, found := params["q"]; !found || positiveQuality(q) {
				return true
			}
		}
	}
	return false
}

// positiveQuality reports whether q is a quality value of RFC 9110 above
// zero: "0" or "1", then optionally a period and at most three digits.
// After a 1, each digit must be zero.
func positiveQuality(q string) bool {
	whole, fraction, _ := strings.Cut(q, ".")
	if len(fraction) > 3 || strings.Trim(fraction, "0123456789") != "" {
		return false
	}
	switch whole {
	case "0":
		return strings.Trim(fraction, "0") != ""
	case "1":
		return strings.Trim(fraction, "0") == ""
	}
	return false
}

func (s *Session) stateHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if err := s.couplingError(); err != nil {
		s.mu.Unlock()
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !acceptsMedia(r, StateMediaType) {
		s.mu.Unlock()
		writeError(w, "use the state media type", http.StatusNotAcceptable)
		return
	}
	topology := s.topologyLocked()
	frame, err := s.presentationFrameLocked()
	s.mu.Unlock()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := EncodeStateJSON(topology, frame)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", StateMediaType)
	w.Header().Add("Vary", "Accept")
	_, _ = w.Write(data)
}
