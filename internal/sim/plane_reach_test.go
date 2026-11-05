package sim

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"
)

// laneReachLocations lists the locations of the reach of a lane: the rule
// that planeReach answers without the list.
func laneReachLocations(lane Lane, incident map[string][]Lane, origins map[string][]SafetyLocation) []SafetyLocation {
	locations := []SafetyLocation{{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}}
	for _, node := range []string{lane.From, lane.To} {
		for _, other := range incident[node] {
			locations = append(locations, SafetyLocation{SeparationGroup: other.SeparationGroup, From: other.From, To: other.To})
		}
	}
	return append(locations, origins[lane.From]...)
}

func berthLocationsByNode(n Network) map[string][]SafetyLocation {
	locations := make(map[string][]SafetyLocation)
	for _, station := range n.Stations {
		for _, berth := range station.Berths {
			locations[berth.Node] = append(locations[berth.Node], SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node})
		}
	}
	return locations
}

// randomReachNetwork returns two stars and some random lanes. The stars have
// more members than a part keeps without a bit set when spokes is large.
// Blank node IDs, blank groups, loops and repeated lane IDs are for the
// editor, which reads a network that is not valid.
func randomReachNetwork(random *rand.Rand, spokes int) Network {
	var n Network
	group := func(prefix string) string {
		switch random.IntN(12) {
		case 0:
			return ""
		case 1:
			return "shared"
		}
		return fmt.Sprintf("%s-%d", prefix, random.IntN(spokes+2))
	}
	node := func(prefix string) string {
		if random.IntN(40) == 0 {
			return ""
		}
		return fmt.Sprintf("%s-%d", prefix, random.IntN(spokes+2))
	}
	for hub, prefix := range []string{"a", "b"} {
		if random.IntN(3) == 0 {
			// A star where every group of the prefix can stay clear of the
			// other star, so that a pair can be separated.
			group = func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, random.IntN(spokes+2)) }
		}
		for spoke := range spokes {
			n.Lanes = append(n.Lanes, Lane{ID: fmt.Sprintf("%s-lane-%d", prefix, spoke), From: fmt.Sprintf("hub-%d", hub), To: node(prefix), SeparationGroup: group(prefix)})
		}
		for spoke := range 3 {
			from, to := node(prefix), node(prefix)
			n.Lanes = append(n.Lanes, Lane{ID: fmt.Sprintf("%s-free-%d", prefix, random.IntN(spoke+1)), From: from, To: to, SeparationGroup: group(prefix)})
		}
		station := Station{ID: prefix}
		for berth := range 3 {
			station.Berths = append(station.Berths, Berth{ID: fmt.Sprintf("%s-berth-%d", prefix, berth), Node: node(prefix), SeparationGroup: group(prefix)})
		}
		station.Berths = append(station.Berths, Berth{ID: prefix + "-hub-berth", Node: fmt.Sprintf("hub-%d", hub), SeparationGroup: group(prefix)})
		n.Stations = append(n.Stations, station)
	}
	return n
}

// The index gives the result of the rule for each lane and berth pair.
func TestPlaneReachMatchesLocations(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(1, 2))
	counts := map[bool]int{}
	bitSets := 0
	for trial := range 400 {
		spokes := 1 + random.IntN(12)
		if trial%20 == 0 {
			spokes = 80 + random.IntN(40)
		}
		n := randomReachNetwork(random, spokes)
		incident, origins := incidentLanes(n), berthLocationsByNode(n)
		reach := newPlaneReach(n)
		type path struct {
			key       separationKey
			locations []SafetyLocation
		}
		var paths []path
		for index, lane := range n.Lanes {
			paths = append(paths, path{laneKey(index), laneReachLocations(lane, incident, origins)})
		}
		index := 0
		for _, station := range n.Stations {
			for _, berth := range station.Berths {
				paths = append(paths, path{berthKey(index), berthGeometryLocations(berth, incident[berth.Node])})
				index++
			}
		}
		for _, parts := range reach.lanes {
			for _, part := range parts {
				if part != nil && part.bits != nil {
					bitSets++
				}
			}
		}
		for _, first := range paths {
			for _, second := range paths {
				want := envelopeLocationsSeparated(first.locations, second.locations)
				if got := reach.path(first.key).separated(reach.path(second.key)); got != want {
					t.Fatalf("trial %d: %v and %v: separated = %v, want %v", trial, first.key, second.key, got, want)
				}
				counts[want]++
			}
		}
	}
	if counts[true] == 0 || counts[false] == 0 || bitSets == 0 {
		t.Fatalf("the trials did not reach each branch: %v, %d bit sets", counts, bitSets)
	}
}

