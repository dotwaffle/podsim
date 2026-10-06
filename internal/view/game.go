// Package view draws the simulation and translates input into commands.
package view

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image"
	"math"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	background    = 0x0b121a
	panel         = 0x121f2b
	foreground    = 0xe5edf5
	muted         = 0x91a6ba
	accent        = 0x6de5c1
	track         = 0x354b5e
	amber         = 0xf3c479
	detailedLanes = 100
	// markerOutlineWidth is the width in display units of the outline of a
	// collapsed station marker.
	markerOutlineWidth = 2
	// routeWidth is the width in display units of the route of the selected
	// pod. It is wider than the line lanes of a dense map at Fit
	// (denseLaneWidth.minimum).
	routeWidth = 3
	// berthRingRadius is the radius in display units of the ring around a
	// berth. The ring line is 2 units wide.
	berthRingRadius = 13
	// podLabelLeft and podLabelTop place the pod label in display units from
	// the center of the pod. The label is up and to the right of the pod.
	podLabelLeft = 12
	podLabelTop  = -16.5

	inspectionLeft       = 816.0
	inspectionRight      = 1054.0
	inspectionValueLeft  = 916.0
	inspectionRowsTop    = 226.0
	inspectionRowSpacing = 27.0
	podSelectorTop       = 361.0
	// podsPerPage is the number of pod buttons on one page of the pod
	// selector.
	podsPerPage = 5
	// podButtonStep is the distance in design units from one pod button to
	// the next.
	podButtonStep = 35.0
	// podPagerLeft and podPagerArrowWidth set the page arrows of the pod
	// selector in design units. The page count is between the arrows.
	podPagerLeft       = 985.0
	podPagerArrowWidth = 20.0
)

// Game owns presentation state and submits commands to the simulation.
type Game struct {
	network              sim.Network
	client               *remote.Client
	motion               remote.Motion
	state                session.State
	connected, pending   bool
	showDemand           bool
	font                 *text.GoTextFaceSource
	origin, destination  string
	orderPartySize       int
	orderSharingConsent  sim.SharingConsent
	journeySearch        journeySearch
	stationPagesCache    *stationPageCache
	labelMeasures        *labelMeasureCache
	browserJourney       *browserJourney
	message              string
	selected             int
	followSelected       bool
	stationPage, podPage int
	mapScale             float64
	mapOrigin            sim.Point
	camera               mapCamera
	mapBackground        mapPublisher
	touch                touchGestures
	cameraKey            cameraFitKey
	// networkBase is the cached base layer of the map. See
	// drawCachedNetworkBase.
	networkBase networkBaseCache
	index       *networkIndex
	indexKey    networkIndexKey
	showOrders  bool
	// orderPage is the zero-based page of the Orders panel. A page past
	// the last page shows the last page.
	orderPage int
	// notice is the notice in the hint line.
	notice noticeState
	// commands tracks the commands that the game sent and their replies.
	commands commandTracking
	// shownFault is the fault button of the last drawn frame, laid out,
	// and faultShown reports whether that frame had one. A click uses
	// this button, so it sends the command that the user saw. See
	// clickButtons.
	shownFault button
	faultShown bool
	layout     displayLayout
	// imageLimit is the largest side in pixels of an image. Draw reads it
	// from Ebiten in each frame. See imageSideLimit.
	imageLimit int
	// shell is the shell page that holds the game. It is nil in the
	// desktop client and outside the shell page. See Shell. shellReadySent
	// is true after the game sent ShellReady. shellFailure is the text of
	// the last capture failure that readShell put in the message line. When
	// the message is still this text, a capture result clears it.
	shell          Shell
	shellReadySent bool
	shellFailure   string
	// hidden is true while the shell page hides the game. Then Draw does
	// not draw. drawnFrames counts the frames that Draw drew.
	hidden      bool
	drawnFrames int
	// reload loads the page again. It is nil in the desktop client.
	// serverUpdated becomes true when the game sees a new server build, so
	// the game reloads the page only once.
	reload        func()
	serverUpdated bool
	// lastFrame is the time of the last state frame that the client read.
	// While the connection is lost, the map banner gives its age.
	lastFrame time.Time
	// sprites keeps the antialiased pod, ring, marker and route arrow
	// shapes of the map.
	sprites spriteCache
}

