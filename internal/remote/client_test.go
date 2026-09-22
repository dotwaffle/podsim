package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
