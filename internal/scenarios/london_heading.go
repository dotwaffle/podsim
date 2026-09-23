package scenarios

import (
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	// londonHeadingCount is the number of headings that the search tries
	// for each station, one each 5 degrees.
	londonHeadingCount = 72
	// londonHeadingPasses is the maximum number of search passes over all
	// stations. A pass that changes no heading stops the search.
	londonHeadingPasses = 3
	// londonConflictPenalty is the score of one crossing or overlap.
	londonConflictPenalty = 10.0
	// londonShortRoadPenalty is the score of one road lane that is too
	// short for project validation. It is 100 times londonConflictPenalty,
	// so the search avoids a short road lane before it avoids crossings.
	// With a short road lane, project validation rejects the preset and
	// London panics.
	londonShortRoadPenalty = 1000.0
	// londonShortRoad is the length below which a road lane gets
	// londonShortRoadPenalty. It is 1 meter more than the validation
	// limit.
	londonShortRoad = 2*sim.Clearance + 1
	// londonDeviationUnit is the deviation from a preferred heading that
	// adds 1 to the score.
	londonDeviationUnit = math.Pi / 6
	// londonOppositePenalty is the deviation that the search adds when a
	// through station uses the side of its line with the narrower free
	// angle.
	londonOppositePenalty = math.Pi / 12
	// londonStationGap is the smallest distance between a core lane of
	// one station and a core lane of another station. It is the distance
	// between the two carriageways of a guideway.
	londonStationGap = 2 * londonTrackOffset
)

// londonSegment is a straight lane between two node positions.
type londonSegment struct {
	from, to sim.Point
}

// crosses reports whether the two segments cross at a point inside both. A
// shared end point or a touch is not a crossing.
func (s londonSegment) crosses(other londonSegment) bool {
	const tolerance = 1e-9
	opposite := func(a, b float64) bool {
		return (a > tolerance && b < -tolerance) || (a < -tolerance && b > tolerance)
	}
	return opposite(other.side(s.from), other.side(s.to)) && opposite(s.side(other.from), s.side(other.to))
}

// side is positive when the point is on one side of the line through the
// segment, negative on the other side, and zero on the line.
func (s londonSegment) side(point sim.Point) float64 {
	return (s.to.X-s.from.X)*(point.Y-s.from.Y) - (s.to.Y-s.from.Y)*(point.X-s.from.X)
}

// distance returns the distance from the point to the nearest point of the
// segment.
func (s londonSegment) distance(point sim.Point) float64 {
	dx, dy := s.to.X-s.from.X, s.to.Y-s.from.Y
	fraction := 0.0
	if lengthSquared := dx*dx + dy*dy; lengthSquared > 0 {
		fraction = min(1, max(0, ((point.X-s.from.X)*dx+(point.Y-s.from.Y)*dy)/lengthSquared))
	}
	return math.Hypot(point.X-s.from.X-fraction*dx, point.Y-s.from.Y-fraction*dy)
}

// gap returns the smallest distance between the two segments. It is 0 when
// they cross.
func (s londonSegment) gap(other londonSegment) float64 {
	if s.crosses(other) {
		return 0
	}
	return min(s.distance(other.from), s.distance(other.to), other.distance(s.from), other.distance(s.to))
}

// near reports whether the segments are nearer to each other than the
// limit. Segments that cross are nearer than any positive limit.
func (s londonSegment) near(other londonSegment, limit float64) bool {
	// Segments with boxes that are the limit or more apart are not near.
	// This check is faster than gap.
	if min(other.from.X, other.to.X)-max(s.from.X, s.to.X) >= limit || min(s.from.X, s.to.X)-max(other.from.X, other.to.X) >= limit ||
		min(other.from.Y, other.to.Y)-max(s.from.Y, s.to.Y) >= limit || min(s.from.Y, s.to.Y)-max(other.from.Y, other.to.Y) >= limit {
		return false
	}
	return s.gap(other) < limit
}

