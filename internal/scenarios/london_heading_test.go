package scenarios

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestLondonGapBisector(t *testing.T) {
	t.Parallel()
	degrees := func(values ...float64) []float64 {
		for index := range values {
			values[index] *= math.Pi / 180
		}
		return values
	}
	tests := []struct {
		name       string
		directions []float64
		want       float64
	}{
		{name: "no link", directions: nil, want: 0},
		{name: "terminus extends past the end of its line", directions: degrees(30), want: 210},
		{name: "straight through station", directions: degrees(0, 180), want: 90},
		{name: "bent line opens on the outside", directions: degrees(10, 110), want: 240},
		{name: "gap across zero degrees", directions: degrees(100, 170, 260), want: 0},
		{name: "negative direction", directions: degrees(-90, 0, 90), want: 180},
		{name: "widest of three gaps", directions: degrees(0, 90, 135), want: 247.5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := londonGapBisector(test.directions) * 180 / math.Pi
			if angleBetween(got*math.Pi/180, test.want*math.Pi/180) > 1e-9 {
				t.Fatalf("bisector = %.3f degrees, want %.3f", got, test.want)
			}
		})
	}
}

func TestLondonSegmentCrosses(t *testing.T) {
	t.Parallel()
	segment := func(ax, ay, bx, by float64) londonSegment {
		return londonSegment{from: sim.Point{X: ax, Y: ay}, to: sim.Point{X: bx, Y: by}}
	}
	tests := []struct {
		name          string
		first, second londonSegment
		want          bool
		// gap is the smallest distance between the segments.
		gap float64
	}{
		{name: "cross in the middle", first: segment(0, 0, 10, 10), second: segment(0, 10, 10, 0), want: true, gap: 0},
		{name: "shared end point", first: segment(0, 0, 10, 0), second: segment(10, 0, 10, 10), want: false, gap: 0},
		{name: "end point on the other segment", first: segment(0, 0, 10, 0), second: segment(5, 0, 5, 10), want: false, gap: 0},
		{name: "parallel", first: segment(0, 0, 10, 0), second: segment(0, 1, 10, 1), want: false, gap: 1},
		{name: "apart on one line", first: segment(0, 0, 1, 0), second: segment(2, 0, 3, 0), want: false, gap: 1},
		{name: "lines cross outside the segments", first: segment(0, 0, 1, 1), second: segment(3, 0, 2, 1), want: false, gap: 1},
		{name: "end point beside the other segment", first: segment(0, 0, 10, 0), second: segment(5, 4, 20, 40), want: false, gap: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, pair := range [][2]londonSegment{{test.first, test.second}, {test.second, test.first}} {
				if got := pair[0].crosses(pair[1]); got != test.want {
					t.Fatalf("%v crosses %v = %t, want %t", pair[0], pair[1], got, test.want)
				}
				if got := pair[0].gap(pair[1]); math.Abs(got-test.gap) > 1e-9 {
					t.Fatalf("%v gap to %v = %v, want %v", pair[0], pair[1], got, test.gap)
				}
				if pair[0].near(pair[1], test.gap) || !pair[0].near(pair[1], test.gap+0.5) {
					t.Fatalf("%v near %v changes at a limit other than %v", pair[0], pair[1], test.gap)
				}
			}
		})
	}
}

func TestLondonInArea(t *testing.T) {
	t.Parallel()
	shape := londonStationShape{center: sim.Point{X: 100, Y: 100}, direction: math.Pi / 2, berths: 2}
	area := shape.area()
	tests := []struct {
		name  string
		point sim.Point
		want  bool
	}{
		// The station points down the screen, so its area is from y 180
		// to y 485 and from x 0 to x 200.
		{name: "center of the berths", point: sim.Point{X: 100, Y: 427.5}, want: true},
		{name: "corner", point: sim.Point{X: 0, Y: 180}, want: true},
		{name: "junction", point: sim.Point{X: 100, Y: 100}, want: false},
		{name: "past the last berth", point: sim.Point{X: 100, Y: 490}, want: false},
		{name: "beside the berths", point: sim.Point{X: 201, Y: 300}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := inArea(test.point, area); got != test.want {
				t.Fatalf("inArea(%v) = %t, want %t", test.point, got, test.want)
			}
		})
	}
}

