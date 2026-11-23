package scenarios

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// hubPairLayoutEnv names the directory that TestHubPairStudyFixes of
// internal/sim writes. The tests of this file are slice 0 of
// docs/design/london-hubs-d8-d9.md, and they skip without it.
const hubPairLayoutEnv = "PODSIM_HUB_PAIR_LAYOUT"

const bakerLayoutEnv = "PODSIM_BAKER_LAYOUT"

const (
	hubPairLondonBridge = "940GZZLULNB"
	hubPairCanaryWharf  = "940GZZLUCYF"
	hubPairComb         = "express-parking-hub"
)

// hubPairSite is a comb site. The comb is that of addSharedTrackParking with
// parts of 41 m, and mirror swaps the sides of its entry and exit.
type hubPairSite struct {
	name, link string
	center     sim.Point
	heading    float64
	mirror     bool
}

// hubPairSites are sites from a scratch screen of center and heading.
var hubPairSites = []hubPairSite{
	{"LB1", "london-link-089-b", sim.Point{X: 2678.6, Y: 290.1}, 50, true},
	{"LB2", "london-link-089-b", sim.Point{X: 2728.3, Y: 273.3}, 80, true},
	{"LB3", "london-link-089-b", sim.Point{X: 2695.7, Y: 302.1}, 70, true},
	{"LB4", "london-link-089-b", sim.Point{X: 2627.1, Y: 351.4}, 30, false},
	{"CW1", "london-link-154-b", sim.Point{X: 7390.9, Y: 548.8}, 90, false},
}

// parking returns the comb of the site, with no exemption in the layout audit.
func (s hubPairSite) parking() sharedTrackParking {
	return sharedTrackParking{
		center: s.center, heading: s.heading, part: 41, mirror: s.mirror,
		comb: expressComb{id: hubPairComb, name: "Express Parking Hub", prefix: "xq", plane: "xq-parking"}, pods: "express-hub",
		arrival: s.link + "-arrival", departure: s.link + "-departure",
	}
}

// TestHubPairStudySites runs the layout audit of the preset, with no gateway
// for the new comb, on each fix network, and then the layout audit, the bank
// audit, project validation, the parking gaps, the tail warnings, and the
// Express routes with the comb at each site.
func TestHubPairStudySites(t *testing.T) {
	dir := os.Getenv(hubPairLayoutEnv)
	if dir == "" {
		t.Skip("set " + hubPairLayoutEnv + " to the output directory of TestHubPairStudyFixes")
	}
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	preset, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no network in %s: %v", dir, err)
	}
	slices.Sort(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fix struct {
			Network sim.Network `json:"network"`
		}
		if err := jsonv2.Unmarshal(data, &fix); err != nil {
			t.Fatal(err)
		}
		conflicts := auditLondonLayout(fix.Network, sharedTrackLayoutInput(source, sharedTrackPresetParking()))
		t.Logf("%s with no comb: layout conflicts %d, %v", filepath.Base(path), len(conflicts), layoutError(fix.Network, conflicts))
		for _, site := range hubPairSites {
			t.Run(filepath.Base(path)+" "+site.name, func(t *testing.T) {
				c := preset
				c.Network = fix.Network
				c.Network.Nodes, c.Network.Lanes, c.Network.Stations = slices.Clone(c.Network.Nodes), slices.Clone(c.Network.Lanes), slices.Clone(c.Network.Stations)
				c.Fleet, c.ExpressServices = slices.Clone(c.Fleet), slices.Clone(c.ExpressServices)
				for _, pair := range [][2]string{{hubPairLondonBridge, hubPairCanaryWharf}, {hubPairCanaryWharf, hubPairLondonBridge}} {
					c.ExpressServices = append(c.ExpressServices, sim.ExpressService{ID: "xs-" + pair[0][8:] + "-" + pair[1][8:], From: pair[0], To: pair[1], Class: sim.ExpressClass, PartyLimit: 20})
				}
				if err := addSharedTrackParking(&c, site.parking()); err != nil {
					t.Fatal(err)
				}
				stage, err := hubPairCheck(t, c, source)
				t.Logf("refused at %q: %v", stage, err)
				if want := hubPairRefusal(strings.TrimSuffix(filepath.Base(path), ".json"), site.name); stage != want {
					t.Errorf("refused at %q, want %q", stage, want)
				}
			})
		}
	}
}

