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
	"strconv"
	"strings"
	"time"

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

	inspectionLeft       = 816.0
	inspectionRight      = 1054.0
	inspectionValueLeft  = 924.0
	inspectionRowsTop    = 258.0
	inspectionRowSpacing = 27.0
	podSelectorTop       = 393.0
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
	cameraKey            cameraFitKey
	networkBase          *ebiten.Image
	networkBaseKey       networkCacheKey
	networkBaseValid     bool
	showOrders           bool
	notice               string
	noticeAction         string
	noticeTicks          int
	// rewindProjectRevision is the project revision at the last rewind click.
	// A larger revision in the reply shows that the rewind restored a project.
	rewindProjectRevision uint64
	// savedEpoch and savedRevision come from the last accepted save point.
	// The command reply can arrive before the state that lists the save point,
	// so Rewind waits for that state and does not target an older save point.
	savedEpoch    string
	savedRevision uint64
	layout        displayLayout
}

// New creates the first playable scenario.
func New(ctx context.Context, serverURL string) (*Game, error) {
	network := sim.Example()
	simulation, err := sim.NewFleet(network, []sim.Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		return nil, fmt.Errorf("create simulation: %w", err)
	}
	font, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		return nil, fmt.Errorf("load font: %w", err)
	}
	return &Game{network: network, client: remote.New(ctx, serverURL), state: session.State{Simulation: simulation.Snapshot(), Speed: 1}, font: font, origin: "harbor", destination: "market", layout: newDisplayLayout(layoutInput{outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 1})}, nil
}

// Update reads shared state and handles local input.
func (g *Game) Update() error {
	g.readRemote()
	g.fitNetwork()
	if g.noticeTicks > 0 {
		g.noticeTicks--
		if g.noticeTicks == 0 {
			g.notice, g.noticeAction = "", ""
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		g.showOrders, g.showDemand = false, false
		g.selected = (g.selected + 1) % len(g.state.Simulation.Vehicles)
		g.podPage = g.selected / 6
		g.message = ""
	}
	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		g.pause()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
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
	return nil
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
			return g.click(point)
		}
	}
	return false
}

func (g *Game) pause() {
	g.submit(session.Command{Action: "pause", Paused: !g.state.Simulation.Paused})
}
func (g *Game) reset() { g.submit(session.Command{Action: "reset"}) }
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
	g.rewindProjectRevision = g.state.ProjectRevision
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
	requestLabel := "Order"
	if g.noticeAction == "trip" && g.noticeTicks > 150 {
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
		{x: 810, y: 529, w: 120, h: 26, label: fmt.Sprintf("Orders %d", outstandingOrderCount(state)), selected: g.showOrders, action: "orders"},
		{x: 940, y: 529, w: 120, h: 26, label: "Demand", selected: g.showDemand, action: "demand"},
		{x: 930, y: 644, w: 130, h: 42, label: requestLabel, selected: true, disabled: busy || g.destination == g.origin, action: "request"},
		{x: 810, y: 440, w: 250, h: 36, label: pauseLabel, action: "pause"},
		{x: 810, y: 486, w: 119, h: 36, label: fmt.Sprintf("Speed %dx [S]", g.state.Speed), action: "speed"},
		{x: 941, y: 486, w: 119, h: 36, label: "Reset [R]", action: "reset"},
	}
	for i, v := range state.Vehicles {
		if i/6 != g.podPage {
			continue
		}
		buttons = append(buttons, button{x: 810 + float64((i%6)*36), y: podSelectorTop, w: 32, h: 34, label: fleetPodLabel(i), selected: !g.showOrders && !g.showDemand && g.selected == i, action: "pod/" + v.Pod.ID})
	}
	if len(state.Vehicles) > 6 {
		buttons = append(buttons, button{x: 1026, y: podSelectorTop, w: 34, h: 34, label: ">", action: "pods-next"})
	}
	buttons = append(buttons, g.journeyButtons(busy)...)
	if g.showDemand {
		buttons = append(buttons, g.demandButtons()...)
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
	if b.y >= 529 {
		y += g.layout.extraY
	}
	b.x, b.y, b.w, b.h = x, y, b.w*g.layout.unit, b.h*g.layout.unit
	return b
}

