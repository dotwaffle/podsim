package scenarios

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// londonStar returns the carriageways of a station "hub" at the origin with
// a link of each length in each direction, in degrees, and the junction
// input of the station. The station direction is the middle of the widest
// free angle, as in the presets.
func londonStar(lengths, degrees []float64) (sim.Network, londonJunctionInput) {
	var network sim.Network
	portals := map[string]*londonStationPortals{"hub": {}}
	input := londonJunctionInput{id: "hub", name: "Hub"}
	var neighbors []sim.Point
	for index, length := range lengths {
		id := fmt.Sprintf("n%d", index+1)
		portals[id] = &londonStationPortals{}
		angle := degrees[index] * math.Pi / 180
		neighbor := sim.Point{X: length * math.Cos(angle), Y: length * math.Sin(angle)}
		neighbors = append(neighbors, neighbor)
		addLondonCarriageway(&network, portals, index, londonSourceLink{A: "hub", B: id, Lines: []string{"test"}}, sim.Point{}, neighbor)
		input.directions = append(input.directions, angle)
	}
	input.portals, input.lanes = *portals["hub"], network.Lanes
	input.slips = []float64{londonStationPreferences(sim.Point{}, neighbors)[0].direction}
	return network, input
}

// regularLondonStar returns a star of n links of the length at equal
// angles.
func regularLondonStar(n int, length float64) (sim.Network, londonJunctionInput) {
	lengths, degrees := make([]float64, n), make([]float64, n)
	for index := range n {
		lengths[index], degrees[index] = length, 7+360*float64(index)/float64(n)
	}
	return londonStar(lengths, degrees)
}

// planLondonStar plans the junction of the star and adds it to the
// network.
func planLondonStar(t *testing.T, network sim.Network, input londonJunctionInput) (sim.Network, londonJunction) {
	t.Helper()
	junction, err := planLondonJunction(input)
	if err != nil {
		t.Fatal(err)
	}
	network.Lanes = slices.Clone(network.Lanes)
	junction.add(&network)
	return network, junction
}

// londonRampLanes returns the ramps of the station by lane ID.
func londonRampLanes(network sim.Network, station string) map[string]sim.Lane {
	ramps := make(map[string]sim.Lane)
	for _, lane := range network.Lanes {
		if strings.HasPrefix(lane.ID, station+"-ramp-") {
			ramps[lane.ID] = lane
		}
	}
	return ramps
}

// londonArrivalCrossings returns the pairs of ramps of one arrival of the
// station that cross, as the layout audit finds crossings.
func londonArrivalCrossings(network sim.Network, station string) []string {
	arrivals := make(map[string][]auditLane)
	for _, lane := range newAuditLanes(network) {
		if rest, ok := strings.CutPrefix(lane.lane.ID, station+"-ramp-"); ok {
			arrival := rest[:2]
			arrivals[arrival] = append(arrivals[arrival], lane)
		}
	}
	var pairs []string
	for _, lanes := range arrivals {
		for index, first := range lanes {
			for _, second := range lanes[index+1:] {
				if first.crosses(second) {
					pairs = append(pairs, first.lane.ID+" "+second.lane.ID)
				}
			}
		}
	}
	slices.Sort(pairs)
	return pairs
}

// checkLondonStarRoutes checks that each arrival of the star connects to
// each departure through the station without ramps: by the off-slip, the
// lanes of the station, and the on-slip, as addLondonStation adds them.
func checkLondonStarRoutes(t *testing.T, network sim.Network, input londonJunctionInput, junction londonJunction) {
	t.Helper()
	next := make(map[string][]string)
	for _, lane := range network.Lanes {
		if !strings.Contains(lane.ID, "-ramp-") {
			next[lane.From] = append(next[lane.From], lane.To)
		}
	}
	for _, portal := range junction.slips[0].arrivals {
		next[portal.node] = append(next[portal.node], "station")
	}
	for _, portal := range junction.slips[0].departures {
		next["station"] = append(next["station"], portal.node)
	}
	for arrival, portal := range input.portals.arrivals {
		reached := map[string]bool{input.lanes[portal.half].From: true}
		queue := []string{input.lanes[portal.half].From}
		for len(queue) != 0 {
			node := queue[0]
			queue = queue[1:]
			for _, to := range next[node] {
				if !reached[to] {
					reached[to] = true
					queue = append(queue, to)
				}
			}
		}
		for departure, portal := range input.portals.departures {
			if !reached[input.lanes[portal.half].To] {
				t.Fatalf("arrival %d does not connect to departure %d through the station", arrival+1, departure+1)
			}
		}
	}
}

