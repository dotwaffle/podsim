package view

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
)

// TestServerUpdate replaces the server with one that has a new build. The
// browser game reloads the page once. The desktop game shows the update
// message. When a command result or a user action clears the message, the
// desktop game shows it again.
func TestServerUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		browser     bool
		wantReloads int
		wantMessage string
	}{
		{name: "browser", browser: true, wantReloads: 1},
		{name: "desktop", wantMessage: serverUpdateMessage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before, after := buildTestHandler(t, "build-a"), buildTestHandler(t, "build-b")
			var upgraded atomic.Bool
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/state" {
					polls.Add(1)
				}
				if upgraded.Load() {
					after.ServeHTTP(w, r)
					return
				}
				before.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			reloads := 0
			var options []Option
			if test.browser {
				options = append(options, WithReload(func() { reloads++ }))
			}
			game, err := New(t.Context(), server.URL, options...)
			if err != nil {
				t.Fatalf("create game: %v", err)
			}
			syncGame(t, game, func() bool { return game.state.Epoch != "" })
			if reloads != 0 || game.message != "" {
				t.Fatalf("%d reloads and message %q before the update", reloads, game.message)
			}
			oldEpoch := game.state.Epoch
			upgraded.Store(true)
			// Wait for the state of the new server too, so that the command
			// below has the epoch of the new server.
			syncGame(t, game, func() bool { return game.serverUpdated && game.state.Epoch != oldEpoch })
			check := func(when string) {
				t.Helper()
				if reloads != test.wantReloads || game.message != test.wantMessage {
					t.Fatalf("%d reloads and message %q %s, want %d and %q", reloads, game.message, when, test.wantReloads, test.wantMessage)
				}
			}
			check("after the update")
			game.pause()
			syncGame(t, game, func() bool { return true })
			check("after a command result")
			game.message = ""
			seen := polls.Load()
			syncGame(t, game, func() bool { return polls.Load() >= seen+3 })
			check("after a user action and more frames")
		})
	}
}

// buildTestHandler returns the HTTP handler of a new session with the build
// ID build. The session clock does not run.
func buildTestHandler(t *testing.T, build string) http.Handler {
	t.Helper()
	shared, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)), session.WithBuildID(build))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return shared.HandlerFS(fstest.MapFS{})
}
