package view

import (
	"cmp"
	"net/http"
	"sync"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// faultTestGame returns a connected game with three pods and the fault
// marker. The second pod is selected.
func faultTestGame(t *testing.T) *Game {
	t.Helper()
	game := journeyTestGame(t, 2)
	game.state.Simulation.FaultContract = sim.FaultV1Contract
	game.state.Simulation.Vehicles = []sim.Vehicle{{Pod: sim.Pod{ID: "pod-a"}}, {Pod: sim.Pod{ID: "pod-b"}}, {Pod: sim.Pod{ID: "pod-c"}}}
	game.selected = 1
	return game
}

// TestFaultButton checks when the pod inspector shows the fault button,
// and its label, its target, and its state. The button shows only with
// the fault marker and while the inspector shows. Only a pod fault of the
// selected pod changes it to Clear fault. A pause does not disable it: the
// server accepts both commands in a paused session.
func TestFaultButton(t *testing.T) {
	t.Parallel()
	podFaultA := sim.FaultView{ID: "i1.1", Kind: sim.FaultKindPod, PodID: "pod-a"}
	podFaultB := sim.FaultView{ID: "i1.4", Kind: sim.FaultKindPod, PodID: "pod-b"}
	debris := sim.FaultView{ID: "i1.2", Kind: sim.FaultKindDebris, LaneID: "lane-1"}
	tests := []struct {
		name   string
		change func(*Game)
		// wantShown is false when the button must not show. Then the other
		// members do not apply.
		wantShown    bool
		wantLabel    string
		wantAction   string
		wantDisabled bool
	}{
		{name: "no fault marker", change: func(g *Game) { g.state.Simulation.FaultContract = "" }},
		{name: "orders panel", change: func(g *Game) { g.showOrders = true }},
		{name: "demand panel", change: func(g *Game) { g.showDemand = true }},
		{name: "no pods", change: func(g *Game) { g.state.Simulation.Vehicles, g.selected = nil, 0 }},
		{name: "healthy pod", change: func(*Game) {}, wantShown: true, wantLabel: "Fault", wantAction: "fault/pod-b"},
		{name: "faults of other targets", change: func(g *Game) {
			g.state.Simulation.Faults.Active = []sim.FaultView{podFaultA, debris}
		}, wantShown: true, wantLabel: "Fault", wantAction: "fault/pod-b"},
		{name: "debris and a pod without an ID", change: func(g *Game) {
			g.state.Simulation.Faults.Active = []sim.FaultView{{ID: "i1.3", Kind: sim.FaultKindDebris}}
			g.state.Simulation.Vehicles[1].Pod.ID = ""
		}, wantShown: true, wantLabel: "Fault", wantAction: "fault/"},
		{name: "faulted pod", change: func(g *Game) {
			g.state.Simulation.Faults.Active = []sim.FaultView{podFaultA, debris, podFaultB}
		}, wantShown: true, wantLabel: "Clear fault", wantAction: "clear-fault/i1.4"},
		{name: "paused", change: func(g *Game) { g.state.Simulation.Paused = true }, wantShown: true, wantLabel: "Fault", wantAction: "fault/pod-b"},
		{name: "command waits", change: func(g *Game) { g.pending = true }, wantShown: true, wantLabel: "Fault", wantAction: "fault/pod-b", wantDisabled: true},
		{name: "connection lost", change: func(g *Game) {
			g.connected = false
			g.state.Simulation.Faults.Active = []sim.FaultView{podFaultB}
		}, wantShown: true, wantLabel: "Clear fault", wantAction: "clear-fault/i1.4", wantDisabled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := faultTestGame(t)
			test.change(game)
			var shown []button
			for _, control := range game.buttons() {
				if _, ok := faultCommand(control.action); ok {
					shown = append(shown, control)
				}
			}
			if !test.wantShown {
				if len(shown) != 0 {
					t.Fatalf("fault buttons = %+v, want none", shown)
				}
				return
			}
			if len(shown) != 1 {
				t.Fatalf("fault buttons = %+v, want one", shown)
			}
			got := shown[0]
			if got.label != test.wantLabel || got.action != test.wantAction || got.disabled != test.wantDisabled {
				t.Fatalf("fault button %q action %q disabled %t, want %q action %q disabled %t", got.label, got.action, got.disabled, test.wantLabel, test.wantAction, test.wantDisabled)
			}
		})
	}
}

