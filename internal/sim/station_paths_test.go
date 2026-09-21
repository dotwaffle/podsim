package sim

import (
	"errors"
	"slices"
	"testing"
)

func TestStationAccessPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Network)
		valid  bool
	}{
		{name: "multi-lane access", valid: true},
		{name: "missing through lane", change: func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "market-through" })
		}},
		{name: "missing arrival link", change: func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "market-arrival-link" })
		}},
		{name: "arrival through exit", change: func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "market-arrival-link" })
			n.Lanes = append(n.Lanes, Lane{ID: "market-exit-arrival", From: "market-exit", To: "market-arrival-1", SpeedLimit: 14})
		}},
		{name: "arrival through other berth", change: func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "market-arrival-next" })
			n.Lanes = append(n.Lanes, Lane{ID: "market-berth-next", From: "market-berth", To: "market-arrival-2", SpeedLimit: 14})
		}},
		{name: "arrival through other station", change: func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "market-arrival-link" })
			n.Lanes = append(n.Lanes,
				Lane{ID: "market-garden-entry", From: "market-entry", To: "garden-entry", SpeedLimit: 14},
				Lane{ID: "garden-market-arrival", From: "garden-entry", To: "market-arrival-1", SpeedLimit: 14},
			)
		}},
		{name: "departure through entry", change: func(n *Network) {
			n.Lanes = slices.DeleteFunc(n.Lanes, func(l Lane) bool { return l.ID == "market-departure-link" })
			n.Lanes = append(n.Lanes, Lane{ID: "market-departure-entry", From: "market-departure-1", To: "market-entry", SpeedLimit: 14})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := ladderNetwork()
			if tc.change != nil {
				tc.change(&network)
			}
			_, err := New(network, "harbor")
			if (err == nil) != tc.valid {
				t.Fatalf("New() error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestStationPathCacheDoesNotShareRoadRoutes(t *testing.T) {
	t.Parallel()
	for _, stationFirst := range []bool{false, true} {
		s, err := New(ladderNetwork(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		stationPath := func() {
			if _, err := s.stationPath("market-exit", "harbor-berth"); !errors.Is(err, ErrUnreachable) {
				t.Fatalf("station path crossed a road and station entry: %v", err)
			}
		}
		if stationFirst {
			stationPath()
		}
		if _, err := s.route("market-exit", "harbor-berth"); err != nil {
			t.Fatalf("road route inherited a station-path error: %v", err)
		}
		stationPath()
	}
}

func ladderNetwork() Network {
	network := Example()
	network.Nodes = append(network.Nodes,
		Node{ID: "market-arrival-1", Position: Point{X: 745, Y: 300}},
		Node{ID: "market-arrival-2", Position: Point{X: 760, Y: 330}},
		Node{ID: "market-departure-1", Position: Point{X: 855, Y: 300}},
		Node{ID: "market-departure-2", Position: Point{X: 840, Y: 330}},
		Node{ID: "market-berth-2", Position: Point{X: 800, Y: 390}},
	)
	network.Lanes = slices.DeleteFunc(network.Lanes, func(l Lane) bool {
		return l.ID == "market-in" || l.ID == "market-out"
	})
	network.Lanes = append(network.Lanes,
		Lane{ID: "market-arrival-link", From: "market-entry", To: "market-arrival-1", SpeedLimit: 14},
		Lane{ID: "market-in", From: "market-arrival-1", To: "market-berth", SpeedLimit: 14},
		Lane{ID: "market-arrival-next", From: "market-arrival-1", To: "market-arrival-2", SpeedLimit: 14},
		Lane{ID: "market-in-2", From: "market-arrival-2", To: "market-berth-2", SpeedLimit: 14},
		Lane{ID: "market-out", From: "market-berth", To: "market-departure-1", SpeedLimit: 14},
		Lane{ID: "market-out-2", From: "market-berth-2", To: "market-departure-2", SpeedLimit: 14},
		Lane{ID: "market-departure-next", From: "market-departure-2", To: "market-departure-1", SpeedLimit: 14},
		Lane{ID: "market-departure-link", From: "market-departure-1", To: "market-exit", SpeedLimit: 14},
	)
	for i := range network.Stations {
		if network.Stations[i].ID == "market" {
			network.Stations[i].Berths = append(network.Stations[i].Berths, Berth{ID: "market-2", Node: "market-berth-2"})
		}
	}
	return network
}
