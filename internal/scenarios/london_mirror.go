package scenarios

// mirrorLondonStations keeps the chosen headings and berth positions.
// It reverses complete station layouts only when crossings decrease and
// the existing layout penalties do not increase. Ties keep the layout.
func mirrorLondonStations(input londonHeadingInput, headings []float64) []londonStationShape {
	if len(headings) != len(input.sites) {
		panic("station headings do not match sites")
	}
	search := newLondonHeadingSearch(input)
	shapes := make([]londonStationShape, len(input.sites))
	for index, site := range input.sites {
		shapes[index] = site.shape(headings[index]) // #nosec G602 -- The heading count matches the site count above.
		search.footprints[index] = site.shapeFootprint(shapes[index])
	}
	for range londonHeadingPasses {
		changed := false
		for index, site := range input.sites {
			candidate := shapes[index]
			candidate.mirrored = !candidate.mirrored
			current := search.footprints[index]
			next := site.shapeFootprint(candidate)
			if search.fixedScore(index, next) > search.fixedScore(index, current)+1e-9 ||
				search.siteScore(index, next) > search.siteScore(index, current)+1e-9 ||
				search.roadCrossings(index, next) >= search.roadCrossings(index, current) {
				continue
			}
			shapes[index], search.footprints[index], changed = candidate, next, true
		}
		if !changed {
			break
		}
	}
	return shapes
}

// roadCrossings counts crossings that involve the access roads of one
// station, including crossings with its own roads and core lanes.
func (search *londonHeadingSearch) roadCrossings(index int, footprint londonFootprint) int {
	count := countCrossings(footprint.roads, search.nearLinks[index]) + countCrossings(footprint.roads, footprint.core)
	for i, road := range footprint.roads {
		for _, other := range footprint.roads[i+1:] {
			if road.crosses(other) {
				count++
			}
		}
	}
	for _, other := range search.nearSites[index] {
		near := search.footprints[other]
		if !footprint.apart(near, 0) {
			count += countCrossings(footprint.roads, near.roads) + countCrossings(footprint.roads, near.core)
		}
	}
	return count
}
