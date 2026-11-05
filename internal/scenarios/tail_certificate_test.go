package scenarios_test

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// certPoint is the point type of the certificate. The certificate has its own
// polyline code, and it does not read a production bound.
type certPoint struct{ x, y float64 }

func (a certPoint) minus(b certPoint) certPoint { return certPoint{a.x - b.x, a.y - b.y} }

func (a certPoint) along(direction certPoint, distance float64) certPoint {
	return certPoint{a.x + direction.x*distance, a.y + direction.y*distance}
}

func (a certPoint) dot(b certPoint) float64 { return a.x*b.x + a.y*b.y }

// certPolyline returns the 65 points of a quadratic lane or the 2 points of a
// straight lane, with no repeated point.
func certPolyline(from, to sim.Point, control *sim.Point) []certPoint {
	points := []certPoint{{from.X, from.Y}}
	if control != nil {
		for step := 1; step < 64; step++ {
			t := float64(step) / 64
			u := 1 - t
			next := certPoint{u*u*from.X + 2*u*t*control.X + t*t*to.X, u*u*from.Y + 2*u*t*control.Y + t*t*to.Y}
			if next != points[len(points)-1] {
				points = append(points, next)
			}
		}
	}
	if last := (certPoint{to.X, to.Y}); last != points[len(points)-1] {
		points = append(points, last)
	}
	return points
}

// certSegment is a segment with a unit direction, a length, and the path
// distance at its start.
type certSegment struct {
	from, unit    certPoint
	length, start float64
}

func certSegments(points []certPoint) []certSegment {
	segments := make([]certSegment, 0, len(points)-1)
	start := 0.0
	for index := 1; index < len(points); index++ {
		delta := points[index].minus(points[index-1])
		length := math.Hypot(delta.x, delta.y)
		segments = append(segments, certSegment{from: points[index-1], unit: certPoint{delta.x / length, delta.y / length}, length: length, start: start})
		start += length
	}
	return segments
}

func certClamp(value, low, high float64) float64 { return max(low, min(high, value)) }

// certFreeMinimum returns the closest pair of two segments, as distances
// along them.
func certFreeMinimum(first, second certSegment) (p, q float64) {
	w := first.from.minus(second.from)
	b := first.unit.dot(second.unit)
	d, e := first.unit.dot(w), second.unit.dot(w)
	if denominator := 1 - b*b; denominator > 1e-12 {
		p = certClamp((b*e-d)/denominator, 0, first.length)
	}
	q = b*p + e
	if q < 0 {
		return certClamp(-d, 0, first.length), 0
	}
	if q > second.length {
		return certClamp(b*second.length-d, 0, first.length), second.length
	}
	return p, q
}

// certPairMinimum returns the smallest squared distance of a point at p on
// the first segment and a point at q on the second, with q-p of at least
// offset, and false for an empty domain. The domain is a rectangle cut by one
// line, and the squared distance is convex. So the result is the free minimum
// when it is in the domain, and otherwise a minimum on the line.
func certPairMinimum(first, second certSegment, offset float64) (float64, bool) {
	squared := func(p, q float64) float64 {
		delta := first.from.along(first.unit, p).minus(second.from.along(second.unit, q))
		return delta.dot(delta)
	}
	if second.length < offset {
		return 0, false
	}
	if p, q := certFreeMinimum(first, second); q-p >= offset {
		return squared(p, q), true
	}
	low, high := max(0, -offset), min(first.length, second.length-offset)
	// On the line q = p + offset, the difference is r + p*v.
	r := first.from.minus(second.from.along(second.unit, offset))
	v := first.unit.minus(second.unit)
	p := low
	if denominator := v.dot(v); denominator > 0 {
		p = certClamp(-r.dot(v)/denominator, low, high)
	}
	return squared(p, p+offset), true
}

