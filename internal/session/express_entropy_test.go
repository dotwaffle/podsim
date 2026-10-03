package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

func expressEntropyText(random *rand.Rand) string {
	text := make([]byte, 1019)
	for i := range text {
		text[i] = byte(32 + random.IntN(95))
	}
	return string(text) + "é中"
}

// Independent typed field shapes test gzip with distinct bounded UTF-8 text.
func TestExpressIncompressibleAssetAdapters(t *testing.T) {
	dir := os.Getenv("PODSIM_EXPRESS_PUBLIC_ASSET_DIR")
	if dir == "" {
		t.Skip("external widest assets not selected")
	}
	random := rand.New(rand.NewPCG(20261003, 8))
	savedRaw, err := os.ReadFile(dir + "/save-modern.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	file, err := decodeStateFile(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.resolveBoardings(); err != nil {
		t.Fatal(err)
	}
	for i := range file.Simulation.Waiting {
		file.Simulation.Waiting[i].Request.DispatchReason = expressEntropyText(random)
	}
	for i := range file.Simulation.Pods {
		for j := range file.Simulation.Pods[i].Riders {
			file.Simulation.Pods[i].Riders[j].DispatchReason = expressEntropyText(random)
		}
	}
	started := time.Now()
	saved := encodeTestState(t, file)
	unpacked := decompressTestJSON(t, saved)
	if len(saved) > MaxStateBytes || len(unpacked) > MaxStateBytes {
		t.Fatal("save cap exceeded")
	}
	decoded, err := decodeStateFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err = decoded.resolveBoardings(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unpacked, decompressTestJSON(t, encodeTestState(t, decoded))) {
		t.Fatal("incompressible save changed")
	}
	t.Logf("asset save-entropy raw=%d gzip=%d elapsed=%s", len(unpacked), len(saved), time.Since(started))
	exportExpressAsset(t, "save-entropy.json", unpacked)
	exportExpressAsset(t, "save-entropy.json.gz", saved)
	raw, err := os.ReadFile(dir + "/full.json")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := DecodeStreamJSONVersion(raw, 4)
	if err != nil {
		t.Fatal(err)
	}
	before := ownStreamBoardings(*envelope.Full)
	for i := range envelope.Full.State.Simulation.Pending {
		envelope.Full.State.Simulation.Pending[i].DispatchReason = expressEntropyText(random)
	}
	for i := range envelope.Full.State.Simulation.Vehicles {
		for j := range envelope.Full.State.Simulation.Vehicles[i].Riders {
			envelope.Full.State.Simulation.Vehicles[i].Riders[j].DispatchReason = expressEntropyText(random)
		}
	}
	delta, err := makeDelta(before, *envelope.Full)
	if err != nil {
		t.Fatal(err)
	}
	replacement := StreamEnvelope{OrderContract: sim.ExpressOrderContract, TextEncoding: ExpressTextEncoding, Kind: "delta", Stream: envelope.Stream, Sequence: 2, Base: 1, Source: envelope.Source, Build: envelope.Build, Delta: &delta}
	for name, value := range map[string]StreamEnvelope{"full-entropy": envelope, "delta-entropy": replacement} {
		started = time.Now()
		encoded, encodeErr := EncodeStreamJSON(value)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if _, decodeErr := DecodeStreamJSONVersion(encoded, 4); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		zipped, zipErr := encodeStream(value)
		if zipErr != nil {
			t.Fatal(zipErr)
		}
		inflated, inflateErr := InflateStream(zipped)
		if inflateErr != nil || !bytes.Equal(encoded, inflated) {
			t.Fatal("production gzip changed", inflateErr)
		}
		if len(encoded) > MaxStreamJSON || len(zipped) > MaxStreamMessage {
			t.Fatal("stream cap exceeded")
		}
		t.Logf("asset %s raw=%d gzip=%d elapsed=%s", name, len(encoded), len(zipped), time.Since(started))
		exportExpressAsset(t, name+".json", encoded)
		exportExpressAsset(t, name+".json.gz", zipped)
	}
	httpRaw, err := os.ReadFile(dir + "/http.json")
	if err != nil {
		t.Fatal(err)
	}
	var httpEnvelope ExpressStateEnvelope
	if err = jsonv2.Unmarshal(httpRaw, &httpEnvelope, json.DefaultOptionsV1(), packedDecodeOptions()); err != nil {
		t.Fatal(err)
	}
	for i := range httpEnvelope.Frame.State.Simulation.Pending {
		httpEnvelope.Frame.State.Simulation.Pending[i].DispatchReason = expressEntropyText(random)
	}
	for i := range httpEnvelope.Frame.State.Simulation.Vehicles {
		for j := range httpEnvelope.Frame.State.Simulation.Vehicles[i].Riders {
			httpEnvelope.Frame.State.Simulation.Vehicles[i].Riders[j].DispatchReason = expressEntropyText(random)
		}
	}
	started = time.Now()
	httpRaw, err = EncodeExpressStateJSON(httpEnvelope.Topology, httpEnvelope.Frame)
	if err != nil {
		t.Fatal(err)
	}
	httpState, err := DecodeExpressStateJSON(httpRaw)
	if err != nil {
		t.Fatal(err)
	}
	if httpState.Simulation.Pending[0].DispatchReason != httpEnvelope.Frame.State.Simulation.Pending[0].DispatchReason {
		t.Fatal("HTTP entropy text changed")
	}
	httpZip, err := compressStreamJSON(httpRaw)
	if err != nil {
		t.Fatal(err)
	}
	inflated, err := InflateStream(httpZip)
	if err != nil || !bytes.Equal(inflated, httpRaw) {
		t.Fatal("HTTP entropy gzip changed", err)
	}
	t.Logf("asset http-entropy raw=%d gzip=%d elapsed=%s", len(httpRaw), len(httpZip), time.Since(started))
	exportExpressAsset(t, "http-entropy.json", httpRaw)
	exportExpressAsset(t, "http-entropy.json.gz", httpZip)

}
