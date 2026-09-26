// Package view draws the simulation and translates input into commands.
package view

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/observe"
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
	// the center of the pod. The label is up and to the right of the pod,
	// outside the berth ring of a pod at a berth.
	podLabelLeft = 16
	podLabelTop  = -22

	inspectionLeft       = 816.0
	inspectionRight      = 1054.0
	inspectionValueLeft  = 916.0
	inspectionRowsTop    = 258.0
	inspectionRowSpacing = 27.0
	podSelectorTop       = 393.0
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
	message              string
	selected             int
	followSelected       bool
	stationPage, podPage int
	mapScale             float64
	mapOrigin            sim.Point
	camera               mapCamera
	touch                touchGestures
	cameraKey            cameraFitKey
	networkBase          *ebiten.Image
	networkBaseKey       networkCacheKey
	networkBaseValid     bool
	networkBaseLanes     []lanePath
	// networkBaseOrigin is the camera origin of the cached base layer.
	networkBaseOrigin sim.Point
	index             *networkIndex
	indexKey          networkIndexKey
	showOrders        bool
	// orderPage is the zero-based page of the Orders panel. A page past
	// the last page shows the last page.
	orderPage    int
	notice       string
	noticeAction string
	noticeTicks  int
	// acceptedOrigin and acceptedDestination are the stations of the last
	// accepted order. The request button reads Order accepted only while
	// From and To are these stations.
	acceptedOrigin, acceptedDestination string
	// savedEpoch and savedRevision come from the last accepted save point.
	// The command reply can arrive before the state that lists the save point,
	// so Rewind waits for that state and does not target an older save point.
	savedEpoch    string
	savedRevision uint64
	// sentAction is the action of the last command that the client
	// accepted. While pending is true, this command waits for its reply.
	sentAction string
	// ownEpoch and ownGeneration come from the last accepted reply to a
	// command of this game that starts a new generation. A change in the
	// same epoch to this generation or to an earlier one shows no session
	// change notice.
	ownEpoch      string
	ownGeneration uint64
	layout        displayLayout
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
	g.fitNetwork()
	g.tickNotice()
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
		g.reset()
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
	if g.followSelected {
		g.followSelectedPod()
	}
	if g.updateMapInput() {
		return nil
	}
	g.updateTouchInput()
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

// podPagerLabel returns the page count of the pod selector, for example
// "3 / 19". It shows between the page arrows. It returns false when all pods
// fit on one page.
func (g *Game) podPagerLabel() (label, bool) {
	pages := podPageCount(len(g.state.Simulation.Vehicles))
	if pages < 2 {
		return label{}, false
	}
	left := g.layout.right(podPagerLeft + podPagerArrowWidth)
	right := g.layout.right(1060 - podPagerArrowWidth)
	top := g.layout.bottom(podSelectorTop)
	area := image.Rect(int(math.Round(left)), int(math.Round(top)), int(math.Round(right)), int(math.Round(top+34*g.layout.unit)))
	return g.centerLabel(area, label{size: 10, value: fmt.Sprintf("%d / %d", g.podPage+1, pages), color: muted}), true
}

// tickNotice counts down the notice time by one tick. It clears the notice
// when the time ends.
func (g *Game) tickNotice() {
	if g.noticeTicks == 0 {
		return
	}
	g.noticeTicks--
	if g.noticeTicks == 0 {
		g.notice, g.noticeAction = "", ""
	}
}

