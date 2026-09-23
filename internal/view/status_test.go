package view

import (
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestPodPurpose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		vehicle sim.Vehicle
		pending []sim.Request
		want    podPurpose
	}{
		{name: "idle", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Idle}}, want: purposeIdle},
		{name: "old completed trip", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Idle}, Request: &sim.Request{Completed: true}}, want: purposeIdle},
		{name: "pickup pending", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}, RelocatingTo: "garden"}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "pickup blocked", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling, WaitReason: sim.JunctionOccupied}, RelocatingTo: "garden"}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "assigned at station", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Idle}}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "passenger before next pickup", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling, Occupied: true}}, pending: []sim.Request{{PodID: "01"}}, want: purposePassengers},
		{name: "boarding before occupancy", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Boarding}}, want: purposePassengers},
		{name: "unloading", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Unloading, Occupied: true}}, want: purposePassengers},
		{name: "parking with previous trip", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.DepartingEmpty}, RelocatingTo: "parking", Request: &sim.Request{Completed: true}}, want: purposeParking},
		{name: "redistribution", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}, RelocatingTo: "garden", Rebalancing: true}, want: purposeRedistribution},
		{name: "diverted pickup wins", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}, RelocatingTo: "parking", Rebalancing: true}, pending: []sim.Request{{PodID: "01"}}, want: purposePickup},
		{name: "unassigned empty", vehicle: sim.Vehicle{Pod: sim.Pod{ID: "01", Activity: sim.Traveling}}, pending: []sim.Request{{PodID: "02"}}, want: purposeEmpty},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			game := &Game{network: sim.Example()}
			game.state.Simulation = sim.Snapshot{Vehicles: []sim.Vehicle{test.vehicle}, Pending: test.pending}
			got := game.podPurpose(test.vehicle, game.state.Simulation)
			if got != test.want {
				t.Fatalf("purpose %s, want %s", got.label(), test.want.label())
			}
			if color := game.podButtonColor(test.vehicle.Pod.ID); color != got.color() {
				t.Fatalf("button color %x differs from purpose color %x", color, got.color())
			}
		})
	}
}

func TestSummarizeFleet(t *testing.T) {
	t.Parallel()
	state := sim.Snapshot{
		Vehicles: []sim.Vehicle{
			{Pod: sim.Pod{ID: "idle", Activity: sim.Idle}},
			{Pod: sim.Pod{ID: "assigned", Activity: sim.Idle}},
			{Pod: sim.Pod{ID: "empty", Activity: sim.Traveling}},
			{Pod: sim.Pod{ID: "boarding", Activity: sim.Boarding}},
			{Pod: sim.Pod{ID: "occupied", Activity: sim.Traveling, Occupied: true}},
		},
		Pending: []sim.Request{{PodID: "assigned"}},
	}
	use := summarizeFleet(state)
	if use.active != 4 || use.passenger != 2 || use.total != 5 {
		t.Fatalf("fleet use = %+v", use)
	}
	if use.activePercent() != 80 || use.passengerPercent() != 40 {
		t.Fatalf("fleet percentages = %d active / %d passenger", use.activePercent(), use.passengerPercent())
	}
	if got := summarizeFleet(sim.Snapshot{}); got.activePercent() != 0 || got.passengerPercent() != 0 {
		t.Fatalf("empty fleet percentages = %+v", got)
	}
}

func TestPodPurposeFollowsActualPickup(t *testing.T) {
	t.Parallel()
	network := sim.Example()
	simulation, err := sim.NewFleet(network, []sim.Placement{{ID: "01", StationID: "parking"}})
	if err != nil {
		t.Fatal(err)
	}
	game := &Game{network: network}
	if err := simulation.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	state := simulation.Snapshot()
	if got := game.podPurpose(state.Vehicles[0], state); got != purposePickup {
		t.Fatalf("dispatched pod is %s", got.label())
	}
	sawPassengers := false
	for range 5 * 60 * sim.TicksPerSecond {
		simulation.Step()
		state = simulation.Snapshot()
		purpose := game.podPurpose(state.Vehicles[0], state)
		sawPassengers = sawPassengers || purpose == purposePassengers
		if state.Completed == 1 {
			if !sawPassengers || purpose != purposeIdle {
				t.Fatalf("completed trip: passenger phase %t, final purpose %s", sawPassengers, purpose.label())
			}
			return
		}
	}
	t.Fatal("pickup journey did not complete")
}

var allPurposes = []podPurpose{purposeIdle, purposePickup, purposePassengers, purposeParking, purposeRedistribution, purposeEmpty}

func TestPurposeColorsRemainDistinct(t *testing.T) {
	t.Parallel()
	colors := make(map[uint32]podPurpose)
	for _, purpose := range allPurposes {
		if previous, ok := colors[purpose.color()]; ok {
			t.Fatalf("%s and %s share a color", previous.label(), purpose.label())
		}
		colors[purpose.color()] = purpose
	}
}

