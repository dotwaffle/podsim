package scenarios

import "github.com/dotwaffle/podsim/internal/project"

// wharfHubsLine returns the line of LondonFull Shared Express Wharf Hubs:
// London Bridge and Waterloo, and Waterloo and Canary Wharf, with decisions
// HB-D19 C, HB-D20 A, and HB-D21 A of docs/design/london-hubs-d8-d9.md
// (section 13). The three hubs have their Express shape from the first two
// lines, so the line changes no geometry, and it adds no comb and no pod.
func wharfHubsLine() sharedTrackLine {
	return sharedTrackLine{
		name:   "LondonFull Shared Express Wharf Hubs",
		routes: wharfHubsRoutes(),
	}
}

// wharfHubsRoutes returns the shared routes of the line, in both
// directions. London Bridge and Waterloo pass Southwark on links 280 and
// 350. Waterloo and Canary Wharf take that route and the moves of London
// Bridge to the route of the wharf line.
func wharfHubsRoutes() []sharedTrackRoute {
	bridgeOut := []string{"london-link-350-ba-1", "london-link-350-ba-2", "940GZZLUSWK-move-02-01", "london-link-280-ba-1", "london-link-280-ba-2"}
	bridgeIn := []string{"london-link-280-ab-1", "london-link-280-ab-2", "940GZZLUSWK-move-01-02", "london-link-350-ab-1", "london-link-350-ab-2"}
	wharfOut := []string{"london-link-089-ba-1", "london-link-089-ba-2", "940GZZLUBMY-move-02-01", "london-link-088-ab-1", "london-link-088-ab-2", "940GZZLUCWR-move-01-02", "london-link-154-ab-1", "london-link-154-ab-2"}
	wharfIn := []string{"london-link-154-ba-1", "london-link-154-ba-2", "940GZZLUCWR-move-02-01", "london-link-088-ba-1", "london-link-088-ba-2", "940GZZLUBMY-move-01-02", "london-link-089-ab-1", "london-link-089-ab-2"}
	join := func(parts ...[]string) []string {
		var lanes []string
		for _, part := range parts {
			lanes = append(lanes, part...)
		}
		return lanes
	}
	return []sharedTrackRoute{
		{from: expressLondonBridge, to: expressWaterloo, lanes: join(
			[]string{expressLondonBridge + "-access-out", expressLondonBridge + "-road-out-04"}, bridgeIn,
			[]string{expressWaterloo + "-road-in-05", expressWaterloo + "-access-in"})},
		{from: expressWaterloo, to: expressLondonBridge, lanes: join(
			[]string{expressWaterloo + "-access-out", expressWaterloo + "-road-out-05"}, bridgeOut,
			[]string{expressLondonBridge + "-road-in-04", expressLondonBridge + "-access-in"})},
		{from: expressWaterloo, to: expressCanaryWharf, lanes: join(
			[]string{expressWaterloo + "-access-out", expressWaterloo + "-road-out-05"}, bridgeOut,
			[]string{expressLondonBridge + "-move-04-01"}, wharfOut,
			[]string{expressCanaryWharf + "-road-in-01", expressCanaryWharf + "-access-in"})},
		{from: expressCanaryWharf, to: expressWaterloo, lanes: join(
			[]string{expressCanaryWharf + "-access-out", expressCanaryWharf + "-road-out-01"}, wharfIn,
			[]string{expressLondonBridge + "-move-01-04"}, bridgeIn,
			[]string{expressWaterloo + "-road-in-05", expressWaterloo + "-access-in"})},
	}
}

// GenerateLondonFullSharedExpressWharfHubs returns the opt-in LondonFull
// Shared Express Wharf Hubs preset without a cache. It is LondonFull Shared
// Express Wharf with two more Express pairs on shared track, London Bridge
// and Waterloo, and Waterloo and Canary Wharf. The two Express networks of
// the wharf preset become one, and its 32 Express pods serve all services.
func GenerateLondonFullSharedExpressWharfHubs() (project.Config, error) {
	config, err := GenerateLondonFull()
	if err != nil {
		return project.Config{}, err
	}
	return londonFullSharedExpressWharfHubsPreset(config)
}

// londonFullSharedExpressWharfHubsPreset returns the preset from c, which is
// a LondonFull project that the caller owns.
func londonFullSharedExpressWharfHubsPreset(c project.Config) (project.Config, error) {
	return londonFullSharedTrack(c, sharedTrackPresetLine(), wharfLine(), wharfHubsLine())
}
