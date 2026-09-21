// Package view draws the simulation and translates input into commands.
package view

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	width, height = 1100, 760
	background    = 0x0b121a
	panel         = 0x121f2b
	foreground    = 0xe5edf5
	muted         = 0x91a6ba
	accent        = 0x6de5c1
	track         = 0x354b5e
	amber         = 0xf3c479
	detailedLanes = 100
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
	stationPage, podPage int
	mapScale             float64
	mapOrigin            sim.Point
	networkBase          *ebiten.Image
	networkBaseKey       networkCacheKey
	networkBaseValid     bool
	showOrders           bool
	notice               string
	noticeTicks          int
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
	return &Game{network: network, client: remote.New(ctx, serverURL), state: session.State{Simulation: simulation.Snapshot(), Speed: 1}, font: font, origin: "harbor", destination: "market"}, nil
}

// Update reads shared state and handles local input.
func (g *Game) Update() error {
	g.readRemote()
	if g.noticeTicks > 0 {
		g.noticeTicks--
		if g.noticeTicks == 0 {
			g.notice = ""
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		g.showOrders, g.showDemand = false, false
		g.selected = (g.selected + 1) % len(g.state.Simulation.Vehicles)
		g.podPage = g.selected / 6
		g.message = ""
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyD) {
		g.runDemo()
		return nil
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
	for i, key := range []ebiten.Key{ebiten.Key1, ebiten.Key2, ebiten.Key3} {
		if stations := g.passengerStations(); inpututil.IsKeyJustPressed(key) && g.stationPage*3+i < len(stations) {
			g.destination = stations[g.stationPage*3+i].ID
			g.message = ""
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyS) {
		g.cycleSpeed()
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		x, y := ebiten.CursorPosition()
		if g.click(sim.Point{X: float64(x), Y: float64(y)}) {
			return nil
		}
	}
	return nil
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

type button struct {
	x, y, w, h         float64
	label              string
	selected, disabled bool
	action             string
}

func (g *Game) buttons() []button {
	state := g.state.Simulation
	busy := state.Demo || !g.connected || g.pending
	requestLabel := "Request journey"
	if g.noticeTicks > 150 {
		requestLabel = "Order accepted"
	}
	pauseLabel := "Pause [Space]"
	if state.Paused {
		pauseLabel = "Resume [Space]"
	}
	buttons := []button{
		{x: 810, y: 529, w: 120, h: 26, label: fmt.Sprintf("Orders %d", len(outstandingOrders(state))), selected: g.showOrders, action: "orders"},
		{x: 940, y: 529, w: 120, h: 26, label: "Demand", selected: g.showDemand, action: "demand"},
		{x: 840, y: 644, w: 215, h: 42, label: "Run traffic demo [D]", action: "demo"},
		{x: 646, y: 644, w: 174, h: 42, label: requestLabel, selected: true, disabled: busy || g.destination == g.origin, action: "request"},
		{x: 810, y: 440, w: 250, h: 36, label: pauseLabel, action: "pause"},
		{x: 810, y: 486, w: 119, h: 36, label: fmt.Sprintf("Speed %dx [S]", g.state.Speed), action: "speed"},
		{x: 941, y: 486, w: 119, h: 36, label: "Reset [R]", action: "reset"},
	}
	for i, v := range state.Vehicles {
		if i/6 != g.podPage {
			continue
		}
		buttons = append(buttons, button{x: 810 + float64((i%6)*36), y: 393, w: 32, h: 34, label: fleetPodLabel(i), selected: !g.showOrders && !g.showDemand && g.selected == i, action: "pod/" + v.Pod.ID})
	}
	if len(state.Vehicles) > 6 {
		buttons = append(buttons, button{x: 1026, y: 393, w: 34, h: 34, label: ">", action: "pods-next"})
	}
	stations := g.passengerStations()
	if len(stations) > 3 {
		buttons = append(buttons, button{x: 595, y: 649, w: 36, h: 30, label: ">", action: "stations-next"})
	}
	for i, station := range stations {
		if i/3 != g.stationPage {
			continue
		}
		i %= 3
		buttons = append(buttons,
			button{x: float64(100 + i*165), y: 632, w: 150, h: 28, label: shortText(station.Name, 17), selected: g.origin == station.ID, disabled: busy, action: "from/" + station.ID},
			button{x: float64(100 + i*165), y: 668, w: 150, h: 28, label: fmt.Sprintf("%s [%d]", shortText(station.Name, 12), i+1), selected: g.destination == station.ID, disabled: busy, action: station.ID},
		)
	}
	if g.showDemand {
		buttons = append(buttons, g.demandButtons()...)
	}
	for i := range buttons {
		switch buttons[i].action {
		case "pause", "speed", "reset", "demo":
			buttons[i].disabled = !g.connected || g.pending
		}
	}
	return buttons
}

// click reports a reset so its zero-time state can render before the next tick.
func (g *Game) click(point sim.Point) bool {
	for _, b := range g.buttons() {
		if b.disabled || point.X < b.x || point.X >= b.x+b.w || point.Y < b.y || point.Y >= b.y+b.h {
			continue
		}
		switch b.action {
		case "pods-next":
			g.podPage = (g.podPage + 1) % ((len(g.state.Simulation.Vehicles) + 5) / 6)
		case "stations-next":
			g.stationPage = (g.stationPage + 1) % ((len(g.passengerStations()) + 2) / 3)
		case "demo":
			g.runDemo()
			return true
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
			} else {
				g.destination = b.action
			}
			g.message = ""
		}
		return false
	}
	for i, v := range g.mapSnapshot().Vehicles {
		p := g.mapPoint(v.Pod.Position)
		if math.Hypot(point.X-p.X, point.Y-p.Y) < 18 {
			g.selected, g.message = i, ""
			g.showOrders, g.showDemand = false, false
			break
		}
	}
	return false
}

func (g *Game) runDemo() { g.submit(session.Command{Action: "demo"}) }

// Layout fixes logical coordinates while Ebitengine scales to the browser window.
func (g *Game) Layout(_, _ int) (int, int) { return width, height }

// Draw renders an independent snapshot without changing simulation state.
func (g *Game) Draw(screen *ebiten.Image) {
	g.fitNetwork()
	screen.Fill(rgb(background))
	g.label(screen, label{x: 28, y: 22, size: 30, value: "podsim", color: foreground})
	g.label(screen, label{x: 157, y: 34, size: 14, value: "NETWORK PLAYGROUND / LOCAL TRAFFIC", color: muted})
	g.label(screen, label{x: 815, y: 34, size: 14, value: fmt.Sprintf("%d STOPS     %d PODS", len(g.passengerStations()), len(g.state.Simulation.Vehicles)), color: accent})
	vector.FillRect(screen, 24, 96, 748, 474, rgb(panel), false)
	vector.FillRect(screen, 796, 96, 280, 474, rgb(panel), false)
	vector.FillRect(screen, 24, 590, 1052, 142, rgb(panel), false)
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
	g.label(screen, label{x: 28, y: 740, size: 11, value: g.connectionLabel(), color: muted})
}

func (g *Game) mapPoint(p sim.Point) sim.Point {
	return sim.Point{X: g.mapOrigin.X + p.X*g.mapScale, Y: g.mapOrigin.Y + p.Y*g.mapScale}
}

func (g *Game) drawNetwork(screen *ebiten.Image, state sim.Snapshot) {
	g.label(screen, label{x: 44, y: 113, size: 12, value: "NETWORK", color: muted})
	g.label(screen, label{x: 520, y: 113, size: 12, value: "ONE-WAY LANES  /  METERS", color: muted})
	detailed := len(g.network.Lanes) <= detailedLanes
	if detailed {
		g.releaseNetworkBase()
		g.drawBaseNetwork(screen, true)
	} else {
		g.drawCachedNetworkBase(screen)
	}
	selected := state.Vehicles[g.selected]
	if selected.Pod.Activity != sim.Idle {
		shade := podColor(selected.Pod.ID)
		for _, lane := range selected.Route {
			geometry := g.laneGeometry(lane, detailed)
			geometry.draw(screen, laneStroke{width: 2, color: shade, antialias: detailed})
			drawArrow(screen, arrow{from: geometry.arrowFrom, to: geometry.arrowTo, color: shade, antialias: detailed})
		}
	}
	for _, station := range g.network.Stations {
		occupied, reserved := 0, 0
		center := sim.Point{}
		for j, berth := range station.Berths {
			node, _ := g.network.Node(berth.Node)
			p := g.mapPoint(node.Position)
			shade := uint32(muted)
			for _, v := range state.Vehicles {
				if v.Pod.BerthID == berth.ID {
					shade = podColor(v.Pod.ID)
				}
			}
			vector.StrokeCircle(screen, float32(p.X), float32(p.Y), 13, 2, rgb(shade), detailed)
			name := station.Name
			labelX, labelY := p.X-26, p.Y+20
			if station.ParkingOnly || len(station.Berths) > 1 {
				name = strconv.Itoa(j + 1)
				labelX, labelY = p.X-26, p.Y-9
				center.X += p.X
				center.Y += p.Y
			}
			g.label(screen, label{x: labelX, y: labelY, size: 16, value: name, color: foreground})
			occupancy := "BERTH 0/1"
			for _, b := range state.Berths {
				if b.ID == berth.ID {
					if b.Occupant != "" {
						occupancy = "BERTH 1/1"
						occupied++
					} else if b.ReservedBy != "" {
						occupancy = "ARRIVING " + b.ReservedBy
						for _, v := range state.Vehicles {
							if v.Pod.ID == b.ReservedBy && len(v.Route) > 0 && v.Route[0].From == berth.Node {
								occupancy = "DEPARTING " + b.ReservedBy
							}
						}
						reserved++
					}
				}
			}
			if !station.ParkingOnly && len(station.Berths) == 1 {
				g.label(screen, label{x: labelX, y: labelY + 21, size: 10, value: occupancy, color: shade})
			}
		}
		if station.ParkingOnly || len(station.Berths) > 1 {
			x := center.X/float64(len(station.Berths)) + 25
			y := center.Y/float64(len(station.Berths)) - 24
			g.label(screen, label{x: x, y: y, size: 16, value: station.Name, color: foreground})
			g.label(screen, label{x: x, y: y + 23, size: 11, value: fmt.Sprintf("%d/%d occupied", occupied, len(station.Berths)), color: muted})
			if reserved > 0 {
				g.label(screen, label{x: x, y: y + 40, size: 10, value: fmt.Sprintf("%d reserved", reserved), color: amber})
			}
		}
	}
	for i, v := range state.Vehicles {
		p := g.mapPoint(v.Pod.Position)
		shade := podColor(v.Pod.ID)
		if v.Pod.WaitReason != sim.NoWait {
			shade = amber
		}
		if i == g.selected {
			vector.StrokeCircle(screen, float32(p.X), float32(p.Y), 9, 1.5, rgb(shade), detailed)
		}
		vector.FillCircle(screen, float32(p.X), float32(p.Y), 4, rgb(shade), detailed)
		g.label(screen, label{x: p.X + 11, y: p.Y - 18, size: 11, value: fleetPodLabel(i), color: shade})
	}
	vector.StrokeLine(screen, 48, 540, 125, 540, 2, rgb(muted), detailed)
	g.label(screen, label{x: 141, y: 531, size: 12, value: fmt.Sprintf("%.0f m", 77/g.mapScale), color: muted})
	g.label(screen, label{x: 433, y: 531, size: 12, value: "Selected pod and route highlighted", color: muted})
}

type networkCacheKey struct {
	epoch      string
	generation uint64
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
		geometry.draw(screen, laneStroke{width: 5, color: track, antialias: detailed})
		drawArrow(screen, arrow{from: geometry.arrowFrom, to: geometry.arrowTo, color: track, antialias: detailed})
	}
	for _, node := range g.network.Nodes {
		point := g.mapPoint(node.Position)
		vector.FillCircle(screen, float32(point.X), float32(point.Y), 3, rgb(muted), detailed)
	}
}

func (g *Game) drawCachedNetworkBase(screen *ebiten.Image) {
	key := networkCacheKey{epoch: g.state.Epoch, generation: g.state.Generation}
	if g.networkBase == nil {
		g.networkBase = ebiten.NewImage(width, height)
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
	key := networkCacheKey{epoch: g.state.Epoch, generation: g.state.Generation}
	return !g.networkBaseValid || g.networkBaseKey != key
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
		vector.StrokeLine(screen, float32(x), float32(y), float32(x-ux*7+uy*4*side), float32(y-uy*7-ux*4*side), 1.5, rgb(a.color), a.antialias)
	}
}

func (g *Game) drawInspection(screen *ebiten.Image, state sim.Snapshot) {
	podID := state.Vehicles[g.selected].Pod.ID
	podLabel := fleetPodLabel(g.selected)
	if podID != podLabel {
		podLabel += " / " + podID
	}
	g.label(screen, label{x: 816, y: 115, size: 12, value: "POD " + podLabel, color: muted})
	g.label(screen, label{x: 816, y: 143, size: 26, value: activityLabel(state.Vehicles[g.selected].Pod), color: podColor(state.Vehicles[g.selected].Pod.ID)})
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
	g.label(screen, label{x: 816, y: 183, size: 13, value: status, color: muted})
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
	g.label(screen, label{x: 816, y: 224, size: 17, value: journey, color: foreground})
	passengers := "Empty"
	if state.Vehicles[g.selected].Pod.Occupied {
		passengers = "1 party / 1 passenger"
	}
	rows := []string{
		fmt.Sprintf("Speed         %3.0f km/h", state.Vehicles[g.selected].Pod.Speed*3.6),
		"On board     " + passengers,
		fmt.Sprintf("Completed   %d journeys", state.Completed),
		fmt.Sprintf("Sim time      %.1f s", float64(state.Tick)/sim.TicksPerSecond),
	}
	for i, row := range rows {
		g.label(screen, label{x: 816, y: 266 + float64(i*31), size: 14, value: row, color: muted})
	}
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
	g.label(screen, label{x: 44, y: 638, size: 11, value: "FROM", color: muted})
	g.label(screen, label{x: 44, y: 674, size: 11, value: "TO", color: muted})
	g.label(screen, label{x: 816, y: 557, size: 10, value: fmt.Sprintf("Pickup wait: avg %.0f s / max %.0f s", state.Wait.AverageSeconds, state.Wait.MaxSeconds), color: muted})
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
	padding := 12.0
	fontSize := 14.0
	if id, ok := strings.CutPrefix(b.action, "pod/"); ok {
		ink, selectedFill = podColor(id), podColor(id)
		padding = 5
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
	g.label(screen, label{x: b.x + padding, y: b.y + (b.h-17)/2, size: fontSize, value: b.label, color: ink})
}

type label struct {
	x, y, size float64
	value      string
	color      uint32
}

func (g *Game) label(screen *ebiten.Image, label label) {
	options := &text.DrawOptions{}
	options.GeoM.Translate(label.x, label.y)
	options.ColorScale.ScaleWithColor(rgb(label.color))
	text.Draw(screen, label.value, &text.GoTextFace{Source: g.font, Size: label.size}, options)
}

func rgb(hex uint32) color.RGBA {
	return color.RGBA{R: uint8((hex >> 16) & 255), G: uint8((hex >> 8) & 255), B: uint8(hex & 255), A: 255}
}

func podColor(id string) uint32 {
	switch id {
	case "01":
		return accent
	case "02":
		return 0x89b9ff
	case "03":
		return 0xf2a4cf
	case "04":
		return 0xe6d889
	}
	palette := []uint32{0xb6a0ff, 0xffa879, 0x8cdba0, 0x83d6e8, 0xdfa9ea, 0xd4d48a}
	hash := uint32(2166136261)
	for i := range len(id) {
		hash = (hash ^ uint32(id[i])) * 16777619
	}
	return palette[int(hash)%len(palette)]
}

func activityLabel(pod sim.Pod) string {
	if pod.WaitReason != sim.NoWait && pod.Speed < 0.01 {
		return "Waiting"
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

func (g *Game) fitNetwork() {
	left, top, right, bottom := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	include := func(p sim.Point) {
		left = min(left, p.X)
		top = min(top, p.Y)
		right = max(right, p.X)
		bottom = max(bottom, p.Y)
	}
	for _, n := range g.network.Nodes {
		include(n.Position)
	}
	for _, l := range g.network.Lanes {
		if l.Control != nil {
			include(*l.Control)
		}
	}
	if len(g.network.Nodes) == 0 {
		g.mapScale = 1
		return
	}
	g.mapScale = min(590/max(1, right-left), 300/max(1, bottom-top))
	g.mapOrigin = sim.Point{X: 80 + (590-(right-left)*g.mapScale)/2 - left*g.mapScale, Y: 165 + (300-(bottom-top)*g.mapScale)/2 - top*g.mapScale}
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
	g.stationPage = min(g.stationPage, (len(stations)-1)/3)
	g.podPage = min(g.podPage, (len(g.state.Simulation.Vehicles)-1)/6)
}

func shortText(value string, limit int) string {
	letters := []rune(value)
	if len(letters) <= limit {
		return value
	}
	return string(letters[:limit-1]) + "…"
}
