package view

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hajimehoshi/ebiten/v2/text/v2"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// rankTestStation is a station of rankTestNetwork with a number of approach
// lanes and a number of lanes with other station roles.
type rankTestStation struct {
	id         string
	parking    bool
	approaches int
	others     int
}

// rankTestNetwork returns a network with the stations and their lanes. Only
// the approach lanes count for the rank.
func rankTestNetwork(stations []rankTestStation) sim.Network {
	otherRoles := []sim.StationLaneRole{sim.StationEntryRole, sim.StationBerthAccessRole, sim.StationThroughRole, sim.StationDepartureRole, sim.StationExitRole}
	var network sim.Network
	for _, station := range stations {
		network.Stations = append(network.Stations, sim.Station{ID: station.id, ParkingOnly: station.parking})
		for index := range station.approaches {
			network.Lanes = append(network.Lanes, sim.Lane{ID: fmt.Sprintf("%s-approach-%d", station.id, index), StationID: station.id, StationRole: sim.StationApproachRole})
		}
		for index := range station.others {
			role := otherRoles[index%len(otherRoles)]
			network.Lanes = append(network.Lanes, sim.Lane{ID: fmt.Sprintf("%s-%s-%d", station.id, role, index), StationID: station.id, StationRole: role})
		}
	}
	return network
}

// rankOrder returns the station IDs in rank order.
func rankOrder(t *testing.T, ranks map[string]int) []string {
	t.Helper()
	order := make([]string, len(ranks))
	for id, rank := range ranks {
		if rank < 0 || rank >= len(order) || order[rank] != "" {
			t.Fatalf("station %q has rank %d, want a unique rank below %d", id, rank, len(order))
		}
		order[rank] = id
	}
	return order
}

func TestStationLabelRanks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		network sim.Network
		want    []string
	}{
		{
			name: "parking first then more approach lanes",
			network: rankTestNetwork([]rankTestStation{
				// The other lanes of "one" must not put it before "three".
				{id: "one", approaches: 1, others: 5},
				{id: "three", approaches: 3, others: 1},
				{id: "small-parking", parking: true, approaches: 1},
				{id: "other-three", approaches: 3},
				{id: "none"},
				{id: "large-parking", parking: true, approaches: 2},
			}),
			want: []string{"large-parking", "small-parking", "three", "other-three", "one", "none"},
		},
		{
			name:    "same lanes keep network order",
			network: rankTestNetwork([]rankTestStation{{id: "c", approaches: 2}, {id: "a", approaches: 2}, {id: "b", approaches: 2}}),
			want:    []string{"c", "a", "b"},
		},
		{name: "example", network: sim.Example(), want: []string{"parking", "harbor", "garden", "market"}},
		{name: "empty", network: sim.Network{}, want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rankOrder(t, stationLabelRanks(test.network)); !slices.Equal(got, test.want) {
				t.Fatalf("rank order = %v, want %v", got, test.want)
			}
		})
	}
}

// TestStationLabelRanksOnLondon checks that the London Parking facilities and
// interchanges come first, not the first stations in alphabetical order.
func TestStationLabelRanksOnLondon(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	names := make(map[string]string, len(network.Stations))
	for _, station := range network.Stations {
		names[station.ID] = station.Name
	}
	order := rankOrder(t, stationLabelRanks(network))
	var got []string
	for _, id := range order[:12] {
		got = append(got, names[id])
	}
	want := []string{
		"West London Parking", "North London Parking", "East London Parking",
		// 7 approach lanes.
		"Baker Street", "Bank and Monument", "King's Cross St. Pancras",
		// 6 approach lanes.
		"Earl's Court", "Green Park", "Oxford Circus", "Waterloo",
		// 5 approach lanes.
		"Liverpool Street", "Paddington",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("first labels = %q, want %q", got, want)
	}
}

