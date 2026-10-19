package scenarios

import (
	"cmp"
	"math"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// londonLayoutTrips returns trips for each depot and each Compact station.
// Trips from the stations near a depot take pods out of the depot. Then
// trips into the two nearest stations fill their berths, so that idle pods
// there go to a depot. Last come trips to and from each Compact station.
func londonLayoutTrips(config project.Config, compact []string) [][2]string {
	at := make(map[string]sim.Point)
	for _, node := range config.Network.Nodes {
		at[node.ID] = node.Position
	}
	entry := func(id string) sim.Point {
		station, _ := config.Network.Station(id)
		return at[station.Entry]
	}
	var others []string
	for _, station := range config.Network.Stations {
		if !station.ParkingOnly && !slices.Contains(compact, station.ID) {
			others = append(others, station.ID)
		}
	}
	var trips [][2]string
	for _, depot := range config.Network.Depots {
		origin := entry(depot.Station)
		distance := func(id string) float64 {
			p := entry(id)
			return math.Hypot(p.X-origin.X, p.Y-origin.Y)
		}
		near := slices.Clone(others)
		slices.SortStableFunc(near, func(a, b string) int {
			return cmp.Compare(distance(a), distance(b))
		})
		for index := range 12 {
			trips = append(trips, [2]string{near[index%6], near[len(near)-1-index]})
		}
		for index := range 12 {
			trips = append(trips, [2]string{near[len(near)-20-index], near[index%2]})
		}
	}
	for index, id := range compact {
		trips = append(trips,
			[2]string{others[index*7%len(others)], id},
			[2]string{others[(index+3)*7%len(others)], id},
			[2]string{id, others[(index+5)*7%len(others)]})
	}
	return trips
}

// TestLondonLayoutRun runs LondonCentral, which has three depots and seven
// Compact stations. Each depot releases pods from its lane heads and takes
// new pods into its own slots. Different maneuver groups move pods at the
// same time, and a group never moves two pods at once. A physical restore
// every five minutes keeps every pod.
func TestLondonLayoutRun(t *testing.T) {
	t.Parallel()
	compact := suggestedCompact(londonVariantPresets[0])
	config := LondonCentral()
	groups := make(map[string]string)
	for _, group := range config.Network.ManeuverGroups {
		for _, lane := range group.LaneIDs {
			groups[lane] = group.ID
		}
	}
	depotOf := depotLanes(config.Network)
	slots := make(map[string]string)
	heads := make(map[string]bool)
	for _, depot := range config.Network.Depots {
		for _, lane := range depot.Lanes {
			heads[lane.BerthIDs[0]] = true
			for _, berth := range lane.BerthIDs {
				slots[berth] = depot.Station
			}
		}
	}
	// parked holds the depot of each pod inside a depot, and berth holds
	// the depot slot of its last rest.
	parked := make(map[string]string)
	berth := make(map[string]string)
	for _, pod := range config.Fleet {
		parked[pod.ID], berth[pod.ID] = slots[pod.BerthID], pod.BerthID
	}
	trips := londonLayoutTrips(config, compact)
	arrived := make(map[string]int)
	released := make(map[string]int)
	visited := make(map[string]bool)
	together := 0
	s := newSimulation(t, config)
	for second := range 30 * 60 {
		if second < len(trips) {
			if err := s.RequestTrip(trips[second][0], trips[second][1]); err != nil {
				t.Fatal(err)
			}
		}
		for range sim.TicksPerSecond {
			s.Step()
		}
		checkScaleSafety(t, s.SafetyObservation())
		moving := make(map[string]int)
		for _, vehicle := range s.Snapshot().Vehicles {
			pod := vehicle.Pod
			if group, ok := groups[pod.LaneID]; ok {
				moving[group]++
				visited[group] = true
				if moving[group] > 1 {
					t.Fatalf("second %d: group %s has two pods on its lanes", second, group)
				}
			}
			// A pod is inside a depot on a depot lane or at a depot slot.
			// A pod between a lane and a berth has neither for a tick.
			var depot string
			switch {
			case pod.LaneID != "":
				depot = depotOf[pod.LaneID]
			case pod.BerthID != "":
				depot = slots[pod.BerthID]
				if depot != "" {
					berth[pod.ID] = pod.BerthID
				}
			default:
				continue
			}
			switch was := parked[pod.ID]; {
			case depot != "" && was == "":
				arrived[depot]++
			case was != "" && depot == "":
				if !heads[berth[pod.ID]] {
					t.Fatalf("second %d: pod %s leaves %s from slot %s, not a lane head", second, pod.ID, was, berth[pod.ID])
				}
				released[was]++
			}
			parked[pod.ID] = depot
		}
		together = max(together, len(moving))
		if second%300 == 299 {
			s, _ = checkLondonRestore(t, s, config)
		}
	}
	for _, depot := range config.Network.Depots {
		if arrived[depot.Station] == 0 || released[depot.Station] == 0 {
			t.Errorf("%s: %d arrivals and %d releases, want both", depot.Station, arrived[depot.Station], released[depot.Station])
		}
	}
	if len(visited) != len(compact) || together < 2 {
		t.Errorf("%d of %d groups moved pods, at most %d at once", len(visited), len(compact), together)
	}
	if got := s.Snapshot().Completed; got != len(trips) {
		t.Errorf("%d of %d trips completed", got, len(trips))
	}
}
