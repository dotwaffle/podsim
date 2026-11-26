package scenarios

import (
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	expressLondonBridge = "940GZZLULNB"
	expressCanaryWharf  = "940GZZLUCYF"
	// expressWharfParking is the Express parking beside London Bridge.
	expressWharfParking = "express-parking-lnb"
)

// wharfLine returns the line of LondonFull Shared Express Wharf: London
// Bridge and Canary Wharf, with decisions HB-D13 and HB-D14 of
// docs/design/london-hubs-d8-d9.md.
func wharfLine() sharedTrackLine {
	return sharedTrackLine{
		name:   "LondonFull Shared Express Wharf",
		routes: wharfRoutes(),
		geometry: sharedTrackGeometry{
			hubs:   []string{expressLondonBridge, expressCanaryWharf},
			part:   41,
			curves: []sharedTrackCurve{{lane: expressLondonBridge + "-road-out-01", offset: 30}},
		},
		parking: []sharedTrackParking{wharfParking()},
	}
}

// wharfRoutes returns the shared route between London Bridge and Canary
// Wharf, in both directions, through Bermondsey and Canada Water.
func wharfRoutes() []sharedTrackRoute {
	return []sharedTrackRoute{
		{from: expressLondonBridge, to: expressCanaryWharf, lanes: []string{
			expressLondonBridge + "-access-out", expressLondonBridge + "-road-out-01", "london-link-089-ba-1", "london-link-089-ba-2",
			"940GZZLUBMY-move-02-01", "london-link-088-ab-1", "london-link-088-ab-2", "940GZZLUCWR-move-01-02",
			"london-link-154-ab-1", "london-link-154-ab-2", expressCanaryWharf + "-road-in-01", expressCanaryWharf + "-access-in",
		}},
		{from: expressCanaryWharf, to: expressLondonBridge, lanes: []string{
			expressCanaryWharf + "-access-out", expressCanaryWharf + "-road-out-01", "london-link-154-ba-1", "london-link-154-ba-2",
			"940GZZLUCWR-move-02-01", "london-link-088-ba-1", "london-link-088-ba-2", "940GZZLUBMY-move-01-02",
			"london-link-089-ab-1", "london-link-089-ab-2", expressLondonBridge + "-road-in-01", expressLondonBridge + "-access-in",
		}},
	}
}

// wharfParking returns the site LB3 of section 9.3 of the design. It has no
// gateway, because it passes the layout audit with no exemption.
func wharfParking() sharedTrackParking {
	return sharedTrackParking{
		center: sim.Point{X: 2695.7, Y: 302.1}, heading: 70, part: 41, mirror: true,
		comb: expressComb{id: expressWharfParking, name: "Express Parking London Bridge", prefix: "xq", plane: "xq-parking"}, pods: "express-lnb",
		arrival: "london-link-089-b-arrival", departure: "london-link-089-b-departure",
	}
}

// generateLondonFullSharedExpressWharf returns the opt-in LondonFull Shared
// Express Wharf preset without a cache. It is LondonFull Shared Express
// with a second Express pair on shared track, London Bridge and Canary
// Wharf, and 16 more Express pods in a comb beside London Bridge.
func generateLondonFullSharedExpressWharf() (project.Config, error) {
	config, err := GenerateLondonFull()
	if err != nil {
		return project.Config{}, err
	}
	return londonFullSharedExpressWharfPreset(config)
}

// londonFullSharedExpressWharfPreset returns the preset from c, which is a
// LondonFull project that the caller owns.
func londonFullSharedExpressWharfPreset(c project.Config) (project.Config, error) {
	return londonFullSharedTrack(c, sharedTrackPresetLine(), wharfLine())
}
