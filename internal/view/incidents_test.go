package view

import (
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// fillIncidents gives game count rows, each with a Clear button, and two
// emergencies after them.
func fillIncidents(game *Game, count int) {
	state := &game.state.Simulation
	state.Faults.Active, state.Emergencies.Active = nil, nil
	for i := range count {
		fault := sim.FaultView{ID: fmt.Sprintf("i1.%d", i), Kind: sim.FaultKindDebris, LaneID: fmt.Sprintf("lane-%d", i)}
		if i%2 == 1 {
			fault = sim.FaultView{ID: fault.ID, Kind: sim.FaultKindPod, PodID: state.Vehicles[0].Pod.ID, Phase: sim.FaultPhaseEvacuated, EndTick: 123456789}
		}
		state.Faults.Active = append(state.Faults.Active, fault)
	}
	state.Emergencies.Active = []sim.EmergencyView{{ID: "i1.900", PodID: state.Vehicles[0].Pod.ID, Phase: sim.EmergencyPhaseUnloading}, {ID: "i1.901", PodID: "unknown", Phase: sim.EmergencyPhaseBound}}
}

// actionsWithPrefix returns the actions of the controls that start with
// prefix.
func actionsWithPrefix(controls []button, prefix string) []string {
	var actions []string
	for _, control := range controls {
		if strings.HasPrefix(control.action, prefix) {
			actions = append(actions, control.action)
		}
	}
	return actions
}

// TestIncidentRows checks the content of each kind of row. Debris and a pod
// fault have the fault ID for Clear, and an emergency has none.
func TestIncidentRows(t *testing.T) {
	t.Parallel()
	state := sim.Snapshot{
		Vehicles: []sim.Vehicle{{Pod: sim.Pod{ID: "pod-a"}}, {Pod: sim.Pod{ID: "pod-b"}}},
		Faults: sim.FaultsView{Active: []sim.FaultView{
			{ID: "i1.1", Kind: sim.FaultKindDebris, LaneID: "lane-1"},
			{ID: "i1.2", Kind: sim.FaultKindPod, PodID: "pod-b", Phase: sim.FaultPhaseStopped, EndTick: 25 * sim.TicksPerSecond},
			{ID: "i1.3", Kind: sim.FaultKindPod, PodID: "gone", Phase: sim.FaultPhaseBraking},
		}},
		Emergencies: sim.EmergenciesView{Active: []sim.EmergencyView{{ID: "i1.4", PodID: "pod-a", Phase: sim.EmergencyPhaseBound}}},
	}
	want := []incidentRow{
		{text: "Debris / lane-1", faultID: "i1.1"},
		{text: "Pod 02 / stopped / ends 25.0 s", faultID: "i1.2"},
		{text: "Pod gone / braking / no end", faultID: "i1.3"},
		{text: "Emergency / Pod 01 / bound"},
	}
	got := incidentRows(state)
	if len(got) != len(want) {
		t.Fatalf("rows = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestIncidentSummary checks each number of the summary. The mean is
// EndedTicks / Ended, and not EmergencyTicks / Ended. Without a sampler, the
// service-effect numbers are absent also after a manual fault.
func TestIncidentSummary(t *testing.T) {
	t.Parallel()
	state := sim.Snapshot{
		Interrupted: 7,
		Faults: sim.FaultsView{
			Active:   []sim.FaultView{{ID: "i1.1"}},
			Counters: sim.FaultCounters{Sampled: 11, Skipped: 12, Cleared: 4, ClearedTicks: 40 * sim.TicksPerSecond, DelayedRiderTicks: 15 * sim.TicksPerSecond},
		},
		Emergencies: sim.EmergenciesView{
			Active:   []sim.EmergencyView{{ID: "i1.2"}, {ID: "i1.3"}},
			Counters: sim.EmergencyCounters{Sampled: 21, Skipped: 22, Ended: 2, EmergencyTicks: 1000 * sim.TicksPerSecond, EndedTicks: 30 * sim.TicksPerSecond, MaxTicks: 20 * sim.TicksPerSecond},
		},
	}
	want := []string{
		"Active: 1 faults / 2 emergencies",
		"Sampled: 11 faults / 21 emergencies",
		"Skipped: 12 faults / 22 emergencies",
		"Interrupted orders: 7",
		"Faults: mean 10.0 s / rider delay 15.0 s",
		"Emergencies: mean 15.0 s / longest 20.0 s",
	}
	if got := incidentSummary(state); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("summary = %q, want %q", got, want)
	}
	manual := sim.Snapshot{
		Faults:      sim.FaultsView{Counters: sim.FaultCounters{Started: 1, Cleared: 1}},
		Emergencies: sim.EmergenciesView{Counters: sim.EmergencyCounters{Started: 1, Ended: 1, EmergencyTicks: 50}},
	}
	got := incidentSummary(manual)
	if got[4] != "Faults: mean - / rider delay -" || got[5] != "Emergencies: mean - / longest -" {
		t.Errorf("summary with no sampler = %q", got)
	}
}

// TestIncidentPanelHidesWithOperations checks that the tab, the panel, and
// its buttons hide while the operations are off, that a state with the
// operations off closes an open panel, and that the panel stays closed after
// the operations come back.
func TestIncidentPanelHidesWithOperations(t *testing.T) {
	t.Parallel()
	game := faultTestGame(t)
	fillIncidents(game, 2)
	game.showIncidents = true
	if !game.incidentsOpen(game.state.Simulation) {
		t.Fatal("panel does not open")
	}
	if got := actionsWithPrefix(game.buttons(), incidentClearPrefix); len(got) != 2 {
		t.Fatalf("Clear buttons = %q, want two", got)
	}
	game.state.Simulation.OperationsOff = true
	if game.incidentsOpen(game.state.Simulation) {
		t.Error("panel open with the operations off")
	}
	controls := game.buttons()
	if got := actionsWithPrefix(controls, incidentClearPrefix); len(got) != 0 {
		t.Errorf("Clear buttons with the operations off = %q", got)
	}
	if got := actionsWithPrefix(controls, incidentsAction); len(got) != 0 {
		t.Errorf("Incidents tab with the operations off = %q", got)
	}
}

// TestDemoClosesIncidentPanel checks that a demo that another client starts
// closes an open panel, and that the panel stays closed after a reset.
func TestDemoClosesIncidentPanel(t *testing.T) {
	t.Parallel()
	shared, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	server := httptest.NewServer(shared.HandlerFS(fstest.MapFS{}))
	t.Cleanup(server.Close)
	game := journeyTestGame(t, 2)
	game.client = remote.New(t.Context(), server.URL)
	syncGame(t, game, func() bool { return game.state.Epoch != "" })
	game.press(incidentsAction)
	game.press(placeDebrisAction)
	if !game.showIncidents || !game.debrisArmed {
		t.Fatalf("panel %t armed %t, want both", game.showIncidents, game.debrisArmed)
	}
	other := func(sequence uint64, action string) {
		t.Helper()
		if reply := shared.Apply(session.Command{Client: "other", Sequence: sequence, Epoch: shared.State().Epoch, Action: action}); reply.Error != "" {
			t.Fatalf("%s: %s", action, reply.Error)
		}
	}
	other(1, "demo")
	syncGame(t, game, func() bool { return game.state.Simulation.OperationsOff })
	if game.showIncidents {
		t.Fatal("a state with the operations off leaves the panel open")
	}
	if game.debrisArmed {
		t.Fatal("a state with the operations off leaves debris armed")
	}
	other(2, "reset")
	syncGame(t, game, func() bool { return !game.state.Simulation.OperationsOff })
	if game.showIncidents || game.debrisArmed {
		t.Fatalf("after a reset: panel %t armed %t", game.showIncidents, game.debrisArmed)
	}
}

// TestIncidentTabSwitchesPanels checks that the side panels exclude one
// another, and that the inspector buttons hide while the panel is open.
func TestIncidentTabSwitchesPanels(t *testing.T) {
	t.Parallel()
	game := faultTestGame(t)
	boardParty(&game.state.Simulation.Vehicles[game.selected])
	game.showOrders = true
	game.press(incidentsAction)
	if !game.showIncidents || game.showOrders || game.showDemand {
		t.Fatalf("incidents press: incidents %t orders %t demand %t", game.showIncidents, game.showOrders, game.showDemand)
	}
	if _, shown := game.faultButton(game.state.Simulation); shown {
		t.Error("fault button shows with the panel open")
	}
	if _, shown := game.emergencyButton(game.state.Simulation); shown {
		t.Error("emergency button shows with the panel open")
	}
	for _, control := range game.buttons() {
		if control.selected && strings.HasPrefix(control.action, "pod/") {
			t.Errorf("pod button %q is selected with the panel open", control.action)
		}
	}
	game.press("demand")
	if game.showIncidents || !game.showDemand {
		t.Fatalf("demand press: incidents %t demand %t", game.showIncidents, game.showDemand)
	}
	game.press(incidentsAction)
	game.press("orders")
	if game.showIncidents || !game.showOrders {
		t.Fatalf("orders press: incidents %t orders %t", game.showIncidents, game.showOrders)
	}
	game.press(incidentsAction)
	game.selectPod(0)
	if game.showIncidents {
		t.Fatal("a pod selection leaves the panel open")
	}
	game.press(incidentsAction)
	game.press("pod/pod-a")
	if game.showIncidents {
		t.Fatal("a pod button leaves the panel open")
	}
	game.press(incidentsAction)
	game.press(incidentsAction)
	if game.showIncidents {
		t.Fatal("a second press leaves the panel open")
	}
}

// TestIncidentPanelPages checks that the rows page, that each page has the
// Clear buttons of its own fault rows, and that an emergency row has none.
func TestIncidentPanelPages(t *testing.T) {
	t.Parallel()
	game := faultTestGame(t)
	fillIncidents(game, minimumIncidentRows+2)
	game.showIncidents = true
	first := actionsWithPrefix(game.buttons(), incidentClearPrefix)
	if len(first) != minimumIncidentRows || first[0] != incidentClearPrefix+"i1.0" {
		t.Fatalf("first page Clear buttons = %q", first)
	}
	findButton(t, game.buttons(), incidentsPagerPrefix+"next")
	game.press(incidentsPagerPrefix + "next")
	second := actionsWithPrefix(game.buttons(), incidentClearPrefix)
	if len(second) != 2 || second[0] != fmt.Sprintf("%si1.%d", incidentClearPrefix, minimumIncidentRows) {
		t.Fatalf("second page Clear buttons = %q, want two fault rows", second)
	}
	game.press(incidentsPagerPrefix + "next")
	if game.incidentPage != 1 {
		t.Fatalf("page = %d past the last page", game.incidentPage)
	}
	game.press(incidentsPagerPrefix + "prev")
	if game.incidentPage != 0 {
		t.Fatalf("page = %d after prev", game.incidentPage)
	}
	fillIncidents(game, 1)
	if got := actionsWithPrefix(game.buttons(), incidentsPagerPrefix); len(got) != 0 {
		t.Errorf("page arrows for one page = %q", got)
	}
}

// TestIncidentPanelFits checks in each layout that the panel stays inside
// the right panel and above the pod selector, that the text of a row ends
// before its Clear button, that a row in a tall window keeps its button,
// and that each tab label fits.
func TestIncidentPanelFits(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			fillIncidents(game, 200)
			game.showIncidents = true
			game.state.Simulation.Faults.Counters = sim.FaultCounters{Sampled: sim.MaxCounter, Skipped: sim.MaxCounter, ClearedTicks: sim.MaxCounter, Cleared: 1, DelayedRiderTicks: sim.MaxCounter}
			controls := game.buttons()
			panel := game.rightPanelArea()
			selectorTop := findButton(t, controls, "pod/"+fleetPodLabel(0, len(game.state.Simulation.Vehicles))).y
			clears := map[float64]button{}
			for _, control := range controls {
				if !isIncidentClear(control) {
					continue
				}
				clears[control.y] = control
				if got := buttonArea(control); got.left < panel.left || got.right > panel.right || got.bottom > selectorTop {
					t.Errorf("Clear %+v escapes panel %+v above %g", got, panel, selectorTop)
				}
			}
			if len(clears) < minimumIncidentRows {
				t.Errorf("%d rows with Clear buttons", len(clears))
			}
			for _, value := range game.incidentLabels(game.state.Simulation) {
				got := game.labelArea(value)
				if got.left < panel.left || got.right > panel.right || got.top < panel.top || got.bottom >= selectorTop {
					t.Errorf("label %q %+v escapes panel %+v above %g", value.value, got, panel, selectorTop)
				}
				for _, control := range clears {
					if got.overlaps(buttonArea(control)) {
						t.Errorf("label %q %+v overlaps Clear %+v", value.value, got, buttonArea(control))
					}
				}
			}
			game.state.Simulation.Pending = make([]sim.Request, 9999)
			controls = game.buttons()
			for _, action := range []string{"orders", "demand", incidentsAction} {
				control := findButton(t, controls, action)
				if got := game.fitButtonText(control.label, control.fontSize, control.w); got != control.label {
					t.Errorf("tab %q shortened to %q", control.label, got)
				}
			}
			row := findButton(t, controls, incidentClearPrefix+"i1.0")
			width, _ := text.Measure("Clear", game.textFace(row.fontSize), 0)
			if width > row.w {
				t.Errorf("Clear label width %g in button width %g", width, row.w)
			}
		})
	}
}

