package main

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/observe"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestMetricsSnapshotConsumers(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, parties := range []int{1, 4} {
		t.Run(strconv.Itoa(parties), func(t *testing.T) {
			t.Parallel()
			input := smallBurstInput(t)
			simulation, err := sim.NewFleet(input.scenario.network, input.scenario.fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err = simulation.SetSharedRidePartyLimit(parties); err != nil {
				t.Fatal(err)
			}
			full, err := newRunMetrics(input.scenario, input.schedule)
			if err != nil {
				t.Fatal(err)
			}
			metrics, err := newRunMetrics(input.scenario, input.schedule)
			if err != nil {
				t.Fatal(err)
			}
			monitor := observe.NewNetworkMonitor(input.scenario.network)
			var fullWaits, metricsWaits trafficWaits
			next := 0
			for tick := range durationTicks(input.duration) {
				for next < len(input.schedule) && input.schedule[next].tick == tick {
					request := input.schedule[next]
					if _, err := simulation.SubmitTripOptions(sim.TripOptions{From: request.origin, To: request.destination, SharingConsent: sim.SharedConsent}); err != nil {
						t.Fatal(err)
					}
					next++
				}
				simulation.Step()
				reference, compact := simulation.Snapshot(), simulation.MetricsSnapshot()
				full.observe(reference)
				metrics.observe(compact)
				fullWaits.sampleWaits(reference.Vehicles)
				metricsWaits.sampleWaits(compact.Vehicles)
				if !reflect.DeepEqual(full, metrics) || fullWaits != metricsWaits {
					t.Fatalf("tick %d: comparison metrics differ", reference.Tick)
				}
				if reference.Tick%sim.TicksPerSecond == 0 {
					if got, want := monitor.Summarize(compact), monitor.Summarize(reference); !reflect.DeepEqual(got, want) {
						t.Fatalf("tick %d: network summaries differ", reference.Tick)
					}
				}
			}
			if next != len(input.schedule) {
				t.Fatal("replay omitted scheduled requests")
			}
		})
	}
}