func TestStationLabelRanksBuildOncePerNetwork(t *testing.T) {
	t.Parallel()
	first := session.State{Epoch: "first", ProjectRevision: 1, Generation: 1}
	tests := []struct {
		name    string
		state   session.State
		rebuild bool
	}{
		{name: "same network", state: first, rebuild: false},
		{name: "new generation", state: session.State{Epoch: "first", ProjectRevision: 1, Generation: 2}, rebuild: true},
		{name: "new project revision", state: session.State{Epoch: "first", ProjectRevision: 2, Generation: 1}, rebuild: true},
		{name: "new epoch", state: session.State{Epoch: "second", ProjectRevision: 1, Generation: 1}, rebuild: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{network: sim.Example(), state: first}
			if got := game.currentStationLabelRanks()["parking"]; got != 0 {
				t.Fatalf("parking rank = %d, want 0", got)
			}
			changed := sim.Example()
			changed.Stations[3].ParkingOnly = false
			game.network, game.state = changed, test.state
			want := 0
			if test.rebuild {
				want = 3
			}
			if got := game.currentStationLabelRanks()["parking"]; got != want {
				t.Fatalf("parking rank = %d, want %d", got, want)
			}
		})
	}
}

func TestCollapsedStationLabelBounds(t *testing.T) {
	t.Parallel()
	layouts := []layoutInput{
		{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1},
		{outsideWidth: 1366, outsideHeight: 617, deviceScale: 1},
		{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1.5},
	}
	tests := []struct {
		name        string
		stationName string
		queue       bool
		wantName    string
	}{
		{name: "one line", stationName: "Waterloo", wantName: "Waterloo"},
		{name: "prefix removed", stationName: "Station 07", wantName: "07"},
		{name: "20 runes stay", stationName: "North London Parking", wantName: "North London Parking"},
		{name: "long name cut", stationName: "King's Cross St. Pancras", wantName: "King's Cross St. Pa…"},
		{name: "cut after a space", stationName: "Great Portland St. Station", wantName: "Great Portland St.…"},
		{name: "queue line", stationName: "Oval", queue: true, wantName: "Oval"},
		{name: "queue line under long name", stationName: "Tottenham Court Road", queue: true, wantName: "Tottenham Court Road"},
	}
	for _, layout := range layouts {
		for _, test := range tests {
			t.Run(fmt.Sprintf("%dx%d@%g/%s", layout.outsideWidth, layout.outsideHeight, layout.deviceScale, test.name), func(t *testing.T) {
				t.Parallel()
				game := journeyTestGame(t, 2)
				game.layoutFor(layout)
				unit := game.layout.unit
				station := sim.Station{ID: "s", Name: test.stationName, Berths: make([]sim.Berth, 12)}
				status := observe.StationMetrics{Occupied: 3}
				if test.queue {
					status.EntranceStopped, status.ExitStopped = 2, 11
				}
				input := collapsedStationLabelInput{station: station, status: status, marker: sim.Point{X: 300.4, Y: 250.7}}
				candidate := game.collapsedStationLabel(input)
				if want := test.wantName + "  3/12"; candidate.primary.value != want {
					t.Fatalf("primary = %q, want %q", candidate.primary.value, want)
				}
				if name, _, _ := strings.Cut(candidate.primary.value, "  "); utf8.RuneCountInString(name) > overviewNameRunes {
					t.Fatalf("name %q has more than %d runes", name, overviewNameRunes)
				}
				bounds := game.collapsedStationLabelBounds(candidate)

				// The box holds each drawn line and the widest occupancy.
				area := func(x, y, size float64, value string) image.Rectangle {
					width, height := text.Measure(value, &text.GoTextFace{Source: game.font, Size: size * unit}, 0)
					return image.Rect(int(math.Floor(x)), int(math.Floor(y)), int(math.Ceil(x+width)), int(math.Ceil(y+height)))
				}
				x, y := input.marker.X+16*unit, input.marker.Y-14*unit
				drawn := area(x, y, 10, candidate.collisionValue)
				if test.queue {
					if want := "In 2 · Out 11"; candidate.secondary != want {
						t.Fatalf("secondary = %q, want %q", candidate.secondary, want)
					}
					drawn = drawn.Union(area(x, y+16*unit, 9, candidate.secondary))
				} else if candidate.secondary != "" {
					t.Fatalf("secondary = %q, want no queue line", candidate.secondary)
				}
				if !area(x, y, 10, candidate.primary.value).In(bounds) || !drawn.In(bounds) {
					t.Fatalf("bounds %v do not hold the text %v", bounds, drawn)
				}
				// The padding is 3 units on each side, and nothing more.
				padding := int(math.Ceil(3 * unit))
				if got := bounds.Inset(padding); got != drawn {
					t.Fatalf("bounds without padding = %v, want the text area %v", got, drawn)
				}
				bounded := game.boundedStationLabels(collapsedLabelsInput{labels: []collapsedStationLabel{candidate}, markers: map[string]sim.Point{"s": input.marker}})[0]
				if bounded.bounds != bounds || bounded.queued != test.queue {
					t.Fatalf("bounded label = %v with queue line %t, want %v with queue line %t", bounded.bounds, bounded.queued, bounds, test.queue)
				}
				// A label without a queue line does not keep room for it.
				if !test.queue && float64(bounds.Dy()) >= 16*unit+float64(2*padding) {
					t.Fatalf("one-line bounds are %d pixels high, want less than %g", bounds.Dy(), 16*unit+float64(2*padding))
				}

				// The box does not change with the occupancy.
				input.status.Occupied = 12
				if got := game.collapsedStationLabelBounds(game.collapsedStationLabel(input)); got != bounds {
					t.Fatalf("bounds with 12 occupied = %v, want %v", got, bounds)
				}
			})
		}
	}
}

