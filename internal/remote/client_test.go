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
	client := &Client{url: server.URL, http: server.Client(), topology: shared.Topology(), topologyStart: shared.Frame().ServerStart}
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
	client := &Client{url: server.URL, http: server.Client(), topology: shared.Topology(), topologyStart: shared.Frame().ServerStart}
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

// accept runs acceptLocked with the client lock, as poll does.
func accept(c *Client, state session.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acceptLocked(state)
}

func TestStateOrdering(t *testing.T) {
	t.Parallel()
	c := &Client{oldEpochs: make(map[string]bool)}
	for _, state := range []session.State{{Epoch: "first", Revision: 10}, {Epoch: "first", Revision: 9}, {Epoch: "second", Revision: 0}, {Epoch: "first", Revision: 11}, {Epoch: "second", Revision: 5}, {Epoch: "second", Revision: 3}} {
		accept(c, state)
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
				accept(c, state)
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
		accept(client, state)
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

// TestPollReportsBuildChange gives the poll loop one frame for each state
// request. The frames have the builds "", "a", "a" and "b". The client can
// use the "b" frame, or it drops it. The build change must show in each
// case.
func TestPollReportsBuildChange(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	encode := func(build, epoch string) []byte {
		t.Helper()
		frame := shared.Frame()
		frame.Build = build
		if epoch != "" {
			frame.Epoch = epoch
		}
		data, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	noBuild, buildA := encode("", ""), encode("a", "")
	tests := []struct {
		name string
		// last is the frame with the build "b".
		last          []byte
		wantConnected bool
	}{
		{name: "valid frame", last: encode("b", ""), wantConnected: true},
		// The topology endpoint does not serve this epoch, so the client
		// drops the frame.
		{name: "topology error", last: encode("b", "restarted")},
		// A future frame format can change the type of a member. The
		// client cannot decode the frame, but build stays a top-level
		// string.
		{name: "changed member type", last: []byte(`{"epoch":"new","revision":"v2","build":"b"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			frames := make(chan []byte)
			polled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/topology" {
					_ = json.NewEncoder(w).Encode(shared.Topology())
					return
				}
				select {
				case polled <- struct{}{}:
				case <-r.Context().Done():
					return
				}
				select {
				case frame := <-frames:
					_, _ = w.Write(frame)
				case <-r.Context().Done():
				}
			}))
			t.Cleanup(server.Close)
			// This cleanup runs first. It stops the client, so no request
			// blocks server.Close.
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			waitForPoll := func() {
				t.Helper()
				select {
				case <-polled:
				case <-time.After(5 * time.Second):
					t.Fatal("the client did not request a state frame")
				}
			}
			client := New(ctx, server.URL)
			waitForPoll()
			steps := []struct {
				frame         []byte
				wantChanged   bool
				wantConnected bool
			}{
				{frame: noBuild, wantConnected: true},
				{frame: buildA, wantConnected: true},
				{frame: buildA, wantConnected: true},
				{frame: test.last, wantChanged: true, wantConnected: test.wantConnected},
			}
			for index, step := range steps {
				frames <- step.frame
				// The next state request shows that the client handled this
				// frame.
				waitForPoll()
				_, connected, _ := client.View()
				if got := client.BuildChanged(); got != step.wantChanged || connected != step.wantConnected {
					t.Fatalf("frame %d: BuildChanged() = %t and connected %t, want %t and %t",
						index, got, connected, step.wantChanged, step.wantConnected)
				}
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

// TestLastFrame checks the time of the last good state frame. The time is
// zero before the first frame. While polls fail, it stays at the time of the
// last good frame.
func TestLastFrame(t *testing.T) {
	t.Parallel()
	shared, err := session.New()
	if err != nil {
		t.Fatal(err)
	}
	var failPoll atomic.Bool
	var polls atomic.Int32
	failPoll.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/state" {
			polls.Add(1)
		}
		if failPoll.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/api/topology" {
			_ = json.NewEncoder(w).Encode(shared.Topology())
			return
		}
		_ = json.NewEncoder(w).Encode(shared.Frame())
	}))
	defer server.Close()
	client := New(t.Context(), server.URL)
	// The client polls in one goroutine. When the client sends a poll, it
	// has handled the previous poll.
	waitForPoll := func() {
		t.Helper()
		seen := polls.Load()
		waitFor(t, func() bool { return polls.Load() >= seen+2 })
	}
	waitForPoll()
	if got := client.LastFrame(); !got.IsZero() {
		t.Fatalf("LastFrame() = %v before the first frame, want the zero time", got)
	}
	before := time.Now()
	failPoll.Store(false)
	waitFor(t, func() bool { _, connected, _ := client.View(); return connected })
	if got, now := client.LastFrame(), time.Now(); got.Before(before) || got.After(now) {
		t.Fatalf("LastFrame() = %v after the first frame, want a time from %v to %v", got, before, now)
	}
	failPoll.Store(true)
	waitFor(t, func() bool { _, connected, _ := client.View(); return !connected })
	lost := client.LastFrame()
	waitForPoll()
	if got := client.LastFrame(); !got.Equal(lost) {
		t.Fatalf("LastFrame() = %v after failed polls, want %v", got, lost)
	}
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

// TestStateOrderingServerRestart checks that a restart that restores an
// older final save with the same epoch reaches the view, although its
// revision is lower. A lower revision from the same process is still
// dropped.
func TestStateOrderingServerRestart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		states       []session.State
		wantRevision uint64
		wantStart    string
	}{
		{
			name:         "rollback with a new start ID",
			states:       []session.State{{Epoch: "e", Revision: 10, ServerStart: "a"}, {Epoch: "e", Revision: 4, ServerStart: "b"}},
			wantRevision: 4, wantStart: "b",
		},
		{
			name:         "older frame from the same process",
			states:       []session.State{{Epoch: "e", Revision: 10, ServerStart: "a"}, {Epoch: "e", Revision: 4, ServerStart: "a"}},
			wantRevision: 10, wantStart: "a",
		},
		{
			name:         "older server without start IDs",
			states:       []session.State{{Epoch: "e", Revision: 10}, {Epoch: "e", Revision: 4}},
			wantRevision: 10,
		},
		{
			name:         "new process then its later frames",
			states:       []session.State{{Epoch: "e", Revision: 10, ServerStart: "a"}, {Epoch: "e", Revision: 4, ServerStart: "b"}, {Epoch: "e", Revision: 3, ServerStart: "b"}},
			wantRevision: 4, wantStart: "b",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Client{oldEpochs: make(map[string]bool)}
			for _, state := range tc.states {
				accept(c, state)
			}
			state, _, _ := c.View()
			if state.Revision != tc.wantRevision || state.ServerStart != tc.wantStart {
				t.Fatalf("revision %d start %q, want %d %q", state.Revision, state.ServerStart, tc.wantRevision, tc.wantStart)
			}
		})
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(shared.Topology())
	}))
	defer server.Close()
	frame := shared.Frame()
	client := &Client{url: server.URL, http: server.Client(), topology: shared.Topology(), topologyStart: frame.ServerStart}
	if _, err := client.stateForFrame(t.Context(), frame); err != nil || requests.Load() != 0 {
		t.Fatalf("same process: %d topology requests, error %v, want 0", requests.Load(), err)
	}
	frame.ServerStart = "restarted"
	if _, err := client.stateForFrame(t.Context(), frame); err != nil || requests.Load() != 1 {
		t.Fatalf("new process: %d topology requests, error %v, want 1", requests.Load(), err)
	}
	if _, err := client.stateForFrame(t.Context(), frame); err != nil || requests.Load() != 1 {
		t.Fatalf("new process again: %d topology requests, error %v, want 1", requests.Load(), err)
	}
}