func (g *Game) updateMapInput() bool {
	x, y := ebiten.CursorPosition()
	point := sim.Point{X: float64(x), Y: float64(y)}
	if _, wheelY := ebiten.Wheel(); wheelY != 0 && g.camera.contains(point) {
		factor := wheelZoomFactor(wheelY, runtime.GOOS == "js")
		if g.camera.zoomAt(point, factor) {
			g.syncCamera()
		}
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		if g.camera.contains(point) {
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

// reset sends the reset command only while the reset confirmation shows.
// The first press shows the confirmation and sends nothing. The confirmation
// shows for noticeDuration ticks, or until a different notice replaces it.
// The first press also clears an old message, so that the message does not
// show again when the confirmation ends. The second press removes the
// confirmation before it sends. If the send fails, the error shows and the
// next press asks again.
func (g *Game) reset() {
	if g.noticeAction == resetConfirmAction && g.noticeTicks > 0 {
		g.notice, g.noticeAction, g.noticeTicks = "", "", 0
		g.submit(session.Command{Action: "reset"})
		return
	}
	g.message = ""
	g.showNotice(resetConfirmAction, resetConfirmNotice)
}

func (g *Game) cycleSpeed() {
	speed := g.state.Speed * 2
	if speed > 8 {
		speed = 1
	}
	g.submit(session.Command{Action: "speed", Speed: speed})
}
func (g *Game) request() {
	if g.state.Simulation.Demo {
		return
	}
	g.submit(session.Command{Action: "trip", Origin: g.origin, Destination: g.destination})
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
// point. A state from a new epoch does not wait, because a restart clears the
// save points.
func (g *Game) rewindReady() bool {
	return g.state.Epoch != g.savedEpoch || g.state.Revision >= g.savedRevision
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
	busy := state.Demo || !g.connected || g.pending
	// The request button reads Order accepted for as long as the notice of
	// the accepted order shows. The user can change From and To during and
	// after the order, so the label shows only while From and To are the
	// stations of that order. Enter also sends the order, so the label
	// names the key.
	requestLabel := "Order [Enter]"
	if g.noticeAction == "trip" && g.origin == g.acceptedOrigin && g.destination == g.acceptedDestination {
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
		{x: 810, y: 60, w: 90, h: 28, label: "Save point", action: "checkpoint"},
		{x: 912, y: 60, w: 148, h: 28, label: rewindLabel(g.state), action: "rewind"},
		{x: 617, y: 104, w: 28, h: 24, label: "−", action: "map-zoom-out"},
		{x: 651, y: 104, w: 28, h: 24, label: "+", action: "map-zoom-in"},
		{x: 685, y: 104, w: 66, h: 24, label: "Fit", action: "map-fit"},
		{x: 964, y: 104, w: 96, h: 24, label: followLabel, selected: g.followSelected, action: "map-follow", fontSize: 11},
		{x: 810, y: 509, w: 120, h: 26, label: fmt.Sprintf("Orders %d", outstandingOrderCount(state)), selected: g.showOrders, action: "orders"},
		{x: 940, y: 509, w: 120, h: 26, label: "Demand", selected: g.showDemand, action: "demand"},
		{x: 930, y: 644, w: 130, h: 42, label: requestLabel, selected: true, disabled: busy || g.destination == g.origin, action: "request"},
		{x: 810, y: 433, w: 250, h: 32, label: pauseLabel, action: "pause"},
		{x: 810, y: 471, w: 119, h: 32, label: fmt.Sprintf("Speed %dx [S]", g.state.Speed), action: "speed"},
		{x: 941, y: 471, w: 119, h: 32, label: "Reset [Shift+R]", action: "reset"},
	}
	for i, v := range state.Vehicles {
		if i/podsPerPage != g.podPage {
			continue
		}
		buttons = append(buttons, button{x: 810 + float64(i%podsPerPage)*podButtonStep, y: podSelectorTop, w: 32, h: 34, label: fleetPodLabel(i), selected: !g.showOrders && !g.showDemand && g.selected == i, action: "pod/" + v.Pod.ID})
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
	if b.x >= 796 && !b.expandsWithMap || strings.HasPrefix(b.action, "map-") || b.action == "request" {
		x += g.layout.extraX
	}
	// The Orders page controls are above the pod selector, but they move
	// down with it.
	if movesDown(b.x, b.y) || strings.HasPrefix(b.action, "orders-") {
		y += g.layout.extraY
	}
	b.x, b.y, b.w, b.h = x, y, b.w*g.layout.unit, b.h*g.layout.unit
	return b
}

// click reports a press of Reset or Rewind, so that the new state can render
// before the next tick.
func (g *Game) click(point sim.Point) bool {
	for _, b := range g.buttons() {
		if b.disabled || point.X < b.x || point.X >= b.x+b.w || point.Y < b.y || point.Y >= b.y+b.h {
			continue
		}
		switch b.action {
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
		case "pods-prev":
			g.turnPodPage(-1)
		case "pods-next":
			g.turnPodPage(1)
		case "stations-prev":
			g.stationPage--
		case "stations-next":
			g.stationPage++
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
			g.changeDemand(b.action)
		case "request":
			g.request()
		case "pause":
			g.pause()
		case "speed":
			g.cycleSpeed()
		case "reset":
			g.reset()
			return true
		case "checkpoint":
			g.checkpoint()
		case "rewind":
			g.rewind()
			return true
		default:
			if id, ok := strings.CutPrefix(b.action, "pod/"); ok {
				for i, v := range g.state.Simulation.Vehicles {
					if v.Pod.ID == id {
						g.selected = i
						break
					}
				}
				g.showOrders, g.showDemand = false, false
			} else if origin, ok := strings.CutPrefix(b.action, "from/"); ok {
				g.origin = origin
			} else if destination, ok := strings.CutPrefix(b.action, "to/"); ok {
				g.destination = destination
			}
			g.message = ""
		}
		return false
	}
	if g.camera.contains(point) {
		g.pickOnMap(point, false)
	}
	return false
}

// Layout is the integer fallback for platforms that do not use LayoutF.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return g.layoutFor(layoutInput{outsideWidth: outsideWidth, outsideHeight: outsideHeight, deviceScale: 1})
}

// LayoutF renders at the monitor's native pixel density while retaining CSS-sized controls.
func (g *Game) LayoutF(outsideWidth, outsideHeight float64) (float64, float64) {
	scale := 1.0
	if monitor := ebiten.Monitor(); monitor != nil {
		scale = monitor.DeviceScaleFactor()
	}
	w, h := g.layoutFor(layoutInput{outsideWidth: int(math.Round(outsideWidth)), outsideHeight: int(math.Round(outsideHeight)), deviceScale: scale})
	return float64(w), float64(h)
}

func (g *Game) layoutFor(input layoutInput) (int, int) {
	next := newDisplayLayout(input)
	if g.layout != next {
		g.layout = next
		g.camera.initialized = false
		g.networkBaseValid = false
	}
	return next.width, next.height
}

func (g *Game) ensureLayout() {
	if g.layout.width == 0 {
		g.layout = newDisplayLayout(layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1})
	}
}

// Draw renders an independent snapshot without changing simulation state.
func (g *Game) Draw(screen *ebiten.Image) {
	g.ensureLayout()
	g.fitNetwork()
	screen.Fill(rgb(background))
	for _, header := range g.headerLabels() {
		g.label(screen, header)
	}
	vector.FillRect(screen, float32(g.layout.x(24)), float32(g.layout.y(96)), float32(g.layout.x(748)+g.layout.extraX), float32(g.layout.y(474)+g.layout.extraY), rgb(panel), false)
	vector.FillRect(screen, float32(g.layout.right(796)), float32(g.layout.y(96)), float32(g.layout.x(280)), float32(g.layout.y(474)+g.layout.extraY), rgb(panel), false)
	vector.FillRect(screen, float32(g.layout.x(24)), float32(g.layout.bottom(590)), float32(g.layout.x(1052)+g.layout.extraX), float32(g.layout.y(142)), rgb(panel), false)
	state := g.state.Simulation
	g.drawNetwork(screen, g.mapSnapshot())
	g.drawConnectionState(screen, time.Now())
	switch {
	case g.showDemand:
		g.drawDemand(screen)
	case g.showOrders:
		g.drawOrders(screen, state)
	default:
		g.drawInspection(screen, state)
	}
	g.drawControls(screen, state)
	for _, b := range g.buttons() {
		g.drawButton(screen, b)
	}
	g.label(screen, g.connectionFooter(ebiten.IsFocused()))
}

// headerLabels returns the title and the header text above the panels.
// Beside the title, the run status shows in muted text, or in amber while
// the run is paused. Before the first state frame, the game has no run
// state, no network, and no pods, so the header shows only the title.
func (g *Game) headerLabels() []label {
	labels := []label{{x: 28, y: 22, size: 30, value: "podsim", color: foreground}}
	if g.state.Epoch == "" {
		return labels
	}
	status := label{x: 157, y: 34, size: 14, value: runStatus(g.state), color: muted}
	if g.state.Simulation.Paused {
		status.color = amber
	}
	return append(labels, status, label{x: 815, y: 34, size: 14, value: fmt.Sprintf("%d STATIONS     %d PODS", len(g.passengerStations()), len(g.state.Simulation.Vehicles)), color: accent})
}

// runStatus returns the run status of state for the header. The status
// gives PAUSED while the run is paused, then the playback speed, the
// simulated time, and the number of completed journeys. An example is
// "PAUSED  2x  1234.5 s  57 completed". Before the first state frame, the
// state has no epoch, and the status is empty.
func runStatus(state session.State) string {
	if state.Epoch == "" {
		return ""
	}
	status := fmt.Sprintf("%dx  %.1f s  %d completed", state.Speed, float64(state.Simulation.Tick)/sim.TicksPerSecond, state.Simulation.Completed)
	if state.Simulation.Paused {
		status = "PAUSED  " + status
	}
	return status
}

// focusHint tells the user how to give the keyboard focus to the simulation.
const focusHint = "Click the simulation to use keyboard shortcuts"

// connectionFooter returns the connection status line below the panels.
// focused is true when the simulation has the keyboard focus. Without the
// focus, the line shows the focus hint in amber.
func (g *Game) connectionFooter(focused bool) label {
	footer := label{x: 28, y: 740, size: 11, value: g.connectionLabel(), color: muted}
	// A lost connection and a pending command win over the focus hint. In
	// these states, the client rejects commands until the connection comes
	// back or the server confirms the command. The focus hint shows after
	// that.
	if g.connected && !g.pending && !focused {
		footer.value, footer.color = focusHint, amber
	}
	return footer
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

// mapHintLabel names the map input above the map. A click selects a pod or
// sets From, Shift+click sets To, Tab and Shift+Tab select the next and the
// previous pod, the wheel zooms, and a drag pans. It ends to the left of the
// zoom buttons.
var mapHintLabel = label{x: 136, y: 113, size: 11, value: "CLICK POD / CLICK FROM / SHIFT+CLICK TO / TAB POD / SCROLL ZOOM / DRAG PAN", color: muted}

func (g *Game) drawNetwork(screen *ebiten.Image, state sim.Snapshot) {
	g.label(screen, label{x: 44, y: 113, size: 12, value: "NETWORK", color: muted})
	g.label(screen, mapHintLabel)
	mapScreen, ok := screen.SubImage(g.layout.mapViewport).(*ebiten.Image)
	if !ok {
		return
	}
	markers := g.collapsedStationMarkers()
	style := newNetworkStyle(networkStyleInput{network: g.network, markers: markers, lineLanes: g.currentLineLanes(), scale: g.mapScale, unit: g.layout.unit})
	detailed := style.detailed
	var lanes []lanePath
	var lanesShift sim.Point
	if detailed {
		g.releaseNetworkBase()
		lanes = g.drawBaseNetwork(mapScreen, baseNetworkInput{style: style, area: g.layout.mapViewport})
	} else {
		lanes, lanesShift = g.drawCachedNetworkBase(mapScreen, style)
	}
	// Pods, rings, markers and route arrows are antialiased sprites on all
	// networks.
	scale := spriteScale{unit: g.layout.unit, markerRadius: style.markerRadius}
	markerKey := markerSprite(style.markerRadius, g.layout.unit)
	for _, station := range g.network.Stations {
		if marker, ok := markers[station.ID]; ok {
			g.sprites.draw(mapScreen, spriteDraw{center: marker, key: markerKey, scale: scale})
		}
	}
	selected, hasSelected := selectedVehicle(state, g.selected)
	collapsedStations := make(map[string]bool)
	stationMonitor := observe.NewStationMonitor(g.network)
	labelRanks := g.currentStationLabelRanks()
	var collapsedLabels []collapsedStationLabel
	var expanded []expandedStationText
	for _, station := range g.network.Stations {
		status := stationMonitor.Summarize(station, state)
		if marker, ok := markers[station.ID]; ok {
			collapsedStations[station.ID] = true
			if len(station.Berths) > 0 {
				collapsedLabels = append(collapsedLabels, g.collapsedStationLabel(collapsedStationLabelInput{station: station, status: status, marker: marker, rank: labelRanks[station.ID]}))
			}
			continue
		}
		stationText := g.expandedStationText(expandedStationInput{station: station, status: status, state: state})
		for _, berth := range stationText.berths {
			g.sprites.draw(mapScreen, spriteDraw{center: berth.center, key: ringSprite(berthRingRadius*g.layout.unit, 2*g.layout.unit, berth.shade), scale: scale})
		}
		expanded = append(expanded, stationText)
	}
	// The station text keeps off the lanes at their place on the screen.
	if len(expanded) > 0 {
		lanes = movedLanePaths(lanes, lanesShift)
	}
	// Draw the station text after all berth rings, so that no ring covers
	// the text.
	placedText := g.placeExpandedStationText(expanded, lanes)
	g.drawStationText(mapScreen, placedText)
	podLabels := g.podMapLabels(state.Vehicles, collapsedStations)
	// The selected index can be past the pods of an empty or smaller state.
	var selectedPodLabel label
	if g.selected >= 0 && g.selected < len(podLabels) {
		selectedPodLabel = podLabels[g.selected]
	}
	shownLabels := g.drawCollapsedStationLabels(mapScreen, collapsedLabelsInput{
		labels: collapsedLabels, selected: selected,
		markers: markers, markerRadius: style.markerRadius, selectedPodLabel: selectedPodLabel,
	})
	shownLabels = appendStationTextAreas(shownLabels, placedText)
	podLabels = g.clearPodLabels(podLabels, shownLabels)
	// Draw the route after the station markers and labels, and before the
	// pods. A marker can sit on a line junction, and a label can cover a
	// line. The route must stay visible through them, and the pods stay on
	// top of the route.
	if hasSelected && selected.Pod.Activity != sim.Idle {
		shade := g.podPurpose(selected, state).color()
		// The route lines and arrows have one color, so the arrows can go
		// after all the lines. Ebiten can then draw the arrow sprites in one
		// batch.
		arrows := make([]arrow, 0, len(selected.Route))
		for _, lane := range selected.Route {
			geometry := g.laneGeometry(lane, detailed)
			geometry.draw(mapScreen, laneStroke{width: float32(routeWidth * g.layout.unit), color: shade, antialias: detailed})
			arrows = append(arrows, arrow{tip: geometry.arrowTip, direction: geometry.arrowDirection, size: routeArrowSize, color: shade, unit: g.layout.unit})
		}
		for _, routeArrow := range arrows {
			if d, ok := routeArrow.spriteDraw(scale); ok {
				g.sprites.draw(mapScreen, d)
			}
		}
	}
	for i, v := range state.Vehicles {
		parkedInCluster := v.Pod.Activity == sim.Idle && collapsedStations[v.Pod.StationID]
		if parkedInCluster && i != g.selected {
			continue
		}
		p := g.mapPoint(v.Pod.Position)
		purpose := g.podPurpose(v, state)
		shade := purpose.color()
		if v.Pod.WaitReason != sim.NoWait {
			g.sprites.draw(mapScreen, spriteDraw{center: p, key: ringSprite(11*g.layout.unit, 2*g.layout.unit, amber), scale: scale})
		}
		if i == g.selected {
			g.sprites.draw(mapScreen, spriteDraw{center: p, key: ringSprite(9*g.layout.unit, 1.5*g.layout.unit, foreground), scale: scale})
		}
		g.sprites.draw(mapScreen, spriteDraw{center: p, key: podMarkSprite(purpose, g.layout.unit), scale: scale})
		if podLabel := podLabels[i]; podLabel.value != "" {
			podLabel.color = shade
			g.label(mapScreen, podLabel)
		}
	}
	// Without a network, the map has no scale.
	if bar, ok := newScaleBar(g.mapScale, g.layout.unit); ok && len(g.network.Nodes) > 0 {
		vector.StrokeLine(screen, float32(g.layout.x(48)), float32(g.layout.bottom(540)), float32(g.layout.x(48+bar.length)), float32(g.layout.bottom(540)), float32(2*g.layout.unit), rgb(muted), detailed)
		g.label(screen, label{x: 64 + bar.length, y: 531, size: 12, value: bar.label, color: muted})
	}
	g.drawPodLegend(screen, scale)
}

// selectedVehicle returns the vehicle at index in state. It returns false
// when state has no vehicle at index, for example before the first state
// frame.
func selectedVehicle(state sim.Snapshot, index int) (sim.Vehicle, bool) {
	if index < 0 || index >= len(state.Vehicles) {
		return sim.Vehicle{}, false
	}
	return state.Vehicles[index], true
}

// showStationBerths reports whether the berths of a station separate on the
// screen. A station with fewer than two berth nodes in the network always
// shows its berths.
func (g *Game) showStationBerths(station sim.Station) bool {
	spacing, ok := g.displayIndex().berthSpacing[station.ID]
	return !ok || spacing*g.mapScale >= 34*g.layout.unit
}

// overviewNameRunes is the largest number of runes of a station name in an
// overview label. A longer name ends with an ellipsis. The limit keeps the
// names of the London Parking facilities complete.
const overviewNameRunes = 20

// collapsedStationLabel is the overview label of a collapsed station.
type collapsedStationLabel struct {
	stationID string
	// rank is the place of the station in the label order. See
	// stationLabelRanks and selectCollapsedStationLabels.
	rank    int
	primary label
	// collisionValue is the primary text with all berths occupied. The label
	// bounds use it, so they do not change when a pod arrives or departs.
	collisionValue string
	// secondary is the queue line below the primary text. It is empty when
	// the station has no queue.
	secondary string
}

// collapsedStationLabelInput holds the station state of an overview label.
type collapsedStationLabelInput struct {
	station sim.Station
	status  observe.StationMetrics
	// marker is the screen position of the collapsed station marker.
	marker sim.Point
	rank   int
}

// collapsedStationLabel returns the overview label of a collapsed station.
// The label starts to the right of the marker and above it.
func (g *Game) collapsedStationLabel(input collapsedStationLabelInput) collapsedStationLabel {
	name := shortText(strings.TrimPrefix(input.station.Name, "Station "), overviewNameRunes)
	berths := len(input.station.Berths)
	collapsed := collapsedStationLabel{
		stationID: input.station.ID,
		rank:      input.rank,
		primary: label{
			x: input.marker.X + 16*g.layout.unit, y: input.marker.Y - 14*g.layout.unit,
			size: 10, value: fmt.Sprintf("%s  %d/%d", name, input.status.Occupied, berths), color: foreground, mapLabel: true,
		},
		collisionValue: fmt.Sprintf("%s  %d/%d", name, berths, berths),
	}
	if input.status.EntranceStopped > 0 || input.status.ExitStopped > 0 {
		collapsed.secondary = fmt.Sprintf("In %d · Out %d", input.status.EntranceStopped, input.status.ExitStopped)
	}
	return collapsed
}

// secondaryLabel returns the queue line of the overview label. deviceScale
// is the number of screen pixels in one CSS pixel. The line spacing is in
// CSS pixels, as the label size is.
func (candidate collapsedStationLabel) secondaryLabel(deviceScale float64) label {
	return label{x: candidate.primary.x, y: candidate.primary.y + 16*deviceScale, size: 9, value: candidate.secondary, color: amber, mapLabel: true}
}

// boundedStationLabel holds the screen areas of an overview label and of its
// station marker.
type boundedStationLabel struct {
	stationID string
	rank      int
	// queued is true when the label has a queue line.
	queued bool
	bounds image.Rectangle
	// marker is the screen area of the station marker with its outline.
	marker image.Rectangle
}

// collapsedLabelsInput holds the overview labels and the map items that the
// labels must not cover.
type collapsedLabelsInput struct {
	labels   []collapsedStationLabel
	selected sim.Vehicle
	// markers holds the screen position of each collapsed station marker by
	// station ID. markerRadius is the marker radius in screen pixels.
	markers      map[string]sim.Point
	markerRadius float64
	// selectedPodLabel is the map label of the selected pod. Its value is
	// empty when the pod has no map label.
	selectedPodLabel label
}

// drawCollapsedStationLabels draws the overview labels that
// visibleCollapsedStationLabels selects. It returns the screen areas of the
// labels that it draws.
func (g *Game) drawCollapsedStationLabels(screen *ebiten.Image, input collapsedLabelsInput) []image.Rectangle {
	bounded, visible := g.visibleCollapsedStationLabels(input)
	var shown []image.Rectangle
	for index, candidate := range input.labels {
		if !visible[index] {
			continue
		}
		g.label(screen, candidate.primary)
		if candidate.secondary != "" {
			g.label(screen, candidate.secondaryLabel(g.layout.deviceScale))
		}
		shown = append(shown, bounded[index].bounds)
	}
	return shown
}

// visibleCollapsedStationLabels returns the screen areas of the overview
// labels and reports which labels show. The labels of the From and To
// stations and of the stations of the selected pod always show. On a dense
// map, the other labels do not cover the label of the selected pod. See
// selectCollapsedStationLabels.
func (g *Game) visibleCollapsedStationLabels(input collapsedLabelsInput) ([]boundedStationLabel, []bool) {
	selected := input.selected
	preferred := map[string]bool{g.origin: true, g.destination: true, selected.Pod.StationID: true, selected.RelocatingTo: true}
	if selected.Request != nil {
		preferred[selected.Request.From] = true
		preferred[selected.Request.To] = true
	}
	delete(preferred, "")
	selection := collapsedLabelSelection{labels: g.boundedStationLabels(input), preferred: preferred, dense: len(g.network.Stations) > 30}
	if selection.dense && input.selectedPodLabel.value != "" {
		selection.occupied = append(selection.occupied, g.labelBounds(input.selectedPodLabel))
	}
	return selection.labels, selectCollapsedStationLabels(selection)
}

// boundedStationLabels returns the screen areas of the overview labels and of
// their station markers.
func (g *Game) boundedStationLabels(input collapsedLabelsInput) []boundedStationLabel {
	// The outline is centered on the marker radius.
	markerRadius := input.markerRadius + markerOutlineWidth*g.layout.unit/2
	bounded := make([]boundedStationLabel, len(input.labels))
	for index, candidate := range input.labels {
		marker := input.markers[candidate.stationID]
		bounded[index] = boundedStationLabel{
			stationID: candidate.stationID,
			rank:      candidate.rank,
			queued:    candidate.secondary != "",
			bounds:    g.collapsedStationLabelBounds(candidate),
			marker: image.Rect(
				int(math.Floor(marker.X-markerRadius)), int(math.Floor(marker.Y-markerRadius)),
				int(math.Ceil(marker.X+markerRadius)), int(math.Ceil(marker.Y+markerRadius)),
			),
		}
	}
	return bounded
}

// labelBounds returns the screen area of the text of a map label.
func (g *Game) labelBounds(value label) image.Rectangle {
	width, height := text.Measure(value.value, g.labelFace(value), 0)
	return image.Rect(
		int(math.Floor(value.x)), int(math.Floor(value.y)),
		int(math.Ceil(value.x+width)), int(math.Ceil(value.y+height)),
	)
}

// collapsedStationLabelBounds returns the screen area of an overview label
// with a padding of 3 units. The area holds only the lines that the map
// draws. The primary line has the width of the text with all berths occupied.
func (g *Game) collapsedStationLabelBounds(candidate collapsedStationLabel) image.Rectangle {
	primary := candidate.primary
	primary.value = candidate.collisionValue
	bounds := g.labelBounds(primary)
	if candidate.secondary != "" {
		bounds = bounds.Union(g.labelBounds(candidate.secondaryLabel(g.layout.deviceScale)))
	}
	return bounds.Inset(-int(math.Ceil(3 * g.layout.unit)))
}

// collapsedLabelSelection holds the overview labels for
// selectCollapsedStationLabels.
type collapsedLabelSelection struct {
	labels []boundedStationLabel
	// preferred holds the IDs of the stations whose labels always show.
	preferred map[string]bool
	// occupied holds the screen areas that no label other than a preferred
	// label can cover, such as the label of the selected pod.
	occupied []image.Rectangle
	// dense is true on a map with more than 30 stations. On other maps, all
	// labels show.
	dense bool
}

// selectCollapsedStationLabels reports which overview labels show. On a
// dense map, it places the preferred labels first and then the other labels.
// In each of the two groups, the labels with a queue line come first, and
// then the rank order applies. A label that is not preferred shows only when
// it does not cover an occupied area, an earlier label or the marker of an
// earlier station. Its own marker also must not be under an earlier label.
//
// Thus, when neither label has a queue line, the label of a large station
// can cover the marker of a smaller station, but not the opposite. At Fit,
// the markers of a dense map are too close for most labels to stay clear of
// all of them. The queue line is an alert, and it makes the label taller.
// Because the labels with a queue line come first, a label without a queue
// line cannot hide them.
func selectCollapsedStationLabels(selection collapsedLabelSelection) []bool {
	labels := selection.labels
	visible := make([]bool, len(labels))
	if !selection.dense {
		for index := range visible {
			visible[index] = true
		}
		return visible
	}
	order := make([]int, len(labels))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return cmp.Or(trueFirst(labels[a].queued, labels[b].queued), cmp.Compare(labels[a].rank, labels[b].rank))
	})
	occupied := slices.Clone(selection.occupied)
	var placed []image.Rectangle
	for _, priority := range []bool{true, false} {
		for _, index := range order {
			candidate := labels[index]
			if selection.preferred[candidate.stationID] != priority {
				continue
			}
			blocked := slices.ContainsFunc(occupied, candidate.bounds.Overlaps) ||
				slices.ContainsFunc(placed, candidate.marker.Overlaps)
			occupied = append(occupied, candidate.marker)
			if blocked && !priority {
				continue
			}
			visible[index] = true
			occupied = append(occupied, candidate.bounds)
			placed = append(placed, candidate.bounds)
		}
	}
	return visible
}

// podMapLabels returns the map label of each pod by fleet index, without a
// color. The label of a pod without a map label has an empty value. Other
// than the selected pod, a pod that is parked in a collapsed station has no
// map label. collapsedStations is true for the ID of each collapsed station.
// See showPodMapLabel for the zoom rule.
func (g *Game) podMapLabels(vehicles []sim.Vehicle, collapsedStations map[string]bool) []label {
	labels := make([]label, len(vehicles))
	for index, vehicle := range vehicles {
		parkedInCluster := vehicle.Pod.Activity == sim.Idle && collapsedStations[vehicle.Pod.StationID]
		if (parkedInCluster && index != g.selected) || !g.showPodMapLabel(index) {
			continue
		}
		p := g.mapPoint(vehicle.Pod.Position)
		labels[index] = label{x: p.X + podLabelLeft*g.layout.unit, y: p.Y + podLabelTop*g.layout.unit, size: 11, value: fleetPodLabel(index), mapLabel: true}
	}
	return labels
}

// clearPodLabels returns the pod labels without the labels that overlap
// shown station text on a dense map. The label of the selected pod stays.
// stationLabels holds the screen areas of the shown overview labels and of
// the text of expanded stations.
func (g *Game) clearPodLabels(podLabels []label, stationLabels []image.Rectangle) []label {
	if len(g.network.Stations) <= 30 {
		return podLabels
	}
	cleared := slices.Clone(podLabels)
	for index, podLabel := range cleared {
		if index == g.selected || podLabel.value == "" {
			continue
		}
		if slices.ContainsFunc(stationLabels, g.labelBounds(podLabel).Overlaps) {
			cleared[index] = label{}
		}
	}
	return cleared
}

func (g *Game) showPodMapLabel(index int) bool {
	return index == g.selected || len(g.network.Stations) <= 30 || g.mapScale >= 4*g.camera.minScale
}

func (g *Game) podHiddenInCluster(vehicle sim.Vehicle, index int) bool {
	if index == g.selected || vehicle.Pod.Activity != sim.Idle {
		return false
	}
	station, ok := g.network.Station(vehicle.Pod.StationID)
	return ok && !g.showStationBerths(station)
}

// networkCacheKey identifies the content of the cached base layer. The
// camera origin is not in the key. A pan moves the cached layer on the
// screen. See baseLayerShift.
type networkCacheKey struct {
	network networkIndexKey
	scale   float64
	unit    float64
	// viewport is the map viewport. The cached layer covers it and a
	// margin around it.
	viewport image.Rectangle
}

type laneGeometry struct {
	points [33]sim.Point
	count  int
	// arrowTip is the point on the drawn lane at 61 percent of its screen
	// length. arrowDirection points in the direction of travel at the tip.
	arrowTip, arrowDirection sim.Point
}

type laneStroke struct {
	width     float32
	color     uint32
	antialias bool
}

// screenLength returns the length of the lane on the screen in pixels.
func (geometry laneGeometry) screenLength() float64 {
	length := 0.0
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		length += math.Hypot(to.X-from.X, to.Y-from.Y)
	}
	return length
}

func (geometry laneGeometry) draw(screen *ebiten.Image, stroke laneStroke) {
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		vector.StrokeLine(screen, float32(from.X), float32(from.Y), float32(to.X), float32(to.Y), stroke.width, rgb(stroke.color), stroke.antialias)
	}
}

