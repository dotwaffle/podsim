package remote

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// expressFullPublication returns the topology and a gzip full publication
// of an Express project.
func expressFullPublication(t *testing.T) (session.TopologySnapshot, session.StateFrame, []byte) {
	t.Helper()
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
		Frame json.RawMessage `json:"frame"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	state := shared.Frame()
	source := session.StreamSource{ServerStart: state.ServerStart, Epoch: state.Epoch, ProjectRevision: state.ProjectRevision, Generation: state.Generation, Revision: state.Revision}
	wire := map[string]any{"orderContract": sim.ExpressOrderContract, "kind": "full", "stream": "markers", "sequence": "1", "build": state.Build, "source": source, "full": envelope.Frame}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(streamJSON(t, wire)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return shared.Topology(), state, compressed.Bytes()
}

// The markers of the hello select the rules of the connection. The client
// refuses a publication or a topology with other markers.
func TestStreamMarkersMatchHello(t *testing.T) {
	t.Parallel()
	topology, state, publication := expressFullPublication(t)
	plainTopology := topology
	plainTopology.OrderContract, plainTopology.ExpressServices = "", nil
	for _, test := range []struct {
		name     string
		hello    sim.OrderContract
		topology session.TopologySnapshot
		want     string
	}{
		{"matching markers", sim.ExpressOrderContract, topology, ""},
		{"publication differs from hello", "", topology, "stream publication contract differs from hello"},
		{"topology differs from hello", sim.ExpressOrderContract, plainTopology, "topology contract differs from hello"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/topology" {
					_ = json.NewEncoder(w).Encode(test.topology)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = conn.CloseNow() }()
				hello := session.StreamHello{Kind: "hello", Version: session.StreamVersion, Build: state.Build, ServerStart: state.ServerStart, OrderContract: test.hello}
				if err := conn.Write(r.Context(), websocket.MessageText, streamJSON(t, hello)); err != nil {
					return
				}
				if err := conn.Write(r.Context(), websocket.MessageBinary, publication); err != nil {
					return
				}
				_, _, _ = conn.Read(r.Context())
			}))
			t.Cleanup(server.Close)
			client := &Client{url: server.URL, http: server.Client()}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if test.want == "" {
				go func() { _ = client.receiveStream(ctx) }()
				waitFor(t, func() bool { _, connected, _ := client.View(); return connected })
				return
			}
			err := client.receiveStream(ctx)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
			if _, connected, _ := client.View(); connected {
				t.Fatal("client published a refused state")
			}
		})
	}
}