// londonNodeDegrees returns the number of lanes into and out of each node.
func londonNodeDegrees(network sim.Network) (in, out map[string]int) {
	in, out = make(map[string]int), make(map[string]int)
	for _, lane := range network.Lanes {
		out[lane.From]++
		in[lane.To]++
	}
	return in, out
}

func TestLondonJunctionShapes(t *testing.T) {
	t.Parallel()
	t.Run("terminus", func(t *testing.T) {
		t.Parallel()
		network, input := londonStar([]float64{1000}, []float64{30})
		before := len(network.Nodes)
		network, junction := planLondonStar(t, network, input)
		if len(network.Nodes) != before || len(junction.lanes) != 0 || len(junction.cuts) != 0 {
			t.Fatalf("terminus adds %d nodes, %d lanes, and %d cuts, want none", len(network.Nodes)-before, len(junction.lanes), len(junction.cuts))
		}
		slips := junction.slips[0]
		if slips.arrivals[0].node != input.portals.arrivals[0].node || slips.departures[0].node != input.portals.departures[0].node {
			t.Fatalf("terminus slips %+v, want the portals", slips)
		}
	})
	t.Run("through station", func(t *testing.T) {
		t.Parallel()
		network, input := londonStar([]float64{1000, 1000}, []float64{10, 185})
		before := len(network.Nodes)
		network, junction := planLondonStar(t, network, input)
		if len(network.Nodes) != before {
			t.Fatalf("through station adds %d nodes, want none", len(network.Nodes)-before)
		}
		ramps := londonRampLanes(network, "hub")
		if len(ramps) != 2 || ramps["hub-ramp-01-02"].From != input.portals.arrivals[0].node || ramps["hub-ramp-02-01"].To != input.portals.departures[0].node {
			t.Fatalf("through station ramps %v, want hub-ramp-01-02 and hub-ramp-02-01 between the portals", ramps)
		}
		// The portal is the one split of each arrival and the one join of
		// each departure: the slip road leaves it with the ramp.
		_, out := londonNodeDegrees(network)
		for index := range 2 {
			arrival, departure := input.portals.arrivals[index].node, input.portals.departures[index].node
			if junction.slips[0].arrivals[index].node != arrival || junction.slips[0].departures[index].node != departure || out[arrival] != 1 {
				t.Fatalf("arrival %d: slip %q and %d lanes out of the portal, want the portal and 1 ramp", index+1, junction.slips[0].arrivals[index].node, out[arrival])
			}
		}
	})
	t.Run("near reversal", func(t *testing.T) {
		t.Parallel()
		// Links 1 and 2 are 15 degrees apart. A turn between them is
		// almost a U-turn, so it gets no ramp. Pods use the station.
		network, input := londonStar([]float64{1000, 1000, 1000}, []float64{0, 15, 180})
		network, junction := planLondonStar(t, network, input)
		ramps := londonRampLanes(network, "hub")
		if got, want := slices.Sorted(maps.Keys(ramps)), []string{"hub-ramp-01-03", "hub-ramp-02-03", "hub-ramp-03-01", "hub-ramp-03-02"}; !slices.Equal(got, want) {
			t.Fatalf("ramps %v, want %v", got, want)
		}
		checkLondonStarRoutes(t, network, input, junction)
	})
	// The stars of 5 and 6 links drop some of the tight turns between
	// neighboring links.
	for n, wantRamps := range map[int]int{3: 6, 4: 12, 5: 17, 6: 26} {
		t.Run(fmt.Sprintf("junction of %d links", n), func(t *testing.T) {
			t.Parallel()
			network, input := regularLondonStar(n, 1000)
			network, _ = planLondonStar(t, network, input)
			ramps := londonRampLanes(network, "hub")
			if len(ramps) != wantRamps {
				t.Fatalf("%d ramps, want %d", len(ramps), wantRamps)
			}
			for index := range n {
				if _, ok := ramps[fmt.Sprintf("hub-ramp-%02d-%02d", index+1, index+1)]; ok {
					t.Fatalf("U-turn ramp from link %d", index+1)
				}
			}
			for id, ramp := range ramps {
				if ramp.SpeedLimit < londonRampMinimumSpeed {
					t.Fatalf("ramp %s has speed %v, less than %v", id, ramp.SpeedLimit, londonRampMinimumSpeed)
				}
			}
			// Each new node is a 2-way split or join.
			in, out := londonNodeDegrees(network)
			for _, node := range network.Nodes {
				split, join := strings.HasPrefix(node.ID, "hub-split-"), strings.HasPrefix(node.ID, "hub-join-")
				if split && (in[node.ID] != 1 || out[node.ID] != 2) || join && (in[node.ID] != 2 || out[node.ID] != 1) {
					t.Fatalf("node %s has %d lanes in and %d out", node.ID, in[node.ID], out[node.ID])
				}
			}
			// The plain turn order nests the ramps of one arrival up to 4
			// links. With more links, ramps from far chain nodes cross.
			crossings := londonArrivalCrossings(network, "hub")
			if n <= 4 && len(crossings) != 0 {
				t.Fatalf("ramps of one arrival cross: %v", crossings)
			}
		})
	}
}

