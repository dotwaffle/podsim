package view

import (
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestRunStatus checks the run status text in the header for each state.
func TestRunStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state session.State
		want  string
	}{
		{name: "before the first frame", state: session.State{Speed: 1}},
		{name: "new run", state: session.State{Epoch: "a", Speed: 1}, want: "1x  0.0 s  0 completed"},
		{name: "running", state: session.State{Epoch: "a", Speed: 2, Simulation: sim.Snapshot{Tick: 74070, Completed: 57}}, want: "2x  1234.5 s  57 completed"},
		{name: "paused", state: session.State{Epoch: "a", Speed: 2, Simulation: sim.Snapshot{Tick: 74070, Completed: 57, Paused: true}}, want: "PAUSED  2x  1234.5 s  57 completed"},
		{name: "paused new run", state: session.State{Epoch: "a", Speed: 1, Simulation: sim.Snapshot{Paused: true}}, want: "PAUSED  1x  0.0 s  0 completed"},
		{name: "part of a tenth", state: session.State{Epoch: "a", Speed: 8, Simulation: sim.Snapshot{Tick: 7, Completed: 1}}, want: "8x  0.1 s  1 completed"},
		{name: "London AM peak end", state: session.State{Epoch: "a", Speed: 4, Simulation: sim.Snapshot{Tick: 10800 * sim.TicksPerSecond, Completed: 3120}}, want: "4x  10800.0 s  3120 completed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := runStatus(test.state); got != test.want {
				t.Errorf("runStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestHeaderStatusLabel checks that the header shows the run status beside
// the title after the first state frame, in amber while the run is paused.
func TestHeaderStatusLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		epoch     string
		paused    bool
		want      string
		wantColor uint32
	}{
		{name: "before the first frame"},
		{name: "running", epoch: "a", want: "1x  0.0 s  0 completed", wantColor: muted},
		{name: "paused", epoch: "a", paused: true, want: "PAUSED  1x  0.0 s  0 completed", wantColor: amber},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := exampleTestGame(t)
			game.state.Epoch = test.epoch
			game.state.Simulation.Paused = test.paused
			// The header has the title, the run status, and the counts.
			// Before the first frame, it has only the title.
			labels := game.headerLabels()
			if test.want == "" {
				if len(labels) != 1 {
					t.Fatalf("header has %d labels before the first frame, want the title only", len(labels))
				}
				return
			}
			if len(labels) != 3 {
				t.Fatalf("header has %d labels, want 3", len(labels))
			}
			if got := labels[1]; got.value != test.want || got.color != test.wantColor || got.x != 157 {
				t.Errorf("run status = %q color %#06x at x %g, want %q color %#06x at x 157", got.value, got.color, got.x, test.want, test.wantColor)
			}
		})
	}
}

// TestHeaderStatusFitsHeader checks that a long run status stays between
// the title and the right edge of the map panel. The control test layouts
// include the minimum window size at device scales 1 and 1.5. The test adds
// device scale 2.
func TestHeaderStatusFitsHeader(t *testing.T) {
	t.Parallel()
	inputs := map[string]layoutInput{
		"minimum DPR2": {outsideWidth: minimumWidth, outsideHeight: minimumHeight, deviceScale: 2},
	}
	for _, layout := range controlLayouts {
		inputs[layout.name] = layout.input
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, input)
			// About 116 simulated days at the highest speed, with a
			// completed count of the same length.
			game.state.Epoch, game.state.Speed = "a", 8
			game.state.Simulation.Paused = true
			game.state.Simulation.Tick = 99999999 * sim.TicksPerSecond / 10
			game.state.Simulation.Completed = 9999999
			labels := game.headerLabels()
			if len(labels) != 3 {
				t.Fatalf("header has %d labels, want the title, the run status, and the counts", len(labels))
			}
			title, status, counts := game.labelArea(labels[0]), game.labelArea(labels[1]), game.labelArea(labels[2])
			if want := "PAUSED  8x  9999999.9 s  9999999 completed"; labels[1].value != want {
				t.Fatalf("run status = %q, want %q", labels[1].value, want)
			}
			if status.left <= title.right {
				t.Errorf("run status starts at %g, title ends at %g", status.left, title.right)
			}
			if limit := float64(game.layout.mapViewport.Max.X); status.right > limit {
				t.Errorf("run status ends at %g, map panel ends at %g", status.right, limit)
			}
			if status.overlaps(counts) {
				t.Errorf("run status %+v overlaps counts %+v", status, counts)
			}
			if status.top < 0 || status.bottom > game.layout.y(96) {
				t.Errorf("run status %+v is not above the panels at %g", status, game.layout.y(96))
			}
		})
	}
}

// TestInspectionRowsOmitRunStatus checks that the pod inspector does not
// repeat the run status of the header.
func TestInspectionRowsOmitRunStatus(t *testing.T) {
	t.Parallel()
	game := exampleTestGame(t)
	var names []string
	for _, row := range game.inspectionRows(game.state.Simulation.Vehicles[0]) {
		names = append(names, row.name)
	}
	// The pod is at a berth, so the last row is the station name, which has
	// no row name.
	if want := []string{"Speed", "On board", "Station phase", ""}; !slices.Equal(names, want) {
		t.Errorf("inspection rows = %q, want %q", names, want)
	}
}