// Two hubs with bit sets differ in one member only. The other parts of the
// tested lanes do not have this member, so only the bit sets can find it.
func TestPlaneReachBitSets(t *testing.T) {
	t.Parallel()
	large := largeGeometryClasses(t, "group")
	for name, edit := range map[string]func(Network){
		"no member in common": func(Network) {},
		"one group in common": func(n Network) { n.Lanes[150].SeparationGroup = n.Lanes[50].SeparationGroup },
		"one node in common":  func(n Network) { n.Lanes[150].To = n.Lanes[50].To },
	} {
		n := planeReachHubs(large, 2, 100, planeReachGroup("distinct"))
		edit(n)
		reach := newPlaneReach(n)
		first, second := reach.path(laneKey(1)), reach.path(laneKey(101))
		if first[0].bits == nil || second[0].bits == nil {
			t.Fatalf("%s: a hub has no bit set", name)
		}
		incident, origins := incidentLanes(n), berthLocationsByNode(n)
		want := envelopeLocationsSeparated(laneReachLocations(n.Lanes[1], incident, origins), laneReachLocations(n.Lanes[101], incident, origins))
		if want != (name == "no member in common") {
			t.Fatalf("%s: the rule gives %v", name, want)
		}
		if got := first.separated(second); got != want {
			t.Errorf("%s: separated = %v, want %v", name, got, want)
		}
	}
}

// The index of the editor reads the first lane of an ID, and it refuses a
// large lane that the network does not have.
func TestLaneClearancePlanesSeparatedLookup(t *testing.T) {
	t.Parallel()
	large, small := largeGeometryClasses(t, "group"), largeGeometryClasses(t, "legacy")
	n := Network{Lanes: []Lane{
		{ID: "one", From: "a", To: "b", VehicleClasses: large, SeparationGroup: "P"},
		{ID: "two", From: "c", To: "d", VehicleClasses: large, SeparationGroup: "Q"},
		// A repeat of "two" that is on plane P.
		{ID: "two", From: "e", To: "f", VehicleClasses: large, SeparationGroup: "P"},
	}}
	clearance := NewLaneClearance(n)
	if !clearance.PlanesSeparated(n.Lanes[0], n.Lanes[2]) {
		t.Error("the repeated lane ID did not read the first lane")
	}
	if clearance.PlanesSeparated(n.Lanes[1], Lane{ID: "absent", VehicleClasses: large}) {
		t.Error("a large lane that is not in the network got the exemption")
	}
	if !clearance.PlanesSeparated(Lane{ID: "absent", VehicleClasses: small}, Lane{ID: "gone", VehicleClasses: small}) {
		t.Error("a small pair did not keep the exemption")
	}
	if !(LaneClearance{}).PlanesSeparated(n.Lanes[0], n.Lanes[1]) {
		t.Error("the zero value did not exempt the pair")
	}
}

// allocated returns the bytes that one call of run allocates. The test that
// calls it must not be parallel.
func allocated(run func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	run()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A hub of 2,000 lanes costs no reach work when no pair needs it, and at
// most the size of the network when pairs do. Before the index, the hub took
// 640 MB, and every lane of two hubs held a list of a thousand locations.
// The counts do not depend on the load of the host, as a time would.
func TestPlaneReachHubBound(t *testing.T) {
	// The hub audit ceiling is 1.15 times the measured value, because a built
	// index adds about 1.6 MB to it. The others are twice the measured values.
	const (
		planeReachHubAuditBytes = 5_300_000
		planeReachIndexBytes    = 3_200_000
		planeReachTwoHubsBytes  = 12_200_000
	)
	small, large := largeGeometryClasses(t, "legacy", "compact"), largeGeometryClasses(t, "legacy", "compact", "group")
	for _, groups := range []string{"", "distinct"} {
		smallHub := planeReachHubs(small, 1, 2000, planeReachGroup(groups))
		clearance := NewLaneClearance(smallHub)
		if !clearance.PlanesSeparated(smallHub.Lanes[0], smallHub.Lanes[1]) {
			t.Errorf("groups %q: a small pair lost the exemption", groups)
		}
		if clearance.reach.index != nil {
			t.Errorf("groups %q: a small network built the reach index", groups)
		}
		largeHub := planeReachHubs(large, 1, 2000, planeReachGroup(groups))
		if clearance := NewLaneClearance(largeHub); clearance.reach.index != nil {
			t.Errorf("groups %q: the index of a large network built reach before a pair needed it", groups)
		}
		bytes := allocated(func() {
			if err := largeHub.ValidateBankGeometry(); err != nil {
				t.Error(err)
			}
		})
		// The audit of a hub reads no reach, because each pair shares the hub.
		if bytes > planeReachHubAuditBytes {
			t.Errorf("groups %q: the audit of the hub allocated %d bytes", groups, bytes)
		}
	}
	two := planeReachHubs(large, 2, 1000, planeReachGroup("distinct"))
	var index *planeReach
	bytes := allocated(func() { index = newPlaneReach(two) })
	if bytes > planeReachIndexBytes {
		t.Errorf("the index of two hubs allocated %d bytes", bytes)
	}
	members := 0
	seen := map[*reachPart]bool{}
	for _, parts := range slices.Concat(index.lanes, index.berths) {
		for _, part := range parts {
			if part != nil && !seen[part] {
				seen[part] = true
				members += len(part.members)
			}
		}
	}
	if limit := 6 * len(two.Lanes); members > limit {
		t.Errorf("the index of two hubs has %d members, more than %d", members, limit)
	}
	bytes = allocated(func() {
		if err := two.ValidateBankGeometry(); err != nil {
			t.Error(err)
		}
	})
	if bytes > planeReachTwoHubsBytes {
		t.Errorf("the audit of two hubs allocated %d bytes", bytes)
	}
}
