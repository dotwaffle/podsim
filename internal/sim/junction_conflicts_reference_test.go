package sim

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// referenceJunctionConflicts is the junction conflict table before the
// segment bounds. It measures every sample against every segment.
func referenceJunctionConflicts(network Network) map[string][]laneConflict {
	conflicts := make(map[string][]laneConflict)
	points := make(map[string][]Point, len(network.Lanes))
	incident := make(map[string][]Lane)
	for _, lane := range network.Lanes {
		points[lane.ID] = network.lanePoints(lane, make([]Point, 0, 65))
		incident[lane.From] = append(incident[lane.From], lane)
		incident[lane.To] = append(incident[lane.To], lane)
	}
	for _, node := range network.Nodes {
		for _, lane := range incident[node.ID] {
			for _, other := range incident[node.ID] {
				if lane.ID == other.ID {
					continue
				}
				start, end := referenceConflictExtent(points[lane.ID], points[other.ID])
				if !math.IsInf(start, 1) {
					conflicts[lane.ID] = append(conflicts[lane.ID], laneConflict{junction: node.ID, start: start, end: end})
				}
			}
		}
	}
	return conflicts
}

func referenceConflictExtent(points, other []Point) (float64, float64) {
	start, end, distance := math.Inf(1), math.Inf(-1), 0.0
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		length := pointDistance(a, b)
		for offset := 0.0; ; {
			fraction := 0.0
			if length > 0 {
				fraction = offset / length
			}
			point := Point{X: a.X + fraction*(b.X-a.X), Y: a.Y + fraction*(b.Y-a.Y)}
			separation := referencePointToPolylineDistance(point, other)
			if separation <= Clearance+conflictSampleStep {
				at := distance + offset
				start, end = min(start, at), max(end, at)
			}
			if offset == length {
				break
			}
			step := max(conflictSampleStep, separation-Clearance-conflictSampleStep)
			offset = min(length, offset+step)
		}
		distance += length
	}
	return max(0, start-conflictSampleStep), min(distance, end+conflictSampleStep)
}

func referencePointToPolylineDistance(point Point, points []Point) float64 {
	best := math.Inf(1)
	for i := 1; i < len(points); i++ {
		start, end := points[i-1], points[i]
		dx, dy := end.X-start.X, end.Y-start.Y
		lengthSquared := dx*dx + dy*dy
		fraction := 0.0
		if lengthSquared > 0 {
			fraction = max(0, min(1, ((point.X-start.X)*dx+(point.Y-start.Y)*dy)/lengthSquared))
		}
		closest := Point{X: start.X + fraction*dx, Y: start.Y + fraction*dy}
		best = min(best, pointDistance(point, closest))
	}
	return best
}

// encodeJunctionConflicts writes one line for each conflict in lane order
// and then in table order, with each value as the exact bits of the float.
func encodeJunctionConflicts(conflicts map[string][]laneConflict) string {
	var b strings.Builder
	for _, lane := range slices.Sorted(maps.Keys(conflicts)) {
		for _, conflict := range conflicts[lane] {
			fmt.Fprintf(&b, "%s %s %s %s\n", lane, conflict.junction,
				strconv.FormatUint(math.Float64bits(conflict.start), 16), strconv.FormatUint(math.Float64bits(conflict.end), 16))
		}
	}
	return b.String()
}

// checkJunctionConflictsMatchReference fails the test at the first line that
// differs from the reference table.
func checkJunctionConflictsMatchReference(t *testing.T, network Network) {
	t.Helper()
	got := encodeJunctionConflicts(buildJunctionConflicts(network))
	want := encodeJunctionConflicts(referenceJunctionConflicts(network))
	if want == "" {
		t.Fatal("network has no junction conflicts")
	}
	if got == want {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range max(len(gotLines), len(wantLines)) {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("line %d: got %q, want %q", i, g, w)
		}
	}
}

func TestJunctionConflictsMatchReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		network Network
	}{
		{name: "example", network: Example()},
		{name: "ladder", network: ladderNetwork()},
		{name: "far example", network: translateNetwork(Example(), Point{X: 3e7, Y: -2e7})},
		{name: "huge example", network: translateNetwork(Example(), Point{X: 1e12, Y: 1e12})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkJunctionConflictsMatchReference(t, tc.network)
		})
	}
	for seed := range uint64(12) {
		t.Run(fmt.Sprintf("fan %d", seed), func(t *testing.T) {
			t.Parallel()
			origins := []Point{{}, {X: 5e4, Y: -3e4}, {X: -4e6, Y: 7e6}, {X: 2e9, Y: 1e9}}
			checkJunctionConflictsMatchReference(t, fanNetwork(seed, origins[seed%uint64(len(origins))]))
		})
	}
}

