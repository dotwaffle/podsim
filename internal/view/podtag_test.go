package view

import (
	"image"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPodDestinationCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		vehicle sim.Vehicle
		want    string
	}{
		{name: "pickup overrides old riders", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Traveling}, RelocatingTo: "940GZZLUACT", Riders: []sim.Request{{To: "940GZZLUCHX", Completed: true}}}, want: "ACT"},
		{name: "shared next stop", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Traveling}, Stops: []string{"940GZZLUCHX", "940GZZLUACT"}}, want: "CHX"},
		{name: "boarding", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Boarding}, Stops: []string{"940GZZBPSUST"}}, want: "BPS"},
		{name: "continuing", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Continuing}, Stops: []string{"940GZZNEUGST"}}, want: "NEL"},
		{name: "idle old journey", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Idle}, Stops: []string{"940GZZLUCHX"}}, want: ""},
		{name: "empty reposition", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.DepartingEmpty}, RelocatingTo: "940GZZLUACT"}, want: "ACT"},
		{name: "custom station", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Traveling}, RelocatingTo: "custom-CHX"}, want: ""},
		{name: "unknown destination", vehicle: sim.Vehicle{Pod: sim.Pod{Activity: sim.Traveling}}, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := podDestinationCode(test.vehicle); got != test.want {
				t.Fatalf("destination code = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPodTagBoundsIncludeDestination(t *testing.T) {
	t.Parallel()
	game := journeyTestGame(t, 31)
	game.selected = 0
	vehicle := sim.Vehicle{Pod: sim.Pod{Activity: sim.Traveling}, Stops: []string{"940GZZLUACT"}}
	tag := game.podMapLabels([]sim.Vehicle{vehicle}, nil)[0]
	if tag.value != "01\nACT" || tag.lineSpacing <= 0 {
		t.Fatalf("tag = %+v", tag)
	}
	bounds := game.labelBounds(tag)
	number := tag
	number.value, number.lineSpacing = "01", 0
	numberBounds := game.labelBounds(number)
	if bounds.Dy() <= numberBounds.Dy() {
		t.Fatalf("destination missing from bounds: %v vs %v", bounds, numberBounds)
	}
	obstacle := image.Rect(bounds.Min.X, numberBounds.Max.Y+1, bounds.Max.X, bounds.Max.Y)
	game.selected = -1
	if got := game.clearPodLabels([]label{tag}, []image.Rectangle{obstacle})[0]; got.value != "" {
		t.Fatal("destination overlapped station text")
	}
}
