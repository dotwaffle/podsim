package view

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestSessionChangeNotice compares two states and checks the notice for a
// change that this game did not cause.
func TestSessionChangeNotice(t *testing.T) {
	t.Parallel()
	physical := session.RestoreInfo{Tier: string(sim.RestorePhysical)}
	logical := session.RestoreInfo{Tier: string(sim.RestoreLogical), Reason: "physical_failed"}
	savePoints := []session.Checkpoint{{ID: 1, Tick: 600}}
	tests := []struct {
		name  string
		input sessionChangeInput
		want  string
	}{
		{name: "first frame", input: sessionChangeInput{current: session.State{Epoch: "a", Generation: 1}}},
		{name: "same generation", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Revision: 10, Generation: 4},
			current:  session.State{Epoch: "a", Revision: 11, Generation: 4},
		}},
		{name: "new epoch", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "b", Generation: 1},
		}, want: restartNotice},
		{name: "new epoch with the same generation", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "b", Generation: 4},
		}, want: restartNotice},
		{name: "new epoch while a reset waits", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "b", Generation: 1},
			inFlight: true, ownEpoch: "a", ownGeneration: 4,
		}, want: restartNotice},
		{name: "new generation", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5},
		}, want: otherBrowserNotice},
		{name: "new generation while a reset waits", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5},
			inFlight: true,
		}},
		{name: "reply gave the new generation", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5},
			ownEpoch: "a", ownGeneration: 5,
		}},
		{name: "reply gave a later generation", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5},
			ownEpoch: "a", ownGeneration: 6,
		}},
		{name: "reply gave an earlier generation", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 5},
			current:  session.State{Epoch: "a", Generation: 6},
			ownEpoch: "a", ownGeneration: 5,
		}, want: otherBrowserNotice},
		{name: "reply from an earlier epoch", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5},
			ownEpoch: "b", ownGeneration: 9,
		}, want: otherBrowserNotice},
		{name: "restart with a kept epoch", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4, Checkpoints: savePoints},
			current:  session.State{Epoch: "a", Generation: 5, Restore: physical},
		}, want: restartNotice},
		{name: "logical restart with a kept epoch", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5, Restore: logical},
		}, want: restartNotice},
		{name: "restart with a kept epoch while a reset waits", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5, Restore: physical},
			inFlight: true, ownEpoch: "a", ownGeneration: 5,
		}, want: restartNotice},
		{name: "frame after a restart with a kept epoch", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Revision: 8, Generation: 5, Restore: physical},
			current:  session.State{Epoch: "a", Revision: 9, Generation: 5, Restore: physical},
		}},
		{name: "rewind after a restart", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 5, Restore: physical, Checkpoints: savePoints},
			current:  session.State{Epoch: "a", Generation: 6, Restore: physical, Checkpoints: savePoints},
		}, want: otherBrowserNotice},
		{name: "empty restore tier", input: sessionChangeInput{
			previous: session.State{Epoch: "a", Generation: 4},
			current:  session.State{Epoch: "a", Generation: 5, Restore: session.RestoreInfo{Tier: "empty", Reason: "project_changed"}},
		}, want: otherBrowserNotice},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sessionChangeNotice(test.input); got != test.want {
				t.Fatalf("sessionChangeNotice = %q, want %q", got, test.want)
			}
		})
	}
}

// TestStartsGeneration checks which command actions start a new generation
// of the shared session.
func TestStartsGeneration(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"reset": true, "demo": true, "rewind": true, "project": true,
		"trip": false, "pause": false, "speed": false, "demand": false, "checkpoint": false, "": false,
	}
	for action, want := range tests {
		if got := startsGeneration(action); got != want {
			t.Errorf("startsGeneration(%q) = %t, want %t", action, got, want)
		}
	}
}

// TestOwnGenerationFromResult checks that only an accepted reply to a
// command that starts a new generation sets the own generation.
func TestOwnGenerationFromResult(t *testing.T) {
	t.Parallel()
	reply := session.Reply{Epoch: "a", Revision: 12, Generation: 5}
	tests := []struct {
		name           string
		result         remote.Result
		wantEpoch      string
		wantGeneration uint64
	}{
		{name: "reset", result: remote.Result{Command: session.Command{Action: "reset"}, Reply: reply}, wantEpoch: "a", wantGeneration: 5},
		{name: "rewind", result: remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 1}, Reply: reply}, wantEpoch: "a", wantGeneration: 5},
		{name: "rejected reset", result: remote.Result{Command: session.Command{Action: "reset"}, Reply: session.Reply{Epoch: "a", Generation: 4, ErrorCode: session.CommandRejected, Error: "rejected"}}, wantEpoch: "b", wantGeneration: 3},
		{name: "failed send", result: remote.Result{Command: session.Command{Action: "reset"}, Err: errors.New("server returned HTTP 500")}, wantEpoch: "b", wantGeneration: 3},
		{name: "pause", result: remote.Result{Command: session.Command{Action: "pause", Paused: true}, Reply: reply}, wantEpoch: "b", wantGeneration: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.ownEpoch, game.ownGeneration = "b", 3
			game.handleResult(test.result)
			if game.ownEpoch != test.wantEpoch || game.ownGeneration != test.wantGeneration {
				t.Fatalf("own generation %q/%d, want %q/%d", game.ownEpoch, game.ownGeneration, test.wantEpoch, test.wantGeneration)
			}
		})
	}
}

