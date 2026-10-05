package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func remoteCouplingFrame(t *testing.T, order sim.OrderContract) (session.TopologySnapshot, session.StreamFrame) {
	t.Helper()
	raw, err := os.ReadFile("../session/testdata/coupling_native_phases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Cohorts map[string]sim.RestoreStateInput `json:"cohorts"`
		Frames  []struct {
			Cohort string         `json:"cohort"`
			State  sim.SavedState `json:"state"`
		} `json:"frames"`
	}
	if decodeErr := json.Unmarshal(raw, &fixtures); decodeErr != nil { //nolint:musttag // Preserve the frozen native API fixture field names.
		t.Fatal(decodeErr)
	}
	input := fixtures.Cohorts["occupied"]
	found := false
	for _, frame := range fixtures.Frames {
		if frame.Cohort == "occupied" && frame.State.CouplingGroups[0].Phase == sim.CouplingConnected {
			input.State, found = frame.State, true
			break
		}
	}
	if !found {
		t.Fatal("missing committed occupied fixture")
	}
	input.OrderContract, input.State.OrderContract = order, order
	s, result, err := sim.RestoreState(input)
	if err != nil || result.Tier != sim.RestorePhysical {
		t.Fatal("fixture restore", err)
	}
	snapshot, routes, err := s.PresentationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	frame := session.StreamFrame{State: session.StateFrame{Epoch: "occupied", ServerStart: "coupling-source", Revision: 1, ProjectRevision: 1, Speed: 1}, Routes: routes}
	frame.State.Simulation = session.SimulationFrame{CouplingContract: snapshot.CouplingContract, CouplingEnabled: snapshot.CouplingEnabled,
		CouplingGroups: snapshot.CouplingGroups, OrderContract: snapshot.OrderContract, Tick: snapshot.Tick, Paused: snapshot.Paused, Berths: snapshot.Berths,
		Submitted: snapshot.Submitted, Completed: snapshot.Completed, SharedRidePartyLimit: snapshot.SharedRidePartyLimit}
	for _, cabin := range snapshot.Vehicles {
		frame.State.Simulation.Vehicles = append(frame.State.Simulation.Vehicles, session.VehicleFrame{Pod: cabin.Pod,
			Riders: cabin.Riders, Boardings: cabin.Boardings, RiddenMeters: cabin.RiddenMeters, CouplingID: cabin.CouplingID,
			Stops: cabin.Stops, RelocatingTo: cabin.RelocatingTo, Rebalancing: cabin.Rebalancing, PlatoonID: cabin.PlatoonID, PlatoonIndex: cabin.PlatoonIndex})
	}
	topology := session.TopologySnapshot{ProjectVersion: project.CurrentVersion, CouplingContract: input.CouplingContract,
		CouplingEnabled: input.CouplingEnabled, OrderContract: order, Network: input.Network, CouplingSites: input.CouplingSites,
		CouplingCorridors: input.CouplingCorridors, ServerStart: frame.State.ServerStart, Epoch: frame.State.Epoch, ProjectRevision: frame.State.ProjectRevision}
	return topology, frame
}

func TestCouplingRemoteHTTP(t *testing.T) {
	t.Parallel()
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			topology, frame := remoteCouplingFrame(t, order)
			raw, err := session.EncodeStateJSON(topology, frame)
			if err != nil {
				t.Fatal(err)
			}
			want, err := session.DecodeStateJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(strings.Join(r.Header.Values("Accept"), ","), session.StateMediaType) {
					t.Error("missing coupling media negotiation")
				}
				w.Header().Set("Content-Type", session.StateMediaType)
				_, _ = w.Write(raw)
			}))
			t.Cleanup(server.Close)
			client := &Client{url: server.URL, http: server.Client()}
			state := session.State{Epoch: "prior"}
			if err := client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state); err != nil || !reflect.DeepEqual(state, want) {
				t.Fatal("remote HTTP changed train facts", err)
			}
		})
	}
}

