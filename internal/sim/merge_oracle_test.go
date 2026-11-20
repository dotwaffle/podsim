package sim

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

// The tests of the body-and-ownership oracle of decision D6 of
// docs/design/shape-clearance.md (see lateralBodyCheck).

// oracleDegrees is the angle of the merge and split pairs of the oracle
// tests. At 6 degrees, the body range on each lane is about 33 m, and
// the lanes are 3.1 m apart about 30 m from the node. So a pod that stops
// 31 m from the node is in its extent, past the node range, and a pod that
// passes it on the other lane stays more than 3.1 m from it.
const oracleDegrees = 6

// oracleBand is the distance in meters from the node of a pod that only
// the junction orders against the other lane of a split pair: in the body
// range, past the node range, and with the lanes more than 3.1 m apart. It
// is also past the start of the second junction cell, at 30 m.
const oracleBand = 31.0

// oracleLaneLength is the length of the lanes of the pair.
const oracleLaneLength = 300.0

// oracleFaultSeconds is the duration of the faults of the oracle cases.
const oracleFaultSeconds = int64(20)

// oracleClassOrders holds the class of the first pod and of the pods on
// the other lane, in both orders: the two small classes, and Express and
// Group, each with a small class and with each other. An order with a
// large class runs only on largeOracleNetwork, where the lanes of the
// merge and split pairs admit each class and have the union body.
var oracleClassOrders = [][2]VehicleClass{
	{LegacyClass, CompactClass},
	{CompactClass, LegacyClass},
	{ExpressClass, CompactClass},
	{CompactClass, ExpressClass},
	{GroupClass, LegacyClass},
	{LegacyClass, GroupClass},
	{ExpressClass, GroupClass},
	{GroupClass, ExpressClass},
}

// largeOrder reports whether an order of oracleClassOrders has a large
// class.
func largeOrder(classes [2]VehicleClass) bool {
	return largeVehicleClass(classes[0]) || largeVehicleClass(classes[1])
}

// oracleVariant is a network of the oracle tests: oracleNetwork or
// largeOracleNetwork.
type oracleVariant struct {
	name  string
	large bool
}

// oracleVariants are the networks of the oracle tests.
var oracleVariants = []oracleVariant{
	{name: "shape"},
	{name: "shape large lanes", large: true},
}

// network returns the network of the variant.
func (v oracleVariant) network(t *testing.T, converging bool) Network {
	t.Helper()
	if v.large {
		return largeOracleNetwork(t, converging)
	}
	return oracleNetwork(t, converging)
}

// orders returns the class orders that run on the network of the variant.
func (v oracleVariant) orders() [][2]VehicleClass {
	return slices.DeleteFunc(slices.Clone(oracleClassOrders), func(classes [2]VehicleClass) bool { return largeOrder(classes) && !v.large })
}

// oracleNetwork returns the network of laneEndNetwork with lanes a and b
// of oracleLaneLength meters at oracleDegrees. The destination station has
// oracleBerths berths on an arc 100 m from the entry, and its exit is 600 m
// beyond the entry, so that the audit accepts the station.
func oracleNetwork(t *testing.T, converging bool) Network {
	t.Helper()
	network := laneEndNetwork(converging, oracleDegrees, oracleLaneLength)
	entry, _ := network.Node("dest-entry")
	exit := slices.IndexFunc(network.Nodes, func(node Node) bool { return node.ID == "dest-exit" })
	network.Nodes[exit].Position = Point{X: entry.Position.X + 600, Y: entry.Position.Y}
	berths := network.Stations[1].Berths[:0]
	for index := range network.Nodes {
		node := &network.Nodes[index]
		var berth int
		if _, err := fmt.Sscanf(node.ID, "dest-berth-%02d", &berth); err != nil {
			continue
		}
		if berth > oracleBerths {
			continue
		}
		radians := oracleBerthDegrees[berth-1] * math.Pi / 180
		node.Position = Point{X: entry.Position.X + 100*math.Cos(radians), Y: entry.Position.Y + 100*math.Sin(radians)}
		berths = append(berths, Berth{ID: fmt.Sprintf("dest-%02d", berth), Node: node.ID})
	}
	network.Stations[1].Berths = berths
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane Lane) bool {
		var berth int
		_, err := fmt.Sscanf(lane.ID, "dest-berth-%02d", &berth)
		return err == nil && berth > oracleBerths
	})
	network.Nodes = slices.DeleteFunc(network.Nodes, func(node Node) bool {
		var berth int
		_, err := fmt.Sscanf(node.ID, "dest-berth-%02d", &berth)
		return err == nil && berth > oracleBerths
	})
	if !mergePairOf(network) {
		t.Fatal("lanes a and b do not form a merge pair")
	}
	return network
}

