package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestLegacyStreamRejectsServicePresenceBeforeTopology(t *testing.T) {
	t.Parallel()
	for _, version := range []int{1, 2} {
		for _, field := range []string{`"Class":null`, `"Class":"compact"`} {
			t.Run(fmt.Sprintf("hello%d/%s", version, field), func(t *testing.T) {
				t.Parallel()
				shared, err := session.New()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(shared.Close)
				state := shared.Frame()
				routes := make([]sim.RoutePresentation, len(state.Simulation.Vehicles))
				for i := range routes {
					routes[i].Origin = -1
				}
				frame := session.StreamFrame{State: state, Routes: routes}
				envelope := session.StreamEnvelope{Kind: "full", Stream: "legacy", Sequence: 1, Full: &frame,
					Source: session.StreamSource{ServerStart: state.ServerStart, Epoch: state.Epoch, ProjectRevision: state.ProjectRevision, Generation: state.Generation, Revision: state.Revision}}
				raw, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte(`"Pod":{`), []byte(`"Pod":{`+field+`,`), 1)
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				if _, err := writer.Write(raw); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				var fetches atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/topology" {
						fetches.Add(1)
						topology := shared.Topology()
						topology.ProjectVersion = 0
						_ = json.NewEncoder(w).Encode(topology)
						return
					}
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer func() { _ = conn.CloseNow() }()
					hello := map[string]any{"kind": "hello", "version": version, "serverStart": state.ServerStart}
					if err := conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello)); err != nil {
						return
					}
					if err := conn.Write(r.Context(), websocket.MessageBinary, compressed.Bytes()); err != nil {
						return
					}
					_, _, _ = conn.Read(r.Context())
				}))
				t.Cleanup(server.Close)
				client := &Client{url: server.URL, http: server.Client()}
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				if err := client.receiveStream(ctx); err == nil {
					t.Fatal("accepted service metadata in a legacy stream")
				}
				if fetches.Load() != 0 || client.connected || client.state.Epoch != "" {
					t.Fatal("rejected service fields fetched or published state")
				}
			})
		}
	}
}
