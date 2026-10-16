package session

import (
	"log/slog"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// BenchmarkLondonPublication repeats one representative 20 Hz transition.
// The fixed frame pair keeps the workload stable across implementations.
// Stepping, warm-up and the first client baseline are outside the timer.
func BenchmarkLondonPublication(b *testing.B) {
	for _, pods := range []int{327, 600, 1000} {
		b.Run(strconv.Itoa(pods), func(b *testing.B) {
			if pods > project.MaxPods {
				b.Skip("fleet requires the candidate limit overlay")
			}
			config := londonQualificationProject(b, pods)
			config.Demand.PerMinute = 120
			shared, err := NewWithProject(config, WithLogger(slog.New(slog.DiscardHandler)))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(shared.Close)
			for range 600 * sim.TicksPerSecond {
				shared.step()
			}
			previous, err := shared.presentationFrame()
			if err != nil {
				b.Fatal(err)
			}
			for range sim.TicksPerSecond / 20 {
				shared.step()
			}
			assembler, err := NewStreamAssembler(shared.Topology())
			if err != nil {
				b.Fatal(err)
			}
			if _, err := assembler.State(previous); err != nil {
				b.Fatal(err)
			}
			bytes := 0
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				current, err := shared.presentationFrame()
				if err != nil {
					b.Fatal(err)
				}
				delta, err := makeDelta(previous, current)
				if err != nil {
					b.Fatal(err)
				}
				data, err := encodeStream(StreamEnvelope{Kind: "delta", Stream: "benchmark", Sequence: 2, Base: 1, Source: sourceOf(current), Build: current.State.Build, Delta: &delta})
				if err != nil {
					b.Fatal(err)
				}
				bytes = len(data)
				raw, err := InflateStream(data)
				if err != nil {
					b.Fatal(err)
				}
				envelope, err := DecodeStreamJSON(raw)
				if err != nil {
					b.Fatal(err)
				}
				applied, err := ApplyStream(previous, "benchmark", 1, envelope)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := assembler.State(applied); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(bytes), "gzip-B/pub")
		})
	}
}
