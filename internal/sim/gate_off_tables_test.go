package sim

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// gateOffTablesPath holds the digests of TestGateOffTables. Run the test
// with -update to write it again.
const gateOffTablesPath = "testdata/gate_off_tables.json"

// tableDigest is the number of entries of a static table and the SHA-256 of
// its text. The text of a map is sorted. The junction conflicts keep the
// order of each lane, in lane ID order (see encodeJunctionConflicts). A table that the network does not build has the digest
// "nil".
type tableDigest struct {
	Count  int    `json:"count"`
	SHA256 string `json:"sha256"`
}

// gateOffTables holds the results of the network audits and the digests of
// the static safety tables of one network. Audit is the result of
// PrepareNetwork, and BankAudit the result of ValidateBankGeometry. Each is
// "accepted" or the refusal text. A refused network has no tables.
type gateOffTables struct {
	Audit             string       `json:"audit"`
	BankAudit         string       `json:"bankAudit"`
	DepotLateral      *tableDigest `json:"depotLateral,omitempty"`
	MergePairs        *tableDigest `json:"mergePairs,omitempty"`
	MergeExtents      *tableDigest `json:"mergeExtents,omitempty"`
	JunctionConflicts *tableDigest `json:"junctionConflicts,omitempty"`
}

// digestLines returns the digest of lines in their order.
func digestLines(lines []string) *tableDigest {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return &tableDigest{Count: len(lines), SHA256: hex.EncodeToString(sum[:])}
}

// floatBits writes a float as the exact bits of its value.
func floatBits(value float64) string {
	return strconv.FormatUint(math.Float64bits(value), 16)
}

// keyNames returns a function that names a separation key by the ID of its
// lane or berth.
func keyNames(network Network) func(separationKey) string {
	var berths []string
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			berths = append(berths, berth.ID)
		}
	}
	return func(key separationKey) string {
		switch key.kind {
		case laneKeyKind:
			return "lane:" + network.Lanes[key.index].ID
		case berthKeyKind:
			return "berth:" + berths[key.index]
		case noKeyKind:
		}
		return fmt.Sprintf("key:%d:%d", key.kind, key.index)
	}
}

// pairDigest returns the digest of the pairs of a lateral table, with the
// clearance of each pair, or "nil" when the network builds no table.
func pairDigest(table *depotLateral, name func(separationKey) string) *tableDigest {
	if table == nil {
		return &tableDigest{SHA256: "nil"}
	}
	lines := make([]string, 0, len(table.pairs))
	for pair, clearance := range table.pairs {
		lines = append(lines, fmt.Sprintf("%s %s %s", name(pair[0]), name(pair[1]), floatBits(clearance)))
	}
	slices.Sort(lines)
	return digestLines(lines)
}

// extentDigest returns the digest of the extents of the lateral merge
// pairs, or "nil" when the network builds no merge table.
func extentDigest(table *depotLateral, network Network) *tableDigest {
	if table == nil {
		return &tableDigest{SHA256: "nil"}
	}
	lines := make([]string, 0, len(table.extents))
	for lanes, extent := range table.extents {
		lines = append(lines, fmt.Sprintf("%s %s %s %s", network.Lanes[lanes[0]].ID, network.Lanes[lanes[1]].ID, floatBits(extent[0]), floatBits(extent[1])))
	}
	slices.Sort(lines)
	return digestLines(lines)
}

// gateOffTablesOf prepares network, as a simulation does, and returns the
// audit result and the digests of its static safety tables.
func gateOffTablesOf(network Network) gateOffTables {
	bank := "accepted"
	if err := network.ValidateBankGeometry(); err != nil {
		bank = err.Error()
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		return gateOffTables{Audit: err.Error(), BankAudit: bank}
	}
	owned := prepared.network
	name := keyNames(owned)
	conflicts := strings.Split(strings.TrimSuffix(encodeJunctionConflicts(prepared.junctionConflicts), "\n"), "\n")
	if len(prepared.junctionConflicts) == 0 {
		conflicts = nil
	}
	return gateOffTables{
		Audit:             "accepted",
		BankAudit:         bank,
		DepotLateral:      pairDigest(prepared.lateral, name),
		MergePairs:        pairDigest(prepared.merges, name),
		MergeExtents:      extentDigest(prepared.merges, owned),
		JunctionConflicts: digestLines(conflicts),
	}
}

// expressNetworkFixture reads the network with Express on all lanes of
// London Full.
func expressNetworkFixture(t *testing.T) Network {
	t.Helper()
	return readNetworkFixture(t, "testdata/express_network.json.gz")
}