// oracleBerthDegrees holds the angle of each berth of the destination
// station of oracleNetwork, from the line from the entry to the exit.
var oracleBerthDegrees = []float64{10, -10, 25, -25, 40, -40}

// oracleLargeClasses is the class set of each lane, station, and berth of
// largeOracleNetwork.
var oracleLargeClasses = mustClassSet("legacy", "compact", "group", "express")

// largeOracleNetwork returns the network of oracleNetwork, where each
// lane, station, and berth admits each class, so the merge and split pair
// of lanes a and b has the union body. The
// berths of the destination station are 200 m from the entry, 12 degrees
// apart, and the exit is 1200 m beyond the entry, so that the audit
// accepts the station at 20 m. A lane from the origin station to the start
// of the network gives the Express riders a path.
func largeOracleNetwork(t *testing.T, converging bool) Network {
	t.Helper()
	network := oracleNetwork(t, converging)
	entry, _ := network.Node("dest-entry")
	for index := range network.Nodes {
		node := &network.Nodes[index]
		var berth int
		if _, err := fmt.Sscanf(node.ID, "dest-berth-%02d", &berth); err == nil {
			radians := float64(12*((berth+1)/2)) * math.Pi / 180
			if berth%2 == 0 {
				radians = -radians
			}
			node.Position = Point{X: entry.Position.X + 200*math.Cos(radians), Y: entry.Position.Y + 200*math.Sin(radians)}
		}
		if node.ID == "dest-exit" {
			node.Position.X = entry.Position.X + 1200
		}
	}
	start := "p-start"
	if !converging {
		start = "feed-start"
	}
	network.Lanes = append(network.Lanes, Lane{ID: "origin-link", From: "origin-exit", To: start, SpeedLimit: 14})
	for index := range network.Lanes {
		network.Lanes[index].VehicleClasses = oracleLargeClasses
	}
	for index := range network.Stations {
		station := &network.Stations[index]
		station.VehicleClasses = oracleLargeClasses
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = oracleLargeClasses
		}
	}
	if !mergePairOf(network) {
		t.Fatal("lanes a and b do not form a merge pair")
	}
	return network
}

// oracleBerths is the number of berths of the destination station of
// oracleNetwork, and the largest number of pods of an oracle case.
var oracleBerths = len(oracleBerthDegrees)

// oraclePod is a pod that restoreMergeOracle places on the first lane of
// its route, stopped.
type oraclePod struct {
	class    VehicleClass
	route    []string
	distance float64
}

// oracleLayout places pods on a network of oracleNetwork, or of
// largeOracleNetwork when large is set.
type oracleLayout struct {
	converging, large bool
}

// before returns a pod gap meters before the node on its way to lane: on
// the lane of a merge, or on the feed lane of a split.
func (l oracleLayout) before(lane string, gap float64, class VehicleClass) oraclePod {
	if l.converging {
		return oraclePod{class: class, route: []string{lane, "exit", "approach"}, distance: oracleLaneLength - gap}
	}
	return oraclePod{class: class, route: []string{"feed", lane, splitNext(lane), "approach"}, distance: 3000 - gap}
}

// at returns a pod on lane, distance meters from the node.
func (l oracleLayout) at(lane string, distance float64, class VehicleClass) oraclePod {
	if l.converging {
		return l.before(lane, distance, class)
	}
	return oraclePod{class: class, route: []string{lane, splitNext(lane), "approach"}, distance: distance}
}

// splitNext returns the lane after a lane of the split.
func splitNext(lane string) string {
	if lane == "a" {
		return "d"
	}
	return "b2"
}

// stream returns three pods on their way to lane, the first first meters
// before the node, and step meters apart.
func (l oracleLayout) stream(lane string, first, step float64, class VehicleClass) []oraclePod {
	var pods []oraclePod
	for index := range 3 {
		pods = append(pods, l.before(lane, first+step*float64(index), class))
	}
	return pods
}

