package view

import (
	"cmp"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// boardParty gives vehicle one occupied party of two that has not
// arrived, as a traveling pod has.
func boardParty(vehicle *sim.Vehicle) {
	vehicle.Pod.Activity, vehicle.Pod.Occupied = sim.Traveling, true
	vehicle.Riders = []sim.Request{{ID: 7, From: "station-01", To: "station-02", PartySize: 2}}
}

// emergencyTestGame returns a connected game with three pods and the
// emergency marker. The second pod is selected and carries a party.
func emergencyTestGame(t *testing.T) *Game {
	t.Helper()
	game := journeyTestGame(t, 2)
	game.state.Simulation.EmergencyContract = sim.EmergencyV1Contract
	game.state.Simulation.Vehicles = []sim.Vehicle{{Pod: sim.Pod{ID: "pod-a"}}, {Pod: sim.Pod{ID: "pod-b"}}, {Pod: sim.Pod{ID: "pod-c"}}}
	boardParty(&game.state.Simulation.Vehicles[0])
	boardParty(&game.state.Simulation.Vehicles[1])
	game.selected = 1
	return game
}

// TestEmergencyButton checks when the pod inspector shows the emergency
// button, and its target and its state. The button shows only with the
// emergency marker, while the inspector shows, on a pod that carries a
// party by the rule of the emergency command, and without an emergency of
// that pod. A pause does not disable it: the server accepts the command in
// a paused session.
func TestEmergencyButton(t *testing.T) {
	t.Parallel()
	emergencyA := sim.EmergencyView{ID: "i1.1", PodID: "pod-a", OrderID: 7}
	emergencyB := sim.EmergencyView{ID: "i1.2", PodID: "pod-b", OrderID: 7}
	tests := []struct {
		name   string
		change func(*Game)
		// wantShown is false when the button must not show. Then
		// wantDisabled does not apply.
		wantShown    bool
		wantDisabled bool
	}{
		{name: "no emergency marker", change: func(g *Game) { g.state.Simulation.EmergencyContract = "" }},
		{name: "orders panel", change: func(g *Game) { g.showOrders = true }},
		{name: "demand panel", change: func(g *Game) { g.showDemand = true }},
		{name: "no pods", change: func(g *Game) { g.state.Simulation.Vehicles, g.selected = nil, 0 }},
		{name: "empty pod", change: func(g *Game) { g.state.Simulation.Vehicles[1] = sim.Vehicle{Pod: sim.Pod{ID: "pod-b"}} }},
		{name: "arrived party", change: func(g *Game) { g.state.Simulation.Vehicles[1].Riders[0].Completed = true }},
		{name: "riders of a pod that is not occupied", change: func(g *Game) { g.state.Simulation.Vehicles[1].Pod.Occupied = false }},
		{name: "boarding party", change: func(g *Game) {
			g.state.Simulation.Vehicles[1].Pod.Activity, g.state.Simulation.Vehicles[1].Pod.Occupied = sim.Boarding, false
		}, wantShown: true},
		{name: "pod with a party", change: func(*Game) {}, wantShown: true},
		{name: "emergency of the pod", change: func(g *Game) {
			g.state.Simulation.Emergencies.Active = []sim.EmergencyView{emergencyA, emergencyB}
		}},
		{name: "emergency of another pod", change: func(g *Game) {
			g.state.Simulation.Emergencies.Active = []sim.EmergencyView{emergencyA}
		}, wantShown: true},
		{name: "paused", change: func(g *Game) { g.state.Simulation.Paused = true }, wantShown: true},
		{name: "command waits", change: func(g *Game) { g.pending = true }, wantShown: true, wantDisabled: true},
		{name: "connection lost", change: func(g *Game) { g.connected = false }, wantShown: true, wantDisabled: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := emergencyTestGame(t)
			test.change(game)
			var shown []button
			for _, control := range game.buttons() {
				if isEmergencyButton(control) {
					shown = append(shown, control)
				}
			}
			if !test.wantShown {
				if len(shown) != 0 {
					t.Fatalf("emergency buttons = %+v, want none", shown)
				}
				return
			}
			if len(shown) != 1 {
				t.Fatalf("emergency buttons = %+v, want one", shown)
			}
			const wantAction = emergencyActionPrefix + "pod-b"
			if got := shown[0]; got.label != "Emergency" || got.action != wantAction || got.disabled != test.wantDisabled {
				t.Fatalf("emergency button %q action %q disabled %t, want %q action %q disabled %t", got.label, got.action, got.disabled, "Emergency", wantAction, test.wantDisabled)
			}
		})
	}
}