func (g *Game) laneGeometry(lane sim.Lane, detailed bool) laneGeometry {
	from, to := g.nodePosition(lane.From), g.nodePosition(lane.To)
	position := func(t float64) sim.Point {
		if lane.Control == nil {
			return sim.Point{X: from.X + (to.X-from.X)*t, Y: from.Y + (to.Y-from.Y)*t}
		}
		u := 1 - t
		return sim.Point{X: u*u*from.X + 2*u*t*lane.Control.X + t*t*to.X, Y: u*u*from.Y + 2*u*t*lane.Control.Y + t*t*to.Y}
	}
	segments := 1
	if lane.Control != nil {
		segments = 32
		if !detailed {
			extent := math.Hypot(lane.Control.X-from.X, lane.Control.Y-from.Y) + math.Hypot(to.X-lane.Control.X, to.Y-lane.Control.Y)
			segments = min(32, max(4, int(math.Ceil(extent*g.mapScale/8))))
		}
	}
	length := 0.0
	if detailed {
		length = g.network.Length(lane)
	}
	geometry := laneGeometry{count: segments + 1}
	for i := 0; i <= segments; i++ {
		point := position(float64(i) / float64(segments))
		if detailed {
			point = g.network.Position(lane, length*float64(i)/float64(segments))
		}
		geometry.points[i] = g.mapPoint(point)
	}
	geometry.arrowTip, geometry.arrowDirection = geometry.along(.61)
	return geometry
}

