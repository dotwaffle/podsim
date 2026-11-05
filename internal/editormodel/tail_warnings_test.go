package editormodel

import (
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

// TestDraftTailWarnings checks that a draft gets the warnings of the typed
// project, with a target for each named lane, and that the warnings do not
// make an error.
func TestDraftTailWarnings(t *testing.T) {
	t.Parallel()
	config := allClassRailHub(t)
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
	synchronize(t, model, allClassRailHub(t))
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
