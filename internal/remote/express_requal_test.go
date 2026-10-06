package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// expressRequalFrame returns the topology and the current frame of an
// Express session, as a stream full frame.
func expressRequalFrame(t *testing.T) (session.TopologySnapshot, session.StreamFrame) {
	t.Helper()
	shared, err := session.NewWithProject(remoteExpressProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
	request.Header.Set("Accept", session.StateMediaType)
	response := httptest.NewRecorder()
	shared.HandlerFS(nil).ServeHTTP(response, request)
	var envelope struct {
		Frame json.RawMessage `json:"frame"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	state := shared.Frame()
	source := session.StreamSource{ServerStart: state.ServerStart, Epoch: state.Epoch, ProjectRevision: state.ProjectRevision, Generation: state.Generation, Revision: state.Revision}
	wire := map[string]any{"orderContract": sim.ExpressOrderContract, "kind": "full", "stream": "requal", "sequence": "1", "source": source, "full": envelope.Frame}
	decoded, err := session.DecodeStreamJSON(streamJSON(t, wire))
	if err != nil {
		t.Fatal(err)
	}
	return shared.Topology(), *decoded.Full
}

// expressRequalMessage returns the gzip publication of e.
func expressRequalMessage(t *testing.T, e session.StreamEnvelope) []byte {
	t.Helper()
	raw, err := session.EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

// TestExpressRequalRemoteRecovery runs the Go client against two Express
// connections. On the first, the client acknowledges a full frame and
// then refuses a delta after a sequence gap without an acknowledgement.
// It keeps the accepted state. On the second, a full frame of a new epoch
// on a new stream replaces the state after the client reads the topology
// of that epoch.
func TestExpressRequalRemoteRecovery(t *testing.T) {
	topology, frame := expressRequalFrame(t)
	recovered := frame
	recovered.State.Epoch, recovered.State.Revision = "requal-recovered", 1
	recoveredTopology := topology
	recoveredTopology.Epoch = recovered.State.Epoch
	full := session.StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "first", Sequence: 1, Source: sourceOfFrame(frame), Build: frame.State.Build, Full: &frame}
	gap := session.StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "first", Sequence: 4, Base: 3, Source: sourceOfFrame(frame), Build: frame.State.Build, Delta: &session.StreamDelta{}}
	recovery := session.StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "second", Sequence: 1, Source: sourceOfFrame(recovered), Build: recovered.State.Build, Full: &recovered}
	connections := [][][]byte{
		{expressRequalMessage(t, full), expressRequalMessage(t, gap)},
		{expressRequalMessage(t, recovery)},
	}
	var connection, phase atomic.Int32
	// acks holds the acknowledged sequences of each connection, and -1
	// after a message that the client did not acknowledge.
	acks := make(chan []string, len(connections))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/topology" {
			current := topology
			if phase.Load() > 0 {
				current = recoveredTopology
			}
			_ = json.NewEncoder(w).Encode(current)
			return
		}
		index := int(connection.Add(1)) - 1
		conn, err := websocket.Accept(w, r, nil)
		if err != nil || index >= len(connections) {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		phase.Store(int32(index))
		hello := session.StreamHello{Kind: "hello", Version: session.StreamVersion, Build: frame.State.Build, ServerStart: frame.State.ServerStart, OrderContract: sim.ExpressOrderContract}
		if err = conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello)); err != nil {
			return
		}
		var got []string
		defer func() { acks <- got }()
		for _, message := range connections[index] {
			if err = conn.Write(r.Context(), websocket.MessageBinary, message); err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			_, body, readErr := conn.Read(ctx)
			cancel()
			if readErr != nil {
				got = append(got, "-1")
				return
			}
			var ack struct {
				Kind     string `json:"kind"`
				Stream   string `json:"stream"`
				Sequence string `json:"sequence"`
			}
			if json.Unmarshal(body, &ack) != nil || ack.Kind != "ack" {
				got = append(got, "invalid")
				return
			}
			got = append(got, ack.Stream+"/"+ack.Sequence)
		}
	}))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client(), build: frame.State.Build}
	receive := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		return client.receiveStream(ctx)
	}
	if err := receive(); err == nil || err.Error() != "delta base mismatch" {
		t.Fatalf("the first connection ended with %v, want the sequence gap", err)
	}
	if got := strings.Join(<-acks, ","); got != "first/1,-1" {
		t.Fatalf("first connection acknowledgements %s, want first/1,-1", got)
	}
	state, connected, _ := client.View()
	if !connected || state.Epoch != frame.State.Epoch || state.Simulation.OrderContract != sim.ExpressOrderContract {
		t.Fatalf("the refused delta changed the accepted state: connected %t, epoch %q", connected, state.Epoch)
	}
	if err := receive(); err == nil {
		t.Fatal("the second connection ended without an error")
	}
	if got := strings.Join(<-acks, ","); got != "second/1" {
		t.Fatalf("second connection acknowledgements %s, want second/1", got)
	}
	state, _, _ = client.View()
	if state.Epoch != recovered.State.Epoch || state.Simulation.OrderContract != sim.ExpressOrderContract || len(state.Network.Stations) != len(recoveredTopology.Network.Stations) {
		t.Fatalf("the recovery did not replace the state: epoch %q, marker %q", state.Epoch, state.Simulation.OrderContract)
	}
}

func sourceOfFrame(f session.StreamFrame) session.StreamSource {
	return session.StreamSource{ServerStart: f.State.ServerStart, Epoch: f.State.Epoch, ProjectRevision: f.State.ProjectRevision, Generation: f.State.Generation, Revision: f.State.Revision}
}
