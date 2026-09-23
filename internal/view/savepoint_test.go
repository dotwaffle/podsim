package view

import (
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestSavePointButtons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		connected, pending bool
		demo               bool
		checkpoints        []session.Checkpoint
		wantSaveDisabled   bool
		wantRewindDisabled bool
		wantRewindLabel    string
	}{
		{name: "no save points", connected: true, wantRewindDisabled: true, wantRewindLabel: "Rewind"},
		{name: "one save point", connected: true, checkpoints: []session.Checkpoint{{ID: 1, Tick: 2520}}, wantRewindLabel: "Rewind 42.0 s"},
		{name: "many save points", connected: true, checkpoints: []session.Checkpoint{{ID: 3, Tick: 600}, {ID: 5, Tick: 3600}, {ID: 4, Tick: 1200}}, wantRewindLabel: "Rewind 60.0 s"},
		{name: "newest restores project", connected: true, checkpoints: []session.Checkpoint{{ID: 1, Tick: 600}, {ID: 2, Tick: 1200, RestoresProject: true}}, wantRewindLabel: "Rewind + project"},
		{name: "older restores project", connected: true, checkpoints: []session.Checkpoint{{ID: 1, Tick: 600, RestoresProject: true}, {ID: 2, Tick: 1230}}, wantRewindLabel: "Rewind 20.5 s"},
		{name: "demo", connected: true, demo: true, checkpoints: []session.Checkpoint{{ID: 1, Tick: 600}}, wantRewindLabel: "Rewind 10.0 s"},
		{name: "pending", connected: true, pending: true, checkpoints: []session.Checkpoint{{ID: 1, Tick: 600}}, wantSaveDisabled: true, wantRewindDisabled: true, wantRewindLabel: "Rewind 10.0 s"},
		{name: "disconnected", checkpoints: []session.Checkpoint{{ID: 1, Tick: 600}}, wantSaveDisabled: true, wantRewindDisabled: true, wantRewindLabel: "Rewind 10.0 s"},
		{name: "disconnected without save points", wantSaveDisabled: true, wantRewindDisabled: true, wantRewindLabel: "Rewind"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.connected, game.pending = test.connected, test.pending
			game.state.Simulation.Demo = test.demo
			game.state.Checkpoints = test.checkpoints
			controls := game.buttons()
			save := findButton(t, controls, "checkpoint")
			rewind := findButton(t, controls, "rewind")
			if save.label != "Save point" || save.disabled != test.wantSaveDisabled {
				t.Errorf("save button = %q disabled %t, want %q disabled %t", save.label, save.disabled, "Save point", test.wantSaveDisabled)
			}
			if rewind.label != test.wantRewindLabel || rewind.disabled != test.wantRewindDisabled {
				t.Errorf("rewind button = %q disabled %t, want %q disabled %t", rewind.label, rewind.disabled, test.wantRewindLabel, test.wantRewindDisabled)
			}
		})
	}
}

// TestRewindWaitsForSavedState checks that Rewind does not target an older
// save point while the state does not yet list the one that was just saved.
func TestRewindWaitsForSavedState(t *testing.T) {
	t.Parallel()
	older := []session.Checkpoint{{ID: 1, Tick: 600}}
	both := []session.Checkpoint{{ID: 1, Tick: 600}, {ID: 2, Tick: 1200}}
	tests := []struct {
		name         string
		state        session.State
		wantDisabled bool
		wantLabel    string
	}{
		{name: "state before the save point", state: session.State{Epoch: "a", Revision: 10, Checkpoints: older}, wantDisabled: true, wantLabel: "Rewind 10.0 s"},
		{name: "state with the save point", state: session.State{Epoch: "a", Revision: 11, Checkpoints: both}, wantLabel: "Rewind 20.0 s"},
		{name: "newer state", state: session.State{Epoch: "a", Revision: 14, Checkpoints: both}, wantLabel: "Rewind 20.0 s"},
		{name: "new epoch", state: session.State{Epoch: "b", Revision: 3, Checkpoints: older}, wantLabel: "Rewind 10.0 s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.state = session.State{Epoch: "a", Revision: 10, Checkpoints: older}
			game.handleResult(remote.Result{
				Command: session.Command{Action: "checkpoint"},
				Reply:   session.Reply{Epoch: "a", Revision: 11, Checkpoint: 2},
			})
			game.state = test.state
			rewind := findButton(t, game.buttons(), "rewind")
			if rewind.disabled != test.wantDisabled || rewind.label != test.wantLabel {
				t.Fatalf("rewind button = %q disabled %t, want %q disabled %t", rewind.label, rewind.disabled, test.wantLabel, test.wantDisabled)
			}
		})
	}
}

