package sim

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	jsonv2 "encoding/json/v2"
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

// junctionConflictsOf returns the junction conflicts of a network, with
// the lateral merge pairs that PrepareNetwork builds. The network does not
// need to be valid.
func junctionConflictsOf(network Network) map[string][]laneConflict {
	positions := indexNodePositions(network.Nodes)
	maneuvers, _ := newManeuverIndex(network, positions)
	depots, _ := newDepotIndex(network, positions)
	return buildJunctionConflicts(network, newMergeLateral(network, maneuvers, depots))
}

// isotropicConflictsOf returns the junction conflicts of a network with
// the isotropic extent for each pair, as if the network had no lateral
// merge pair.
func isotropicConflictsOf(network Network) map[string][]laneConflict {
	return buildJunctionConflicts(network, nil)
}

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
				start, end := referenceConflictExtent(points[lane.ID], points[other.ID], Clearance)
				if !math.IsInf(start, 1) {
					conflicts[lane.ID] = append(conflicts[lane.ID], laneConflict{junction: node.ID, start: start, end: end})
				}
			}
		}
	}
	return conflicts
}

// referenceConflictExtent returns the isotropic extent of a lane against
// the other lane at a clearance.
func referenceConflictExtent(points, other []Point, clearance float64) (float64, float64) {
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
			if separation <= clearance+conflictSampleStep {
				at := distance + offset
				start, end = min(start, at), max(end, at)
			}
			if offset == length {
				break
			}
			step := max(conflictSampleStep, separation-clearance-conflictSampleStep)
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
// differs from the reference table. It checks the table with the isotropic
// extent for each pair, which each pair that is not lateral keeps.
func checkJunctionConflictsMatchReference(t *testing.T, network Network) {
	t.Helper()
	got := encodeJunctionConflicts(isotropicConflictsOf(network))
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
	skipLong(t)
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
// networks. Each network runs with the isotropic extent for each pair,
// against the isotropic reference, and with the lateral merge pairs,
// against the lateral merge reference.
func TestScenarioJunctionConflictsMatchReference(t *testing.T) {
	t.Parallel()
	skipLong(t)
	networks := scenarioNetworks(t)
	networks["far London"] = translateNetwork(networks["London"], Point{X: 3e7, Y: -2e7})
	for _, name := range slices.Sorted(maps.Keys(networks)) {
		for _, lateral := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s lateral %v", name, lateral), func(t *testing.T) {
				t.Parallel()
				network := networks[name]
				if !lateral {
					checkJunctionConflictsMatchReference(t, network)
					return
				}
				counts := checkMergeConflictsMatchReference(t, network)
				if counts.lateral == 0 {
					t.Fatal("the network has no lateral merge pair")
				}
				if strings.HasSuffix(name, "London") && counts.excluded == 0 {
					t.Fatal("clause 5 excludes no pair of the network")
				}
			})
		}
	}
}

// mergeReferenceStep is the sample step in meters of the lateral merge
// reference.
const mergeReferenceStep = 0.05

// mergeExpected is an extent of the lateral merge reference: the least and
// the greatest start and end that the table can have. An extent with
// exact set is an isotropic extent, and the table must have it bit for
// bit.
type mergeExpected struct {
	junction   string
	start, end [2]float64
	exact      bool
}

// mergeCounts are the numbers of the candidate pairs of a network, which
// meet clauses 1 to 4 of a lateral merge pair: the pairs that clause 5
// excludes and the pairs that it keeps. undecided names the pairs where
// the reference cannot tell.
type mergeCounts struct {
	excluded, lateral int
	undecided         []string
}