// clickResult waits for the reply to the command of a click.
func clickResult(t *testing.T, game *Game) remote.Result {
	t.Helper()
	if !game.pending {
		t.Fatalf("click sent no command: %q", game.message)
	}
	select {
	case result := <-game.client.Results():
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("command timed out")
		return remote.Result{}
	}
}

// TestIncidentClearSendsFault clicks Clear on a row of the panel in a game
// that is connected to a session.
func TestIncidentClearSendsFault(t *testing.T) {
	t.Parallel()
	game := sharedProjectGame(t, faultProject())
	podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
	faultID := clickFault(t, game, faultActionPrefix+podID).Reply.FaultID
	syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
	game.press(incidentsAction)
	result := clickFault(t, game, incidentClearPrefix+faultID)
	if command := result.Command; command.Action != "clearFault" || command.FaultID != faultID {
		t.Fatalf("clear command = %+v, want fault %q", command, faultID)
	}
	syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 0 })
}

// TestIncidentClearUsesLastFrame changes the rows after a frame and before a
// click. The click sends the fault ID of the row that the frame showed. The
// server replies that the fault is unknown, and the message line shows it.
func TestIncidentClearUsesLastFrame(t *testing.T) {
	t.Parallel()
	game := sharedProjectGame(t, faultProject())
	game.showIncidents = true
	game.state.Simulation.Faults.Active = []sim.FaultView{{ID: "i9.1", Kind: sim.FaultKindDebris, LaneID: "gone"}}
	point := centerOfButton(findButton(t, game.frameButtons(), incidentClearPrefix+"i9.1"))
	game.state.Simulation.Faults.Active = []sim.FaultView{{ID: "i9.2", Kind: sim.FaultKindDebris, LaneID: "new"}}
	game.click(point)
	result := clickResult(t, game)
	if result.Command.Action != "clearFault" || result.Command.FaultID != "i9.1" {
		t.Fatalf("command = %+v, want the fault of the last frame", result.Command)
	}
	game.handleResult(result)
	if game.message != "unknown fault" {
		t.Fatalf("message = %q, want the error of the server", game.message)
	}
	// A frame with no panel removes the Clear buttons of a click.
	game.showIncidents = false
	game.frameButtons()
	game.pending = false
	game.showIncidents = true
	if got := actionsWithPrefix(game.clickButtons(), incidentClearPrefix); len(got) != 0 {
		t.Fatalf("click buttons = %q, want none from a frame with no panel", got)
	}
}

