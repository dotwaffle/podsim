package sim

import (
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// envelopeReachCase is a network of the envelope reach test. lanes selects
// the lanes whose routes the test drives. routes is the number of sampled
// free-flow routes between berths that the test also drives. They can
// hold a lane with a large front bound far from the position.
type envelopeReachCase struct {
	network Network
	lanes   func(index int, lane Lane, tails geometryTails) bool
	routes  int
}

// envelopeReachStats counts the positions that the test checks, and the
// planes that the reach holds and the envelope does not.
type envelopeReachStats struct {
	positions, berths, extra, largest int
}

// envelopeReachProbe drives appendVehicleSafetyLocations on a prepared
// network and checks each plane against an envelope reach.
type envelopeReachProbe struct {
	s       *Simulation
	reach   *envelopeReach
	lengths []float64
	// berths holds the berth at each node.
	berths             map[string]Berth
	incoming, outgoing map[string][]int
	// behind and ahead are the longest tail or origin tail, and the
	// largest front bound, of the network.
	behind, ahead float64
	stats         envelopeReachStats
	name          string
	// maxRoutes and maxDepth bound the windows of a lane.
	maxRoutes, maxDepth int
	failures            int
}

func newEnvelopeReachProbe(t *testing.T, name string, n Network) *envelopeReachProbe {
	t.Helper()
	// The test drives the envelope on networks that the plane reach of the
	// audit refuses today, so it holds that refusal.
	prepared, err := prepareNetworkTails(n, nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	p := newPreparedNetwork(prepared)
	probe := &envelopeReachProbe{
		s: &Simulation{networkIndexes: &p.networkIndexes}, reach: newEnvelopeReach(p.network, p.tails),
		berths: make(map[string]Berth), incoming: make(map[string][]int), outgoing: make(map[string][]int),
		name: name, maxRoutes: 48, maxDepth: 8,
	}
	// The windows of the routes are independent of the walks of the index:
	// the longest tail and origin tail behind, the largest front ahead.
	probe.behind, probe.ahead = largeClearance, largeEnvelopeRadius
	for index, lane := range p.network.Lanes {
		probe.lengths = append(probe.lengths, p.network.Length(lane))
		probe.incoming[lane.To] = append(probe.incoming[lane.To], index)
		probe.outgoing[lane.From] = append(probe.outgoing[lane.From], index)
		bounds := p.tails[lane.ID]
		probe.behind = max(probe.behind, bounds.tail, bounds.fromTail)
		probe.ahead = max(probe.ahead, bounds.front, p.laneCells[lane.ID].front)
	}
	for _, station := range p.network.Stations {
		for _, berth := range station.Berths {
			probe.berths[berth.Node] = berth
		}
	}
	return probe
}

// windows returns the routes through a lane, as lane indexes, and the
// route index of the lane in each. A route starts with the lanes into the
// lane, as far back as a lane can hold its tail or an origin, and ends with
// the lanes out of it, as far ahead as the front bound reaches.
func (p *envelopeReachProbe) windows(lane int, rng *rand.Rand) ([][]int, []int) {
	lanes := p.s.network.Lanes
	var prefixes, suffixes [][]int
	var back func(prefix []int, gap float64)
	back = func(prefix []int, gap float64) {
		prefixes = append(prefixes, prefix)
		if len(prefix) == p.maxDepth {
			return
		}
		first := lane
		if len(prefix) > 0 {
			first, gap = prefix[0], gap+p.lengths[prefix[0]]
		}
		if gap > p.behind+1 {
			return
		}
		for _, previous := range p.incoming[lanes[first].From] {
			back(append([]int{previous}, prefix...), gap)
		}
	}
	var forward func(suffix []int, gap float64)
	forward = func(suffix []int, gap float64) {
		suffixes = append(suffixes, suffix)
		if len(suffix) == p.maxDepth {
			return
		}
		last := lane
		if len(suffix) > 0 {
			last, gap = suffix[len(suffix)-1], gap+p.lengths[suffix[len(suffix)-1]]
		}
		if gap > p.ahead+1 {
			return
		}
		for _, next := range p.outgoing[lanes[last].To] {
			forward(append(slices.Clone(suffix), next), gap)
		}
	}
	back(nil, 0)
	forward(nil, 0)
	type pair struct{ prefix, suffix int }
	pairs := make([]pair, 0, len(prefixes)*len(suffixes))
	for prefix := range prefixes {
		for suffix := range suffixes {
			pairs = append(pairs, pair{prefix, suffix})
		}
	}
	if len(pairs) > p.maxRoutes {
		rng.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })
		pairs = pairs[:p.maxRoutes]
	}
	routes, at := make([][]int, 0, len(pairs)), make([]int, 0, len(pairs))
	for _, pair := range pairs {
		route := append(slices.Clone(prefixes[pair.prefix]), lane)
		routes = append(routes, append(route, suffixes[pair.suffix]...))
		at = append(at, len(prefixes[pair.prefix]))
	}
	return routes, at
}

