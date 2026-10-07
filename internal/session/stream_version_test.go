package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/sim"
)

// A server of another version sends a hello of another version. The
// client refuses it, and still gets the build of the server, so that it can
// reload to the files of that server.
func TestStreamHelloRefusesOtherVersions(t *testing.T) {
	t.Parallel()
	express := sim.ExpressOrderContract
	var hellos []string
	for _, version := range []int{1, 2, 3, 4, 5, 7} {
		hellos = append(hellos, fmt.Sprintf(`{"kind":"hello","version":%d,"build":"other-build","serverStart":"source"}`, version))
	}
	hellos = append(hellos,
		// Hello 4 and 5 of an Express project had the textEncoding marker.
		`{"kind":"hello","version":4,"build":"other-build","serverStart":"source","orderContract":"`+string(express)+`","textEncoding":"order-text-base64-v1"}`,
		`{"kind":"hello","version":5,"build":"other-build","serverStart":"source","orderContract":"`+string(express)+`","textEncoding":"order-text-base64-v1"}`,
		// Hello 6 has no textEncoding marker.
		`{"kind":"hello","version":6,"build":"other-build","serverStart":"source","orderContract":"`+string(express)+`","textEncoding":"order-text-base64-v1"}`,
	)
	for _, raw := range hellos {
		hello, err := DecodeStreamHello([]byte(raw))
		if err == nil {
			t.Errorf("accepted %s", raw)
		}
		if hello.Build != "other-build" {
			t.Errorf("lost the build of %s: %q", raw, hello.Build)
		}
	}
	for _, order := range []sim.OrderContract{"", express} {
		want := StreamHello{Kind: "hello", Version: StreamVersion, Build: "build", ServerStart: "source", OrderContract: order}
		raw, err := jsonv2.Marshal(want, json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		if hello, err := DecodeStreamHello(raw); err != nil || hello != want {
			t.Errorf("hello %s: %+v, %v", raw, hello, err)
		}
	}
}

// The server sends hello 6 with the markers of its project and no other
// member.
func TestStreamHelloBytes(t *testing.T) {
	t.Parallel()
	plain, _ := streamFixture(t)
	t.Cleanup(plain.Close)
	for _, test := range []struct {
		name   string
		shared *Session
		order  sim.OrderContract
	}{{"plain", plain, ""}, {"express", expressSession(t), sim.ExpressOrderContract}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(test.shared.HandlerFS(fstest.MapFS{}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.CloseNow() }()
			kind, raw, err := conn.Read(ctx)
			if err != nil || kind != websocket.MessageText {
				t.Fatal("hello publication", err)
			}
			want := fmt.Sprintf(`{"kind":"hello","version":6,"build":%q,"serverStart":%q}`, test.shared.build, test.shared.serverStart)
			if test.order != "" {
				want = strings.TrimSuffix(want, "}") + fmt.Sprintf(`,"orderContract":%q}`, test.order)
			}
			if string(raw) != want {
				t.Fatalf("hello bytes: got %s, want %s", raw, want)
			}
		})
	}
}

