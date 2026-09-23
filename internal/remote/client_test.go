package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
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
	client := &Client{url: server.URL, http: server.Client(), topology: shared.Topology()}
	if _, err := client.stateForFrame(context.Background(), shared.Frame()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("fetched unchanged topology")
	}
	client.topology = session.TopologySnapshot{}
	if _, err := client.stateForFrame(context.Background(), shared.Frame()); err != nil {
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
	client := &Client{url: server.URL, http: server.Client(), topology: shared.Topology()}
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
		state, err := client.stateForFrame(t.Context(), shared.Frame())
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

func TestStateOrdering(t *testing.T) {
	t.Parallel()
	c := &Client{oldEpochs: make(map[string]bool)}
	for _, state := range []session.State{{Epoch: "first", Revision: 10}, {Epoch: "first", Revision: 9}, {Epoch: "second", Revision: 0}, {Epoch: "first", Revision: 11}, {Epoch: "second", Revision: 5}, {Epoch: "second", Revision: 3}} {
		c.accept(state)
	}
	state, _, _ := c.View()
	if state.Epoch != "second" || state.Revision != 5 {
		t.Fatalf("state rolled back: %+v", state)
	}
}

func TestLostReplyRetryAndReconnect(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var failPoll atomic.Bool
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if failPoll.Load() {
				http.Error(w, "offline", http.StatusServiceUnavailable)
				return
			}
			if r.URL.Path == "/api/topology" {
				_ = json.NewEncoder(w).Encode(shared.Topology())
			} else {
				_ = json.NewEncoder(w).Encode(shared.Frame())
			}
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
	failPoll.Store(true)
	waitFor(t, func() bool { _, connected, _ := client.View(); return !connected })
	if err := client.Submit(session.Command{Action: "reset"}); err == nil {
		t.Fatal("accepted disconnected command")
	}
	failPoll.Store(false)
	waitFor(t, func() bool { state, connected, _ := client.View(); return connected && state.Simulation.Submitted == 1 })
}

func TestLostReplyRewindAppliesOnce(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var rewinds atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/topology":
			_ = json.NewEncoder(w).Encode(shared.Topology())
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(shared.Frame())
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