// positions returns the positions on the lane at a route index that the
// test checks: both ends, the first and last 20 m, interior points, and
// the positions at which the oracle adds or drops a plane, in the
// arithmetic of the route.
func (p *envelopeReachProbe) positions(v *vehicle, at int) []float64 {
	blocks := v.blocks.lanes
	start, length := blocks[at].start, blocks[at+1].start-blocks[at].start
	positions := []float64{0, 0.5, length / 3, length / 2, 2 * length / 3, length - 0.5, length}
	for step := 1.0; step <= 20; step += 2 {
		positions = append(positions, step, length-step)
	}
	front := largeEnvelopeRadius
	fronts := []float64{largeEnvelopeRadius}
	for _, entry := range blocks {
		if entry.cells != nil {
			front = max(front, entry.cells.front)
			fronts = append(fronts, entry.cells.front)
		}
	}
	for index := range v.Route {
		positions = append(positions, blocks[index+1].start+max(largeClearance, blocks[index].cells.tail)-start, blocks[index].start-front-start)
		// The local front of a network with ShapeClearance is one of the
		// front bounds of the route.
		for _, local := range fronts {
			positions = append(positions, blocks[index].start-local-start)
		}
	}
	positions = append(positions, v.originTail()-start)
	kept := positions[:0]
	for _, position := range positions {
		if position >= 0 && position <= length {
			kept = append(kept, position)
		}
	}
	slices.Sort(kept)
	return slices.Compact(kept)
}

// check drives a large pod on a route, at each position of the lane at a
// route index, and requires each plane of its envelope in the reach. After
// a restore, the route distance and the published position can each
// differ by up to two restoreTolerance steps, so it checks each shift of
// both. Without ShapeClearance, the envelope reads the route distance, and
// with it, the published position.
func (p *envelopeReachProbe) check(t *testing.T, route []int, at int) {
	t.Helper()
	lanes := make([]Lane, len(route))
	for index, lane := range route {
		lanes[index] = p.s.network.Lanes[lane]
	}
	v := &vehicle{Pod: Pod{ID: "probe", Class: GroupClass, Activity: Traveling, LaneID: lanes[at].ID}}
	p.s.setVehicleRoute(v, lanes)
	v.origin = p.berths[lanes[0].From]
	shifts := []float64{0, -2 * restoreTolerance, 2 * restoreTolerance}
	for _, position := range p.positions(v, at) {
		v.distance = v.blocks.lanes[at].start + position
		v.originReleased = v.origin.ID == "" || v.distance >= v.originTail()
		for _, moved := range shifts {
			for _, shift := range shifts {
				published := min(p.lengths[route[at]], max(0, position+shift))
				v.Pod.LaneDistance = published
				got := p.s.appendVehicleSafetyLocations(nil, v, v.distance+moved)
				held := p.spanLocations(p.reach.laneAt(route[at], published))
				for _, location := range got {
					if !slices.Contains(held, location) {
						p.fail(t, fmt.Sprintf("lane %q at %g m (route distance moved %g m, published %g m), origin released %v, route %s: plane %+v is not in the reach %s", lanes[at].ID, position, moved, published, v.originReleased, strings.Join(routeIDs(lanes), " "), location, planesText(held)))
						return
					}
				}
				if shift == 0 && moved == 0 {
					p.count(len(held) - len(got))
				}
			}
		}
	}
}