// TestScenarioJunctionConflictsMatchReference compares the table on each
// preset network and on rings with many berths, where many long lanes meet
// at one node. TestWriteConflictFixture in package scenarios writes the
// networks.
func TestScenarioJunctionConflictsMatchReference(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/scenario_networks.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var networks map[string]Network
	if err := json.NewDecoder(reader).Decode(&networks); err != nil {
		t.Fatal(err)
	}
	networks["far London"] = translateNetwork(networks["London"], Point{X: 3e7, Y: -2e7})
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			checkJunctionConflictsMatchReference(t, networks[name])
		})
	}
}

func TestConflictSeparationKeepsCloseValues(t *testing.T) {
	t.Parallel()
	// A curve and a point path that stays near it, with joints close to the
	// path, so the nearest segment changes often.
	rng := rand.New(rand.NewPCG(7, 11))
	for range 200 {
		points := make([]Point, 0, 65)
		origin := Point{X: rng.Float64()*1e5 - 5e4, Y: rng.Float64()*1e5 - 5e4}
		control := Point{X: origin.X + rng.Float64()*400 - 200, Y: origin.Y + rng.Float64()*400 - 200}
		end := Point{X: origin.X + rng.Float64()*400 - 200, Y: origin.Y + rng.Float64()*400 - 200}
		for i := range 65 {
			u := float64(i) / 64
			v := 1 - u
			points = append(points, Point{X: v*v*origin.X + 2*u*v*control.X + u*u*end.X, Y: v*v*origin.Y + 2*u*v*control.Y + u*u*end.Y})
		}
		polyline := newConflictPolyline(points)
		scale := polyline.scale + 500
		for i := range 400 {
			base := points[i%64]
			point := Point{X: base.X + rng.Float64()*60 - 30, Y: base.Y + rng.Float64()*60 - 30}
			want := referencePointToPolylineDistance(point, points)
			got, _ := polyline.separation(point, Clearance+conflictSampleStep, scale)
			if want <= Clearance+conflictSampleStep {
				if got > Clearance+conflictSampleStep {
					t.Fatalf("separation %v, want at most the limit for %v", got, want)
				}
				continue
			}
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("separation %v, want %v", got, want)
			}
		}
	}
}

// translateNetwork moves each node and control point of the network.
func translateNetwork(network Network, by Point) Network {
	network.Nodes = slices.Clone(network.Nodes)
	for i := range network.Nodes {
		network.Nodes[i].Position = Point{X: network.Nodes[i].Position.X + by.X, Y: network.Nodes[i].Position.Y + by.Y}
	}
	network.Lanes = slices.Clone(network.Lanes)
	for i, lane := range network.Lanes {
		if lane.Control != nil {
			network.Lanes[i].Control = &Point{X: lane.Control.X + by.X, Y: lane.Control.Y + by.Y}
		}
	}
	return network
}

// fanNetwork returns a random fan of lanes at one hub near origin. Some
// lanes run close together for a long way, some are short, and some have
// a zero length.
func fanNetwork(seed uint64, origin Point) Network {
	rng := rand.New(rand.NewPCG(seed, 99))
	network := Network{Nodes: []Node{{ID: "hub", Position: origin}}}
	at := func(x, y float64) Point { return Point{X: origin.X + x, Y: origin.Y + y} }
	lanes := 20 + rng.IntN(30)
	for i := range lanes {
		id := fmt.Sprintf("n%02d", i)
		angle := rng.Float64() * 2 * math.Pi
		radius := 5 + rng.Float64()*3000
		if i%7 == 0 {
			radius = 0
		}
		network.Nodes = append(network.Nodes, Node{ID: id, Position: at(radius*math.Cos(angle), radius*math.Sin(angle))})
		lane := Lane{ID: "l" + id, From: "hub", To: id}
		if rng.IntN(2) == 0 {
			lane.From, lane.To = id, "hub"
		}
		switch i % 3 {
		case 0:
			// A family of curves with close controls, like station berths.
			family := float64(i % 9)
			lane.Control = new(at(-200-20*family, 100+7*family))
			network.Nodes[len(network.Nodes)-1].Position = at(300+15*family, 50+10*family)
		case 1:
			lane.Control = new(at(rng.Float64()*4000-2000, rng.Float64()*4000-2000))
		}
		network.Lanes = append(network.Lanes, lane)
	}
	return network
}

func TestLastSampleMatchesSteps(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(3, 5))
	offsets := []float64{0, 1e-300, 0.1, 0.3, 0.5, 0.7, 1, 1.25, 3.9, 1023.75, 1023.9, 4095.6, 1e6 + 0.1}
	for range 2000 {
		offsets = append(offsets, rng.Float64()*math.Ldexp(1, rng.IntN(20)))
	}
	for _, offset := range offsets {
		for _, length := range []float64{offset, offset + 0.2, offset + 7.3, offset + 1000.01, offset*3 + 5000} {
			for _, cleared := range []float64{offset, offset + 0.5, offset + rng.Float64()*(length-offset), length} {
				want := offset
				for want != length {
					next := min(length, want+conflictSampleStep)
					if next > cleared {
						break
					}
					want = next
				}
				if got := lastSample(offset, cleared, length); math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("lastSample(%v, %v, %v) = %v, want %v", offset, cleared, length, got, want)
				}
			}
		}
	}
}
