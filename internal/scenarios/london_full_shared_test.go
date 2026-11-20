package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonFullSharedExpress checks the static results of the shared-track
// preset (docs/design/shared-track-preset.md, sections 3.5, 3.7, and 4):
// project validation and the bank audit pass, with 47 shape pairs, the
// checks of cmd/projectcheck give one tail warning, Express runs on the
// shared route in each direction, and the 16 Express pods start in the
// Express parking.
func TestLondonFullSharedExpress(t *testing.T) {
	t.Parallel()
	base := LondonFull()
	config, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	if err = project.Validate(config); err != nil {
		t.Fatal(err)
	}
	// With the index for each lane, the audit refuses no pair. The comb
	// and its roads add 9 shape pairs to the 38 of section 3.5.
	shapes, err := n.BankGeometryShapePairs()
	if err != nil || shapes != 47 {
		t.Fatalf("bank audit: %d shape pairs, error %v, want 47 and no error", shapes, err)
	}
	if err := n.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
	warnings := project.TailWarnings(n)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Text, "940GZZLUBNK-move-08-02 ") {
		t.Fatalf("tail warnings %v, want one for 940GZZLUBNK-move-08-02", warnings)
	}
	// Two junction lanes go, and the comb adds 65 lanes and two roads.
	if len(n.Lanes) != len(base.Network.Lanes)-2+67 {
		t.Fatalf("%d lanes, want %d", len(n.Lanes), len(base.Network.Lanes)-2+67)
	}
	for _, id := range sharedTrackPresetGeometry().remove {
		if slices.ContainsFunc(n.Lanes, func(lane sim.Lane) bool { return lane.ID == id }) {
			t.Fatalf("lane %s is not removed", id)
		}
	}
	if config.Name != "LondonFull Shared Express" || config.OrderContract != sim.ExpressOrderContract || len(config.ExpressServices) != 2 {
		t.Fatalf("name %q, contract %q, %d services", config.Name, config.OrderContract, len(config.ExpressServices))
	}
	if len(config.Fleet) != len(base.Fleet)+expressPods || !slices.Equal(config.Fleet[:len(base.Fleet)], base.Fleet) {
		t.Fatalf("a fleet of %d pods, want the %d pods of LondonFull and %d Express pods", len(config.Fleet), len(base.Fleet), expressPods)
	}
	if _, err := sim.NewFleetWithOrderContract(n, config.Fleet, config.OrderContract); err != nil {
		t.Fatal(err)
	}
	if gaps := project.ParkingGaps(config); len(gaps) != 0 {
		t.Fatalf("%d parking gaps, the first %+v", len(gaps), gaps[0])
	}
	waterloo, _ := n.Station(expressWaterloo)
	bank, _ := n.Station(expressBank)
	for i, pair := range [][2]sim.Station{{waterloo, bank}, {bank, waterloo}} {
		route, err := n.RouteForClass(pair[0].Exit, pair[1].Entry, sim.ExpressClass)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(route))
		for _, lane := range route {
			ids = append(ids, lane.ID)
		}
		if want := sharedTrackRoute[6*i : 6*i+6]; !slices.Equal(ids, want) {
			t.Fatalf("Express route from %s to %s is %v, want %v", pair[0].ID, pair[1].ID, ids, want)
		}
	}
}