// certMinimum returns the smallest squared distance over each pair of a
// polyline with a path gap of at least tail, and the number of segment pairs.
func certMinimum(points []certPoint, tail float64) (minimum float64, pairs int) {
	segments := certSegments(points)
	minimum = math.Inf(1)
	for index, first := range segments {
		for _, second := range segments[index:] {
			pairs++
			if value, ok := certPairMinimum(first, second, tail-(second.start-first.start)); ok {
				minimum = min(minimum, value)
			}
		}
	}
	return minimum, pairs
}

// allClassNetwork admits all classes on each lane, station, and berth.
func allClassNetwork(tb testing.TB, network sim.Network) sim.Network {
	tb.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		tb.Fatal(err)
	}
	for index := range network.Lanes {
		network.Lanes[index].VehicleClasses = classes
	}
	for index := range network.Stations {
		station := &network.Stations[index]
		station.VehicleClasses = classes
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = classes
		}
	}
	return network
}

// tailPresets holds the network of each preset.
func tailPresets(t *testing.T) []struct {
	name    string
	network sim.Network
} {
	t.Helper()
	compact, err := scenarios.CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	type preset = struct {
		name    string
		network sim.Network
	}
	configs := []struct {
		name   string
		config project.Config
	}{
		{"small", scenarios.Small()}, {"busy", scenarios.Busy()}, {"parking-constrained", scenarios.ParkingConstrained()},
		{"rail-hub", scenarios.RailHub()}, {"rail-hub-incidents", scenarios.RailHubIncidents()}, {"scale100", scenarios.Scale100()},
		{"compact-station", compact}, {"london-central", scenarios.LondonCentral()}, {"london-full", scenarios.LondonFull()},
	}
	presets := []preset{{"independent-banks", sim.BankExample()}}
	for _, entry := range configs {
		presets = append(presets, preset{entry.name, entry.config.Network})
	}
	return presets
}

func certNodes(network sim.Network) map[string]sim.Point {
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	return nodes
}

// certifyLanes runs the certificate on each curved lane of a network.
func certifyLanes(t *testing.T, name string, network sim.Network) (lanes int) {
	t.Helper()
	nodes, tails := certNodes(network), sim.LaneTailsForTest(network)
	for _, lane := range network.Lanes {
		if lane.Control == nil {
			continue
		}
		tail := tails[lane.ID].Tail
		if tail == 0 || !finiteTail(tail) {
			continue
		}
		lanes++
		minimum, pairs := certMinimum(certPolyline(nodes[lane.From], nodes[lane.To], lane.Control), tail)
		if pairs > 2080 {
			t.Errorf("%s lane %s: %d segment pairs", name, lane.ID, pairs)
		}
		if minimum < 400 {
			t.Errorf("%s lane %s: tail %v has a smallest distance of %v m", name, lane.ID, tail, math.Sqrt(minimum))
		}
	}
	return lanes
}

func finiteTail(tail float64) bool { return !math.IsInf(tail, 0) && !math.IsNaN(tail) }

// oneLaneNetwork is a network of one all-class curved lane.
func oneLaneNetwork(tb testing.TB, to, control sim.Point) sim.Network {
	tb.Helper()
	return allClassNetwork(tb, sim.Network{
		Nodes: []sim.Node{{ID: "a"}, {ID: "b", Position: to}},
		Lanes: []sim.Lane{{ID: "curve", From: "a", To: "b", SpeedLimit: 14, Control: &control}},
	})
}