// referenceMergeConflicts returns the junction conflict table of a network.
// It decides clauses 1, 2, and 4 from the lanes, the maneuver groups, and
// the depots. Clause 3 does not apply, and each lane has the union of the
// bodies of the classes that it admits (mergeLaneShape). It decides
// clause 5 and the body range against the other lane sampled every
// mergeReferenceStep meters (mergeSide).
//
// A candidate pair keeps the isotropic extent when a rectangle that the
// sweep tests at the far end of either lane meets the other lane, and it
// is lateral when no such rectangle, grown by the sample step and the
// resolution of the sweep, meets it (mergeSide.reachesFar). A pair between
// these cases is undecided, and the reference gives no extent for it.
// The isotropic extent of a pair that is not lateral has the clearance of
// its lane classes: 12 m, or 20 m when a lane admits a large class.
//
// The extent of a lateral pair is the body range and the node range, with
// the sample pad. The sweep counts a meeting at a part of at most
// conflictSampleStep, from the start of the part when converging, or to
// its end otherwise. The reference bounds the far end of the body range
// of the sweep by two poses from the far end: inner, the first pose whose
// exact body meets the other lane, and outer, the first pose whose grown
// body meets it (mergeSide). The far end is at most inner, and at least
// outer less conflictSampleStep and less step/2, the distance from the
// meeting of the sweep to the nearest pose. Thus the tolerance comes from
// the sample step: the body grows by step plus the resolution of the
// sweep across the lane, and the far end can move by conflictSampleStep
// plus step/2 along it. Every other extent is the isotropic extent of
// referenceConflictExtent, bit for bit.
func referenceMergeConflicts(network Network, step float64) (map[string][]mergeExpected, mergeCounts) {
	positions := indexNodePositions(network.Nodes)
	maneuvers, _ := newManeuverIndex(network, positions)
	depots, _ := newDepotIndex(network, positions)
	group, express := string(GroupClass), string(ExpressClass)
	eligible := func(lane Lane) bool {
		return !maneuvers.groupLane(lane.ID) && !depots.depotLane(lane.ID)
	}
	candidate := func(node string, lane, other Lane) bool {
		if lane.From == lane.To || other.From == other.To ||
			lane.From == other.From && lane.To == other.To || lane.From == other.To && lane.To == other.From {
			return false
		}
		return (lane.To == node && other.To == node || lane.From == node && other.From == node) && eligible(lane) && eligible(other)
	}
	joins := newMergeJoins(network)
	points := make(map[string][]Point, len(network.Lanes))
	incident := make(map[string][]Lane)
	for _, lane := range network.Lanes {
		points[lane.ID] = network.Polyline(lane)
		incident[lane.From] = append(incident[lane.From], lane)
		incident[lane.To] = append(incident[lane.To], lane)
	}
	var counts mergeCounts
	// lateral holds each side of each lateral pair, by lane and other lane.
	lateral := make(map[[2]string]mergeExpected)
	decided := make(map[[2]string]bool)
	conflicts := make(map[string][]mergeExpected)
	for _, node := range network.Nodes {
		for _, lane := range incident[node.ID] {
			for _, other := range incident[node.ID] {
				if lane.ID == other.ID {
					continue
				}
				if candidate(node.ID, lane, other) && !decided[[2]string{lane.ID, other.ID}] {
					decided[[2]string{lane.ID, other.ID}], decided[[2]string{other.ID, lane.ID}] = true, true
					sides := [2]*mergeSide{newMergeSide(network, joins, lane, other, node.ID, step), newMergeSide(network, joins, other, lane, node.ID, step)}
					certain, possible := false, false
					for _, side := range sides {
						c, p := side.reachesFar()
						certain, possible = certain || c, possible || p
					}
					switch {
					case certain:
						counts.excluded++
					case possible:
						counts.undecided = append(counts.undecided, fmt.Sprintf("%s and %s at %s", lane.ID, other.ID, node.ID))
					default:
						counts.lateral++
					}
					if !possible {
						lateral[[2]string{lane.ID, other.ID}] = sides[0].expected(node.ID)
						lateral[[2]string{other.ID, lane.ID}] = sides[1].expected(node.ID)
					}
				}
				if expected, ok := lateral[[2]string{lane.ID, other.ID}]; ok {
					conflicts[lane.ID] = append(conflicts[lane.ID], expected)
					continue
				}
				clearance := Clearance
				if lane.VehicleClasses.Allows(group) || lane.VehicleClasses.Allows(express) || other.VehicleClasses.Allows(group) || other.VehicleClasses.Allows(express) {
					clearance = largeClearance
				}
				start, end := referenceConflictExtent(points[lane.ID], points[other.ID], clearance)
				if !math.IsInf(start, 1) {
					conflicts[lane.ID] = append(conflicts[lane.ID], mergeExpected{junction: node.ID, start: [2]float64{start, start}, end: [2]float64{end, end}, exact: true})
				}
			}
		}
	}
	return conflicts, counts
}