// noticeState is the notice in the hint line. A notice can also be a
// confirmation. See confirmSubmit.
type noticeState struct {
	// text is the text of the notice. action names the command or the
	// prompt that caused the notice. ticks is the time in game ticks until
	// the notice ends. See showNotice and tickNotice.
	text   string
	action string
	ticks  int
	// confirmDeadline is the wall-clock time at which the confirmation
	// that shows ends. See confirmSubmit.
	confirmDeadline time.Time
}

// commandTracking holds the state of the commands that the game sent and
// of their accepted replies.
type commandTracking struct {
	// acceptedOrigin and acceptedDestination are the stations of the last
	// accepted order. The request button reads Order accepted only while
	// From and To are these stations.
	acceptedOrigin, acceptedDestination string
	// savedEpoch and savedRevision come from the last accepted save point.
	// The command reply can arrive before the state that lists the save point,
	// so Rewind waits for that state and does not target an older save point.
	// savedStart is the sentStart of that command. The revision applies
	// only to states with this server start ID.
	savedEpoch    string
	savedRevision uint64
	savedStart    string
	// sentAction is the action of the last command that the client
	// accepted. While Game.pending is true, this command waits for its reply.
	sentAction string
	// sentStart is the server start ID of the state when the game sent
	// the last command. A reply has no start ID. It comes from this server
	// process or from a later one, but never from an earlier one.
	sentStart string
	// ownEpoch and ownGeneration come from the last accepted reply to a
	// command of this game that starts a new generation. A change in the
	// same epoch and with the server start ID ownStart to this generation
	// or to an earlier one shows no session change notice. ownStart is the
	// sentStart of that command.
	ownEpoch      string
	ownGeneration uint64
	ownStart      string
}

// Option configures a Game.
type Option func(*Game)

// WithReload sets the func that loads the page again when the server build
// changes. With a nil func, the game shows a message that tells the user to
// restart the desktop client.
func WithReload(reload func()) Option {
	return func(g *Game) { g.reload = reload }
}

// New creates a game for the shared session of the server at serverURL.
// The game has no network and no pods until the first state frame arrives.
// Until then, the map shows connectingMessage.
func New(ctx context.Context, serverURL string, options ...Option) (*Game, error) {
	font, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		return nil, fmt.Errorf("load font: %w", err)
	}
	game := &Game{client: remote.New(ctx, serverURL), state: session.State{Speed: 1}, font: font, layout: newDisplayLayout(layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1})}
	for _, option := range options {
		option(game)
	}
	return game, nil
}

// Update reads shared state and handles local input.
func (g *Game) Update() error {
	g.readRemote()
	g.readShell()
	g.fitNetwork()
	g.tickNotice()
	if !g.updateJourneyInput() {
		if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
			step := 1
			if ebiten.IsKeyPressed(ebiten.KeyShift) {
				step = -1
			}
			g.selectAdjacentPod(step)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
			g.pause()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyR) && ebiten.IsKeyPressed(ebiten.KeyShift) {
			g.reset(time.Now())
			return nil
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			g.request()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyS) {
			g.cycleSpeed()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyF) {
			g.toggleFollow()
		}
	}
	if g.followSelected {
		g.followSelectedPod()
	}
	buffer, canvas := readDrawingBuffer()
	pointer := newPointerTransform(pointerTransformInput{buffer: buffer, canvas: canvas, screen: image.Pt(g.layout.width, g.layout.height)})
	if g.updateMapInput(pointer) {
		return nil
	}
	g.updateTouchInput(pointer)
	return nil
}

// selectAdjacentPod selects the pod step places after the selected pod for
// inspection. Tab gives step 1 and Shift+Tab gives step -1. After the last
// pod, it selects the first pod, and before the first pod, the last pod.
// Before the first state frame, the game has no pods, and it does nothing.
func (g *Game) selectAdjacentPod(step int) {
	count := len(g.state.Simulation.Vehicles)
	if count == 0 {
		return
	}
	g.selectPod(((g.selected+step)%count + count) % count)
}

