package session

import (
	"log/slog"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// streamDecodeEnvelopes returns the JSON of a full envelope and of a delta
// envelope one publication later, from a running network with demand on.
func streamDecodeEnvelopes(tb testing.TB, config project.Config, warmup int) (full, delta []byte) {
	tb.Helper()
	config.Demand.Enabled = true
	shared, err := NewWithProject(config, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(shared.Close)
	for range warmup {
		shared.step()
	}
	previous, err := shared.presentationFrame()
	if err != nil {
		tb.Fatal(err)
	}
	for range sim.TicksPerSecond / 20 {
		shared.step()
	}
	current, err := shared.presentationFrame()
	if err != nil {
		tb.Fatal(err)
	}
	changes, err := makeDelta(previous, current)
	if err != nil {
		tb.Fatal(err)
	}
	marker := current.State.Simulation.OrderContract
	full, err = EncodeStreamJSON(StreamEnvelope{OrderContract: marker, Kind: "full", Stream: "decode", Sequence: 1, Build: current.State.Build, Source: sourceOf(current), Full: &current})
	if err != nil {
		tb.Fatal(err)
	}
	delta, err = EncodeStreamJSON(StreamEnvelope{OrderContract: marker, Kind: "delta", Stream: "decode", Sequence: 2, Base: 1, Build: current.State.Build, Source: sourceOf(current), Delta: &changes})
	if err != nil {
		tb.Fatal(err)
	}
	return full, delta
}

// busyLondonFull returns LondonFull with enough demand that a delta has
// the size of section 4.6 of the tick-rate proposal after the warm-up.
func busyLondonFull() project.Config {
	config := scenarios.LondonFull()
	config.Demand.PerMinute = 20
	return config
}

// BenchmarkDecodeStreamJSON measures the decode of the JSON of an inflated
// envelope. The fixed documents keep the workload stable across
// implementations, and the warm-up is outside the timer.
func BenchmarkDecodeStreamJSON(b *testing.B) {
	for _, network := range []struct {
		name   string
		config func() project.Config
	}{
		{"LondonFull", busyLondonFull},
		{"RailHub", scenarios.RailHub},
	} {
		full, delta := streamDecodeEnvelopes(b, network.config(), 480*sim.TicksPerSecond)
		for _, document := range []struct {
			name string
			raw  []byte
		}{{"full", full}, {"delta", delta}} {
			b.Run(network.name+"/"+document.name, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(document.raw)))
				for b.Loop() {
					if _, err := DecodeStreamJSON(document.raw); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