// readNetworkFixture reads one gzip network from testdata.
func readNetworkFixture(t *testing.T, path string) Network {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var network Network
	if err := jsonv2.UnmarshalRead(reader, &network, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	return network
}

// gateOffRefusedNetworks returns networks that the audit refuses today,
// from the network of london-full-express with the classes of the shape
// clearance study (docs/design/shape-clearance.md, sections 2.1 and 2.3):
// the Express classes on the routes between Waterloo and Bank and on the
// two hubs, and on all lanes of Bank, roads included.
func gateOffRefusedNetworks(t *testing.T, express Network) map[string]Network {
	t.Helper()
	routes := express.clone()
	waterloo, okWaterloo := routes.Station("940GZZLUWLO")
	bank, okBank := routes.Station("940GZZLUBNK")
	if !okWaterloo || !okBank {
		t.Fatal("london-full-express has no Waterloo or Bank station")
	}
	keep := make(map[string]bool)
	for _, route := range [][]Lane{shapeStudyHubRoute(t, routes, waterloo, bank), shapeStudyHubRoute(t, routes, bank, waterloo)} {
		for _, lane := range route {
			keep[lane.ID] = true
		}
	}
	setShapeStudyClasses(&routes, keep, false, waterloo.ID, bank.ID)
	hub := express.clone()
	setShapeStudyClasses(&hub, nil, true, bank.ID)
	return map[string]Network{
		"london-full-express, Express on the Waterloo and Bank routes": routes,
		"london-full-express, Express on all lanes of Bank":            hub,
	}
}

// gateOffAuditProbes returns the bank audit of two straight parallel lanes
// 100 m long at the distances on each side of the clearance of the audit:
// an Express lane and a small lane at 20 m, and two small lanes at 12 m,
// which the audit of a network with no bank, large class, maneuver group,
// or depot does not check.
func gateOffAuditProbes() map[string]gateOffTables {
	pair := func(gap float64, classes ClassSet, lateral bool) gateOffTables {
		n := Network{
			Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 100}}, {ID: "c", Position: Point{Y: gap}}, {ID: "d", Position: Point{X: 100, Y: gap}}},
			Lanes: []Lane{{ID: "first", From: "a", To: "b", SpeedLimit: 14, VehicleClasses: classes}, {ID: "second", From: "c", To: "d", SpeedLimit: 14}},
			// LateralMerges does not change the audit.
			LateralMerges: lateral,
		}
		audit := "accepted"
		if err := n.ValidateBankGeometry(); err != nil {
			audit = err.Error()
		}
		return gateOffTables{Audit: "not prepared", BankAudit: audit}
	}
	probes := make(map[string]gateOffTables)
	for _, gap := range []float64{Clearance - 0.01, Clearance + 0.01, largeClearance - 0.01, largeClearance + 0.01} {
		probes[fmt.Sprintf("audit probe, Express and small lanes %g m apart", gap)] = pair(gap, shapeStudyClasses, false)
		probes[fmt.Sprintf("audit probe, small lanes %g m apart", gap)] = pair(gap, 0, false)
		probes[fmt.Sprintf("audit probe, small lanes %g m apart, lateralMerges true", gap)] = pair(gap, 0, true)
	}
	return probes
}

// TestGateOffTables pins the static safety tables that the shape clearance
// change touches, for a network that does not set the new field: the depot
// lateral pairs, the lateral merge pairs and their extents, the junction
// conflicts, and the result of the network audit. The networks are those
// of testdata/scenario_networks.json.gz and the network of
// london-full-express (TestExpressNetworkFixture in package scenarios
// writes it). Each network runs as stored and with LateralMerges flipped.
// Two networks from london-full-express that the audit refuses today pin
// the refusal text (see gateOffRefusedNetworks), and pairs of lanes on each
// side of the audit clearances pin the distances and the scope of the
// audit (see gateOffAuditProbes).
// Run the test with -update to write the golden file again.
func TestGateOffTables(t *testing.T) {
	t.Parallel()
	networks := scenarioNetworks(t)
	express := expressNetworkFixture(t)
	networks["london-full-express"] = express
	got := make(map[string]gateOffTables, 2*len(networks)+2)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, network := range gateOffRefusedNetworks(t, express) {
		wg.Go(func() {
			tables := gateOffTablesOf(network)
			mu.Lock()
			got[name] = tables
			mu.Unlock()
		})
	}
	for name, network := range networks {
		for _, lateral := range []bool{false, true} {
			wg.Go(func() {
				variant := network
				variant.LateralMerges = lateral
				tables := gateOffTablesOf(variant)
				mu.Lock()
				got[fmt.Sprintf("%s, lateralMerges %v", name, lateral)] = tables
				mu.Unlock()
			})
		}
	}
	wg.Wait()
	maps.Copy(got, gateOffAuditProbes())
	data, err := jsonv2.Marshal(got, jsonv2.Deterministic(true), jsontext.WithIndent("\t"))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *update {
		if err = os.WriteFile(gateOffTablesPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(gateOffTablesPath)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]gateOffTables
	if err = jsonv2.Unmarshal(stored, &want); err != nil {
		t.Fatal(err)
	}
	for _, name := range slices.Sorted(maps.Keys(want)) {
		if _, ok := got[name]; !ok {
			t.Errorf("%s: network is in the golden file, but not in the fixtures", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		g, w := got[name], want[name]
		if g.Audit != w.Audit {
			t.Errorf("%s: audit %q, golden %q", name, g.Audit, w.Audit)
		}
		if g.BankAudit != w.BankAudit {
			t.Errorf("%s: bank audit %q, golden %q", name, g.BankAudit, w.BankAudit)
		}
		for _, table := range []struct {
			name     string
			got, was *tableDigest
		}{
			{"depot lateral pairs", g.DepotLateral, w.DepotLateral},
			{"lateral merge pairs", g.MergePairs, w.MergePairs},
			{"lateral merge extents", g.MergeExtents, w.MergeExtents},
			{"junction conflicts", g.JunctionConflicts, w.JunctionConflicts},
		} {
			if table.got == nil || table.was == nil {
				if table.got != table.was {
					t.Errorf("%s: %s %+v, golden %+v", name, table.name, table.got, table.was)
				}
				continue
			}
			if *table.got != *table.was {
				t.Errorf("%s: %s has %d entries with digest %s, golden %d entries with digest %s",
					name, table.name, table.got.Count, table.got.SHA256, table.was.Count, table.was.SHA256)
			}
		}
	}
	if !bytes.Equal(stored, data) && !t.Failed() {
		t.Errorf("%s differs from the tables; run with -update", gateOffTablesPath)
	}
}
