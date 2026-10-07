package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestEncodeCapsAtCallers puts a document at each encode cap of the save,
// the stream, and the HTTP state, and one byte over it. It also puts the
// topology member of an Express HTTP state at the topology decode cap and
// one byte over it. Each document at a cap is accepted, and each document
// over a cap gets the text of that cap.
func TestEncodeCapsAtCallers(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the cap documents run without -short and without the race detector")
	}

	t.Run("compressed save", func(t *testing.T) {
		// No session writes a save that reaches this cap: the widest save
		// has less raw JSON than the cap, and gzip adds less than the
		// difference. So the test calls the encoder with a demo error of
		// text that does not compress. The raw writer cap passes, because
		// the raw JSON stays at most MaxStateBytes.
		file := newTestStateFile(t)
		text := incompressibleText(MaxStateBytes)
		var encoder stateEncoder
		encode := func(n int) ([]byte, error) {
			file.Simulation.DemoError = textPrefix(text, n)
			return encoder.encode(file)
		}
		n, size := capTextLength(t, encode, MaxStateBytes-(1<<20), MaxStateBytes)
		if size != MaxStateBytes {
			t.Fatalf("the compressed save has %d bytes, want %d", size, MaxStateBytes)
		}
		_, err := encode(n + 1)
		wantError(t, err, fmt.Sprintf("compressed session state has %d bytes: %v", MaxStateBytes+1, ErrStateTooLarge))
	})

	s := expressSession(t)
	topology := s.Topology()
	frame, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("stream envelope", func(t *testing.T) {
		// The widest frame that a session writes is below this cap (see
		// the composed format record), so the frame gets a long demo
		// error. streamEncoder.encode, the only caller, refuses with the
		// same text when EncodeStreamJSON does, so the direct call of
		// EncodeStreamJSON is the only call that tells the two caps apart.
		envelope := fullStreamEnvelope(frame)
		simulation := &envelope.Full.State.Simulation
		simulation.DemoError = demoErrorTo(t, func(text string) ([]byte, error) {
			simulation.DemoError = text
			return EncodeStreamJSON(envelope)
		}, MaxStreamJSON)
		raw, err := EncodeStreamJSON(envelope)
		if err != nil || len(raw) != MaxStreamJSON {
			t.Fatalf("encoded %d bytes and %v at the cap", len(raw), err)
		}
		message, err := new(streamEncoder).encode(envelope)
		if err != nil {
			t.Fatal("the stream encoder refused the envelope at the cap", err)
		}
		inflated, err := InflateStream(message)
		if err != nil || !bytes.Equal(inflated, raw) {
			t.Fatal("the message at the cap does not inflate to the envelope", err)
		}
		simulation.DemoError += "x"
		raw, err = EncodeStreamJSON(envelope)
		wantError(t, err, "state JSON exceeds supported limit")
		if len(raw) != MaxStreamJSON+1 {
			t.Fatalf("the envelope over the cap has %d bytes", len(raw))
		}
		message, err = new(streamEncoder).encode(envelope)
		if message != nil {
			t.Fatalf("the stream encoder gave %d bytes over the cap", len(message))
		}
		wantError(t, err, "state JSON exceeds supported limit")
	})

	t.Run("HTTP state", func(t *testing.T) {
		// The widest HTTP state that a session writes is below this cap
		// (see the composed format record), so the frame gets a long demo
		// error.
		wide := frame
		wide.State.Simulation.DemoError = demoErrorTo(t, func(text string) ([]byte, error) {
			wide.State.Simulation.DemoError = text
			return EncodeStateJSON(topology, wide)
		}, MaxStreamJSON)
		raw, err := EncodeStateJSON(topology, wide)
		if err != nil || len(raw) != MaxStreamJSON {
			t.Fatalf("encoded %d bytes and %v at the cap", len(raw), err)
		}
		wide.State.Simulation.DemoError += "x"
		raw, err = EncodeStateJSON(topology, wide)
		wantError(t, err, "HTTP state exceeds supported limit")
		if len(raw) != MaxStreamJSON+1 {
			t.Fatalf("the HTTP state over the cap has %d bytes", len(raw))
		}
	})

	t.Run("HTTP topology", func(t *testing.T) {
		// No valid project has a topology at this cap (see
		// TestTopologyPreflightAtCallers). The test gives the topology of
		// a session the station names of a project whose topology, as the
		// preflight measures it, is at the cap. It calls EncodeStateJSON
		// with that topology and the frame of the session at the largest
		// project revision, and then with an epoch of one more character.
		config := topologyProject(t, project.MaxFileBytes+4096)
		s, err := NewWithProject(topologyProject(t, 0))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		escaped := s.Topology()
		escaped.Network = config.Network
		escapedFrame, err := s.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		encode := func(epoch string) ([]byte, error) {
			escaped.Epoch, escaped.ProjectRevision = epoch, sim.MaxCounter
			escapedFrame.State.Epoch, escapedFrame.State.ProjectRevision = epoch, sim.MaxCounter
			return EncodeStateJSON(escaped, escapedFrame)
		}
		raw, err := encode(escaped.Epoch)
		if err != nil {
			t.Fatal("refused the HTTP state at the topology cap", err)
		}
		state, err := DecodeStateJSON(raw)
		if err != nil || len(state.Network.Lanes) != len(config.Network.Lanes) {
			t.Fatal("the client refused the HTTP state at the topology cap", err)
		}
		_, err = encode(escaped.Epoch + "A")
		wantError(t, err, "HTTP topology exceeds supported limit")
	})

	t.Run("HTTP topology decode", func(t *testing.T) {
		// The typed decode of the topology refuses a topology member over
		// the cap.
		raw, err := EncodeStateJSON(topology, frame)
		if err != nil {
			t.Fatal(err)
		}
		state, err := DecodeStateJSON(padTopologyMember(t, raw, MaxTopologyJSON))
		if err != nil || state.Simulation.OrderContract != sim.ExpressOrderContract {
			t.Fatal("refused the topology at the cap", err)
		}
		_, err = DecodeStateJSON(padTopologyMember(t, raw, MaxTopologyJSON+1))
		wantError(t, err, "topology JSON is too large")
	})
}