// TestFaultCommand checks the command of each fault button action. A
// fault has no duration, so it lasts until a user clears it.
func TestFaultCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action string
		want   session.Command
		wantOK bool
	}{
		{action: "fault/pod-b", want: session.Command{Action: "fault", PodID: "pod-b"}, wantOK: true},
		{action: "clear-fault/i1.4", want: session.Command{Action: "clearFault", FaultID: "i1.4"}, wantOK: true},
		{action: "pod/pod-b"},
		{action: "pause"},
	}
	for _, test := range tests {
		got, ok := faultCommand(test.action)
		if ok != test.wantOK || got.Action != test.want.Action || got.PodID != test.want.PodID || got.FaultID != test.want.FaultID || got.DurationSeconds != nil {
			t.Errorf("faultCommand(%q) = %+v %t, want %+v %t", test.action, got, ok, test.want, test.wantOK)
		}
	}
}

// TestFaultButtonFits checks in each layout that both labels of the fault
// button fit in it, that it is inside the right panel, and that each
// activity label of the inspector ends before it.
func TestFaultButtonFits(t *testing.T) {
	t.Parallel()
	var activities []string
	for _, purpose := range []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty} {
		activities = append(activities, activityLabel(sim.Pod{WaitReason: sim.TrackOccupied}, purpose))
		for _, activity := range []sim.Activity{sim.Idle, sim.DepartingEmpty, sim.Boarding, sim.Traveling, sim.Unloading, sim.Continuing} {
			activities = append(activities, activityLabel(sim.Pod{Activity: activity}, purpose))
		}
	}
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.state.Simulation.FaultContract = sim.FaultV1Contract
			for _, faults := range [][]sim.FaultView{nil, {{ID: "i1.1", Kind: sim.FaultKindPod, PodID: fleetPodLabel(0)}}} {
				game.state.Simulation.Faults.Active = faults
				control, ok := game.faultButton(game.state.Simulation)
				if !ok {
					t.Fatal("fault button does not show")
				}
				control = game.layoutButton(control)
				if got := game.fitButtonText(control.label, cmp.Or(control.fontSize, 14), control.w); got != control.label {
					t.Errorf("label %q shortened to %q in width %g", control.label, got, control.w)
				}
				panel, got := game.rightPanelArea(), buttonArea(control)
				if got.left < panel.left || got.top < panel.top || got.right > panel.right || got.bottom > panel.bottom {
					t.Errorf("fault button %+v escapes right panel %+v", got, panel)
				}
				for _, activity := range activities {
					if value := game.labelArea(activityLine(activity, foreground)); value.overlaps(got) {
						t.Errorf("activity %q %+v overlaps fault button %+v", activity, value, got)
					}
				}
			}
		})
	}
}

// faultProject returns the default project with the fault marker.
func faultProject() project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.FaultContract = project.FaultV1Contract
	config.Faults = &project.FaultConfig{}
	return config
}

// clickFault draws a frame without an image, clicks the fault button with
// action, and waits for the reply, as clickCommand does.
func clickFault(t *testing.T, game *Game, action string) remote.Result {
	t.Helper()
	game.frameButtons()
	return clickCommand(t, game, action)
}

