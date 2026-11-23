package sim

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// This file is slice 0 of docs/design/london-hubs-d8-d9.md. It does not test
// the simulator. It runs only when the variable that hubPairEnv names holds
// the path of a london-full-shared-express project file from cmd/scenario.
// It runs the audit copy of the shared-track study on that preset, with
// Express on the route of each hub pair. When the variable that hubPairOutEnv
// names holds a directory, it writes there the network of each London Bridge
// fix that removes all refusals, for the site study of internal/scenarios.

const (
	hubPairEnv    = "PODSIM_HUB_PAIR_STUDY"
	hubPairOutEnv = "PODSIM_HUB_PAIR_OUT"
	// hubPairNetwork is the network SHA-256 of the preset.
	hubPairNetwork = "cba69149281760dbba334323497b9d94cb1d3a980a631b68de0be45e78aac6bb"

	studyLondonBridge = "940GZZLULNB"
	studyCanaryWharf  = "940GZZLUCYF"
	studyKingsCross   = "940GZZLUKSX"
	studyLiverpool    = "940GZZLULVT"
	studyOxford       = "940GZZLUOXC"
	studyPaddington   = "940GZZLUPAC"
	studyTottenham    = "940GZZLUTCR"
	studyVictoria     = "940GZZLUVIC"
	studyEuston       = "940GZZLUEUS"
	studyStratford    = "940GZZLUSTD"
	studyMoorgate     = "940GZZLUMGT"
)

func hubPairLoad(t *testing.T) Network {
	t.Helper()
	path := os.Getenv(hubPairEnv)
	if path == "" {
		t.Skip("set " + hubPairEnv + " to a london-full-shared-express project file to run the hub pair study")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Network Network `json:"network"`
	}
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	if hash := sharedTrackHash(t, project.Network); hash != hubPairNetwork {
		t.Fatalf("network SHA-256 %s, want %s", hash, hubPairNetwork)
	}
	return project.Network
}

// hubPairBase sets the study classes on the routes of a hub pair in each
// direction and on both hubs.
func hubPairBase(t *testing.T, preset Network, pair [2]string) Network {
	t.Helper()
	n := preset.clone()
	first, okFirst := n.Station(pair[0])
	second, okSecond := n.Station(pair[1])
	if !okFirst || !okSecond {
		t.Fatalf("unknown hub in %v", pair)
	}
	keep := make(map[string]bool)
	for _, route := range [][]Lane{shapeStudyHubRoute(t, n, first, second), shapeStudyHubRoute(t, n, second, first)} {
		for _, lane := range route {
			keep[lane.ID] = true
		}
	}
	setShapeStudyClasses(&n, keep, false, pair[0], pair[1])
	return n
}

// hubPairShape gives the new hubs of a pair their straight roads and 41 m
// parts. Waterloo and Bank have them in the preset.
func hubPairShape(t *testing.T, n Network, pair [2]string, change sharedTrackChange) Network {
	t.Helper()
	change.hubs, change.part = nil, 41
	for _, id := range pair {
		if id != sharedWaterloo && id != sharedBank {
			change.hubs = append(change.hubs, id)
		}
	}
	return change.apply(t, straightStudyHubs(n, change.hubs...))
}

// hubPairList returns the pairs of the study.
func hubPairList() [][2]string {
	return [][2]string{
		{studyLondonBridge, studyCanaryWharf},
		{sharedWaterloo, studyCanaryWharf},
		{studyLondonBridge, sharedWaterloo},
		{studyKingsCross, studyLiverpool},
		{studyKingsCross, sharedWaterloo},
		{studyEuston, sharedWaterloo},
		{studyPaddington, studyTottenham},
		{studyVictoria, studyOxford},
		{studyKingsCross, studyOxford},
		{studyLiverpool, studyTottenham},
		{studyKingsCross, studyVictoria},
		{studyCanaryWharf, studyStratford},
		{studyKingsCross, studyPaddington},
		{studyPaddington, studyLiverpool},
	}
}

// hubPairAudit runs the audit copy with the index of the code.
func hubPairAudit(t *testing.T, n Network) sharedTrackAuditResult {
	t.Helper()
	tails := indexGeometryTails(n)
	return sharedTrackAudit(t, n, tails, newEnvelopeReach(n, tails))
}