// TestEmergencyCommand checks the command of the emergency button action.
// The command has no order ID, so the server takes the first active rider.
func TestEmergencyCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action string
		want   session.Command
		wantOK bool
	}{
		{action: "emergency/pod-b", want: session.Command{Action: "emergency", PodID: "pod-b"}, wantOK: true},
		{action: "fault/pod-b"},
		{action: "pod/pod-b"},
		{action: "pause"},
	}
	for _, test := range tests {
		got, ok := emergencyCommand(test.action)
		if ok != test.wantOK || got != test.want {
			t.Errorf("emergencyCommand(%q) = %+v %t, want %+v %t", test.action, got, ok, test.want, test.wantOK)
		}
	}
}

// TestEmergencyButtonFits checks in each layout, with the fault marker and
// the emergency marker, that the label of the emergency button fits in it
// and that it is inside the right panel. The button must not overlap an
// activity label, the fault button, a full status line, a full journey
// line, or another control.
func TestEmergencyButtonFits(t *testing.T) {
	t.Parallel()
	var activities []string
	for _, purpose := range []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty} {
		activities = append(activities, activityLabel(sim.Pod{WaitReason: sim.TrackOccupied}, purpose))
		for _, activity := range []sim.Activity{sim.Idle, sim.DepartingEmpty, sim.Boarding, sim.Traveling, sim.Unloading, sim.Continuing} {
			activities = append(activities, activityLabel(sim.Pod{Activity: activity}, purpose))
		}
	}
	const long = "Waiting for destination access at Waterloo Underground Station and beyond"
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.state.Simulation.FaultContract = sim.FaultV1Contract
			game.state.Simulation.EmergencyContract = sim.EmergencyV1Contract
			boardParty(&game.state.Simulation.Vehicles[game.selected])
			lines := []label{
				{x: inspectionLeft, y: 151, size: 13, value: game.fitText(long, 13, inspectionRight-inspectionLeft)},
				{x: inspectionLeft, y: 192, size: 17, value: game.fitText(long, 17, inspectionRight-inspectionLeft)},
			}
			for _, faults := range [][]sim.FaultView{nil, {{ID: "i1.1", Kind: sim.FaultKindPod, PodID: fleetPodLabel(game.selected, len(game.state.Simulation.Vehicles))}}} {
				game.state.Simulation.Faults.Active = faults
				controls := game.buttons()
				i := slices.IndexFunc(controls, isEmergencyButton)
				if i < 0 {
					t.Fatal("emergency button does not show")
				}
				control := controls[i]
				if got := game.fitButtonText(control.label, cmp.Or(control.fontSize, 14), control.w); got != control.label {
					t.Errorf("label %q shortened to %q in width %g", control.label, got, control.w)
				}
				panel, got := game.rightPanelArea(), buttonArea(control)
				if got.left < panel.left || got.top < panel.top || got.right > panel.right || got.bottom > panel.bottom {
					t.Errorf("emergency button %+v escapes right panel %+v", got, panel)
				}
				for _, activity := range activities {
					if value := game.labelArea(activityLine(activity, foreground)); value.overlaps(got) {
						t.Errorf("activity %q %+v overlaps emergency button %+v", activity, value, got)
					}
				}
				for _, line := range lines {
					if value := game.labelArea(line); value.overlaps(got) {
						t.Errorf("line at y %g %+v overlaps emergency button %+v", line.y, value, got)
					}
				}
				for j, other := range controls {
					if j != i && buttonArea(other).overlaps(got) {
						t.Errorf("control %q %+v overlaps emergency button %+v", other.action, buttonArea(other), got)
					}
				}
				if faults == nil {
					t.Logf("emergency button %+v, status %+v, journey %+v", got, game.labelArea(lines[0]), game.labelArea(lines[1]))
				}
			}
		})
	}
}

