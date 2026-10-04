package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
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

func TestStreamBankProtocolVersions(t *testing.T) {
	t.Parallel()
	for _, version := range []int{1, 2, 3, 4} {
		for _, banks := range []string{"omitted", "valid", "empty", "null"} {
			t.Run(fmt.Sprintf("hello%d/%s", version, banks), func(t *testing.T) {
				t.Parallel()
				shared, err := session.New()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(shared.Close)
				topology := shared.Topology()
				if banks == "valid" {
					topology.Network = sim.BankExample()
				}
				topologyJSON := streamJSON(t, topology)
				if banks == "empty" || banks == "null" {
					value := "[]"
					if banks == "null" {
						value = "null"
					}
					topologyJSON = bytes.Replace(topologyJSON, []byte(`"Stations":[{`), []byte(`"Stations":[{"Banks":`+value+`,`), 1)
				}
				state := shared.Frame()
				state.Simulation.Vehicles = nil
				state.Simulation.Berths = nil
				frame := session.StreamFrame{State: state, Routes: make([]sim.RoutePresentation, len(state.Simulation.Vehicles))}
				for index := range frame.Routes {
					frame.Routes[index].Origin = -1
				}
				envelope := session.StreamEnvelope{
					Kind: "full", Stream: "banks", Sequence: 1, Full: &frame, Build: state.Build,
					Source: session.StreamSource{ServerStart: state.ServerStart, Epoch: state.Epoch, ProjectRevision: state.ProjectRevision, Generation: state.Generation, Revision: state.Revision},
				}
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				if _, writeErr := writer.Write(streamJSON(t, envelope)); writeErr != nil {
					t.Fatal(writeErr)
				}
				if closeErr := writer.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
				var fetches atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/topology" {
						fetches.Add(1)
						_, _ = w.Write(topologyJSON)
						return
					}
					conn, acceptErr := websocket.Accept(w, r, nil)
					if acceptErr != nil {
						return
					}
					defer func() { _ = conn.CloseNow() }()
					hello := map[string]any{"kind": "hello", "version": version, "build": state.Build, "serverStart": state.ServerStart}
					if writeErr := conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello)); writeErr != nil {
						return
					}
					if writeErr := conn.Write(r.Context(), websocket.MessageBinary, compressed.Bytes()); writeErr != nil {
						return
					}
					_, _, _ = conn.Read(r.Context())
				}))
				t.Cleanup(server.Close)
				client := &Client{url: server.URL, http: server.Client()}
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				err = client.receiveStream(ctx)
				valid := version == session.FoundationStreamVersion && (banks == "omitted" || banks == "valid")
				_, connected, _ := client.View()
				if connected != valid {
					t.Fatalf("published=%t want=%t: %v", connected, valid, err)
				}
				// The client rejects hello 1 and 2 from servers older than
				// the version 3 service fields.
				if version < session.FoundationStreamVersion && (err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unsupported state stream version %d", version))) {
					t.Fatalf("wrong rejection: %v", err)
				}
				if version != session.FoundationStreamVersion && fetches.Load() != 0 {
					t.Fatal("unsupported hello fetched topology")
				}
			})
		}
	}
}

func TestStreamTopologyRejectsUnknownBankMembers(t *testing.T) {
	t.Parallel()
	var topology session.TopologySnapshot
	if err := json.Unmarshal([]byte(`{"network":{"Stations":[{"Banks":[{"extra":1}]}]}}`), &topology); err == nil {
		t.Fatal("accepted unknown bank member")
	}
}