// click reports a reset or a rewind so its new state can render before the
// next tick.
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
		case "pods-next":
			g.podPage = (g.podPage + 1) % ((len(g.state.Simulation.Vehicles) + 5) / 6)
		case "stations-prev":
			g.stationPage--
		case "stations-next":
			g.stationPage++
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
	if !g.camera.contains(point) {
		return false
	}
	for i, v := range g.mapSnapshot().Vehicles {
		if g.podHiddenInCluster(v, i) {
			continue
		}
		p := g.mapPoint(v.Pod.Position)
		if math.Hypot(point.X-p.X, point.Y-p.Y) < 18*g.layout.unit {
			g.selected, g.message = i, ""
			g.showOrders, g.showDemand = false, false
			break
		}
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
	g.label(screen, g.connectionFooter())
}

// headerLabels returns the title and the header text above the panels.
func (g *Game) headerLabels() []label {
	return []label{
		{x: 28, y: 22, size: 30, value: "podsim", color: foreground},
		{x: 157, y: 34, size: 14, value: "NETWORK PLAYGROUND / LOCAL TRAFFIC", color: muted},
		{x: 815, y: 34, size: 14, value: fmt.Sprintf("%d STOPS     %d PODS", len(g.passengerStations()), len(g.state.Simulation.Vehicles)), color: accent},
	}
}

