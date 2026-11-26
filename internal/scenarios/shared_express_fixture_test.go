package scenarios

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"flag"
	"io"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

var updateSharedExpressFixture = flag.Bool("update-shared-express", false, "write the shared-track preset fixtures for the SX and HX tests in package sim")

// sharedExpressFixturePath holds the parts of london-full:shared
// that the SX tests in package sim need. Package sim cannot import this
// package or package project.
const sharedExpressFixturePath = "../sim/testdata/shared_express_project.json.gz"

// sharedExpressFixture is the file at sharedExpressFixturePath.
// sharedExpressProject in package sim reads it.
type sharedExpressFixture struct {
	Network         sim.Network          `json:"network"`
	Fleet           []sim.Placement      `json:"fleet"`
	ExpressServices []sim.ExpressService `json:"expressServices"`
	PlatoonLimit    int                  `json:"platoonLimit"`
	Faults          sim.FaultSettings    `json:"faults"`
}

// TestSharedExpressFixture checks that the file in package sim holds the
// network, the fleet, the Express services, the platoon limit, and the
// fault settings of the preset. Run it with -update-shared-express after a
// change to the preset.
func TestSharedExpressFixture(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	checkExpressFixture(t, config, sharedExpressFixturePath)
}

// TestWharfExpressFixture is TestSharedExpressFixture for
// london-full:wharf and the HX tests in package sim.
func TestWharfExpressFixture(t *testing.T) {
	t.Parallel()
	config, err := londonFullSharedExpressWharfPreset(LondonFull())
	if err != nil {
		t.Fatal(err)
	}
	checkExpressFixture(t, config, "../sim/testdata/wharf_express_project.json.gz")
}

// checkExpressFixture checks the fixture of config at path, and writes it
// first with -update-shared-express.
func checkExpressFixture(t *testing.T, config project.Config, path string) {
	t.Helper()
	data, err := jsonv2.Marshal(sharedExpressFixture{Network: config.Network, Fleet: config.Fleet, ExpressServices: config.ExpressServices,
		PlatoonLimit: config.PlatoonLimit, Faults: project.EffectiveFaultSettings(config)}, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if *updateSharedExpressFixture {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err = writer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatalf("%s differs from the preset; run with -update-shared-express", path)
	}
}
