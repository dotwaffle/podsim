package scenarios

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonFullExpress checks that the Express preset is a valid project,
// that the generator without the cache gives the same project, and that it
// leaves LondonFull unchanged.
func TestLondonFullExpress(t *testing.T) {
	t.Parallel()
	before := LondonFull()
	config := LondonFullExpress()
	if !reflect.DeepEqual(LondonFull(), before) {
		t.Fatal("the Express preset changed LondonFull")
	}
	generated, err := GenerateLondonFullExpress()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated, config) {
		t.Fatal("GenerateLondonFullExpress differs from LondonFullExpress")
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.OrderContract != sim.ExpressOrderContract || len(config.ExpressServices) != 2 {
		t.Fatalf("contract %q, %d services", config.OrderContract, len(config.ExpressServices))
	}
	express := 0
	for _, placement := range config.Fleet {
		if placement.Class == sim.ExpressClass {
			express++
			if placement.StationID != expressParking {
				t.Fatalf("Express pod %s starts at %s", placement.ID, placement.StationID)
			}
		}
	}
	if express != expressPods || len(config.Fleet) != len(before.Fleet)+expressPods {
		t.Fatalf("%d Express pods in a fleet of %d", express, len(config.Fleet))
	}
	if _, err := sim.NewFleetWithOrderContract(config.Network, config.Fleet, config.OrderContract); err != nil {
		t.Fatal(err)
	}
}

// TestLondonFullExpressRoutes checks that an Express pod has a route from
// parking to each bank of each hub and back, and from each bank of each hub
// to each bank of the other hub.
func TestLondonFullExpressRoutes(t *testing.T) {
	t.Parallel()
	n := LondonFullExpress().Network
	station := func(id string) sim.Station {
		s, ok := n.Station(id)
		if !ok {
			t.Fatalf("no station %s", id)
		}
		return s
	}
	parking, waterloo, bank := station(expressParking), station(expressWaterloo), station(expressBank)
	var pairs [][2]string
	for _, hub := range []sim.Station{waterloo, bank} {
		for _, b := range hub.Banks {
			pairs = append(pairs, [2]string{parking.Exit, b.Entry}, [2]string{b.Exit, parking.Entry})
		}
	}
	for _, hubs := range [][2]sim.Station{{waterloo, bank}, {bank, waterloo}} {
		if len(hubs[1].Banks) != 2 {
			t.Fatalf("%s has %d banks", hubs[1].ID, len(hubs[1].Banks))
		}
		for _, from := range hubs[0].Banks {
			for _, to := range hubs[1].Banks {
				pairs = append(pairs, [2]string{from.Exit, to.Entry})
			}
		}
	}
	for _, pair := range pairs {
		route, err := n.RouteForClass(pair[0], pair[1], sim.ExpressClass)
		if err != nil || len(route) == 0 {
			t.Fatalf("no Express route from %s to %s: %v", pair[0], pair[1], err)
		}
		for _, lane := range route {
			if !lane.VehicleClasses.Allows(string(sim.ExpressClass)) {
				t.Fatalf("route from %s to %s uses lane %s, which refuses Express", pair[0], pair[1], lane.ID)
			}
		}
	}
	for _, hub := range []sim.Station{waterloo, bank} {
		ids := slices.Concat(hub.Banks[0].BerthIDs, hub.Banks[1].BerthIDs)
		if len(ids) != len(hub.Berths) {
			t.Fatalf("%s banks hold %d of %d berths", hub.ID, len(ids), len(hub.Berths))
		}
	}
}