// TestHubPairStudyPairs runs the audit copy on the route of each pair with
// the hub shape of the design, and pins the refusals, the refusals under
// 4.0 m, the shape pairs, and the large lanes under 40 m.
func TestHubPairStudyPairs(t *testing.T) {
	preset := hubPairLoad(t)
	want := [][4]int{
		{1, 1, 49, 0},
		{1, 1, 62, 0},
		{0, 0, 60, 0},
		{4, 2, 72, 0},
		{7, 6, 98, 1},
		{7, 5, 80, 1},
		{10, 8, 107, 0},
		{11, 10, 79, 0},
		{15, 15, 98, 1},
		{18, 15, 84, 0},
		{24, 23, 118, 1},
		{31, 29, 134, 0},
		{32, 30, 124, 0},
		{35, 31, 145, 0},
	}
	for i, pair := range hubPairList() {
		n := hubPairShape(t, hubPairBase(t, preset, pair), pair, sharedTrackChange{})
		result := hubPairAudit(t, n)
		got := [4]int{len(result.refusals), result.underFour(), result.shapes, len(shortStudyLanes(n))}
		t.Logf("%v: refusals, under 4.0 m, shape pairs, and large lanes under 40 m %v %v", pair, got, shortStudyLanes(n))
		if got != want[i] {
			t.Errorf("%v: %v, want %v", pair, got, want[i])
		}
		for _, refusal := range result.refusals {
			t.Logf("  %s and %s, %.2f m, sweep %s, %s%s", refusal.first, refusal.second, refusal.distance, refusal.sweep, refusal.cause, refusal.crossing)
		}
	}
}

// hubPairFixes are the changes at London Bridge that the study compares.
func hubPairFixes() []hubPairFix {
	roadOut, reversal := studyLondonBridge+"-road-out-01", studyLondonBridge+"-move-01-01"
	turn := func(degrees float64) sharedTrackChange {
		return sharedTrackChange{turn: map[string]float64{studyLondonBridge: degrees}}
	}
	return []hubPairFix{
		{"remove", sharedTrackChange{remove: []string{reversal}}, 0},
		{"curve-20", sharedTrackChange{curve: roadOut, offset: -20}, 0},
		{"curve-10", sharedTrackChange{curve: roadOut, offset: -10}, 1},
		{"curve+20", sharedTrackChange{curve: roadOut, offset: 20}, 1},
		{"curve+30", sharedTrackChange{curve: roadOut, offset: 30}, 0},
		{"curve+50", sharedTrackChange{curve: roadOut, offset: 50}, 0},
		{"turn-20", turn(-20), 0},
		{"turn-15", turn(-15), 1},
		{"turn+30", turn(30), 0},
		{"throat-140", sharedTrackChange{throat: map[string]float64{studyLondonBridge: 140}}, 1},
	}
}

type hubPairFix struct {
	name     string
	change   sharedTrackChange
	refusals int
}

// TestHubPairStudyFixes runs the audit copy on London Bridge and Canary
// Wharf with each fix, pins the refusals, and logs the free-flow cost of
// each fix to the station routes against the same hub shape with no fix.
// With no refusal, ValidateBankGeometry must accept the network too.
func TestHubPairStudyFixes(t *testing.T) {
	pair := [2]string{studyLondonBridge, studyCanaryWharf}
	base := hubPairBase(t, hubPairLoad(t), pair)
	before := newSharedTrackCosts(hubPairShape(t, base, pair, sharedTrackChange{}))
	for _, fix := range hubPairFixes() {
		n := hubPairShape(t, base, pair, fix.change)
		result := hubPairAudit(t, n)
		err := n.ValidateBankGeometry()
		if len(result.refusals) != fix.refusals || (err == nil) != (fix.refusals == 0) {
			t.Errorf("%s: %d refusals, want %d, and ValidateBankGeometry %v", fix.name, len(result.refusals), fix.refusals, err)
		}
		after := newSharedTrackCosts(n)
		longer, unreachable, worst, total := 0, 0, 0.0, 0.0
		for i, row := range after.cost {
			for j, cost := range row {
				delta := cost - before.cost[i][j]
				switch {
				case i == j:
				case math.IsInf(cost, 1) && !math.IsInf(before.cost[i][j], 1):
					unreachable++
				case delta > 1e-9:
					longer, worst, total = longer+1, max(worst, delta), total+delta
				}
			}
		}
		t.Logf("%s: %d refusals, %d shape pairs, %d of %d station routes longer, %d unreachable, worst +%.1f s, total +%.1f s", fix.name, len(result.refusals), result.shapes, longer, len(before.cost)*(len(before.cost)-1), unreachable, worst, total)
		if dir := os.Getenv(hubPairOutEnv); dir != "" && fix.refusals == 0 {
			writeStudyNetwork(t, dir, fix.name, n)
		}
	}
}

// writeStudyNetwork writes a network for the study of internal/scenarios.
func writeStudyNetwork(t *testing.T, dir, name string, n Network) {
	t.Helper()
	data, err := json.Marshal(map[string]Network{"network": n}, jsonv1.DefaultOptionsV1())
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
}