// TestSessionChangeReplacesConfirmation checks that a session change
// notice replaces an old message and the reset confirmation. The next
// Reset press then asks again.
func TestSessionChangeReplacesConfirmation(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 2)
	game.state = session.State{Epoch: "a", Generation: 4}
	game.reset()
	game.message = "save point #1 is no longer available"
	previous := game.state
	game.state.Generation = 5
	game.announceSessionChange(previous)
	if game.message != "" || game.notice != otherBrowserNotice || game.noticeAction != sessionChangeAction || game.noticeTicks != noticeDuration {
		t.Fatalf("message %q, notice %q action %q for %d ticks, want no message and notice %q action %q for %d ticks",
			game.message, game.notice, game.noticeAction, game.noticeTicks, otherBrowserNotice, sessionChangeAction, noticeDuration)
	}
	game.reset()
	if game.pending || game.notice != resetConfirmNotice {
		t.Fatalf("press after the change: pending %t notice %q, want no command and notice %q", game.pending, game.notice, resetConfirmNotice)
	}
}

// TestSessionChangeWhileCommandWaits changes the generation while a
// command of the game waits for its reply. Only a command that starts a new
// generation stops the session change notice.
func TestSessionChangeWhileCommandWaits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action string
		want   string
	}{
		{action: "pause", want: otherBrowserNotice},
		{action: "trip", want: otherBrowserNotice},
		{action: "checkpoint", want: otherBrowserNotice},
		{action: "reset"},
		{action: "rewind"},
		{action: "demo"},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.state = session.State{Epoch: "a", Generation: 5}
			game.pending, game.sentAction = true, test.action
			game.announceSessionChange(session.State{Epoch: "a", Generation: 4})
			if game.notice != test.want {
				t.Fatalf("notice %q, want %q", game.notice, test.want)
			}
		})
	}
}

// TestSessionChangeFromServer connects a game to a session server and
// changes the session in different ways. Only a change that the game did
// not cause shows a session change notice.
func TestSessionChangeFromServer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// change changes the session of env.
		change         func(t *testing.T, env changeEnv)
		wantEpochKept  bool
		wantNotice     string
		wantAction     string
		wantSavePoints int
	}{
		{
			name: "other browser reset",
			change: func(t *testing.T, env changeEnv) {
				t.Helper()
				other := remote.New(t.Context(), env.url)
				waitFor(t, func() bool {
					state, connected, _ := other.View()
					return connected && state.Epoch != ""
				})
				if err := other.Submit(session.Command{Action: "reset"}); err != nil {
					t.Fatalf("submit reset: %v", err)
				}
				select {
				case result := <-other.Results():
					if result.Err != nil || result.Reply.Error != "" {
						t.Fatalf("reset failed: %+v", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("reset timed out")
				}
			},
			wantEpochKept: true, wantNotice: otherBrowserNotice, wantAction: sessionChangeAction, wantSavePoints: 1,
		},
		{
			name: "own reset",
			change: func(t *testing.T, env changeEnv) {
				t.Helper()
				env.game.reset()
				env.game.reset()
				if !env.game.pending {
					t.Fatalf("second press sent no command: %q", env.game.message)
				}
			},
			wantEpochKept: true, wantNotice: resetNotice, wantAction: "reset", wantSavePoints: 1,
		},
		{
			name: "restart",
			change: func(t *testing.T, env changeEnv) {
				t.Helper()
				shared, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
				if err != nil {
					t.Fatalf("create session: %v", err)
				}
				env.server.use(shared)
			},
			wantNotice: restartNotice, wantAction: sessionChangeAction,
		},
		{
			name: "restart with a kept epoch",
			change: func(t *testing.T, env changeEnv) {
				t.Helper()
				env.shared.Close()
				if err := env.shared.SaveState(t.Context(), session.SaveFinal); err != nil {
					t.Fatalf("final save: %v", err)
				}
				env.server.use(storedSession(t, env.store))
			},
			wantEpochKept: true, wantNotice: restartNotice, wantAction: sessionChangeAction,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := newChangeEnv(t)
			game := env.game
			clickCommand(t, game, "checkpoint")
			syncGame(t, game, func() bool { return len(game.state.Checkpoints) == 1 })
			before := game.state
			game.message = "save point #1 is no longer available"
			test.change(t, env)
			syncGame(t, game, func() bool {
				return game.state.Epoch != before.Epoch || game.state.Generation != before.Generation
			})
			if kept := game.state.Epoch == before.Epoch; kept != test.wantEpochKept {
				t.Fatalf("epoch kept %t, want %t", kept, test.wantEpochKept)
			}
			if count := len(game.state.Checkpoints); count != test.wantSavePoints {
				t.Fatalf("save points %d, want %d", count, test.wantSavePoints)
			}
			if game.message != "" || game.notice != test.wantNotice || game.noticeAction != test.wantAction {
				t.Fatalf("message %q, notice %q action %q, want no message and notice %q action %q",
					game.message, game.notice, game.noticeAction, test.wantNotice, test.wantAction)
			}
		})
	}
}