// connectionFooter returns the connection status line below the panels.
func (g *Game) connectionFooter() label {
	return label{x: 28, y: 740, size: 11, value: g.connectionLabel(), color: muted}
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

func (g *Game) syncCamera() {
	g.mapScale, g.mapOrigin = g.camera.scale, g.camera.origin
	g.networkBaseValid = false
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

func (g *Game) drawNetwork(screen *ebiten.Image, state sim.Snapshot) {
	g.label(screen, label{x: 44, y: 113, size: 12, value: "NETWORK", color: muted})
	g.label(screen, label{x: 410, y: 113, size: 11, value: "SCROLL ZOOM / DRAG PAN", color: muted})
	mapScreen, ok := screen.SubImage(g.layout.mapViewport).(*ebiten.Image)
	if !ok {
		return
	}
	detailed := len(g.network.Lanes) <= detailedLanes
	if detailed {
		g.releaseNetworkBase()
		g.drawBaseNetwork(mapScreen, true)
	} else {
		g.drawCachedNetworkBase(mapScreen)
	}
	selected := state.Vehicles[g.selected]
	if selected.Pod.Activity != sim.Idle {
		shade := g.podPurpose(selected, state).color()
		for _, lane := range selected.Route {
			geometry := g.laneGeometry(lane, detailed)
			geometry.draw(mapScreen, laneStroke{width: float32(2 * g.layout.unit), color: shade, antialias: detailed})
			drawArrow(mapScreen, arrow{from: geometry.arrowFrom, to: geometry.arrowTo, color: shade, antialias: detailed, unit: g.layout.unit})
		}
	}
	collapsedStations := make(map[string]bool)
	stationMonitor := observe.NewStationMonitor(g.network)
	var collapsedLabels []collapsedStationLabel
	for _, station := range g.network.Stations {
		status := stationMonitor.Summarize(station, state)
		center := sim.Point{}
		showBerths := g.showStationBerths(station)
		collapsedStations[station.ID] = !showBerths
		for j, berth := range station.Berths {
			node, _ := g.network.Node(berth.Node)
			p := g.mapPoint(node.Position)
			center.X += p.X
			center.Y += p.Y
			shade := uint32(muted)
			for _, v := range state.Vehicles {
				if v.Pod.BerthID == berth.ID {
					shade = g.podPurpose(v, state).color()
				}
			}
			if showBerths {
				vector.StrokeCircle(mapScreen, float32(p.X), float32(p.Y), float32(13*g.layout.unit), float32(2*g.layout.unit), rgb(shade), detailed)
			}
			name := station.Name
			labelX, labelY := p.X-26*g.layout.unit, p.Y+20*g.layout.unit
			if station.ParkingOnly || len(station.Berths) > 1 {
				name = strconv.Itoa(j + 1)
				labelX, labelY = p.X-26*g.layout.unit, p.Y-9*g.layout.unit
			}
			if showBerths {
				g.label(mapScreen, label{x: labelX, y: labelY, size: 16, value: name, color: foreground})
			}
			occupancy := "BERTH 0/1"
			for _, b := range state.Berths {
				if b.ID == berth.ID {
					if b.Occupant != "" {
						occupancy = "BERTH 1/1"
					}
					if b.ReservedBy != "" && b.Occupant == "" {
						occupancy = "ARRIVING " + b.ReservedBy
						for _, v := range state.Vehicles {
							if v.Pod.ID == b.ReservedBy && len(v.Route) > 0 && v.Route[0].From == berth.Node {
								occupancy = "DEPARTING " + b.ReservedBy
							}
						}
					}
				}
			}
			if showBerths && !station.ParkingOnly && len(station.Berths) == 1 {
				g.label(mapScreen, label{x: labelX, y: labelY + 21*g.layout.unit, size: 10, value: occupancy, color: shade})
				g.label(mapScreen, label{x: labelX, y: labelY + 37*g.layout.unit, size: 9, value: fmt.Sprintf("%d occupied · %d reserved empty · %d free", status.Occupied, status.ReservedEmpty, status.Free), color: muted})
				g.label(mapScreen, label{x: labelX, y: labelY + 52*g.layout.unit, size: 9, value: fmt.Sprintf("In %d stopped / %d approaching · Out %d stopped", status.EntranceStopped, status.Approaching, status.ExitStopped), color: muted})
			}
		}
		if len(station.Berths) == 0 {
			continue
		}
		center.X /= float64(len(station.Berths))
		center.Y /= float64(len(station.Berths))
		if !showBerths {
			vector.FillCircle(mapScreen, float32(center.X), float32(center.Y), float32(10*g.layout.unit), rgb(track), detailed)
			vector.StrokeCircle(mapScreen, float32(center.X), float32(center.Y), float32(10*g.layout.unit), float32(2*g.layout.unit), rgb(muted), detailed)
			x := center.X + 16*g.layout.unit
			y := center.Y - 14*g.layout.unit
			shortName := shortText(strings.TrimPrefix(station.Name, "Station "), 7)
			collapsed := collapsedStationLabel{
				stationID:      station.ID,
				primary:        label{x: x, y: y, size: 10, value: fmt.Sprintf("%s  %d/%d", shortName, status.Occupied, len(station.Berths)), color: foreground},
				collisionValue: fmt.Sprintf("%s  %d/%d", shortName, len(station.Berths), len(station.Berths)),
			}
			if status.EntranceStopped > 0 || status.ExitStopped > 0 {
				collapsed.secondary = fmt.Sprintf("In %d · Out %d", status.EntranceStopped, status.ExitStopped)
			}
			collapsedLabels = append(collapsedLabels, collapsed)
		} else if station.ParkingOnly || len(station.Berths) > 1 {
			x := center.X + 25*g.layout.unit
			y := center.Y - 32*g.layout.unit
			g.label(mapScreen, label{x: x, y: y, size: 16, value: station.Name, color: foreground})
			g.label(mapScreen, label{x: x, y: y + 23*g.layout.unit, size: 10, value: fmt.Sprintf("%d/%d occupied · %d reserved empty · %d free", status.Occupied, len(station.Berths), status.ReservedEmpty, status.Free), color: muted})
			g.label(mapScreen, label{x: x, y: y + 40*g.layout.unit, size: 9, value: fmt.Sprintf("In %d stopped / %d approaching · Out %d stopped", status.EntranceStopped, status.Approaching, status.ExitStopped), color: muted})
		}
	}
	g.drawCollapsedStationLabels(mapScreen, collapsedLabels, selected)
	for i, v := range state.Vehicles {
		parkedInCluster := v.Pod.Activity == sim.Idle && collapsedStations[v.Pod.StationID]
		if parkedInCluster && i != g.selected {
			continue
		}
		p := g.mapPoint(v.Pod.Position)
		shade := g.podPurpose(v, state).color()
		if v.Pod.WaitReason != sim.NoWait {
			vector.StrokeCircle(mapScreen, float32(p.X), float32(p.Y), float32(11*g.layout.unit), float32(2*g.layout.unit), rgb(amber), detailed)
		}
		if i == g.selected {
			vector.StrokeCircle(mapScreen, float32(p.X), float32(p.Y), float32(9*g.layout.unit), float32(1.5*g.layout.unit), rgb(foreground), detailed)
		}
		vector.FillCircle(mapScreen, float32(p.X), float32(p.Y), float32(4*g.layout.unit), rgb(shade), detailed)
		if (!parkedInCluster || i == g.selected) && g.showPodMapLabel(i) {
			g.label(mapScreen, label{x: p.X + 11*g.layout.unit, y: p.Y - 18*g.layout.unit, size: 11, value: fleetPodLabel(i), color: shade})
		}
	}
	vector.StrokeLine(screen, float32(g.layout.x(48)), float32(g.layout.bottom(540)), float32(g.layout.x(125)), float32(g.layout.bottom(540)), float32(2*g.layout.unit), rgb(muted), detailed)
	g.label(screen, label{x: 141, y: 531, size: 12, value: fmt.Sprintf("%.0f m", 77*g.layout.unit/g.mapScale), color: muted})
	g.drawPodLegend(screen)
}

func (g *Game) showStationBerths(station sim.Station) bool {
	if len(station.Berths) < 2 {
		return true
	}
	minimum := math.Inf(1)
	for i, berth := range station.Berths {
		node, ok := g.network.Node(berth.Node)
		if !ok {
			continue
		}
		for _, other := range station.Berths[i+1:] {
			otherNode, found := g.network.Node(other.Node)
			if found {
				minimum = min(minimum, math.Hypot(node.Position.X-otherNode.Position.X, node.Position.Y-otherNode.Position.Y)*g.mapScale)
			}
		}
	}
	return minimum >= 34*g.layout.unit
}

type collapsedStationLabel struct {
	stationID      string
	primary        label
	collisionValue string
	secondary      string
}

type boundedStationLabel struct {
	stationID string
	bounds    image.Rectangle
}

func (g *Game) drawCollapsedStationLabels(screen *ebiten.Image, labels []collapsedStationLabel, selected sim.Vehicle) {
	preferred := map[string]bool{g.origin: true, g.destination: true, selected.Pod.StationID: true, selected.RelocatingTo: true}
	if selected.Request != nil {
		preferred[selected.Request.From] = true
		preferred[selected.Request.To] = true
	}
	delete(preferred, "")
	bounded := make([]boundedStationLabel, len(labels))
	for index, candidate := range labels {
		bounded[index] = boundedStationLabel{stationID: candidate.stationID, bounds: g.collapsedStationLabelBounds(candidate)}
	}
	visible := selectCollapsedStationLabels(bounded, preferred, len(g.network.Stations) > 30)
	for index, candidate := range labels {
		if !visible[index] {
			continue
		}
		g.label(screen, candidate.primary)
		if candidate.secondary != "" {
			g.label(screen, label{
				x: candidate.primary.x, y: candidate.primary.y + 16*g.layout.unit,
				size: 9, value: candidate.secondary, color: amber,
			})
		}
	}
}

func (g *Game) collapsedStationLabelBounds(candidate collapsedStationLabel) image.Rectangle {
	width, _ := text.Measure(candidate.collisionValue, g.textFace(candidate.primary.size), 0)
	secondaryWidth, _ := text.Measure("In 000 · Out 000", g.textFace(9), 0)
	width = max(width, secondaryWidth)
	padding := 3 * g.layout.unit
	return image.Rect(
		int(math.Floor(candidate.primary.x-padding)),
		int(math.Floor(candidate.primary.y-padding)),
		int(math.Ceil(candidate.primary.x+width+padding)),
		int(math.Ceil(candidate.primary.y+32*g.layout.unit+padding)),
	)
}

func selectCollapsedStationLabels(labels []boundedStationLabel, preferred map[string]bool, dense bool) []bool {
	visible := make([]bool, len(labels))
	if !dense {
		for index := range visible {
			visible[index] = true
		}
		return visible
	}
	var occupied []image.Rectangle
	for _, priority := range []bool{true, false} {
		for index, candidate := range labels {
			if preferred[candidate.stationID] != priority {
				continue
			}
			blocked := slices.ContainsFunc(occupied, candidate.bounds.Overlaps)
			if blocked && !priority {
				continue
			}
			visible[index] = true
			occupied = append(occupied, candidate.bounds)
		}
	}
	return visible
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

type networkCacheKey struct {
	epoch      string
	generation uint64
	scale      float64
	origin     sim.Point
	viewport   image.Rectangle
}

type laneGeometry struct {
	points             [33]sim.Point
	count              int
	arrowFrom, arrowTo sim.Point
}

type laneStroke struct {
	width     float32
	color     uint32
	antialias bool
}

func (geometry laneGeometry) draw(screen *ebiten.Image, stroke laneStroke) {
	for i := 1; i < geometry.count; i++ {
		from, to := geometry.points[i-1], geometry.points[i]
		vector.StrokeLine(screen, float32(from.X), float32(from.Y), float32(to.X), float32(to.Y), stroke.width, rgb(stroke.color), stroke.antialias)
	}
}

func (g *Game) laneGeometry(lane sim.Lane, detailed bool) laneGeometry {
	fromNode, _ := g.network.Node(lane.From)
	toNode, _ := g.network.Node(lane.To)
	position := func(t float64) sim.Point {
		if lane.Control == nil {
			return sim.Point{X: fromNode.Position.X + (toNode.Position.X-fromNode.Position.X)*t, Y: fromNode.Position.Y + (toNode.Position.Y-fromNode.Position.Y)*t}
		}
		u := 1 - t
		return sim.Point{X: u*u*fromNode.Position.X + 2*u*t*lane.Control.X + t*t*toNode.Position.X, Y: u*u*fromNode.Position.Y + 2*u*t*lane.Control.Y + t*t*toNode.Position.Y}
	}
	segments := 1
	if lane.Control != nil {
		segments = 32
		if !detailed {
			extent := math.Hypot(lane.Control.X-fromNode.Position.X, lane.Control.Y-fromNode.Position.Y) + math.Hypot(toNode.Position.X-lane.Control.X, toNode.Position.Y-lane.Control.Y)
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
	geometry.arrowFrom, geometry.arrowTo = g.mapPoint(position(.55)), g.mapPoint(position(.65))
	if detailed {
		geometry.arrowFrom = g.mapPoint(g.network.Position(lane, length*.55))
		geometry.arrowTo = g.mapPoint(g.network.Position(lane, length*.65))
	}
	return geometry
}

func (g *Game) drawBaseNetwork(screen *ebiten.Image, detailed bool) {
	for _, lane := range g.network.Lanes {
		geometry := g.laneGeometry(lane, detailed)
		geometry.draw(screen, laneStroke{width: float32(5 * g.layout.unit), color: track, antialias: detailed})
		drawArrow(screen, arrow{from: geometry.arrowFrom, to: geometry.arrowTo, color: track, antialias: detailed, unit: g.layout.unit})
	}
	for _, node := range g.network.Nodes {
		point := g.mapPoint(node.Position)
		vector.FillCircle(screen, float32(point.X), float32(point.Y), float32(3*g.layout.unit), rgb(muted), detailed)
	}
}

func (g *Game) drawCachedNetworkBase(screen *ebiten.Image) {
	key := g.currentNetworkCacheKey()
	if g.networkBase == nil || g.networkBase.Bounds().Dx() != g.layout.width || g.networkBase.Bounds().Dy() != g.layout.height {
		g.releaseNetworkBase()
		g.networkBase = ebiten.NewImage(g.layout.width, g.layout.height)
	}
	if g.networkBaseNeedsRefresh() {
		g.networkBase.Clear()
		g.drawBaseNetwork(g.networkBase, false)
		g.networkBaseKey = key
		g.networkBaseValid = true
	}
	screen.DrawImage(g.networkBase, nil)
}

func (g *Game) networkBaseNeedsRefresh() bool {
	key := g.currentNetworkCacheKey()
	return !g.networkBaseValid || g.networkBaseKey != key
}

func (g *Game) currentNetworkCacheKey() networkCacheKey {
	return networkCacheKey{epoch: g.state.Epoch, generation: g.state.Generation, scale: g.mapScale, origin: g.mapOrigin, viewport: g.layout.mapViewport}
}

func (g *Game) releaseNetworkBase() {
	if g.networkBase == nil {
		return
	}
	g.networkBase.Deallocate()
	g.networkBase = nil
	g.networkBaseKey = networkCacheKey{}
	g.networkBaseValid = false
}

type arrow struct {
	from, to  sim.Point
	color     uint32
	antialias bool
	unit      float64
}

func drawArrow(screen *ebiten.Image, a arrow) {
	dx, dy := a.to.X-a.from.X, a.to.Y-a.from.Y
	length := math.Hypot(dx, dy)
	if length < 0.001 {
		return
	}
	ux, uy := dx/length, dy/length
	x, y := a.from.X+dx*.6, a.from.Y+dy*.6
	for _, side := range []float64{-1, 1} {
		vector.StrokeLine(screen, float32(x), float32(y), float32(x-ux*7*a.unit+uy*4*a.unit*side), float32(y-uy*7*a.unit-ux*4*a.unit*side), float32(1.5*a.unit), rgb(a.color), a.antialias)
	}
}

func (g *Game) drawInspection(screen *ebiten.Image, state sim.Snapshot) {
	podID := state.Vehicles[g.selected].Pod.ID
	podLabel := fleetPodLabel(g.selected)
	if podID != podLabel {
		podLabel += " / " + podID
	}
	heading := g.fitText("POD "+podLabel, 12, 140)
	g.label(screen, label{x: inspectionLeft, y: 115, size: 12, value: heading, color: muted})
	g.label(screen, label{x: 816, y: 143, size: 26, value: activityLabel(state.Vehicles[g.selected].Pod, g.podPurpose(state.Vehicles[g.selected], state)), color: g.podPurpose(state.Vehicles[g.selected], state).color()})
	status := "Available for passenger requests."
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
		status = string(state.Vehicles[g.selected].Pod.WaitReason) + " / pod " + state.Vehicles[g.selected].Pod.BlockedBy
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
	passengers := "Empty"
	selected := state.Vehicles[g.selected]
	if selected.Pod.Occupied && selected.Request != nil {
		parties := max(1, selected.Parties)
		passengers = fmt.Sprintf("%d %s / %d %s", parties, countNoun(parties, "party", "parties"),
			selected.Request.PartySize, countNoun(selected.Request.PartySize, "passenger", "passengers"))
	}
	rows := []struct{ name, value string }{
		{"Speed", fmt.Sprintf("%.0f km/h", state.Vehicles[g.selected].Pod.Speed*3.6)},
		{"Station phase", stationPhaseLabel(state.Vehicles[g.selected].Pod, g.network)},
		{"On board", passengers},
		{"Completed", fmt.Sprintf("%d journeys", state.Completed)},
		{"Sim time", fmt.Sprintf("%.1f s", float64(state.Tick)/sim.TicksPerSecond)},
	}
	for i, row := range rows {
		y := inspectionRowsTop + float64(i)*inspectionRowSpacing
		value := g.fitText(row.value, 14, inspectionRight-inspectionValueLeft)
		g.label(screen, label{x: inspectionLeft, y: y, size: 14, value: row.name, color: muted})
		g.label(screen, label{x: inspectionValueLeft, y: y, size: 14, value: value, color: foreground})
	}
}

func stationPhaseLabel(pod sim.Pod, network sim.Network) string {
	if pod.StationPhase == "" {
		return "Main network"
	}
	value := string(pod.StationPhase)
	if station, ok := network.Station(pod.ManeuverStationID); ok {
		value += " / " + station.Name
	}
	return value
}

func countNoun(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func fleetPodLabel(index int) string {
	return fmt.Sprintf("%02d", index+1)
}

func (g *Game) drawControls(screen *ebiten.Image, state sim.Snapshot) {
	title := "REQUEST A JOURNEY"
	hint := "Choose pickup and destination. An available pod collects the passenger; requests wait when all pods are busy."
	if state.Demo {
		title = "TRAFFIC DEMO"
		hint = "Four pods, eight journeys: Market arrivals, pickups from parking, and follow-up orders."
	}
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
	g.label(screen, label{x: 816, y: 557, size: 10, value: fmt.Sprintf("Pickup wait: avg %.0f s / max %.0f s", state.Wait.AverageSeconds, state.Wait.MaxSeconds), color: muted})
	use := summarizeFleet(state)
	g.label(screen, label{x: 816, y: 576, size: 10, value: fmt.Sprintf("Fleet use: %d%% active / %d%% passenger", use.activePercent(), use.passengerPercent()), color: muted})
	shade := uint32(muted)
	if g.notice != "" {
		hint, shade = g.notice, accent
	}
	if state.DemoError != "" {
		hint, shade = state.DemoError, amber
	}
	if g.message != "" {
		hint, shade = g.message, amber
	}
	g.label(screen, label{x: 44, y: 701, size: 13, value: hint, color: shade})
}

func (g *Game) drawButton(screen *ebiten.Image, b button) {
	fill, ink := uint32(track), uint32(foreground)
	selectedFill := uint32(accent)
	fontSize := 14.0
	if b.fontSize > 0 {
		fontSize = b.fontSize
	}
	if id, ok := strings.CutPrefix(b.action, "pod/"); ok {
		ink = g.podButtonColor(id)
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
}

func (g *Game) label(screen *ebiten.Image, label label) {
	if !label.physical && screen.Bounds() == image.Rect(0, 0, g.layout.width, g.layout.height) {
		label.x, label.y = g.layout.labelPosition(label.x, label.y)
	}
	options := &text.DrawOptions{}
	options.GeoM.Translate(label.x, label.y)
	options.ColorScale.ScaleWithColor(rgb(label.color))
	text.Draw(screen, label.value, &text.GoTextFace{Source: g.font, Size: label.size * g.layout.unit}, options)
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
	g.podPage = min(g.podPage, (len(g.state.Simulation.Vehicles)-1)/6)
}

func shortText(value string, limit int) string {
	letters := []rune(value)
	if len(letters) <= limit {
		return value
	}
	return string(letters[:limit-1]) + "…"
}
