package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// BenchmarkSharedExpressChecks measures the per-tick checks of the incident
// soak on the shared-track preset after 3,600 ticks with demand: the safety
// observation, the checked export, and the D6 oracle.
func BenchmarkSharedExpressChecks(b *testing.B) {
	config, err := scenarios.GenerateLondonFullSharedExpress()
	if err != nil {
		b.Fatal(err)
	}
	config.Demand.Enabled = true
	shared, err := NewWithProject(config)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(shared.Close)
	for range 3600 {
		shared.advance()
	}
	simulation := shared.simulation
	moving := 0
	for _, pod := range simulation.SafetyObservation().Pods {
		if pod.Activity == sim.Traveling {
			moving++
		}
	}
	if moving == 0 {
		b.Fatal("no pod travels after the warm-up")
	}
	b.Run("observation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkSafetyObservation = simulation.SafetyObservation()
		}
	})
	b.Run("export", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := simulation.CheckedExportForTest(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("lateral", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := simulation.LateralBodyCheckForTest(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
