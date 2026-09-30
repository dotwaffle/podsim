package view

import (
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestJourneyStationNamesAndCodes(t *testing.T) {
	t.Parallel()
	game := journeyNetworkGame(t, scenarios.LondonFull().Network)
	stations := game.journeyStations()
	if stations[0].Name != "Acton Town" {
		t.Fatalf("first station: %s", stations[0].Name)
	}
	if !slices.IsSortedFunc(stations, func(a, b sim.Station) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }) {
		t.Fatal("stations not alphabetic")
	}
	for _, tc := range []struct{ query, id string }{
		{"chx", "940GZZLUCHX"}, {"Charing Cross", "940GZZLUCHX"}, {"charing", "940GZZLUCHX"},
		{"BPS", "940GZZBPSUST"}, {"nel", "940GZZNEUGST"}, {"940GZZLUCHX", "940GZZLUCHX"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			matches := newJourneyCatalog(stations).matches(tc.query)
			if len(matches) != 1 || matches[0].ID != tc.id {
				t.Fatalf("matches for %q: %+v", tc.query, matches)
			}
		})
	}
}

func TestJourneySearchRejectsUnresolvedOrders(t *testing.T) {
	t.Parallel()
	game := journeyNetworkGame(t, scenarios.LondonFull().Network)
	game.startJourneySearch(1)
	game.searchJourney("Ruis")
	if !game.journeySearch.unresolved[0] || !findButton(t, game.buttons(), "request").disabled {
		t.Fatal("ambiguous station accepted")
	}
	game.request()
	if game.pending {
		t.Fatal("unresolved order sent")
	}
	game.searchJourney("No station has this name")
	if len(game.stationPages()) != 0 || len(game.journeyButtons(false)) != 2 {
		t.Fatal("no-match fields or chips incorrect")
	}
	game.normalizeSelection()
	if game.stationPage < 0 {
		t.Fatal("negative station page")
	}
	game.searchJourney("CHX")
	if game.journeySearch.unresolved[0] || game.origin != "940GZZLUCHX" {
		t.Fatal("code not resolved")
	}
	game.startJourneySearch(2)
	game.searchJourney("BPS")
	if game.journeySearch.unresolved[1] || game.destination != "940GZZBPSUST" {
		t.Fatal("destination not resolved")
	}
	if findButton(t, game.buttons(), "request").disabled {
		t.Fatal("resolved order disabled")
	}
	game.searchJourney("No station")
	game.chooseJourneyStation(2, "940GZZNEUGST")
	if game.journeySearch.unresolved[1] || game.journeySearch.focus != 0 {
		t.Fatal("chip selection did not clear search")
	}
}

func TestJourneySearchDoesNotGuessAmbiguousCodes(t *testing.T) {
	t.Parallel()
	stations := []sim.Station{{ID: "940GZZLUBNK", Name: "Bank"}, {ID: "BNK", Name: "Other"}}
	if got := newJourneyCatalog(stations).matches("bnk"); len(got) != 2 {
		t.Fatalf("ambiguous code matched %d", len(got))
	}
	if got := stationCode("custom-CHX"); got != "" {
		t.Fatalf("invented custom station code %q", got)
	}
}

func TestJourneyResultClickKeepsFilterAfterBlur(t *testing.T) {
	t.Parallel()
	game := journeyNetworkGame(t, scenarios.LondonFull().Network)
	game.startJourneySearch(1)
	game.searchJourney("arc")
	result := findButton(t, game.buttons(), "from/940GZZLUACY")
	// A native input blurs before the canvas handles the same pointer press.
	game.journeySearch.focus = 0
	game.click(centerOfButton(result))
	if game.origin != "940GZZLUACY" {
		t.Fatalf("clicked Archway but selected %s", game.origin)
	}
	if game.journeySearch.filter != 0 {
		t.Fatal("selection retained the old filter")
	}
}
