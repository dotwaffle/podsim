package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
)

func TestAdvanceIdleFleetAllocations(t *testing.T) {
	config := scenarios.London()
	config.Demand.Enabled, config.Redistribution = false, false
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	// Status reads must not copy the idle fleet or its berth states each tick.
	if got := testing.AllocsPerRun(100, shared.advance); got > 4 {
		t.Fatalf("idle clock allocated %.0f times per tick, want at most 4", got)
	}
}

func BenchmarkAdvanceLondonIdle(b *testing.B) {
	config := scenarios.London()
	config.Demand.Enabled, config.Redistribution = false, false
	shared, err := NewWithProject(config)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		shared.advance()
	}
}
