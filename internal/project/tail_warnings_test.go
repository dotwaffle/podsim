package project_test

import (
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func allClasses(t *testing.T) sim.ClassSet {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	return classes
}

// railHubAllClasses returns rail-hub with all classes on each lane. Each
// control point moves by the fraction nearer to the midpoint of its chord.
func railHubAllClasses(t *testing.T, nearer float64) project.Config {
	t.Helper()
	config := scenarios.RailHub()
	positions := make(map[string]sim.Point, len(config.Network.Nodes))
	for _, node := range config.Network.Nodes {
		positions[node.ID] = node.Position
	}
	classes := allClasses(t)
	for index := range config.Network.Lanes {
		lane := &config.Network.Lanes[index]
		lane.VehicleClasses = classes
		if lane.Control != nil {
			from, to := positions[lane.From], positions[lane.To]
			lane.Control = &sim.Point{
				X: lane.Control.X + nearer*((from.X+to.X)/2-lane.Control.X),
				Y: lane.Control.Y + nearer*((from.Y+to.Y)/2-lane.Control.Y),
			}
		}
	}
	return config
}

func warningLanes(warnings []project.TailWarning) []string {
	var lanes []string
	for _, warning := range warnings {
		lanes = append(lanes, warning.LaneID)
	}
	return lanes
}

func TestTailWarningsRailHubAllClasses(t *testing.T) {
	t.Parallel()
	config := railHubAllClasses(t, 0)
	if err := project.Validate(config); err != nil {
		t.Fatal("the warning must not refuse the project:", err)
	}
	warnings := project.TailWarnings(config.Network)
	if len(warnings) != project.MaxTailWarnings+1 {
		t.Fatalf("got %d warnings: %+v", len(warnings), warnings)
	}
	if want := "Lane s01-in-01 has a large-class tail of 299.1 m on a length of 189.3 m, which can stop the network."; warnings[0].Text != want || warnings[0].LaneID != "s01-in-01" {
		t.Fatalf("first warning = %+v", warnings[0])
	}
	last := warnings[project.MaxTailWarnings]
	if want := "65 more lanes have a large-class tail longer than their length."; last.Text != want || last.LaneID != "" {
		t.Fatalf("count line = %+v", last)
	}
}

// TestTailWarningsGentlerCurves pins the facts of three projects: the curves
// that deadlock, a tenth nearer to the chord, where some tails still exceed
// their lanes, and a quarter nearer, where each tail fits.
func TestTailWarningsGentlerCurves(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		nearer float64
		lanes  int
	}{{0, 70}, {0.10, 28}, {0.25, 0}} {
		config := railHubAllClasses(t, test.nearer)
		if err := project.Validate(config); err != nil {
			t.Fatal(test.nearer, err)
		}
		if got := len(config.Network.LaneTailOverruns()); got != test.lanes {
			t.Errorf("%g nearer: %d lanes, want %d", test.nearer, got, test.lanes)
		}
		if got := project.TailWarnings(config.Network); (len(got) == 0) != (test.lanes == 0) {
			t.Errorf("%g nearer: warnings = %+v", test.nearer, got)
		}
	}
}

// TestTailWarningsOneStraightLane shows that the tail is on the lanes that
// touch the nodes of a large lane: the straight lane fits its tail, and the
// curved lanes at its nodes do not.
func TestTailWarningsOneStraightLane(t *testing.T) {
	t.Parallel()
	config := scenarios.RailHub()
	for index := range config.Network.Lanes {
		if lane := &config.Network.Lanes[index]; lane.ID == "s01-through" {
			lane.VehicleClasses = allClasses(t)
		}
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	warnings := project.TailWarnings(config.Network)
	if len(warnings) == 0 || slices.Contains(warningLanes(warnings), "s01-through") {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// TestTailWarningsPresets checks each preset, and that none of them has a
// large class. A preset with a large class needs its own decision here.
func TestTailWarningsPresets(t *testing.T) {
	t.Parallel()
	compact, err := scenarios.CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		config project.Config
	}{
		{"small", scenarios.Small()},
		{"busy", scenarios.Busy()},
		{"parking-constrained", scenarios.ParkingConstrained()},
		{"rail-hub", scenarios.RailHub()},
		{"rail-hub-incidents", scenarios.RailHubIncidents()},
		{"scale100", scenarios.Scale100()},
		{"compact-station", compact},
		{"london-central", scenarios.LondonCentral()},
		{"london-full", scenarios.LondonFull()},
	} {
		if large := largeClasses(test.config.Network); large {
			t.Errorf("%s has a large class", test.name)
		}
		if got := project.TailWarnings(test.config.Network); len(got) != 0 {
			t.Errorf("%s: warnings = %+v", test.name, got)
		}
	}
}

func largeClasses(network sim.Network) bool {
	large := func(classes sim.ClassSet) bool {
		return classes.Allows(string(sim.GroupClass)) || classes.Allows(string(sim.ExpressClass))
	}
	for _, lane := range network.Lanes {
		if large(lane.VehicleClasses) {
			return true
		}
	}
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			if large(station.VehicleClasses) && large(berth.VehicleClasses) {
				return true
			}
		}
	}
	return false
}

// TestTailWarningsCap checks the boundary of the cap and the count line.
func TestTailWarningsCap(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		lanes int
		count string
	}{
		{project.MaxTailWarnings, ""},
		{project.MaxTailWarnings + 1, "1 more lane has a large-class tail longer than its length."},
		{project.MaxTailWarnings + 2, "2 more lanes have a large-class tail longer than their length."},
	} {
		classes := allClasses(t)
		network := sim.Network{Nodes: []sim.Node{{ID: "hub"}}}
		for index := range test.lanes {
			node := string(rune('a' + index))
			network.Nodes = append(network.Nodes, sim.Node{ID: node, Position: sim.Point{X: 100 + float64(index), Y: 0}})
			network.Lanes = append(network.Lanes, sim.Lane{ID: "lane-" + node, From: "hub", To: node, VehicleClasses: classes, Control: &sim.Point{X: 5, Y: 400}})
		}
		warnings := project.TailWarnings(network)
		named := warnings
		if test.count != "" {
			named = warnings[:len(warnings)-1]
			if got := warnings[len(warnings)-1]; got.Text != test.count || got.LaneID != "" {
				t.Errorf("%d lanes: count line = %+v", test.lanes, got)
			}
		}
		if len(named) != project.MaxTailWarnings || slices.Contains(warningLanes(named), "") {
			t.Errorf("%d lanes: warnings = %+v", test.lanes, warnings)
		}
	}
}