// length returns the distance between the end points of the segment.
func (s londonSegment) length() float64 {
	return math.Hypot(s.to.X-s.from.X, s.to.Y-s.from.Y)
}

// countCrossings returns the number of segment pairs that cross.
func countCrossings(first, second []londonSegment) int {
	count := 0
	for _, a := range first {
		for _, b := range second {
			if a.crosses(b) {
				count++
			}
		}
	}
	return count
}

// countNear returns the number of segment pairs that are nearer to each
// other than the limit.
func countNear(first, second []londonSegment, limit float64) int {
	count := 0
	for _, a := range first {
		for _, b := range second {
			if a.near(b, limit) {
				count++
			}
		}
	}
	return count
}

// londonStationShape sets the position of a station: the source position
// of its junction, the direction of its berth rows from that position, and
// its berth count.
type londonStationShape struct {
	center    sim.Point
	direction float64
	berths    int
}

// londonStationNodes holds the node positions of one station.
type londonStationNodes struct {
	diverge, merge, entry, exit sim.Point
	berths                      []londonBerthNodes
}

// londonBerthNodes are the node positions of one berth row.
type londonBerthNodes struct {
	arrival, berth, departure sim.Point
}

// frame returns the station frame of the shape. Its outward axis points
// along the heading, from the center to the berth rows.
func (shape londonStationShape) frame() stationFrame {
	outward := sim.Point{X: math.Cos(shape.direction), Y: math.Sin(shape.direction)}
	return stationFrame{origin: shape.center, along: sim.Point{X: -outward.Y, Y: outward.X}, outward: outward}
}

// lastBerthDepth returns the distance from the center to the last berth row.
func (shape londonStationShape) lastBerthDepth() float64 {
	return 290 + 75*float64(shape.berths-1)
}

// nodes returns the node positions of the station. The diverge and merge
// nodes are 80 meters out from the center, londonStationThroatOffset to each
// side of the station axis. The entry and exit nodes are 200 meters out and
// 100 meters to each side. The berth rows start 290 meters out, with a
// 75-meter pitch.
func (shape londonStationShape) nodes() londonStationNodes {
	frame := shape.frame()
	nodes := londonStationNodes{
		diverge: frame.position(-londonStationThroatOffset, 80),
		merge:   frame.position(londonStationThroatOffset, 80),
		entry:   frame.position(-100, 200),
		exit:    frame.position(100, 200),
	}
	for index := range shape.berths {
		depth := 290 + 75*float64(index)
		nodes.berths = append(nodes.berths, londonBerthNodes{
			arrival:   frame.position(-100, depth),
			berth:     frame.position(0, depth),
			departure: frame.position(100, depth),
		})
	}
	return nodes
}

// area returns the corners of the rectangle that holds the station lanes
// past the throat. It ends 20 meters past the last berth row.
func (shape londonStationShape) area() [4]sim.Point {
	frame := shape.frame()
	far := shape.lastBerthDepth() + 20
	return [4]sim.Point{frame.position(-100, 80), frame.position(100, 80), frame.position(100, far), frame.position(-100, far)}
}

// reach returns the distance from the center to the farthest point of the
// station area. The portals are nearer than this distance.
func (shape londonStationShape) reach() float64 {
	return math.Hypot(shape.lastBerthDepth()+20, 100)
}

// inArea reports whether the point is inside the rectangle or on its edge.
func inArea(point sim.Point, area [4]sim.Point) bool {
	sign := 0.0
	for index := range area {
		side := londonSegment{from: area[index], to: area[(index+1)%len(area)]}.side(point)
		switch {
		case side == 0:
		case sign == 0:
			sign = side
		case (side > 0) != (sign > 0):
			return false
		}
	}
	return true
}

// londonFootprint is the ground that one station heading takes.
type londonFootprint struct {
	// core holds the lanes from the diverge node to the merge node.
	core []londonSegment
	// roads holds the road-in and road-out lanes.
	roads []londonSegment
	// points holds the nodes of the core lanes.
	points []sim.Point
	area   [4]sim.Point
	// marker is the mean position of the berths.
	marker sim.Point
	// low and high are the corners of the box that holds the area and the
	// road lanes.
	low, high sim.Point
}