func TestLondonChainSpacing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		room         float64
		needed, line int
		spacing      float64
	}{
		{"no new node", 50, 0, 0, 0},
		{"fits at 40 m", 200, 3, 3, 40},
		{"shrinks", 100.8, 2, 2, (100.8 - londonShortestLane) / 2},
		{"shrinks to 24 m", 4 * londonShortestLane, 3, 3, londonShortestLane},
		{"stages with fewer nodes", 50.1, 5, 1, 50.1 - londonShortestLane},
		{"stages with no new node", 45.6, 6, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			line, spacing := londonChainLine(test.room, test.needed)
			if line != test.line || math.Abs(spacing-test.spacing) > 1e-9 {
				t.Fatalf("londonChainLine(%v, %d) = %d, %v, want %d, %v", test.room, test.needed, line, spacing, test.line, test.spacing)
			}
		})
	}
	// A star with three long links and one link of 300 m: the chains of
	// the long links have 40 m spacing, and the chains of the short link
	// shrink to fit in its 90 m half.
	network, input := londonStar([]float64{1000, 1000, 1000, 300}, []float64{0, 90, 180, 270})
	network, _ = planLondonStar(t, network, input)
	chains := 0
	for _, lane := range network.Lanes {
		// A carriageway lane without an end at the middle of its link
		// joins two nodes of a chain.
		if !strings.HasPrefix(lane.ID, "london-link-") || strings.HasPrefix(lane.From, "london-mid-") || strings.HasPrefix(lane.To, "london-mid-") {
			continue
		}
		chains++
		want := londonChainSpacing
		if strings.HasPrefix(lane.ID, "london-link-004-") {
			want = (300.0/2 - londonPortalInset - londonShortestLane) / 2
		}
		if length := network.Length(lane); math.Abs(length-want) > 1e-6 {
			t.Fatalf("chain lane %s is %.6f m, want %.6f", lane.ID, length, want)
		}
	}
	// Each of the 8 portals has 2 new nodes and 2 lanes between chain
	// nodes.
	if chains != 16 {
		t.Fatalf("%d chain lanes, want 16", chains)
	}
}

