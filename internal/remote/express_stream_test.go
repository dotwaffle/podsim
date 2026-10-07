package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestExpressRemoteStream(t *testing.T) {
	shared, err := session.NewWithProject(remoteExpressProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	server := httptest.NewServer(shared.HandlerFS(nil))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go shared.Run(ctx)
	client := New(ctx, server.URL)
	waitFor(t, func() bool {
		state, ok, _ := client.View()
		return ok && state.Simulation.OrderContract == sim.ExpressOrderContract
	})
	if err = client.Submit(session.Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 20, SharingConsent: sim.SharedConsent}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		state, ok, _ := client.View()
		if !ok || state.Simulation.Submitted != 1 {
			return false
		}
		for _, r := range state.Simulation.Pending {
			if r.PartySize == 20 {
				return true
			}
		}
		for _, v := range state.Simulation.Vehicles {
			for _, r := range v.Riders {
				if r.PartySize == 20 {
					return true
				}
			}
		}
		return false
	})
}

func TestExpressRemoteInvalidStateHasNoACK(t *testing.T) {
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
		Frame jsontext.Value `json:"frame"`
	}
	if err = jsonv2.Unmarshal(response.Body.Bytes(), &envelope, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	state := shared.Frame()
	source := session.StreamSource{ServerStart: state.ServerStart, Epoch: state.Epoch, ProjectRevision: state.ProjectRevision, Generation: state.Generation, Revision: state.Revision}
	wire := map[string]any{"orderContract": sim.ExpressOrderContract, "kind": "full", "stream": "test-invalid", "sequence": "1", "source": source, "full": envelope.Frame}
	decoded, err := session.DecodeStreamJSON(streamJSON(t, wire))
	if err != nil {
		t.Fatal(err)
	}
	decoded.Full.State.Simulation.Pending = []sim.Request{{ID: 1, From: "harbor", To: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "absent-registry-entry"}}
	raw, err := session.EncodeStreamJSON(decoded)
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
	ack := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/topology" {
			_ = jsonv2.MarshalWrite(w, shared.Topology(), json.DefaultOptionsV1())
			return
		}
		conn, acceptErr := websocket.Accept(w, r, nil)
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		hello := session.StreamHello{Kind: "hello", Version: session.StreamVersion, Build: "express-review", ServerStart: state.ServerStart, OrderContract: sim.ExpressOrderContract}
		if writeErr := conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello)); writeErr != nil {
			return
		}
		if writeErr := conn.Write(r.Context(), websocket.MessageBinary, compressed.Bytes()); writeErr != nil {
			return
		}
		readCtx, readCancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer readCancel()
		_, body, readErr := conn.Read(readCtx)
		ack <- readErr == nil && len(body) > 0
	}))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client(), state: session.State{Epoch: "accepted"}, build: "old-build"}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err = client.receiveStream(ctx); err == nil {
		t.Fatal("accepted invalid Express publication")
	}
	if !strings.Contains(err.Error(), "unknown Express service or directed pair") {
		t.Fatal("rejected before qualified semantic assembly", err)
	}
	if client.state.Epoch != "accepted" || !client.BuildChanged() {
		t.Fatal("rejection changed accepted state or hid build change")
	}
	select {
	case acknowledged := <-ack:
		if acknowledged {
			t.Fatal("invalid state acknowledged")
		}
	case <-ctx.Done():
		t.Fatal("no terminal acknowledgement observation")
	}
}
