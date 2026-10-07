package remote

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
)

// boundsClient returns a client of a server that sends body with media
// for each request.
func boundsClient(t *testing.T, media string, body []byte) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", media)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return &Client{url: server.URL, http: server.Client()}
}

// padded returns prefix and suffix around filler bytes, to a total of size
// bytes.
func padded(prefix, suffix string, size int) []byte {
	body := make([]byte, 0, size)
	body = append(body, prefix...)
	body = append(body, bytes.Repeat([]byte("a"), size-len(prefix)-len(suffix))...)
	return append(body, suffix...)
}

// boundsTarget returns a nonempty target for path, so that a test can see a
// change.
func boundsTarget(path string) any {
	switch path {
	case "/api/topology":
		return &session.TopologySnapshot{Epoch: "accepted"}
	case "/api/command":
		return &session.Reply{Epoch: "accepted"}
	default:
		return &session.State{Epoch: "accepted"}
	}
}

func TestExchangeBoundsResponseBodies(t *testing.T) {
	topology := func(size int) []byte {
		return padded(`{"projectVersion":1,"serverStart":"s","epoch":"`, `","projectRevision":1,"network":{}}`, size)
	}
	tests := []struct {
		name  string
		path  string
		media string
		body  []byte
		ok    bool
	}{
		{"state at the limit", "/api/state", session.StateMediaType, padded(`{"epoch":"`, `"}`, session.MaxStreamJSON), true},
		{"state past the limit", "/api/state", session.StateMediaType, padded(`{"epoch":"`, `"}`, session.MaxStreamJSON+1), false},
		{"topology at the limit", "/api/topology", "application/json", append(topology(session.MaxTopologyJSON), '\n'), true},
		{"topology past the limit", "/api/topology", "application/json", topology(session.MaxTopologyJSON + 2), false},
		{"command reply past the limit", "/api/command", "application/json", padded(`{"epoch":"`, `"}`, session.MaxCommandBytes+1), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := boundsClient(t, test.media, test.body)
			target := boundsTarget(test.path)
			before := reflect.ValueOf(target).Elem().Interface()
			err := client.exchange(t.Context(), http.MethodGet, test.path, nil, target)
			if test.ok {
				// The state at the limit is not a valid state, so only the
				// size must pass.
				if errors.Is(err, errResponseTooLarge) || err != nil && test.path != "/api/state" {
					t.Fatal("rejected a response at the limit", err)
				}
				return
			}
			if !errors.Is(err, errResponseTooLarge) {
				t.Fatalf("got %v, want %v", err, errResponseTooLarge)
			}
			if !reflect.DeepEqual(before, reflect.ValueOf(target).Elem().Interface()) {
				t.Fatal("rejected response changed the target")
			}
		})
	}
}

// The bounded scans run before any other parse. A document deeper than the
// jsontext decoder's own cap fails with the scan error. Without the scan,
// the marker walk or the JSON decoder fails first with its own error.
func TestExchangeBoundsBeforeParsing(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	members := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, `,"m%d":0`, i)
		}
		return b.String()
	}
	deep := strings.Repeat("[", 20000) + strings.Repeat("]", 20000)
	const tooDeep, tooLong, tooMany = "JSON nesting is too deep", "JSON array has too many elements", "JSON object has too many members"
	tests := []struct {
		path string
		name string
		raw  string
		want string
	}{
		{"/api/state", "deeper than the decoder cap", `{"epoch":"x","x":` + deep + `}`, tooDeep},
		{"/api/state", "vehicles past the fleet bound", `{"frame":{"state":{"simulation":{"vehicles":` + zeros(project.MaxPods+1) + `}}}}`, tooLong},
		{"/api/state", "object past the member limit", `{"x":0` + members(256) + `}`, tooMany},
		{"/api/topology", "deeper than the decoder cap", `{"epoch":"x","x":` + deep + `}`, tooDeep},
		{"/api/topology", "nodes past the network bound", `{"network":{"nodes":` + zeros(project.MaxNodes+1) + `}}`, tooLong},
		{"/api/topology", "object past the member limit", `{"x":0` + members(256) + `}`, tooMany},
	}
	for _, test := range tests {
		t.Run(test.path+"/"+test.name, func(t *testing.T) {
			t.Parallel()
			media := "application/json"
			if test.path == "/api/state" {
				media = session.StateMediaType
			}
			client := boundsClient(t, media, []byte(test.raw+"\n"))
			target := boundsTarget(test.path)
			before := reflect.ValueOf(target).Elem().Interface()
			err := client.exchange(t.Context(), http.MethodGet, test.path, nil, target)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %s", err, test.want)
			}
			if !reflect.DeepEqual(before, reflect.ValueOf(target).Elem().Interface()) {
				t.Fatal("rejected response changed the target")
			}
		})
	}
}