// TestOwnResetBeforeReply resets the session while the server holds the
// reply. The state with the new generation arrives before the reply. The
// game shows no session change notice, and then the reset notice.
func TestOwnResetBeforeReply(t *testing.T) {
	t.Parallel()
	held := make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	game := sharedHandlerGame(t, project.Default(), func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/command" {
				handler.ServeHTTP(w, r)
				return
			}
			// Apply the command at once, but send the reply only after
			// release.
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			<-held
			maps.Copy(w.Header(), recorder.Header())
			w.WriteHeader(recorder.Code)
			_, _ = w.Write(recorder.Body.Bytes())
		})
	})
	// Cleanups run in the opposite order, so release runs before the
	// server closes, also when the test stops early.
	t.Cleanup(release)
	before := game.state.Generation
	game.reset()
	game.reset()
	if !game.pending {
		t.Fatalf("second press sent no command: %q", game.message)
	}
	waitFor(t, func() bool {
		game.readRemote()
		return game.state.Generation != before
	})
	if !game.pending || game.notice != "" || game.noticeAction != "" {
		t.Fatalf("new generation before the reply: pending %t, notice %q action %q, want a waiting command and no notice",
			game.pending, game.notice, game.noticeAction)
	}
	release()
	syncGame(t, game, func() bool { return true })
	if game.notice != resetNotice || game.noticeAction != "reset" {
		t.Fatalf("after the reply: notice %q action %q, want %q action %q", game.notice, game.noticeAction, resetNotice, "reset")
	}
}

// changeEnv is a game that is connected to a session server that can
// restart. store keeps the saved state of shared, the first session.
type changeEnv struct {
	game   *Game
	url    string
	server *switchServer
	store  *memoryStore
	shared *session.Session
}

// newChangeEnv starts a session with a state store and connects a game to
// it.
func newChangeEnv(t *testing.T) changeEnv {
	t.Helper()
	store := &memoryStore{}
	shared := storedSession(t, store)
	server := &switchServer{}
	server.use(shared)
	test := httptest.NewServer(server)
	t.Cleanup(test.Close)
	game := journeyTestGame(t, 2)
	game.client = remote.New(t.Context(), test.URL)
	syncGame(t, game, func() bool { return game.state.Epoch != "" })
	return changeEnv{game: game, url: test.URL, server: server, store: store, shared: shared}
}

// storedSession starts a session from the state in store, as a server
// with the -state option does. The session clock does not run.
func storedSession(t *testing.T, store *memoryStore) *session.Session {
	t.Helper()
	shared, err := session.NewFromStore(t.Context(), session.StoreInput{
		Store: store, Options: []session.Option{session.WithLogger(slog.New(slog.DiscardHandler))},
	})
	if err != nil {
		t.Fatalf("start session from store: %v", err)
	}
	return shared
}

// switchServer serves the handler of one session. use replaces the
// session, as a server restart at the same address does.
type switchServer struct {
	handler atomic.Pointer[http.Handler]
}

func (s *switchServer) use(shared *session.Session) {
	handler := shared.HandlerFS(fstest.MapFS{})
	s.handler.Store(&handler)
}

func (s *switchServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.handler.Load()).ServeHTTP(w, r)
}

// memoryStore keeps a saved session state in memory.
type memoryStore struct {
	mu   sync.Mutex
	data []byte
}

func (m *memoryStore) Read(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		return nil, fmt.Errorf("read memory state: %w", fs.ErrNotExist)
	}
	return bytes.Clone(m.data), nil
}

func (m *memoryStore) Write(_ context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = bytes.Clone(data)
	return nil
}

func (m *memoryStore) Reject(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = nil
	return nil
}

func (m *memoryStore) Backup(context.Context) error { return nil }

// waitFor calls ready until it returns true, or fails the test after 5 s.
func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}
