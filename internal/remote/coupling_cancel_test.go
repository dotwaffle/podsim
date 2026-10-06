package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingPublication returns the topology and the gzip publication of a
// full frame with one committed coupling group.
func couplingPublication(t *testing.T) (session.TopologySnapshot, []byte) {
	t.Helper()
	topology, frame := remoteCouplingFrame(t, "")
	e := session.StreamEnvelope{CouplingContract: sim.CompactPairV1CouplingContract, Kind: "full", Stream: "cancel-stream", Sequence: 1,
		Source: session.StreamSource{ServerStart: frame.State.ServerStart, Epoch: frame.State.Epoch,
			ProjectRevision: frame.State.ProjectRevision, Revision: frame.State.Revision}, Full: &frame}
	raw, err := session.EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return topology, buffer.Bytes()
}

// TestCouplingInflateCanceled inflates a coupling publication with a live
// context and with an ended one. The canceled call gives the cause and no
// bytes. In a js/wasm build the test needs the PodsimStream decoder of
// web/stream.js.
func TestCouplingInflateCanceled(t *testing.T) {
	t.Parallel()
	if err := streamSupported(); err != nil {
		t.Skip(err)
	}
	_, publication := couplingPublication(t)
	inflated, err := inflatePublication(t.Context(), publication)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := session.DecodeStreamJSON(inflated)
	if err != nil || len(envelope.Full.State.Simulation.CouplingGroups) != 1 {
		t.Fatal("inflated publication lost its coupling group", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	inflated, err = inflatePublication(ctx, publication)
	if inflated != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %d bytes and %v, want no bytes and %v", len(inflated), err, context.Canceled)
	}
}

// cancelAfterTopology reads the whole topology reply, and then cancels the
// stream of the client. So the client gets the topology and assembles the
// state before it sees the cancel.
type cancelAfterTopology struct {
	next   http.RoundTripper
	cancel context.CancelFunc
}

func (c cancelAfterTopology) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := c.next.RoundTrip(r)
	if err != nil || r.URL.Path != "/api/topology" {
		return response, err
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	c.cancel()
	return response, nil
}

// TestCouplingStreamCanceledAfterAssembly cancels the stream of the client
// after it decodes a coupling publication and its topology, and before it
// shows the state. The client keeps its earlier state and sends no ACK.
func TestCouplingStreamCanceledAfterAssembly(t *testing.T) {
	t.Parallel()
	topology, publication := couplingPublication(t)
	acknowledged := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/topology" {
			_ = json.NewEncoder(w).Encode(topology)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		hello := session.StreamHello{Kind: "hello", Version: session.StreamVersion, ServerStart: topology.ServerStart, CouplingContract: topology.CouplingContract}
		if conn.Write(ctx, websocket.MessageText, streamJSON(t, hello)) != nil || conn.Write(ctx, websocket.MessageBinary, publication) != nil {
			acknowledged <- false
			return
		}
		kind, _, err := conn.Read(ctx)
		acknowledged <- err == nil && kind == websocket.MessageText
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	client := &Client{url: server.URL, http: &http.Client{Transport: cancelAfterTopology{next: server.Client().Transport, cancel: cancel}},
		state: session.State{Epoch: "prior"}}
	if err := client.receiveStream(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want %v", err, context.Canceled)
	}
	if client.state.Epoch != "prior" || client.connected {
		t.Fatal("canceled stream showed the coupling state")
	}
	select {
	case ack := <-acknowledged:
		if ack {
			t.Fatal("canceled stream acknowledged the coupling publication")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing ACK observation")
	}
}