// along returns the point at a fraction of the screen length of the lane,
// and the direction of the lane segment at that point. On a lane with no
// screen length, the direction is zero.
func (geometry laneGeometry) along(fraction float64) (point, direction sim.Point) {
	remaining := fraction * geometry.screenLength()
	point = geometry.points[0]
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		direction = sim.Point{X: to.X - from.X, Y: to.Y - from.Y}
		length := math.Hypot(direction.X, direction.Y)
		if length > 0 && remaining <= length {
			t := remaining / length
			return sim.Point{X: from.X + direction.X*t, Y: from.Y + direction.Y*t}, direction
		}
		remaining -= length
		point = to
	}
	return point, direction
}

// baseNetworkInput holds the style and the place of a network base layer.
type baseNetworkInput struct {
	style networkStyle
	// area is the screen area of the layer. drawBaseNetwork returns the
	// paths of the lanes in this area.
	area image.Rectangle
	// offset moves each screen point before drawBaseNetwork draws it. The
	// cached layer uses it, because its image starts at the corner of area.
	offset sim.Point
}

// drawBaseNetwork draws the lanes, their direction arrows, and the node dots
// in the network style. It draws the arrows after all the lanes, so that no
// lane covers an arrow. It returns the screen paths of the lanes in the
// area of the layer, without the offset.
func (g *Game) drawBaseNetwork(screen *ebiten.Image, input baseNetworkInput) []lanePath {
	style := input.style
	var arrows []arrow
	var paths []lanePath
	for _, lane := range g.network.Lanes {
		geometry, path, ok := g.baseLane(lane, style.detailed, input.area)
		if ok {
			paths = append(paths, path)
		}
		geometry = geometry.moved(input.offset)
		geometry.draw(screen, style.laneStroke(lane))
		if a, ok := style.laneArrow(lane, geometry); ok {
			arrows = append(arrows, a)
		}
	}
	for _, a := range style.spacedArrows(arrows) {
		drawArrow(screen, a)
	}
	if !style.nodeDots {
		return paths
	}
	for _, node := range g.network.Nodes {
		point := movePoint(g.mapPoint(node.Position), input.offset)
		vector.FillCircle(screen, float32(point.X), float32(point.Y), float32(3*g.layout.unit), rgb(muted), style.detailed)
	}
	return paths
}