// selectPod selects the pod at index for inspection. The pod selector then
// shows the page with the button of the pod. A new selection closes Orders
// and Demand and clears the message.
func (g *Game) selectPod(index int) {
	g.selected, g.podPage = index, index/podsPerPage
	g.showOrders, g.showDemand = false, false
	g.message = ""
}

// podPageCount returns the number of pages of the pod selector for count
// pods. With no pods, there is one empty page.
func podPageCount(count int) int {
	return max(1, (count+podsPerPage-1)/podsPerPage)
}

// turnPodPage moves the pod selector by step pages. It stops at the first
// and the last page.
func (g *Game) turnPodPage(step int) {
	g.podPage = min(max(0, g.podPage+step), podPageCount(len(g.state.Simulation.Vehicles))-1)
}

// tickNotice counts down the notice time by one tick. It clears the notice
// when the time ends.
func (g *Game) tickNotice() {
	if g.notice.ticks == 0 {
		return
	}
	g.notice.ticks--
	if g.notice.ticks == 0 {
		g.notice.text, g.notice.action = "", ""
	}
}

// updateMapInput applies the mouse input of this frame. pointer corrects
// the cursor position. See pointerTransform. It returns true when a click
// presses Reset, Start traffic demo, or Rewind. See click.
func (g *Game) updateMapInput(pointer pointerTransform) bool {
	x, y := ebiten.CursorPosition()
	point := pointer.apply(sim.Point{X: float64(x), Y: float64(y)})
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
		g.rightClick(point)
		return false
	}
	if _, wheelY := ebiten.Wheel(); wheelY != 0 && g.camera.contains(point) {
		factor := wheelZoomFactor(wheelY, runtime.GOOS == "js")
		if g.camera.zoomAt(point, factor) {
			g.syncCamera()
		}
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		if g.camera.contains(point) {
			g.journeySearch.focus = 0
			g.followSelected = false
			g.camera.beginDrag(point)
			return false
		}
		return g.click(point)
	}
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && g.camera.dragging {
		if g.camera.drag(point) {
			g.syncCamera()
		}
	}
	if inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) && g.camera.dragging {
		if g.camera.endDrag() {
			g.pickOnMap(point, ebiten.IsKeyPressed(ebiten.KeyShift) || pointerShift())
		}
	}
	return false
}

func (g *Game) pause() {
	g.submit(session.Command{Action: "pause", Paused: !g.state.Simulation.Paused})
}

// resetConfirmAction is the notice action of the reset confirmation.
const resetConfirmAction = "reset-confirm"

// resetConfirmNotice asks for a second Reset press. A reset affects every
// browser, so one press does not reset the session.
const resetConfirmNotice = "Select Reset again within 3 s to reset the shared session."

// resetNotice tells the user that the server accepted the reset.
const resetNotice = "Session reset."

// demoConfirmAction is the notice action of the demo confirmation.
const demoConfirmAction = "demo-confirm"

// demoConfirmNotice asks for a second press of Start traffic demo. The demo
// resets the session for every browser, so one press does not start it.
const demoConfirmNotice = "Select Start traffic demo again within 3 s to reset the shared session and start the demo."

// confirmation is a notice that asks for a second press of a control. It
// guards a command that resets the shared session.
type confirmation struct {
	// action is the notice action of the confirmation, and notice is its
	// text.
	action, notice string
}

// isConfirmation reports whether action is the notice action of a
// confirmation.
func isConfirmation(action string) bool {
	return action == resetConfirmAction || action == demoConfirmAction
}

// confirmWindow is the wall-clock time in which a second press sends the
// command of a confirmation. It is the time of noticeDuration.
const confirmWindow = 3 * time.Second