// emergencyProject returns the default project with the emergency marker.
func emergencyProject() project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.EmergencyContract = project.EmergencyV1Contract
	config.Emergencies = &project.EmergencyConfig{}
	return config
}

// TestEmergencyClickUsesShownButton changes the state after a frame and
// before a click on the emergency button of that frame. The click sends
// the command that the frame showed, because the user saw that button.
// The server can refuse it. A button that the frame did not show does not
// take a click. The connection and a waiting command come from the
// current state.
func TestEmergencyClickUsesShownButton(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// show prepares the state of the frame. change changes the
		// state after the frame. Both get the selected pod.
		show, change func(game *Game, podID string)
		// wantCommand is false when the click must send no command.
		wantCommand bool
	}{
		{
			name: "emergency starts",
			change: func(game *Game, podID string) {
				game.state.Simulation.Emergencies.Active = []sim.EmergencyView{{ID: "i9.9", PodID: podID}}
			},
			wantCommand: true,
		},
		{
			name:        "party leaves",
			change:      func(game *Game, _ string) { game.state.Simulation.Vehicles[game.selected].Riders[0].Completed = true },
			wantCommand: true,
		},
		{
			name: "fleet changes",
			change: func(game *Game, _ string) {
				vehicles := game.state.Simulation.Vehicles
				vehicles[0], vehicles[1] = vehicles[1], vehicles[0]
			},
			wantCommand: true,
		},
		{
			name:        "selection changes",
			change:      func(game *Game, _ string) { game.selected = 1 },
			wantCommand: true,
		},
		{
			name:   "button not shown",
			show:   func(game *Game, _ string) { game.showOrders = true },
			change: func(game *Game, _ string) { game.showOrders = false },
		},
		{
			name:   "connection lost",
			change: func(game *Game, _ string) { game.connected = false },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := sharedProjectGame(t, emergencyProject())
			// The pods of the session carry no party, so the server
			// refuses each command. The test checks only the command.
			boardParty(&game.state.Simulation.Vehicles[game.selected])
			podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
			// The click point is the emergency button of a frame that
			// shows it, also when the frame of the test does not.
			point := centerOfButton(findButton(t, game.frameButtons(), emergencyActionPrefix+podID))
			if test.show != nil {
				test.show(game, podID)
			}
			game.frameButtons()
			test.change(game, podID)
			game.click(point)
			if !test.wantCommand {
				if game.pending || game.message != "" {
					t.Fatalf("click sent a command: pending %t message %q", game.pending, game.message)
				}
				return
			}
			if !game.pending {
				t.Fatalf("click sent no command: %q", game.message)
			}
			if got, want := commandResult(t, game).Command, (session.Command{Action: "emergency", PodID: podID}); got.Action != want.Action || got.PodID != want.PodID || got.OrderID != 0 {
				t.Fatalf("command = %+v, want %+v", got, want)
			}
		})
	}
}

// TestDrawKeepsEmergencyButton draws one frame and checks that the frame
// keeps its emergency button for a click.
func TestDrawKeepsEmergencyButton(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	game.state.Simulation.EmergencyContract = sim.EmergencyV1Contract
	boardParty(&game.state.Simulation.Vehicles[0])
	screen := ebiten.NewImage(game.layout.width, game.layout.height)
	defer screen.Deallocate()
	game.Draw(screen)
	if want := emergencyActionPrefix + "01"; !game.emergencyShown || game.shownEmergency.action != want {
		t.Fatalf("frame emergency button %+v shown %t, want action %q", game.shownEmergency, game.emergencyShown, want)
	}
}

