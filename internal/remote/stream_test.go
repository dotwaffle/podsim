package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
)

// A server of another build sends a hello of another version, or a
// publication that the client cannot read. The client records the build
// of the hello before it refuses the connection, so that the page reloads.
func TestStreamBuildBeforeIncompatiblePayload(t *testing.T) {
	for name, hello := range map[string]map[string]any{
		"hello 3":       {"kind": "hello", "version": 3, "build": "b", "serverStart": "new"},
		"hello 4":       {"kind": "hello", "version": 4, "build": "b", "serverStart": "new", "orderContract": "express-v1", "textEncoding": "order-text-base64-v1"},
		"hello 5":       {"kind": "hello", "version": 5, "build": "b", "serverStart": "new"},
		"hello 7":       {"kind": "hello", "version": 7, "build": "b", "serverStart": "new"},
		"hello 999":     {"kind": "hello", "version": 999, "build": "b", "serverStart": "new"},
		"current hello": {"kind": "hello", "version": session.StreamVersion, "build": "b", "serverStart": "new"},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_ = conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello))
				if hello["version"] == session.StreamVersion {
					_ = conn.Write(r.Context(), websocket.MessageBinary, []byte("future state format"))
				}
			}))
			defer server.Close()
			c := &Client{url: server.URL, http: server.Client(), build: "a", state: session.State{Epoch: "last-valid"}}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := c.receiveStream(ctx); err == nil {
				t.Fatal("accepted incompatible stream")
			}
			if !c.BuildChanged() || c.state.Epoch != "last-valid" {
				t.Fatal("lost build detection or last valid view")
			}
		})
	}
}
func TestStreamViewIsImmutableAndNoPolling(t *testing.T) {
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	handler := shared.HandlerFS(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/state" {
			t.Error("stream client polled HTTP state")
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go shared.Run(ctx)
	c := New(ctx, server.URL)
	waitFor(t, func() bool { _, ok, _ := c.View(); return ok })
	old, _, _ := c.View()
	before := streamJSON(t, old)
	shared.Apply(session.Command{Client: "stream-test", Sequence: 1, Epoch: old.Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
	waitFor(t, func() bool {
		now, ok, _ := c.View()
		return ok && now.Revision > old.Revision && now.Simulation.Submitted == 1
	})
	after := streamJSON(t, old)
	if !bytes.Equal(before, after) {
		t.Fatal("later publication changed retained view")
	}
	now, _, _ := c.View()
	if now.Simulation.Vehicles[0].Presentation == nil {
		t.Fatal("stream did not publish bounded presentation")
	}
	if &old.Network.Lanes[0] != &now.Network.Lanes[0] {
		t.Fatal("unchanged topology was recopied")
	}
}
func TestStreamRejectsRestartTopologyRace(t *testing.T) {
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	handler := shared.HandlerFS(nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/topology" {
			topology := shared.Topology()
			topology.ServerStart = "different"
			_ = json.NewEncoder(w).Encode(topology)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	c := &Client{url: server.URL, http: server.Client()}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err = c.receiveStream(ctx); err == nil || !strings.Contains(err.Error(), "topology") {
		t.Fatal("mixed restart identities", err)
	}
	if c.state.Epoch != "" {
		t.Fatal("rejected frame was published")
	}
}

func TestStreamBaselineAcceptsRestoredRetiredEpoch(t *testing.T) {
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	server := httptest.NewServer(shared.HandlerFS(nil))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := &Client{url: server.URL, http: server.Client(), state: session.State{ServerStart: "previous-process", Epoch: shared.Frame().Epoch, Revision: math.MaxUint64}}
	finished := make(chan error, 1)
	go func() { finished <- c.receiveStream(ctx) }()
	waitFor(t, func() bool {
		state, ok, _ := c.View()
		return ok && state.ServerStart == shared.Frame().ServerStart && state.Revision < math.MaxUint64
	})
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("stream worker did not stop")
	}
}

func TestStreamRejectsSameServerRollback(t *testing.T) {
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	server := httptest.NewServer(shared.HandlerFS(nil))
	defer server.Close()
	frame := shared.Frame()
	c := &Client{url: server.URL, http: server.Client(), state: session.State{ServerStart: frame.ServerStart, Epoch: frame.Epoch, Revision: math.MaxUint64}}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err = c.receiveStream(ctx); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatal("accepted same-server revision rollback", err)
	}
	if c.state.Revision != math.MaxUint64 {
		t.Fatal("rollback replaced retained view")
	}
}

func streamJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