// confirmSubmit sends command only while prompt shows and before the
// confirmation window ends. now is the time of the press. The first press
// shows prompt, starts the window, and sends nothing. prompt shows for
// noticeDuration ticks, or until a different notice replaces it. The
// confirmation of a different control also replaces it, so each command
// needs two presses of its own control. The first press also clears an old
// message, so that the message does not show again when prompt ends. The
// second press removes prompt before it sends. If the send fails, the error
// shows and the next press asks again.
//
// The window uses wall-clock time, because the browser stops Update in a
// hidden tab and Ebitengine runs only some of the missed ticks later. The
// notice ticks then stop, but the window ends after confirmWindow. A press
// after the window is a new first press.
func (g *Game) confirmSubmit(prompt confirmation, command session.Command, now time.Time) {
	if g.notice.action == prompt.action && g.notice.ticks > 0 && now.Before(g.notice.confirmDeadline) {
		g.notice.text, g.notice.action, g.notice.ticks = "", "", 0
		g.notice.confirmDeadline = time.Time{}
		g.submit(command)
		return
	}
	g.message = ""
	g.showNotice(prompt.action, prompt.notice)
	g.notice.confirmDeadline = now.Add(confirmWindow)
}

// reset sends the reset command on the second press of Reset or Shift+R.
// now is the time of the press. See confirmSubmit.
func (g *Game) reset(now time.Time) {
	g.confirmSubmit(confirmation{action: resetConfirmAction, notice: resetConfirmNotice}, session.Command{Action: "reset"}, now)
}

// startDemo sends the demo command on the second press of Start traffic
// demo. now is the time of the press. See confirmSubmit. The server accepts
// the demo only for the supplied example scenario. The game cannot know the
// scenario, so a rejection shows in the message line, as for other
// commands.
func (g *Game) startDemo(now time.Time) {
	g.confirmSubmit(confirmation{action: demoConfirmAction, notice: demoConfirmNotice}, session.Command{Action: "demo"}, now)
}

func (g *Game) cycleSpeed() {
	g.submit(session.Command{Action: "speed", Speed: session.NextSpeed(g.state.Speed)})
}
func (g *Game) request() {
	if g.state.Simulation.Demo || g.journeySearch.unresolved[0] || g.journeySearch.unresolved[1] {
		return
	}
	g.submit(g.orderCommand())
}

// checkpoint asks the server to save the shared session.
func (g *Game) checkpoint() { g.submit(session.Command{Action: "checkpoint"}) }

// rewind goes back to the newest save point. It sends an explicit ID, because
// the server has no default save point.
func (g *Game) rewind() {
	target, ok := rewindTarget(g.state)
	if !ok || !g.rewindReady() {
		return
	}
	g.submit(session.Command{Action: "rewind", Checkpoint: target.ID})
}

// rewindReady reports whether the state includes the last accepted save
// point. A state from a new epoch or with a different server start ID does
// not wait, because a restart clears the save points. A restart can also
// keep the epoch and lower the revision. The revision of a save point from
// an earlier server process thus does not apply.
func (g *Game) rewindReady() bool {
	return g.state.Epoch != g.commands.savedEpoch || g.state.ServerStart != g.commands.savedStart || g.state.Revision >= g.commands.savedRevision
}

// rewindTarget returns the save point with the highest ID. It does not
// depend on the order of the list.
func rewindTarget(state session.State) (session.Checkpoint, bool) {
	if len(state.Checkpoints) == 0 {
		return session.Checkpoint{}, false
	}
	return slices.MaxFunc(state.Checkpoints, func(a, b session.Checkpoint) int { return cmp.Compare(a.ID, b.ID) }), true
}

// rewindLabel names the rewind target by its simulated time. When the target
// restores a project, the label says so in place of the time. The button is
// too narrow for both.
func rewindLabel(state session.State) string {
	target, ok := rewindTarget(state)
	switch {
	case !ok:
		return "Rewind"
	case target.RestoresProject:
		return "Rewind + project"
	default:
		return fmt.Sprintf("Rewind %.1f s", float64(target.Tick)/sim.TicksPerSecond)
	}
}

type button struct {
	x, y, w, h         float64
	label              string
	selected, disabled bool
	action             string
	expandsWithMap     bool
	fontSize           float64
}