func TestSearchLondonHeadings(t *testing.T) {
	t.Parallel()
	east := []londonPreference{{direction: 0}}
	south := []londonPreference{{direction: math.Pi / 2}}
	site := func(x, y float64, preferred []londonPreference) londonSite {
		return londonSite{center: sim.Point{X: x, Y: y}, berths: 2, preferred: preferred}
	}
	// A heading of 0 points east. Its diverge node is at (80, -30).
	shortRoad := site(0, 0, east)
	shortRoad.arrivals = []sim.Point{{X: 70, Y: -30}}
	tests := []struct {
		name  string
		input londonHeadingInput
		// want holds the heading of each site in degrees. When it is nil,
		// only the conflict checks apply, and the preferred heading of the
		// first site must have a conflict.
		want []float64
	}{
		{name: "free station keeps the nearest step", input: londonHeadingInput{sites: []londonSite{site(0, 0, []londonPreference{{direction: 3 * math.Pi / 180}})}}, want: []float64{5}},
		{name: "opposite side costs more", input: londonHeadingInput{sites: []londonSite{site(0, 0, []londonPreference{{direction: math.Pi}, {direction: 0, penalty: londonOppositePenalty}})}}, want: []float64{180}},
		{name: "short road lane", input: londonHeadingInput{sites: []londonSite{shortRoad}}},
		{
			name: "guideway across the preferred side",
			input: londonHeadingInput{
				sites: []londonSite{site(0, 0, east)},
				links: []londonSegment{{from: sim.Point{X: 300, Y: -1000}, to: sim.Point{X: 300, Y: 1000}}},
			},
		},
		{
			name: "berths nearer to another station",
			input: londonHeadingInput{
				sites:   []londonSite{site(0, 0, south)},
				centers: []sim.Point{{X: 0, Y: 0}, {X: 0, Y: 500}},
			},
		},
		{
			name: "two stations that face each other",
			input: londonHeadingInput{
				sites:   []londonSite{site(0, 0, east), site(500, 0, []londonPreference{{direction: math.Pi}})},
				centers: []sim.Point{{X: 0, Y: 0}, {X: 500, Y: 0}},
			},
		},
		{
			// The core lanes of the two stations are 20 meters apart.
			name:  "two stations nearer than the gap",
			input: londonHeadingInput{sites: []londonSite{site(0, 0, south), site(220, 0, south)}},
		},
		{
			// The second site keeps its preferred heading only when the
			// search uses the new heading of the first site.
			name:  "search uses the current headings of the other sites",
			input: londonHeadingInput{sites: []londonSite{site(0, 0, east), site(500, 0, []londonPreference{{direction: math.Pi}})}},
			want:  []float64{75, 180},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if len(test.input.centers) > 0 {
				for index := range test.input.sites {
					test.input.sites[index].own = index
				}
			}
			headings := searchLondonHeadings(test.input)
			for index, want := range test.want {
				if angleBetween(headings[index], want*math.Pi/180) > 1e-9 {
					t.Fatalf("site %d heading = %.1f degrees, want %.1f", index, headings[index]*180/math.Pi, want)
				}
			}
			// Score the result against the final headings of all sites.
			search := newLondonHeadingSearch(test.input)
			for index, site := range test.input.sites {
				search.headings[index], search.footprints[index] = headings[index], site.footprint(headings[index])
			}
			for index, footprint := range search.footprints {
				if score := search.fixedScore(index, footprint) + search.siteScore(index, footprint); score != 0 {
					t.Errorf("site %d with heading %.1f degrees has conflict score %v, want 0", index, headings[index]*180/math.Pi, score)
				}
			}
			if test.want != nil {
				return
			}
			// The case must give the preferred heading a conflict.
			first := test.input.sites[0]
			preferred := first.footprint(first.preferred[0].direction)
			search.footprints[0] = preferred
			for index, site := range test.input.sites[1:] {
				search.footprints[index+1] = site.footprint(site.preferred[0].direction)
			}
			if score := search.fixedScore(0, preferred) + search.siteScore(0, preferred); score == 0 {
				t.Error("the preferred heading of the first site has no conflict")
			}
		})
	}
}

// londonLaneKind sorts the London lanes for the geometry checks.
type londonLaneKind int

const (
	londonMovementLane londonLaneKind = iota
	londonLinkLane
	londonRoadLane
	londonCoreLane
)

// londonKind returns the kind of a lane in the London preset.
func londonKind(lane sim.Lane) londonLaneKind {
	switch {
	case strings.HasPrefix(lane.ID, "london-link-"):
		return londonLinkLane
	case lane.StationID == "":
		return londonMovementLane
	case strings.Contains(lane.ID, "-road-in-"), strings.Contains(lane.ID, "-road-out-"):
		return londonRoadLane
	}
	return londonCoreLane
}