// other returns the stream on the other lane of a case whose first pod is
// near the node. It starts 75 m before the node, outside the junction
// cells of the merge. On a split, it shares the feed lane with the first
// pod.
func (l oracleLayout) other(lane string, class VehicleClass) []oraclePod {
	return l.stream(lane, 75, corridorQueueGap, class)
}

// restoreMergeOracle restores the pods on a network of oracleNetwork, each
// with a party to the destination station, with faults and emergencies on.
// Pod i is "p<i+1>". The restore must keep each pod in place.
func restoreMergeOracle(t *testing.T, network Network, pods []oraclePod) *Simulation {
	t.Helper()
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	for index, pod := range pods {
		id := fmt.Sprintf("p%d", index+1)
		fleet = append(fleet, Placement{ID: id, Class: pod.class, StationID: "dest", BerthID: fmt.Sprintf("dest-%02d", index+1)})
		state.RequestID++
		state.Boarded++
		var route []int
		for _, lane := range pod.route {
			route = append(route, laneIndex[lane])
		}
		state.Pods = append(state.Pods, SavedPod{
			ID: id, Class: pod.class, Activity: "traveling", Occupied: true, Origin: "origin-1", DestinationStation: "dest",
			Riders:    []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}},
			Boardings: []RiderBoarding{{BerthID: "origin-1"}},
			Stops:     []string{"dest"},
			Route:     route, LaneID: pod.route[0], LaneDistance: pod.distance, Distance: pod.distance,
		})
	}
	// An Express pod needs the Express order contract.
	if slices.ContainsFunc(pods, func(pod oraclePod) bool { return pod.class == ExpressClass }) {
		state.OrderContract = ExpressOrderContract
	}
	s, result, err := RestoreState(RestoreStateInput{OrderContract: state.OrderContract, Network: network, Fleet: fleet, State: state, FaultsOn: true, EmergenciesOn: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the pods did not restore in place: %+v", result)
	}
	return s
}

// oracleRun is the result of runMergeOracle: the first error of the
// oracle and the first error of Check, and the smallest distance between
// two pods on the lanes of a merge pair.
type oracleRun struct {
	oracle, check error
	closest       float64
}

// runMergeOracle steps s for the given seconds. After each step, it runs
// the oracle and Check, and it keeps the first error of each. It does not
// stop at an error, so that a test can see if Check fails later.
func runMergeOracle(s *Simulation, seconds int) oracleRun {
	run := oracleRun{closest: math.Inf(1)}
	for range seconds * TicksPerSecond {
		s.Step()
		if err := s.lateralBodyCheck(); err != nil && run.oracle == nil {
			run.oracle = err
		}
		if _, err := s.SafetyObservation().Check(); err != nil && run.check == nil {
			run.check = err
		}
		run.closest = min(run.closest, mergePairClosest(s))
	}
	return run
}

// mergePairClosest returns the smallest distance between two pods on the
// lanes of a merge pair, or +Inf.
func mergePairClosest(s *Simulation) float64 {
	closest := math.Inf(1)
	for i := range s.vehicles {
		a, ok := s.graph.lanes[s.vehicles[i].Pod.LaneID]
		if !ok {
			continue
		}
		for j := i + 1; j < len(s.vehicles); j++ {
			b, ok := s.graph.lanes[s.vehicles[j].Pod.LaneID]
			if ok && s.merges.mergePartner(a, b) {
				closest = min(closest, pointDistance(s.vehicles[i].Pod.Position, s.vehicles[j].Pod.Position))
			}
		}
	}
	return closest
}

// faultP1 faults pod p1 of s for oracleFaultSeconds.
func faultP1(s *Simulation) error {
	seconds := oracleFaultSeconds
	_, err := s.Fault(FaultRequest{PodID: "p1", DurationSeconds: &seconds})
	return err
}

// oracleCase is a case of the oracle tests. pods returns the pods of the
// case: p1 is the pod of the case on lane first, and the pods on lane
// other follow it. start, if not nil, runs before the first step.
type oracleCase struct {
	name  string
	pods  func(l oracleLayout, first, other string, classes [2]VehicleClass) []oraclePod
	start func(s *Simulation) error
}

