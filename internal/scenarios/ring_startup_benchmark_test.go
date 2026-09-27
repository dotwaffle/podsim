package scenarios

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// BenchmarkLargeRingStartup starts the largest four-station ring that the
// project limits accept. Each station has maximumStationBerths berths, so
// project.MaxNodeLanes long lanes meet at each station entry and exit, and
// the junction conflict table dominates the start time. Run it with
// -benchtime=1x.
func BenchmarkLargeRingStartup(b *testing.B) {
	config, err := Config(Parameters{
		Name: "Large ring", Stations: 4, Pods: 1,
		PassengerBerths: maximumStationBerths, ParkingBerths: maximumStationBerths, DemandPerMinute: 1, DemandSeed: 1,
	})
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := sim.NewFleet(config.Network, config.Fleet); err != nil {
			b.Fatal(err)
		}
	}
}