// TestStaleClearAfterReconnect reconnects to a new server process that has
// a fault with the same ID, and clicks a Clear button of the old session
// before another frame. The click must not clear the fault of the new
// session, also when the game has not read the new state. A new frame then
// sends the command.
func TestStaleClearAfterReconnect(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		panel  bool
		prefix string
		read   bool
	}{
		{name: "panel, state read", panel: true, prefix: incidentClearPrefix, read: true},
		{name: "panel, state not read", panel: true, prefix: incidentClearPrefix},
		{name: "inspector, state read", prefix: clearFaultActionPrefix, read: true},
		{name: "inspector, state not read", prefix: clearFaultActionPrefix},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			env := newChangeEnv(t)
			game := env.game
			podID := game.state.Simulation.Vehicles[game.selected].Pod.ID
			faultID := clickFault(t, game, faultActionPrefix+podID).Reply.FaultID
			syncGame(t, game, func() bool { return len(game.state.Simulation.Faults.Active) == 1 })
			if test.panel {
				game.press(incidentsAction)
			}
			point := centerOfButton(findButton(t, game.frameButtons(), test.prefix+faultID))
			before := game.state.Epoch

			replacement, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			reply := replacement.Apply(session.Command{Action: "fault", PodID: podID, Client: "other", Sequence: 1, Epoch: replacement.State().Epoch})
			if reply.FaultID != faultID {
				t.Fatalf("new session fault = %+v, want the ID %q again", reply, faultID)
			}
			env.server.use(replacement)
			waitFor(t, func() bool {
				state, connected, _ := game.client.View()
				return connected && state.Epoch != before
			})
			if test.read {
				game.readRemote()
			}
			active := func() int { return len(replacement.State().Simulation.Faults.Active) }

			game.click(point)
			if game.pending || game.message != remote.ErrSessionChanged.Error() {
				t.Fatalf("pending %t message %q, want a refusal", game.pending, game.message)
			}
			if got := active(); got != 1 {
				t.Fatalf("the new session has %d faults, want 1", got)
			}

			syncGame(t, game, func() bool { return game.state.Epoch != before })
			point = centerOfButton(findButton(t, game.frameButtons(), test.prefix+faultID))
			game.click(point)
			if result := clickResult(t, game); result.Reply.Error != "" || result.Command.FaultID != faultID {
				t.Fatalf("clear after a new frame = %+v", result)
			}
			if got := active(); got != 0 {
				t.Fatalf("the new session has %d faults after the clear, want 0", got)
			}
		})
	}
}

