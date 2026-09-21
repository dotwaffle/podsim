package sim

import (
	"reflect"
	"testing"
)

func TestRedistributionReducesForecastDemandWait(t *testing.T) {
	t.Parallel()
	makeSimulation := func(t *testing.T, enabled bool) *Simulation {
		t.Helper()
		s, err := New(Example(), "parking")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetDemandWeights(map[string]float64{"market": 10, "harbor": 1, "garden": 1}); err != nil {
			t.Fatal(err)
		}
		s.SetRedistribution(enabled)
		return s
	}

	off, on := makeSimulation(t, false), makeSimulation(t, true)
	for range 180 * TicksPerSecond {
		off.Step()
		on.Step()
		checkTraffic(t, on.Snapshot())
		if on.Snapshot().Vehicles[0].Pod.StationID == "market" {
			break
		}
	}
	if on.Snapshot().Vehicles[0].Pod.StationID != "market" {
		t.Fatal("redistribution did not position the pod at forecast demand")
	}
	for _, s := range []*Simulation{off, on} {
		if err := s.RequestTrip("market", "garden"); err != nil {
			t.Fatal(err)
		}
	}
	if on.Snapshot().Wait.MaxSeconds != 0 {
		t.Fatalf("positioned pod had pickup wait: %+v", on.Snapshot().Wait)
	}
	for range 360 * TicksPerSecond {
		off.Step()
		on.Step()
		if off.Snapshot().Completed == 1 && on.Snapshot().Completed == 1 {
			break
		}
	}
	if off.Snapshot().Wait.AverageSeconds <= on.Snapshot().Wait.AverageSeconds {
		t.Fatalf("wait did not improve: off=%+v on=%+v", off.Snapshot().Wait, on.Snapshot().Wait)
	}
	if state := on.Snapshot(); state.Completed != 1 || state.RebalanceMoves < 1 || state.RebalanceMoves > 2 || state.PassengerDistanceMeters <= 0 || state.EmptyDistanceMeters <= 0 {
		t.Fatalf("incorrect redistribution accounting: %+v", state)
	}
}

func TestRedistributionIsBoundedAndPassengerFirst(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "parking"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDemandWeights(map[string]float64{"harbor": 1, "garden": 1, "market": 1}); err != nil {
		t.Fatal(err)
	}
	s.SetRedistribution(true)
	s.Step()
	rebalancing := s.findVehicle("01")
	if !rebalancing.Rebalancing || rebalancing.RelocatingTo == "" {
		t.Fatalf("no initial redistribution: %+v", s.Snapshot())
	}
	if err := s.RequestTrip(rebalancing.RelocatingTo, "market"); err != nil {
		t.Fatal(err)
	}
	if rebalancing.Rebalancing || !s.assigned(rebalancing.Pod.ID) {
		t.Fatalf("passenger did not divert redistribution: %+v", s.Snapshot())
	}
	for range 600 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 1 {
			break
		}
	}
	state := s.Snapshot()
	if state.Completed != 1 || state.RebalanceMoves > 3 {
		t.Fatalf("redistribution was unbounded or blocked service: %+v", state)
	}
	s.SetRedistribution(false)
	before := state.RebalanceMoves
	advance(s, 120*TicksPerSecond)
	if s.Snapshot().RebalanceMoves != before {
		t.Fatal("disabled redistribution started a move")
	}
}

func TestRedistributionResetAndRepeatability(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T) Snapshot {
		t.Helper()
		s, err := New(Example(), "parking")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetDemandWeights(map[string]float64{"market": 1}); err != nil {
			t.Fatal(err)
		}
		s.SetRedistribution(true)
		advance(s, 90*TicksPerSecond)
		return s.Snapshot()
	}
	a, b := run(t), run(t)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("redistribution changed between identical runs")
	}
	s, err := New(Example(), "parking")
	if err != nil {
		t.Fatal(err)
	}
	s.SetRedistribution(true)
	advance(s, 60*TicksPerSecond)
	s.Reset()
	if state := s.Snapshot(); state.Tick != 0 || state.RebalanceMoves != 0 || state.EmptyDistanceMeters != 0 {
		t.Fatalf("reset retained redistribution state: %+v", state)
	}
	advance(s, 60*TicksPerSecond)
	if s.Snapshot().RebalanceMoves != 0 {
		t.Fatal("reset retained the enabled policy")
	}
}

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
	if v.destination.ID != "market-2" || len(v.Route) == 0 || v.Route[len(v.Route)-1].To != "market-berth-2" {
		t.Fatalf("dispatch did not preserve the free berth route: %+v", v.Vehicle)
	}
	for range 240 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if s.Snapshot().Completed == 1 {
			break
		}
	}
	if state := s.Snapshot(); state.Completed != 1 || state.Vehicles[0].Pod.BerthID != "market-2" || state.Vehicles[1].Pod.BerthID != "market-1" {
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
	if first.destination.ID == second.destination.ID {
		t.Fatalf("concurrent arrivals share berth %q", first.destination.ID)
	}
	assigned := map[string]string{"01": first.destination.ID, "02": second.destination.ID}
	for range 240 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		for _, v := range s.vehicles {
			if v.Pod.Activity == Traveling && v.destination.ID != assigned[v.Pod.ID] {
				t.Fatalf("pod %s retargeted from %s to %s", v.Pod.ID, assigned[v.Pod.ID], v.destination.ID)
			}
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
}
