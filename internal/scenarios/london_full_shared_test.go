package scenarios

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonFullSharedExpress checks the static results of the shared-track
// preset (docs/design/shared-track-preset.md, sections 3.5 and 4): project
// validation and the bank audit pass, with 38 shape pairs, the checks of
// cmd/projectcheck give one tail warning, and Express runs on the shared
// route in each direction.
func TestLondonFullSharedExpress(t *testing.T) {
	t.Parallel()
	base := LondonFull()
	config, err := londonFullSharedExpress(LondonFull(), sharedTrackPresetGeometry())
	if err != nil {
		t.Fatal(err)
	}
	n := config.Network
	if !n.ShapeClearance || !n.LateralMerges {
		t.Fatalf("ShapeClearance %v, LateralMerges %v, want both set", n.ShapeClearance, n.LateralMerges)
	}
	if err = project.Validate(config); err != nil {
		t.Fatal(err)
	}
	// With the index for each lane, the audit refuses no pair, and it has
	// the 38 shape pairs of section 3.5.
	shapes, err := n.BankGeometryShapePairs()
	if err != nil || shapes != 38 {
		t.Fatalf("bank audit: %d shape pairs, error %v, want 38 and no error", shapes, err)
	}
	if err := n.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
	warnings := project.TailWarnings(n)
	if len(warnings) != 1 || !strings.Contains(warnings[0].Text, "940GZZLUBNK-move-08-02 ") {
		t.Fatalf("tail warnings %v, want one for 940GZZLUBNK-move-08-02", warnings)
	}
	if len(n.Lanes) != len(base.Network.Lanes)-2 {
		t.Fatalf("%d lanes, want %d", len(n.Lanes), len(base.Network.Lanes)-2)
	}
	for _, id := range sharedTrackPresetGeometry().remove {
		if slices.ContainsFunc(n.Lanes, func(lane sim.Lane) bool { return lane.ID == id }) {
			t.Fatalf("lane %s is not removed", id)
		}
	}
	if config.Name != "LondonFull Shared Express" || config.OrderContract != sim.ExpressOrderContract || len(config.ExpressServices) != 2 {
		t.Fatalf("name %q, contract %q, %d services", config.Name, config.OrderContract, len(config.ExpressServices))
	}
	// The Express parking of decision S4 comes in a later slice.
	if !slices.Equal(config.Fleet, base.Fleet) {
		t.Fatal("the fleet differs from the LondonFull fleet")
	}
	waterloo, _ := n.Station(expressWaterloo)
	bank, _ := n.Station(expressBank)
	for i, pair := range [][2]sim.Station{{waterloo, bank}, {bank, waterloo}} {
		route, err := n.RouteForClass(pair[0].Exit, pair[1].Entry, sim.ExpressClass)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(route))
		for _, lane := range route {
			ids = append(ids, lane.ID)
		}
		if want := sharedTrackRoute[6*i : 6*i+6]; !slices.Equal(ids, want) {
			t.Fatalf("Express route from %s to %s is %v, want %v", pair[0].ID, pair[1].ID, ids, want)
		}
	}
}

// TestLondonFullSharedExpressKeepsPresets checks that the shared-track
// preset changes neither LondonFull nor project D (decision SX1). It builds
// the preset first, from the cached LondonFull and with the generator, so
// that a change to a shared base fails.
func TestLondonFullSharedExpressKeepsPresets(t *testing.T) {
	t.Parallel()
	if _, err := londonFullSharedExpress(LondonFull(), sharedTrackPresetGeometry()); err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateLondonFullSharedExpress()
	if err != nil {
		t.Fatal(err)
	}
	if !generated.Network.ShapeClearance {
		t.Fatal("the generated preset does not set ShapeClearance")
	}
	for _, test := range []struct {
		name    string
		network sim.Network
		want    string
	}{
		{"LondonFull", LondonFull().Network, "e194426aa95104424a6365370f014c166f25d0572199f9b875e07bb58f2e880e"},
		{"LondonFull Express", LondonFullExpress().Network, "3aa0a2eb9e99eab4ea80ecfad481c7718b57cd38e0b35876b44b5ab5dc1d22e5"},
		// The network of variant 3.5 of the shared-track study in
		// internal/sim, which builds its own copy of this construction.
		{"LondonFull Shared Express", generated.Network, "4e8503da2e9a4778b7f1310fcba0729dc8c160c2d4055525662042a00df97597"},
	} {
		if test.name != "LondonFull Shared Express" && test.network.ShapeClearance {
			t.Errorf("%s sets ShapeClearance", test.name)
		}
		data, err := jsonv2.Marshal(test.network, json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != test.want {
			t.Errorf("%s network SHA-256 %s, want %s", test.name, got, test.want)
		}
	}
}
