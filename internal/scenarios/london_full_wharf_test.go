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

// TestLondonFullSharedExpressWharf checks the static results of the preset
// (docs/design/london-hubs-d8-d9.md, section 9): project validation, the
// bank audit with 53 shape pairs, the one tail warning of the first line,
// the fleet, and the parking gaps.
func TestLondonFullSharedExpressWharf(t *testing.T) {
	t.Parallel()
	base, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	config, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if shapes, err := n.BankGeometryShapePairs(); err != nil || shapes != 53 {
		t.Fatalf("bank audit: %d shape pairs, error %v, want 53 and no error", shapes, err)
	}
	if err := n.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
	warnings := project.TailWarnings(n)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Text, "940GZZLUBNK-move-08-02 ") {
		t.Fatalf("tail warnings %v, want one for 940GZZLUBNK-move-08-02", warnings)
	}
	// The comb adds 65 lanes and two roads, and no lane goes.
	if len(n.Lanes) != len(base.Network.Lanes)+67 {
		t.Fatalf("%d lanes, want %d", len(n.Lanes), len(base.Network.Lanes)+67)
	}
	if config.Name != "LondonFull Shared Express Wharf" || config.OrderContract != sim.ExpressOrderContract || len(config.ExpressServices) != 4 {
		t.Fatalf("name %q, contract %q, %d services", config.Name, config.OrderContract, len(config.ExpressServices))
	}
	if len(config.Fleet) != len(base.Fleet)+expressPods || !slices.Equal(config.Fleet[:len(base.Fleet)], base.Fleet) {
		t.Fatalf("a fleet of %d pods, want the %d pods of the base and %d Express pods", len(config.Fleet), len(base.Fleet), expressPods)
	}
	ids := make(map[string]bool)
	for _, pod := range config.Fleet {
		if ids[pod.ID] {
			t.Fatalf("two pods have the ID %s", pod.ID)
		}
		ids[pod.ID] = true
	}
	if _, err := sim.NewFleetWithOrderContract(n, config.Fleet, config.OrderContract); err != nil {
		t.Fatal(err)
	}
	if gaps := project.ParkingGaps(config); len(gaps) != 0 {
		t.Fatalf("%d parking gaps, the first %+v", len(gaps), gaps[0])
	}
}

// TestLondonFullSharedExpressWharfRoutes checks the routes of row HX2: each
// service takes its shared route, and each berth of a hub has an Express
// route to and from each berth of the other hub and of the comb.
func TestLondonFullSharedExpressWharfRoutes(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	station := func(id string) sim.Station {
		s, ok := n.Station(id)
		if !ok {
			t.Fatalf("no station %s", id)
		}
		return s
	}
	for _, route := range append(sharedTrackPresetRoutes(), wharfRoutes()...) {
		lanes, err := n.RouteForClass(station(route.from).Exit, station(route.to).Entry, sim.ExpressClass)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(lanes))
		for _, lane := range lanes {
			ids = append(ids, lane.ID)
		}
		if !slices.Equal(ids, route.lanes) {
			t.Fatalf("Express route from %s to %s is %v, want %v", route.from, route.to, ids, route.lanes)
		}
	}
	for _, pair := range [][2]string{
		{expressWaterloo, expressBank}, {expressWaterloo, expressParking}, {expressBank, expressParking},
		{expressLondonBridge, expressCanaryWharf}, {expressLondonBridge, expressWharfParking}, {expressCanaryWharf, expressWharfParking},
	} {
		for _, a := range station(pair[0]).Berths {
			for _, b := range station(pair[1]).Berths {
				for _, ends := range [][2]string{{a.Node, b.Node}, {b.Node, a.Node}} {
					if _, err := n.RouteForClass(ends[0], ends[1], sim.ExpressClass); err != nil {
						t.Errorf("no Express route from %s to %s: %v", ends[0], ends[1], err)
					}
				}
			}
		}
	}
	// The two lines share no Express route, so each comb serves one line
	// (decision HB-D11 does not matter here).
	for _, pair := range [][2]string{{expressWaterloo, expressLondonBridge}, {expressLondonBridge, expressWaterloo}, {expressParking, expressLondonBridge}, {expressWharfParking, expressWaterloo}} {
		if _, err := n.RouteForClass(station(pair[0]).Exit, station(pair[1]).Entry, sim.ExpressClass); err == nil {
			t.Errorf("an Express route from %s to %s", pair[0], pair[1])
		}
	}
}