// TestPurposeColorsWithColorVisionDeficiency checks that the purpose colors
// stay apart with each full color vision deficiency. The previous palette
// had a difference of 0.3 between Passengers and Redistributing with
// deuteranopia.
func TestPurposeColorsWithColorVisionDeficiency(t *testing.T) {
	t.Parallel()
	const minimumDifference = 4.5
	for _, vision := range colorVisions {
		t.Run(vision.name, func(t *testing.T) {
			t.Parallel()
			for i, first := range allPurposes {
				for _, second := range allPurposes[i+1:] {
					difference := ciede2000(vision.lab(first.color()), vision.lab(second.color()))
					if difference < minimumDifference {
						t.Errorf("%s and %s differ by %.1f, want %.1f or more", first.label(), second.label(), difference, minimumDifference)
					}
				}
			}
		})
	}
}

func TestPurposeColorContrast(t *testing.T) {
	t.Parallel()
	for _, purpose := range allPurposes {
		for _, surface := range []uint32{panel, background, podButtonFill} {
			if ratio := contrastRatio(purpose.color(), surface); ratio < 4.5 {
				t.Errorf("%s has a contrast of %.2f:1 on %06x, want 4.5:1 or more", purpose.label(), ratio, surface)
			}
		}
	}
}

func TestPurposeEmptyMove(t *testing.T) {
	t.Parallel()
	want := map[podPurpose]bool{
		purposeIdle:           false,
		purposePickup:         true,
		purposePassengers:     false,
		purposeParking:        true,
		purposeRedistribution: true,
		purposeEmpty:          true,
	}
	for _, purpose := range allPurposes {
		if got := purpose.emptyMove(); got != want[purpose] {
			t.Errorf("%s empty move = %t, want %t", purpose.label(), got, want[purpose])
		}
	}
}

func TestStationPhaseLabel(t *testing.T) {
	t.Parallel()
	network := sim.Example()
	for _, test := range []struct {
		name string
		pod  sim.Pod
		want string
	}{
		{name: "main network", want: "Main network"},
		{name: "known station", pod: sim.Pod{StationPhase: sim.AccessingBerth, ManeuverStationID: "market"}, want: "Accessing berth / Market"},
		{name: "missing station", pod: sim.Pod{StationPhase: sim.ExitingStation, ManeuverStationID: "missing"}, want: "Exiting station"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := stationPhaseLabel(test.pod, network); got != test.want {
				t.Fatalf("label = %q, want %q", got, test.want)
			}
		})
	}
}

// colorVision simulates one type of color vision. The matrix applies to
// linear RGB. The matrices for the deficiencies are the full-severity
// matrices of Machado, Oliveira and Fernandes (2009).
type colorVision struct {
	name   string
	matrix [3][3]float64
}

var (
	normalVision = colorVision{name: "normal", matrix: [3][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}}
	protanopia   = colorVision{name: "protanopia", matrix: [3][3]float64{{0.152286, 1.052583, -0.204868}, {0.114503, 0.786281, 0.099216}, {-0.003882, -0.048116, 1.051998}}}
	deuteranopia = colorVision{name: "deuteranopia", matrix: [3][3]float64{{0.367322, 0.860646, -0.227968}, {0.280085, 0.672501, 0.047413}, {-0.011820, 0.042940, 0.968881}}}
	tritanopia   = colorVision{name: "tritanopia", matrix: [3][3]float64{{1.255528, -0.076749, -0.178779}, {-0.078411, 0.930809, 0.147602}, {0.004733, 0.691367, 0.303900}}}
	colorVisions = []colorVision{normalVision, protanopia, deuteranopia, tritanopia}
)

// TestColorVisionSimulation pins the simulation to known results. The
// previous palette merged these pairs, so a wrong matrix or a wrong color
// difference cannot let that palette pass.
func TestColorVisionSimulation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		vision        colorVision
		first, second uint32
		want          float64
	}{
		{name: "normal passengers and redistribution", vision: normalVision, first: 0x76df70, second: 0xffa879, want: 48.7846},
		{name: "deuteranopia passengers and redistribution", vision: deuteranopia, first: 0x76df70, second: 0xffa879, want: 0.2715},
		{name: "normal pickup and parking", vision: normalVision, first: 0x579dff, second: 0xb6a0ff, want: 18.6431},
		{name: "protanopia pickup and parking", vision: protanopia, first: 0x579dff, second: 0xb6a0ff, want: 3.4527},
		{name: "protanopia passengers and empty travel", vision: protanopia, first: 0x76df70, second: 0xe6d889, want: 3.8435},
		{name: "tritanopia idle and pickup", vision: tritanopia, first: muted, second: 0x579dff, want: 11.9871},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ciede2000(test.vision.lab(test.first), test.vision.lab(test.second)); math.Abs(got-test.want) > 1e-3 {
				t.Fatalf("difference = %.4f, want %.4f", got, test.want)
			}
		})
	}
}