// baseLane returns the screen geometry of a lane and its screen path. ok is
// false when no part of the lane is in area. It does not draw.
func (g *Game) baseLane(lane sim.Lane, detailed bool, area image.Rectangle) (geometry laneGeometry, path lanePath, ok bool) {
	geometry = g.laneGeometry(lane, detailed)
	path, ok = geometry.pathIn(area)
	return geometry, path, ok
}

// movePoint returns point moved by offset.
func movePoint(point, offset sim.Point) sim.Point {
	return sim.Point{X: point.X + offset.X, Y: point.Y + offset.Y}
}

// moved returns the lane geometry moved on the screen by offset.
func (geometry laneGeometry) moved(offset sim.Point) laneGeometry {
	if offset == (sim.Point{}) {
		return geometry
	}
	for i := range geometry.count {
		geometry.points[i] = movePoint(geometry.points[i], offset)
	}
	geometry.arrowTip = movePoint(geometry.arrowTip, offset)
	return geometry
}

// movedLanePaths returns the lane paths moved on the screen by offset.
func movedLanePaths(paths []lanePath, offset sim.Point) []lanePath {
	if offset == (sim.Point{}) {
		return paths
	}
	moved := make([]lanePath, len(paths))
	for i, path := range paths {
		moved[i] = make(lanePath, len(path))
		for j, point := range path {
			moved[i][j] = movePoint(point, offset)
		}
	}
	return moved
}

