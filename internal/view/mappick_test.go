package view

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPickMapTarget checks the hit test of a map click. A pod in reach wins
// over a station in reach, and of two objects of the same kind the nearer
// one wins.
func TestPickMapTarget(t *testing.T) {
	t.Parallel()
	pod := func(index int, x, y float64) mapCandidate {
		return mapCandidate{pod: index, center: sim.Point{X: x, Y: y}}
	}
	station := func(id string, x, y float64) mapCandidate {
		return mapCandidate{pod: -1, station: id, center: sim.Point{X: x, Y: y}}
	}
	tests := []struct {
		name     string
		pods     []mapCandidate
		stations []mapCandidate
		want     mapCandidate
		wantOK   bool
	}{
		{name: "nothing on the map"},
		{name: "nothing in reach", pods: []mapCandidate{pod(0, 30, 0)}, stations: []mapCandidate{station("bank", 0, 20)}},
		{name: "station only", stations: []mapCandidate{station("bank", 5, 0)}, want: station("bank", 5, 0), wantOK: true},
		{name: "pod only", pods: []mapCandidate{pod(3, 0, 12)}, want: pod(3, 0, 12), wantOK: true},
		{
			name: "pod wins over nearer station",
			pods: []mapCandidate{pod(2, 15, 0)}, stations: []mapCandidate{station("bank", 1, 0)},
			want: pod(2, 15, 0), wantOK: true,
		},
		{
			name: "station when the pod is out of reach",
			pods: []mapCandidate{pod(2, 19, 0)}, stations: []mapCandidate{station("bank", 13, 0)},
			want: station("bank", 13, 0), wantOK: true,
		},
		{
			name:     "nearer station wins",
			stations: []mapCandidate{station("bank", 10, 0), station("monument", 0, -4), station("bank", 8, 8)},
			want:     station("monument", 0, -4), wantOK: true,
		},
		{name: "nearer pod wins", pods: []mapCandidate{pod(0, 12, 0), pod(1, -6, 0)}, want: pod(1, -6, 0), wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := pickMapTarget(mapPickInput{pods: test.pods, stations: test.stations, podRadius: 18, stationRadius: 14})
			if got != test.want || ok != test.wantOK {
				t.Errorf("pickMapTarget = %+v, %t, want %+v, %t", got, ok, test.want, test.wantOK)
			}
		})
	}
}

// londonPickGame returns a connected game with the London network and no
// pods. The camera fits the network.
func londonPickGame(t *testing.T) *Game {
	t.Helper()
	game := journeyNetworkGame(t, scenarios.London().Network)
	game.state = session.State{Epoch: "test", Speed: 1}
	game.fitNetwork()
	return game
}

// TestPickStationOnMap checks that a click on a station anchor sets From,
// that Shift+click sets To, and that the station page then shows the chip of
// the station. While the chips are disabled, a click on a station changes
// nothing.
func TestPickStationOnMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		shift           bool
		demo            bool
		disconnected    bool
		wantOrigin      bool
		wantDestination bool
	}{
		{name: "click sets From", wantOrigin: true},
		{name: "Shift+click sets To", shift: true, wantDestination: true},
		{name: "traffic demo", demo: true},
		{name: "no connection", shift: true, disconnected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := londonPickGame(t)
			game.state.Simulation.Demo, game.connected = test.demo, !test.disconnected
			game.message = "x"
			pages := game.stationPages()
			if len(pages) < 3 {
				t.Fatalf("station pages = %d, want at least 3", len(pages))
			}
			last := len(pages) - 1
			target := pages[last][0].station.ID
			origin, destination := game.origin, game.destination
			game.pickOnMap(game.mapPoint(game.stationAnchors()[target]), test.shift)
			wantOrigin, wantDestination, wantPage, wantMessage := origin, destination, 0, "x"
			if test.wantOrigin {
				wantOrigin, wantPage, wantMessage = target, last, ""
			}
			if test.wantDestination {
				wantDestination, wantPage, wantMessage = target, last, ""
			}
			if game.origin != wantOrigin || game.destination != wantDestination {
				t.Errorf("From %q, To %q, want %q, %q", game.origin, game.destination, wantOrigin, wantDestination)
			}
			if game.stationPage != wantPage || game.message != wantMessage {
				t.Errorf("station page %d, message %q, want %d, %q", game.stationPage, game.message, wantPage, wantMessage)
			}
			if test.wantOrigin || test.wantDestination {
				action := "from/" + target
				if test.shift {
					action = "to/" + target
				}
				chip := findButton(t, game.buttons(), action)
				if !chip.selected {
					t.Errorf("chip of %q is not selected", target)
				}
			}
		})
	}
}

// TestPickPodBeforeStation clicks an idle pod in a station of the example
// network. The pod wins over the station, and From and To do not change.
func TestPickPodBeforeStation(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	game.connected = true
	game.fitNetwork()
	game.selected, game.podPage = 0, 3
	pod := game.state.Simulation.Vehicles[1]
	game.pickOnMap(game.mapPoint(pod.Pod.Position), false)
	if game.selected != 1 || game.podPage != 0 {
		t.Errorf("selected pod %d on page %d, want 1 on page 0", game.selected, game.podPage)
	}
	if game.origin != "harbor" || game.destination != "market" {
		t.Errorf("From %q, To %q, want harbor, market", game.origin, game.destination)
	}
}

