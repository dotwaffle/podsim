package view

import (
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestMapNotice checks the text over the map for each connection state.
func TestMapNotice(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		epoch     string
		connected bool
		lastFrame time.Time
		want      string
		wantStale bool
	}{
		{name: "before the first frame", want: connectingMessage},
		{name: "connected before the first frame", connected: true, want: connectingMessage},
		{name: "connected", epoch: "a", connected: true, lastFrame: now},
		{name: "lost at once", epoch: "a", lastFrame: now, want: "Connection lost. Showing state from 0 s ago.", wantStale: true},
		{name: "lost for part of a second", epoch: "a", lastFrame: now.Add(-900 * time.Millisecond), want: "Connection lost. Showing state from 0 s ago.", wantStale: true},
		{name: "lost for seconds", epoch: "a", lastFrame: now.Add(-12900 * time.Millisecond), want: "Connection lost. Showing state from 12 s ago.", wantStale: true},
		{name: "lost for minutes", epoch: "a", lastFrame: now.Add(-2 * time.Minute), want: "Connection lost. Showing state from 120 s ago.", wantStale: true},
		{name: "last frame after now", epoch: "a", lastFrame: now.Add(time.Second), want: "Connection lost. Showing state from 0 s ago.", wantStale: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{state: session.State{Epoch: test.epoch}, connected: test.connected, lastFrame: test.lastFrame}
			got, stale := game.mapNotice(now)
			if got != test.want || stale != test.wantStale {
				t.Errorf("mapNotice() = %q, %t, want %q, %t", got, stale, test.want, test.wantStale)
			}
		})
	}
}

// TestMapBannerFitsLayout checks that the banner is a 32u strip at the top
// of the map, and that a long banner text fits in the center of it.
func TestMapBannerFitsLayout(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			banner, viewport := game.mapBanner(), game.layout.mapViewport
			if !banner.In(viewport) || banner.Min != viewport.Min || banner.Dx() != viewport.Dx() || banner.Dy() != int(math.Round(32*game.layout.unit)) {
				t.Fatalf("banner %v is not a 32u strip at the top of map %v", banner, viewport)
			}
			value := game.mapBannerLabel(staleStateText(99999 * time.Second))
			if !value.physical {
				t.Fatal("banner text position is not in physical pixels")
			}
			width, height := text.Measure(value.value, game.textFace(value.size), 0)
			left, right := value.x-float64(banner.Min.X), float64(banner.Max.X)-(value.x+width)
			top, bottom := value.y-float64(banner.Min.Y), float64(banner.Max.Y)-(value.y+height)
			if min(left, right, top, bottom) < 0 {
				t.Errorf("banner text %q at %g,%g size %gx%g escapes banner %v", value.value, value.x, value.y, width, height, banner)
			}
			if math.Abs(left-right) > 1 || math.Abs(top-bottom) > 1 {
				t.Errorf("banner text has margins %g, %g, %g, %g (left, right, top, bottom), want it in the center", left, right, top, bottom)
			}
		})
	}
}

// TestConnectionStateFromServer runs a game against a server that goes
// offline. The map shows the connecting message before the first frame, and
// no notice with a connection. After the connection is lost, the banner
// gives the age of the last frame that the client read.
func TestConnectionStateFromServer(t *testing.T) {
	t.Parallel()
	handler := buildTestHandler(t, "build-a")
	var offline atomic.Bool
	offline.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	game, err := New(t.Context(), server.URL)
	if err != nil {
		t.Fatalf("create game: %v", err)
	}
	check := func(when string, now time.Time, want string, wantStale bool) {
		t.Helper()
		if got, stale := game.mapNotice(now); got != want || stale != wantStale {
			t.Fatalf("mapNotice() %s = %q, %t, want %q, %t", when, got, stale, want, wantStale)
		}
	}
	game.readRemote()
	check("before the first frame", time.Now(), connectingMessage, false)
	before := time.Now()
	offline.Store(false)
	syncGame(t, game, func() bool { return game.state.Epoch != "" })
	check("with a connection", time.Now(), "", false)
	offline.Store(true)
	deadline := time.Now().Add(5 * time.Second)
	for game.connected {
		if time.Now().After(deadline) {
			t.Fatal("the game did not see the lost connection")
		}
		time.Sleep(5 * time.Millisecond)
		game.readRemote()
	}
	lost := time.Now()
	if game.lastFrame.Before(before) || game.lastFrame.After(lost) {
		t.Fatalf("last frame at %v, want a time from %v to %v", game.lastFrame, before, lost)
	}
	check("after the connection is lost", lost, staleStateText(lost.Sub(game.lastFrame)), true)
}