func TestLondonStagedRamps(t *testing.T) {
	t.Parallel()
	t.Run("short link stages", func(t *testing.T) {
		t.Parallel()
		// The 165 m link has a 41 m half, which has no room for a chain
		// node, as at Canary Wharf. Its departure has one ramp, because
		// the turn from link 3 drops, so only its arrival stages.
		network, input := londonStar([]float64{165, 1000, 1000}, []float64{100, -52, -128})
		network, junction := planLondonStar(t, network, input)
		checkLondonStarRoutes(t, network, input, junction)
		var staged []string
		for _, lane := range network.Lanes {
			if strings.Contains(lane.ID, "-stage-") {
				staged = append(staged, lane.ID)
				if lane.SeparationGroup != lane.ID || lane.Control == nil {
					t.Fatalf("staged lane %+v needs its own separation group and a curve", lane)
				}
			}
		}
		if want := []string{"hub-stage-in-01-1"}; !slices.Equal(staged, want) {
			t.Fatalf("staged lanes %v, want %v", staged, want)
		}
		for _, lane := range network.Lanes {
			if length := network.Length(lane); length < 2*sim.Clearance {
				t.Fatalf("lane %s is %.3f m, shorter than %v", lane.ID, length, 2*sim.Clearance)
			}
		}
	})
	t.Run("staging does not fit", func(t *testing.T) {
		t.Parallel()
		// Each link is 150 m long. Each half has room for no chain node,
		// and the other portals are too near for a staged lane.
		_, input := londonStar([]float64{150, 150, 150, 150, 150}, []float64{0, 72, 144, 216, 288})
		_, err := planLondonJunction(input)
		want := `layout conflict at station Hub (hub): the ramps at link "london-link-002" do not fit, even when they stage`
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	})
}

func TestLondonRampSpeed(t *testing.T) {
	t.Parallel()
	// The parabola from (-h, 0) to (h, 0) with control (0, -d) has its
	// smallest radius h²/d at its vertex.
	parabola := func(h, d float64) (sim.Point, sim.Point, sim.Point) {
		return sim.Point{X: -h}, sim.Point{Y: -d}, sim.Point{X: h}
	}
	tests := []struct {
		name string
		h, d float64
		want float64
	}{
		{"tight", 10, 50, 2},
		{"medium", 40, 20, math.Sqrt(160)},
		{"wide is at most 14 m/s", 200, 10, speedLimit},
		{"straight", 50, 0, speedLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			from, control, to := parabola(test.h, test.d)
			if got := londonRampSpeed(from, control, to); math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("speed = %v, want %v", got, test.want)
			}
		})
	}
	t.Run("smallest radius of an asymmetric curve", func(t *testing.T) {
		t.Parallel()
		from, control, to := sim.Point{X: 100, Y: -18}, sim.Point{}, sim.Point{X: 18, Y: -60}
		// The circle through three close points of the curve gives the
		// radius at each sample.
		radius := math.Inf(1)
		const samples = 100000
		for index := 1; index < samples; index++ {
			a := londonCurvePoint(from, control, to, float64(index-1)/samples)
			b := londonCurvePoint(from, control, to, float64(index)/samples)
			c := londonCurvePoint(from, control, to, float64(index+1)/samples)
			area := math.Abs((b.X-a.X)*(c.Y-a.Y)-(b.Y-a.Y)*(c.X-a.X)) / 2
			radius = min(radius, math.Hypot(b.X-a.X, b.Y-a.Y)*math.Hypot(c.X-b.X, c.Y-b.Y)*math.Hypot(c.X-a.X, c.Y-a.Y)/(4*area))
		}
		want := math.Sqrt(londonRampAcceleration * radius)
		if got := londonRampSpeed(from, control, to); math.Abs(got-want) > 1e-3*want {
			t.Fatalf("speed = %v, want %v from the sampled radius %v", got, want, radius)
		}
	})
}

// londonRampsPresets builds each preset with ramps once.
var londonRampsPresets = sync.OnceValue(func() map[string]londonRampsBuild {
	builds := make(map[string]londonRampsBuild)
	var mutex sync.Mutex
	var group sync.WaitGroup
	for _, preset := range londonVariantPresets {
		group.Go(func() {
			options := preset.options()
			options.Junctions = LondonRamps
			config, err := preset.build(options)
			mutex.Lock()
			defer mutex.Unlock()
			builds[preset.name] = londonRampsBuild{config: config, err: err}
		})
	}
	group.Wait()
	return builds
})

