package project_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
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

// withUTurns adds a U-turn loop to the middle of the first count links of
// a rail-hub project. Each loop leaves a link node and comes back to it. The U-turn has a
// chord of 10 m and a length of 101 m, so its tail exceeds its length.
func withUTurns(t *testing.T, config project.Config, count int) project.Config {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]sim.Point, len(config.Network.Nodes))
	for _, node := range config.Network.Nodes {
		positions[node.ID] = node.Position
	}
	loops := 0
	for index := range config.Network.Lanes {
		if loops == count {
			break
		}
		lane := &config.Network.Lanes[index]
		if !strings.HasPrefix(lane.ID, "link-") {
			continue
		}
		loops++
		from, to := positions[lane.From], positions[lane.To]
		middle := sim.Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
		radius := math.Hypot(middle.X, middle.Y)
		out := sim.Point{X: middle.X / radius, Y: middle.Y / radius}
		across := sim.Point{X: -out.Y, Y: out.X}
		at := func(outward, sideways float64) sim.Point {
			return sim.Point{X: middle.X + outward*out.X + sideways*across.X, Y: middle.Y + outward*out.Y + sideways*across.Y}
		}
		prefix := fmt.Sprintf("u%02d", loops)
		hub, in, back := prefix+"-hub", prefix+"-in", prefix+"-back"
		end := lane.To
		lane.To = hub
		speed, control := lane.SpeedLimit, at(200, 0)
		config.Network.Nodes = append(config.Network.Nodes,
			sim.Node{ID: hub, Position: middle}, sim.Node{ID: in, Position: at(100, 5)}, sim.Node{ID: back, Position: at(100, -5)})
		config.Network.Lanes = append(config.Network.Lanes,
			sim.Lane{ID: prefix + "-link", From: hub, To: end, SpeedLimit: speed, VehicleClasses: classes},
			sim.Lane{ID: prefix + "-out", From: hub, To: in, SpeedLimit: speed, VehicleClasses: classes},
			sim.Lane{ID: prefix + "-bend", From: in, To: back, SpeedLimit: speed, VehicleClasses: classes, Control: &control},
			sim.Lane{ID: prefix + "-return", From: back, To: hub, SpeedLimit: speed, VehicleClasses: classes})
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

// TestTailWarningsRailHubAllClasses checks the project that stopped with the
// projection bound: each tail of its lanes fits its lane.
func TestTailWarningsRailHubAllClasses(t *testing.T) {
	t.Parallel()
	config := railHubAllClasses(t, 0)
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if warnings := project.TailWarnings(config.Network); len(warnings) != 0 {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// TestTailWarningsUTurns pins the text of a lane with a tail longer than its
// length, and the count line. The warning does not refuse the project.
func TestTailWarningsUTurns(t *testing.T) {
	t.Parallel()
	config := withUTurns(t, railHubAllClasses(t, 0), project.MaxTailWarnings+2)
	if err := project.Validate(config); err != nil {
		t.Fatal("the warning must not refuse the project:", err)
	}
	warnings := project.TailWarnings(config.Network)
	if len(warnings) != project.MaxTailWarnings+1 {
		t.Fatalf("got %d warnings: %+v", len(warnings), warnings)
	}
	if want := "Lane u01-bend has a large-class tail of 101.5 m on a length of 101.0 m, so the lane holds one pod."; warnings[0].Text != want || warnings[0].LaneID != "u01-bend" {
		t.Fatalf("first warning = %+v", warnings[0])
	}
	last := warnings[project.MaxTailWarnings]
	if want := "2 more lanes have a large-class tail longer than their length."; last.Text != want || last.LaneID != "" {
		t.Fatalf("count line = %+v", last)
	}
}

// TestTailWarningsGentlerCurves pins the facts of three projects with curves
// that stopped or warned with the projection bound. Each tail now fits.
func TestTailWarningsGentlerCurves(t *testing.T) {
	t.Parallel()
	for _, nearer := range []float64{0, 0.10, 0.25} {
		config := railHubAllClasses(t, nearer)
		if err := project.Validate(config); err != nil {
			t.Fatal(nearer, err)
		}
		if got := config.Network.LaneTailOverruns(); len(got) != 0 {
			t.Errorf("%g nearer: overruns = %+v", nearer, got)
		}
	}
}

// TestTailWarningsOneStraightLane shows that the curved lanes at the nodes of
// one large straight lane fit their tails.
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
	if warnings := project.TailWarnings(config.Network); len(warnings) != 0 {
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
			network.Nodes = append(network.Nodes, sim.Node{ID: node, Position: sim.Point{X: 10 + float64(index), Y: 0}})
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
