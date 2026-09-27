package scenarios

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

var updateConflictFixture = flag.Bool("update-conflict-fixture", false, "write the scenario networks for the junction conflict tests in package sim")

// conflictFixturePath holds the networks that the junction conflict tests in
// package sim compare with the reference table. Package sim cannot import
// this package, so it reads the networks from the file.
const conflictFixturePath = "../sim/testdata/scenario_networks.json.gz"

// TestWriteConflictFixture writes each preset network and rings with many
// berths, where many long lanes meet at one node. Run it with
// -update-conflict-fixture after a change to the presets.
func TestWriteConflictFixture(t *testing.T) {
	if !*updateConflictFixture {
		t.Skip("run with -update-conflict-fixture to write the file")
	}
	networks := map[string]sim.Network{}
	for name, config := range map[string]func() project.Config{
		"small": Small, "busy": Busy, "parking constrained": ParkingConstrained,
		"rail hub": RailHub, "scale 100": Scale100, "London": London,
	} {
		networks[name] = config().Network
	}
	for _, size := range [][2]int{{4, 12}, {3, 16}, {100, 3}} {
		config, err := Config(Parameters{
			Name: "Berth ring", Stations: size[0], Pods: 1,
			PassengerBerths: size[1], ParkingBerths: size[1], DemandPerMinute: 1, DemandSeed: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		networks[fmt.Sprintf("ring %dx%d", size[0], size[1])] = config.Network
	}
	var data bytes.Buffer
	writer := gzip.NewWriter(&data)
	if err := json.NewEncoder(writer).Encode(networks); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictFixturePath, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