// markerSectionRequests returns an order that the assembler accepts in the
// frames of each kind of streamFamilyFrames.
func markerSectionRequest(family string, id int) sim.Request {
	switch family {
	case "express":
		return sim.Request{ID: id, From: "harbor", To: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"}
	default:
		return sim.Request{ID: id, From: "harbor", To: "market", PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
	}
}

// TestStreamMarkersSelectSections round-trips the stream and the HTTP
// state of a plain and an Express project: a full frame, a delta that replaces the pending orders, a delta
// that replaces the riders of a vehicle, and the HTTP state. The assembler
// accepts each frame. Each document carries the order text packed, and
// only the markers of its project.
func TestStreamMarkersSelectSections(t *testing.T) {
	t.Parallel()
	for family, fixture := range markerSectionFixtures(t) {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			base := ownStreamBoardings(fixture.frame)
			request := markerSectionRequest(family, 1_000_001)
			pending := base
			pending.State.Revision++
			pending.State.Simulation.Pending = append(slices.Clip(pending.State.Simulation.Pending), request)
			riders := ownStreamBoardings(pending)
			riders.State.Revision++
			riders.State.Simulation.Pending = base.State.Simulation.Pending
			if len(riders.State.Simulation.Vehicles) == 0 {
				t.Fatal("fixture has no vehicle")
			}
			rider := request
			rider.PodID = riders.State.Simulation.Vehicles[0].Pod.ID
			vehicles := slices.Clone(riders.State.Simulation.Vehicles)
			vehicle := &vehicles[0]
			if n := len(vehicle.Riders); n > 0 {
				// The new rider replaces the last rider. A private party
				// rides alone, and the boardings stay valid.
				vehicle.Riders = append(slices.Clone(vehicle.Riders[:n-1]), rider)
			} else {
				vehicle.Riders = []sim.Request{rider}
				if len(vehicle.Boardings) > 0 {
					vehicle.Boardings = append(slices.Clip(vehicle.Boardings), sim.RiderBoarding{BerthID: vehicle.Boardings[0].BerthID})
				}
			}
			riders.State.Simulation.Vehicles = vehicles
			markers := contractMarkers{order: base.State.Simulation.OrderContract}
			packedFrom := `"from":"` + base64.StdEncoding.EncodeToString([]byte(request.From)) + `"`

			full := fullStreamEnvelope(base)
			previous := StreamFrame{}
			assembler, err := NewStreamAssembler(fixture.topology)
			if err != nil {
				t.Fatal(err)
			}
			for i, next := range []StreamFrame{base, pending, riders} {
				e := full
				if i > 0 {
					delta, err := makeDelta(previous, next)
					if err != nil {
						t.Fatal(err)
					}
					e.Kind, e.Full, e.Delta, e.Sequence, e.Base, e.Source = "delta", nil, &delta, uint64(i+1), uint64(i), sourceOf(next)
				}
				raw, err := EncodeStreamJSON(e)
				if err != nil {
					t.Fatal(i, err)
				}
				assertMarkerSections(t, raw, markers)
				if i > 0 && !bytes.Contains(raw, []byte(packedFrom)) {
					t.Fatalf("publication %d does not pack the order text: %s", i, raw)
				}
				decoded, err := DecodeStreamJSON(raw)
				if err != nil {
					t.Fatal(i, err)
				}
				if decoded.OrderContract != markers.order {
					t.Fatalf("publication %d marker: %q", i, decoded.OrderContract)
				}
				accepted, err := ApplyStream(previous, e.Stream, uint64(i), decoded)
				if err != nil {
					t.Fatal(i, err)
				}
				if !reflect.DeepEqual(accepted, ownStreamBoardings(next)) {
					t.Fatalf("publication %d changed the frame", i)
				}
				if _, err := assembler.State(accepted); err != nil {
					t.Fatalf("publication %d: %v", i, err)
				}
				previous = accepted
			}

			topology := fixture.topology
			for i, frame := range []StreamFrame{pending, riders} {
				raw, err := EncodeStateJSON(topology, frame)
				if err != nil {
					t.Fatal("HTTP state", i, err)
				}
				assertMarkerSections(t, raw, markers)
				if !bytes.Contains(raw, []byte(packedFrom)) {
					t.Fatalf("HTTP state %d does not pack the order text", i)
				}
				state, err := DecodeStateJSON(raw)
				if err != nil {
					t.Fatal("HTTP state", i, err)
				}
				assembler, err := NewStreamAssembler(topology)
				if err != nil {
					t.Fatal(err)
				}
				want, err := assembler.State(frame)
				if err != nil || !reflect.DeepEqual(state, want) {
					t.Fatalf("HTTP state %d changed the state: %v", i, err)
				}
			}
		})
	}
}

// assertMarkerSections checks the root markers of a stream document or an
// HTTP state, and that it has no textEncoding marker.
func assertMarkerSections(t *testing.T, raw []byte, markers contractMarkers) {
	t.Helper()
	got, err := scanRootMarkers(raw)
	if err != nil || got != markers {
		t.Fatalf("root markers %+v, want %+v: %v", got, markers, err)
	}
	if bytes.Contains(raw, []byte(`"textEncoding"`)) {
		t.Fatal("document has a text encoding marker")
	}
}

// markerSectionFixture is a frame and the topology of its session.
type markerSectionFixture struct {
	topology TopologySnapshot
	frame    StreamFrame
}

// markerSectionFixtures returns a fixture of each kind of
// streamFamilyFrames.
func markerSectionFixtures(t *testing.T) map[string]markerSectionFixture {
	t.Helper()
	fixtures := map[string]markerSectionFixture{}
	plain, frame := streamFixture(t)
	t.Cleanup(plain.Close)
	fixtures["foundation"] = markerSectionFixture{plain.Topology(), frame}
	express := expressSession(t)
	frame, err := express.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	fixtures["express"] = markerSectionFixture{express.Topology(), frame}
	return fixtures
}

// TestStreamGoldenBytes pins one full and one delta publication of a
// plain and an Express project. The source, build and order
// text are fixed, so the bytes do not depend on the session. Run the test
// with -update to write the files again.
func TestStreamGoldenBytes(t *testing.T) {
	t.Parallel()
	families := streamFamilyFrames(t)
	for _, family := range []struct{ name, frames string }{{"plain", "foundation"}, {"express", "express"}} {
		frames := families[family.frames]
		for i := range frames {
			frame := ownStreamBoardings(frames[i])
			frame.State.ServerStart, frame.State.Epoch, frame.State.Build = "golden-start", "golden-epoch", "golden-build"
			frame.State.Simulation.Pending = []sim.Request{markerSectionRequest(family.frames, 1)}
			frames[i] = frame
		}
		for _, kind := range []string{"full", "delta"} {
			t.Run(family.name+"/"+kind, func(t *testing.T) {
				t.Parallel()
				e := streamFamilyEnvelope(t, frames, kind)
				raw, err := EncodeStreamJSON(e)
				if err != nil {
					t.Fatal(err)
				}
				path := "testdata/stream_" + family.name + "_" + kind + ".json"
				if *update {
					value := jsontext.Value(bytes.Clone(raw))
					if indentErr := value.Indent(jsontext.WithIndent("  ")); indentErr != nil {
						t.Fatal(indentErr)
					}
					if writeErr := os.WriteFile(path, append(value, '\n'), 0o600); writeErr != nil {
						t.Fatal(writeErr)
					}
				}
				golden, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want := jsontext.Value(golden)
				if err := want.Compact(); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(raw, want) {
					t.Fatalf("%s differs from the encoder output", path)
				}
				if _, err := DecodeStreamJSON(want); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
