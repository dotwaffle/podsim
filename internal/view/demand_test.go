package view

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
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

// londonDemandError is a demand error with London station IDs. It is one
// of the longest errors that the demand stream can report.
const londonDemandError = "passenger route 940GZZLUEUS to 940GZZLUGDG: destination is unreachable"

// TestDemandLabelsFitPanel checks that each label of the Demand panel stays
// in the right panel and clears the buttons and the other labels, also with
// large counters. The panel shows a long demand error in full, in amber.
func TestDemandLabelsFitPanel(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.showDemand = true
			game.state.Demand.Error = londonDemandError
			game.state.Demand.Generated, game.state.Demand.Skipped = 123456, 123456
			game.state.Redistribution = true
			game.state.Simulation.RebalanceMoves, game.state.Simulation.EmptyDistanceMeters = 12345, 12345600
			controls := game.buttons()
			panel := area{left: game.layout.right(796), top: game.layout.y(96), right: game.layout.right(1076), bottom: game.layout.bottom(570)}
			labels := game.demandLabels()
			if !slices.ContainsFunc(labels, func(value label) bool { return value.value == demandSavesNote && value.color == muted }) {
				t.Fatalf("Demand panel does not show %q in the muted color", demandSavesNote)
			}
			var errorLines []string
			for _, value := range labels {
				if value.color == amber {
					errorLines = append(errorLines, value.value)
				}
			}
			if got := strings.Join(errorLines, " "); got != londonDemandError || len(errorLines) > demandErrorLines {
				t.Errorf("amber error lines = %q, want %q on at most %d lines", errorLines, londonDemandError, demandErrorLines)
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

// TestDemandLabelsWithoutError checks that the Demand panel shows no amber
// line when the demand stream has no error. It also checks that the panel
// shows the redistribution line.
func TestDemandLabelsWithoutError(t *testing.T) {
	t.Parallel()
	game := controlTestGame(t, controlLayouts[0].input)
	game.state.Redistribution = true
	game.state.Simulation.RebalanceMoves, game.state.Simulation.EmptyDistanceMeters = 3, 15327
	labels := game.demandLabels()
	for _, value := range labels {
		if value.color == amber {
			t.Errorf("label %q is amber without a demand error", value.value)
		}
	}
	const want = "Redistribution: on / 3 moves / 15.3 km empty"
	if !slices.ContainsFunc(labels, func(value label) bool { return value.value == want && value.color == muted }) {
		t.Errorf("Demand panel does not show %q in the muted color", want)
	}
}

func TestDemandPatternLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		config      session.DemandConfig
		destination string
		want        string
	}{
		{name: "balanced", config: session.DemandConfig{Pattern: "balanced"}, want: "Pattern: Balanced"},
		{name: "market", config: session.DemandConfig{Pattern: "market"}, want: "Pattern: Market-bound"},
		{name: "destination", config: session.DemandConfig{Pattern: "destination", Destination: "garden"}, destination: "Garden", want: "Pattern: Garden-bound"},
		{name: "profile gives the band first", config: session.DemandConfig{Pattern: "profile", Profile: "tfl-numbat-2019-midweek", Band: "am-peak"}, want: "Pattern: am-peak / tfl-numbat-2019-midweek"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := demandPatternLabel(test.config, test.destination); got != test.want {
				t.Fatalf("demandPatternLabel() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestDemandPatternButtonShowsBand checks that the Pattern button shows
// the London band in each layout, also when it cuts the profile ID.
func TestDemandPatternButtonShowsBand(t *testing.T) {
	t.Parallel()
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			game.showDemand = true
			game.state.Demand.Config = session.DemandConfig{Pattern: "profile", Profile: "tfl-numbat-2019-midweek", Band: "am-peak", PerMinute: 20}
			control := findButton(t, game.buttons(), "demand-pattern")
			const want = "Pattern: am-peak / "
			if got := game.fitButtonText(control.label, 14, control.w); !strings.HasPrefix(got, want) {
				t.Fatalf("Pattern button shows %q, want the prefix %q", got, want)
			}
		})
	}
}

func TestRedistributionText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state session.State
		want  string
	}{
		{name: "off", want: "Redistribution: off / 0 moves / 0 m empty"},
		{name: "one move", state: session.State{Redistribution: true, Simulation: sim.Snapshot{RebalanceMoves: 1, EmptyDistanceMeters: 480}}, want: "Redistribution: on / 1 move / 480 m empty"},
		{name: "London sample", state: session.State{Redistribution: true, Simulation: sim.Snapshot{RebalanceMoves: 3, EmptyDistanceMeters: 15327}}, want: "Redistribution: on / 3 moves / 15.3 km empty"},
		{name: "off with empty travel", state: session.State{Simulation: sim.Snapshot{EmptyDistanceMeters: 2049.9}}, want: "Redistribution: off / 0 moves / 2.0 km empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := redistributionText(test.state); got != test.want {
				t.Fatalf("redistributionText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTravelDistanceText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		meters float64
		want   string
	}{
		{meters: 0, want: "0 m"},
		{meters: 12.4, want: "12 m"},
		{meters: 999.4, want: "999 m"},
		{meters: 999.5, want: "1.0 km"},
		{meters: 1000, want: "1.0 km"},
		{meters: 15327, want: "15.3 km"},
		{meters: 123456, want: "123.5 km"},
	}
	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			t.Parallel()
			if got := travelDistanceText(test.meters); got != test.want {
				t.Fatalf("travelDistanceText(%g) = %q, want %q", test.meters, got, test.want)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	t.Parallel()
	source, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		t.Fatalf("load font: %v", err)
	}
	face := &text.GoTextFace{Source: source, Size: 10}
	widthOf := func(value string) float64 {
		width, _ := text.Measure(value, face, 0)
		return width
	}
	// cut returns value cut to width with an ellipsis, as fitText cuts it.
	cut := func(value string, width float64) string {
		got := fitText(value, textFit{face: face, width: width})
		if !strings.HasSuffix(got, "…") {
			t.Fatalf("test text %q fits width %g and has no cut", value, width)
		}
		return got
	}
	tests := []struct {
		name, value string
		width       float64
		limit       int
		want        []string
	}{
		{name: "no words", value: " ", width: widthOf("alpha"), limit: 2},
		{name: "one line", value: "alpha beta", width: widthOf("alpha beta"), limit: 2, want: []string{"alpha beta"}},
		{name: "two lines", value: "alpha beta gamma", width: widthOf("alpha beta"), limit: 2, want: []string{"alpha beta", "gamma"}},
		{name: "extra spaces", value: "  alpha   beta  gamma ", width: widthOf("alpha beta"), limit: 2, want: []string{"alpha beta", "gamma"}},
		{name: "last line cut", value: "alpha beta gamma delta epsilon", width: widthOf("alpha beta"), limit: 2, want: []string{"alpha beta", cut("gamma delta epsilon", widthOf("alpha beta"))}},
		{name: "long word cut", value: "alphabetical order", width: widthOf("alpha"), limit: 2, want: []string{cut("alphabetical", widthOf("alpha")), "order"}},
		{name: "one line limit", value: "alpha beta gamma", width: widthOf("alpha beta"), limit: 1, want: []string{cut("alpha beta gamma", widthOf("alpha beta"))}},
		{name: "no line limit", value: "alpha", width: widthOf("alpha"), limit: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := wrapText(test.value, textFit{face: face, width: test.width}, test.limit)
			for _, line := range got {
				if widthOf(line) > test.width {
					t.Errorf("line %q width %g exceeds %g", line, widthOf(line), test.width)
				}
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("wrapText(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