// TestStaleInspectorCommandAfterReconnect presses the fault button and the
// emergency button of the last frame after a reconnect. A pod in the new
// session can have the same ID, but it is not the pod that the user saw.
func TestStaleInspectorCommandAfterReconnect(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{faultActionPrefix, emergencyActionPrefix} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			env := newChangeEnv(t)
			game := env.game
			game.frameButtons()
			before := game.state.Epoch
			replacement, err := session.NewWithProject(project.Default(), session.WithLogger(slog.New(slog.DiscardHandler)))
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			env.server.use(replacement)
			syncGame(t, game, func() bool { return game.state.Epoch != before })
			game.press(prefix + game.state.Simulation.Vehicles[game.selected].Pod.ID)
			if game.pending || game.message != remote.ErrSessionChanged.Error() {
				t.Fatalf("pending %t message %q, want a refusal", game.pending, game.message)
			}
		})
	}
}

// TestIncidentClearDisabled checks that a stored Clear button takes the
// connection and a waiting command from the current state.
func TestIncidentClearDisabled(t *testing.T) {
	t.Parallel()
	game := faultTestGame(t)
	fillIncidents(game, 1)
	game.showIncidents = true
	game.frameButtons()
	game.pending = true
	if control := findButton(t, game.clickButtons(), incidentClearPrefix+"i1.0"); !control.disabled {
		t.Error("Clear enabled while a command waits")
	}
	game.pending, game.connected = false, false
	if control := findButton(t, game.clickButtons(), incidentClearPrefix+"i1.0"); !control.disabled {
		t.Error("Clear enabled with the connection lost")
	}
	game.connected = true
	if control := findButton(t, game.clickButtons(), incidentClearPrefix+"i1.0"); control.disabled {
		t.Error("Clear disabled when the game can send")
	}
}

// TestHandleResultClosesIncidentPanel checks the commands that close the
// panel: an accepted order shows Orders, and a reset or a demo clears the
// panels.
func TestHandleResultClosesIncidentPanel(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"trip", "reset", "demo"} {
		game := faultTestGame(t)
		game.showIncidents = true
		game.handleResult(remote.Result{Command: session.Command{Action: action}})
		if game.showIncidents {
			t.Errorf("%s leaves the panel open", action)
		}
	}
}