// TestFaultButtonCommands clicks Fault and then Clear fault in a game that
// is connected to a session with the fault marker. Fault sends the ID of
// the selected pod and no duration. Clear fault sends the fault ID from the
// faults group.
func TestFaultButtonCommands(t *testing.T) {
	t.Parallel()
	game := sharedProjectGame(t, faultProject())
	podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
	if label := findButton(t, game.buttons(), faultActionPrefix+podID).label; label != "Fault" {
		t.Fatalf("label = %q, want %q", label, "Fault")
	}
	result := clickFault(t, game, faultActionPrefix+podID)
	if command := result.Command; command.Action != "fault" || command.PodID != podID || command.DurationSeconds != nil {
		t.Fatalf("fault command = %+v, want pod %q without a duration", command, podID)
	}
	faultID := result.Reply.FaultID
	if faultID == "" {
		t.Fatalf("fault reply %+v has no fault ID", result.Reply)
	}
	syncGame(t, game, func() bool {
		fault, ok := podFault(game.state.Simulation.Faults, podID)
		return ok && fault.ID == faultID
	})
	if label := findButton(t, game.buttons(), clearFaultActionPrefix+faultID).label; label != "Clear fault" {
		t.Fatalf("label = %q, want %q", label, "Clear fault")
	}
	result = clickFault(t, game, clearFaultActionPrefix+faultID)
	if command := result.Command; command.Action != "clearFault" || command.FaultID != faultID {
		t.Fatalf("clear command = %+v, want fault %q", command, faultID)
	}
	syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 0 })
	findButton(t, game.buttons(), faultActionPrefix+podID)
}

// TestFaultClickUsesShownButton changes the state after a frame and before
// a click on the fault button of that frame. The click sends the command
// that the frame showed, because the user saw that button. The server can
// refuse it. A button that the frame did not show does not take a click.
// The connection and a waiting command come from the current state.
func TestFaultClickUsesShownButton(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// show prepares the state of the frame. change changes the
		// state after the frame. Both get the selected pod and, for
		// a faulted pod, its fault ID.
		show, change func(game *Game, podID, faultID string)
		// faulted starts a fault on the selected pod before the frame.
		faulted bool
		// want is the command of the click, or nil for no command.
		want func(podID, faultID string) *session.Command
	}{
		{
			name:    "fault clears",
			faulted: true,
			change:  func(game *Game, _, _ string) { game.state.Simulation.Faults = sim.FaultsView{} },
			want: func(_, faultID string) *session.Command {
				return &session.Command{Action: "clearFault", FaultID: faultID}
			},
		},
		{
			name: "fault starts",
			change: func(game *Game, podID, _ string) {
				game.state.Simulation.Faults.Active = []sim.FaultView{{ID: "i9.9", Kind: sim.FaultKindPod, PodID: podID}}
			},
			want: func(podID, _ string) *session.Command { return &session.Command{Action: "fault", PodID: podID} },
		},
		{
			name: "fleet changes",
			change: func(game *Game, _, _ string) {
				vehicles := game.state.Simulation.Vehicles
				vehicles[0], vehicles[1] = vehicles[1], vehicles[0]
			},
			want: func(podID, _ string) *session.Command { return &session.Command{Action: "fault", PodID: podID} },
		},
		{
			name:   "selection changes",
			change: func(game *Game, _, _ string) { game.selected = 1 },
			want:   func(podID, _ string) *session.Command { return &session.Command{Action: "fault", PodID: podID} },
		},
		{
			name:   "button not shown",
			show:   func(game *Game, _, _ string) { game.showOrders = true },
			change: func(game *Game, _, _ string) { game.showOrders = false },
		},
		{
			name:   "connection lost",
			change: func(game *Game, _, _ string) { game.connected = false },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedProjectGame(t, faultProject())
			podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
			var faultID string
			if test.faulted {
				faultID = clickFault(t, game, faultActionPrefix+podID).Reply.FaultID
				syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
			}
			// The click point is the fault button of a frame that shows
			// it, also when the frame of the test does not.
			action := faultActionPrefix + podID
			if faultID != "" {
				action = clearFaultActionPrefix + faultID
			}
			point := centerOfButton(findButton(t, game.frameButtons(), action))
			if test.show != nil {
				test.show(game, podID, faultID)
			}
			game.frameButtons()
			test.change(game, podID, faultID)
			game.click(point)
			if test.want == nil {
				if game.pending || game.message != "" {
					t.Fatalf("click sent a command: pending %t message %q", game.pending, game.message)
				}
				return
			}
			if !game.pending {
				t.Fatalf("click sent no command: %q", game.message)
			}
			want, got := test.want(podID, faultID), commandResult(t, game).Command
			if got.Action != want.Action || got.PodID != want.PodID || got.FaultID != want.FaultID {
				t.Fatalf("command = %+v, want %+v", got, *want)
			}
		})
	}
}