type londonRampsBuild struct {
	config project.Config
	err    error
}

func TestLondonRampsPresets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source func() (londonSource, error)
		// dropped counts the turns that are too tight for a ramp.
		dropped int
		staged  []string
	}{
		{"LondonCentral", func() (londonSource, error) {
			var source londonSource
			err := decodeLondonSource(&source)
			return source, err
		}, 105, []string{"940GZZLUBNK"}},
		{"LondonFull", decodeLondonFullSource, 186, []string{"940GZZDLCAN", "940GZZLUBNK"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			build := londonRampsPresets()[test.name]
			if build.err != nil {
				t.Fatal(build.err)
			}
			config, network := build.config, build.config.Network
			if !strings.HasSuffix(config.Name, CustomCapacitySuffix) {
				t.Fatalf("project name %q has no custom capacity suffix", config.Name)
			}
			source, err := test.source()
			if err != nil {
				t.Fatal(err)
			}
			if err := layoutError(network, auditLondonLayout(network, newLondonAuditInput(source))); err != nil {
				t.Fatal(err)
			}
			links := make(map[string]int)
			for _, link := range source.Links {
				links[link.A]++
				links[link.B]++
			}
			wantRamps := 0
			for _, count := range links {
				wantRamps += count * (count - 1)
			}
			checkLondonRampLanes(t, network, wantRamps-test.dropped, test.staged)
			checkLondonStationRoutes(t, network, source)
		})
	}
}

// checkLondonStationRoutes checks that each station connects each arrival
// to each departure through the station, without ramps. Each arrival
// starts at the middle of its link, and each departure ends at the middle
// of its link. The search does not use the lanes of other stations, so it
// cannot go round through another station.
func checkLondonStationRoutes(t *testing.T, network sim.Network, source londonSource) {
	t.Helper()
	arrivals, departures := make(map[string][]string), make(map[string][]string)
	for index, link := range source.Links {
		arrivals[link.A] = append(arrivals[link.A], fmt.Sprintf("london-mid-%03d-b", index+1))
		departures[link.A] = append(departures[link.A], fmt.Sprintf("london-mid-%03d-a", index+1))
		arrivals[link.B] = append(arrivals[link.B], fmt.Sprintf("london-mid-%03d-a", index+1))
		departures[link.B] = append(departures[link.B], fmt.Sprintf("london-mid-%03d-b", index+1))
	}
	var lanes []sim.Lane
	for _, lane := range network.Lanes {
		if !strings.Contains(lane.ID, "-ramp-") {
			lanes = append(lanes, lane)
		}
	}
	for _, station := range source.Stations {
		next := make(map[string][]string)
		for _, lane := range lanes {
			if lane.StationID == "" || lane.StationID == station.ID {
				next[lane.From] = append(next[lane.From], lane.To)
			}
		}
		for _, start := range arrivals[station.ID] {
			reached := map[string]bool{start: true}
			queue := []string{start}
			for len(queue) != 0 {
				node := queue[0]
				queue = queue[1:]
				for _, to := range next[node] {
					if !reached[to] {
						reached[to] = true
						queue = append(queue, to)
					}
				}
			}
			for _, end := range departures[station.ID] {
				if !reached[end] {
					t.Fatalf("station %s: %s does not connect to %s through the station", station.ID, start, end)
				}
			}
		}
	}
}

