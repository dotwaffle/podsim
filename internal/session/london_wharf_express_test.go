package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
)

// The tests of this file give the evidence of rows HX4, HX5, HX8, HX9, and
// HX13 of the Express wharf qualification on the preset
// london-full:wharf. They run the shared-track tests with
// demand on both lines of the preset, and the states of each line.

// wharfExpress is the preset london-full:wharf.
var wharfExpress = sharedExpressPreset{
	generate: scenarios.LondonFullWharf.Generate, services: 4, express: 32,
	lines: []sharedExpressLine{sharedExpressBankLine, {
		from: "940GZZLULNB", to: "940GZZLUCYF", link: []string{
			"london-link-089-ab-1", "london-link-089-ab-2", "london-link-089-ba-1", "london-link-089-ba-2",
			"london-link-088-ab-1", "london-link-088-ab-2", "london-link-088-ba-1", "london-link-088-ba-2",
			"london-link-154-ab-1", "london-link-154-ab-2", "london-link-154-ba-1", "london-link-154-ba-2",
		},
		parking: "express-parking-lnb", prefix: "xq-", pod: "express-lnb-01", on: "links 089, 088, and 154",
	}},
}

// runWharfExpress runs a shared-track test on each line of the preset.
// These tests run only in the task test:express-wharf, and not with -short
// or -race.
func runWharfExpress(t *testing.T, run func(*testing.T, sharedExpressPreset, int)) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("the wharf Express tests run without -short or -race")
	}
	for i, line := range wharfExpress.lines {
		t.Run(line.from, func(t *testing.T) {
			t.Parallel()
			run(t, wharfExpress, i)
		})
	}
}

// TestLondonWharfExpressPhysicalCheckpoints is
// TestLondonSharedExpressPhysicalCheckpoints on the preset (rows HX4 and
// HX9).
func TestLondonWharfExpressPhysicalCheckpoints(t *testing.T) {
	t.Parallel()
	runWharfExpress(t, runSharedExpressCheckpoints)
}

// TestLondonWharfExpressIncidentFixtures is
// TestLondonSharedExpressIncidentFixtures on the preset (rows HX8 and HX9).
func TestLondonWharfExpressIncidentFixtures(t *testing.T) {
	t.Parallel()
	runWharfExpress(t, runSharedExpressIncidents)
}

// TestLondonWharfExpressByteBounds is TestLondonSharedExpressByteBounds on
// the preset (rows HX5 and HX13).
func TestLondonWharfExpressByteBounds(t *testing.T) {
	t.Parallel()
	runWharfExpress(t, runSharedExpressBounds)
}

// TestLondonWharfExpressRecoveryChain is
// TestLondonSharedExpressRecoveryChain on the preset (row HX8).
func TestLondonWharfExpressRecoveryChain(t *testing.T) {
	t.Parallel()
	runWharfExpress(t, runSharedExpressRecoveryChain)
}
