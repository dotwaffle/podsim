package sim

import (
	"cmp"
	"math"
	"slices"
)

const (
	// emergencyChoiceTicks is the cadence of the station choice of a
	// deferred, divertible pod (section 5.4 of the incident emergency
	// contract).
	emergencyChoiceTicks = 60
	// pruneTermLimit bounds the terms of the float sums of a pruned choice
	// (section 9.1, pruning step 6). Above it, the choice does not prune.
	pruneTermLimit = 90_000
)

// emergencyMiss is an entry of the no-candidate memo: the station choice
// of the record with the serial found no candidate from the divert node
// with the node index from, for a pod of the class (section 9.1 of the
// incident emergency contract). The candidates depend only on these
// inputs, the network, and the blocked set, and a change of either of the
// last two clears the memo.
type emergencyMiss struct {
	serial uint64
	from   int
	class  VehicleClass
}

// choiceKey orders the candidate pairs of the station choice: the
// estimate in whole ticks, rounded up, then the station index, then the
// berth index. It is a total order on the pairs.
type choiceKey struct {
	tick           int64
	station, berth int
}

// compareChoiceKeys compares two keys in the order of choiceKey.
func compareChoiceKeys(a, b choiceKey) int {
	return cmp.Or(cmp.Compare(a.tick, b.tick), cmp.Compare(a.station, b.station), cmp.Compare(a.berth, b.berth))
}

// stationCandidate is a candidate pair of the station choice, with its key
// and the suffix of its route.
type stationCandidate struct {
	key     choiceKey
	station Station
	berth   Berth
	suffix  []Lane
}

// estimateCursor is the state of an estimate along a route. acc is the
// cost so far. Before the lane of the position, distance is the part of
// the position that the lanes so far do not hold, and placed is false.
type estimateCursor struct {
	acc      float64
	distance float64
	placed   bool
}

// stationChoice holds the inputs of one station choice for pod v. The
// candidate routes start at from and follow the kept prefix of v.
type stationChoice struct {
	s         *Simulation
	v         *vehicle
	from      string
	prefix    int
	discharge []float64
	// base is the estimate cursor at the end of the prefix. Its acc is the
	// prefix estimate P.
	base estimateCursor
	best stationCandidate
	// found reports that best holds a candidate.
	found bool
}

// stationVisit is a station in the visit order of a pruned choice, with
// the lower bound of its estimates.
type stationVisit struct {
	station int
	bound   float64
	low     int64
}

// chooseEmergencyStation runs step 7 of the advance (section 5.4 of the
// incident emergency contract) for the deferred record pod, which is not
// at a berth. On the start tick and on each cadence tick after it, a
// traveling pod that can divert gets the station choice of section 9.1,
// and the chosen pair binds the pod with the interrupt mask. The choice
// and the installation read one routing view, so the installed route is
// the candidate route. The choice writes no state other than the
// no-candidate memo. A refused installation changes nothing, and the pod
// stays deferred.
func (s *Simulation) chooseEmergencyStation(record emergencyRecord, mask uint32) {
	v := &s.vehicles[record.pod]
	if (s.tick-record.start)%emergencyChoiceTicks != 0 {
		return
	}
	prefix, from, ok := s.divertStart(v)
	if !ok {
		return
	}
	s.ensureNetworkIndexes()
	miss := emergencyMiss{serial: record.serial, from: s.graph.nodes[from], class: v.Pod.Class}
	if slices.Contains(s.emergencyMisses, miss) {
		s.searchCounters.memoHits++
		return
	}
	defer s.leaveRouteView(s.enterRouteView(v))
	choice, found := s.chooseStation(v, prefix, from, true)
	s.dropEmergencyMiss(record.serial)
	if !found {
		s.emergencyMisses = append(s.emergencyMisses, miss)
		return
	}
	_ = s.setOperationalDestination(v, operationalTarget{purpose: opEmergencyUnload, owner: emergencyHold, interrupt: mask, station: choice.station.ID, berth: choice.berth.ID})
}