// baseLayerMargin returns the margin in screen pixels of the cached base
// layer around the map viewport. It is a quarter of the shorter side of the
// viewport.
func baseLayerMargin(viewport image.Rectangle) int {
	return min(viewport.Dx(), viewport.Dy()) / 4
}

// baseLayerInput holds the cached base layer state and the current map
// state for baseLayerShift.
type baseLayerInput struct {
	// valid is false when the cache has no layer.
	valid           bool
	cached, current networkCacheKey
	// drawnOrigin is the camera origin of the cached layer. origin is the
	// current camera origin.
	drawnOrigin, origin sim.Point
	// margin is the margin in screen pixels of the cached layer around the
	// map viewport.
	margin float64
}

// baseLayerShift returns the screen distance from the cached base layer to
// its current place. ok is false when the cache must draw the layer again:
// when it has no layer, when the network, the scale, the unit or the
// viewport changed, or when a pan moved the viewport past the margin of the
// layer.
func baseLayerShift(input baseLayerInput) (shift sim.Point, ok bool) {
	if !input.valid || input.cached != input.current {
		return sim.Point{}, false
	}
	shift = sim.Point{X: input.origin.X - input.drawnOrigin.X, Y: input.origin.Y - input.drawnOrigin.Y}
	if math.Abs(shift.X) > input.margin || math.Abs(shift.Y) > input.margin {
		return sim.Point{}, false
	}
	return shift, true
}

// baseLayerArea returns the screen area of the cached base layer: the map
// viewport and the margin around it.
func baseLayerArea(viewport image.Rectangle) image.Rectangle {
	return viewport.Inset(-baseLayerMargin(viewport))
}

// baseLayerPlan tells drawCachedNetworkBase how to show the cached base
// layer in the current frame.
type baseLayerPlan struct {
	// area is the screen area of the layer when the cache drew it.
	area image.Rectangle
	// shift is the screen distance from the cached layer to its current
	// place.
	shift sim.Point
	// redraw is true when the cache must draw the layer again.
	redraw bool
}

// imageOffset returns the distance that moves a screen point to its point
// in the layer image. The image starts at the corner of the layer area.
func (plan baseLayerPlan) imageOffset() sim.Point {
	return sim.Point{X: -float64(plan.area.Min.X), Y: -float64(plan.area.Min.Y)}
}

// planBaseLayer decides how to show the cached base layer in the current
// frame. It does not draw. When the layer must be drawn again, it records
// the new key and camera origin of the cache, so a later pan is measured
// from the new layer.
func (g *Game) planBaseLayer() baseLayerPlan {
	viewport := g.layout.mapViewport
	key := g.currentNetworkCacheKey()
	shift, ok := baseLayerShift(baseLayerInput{
		valid: g.networkBaseValid, cached: g.networkBaseKey, current: key,
		drawnOrigin: g.networkBaseOrigin, origin: g.mapOrigin, margin: float64(baseLayerMargin(viewport)),
	})
	plan := baseLayerPlan{area: baseLayerArea(viewport), shift: shift, redraw: !ok}
	if plan.redraw {
		g.networkBaseKey = key
		g.networkBaseOrigin = g.mapOrigin
		g.networkBaseValid = true
	}
	return plan
}

// drawCachedNetworkBase draws the cached base network. The cached layer
// covers the map viewport and a margin around it. See planBaseLayer. It
// returns the screen paths of the lanes in the layer, as they were when it
// drew the layer, and the distance to move them.
func (g *Game) drawCachedNetworkBase(screen *ebiten.Image, style networkStyle) ([]lanePath, sim.Point) {
	area := baseLayerArea(g.layout.mapViewport)
	if g.networkBase == nil || g.networkBase.Bounds().Size() != area.Size() {
		if g.networkBase != nil {
			g.networkBase.Deallocate()
		}
		g.networkBase = ebiten.NewImage(area.Dx(), area.Dy())
		g.networkBaseValid = false
	}
	plan := g.planBaseLayer()
	if plan.redraw {
		g.networkBase.Clear()
		g.networkBaseLanes = g.drawBaseNetwork(g.networkBase, baseNetworkInput{
			style: style, area: plan.area, offset: plan.imageOffset(),
		})
	}
	options := &ebiten.DrawImageOptions{}
	options.GeoM.Translate(float64(plan.area.Min.X)+plan.shift.X, float64(plan.area.Min.Y)+plan.shift.Y)
	screen.DrawImage(g.networkBase, options)
	return g.networkBaseLanes, plan.shift
}

func (g *Game) currentNetworkCacheKey() networkCacheKey {
	return networkCacheKey{network: g.currentNetworkIndexKey(), scale: g.mapScale, unit: g.layout.unit, viewport: g.layout.mapViewport}
}

func (g *Game) releaseNetworkBase() {
	if g.networkBase == nil {
		return
	}
	g.networkBase.Deallocate()
	g.networkBase = nil
	g.networkBaseLanes = nil
	g.networkBaseKey = networkCacheKey{}
	g.networkBaseOrigin = sim.Point{}
	g.networkBaseValid = false
}

// arrowSize is the size of a direction arrow in display units. Each of the
// two legs goes back from the tip by length and to one side by halfWidth.
type arrowSize struct {
	length, halfWidth float64
}

// arrowLineWidth is the width in display units of the two arrow legs.
const arrowLineWidth = 1.5

var (
	// routeArrowSize is the arrow size on the route of the selected pod.
	routeArrowSize = arrowSize{length: 7, halfWidth: 4}
	// laneArrowSize is the arrow size on the lanes of the network base
	// layer. The arrow stays on a lane that is 5 units wide.
	laneArrowSize = arrowSize{length: 4, halfWidth: 2}
)

type arrow struct {
	tip sim.Point
	// direction is a vector in the direction of the arrow. Its length does
	// not change the arrow.
	direction sim.Point
	size      arrowSize
	color     uint32
	antialias bool
	unit      float64
}

// legEnds returns the free ends of the two arrow legs. The other end of each
// leg is at the tip. ok is false if the arrow has no direction.
func (a arrow) legEnds() (ends [2]sim.Point, ok bool) {
	length := math.Hypot(a.direction.X, a.direction.Y)
	if length < 0.001 {
		return ends, false
	}
	ux, uy := a.direction.X/length, a.direction.Y/length
	back, side := a.size.length*a.unit, a.size.halfWidth*a.unit
	for i, sign := range []float64{-1, 1} {
		ends[i] = sim.Point{X: a.tip.X - ux*back + uy*side*sign, Y: a.tip.Y - uy*back - ux*side*sign}
	}
	return ends, true
}