// TestLondonStationsLieBesideTheirLines checks the station headings that
// the heading search gives. No station lies on a guideway or on another
// station, and the berths are nearest to their own station. A through
// station lies parallel to its line, and pods do not turn back sharply
// between a guideway and a station road.
func TestLondonStationsLieBesideTheirLines(t *testing.T) {
	t.Parallel()
	config := London()
	network := config.Network
	berths := 0
	for _, station := range network.Stations {
		berths += len(station.Berths)
	}
	if len(network.Nodes) != 1842 || len(network.Lanes) != 3101 || len(network.Stations) != 99 || berths != 228 || len(config.Fleet) != 114 {
		t.Fatalf("got %d nodes, %d lanes, %d stations, %d berths, and %d pods, want 1842, 3101, 99, 228, and 114",
			len(network.Nodes), len(network.Lanes), len(network.Stations), berths, len(config.Fleet))
	}
	nodes := make(map[string]sim.Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	segment := func(lane sim.Lane) lineSegment {
		return lineSegment{from: nodes[lane.From], to: nodes[lane.To]}
	}
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]sim.Point, len(source.Stations))
	for _, station := range source.Stations {
		positions[station.ID] = londonPoint(station.Latitude, station.Longitude)
	}
	byStation := make(map[string][]sim.Lane)
	// guideways holds the link and movement lanes. A curved lane has 12
	// parts.
	type guideway struct {
		id    string
		parts []lineSegment
	}
	var guideways []guideway
	linkInto, linkFrom := make(map[string]sim.Lane), make(map[string]sim.Lane)
	for _, lane := range network.Lanes {
		switch londonKind(lane) {
		case londonLinkLane, londonMovementLane:
			parts := []lineSegment{segment(lane)}
			if lane.Control != nil {
				parts = parts[:0]
				length := network.Length(lane)
				for part := range 12 {
					parts = append(parts, lineSegment{
						from: network.Position(lane, length*float64(part)/12),
						to:   network.Position(lane, length*float64(part+1)/12),
					})
				}
			}
			guideways = append(guideways, guideway{id: lane.ID, parts: parts})
			if londonKind(lane) == londonLinkLane {
				linkInto[lane.To], linkFrom[lane.From] = lane, lane
			}
		case londonRoadLane, londonCoreLane:
			if lane.Control != nil {
				t.Fatalf("station lane %q is curved", lane.ID)
			}
			byStation[lane.StationID] = append(byStation[lane.StationID], lane)
		}
	}

	t.Run("no core station lane on a guideway", func(t *testing.T) {
		t.Parallel()
		// Road lanes can cross guideways near the junction. Each road lane
		// has its own separation group.
		for _, station := range network.Stations {
			for _, lane := range byStation[station.ID] {
				if londonKind(lane) != londonCoreLane {
					continue
				}
				for _, guideway := range guideways {
					if slices.ContainsFunc(guideway.parts, func(part lineSegment) bool { return segmentsCross(segment(lane), part) }) {
						t.Errorf("%s lane %q crosses %q", station.Name, lane.ID, guideway.id)
					}
				}
			}
		}
	})

	t.Run("berths nearest to their own station", func(t *testing.T) {
		t.Parallel()
		// Each heading of Mansion House crosses a guideway or puts its
		// berths nearer to another station.
		for _, station := range network.Stations {
			if station.ParkingOnly || station.Name == "Mansion House" {
				continue
			}
			var middle sim.Point
			for _, berth := range station.Berths {
				middle = add(middle, scale(nodes[berth.Node], 1/float64(len(station.Berths))))
			}
			own := math.Hypot(middle.X-positions[station.ID].X, middle.Y-positions[station.ID].Y)
			for _, other := range source.Stations {
				if other.ID != station.ID && math.Hypot(middle.X-positions[other.ID].X, middle.Y-positions[other.ID].Y) < own {
					t.Errorf("%s berths are nearer to %s", station.Name, other.Name)
				}
			}
		}
	})

	t.Run("no station on another station", func(t *testing.T) {
		t.Parallel()
		// A Parking facility and its gateway station connect to the same
		// portals, so their road lanes can cross. Each road lane has its
		// own separation group, so the simulation treats such a crossing
		// as grade-separated.
		gateways := map[string]string{"parking-west": "940GZZLUHSD", "parking-north": "940GZZLUFPK", "parking-east": "940GZZLUMED"}
		shared := func(a, b sim.Station) bool {
			return gateways[a.ID] == b.ID || gateways[b.ID] == a.ID
		}
		areas := make(map[string][4]sim.Point, len(network.Stations))
		for _, station := range network.Stations {
			last := station.Berths[len(station.Berths)-1].ID
			areas[station.ID] = [4]sim.Point{nodes[station.Entry], nodes[station.Exit], nodes[last+"-departure"], nodes[last+"-arrival"]}
		}
		for index, first := range network.Stations {
			area := areas[first.ID]
			for _, second := range network.Stations[index+1:] {
				for _, a := range byStation[first.ID] {
					for _, b := range byStation[second.ID] {
						if londonKind(a) == londonRoadLane && londonKind(b) == londonRoadLane && shared(first, second) {
							continue
						}
						if segmentsCross(segment(a), segment(b)) {
							t.Errorf("%s lane %q crosses %s lane %q", first.Name, a.ID, second.Name, b.ID)
						}
						if londonKind(a) != londonCoreLane || londonKind(b) != londonCoreLane {
							continue
						}
						// The lanes do not cross, so the gap is at an end point.
						aSegment, bSegment := segment(a), segment(b)
						gap := min(distanceToSegment(aSegment.from, bSegment), distanceToSegment(aSegment.to, bSegment),
							distanceToSegment(bSegment.from, aSegment), distanceToSegment(bSegment.to, aSegment))
						if gap < londonStationGap {
							t.Errorf("%s lane %q is %.1f meters from %s lane %q, want at least %.0f", first.Name, a.ID, gap, second.Name, b.ID, londonStationGap)
						}
					}
				}
				other := areas[second.ID]
				for _, lane := range byStation[second.ID] {
					if londonKind(lane) == londonCoreLane && (inArea(nodes[lane.From], area) || inArea(nodes[lane.To], area)) {
						t.Errorf("%s lane %q is on %s", second.Name, lane.ID, first.Name)
					}
				}
				for _, lane := range byStation[first.ID] {
					if londonKind(lane) == londonCoreLane && (inArea(nodes[lane.From], other) || inArea(nodes[lane.To], other)) {
						t.Errorf("%s lane %q is on %s", first.Name, lane.ID, second.Name)
					}
				}
			}
		}
	})

	t.Run("through stations lie along their line", func(t *testing.T) {
		t.Parallel()
		// Mansion House lies between Bank, Cannon Street, and St. Paul's.
		// The two links of Willesden Green go in almost the same direction.
		allowed := []string{"Mansion House", "Willesden Green"}
		neighbors := make(map[string][]sim.Point)
		for _, link := range source.Links {
			neighbors[link.A] = append(neighbors[link.A], positions[link.B])
			neighbors[link.B] = append(neighbors[link.B], positions[link.A])
		}
		var deviations []float64
		over := 0
		for _, station := range network.Stations {
			line := neighbors[station.ID]
			if len(line) != 2 {
				continue
			}
			entry, exit := nodes[station.Entry], nodes[station.Exit]
			axis := math.Atan2(exit.Y-entry.Y, exit.X-entry.X)
			// The angle between two lines is at most 90 degrees.
			deviation := angleBetween(axis, math.Atan2(line[1].Y-line[0].Y, line[1].X-line[0].X))
			deviation = min(deviation, math.Pi-deviation) * 180 / math.Pi
			deviations = append(deviations, deviation)
			if deviation > 30 {
				over++
				t.Logf("%s axis error %.1f degrees", station.Name, deviation)
				if !slices.Contains(allowed, station.Name) {
					t.Errorf("%s axis error = %.1f degrees, want at most 30", station.Name, deviation)
				}
			}
		}
		if len(deviations) != 48 {
			t.Fatalf("got %d through stations, want 48", len(deviations))
		}
		slices.Sort(deviations)
		median := (deviations[23] + deviations[24]) / 2
		t.Logf("48 through stations: median axis error %.1f degrees, %d over 30 degrees", median, over)
		if median > 20 {
			t.Fatalf("median axis error = %.1f degrees, want at most 20", median)
		}
	})

	t.Run("no sharp turn onto a station road", func(t *testing.T) {
		t.Parallel()
		heading := func(from, to sim.Point) float64 {
			return math.Atan2(to.Y-from.Y, to.X-from.X)
		}
		largest := 0.0
		for _, station := range network.Stations {
			for _, lane := range byStation[station.ID] {
				if londonKind(lane) != londonRoadLane {
					continue
				}
				var turn float64
				if in, ok := linkInto[lane.From]; ok {
					turn = angleBetween(heading(nodes[in.From], nodes[in.To]), heading(nodes[lane.From], nodes[lane.To]))
				} else if out, ok := linkFrom[lane.To]; ok {
					turn = angleBetween(heading(nodes[lane.From], nodes[lane.To]), heading(nodes[out.From], nodes[out.To]))
				} else {
					t.Fatalf("road lane %q has no guideway", lane.ID)
				}
				largest = max(largest, turn)
				if turn > 135*math.Pi/180 {
					t.Errorf("%s road lane %q turns %.1f degrees from its guideway, want at most 135", station.Name, lane.ID, turn*180/math.Pi)
				}
			}
		}
		t.Logf("largest turn between a guideway and a station road: %.1f degrees", largest*180/math.Pi)
	})
}
