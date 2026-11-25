package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestWharfHubsPreset checks the static results of the preset
// (docs/design/london-hubs-d8-d9.md, section 13): project validation, the
// bank audit with 67 shape pairs, the tail warnings, the services, and the
// parking gaps. It also checks that the preset is the wharf preset with
// the Express class on the new routes: no node moves, no lane is added or
// removed, and only the classes of the route lanes change (HB-D21 A keeps
// the fleet).
func TestWharfHubsPreset(t *testing.T) {
	t.Parallel()
	base, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	config, err := londonFullSharedExpressWharfHubsPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if shapes, err := n.BankGeometryShapePairs(); err != nil || shapes != 67 {
		t.Fatalf("bank audit: %d shape pairs, error %v, want 67 and no error", shapes, err)
	}
	if warnings := project.TailWarnings(n); len(warnings) != 2 {
		t.Fatalf("tail warnings %v, want 2", warnings)
	}
	if config.Name != "LondonFull Shared Express Wharf Hubs" || config.OrderContract != sim.ExpressOrderContract {
		t.Fatalf("name %q, contract %q", config.Name, config.OrderContract)
	}
	service := func(from, to string) sim.ExpressService {
		return sim.ExpressService{ID: "xs-" + from[8:] + "-" + to[8:], From: from, To: to, Class: sim.ExpressClass, PartyLimit: 20}
	}
	want := slices.Concat(base.ExpressServices, []sim.ExpressService{
		service(expressLondonBridge, expressWaterloo), service(expressWaterloo, expressLondonBridge),
		service(expressWaterloo, expressCanaryWharf), service(expressCanaryWharf, expressWaterloo),
	})
	if len(base.ExpressServices) != 4 || !slices.Equal(config.ExpressServices, want) {
		t.Fatalf("services %+v, want %+v", config.ExpressServices, want)
	}
	if !reflect.DeepEqual(config.Fleet, base.Fleet) {
		t.Fatal("the fleet differs from the wharf preset")
	}
	if gaps := project.ParkingGaps(config); len(gaps) != 0 {
		t.Fatalf("%d parking gaps, the first %+v", len(gaps), gaps[0])
	}
	if !reflect.DeepEqual(n.Nodes, base.Network.Nodes) || !reflect.DeepEqual(n.Stations, base.Network.Stations) {
		t.Fatal("the nodes or the stations differ from the wharf preset")
	}
	if len(n.Lanes) != len(base.Network.Lanes) {
		t.Fatalf("%d lanes, want the %d of the wharf preset", len(n.Lanes), len(base.Network.Lanes))
	}
	routes := make(map[string]bool)
	for _, route := range wharfHubsRoutes() {
		for _, id := range route.lanes {
			routes[id] = true
		}
	}
	changed := 0
	for i, lane := range n.Lanes {
		before := base.Network.Lanes[i]
		before.VehicleClasses = lane.VehicleClasses
		if !reflect.DeepEqual(lane, before) {
			t.Fatalf("lane %d is %s, and more than its classes changed", i, lane.ID)
		}
		if lane.VehicleClasses != base.Network.Lanes[i].VehicleClasses {
			changed++
			if !routes[lane.ID] || !lane.VehicleClasses.Allows(string(sim.ExpressClass)) {
				t.Errorf("lane %s changes its classes to %v, and is not on a new route", lane.ID, lane.VehicleClasses)
			}
		}
	}
	if changed == 0 {
		t.Fatal("no lane gets the Express class")
	}
}

// TestWharfHubsRoutes checks the routes: each Express service of the
// preset takes its shared route, and one berth of the Waterloo comb has an
// Express route to and from each berth of the four hubs and the two combs.
// The two Express networks of the wharf preset are one.
func TestWharfHubsRoutes(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressWharfHubsPreset(LondonFull())
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
	routes := make(map[[2]string][]string)
	for _, route := range slices.Concat(sharedTrackPresetRoutes(), wharfRoutes(), wharfHubsRoutes()) {
		routes[[2]string{route.from, route.to}] = route.lanes
	}
	if len(config.ExpressServices) != len(routes) {
		t.Fatalf("%d services, want one for each of the %d routes", len(config.ExpressServices), len(routes))
	}
	for _, service := range config.ExpressServices {
		want, ok := routes[[2]string{service.From, service.To}]
		if !ok {
			t.Fatalf("service %s from %s to %s has no route", service.ID, service.From, service.To)
		}
		lanes, err := n.RouteForClass(station(service.From).Exit, station(service.To).Entry, sim.ExpressClass)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(lanes))
		for _, lane := range lanes {
			ids = append(ids, lane.ID)
		}
		if !slices.Equal(ids, want) {
			t.Fatalf("service %s takes %v, want %v", service.ID, ids, want)
		}
	}
	// Each berth reaches the comb berth and the comb berth reaches each
	// berth, so each pair of these berths has a route through it.
	comb := station(expressParking).Berths[0].Node
	for _, id := range []string{expressWaterloo, expressBank, expressLondonBridge, expressCanaryWharf, expressParking, expressWharfParking} {
		for _, berth := range station(id).Berths {
			if berth.Node == comb {
				continue
			}
			for _, way := range [][2]string{{comb, berth.Node}, {berth.Node, comb}} {
				if _, err := n.RouteForClass(way[0], way[1], sim.ExpressClass); err != nil {
					t.Errorf("no Express route from %s to %s: %v", way[0], way[1], err)
				}
			}
		}
	}
	// HB-D20 A keeps park as it is: it takes the first parking in network
	// order, the comb beside Waterloo.
	var parking []string
	for _, s := range n.Stations {
		if s.ParkingOnly && s.VehicleClasses.Allows(string(sim.ExpressClass)) {
			parking = append(parking, s.ID)
		}
	}
	if !slices.Equal(parking, []string{expressParking, expressWharfParking}) {
		t.Fatalf("Express parking in network order %v", parking)
	}
}

// TestWharfHubsKeepsPresets checks that the preset changes no other preset,
// that the generator and the cached base agree, and pins the network of
// the preset.
func TestWharfHubsKeepsPresets(t *testing.T) {
	t.Parallel()
	cached, err := londonFullSharedExpressWharfHubsPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateLondonFullSharedExpressWharfHubs()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated, cached) {
		t.Fatal("GenerateLondonFullSharedExpressWharfHubs differs from the preset built on the cached LondonFull")
	}
	shared, err := GenerateLondonFullSharedExpress()
	if err != nil {
		t.Fatal(err)
	}
	wharf, err := GenerateLondonFullSharedExpressWharf()
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
		{"LondonFull Shared Express Wharf", wharf.Network, "d68a69b8c326f292f7c3a7839b51bf9837c5e714287f60470436781705217359"},
		{"LondonFull Shared Express Wharf Hubs", cached.Network, "fa153ec8f8c85e98d8e056155e8915240ccca405efa62d0e5fad22909f660196"},
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
