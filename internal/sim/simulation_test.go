package sim

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"
)

func newExample(t *testing.T) *Simulation {
	t.Helper()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func advance(s *Simulation, ticks int) {
	for range ticks {
		s.Step()
	}
}

func TestRoutes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, from, to string
		want           []string
	}{
		{"branch", "harbor-berth", "garden-berth", []string{"harbor-out", "approach-branch", "garden-approach", "garden-in"}},
		{"bypass", "harbor-berth", "market-berth", []string{"harbor-out", "approach-branch", "bypass-in", "bypass-merge", "market-approach", "market-in"}},
		{"merge", "garden-berth", "market-berth", []string{"garden-out", "garden-merge", "market-approach", "market-in"}},
		{"return", "market-berth", "harbor-berth", []string{"market-out", "return-start", "return-to-parking", "parking-through", "return", "harbor-approach", "harbor-in"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route, err := Example().Route(tc.from, tc.to)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, lane := range route {
				ids = append(ids, lane.ID)
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("route = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestJourneyLifecycle(t *testing.T) {
	t.Parallel()
	for _, destination := range []string{"garden", "market"} {
		t.Run(destination, func(t *testing.T) {
			s := newExample(t)
			if err := s.RequestJourney("01", destination); err != nil {
				t.Fatal(err)
			}
			advance(s, boardingTicks-1)
			before := s.Snapshot()
			if before.Vehicles[0].Pod.Activity != Boarding || before.Vehicles[0].Pod.BerthID != "harbor-1" || before.Vehicles[0].Pod.Occupied {
				t.Fatalf("boarding state: %+v", before.Vehicles[0].Pod)
			}
			s.Step()
			departed := s.Snapshot()
			if departed.Vehicles[0].Pod.Activity != Traveling || departed.Vehicles[0].Pod.BerthID != "" || !departed.Vehicles[0].Pod.Occupied {
				t.Fatalf("departure state: %+v", departed.Vehicles[0].Pod)
			}
			for range 300 * TicksPerSecond {
				s.Step()
				state := s.Snapshot()
				if state.Vehicles[0].Pod.Speed < 0 || state.Vehicles[0].Pod.Speed > 14 || !finite(state.Vehicles[0].Pod.Position.X) || !finite(state.Vehicles[0].Pod.Position.Y) {
					t.Fatalf("invalid movement: %+v", state.Vehicles[0].Pod)
				}
				if state.Vehicles[0].Pod.Activity != Traveling {
					break
				}
			}
			arrived := s.Snapshot()
			station, _ := Example().Station(destination)
			node, _ := Example().Node(station.Berths[0].Node)
			if arrived.Vehicles[0].Pod.Activity != Unloading || arrived.Vehicles[0].Pod.Position != node.Position || arrived.Vehicles[0].Pod.BerthID != station.Berths[0].ID || arrived.Vehicles[0].Pod.Speed != 0 {
				t.Fatalf("arrival state: %+v", arrived.Vehicles[0].Pod)
			}
			if arrived.Completed != 0 || arrived.Vehicles[0].Request.Completed {
				t.Fatal("journey completed before unloading")
			}
			advance(s, unloadingTicks-1)
			if s.Snapshot().Vehicles[0].Pod.Activity != Unloading {
				t.Fatal("unloading ended early")
			}
			s.Step()
			completed := s.Snapshot()
			if completed.Vehicles[0].Pod.Activity != Idle || completed.Vehicles[0].Pod.Occupied || completed.Completed != 1 || !completed.Vehicles[0].Request.Completed {
				t.Fatalf("completion state: %+v", completed)
			}
			if err := s.RequestJourney("01", "harbor"); err != nil {
				t.Fatalf("request return journey: %v", err)
			}
			advance(s, 300*TicksPerSecond)
			if got := s.Snapshot(); got.Completed != 2 || got.Vehicles[0].Pod.StationID != "harbor" {
				t.Fatalf("return journey: %+v", got)
			}
		})
	}
}

func TestRejectedRequestsDoNotMutate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, to string
		busy     bool
		want     error
	}{
		{"same station", "harbor", false, ErrSameStation},
		{"unknown station", "missing", false, nil},
		{"busy", "market", true, ErrBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newExample(t)
			if tc.busy {
				if err := s.RequestJourney("01", "garden"); err != nil {
					t.Fatal(err)
				}
			}
			before := s.Snapshot()
			err := s.RequestJourney("01", tc.to)
			if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Fatalf("request error = %v, want %v", err, tc.want)
			}
			if !reflect.DeepEqual(before, s.Snapshot()) {
				t.Fatal("rejected request changed state")
			}
		})
	}
}