func (p *envelopeReachProbe) fail(t *testing.T, text string) {
	t.Helper()
	p.failures++
	if p.failures <= 3 {
		t.Errorf("%s: %s", p.name, text)
	}
}

func (p *envelopeReachProbe) count(extra int) {
	p.stats.positions++
	p.stats.extra += extra
	p.stats.largest = max(p.stats.largest, extra)
}

// spanLocations returns the planes of a span, with the planes of rule 2.
func (p *envelopeReachProbe) spanLocations(span *reachSpan) []SafetyLocation {
	held := make([]SafetyLocation, 0, len(span.locations))
	for _, number := range p.reach.closure(span.locations) {
		if location := p.reach.locations[number]; !slices.Contains(held, location) {
			held = append(held, location)
		}
	}
	return held
}

// checkSeparation requires the plane test of pairs of spans to equal
// envelopeLocationsSeparated on their planes. It builds the spans of each
// lane, and checks count random pairs, or each pair when there are fewer.
func (p *envelopeReachProbe) checkSeparation(t *testing.T, count int, rng *rand.Rand) {
	t.Helper()
	var spans []*reachSpan
	for lane := range p.reach.lanes {
		lane := p.reach.laneSpans(lane)
		for index := range lane {
			spans = append(spans, &lane[index])
		}
	}
	for index := range p.reach.berths {
		spans = append(spans, &p.reach.berths[index])
	}
	compare := func(first, second *reachSpan) {
		a, b := p.spanLocations(first), p.spanLocations(second)
		if got, want := first.separated(second), envelopeLocationsSeparated(a, b); got != want {
			p.fail(t, fmt.Sprintf("spans %s and %s: separated %v, planes separated %v", planesText(a), planesText(b), got, want))
		}
	}
	if len(spans)*len(spans) <= count {
		for _, first := range spans {
			for _, second := range spans {
				compare(first, second)
			}
		}
		return
	}
	for range count {
		compare(spans[rng.IntN(len(spans))], spans[rng.IntN(len(spans))])
	}
}

// planesText returns the first planes of a reach and their count.
func planesText(planes []SafetyLocation) string {
	if len(planes) <= 4 {
		return fmt.Sprintf("%+v", planes)
	}
	return fmt.Sprintf("%+v and %d more", planes[:4], len(planes)-4)
}

// checkBerths requires the envelope of a pod at each berth in the reach of
// the berth.
func (p *envelopeReachProbe) checkBerths(t *testing.T) {
	t.Helper()
	index := 0
	for _, station := range p.s.network.Stations {
		for _, berth := range station.Berths {
			v := &vehicle{Pod: Pod{ID: "probe", Class: GroupClass, Activity: Idle, StationID: station.ID, BerthID: berth.ID}}
			got := p.s.appendVehicleSafetyLocations(nil, v, 0)
			held := p.spanLocations(&p.reach.berths[index])
			for _, location := range got {
				if !slices.Contains(held, location) {
					p.fail(t, fmt.Sprintf("berth %q: plane %+v is not in the reach %s", berth.ID, location, planesText(held)))
				}
			}
			p.stats.berths++
			index++
		}
	}
}

// checkSampledRoutes drives free-flow routes between random berths, and
// returns the number of routes that it drove.
func (p *envelopeReachProbe) checkSampledRoutes(t *testing.T, count int, rng *rand.Rand) int {
	t.Helper()
	driven := 0
	var nodes []string
	for node := range p.berths {
		nodes = append(nodes, node)
	}
	slices.Sort(nodes)
	if len(nodes) < 2 {
		return 0
	}
	for range count {
		from, to := nodes[rng.IntN(len(nodes))], nodes[rng.IntN(len(nodes))]
		lanes, err := p.s.network.route(networkRouteInput{from: from, to: to})
		if err != nil || len(lanes) == 0 {
			continue
		}
		route := make([]int, len(lanes))
		for index, lane := range lanes {
			route[index] = p.s.graph.lanes[lane.ID]
		}
		for at := range route {
			p.check(t, route, at)
		}
		driven++
	}
	return driven
}

