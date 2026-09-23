package view

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestOrderAcceptedLabel checks that the request button reads Order
// accepted for as long as the notice of the accepted order shows, and only
// while From and To are the stations of that order.
func TestOrderAcceptedLabel(t *testing.T) {
	t.Parallel()
	const notice = "Order #7 accepted: Station 01 > Station 02. See Orders for status."
	tests := []struct {
		name string
		// ticks is the number of game ticks after the result. clicks are
		// the station chips that the user clicks after these ticks.
		ticks      int
		clicks     []string
		wantLabel  string
		wantNotice string
	}{
		{name: "at once", wantLabel: "Order accepted", wantNotice: notice},
		{name: "after 0.5 s", ticks: sim.TicksPerSecond / 2, wantLabel: "Order accepted", wantNotice: notice},
		{name: "last notice tick", ticks: noticeDuration - 1, wantLabel: "Order accepted", wantNotice: notice},
		{name: "after the notice", ticks: noticeDuration, wantLabel: "Order"},
		{name: "other destination", ticks: 1, clicks: []string{"to/station-03"}, wantLabel: "Order", wantNotice: notice},
		{name: "same station", ticks: 1, clicks: []string{"to/station-01"}, wantLabel: "Order", wantNotice: notice},
		{name: "stations of the order again", ticks: 1, clicks: []string{"to/station-03", "to/station-02"}, wantLabel: "Order accepted", wantNotice: notice},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 3)
			game.handleResult(remote.Result{
				Command: session.Command{Action: "trip", Origin: "station-01", Destination: "station-02"},
				Reply:   session.Reply{OrderID: 7},
			})
			for range test.ticks {
				game.tickNotice()
			}
			for _, action := range test.clicks {
				game.click(centerOfButton(findButton(t, game.buttons(), action)))
			}
			if label := findButton(t, game.buttons(), "request").label; label != test.wantLabel || game.notice != test.wantNotice {
				t.Fatalf("order button %q with notice %q, want %q with notice %q", label, game.notice, test.wantLabel, test.wantNotice)
			}
		})
	}
}

// TestCommandWaitShowsInFooter clicks a control that sends a command in a
// game that is connected to a session server. While the command waits for
// the server, the line below the panels tells the user, and the hint line
// shows the hint in muted text. An old message and an old notice clear, so
// the request button no longer reads Order accepted.
func TestCommandWaitShowsInFooter(t *testing.T) {
	t.Parallel()
	const (
		hint    = "Choose pickup and destination."
		waiting = "Shared session / waiting for command confirmation"
	)
	for _, action := range []string{"pause", "speed", "checkpoint", "request"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			game := sharedTestGame(t)
			game.handleResult(remote.Result{
				Command: session.Command{Action: "trip", Origin: game.origin, Destination: game.destination},
				Reply:   session.Reply{OrderID: 1},
			})
			game.message = "save point #1 is no longer available"
			if label := findButton(t, game.buttons(), "request").label; label != "Order accepted" {
				t.Fatalf("order button before the click = %q, want %q", label, "Order accepted")
			}
			game.click(centerOfButton(findButton(t, game.buttons(), action)))
			if !game.pending || game.message != "" || game.notice != "" {
				t.Fatalf("click sent no command or kept old text: pending %t message %q notice %q", game.pending, game.message, game.notice)
			}
			if label := findButton(t, game.buttons(), "request").label; label != "Order" {
				t.Errorf("order button while the command waits = %q, want %q", label, "Order")
			}
			if got := game.hintLine(game.state.Simulation, hint); got.value != hint || got.color != muted {
				t.Errorf("hint line = %q color %#06x, want %q color %#06x", got.value, got.color, hint, muted)
			}
			if got := game.connectionFooter(true); got.value != waiting || got.color != muted {
				t.Errorf("footer = %q color %#06x, want %q color %#06x", got.value, got.color, waiting, muted)
			}
			result := commandResult(t, game)
			if result.Err != nil || result.Reply.Error != "" {
				t.Fatalf("%s command failed: %+v", action, result)
			}
			game.handleResult(result)
			syncGame(t, game, func() bool { return true })
		})
	}
}

