package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"maps"
	"os"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// packedTextWireGate is the largest aggregate ratio of packed to unpacked
// gzip bytes that the typical-frame gate accepts.
const packedTextWireGate = 1.05

// packedTextWireSizes holds the gzip sizes of one sample kind of one
// workload, in the two text encodings.
type packedTextWireSizes struct {
	Samples        int     `json:"samples"`
	PackedBytes    int     `json:"packed_gzip_bytes"`
	UnpackedBytes  int     `json:"unpacked_gzip_bytes"`
	Ratio          float64 `json:"ratio"`
	MaxSampleRatio float64 `json:"max_sample_ratio"`
}

func (s *packedTextWireSizes) add(packed, unpacked int) {
	s.Samples++
	s.PackedBytes += packed
	s.UnpackedBytes += unpacked
	s.Ratio = float64(s.PackedBytes) / float64(s.UnpackedBytes)
	s.MaxSampleRatio = max(s.MaxSampleRatio, float64(packed)/float64(unpacked))
}

// packedTextWireWorkload is the measurement of one workload.
type packedTextWireWorkload struct {
	Name   string              `json:"name"`
	Fulls  packedTextWireSizes `json:"fulls"`
	Deltas packedTextWireSizes `json:"deltas"`
}

// TestPackedTextWireCost measures the gzip cost of packed order text on
// typical stream frames. For each workload it warms the simulation for 20
// simulated minutes, then takes 100 publications at 20 Hz. It encodes a
// full frame at publication 0 and at every 20th publication, and a delta
// from each publication to the next. Each sample is encoded with packed
// and with unpacked order text, from the same frames. The test fails when
// an aggregate ratio of packed to unpacked gzip bytes is more than
// packedTextWireGate. When PODSIM_PACKED_TEXT_WIRE_RECORD names a file,
// the test writes the measurement record to it.
func TestPackedTextWireCost(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("measurement runs without -short and without the race detector")
	}
	workloads := []struct {
		name   string
		config func() project.Config
	}{
		{"default", project.Default},
		{"scale100", scenarios.Scale100},
		{"london-central", scenarios.LondonCentral},
	}
	var results []packedTextWireWorkload
	for _, workload := range workloads {
		config := workload.config()
		config.Demand.Enabled = true
		result := measurePackedTextWire(t, workload.name, config)
		t.Logf("%s: fulls %d packed / %d unpacked = %.4f (max sample %.4f); deltas %d packed / %d unpacked = %.4f (max sample %.4f)",
			result.Name, result.Fulls.PackedBytes, result.Fulls.UnpackedBytes, result.Fulls.Ratio, result.Fulls.MaxSampleRatio,
			result.Deltas.PackedBytes, result.Deltas.UnpackedBytes, result.Deltas.Ratio, result.Deltas.MaxSampleRatio)
		results = append(results, result)
	}
	if path := os.Getenv("PODSIM_PACKED_TEXT_WIRE_RECORD"); path != "" {
		record := struct {
			Format    string                   `json:"format"`
			Test      string                   `json:"test"`
			Method    string                   `json:"method"`
			Gate      float64                  `json:"gate"`
			Workloads []packedTextWireWorkload `json:"workloads"`
		}{
			Format:    "podsim-packed-text-wire-v1",
			Test:      "internal/session TestPackedTextWireCost",
			Method:    "Demand enabled with the project seed; 20 simulated minutes of warm-up; 100 publications at 20 Hz after publication 0; full frames at publications 0, 20, 40, 60, 80 and 100; one delta from each publication to the next; each sample encoded with packed and with unpacked order text from the same frames; gzip level 1; ratio is packed over unpacked gzip bytes.",
			Gate:      packedTextWireGate,
			Workloads: results,
		}
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range results {
		if result.Fulls.Ratio > packedTextWireGate || result.Deltas.Ratio > packedTextWireGate {
			t.Errorf("%s: packed order text exceeds the %.2f gate: fulls %.4f, deltas %.4f", result.Name, packedTextWireGate, result.Fulls.Ratio, result.Deltas.Ratio)
		}
	}
}

func measurePackedTextWire(t *testing.T, name string, config project.Config) packedTextWireWorkload {
	t.Helper()
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	step := func() { shared.simulation.Step(); shared.demand.step(shared.simulation); shared.revision++ }
	for range 20 * 60 * sim.TicksPerSecond {
		step()
	}
	result := packedTextWireWorkload{Name: name}
	previous, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	full := func(sequence uint64, frame StreamFrame) {
		e := StreamEnvelope{Kind: "full", Stream: "measurement", Sequence: sequence, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
		result.Fulls.add(packedTextWireBytes(t, e, true), packedTextWireBytes(t, e, false))
	}
	full(1, previous)
	const publications = 100
	for i := 1; i <= publications; i++ {
		for range sim.TicksPerSecond / 20 {
			step()
		}
		current, err := shared.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		delta, err := makeDelta(previous, current)
		if err != nil {
			t.Fatal(err)
		}
		e := StreamEnvelope{Kind: "delta", Stream: "measurement", Sequence: uint64(i + 1), Base: uint64(i), Source: sourceOf(current), Build: current.State.Build}
		packed, unpacked := delta, delta
		if _, replaced := delta.Groups["pending"]; replaced {
			packed.Groups = packedTextWireGroups(t, delta.Groups, current, true)
			unpacked.Groups = packedTextWireGroups(t, delta.Groups, current, false)
		}
		e.Delta = &packed
		packedBytes := packedTextWireBytes(t, e, true)
		e.Delta = &unpacked
		result.Deltas.add(packedBytes, packedTextWireBytes(t, e, false))
		if i%20 == 0 {
			full(uint64(i+1), current)
		}
		previous = current
	}
	return result
}

// packedTextWireGroups returns a copy of groups with the pending group of
// frame built in one text encoding. The outer encoder options do not
// reach a built group, so the group is built here.
func packedTextWireGroups(t *testing.T, groups map[string]json.RawMessage, frame StreamFrame, packed bool) map[string]json.RawMessage {
	t.Helper()
	next := maps.Clone(groups)
	var err error
	if packed {
		next["pending"], err = jsonv2.Marshal(frame.State.Simulation.Pending, json.DefaultOptionsV1(), packedRequestOptions())
	} else {
		next["pending"], err = json.Marshal(frame.State.Simulation.Pending)
	}
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// packedTextWireBytes returns the gzip size of e with packed or unpacked
// order text.
func packedTextWireBytes(t *testing.T, e StreamEnvelope, packed bool) int {
	t.Helper()
	var data []byte
	var err error
	if packed {
		data, err = jsonv2.Marshal(e, json.DefaultOptionsV1(), packedRequestOptions())
	} else {
		data, err = json.Marshal(e)
	}
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := compressStreamJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return len(compressed)
}