// incompressibleText returns size bytes of valid UTF-8 that a JSON string
// holds without escapes. The bytes are near uniform over the byte values
// of UTF-8, so gzip BestSpeed stores them without compression.
func incompressibleText(size int) string {
	var ascii []byte
	for c := byte(0x20); c < 0x7f; c++ {
		if c != '"' && c != '\\' {
			ascii = append(ascii, c)
		}
	}
	text := make([]byte, 0, size+utf8.UTFMax)
	var x uint64 = 0x9e3779b97f4a7c15
	next := func() uint64 {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return x
	}
	// The weights make each lead byte as frequent as each ASCII byte.
	for len(text) < size {
		v := next()
		var r rune
		switch choice := v % 144; {
		case choice < 93:
			text = append(text, ascii[choice])
			continue
		case choice < 123:
			r = rune(0x80 + v>>8%0x780)
		case choice < 139:
			r = rune(0x800 + v>>8%0xf800)
			if utf8.RuneLen(r) < 0 {
				r = 0xe000
			}
		default:
			r = rune(0x10000 + v>>8%0x100000)
		}
		text = utf8.AppendRune(text, r)
	}
	return string(text)
}

// textPrefix returns n bytes: the longest prefix of text at a rune
// boundary, then ASCII letters.
func textPrefix(text string, n int) string {
	k := n
	for k > 0 && !utf8.RuneStart(text[k]) {
		k--
	}
	return text[:k] + strings.Repeat("a", n-k)
}

// capTextLength returns the length n of text for which encode gives
// exactly limit bytes, and the size of that result. The result grows with
// n: by one byte for each byte of text, and by a block header at each
// block boundary of the stored gzip blocks. encode must accept start.
func capTextLength(t *testing.T, encode func(int) ([]byte, error), start, limit int) (int, int) {
	t.Helper()
	// low is accepted, and high is refused or not yet known.
	low, high, n, size := start, 0, start, 0
	for range 64 {
		data, err := encode(n)
		switch {
		case errors.Is(err, ErrStateTooLarge):
			high = n
		case err != nil:
			t.Fatal(err)
		case len(data) == limit:
			return n, len(data)
		default:
			low, size = n, len(data)
		}
		n = low + limit - size
		if high != 0 {
			n = min(n, low+(high-low)/2)
		}
		if n == low {
			break
		}
	}
	t.Fatalf("no text length gives %d bytes; at length %d the result has %d bytes", limit, low, size)
	return 0, 0
}

// demoErrorTo returns a demo error of ASCII letters for which encode gives
// exactly size bytes. Each letter adds one byte.
func demoErrorTo(t *testing.T, encode func(string) ([]byte, error), size int) string {
	t.Helper()
	base, err := encode("")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Repeat("x", size-len(base))
}

// padTopologyMember returns the HTTP state raw with JSON whitespace in its
// topology member, so that the member has size bytes.
func padTopologyMember(t *testing.T, raw []byte, size int) []byte {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.ReadToken(); err != nil {
		t.Fatal(err)
	}
	for {
		token, err := decoder.ReadToken()
		if err != nil {
			t.Fatal("no topology member", err)
		}
		name := token.String()
		start := decoder.InputOffset()
		value, err := decoder.ReadValue()
		if err != nil {
			t.Fatal(err)
		}
		if name != "topology" {
			continue
		}
		if len(value) > size || value[0] != '{' {
			t.Fatalf("the topology member has %d bytes, more than %d", len(value), size)
		}
		// The colon and any whitespace come before the value.
		open := int(start) + bytes.IndexByte(raw[start:], '{') + 1
		padded := make([]byte, 0, len(raw)+size-len(value))
		padded = append(padded, raw[:open]...)
		padded = append(padded, bytes.Repeat([]byte{' '}, size-len(value))...)
		return append(padded, raw[open:]...)
	}
}

