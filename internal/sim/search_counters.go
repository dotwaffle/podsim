package sim

// searchCounters counts the graph searches of a simulation by kind. The
// tests and the benchmarks of the station choice read them (sections 14.1
// and 15 of the incident emergency contract). No rule reads them, they are
// not saved, and Reset keeps them.
type searchCounters struct {
	// graph counts the shortest-path searches of searchRoute. Each part of
	// a bank route is one search. static counts the shortest-path searches
	// on the static graph.
	graph, static int64
	// localFailed counts the same-bank local attempts of a bank route that
	// found no path.
	localFailed int64
	// routes counts the calls of searchRoute, and failed the calls that
	// returned an error.
	routes, failed int64
	// trees counts the pruning trees of the station choice. choices counts
	// the station choices that searched, and memoHits the choices that the
	// no-candidate memo answered with no search.
	trees, choices, memoHits int64
}

// countRoute counts one call of searchRoute with its error.
// graphs and localFailed are the searches and the failed local attempts
// that the call made.
func (c *searchCounters) countRoute(err error, graphs, localFailed int64) {
	c.routes++
	c.graph += graphs
	c.localFailed += localFailed
	if err != nil {
		c.failed++
	}
}
