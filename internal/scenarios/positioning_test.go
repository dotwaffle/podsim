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
		{
			name: "guarded then off",
			want: func(*sim.Simulation) error { return nil },
			got: func(s *sim.Simulation) error {
				if err := s.SetPositioning(sim.PositioningGuarded); err != nil {
					return err
				}
				return s.SetPositioning(sim.PositioningOff)
			},
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

const (
	// londonInertInterval gives 6 requests per minute. The gate of the
	// London fleet is active below 5.7 requests per minute.
	londonInertInterval = 10 * sim.TicksPerSecond
	londonInertTicks    = 20 * 60 * sim.TicksPerSecond
)

// TestLondonGuardedIsInertAtSixPerMinute runs London with AM peak journeys
// at 6 per minute in off mode and in guarded mode. At this rate the gate is
// not active, so guarded mode gives the off snapshot at each second. The
// saved states differ only in the time of the next guarded check.
func TestLondonGuardedIsInertAtSixPerMinute(t *testing.T) {
	t.Parallel()
	band := LondonDemand()[2]
	schedule := londonDemandSchedule(londonCloneSeed, band, (londonInertTicks-1)/londonInertInterval)
	for index := range schedule {
		schedule[index].tick = int64((index + 1) * londonInertInterval)
	}
	// The gate is active while ID*20 requests per minute is less than
	// the fleet times the minutes to the request.
	if fleet := len(London().Fleet); 6*20 < fleet {
		t.Fatalf("the gate of a fleet of %d pods is active at 6 requests per minute", fleet)
	}
	off := newLondonPositioning(t, band, func(*sim.Simulation) error { return nil })
	guarded := newLondonPositioning(t, band, func(s *sim.Simulation) error { return s.SetPositioning(sim.PositioningGuarded) })
	for tick := range int64(londonInertTicks) {
		for _, simulation := range []*sim.Simulation{off, guarded} {
			if err := stepScheduled(simulation, scheduledStep{schedule: schedule, tick: tick}); err != nil {
				t.Fatal(err)
			}
		}
		if (tick+1)%sim.TicksPerSecond != 0 {
			continue
		}
		if !reflect.DeepEqual(guarded.Snapshot(), off.Snapshot()) {
			t.Fatalf("the snapshots differ at tick %d", tick+1)
		}
		want, got := off.ExportState(), guarded.ExportState()
		if got.NextRedistributionTick <= tick+1 {
			t.Fatalf("tick %d: the next guarded check is at tick %d", tick+1, got.NextRedistributionTick)
		}
		got.NextRedistributionTick = want.NextRedistributionTick
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("the saved states differ at tick %d", tick+1)
		}
	}
	if state := guarded.Snapshot(); state.Submitted != len(schedule) || state.Completed == 0 {
		t.Fatalf("the run submitted %d of %d requests and completed %d", state.Submitted, len(schedule), state.Completed)
	}
}
