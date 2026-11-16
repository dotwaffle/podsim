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
)

var updateExpressNetworkFixture = flag.Bool("update-express-network", false, "write the network of london-full-express for the gate-off tables test in package sim")

// expressNetworkFixturePath holds the network of london-full-express for
// TestGateOffTables in package sim. Package sim cannot import this package.
const expressNetworkFixturePath = "../sim/testdata/express_network.json.gz"

// TestExpressNetworkFixture checks that the file in package sim holds the
// network of the preset. Run it with -update-express-network after a change
// to the preset.
func TestExpressNetworkFixture(t *testing.T) {
	t.Parallel()
	data, err := jsonv2.Marshal(LondonFullExpress().Network, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if *updateExpressNetworkFixture {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err = writer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err = writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(expressNetworkFixturePath, compressed.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Open(expressNetworkFixturePath)
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
		t.Fatalf("%s differs from the preset; run with -update-express-network", expressNetworkFixturePath)
	}
}
