package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCouplingStreamHelloAndPublication(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		for _, enabled := range []bool{false, true} {
			for _, empty := range []bool{false, true} {
				t.Run(fmt.Sprintf("order=%s/enabled=%t/empty=%t", order, enabled, empty), func(t *testing.T) {
					t.Parallel()
					input := couplingPhaseInput(t, data, data.Frames[0])
					config := couplingProject(input)
					config.OrderContract, config.CouplingEnabled = order, enabled
					if empty {
						config.CouplingSites, config.CouplingCorridors = nil, nil
					}
					s, err := NewWithProject(config)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(s.Close)
					server := httptest.NewServer(s.HandlerFS(nil))
					t.Cleanup(server.Close)
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					t.Cleanup(cancel)
					conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
					if response != nil && response.Body != nil {
						_ = response.Body.Close()
					}
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = conn.CloseNow() })
					kind, raw, err := conn.Read(ctx)
					if err != nil || kind != websocket.MessageText {
						t.Fatal("hello publication", err)
					}
					hello, err := DecodeStreamHello(raw)
					if err != nil || hello.Version != CouplingStreamVersion || hello.CouplingContract != sim.CompactPairV1CouplingContract ||
						hello.OrderContract != order || hello.TextEncoding != streamTextEncoding(order) {
						t.Fatal("coupling hello changed independent contracts", err)
					}
					kind, raw, err = conn.Read(ctx)
					if err != nil || kind != websocket.MessageBinary {
						t.Fatal("full publication", err)
					}
					inflated, err := InflateStream(raw)
					if err != nil {
						t.Fatal(err)
					}
					envelope, err := DecodeStreamJSONVersion(inflated, hello.Version)
					if err != nil || envelope.CouplingContract != hello.CouplingContract {
						t.Fatal("publication lost negotiated train contract", err)
					}
					frame, err := ApplyStream(StreamFrame{}, "", 0, envelope)
					if err != nil || frame.State.Simulation.CouplingEnabled != enabled {
						t.Fatal("full lost recruitment setting", err)
					}
					assembler, err := NewStreamAssemblerVersion(s.Topology(), hello.Version)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := assembler.State(frame); err != nil {
						t.Fatal("published frame rejected", err)
					}
					for _, accept := range []string{"", ExpressMediaType, CouplingMediaType} {
						request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/state", http.NoBody)
						request.Header.Set("Accept", accept)
						response := httptest.NewRecorder()
						s.HandlerFS(nil).ServeHTTP(response, request)
						if accept != CouplingMediaType {
							if response.Code != http.StatusNotAcceptable {
								t.Fatal("unqualified HTTP accepted trains", response.Code)
							}
							continue
						}
						if response.Code != http.StatusOK || response.Header().Get("Content-Type") != CouplingMediaType {
							t.Fatal("qualified HTTP rejected", response.Code, response.Body.String())
						}
						state, err := DecodeCouplingStateJSON(response.Body.Bytes())
						if err != nil || state.Simulation.CouplingEnabled != enabled || state.Simulation.CouplingContract != hello.CouplingContract {
							t.Fatal("HTTP lost qualified empty/off train facts", err)
						}
					}
				})
			}
		}
	}
}

func TestCouplingPublisherFullDeltaAndResync(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			s, _, original := couplingStreamFixture(t, data.Frames[0], order)
			p := &statePublisher{session: s, clients: map[*streamSubscriber]bool{}}
			if err := p.publish(t.Context(), true, true); err != nil {
				t.Fatal(err)
			}
			decode := func(payload *streamPayload) StreamEnvelope {
				t.Helper()
				inflated, err := InflateStream(payload.data)
				if err != nil {
					t.Fatal(err)
				}
				e, err := DecodeStreamJSONVersion(inflated, CouplingStreamVersion)
				if err != nil {
					t.Fatal(err)
				}
				if e.CouplingContract != sim.CompactPairV1CouplingContract {
					t.Fatal("publisher lost coupling marker")
				}
				return e
			}
			full := decode(p.full)
			previous, err := ApplyStream(StreamFrame{}, "", 0, full)
			if err != nil || !reflect.DeepEqual(previous, original) {
				t.Fatal("publisher full differs", err)
			}
			newTestClient(s, "publisher").mustApply(t, Command{Action: "pause", Paused: false})
			s.advance()
			if publishErr := p.publish(t.Context(), false, true); publishErr != nil {
				t.Fatal(publishErr)
			}
			if len(p.history) != 1 {
				t.Fatal("publisher failed to retain delta")
			}
			delta := decode(p.history[0])
			if _, exists := delta.Delta.Groups["coupling"]; !exists {
				t.Fatal("publisher omitted coherent train replacement")
			}
			current, err := ApplyStream(previous, full.Stream, full.Sequence, delta)
			if err != nil || !reflect.DeepEqual(current, p.frame) {
				t.Fatal("publisher delta differs", err)
			}
			if publishErr := p.publish(t.Context(), true, false); publishErr != nil {
				t.Fatal(publishErr)
			}
			resync := decode(p.full)
			recovered, err := ApplyStream(StreamFrame{}, "", 0, resync)
			if err != nil || !reflect.DeepEqual(recovered, current) {
				t.Fatal("resync full differs", err)
			}
			if !bytes.Equal(mustCouplingJSON(t, previous), mustCouplingJSON(t, original)) {
				t.Fatal("publisher mutated accepted baseline")
			}
		})
	}
}

func TestCouplingHelloRequiresQualifiedMarkers(t *testing.T) {
	t.Parallel()
	for version := 1; version <= CouplingStreamVersion; version++ {
		for _, literal := range []string{"null", "false", "[]", `"unknown"`} {
			raw := fmt.Appendf(nil, `{"kind":"hello","version":%d,"serverStart":"source","COUPLINGCONTRACT":%s}`, version, literal)
			if _, err := DecodeStreamHello(raw); err == nil {
				t.Fatal("invalid hello marker accepted", version, literal)
			}
		}
	}
	p := statePublisher{sequence: 1, frame: StreamFrame{State: StateFrame{Simulation: SimulationFrame{CouplingContract: sim.CompactPairV1CouplingContract}}}}
	subscriber := streamSubscriber{}
	if err := p.sendAvailable(t.Context(), &subscriber); err == nil || subscriber.bytes != 0 || len(subscriber.sent) != 0 {
		t.Fatal("old subscriber received coupling bytes or acquired credit")
	}
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		hello := StreamHello{Kind: "hello", Version: CouplingStreamVersion, ServerStart: "source", CouplingContract: sim.CompactPairV1CouplingContract,
			OrderContract: order, TextEncoding: streamTextEncoding(order)}
		raw, err := json.Marshal(hello)
		if err != nil {
			t.Fatal(err)
		}
		if decoded, err := DecodeStreamHello(raw); err != nil || decoded != hello {
			t.Fatal("qualified hello rejected", err)
		}
	}
}