// run checks the lanes that the case selects, the berths, and the sampled
// routes of a case.
func (p *envelopeReachProbe) run(t *testing.T, c envelopeReachCase) {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 17))
	for index, lane := range p.s.network.Lanes {
		if c.lanes != nil && !c.lanes(index, lane, p.s.tails[lane.ID]) {
			continue
		}
		routes, at := p.windows(index, rng)
		for route := range routes {
			p.check(t, routes[route], at[route])
		}
	}
	p.checkBerths(t)
	driven := p.checkSampledRoutes(t, c.routes, rng)
	if c.routes > 0 && driven == 0 {
		t.Errorf("%s: no sampled route", p.name)
	}
	p.checkSeparation(t, 4000, rng)
	if p.failures > 3 {
		t.Errorf("%s: %d more failures", p.name, p.failures-3)
	}
	t.Logf("%s: %d sampled routes, %d positions and %d berths, front bound %g m, longest tail or origin tail %g m, reach holds %.2f planes more than the envelope on average, %d at most",
		p.name, driven, p.stats.positions, p.stats.berths, p.ahead, p.behind, float64(p.stats.extra)/float64(max(1, p.stats.positions)), p.stats.largest)
}

// envelopeReachCurves is a network for the envelope reach test. A berth
// starts a route of a straight large lane, a sharply curved large lane
// whose tail and front bound are longer than the two short lanes after it,
// and a long lane. Each lane and the berth have their own plane.
func envelopeReachCurves(t *testing.T) Network {
	t.Helper()
	large := largeGeometryClasses(t, "legacy", "group")
	lane := func(id, from, to string, classes ClassSet, control *Point) Lane {
		return Lane{ID: id, From: from, To: to, SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: "g-" + id, Control: control}
	}
	return Network{
		Nodes: []Node{
			{ID: "entry", Position: Point{Y: -80}}, {ID: "berth"}, {ID: "a", Position: Point{X: 50}}, {ID: "b", Position: Point{X: 90}},
			{ID: "c", Position: Point{X: 114}}, {ID: "d", Position: Point{X: 138}}, {ID: "e", Position: Point{X: 238}}, {ID: "exit", Position: Point{X: 238, Y: -80}},
		},
		Lanes: []Lane{
			lane("arrive", "entry", "berth", 0, nil),
			lane("out", "berth", "a", large, nil),
			lane("bend", "a", "b", large, &Point{X: 70, Y: 100}),
			lane("short", "b", "c", 0, nil),
			lane("short2", "c", "d", 0, nil),
			lane("far", "d", "e", 0, nil),
			lane("leave", "e", "exit", 0, nil),
			lane("through", "entry", "exit", 0, nil),
		},
		Stations: []Station{{ID: "station", Entry: "entry", Exit: "exit", VehicleClasses: large, Berths: []Berth{{ID: "berth", Node: "berth", VehicleClasses: large, SeparationGroup: "g-berth"}}}},
	}
}

// TestEnvelopeReachHoldsEnvelope is the envelope reach test of section 7 of
// the shape clearance design: for each lane of a test network and positions
// along it, the planes of appendVehicleSafetyLocations for a large pod on
// each route through the lane are in the envelope reach at that position.
// The routes are the windows of each lane, and on the large networks a
// sample of the lanes and of free-flow routes between berths. A pod at each
// berth has its envelope in the reach of the berth.
func TestEnvelopeReachHoldsEnvelope(t *testing.T) {
	t.Parallel()
	express := expressNetworkFixture(t)
	refused := gateOffRefusedNetworks(t, express)
	// A large network checks the lanes with a geometry tail and each
	// seventh lane, or each 29th lane with -short.
	every := 7
	if testing.Short() {
		every = 29
	}
	sampled := func(index int, _ Lane, tails geometryTails) bool {
		return tails != (geometryTails{}) || index%every == 0
	}
	cases := map[string]envelopeReachCase{
		"curves":              {network: envelopeReachCurves(t)},
		"london-full-express": {network: express, lanes: sampled, routes: 12},
		"london-full-express, Express on the Waterloo and Bank routes": {network: refused["london-full-express, Express on the Waterloo and Bank routes"], lanes: sampled, routes: 12},
	}
	for name, network := range scenarioNetworks(t) {
		c := envelopeReachCase{network: network, routes: 12}
		if len(network.Lanes) > 1000 {
			c.lanes = sampled
		}
		cases[name] = c
	}
	// With ShapeClearance, the oracle takes the local front and the
	// small-pod rules, and the index the reach bound of each lane.
	for name, c := range maps.Clone(cases) {
		c.network = withShapeClearance(c.network, true)
		cases[name+", shapeClearance"] = c
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe := newEnvelopeReachProbe(t, name, c.network)
			probe.run(t, c)
		})
	}
}

