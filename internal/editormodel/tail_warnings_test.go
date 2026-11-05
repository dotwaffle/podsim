package editormodel

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func allClassRailHub(t *testing.T) project.Config {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	config := scenarios.RailHub()
	for index := range config.Network.Lanes {
		config.Network.Lanes[index].VehicleClasses = classes
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

// TestDraftTailWarnings checks that a draft gets the warnings of the typed
// project, with a target for each named lane, and that the warnings do not
// make an error.
func TestDraftTailWarnings(t *testing.T) {
	t.Parallel()
	config := withUTurns(t, allClassRailHub(t), project.MaxTailWarnings+2)
	report := draftChecks(configDraft(t, config))
	if len(report.Errors) != 0 {
		t.Fatalf("errors: %+v", report.Errors)
	}
	want := project.TailWarnings(config.Network)
	if len(want) == 0 || len(report.Warnings) != len(want) {
		t.Fatalf("warnings = %+v, want %+v", report.Warnings, want)
	}
	for index, warning := range want {
		got := report.Warnings[index]
		if got.Text != warning.Text {
			t.Errorf("warning %d = %q, want %q", index, got.Text, warning.Text)
		}
		if warning.LaneID == "" && got.Target != nil {
			t.Errorf("count line has target %+v", got.Target)
		}
		if warning.LaneID != "" && (got.Target == nil || got.Target.Type != "lane" || got.Target.ID != warning.LaneID) {
			t.Errorf("warning %d has target %+v, want lane %s", index, got.Target, warning.LaneID)
		}
	}
}

func TestDraftTailWarningsSilent(t *testing.T) {
	t.Parallel()
	if report := draftChecks(configDraft(t, scenarios.RailHub())); len(report.Warnings) != 0 {
		t.Errorf("plain rail-hub: %+v", report.Warnings)
	}
	// A network error makes the geometry unreliable. An invalid class set
	// gives no typed network.
	badSpeed := configDraft(t, allClassRailHub(t))
	object(items(member(member(badSpeed, "network"), "lanes"))[0])["speedLimit"] = -1.0
	if report := draftChecks(badSpeed); len(report.Errors) == 0 || len(report.Warnings) != 0 {
		t.Errorf("network error: %+v", report)
	}
	badClass := configDraft(t, allClassRailHub(t))
	object(items(member(member(badClass, "network"), "lanes"))[0])["vehicleClasses"] = []any{"unknown"}
	if report := draftChecks(badClass); len(report.Errors) == 0 || len(report.Warnings) != 0 {
		t.Errorf("invalid class set: %+v", report)
	}
}

// TestEngineTailWarnings checks that the cached network checks keep the
// warnings, and that an edit of the network clears them.
func TestEngineTailWarnings(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, withUTurns(t, allClassRailHub(t), 1))
	for range 2 {
		report, err := model.draftChecks()
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Warnings) == 0 {
			t.Fatal("no warnings")
		}
	}
	synchronize(t, model, scenarios.RailHub())
	report, err := model.draftChecks()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("warnings after the edit: %+v", report.Warnings)
	}
}