func (g *Game) buttons() []button {
	g.ensureLayout()
	state := g.state.Simulation
	orders := outstandingOrderCount(state)
	busy := state.Demo || !g.connected || g.pending
	// The request button reads Order accepted for as long as the notice of
	// the accepted order shows. The user can change From and To during and
	// after the order, so the label shows only while From and To are the
	// stations of that order. Enter also sends the order, so the label
	// names the key.
	requestLabel := "Order [Enter]"
	if g.notice.action == "trip" && g.origin == g.commands.acceptedOrigin && g.destination == g.commands.acceptedDestination {
		requestLabel = "Order accepted"
	}
	pauseLabel := "Pause [Space]"
	if state.Paused {
		pauseLabel = "Resume [Space]"
	}
	followLabel := "Follow [F]"
	if g.followSelected {
		followLabel = "Following [F]"
	}
	_, canRewind := rewindTarget(g.state)
	canRewind = canRewind && g.rewindReady()
	buttons := []button{
		{x: 810, y: headerButtonTop, w: 90, h: headerButtonHeight, label: "Save point", action: "checkpoint"},
		{x: 912, y: headerButtonTop, w: 148, h: headerButtonHeight, label: rewindLabel(g.state), action: "rewind"},
		{x: 617, y: 72, w: 28, h: 24, label: "−", action: "map-zoom-out"},
		{x: 651, y: 72, w: 28, h: 24, label: "+", action: "map-zoom-in"},
		{x: 685, y: 72, w: 66, h: 24, label: "Fit", action: "map-fit"},
		{x: 964, y: 72, w: 96, h: 24, label: followLabel, selected: g.followSelected, action: "map-follow", fontSize: 11},
		{x: 810, y: 465, w: 120, h: 26, label: fmt.Sprintf("Orders %d", orders), selected: g.showOrders, action: "orders"},
		{x: 940, y: 465, w: 120, h: 26, label: "Demand", selected: g.showDemand, action: "demand"},
		{x: 930, y: 612, w: 130, h: 42, label: requestLabel, selected: true, disabled: busy || g.destination == g.origin || g.journeySearch.unresolved[0] || g.journeySearch.unresolved[1], action: "request"},
		{x: 810, y: 397, w: 250, h: 32, label: pauseLabel, action: "pause"},
		{x: 810, y: 431, w: 119, h: 32, label: fmt.Sprintf("Speed %dx [S]", g.state.Speed), action: "speed"},
		{x: 941, y: 431, w: 119, h: 32, label: "Reset [Shift+R]", action: "reset"},
	}
	buttons = append(buttons, g.shellButtons()...)
	for i, v := range state.Vehicles {
		if i/podsPerPage != g.podPage {
			continue
		}
		buttons = append(buttons, button{x: 810 + float64(i%podsPerPage)*podButtonStep, y: podSelectorTop, w: 32, h: 34, label: fleetPodLabel(i), selected: !g.showOrders && !g.showDemand && g.selected == i, action: "pod/" + v.Pod.ID})
	}
	if control, ok := g.faultButton(state); ok {
		buttons = append(buttons, control)
	}
	if pages := podPageCount(len(state.Vehicles)); pages > 1 {
		buttons = append(buttons,
			button{x: podPagerLeft, y: podSelectorTop, w: podPagerArrowWidth, h: 34, label: "‹", disabled: g.podPage == 0, action: "pods-prev"},
			button{x: 1060 - podPagerArrowWidth, y: podSelectorTop, w: podPagerArrowWidth, h: 34, label: "›", disabled: g.podPage >= pages-1, action: "pods-next"},
		)
	}
	// A station chip changes only the local selection and sends no command,
	// so the chips stay enabled while a command waits for the server.
	buttons = append(buttons, g.journeyButtons(state.Demo || !g.connected)...)
	buttons = append(buttons, g.orderChoiceButtons(state.Demo || !g.connected)...)
	if g.showDemand {
		buttons = append(buttons, g.demandButtons()...)
	}
	if g.showOrders {
		buttons = append(buttons, g.orderPagerButtons(state)...)
	}
	for i := range buttons {
		switch buttons[i].action {
		case "pause", "speed", "reset", "checkpoint":
			buttons[i].disabled = !g.connected || g.pending
		case "rewind":
			buttons[i].disabled = !g.connected || g.pending || !canRewind
		}
		buttons[i] = g.layoutButton(buttons[i])
	}
	return buttons
}