// TestEnvelopeReachCurves checks the cases of the curves network that the
// envelope reach test must cover, and that the reach is by position.
func TestEnvelopeReachCurves(t *testing.T) {
	t.Parallel()
	probe := newEnvelopeReachProbe(t, "curves", envelopeReachCurves(t))
	tails := probe.s.tails
	index := func(id string) int { return probe.s.graph.lanes[id] }
	bend, short, short2 := tails["bend"], index("short"), index("short2")
	// The curved lane has a tail longer than the next lane, and a front
	// bound longer than the lane after that.
	if bend.tail <= probe.lengths[short] || bend.front <= probe.lengths[short2] {
		t.Fatalf("curved lane tail %g m and front %g m, next lanes %g m and %g m", bend.tail, bend.front, probe.lengths[short], probe.lengths[short2])
	}
	t.Logf("curved lane: length %g m, tail %g m, front %g m", probe.lengths[index("bend")], bend.tail, bend.front)
	for _, tc := range []struct {
		name  string
		route []string
		lane  string
	}{
		{"origin before and after release", []string{"out", "bend"}, "out"},
		{"two lanes back", []string{"out", "bend", "short", "short2"}, "short2"},
		{"two lanes ahead from the front bound of the lane", []string{"bend", "short", "short2", "far"}, "bend"},
		{"two lanes ahead from the front bound of a lane behind", []string{"bend", "short", "short2", "far"}, "short"},
	} {
		route := make([]int, len(tc.route))
		for at, id := range tc.route {
			route[at] = index(id)
		}
		probe.name = "curves, " + tc.name
		probe.check(t, route, slices.Index(tc.route, tc.lane))
	}
	probe.checkBerths(t)
	// The middle of the long lane holds only its own plane.
	far := index("far")
	middle := probe.reach.laneAt(far, probe.lengths[far]/2)
	if want := []SafetyLocation{probe.reach.locations[far]}; !slices.Equal(probe.spanLocations(middle), want) {
		t.Fatalf("the middle of lane far holds %+v", probe.spanLocations(middle))
	}
}

// eagerReachSpan is a span of eagerReachSpans.
type eagerReachSpan struct {
	from, to  float64
	locations []int
	part      reachPart
}

// eagerReachSpans is reachSpans before the spans of a lane shared one
// array (commit 29af1248), with the plane test of reachPart. It is the
// reference of TestEnvelopeReachLazySpans.
func eagerReachSpans(items []reachItem, length float64, locations []SafetyLocation, members *eagerReachMembers) []eagerReachSpan {
	cuts := []float64{0}
	for _, item := range items {
		for _, bound := range []float64{item.from, item.to} {
			if bound > 0 && bound < length {
				cuts = append(cuts, bound)
			}
		}
	}
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	spans := make([]eagerReachSpan, 0, len(cuts))
	for index, from := range cuts {
		to := length
		if index+1 < len(cuts) {
			to = cuts[index+1]
		}
		var numbers []int
		for _, item := range items {
			if item.from < to && item.to > from {
				numbers = append(numbers, item.location)
			}
		}
		slices.Sort(numbers)
		numbers = slices.Clip(slices.Compact(numbers))
		if last := len(spans) - 1; last >= 0 && slices.Equal(spans[last].locations, numbers) {
			spans[last].to = to
			continue
		}
		spans = append(spans, eagerReachSpan{from: from, to: to, locations: numbers, part: members.part(numbers, locations)})
	}
	return spans
}