// oracleCases are the cases of section 7 of the design: two streams, a
// stopped pod, a faulted pod, an emergency journey, and a pod on a join
// turn at the end of a lane.
var oracleCases = []oracleCase{
	{
		// The stream on the first lane comes to the node ahead of the
		// stream on the other lane. On a split, the pods of the two streams
		// alternate on the feed lane.
		name: "two streams",
		pods: func(l oracleLayout, first, other string, classes [2]VehicleClass) []oraclePod {
			if !l.converging {
				return append(l.stream(first, 50, 2*corridorQueueGap, classes[0]), l.stream(other, 50+corridorQueueGap, 2*corridorQueueGap, classes[1])...)
			}
			return append(l.stream(first, 50, corridorQueueGap, classes[0]), l.stream(other, 60, corridorQueueGap, classes[1])...)
		},
	},
	{
		// The faulted p1 holds the cells after the pods of the first lane,
		// so they stop. A pod with junction cells drives through them, so
		// on a merge, the pods stop on the lane after the node and at the
		// start of their junction cells, outside their extents, and the
		// stream on the other lane stops beside them. On a split, p2 stops
		// at the end of its junction cells, just past its extent. On a
		// merge of largeOracleNetwork, p1 is 60 m past the node, because
		// its retained tail of 20 m reaches the first cell of the exit
		// lane, which holds the node, from 45 m.
		name: "stopped pod",
		pods: func(l oracleLayout, first, other string, classes [2]VehicleClass) []oraclePod {
			if l.converging {
				exit := 45.0
				if l.large {
					exit = 60
				}
				pods := append([]oraclePod{{class: LegacyClass, route: []string{"exit", "approach"}, distance: exit}}, l.stream(first, 30, corridorQueueGap, classes[0])[:2]...)
				return append(pods, l.other(other, classes[1])...)
			}
			return append([]oraclePod{l.at(first, 80, LegacyClass), l.at(first, 20, classes[0])}, l.other(other, classes[1])...)
		},
		start: faultP1,
	},
	{
		// p1 is faulted in its extent, where on a split only the junction
		// orders it against the other lane.
		name: "faulted pod",
		pods: func(l oracleLayout, first, other string, classes [2]VehicleClass) []oraclePod {
			return append([]oraclePod{l.at(first, oracleBand, classes[0])}, l.other(other, classes[1])...)
		},
		start: faultP1,
	},
	{
		// p1 starts an emergency journey 40 m from the node, before the
		// node on a merge and after it on a split, so that the journey keeps
		// the lane. A pod on the first lane follows p1.
		name: "emergency journey",
		pods: func(l oracleLayout, first, other string, classes [2]VehicleClass) []oraclePod {
			pods := append([]oraclePod{l.at(first, 40, classes[0])}, l.other(other, classes[1])...)
			return append(pods, l.before(first, 210, classes[0]))
		},
		start: func(s *Simulation) error {
			_, err := s.Emergency("p1", 0)
			return err
		},
	},
	{
		// p1 is faulted at the node, at the end of the lane before the
		// node. The simulation publishes it at the start of the lane after
		// the node. On a split, that is the first lane, so the oracle takes
		// the join turn of p1 from the feed lane, and on the split, a pod
		// starts on the other lane past its junction cells. On a merge, the
		// lane after the node is not in a merge pair. A pod on the first
		// lane follows p1. The live run cannot show that the join turn
		// matters, because the junction keeps the bodies apart. The join
		// turn controls of TestMergeOracleControls show it.
		name: "join turn",
		pods: func(l oracleLayout, first, other string, classes [2]VehicleClass) []oraclePod {
			pods := append([]oraclePod{l.before(first, 0, classes[0])}, l.other(other, classes[1])...)
			pods = append(pods, l.before(first, 210, classes[0]))
			if !l.converging {
				pods = append(pods, l.at(other, 70, classes[1]))
			}
			return pods
		},
		start: faultP1,
	},
}

