package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestExpressFixedStreamFamilyConsumers(t *testing.T) {
	topology, frame := expressGuardFrame(t)
	rawTopology, err := json.Marshal(topology)
	if err != nil {
		t.Fatal(err)
	}
	var decodedTopology TopologySnapshot
	if err = json.Unmarshal(rawTopology, &decodedTopology); err != nil {
		t.Fatal("Express topology", err)
	}
	assembler, err := NewStreamAssembler(decodedTopology)
	if err != nil {
		t.Fatal(err)
	}
	previous := StreamFrame{}
	for sequence := uint64(1); sequence <= 2; sequence++ {
		envelope := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Stream: "fixed-family", Sequence: sequence, Source: sourceOf(frame), Build: frame.State.Build}
		if sequence == 1 {
			envelope.Kind, envelope.Full = "full", &frame
		} else {
			delta, deltaErr := makeDelta(previous, frame)
			if deltaErr != nil {
				t.Fatal(deltaErr)
			}
			envelope.Kind, envelope.Base, envelope.Delta = "delta", sequence-1, &delta
		}
		raw, encodeErr := EncodeStreamJSON(envelope)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		decoded, decodeErr := DecodeStreamJSONVersion(raw, 4)
		if decodeErr != nil {
			t.Fatal("hello4 publication", decodeErr)
		}
		reencoded, encodeErr := EncodeStreamJSON(decoded)
		if encodeErr != nil || !bytes.Equal(raw, reencoded) {
			t.Fatal("hello4 publication bytes changed", encodeErr)
		}
		accepted, applyErr := ApplyStream(previous, envelope.Stream, sequence-1, decoded)
		if applyErr != nil || !reflect.DeepEqual(accepted, frame) {
			t.Fatal("hello4 full/delta facts changed", applyErr)
		}
		native, stateErr := assembler.State(accepted)
		if stateErr != nil {
			t.Fatal("hello4 assembly", stateErr)
		}
		httpRaw, httpErr := EncodeExpressStateJSON(decodedTopology, accepted)
		if httpErr != nil {
			t.Fatal("Express HTTP encoding", httpErr)
		}
		httpState, httpErr := DecodeExpressStateJSON(httpRaw)
		if httpErr != nil || !reflect.DeepEqual(httpState, native) {
			t.Fatal("Express HTTP facts changed", httpErr)
		}
		previous = frame
		frame.State.Revision++
		frame.State.Simulation.Tick++
	}
}

func TestExpressFixedStreamFamilyHello(t *testing.T) {
	shared := expressSession(t)
	server := httptest.NewServer(shared.HandlerFS(fstest.MapFS{}))
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
		t.Fatal("Express hello publication", err)
	}
	want, err := json.Marshal(StreamHello{Kind: "hello", Version: 4, Build: shared.build, ServerStart: shared.serverStart, OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding})
	if err != nil || !bytes.Equal(raw, want) {
		t.Fatalf("hello4 bytes changed: %s, %v", raw, err)
	}
	hello, err := DecodeStreamHello(raw)
	if err != nil || hello.Version != 4 {
		t.Fatal("hello4 negotiation", err)
	}
}

func TestStreamFutureFamilyRejected(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"hello","version":5,"serverStart":"source"}`,
		`{"kind":"hello","version":5,"serverStart":"source","orderContract":"express-v1","textEncoding":"order-text-base64-v1"}`,
	} {
		if _, err := DecodeStreamHello([]byte(raw)); err == nil {
			t.Fatal("accepted unqualified hello5", raw)
		}
	}
	if _, err := DecodeStreamJSONVersion([]byte(`{}`), 5); err == nil {
		t.Fatal("accepted unqualified stream5 publication")
	}
	if _, err := NewStreamAssemblerVersion(TopologySnapshot{}, 5); err == nil {
		t.Fatal("accepted unqualified stream5 assembler")
	}
}

// Servers older than the version 3 service fields sent hello 1 or 2. The
// client rejects those families at each entry point.
func TestStreamLegacyFamiliesRejected(t *testing.T) {
	t.Parallel()
	shared, frame := streamFixture(t)
	raw, err := EncodeStreamJSON(StreamEnvelope{Kind: "full", Stream: "s", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeStreamJSONVersion(raw, FoundationStreamVersion); err != nil {
		t.Fatal("hello3 publication", err)
	}
	if _, err = NewStreamAssemblerVersion(shared.Topology(), FoundationStreamVersion); err != nil {
		t.Fatal("hello3 assembler", err)
	}
	for version := 1; version < FoundationStreamVersion; version++ {
		hello := fmt.Appendf(nil, `{"kind":"hello","version":%d,"serverStart":"source"}`, version)
		if _, err := DecodeStreamHello(hello); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unsupported state stream version %d", version)) {
			t.Errorf("hello%d negotiation: %v", version, err)
		}
		if _, err := DecodeStreamJSONVersion(raw, version); err == nil {
			t.Errorf("stream%d accepted a publication", version)
		}
		if _, err := NewStreamAssemblerVersion(shared.Topology(), version); err == nil {
			t.Errorf("stream%d accepted an assembler", version)
		}
	}
}