// eagerReachMembers numbers the members of eagerReachSpans when they first
// occur, as the reach of commit 29af1248 did.
type eagerReachMembers struct {
	numbers map[reachMember]int
}

func (m *eagerReachMembers) number(member reachMember) int {
	value, found := m.numbers[member]
	if !found {
		value = len(m.numbers)
		m.numbers[member] = value
	}
	return value
}

func (m *eagerReachMembers) part(numbers []int, locations []SafetyLocation) reachPart {
	var part reachPart
	for _, number := range numbers {
		location := locations[number]
		part.blank = part.blank || location.SeparationGroup == ""
		part.members = append(part.members, m.number(reachMember{location.SeparationGroup, true}))
		for _, node := range []string{location.From, location.To} {
			// A location with no node shares no node with another.
			if node != "" {
				part.members = append(part.members, m.number(reachMember{node, false}))
			}
		}
	}
	return part
}

// eagerSeparated is reachSpan.separated of commit 29af1248.
func (s *eagerReachSpan) separated(other *eagerReachSpan) bool {
	return !s.part.blank && !other.part.blank && !s.part.overlaps(&other.part)
}

// setMembers returns the members of a set in order.
func (s *reachSet) setMembers() []int {
	if s.bits == nil {
		return s.members
	}
	var members []int
	for word, bits := range s.bits {
		for bit := range 64 {
			if bits&(1<<bit) != 0 {
				members = append(members, word*64+bit)
			}
		}
	}
	return members
}

