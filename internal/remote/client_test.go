package remote

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStateForFrameCachesMatchingTopology(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/topology" {
			t.Errorf("path = %q", r.URL.Path)
		}
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(shared.Topology())
	}))
	defer server.Close()
	client := &Client{url: server.URL, http: server.Client()}
	cache := testStreamCache(t, shared.Topology())
	if _, err := testStreamState(t, &cache, client, shared.Frame()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("fetched unchanged topology")
	}
	cache = streamTopology{}
	if _, err := testStreamState(t, &cache, client, shared.Frame()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("topology requests = %d", requests.Load())
	}
}

func TestStateForFrameRefetchesTopologyAfterProjectRestore(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/topology" {
			t.Errorf("path = %q", r.URL.Path)
		}
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(shared.Topology())
	}))
	defer server.Close()
	client := &Client{url: server.URL, http: server.Client()}
	cache := testStreamCache(t, shared.Topology())
	epoch, sequence := shared.Frame().Epoch, uint64(0)
	apply := func(command session.Command) session.Reply {
		t.Helper()
		sequence++
		command.Client, command.Sequence, command.Epoch = "test", sequence, epoch
		reply := shared.Apply(command)
		if reply.Error != "" {
			t.Fatalf("%s: %s", command.Action, reply.Error)
		}
		return reply
	}
	original, renamed := project.Default(), project.Default()
	renamed.Network.Stations[0].Name = "Renamed station"
	id := apply(session.Command{Action: "checkpoint"}).Checkpoint
	apply(session.Command{Action: "pause", Paused: true})
	// Each step runs in order. The client fetches the topology only when the
	// project revision changes.
	steps := []struct {
		name     string
		command  session.Command
		want     project.Config
		requests int32
	}{
		{"project apply", session.Command{Action: "project", ProjectRevision: 1, Project: &renamed}, renamed, 1},
		{"project-restoring rewind", session.Command{Action: "rewind", Checkpoint: id}, original, 2},
		{"repeated rewind", session.Command{Action: "rewind", Checkpoint: id}, original, 2},
	}
	for _, step := range steps {
		reply := apply(step.command)
		state, err := testStreamState(t, &cache, client, shared.Frame())
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if requests.Load() != step.requests || state.ProjectRevision != reply.ProjectRevision {
			t.Fatalf("%s: %d topology requests at project revision %d, want %d at %d",
				step.name, requests.Load(), state.ProjectRevision, step.requests, reply.ProjectRevision)
		}
		if !reflect.DeepEqual(state.Network, step.want.Network) {
			t.Fatalf("%s: the state network is not the network of the active project", step.name)
		}
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestReturnedEpochUsesItsTopology(t *testing.T) {
	t.Parallel()
	first, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	// Give the second epoch another network, so the check below shows which
	// topology the state uses.
	renamed := project.Default()
	renamed.Network.Stations[0].Name = "Renamed station"
	for sequence, command := range []session.Command{
		{Action: "pause", Paused: true},
		{Action: "project", ProjectRevision: 1, Project: &renamed},
	} {
		command.Client, command.Sequence, command.Epoch = "test", uint64(sequence+1), second.Frame().Epoch
		if reply := second.Apply(command); reply.Error != "" {
			t.Fatalf("%s: %s", command.Action, reply.Error)
		}
	}
	var serving atomic.Pointer[session.Session]
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(serving.Load().Topology())
	}))
	defer server.Close()
	client := &Client{url: server.URL, http: server.Client()}
	cache := streamTopology{}
	read := func(shared *session.Session) {
		t.Helper()
		serving.Store(shared)
		state, err := testStreamState(t, &cache, client, shared.Frame())
		if err != nil {
			t.Fatal(err)
		}
		client.state = state
	}
	read(first)
	read(second)
	// The returned epoch fetches its topology once, then reuses the cache.
	for range 20 {
		read(first)
	}
	state, _, _ := client.View()
	if state.Epoch != first.Frame().Epoch || !reflect.DeepEqual(state.Network, project.Default().Network) {
		t.Fatal("the client did not switch back to the first epoch and its network")
	}
	if requests.Load() != 3 {
		t.Fatalf("topology requests = %d, want 3", requests.Load())
	}
}

func TestNoteBuild(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		builds []string
		want   bool
	}{
		{name: "no frames"},
		{name: "empty builds", builds: []string{"", ""}},
		{name: "empty build before the baseline", builds: []string{"", "a"}},
		{name: "same build", builds: []string{"a", "", "a"}},
		{name: "new build", builds: []string{"a", "b"}, want: true},
		{name: "empty build after a change", builds: []string{"a", "b", ""}, want: true},
		{name: "first build again", builds: []string{"a", "b", "a"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{}
			for _, build := range test.builds {
				c.noteBuild(build)
			}
			if got := c.BuildChanged(); got != test.want {
				t.Fatalf("BuildChanged() = %t after builds %q, want %t", got, test.builds, test.want)
			}
		})
	}
}