func TestCouplingRemoteHTTPRejectsUnqualifiedFields(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"couplingContract", "couplingEnabled", "couplingGroups", "couplingSites", "couplingCorridors", "couplingID"} {
		for _, literal := range []string{"null", "false", "[]"} {
			t.Run(name+literal, func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"epoch":"new","simulation":{"%s":%s}}`, name, literal)
				}))
				t.Cleanup(server.Close)
				client := &Client{url: server.URL, http: server.Client()}
				state := session.State{Epoch: "accepted"}
				if err := client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state); err == nil || state.Epoch != "accepted" {
					t.Fatal("unqualified train fields changed prior state", err)
				}
			})
		}
	}
}

func TestCouplingRemoteInvalidUpdateHasNoACK(t *testing.T) {
	t.Parallel()
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		for _, refusal := range []struct {
			name  string
			cause string
			edit  func(*session.StreamFrame)
		}{
			{"connector", "connector differs", func(f *session.StreamFrame) { f.State.Simulation.CouplingGroups[0].Connector.Corners[0].X++ }},
			{"missing registry", "no matching train registry", func(f *session.StreamFrame) { f.State.Simulation.CouplingGroups = nil }},
			{"detached close cabins", "lack ordinary separation", func(f *session.StreamFrame) {
				f.State.Simulation.CouplingGroups = nil
				for i := range f.State.Simulation.Vehicles {
					f.State.Simulation.Vehicles[i].CouplingID = ""
				}
			}},
		} {
			t.Run(string(order)+"/"+refusal.name, func(t *testing.T) {
				t.Parallel()
				topology, frame := remoteCouplingFrame(t, order)
				e := session.StreamEnvelope{CouplingContract: sim.CompactPairV1CouplingContract, OrderContract: order,
					Kind: "full", Stream: "occupied-stream", Sequence: 1, Source: session.StreamSource{ServerStart: frame.State.ServerStart, Epoch: frame.State.Epoch,
						ProjectRevision: frame.State.ProjectRevision, Revision: frame.State.Revision}, Full: &frame}
				encode := func(envelope session.StreamEnvelope) []byte {
					t.Helper()
					raw, err := session.EncodeStreamJSON(envelope)
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
					return buffer.Bytes()
				}
				valid := encode(e)
				frame.State.Revision++
				e.Sequence, e.Source.Revision = 2, frame.State.Revision
				refusal.edit(&frame)
				invalid := encode(e)
				acks := make(chan [2]bool, 1)
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
					hello := session.StreamHello{Kind: "hello", Version: session.StreamVersion, ServerStart: topology.ServerStart,
						CouplingContract: topology.CouplingContract, OrderContract: order}
					var acknowledged [2]bool
					defer func() { acks <- acknowledged }()
					if err := conn.Write(ctx, websocket.MessageText, streamJSON(t, hello)); err != nil {
						return
					}
					for i, payload := range [][]byte{valid, invalid} {
						if err := conn.Write(ctx, websocket.MessageBinary, payload); err != nil {
							return
						}
						kind, body, err := conn.Read(ctx)
						var control map[string]string
						acknowledged[i] = err == nil && kind == websocket.MessageText && json.Unmarshal(body, &control) == nil && control["kind"] == "ack"
						if err != nil {
							return
						}
					}
				}))
				t.Cleanup(server.Close)
				client := &Client{url: server.URL, http: server.Client(), state: session.State{Epoch: "prior"}}
				ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
				t.Cleanup(cancel)
				err := client.receiveStream(ctx)
				select {
				case acknowledged := <-acks:
					if !acknowledged[0] || acknowledged[1] {
						t.Fatal("valid/invalid ACK behavior changed", acknowledged)
					}
				case <-ctx.Done():
					t.Fatal("missing terminal ACK observation")
				}
				if err == nil || !strings.Contains(err.Error(), refusal.cause) {
					t.Fatal("invalid update did not reach semantic rejection", err)
				}
				if client.state.Epoch != topology.Epoch || client.state.Revision != 1 || len(client.state.Simulation.CouplingGroups) != 1 {
					t.Fatal("rejection replaced the accepted train frame")
				}
			})
		}
	}
}
