package sim

import "testing"

// The tests of this file give the evidence of rows HX2, HX3, HX4, and HX9
// of the Express wharf qualification on the preset
// london-full-shared-express-wharf. They run the checks of the shared-track
// tests on both lines of the preset. TestWharfExpressFixture in package
// scenarios writes the preset to testdata.

const (
	wharfExpressLondonBridge, wharfExpressCanaryWharf = "940GZZLULNB", "940GZZLUCYF"
	wharfExpressName                                  = "london-full-shared-express-wharf"
	// wharfExpressRunTicks is the length of TestWharfExpressEnvelopeRun, 8
	// simulated minutes. The first Express pod is on link 154 with a
	// Compact pod after about 5 minutes.
	wharfExpressRunTicks = 8 * 60 * TicksPerSecond
)

// readWharfExpressProject reads testdata/wharf_express_project.json.gz.
func readWharfExpressProject(t *testing.T) sharedExpressProject {
	t.Helper()
	p := readExpressProject(t, "testdata/wharf_express_project.json.gz")
	if len(p.Fleet) != 359 || len(p.ExpressServices) != 4 {
		t.Fatalf("the fixture is not the preset: %d pods, %d services", len(p.Fleet), len(p.ExpressServices))
	}
	p.lines = []sharedExpressLine{sharedExpressBankLine, {from: wharfExpressLondonBridge, to: wharfExpressCanaryWharf, links: []int{1, 2, 3}}}
	return p
}

// skipWharfExpress skips a test of the preset with -short or -race. Only
// the task test:wharf-express runs it.
func skipWharfExpress(t *testing.T) {
	t.Helper()
	if testing.Short() || raceEnabled {
		t.Skip("the wharf Express tests run without -short or -race")
	}
}

// wharfExpressNetwork returns the network of the preset for the tests that
// take several networks, and whether the test runs it.
func wharfExpressNetwork(t *testing.T) (Network, bool) {
	t.Helper()
	if testing.Short() || raceEnabled {
		return Network{}, false
	}
	return readWharfExpressProject(t).Network, true
}

// TestWharfExpressEnvelopeRun is TestSharedExpressEnvelopeRun on both lines
// of the preset (row HX3), with mixed traffic on links 100, 089, 088, and
// 154, and with a fault, an emergency, and the restores on each line.
func TestWharfExpressEnvelopeRun(t *testing.T) {
	t.Parallel()
	skipWharfExpress(t)
	runSharedExpressEnvelopes(t, readWharfExpressProject(t), wharfExpressRunTicks)
}

// TestWharfExpressShapePairSweep is TestSharedExpressShapePairSweep on the
// preset (row HX2).
func TestWharfExpressShapePairSweep(t *testing.T) {
	t.Parallel()
	skipWharfExpress(t)
	runSharedExpressSweep(t, readWharfExpressProject(t), 53)
}

// TestWharfExpressDwell is TestSharedExpressDwell on each line of the
// preset (row HX9).
func TestWharfExpressDwell(t *testing.T) {
	skipWharfExpress(t)
	p := readWharfExpressProject(t)
	for _, line := range p.lines {
		t.Run(line.from, func(t *testing.T) { runSharedExpressDwell(t, p, line) })
	}
}

// TestWharfExpressOccupiedPickupPolicy is
// TestSharedExpressOccupiedPickupPolicy on the preset (row HX4), with the
// stops of the shared-track test and with a pickup at Bermondsey, between
// London Bridge and Canary Wharf.
func TestWharfExpressOccupiedPickupPolicy(t *testing.T) {
	t.Parallel()
	skipWharfExpress(t)
	p := readWharfExpressProject(t)
	for _, stops := range [][3]string{{"940GZZLUCHL", "940GZZLUSPU", sharedExpressBank}, {wharfExpressLondonBridge, "940GZZLUBMY", wharfExpressCanaryWharf}} {
		t.Run(stops[2], func(t *testing.T) {
			t.Parallel()
			runSharedExpressOccupiedPickup(t, p, stops[0], stops[1], stops[2])
		})
	}
}