// apart reports whether the boxes of the two footprints are at least the
// given distance apart. Then no lane of one footprint can come nearer
// than that distance to a lane of the other footprint.
func (footprint londonFootprint) apart(other londonFootprint, distance float64) bool {
	return other.low.X-footprint.high.X >= distance || footprint.low.X-other.high.X >= distance ||
		other.low.Y-footprint.high.Y >= distance || footprint.low.Y-other.high.Y >= distance
}

// londonPreference is a heading that the search prefers, with the
// deviation that it adds when the station uses it.
type londonPreference struct {
	direction, penalty float64
}

// londonSite is one station in the heading search.
type londonSite struct {
	center sim.Point
	berths int
	// own is the index of the passenger station at center. For a Parking
	// facility, it is the gateway station.
	own                  int
	arrivals, departures []sim.Point
	preferred            []londonPreference
}

// shape returns the station shape of the site with the heading.
func (site londonSite) shape(direction float64) londonStationShape {
	return londonStationShape{center: site.center, direction: direction, berths: site.berths}
}

// footprint returns the lanes and the area of the station with the heading.
func (site londonSite) footprint(direction float64) londonFootprint {
	shape := site.shape(direction)
	nodes := shape.nodes()
	footprint := londonFootprint{
		core: []londonSegment{
			{from: nodes.diverge, to: nodes.entry},
			{from: nodes.entry, to: nodes.exit},
			{from: nodes.exit, to: nodes.merge},
		},
		points: []sim.Point{nodes.diverge, nodes.merge, nodes.entry, nodes.exit},
		area:   shape.area(),
	}
	previousArrival, previousDeparture := nodes.entry, nodes.exit
	for _, berth := range nodes.berths {
		footprint.core = append(footprint.core,
			londonSegment{from: previousArrival, to: berth.arrival},
			londonSegment{from: berth.departure, to: previousDeparture},
			londonSegment{from: berth.arrival, to: berth.berth},
			londonSegment{from: berth.berth, to: berth.departure},
		)
		footprint.points = append(footprint.points, berth.arrival, berth.berth, berth.departure)
		footprint.marker = add(footprint.marker, berth.berth)
		previousArrival, previousDeparture = berth.arrival, berth.departure
	}
	footprint.marker = scale(footprint.marker, 1/float64(len(nodes.berths)))
	for _, arrival := range site.arrivals {
		footprint.roads = append(footprint.roads, londonSegment{from: arrival, to: nodes.diverge})
	}
	for _, departure := range site.departures {
		footprint.roads = append(footprint.roads, londonSegment{from: nodes.merge, to: departure})
	}
	footprint.low, footprint.high = footprint.area[0], footprint.area[0]
	for _, points := range [][]sim.Point{footprint.area[:], site.arrivals, site.departures} {
		for _, point := range points {
			footprint.low = sim.Point{X: min(footprint.low.X, point.X), Y: min(footprint.low.Y, point.Y)}
			footprint.high = sim.Point{X: max(footprint.high.X, point.X), Y: max(footprint.high.Y, point.Y)}
		}
	}
	return footprint
}

// deviation returns the smallest deviation of the heading from a preferred
// heading, with the penalty of that preference.
func (site londonSite) deviation(direction float64) float64 {
	deviation := math.Inf(1)
	for _, preference := range site.preferred {
		deviation = min(deviation, angleBetween(direction, preference.direction)+preference.penalty)
	}
	if math.IsInf(deviation, 1) {
		return 0
	}
	return deviation
}

// angleBetween returns the angle between two directions, from 0 to pi.
func angleBetween(a, b float64) float64 {
	return math.Abs(math.Remainder(a-b, 2*math.Pi))
}