// TestDrawKeepsFaultButton draws one frame and checks that the frame
// keeps its fault button for a click.
func TestDrawKeepsFaultButton(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	game.state.Simulation.FaultContract = sim.FaultV1Contract
	screen := ebiten.NewImage(game.layout.width, game.layout.height)
	defer screen.Deallocate()
	game.Draw(screen)
	if want := faultActionPrefix + "01"; !game.faultShown || game.shownFault.action != want {
		t.Fatalf("frame fault button %+v shown %t, want action %q", game.shownFault, game.faultShown, want)
	}
}

// TestFaultRefusalShowsError clicks the fault button when the server
// refuses the command. The view sends the command and shows the error of
// the server in the message line.
func TestFaultRefusalShowsError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// action returns the fault button action that the test clicks.
		// It can change the state of game, as a state that is not yet
		// current does.
		action func(t *testing.T, game *Game) string
		want   string
	}{
		{name: "faulted pod in an old state", action: func(t *testing.T, game *Game) string {
			t.Helper()
			podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
			clickFault(t, game, faultActionPrefix+podID)
			syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
			game.state.Simulation.Faults = sim.FaultsView{}
			return faultActionPrefix + podID
		}, want: "pod already has a fault"},
		{name: "cleared fault in an old state", action: func(t *testing.T, game *Game) string {
			t.Helper()
			podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
			faultID := clickFault(t, game, faultActionPrefix+podID).Reply.FaultID
			syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
			clickFault(t, game, clearFaultActionPrefix+faultID)
			syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 0 })
			game.state.Simulation.Faults.Active = []sim.FaultView{{ID: faultID, Kind: sim.FaultKindPod, PodID: podID}}
			return clearFaultActionPrefix + faultID
		}, want: "unknown fault"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedProjectGame(t, faultProject())
			action := test.action(t, game)
			game.click(centerOfButton(findButton(t, game.frameButtons(), action)))
			if !game.pending {
				t.Fatalf("click sent no command: %q", game.message)
			}
			result := commandResult(t, game)
			if result.Err != nil || result.Reply.Error != test.want {
				t.Fatalf("result = %+v, want error %q", result, test.want)
			}
			game.handleResult(result)
			if got := game.hintLine(game.state.Simulation, ""); got.value != test.want || got.color != amber {
				t.Fatalf("hint line = %q color %#06x, want %q color %#06x", got.value, got.color, test.want, amber)
			}
		})
	}
}

// TestFaultSubmitErrorShows clicks Fault while the client still waits for
// the previous command, as after a click in the same frame as the first
// one. The client does not send the command, and the message line shows
// why.
func TestFaultSubmitErrorShows(t *testing.T) {
	t.Parallel()
	// The server holds each command until release runs. The client thus
	// waits for the first command during the second click.
	held := make(chan struct{})
	release := sync.OnceFunc(func() { close(held) })
	game := sharedHandlerGame(t, faultProject(), func(handler http.Handler) http.Handler {
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
	action := faultActionPrefix + game.state.Simulation.Vehicles[game.selected].Pod.ID
	game.click(centerOfButton(findButton(t, game.frameButtons(), action)))
	if !game.pending {
		t.Fatalf("click sent no command: %q", game.message)
	}
	game.pending = false
	game.click(centerOfButton(findButton(t, game.frameButtons(), action)))
	const want = "waiting for the previous command"
	if got := game.hintLine(game.state.Simulation, ""); got.value != want || got.color != amber {
		t.Fatalf("hint line = %q color %#06x, want %q color %#06x", got.value, got.color, want, amber)
	}
	release()
	if result := commandResult(t, game); result.Err != nil || result.Reply.Error != "" || result.Reply.FaultID == "" {
		t.Fatalf("first fault command failed: %+v", result)
	}
}