func TestBoundedStationLabelMarkers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		layout layoutInput
		radius float64
	}{
		{name: "fit", layout: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1}, radius: 3},
		{name: "largest", layout: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1}, radius: 10},
		{name: "small window", layout: layoutInput{outsideWidth: 1366, outsideHeight: 617, deviceScale: 1}, radius: 7.5},
		{name: "fractional scale", layout: layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1.5}, radius: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 2)
			game.layoutFor(test.layout)
			unit := game.layout.unit
			center := sim.Point{X: 400.3, Y: 300.6}
			candidate := game.collapsedStationLabel(collapsedStationLabelInput{station: sim.Station{ID: "s", Name: "Oval", Berths: make([]sim.Berth, 2)}, marker: center})
			bounded := game.boundedStationLabels(collapsedLabelsInput{
				labels:       []collapsedStationLabel{candidate},
				markers:      map[string]sim.Point{"s": center},
				markerRadius: test.radius * unit,
			})[0]
			// The outline is centered on the radius.
			outer := (test.radius + markerOutlineWidth/2) * unit
			drawn := image.Rect(
				int(math.Floor(center.X-outer)), int(math.Floor(center.Y-outer)),
				int(math.Ceil(center.X+outer)), int(math.Ceil(center.Y+outer)),
			)
			if bounded.marker != drawn {
				t.Fatalf("marker bounds = %v, want %v", bounded.marker, drawn)
			}
			if bounded.bounds.Overlaps(bounded.marker) {
				t.Fatalf("label %v covers its own marker %v", bounded.bounds, bounded.marker)
			}
		})
	}
}

