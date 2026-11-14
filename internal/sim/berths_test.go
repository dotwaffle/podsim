package sim

import (
	"reflect"
	"testing"
)

func TestPassengerBerthAssignedAtStationAccess(t *testing.T) {
	t.Parallel()
	simulation, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := simulation.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	vehicle := simulation.findVehicle("01")
	if vehicle.destination.ID != "" || vehicle.Route[len(vehicle.Route)-1].To != "market-entry" {
		t.Fatalf("journey chose a berth before station access: %+v", vehicle.Vehicle)
	}
	accessLane := vehicle.Route[len(vehicle.Route)-1].ID

	for range 180 * TicksPerSecond {
		simulation.Step()
		if vehicle.destination.ID == "" {
			continue
		}
		firstAccessBlock := firstBlockForLane(&vehicle.blocks, accessLane)
		if firstAccessBlock > vehicle.reservedThrough {
			t.Fatalf("berth assigned before access admission: block=%d reserved_through=%d", firstAccessBlock, vehicle.reservedThrough)
		}
		if vehicle.Route[len(vehicle.Route)-1].To != vehicle.destination.Node {
			t.Fatalf("assigned route ends at %q, want %q", vehicle.Route[len(vehicle.Route)-1].To, vehicle.destination.Node)
		}
		return
	}
	t.Fatal("passenger reached no station berth assignment")
}

func TestStationRouteBalancesCommittedArrivals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		commit func(*Simulation, Berth)
	}{
		{
			name: "active vehicles",
			commit: func(simulation *Simulation, berth Berth) {
				arrival := vehicle{destination: berth}
				arrival.Pod.Activity = Traveling
				simulation.vehicles = append(simulation.vehicles, arrival)
			},
		},
		{
			name: "assigned pickups",
			commit: func(simulation *Simulation, berth Berth) {
				simulation.waiting = append(simulation.waiting, waitingTrip{destination: berth})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			simulation, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			addMarketBerth(simulation)

			var got []string
			for range 6 {
				_, berth, routeErr := simulation.stationRoute("harbor-berth", "market")
				if routeErr != nil {
					t.Fatal(routeErr)
				}
				got = append(got, berth.ID)
				tc.commit(simulation, berth)
			}

			want := []string{"market-1", "market-2", "market-1", "market-2", "market-1", "market-2"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("destinations = %v, want %v", got, want)
			}
		})
	}
}

// stationApproachRoute is stationApproachRouteForClass for LegacyClass.
func (s *Simulation) stationApproachRoute(fromNode, stationID string) ([]Lane, error) {
	return s.stationApproachRouteForClass(fromNode, stationID, LegacyClass)
}

// stationRoute is stationRouteByLoad with the berth loads of berthLoad.
func (s *Simulation) stationRoute(fromNode, stationID string) ([]Lane, Berth, error) {
	return s.stationRouteByLoad(stationRouteInput{from: fromNode, station: stationID})
}

// TestStationRouteFromEntryCachesNoPlainRoutes checks that a station route
// from an entry of the station caches only station paths. The plain routes
// of the berth batch are never the result there, so their search is wasted
// work.
func TestStationRouteFromEntryCachesNoPlainRoutes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		from  string
		plain bool
	}{
		{from: "market-entry", plain: false},
		{from: "harbor-berth", plain: true},
	} {
		t.Run(tc.from, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
			if err != nil {
				t.Fatal(err)
			}
			addMarketBerth(s)
			route, berth, err := s.stationRoute(tc.from, "market")
			if err != nil {
				t.Fatal(err)
			}
			if tc.from == "market-entry" {
				want, err := s.stationPathForClass(tc.from, berth.Node, LegacyClass)
				if err != nil || !reflect.DeepEqual(route, want) {
					t.Fatalf("route = %v, want station path %v (%v)", route, want, err)
				}
			}
			plain := false
			for key := range s.routes {
				if key.from == tc.from && !key.station {
					plain = true
				}
			}
			if plain != tc.plain {
				t.Fatalf("plain routes cached from %s = %t, want %t", tc.from, plain, tc.plain)
			}
		})
	}
}