// TestEnvelopeReachLazySpans checks that the spans that laneSpans builds
// for a lane, when the audit first asks for it, equal the spans of an
// eager build of commit 29af1248, which built each lane in lane order. The
// test asks for the lanes in a random order, so that the storage that the
// builds share cannot carry one lane into the next. For each span, it
// compares the bounds, the planes, the groups and the nodes of the planes,
// and the plane test of random pairs of spans with the plane test of
// reachPart. It checks each lane, except on the network with Express on
// all lanes of Bank. There, the eager build of a lane takes milliseconds,
// because the front bound of 3,491 m gives a lane up to hundreds of spans.
// Of the lanes that admit a large class, the lanes with a geometry tail,
// and each seventh lane, the test checks each fifth, or each 15th with
// -short, and the lane with the largest front bound.
func TestEnvelopeReachLazySpans(t *testing.T) {
	t.Parallel()
	express := expressNetworkFixture(t)
	networks := gateOffRefusedNetworks(t, express)
	networks["london-full-express"] = express
	networks["curves"] = envelopeReachCurves(t)
	maps.Copy(networks, scenarioNetworks(t))
	every := 5
	if testing.Short() {
		every = 15
	}
	for name, network := range networks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tails := indexGeometryTails(network)
			var lanes []int
			front := 0
			for index, lane := range network.Lanes {
				lanes = append(lanes, index)
				if tails[lane.ID].front > tails[network.Lanes[front].ID].front {
					front = index
				}
			}
			if name == "london-full-express, Express on all lanes of Bank" {
				// Keep the lanes that admit a large class or have a
				// geometry tail, and each seventh lane. Of those, keep each
				// every-th lane, and the lane with the largest front bound.
				kept, candidates := []int{front}, 0
				for _, lane := range lanes {
					if !largeClassSet(network.Lanes[lane].VehicleClasses) && tails[network.Lanes[lane].ID] == (geometryTails{}) && lane%7 != 0 {
						continue
					}
					if candidates%every == 0 && lane != front {
						kept = append(kept, lane)
					}
					candidates++
				}
				lanes = kept
			}
			// The eager build of commit 29af1248, in lane order.
			g := newReachGraph(network, tails)
			var locations []SafetyLocation
			for _, lane := range network.Lanes {
				locations = append(locations, SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To})
			}
			for _, station := range network.Stations {
				for _, berth := range station.Berths {
					locations = append(locations, SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node})
				}
			}
			members := &eagerReachMembers{numbers: make(map[reachMember]int)}
			var walk reachWalk
			var items []reachItem
			want := make(map[int][]eagerReachSpan, len(lanes))
			for _, lane := range lanes {
				items = g.laneItems(&walk, lane, items[:0])
				want[lane] = eagerReachSpans(items, g.lengths[lane], locations, members)
			}
			var wantBerths []eagerReachSpan
			for _, node := range g.berthNode {
				items = g.nodeItems(items[:0], node, math.Inf(1))
				wantBerths = append(wantBerths, eagerReachSpans(items, 0, locations, members)...)
			}
			for _, spans := range want {
				for index := range spans {
					spans[index].part.seal(len(members.numbers))
				}
			}
			for index := range wantBerths {
				wantBerths[index].part.seal(len(members.numbers))
			}

			reach := newEnvelopeReach(network, tails)
			if !slices.Equal(reach.locations, locations) {
				t.Fatal("the locations differ")
			}
			rng := rand.New(rand.NewPCG(3, 29))
			order := slices.Clone(lanes)
			rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			type pair struct {
				got  *reachSpan
				want *eagerReachSpan
			}
			var pairs []pair
			large := 0
			compare := func(what string, got []reachSpan, want []eagerReachSpan) {
				t.Helper()
				if len(got) != len(want) {
					t.Fatalf("%s: %d spans, want %d", what, len(got), len(want))
				}
				for index := range got {
					g, w := &got[index], &want[index]
					locations := slices.Compact(slices.Sorted(slices.Values(g.locations)))
					if g.from != w.from || g.to != w.to || !slices.Equal(locations, w.locations) || g.planes.blank != w.part.blank {
						t.Fatalf("%s span %d: [%g, %g) locations %v blank %v, want [%g, %g) locations %v blank %v", what, index, g.from, g.to, locations, g.planes.blank, w.from, w.to, w.locations, w.part.blank)
					}
					var planes []int
					for _, number := range locations {
						planes = append(planes, reach.members.of[number]...)
					}
					slices.Sort(planes)
					planes = slices.Compact(planes)
					if members := g.planes.setMembers(); !slices.Equal(members, planes) || (g.planes.bits != nil) != (len(planes) > reachBitsMembers) {
						t.Fatalf("%s span %d: members %v (bits %v), want %v", what, index, members, g.planes.bits != nil, planes)
					}
					if g.planes.bits != nil {
						large++
					}
					pairs = append(pairs, pair{g, w})
				}
			}
			for _, lane := range order {
				spans := reach.laneSpans(lane)
				compare(fmt.Sprintf("lane %q", network.Lanes[lane].ID), spans, want[lane])
				if again := reach.laneSpans(lane); &again[0] != &spans[0] {
					t.Fatalf("lane %q: laneSpans builds the lane again", network.Lanes[lane].ID)
				}
				if middle := g.lengths[lane] / 2; reach.laneAt(lane, middle) != &spans[slices.IndexFunc(want[lane], func(span eagerReachSpan) bool { return span.to > middle || span.to == g.lengths[lane] })] {
					t.Fatalf("lane %q: laneAt returns another span", network.Lanes[lane].ID)
				}
			}
			if reach.built != len(lanes) {
				t.Fatalf("built %d lanes, asked for %d", reach.built, len(lanes))
			}
			compare("berths", reach.berths, wantBerths)
			separated := 0
			for range 20000 {
				a, b := pairs[rng.IntN(len(pairs))], pairs[rng.IntN(len(pairs))]
				got, want := a.got.separated(b.got), a.want.separated(b.want)
				if got != want {
					t.Fatalf("spans [%g, %g) and [%g, %g): separated %v, want %v", a.got.from, a.got.to, b.got.from, b.got.to, got, want)
				}
				if got {
					separated++
				}
			}
			t.Logf("%d of %d lanes, %d spans, %d with bits, %d of 20000 random pairs separated", len(lanes), len(network.Lanes), len(pairs), large, separated)
		})
	}
}
