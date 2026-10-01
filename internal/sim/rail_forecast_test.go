package sim

import (
	"reflect"
	"testing"
)

func forecastFixture(t *testing.T, parking bool, pods, berths int) *Simulation {
	t.Helper()
	stations := []lineStation{{id: "source", berths: pods, parking: parking}, {id: "target", berths: berths}, {id: "other", berths: 2}}
	network := lineNetwork(stations)
	fleet := make([]Placement, pods)
	for i, berth := range network.Stations[0].Berths {
		fleet[i] = Placement{ID: berth.ID, StationID: "source", BerthID: berth.ID}
	}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestForecastInputAtomicity(t *testing.T) {
	t.Parallel()
	s := forecastFixture(t, true, 4, 3)
	target := ForecastTarget{Station: "target", ReleaseTick: 300 * TicksPerSecond, Passengers: 1}
	for _, tc := range []struct {
		name    string
		targets []ForecastTarget
	}{
		{"past", []ForecastTarget{{Station: "target", ReleaseTick: 0, Passengers: 1}}},
		{"horizon", []ForecastTarget{{Station: "target", ReleaseTick: 300*TicksPerSecond + 1, Passengers: 1}}},
		{"parking", []ForecastTarget{{Station: "source", ReleaseTick: 60, Passengers: 1}}},
		{"unknown", []ForecastTarget{{Station: "missing", ReleaseTick: 60, Passengers: 1}}},
		{"count", []ForecastTarget{{Station: "target", ReleaseTick: 60, Passengers: 0}}},
		{"duplicate", []ForecastTarget{target, target}},
		{"late invalid", []ForecastTarget{target, {Station: "missing", ReleaseTick: 60, Passengers: 1}}},
		{"bound", make([]ForecastTarget, 301)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := s.ExportState()
			if _, err := s.PositionForForecast(tc.targets); err == nil || !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("invalid input changed state or was accepted")
			}
		})
	}
	result, err := s.PositionForForecast([]ForecastTarget{target})
	if err != nil || result.Pod == "" || result.Searches != 1 || result.StartAttempts != 1 || result.TargetsTried != 1 {
		t.Fatalf("valid horizon: %+v %v", result, err)
	}
}

func TestForecastEligibilityAndReserves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		parking      bool
		pods, berths int
		prepare      func(*Simulation)
	}{
		{name: "last passenger pod", pods: 1, berths: 3},
		{name: "one target berth", parking: true, pods: 4, berths: 1},
		{name: "cooldown", parking: true, pods: 4, berths: 3, prepare: func(s *Simulation) {
			for i := range s.vehicles {
				s.vehicles[i].rebalanceAfter = 60
			}
		}},
		{name: "paused", parking: true, pods: 4, berths: 3, prepare: func(s *Simulation) { s.SetPaused(true) }},
		{name: "unassigned demand", parking: true, pods: 4, berths: 3, prepare: func(s *Simulation) {
			s.waiting = append(s.waiting, waitingTrip{request: Request{ID: 1, From: "other", To: "target"}})
		}},
		{name: "working share", pods: 30, berths: 3, prepare: func(s *Simulation) {
			for i := range 13 {
				s.waiting = append(s.waiting, waitingTrip{request: Request{ID: i + 1, From: "other", To: "target", PodID: s.vehicles[i].Pod.ID}})
			}
		}},
		{name: "target claim", parking: true, pods: 4, berths: 2, prepare: func(s *Simulation) { s.owners[resource{kind: berthResource, id: "target-1"}] = "external" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := forecastFixture(t, tc.parking, tc.pods, tc.berths)
			if tc.prepare != nil {
				tc.prepare(s)
			}
			before := s.ExportState()
			r, err := s.PositionForForecast([]ForecastTarget{{Station: "target", ReleaseTick: 300 * TicksPerSecond, Passengers: 4}})
			if err != nil || r.Pod != "" || !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatalf("gate: %+v %v", r, err)
			}
		})
	}
	s := forecastFixture(t, false, 2, 3)
	r, err := s.PositionForForecast([]ForecastTarget{{Station: "target", ReleaseTick: 300 * TicksPerSecond, Passengers: 4}})
	if err != nil || r.Pod == "" {
		t.Fatalf("two-pod source: %+v %v", r, err)
	}
	idle := 0
	for i := range s.vehicles {
		if s.vehicles[i].Pod.Activity == Idle {
			idle++
		}
	}
	if idle != 1 {
		t.Fatal("did not retain last idle passenger pod")
	}
}

func TestForecastIncomingCapsAndSupply(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                    string
		pods, passengers, moves int
	}{
		{"all incoming supply", 30, 1, 1},
		{"target cap", 30, 4, 2},
		{"global floor", 4, 4, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := forecastFixture(t, true, tc.pods, 6)
			target := []ForecastTarget{{Station: "target", ReleaseTick: 300 * TicksPerSecond, Passengers: tc.passengers}}
			for i := range tc.moves {
				station, _ := s.station("target")
				// Count moves from other controllers with the same physical fields.
				if err := s.startEmptyMove(&s.vehicles[i], emptyDestination{station: "target", berth: station.Berths[i], reserveBerth: true, rebalance: true}); err != nil {
					t.Fatal(err)
				}
			}
			before := s.ExportState()
			r, err := s.PositionForForecast(target)
			if err != nil || r.Pod != "" || !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatalf("incoming cap/supply: %+v %v", r, err)
			}
		})
	}
}