// TestLondonFullSharedExpressParking checks the Express parking of the
// preset (section 3.7 of the design, site P4): a comb of 16 berths with
// parts of 41 m at the measured site, its roads to the portals of link 100
// at Waterloo, one Express pod at each berth, and an Express route from
// the comb to each hub and back.
func TestLondonFullSharedExpressParking(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	parking, ok := n.Station(expressParking)
	express, _ := sim.NewClassSet("express")
	if !ok || !parking.ParkingOnly || parking.VehicleClasses != express || len(parking.Berths) != expressPods {
		t.Fatalf("parking %v: parking only %v, classes %v, %d berths", ok, parking.ParkingOnly, parking.VehicleClasses, len(parking.Berths))
	}
	// The berth nodes are on the axis of the comb from the middle of the
	// entry and the exit, at the heading and the pitch of the site.
	site := sharedTrackPresetParking()
	if site.part != 41 || site.heading != -35 || site.center != (sim.Point{X: 1000, Y: 40}) {
		t.Fatalf("site %+v, want the site P4 of section 3.7", site)
	}
	positions := make(map[string]sim.Point, len(n.Nodes))
	for _, node := range n.Nodes {
		positions[node.ID] = node.Position
	}
	entry, exit := positions[parking.Entry], positions[parking.Exit]
	center := sim.Point{X: (entry.X + exit.X) / 2, Y: (entry.Y + exit.Y) / 2}
	if math.Hypot(center.X-site.center.X, center.Y-site.center.Y) > 1e-9 {
		t.Fatalf("the comb is centered at %v, want %v", center, site.center)
	}
	heading := site.heading * math.Pi / 180
	for i, berth := range parking.Berths {
		want := sim.Point{X: center.X + float64(i+1)*site.part*math.Cos(heading), Y: center.Y + float64(i+1)*site.part*math.Sin(heading)}
		if got := positions[berth.Node]; berth.ID != expressCombBerth(i) || math.Hypot(got.X-want.X, got.Y-want.Y) > 1e-9 {
			t.Fatalf("berth %d is %s at %v, want %s at %v", i, berth.ID, got, expressCombBerth(i), want)
		}
	}
	lanes := 0
	for _, lane := range n.Lanes {
		if lane.StationID != expressParking {
			continue
		}
		lanes++
		if lane.VehicleClasses != express {
			t.Fatalf("lane %s has classes %v", lane.ID, lane.VehicleClasses)
		}
		want := site.part
		switch lane.ID {
		case "xp-through":
			want = 2 * site.part
		case expressParking + "-road-in-01", expressParking + "-road-out-01":
			continue
		}
		if length := n.Length(lane); math.Abs(length-want) > 1e-9 {
			t.Fatalf("lane %s is %g m, want %g m", lane.ID, length, want)
		}
	}
	if lanes != 67 {
		t.Fatalf("%d lanes of the parking, want 67", lanes)
	}
	roads := map[string][2]string{
		expressParking + "-road-in-01":  {sharedTrackDeparturePortal, parking.Entry},
		expressParking + "-road-out-01": {parking.Exit, sharedTrackArrivalPortal},
	}
	for id, ends := range roads {
		index := slices.IndexFunc(n.Lanes, func(lane sim.Lane) bool { return lane.ID == id })
		if index < 0 || n.Lanes[index].From != ends[0] || n.Lanes[index].To != ends[1] {
			t.Fatalf("road %s is missing or does not join %s to %s", id, ends[0], ends[1])
		}
	}
	berths := make(map[string]bool)
	for _, placement := range config.Fleet {
		if placement.Class != sim.ExpressClass {
			continue
		}
		if placement.StationID != expressParking || berths[placement.BerthID] {
			t.Fatalf("Express pod %s starts at %s %s", placement.ID, placement.StationID, placement.BerthID)
		}
		berths[placement.BerthID] = true
	}
	if len(berths) != expressPods {
		t.Fatalf("%d Express pods in the comb, want %d", len(berths), expressPods)
	}
	// Measured in section 3.7: 654.0 m and 693.0 m to and from Waterloo,
	// 2,951.9 m and 2,925.6 m to and from Bank.
	waterloo, _ := n.Station(expressWaterloo)
	bank, _ := n.Station(expressBank)
	for _, test := range []struct {
		from, to string
		limit    float64
	}{
		{parking.Berths[0].Node, waterloo.Berths[0].Node, 1000},
		{waterloo.Berths[0].Node, parking.Berths[0].Node, 1000},
		{parking.Berths[0].Node, bank.Berths[0].Node, 3000},
		{bank.Berths[0].Node, parking.Berths[0].Node, 3000},
	} {
		route, err := n.RouteForClass(test.from, test.to, sim.ExpressClass)
		if err != nil {
			t.Fatalf("no Express route from %s to %s: %v", test.from, test.to, err)
		}
		length := 0.0
		for _, lane := range route {
			length += n.Length(lane)
		}
		if length > test.limit {
			t.Fatalf("the Express route from %s to %s is %.1f m, want at most %g m", test.from, test.to, length, test.limit)
		}
	}
}

// expressCombBerth returns the ID of berth i of the Express comb.
func expressCombBerth(i int) string {
	return fmt.Sprintf("xp-%02d", i+1)
}

// TestLondonFullSharedExpressLayout checks that the layout audit accepts
// the preset only because the Express parking is a Parking facility of
// Waterloo for this preset. Without that entry, the road of the comb to
// the arrival portal of link 100 crosses the Waterloo road-in-05.
func TestLondonFullSharedExpressLayout(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	if conflicts := auditLondonLayout(n, sharedTrackLayoutInput(source)); len(conflicts) != 0 {
		t.Fatalf("%d conflicts, the first: %s", len(conflicts), conflicts[0].describe(n))
	}
	conflicts := auditLondonLayout(n, newLondonAuditInput(source))
	want := `station Waterloo (940GZZLUWLO): lane "940GZZLUWLO-road-in-05" crosses Express Parking lane "express-parking-road-out-01"`
	if len(conflicts) != 1 || conflicts[0].soft || conflicts[0].describe(n) != want {
		t.Fatalf("conflicts without the Parking facility %v, want one: %s", conflicts, want)
	}
	if input := newLondonAuditInput(source); input.gateways[expressParking] != "" {
		t.Fatalf("the London audit input has the gateway %q for %s", input.gateways[expressParking], expressParking)
	}
}