// londonGapBisector returns the direction that halves the widest free angle
// between the given directions. With one direction, it returns the opposite
// direction. With no direction, it returns 0.
func londonGapBisector(directions []float64) float64 {
	if len(directions) == 0 {
		return 0
	}
	sorted := make([]float64, 0, len(directions))
	for _, direction := range directions {
		sorted = append(sorted, math.Mod(math.Mod(direction, 2*math.Pi)+2*math.Pi, 2*math.Pi))
	}
	slices.Sort(sorted)
	widest, bisector := -1.0, 0.0
	for index, direction := range sorted {
		gap := sorted[(index+1)%len(sorted)] - direction
		if index == len(sorted)-1 {
			gap += 2 * math.Pi
		}
		if gap > widest {
			widest, bisector = gap, direction+gap/2
		}
	}
	return math.Mod(bisector, 2*math.Pi)
}

// londonStationPreferences returns the preferred headings of a passenger
// station with links to the given neighbor positions. The first is the
// bisector of the widest free angle between the links. A through station
// can also use the other side of its line.
func londonStationPreferences(center sim.Point, neighbors []sim.Point) []londonPreference {
	directions := make([]float64, 0, len(neighbors))
	for _, neighbor := range neighbors {
		directions = append(directions, math.Atan2(neighbor.Y-center.Y, neighbor.X-center.X))
	}
	bisector := londonGapBisector(directions)
	preferences := []londonPreference{{direction: bisector}}
	if len(neighbors) == 2 {
		preferences = append(preferences, londonPreference{direction: bisector + math.Pi, penalty: londonOppositePenalty})
	}
	return preferences
}

// londonHeadingInput holds the inputs of the heading search.
type londonHeadingInput struct {
	// sites holds the stations in search order.
	sites []londonSite
	// links holds the guideway lanes and the movement lanes.
	links []londonSegment
	// centers holds the source positions of the passenger stations.
	centers []sim.Point
}

// londonHeadingSearch holds the state of the heading search.
type londonHeadingSearch struct {
	londonHeadingInput
	headings   []float64
	footprints []londonFootprint
	// nearLinks holds, for each site, the links that can cross its lanes.
	nearLinks [][]londonSegment
	// nearSites holds, for each site, the other sites whose areas can
	// come nearer than londonStationGap to its area.
	nearSites [][]int
	// fixed holds, for each site and heading step, the part of the score
	// that does not depend on the other sites.
	fixed [][]float64
}

// londonHeading returns the direction of a heading step.
func londonHeading(step int) float64 {
	return 2 * math.Pi * float64(step) / londonHeadingCount
}

// searchLondonHeadings returns a heading for each site. Each site starts at
// its first preferred heading. Each pass then gives each site, in order,
// the heading with the lowest score for the current headings of the other
// sites. The score is the sum of londonHeadingSearch.fixedScore,
// londonHeadingSearch.siteScore, and the deviation from the nearest
// preferred heading in units of londonDeviationUnit.
func searchLondonHeadings(input londonHeadingInput) []float64 {
	search := newLondonHeadingSearch(input)
	for range londonHeadingPasses {
		if !search.pass() {
			break
		}
	}
	return search.headings
}

// newLondonHeadingSearch puts each site at its first preferred heading. It
// also finds the links and sites near each site and the fixed scores.
func newLondonHeadingSearch(input londonHeadingInput) *londonHeadingSearch {
	search := &londonHeadingSearch{
		londonHeadingInput: input,
		headings:           make([]float64, len(input.sites)),
		footprints:         make([]londonFootprint, len(input.sites)),
		nearLinks:          make([][]londonSegment, len(input.sites)),
		nearSites:          make([][]int, len(input.sites)),
		fixed:              make([][]float64, len(input.sites)),
	}
	for index, site := range input.sites {
		reach := site.shape(0).reach()
		if len(site.preferred) > 0 {
			search.headings[index] = site.preferred[0].direction
		}
		search.footprints[index] = site.footprint(search.headings[index])
		for _, link := range input.links {
			if link.distance(site.center) <= reach {
				search.nearLinks[index] = append(search.nearLinks[index], link)
			}
		}
		for other, near := range input.sites {
			if other != index && math.Hypot(near.center.X-site.center.X, near.center.Y-site.center.Y) <= reach+near.shape(0).reach()+londonStationGap {
				search.nearSites[index] = append(search.nearSites[index], other)
			}
		}
		search.fixed[index] = make([]float64, londonHeadingCount)
		for step := range londonHeadingCount {
			direction := londonHeading(step)
			search.fixed[index][step] = search.fixedScore(index, site.footprint(direction)) + site.deviation(direction)/londonDeviationUnit
		}
	}
	return search
}

