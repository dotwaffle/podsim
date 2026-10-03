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

func TestLegacyStreamRejectsServicePresenceBeforeTopology(t *testing.T) {
	t.Parallel()
	for _, version := range []int{1, 2} {
		for _, field := range []string{`"Class":null`, `"Class":"compact"`, `"Boardings":null`, `"bOaRdInGs":[]`, `"RiddenMeters":0`, `"rIdDeNmEtErS":null`} {
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
				if strings.Contains(strings.ToLower(field), "class") {
					raw = bytes.Replace(raw, []byte(`"Pod":{`), []byte(`"Pod":{`+field+`,`), 1)
				} else {
					raw = bytes.Replace(raw, []byte(`"Vehicles":[{`), []byte(`"Vehicles":[{`+field+`,`), 1)
				}
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

func TestBoardingTopologyCacheRollbackAndInvalidation(t *testing.T) {
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	topology.ProjectVersion = 3
	frame := session.StreamFrame{State: shared.Frame()}
	frame.Routes = make([]sim.RoutePresentation, len(frame.State.Simulation.Vehicles))
	for i := range frame.Routes {
		frame.Routes[i].Origin = -1
	}
	v := &frame.State.Simulation.Vehicles[0]
	v.Riders = []sim.Request{{ID: 1, From: topology.Network.Stations[0].ID, To: topology.Network.Stations[1].ID, PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}}
	berth := topology.Network.Stations[0].Berths[0].ID
	v.Boardings = []sim.RiderBoarding{{BerthID: berth}}
	var fetches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fetches++; _ = json.NewEncoder(w).Encode(topology) }))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client()}
	cache := streamTopology{version: 3}
	accepted, err := cache.state(t.Context(), client, frame)
	if err != nil {
		t.Fatal(err)
	}
	first := cache.assembler
	topology.ProjectRevision++
	frame.State.ProjectRevision++
	v.Boardings[0].BerthID = "missing"
	if _, err := cache.state(t.Context(), client, frame); err == nil {
		t.Fatal("accepted invalid new topology candidate")
	}
	if cache.assembler != first || cache.topology.ProjectRevision == topology.ProjectRevision {
		t.Fatal("rejection published cache binding")
	}
	if accepted.Simulation.Vehicles[0].Boardings[0].BerthID != berth {
		t.Fatal("candidate changed accepted state")
	}
	v.Boardings[0].BerthID = berth
	v.Pod.Class = sim.CompactClass
	if _, err := cache.state(t.Context(), client, frame); err != nil {
		t.Fatal("new revision retained old class binding", err)
	}
	if cache.assembler == first {
		t.Fatal("new revision retained old assembler")
	}
	reconnected := streamTopology{version: 3}
	v.Pod.Class = sim.LegacyClass
	if _, err := reconnected.state(t.Context(), client, frame); err != nil {
		t.Fatal("reconnect retained old class binding", err)
	}
	if fetches != 4 {
		t.Fatalf("topology fetches %d, want 4", fetches)
	}
}
