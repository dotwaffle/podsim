package scenarios

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"flag"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

var updateExpressProjectFixture = flag.Bool("update-express-project", false, "write the fleet, the Express services, and the settings of london-full-express for the envelope run in package sim")

// expressProjectFixturePath holds the parts of london-full-express other
// than the network, which is at expressNetworkFixturePath.
// TestLondonExpressEnvelopeRun in package sim reads both files.
const expressProjectFixturePath = "../sim/testdata/express_project.json"

// expressProjectFixture is the file at expressProjectFixturePath.
type expressProjectFixture struct {
	Fleet           []sim.Placement      `json:"fleet"`
	ExpressServices []sim.ExpressService `json:"expressServices"`
	PlatoonLimit    int                  `json:"platoonLimit"`
	Faults          sim.FaultSettings    `json:"faults"`
}

// TestExpressProjectFixture checks that the file in package sim holds the
// fleet, the Express services, the platoon limit, and the fault settings
// of the preset. Run it with -update-express-project after a change to the
// preset.
func TestExpressProjectFixture(t *testing.T) {
	t.Parallel()
	config := LondonFullExpress()
	data, err := jsonv2.Marshal(expressProjectFixture{Fleet: config.Fleet, ExpressServices: config.ExpressServices,
		PlatoonLimit: config.PlatoonLimit, Faults: project.EffectiveFaultSettings(config)}, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if *updateExpressProjectFixture {
		if err = os.WriteFile(expressProjectFixturePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(expressProjectFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatalf("%s differs from the preset; run with -update-express-project", expressProjectFixturePath)
	}
}