// TestMergeOracleCases runs the oracle and Check at each tick of each
// oracle case, on a merge pair and on a split pair, on each network of
// oracleVariants, with the pod of the case on lane a and on lane b, and in
// each class order that the network admits. Each run lasts 60 s, so the
// traffic flows again after each fault ends.
func TestMergeOracleCases(t *testing.T) {
	t.Parallel()
	for _, converging := range []bool{true, false} {
		for _, variant := range oracleVariants {
			for _, test := range oracleCases {
				for _, lanes := range [][2]string{{"a", "b"}, {"b", "a"}} {
					for _, classes := range variant.orders() {
						name := fmt.Sprintf("converging %v %s %s on %s %s then %s", converging, variant.name, test.name, lanes[0], classes[0], classes[1])
						t.Run(name, func(t *testing.T) {
							t.Parallel()
							network := variant.network(t, converging)
							s := restoreMergeOracle(t, network, test.pods(oracleLayout{converging: converging, large: variant.large}, lanes[0], lanes[1], classes))
							if test.start != nil {
								if err := test.start(s); err != nil {
									t.Fatal(err)
								}
							}
							run := runMergeOracle(s, 60)
							if run.oracle != nil {
								t.Error(run.oracle)
							}
							if run.check != nil {
								t.Error(run.check)
							}
							if math.IsInf(run.closest, 1) {
								t.Error("no tick had a pod on each lane of the pair, so the run does not test the oracle")
							}
							t.Logf("closest pods of the pair %.3f m", run.closest)
						})
					}
				}
			}
		}
	}
}