func drawArrow(screen *ebiten.Image, a arrow) {
	ends, ok := a.legEnds()
	if !ok {
		return
	}
	for _, end := range ends {
		vector.StrokeLine(screen, float32(a.tip.X), float32(a.tip.Y), float32(end.X), float32(end.Y), float32(arrowLineWidth*a.unit), rgb(a.color), a.antialias)
	}
}

// spriteDraw returns the sprite of a at scale, turned to the direction of
// a. ok is false if a has no direction.
func (a arrow) spriteDraw(scale spriteScale) (d spriteDraw, ok bool) {
	if _, hasDirection := a.legEnds(); !hasDirection {
		return d, false
	}
	return spriteDraw{center: a.tip, key: arrowSprite(a), scale: scale, direction: a.direction}, true
}

func (g *Game) drawInspection(screen *ebiten.Image, state sim.Snapshot) {
	if _, ok := selectedVehicle(state, g.selected); !ok {
		return
	}
	podID := state.Vehicles[g.selected].Pod.ID
	podLabel := fleetPodLabel(g.selected)
	if podID != podLabel {
		podLabel += " / " + podID
	}
	heading := g.fitText("POD "+podLabel, 12, 140)
	g.label(screen, label{x: inspectionLeft, y: 115, size: 12, value: heading, color: muted})
	g.label(screen, label{x: 816, y: 143, size: 26, value: activityLabel(state.Vehicles[g.selected].Pod, g.podPurpose(state.Vehicles[g.selected], state)), color: g.podPurpose(state.Vehicles[g.selected], state).color()})
	status := "Available for passenger orders."
	station, _ := g.network.Station(state.Vehicles[g.selected].Pod.StationID)
	if station.ParkingOnly {
		status = "Available for pickup requests."
	}
	switch state.Vehicles[g.selected].Pod.Activity {
	case sim.Idle:
	// The station type determines the idle status above.
	case sim.DepartingEmpty:
		status = "Waits to depart without a passenger."
	case sim.Boarding:
		status = "Party enters the pod."
	case sim.Traveling:
		status = "Follows the highlighted route."
	case sim.Unloading:
		status = "Party leaves at its destination."
	}
	for _, request := range state.Pending {
		if request.PodID == state.Vehicles[g.selected].Pod.ID && state.Vehicles[g.selected].Pod.Activity == sim.Idle {
			status = "Waiting for destination access."
		}
	}
	if state.Vehicles[g.selected].Pod.WaitReason != sim.NoWait {
		status = waitStatus(state.Vehicles[g.selected].Pod, state.Vehicles)
	}
	if state.Paused {
		status = "Paused. Resume to advance."
	}
	status = g.fitText(status, 13, inspectionRight-inspectionLeft)
	g.label(screen, label{x: inspectionLeft, y: 183, size: 13, value: status, color: muted})
	journey := "No active journey"
	if state.Vehicles[g.selected].Request != nil {
		from, _ := g.network.Station(state.Vehicles[g.selected].Request.From)
		to, _ := g.network.Station(state.Vehicles[g.selected].Request.To)
		journey = from.Name + " > " + to.Name
	}
	if station.ParkingOnly {
		journey = "Parked at " + station.Name
	}
	if to := state.Vehicles[g.selected].RelocatingTo; to != "" {
		station, _ := g.network.Station(to)
		journey = "Empty > " + station.Name
	}
	journey = g.fitText(journey, 17, inspectionRight-inspectionLeft)
	g.label(screen, label{x: inspectionLeft, y: 224, size: 17, value: journey, color: foreground})
	for i, row := range g.inspectionRows(state.Vehicles[g.selected]) {
		y := inspectionRowsTop + float64(i)*inspectionRowSpacing
		if row.name != "" {
			g.label(screen, label{x: inspectionLeft, y: y, size: 14, value: row.name, color: muted})
		}
		g.label(screen, label{x: row.valueLeft(), y: y, size: 14, value: g.fitInspectionValue(row), color: foreground})
	}
}

// waitStatus returns the inspector status of a pod that waits for a local
// resource. The status gives the wait reason and the pod that holds the
// resource. It names that pod by its fleet number, which is the label of its
// pod button. A pod ID that is not in vehicles does not change. Without a
// blocking pod, the status is the wait reason only.
func waitStatus(pod sim.Pod, vehicles []sim.Vehicle) string {
	status := string(pod.WaitReason)
	if pod.BlockedBy == "" {
		return status
	}
	blocker := pod.BlockedBy
	if i := slices.IndexFunc(vehicles, func(v sim.Vehicle) bool { return v.Pod.ID == pod.BlockedBy }); i >= 0 {
		blocker = fleetPodLabel(i)
	}
	return status + " / pod " + blocker
}

// inspectionRow is a row in the pod inspector. A row with a name shows the
// name and the value in two columns. A row without a name shows the value
// across the full width of the inspector.
type inspectionRow struct{ name, value string }

// valueLeft returns the left edge of the value of row in the pod inspector.
func (row inspectionRow) valueLeft() float64 {
	if row.name == "" {
		return inspectionLeft
	}
	return inspectionValueLeft
}

// fitInspectionValue returns the value of row. It cuts the value when the
// value does not fit between its left edge and the right edge of the
// inspector.
func (g *Game) fitInspectionValue(row inspectionRow) string {
	return g.fitText(row.value, 14, inspectionRight-row.valueLeft())
}

// inspectionRows returns the rows of the pod inspector for vehicle. The
// rows show only values of the pod. The run status in the header shows the
// simulated time and the completed journeys. The station phase rows are
// last, because the station name row shows only during a station maneuver.
// So the other rows do not move when the pod enters or leaves a station.
func (g *Game) inspectionRows(vehicle sim.Vehicle) []inspectionRow {
	passengers := "Empty"
	if vehicle.Pod.Occupied && vehicle.Request != nil {
		passengers = passengerCount(vehicle.Request.PartySize)
	}
	rows := []inspectionRow{
		{"Speed", fmt.Sprintf("%.0f km/h", vehicle.Pod.Speed*3.6)},
		{"On board", passengers},
	}
	return append(rows, stationPhaseRows(vehicle.Pod, g.network)...)
}

// passengerCount returns the On board value for count passengers, such as
// "1 passenger". Each party has one passenger, so a party count would show
// the same number.
func passengerCount(count int) string {
	if count == 1 {
		return "1 passenger"
	}
	return fmt.Sprintf("%d passengers", count)
}

// stationPhaseRows returns the Station phase row of the pod inspector for
// pod. During a station maneuver at a known station, a second row shows the
// station name across the full width of the inspector, so that long names
// such as "Edgware Road (Circle Line)" show in full.
func stationPhaseRows(pod sim.Pod, network sim.Network) []inspectionRow {
	if pod.StationPhase == "" {
		return []inspectionRow{{"Station phase", "Main network"}}
	}
	rows := []inspectionRow{{"Station phase", string(pod.StationPhase)}}
	if station, ok := network.Station(pod.ManeuverStationID); ok {
		rows = append(rows, inspectionRow{value: station.Name})
	}
	return rows
}

func fleetPodLabel(index int) string {
	return fmt.Sprintf("%02d", index+1)
}