// TestPickStationBerth zooms in on London until the map shows the berths of
// the stations. It clicks each shown berth that is out of reach of all
// station anchors and of the points of other stations. Each click sets From
// to the station of the berth.
func TestPickStationBerth(t *testing.T) {
	t.Parallel()
	game := londonPickGame(t)
	game.zoomMap(mapMaxZoom)
	input := game.mapPickTargets(sim.Point{})
	var anchors []mapCandidate
	for id, anchor := range game.stationAnchors() {
		anchors = append(anchors, mapCandidate{pod: -1, station: id, center: game.mapPoint(anchor)})
	}
	clicked := 0
	for _, station := range game.passengerStations() {
		if !game.showStationBerths(station) {
			continue
		}
		others := slices.DeleteFunc(slices.Clone(input.stations), func(candidate mapCandidate) bool { return candidate.station == station.ID })
		for _, berth := range station.Berths {
			node, _ := game.network.Node(berth.Node)
			point := game.mapPoint(node.Position)
			_, anchorInReach := nearestCandidate(point, anchors, input.stationRadius)
			_, otherInReach := nearestCandidate(point, others, input.stationRadius)
			if anchorInReach || otherInReach {
				continue
			}
			game.origin = ""
			game.pickOnMap(point, false)
			if game.origin != station.ID {
				t.Errorf("click on berth %q set From %q, want %q", berth.Node, game.origin, station.ID)
			}
			clicked++
		}
	}
	if clicked == 0 {
		t.Fatal("no shown berth out of reach of the station anchors")
	}
}

// TestPodPager checks the page arrows and the page count of the pod
// selector. The arrows stop at the first and the last page.
func TestPodPager(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		pods      int
		page      int
		action    string
		wantPage  int
		wantCount string
	}{
		{name: "one page", pods: podsPerPage},
		{name: "next page", pods: 12, action: "pods-next", wantPage: 1, wantCount: "2 / 3"},
		{name: "previous page", pods: 12, page: 2, action: "pods-prev", wantPage: 1, wantCount: "2 / 3"},
		{name: "first page", pods: 12, action: "pods-prev", wantCount: "1 / 3"},
		{name: "last page", pods: 12, page: 2, action: "pods-next", wantPage: 2, wantCount: "3 / 3"},
		{name: "London fleet", pods: 114, page: 21, action: "pods-next", wantPage: 22, wantCount: "23 / 23"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := journeyTestGame(t, 4)
			game.state.Simulation.Vehicles = make([]sim.Vehicle, test.pods)
			for i := range game.state.Simulation.Vehicles {
				game.state.Simulation.Vehicles[i].Pod.ID = fmt.Sprintf("pod-%03d", i)
			}
			game.podPage = test.page
			if test.action != "" {
				control := findButton(t, game.buttons(), test.action)
				game.click(centerOfButton(control))
			}
			if game.podPage != test.wantPage {
				t.Errorf("pod page = %d, want %d", game.podPage, test.wantPage)
			}
			count, ok := game.podPagerLabel()
			if count.value != test.wantCount || ok != (test.wantCount != "") {
				t.Errorf("page count = %q, %t, want %q", count.value, ok, test.wantCount)
			}
			pods := 0
			for _, control := range game.buttons() {
				switch control.action {
				case "pods-prev":
					if control.disabled != (game.podPage == 0) {
						t.Errorf("previous arrow disabled %t on page %d", control.disabled, game.podPage)
					}
				case "pods-next":
					if control.disabled != (game.podPage == podPageCount(test.pods)-1) {
						t.Errorf("next arrow disabled %t on page %d", control.disabled, game.podPage)
					}
				}
				if strings.HasPrefix(control.action, "pod/") {
					pods++
				}
			}
			if want := min(podsPerPage, test.pods-game.podPage*podsPerPage); pods != want {
				t.Errorf("pod buttons = %d, want %d", pods, want)
			}
		})
	}
}

// TestPodPagerFits checks at each control layout that the pod buttons, the
// page arrows and the widest London page count do not overlap.
func TestPodPagerFits(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.state.Simulation.Vehicles = make([]sim.Vehicle, 114)
			for i := range game.state.Simulation.Vehicles {
				game.state.Simulation.Vehicles[i].Pod.ID = fleetPodLabel(i)
			}
			game.podPage = podPageCount(114) - 1
			count, ok := game.podPagerLabel()
			if !ok {
				t.Fatal("no page count")
			}
			countArea := game.labelArea(count)
			var row []button
			for _, control := range game.buttons() {
				if control.action == "pods-prev" || control.action == "pods-next" || strings.HasPrefix(control.action, "pod/") {
					row = append(row, control)
				}
			}
			for i, control := range row {
				if countArea.overlaps(buttonArea(control)) {
					t.Errorf("page count %+v overlaps %q %+v", countArea, control.action, buttonArea(control))
				}
				for _, other := range row[i+1:] {
					if buttonArea(control).overlaps(buttonArea(other)) {
						t.Errorf("%q overlaps %q", control.action, other.action)
					}
				}
			}
		})
	}
}