// checkLondonRampLanes checks the ramps, the staged lanes, and the speed
// limits of a London network with ramps.
func checkLondonRampLanes(t *testing.T, network sim.Network, wantRamps int, wantStaged []string) {
	t.Helper()
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	ramps, staged := 0, make(map[string]bool)
	for _, lane := range network.Lanes {
		station, rest, isRamp := strings.Cut(lane.ID, "-ramp-")
		stagedStation, _, isStaged := strings.Cut(lane.ID, "-stage-")
		switch {
		case strings.Contains(lane.ID, "-move-"):
			t.Fatalf("chord %s in a ramps network", lane.ID)
		case isRamp || isStaged:
			if isRamp {
				ramps++
				if rest[:2] == rest[3:] {
					t.Fatalf("U-turn ramp %s at %s", lane.ID, station)
				}
			} else {
				staged[stagedStation] = true
			}
			if lane.SpeedLimit < londonRampMinimumSpeed {
				t.Fatalf("lane %s has speed %v, less than %v", lane.ID, lane.SpeedLimit, londonRampMinimumSpeed)
			}
			want := londonRampSpeed(nodes[lane.From], *lane.Control, nodes[lane.To])
			if lane.SpeedLimit != want || !(lane.SpeedLimit > 0) || lane.SpeedLimit > speedLimit || lane.SeparationGroup != lane.ID || lane.StationID != "" {
				t.Fatalf("lane %+v: want speed %v and its own separation group", lane, want)
			}
		case (strings.HasPrefix(lane.ID, "london-link-") || lane.StationID != "") && lane.SpeedLimit != speedLimit:
			t.Fatalf("lane %s has speed %v, want %v", lane.ID, lane.SpeedLimit, speedLimit)
		case strings.HasPrefix(lane.ID, "london-link-") && lane.SeparationGroup != lane.ID[:len("london-link-000")]:
			t.Fatalf("carriageway lane %s has separation group %q", lane.ID, lane.SeparationGroup)
		}
	}
	if ramps != wantRamps {
		t.Fatalf("%d ramps, want %d", ramps, wantRamps)
	}
	if got := slices.Sorted(maps.Keys(staged)); !slices.Equal(got, wantStaged) {
		t.Fatalf("stations with staged ramps %v, want %v", got, wantStaged)
	}
	for _, node := range network.Nodes {
		if (strings.Contains(node.ID, "-split-") || strings.Contains(node.ID, "-join-")) && strings.Contains(node.ID, "-merge") {
			t.Fatalf("node %s uses the station merge ID", node.ID)
		}
	}
}

func TestLondonRampsSizeEstimate(t *testing.T) {
	t.Parallel()
	full, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	var central londonSource
	if err := decodeLondonSource(&central); err != nil {
		t.Fatal(err)
	}
	sources := map[string]londonSource{"LondonCentral": central, "LondonFull": full}
	for _, preset := range londonVariantPresets {
		for name, junctions := range map[string]LondonJunctions{"chords": LondonChords, "ramps": LondonRamps} {
			t.Run(preset.name+" "+name, func(t *testing.T) {
				t.Parallel()
				source := sources[preset.name]
				options := preset.options()
				options.Junctions = junctions
				capacity, err := options.resolve(source)
				if err != nil {
					t.Fatal(err)
				}
				estimate := func(limits londonLimits, unit string) int {
					t.Helper()
					_, sizeErr := londonNetworkWithHeadings(source, capacity, nil, limits)
					var count int
					if sizeErr == nil {
						t.Fatalf("limits %+v: no error", limits)
					}
					if _, scanErr := fmt.Sscanf(sizeErr.Error(), "generated London network needs %d "+unit, &count); scanErr != nil {
						t.Fatalf("limits %+v: error %v", limits, sizeErr)
					}
					return count
				}
				nodes := estimate(londonLimits{nodes: 0, lanes: math.MaxInt}, "nodes")
				lanes := estimate(londonLimits{nodes: math.MaxInt, lanes: 0}, "lanes")
				network, err := londonNetworkWithHeadings(source, capacity, nil, londonLimits{nodes: nodes, lanes: lanes})
				if err != nil {
					t.Fatal(err)
				}
				var spareNodes, spareLanes int
				if junctions == LondonRamps {
					spareNodes, spareLanes = londonUnbuiltJunctions(t, network, source)
				}
				if len(network.Nodes)+spareNodes != nodes || len(network.Lanes)+spareLanes != lanes {
					t.Fatalf("estimate %d nodes and %d lanes, network has %d and %d, and the dropped turns %d and %d", nodes, lanes, len(network.Nodes), len(network.Lanes), spareNodes, spareLanes)
				}
			})
		}
	}
}