func TestLostReplyRetryAndReconnect(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var failConnect atomic.Bool
	var posts atomic.Int32
	handler := shared.HandlerFS(nil)
	t.Cleanup(shared.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if failConnect.Load() {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
			handler.ServeHTTP(w, r)
			return
		}
		var command session.Command
		if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
			t.Error(err)
			return
		}
		reply := shared.Apply(command)
		if posts.Add(1) == 1 {
			http.Error(w, "reply lost", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(reply)
	}))
	defer server.Close()
	ctx := t.Context()
	client := New(ctx, server.URL)
	waitFor(t, func() bool { _, connected, _ := client.View(); return connected })
	if err := client.Submit(session.Command{Action: "trip", Origin: "harbor", Destination: "market"}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-client.Results():
		if result.Err != nil || result.Reply.Error != "" || result.Reply.OrderID != 1 {
			t.Fatalf("retry failed: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command timed out")
	}
	if posts.Load() != 2 || shared.State().Simulation.Submitted != 1 {
		t.Fatal("retry duplicated order")
	}
	failConnect.Store(true)
	client.mu.Lock()
	socket := client.socket
	client.mu.Unlock()
	if socket != nil {
		_ = socket.CloseNow()
	}
	waitFor(t, func() bool { _, connected, _ := client.View(); return !connected })
	if err := client.Submit(session.Command{Action: "reset"}); err == nil {
		t.Fatal("accepted disconnected command")
	}
	failConnect.Store(false)
	waitFor(t, func() bool { state, connected, _ := client.View(); return connected && state.Simulation.Submitted == 1 })
}

// TestLastFrame checks the time of the last good state frame. The time is
// zero before the first frame. While reconnects fail, it stays at the time of the
// last good frame.
func TestLastFrame(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var failConnect atomic.Bool
	var attempts atomic.Int32
	handler := shared.HandlerFS(nil)
	t.Cleanup(shared.Close)
	failConnect.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/state/stream" {
			attempts.Add(1)
		}
		if failConnect.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	client := New(t.Context(), server.URL)
	// Count failed connection attempts after the socket closes.
	waitForConnect := func() {
		t.Helper()
		seen := attempts.Load()
		waitFor(t, func() bool { return attempts.Load() > seen })
	}
	waitForConnect()
	if got := client.LastFrame(); !got.IsZero() {
		t.Fatalf("LastFrame() = %v before the first frame, want the zero time", got)
	}
	before := time.Now()
	failConnect.Store(false)
	waitFor(t, func() bool { _, connected, _ := client.View(); return connected })
	if got, now := client.LastFrame(), time.Now(); got.Before(before) || got.After(now) {
		t.Fatalf("LastFrame() = %v after the first frame, want a time from %v to %v", got, before, now)
	}
	failConnect.Store(true)
	client.mu.Lock()
	socket := client.socket
	client.mu.Unlock()
	if socket != nil {
		_ = socket.CloseNow()
	}
	waitFor(t, func() bool { _, connected, _ := client.View(); return !connected })
	lost := client.LastFrame()
	waitForConnect()
	if got := client.LastFrame(); !got.Equal(lost) {
		t.Fatalf("LastFrame() = %v after failed reconnects, want %v", got, lost)
	}
}

func TestLostReplyRewindAppliesOnce(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var rewinds atomic.Int32
	handler := shared.HandlerFS(nil)
	t.Cleanup(shared.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handler.ServeHTTP(w, r)
		default:
			var command session.Command
			if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
				t.Error(err)
				return
			}
			reply := shared.Apply(command)
			// Lose the reply to the first rewind after the session applies it.
			if command.Action == "rewind" && rewinds.Add(1) == 1 {
				http.Error(w, "reply lost", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(reply)
		}
	}))
	defer server.Close()
	client := New(t.Context(), server.URL)
	submit := func(command session.Command) Result {
		t.Helper()
		waitFor(t, func() bool { _, connected, pending := client.View(); return connected && !pending })
		if err := client.Submit(command); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-client.Results():
			if result.Err != nil || result.Reply.Error != "" {
				t.Fatalf("%s failed: %+v", command.Action, result)
			}
			return result
		case <-time.After(5 * time.Second):
			t.Fatalf("%s timed out", command.Action)
			return Result{}
		}
	}
	saved := submit(session.Command{Action: "checkpoint"})
	before := shared.State().Generation
	rewound := submit(session.Command{Action: "rewind", Checkpoint: saved.Reply.Checkpoint})
	if rewinds.Load() != 2 {
		t.Fatalf("rewind posts = %d, want 2", rewinds.Load())
	}
	if after := shared.State().Generation; after != before+1 || rewound.Reply.Generation != after {
		t.Fatalf("generation went from %d to %d with reply %d, want one rewind", before, after, rewound.Reply.Generation)
	}
}