// hubPairRefusal returns the stage that refuses a site on a fix network. The
// turn of +30 degrees has a layout conflict with no comb. With the curve
// of -20 m or the turn of -20 degrees, the road of London Bridge crosses the
// road of each comb on link 089.
func hubPairRefusal(fix, site string) string {
	if fix == "turn+30" || strings.HasPrefix(site, "LB") && (fix == "curve-20" || fix == "turn-20") {
		return "layout"
	}
	return ""
}

// hubPairCheck returns the first stage that refuses c, and its error. When
// no stage refuses c, it logs the static measures of the site.
func hubPairCheck(t *testing.T, c project.Config, source londonSource) (string, error) {
	t.Helper()
	if err := layoutError(c.Network, auditLondonLayout(c.Network, sharedTrackLayoutInput(source, sharedTrackPresetParking()))); err != nil {
		return "layout", err
	}
	shapes, err := c.Network.BankGeometryShapePairs()
	if err == nil {
		err = c.Network.ValidateBankGeometry()
	}
	if err != nil {
		return "bank", err
	}
	if err := project.Validate(c); err != nil {
		return "project", err
	}
	gaps := project.ParkingGaps(c)
	length := func(from, to string) string {
		route, err := c.Network.RouteForClass(from, to, sim.ExpressClass)
		if err != nil {
			return "none"
		}
		meters := 0.0
		for _, lane := range route {
			a, _ := c.Network.Node(lane.From)
			b, _ := c.Network.Node(lane.To)
			meters += math.Hypot(a.Position.X-b.Position.X, a.Position.Y-b.Position.Y)
		}
		return fmt.Sprintf("%.0f", meters)
	}
	t.Logf("%d shape pairs, %d parking gaps, %d tail warnings", shapes, len(gaps), len(project.TailWarnings(c.Network)))
	for _, id := range []string{hubPairLondonBridge, hubPairCanaryWharf, expressWaterloo} {
		hub, _ := c.Network.Station(id)
		t.Logf("chord route meters of %s: from the comb %s, to the comb %s", id, length("xq-01-node", hub.Berths[0].Node), length(hub.Berths[0].Node, "xq-01-node"))
	}
	return "", nil
}

// TestBakerStudyLayout runs the layout audit and project validation on the
// preset with a network of TestBakerStudyCosts or TestPairTwoStudy of
// internal/sim. The variable that bakerLayoutEnv names holds its file.
func TestBakerStudyLayout(t *testing.T) {
	path := os.Getenv(bakerLayoutEnv)
	if path == "" {
		t.Skip("set " + bakerLayoutEnv + " to a network file of TestBakerStudyCosts or TestPairTwoStudy")
	}
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	config, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	before := len(project.TailWarnings(config.Network))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Network sim.Network `json:"network"`
	}
	if err := jsonv2.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	config.Network = file.Network
	if err := layoutError(config.Network, auditLondonLayout(config.Network, sharedTrackLayoutInput(source, sharedTrackPresetParking()))); err != nil {
		t.Error(err)
	}
	// A pair with no comb has parking gaps, and validation refuses them.
	if err := project.Validate(config); err != nil && len(project.ParkingGaps(config)) == 0 {
		t.Error(err)
	}
	t.Logf("%d tail warnings (%d in the preset), %d parking gaps", len(project.TailWarnings(config.Network)), before, len(project.ParkingGaps(config)))
}