func TestRewindTarget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		checkpoints []session.Checkpoint
		want        session.Checkpoint
		wantOK      bool
	}{
		{name: "none"},
		{name: "empty list", checkpoints: []session.Checkpoint{}},
		{name: "one", checkpoints: []session.Checkpoint{{ID: 4, Tick: 60}}, want: session.Checkpoint{ID: 4, Tick: 60}, wantOK: true},
		{name: "oldest first", checkpoints: []session.Checkpoint{{ID: 2, Tick: 60}, {ID: 3, Tick: 120}, {ID: 7, Tick: 30}}, want: session.Checkpoint{ID: 7, Tick: 30}, wantOK: true},
		{name: "unsorted", checkpoints: []session.Checkpoint{{ID: 5, Tick: 300}, {ID: 9, Tick: 90, RestoresProject: true}, {ID: 1, Tick: 900}}, want: session.Checkpoint{ID: 9, Tick: 90, RestoresProject: true}, wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := rewindTarget(session.State{Checkpoints: test.checkpoints})
			if got != test.want || ok != test.wantOK {
				t.Fatalf("rewindTarget = %+v, %t, want %+v, %t", got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestCommandResultNotices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		result         remote.Result
		wantNotice     string
		wantMessage    string
		wantOrderLabel string
		// wantOrders is true when the result opens Orders in place of Demand.
		wantOrders bool
	}{
		{
			name:           "checkpoint",
			result:         remote.Result{Command: session.Command{Action: "checkpoint"}, Reply: session.Reply{ProjectRevision: 1, Checkpoint: 3}},
			wantNotice:     "Save point #3 saved.",
			wantOrderLabel: "Order",
		},
		{
			name:           "rewind",
			result:         remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 3}, Reply: session.Reply{ProjectRevision: 1, Generation: 2}},
			wantNotice:     "Rewound to save point #3 (42.0 s). Paused.",
			wantOrderLabel: "Order",
		},
		{
			name:           "rewind restores project",
			result:         remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 2}, Reply: session.Reply{ProjectRevision: 2, Generation: 2, ProjectRestored: true}},
			wantNotice:     "Rewound to save point #2 (10.0 s). Paused. Project settings restored.",
			wantOrderLabel: "Order",
		},
		{
			// Another browser restored the project of the save point before
			// this rewind. The project revision went up, but this rewind
			// restored nothing.
			name:           "rewind after another restore",
			result:         remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 2}, Reply: session.Reply{ProjectRevision: 3, Generation: 3}},
			wantNotice:     "Rewound to save point #2 (10.0 s). Paused.",
			wantOrderLabel: "Order",
		},
		{
			name:           "rewind to unlisted save point",
			result:         remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 9}, Reply: session.Reply{ProjectRevision: 1, Generation: 2}},
			wantNotice:     "Rewound to save point #9. Paused.",
			wantOrderLabel: "Order",
		},
		{
			name:           "trip",
			result:         remote.Result{Command: session.Command{Action: "trip", Origin: "station-01", Destination: "station-02"}, Reply: session.Reply{OrderID: 7}},
			wantNotice:     "Order #7 accepted: Station 01 > Station 02. See Orders for status.",
			wantOrderLabel: "Order accepted",
			wantOrders:     true,
		},
		{
			name:           "rejected rewind",
			result:         remote.Result{Command: session.Command{Action: "rewind", Checkpoint: 1}, Reply: session.Reply{ErrorCode: session.CommandRejected, Error: "save point #1 is no longer available"}},
			wantMessage:    "save point #1 is no longer available",
			wantOrderLabel: "Order",
		},
		{
			name:           "transport error",
			result:         remote.Result{Command: session.Command{Action: "checkpoint"}, Err: errors.New("send command: connection refused")},
			wantMessage:    "send command: connection refused",
			wantOrderLabel: "Order",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.state.Checkpoints = []session.Checkpoint{{ID: 2, Tick: 10 * sim.TicksPerSecond}, {ID: 3, Tick: 42 * sim.TicksPerSecond}}
			game.state.Simulation.Vehicles = make([]sim.Vehicle, 8)
			game.message = "Sending command..."
			game.showDemand, game.selected, game.podPage = true, 7, 1
			game.handleResult(test.result)
			if game.notice != test.wantNotice || game.message != test.wantMessage {
				t.Fatalf("notice %q message %q, want notice %q message %q", game.notice, game.message, test.wantNotice, test.wantMessage)
			}
			if test.wantNotice != "" && game.noticeTicks != 180 {
				t.Fatalf("notice ticks = %d, want 180", game.noticeTicks)
			}
			if label := findButton(t, game.buttons(), "request").label; label != test.wantOrderLabel {
				t.Fatalf("order button = %q, want %q", label, test.wantOrderLabel)
			}
			// Only a trip changes the panel. No result changes the selected pod.
			if game.showOrders != test.wantOrders || game.showDemand != !test.wantOrders {
				t.Fatalf("orders %t demand %t, want orders %t demand %t", game.showOrders, game.showDemand, test.wantOrders, !test.wantOrders)
			}
			if game.selected != 7 || game.podPage != 1 {
				t.Fatalf("selection = pod %d page %d, want pod 7 page 1", game.selected, game.podPage)
			}
		})
	}
}