func TestSubmitOwnsProjectPayload(t *testing.T) {
	t.Parallel()
	client := &Client{connected: true, commands: make(chan session.Command, 1)}
	config := project.Default()
	if err := client.Submit(session.Command{Action: "project", Project: &config}); err != nil {
		t.Fatal(err)
	}
	config.Network.Nodes[0].ID = "changed after submit"
	queued := <-client.commands
	if queued.Project.Network.Nodes[0].ID == config.Network.Nodes[0].ID {
		t.Fatal("queued command aliases caller project")
	}
}

// TestStateForFrameRefetchesTopologyForNewServerStart checks that a frame
// from a new server process fetches the topology again, although its epoch
// and project revision match the cached topology.
func TestStateForFrameRefetchesTopologyForNewServerStart(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	var currentStart atomic.Value
	currentStart.Store(shared.Frame().ServerStart)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		topology := shared.Topology()
		topology.ServerStart, _ = currentStart.Load().(string)
		_ = json.NewEncoder(w).Encode(topology)
	}))
	defer server.Close()
	frame := shared.Frame()
	client := &Client{url: server.URL, http: server.Client()}
	cache := testStreamCache(t, shared.Topology())
	if _, err := testStreamState(t, &cache, client, frame); err != nil || requests.Load() != 0 {
		t.Fatalf("same process: %d topology requests, error %v, want 0", requests.Load(), err)
	}
	frame.ServerStart = "restarted"
	currentStart.Store(frame.ServerStart)
	if _, err := testStreamState(t, &cache, client, frame); err != nil || requests.Load() != 1 {
		t.Fatalf("new process: %d topology requests, error %v, want 1", requests.Load(), err)
	}
	if _, err := testStreamState(t, &cache, client, frame); err != nil || requests.Load() != 1 {
		t.Fatalf("new process again: %d topology requests, error %v, want 1", requests.Load(), err)
	}
}

func testStreamCache(t *testing.T, topology session.TopologySnapshot) streamTopology {
	t.Helper()
	assembler, err := session.NewStreamAssembler(topology)
	if err != nil {
		t.Fatal(err)
	}
	return streamTopology{topology: topology, assembler: assembler}
}
func testStreamState(t *testing.T, cache *streamTopology, client *Client, frame session.StateFrame) (session.State, error) {
	t.Helper()
	routes := make([]sim.RoutePresentation, len(frame.Simulation.Vehicles))
	for i := range routes {
		routes[i].Origin = -1
	}
	return cache.state(t.Context(), client, session.StreamFrame{State: frame, Routes: routes})
}

func TestStreamTopologyRefreshPreservesPublishedState(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"", "identity", "trailing JSON"} {
		name := failure
		if name == "" {
			name = "accepted"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shared, err := session.New()
			if err != nil {
				t.Fatal(err)
			}
			defer shared.Close()
			cache := testStreamCache(t, shared.Topology())
			frame := shared.Frame()
			old, err := testStreamState(t, &cache, &Client{}, frame)
			if err != nil {
				t.Fatal(err)
			}
			before := streamJSON(t, old)
			next := shared.Topology()
			next.ProjectRevision++
			next.Network.Nodes[0].Position.X += 123
			next.Network.Lanes[0].SpeedLimit++
			next.Network.Stations[0].Name = "Refreshed station"
			frame.ProjectRevision = next.ProjectRevision
			if failure == "identity" {
				next.ServerStart = "wrong process"
			}
			body := streamJSON(t, next)
			if failure == "trailing JSON" {
				body = append(body, []byte("{}")...)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
			defer server.Close()
			client := &Client{url: server.URL, http: server.Client()}
			current, err := testStreamState(t, &cache, client, frame)
			if failure == "" && err != nil || failure != "" && err == nil {
				t.Fatal("unexpected refresh result", err)
			}
			if got := streamJSON(t, old); !bytes.Equal(got, before) {
				t.Fatal("topology refresh changed previously published state")
			}
			if failure == "" && !reflect.DeepEqual(current.Network, next.Network) {
				t.Fatal("refresh did not publish new topology")
			}
			if failure != "" && cache.topology.ProjectRevision != old.ProjectRevision {
				t.Fatal("rejected topology replaced cache")
			}
		})
	}
}