// tailShapes holds the end and the control of the curved lanes of the tables.
var tailShapes = []struct {
	name        string
	to, control sim.Point
}{
	{"gentle arc", sim.Point{X: 200}, sim.Point{X: 100, Y: 40}},
	{"hairpin chord 30", sim.Point{X: 30}, sim.Point{X: 15, Y: 200}},
	{"hairpin chord 22", sim.Point{X: 22}, sim.Point{X: 11, Y: 320}},
	{"narrow arc chord 10", sim.Point{X: 10}, sim.Point{X: 5, Y: 100}},
	{"asymmetric hairpin", sim.Point{X: 37, Y: -1.5}, sim.Point{X: 34.3, Y: 260}},
	{"short hairpin", sim.Point{X: 31.6}, sim.Point{X: 11.3, Y: 64.3}},
	{"chord at the limit", sim.Point{X: 20.126}, sim.Point{X: 10, Y: 48}},
	{"fan", sim.Point{X: 144.2}, sim.Point{X: 100, Y: 90}},
	{"long arc", sim.Point{X: 5000}, sim.Point{X: 2500, Y: 2000}},
	{"long hairpin", sim.Point{X: 41}, sim.Point{X: 14, Y: 3113}},
}

// coordinateShapes holds lanes at the coordinate limit of the project format.
var coordinateShapes = []struct {
	name          string
	from, to, ctl sim.Point
}{
	{"largest arc", sim.Point{X: -project.MaxCoordinate, Y: -project.MaxCoordinate}, sim.Point{X: project.MaxCoordinate, Y: -project.MaxCoordinate}, sim.Point{X: 0, Y: project.MaxCoordinate}},
	{"largest narrow arc", sim.Point{X: -10, Y: -project.MaxCoordinate}, sim.Point{X: 10, Y: -project.MaxCoordinate}, sim.Point{X: 0, Y: project.MaxCoordinate}},
	{"largest hairpin", sim.Point{X: -50, Y: -project.MaxCoordinate}, sim.Point{X: 50, Y: -project.MaxCoordinate}, sim.Point{X: 0, Y: project.MaxCoordinate}},
	{"long diagonal", sim.Point{X: -project.MaxCoordinate, Y: -project.MaxCoordinate}, sim.Point{X: project.MaxCoordinate, Y: project.MaxCoordinate}, sim.Point{X: project.MaxCoordinate, Y: -project.MaxCoordinate}},
}

func coordinateNetwork(tb testing.TB, from, to, control sim.Point) sim.Network {
	tb.Helper()
	network := oneLaneNetwork(tb, to, control)
	network.Nodes[0].Position = from
	return network
}

// TestTailCertificatePresets runs the static certificate on each curved lane
// of each preset with all classes.
func TestTailCertificatePresets(t *testing.T) {
	t.Parallel()
	total := 0
	for _, preset := range tailPresets(t) {
		total += certifyLanes(t, preset.name, allClassNetwork(t, preset.network))
	}
	if total < 500 {
		t.Fatalf("the certificate checked only %d curved lanes", total)
	}
}

// TestTailCertificateShapes runs the certificate on the tables of curved
// lanes, and it shows that the certificate refuses a short tail.
func TestTailCertificateShapes(t *testing.T) {
	t.Parallel()
	for _, shape := range tailShapes {
		certifyLanes(t, shape.name, oneLaneNetwork(t, shape.to, shape.control))
	}
	for _, shape := range coordinateShapes {
		certifyLanes(t, shape.name, coordinateNetwork(t, shape.from, shape.to, shape.ctl))
	}
	// A pair of the hairpin at the study scan has a distance under 20 m at
	// 0.7 times that scan, so the certificate can refuse a short tail.
	points := certPolyline(sim.Point{}, sim.Point{X: 30}, &sim.Point{X: 15, Y: 200})
	if minimum, _ := certMinimum(points, 0.7*93.85); minimum >= 400 {
		t.Fatalf("the certificate accepted a short tail, with %v m", math.Sqrt(minimum))
	}
	if minimum, _ := certMinimum(points, 20); minimum >= 400 {
		t.Fatalf("the certificate accepted a tail of 20 m, with %v m", math.Sqrt(minimum))
	}
}