// TestLondonFullSharedExpressWharfParking checks the comb of site LB3
// (decision HB-D14): its place, its lanes and classes, its roads, and one
// Express pod at each berth. The study site and the preset agree.
func TestLondonFullSharedExpressWharfParking(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	site := wharfParking()
	study := hubPairSites[2]
	if study.name != "LB3" || site.center != study.center || site.heading != study.heading || site.mirror != study.mirror || site.gateway != "" ||
		site.arrival != study.link+"-arrival" || site.departure != study.link+"-departure" || site.part != 41 {
		t.Fatalf("site %+v, want the study site %+v", site, study)
	}
	parking, ok := n.Station(expressWharfParking)
	express, _ := sim.NewClassSet("express")
	if !ok || !parking.ParkingOnly || parking.VehicleClasses != express || len(parking.Berths) != expressPods {
		t.Fatalf("parking %v: parking only %v, classes %v, %d berths", ok, parking.ParkingOnly, parking.VehicleClasses, len(parking.Berths))
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
		if got := positions[berth.Node]; berth.ID != fmt.Sprintf("xq-%02d", i+1) || math.Hypot(got.X-want.X, got.Y-want.Y) > 1e-9 {
			t.Fatalf("berth %d is %s at %v, want %v", i, berth.ID, got, want)
		}
	}
	roads := map[string][2]string{
		expressWharfParking + "-road-in-01":  {site.departure, parking.Entry},
		expressWharfParking + "-road-out-01": {parking.Exit, site.arrival},
	}
	lanes := 0
	for _, lane := range n.Lanes {
		if lane.StationID != expressWharfParking {
			continue
		}
		lanes++
		if lane.VehicleClasses != express {
			t.Fatalf("lane %s has classes %v", lane.ID, lane.VehicleClasses)
		}
		if ends, ok := roads[lane.ID]; ok {
			if lane.From != ends[0] || lane.To != ends[1] {
				t.Fatalf("road %s joins %s to %s, want %v", lane.ID, lane.From, lane.To, ends)
			}
			delete(roads, lane.ID)
			continue
		}
		want := site.part
		if lane.ID == "xq-through" {
			want = 2 * site.part
		}
		if length := n.Length(lane); math.Abs(length-want) > 1e-9 {
			t.Fatalf("lane %s is %g m, want %g m", lane.ID, length, want)
		}
	}
	if lanes != 67 || len(roads) != 0 {
		t.Fatalf("%d lanes of the parking and no road %v, want 67 lanes", lanes, roads)
	}
	berths := make(map[string]int)
	for _, placement := range config.Fleet {
		if placement.Class == sim.ExpressClass {
			berths[placement.StationID]++
		}
	}
	if berths[expressParking] != expressPods || berths[expressWharfParking] != expressPods || len(berths) != 2 {
		t.Fatalf("Express pods by station %v, want %d at each comb", berths, expressPods)
	}
}

// TestLondonFullSharedExpressWharfHubs checks the hubs of the new line
// (HB-D8 and HB-D13): each road of London Bridge and Canary Wharf keeps its
// chord at 14 m/s, except the curved London Bridge road-out-01 at 14 m/s,
// and no lane of the network goes.
func TestLondonFullSharedExpressWharfHubs(t *testing.T) {
	t.Parallel()
	base, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	config, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	after := make(map[string]sim.Lane, len(config.Network.Lanes))
	roads := 0
	for _, lane := range config.Network.Lanes {
		after[lane.ID] = lane
		if lane.StationID != expressLondonBridge && lane.StationID != expressCanaryWharf || !strings.Contains(lane.ID, "-road-") {
			continue
		}
		roads++
		if curved := lane.ID == expressLondonBridge+"-road-out-01"; (lane.Control != nil) != curved || lane.SpeedLimit != speedLimit {
			t.Errorf("road %s has control %v and limit %g", lane.ID, lane.Control, lane.SpeedLimit)
		}
	}
	if roads == 0 {
		t.Fatal("no hub road")
	}
	for _, lane := range base.Network.Lanes {
		got, ok := after[lane.ID]
		if !ok {
			t.Fatalf("lane %s is removed", lane.ID)
		}
		first := lane.StationID == expressWaterloo || lane.StationID == expressBank || lane.StationID == expressParking
		if first && !reflect.DeepEqual(lane, got) {
			t.Fatalf("lane %s of the first line changed", lane.ID)
		}
	}
}

// TestLondonFullSharedExpressWharfKeepsPresets checks row HX1: the preset
// changes no other preset, the generator and the cached base agree, and the
// network of the preset is pinned.
func TestLondonFullSharedExpressWharfKeepsPresets(t *testing.T) {
	t.Parallel()
	cached, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateLondonFullSharedExpressWharf()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated, cached) {
		t.Fatal("GenerateLondonFullSharedExpressWharf differs from the preset built on the cached LondonFull")
	}
	shared, err := GenerateLondonFullSharedExpress()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		network sim.Network
		want    string
	}{
		{"LondonFull", LondonFull().Network, "03d04786f72780fe4e0e1a65ee90eefcb8e20fc78ddd0c2b0600556cbea79ec5"},
		{"LondonFull Express", LondonFullExpress().Network, "3aa0a2eb9e99eab4ea80ecfad481c7718b57cd38e0b35876b44b5ab5dc1d22e5"},
		{"LondonFull Shared Express", shared.Network, "cba69149281760dbba334323497b9d94cb1d3a980a631b68de0be45e78aac6bb"},
		{"LondonFull Shared Express Wharf", cached.Network, "d68a69b8c326f292f7c3a7839b51bf9837c5e714287f60470436781705217359"},
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
