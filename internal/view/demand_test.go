package view

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestNextDemandRate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		current, want int
	}{
		{name: "lowest rate", current: 1, want: 2},
		{name: "London rate", current: 20, want: 30},
		{name: "highest rate", current: 60, want: 1},
		{name: "rate between listed rates", current: 7, want: 8},
		{name: "rate above listed rates", current: 120, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := nextDemandRate(test.current); got != test.want {
				t.Fatalf("nextDemandRate(%d) = %d, want %d", test.current, got, test.want)
			}
		})
	}
}

// TestDemandRateCycle clicks Rate from the London rate until the rate is
// the London rate again. Each rate in the cycle must be valid.
func TestDemandRateCycle(t *testing.T) {
	t.Parallel()
	const london = 20
	want := []int{30, 60, 1, 2, 4, 8, 12, london}
	config := project.Default().Demand
	config.PerMinute = london
	for index, wantRate := range want {
		config.PerMinute = nextDemandRate(config.PerMinute)
		if config.PerMinute != wantRate {
			t.Fatalf("click %d rate = %d, want %d", index+1, config.PerMinute, wantRate)
		}
		if err := project.ValidateDemand(config, project.DemandContext{}); err != nil {
			t.Fatalf("click %d rate %d is not valid: %v", index+1, config.PerMinute, err)
		}
	}
}

func TestNextDemandSeed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		seed, want uint64
	}{
		{name: "zero", seed: 0, want: 1},
		{name: "London seed", seed: 20260922, want: 20260923},
		{name: "largest seed", seed: math.MaxUint64, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := nextDemandSeed(test.seed)
			if got != test.want {
				t.Fatalf("nextDemandSeed(%d) = %d, want %d", test.seed, got, test.want)
			}
			config := project.Default().Demand
			config.Seed = got
			if err := project.ValidateDemand(config, project.DemandContext{}); err != nil {
				t.Fatalf("seed %d is not valid: %v", got, err)
			}
		})
	}
}

// TestDemandClicks clicks Rate and Seed in a game that is connected to a
// session with the London demand rate and seed. Each click must send the
// next value, and the server must accept it.
func TestDemandClicks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, action string
		wantRate     int
		wantSeed     uint64
	}{
		{name: "rate", action: "demand-rate", wantRate: 30, wantSeed: 20260922},
		{name: "seed", action: "demand-seed", wantRate: 20, wantSeed: 20260923},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := project.Default()
			config.Demand.PerMinute, config.Demand.Seed = 20, 20260922
			game := sharedProjectGame(t, config)
			game.showDemand = true
			result := clickCommand(t, game, test.action)
			sent := result.Command.Demand
			if sent.PerMinute != test.wantRate || sent.Seed != test.wantSeed {
				t.Fatalf("click sent rate %d seed %d, want rate %d seed %d", sent.PerMinute, sent.Seed, test.wantRate, test.wantSeed)
			}
			syncGame(t, game, func() bool { return game.state.Demand.Config == sent })
			controls := game.buttons()
			wantLabels := map[string]string{
				"demand-rate": fmt.Sprintf("Rate: %d orders/min", test.wantRate),
				"demand-seed": fmt.Sprintf("Seed: %d", test.wantSeed),
			}
			for action, want := range wantLabels {
				if got := findButton(t, controls, action).label; got != want {
					t.Errorf("%s button = %q, want %q", action, got, want)
				}
			}
		})
	}
}

// TestDemandLabelsFitPanel checks that each label of the Demand panel stays
// in the right panel and clears the buttons and the other labels.
func TestDemandLabelsFitPanel(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.showDemand = true
			controls := game.buttons()
			panel := area{left: game.layout.right(796), top: game.layout.y(96), right: game.layout.right(1076), bottom: game.layout.bottom(570)}
			labels := game.demandLabels()
			if !slices.ContainsFunc(labels, func(value label) bool { return value.value == demandSavesNote && value.color == muted }) {
				t.Fatalf("Demand panel does not show %q in the muted color", demandSavesNote)
			}
			for index, value := range labels {
				got := game.labelArea(value)
				if got.left < panel.left || got.top < panel.top || got.right > panel.right || got.bottom > panel.bottom {
					t.Errorf("label %q %+v escapes panel %+v", value.value, got, panel)
				}
				for _, control := range controls {
					if got.overlaps(buttonArea(control)) {
						t.Errorf("label %q %+v overlaps control %q %+v", value.value, got, control.action, buttonArea(control))
					}
				}
				for _, other := range labels[index+1:] {
					if got.overlaps(game.labelArea(other)) {
						t.Errorf("label %q %+v overlaps label %q %+v", value.value, got, other.value, game.labelArea(other))
					}
				}
			}
		})
	}
}
