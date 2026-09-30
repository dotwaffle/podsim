package view

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// originalJourneyStations retains the pre-cache sort and matching rules.
func originalJourneyStations(network sim.Network, query string) []sim.Station {
	var stations []sim.Station
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			stations = append(stations, station)
		}
	}
	slices.SortFunc(stations, func(a, b sim.Station) int {
		if order := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return stations
	}
	var exact, partial []sim.Station
	for _, station := range stations {
		code := ""
		switch station.ID {
		case "940GZZBPSUST":
			code = "bps"
		case "940GZZNEUGST":
			code = "nel"
		default:
			if suffix, ok := strings.CutPrefix(station.ID, "940GZZLU"); ok && len(suffix) == 3 {
				code = strings.ToLower(suffix)
			}
		}
		name := strings.ToLower(station.Name)
		if name == query || code == query || strings.EqualFold(station.ID, query) {
			exact = append(exact, station)
		}
		if strings.Contains(name, query) || code != "" && strings.HasPrefix(code, query) {
			partial = append(partial, station)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

func TestJourneyCatalogMatchesOriginal(t *testing.T) {
	t.Parallel()
	custom := sim.Network{Stations: []sim.Station{
		{ID: "940GZZLUBNK", Name: "Bank"},
		{ID: "BNK", Name: "Other"},
		{ID: "same-b", Name: "Repeated"},
		{ID: "same-a", Name: "repeated"},
		{ID: "ς", Name: "Greek"},
		{ID: "unicode", Name: "École İSTANBUL Σ"},
		{ID: "parking", Name: "Bank", ParkingOnly: true},
	}}
	for _, network := range []sim.Network{scenarios.LondonFull().Network, custom, {}} {
		game := &Game{network: network}
		for _, query := range []string{"", " ", "bank", "bnk", "b", "repeated", "sa", "same-a", "σ", "Σ", "éco", "İSTANBUL", "chx", "arc", "BPS", "nel", "940GZZLUCHX", "no such station"} {
			game.journeySearch.filter = 1
			game.journeySearch.query[0] = query
			if got, want := game.journeyStations(), originalJourneyStations(network, query); !reflect.DeepEqual(got, want) {
				t.Fatalf("query %q: got %+v, want %+v", query, got, want)
			}
		}
	}
}

func TestJourneyCatalogKeepsPassengerOrder(t *testing.T) {
	t.Parallel()
	network := scenarios.LondonFull().Network
	game := journeyNetworkGame(t, network)
	var want []sim.Station
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			want = append(want, station)
		}
	}
	game.journeyStations()
	if got := game.passengerStations(); !reflect.DeepEqual(got, want) {
		t.Fatal("sorted catalog changed network-order passenger stations")
	}
	game.origin, game.destination = "missing", "missing"
	game.normalizeSelection()
	if game.origin != want[0].ID || game.destination != want[len(want)-1].ID {
		t.Fatalf("default selections changed: %s to %s", game.origin, game.destination)
	}
	if !reflect.DeepEqual(game.network, network) {
		t.Fatal("catalog changed network stations")
	}
}

func TestJourneyCatalogFollowsNetworkIdentity(t *testing.T) {
	t.Parallel()
	first := session.State{Epoch: "one", ProjectRevision: 3, Generation: 5, ServerStart: "a"}
	for _, tc := range []struct {
		name  string
		state session.State
	}{
		{"rename", session.State{Epoch: "one", ProjectRevision: 4, Generation: 6, ServerStart: "a"}},
		{"rewind", session.State{Epoch: "one", ProjectRevision: 2, Generation: 6, ServerStart: "a"}},
		{"epoch", session.State{Epoch: "two", ProjectRevision: 3, Generation: 5, ServerStart: "a"}},
		{"generation", session.State{Epoch: "one", ProjectRevision: 3, Generation: 6, ServerStart: "a"}},
		{"restart", session.State{Epoch: "one", ProjectRevision: 3, Generation: 5, ServerStart: "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{state: first, network: sim.Network{Stations: []sim.Station{{ID: "z", Name: "Zulu"}, {ID: "a", Name: "Alpha"}}}}
			index := game.displayIndex()
			if game.journeyStations()[0].ID != "a" || game.displayIndex() != index {
				t.Fatal("unchanged identity rebuilt or reordered catalog")
			}
			game.network = sim.Network{Stations: []sim.Station{{ID: "z", Name: "Aardvark"}, {ID: "a", Name: "Alpha", ParkingOnly: true}}}
			game.state = tc.state
			got := game.journeyStations()
			if len(got) != 1 || got[0].Name != "Aardvark" || game.displayIndex() == index {
				t.Fatal("network change retained old catalog")
			}
			if matches := game.displayIndex().journeys.matches("zulu"); len(matches) != 0 {
				t.Fatal("network change retained old search keys")
			}
		})
	}
}

func BenchmarkJourneyStationCatalog(b *testing.B) {
	network := scenarios.LondonFull().Network
	for _, query := range []string{"", "arc", "CHX"} {
		for _, cached := range []bool{false, true} {
			name := "original/" + query
			if cached {
				name = "cached/" + query
			}
			b.Run(name, func(b *testing.B) {
				game := &Game{network: network}
				game.journeySearch.filter, game.journeySearch.query[0] = 1, query
				game.displayIndex()
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					var got []sim.Station
					if cached {
						got = game.journeyStations()
					} else {
						got = originalJourneyStations(network, query)
					}
					if query == "" && len(got) != 269 || query != "" && len(got) == 0 {
						b.Fatal("unexpected catalog result")
					}
				}
			})
		}
	}
}
