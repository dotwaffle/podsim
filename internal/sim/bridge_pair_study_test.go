package sim

import (
	"encoding/json/v2"
	"math"
	"os"
	"slices"
	"testing"
)

// This file is the London Bridge and Waterloo study of HB-D15 in
// docs/design/london-hubs-d8-d9.md (section 13). It does not test the
// simulator. It runs only when the variable that bridgePairEnv names holds
// the path of a london-full:wharf project file from
// cmd/scenario. When the variable that hubPairOutEnv names holds a
// directory, it writes there the network of each arm, for the study of
// internal/scenarios.

const (
	bridgePairEnv = "PODSIM_BRIDGE_PAIR_STUDY"
	// bridgePairNetwork is the network SHA-256 of the preset.
	bridgePairNetwork = "d68a69b8c326f292f7c3a7839b51bf9837c5e714287f60470436781705217359"
)

func bridgePairLoad(t *testing.T) Network {
	t.Helper()
	path := os.Getenv(bridgePairEnv)
	if path == "" {
		t.Skip("set " + bridgePairEnv + " to a london-full:wharf project file to run the bridge pair study")
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
	if hash := sharedTrackHash(t, project.Network); hash != bridgePairNetwork {
		t.Fatalf("network SHA-256 %s, want %s", hash, bridgePairNetwork)
	}
	return project.Network
}

// bridgePairArm adds Express to the routes of the pairs. The three hubs have
// their Express shape in the preset.
type bridgePairArm struct {
	name  string
	pairs [][2]string
	// want is the refusals, the refusals under 4.0 m, the shape pairs, and
	// the large lanes under 40 m.
	want [4]int
}

func bridgePairArms() []bridgePairArm {
	bridge := [2]string{studyLondonBridge, sharedWaterloo}
	wharf := [2]string{sharedWaterloo, studyCanaryWharf}
	return []bridgePairArm{
		{"preset", nil, [4]int{0, 0, 53, 0}},
		{"bridge", [][2]string{bridge}, [4]int{0, 0, 66, 0}},
		{"wharf", [][2]string{wharf}, [4]int{0, 0, 65, 0}},
		{"both", [][2]string{bridge, wharf}, [4]int{0, 0, 67, 0}},
	}
}

// TestBridgePairStudy runs the audit copy on each arm, and requires
// ValidateBankGeometry to agree with it. It logs the Express route of each
// pair, and the free-flow cost of the station routes against the preset.
func TestBridgePairStudy(t *testing.T) {
	preset := bridgePairLoad(t)
	before := newSharedTrackCosts(preset)
	for _, arm := range bridgePairArms() {
		n := preset
		for _, pair := range arm.pairs {
			n = hubPairBase(t, n, pair)
		}
		result := hubPairAudit(t, n)
		short := shortStudyLanes(n)
		got := [4]int{len(result.refusals), result.underFour(), result.shapes, len(short)}
		err := n.ValidateBankGeometry()
		t.Logf("%s: refusals, under 4.0 m, shape pairs, and large lanes under 40 m %v %v, ValidateBankGeometry %v", arm.name, got, short, err)
		if got != arm.want || (err == nil) != (got[0] == 0) {
			t.Errorf("%s: %v, want %v", arm.name, got, arm.want)
		}
		for _, refusal := range result.refusals {
			t.Logf("  %s and %s, %.2f m, sweep %s, %s%s", refusal.first, refusal.second, refusal.distance, refusal.sweep, refusal.cause, refusal.crossing)
		}
		for _, pair := range arm.pairs {
			for _, way := range [][2]string{pair, {pair[1], pair[0]}} {
				from, _ := n.Station(way[0])
				to, _ := n.Station(way[1])
				logBridgeRoute(t, n, shapeStudyHubRoute(t, n, from, to), way)
			}
		}
		if after := newSharedTrackCosts(n); !slices.EqualFunc(before.cost, after.cost, slices.Equal[[]float64]) {
			t.Errorf("%s: the station routes change", arm.name)
		}
		if dir := os.Getenv(hubPairOutEnv); dir != "" && len(arm.pairs) > 0 && got[0] == 0 {
			writeStudyNetwork(t, dir, "bridge-"+arm.name, n)
		}
	}
}

// logBridgeRoute logs the chord length and the lanes of an Express route.
func logBridgeRoute(t *testing.T, n Network, route []Lane, way [2]string) {
	t.Helper()
	positions := indexNodePositions(n.Nodes)
	meters, stations := 0.0, 0
	var ids []string
	for _, lane := range route {
		a, b := positions[lane.From], positions[lane.To]
		meters += math.Hypot(a.X-b.X, a.Y-b.Y)
		ids = append(ids, lane.ID)
		if lane.StationID != "" {
			stations++
		}
	}
	t.Logf("  %s to %s: %d lanes, %.0f m by chord, %d lanes of stations: %v", way[0], way[1], len(route), meters, stations, ids)
}
