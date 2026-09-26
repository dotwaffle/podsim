package scenarios

import (
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// londonPositioningTicks is the length of each London positioning mode
// comparison.
const londonPositioningTicks = 2000

// londonBandWeights returns the demand weight of each origin station in a
// band. The weight of a station is the sum of the shares of the flows that
// start there, as in compare.
func londonBandWeights(band LondonDemandBand) map[string]float64 {
	weights := make(map[string]float64)
	for _, flow := range band.Flows {
		weights[flow.From] += flow.Share
	}
	return weights
}

// newLondonPositioning returns London with the demand weights of a band and
// the settings that set applies.
func newLondonPositioning(tb testing.TB, band LondonDemandBand, set func(*sim.Simulation) error) *sim.Simulation {
	tb.Helper()
	simulation := newSimulation(tb, London())
	if err := simulation.SetDemandWeights(londonBandWeights(band)); err != nil {
		tb.Fatal(err)
	}
	if err := set(simulation); err != nil {
		tb.Fatal(err)
	}
	return simulation
}

// TestLondonPositioningModesMatchLegacySettings checks that each pair of
// settings gives the same London run. The runs request AM peak journeys at
// 12 per minute.
func TestLondonPositioningModesMatchLegacySettings(t *testing.T) {
	t.Parallel()
	band := LondonDemand()[2]
	schedule := londonDemandSchedule(londonCloneSeed, band, (londonPositioningTicks-1)/(5*sim.TicksPerSecond))
	for _, pair := range []struct {
		name      string
		want, got func(*sim.Simulation) error
		// moves is true when the run must make positioning moves.
		moves bool
	}{
		{
			name: "no call and off",
			want: func(*sim.Simulation) error { return nil },
			got:  func(s *sim.Simulation) error { return s.SetPositioning(sim.PositioningOff) },
		},
		{
			name:  "redistribution",
			want:  func(s *sim.Simulation) error { s.SetRedistribution(true); return nil },
			got:   func(s *sim.Simulation) error { return s.SetPositioning(sim.PositioningRedistribution) },
			moves: true,
		},
	} {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			want, got := newLondonPositioning(t, band, pair.want), newLondonPositioning(t, band, pair.got)
			for tick := range int64(londonPositioningTicks) {
				for _, simulation := range []*sim.Simulation{want, got} {
					if err := stepScheduled(simulation, scheduledStep{schedule: schedule, tick: tick}); err != nil {
						t.Fatal(err)
					}
				}
				if (tick+1)%sim.TicksPerSecond != 0 && tick+1 != londonPositioningTicks {
					continue
				}
				if !reflect.DeepEqual(got.Snapshot(), want.Snapshot()) {
					t.Fatalf("the snapshots differ at tick %d", tick+1)
				}
				if !reflect.DeepEqual(got.ExportState(), want.ExportState()) {
					t.Fatalf("the saved states differ at tick %d", tick+1)
				}
			}
			if state := got.Snapshot(); state.Submitted == 0 || (state.RebalanceMoves > 0) != pair.moves {
				t.Fatalf("the run has %d requests and %d positioning moves", state.Submitted, state.RebalanceMoves)
			}
		})
	}
}
