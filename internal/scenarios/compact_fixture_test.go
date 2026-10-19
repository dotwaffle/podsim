package scenarios

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"flag"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

var updateCompactFixture = flag.Bool("update-compact-fixture", false, "write the compact station for the scenario tests in package sim")

// compactFixturePath holds the network and the fleet of the compact
// station preset for the scenario tests in package sim, which read the
// resource owners. Package sim cannot import this package.
const compactFixturePath = "../sim/testdata/compact_station.json"

// TestCompactStationFixture checks that the file in package sim is the
// preset. Run it with -update-compact-fixture after a change to the
// preset.
func TestCompactStationFixture(t *testing.T) {
	t.Parallel()
	config, err := CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	fixture := struct {
		Network sim.Network     `json:"network"`
		Fleet   []sim.Placement `json:"fleet"`
	}{config.Network, config.Fleet}
	data, err := jsonv2.Marshal(fixture, json.DefaultOptionsV1(), jsontext.WithIndent("\t"))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *updateCompactFixture {
		if err = os.WriteFile(compactFixturePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := os.ReadFile(compactFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Fatalf("%s differs from the preset; run with -update-compact-fixture", compactFixturePath)
	}
}