// TestTailCertificateClosedForm compares the closed form with a dense scan of
// a hairpin polyline.
func TestTailCertificateClosedForm(t *testing.T) {
	t.Parallel()
	points := certPolyline(sim.Point{}, sim.Point{X: 30}, &sim.Point{X: 15, Y: 200})
	segments := certSegments(points)
	at := func(distance float64) certPoint {
		index, found := slices.BinarySearchFunc(segments, distance, func(s certSegment, d float64) int {
			switch {
			case s.start+s.length < d:
				return -1
			case s.start > d:
				return 1
			}
			return 0
		})
		if !found {
			index = len(segments) - 1
		}
		return segments[index].from.along(segments[index].unit, distance-segments[index].start)
	}
	total := segments[len(segments)-1].start + segments[len(segments)-1].length
	for _, tail := range []float64{20, 60, 94.25, 150} {
		dense := math.Inf(1)
		for s := 0.0; s+tail <= total; s += 0.1 {
			for u := s + tail; u <= total; u += 0.1 {
				delta := at(u).minus(at(s))
				dense = min(dense, delta.dot(delta))
			}
		}
		closed, _ := certMinimum(points, tail)
		if closed > dense+1e-9 || math.Sqrt(dense)-math.Sqrt(closed) > 0.2 {
			t.Fatalf("tail %v: closed form %v m, dense scan %v m", tail, math.Sqrt(closed), math.Sqrt(dense))
		}
	}
}

// TestTailLimits checks the limits of section 4.1 with independent values.
func TestTailLimits(t *testing.T) {
	t.Parallel()
	check := func(name string, network sim.Network) {
		t.Helper()
		lane := network.Lanes[0]
		nodes := certNodes(network)
		bounds := sim.LaneTailsForTest(network)[lane.ID]
		from, to := nodes[lane.From], nodes[lane.To]
		points := certPolyline(from, to, lane.Control)
		segments := certSegments(points)
		last := segments[len(segments)-1]
		length := last.start + last.length
		step := max(0.25, length/1024)
		oldBound := bounds.Front * 20 / 6
		if bounds.Tail < 20 || bounds.Tail > oldBound*(1+1e-12) || bounds.FromTail != bounds.Tail {
			t.Errorf("%s: tail %v, from tail %v, old bound %v", name, bounds.Tail, bounds.FromTail, oldBound)
		}
		if math.Abs(bounds.Tail-oldBound) > 1e-9*oldBound {
			if bounds.Tail >= length+2*step {
				t.Errorf("%s: tail %v is not under the length %v plus 2 steps of %v", name, bounds.Tail, length, step)
			}
			if multiple := bounds.Tail / step; math.Abs(multiple-math.Round(multiple)) > 1e-9 && bounds.Tail != 20 {
				t.Errorf("%s: tail %v is not a multiple of %v", name, bounds.Tail, step)
			}
		}
	}
	for _, shape := range tailShapes {
		check(shape.name, oneLaneNetwork(t, shape.to, shape.control))
	}
	for _, shape := range coordinateShapes {
		check(shape.name, coordinateNetwork(t, shape.from, shape.to, shape.ctl))
	}
	for _, end := range []sim.Point{{X: 100}, {X: 300, Y: 700}, {X: 1.7, Y: 41.3}, {X: -33.3, Y: 77.7}} {
		network := allClassNetwork(t, sim.Network{
			Nodes: []sim.Node{{ID: "a"}, {ID: "b", Position: end}},
			Lanes: []sim.Lane{{ID: "line", From: "a", To: "b", SpeedLimit: 14}},
		})
		if tail := sim.LaneTailsForTest(network)["line"].Tail; tail != 20 {
			t.Errorf("straight lane to %+v: tail %v, want exactly 20", end, tail)
		}
	}
}