// topologyProject returns a plain project with a ring of 342 stations,
// the largest ring of 6 and 6 berths within project.MaxNetworkBlocks, and
// project.MaxPods pods. Each ID and each separation group has
// 64 ID characters. When size is 0, each station name has
// project.MaxNameLength "<", which the topology encoding writes as 6
// bytes, and the project is valid. Otherwise the station names give the
// topology, as the topology preflight measures it, size bytes. No valid
// project has a topology at the cap, so such names are longer than a
// valid name. A "<" adds 6 bytes, and a letter adds 1.
func topologyProject(t *testing.T, size int) project.Config {
	t.Helper()
	config, err := scenarios.Config(scenarios.Parameters{Name: "escaped topology", Stations: 342, Pods: project.MaxPods, PassengerBerths: 6, ParkingBerths: 6, DemandPerMinute: 1})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	rename := func(id *string) {
		if _, ok := ids[*id]; !ok {
			ids[*id] = widestID('i', len(ids))
		}
		*id = ids[*id]
	}
	network := &config.Network
	for i := range network.Nodes {
		rename(&network.Nodes[i].ID)
	}
	for i := range network.Lanes {
		lane := &network.Lanes[i]
		rename(&lane.ID)
		lane.SeparationGroup = lane.ID
		rename(&lane.From)
		rename(&lane.To)
		if lane.StationID != "" {
			rename(&lane.StationID)
		}
	}
	for i := range network.Stations {
		station := &network.Stations[i]
		for _, id := range []*string{&station.ID, &station.Entry, &station.Exit} {
			rename(id)
		}
		for j := range station.Berths {
			rename(&station.Berths[j].ID)
			rename(&station.Berths[j].Node)
		}
		for j := range station.Banks {
			bank := &station.Banks[j]
			for _, id := range []*string{&bank.ID, &bank.Entry, &bank.Exit} {
				rename(id)
			}
			for k := range bank.BerthIDs {
				rename(&bank.BerthIDs[k])
			}
		}
		station.Name = strings.Repeat("<", project.MaxNameLength)
	}
	for i := range config.Fleet {
		rename(&config.Fleet[i].StationID)
		rename(&config.Fleet[i].BerthID)
	}
	if config.Demand.Destination != "" {
		rename(&config.Demand.Destination)
	}
	if size == 0 {
		if err := project.Validate(config); err != nil {
			t.Fatal(err)
		}
		return config
	}
	// Each name of n letters "<" and m letters "a" adds 6n+m-1 bytes to a
	// name of one letter.
	for i := range network.Stations {
		network.Stations[i].Name = "a"
	}
	extra := size - escapedTopologySize(t, config)
	if extra < 0 {
		t.Fatalf("the topology has %d bytes with names of one letter, more than %d", size-extra, size)
	}
	for i := range network.Stations {
		add := extra / (len(network.Stations) - i)
		extra -= add
		n := (add + 1) / 6
		network.Stations[i].Name = strings.Repeat("<", n) + strings.Repeat("a", add+1-6*n)
	}
	if escapedTopologySize(t, config) != size {
		t.Fatalf("the names cannot give a topology of %d bytes", size)
	}
	return config
}

// escapedTopologySize returns the size of the topology of the plain
// project config as the topology preflight measures it, with the encoding
// of EncodeStateJSON.
func escapedTopologySize(t *testing.T, config project.Config) int {
	t.Helper()
	// A server start has 16 characters, a new epoch has 26, and the
	// preflight uses the largest project revision.
	raw, err := jsonv2.Marshal(TopologySnapshot{ProjectVersion: config.Version, ServerStart: strings.Repeat("0", 16), Epoch: strings.Repeat("0", 26), ProjectRevision: sim.MaxCounter, Network: config.Network}, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	return len(raw)
}

// stateHTTPReply checks that the topology of s has the size that
// escapedTopologySize gives for config, less the digits that the project
// revision of s does not have, and returns the body of the HTTP state
// reply of s, which must have status.
func stateHTTPReply(t *testing.T, s *Session, config project.Config, status int) []byte {
	t.Helper()
	topology := s.Topology()
	raw, err := jsonv2.Marshal(topology, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	want := escapedTopologySize(t, config) - len(strconv.FormatUint(sim.MaxCounter, 10)) + len(strconv.FormatUint(topology.ProjectRevision, 10))
	if len(raw) != want {
		t.Fatalf("the session topology has %d bytes, want %d", len(raw), want)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
	request.Header.Set("Accept", StateMediaType)
	response := httptest.NewRecorder()
	s.Handler("missing").ServeHTTP(response, request)
	if response.Code != status {
		t.Fatalf("status %d, want %d: %.200s", response.Code, status, response.Body.String())
	}
	return response.Body.Bytes()
}