// TestLondonFullSharedExpressKeepsPresets checks that the shared-track
// preset changes neither LondonFull nor project D (decision SX1). It builds
// the preset first, from the cached LondonFull and with the generator, so
// that a change to a shared base fails. The construction before the
// Express parking keeps the network of variant 3.5 of the shared-track
// study.
func TestLondonFullSharedExpressKeepsPresets(t *testing.T) {
	t.Parallel()
	cached, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateLondonFullSharedExpress()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated, cached) {
		t.Fatal("GenerateLondonFullSharedExpress differs from the preset built on the cached LondonFull")
	}
	before, err := londonFullSharedExpress(LondonFull(), sharedTrackPresetGeometry())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		network sim.Network
		want    string
	}{
		{"LondonFull", LondonFull().Network, "69baad6a8077b3f22c1e250023b4eaeefba10f1ff9bb0812b773634951d84225"},
		{"LondonFull Express", LondonFullExpress().Network, "3aa0a2eb9e99eab4ea80ecfad481c7718b57cd38e0b35876b44b5ab5dc1d22e5"},
		// The network of variant 3.5 of the shared-track study in
		// internal/sim, which builds its own copy of this construction.
		{"LondonFull Shared Express before the parking", before.Network, "09b8fdea19e1b87e438fe2539878e3bf232eaa13af5e4e954216919ff67614df"},
		{"LondonFull Shared Express", generated.Network, "5961c7021dcc4a13712cc8006bd060f94e25d38c4eec295056d6ee5cb9f8c687"},
	} {
		data, err := jsonv2.Marshal(test.network, json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != test.want {
			t.Errorf("%s network SHA-256 %s, want %s", test.name, got, test.want)
		}
	}
}

// TestLondonFullSharedExpressRoutes is the route evidence of row SX2: each
// hub berth admits Express, and an Express route goes from the node of each
// berth of each hub to the node of each berth of the other hub,
// in both directions, over the lanes of link 100, and an Express route from
// each berth of the Express parking to each hub berth and back.
func TestLondonFullSharedExpressRoutes(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	waterloo, _ := n.Station(expressWaterloo)
	bank, _ := n.Station(expressBank)
	parking, _ := n.Station(expressParking)
	// longest is the Express route with the most lanes, for the route caps.
	var longest []string
	route := func(from, to sim.Berth) []string {
		t.Helper()
		lanes, err := n.RouteForClass(from.Node, to.Node, sim.ExpressClass)
		if err != nil {
			t.Fatalf("no Express route from %s to %s: %v", from.ID, to.ID, err)
		}
		ids := make([]string, 0, len(lanes))
		for _, lane := range lanes {
			ids = append(ids, lane.ID)
		}
		if len(ids) > len(longest) {
			longest = ids
		}
		return ids
	}
	hubs := 0
	for _, pair := range [][2]sim.Station{{waterloo, bank}, {bank, waterloo}} {
		// The link 100 lanes of the direction, from the shared route.
		link := sharedTrackRoute[2:4]
		if pair[0].ID == expressBank {
			link = sharedTrackRoute[8:10]
		}
		for _, from := range pair[0].Berths {
			if !from.VehicleClasses.Allows(string(sim.ExpressClass)) {
				t.Fatalf("hub berth %s does not admit Express", from.ID)
			}
			for _, to := range pair[1].Berths {
				if ids := route(from, to); !slices.Contains(ids, link[0]) || !slices.Contains(ids, link[1]) {
					t.Fatalf("the Express route from %s to %s does not take %v: %v", from.ID, to.ID, link, ids)
				}
				hubs++
			}
		}
	}
	parked := 0
	for _, berth := range parking.Berths {
		for _, hub := range []sim.Station{waterloo, bank} {
			for _, other := range hub.Berths {
				route(berth, other)
				route(other, berth)
				parked += 2
			}
		}
	}
	t.Logf("%d Waterloo berths, %d Bank berths, %d routes between the hubs, %d routes to and from the %d parking berths", len(waterloo.Berths), len(bank.Berths), hubs, parked, len(parking.Berths))
	if len(waterloo.Berths) == 0 || len(bank.Berths) == 0 || len(parking.Berths) != expressPods {
		t.Fatal("the hubs have no berths, or the parking is not the comb")
	}
	t.Logf("the longest Express route has %d lanes, from %s to %s", len(longest), longest[0], longest[len(longest)-1])
	if len(longest) > min(sim.MaxSavedRouteLanes, sim.MotionRouteLimit) {
		t.Fatalf("the longest Express route has %d lanes, more than the route caps", len(longest))
	}
}
