package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCompareAuthoredGroupProject(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "internal", "project", "testdata", "group_public.json")
	caseStudy, err := loadScenario(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(caseStudy.fleet) != 1 || caseStudy.fleet[0].Class != sim.GroupClass || caseStudy.focus != "market" {
		t.Fatal("authored group class or focus lost", caseStudy.fleet, caseStudy.focus)
	}
	if _, fleetErr := sim.NewFleet(caseStudy.network, caseStudy.fleet); fleetErr != nil {
		t.Fatal("loaded group project cannot start", fleetErr)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if decodeErr := json.Unmarshal(raw, &config); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	config.Fleet[0].Class = sim.ExpressClass
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(t.TempDir(), "express.json")
	if writeErr := os.WriteFile(invalid, raw, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, loadErr := loadScenario(invalid, ""); loadErr == nil {
		t.Fatal("compare accepted unsupported express project")
	}
}
