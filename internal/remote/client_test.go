package remote

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
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

func TestAcceptRetiredEpoch(t *testing.T) {
	t.Parallel()
	frames := func(epoch string, revision uint64, count int) []session.State {
		return slices.Repeat([]session.State{{Epoch: epoch, Revision: revision}}, count)
	}
	start := slices.Concat(frames("E", 1, 1), frames("E2", 1, 1))
	tests := []struct {
		name     string
		frames   []session.State
		epoch    string
		revision uint64
		retired  []string
	}{
		{"19 retired frames are dropped", slices.Concat(start, frames("E", 2, 19)), "E2", 1, []string{"E"}},
		{"the 20th retired frame switches", slices.Concat(start, frames("E", 2, 20)), "E", 2, []string{"E2"}},
		{"a current frame restarts the count", slices.Concat(start, frames("E", 2, 19), frames("E2", 2, 1), frames("E", 3, 19)), "E2", 2, []string{"E"}},
		{"20 frames after a restart switch", slices.Concat(start, frames("E", 2, 19), frames("E2", 2, 1), frames("E", 3, 20)), "E", 3, []string{"E2"}},
		{"a stale current frame restarts the count", slices.Concat(frames("E", 1, 1), frames("E2", 5, 1), frames("E", 2, 19), frames("E2", 3, 1), frames("E", 2, 1)), "E2", 5, []string{"E"}},
		{"alternate retired epochs are dropped", slices.Concat(start, frames("E3", 1, 1), slices.Repeat(slices.Concat(frames("E", 2, 1), frames("E2", 2, 1)), 20)), "E3", 1, []string{"E", "E2"}},
		{"a single late frame is dropped", slices.Concat(start, frames("E", 9, 1)), "E2", 1, []string{"E"}},
		{"a lower revision is dropped", slices.Concat(frames("E", 5, 1), frames("E", 4, 1)), "E", 5, nil},
		{"a lower revision after a switch is dropped", slices.Concat(start, frames("E", 2, 20), frames("E", 1, 1)), "E", 2, []string{"E2"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{oldEpochs: make(map[string]bool)}
			for _, state := range test.frames {
				c.accept(state)
			}
			state, _, _ := c.View()
			retired := slices.Sorted(maps.Keys(c.oldEpochs))
			if state.Epoch != test.epoch || state.Revision != test.revision || !slices.Equal(retired, test.retired) {
				t.Fatalf("state %s at revision %d with retired epochs %v, want %s at %d with %v",
					state.Epoch, state.Revision, retired, test.epoch, test.revision, test.retired)
			}
		})
	}
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
	client := &Client{url: server.URL, http: server.Client(), oldEpochs: make(map[string]bool)}
	poll := func(shared *session.Session) {
		t.Helper()
		serving.Store(shared)
		state, err := client.stateForFrame(t.Context(), shared.Frame())
		if err != nil {
			t.Fatal(err)
		}
		client.accept(state)
	}
	poll(first)
	poll(second)
	// The first frame of the retired epoch fetches its topology. The next
	// frames use the cached topology, and the 20th frame switches back.
	for range 20 {
		poll(first)
	}
	state, _, _ := client.View()
	if state.Epoch != first.Frame().Epoch || !reflect.DeepEqual(state.Network, project.Default().Network) {
		t.Fatal("the client did not switch back to the first epoch and its network")
	}
	if requests.Load() != 3 {
		t.Fatalf("topology requests = %d, want 3", requests.Load())
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
