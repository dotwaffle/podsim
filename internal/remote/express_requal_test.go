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
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// expressRequalFrame returns the topology and the current frame of an
// Express session of config, as a stream full frame.
func expressRequalFrame(t *testing.T, config project.Config) (session.TopologySnapshot, session.StreamFrame) {
	t.Helper()
	shared, err := session.NewWithProject(config)
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
	wire := map[string]any{"orderContract": sim.ExpressOrderContract, "kind": "full", "stream": "requal", "sequence": "1", "source": source, "full": envelope.Frame}
	decoded, err := session.DecodeStreamJSON(streamJSON(t, wire))
	if err != nil {
		t.Fatal(err)
	}
	return shared.Topology(), *decoded.Full
}

// expressRequalMessage returns the gzip publication of e.
func expressRequalMessage(t *testing.T, e session.StreamEnvelope) []byte {
	t.Helper()
	raw, err := session.EncodeStreamJSON(e)
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
	return compressed.Bytes()
}

// TestExpressRequalRemoteRecovery runs the Go client against two Express
// connections. On the first, the client acknowledges a full frame and
// then refuses a delta after a sequence gap without an acknowledgement.
// It keeps the accepted state. On the second, a full frame of a new epoch
// on a new stream replaces the state after the client reads the topology
// of that epoch.
func TestExpressRequalRemoteRecovery(t *testing.T) {
	checkExpressRemoteRecovery(t, remoteExpressProject(t))
}

// checkExpressRemoteRecovery runs TestExpressRequalRemoteRecovery on the
// frames of an Express session of config.
func checkExpressRemoteRecovery(t *testing.T, config project.Config) {
	t.Helper()
	checkExpressRemoteRecoveryTo(t, config, nil)
}

// checkExpressRemoteRecoveryTo is checkExpressRemoteRecovery. With a nil
// next, the recovery frame is the first frame with a new epoch. Otherwise
// it is the frame of an Express session of next, with its topology.
func checkExpressRemoteRecoveryTo(t *testing.T, config project.Config, next *project.Config) {
	t.Helper()
	topology, frame := expressRequalFrame(t, config)
	recovered, recoveredTopology := frame, topology
	if next != nil {
		recoveredTopology, recovered = expressRequalFrame(t, *next)
		recovered.State.ServerStart, recoveredTopology.ServerStart = frame.State.ServerStart, frame.State.ServerStart
	}
	recovered.State.Epoch, recovered.State.Revision = "requal-recovered", 1
	recoveredTopology.Epoch = recovered.State.Epoch
	full := session.StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "first", Sequence: 1, Source: sourceOfFrame(frame), Build: frame.State.Build, Full: &frame}
	gap := session.StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "first", Sequence: 4, Base: 3, Source: sourceOfFrame(frame), Build: frame.State.Build, Delta: &session.StreamDelta{}}
	recovery := session.StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "second", Sequence: 1, Source: sourceOfFrame(recovered), Build: recovered.State.Build, Full: &recovered}
	connections := [][][]byte{
		{expressRequalMessage(t, full), expressRequalMessage(t, gap)},
		{expressRequalMessage(t, recovery)},
	}
	var connection, phase atomic.Int32
	// acks holds the acknowledged sequences of each connection, and -1
	// after a message that the client did not acknowledge.
	acks := make(chan []string, len(connections))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/topology" {
			current := topology
			if phase.Load() > 0 {
				current = recoveredTopology
			}
			_ = jsonv2.MarshalWrite(w, current, json.DefaultOptionsV1())
			return
		}
		index := int(connection.Add(1)) - 1
		conn, err := websocket.Accept(w, r, nil)
		if err != nil || index >= len(connections) {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		phase.Store(int32(index))
		hello := session.StreamHello{Kind: "hello", Version: session.StreamVersion, Build: frame.State.Build, ServerStart: frame.State.ServerStart, OrderContract: sim.ExpressOrderContract}
		if err = conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello)); err != nil {
			return
		}
		var got []string
		defer func() { acks <- got }()
		for _, message := range connections[index] {
			if err = conn.Write(r.Context(), websocket.MessageBinary, message); err != nil {
				return
			}
			// The timeouts guard against a hang. The client closes the
			// connection when it refuses a message, so a passing run does not
			// wait for them, and a full frame of the London Express preset
			// takes more than 2 s to decode under the race detector.
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			_, body, readErr := conn.Read(ctx)
			cancel()
			if readErr != nil {
				got = append(got, "-1")
				return
			}
			var ack struct {
				Kind     string `json:"kind"`
				Stream   string `json:"stream"`
				Sequence string `json:"sequence"`
			}
			if jsonv2.Unmarshal(body, &ack, json.DefaultOptionsV1()) != nil || ack.Kind != "ack" {
				got = append(got, "invalid")
				return
			}
			got = append(got, ack.Stream+"/"+ack.Sequence)
		}
	}))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client(), build: frame.State.Build}
	receive := func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		return client.receiveStream(ctx)
	}
	if err := receive(); err == nil || err.Error() != "delta base mismatch" {
		t.Fatalf("the first connection ended with %v, want the sequence gap", err)
	}
	if got := strings.Join(<-acks, ","); got != "first/1,-1" {
		t.Fatalf("first connection acknowledgements %s, want first/1,-1", got)
	}
	state, connected, _ := client.View()
	if !connected || state.Epoch != frame.State.Epoch || state.Simulation.OrderContract != sim.ExpressOrderContract {
		t.Fatalf("the refused delta changed the accepted state: connected %t, epoch %q", connected, state.Epoch)
	}
	if err := receive(); err == nil {
		t.Fatal("the second connection ended without an error")
	}
	if got := strings.Join(<-acks, ","); got != "second/1" {
		t.Fatalf("second connection acknowledgements %s, want second/1", got)
	}
	state, _, _ = client.View()
	if state.Epoch != recovered.State.Epoch || state.Simulation.OrderContract != sim.ExpressOrderContract || len(state.Network.Stations) != len(recoveredTopology.Network.Stations) {
		t.Fatalf("the recovery did not replace the state: epoch %q, marker %q", state.Epoch, state.Simulation.OrderContract)
	}
	if next != nil {
		checkReplacedTopology(t, state, topology, recoveredTopology, recovered)
	}
}