func TestPauseResetAndRepeatability(t *testing.T) {
	t.Parallel()
	for _, ticks := range []int{0, 30, boardingTicks + 600, 300 * TicksPerSecond} {
		t.Run(stringPhase(ticks), func(t *testing.T) {
			s := newExample(t)
			initial := s.Snapshot()
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			advance(s, ticks)
			s.SetPaused(true)
			paused := s.Snapshot()
			advance(s, 500)
			if !reflect.DeepEqual(paused, s.Snapshot()) {
				t.Fatal("pause changed state")
			}
			s.Reset()
			if !reflect.DeepEqual(initial, s.Snapshot()) {
				t.Fatal("reset did not restore the initial state")
			}
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			advance(s, ticks)
			s.SetPaused(true)
			if !reflect.DeepEqual(paused, s.Snapshot()) {
				t.Fatal("repeated run changed state")
			}
		})
	}
}

func stringPhase(ticks int) string {
	switch ticks {
	case 0:
		return "request"
	case 30:
		return "boarding"
	case boardingTicks + 600:
		return "travel"
	default:
		return "complete"
	}
}

func TestPlaybackGrouping(t *testing.T) {
	t.Parallel()
	a, b := newExample(t), newExample(t)
	for _, s := range []*Simulation{a, b} {
		if err := s.RequestJourney("01", "market"); err != nil {
			t.Fatal(err)
		}
	}
	advance(a, 8000)
	for range 1000 {
		advance(b, 8)
	}
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
		t.Fatal("step grouping changed simulation results")
	}
}

func TestScenarioAndSnapshotIsolation(t *testing.T) {
	t.Parallel()
	network := Example()
	s, err := New(network, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	network.Nodes[0].Position.X = -999
	network.Lanes[0].SpeedLimit = -1
	network.Stations[0].Berths[0].Node = "missing"
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Snapshot()
	snapshot.Vehicles[0].Request.To = "missing"
	snapshot.Vehicles[0].Route[0].To = "missing"
	advance(s, 300*TicksPerSecond)
	if got := s.Snapshot(); got.Completed != 1 || got.Vehicles[0].Pod.StationID != "market" {
		t.Fatalf("external mutation changed simulation: %+v", got)
	}
}

func TestInvalidNetwork(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Network)
	}{
		{"duplicate node", func(n *Network) { n.Nodes[1].ID = n.Nodes[0].ID }},
		{"missing endpoint", func(n *Network) { n.Lanes[0].To = "missing" }},
		{"zero speed", func(n *Network) { n.Lanes[0].SpeedLimit = 0 }},
		{"nan coordinate", func(n *Network) { n.Nodes[0].Position.X = math.NaN() }},
		{"missing berth", func(n *Network) { n.Stations[0].Berths = nil }},
		{"shared berth node", func(n *Network) { n.Stations[1].Berths[0].Node = n.Stations[0].Berths[0].Node }},
		{"same entry and exit", func(n *Network) { n.Stations[0].Exit = n.Stations[0].Entry }},
		{"missing through lane", func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "harbor-through" })
		}},
		{"berth on entry", func(n *Network) { n.Stations[0].Berths[0].Node = n.Stations[0].Entry }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			network := Example()
			tc.change(&network)
			if _, err := New(network, "harbor"); err == nil {
				t.Fatal("invalid network accepted")
			}
		})
	}
}

func TestUnreachableRequest(t *testing.T) {
	t.Parallel()
	n := Example()
	n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "garden-approach" })
	s, err := New(n, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	if err := s.RequestJourney("01", "garden"); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("error = %v, want ErrUnreachable", err)
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("unreachable request changed state")
	}
}