// TestTailFrontKeepsProjectionBound checks the stored front of each lane of
// each preset against the projection bound, with independent code. The front
// does not follow the tail, so the window of the observer does not change.
func TestTailFrontKeepsProjectionBound(t *testing.T) {
	t.Parallel()
	for _, preset := range tailPresets(t) {
		network := allClassNetwork(t, preset.network)
		nodes, tails := certNodes(network), sim.LaneTailsForTest(network)
		for _, lane := range network.Lanes {
			want := 6.0
			if lane.Control != nil {
				segments := certSegments(certPolyline(nodes[lane.From], nodes[lane.To], lane.Control))
				last := segments[len(segments)-1]
				end := last.from.along(last.unit, last.length)
				chord := end.minus(segments[0].from)
				length := math.Hypot(chord.x, chord.y)
				unit := certPoint{chord.x / length, chord.y / length}
				minimum := 1.0
				for _, segment := range segments {
					minimum = min(minimum, segment.unit.dot(unit))
				}
				want = 6 / (minimum * (1 - 1e-9))
			}
			got := tails[lane.ID].Front
			if finiteTail(got) && math.Abs(got-want) > 1e-6*want {
				t.Errorf("%s lane %s: front %v, want %v", preset.name, lane.ID, got, want)
			}
		}
	}
}

// pastBudgetLane is a lane at the coordinate limit whose walk passes the
// budget of evaluations.
func pastBudgetLane(tb testing.TB) sim.Network {
	tb.Helper()
	return oneLaneNetwork(tb, sim.Point{X: 228.2874, Y: -0.1606}, sim.Point{X: 158.4429, Y: 98983.9033})
}

// TestTailBudget checks that a lane past the budget takes the projection
// bound, and that each lane of the tables stays inside the budget.
func TestTailBudget(t *testing.T) {
	t.Parallel()
	network := pastBudgetLane(t)
	lane := network.Lanes[0]
	if used := sim.TailWalkEvaluationsForTest(network.Polyline(lane)); used <= sim.TailWalkBudgetForTest {
		t.Fatalf("the walk used %d evaluations, inside the budget of %d", used, sim.TailWalkBudgetForTest)
	}
	bounds := sim.LaneTailsForTest(network)[lane.ID]
	if oldBound := bounds.Front * 20 / 6; math.Abs(bounds.Tail-oldBound) > 1e-9*oldBound {
		t.Fatalf("tail %v past the budget, want the projection bound %v", bounds.Tail, oldBound)
	}
	for _, shape := range tailShapes {
		network := oneLaneNetwork(t, shape.to, shape.control)
		if used := sim.TailWalkEvaluationsForTest(network.Polyline(network.Lanes[0])); used > sim.TailWalkBudgetForTest {
			t.Errorf("%s: the walk used %d evaluations", shape.name, used)
		}
	}
}

// TestTailBudgetPresets checks that no lane of a preset falls back.
func TestTailBudgetPresets(t *testing.T) {
	t.Parallel()
	largest := 0
	for _, preset := range tailPresets(t) {
		network := allClassNetwork(t, preset.network)
		for _, lane := range network.Lanes {
			if lane.Control == nil {
				continue
			}
			used := sim.TailWalkEvaluationsForTest(network.Polyline(lane))
			largest = max(largest, used)
			if used > sim.TailWalkBudgetForTest {
				t.Errorf("%s lane %s: the walk used %d evaluations", preset.name, lane.ID, used)
			}
		}
	}
	t.Logf("largest walk of a preset lane: %d evaluations", largest)
}

// certSample returns the points at each step of a path distance. The step is
// the larger of 0.5 m and 1/400 of the length.
func certSample(points []certPoint) (samples []certPoint, distances []float64, length float64) {
	segments := certSegments(points)
	last := segments[len(segments)-1]
	length = last.start + last.length
	step := max(0.5, length/400)
	index := 0
	for distance := 0.0; ; distance = min(length, distance+step) {
		for index < len(segments)-1 && distance > segments[index].start+segments[index].length {
			index++
		}
		samples = append(samples, segments[index].from.along(segments[index].unit, distance-segments[index].start))
		distances = append(distances, distance)
		if distance == length {
			return samples, distances, length
		}
	}
}