// londonUnbuiltJunctions returns the nodes and lanes that the size estimate
// counts for the turns that drop. The estimate counts n(n-1) ramps for a
// station with n links, and 2n(t-2) chain nodes, each with a lane, where t
// counts the turns of a portal: n-1 ramps and a slip road for the station
// and each Parking facility. The chain nodes include the staged nodes. A
// station without dropped turns has exactly these.
func londonUnbuiltJunctions(t *testing.T, network sim.Network, source londonSource) (nodes, lanes int) {
	t.Helper()
	links := make(map[string]int)
	for _, link := range source.Links {
		links[link.A]++
		links[link.B]++
	}
	built, ramps := make(map[string]int), make(map[string]int)
	for _, node := range network.Nodes {
		if station, _, ok := strings.Cut(node.ID, "-split-"); ok {
			built[station]++
		} else if station, _, ok := strings.Cut(node.ID, "-join-"); ok {
			built[station]++
		}
	}
	for _, lane := range network.Lanes {
		if station, _, ok := strings.Cut(lane.ID, "-ramp-"); ok {
			ramps[station]++
		}
	}
	for _, station := range source.Stations {
		n := links[station.ID]
		turns := n
		for _, facility := range londonParkingFacilities {
			if facility.Gateway == station.ID {
				turns++
			}
		}
		chains, wantRamps := 2*n*max(0, turns-2), n*(n-1)
		spareNodes, spareRamps := chains-built[station.ID], wantRamps-ramps[station.ID]
		if spareNodes < 0 || spareRamps < 0 || spareRamps == 0 && spareNodes != 0 {
			t.Fatalf("station %s has %d chain nodes and %d ramps, the estimate counts %d and %d", station.ID, built[station.ID], ramps[station.ID], chains, wantRamps)
		}
		nodes, lanes = nodes+spareNodes, lanes+spareNodes+spareRamps
	}
	return nodes, lanes
}

func TestLondonJunctionsOption(t *testing.T) {
	t.Parallel()
	if DefaultLondonCentralOptions().Junctions != LondonChords || DefaultLondonFullOptions().Junctions != LondonChords {
		t.Fatal("the default junctions are not chords")
	}
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		t.Fatal(err)
	}
	options := DefaultLondonCentralOptions()
	chords, err := options.resolve(source)
	if err != nil {
		t.Fatal(err)
	}
	options.Junctions = LondonRamps
	ramps, err := options.resolve(source)
	if err != nil {
		t.Fatal(err)
	}
	if chords.equal(ramps) || ramps.equal(chords) || !ramps.equal(ramps) {
		t.Fatal("equal does not compare the junctions")
	}
	options.Junctions = 2
	if _, err := LondonCentralWith(options); err == nil || err.Error() != "unknown London junctions 2" {
		t.Fatalf("error = %v, want unknown London junctions 2", err)
	}
}

func TestLondonStationSegments(t *testing.T) {
	t.Parallel()
	// The 165 m link stages, so the segments hold a staged lane too.
	network, input := londonStar([]float64{165, 1000, 1000}, []float64{100, -52, -128})
	network, junction := planLondonStar(t, network, input)
	segments := londonStationSegments([]londonJunction{junction}, 0, sim.Point{}, input.portals)
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	var want []londonSegment
	for _, lane := range junction.lanes {
		if lane.Control == nil {
			continue
		}
		points := londonCurvePoints(nodes[lane.From], *lane.Control, nodes[lane.To], londonRampParts)
		for index := 1; index < len(points); index++ {
			want = append(want, londonSegment{from: points[index-1], to: points[index]})
		}
	}
	if len(want) != (4+1)*londonRampParts || !slices.Equal(segments, want) {
		t.Fatalf("%d segments, want the %d parts of the 4 ramps and the staged lane", len(segments), len(want))
	}
	if chords := londonStationSegments(nil, 0, sim.Point{}, input.portals); !slices.Equal(chords, londonMovementSegments(sim.Point{}, input.portals)) {
		t.Fatal("without junctions, the segments are not the movement lanes")
	}
}
