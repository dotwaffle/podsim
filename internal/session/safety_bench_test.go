package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

var benchmarkSafetyObservation sim.SafetyObservation

// BenchmarkLondonExpressSafety measures the safety oracle on the London
// Express preset after 3,600 ticks with demand: the observation, the check
// of one observation, and the two together as a test makes them each tick.
func BenchmarkLondonExpressSafety(b *testing.B) {
	config := scenarios.LondonFullExpress()
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
	observation := simulation.SafetyObservation()
	moving := 0
	for _, pod := range observation.Pods {
		if pod.Activity == sim.Traveling {
			moving++
		}
	}
	if moving == 0 {
		b.Fatal("no pod travels after the warm-up")
	}
	if _, err := observation.Check(); err != nil {
		b.Fatal(err)
	}
	b.Run("observation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkSafetyObservation = simulation.SafetyObservation()
		}
	})
	b.Run("check", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := observation.Check(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("both", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := simulation.SafetyObservation().Check(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