// TestHeaderLabelsBeforeFirstFrame checks that the header does not count
// stations and pods before the first state frame.
func TestHeaderLabelsBeforeFirstFrame(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		epoch     string
		wantCount bool
	}{
		{name: "before the first frame"},
		{name: "after the first frame", epoch: "a", wantCount: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			game.state.Epoch = test.epoch
			const count = "3 STATIONS     2 PODS"
			got := slices.ContainsFunc(game.headerLabels(), func(header label) bool { return header.value == count })
			if got != test.wantCount {
				t.Errorf("header shows %q: %t, want %t", count, got, test.wantCount)
			}
		})
	}
}

// TestSelectNextPod checks the Tab selection, also for a game without pods.
// A new selection closes Orders and Demand and clears the message. Without
// pods, Tab changes nothing.
func TestSelectNextPod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		pods, selected     int
		wantSelected       int
		wantInspectionOpen bool
	}{
		{name: "no pods"},
		{name: "next pod", pods: 3, wantSelected: 1, wantInspectionOpen: true},
		{name: "after the last pod", pods: 3, selected: 2, wantInspectionOpen: true},
		{name: "second page", pods: 8, selected: 5, wantSelected: 6, wantInspectionOpen: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{selected: test.selected, showOrders: true, showDemand: true, message: "x"}
			game.state.Simulation.Vehicles = make([]sim.Vehicle, test.pods)
			game.selectNextPod()
			if game.selected != test.wantSelected || game.podPage != test.wantSelected/6 {
				t.Errorf("selected %d on page %d, want %d on page %d", game.selected, game.podPage, test.wantSelected, test.wantSelected/6)
			}
			wantPanels, wantMessage := !test.wantInspectionOpen, "x"
			if test.wantInspectionOpen {
				wantMessage = ""
			}
			if game.showOrders != wantPanels || game.showDemand != wantPanels || game.message != wantMessage {
				t.Errorf("Orders %t, Demand %t, message %q, want %t, %t, %q",
					game.showOrders, game.showDemand, game.message, wantPanels, wantPanels, wantMessage)
			}
		})
	}
}

// TestDrawEmptyState draws the game before the first state frame, with a
// state that has no pods, with a selected pod that the state does not have,
// and with a lost connection. Draw, Follow, and Tab must not panic.
func TestDrawEmptyState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(game *Game)
	}{
		{name: "before the first frame", setup: func(game *Game) {
			game.network, game.state = sim.Network{}, session.State{Speed: 1}
			game.origin, game.destination = "", ""
			game.connected = false
		}},
		{name: "before the first frame with a pod selected", setup: func(game *Game) {
			game.network, game.state = sim.Network{}, session.State{Speed: 1}
			game.selected, game.followSelected = 1, true
			game.connected = false
		}},
		{name: "selected pod missing", setup: func(game *Game) { game.selected = 2 }},
		{name: "state without pods", setup: func(game *Game) { game.state.Simulation.Vehicles = nil }},
		{name: "connection lost", setup: func(game *Game) {
			game.connected, game.lastFrame = false, time.Now().Add(-5*time.Second)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			test.setup(game)
			screen := ebiten.NewImage(game.layout.width, game.layout.height)
			defer screen.Deallocate()
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("panic: %v", recovered)
				}
			}()
			game.Draw(screen)
			if game.followSelected {
				game.followSelectedPod()
			}
			game.selectNextPod()
			game.Draw(screen)
		})
	}
}

// exampleTestGame returns a connected game after the first state frame. The
// game has the example network and two pods.
func exampleTestGame(t *testing.T) *Game {
	t.Helper()
	network := sim.Example()
	simulation, err := sim.NewFleet(network, []sim.Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatalf("create fleet: %v", err)
	}
	game := journeyTestGame(t, 2)
	game.network = network
	game.origin, game.destination = "harbor", "market"
	game.state = session.State{Epoch: "test", Speed: 1, Simulation: simulation.Snapshot()}
	return game
}