// dropEmergencyMiss removes the memo entry of the record with the serial.
func (s *Simulation) dropEmergencyMiss(serial uint64) {
	s.emergencyMisses = slices.DeleteFunc(s.emergencyMisses, func(miss emergencyMiss) bool { return miss.serial == serial })
}

// chooseStation returns the candidate pair with the lowest key over the
// passenger stations, for pod v with the kept prefix and the divert node
// from (section 9.1 of the incident emergency contract). It reports false
// when no station has a candidate. With prune, it visits the stations in
// the order of their lower bounds, and it stops when no later station can
// win, when the conditions of the bound hold. Its result is the result of
// the exhaustive scan. The caller sets the routing view of v.
func (s *Simulation) chooseStation(v *vehicle, prefix int, from string, prune bool) (stationCandidate, bool) {
	s.searchCounters.choices++
	choice := stationChoice{s: s, v: v, from: from, prefix: prefix, discharge: s.routeDischarge(v)}
	choice.base = choice.advance(estimateCursor{distance: v.distance}, v.Route[:prefix])
	visits, pruned := choice.visitOrder(prune)
	for _, visit := range visits {
		if pruned && choice.found && visit.low > choice.best.key.tick {
			break
		}
		choice.evaluate(visit.station)
	}
	return choice.best, choice.found
}

// visitOrder returns the passenger stations in the order of the visits,
// and reports whether the stop rule applies. The exhaustive order is the
// network order. A pruned order holds only the stations that the tree
// reaches, in increasing (bound, station index) order. The choice does
// not prune when prune is false, when the term guard fails, when the
// position is past the end of the prefix, or when the tree refuses the
// start node.
func (c *stationChoice) visitOrder(prune bool) ([]stationVisit, bool) {
	s := c.s
	terms := 4*c.prefix + 7*len(s.network.Nodes) + 2*len(s.vehicles)
	// Without a lane of the prefix that holds the position, the first
	// lane of each suffix is the lane of the position. Its part of the
	// lane is all of it only at the start of the lane.
	positioned := c.base.placed || c.base.distance == 0
	graph := s.routingGraph()
	start, ok := graph.nodes[c.from]
	if !prune || terms > pruneTermLimit || !positioned || !ok || !graph.nodeAllows(start, c.v.Pod.Class) {
		var visits []stationVisit
		for index, station := range s.network.Stations {
			if !station.ParkingOnly {
				visits = append(visits, stationVisit{station: index})
			}
		}
		return visits, false
	}
	var targets []string
	var owners []int
	for index, station := range s.network.Stations {
		if station.ParkingOnly {
			continue
		}
		for _, berth := range s.choiceBerths(c.v, station) {
			targets = append(targets, station.Berths[berth].Node)
			owners = append(owners, index)
		}
	}
	s.searchCounters.trees++
	nearest := make([]float64, len(s.network.Stations))
	for index := range nearest {
		nearest[index] = math.Inf(1)
	}
	for index, target := range s.network.routeTargets(routeTargetsInput{class: c.v.Pod.Class, from: c.from, to: targets}, graph) {
		if target.err == nil {
			nearest[owners[index]] = min(nearest[owners[index]], target.seconds)
		}
	}
	var visits []stationVisit
	for index, seconds := range nearest {
		if math.IsInf(seconds, 1) {
			continue
		}
		bound := c.base.acc + seconds
		low := bound*(1-1e-9) - 1e-9
		visits = append(visits, stationVisit{station: index, bound: bound, low: int64(math.Ceil(low * TicksPerSecond))})
	}
	slices.SortFunc(visits, func(a, b stationVisit) int {
		return cmp.Or(cmp.Compare(a.bound, b.bound), cmp.Compare(a.station, b.station))
	})
	return visits, true
}

