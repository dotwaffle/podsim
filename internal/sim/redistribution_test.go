package sim

import "testing"

func TestPassengerDispatchUsesFreeReachableBerth(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	addMarketBerth(s)
	if err := s.RequestTrip("harbor", "market"); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	if v.destination.ID != "" || len(v.Route) == 0 || v.Route[len(v.Route)-1].To != "market-entry" {
		t.Fatalf("dispatch chose a berth before station access: %+v", v.Vehicle)
	}
	assigned := ""
	for range 240 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if v.destination.ID != "" && assigned == "" {
			assigned = v.destination.ID
		}
		if s.Snapshot().Completed == 1 {
			break
		}
	}
	if state := s.Snapshot(); assigned != "market-2" || state.Completed != 1 || state.Vehicles[0].Pod.BerthID != "market-2" || state.Vehicles[1].Pod.BerthID != "market-1" {
		t.Fatalf("multi-berth dispatch failed: %+v", state)
	}
}

func TestPassengerDispatchSpreadsConcurrentArrivals(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	addMarketBerth(s)
	for _, trip := range [][2]string{{"harbor", "market"}, {"garden", "market"}} {
		if err := s.RequestTrip(trip[0], trip[1]); err != nil {
			t.Fatal(err)
		}
	}
	first, second := s.findVehicle("01"), s.findVehicle("02")
	if first.destination.ID != "" || second.destination.ID != "" {
		t.Fatalf("concurrent arrivals chose berths before station access: %q %q", first.destination.ID, second.destination.ID)
	}
	assigned := map[string]string{"01": "", "02": ""}
	for range 240 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		for _, v := range s.vehicles {
			if assigned[v.Pod.ID] == "" && v.destination.ID != "" {
				assigned[v.Pod.ID] = v.destination.ID
			}
			if assigned[v.Pod.ID] != "" && v.Pod.Activity == Traveling && v.destination.ID != assigned[v.Pod.ID] {
				t.Fatalf("pod %s retargeted from %s to %s", v.Pod.ID, assigned[v.Pod.ID], v.destination.ID)
			}
		}
		if assigned["01"] != "" && assigned["01"] == assigned["02"] {
			t.Fatalf("concurrent arrivals share berth %q", assigned["01"])
		}
		if s.Snapshot().Completed == 2 {
			break
		}
	}
	if state := s.Snapshot(); state.Completed != 2 || first.Pod.BerthID == second.Pod.BerthID {
		t.Fatalf("concurrent arrivals did not use both berths: %+v", state)
	}
}

func addMarketBerth(s *Simulation) {
	detachIndexes(s)
	s.network.Nodes = append(s.network.Nodes, Node{ID: "market-berth-2", Position: Point{X: 760, Y: 250}})
	s.network.Lanes = append(s.network.Lanes,
		Lane{ID: "market-in-2", From: "market-entry", To: "market-berth-2", SpeedLimit: 14},
		Lane{ID: "market-out-2", From: "market-berth-2", To: "market-exit", SpeedLimit: 14},
	)
	for i := range s.network.Stations {
		if s.network.Stations[i].ID == "market" {
			s.network.Stations[i].Berths = append(s.network.Stations[i].Berths, Berth{ID: "market-2", Node: "market-berth-2"})
		}
	}
	// Rebuild the derived indexes, including junction conflicts, for the changed network.
	s.ensureNetworkIndexes()
}