func TestForecastBoundedWorkAndRestoredContinuation(t *testing.T) {
	t.Parallel()
	s := forecastFixture(t, true, 30, 3)
	targets := []ForecastTarget{{Station: "target", ReleaseTick: 1, Passengers: 4}, {Station: "other", ReleaseTick: 1, Passengers: 4}}
	before := s.ExportState()
	r, err := s.PositionForForecast(targets)
	if err != nil || r.Pod != "" || r.Searches != 2 || r.StartAttempts != 0 || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatalf("lead-time work: %+v %v", r, err)
	}
	targets[0].ReleaseTick = 300 * TicksPerSecond
	r, err = s.PositionForForecast(targets[:1])
	if err != nil || r.Pod == "" {
		t.Fatalf("start: %+v %v", r, err)
	}
	clone := s.Clone()
	saved := s.ExportState()
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: saved})
	if err != nil || result.Tier != RestorePhysical {
		t.Fatalf("physical restore: %v %+v", err, result)
	}
	for range 2000 {
		s.Step()
		clone.Step()
		restored.Step()
		if _, err := restored.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(s.ExportState(), clone.ExportState()) || !reflect.DeepEqual(s.ExportState(), restored.ExportState()) || s.Snapshot().RebalanceMoves != 1 || restored.Snapshot().RebalanceMoves != 1 {
		t.Fatal("disabled continuation or clone changed move")
	}
	for _, candidate := range []*Simulation{s, clone, restored} {
		if v := candidate.findVehicle(r.Pod); v.Pod.Activity != Idle || v.Pod.StationID != "target" {
			t.Fatal("empty move did not finish without further forecasts")
		}
	}
}

func TestForecastPolicyRouteLeadAndSpareBerth(t *testing.T) {
	t.Parallel()
	network := lineNetwork([]lineStation{{id: "source", berths: 4, parking: true}, {id: "target", berths: 3}, {id: "other", berths: 2}})
	network.Nodes = append(network.Nodes, Node{ID: "detour", Position: Point{X: 225, Y: 100}})
	network.Lanes = append(network.Lanes, Lane{ID: "detour-in", From: "source-exit", To: "detour", SpeedLimit: 14}, Lane{ID: "detour-out", From: "detour", To: "target-entry", SpeedLimit: 14})
	fleet := []Placement{{ID: "01", StationID: "source", BerthID: "source-1"}, {ID: "02", StationID: "source", BerthID: "source-2"}}
	s, err := NewFleet(network, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRoutingPolicy(CongestionRouting); err != nil {
		t.Fatal(err)
	}
	for cell := range 10 {
		s.owners[resource{kind: trackResource, id: "source-link", cell: cell}] = "external"
	}
	from, _ := s.station("source")
	to, _ := s.station("target")
	free, err := s.route(from.Berths[0].Node, to.Berths[0].Node)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.assignedRoute(&s.vehicles[0], from.Berths[0].Node, to.Berths[0].Node)
	if err != nil {
		t.Fatal(err)
	}
	cost := func(route []Lane) float64 {
		sum := 0.0
		for _, lane := range route {
			sum += s.laneLength(lane) / lane.SpeedLimit
		}
		return sum
	}
	if cost(selected) <= cost(free)+1 {
		t.Fatal("fixture did not select a longer policy route")
	}
	before := s.ExportState()
	lead := int64((cost(free) + 1) * TicksPerSecond)
	result, err := s.PositionForForecast([]ForecastTarget{{Station: "target", ReleaseTick: lead, Passengers: 4}})
	if err != nil || result.Searches != 1 || result.StartAttempts != 1 || result.Pod != "" || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatalf("policy lead rejection: %+v %v", result, err)
	}
	// A legacy route can pass a spare target berth before its destination.
	crossing := []Lane{{ID: "cross", From: to.Berths[1].Node, To: to.Berths[0].Node, SpeedLimit: 14}}
	if s.forecastRouteFits(crossing, to.Berths[:2], 300) {
		t.Fatal("counted a traversed berth as spare")
	}
	if !s.forecastRouteFits(crossing, to.Berths, 300) {
		t.Fatal("rejected an untouched third spare berth")
	}
}

func TestForecastThreeTargetSearchBound(t *testing.T) {
	t.Parallel()
	network := lineNetwork([]lineStation{{id: "source", berths: 4, parking: true}, {id: "a", berths: 3}, {id: "b", berths: 3}, {id: "c", berths: 3}, {id: "d", berths: 3}, {id: "e", berths: 3}})
	s, err := NewFleet(network, place("source-1", "source-2"))
	if err != nil {
		t.Fatal(err)
	}
	var targets []ForecastTarget
	for _, id := range []string{"e", "d", "c", "b", "a"} {
		targets = append(targets, ForecastTarget{Station: id, ReleaseTick: 1, Passengers: 4})
	}
	before := s.ExportState()
	result, err := s.PositionForForecast(targets)
	if err != nil || result.TargetsTried != 3 || result.Searches != 3 || result.StartAttempts != 0 || result.Pod != "" || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatalf("three-target bound: %+v %v", result, err)
	}
}