func (g *Game) layoutButton(b button) button {
	x, y := b.x*g.layout.unit, b.y*g.layout.unit
	if b.x >= 796 && !b.expandsWithMap || strings.HasPrefix(b.action, "map-") || strings.HasPrefix(b.action, "shell-") || b.action == "request" {
		x += g.layout.extraX
	}
	// The Orders page controls and Start traffic demo are above the pod
	// selector, but they move down with it.
	if movesDown(b.x, b.y) || strings.HasPrefix(b.action, "orders-") || b.action == "demo" {
		y += g.layout.extraY
	}
	b.x, b.y, b.w, b.h = x, y, b.w*g.layout.unit, b.h*g.layout.unit
	return b
}

// click reports a press of Reset, Start traffic demo, or Rewind, so that the
// new state can render before the next tick. The first enabled button at
// point takes the click. Without a button there, a click in the map picks
// on the map.
func (g *Game) click(point sim.Point) bool {
	for _, b := range g.clickButtons() {
		if b.disabled || point.X < b.x || point.X >= b.x+b.w || point.Y < b.y || point.Y >= b.y+b.h {
			continue
		}
		g.press(b.action)
		return b.action == "demo" || b.action == "reset" || b.action == "rewind"
	}
	if g.camera.contains(point) {
		g.pickOnMap(point, false)
	}
	return false
}

// press applies a press of the button with action. Each region of the
// screen has a handler, and no two handlers accept the same action. A press
// outside the station search boxes removes the search focus. An action
// that no handler accepts clears the message.
func (g *Game) press(action string) {
	if action != "search-from" && action != "search-to" {
		g.journeySearch.focus = 0
	}
	if g.pressHeader(action) || g.pressMapControl(action) || g.pressPodControl(action) || g.pressSidePanel(action) || g.pressJourney(action) {
		return
	}
	g.message = ""
}

// pressHeader applies a press of a header button: Save point, Rewind, and
// the shell buttons. It returns false for any other action.
func (g *Game) pressHeader(action string) bool {
	switch action {
	case "checkpoint":
		g.checkpoint()
	case "rewind":
		g.rewind()
	case shellEditorAction:
		g.shell.Send(ShowEditor)
	case shellDebugAction:
		g.shell.Send(CaptureDebugState)
		g.message = ""
	default:
		return false
	}
	return true
}

// pressMapControl applies a press of a map button: the zoom buttons, Fit,
// and Follow. It returns false for any other action.
func (g *Game) pressMapControl(action string) bool {
	switch action {
	case "map-zoom-out":
		g.zoomMap(1 / mapZoomStep)
	case "map-zoom-in":
		g.zoomMap(mapZoomStep)
	case "map-fit":
		g.followSelected = false
		g.camera.fit(cameraFit{bounds: g.camera.world, viewport: g.layout.mapViewport, unit: g.layout.unit})
		g.syncCamera()
	case "map-follow":
		g.toggleFollow()
	default:
		return false
	}
	return true
}

// pressPodControl applies a press of a pod button of the inspector: the
// fault button, a page arrow of the pod selector, or a pod button. A pod
// button selects its pod and clears the message. It returns false for any
// other action.
func (g *Game) pressPodControl(action string) bool {
	switch action {
	case "pods-prev":
		g.turnPodPage(-1)
		return true
	case "pods-next":
		g.turnPodPage(1)
		return true
	}
	if command, ok := faultCommand(action); ok {
		g.submit(command)
		return true
	}
	id, ok := strings.CutPrefix(action, "pod/")
	if !ok {
		return false
	}
	for i, v := range g.state.Simulation.Vehicles {
		if v.Pod.ID == id {
			g.selected = i
			break
		}
	}
	g.showOrders, g.showDemand = false, false
	g.message = ""
	return true
}

