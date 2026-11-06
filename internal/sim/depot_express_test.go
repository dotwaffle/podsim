package sim

import (
	"strings"
	"testing"
)

// mixedDepotNetwork gives Express pods passenger routes and small pods a depot.
func mixedDepotNetwork() Network {
	network := testDepotNetwork()
	for i := range network.Nodes {
		network.Nodes[i].Position.X *= 4
		network.Nodes[i].Position.Y *= 4
	}
	for i := range network.Lanes {
		lane := &network.Lanes[i]
		if !strings.HasPrefix(lane.ID, "parking-") || lane.ID == "parking-through" {
			lane.VehicleClasses = allClassBits
		}
		if lane.Control != nil {
			lane.Control = &Point{X: lane.Control.X * 4, Y: lane.Control.Y * 4}
		}
	}
	for i := range network.Stations {
		station := &network.Stations[i]
		if !station.ParkingOnly {
			station.VehicleClasses = allClassBits
			for j := range station.Berths {
				station.Berths[j].VehicleClasses = allClassBits
			}
		}
	}
	network.Nodes = append(network.Nodes, Node{ID: "garden-second", Position: Point{X: 1920, Y: 920}})
	network.Lanes = append(network.Lanes,
		Lane{ID: "garden-second-in", From: "garden-entry", To: "garden-second", SpeedLimit: 14, StationID: "garden", StationRole: StationBerthAccessRole, VehicleClasses: allClassBits},
		Lane{ID: "garden-second-out", From: "garden-second", To: "garden-exit", SpeedLimit: 14, StationID: "garden", StationRole: StationDepartureRole, VehicleClasses: allClassBits},
	)
	network.Stations[1].Berths = append(network.Stations[1].Berths, Berth{ID: "garden-2", Node: "garden-second", VehicleClasses: allClassBits})
	return network
}

func mixedDepotFleet(t *testing.T) *Simulation {
	t.Helper()
	network := mixedDepotNetwork()
	s, err := NewFleetWithOrderContract(network, []Placement{
		{ID: "express", StationID: "harbor", Class: ExpressClass},
		{ID: "compact", StationID: "parking", BerthID: depotSlotID(1, 1), Class: CompactClass},
	}, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDepotExpressMixedFleet(t *testing.T) {
	t.Parallel()
	s := mixedDepotFleet(t)
	if s.OrderContract() != ExpressOrderContract || s.findVehicle("express").Pod.Class != ExpressClass || !s.depotParked(s.findVehicle("compact")) {
		t.Fatal("mixed fleet lost its contract, class, or depot placement")
	}
	stepDepot(t, s, TicksPerSecond, func() bool { return s.tick == TicksPerSecond })
}

func TestDepotExpressClassPath(t *testing.T) {
	t.Parallel()
	s := mixedDepotFleet(t)
	station, _ := s.station("parking")
	for _, tc := range []struct {
		pod  string
		want bool
	}{{"express", false}, {"compact", true}} {
		t.Run(tc.pod, func(t *testing.T) {
			t.Parallel()
			v := s.findVehicle(tc.pod)
			if _, slot, ok := s.depotChoice(v, 0); ok != tc.want {
				t.Fatalf("choice %q %v, want eligible %v", slot.ID, ok, tc.want)
			}
			if got := s.depotGoal(v, station); got != tc.want {
				t.Fatalf("goal %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDepotExpressParkingFallback(t *testing.T) {
	t.Parallel()
	s := mixedDepotFleet(t)
	v := s.findVehicle("express")
	if s.park(v) || v.Pod.Activity != Idle || v.Pod.StationID != "harbor" {
		t.Fatal("Express pod selected small-only parking")
	}
	if !s.moveBlocker(v) || v.destinationStation == "parking" || v.destination.ID == "" {
		t.Fatal("Express blocker did not select a passenger berth")
	}
	destination := v.destination.ID
	stepDepot(t, s, 600*TicksPerSecond, func() bool { return v.Pod.Activity == Idle })
	if v.Pod.BerthID != destination {
		t.Fatalf("parked at %s, want %s", v.Pod.BerthID, destination)
	}
}

func TestDepotExpressRedistribution(t *testing.T) {
	t.Parallel()
	s := mixedDepotFleet(t)
	if err := s.SetPositioning(PositioningGuarded); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDemandWeights(map[string]float64{"garden": 1}); err != nil {
		t.Fatal(err)
	}
	// A completed request opens the guarded move budget at a low rate.
	openGate(s)
	s.Step()
	if s.rebalanceMoves != 1 {
		t.Fatalf("redistribution made %d moves, want 1", s.rebalanceMoves)
	}
	stepDepot(t, s, 600*TicksPerSecond, func() bool {
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if v.Pod.Class == ExpressClass && (v.destinationStation == "parking" || v.Pod.StationID == "parking") {
				t.Fatal("redistribution sent Express to the depot")
			}
			if v.Pod.Activity != Idle {
				return false
			}
		}
		return true
	})
	if s.findVehicle("express").Pod.StationID != "garden" && s.findVehicle("compact").Pod.StationID != "garden" {
		t.Fatal("redistribution did not fill the demand station")
	}
}
