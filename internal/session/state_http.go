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
const StateMediaType = "application/vnd.podsim.state-7+json"

// MaxHTTPStateJSON limits an HTTP state, including its topology.
const MaxHTTPStateJSON = 128 << 20

const (
	// maxStateRequests is the number of HTTP states that the server makes
	// at the same time. Each one holds a copy of the frame and the
	// topology, and up to MaxHTTPStateJSON of JSON until its reply ends.
	// Thus 2 bound the JSON at 256 MiB. The editor and the debug capture
	// are the clients of the endpoint, so each can have one request.
	maxStateRequests = 2
	// stateRetryAfter is the Retry-After value in seconds of a reply when
	// all places are in use. The other busy replies use the same value.
	stateRetryAfter = "1"
)

// StateEnvelope binds the stream frame of the HTTP state to its topology.
// The contract markers are the markers of the topology and of the frame.
type StateEnvelope struct {
	OrderContract sim.OrderContract `json:"orderContract,omitzero"`
	Topology      TopologySnapshot  `json:"topology"`
	Frame         StreamFrame       `json:"frame"`
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
	topologyBytes, err := jsonv2.Marshal(topology, json.DefaultOptionsV1())
	if err != nil {
		return nil, err
	}
	if len(topologyBytes) > project.MaxFileBytes+4096 {
		return nil, errors.New("HTTP topology exceeds supported limit")
	}
	envelope := StateEnvelope{OrderContract: topology.OrderContract, Topology: topology, Frame: frame}
	data, err := jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
	if err == nil && len(data) > MaxHTTPStateJSON {
		err = errors.New("HTTP state exceeds supported limit")
	}
	return data, err
}

// DecodeStateJSON validates an HTTP state before assembly. The root
// contract markers select the rules, and they must be the markers of the
// topology.
func DecodeStateJSON(raw []byte) (State, error) {
	if len(raw) > MaxHTTPStateJSON {
		return State{}, errors.New("HTTP state exceeds supported limit")
	}
	var envelope StateEnvelope
	if err := decodeMarkedJSONLimit(raw, &envelope, MaxHTTPStateJSON); err != nil {
		return State{}, err
	}
	if envelope.OrderContract != envelope.Topology.OrderContract {
		return State{}, errors.New("HTTP order contracts disagree")
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
	s.serveState(w, r, stateSteps{capture: s.captureHTTPStateLocked, encode: EncodeStateJSON})
}

// stateSteps holds the steps of serveState that tests replace. capture
// copies the topology and the frame while the caller holds the session
// lock, and encode makes the JSON.
type stateSteps struct {
	capture func() (TopologySnapshot, StreamFrame, error)
	encode  func(TopologySnapshot, StreamFrame) ([]byte, error)
}

// serveState replies with the HTTP state. A request that accepts the state
// media type needs a place in s.stateSlots, and it keeps the place until
// its reply ends. When all places are in use, the request gets HTTP 503
// with Retry-After at once. A request that ends before the encoding gets
// HTTP 503 with no Retry-After, and the server does not encode the state.
// serveState checks for an ended request before the admission, so that
// such a request does not get Retry-After. It checks again after it gets
// the session lock, because that wait can be long, and after the copy.
func (s *Session) serveState(w http.ResponseWriter, r *http.Request, steps stateSteps) {
	if !acceptsMedia(r, StateMediaType) {
		writeError(w, "use the state media type", http.StatusNotAcceptable)
		return
	}
	if r.Context().Err() != nil {
		writeError(w, stateEndedText, http.StatusServiceUnavailable)
		return
	}
	select {
	case s.stateSlots <- struct{}{}:
		defer func() { <-s.stateSlots }()
	default:
		w.Header().Set("Retry-After", stateRetryAfter)
		writeError(w, "the server is busy with other state requests, try again", http.StatusServiceUnavailable)
		return
	}
	s.mu.Lock()
	if r.Context().Err() != nil {
		s.mu.Unlock()
		writeError(w, stateEndedText, http.StatusServiceUnavailable)
		return
	}
	topology, frame, err := steps.capture()
	s.mu.Unlock()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Context().Err() != nil {
		writeError(w, stateEndedText, http.StatusServiceUnavailable)
		return
	}
	data, err := steps.encode(topology, frame)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", StateMediaType)
	w.Header().Add("Vary", "Accept")
	_, _ = w.Write(data)
}

// stateEndedText is the reply text to a state request that ended
// before the encoding.
const stateEndedText = "the request ended before the server encoded the state"

// captureHTTPStateLocked copies the topology and the frame of the HTTP
// state. The caller holds the session lock.
func (s *Session) captureHTTPStateLocked() (TopologySnapshot, StreamFrame, error) {
	topology := s.topologyLocked()
	frame, err := s.presentationFrameLocked()
	return topology, frame, err
}