// TestMergeOracleRestoredContinuation saves a run of an oracle case after
// 3 s, restores the saved state, and runs the oracle and
// Check at each tick of 60 s more. The faulted pod of the split case is in
// its extent, where only the junction orders it, when the state is saved.
// The cases run on oracleNetwork with small pods, and on
// largeOracleNetwork with Express and Group pods.
// The cases do not fail before the save.
func TestMergeOracleRestoredContinuation(t *testing.T) {
	t.Parallel()
	faulted := slices.IndexFunc(oracleCases, func(c oracleCase) bool { return c.name == "faulted pod" })
	streams := slices.IndexFunc(oracleCases, func(c oracleCase) bool { return c.name == "two streams" })
	for _, test := range []struct {
		converging bool
		index      int
		variant    oracleVariant
		classes    [2]VehicleClass
	}{
		{converging: false, index: faulted, variant: oracleVariants[0], classes: oracleClassOrders[0]},
		{converging: true, index: streams, variant: oracleVariants[0], classes: oracleClassOrders[0]},
		{converging: false, index: faulted, variant: oracleVariants[1], classes: [2]VehicleClass{ExpressClass, CompactClass}},
		{converging: true, index: streams, variant: oracleVariants[1], classes: [2]VehicleClass{ExpressClass, CompactClass}},
		{converging: false, index: faulted, variant: oracleVariants[1], classes: [2]VehicleClass{GroupClass, ExpressClass}},
		{converging: true, index: streams, variant: oracleVariants[1], classes: [2]VehicleClass{CompactClass, GroupClass}},
	} {
		oracle := oracleCases[test.index]
		t.Run(fmt.Sprintf("converging %v %s %s %s then %s", test.converging, test.variant.name, oracle.name, test.classes[0], test.classes[1]), func(t *testing.T) {
			t.Parallel()
			network := test.variant.network(t, test.converging)
			s := restoreMergeOracle(t, network, oracle.pods(oracleLayout{converging: test.converging, large: test.variant.large}, "a", "b", test.classes))
			if oracle.start != nil {
				if err := oracle.start(s); err != nil {
					t.Fatal(err)
				}
			}
			if run := runMergeOracle(s, 3); run.oracle != nil || run.check != nil {
				t.Fatalf("before the save: oracle %v, Check %v", run.oracle, run.check)
			}
			// The JSON form of a saved state leaves out the members that the
			// session codec writes, such as the end of a fault, so the
			// restore takes the state as ExportState gives it.
			restored, result, err := RestoreState(RestoreStateInput{OrderContract: s.orderContract, Network: network, Fleet: s.initial, State: s.ExportState(), FaultsOn: true, EmergenciesOn: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
				t.Fatalf("the pods did not restore in place: %+v", result)
			}
			run := runMergeOracle(restored, 60)
			if run.oracle != nil {
				t.Error("after the restore:", run.oracle)
			}
			if run.check != nil {
				t.Error("after the restore:", run.check)
			}
			if math.IsInf(run.closest, 1) {
				t.Error("no tick had a pod on each lane of the pair, so the run does not test the oracle")
			}
			t.Logf("closest pods of the pair after the restore %.3f m", run.closest)
		})
	}
}

// oraclePlace puts pod i of s on a lane at a distance, traveling and
// stopped, as TestLateralMergeCheck does. The owners do not change.
func oraclePlace(s *Simulation, i int, lane string, distance float64) {
	pod := &s.vehicles[i].Pod
	index := s.graph.lanes[lane]
	pod.Activity, pod.BerthID, pod.StationID, pod.LaneID, pod.LaneDistance, pod.Speed = Traveling, "", "", lane, distance, 0
	pod.Position = pointAlong(s.network.Polyline(s.network.Lanes[index]), distance)
}

// pointAlong returns the point of a polyline at a distance along it.
func pointAlong(points []Point, distance float64) Point {
	for i := 1; i < len(points); i++ {
		length := pointDistance(points[i-1], points[i])
		if distance <= length || i == len(points)-1 {
			return lanePoint(points[i-1], points[i], length, min(distance, length))
		}
		distance -= length
	}
	return points[0]
}

// oracleControl is a case of TestMergeOracleControls: p1 on lane a at
// first, p2 on lane b at second, and the owner of the junction of node n,
// or "" for none. fail tells whether the oracle fails. With joinOnly, the
// bodies at the headings of the lanes are disjoint, so that only the join
// turn of p1 can fail the oracle. A shortExtent that is not zero ends the
// extent of lane b against lane a there, in a copy of the merge table of
// the simulation, so that p2 is outside its extent.
type oracleControl struct {
	name           string
	network        Network
	first, second  float64
	owner          string
	fail, joinOnly bool
	shortExtent    float64
}

// TestMergeOracleControls places two pods by hand, in each class order,
// and checks the result of the oracle and of Check. On the split of
// oracleNetwork, p1 is on lane a at oracleBand, and p2 is on lane b 7.1 m
// nearer to the node, or 6.5 m on largeOracleNetwork, so that a small body
// on lane a also meets the large body on lane b. The bodies overlap, but
// the pods are more than 4.0 m apart, so Check passes. Each overlap fails the oracle, also when p1 holds
// the junction and a short extent puts p2 outside it. On a merge with an exit lane that turns 90
// degrees left, p1 is at the end of lane a, and p2 is 6.5 m before the
// node on lane b, which comes in from the right. Only the join turn of p1
// into the exit lane brings its body over the body of p2. The small orders
// run on oracleNetwork, and each order runs on
// largeOracleNetwork.
func TestMergeOracleControls(t *testing.T) {
	t.Parallel()
	for _, variant := range oracleVariants {
		for _, classes := range variant.orders() {
			for _, test := range oracleControls(t, variant) {
				t.Run(fmt.Sprintf("%s %s %s then %s", variant.name, test.name, classes[0], classes[1]), func(t *testing.T) {
					t.Parallel()
					checkOracleControl(t, test, classes)
				})
			}
		}
	}
}

// oracleControls returns the cases of TestMergeOracleControls on the
// networks of a variant.
func oracleControls(t *testing.T, variant oracleVariant) []oracleControl {
	t.Helper()
	turn := variant.network(t, true)
	for index := range turn.Nodes {
		switch node := &turn.Nodes[index]; node.ID {
		case "b-start":
			node.Position = Point{Y: -oracleLaneLength}
		case "m":
			node.Position = Point{Y: 1200}
		}
	}
	split := variant.network(t, false)
	near := 7.1
	if variant.large {
		near = 6.5
	}
	return []oracleControl{
		{name: "overlap without an owner", network: split, first: oracleBand, second: oracleBand - near, fail: true},
		{name: "overlap with the other pod in its extent", network: split, first: oracleBand, second: oracleBand - near, owner: "p1", fail: true},
		{name: "disjoint bodies without an owner", network: split, first: oracleBand, second: oracleBand + 9},
		// The owner holds the junction and p2 is outside its extent, so the
		// accept rule of the design passes the pair, and hides the short
		// extent. The strict oracle fails it.
		{name: "overlap with a short extent", network: split, first: oracleBand, second: oracleBand - near, owner: "p1", fail: true, shortExtent: 20},
		{name: "join turn without an owner", network: turn, first: oracleLaneLength, second: oracleLaneLength - 6.5, fail: true, joinOnly: true},
		{name: "join turn with the other pod in its extent", network: turn, first: oracleLaneLength, second: oracleLaneLength - 6.5, owner: "p1", fail: true, joinOnly: true},
		{name: "join turn with disjoint bodies", network: turn, first: oracleLaneLength, second: oracleLaneLength - 20, owner: "p1"},
	}
}

// oracleBodyText is the size of the grown body of each class in the text
// of the oracle: 7.2 m by 3.1 m for a small class, and 8.0 m by 4.0 m for a
// large class.
func oracleBodyText(class VehicleClass) string {
	if largeVehicleClass(class) {
		return "8.000 by 4.000 m"
	}
	return "7.200 by 3.100 m"
}

// checkOracleControl runs a case of TestMergeOracleControls with p1 of the
// first class and p2 of the second class.
func checkOracleControl(t *testing.T, test oracleControl, classes [2]VehicleClass) {
	t.Helper()
	if !mergePairOf(test.network) {
		t.Fatal("lanes a and b do not form a merge pair")
	}
	var contract OrderContract
	if slices.Contains(classes[:], ExpressClass) {
		contract = ExpressOrderContract
	}
	s, err := NewFleetWithOrderContract(test.network, []Placement{{ID: "p1", Class: classes[0], StationID: "dest", BerthID: "dest-01"}, {ID: "p2", Class: classes[1], StationID: "dest", BerthID: "dest-02"}}, contract)
	if err != nil {
		t.Fatal(err)
	}
	oraclePlace(s, 0, "a", test.first)
	oraclePlace(s, 1, "b", test.second)
	if test.owner != "" {
		s.owners[resource{kind: junctionResource, id: "n"}] = podResourceOwner(test.owner)
	}
	if _, err = s.SafetyObservation().Check(); err != nil {
		t.Fatalf("Check: %v", err)
	}
	a, b := s.graph.lanes["a"], s.graph.lanes["b"]
	if test.shortExtent != 0 {
		indexes := *s.networkIndexes
		merges := *indexes.merges
		merges.extents = maps.Clone(merges.extents)
		start, _, _ := merges.mergeExtent(b, a)
		merges.extents[[2]int{b, a}] = [2]float64{start, test.shortExtent}
		indexes.merges, s.networkIndexes = &merges, &indexes
		if test.second <= test.shortExtent {
			t.Fatalf("p2 at %v m is in the short extent", test.second)
		}
	}
	if test.joinOnly {
		first, second := s.vehicles[0].Pod, s.vehicles[1].Pod
		static := func(pod Pod, lane int) [4]Point {
			return oracleClassBody(pod.Class).enclosure(s.podPoses(pod.Position, lane, pod.LaneDistance)[0])
		}
		if !oracleSeparated(static(first, a), static(second, b)) {
			t.Fatal("the bodies at the headings of the lanes overlap, so the case does not test the join turn")
		}
		if poses := s.podPoses(first.Position, a, first.LaneDistance); len(poses) != 2 || poses[1].turn == 0 {
			t.Fatalf("poses %+v at the end of lane a, want the pose of the lane and the join turn", poses)
		}
	}
	start, end, _ := s.merges.mergeExtent(b, a)
	t.Logf("extent of b [%.3f, %.3f]", start, end)
	err = s.lateralBodyCheck()
	if failed := err != nil; failed != test.fail {
		t.Fatalf("oracle error %v, want an error %v", err, test.fail)
	}
	if err == nil {
		return
	}
	if test.shortExtent != 0 && !strings.Contains(err.Error(), fmt.Sprintf("on lane b at %.3f m, extent [0.000, %.3f], outside true, holds the junction false", test.second, test.shortExtent)) {
		t.Errorf("oracle error %q does not show p2 outside the short extent", err)
	}
	for _, want := range []string{
		"tick 0:", "node n", fmt.Sprintf("junction owner %q", test.owner),
		fmt.Sprintf("pod p1 (%s) on lane a", classes[0]), fmt.Sprintf("pod p2 (%s) on lane b", classes[1]),
		"pose (", "heading ", oracleBodyText(classes[0]), oracleBodyText(classes[1]),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("oracle error %q does not name %q", err, want)
		}
	}
}