// expected returns the extent of the lane of a lateral pair at node.
func (s *mergeSide) expected(node string) mergeExpected {
	inner, outer := s.bodyRange()
	pad := conflictSampleStep + s.step/2
	if s.converging {
		return mergeExpected{
			junction: node,
			start:    [2]float64{max(0, min(outer-pad, s.length-Clearance)-conflictSampleStep), max(0, min(inner, s.length-Clearance)-conflictSampleStep)},
			end:      [2]float64{s.length, s.length},
		}
	}
	return mergeExpected{
		junction: node,
		end:      [2]float64{min(s.length, max(inner, Clearance)+conflictSampleStep), min(s.length, max(outer+pad, Clearance)+conflictSampleStep)},
	}
}

// checkMergeConflictsMatchReference compares the table of a network with
// referenceMergeConflicts. The bounds of a lateral
// extent have a slack of 1e-9 m for the rounding of the distances along a
// lane.
func checkMergeConflictsMatchReference(t *testing.T, network Network) mergeCounts {
	t.Helper()
	got := junctionConflictsOf(network)
	want, counts := referenceMergeConflicts(network, mergeReferenceStep)
	t.Logf("candidate pairs: %d excluded by clause 5, %d lateral", counts.excluded, counts.lateral)
	if len(counts.undecided) != 0 {
		t.Fatalf("the reference cannot decide clause 5 for %d pairs: %s", len(counts.undecided), strings.Join(counts.undecided, ", "))
	}
	const slack = 1e-9
	within := func(value float64, bounds [2]float64) bool {
		return value >= bounds[0]-slack && value <= bounds[1]+slack
	}
	failures := 0
	for _, lane := range slices.Sorted(maps.Keys(want)) {
		if len(got[lane]) != len(want[lane]) {
			t.Fatalf("lane %s has %d extents, want %d", lane, len(got[lane]), len(want[lane]))
		}
		for index, w := range want[lane] {
			g := got[lane][index]
			if g.junction != w.junction {
				t.Fatalf("lane %s extent %d at %s, want %s", lane, index, g.junction, w.junction)
			}
			var ok bool
			if w.exact {
				ok = math.Float64bits(g.start) == math.Float64bits(w.start[0]) && math.Float64bits(g.end) == math.Float64bits(w.end[0])
			} else {
				ok = within(g.start, w.start) && within(g.end, w.end)
			}
			if !ok {
				t.Errorf("lane %s extent %d at %s: [%v, %v], want start in %v and end in %v", lane, index, w.junction, g.start, g.end, w.start, w.end)
				failures++
				if failures == 10 {
					t.FailNow()
				}
			}
		}
	}
	if extra := len(got) - len(want); extra != 0 {
		t.Fatalf("the table has extents for %d lanes more than the reference", extra)
	}
	return counts
}

// scenarioNetworks returns the networks in
// testdata/scenario_networks.json.gz by name.
func scenarioNetworks(t *testing.T) map[string]Network {
	t.Helper()
	data, err := os.ReadFile("testdata/scenario_networks.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var networks map[string]Network
	if err := jsonv2.UnmarshalRead(reader, &networks, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	return networks
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