// certLane holds the samples of one lane of the pair coverage gate.
type certLane struct {
	id, from, to           string
	samples                []certPoint
	distances              []float64
	length                 float64
	minX, minY, maxX, maxY float64
}

func newCertLane(lane sim.Lane, nodes map[string]sim.Point) certLane {
	samples, distances, length := certSample(certPolyline(nodes[lane.From], nodes[lane.To], lane.Control))
	entry := certLane{id: lane.ID, from: lane.From, to: lane.To, samples: samples, distances: distances, length: length,
		minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	for _, sample := range samples {
		entry.minX, entry.maxX = min(entry.minX, sample.x), max(entry.maxX, sample.x)
		entry.minY, entry.maxY = min(entry.minY, sample.y), max(entry.maxY, sample.y)
	}
	return entry
}

func (a certLane) sharesNode(b certLane) bool {
	return a.from == b.from || a.from == b.to || a.to == b.from || a.to == b.to
}

func (a certLane) near(b certLane) bool {
	return a.minX-20 <= b.maxX && b.minX-20 <= a.maxX && a.minY-20 <= b.maxY && b.minY-20 <= a.maxY
}

// TestTailPairCoverage checks each preset that prepares with all classes. A
// close sample on two lanes with a shared node must be in cells of both lanes
// that hold one junction, and two lanes with no shared node have no close
// sample. The other presets refuse the classes at their compact bays.
func TestTailPairCoverage(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long test")
	}
	refused := map[string]bool{"compact-station": true, "london-central": true, "london-full": true}
	for _, preset := range tailPresets(t) {
		t.Run(preset.name, func(t *testing.T) {
			t.Parallel()
			network := allClassNetwork(t, preset.network)
			prepared, err := sim.PrepareNetwork(network)
			if refused[preset.name] {
				if err == nil {
					t.Fatal("the preset prepares with all classes, so the gate must cover it")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			nodes := certNodes(network)
			lanes := make([]certLane, 0, len(network.Lanes))
			for _, lane := range network.Lanes {
				lanes = append(lanes, newCertLane(lane, nodes))
			}
			closeSamples := 0
			for index, first := range lanes {
				for _, second := range lanes[index+1:] {
					if first.near(second) {
						closeSamples += checkPairCoverage(t, prepared, first, second)
					}
				}
			}
			if closeSamples == 0 {
				t.Fatal("no close samples, so the gate checked nothing")
			}
		})
	}
}

// checkPairCoverage checks one lane pair and returns its close samples.
func checkPairCoverage(t *testing.T, prepared *sim.PreparedNetwork, first, second certLane) int {
	t.Helper()
	shared, count := first.sharesNode(second), 0
	for a, pointA := range first.samples {
		var heldA []string
		for b, pointB := range second.samples {
			delta := pointA.minus(pointB)
			if delta.dot(delta) >= 400 {
				continue
			}
			count++
			if !shared {
				t.Fatalf("lanes %s and %s share no node, and they have a gap of %v m", first.id, second.id, math.Sqrt(delta.dot(delta)))
			}
			if heldA == nil {
				heldA = prepared.CellJunctionsForTest(first.id, first.distances[a], first.length)
			}
			heldB := prepared.CellJunctionsForTest(second.id, second.distances[b], second.length)
			if !slices.ContainsFunc(heldA, func(junction string) bool { return slices.Contains(heldB, junction) }) {
				t.Fatalf("lanes %s at %v and %s at %v have a gap of %v m and no held junction: %v and %v",
					first.id, first.distances[a], second.id, second.distances[b], math.Sqrt(delta.dot(delta)), heldA, heldB)
			}
		}
	}
	return count
}

// TestTailValues pins the tail of four lanes of the study. The tail is the whole
// result of the walk, so a change of the margin of section 4.1 moves one of
// them.
func TestTailValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		to, control sim.Point
		tail        float64
	}{
		{"gentle arc", sim.Point{X: 200}, sim.Point{X: 100, Y: 40}, 20.5},
		{"hairpin chord 30", sim.Point{X: 30}, sim.Point{X: 15, Y: 200}, 94.25},
		{"hairpin chord 22", sim.Point{X: 22}, sim.Point{X: 11, Y: 320}, 270.806},
		{"narrow arc chord 10", sim.Point{X: 10}, sim.Point{X: 5, Y: 100}, 101.5},
	} {
		tail := sim.LaneTailsForTest(oneLaneNetwork(t, test.to, test.control))["curve"].Tail
		if math.Abs(tail-test.tail) > 0.001 {
			t.Errorf("%s: tail %v, want %v", test.name, tail, test.tail)
		}
	}
}