// TestCIEDE2000 uses pairs from the CIEDE2000 test data of Sharma, Wu and
// Dalal (2005).
func TestCIEDE2000(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		first, second [3]float64
		want          float64
	}{
		{first: [3]float64{50, 2.6772, -79.7751}, second: [3]float64{50, 0, -82.7485}, want: 2.0425},
		{first: [3]float64{50, 0, 0}, second: [3]float64{50, -1, 2}, want: 2.3669},
		{first: [3]float64{50, 2.49, -0.001}, second: [3]float64{50, -2.49, 0.0009}, want: 7.1792},
		{first: [3]float64{50, 2.49, -0.001}, second: [3]float64{50, -2.49, 0.0011}, want: 7.2195},
		{first: [3]float64{50, -0.001, 2.49}, second: [3]float64{50, 0.0011, -2.49}, want: 4.7461},
		{first: [3]float64{50, 2.5, 0}, second: [3]float64{73, 25, -18}, want: 27.1492},
		{first: [3]float64{50, 2.5, 0}, second: [3]float64{58, 24, 15}, want: 19.4535},
	} {
		if got := ciede2000(test.first, test.second); math.Abs(got-test.want) > 1e-4 {
			t.Errorf("ciede2000(%v, %v) = %.4f, want %.4f", test.first, test.second, got, test.want)
		}
	}
}

// linearRGB converts an sRGB color to linear RGB values from 0 to 1.
func linearRGB(hex uint32) [3]float64 {
	var linear [3]float64
	for i, shift := range []uint{16, 8, 0} {
		value := float64((hex>>shift)&255) / 255
		if value <= 0.04045 {
			linear[i] = value / 12.92
		} else {
			linear[i] = math.Pow((value+0.055)/1.055, 2.4)
		}
	}
	return linear
}

// lab returns the CIELAB value (D65) of an sRGB color as this vision sees it.
func (vision colorVision) lab(hex uint32) [3]float64 {
	linear := linearRGB(hex)
	var seen [3]float64
	for row := range 3 {
		seen[row] = min(1, max(0, vision.matrix[row][0]*linear[0]+vision.matrix[row][1]*linear[1]+vision.matrix[row][2]*linear[2]))
	}
	x := (0.4124*seen[0] + 0.3576*seen[1] + 0.1805*seen[2]) / 0.95047
	y := 0.2126*seen[0] + 0.7152*seen[1] + 0.0722*seen[2]
	z := (0.0193*seen[0] + 0.1192*seen[1] + 0.9505*seen[2]) / 1.08883
	f := func(value float64) float64 {
		if value > 0.008856 {
			return math.Cbrt(value)
		}
		return 7.787*value + 16.0/116
	}
	return [3]float64{116*f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))}
}

// ciede2000 returns the CIEDE2000 color difference of two CIELAB values.
func ciede2000(first, second [3]float64) float64 {
	radians := func(degrees float64) float64 { return degrees * math.Pi / 180 }
	chromaWeight := func(chroma float64) float64 {
		return math.Sqrt(math.Pow(chroma, 7) / (math.Pow(chroma, 7) + math.Pow(25, 7)))
	}
	g := 0.5 * (1 - chromaWeight((math.Hypot(first[1], first[2])+math.Hypot(second[1], second[2]))/2))
	a1, a2 := (1+g)*first[1], (1+g)*second[1]
	c1, c2 := math.Hypot(a1, first[2]), math.Hypot(a2, second[2])
	h1 := math.Mod(math.Atan2(first[2], a1)*180/math.Pi+360, 360)
	h2 := math.Mod(math.Atan2(second[2], a2)*180/math.Pi+360, 360)
	deltaHue, meanHue := 0.0, h1+h2
	if c1*c2 != 0 {
		deltaHue = h2 - h1
		switch {
		case deltaHue > 180:
			deltaHue -= 360
		case deltaHue < -180:
			deltaHue += 360
		}
		switch {
		case math.Abs(h1-h2) <= 180:
			meanHue = (h1 + h2) / 2
		case h1+h2 < 360:
			meanHue = (h1 + h2 + 360) / 2
		default:
			meanHue = (h1 + h2 - 360) / 2
		}
	}
	deltaL := second[0] - first[0]
	deltaC := c2 - c1
	deltaH := 2 * math.Sqrt(c1*c2) * math.Sin(radians(deltaHue/2))
	meanL, meanC := (first[0]+second[0])/2, (c1+c2)/2
	t := 1 - 0.17*math.Cos(radians(meanHue-30)) + 0.24*math.Cos(radians(2*meanHue)) + 0.32*math.Cos(radians(3*meanHue+6)) - 0.20*math.Cos(radians(4*meanHue-63))
	hueOffset := (meanHue - 275) / 25
	rotation := -math.Sin(radians(60*math.Exp(-hueOffset*hueOffset))) * 2 * chromaWeight(meanC)
	lightnessOffset := (meanL - 50) * (meanL - 50)
	lightness := deltaL / (1 + 0.015*lightnessOffset/math.Sqrt(20+lightnessOffset))
	chroma := deltaC / (1 + 0.045*meanC)
	hue := deltaH / (1 + 0.015*meanC*t)
	return math.Sqrt(lightness*lightness + chroma*chroma + hue*hue + rotation*chroma*hue)
}