// pass gives each site, in order, the heading with the lowest score. It
// reports whether a heading changed.
func (search *londonHeadingSearch) pass() bool {
	changed := false
	for index, site := range search.sites {
		best, bestScore := search.headings[index], math.Inf(1)
		var bestFootprint londonFootprint
		for step := range londonHeadingCount {
			direction := londonHeading(step)
			footprint := site.footprint(direction)
			// A tie keeps the first heading.
			if score := search.fixed[index][step] + search.siteScore(index, footprint); score < bestScore-1e-9 {
				best, bestScore, bestFootprint = direction, score, footprint
			}
		}
		if best != search.headings[index] {
			changed = true
		}
		search.headings[index], search.footprints[index] = best, bestFootprint
	}
	return changed
}

// fixedScore returns the score of a footprint for the site at index that
// does not depend on the other sites. Each of these adds
// londonConflictPenalty:
//
//   - a crossing of a core lane and a guideway or movement lane
//   - a passenger station, other than the own station, that is nearer to
//     the berths than the own station, or that is inside the area
//
// Each road lane shorter than londonShortRoad adds londonShortRoadPenalty.
func (search *londonHeadingSearch) fixedScore(index int, footprint londonFootprint) float64 {
	site := search.sites[index]
	conflicts := countCrossings(footprint.core, search.nearLinks[index])
	ownDistance := math.Hypot(footprint.marker.X-site.center.X, footprint.marker.Y-site.center.Y)
	for other, center := range search.centers {
		if other == site.own {
			continue
		}
		if math.Hypot(footprint.marker.X-center.X, footprint.marker.Y-center.Y) < ownDistance {
			conflicts++
		}
		if inArea(center, footprint.area) {
			conflicts++
		}
	}
	score := londonConflictPenalty * float64(conflicts)
	for _, road := range footprint.roads {
		if road.length() < londonShortRoad {
			score += londonShortRoadPenalty
		}
	}
	return score
}

// siteScore returns the score of a footprint for the site at index with the
// current footprints of the other sites. Each of these adds
// londonConflictPenalty:
//
//   - a core lane nearer than londonStationGap to a core lane of another
//     station
//   - a crossing of a core lane and a road lane of another station, or of a
//     road lane and a core lane of another station
//   - a core node inside the area of another station, or a core node of
//     another station inside this area
//
// Without this score, a Parking facility can lie on its gateway station.
// Pods in the two stations can then come too near each other, and the
// separation oracle fails. Without the gap, the lanes of a Parking facility
// can touch the lanes of its gateway station, and the map shows them as one
// station.
func (search *londonHeadingSearch) siteScore(index int, footprint londonFootprint) float64 {
	conflicts := 0
	for _, other := range search.nearSites[index] {
		near := search.footprints[other]
		if footprint.apart(near, londonStationGap) {
			continue
		}
		conflicts += countNear(footprint.core, near.core, londonStationGap) + countCrossings(footprint.core, near.roads) + countCrossings(footprint.roads, near.core)
		for _, point := range footprint.points {
			if inArea(point, near.area) {
				conflicts++
			}
		}
		for _, point := range near.points {
			if inArea(point, footprint.area) {
				conflicts++
			}
		}
	}
	return londonConflictPenalty * float64(conflicts)
}
