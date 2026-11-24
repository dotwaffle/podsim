package scenarios

import (
	jsonv2 "encoding/json/v2"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// bridgeLayoutEnv names the directory that TestBridgePairStudy of
// internal/sim writes. The test of this file skips without it.
const bridgeLayoutEnv = "PODSIM_BRIDGE_PAIR_LAYOUT"

// TestBridgePairStudyProjects runs the layout audit, the bank audit, project
// validation, and the parking gaps on each arm, with the services of the
// arm, and the 16 and 16 Express pods of the preset. It requires each pair
// of combs to have a route in both directions, in contrast to the preset.
func TestBridgePairStudyProjects(t *testing.T) {
	dir := os.Getenv(bridgeLayoutEnv)
	if dir == "" {
		t.Skip("set " + bridgeLayoutEnv + " to the output directory of TestBridgePairStudy")
	}
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	preset, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	if combsJoined(preset.Network) {
		t.Error("the combs of the preset have a route between them")
	}
	for _, arm := range []struct {
		name  string
		pairs [][2]string
	}{
		{"bridge", [][2]string{{hubPairLondonBridge, expressWaterloo}}},
		{"wharf", [][2]string{{expressWaterloo, hubPairCanaryWharf}}},
		{"both", [][2]string{{hubPairLondonBridge, expressWaterloo}, {expressWaterloo, hubPairCanaryWharf}}},
	} {
		t.Run(arm.name, func(t *testing.T) {
			c := preset
			c.Network = readBridgeNetwork(t, filepath.Join(dir, "bridge-"+arm.name+".json"))
			c.ExpressServices = slices.Clone(c.ExpressServices)
			for _, pair := range arm.pairs {
				for _, way := range [][2]string{pair, {pair[1], pair[0]}} {
					c.ExpressServices = append(c.ExpressServices, sim.ExpressService{ID: "xs-" + way[0][8:] + "-" + way[1][8:], From: way[0], To: way[1], Class: sim.ExpressClass, PartyLimit: 20})
				}
			}
			stage, err := hubPairCheck(t, c, source)
			gaps := project.ParkingGaps(c)
			t.Logf("refused at %q: %v; %d parking gaps, %d services", stage, err, len(gaps), len(c.ExpressServices))
			if stage != "" || len(gaps) != 0 {
				t.Error("the arm is refused")
			}
			if !combsJoined(c.Network) {
				t.Error("no route between the combs")
			}
			logCombDistances(t, c.Network)
		})
	}
}

func readBridgeNetwork(t *testing.T, path string) sim.Network {
	t.Helper()
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
	return file.Network
}

// combsJoined reports whether an Express pod has a route between the combs
// in both directions.
func combsJoined(n sim.Network) bool {
	for _, way := range [][2]string{{"xp-01-node", "xq-01-node"}, {"xq-01-node", "xp-01-node"}} {
		if _, err := n.RouteForClass(way[0], way[1], sim.ExpressClass); err != nil {
			return false
		}
	}
	return true
}

// logCombDistances logs the chord length of the Express route between each
// comb and the first berth of each hub, and the order of the parking
// stations in the network.
func logCombDistances(t *testing.T, n sim.Network) {
	t.Helper()
	var order []string
	for _, station := range n.Stations {
		if station.ParkingOnly {
			order = append(order, station.ID)
		}
	}
	t.Logf("parking stations in network order: %v", order)
	for _, comb := range []string{"xp-01-node", "xq-01-node"} {
		for _, hub := range []string{expressWaterloo, expressBank, hubPairLondonBridge, hubPairCanaryWharf} {
			station, _ := n.Station(hub)
			meters := func(from, to string) float64 {
				route, err := n.RouteForClass(from, to, sim.ExpressClass)
				if err != nil {
					return math.NaN()
				}
				total := 0.0
				for _, lane := range route {
					a, _ := n.Node(lane.From)
					b, _ := n.Node(lane.To)
					total += math.Hypot(a.Position.X-b.Position.X, a.Position.Y-b.Position.Y)
				}
				return total
			}
			t.Logf("%s: to %s %.0f m, from it %.0f m", comb, hub, meters(comb, station.Berths[0].Node), meters(station.Berths[0].Node, comb))
		}
	}
}