// choiceBerths returns the indexes in station.Berths of the berths of the
// entry groups of the station for pod v: the berths that allow the class
// of the pod and that no fault blocks, in berth order.
func (s *Simulation) choiceBerths(v *vehicle, station Station) []int {
	var berths []int
	for index, berth := range station.Berths {
		if berthAllows(station, berth, v.Pod.Class) && !s.berthBlocked(berth) {
			berths = append(berths, index)
		}
	}
	return berths
}

// evaluate finds the candidate of each entry group of the station, and
// keeps the best key. A bank entry is one group, and a station without
// banks is one group. A group tries the berths that are available to the
// pod first, and then the others, each part in berth order. Its candidate
// is the first berth with a route. A failed route only moves the search
// to the next berth.
func (c *stationChoice) evaluate(stationIndex int) {
	station := c.s.network.Stations[stationIndex]
	berths := c.s.choiceBerths(c.v, station)
	var entries []string
	for _, berth := range berths {
		if entry := station.berthEntry(station.Berths[berth]); !slices.Contains(entries, entry) {
			entries = append(entries, entry)
		}
	}
	for _, entry := range entries {
		var group []int
		for _, available := range []bool{true, false} {
			for _, berth := range berths {
				b := station.Berths[berth]
				if station.berthEntry(b) == entry && c.s.berthAvailableTo(c.v, b) == available {
					group = append(group, berth)
				}
			}
		}
		c.evaluateGroup(stationIndex, station, group)
	}
}

// evaluateGroup tries the berths of a group in order, and keeps the first
// berth with a route when its key is the best so far. A route that leaves
// the pod where it is cannot bind it, so it does not count as a route.
func (c *stationChoice) evaluateGroup(stationIndex int, station Station, group []int) {
	for _, berth := range group {
		suffix, err := c.s.assignedRoute(c.v, c.from, station.Berths[berth].Node)
		if err != nil || c.prefix+len(suffix) == 0 {
			continue
		}
		estimate := c.advance(c.base, suffix).acc
		key := choiceKey{tick: int64(math.Ceil(estimate * TicksPerSecond)), station: stationIndex, berth: berth}
		if !c.found || compareChoiceKeys(key, c.best.key) < 0 {
			c.best = stationCandidate{key: key, station: station, berth: station.Berths[berth], suffix: suffix}
			c.found = true
		}
		return
	}
}

// advance returns the cursor after the lanes, by the estimate of section
// 9.1 of the incident emergency contract, the cost function of queueCost
// from the position of the pod. A lane before the position costs nothing.
// The lane of the position costs the part of its edge seconds after the
// position, plus queueHeadwaySeconds for each pod ahead on the lane that
// queueDischarge counts. Each later lane costs its edge seconds plus the
// queue delay at the cost so far.
func (c *stationChoice) advance(cursor estimateCursor, lanes []Lane) estimateCursor {
	graph := c.s.graph
	for _, lane := range lanes {
		index := graph.lanes[lane.ID]
		seconds := graph.edges[index].seconds
		if cursor.placed {
			delay := 0.0
			if c.discharge != nil {
				delay = queueDelay(c.discharge[index], cursor.acc)
			}
			cursor.acc += seconds + delay
			continue
		}
		length := c.s.laneLength(lane)
		if cursor.distance >= length {
			cursor.distance -= length
			continue
		}
		cursor.acc += (length-cursor.distance)/length*seconds + queueHeadwaySeconds*float64(c.queueAhead(lane.ID, cursor.distance))
		cursor.placed = true
	}
	return cursor
}

// queueAhead returns the number of pods on the lane past the distance
// that queueDischarge counts. The pod of the choice does not count.
func (c *stationChoice) queueAhead(lane string, distance float64) int {
	count := 0
	for index := range c.s.vehicles {
		other := &c.s.vehicles[index]
		pod := &other.Pod
		if other != c.v && pod.LaneID == lane && pod.LaneDistance > distance && pod.Activity == Traveling && pod.WaitReason != NoWait && pod.Speed < queueStoppedSpeed {
			count++
		}
	}
	return count
}