// pressSidePanel applies a press of a button of the side panel: the Orders
// and Demand tabs, the Orders page arrows, the Demand controls, Start
// traffic demo, Pause, Speed, and Reset. It returns false for any other
// action.
func (g *Game) pressSidePanel(action string) bool {
	switch action {
	case "orders-prev":
		g.turnOrderPage(-1)
	case "orders-next":
		g.turnOrderPage(1)
	case "orders":
		g.showOrders = !g.showOrders
		g.showDemand = false
	case "demand":
		g.showDemand = !g.showDemand
		g.showOrders = false
	case "demand-rate", "demand-pattern", "demand-seed", "demand-toggle":
		g.changeDemand(action)
	case "demo":
		g.startDemo(time.Now())
	case "pause":
		g.pause()
	case "speed":
		g.cycleSpeed()
	case "reset":
		g.reset(time.Now())
	default:
		return false
	}
	return true
}

// pressJourney applies a press of a button of the order form: the party
// size, sharing, the station search boxes, the station pages, a From or To
// station chip, and the request button. A station chip clears the message.
// It returns false for any other action.
func (g *Game) pressJourney(action string) bool {
	switch action {
	case "party-less":
		g.orderPartySize = max(1, g.selectedPartySize()-1)
	case "party-more":
		g.orderPartySize = min(g.orderPartyLimit(), g.selectedPartySize()+1)
	case "order-sharing":
		if g.orderSharingConsent == sim.SharedConsent {
			g.orderSharingConsent = sim.PrivateConsent
		} else {
			g.orderSharingConsent = sim.SharedConsent
		}
	case "search-from":
		g.startJourneySearch(1)
	case "search-to":
		g.startJourneySearch(2)
	case "stations-prev":
		g.stationPage--
	case "stations-next":
		g.stationPage++
	case "request":
		g.request()
	default:
		return g.pressStationChip(action)
	}
	return true
}

// pressStationChip applies a press of a From or To station chip. It sets
// the station and clears the message. It returns false for any other
// action.
func (g *Game) pressStationChip(action string) bool {
	if origin, ok := strings.CutPrefix(action, "from/"); ok {
		g.chooseJourneyStation(1, origin)
	} else if destination, ok := strings.CutPrefix(action, "to/"); ok {
		g.chooseJourneyStation(2, destination)
	} else {
		return false
	}
	g.message = ""
	return true
}

// Layout is the integer fallback for platforms that do not use LayoutF.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return g.layoutFor(layoutInput{outsideWidth: outsideWidth, outsideHeight: outsideHeight, deviceScale: 1, imageLimit: imageSideLimit(ebiten.MaxImageSize())})
}

// LayoutF renders at the monitor's native pixel density while retaining
// CSS-sized controls. When the screen image at that density would be larger
// than the image limit, it renders at a lower density. See screenScale. When
// the browser makes the drawing buffer smaller than the canvas, the screen
// image gets the size of the drawing buffer. See screenSize.
func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	scale := 1.0
	if monitor := ebiten.Monitor(); monitor != nil {
		scale = monitor.DeviceScaleFactor()
	}
	buffer, canvas := readDrawingBuffer()
	w, h := g.layoutFor(layoutInput{
		outsideWidth: int(math.Round(outsideWidth)), outsideHeight: int(math.Round(outsideHeight)), deviceScale: scale,
		imageLimit: imageSideLimit(ebiten.MaxImageSize()), buffer: buffer, canvas: canvas,
	})
	return float64(w), float64(h)
}

func (g *Game) layoutFor(input layoutInput) (int, int) {
	next := newDisplayLayout(input)
	if g.layout != next {
		g.layout = next
		g.camera.initialized = false
		g.networkBase.valid = false
	}
	return next.width, next.height
}

func (g *Game) ensureLayout() {
	if g.layout.width == 0 {
		g.layout = newDisplayLayout(layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1})
	}
}

func (g *Game) mapPoint(p sim.Point) sim.Point {
	return g.camera.screenPoint(p)
}