// The state decoder refuses invalid UTF-8. The plain state of earlier
// servers replaced it.
func TestExchangeStateRefusesInvalidUTF8(t *testing.T) {
	t.Parallel()
	client := boundsClient(t, session.StateMediaType, []byte("{\"frame\":{\"state\":{\"epoch\":\"\xff\"}}}"))
	state := session.State{Epoch: "kept"}
	if err := client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state); err == nil || state.Epoch != "kept" {
		t.Fatal("state with invalid UTF-8 accepted", err, state.Epoch)
	}
}

// A real server's responses decode to the values of the reference
// decoders: the state decoder for the state, plain JSON for the others.
func TestExchangeServerResponsesMatchPlainDecode(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	server := httptest.NewServer(shared.Handler("missing"))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client()}
	reference := func(path string, target any) {
		t.Helper()
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, http.NoBody)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Accept", session.StateMediaType)
		response, getErr := server.Client().Do(request)
		if getErr != nil {
			t.Fatal(getErr)
		}
		defer func() { _ = response.Body.Close() }()
		raw, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if state, ok := target.(*session.State); ok {
			var decodeErr error
			if *state, decodeErr = session.DecodeStateJSON(raw); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			return
		}
		if decodeErr := jsonv2.Unmarshal(raw, target, json.DefaultOptionsV1()); decodeErr != nil {
			t.Fatal(decodeErr)
		}
	}
	var topology, wantTopology session.TopologySnapshot
	reference("/api/topology", &wantTopology)
	if err = client.exchange(t.Context(), http.MethodGet, "/api/topology", nil, &topology); err != nil || !reflect.DeepEqual(topology, wantTopology) {
		t.Fatal("topology response changed", err)
	}
	var state, wantState session.State
	reference("/api/state", &wantState)
	if err = client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state); err != nil || !reflect.DeepEqual(state, wantState) {
		t.Fatal("state response changed", err)
	}
	command, err := jsonv2.Marshal(session.Command{Client: "bounds", Sequence: 1, Epoch: state.Epoch, Action: "trip", Origin: "harbor", Destination: "market"}, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var reply session.Reply
	if err = client.exchange(t.Context(), http.MethodPost, "/api/command", command, &reply); err != nil || reply.Error != "" || reply.Epoch != state.Epoch {
		t.Fatal("command reply changed", err, reply)
	}
}

// countingReader returns a body of size bytes and counts the bytes that a
// caller reads from it.
type countingReader struct{ read, size int64 }

func (r *countingReader) Read(p []byte) (int, error) {
	if r.read >= r.size {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), r.size-r.read)]
	for i := range p {
		p[i] = 'a'
	}
	r.read += int64(len(p))
	return len(p), nil
}

// TestReadResponseStopsAtLimit checks that readResponse reads at most one
// byte past the limit of a body that is much larger than the limit.
func TestReadResponseStopsAtLimit(t *testing.T) {
	t.Parallel()
	const limit = 1 << 16
	body := &countingReader{size: 16 * limit}
	if _, err := readResponse(body, limit); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("endless body gave %v, want errResponseTooLarge", err)
	}
	if body.read > limit+1 {
		t.Fatalf("read %d bytes, want %d or fewer", body.read, limit+1)
	}
}

// TestExchangeFailedDecodeKeepsTarget checks that a body that passes the
// bounds but fails to decode, or has data after its document, leaves the
// target unchanged.
func TestExchangeFailedDecodeKeepsTarget(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, path, body string
	}{
		{"state with a bad field", "/api/state", `{"epoch":"changed","revision":"bad"}`},
		{"command reply with trailing data", "/api/command", `{"epoch":"changed"} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			media := "application/json"
			if test.path == "/api/state" {
				media = session.StateMediaType
			}
			client := boundsClient(t, media, []byte(test.body))
			var err error
			var epoch string
			if test.path == "/api/state" {
				state := session.State{Epoch: "kept"}
				err = client.exchange(t.Context(), http.MethodGet, test.path, nil, &state)
				epoch = state.Epoch
			} else {
				reply := session.Reply{Epoch: "kept"}
				err = client.exchange(t.Context(), http.MethodPost, test.path, []byte("{}"), &reply)
				epoch = reply.Epoch
			}
			if err == nil {
				t.Fatal("invalid body was accepted")
			}
			if epoch != "kept" {
				t.Fatalf("target epoch is %q after a failed decode, want kept", epoch)
			}
		})
	}
}

// A command reply member whose case differs from the declared name is
// unknown, so the client ignores it. The state decoder refuses unknown
// members.
func TestExchangeIgnoresCaseVariantMembers(t *testing.T) {
	t.Parallel()
	client := boundsClient(t, "application/json", []byte(`{"Epoch":"folded","epoch":"exact","Revision":9}`))
	var reply session.Reply
	if err := client.exchange(t.Context(), http.MethodPost, "/api/command", []byte("{}"), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Epoch != "exact" || reply.Revision != 0 {
		t.Fatalf("case variant members decoded: epoch %q, revision %d", reply.Epoch, reply.Revision)
	}
	state := session.State{Epoch: "kept"}
	client = boundsClient(t, session.StateMediaType, []byte(`{"FRAME":{}}`))
	if err := client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state); err == nil || state.Epoch != "kept" {
		t.Fatal("state with a case variant member accepted", err)
	}
}