// TestStationChipsEnabled checks when the From and To chips accept clicks.
// A chip sends no command, so a command that waits for the server does not
// disable the chips.
func TestStationChipsEnabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                     string
		connected, pending, demo bool
		wantDisabled             bool
	}{
		{name: "connected", connected: true},
		{name: "command waits", connected: true, pending: true},
		{name: "connection lost", wantDisabled: true},
		{name: "traffic demo", connected: true, demo: true, wantDisabled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 4)
			game.connected, game.pending = test.connected, test.pending
			game.state.Simulation.Demo = test.demo
			for _, control := range game.buttons() {
				if !control.expandsWithMap || control.action == "stations-prev" || control.action == "stations-next" {
					continue
				}
				if control.disabled != test.wantDisabled {
					t.Errorf("chip %q disabled %t, want %t", control.action, control.disabled, test.wantDisabled)
				}
			}
		})
	}
}

// TestStationChipsDuringOrder changes From and To while an order waits for
// the server. The chips change only the selection. Order stays disabled,
// and Enter cannot send a second order. The server gets one order for the
// stations that were selected when the user sent it.
func TestStationChipsDuringOrder(t *testing.T) {
	t.Parallel()
	// The server holds each command until release runs. The order thus
	// waits during all the checks, and the result does not depend on
	// timing.
	held := make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	game := sharedHandlerGame(t, project.Default(), func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/command" {
				<-held
			}
			handler.ServeHTTP(w, r)
		})
	})
	// Cleanups run in the opposite order, so release runs before the
	// server closes, also when the test stops early.
	t.Cleanup(release)
	stations := game.passengerStations()
	if len(stations) < 3 {
		t.Fatalf("passenger stations = %d, want 3 or more", len(stations))
	}
	origin, destination := game.origin, game.destination
	game.click(centerOfButton(findButton(t, game.buttons(), "request")))
	if !game.pending {
		t.Fatalf("order click sent no command: %q", game.message)
	}
	var other string
	for _, station := range stations {
		if station.ID != origin && station.ID != destination {
			other = station.ID
			break
		}
	}
	game.click(centerOfButton(findButton(t, game.buttons(), "to/"+other)))
	game.click(centerOfButton(findButton(t, game.buttons(), "from/"+destination)))
	if game.origin != destination || game.destination != other {
		t.Fatalf("selection = %q > %q, want %q > %q", game.origin, game.destination, destination, other)
	}
	order := findButton(t, game.buttons(), "request")
	if !order.disabled {
		t.Fatal("order button is enabled while the order waits for the server")
	}
	game.click(centerOfButton(order))
	game.request()
	const busy = "waiting for the previous command"
	if game.message != busy {
		t.Fatalf("enter while the order waits: message %q, want %q", game.message, busy)
	}
	release()
	result := commandResult(t, game)
	if result.Err != nil || result.Reply.Error != "" || result.Reply.OrderID != 1 {
		t.Fatalf("order failed: %+v", result)
	}
	if result.Command.Origin != origin || result.Command.Destination != destination {
		t.Fatalf("order sent %q > %q, want %q > %q", result.Command.Origin, result.Command.Destination, origin, destination)
	}
	game.handleResult(result)
	syncGame(t, game, func() bool { return outstandingOrderCount(game.state.Simulation) > 0 })
	if count := outstandingOrderCount(game.state.Simulation); count != 1 {
		t.Fatalf("outstanding orders = %d, want 1", count)
	}
}

// TestSameStationOrder selects To and checks that Order is disabled only
// while From and To are the same station. The hint line then tells the
// user why.
func TestSameStationOrder(t *testing.T) {
	t.Parallel()
	const hint = "Choose pickup and destination."
	tests := []struct {
		name, destination string
		wantDisabled      bool
		wantHint          string
	}{
		{name: "different stations", destination: "station-02", wantHint: hint},
		{name: "same station", destination: "station-01", wantDisabled: true, wantHint: sameStationHint},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.click(centerOfButton(findButton(t, game.buttons(), "to/"+test.destination)))
			order := findButton(t, game.buttons(), "request")
			got := game.hintLine(game.state.Simulation, hint)
			if order.disabled != test.wantDisabled || got.value != test.wantHint {
				t.Errorf("%s > %s: order disabled %t with hint %q, want %t with hint %q", game.origin, game.destination, order.disabled, got.value, test.wantDisabled, test.wantHint)
			}
		})
	}
}

// commandResult waits for the result of the command that the game sent.
func commandResult(t *testing.T, game *Game) remote.Result {
	t.Helper()
	select {
	case result := <-game.client.Results():
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("command timed out")
		return remote.Result{}
	}
}