func (g *Game) zoomMap(factor float64) {
	viewport := g.layout.mapViewport
	center := sim.Point{X: float64(viewport.Min.X+viewport.Max.X) / 2, Y: float64(viewport.Min.Y+viewport.Max.Y) / 2}
	if g.camera.zoomAt(center, factor) {
		g.syncCamera()
	}
}

// syncCamera copies the camera scale and origin to the map. The cached base
// layer stays. drawCachedNetworkBase draws it again when it cannot move it.
func (g *Game) syncCamera() {
	g.mapScale, g.mapOrigin = g.camera.scale, g.camera.origin
}

func (g *Game) toggleFollow() {
	g.followSelected = !g.followSelected
	if g.followSelected {
		g.followSelectedPod()
	}
}

func (g *Game) followSelectedPod() {
	state := g.mapSnapshot()
	if g.selected < 0 || g.selected >= len(state.Vehicles) {
		g.followSelected = false
		return
	}
	if g.camera.centerOn(state.Vehicles[g.selected].Pod.Position) {
		g.syncCamera()
	}
}

func (g *Game) mapSnapshot() sim.Snapshot {
	state := g.motion.Sample(time.Now())
	if len(state.Vehicles) == 0 {
		return g.state.Simulation
	}
	return state
}

// passengerStations returns the cached network-order slice. Callers must not change it.
func (g *Game) passengerStations() []sim.Station {
	return g.displayIndex().passengers
}

// cameraFitKey identifies the network of the last camera fit. It does not
// contain the state generation or the project revision. Rewinds, resets, and
// demand edits change these values on the same network. The camera then keeps
// the zoom and pan of the user.
type cameraFitKey struct {
	epoch  string
	bounds worldBounds
}

// networkBounds returns the world bounds of the network nodes and the lane
// control points. It returns false when the network has no nodes.
func networkBounds(network sim.Network) (worldBounds, bool) {
	bounds := worldBounds{left: math.Inf(1), top: math.Inf(1), right: math.Inf(-1), bottom: math.Inf(-1)}
	include := func(p sim.Point) {
		bounds.left = min(bounds.left, p.X)
		bounds.top = min(bounds.top, p.Y)
		bounds.right = max(bounds.right, p.X)
		bounds.bottom = max(bounds.bottom, p.Y)
	}
	for _, n := range network.Nodes {
		include(n.Position)
	}
	for _, l := range network.Lanes {
		if l.Control != nil {
			include(*l.Control)
		}
	}
	return bounds, len(network.Nodes) > 0
}

// fitNetwork fits the camera to the whole network when the epoch or the
// network bounds change. A layout change also clears the camera and causes a
// fit. Otherwise the camera keeps the zoom and pan of the user.
func (g *Game) fitNetwork() {
	index := g.displayIndex()
	bounds := index.bounds
	if !index.hasBounds {
		g.camera = mapCamera{scale: 1, minScale: 1, maxScale: mapMaxZoom, initialized: true, viewport: g.layout.mapViewport, panMargin: mapPanMargin * g.layout.unit, dragThreshold: mapDragThreshold * g.layout.unit}
		g.syncCamera()
		return
	}
	key := cameraFitKey{epoch: g.state.Epoch, bounds: bounds}
	if g.camera.initialized && g.cameraKey == key {
		return
	}
	g.camera.fit(cameraFit{bounds: bounds, viewport: g.layout.mapViewport, unit: g.layout.unit})
	g.cameraKey = key
	g.syncCamera()
}

func (g *Game) normalizeSelection() {
	stations := g.passengerStations()
	if len(stations) == 0 {
		return
	}
	valid := func(id string) bool {
		for _, s := range stations {
			if s.ID == id {
				return true
			}
		}
		return false
	}
	if !valid(g.origin) {
		g.origin = stations[0].ID
	}
	if !valid(g.destination) {
		g.destination = stations[len(stations)-1].ID
	}
	g.stationPage = max(0, min(g.stationPage, len(g.stationPages())-1))
	g.podPage = min(g.podPage, podPageCount(len(g.state.Simulation.Vehicles))-1)
}