// TestSavePointClicks clicks Save point and Rewind in a game that is
// connected to a session server. It checks the command that each click
// sends and the notice for each reply.
func TestSavePointClicks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// changeDemand changes the demand seed after the save point. The
		// rewind then restores the earlier demand settings.
		changeDemand    bool
		wantRewindLabel string
		wantNotice      string
	}{
		{name: "same project", wantRewindLabel: "Rewind 0.0 s", wantNotice: "Rewound to save point #1 (0.0 s). Paused."},
		{name: "demand changed", changeDemand: true, wantRewindLabel: "Rewind + project", wantNotice: "Rewound to save point #1 (0.0 s). Paused. Project settings restored."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedTestGame(t)
			saved := clickCommand(t, game, "checkpoint")
			if saved.Command.Action != "checkpoint" || saved.Reply.Checkpoint != 1 || game.notice != "Save point #1 saved." {
				t.Fatalf("save point click sent %q with reply %+v and notice %q", saved.Command.Action, saved.Reply, game.notice)
			}
			syncGame(t, game, func() bool { return len(game.state.Checkpoints) == 1 })
			if test.changeDemand {
				game.showDemand = true
				clickCommand(t, game, "demand-seed")
				syncGame(t, game, func() bool { return len(game.state.Checkpoints) == 1 && game.state.Checkpoints[0].RestoresProject })
			}
			if label := findButton(t, game.buttons(), "rewind").label; label != test.wantRewindLabel {
				t.Fatalf("rewind button = %q, want %q", label, test.wantRewindLabel)
			}
			rewound := clickCommand(t, game, "rewind")
			if rewound.Command.Action != "rewind" || rewound.Command.Checkpoint != saved.Reply.Checkpoint {
				t.Fatalf("rewind click sent %q to save point %d, want rewind to %d", rewound.Command.Action, rewound.Command.Checkpoint, saved.Reply.Checkpoint)
			}
			if game.notice != test.wantNotice || game.message != "" {
				t.Fatalf("notice %q message %q, want notice %q", game.notice, game.message, test.wantNotice)
			}
			syncGame(t, game, func() bool { return game.state.Generation == rewound.Reply.Generation })
			if label := findButton(t, game.buttons(), "pause").label; label != "Resume [Space]" {
				t.Fatalf("pause button = %q after rewind, want %q", label, "Resume [Space]")
			}
		})
	}
}

// sharedTestGame returns a game that is connected over HTTP to a new session.
// The session clock does not run.
func sharedTestGame(t *testing.T) *Game {
	t.Helper()
	shared, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	server := httptest.NewServer(shared.HandlerFS(fstest.MapFS{}))
	t.Cleanup(server.Close)
	game := journeyTestGame(t, 2)
	game.client = remote.New(t.Context(), server.URL)
	syncGame(t, game, func() bool { return game.state.Epoch != "" })
	return game
}

// clickCommand clicks the button for action and waits for the reply. It
// gives the reply to handleResult, as readRemote does, and returns it.
func clickCommand(t *testing.T, game *Game, action string) remote.Result {
	t.Helper()
	game.click(centerOfButton(findButton(t, game.buttons(), action)))
	if !game.pending {
		t.Fatalf("%s click sent no command: %q", action, game.message)
	}
	select {
	case result := <-game.client.Results():
		if result.Err != nil || result.Reply.Error != "" {
			t.Fatalf("%s command failed: %+v", action, result)
		}
		game.handleResult(result)
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("%s command timed out", action)
		return remote.Result{}
	}
}

// syncGame reads the client state until the game is connected, has no
// pending command, and ready is true.
func syncGame(t *testing.T, game *Game, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		game.readRemote()
		if game.connected && !game.pending && ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("shared state timed out")
}