func TestSelectCollapsedStationLabels(t *testing.T) {
	t.Parallel()
	// Each label is 60 by 20 pixels. Its marker is 10 by 10 pixels to the
	// left of it.
	at := func(id string, rank, x, y int) boundedStationLabel {
		return boundedStationLabel{
			stationID: id, rank: rank,
			bounds: image.Rect(x, y, x+60, y+20),
			marker: image.Rect(x-15, y+5, x-5, y+15),
		}
	}
	queued := func(label boundedStationLabel) boundedStationLabel {
		label.queued = true
		return label
	}
	tests := []struct {
		name      string
		labels    []boundedStationLabel
		preferred map[string]bool
		occupied  []image.Rectangle
		dense     bool
		want      []bool
	}{
		{
			name:   "lower rank first",
			labels: []boundedStationLabel{at("small", 5, 0, 0), at("large", 1, 30, 0), at("clear", 9, 200, 0)},
			dense:  true,
			want:   []bool{false, true, true},
		},
		{
			// The hidden label and its marker overlap only the label and
			// the marker of "small".
			name:   "hidden label does not block",
			labels: []boundedStationLabel{at("large", 0, 0, 0), at("hidden", 1, 40, 0), at("small", 2, 90, 10)},
			dense:  true,
			want:   []bool{true, false, true},
		},
		{
			name:   "queue line before rank",
			labels: []boundedStationLabel{at("large", 0, 0, 0), queued(at("small", 5, 30, 0))},
			dense:  true,
			want:   []bool{false, true},
		},
		{
			name:   "lower rank first with queue lines",
			labels: []boundedStationLabel{queued(at("small", 5, 0, 0)), queued(at("large", 1, 30, 0))},
			dense:  true,
			want:   []bool{false, true},
		},
		{
			name:      "preferred label before queue line",
			labels:    []boundedStationLabel{queued(at("queued", 0, 0, 0)), at("selected", 7, 30, 0)},
			preferred: map[string]bool{"selected": true},
			dense:     true,
			want:      []bool{false, true},
		},
		{
			name:      "preferred label before rank",
			labels:    []boundedStationLabel{at("large", 0, 0, 0), at("selected", 7, 30, 0)},
			preferred: map[string]bool{"selected": true},
			dense:     true,
			want:      []bool{false, true},
		},
		{
			name:      "pod label hides only other labels",
			labels:    []boundedStationLabel{at("large", 0, 0, 0), at("selected", 1, 200, 0)},
			preferred: map[string]bool{"selected": true},
			occupied:  []image.Rectangle{image.Rect(40, 10, 55, 25), image.Rect(210, 0, 220, 10)},
			dense:     true,
			want:      []bool{false, true},
		},
		{
			// The label of "small" covers the marker of "large", which
			// comes first, so it does not show.
			name:   "no label on a larger station marker",
			labels: []boundedStationLabel{at("large", 0, 100, 0), at("small", 1, 30, 0)},
			dense:  true,
			want:   []bool{true, false},
		},
		{
			// The label of "large" covers the marker of "small". The label
			// of "small" is clear, but its marker is under a label, so it
			// does not show.
			name: "larger label on a smaller station marker",
			labels: []boundedStationLabel{
				{stationID: "small", rank: 1, bounds: image.Rect(55, 21, 115, 41), marker: image.Rect(40, 15, 50, 25)},
				at("large", 0, 0, 0),
			},
			dense: true,
			want:  []bool{false, true},
		},
		{
			name:   "separate labels",
			labels: []boundedStationLabel{at("a", 0, 0, 0), at("b", 1, 0, 40), at("c", 2, 100, 0)},
			dense:  true,
			want:   []bool{true, true, true},
		},
		{
			name:     "all labels on a map that is not dense",
			labels:   []boundedStationLabel{at("small", 1, 0, 0), at("large", 0, 20, 0)},
			occupied: []image.Rectangle{image.Rect(0, 0, 500, 500)},
			want:     []bool{true, true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := selectCollapsedStationLabels(collapsedLabelSelection{labels: test.labels, preferred: test.preferred, occupied: test.occupied, dense: test.dense})
			if !slices.Equal(got, test.want) {
				t.Fatalf("visible = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPodMapLabels(t *testing.T) {
	t.Parallel()
	vehicles := []sim.Vehicle{
		{Pod: sim.Pod{ID: "moving", Activity: sim.Traveling, Position: sim.Point{X: 10, Y: 20}}},
		{Pod: sim.Pod{ID: "parked", Activity: sim.Idle, StationID: "collapsed", Position: sim.Point{X: 30, Y: 40}}},
		{Pod: sim.Pod{ID: "selected", Activity: sim.Idle, StationID: "collapsed", Position: sim.Point{X: 50, Y: 60}}},
		{Pod: sim.Pod{ID: "at berth", Activity: sim.Idle, StationID: "expanded", Position: sim.Point{X: 70, Y: 80}}},
	}
	collapsed := map[string]bool{"collapsed": true, "expanded": false}
	tests := []struct {
		name     string
		stations int
		zoom     float64
		want     []string
	}{
		{name: "dense overview", stations: 31, zoom: 1, want: []string{"", "", "03", ""}},
		{name: "dense zoom", stations: 31, zoom: 4, want: []string{"01", "", "03", "04"}},
		{name: "small network", stations: 30, zoom: 1, want: []string{"01", "", "03", "04"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, test.stations)
			game.layoutFor(layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1.5})
			game.selected = 2
			game.camera = mapCamera{scale: 0.5 * test.zoom, minScale: 0.5, origin: sim.Point{X: 100, Y: 200}}
			game.syncCamera()
			labels := game.podMapLabels(vehicles, collapsed)
			for index, podLabel := range labels {
				if podLabel.value != test.want[index] {
					t.Fatalf("pod %d label = %q, want %q", index, podLabel.value, test.want[index])
				}
				if podLabel.value == "" {
					continue
				}
				p := game.mapPoint(vehicles[index].Pod.Position)
				if want := (label{x: p.X + podLabelLeft*game.layout.unit, y: p.Y + podLabelTop*game.layout.unit, size: 11, value: podLabel.value}); podLabel != want {
					t.Fatalf("pod %d label = %+v, want %+v", index, podLabel, want)
				}
			}
		})
	}
}

// TestPodLabelClearsBerthRing checks that the label of a pod at a berth
// stays outside the berth ring. Before, the label covered the ring.
func TestPodLabelClearsBerthRing(t *testing.T) {
	t.Parallel()
	for _, deviceScale := range []float64{1, 1.5, 2} {
		for _, size := range []struct{ width, height int }{{1100, 760}, {1920, 1080}, {800, 600}} {
			game := journeyTestGame(t, 3)
			game.layoutFor(layoutInput{outsideWidth: size.width, outsideHeight: size.height, deviceScale: deviceScale})
			game.camera = mapCamera{scale: 1, minScale: 0.5}
			game.syncCamera()
			vehicles := []sim.Vehicle{{Pod: sim.Pod{ID: "at berth", Activity: sim.Idle, StationID: "expanded", Position: sim.Point{X: 70, Y: 80}}}}
			podLabel := game.podMapLabels(vehicles, map[string]bool{})[0]
			bounds := game.labelBounds(podLabel)
			center := game.mapPoint(vehicles[0].Pod.Position)
			// The nearest point of the label area to the center of the ring.
			nearX := min(max(center.X, float64(bounds.Min.X)), float64(bounds.Max.X))
			nearY := min(max(center.Y, float64(bounds.Min.Y)), float64(bounds.Max.Y))
			outerRadius := (berthRingRadius + 1) * game.layout.unit
			if distance := math.Hypot(nearX-center.X, nearY-center.Y); distance <= outerRadius {
				t.Errorf("scale %.1f, window %dx%d: label %v is %.1f from the pod, want more than the ring radius %.1f", deviceScale, size.width, size.height, bounds, distance, outerRadius)
			}
		}
	}
}

func TestClearPodLabels(t *testing.T) {
	t.Parallel()
	podLabel := func(x, y float64, value string) label { return label{x: x, y: y, size: 11, value: value} }
	stationLabels := []image.Rectangle{image.Rect(100, 100, 200, 120)}
	labels := []label{
		podLabel(10, 10, "01"),   // clear of the station label
		podLabel(150, 105, "02"), // on the station label
		podLabel(150, 105, "03"), // selected, on the station label
		{},                       // no label
		podLabel(95, 90, "05"),   // on the edge of the station label
	}
	tests := []struct {
		name     string
		stations int
		want     []string
	}{
		{name: "dense map", stations: 31, want: []string{"01", "", "03", "", ""}},
		{name: "small network", stations: 30, want: []string{"01", "02", "03", "", "05"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, test.stations)
			game.selected = 2
			got := game.clearPodLabels(labels, stationLabels)
			for index, podLabel := range got {
				if podLabel.value != test.want[index] {
					t.Fatalf("pod %d label = %q, want %q", index, podLabel.value, test.want[index])
				}
			}
			if labels[1].value != "02" {
				t.Fatal("clearPodLabels changed its input")
			}
		})
	}
}

// TestOverviewLabelsOnLondon checks the overview labels of London at Fit and
// when zoomed in. A shown label does not cover another label, the label of
// the selected pod, or the marker of a station placed before it. Its own
// marker is not under a label placed before it.
func TestOverviewLabelsOnLondon(t *testing.T) {
	t.Parallel()
	network := scenarios.London().Network
	for _, zoom := range []float64{1, 2, 4} {
		t.Run(fmt.Sprintf("zoom %g", zoom), func(t *testing.T) {
			t.Parallel()
			game := journeyNetworkGame(t, network)
			game.state = session.State{Epoch: "london"}
			game.layoutFor(layoutInput{outsideWidth: 1100, outsideHeight: 760, deviceScale: 1})
			game.fitNetwork()
			viewport := game.layout.mapViewport
			center := sim.Point{X: float64(viewport.Min.X+viewport.Max.X) / 2, Y: float64(viewport.Min.Y+viewport.Max.Y) / 2}
			game.camera.zoomAt(center, zoom)
			game.syncCamera()
			markers := game.collapsedStationMarkers()
			if len(markers) != len(network.Stations) {
				t.Fatalf("%d collapsed stations, want %d", len(markers), len(network.Stations))
			}
			style := newNetworkStyle(networkStyleInput{network: network, markers: markers, lineLanes: game.currentLineLanes(), scale: game.mapScale, unit: game.layout.unit})
			ranks := game.currentStationLabelRanks()
			var labels []collapsedStationLabel
			for _, station := range network.Stations {
				labels = append(labels, game.collapsedStationLabel(collapsedStationLabelInput{station: station, marker: markers[station.ID], rank: ranks[station.ID]}))
			}
			// The label of the selected pod is in the middle of the map.
			podLabel := label{x: center.X, y: center.Y, size: 11, value: "01"}
			input := collapsedLabelsInput{labels: labels, markers: markers, markerRadius: style.markerRadius, selectedPodLabel: podLabel}
			bounded, visible := game.visibleCollapsedStationLabels(input)
			preferred := map[string]bool{game.origin: true, game.destination: true}
			// placedBefore reports whether the label at index a comes before
			// the label at index b. No station has a queue line here. The
			// order comes from the ranks, not from the selection output.
			placedBefore := func(a, b int) bool {
				idA, idB := bounded[a].stationID, bounded[b].stationID
				if preferred[idA] != preferred[idB] {
					return preferred[idA]
				}
				return ranks[idA] < ranks[idB]
			}
			shown := 0
			for index, candidate := range bounded {
				if candidate.stationID != network.Stations[index].ID || candidate.rank != ranks[candidate.stationID] || candidate.queued {
					t.Fatalf("label %d = station %q, rank %d, queue line %t, want station %q, rank %d, no queue line",
						index, candidate.stationID, candidate.rank, candidate.queued, network.Stations[index].ID, ranks[network.Stations[index].ID])
				}
				if !visible[index] {
					continue
				}
				if candidate.bounds.Overlaps(viewport) {
					shown++
				}
				if preferred[candidate.stationID] {
					continue
				}
				if candidate.bounds.Overlaps(game.labelBounds(podLabel)) {
					t.Errorf("label of %s covers the label of the selected pod", candidate.stationID)
				}
				for other, earlier := range bounded {
					if other == index || !placedBefore(other, index) {
						continue
					}
					if candidate.bounds.Overlaps(earlier.marker) {
						t.Errorf("label of %s covers the marker of %s", candidate.stationID, earlier.stationID)
					}
					if visible[other] && candidate.bounds.Overlaps(earlier.bounds) {
						t.Errorf("label of %s covers the label of %s", candidate.stationID, earlier.stationID)
					}
					if visible[other] && candidate.marker.Overlaps(earlier.bounds) {
						t.Errorf("marker of %s is under the label of %s", candidate.stationID, earlier.stationID)
					}
				}
			}
			if shown < 8 {
				t.Fatalf("%d labels show in the map viewport, want at least 8", shown)
			}
		})
	}
}