// runningEmergencyGame returns a game that is connected over HTTP to a new
// session of emergencyProject. The session clock runs until the test ends.
func runningEmergencyGame(t *testing.T) *Game {
	t.Helper()
	shared, err := session.NewWithProject(emergencyProject(), session.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	server := httptest.NewServer(shared.HandlerFS(fstest.MapFS{}))
	t.Cleanup(server.Close)
	// The test context ends before the cleanups run, so the clock stops
	// before the server closes.
	go shared.Run(t.Context())
	game := journeyTestGame(t, 2)
	game.client = remote.New(t.Context(), server.URL)
	syncGame(t, game, func() bool { return game.state.Epoch != "" })
	return game
}

// sendCommand sends command from game and waits for the reply. It gives
// the reply to handleResult.
func sendCommand(t *testing.T, game *Game, command session.Command) {
	t.Helper()
	game.submit(command)
	if !game.pending {
		t.Fatalf("%s command not sent: %q", command.Action, game.message)
	}
	result := commandResult(t, game)
	if result.Err != nil || result.Reply.Error != "" {
		t.Fatalf("%s command failed: %+v", command.Action, result)
	}
	game.handleResult(result)
}

// TestEmergencyButtonStartsEmergency orders a journey in a session with
// the emergency marker, runs the clock until a pod carries the party, and
// pauses the session. It then clicks Emergency on that pod. The command
// has the pod ID and no order ID, the reply has the emergency ID, and the
// button hides when the state shows the emergency of the pod.
func TestEmergencyButtonStartsEmergency(t *testing.T) {
	t.Parallel()
	game := runningEmergencyGame(t)
	sendCommand(t, game, session.Command{Action: "speed", Speed: 15})
	trip := session.Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
	deadline := time.Now().Add(30 * time.Second)
	// The clock runs while the test waits, so the party can arrive before
	// the pause. Then no pod carries a party in the paused state, and the
	// test orders another journey and waits again.
	for game.selected = -1; game.selected < 0; game.selected = slices.IndexFunc(game.state.Simulation.Vehicles, carriesParty) {
		if time.Now().After(deadline) {
			t.Fatal("no pod carries the party")
		}
		sendCommand(t, game, trip)
		sendCommand(t, game, session.Command{Action: "pause", Paused: false})
		for !slices.ContainsFunc(game.state.Simulation.Vehicles, carriesParty) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			game.readRemote()
		}
		sendCommand(t, game, session.Command{Action: "pause", Paused: true})
		syncGame(t, game, func() bool { return game.state.Simulation.Paused })
	}
	game.showOrders = false
	podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
	action := emergencyActionPrefix + podID
	game.frameButtons()
	result := clickCommand(t, game, action)
	if command := result.Command; command.Action != "emergency" || command.PodID != podID || command.OrderID != 0 {
		t.Fatalf("emergency command = %+v, want pod %q without an order ID", command, podID)
	}
	emergencyID := result.Reply.EmergencyID
	if emergencyID == "" {
		t.Fatalf("emergency reply %+v has no emergency ID", result.Reply)
	}
	syncGame(t, game, func() bool {
		return slices.ContainsFunc(game.state.Simulation.Emergencies.Active, func(emergency sim.EmergencyView) bool {
			return emergency.ID == emergencyID && emergency.PodID == podID
		})
	})
	if slices.ContainsFunc(game.frameButtons(), isEmergencyButton) {
		t.Fatalf("emergency button shows for pod %q with emergency %q", podID, emergencyID)
	}
}

// TestEmergencyRefusalShowsError clicks Emergency on a pod that carries a
// party only in an old state. The view sends the command, and the message
// line shows the error of the server.
func TestEmergencyRefusalShowsError(t *testing.T) {
	t.Parallel()
	game := sharedProjectGame(t, emergencyProject())
	boardParty(&game.state.Simulation.Vehicles[game.selected])
	action := emergencyActionPrefix + game.state.Simulation.Vehicles[game.selected].Pod.ID
	game.click(centerOfButton(findButton(t, game.frameButtons(), action)))
	if !game.pending {
		t.Fatalf("click sent no command: %q", game.message)
	}
	result := commandResult(t, game)
	const want = "pod carries no passenger"
	if result.Err != nil || result.Reply.Error != want {
		t.Fatalf("result = %+v, want error %q", result, want)
	}
	game.handleResult(result)
	if got := game.hintLine(game.state.Simulation, ""); got.value != want || got.color != amber {
		t.Fatalf("hint line = %q color %#06x, want %q color %#06x", got.value, got.color, want, amber)
	}
}