func (g *Game) drawControls(screen *ebiten.Image, state sim.Snapshot) {
	title, hint := journeyText(state.Demo)
	g.label(screen, label{x: 44, y: 607, size: 13, value: title, color: foreground})
	if from, fromOK := g.network.Station(g.origin); fromOK {
		if to, toOK := g.network.Station(g.destination); toOK {
			value := fitText(from.Name+" > "+to.Name, textFit{face: g.textFace(11), width: 560 * g.layout.unit})
			g.label(screen, label{x: 230, y: 609, size: 11, value: value, color: muted})
		}
	}
	g.label(screen, label{x: 44, y: 638, size: 11, value: "FROM", color: muted})
	g.label(screen, label{x: 44, y: 674, size: 11, value: "TO", color: muted})
	if pages := g.stationPages(); len(pages) > 1 {
		g.label(screen, label{x: 850, y: 609, size: 10, value: fmt.Sprintf("%d / %d", g.stationPage+1, len(pages)), color: muted})
	}
	for _, value := range fleetStatLabels(state) {
		g.label(screen, value)
	}
	if pager, ok := g.podPagerLabel(); ok {
		g.label(screen, pager)
	}
	g.label(screen, g.hintLine(state, hint))
}

// journeyText returns the title and the hint of the journey controls. The
// title uses the verb of the Order button. While the traffic demo runs, they
// describe the demo.
func journeyText(demo bool) (title, hint string) {
	if demo {
		return "TRAFFIC DEMO", "Four pods, eight journeys: Market arrivals, pickups from parking, and follow-up orders. Reset stops the demo."
	}
	return "ORDER A JOURNEY", "Choose From and To. An available pod collects the passenger. Orders wait when all pods are busy."
}

// fleetStatLabels returns the Pickup wait and Fleet use lines for state.
// They are the last lines of the right panel, below Orders and Demand.
func fleetStatLabels(state sim.Snapshot) []label {
	use := summarizeFleet(state)
	return []label{
		{x: 816, y: 539, size: 10, value: fmt.Sprintf("Pickup wait: avg %.0f s / max %.0f s", state.Wait.AverageSeconds, state.Wait.MaxSeconds), color: muted},
		{x: 816, y: 553, size: 10, value: fmt.Sprintf("Fleet use: %d%% active / %d%% passenger", use.activePercent(), use.passengerPercent()), color: muted},
	}
}

// sameStationHint tells the user why Order is disabled when From and To are
// the same station.
const sameStationHint = "Choose a different destination."

// hintLine returns the line below the journey controls. It shows the first
// text that is set, in this order: the reset confirmation, the message, the
// demo error, the notice, sameStationHint, and hint. A second Reset press
// resets the session while the confirmation is set, so the confirmation
// shows in place of all other text. The server update message of the
// desktop client and a demo error can stay for a long time. They must not
// hide the confirmation. sameStationHint shows while From and To are the
// same station, but not in the traffic demo. It stays until the user
// changes the selection, so a notice shows before it.
func (g *Game) hintLine(state sim.Snapshot, hint string) label {
	value, shade := hint, uint32(muted)
	switch {
	case g.noticeAction == resetConfirmAction:
		value, shade = g.notice, accent
	case g.message != "":
		value, shade = g.message, amber
	case state.DemoError != "":
		value, shade = state.DemoError, amber
	case g.notice != "":
		value, shade = g.notice, accent
	case !state.Demo && g.origin != "" && g.origin == g.destination:
		value, shade = sameStationHint, amber
	}
	return label{x: 44, y: 701, size: 13, value: value, color: shade}
}

func (g *Game) drawButton(screen *ebiten.Image, b button) {
	fill, ink := uint32(track), uint32(foreground)
	selectedFill := uint32(accent)
	fontSize := 14.0
	if b.fontSize > 0 {
		fontSize = b.fontSize
	}
	if id, ok := strings.CutPrefix(b.action, "pod/"); ok {
		fill, ink = podButtonFill, g.podButtonColor(id)
		selectedFill = ink
		fontSize = 12
		b.label = shortText(b.label, 3)
	}
	if b.selected {
		fill, ink = selectedFill, background
	}
	if b.disabled {
		fill, ink = 0x1b2a36, 0x63788a
	}
	vector.FillRect(screen, float32(b.x), float32(b.y), float32(b.w), float32(b.h), rgb(fill), false)
	face := g.textFace(fontSize)
	b.label = g.fitButtonText(b.label, fontSize, b.w)
	textWidth, textHeight := text.Measure(b.label, face, 0)
	g.label(screen, label{x: b.x + (b.w-textWidth)/2, y: b.y + (b.h-textHeight)/2, size: fontSize, value: b.label, color: ink, physical: true})
}

func (g *Game) fitButtonText(value string, size, width float64) string {
	return fitText(value, textFit{face: g.textFace(size), width: max(1, width-12*g.layout.unit)})
}

func (g *Game) fitText(value string, size, width float64) string {
	return fitText(value, textFit{face: g.textFace(size), width: width * g.layout.unit})
}

func (g *Game) textFace(size float64) *text.GoTextFace {
	return &text.GoTextFace{Source: g.font, Size: size * g.layout.unit}
}

type label struct {
	x, y, size float64
	value      string
	color      uint32
	physical   bool
	// mapLabel is true for a map label, such as a station name, a station
	// count or a pod label. Its size is in CSS pixels. See
	// displayLayout.mapLabelSize.
	mapLabel bool
}

// labelFace returns the font face of a label. The size of a map label does
// not follow the display unit. See displayLayout.mapLabelSize.
func (g *Game) labelFace(value label) *text.GoTextFace {
	if value.mapLabel {
		return &text.GoTextFace{Source: g.font, Size: g.layout.mapLabelSize(value.size)}
	}
	return g.textFace(value.size)
}

func (g *Game) label(screen *ebiten.Image, label label) {
	if !label.physical && screen.Bounds() == image.Rect(0, 0, g.layout.width, g.layout.height) {
		label.x, label.y = g.layout.labelPosition(label.x, label.y)
	}
	options := &text.DrawOptions{}
	options.GeoM.Translate(label.x, label.y)
	options.ColorScale.ScaleWithColor(rgb(label.color))
	text.Draw(screen, label.value, g.labelFace(label), options)
}

// centerLabel returns value as a physical label in the center of area.
func (g *Game) centerLabel(area image.Rectangle, value label) label {
	width, height := text.Measure(value.value, g.labelFace(value), 0)
	value.x = float64(area.Min.X) + (float64(area.Dx())-width)/2
	value.y = float64(area.Min.Y) + (float64(area.Dy())-height)/2
	value.physical = true
	return value
}

func rgb(hex uint32) color.RGBA {
	return color.RGBA{R: uint8((hex >> 16) & 255), G: uint8((hex >> 8) & 255), B: uint8(hex & 255), A: 255}
}

func activityLabel(pod sim.Pod, purpose podPurpose) string {
	if pod.WaitReason != sim.NoWait && pod.Speed < 0.01 {
		return "Waiting"
	}
	if pod.Activity == sim.Traveling || pod.Activity == sim.DepartingEmpty || pod.Activity == sim.Idle {
		return purpose.label()
	}
	return string(pod.Activity)
}

func (g *Game) mapSnapshot() sim.Snapshot {
	state := g.motion.Sample(time.Now())
	if len(state.Vehicles) == 0 {
		return g.state.Simulation
	}
	return state
}

func (g *Game) passengerStations() []sim.Station {
	var stations []sim.Station
	for _, s := range g.network.Stations {
		if !s.ParkingOnly {
			stations = append(stations, s)
		}
	}
	return stations
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
	bounds, ok := networkBounds(g.network)
	if !ok {
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
	g.stationPage = min(g.stationPage, len(g.stationPages())-1)
	g.podPage = min(g.podPage, podPageCount(len(g.state.Simulation.Vehicles))-1)
}

func shortText(value string, limit int) string {
	letters := []rune(value)
	if len(letters) <= limit {
		return value
	}
	return strings.TrimRightFunc(string(letters[:limit-1]), unicode.IsSpace) + "…"
}
