package sim

import (
	"reflect"
	"testing"
)

func TestPassengerBerthAssignedAtStationAccess(t *testing.T) {
	t.Parallel()
	simulation, err := New(Example(), "harbor")
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
		firstAccessBlock := firstBlockForLane(vehicle.blocks, accessLane)
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