// TestValidateNetworkClearanceReadsTails checks that the validation of an
// editor check reads the tails of its index and does not build them again.
// The index of a straight lane hides the folded path of the same lane, which
// the zero index refuses.
func TestValidateNetworkClearanceReadsTails(t *testing.T) {
	t.Parallel()
	folded := oneLaneNetwork(t, sim.Point{X: 10}, sim.Point{X: 100, Y: 100})
	const refusal = "unsupported large-vehicle path shape"
	err := sim.ValidateNetworkClearance(folded, sim.LaneClearance{})
	if err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("zero index: %v", err)
	}
	straight := allClassNetwork(t, sim.Network{Nodes: folded.Nodes, Lanes: []sim.Lane{{ID: "curve", From: "a", To: "b", SpeedLimit: 14}}})
	err = sim.ValidateNetworkClearance(folded, sim.NewLaneClearance(straight))
	if err != nil && strings.Contains(err.Error(), refusal) {
		t.Fatalf("the validation built its own tails: %v", err)
	}
}

// TestTailRailHubCurves pins the facts of rail-hub. With all classes, each
// curved lane has a tail above 20 m and under 23 m, also with its control
// points nearer to the middle of the chord. With one large straight lane, the
// lanes at its nodes get a tail that fits them, and a lane of another station
// gets none.
func TestTailRailHubCurves(t *testing.T) {
	t.Parallel()
	for _, nearer := range []float64{0, 0.10, 0.25} {
		network := allClassNetwork(t, scenarios.RailHub().Network)
		nodes := certNodes(network)
		for index := range network.Lanes {
			lane := &network.Lanes[index]
			if lane.Control != nil {
				from, to := nodes[lane.From], nodes[lane.To]
				lane.Control = &sim.Point{X: lane.Control.X + nearer*((from.X+to.X)/2-lane.Control.X), Y: lane.Control.Y + nearer*((from.Y+to.Y)/2-lane.Control.Y)}
			}
		}
		tails := sim.LaneTailsForTest(network)
		for _, lane := range network.Lanes {
			if tail := tails[lane.ID].Tail; lane.Control != nil && (tail <= 20 || tail >= 23) {
				t.Errorf("%g nearer: lane %s has tail %v", nearer, lane.ID, tail)
			}
		}
		if got := network.LaneTailOverruns(); len(got) != 0 {
			t.Errorf("%g nearer: overruns = %+v", nearer, got)
		}
	}
	network := scenarios.RailHub().Network
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	for index := range network.Lanes {
		if network.Lanes[index].ID == "s01-through" {
			network.Lanes[index].VehicleClasses = classes
		}
	}
	tails := sim.LaneTailsForTest(network)
	for id, want := range map[string]bool{"s01-through": true, "s01-in-01": true, "s01-out-01": true, "s02-in-01": false, "s02-through": false} {
		if got := tails[id].Tail > 0; got != want {
			t.Errorf("lane %s has tail %v, want a tail %t", id, tails[id].Tail, want)
		}
	}
	if got := network.LaneTailOverruns(); len(got) != 0 {
		t.Errorf("overruns = %+v", got)
	}
}
