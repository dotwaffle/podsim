package sim

import (
	"math"
	"testing"
)

// TestPlannedRiderDetourMaximum pins the largest ratio of the riders of a
// pod that boarded at one berth. The pins are the values of the removed
// all-stop plan, which took each stop as the destination of a rider.
func TestPlannedRiderDetourMaximum(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, origin string
		network      Network
		stops        []string
		berthID      string
		want         float64
	}{
		{"ordinary", "harbor-berth", Example(), []string{"garden", "market"}, "", 1.1986218556888293},
		{"multiple berths", "harbor-berth", ladderNetwork(), []string{"garden", "market"}, "", 1.1978392041556665},
		{"chosen berth", "harbor-berth", ladderNetwork(), []string{"market"}, "market-2", 1},
		{"bank entry", "origin-berth", BankExample(), []string{"hub", "origin"}, "", 1.0393082896036741},
		{"chosen bank berth", "origin-berth", BankExample(), []string{"hub", "origin"}, "bank-b-1", 1.0393082896036741},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(test.network, []Placement{{ID: "01", StationID: test.network.Stations[0].ID}})
			if err != nil {
				t.Fatal(err)
			}
			for _, class := range []VehicleClass{LegacyClass, CompactClass} {
				station, _ := s.station(test.stops[0])
				route, err := s.stationApproachForStops(test.origin, test.stops, class)
				if err != nil {
					t.Fatal(err)
				}
				start := detourStart{class: class, from: test.origin, ridden: s.lanesMeters(route), entry: station.routeEntry(route, Berth{})}
				if test.berthID != "" {
					start.berth, _ = station.berth(test.berthID)
					start.ridden = s.directDistanceForClass(test.origin, station.ID, start.berth, class)
				}
				largest := 1.0
				for _, destination := range test.stops {
					rider := riderDetour{origin: test.origin, destination: destination}
					largest = max(largest, s.plannedRiderDetour(rider, test.stops, start))
				}
				if math.Abs(largest-test.want) > 1e-12 {
					t.Fatalf("class %s: rider maximum %.17g, want %.17g", class, largest, test.want)
				}
			}
		})
	}
}

func TestPlannedRiderDetourOriginsAndBaselines(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	garden, _ := s.station("garden")
	berth := garden.Berths[0]
	first := s.directDistance("harbor-berth", "garden", berth)
	route, err := s.stationApproachForStops(berth.Node, []string{"market"}, LegacyClass)
	if err != nil {
		t.Fatal(err)
	}
	market, _ := s.station("market")
	path, err := s.stationPath(market.Entry, market.Berths[0].Node)
	if err != nil {
		t.Fatal(err)
	}
	remaining := s.lanesMeters(route) + s.lanesMeters(path)
	start := detourStart{ridden: first, berth: berth}
	for _, test := range []struct {
		name  string
		rider riderDetour
		want  float64
	}{
		{"earlier alight", riderDetour{origin: "harbor-berth", destination: "garden"}, 1},
		{"original rider", riderDetour{origin: "harbor-berth", destination: "market"}, (first + remaining) / s.directDistance("harbor-berth", "market", market.Berths[0])},
		{"new origin", riderDetour{origin: berth.Node, destination: "market", baseline: first}, 1},
		{"new baseline", riderDetour{origin: berth.Node, destination: "market", baseline: first - 50}, (remaining + 50) / remaining},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Each case owns its route caches.
			local, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
			if err != nil {
				t.Fatal(err)
			}
			got := local.plannedRiderDetour(test.rider, []string{"garden", "market"}, start)
			if math.Abs(got-test.want) > 1e-12 {
				t.Fatalf("ratio %g, want %g", got, test.want)
			}
		})
	}
}

func TestPlannedRiderDetourChecksOnlyDestination(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	route, err := s.stationApproachForStops("harbor-berth", []string{"garden", "market"}, LegacyClass)
	if err != nil {
		t.Fatal(err)
	}
	start := detourStart{ridden: s.lanesMeters(route) + 1000}
	rider := riderDetour{origin: "harbor-berth", destination: "market"}
	got := s.plannedRiderDetour(rider, []string{"garden", "market"}, start)
	earlier := s.plannedRiderDetour(riderDetour{origin: rider.origin, destination: "garden"}, []string{"garden", "market"}, start)
	if !finite(got) || got <= 1 || got >= earlier {
		t.Fatalf("destination ratio %g, ratio at the earlier stop %g", got, earlier)
	}
}

func TestPlannedRiderDetourRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		rider riderDetour
		start detourStart
	}{
		{"missing origin", riderDetour{destination: "market"}, detourStart{ridden: 100}},
		{"missing destination", riderDetour{origin: "harbor-berth"}, detourStart{ridden: 100}},
		{"absent destination", riderDetour{origin: "harbor-berth", destination: "garden"}, detourStart{ridden: 100}},
		{"negative baseline", riderDetour{origin: "harbor-berth", destination: "market", baseline: -1}, detourStart{ridden: 100}},
		{"future baseline", riderDetour{origin: "harbor-berth", destination: "market", baseline: 101}, detourStart{ridden: 100}},
		{"nan baseline", riderDetour{origin: "harbor-berth", destination: "market", baseline: math.NaN()}, detourStart{ridden: 100}},
		{"infinite cumulative", riderDetour{origin: "harbor-berth", destination: "market"}, detourStart{ridden: math.Inf(1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
			if err != nil {
				t.Fatal(err)
			}
			if got := s.plannedRiderDetour(test.rider, []string{"market"}, test.start); !math.IsInf(got, 1) {
				t.Fatalf("invalid input ratio %g", got)
			}
		})
	}
}