// checkReplacedTopology checks a state after a recovery to a different
// topology. The network of the state is the new network, each pod is the
// pod of the recovery frame and is on a lane, at a berth, or at a station
// of the new network, and the old network does not have some lane of the
// new network.
func checkReplacedTopology(t *testing.T, state session.State, old, next session.TopologySnapshot, recovered session.StreamFrame) {
	t.Helper()
	if !reflect.DeepEqual(state.Network, next.Network) {
		t.Fatalf("the client kept a network of %d lanes, want the new network of %d lanes", len(state.Network.Lanes), len(next.Network.Lanes))
	}
	places := func(n sim.Network) map[string]bool {
		ids := map[string]bool{}
		for _, lane := range n.Lanes {
			ids[lane.ID] = true
		}
		for _, station := range n.Stations {
			ids[station.ID] = true
			for _, berth := range station.Berths {
				ids[berth.ID] = true
			}
		}
		return ids
	}
	oldPlaces, nextPlaces := places(old.Network), places(next.Network)
	added := 0
	for _, lane := range next.Network.Lanes {
		if !oldPlaces[lane.ID] {
			added++
		}
	}
	if added == 0 {
		t.Fatal("the old network has each lane of the new network")
	}
	want, got := recovered.State.Simulation.Vehicles, state.Simulation.Vehicles
	if len(got) != len(want) {
		t.Fatalf("the client has %d pods, want the %d pods of the recovery frame", len(got), len(want))
	}
	for index, v := range got {
		if !reflect.DeepEqual(v.Pod, want[index].Pod) {
			t.Fatalf("pod %d is %+v, want the pod of the recovery frame %+v", index, v.Pod, want[index].Pod)
		}
		for _, place := range []string{v.Pod.LaneID, v.Pod.BerthID, v.Pod.StationID} {
			if place != "" && !nextPlaces[place] {
				t.Fatalf("pod %s is at %q, which the new network does not have", v.Pod.ID, place)
			}
		}
	}
	t.Logf("replaced %d lanes with %d lanes, %d of them new, with %d pods", len(old.Network.Lanes), len(next.Network.Lanes), added, len(got))
}

func sourceOfFrame(f session.StreamFrame) session.StreamSource {
	return session.StreamSource{ServerStart: f.State.ServerStart, Epoch: f.State.Epoch, ProjectRevision: f.State.ProjectRevision, Generation: f.State.Generation, Revision: f.State.Revision}
}