func TestPlannedRiderDetourBankSelection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		from, entry string
		berthID     string
		wantInvalid bool
	}{
		{name: "actual source", from: "origin-berth"},
		{name: "selected second bank", entry: "bank-b-entry"},
		{name: "chosen second bank berth", berthID: "bank-b-1"},
		{name: "missing selected approach", wantInvalid: true},
		{name: "unreachable actual source", from: "missing", wantInvalid: true},
		{name: "unreachable selected entry", entry: "missing", wantInvalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(BankExample(), []Placement{{ID: "01", StationID: "origin"}})
			if err != nil {
				t.Fatal(err)
			}
			station, _ := s.station("hub")
			start := detourStart{from: test.from, entry: test.entry}
			if test.berthID != "" {
				start.berth, _ = station.berth(test.berthID)
				start.ridden = s.directDistance("origin-berth", "hub", start.berth)
			} else {
				entry := test.entry
				if entry == "" {
					entry = "bank-a-entry"
				}
				if route, err := s.route("origin-berth", entry); err == nil {
					start.ridden = s.lanesMeters(route)
				}
			}
			start.ridden += 50
			rider := riderDetour{origin: "origin-berth", destination: "hub", baseline: 50}
			got := s.plannedRiderDetour(rider, []string{"hub"}, start)
			if test.wantInvalid {
				if !math.IsInf(got, 1) {
					t.Fatalf("invalid bank plan ratio %g", got)
				}
			} else if math.Abs(got-1) > 1e-12 {
				t.Fatalf("direct bank plan ratio %g", got)
			}
		})
	}
}

func TestPlannedRiderDetourActualClassContinuation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		class       VehicleClass
		berthID     string
		wantInvalid bool
	}{
		{name: "legacy chooses continuing berth", class: LegacyClass},
		{name: "legacy chosen berth cannot continue", class: LegacyClass, berthID: "garden-1", wantInvalid: true},
		{name: "compact chosen berth can continue", class: CompactClass, berthID: "garden-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(continuationNetwork(t), []Placement{{ID: "01", StationID: "harbor"}})
			if err != nil {
				t.Fatal(err)
			}
			stops := []string{"garden", "market"}
			route, err := s.stationApproachForStops("harbor-berth", stops, test.class)
			if err != nil {
				t.Fatal(err)
			}
			start := detourStart{class: test.class, ridden: s.lanesMeters(route)}
			if test.berthID != "" {
				station, _ := s.station("garden")
				start.berth, _ = station.berth(test.berthID)
				start.ridden = s.directDistanceForClass("harbor-berth", "garden", start.berth, test.class)
			}
			rider := riderDetour{origin: "harbor-berth", destination: "market"}
			got := s.plannedRiderDetour(rider, stops, start)
			if math.IsInf(got, 1) != test.wantInvalid || math.IsNaN(got) {
				t.Fatalf("class %s, berth %s: ratio %g", test.class, test.berthID, got)
			}
		})
	}
}

func TestPlannedRiderDetourConservativeBerths(t *testing.T) {
	t.Parallel()
	network := continuationNetwork(t)
	for index := range network.Nodes {
		if network.Nodes[index].ID == "garden-berth-2" {
			network.Nodes[index].Position.Y = -800
		}
	}
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	stops := []string{"garden", "market"}
	route, err := s.stationApproachForStops("harbor-berth", stops, CompactClass)
	if err != nil {
		t.Fatal(err)
	}
	start := detourStart{class: CompactClass, ridden: s.lanesMeters(route)}
	garden, _ := s.station("garden")
	market, _ := s.station("market")
	target := market.Berths[0]
	want := 1.0
	for _, berth := range garden.Berths {
		inlet, err := s.stationPathForClass(garden.Entry, berth.Node, CompactClass)
		if err != nil {
			t.Fatal(err)
		}
		onward := s.directDistanceForClass(berth.Node, "market", target, CompactClass)
		direct := s.directDistanceForClass("harbor-berth", "market", target, CompactClass)
		want = max(want, (start.ridden+s.lanesMeters(inlet)+onward)/direct)
	}
	rider := riderDetour{origin: "harbor-berth", destination: "market"}
	if got := s.plannedRiderDetour(rider, stops, start); want <= maxSharedRideDetour || math.Abs(got-want) > 1e-12 {
		t.Fatalf("conservative ratio %g, want %g", got, want)
	}
}

func TestPlannedRiderDetourBankBoardingOrigin(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(BankExample(), []Placement{{ID: "01", StationID: "origin"}})
	if err != nil {
		t.Fatal(err)
	}
	station, _ := s.station("hub")
	berth, _ := station.berth("bank-b-1")
	baseline := s.directDistance("origin-berth", "hub", berth)
	rider := riderDetour{origin: berth.Node, destination: "origin", baseline: baseline}
	start := detourStart{ridden: baseline, berth: berth}
	if got := s.plannedRiderDetour(rider, []string{"hub", "origin"}, start); math.Abs(got-1) > 1e-12 {
		t.Fatalf("new bank origin ratio %g", got)
	}
}
