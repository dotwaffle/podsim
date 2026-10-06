package sim

import (
	"fmt"
	"math"
	"testing"
)

const (
	corridorFeedLength = 3000.0
	corridorExitLength = 6000.0
	// corridorApproachLength keeps the destination station far enough away
	// that every pod passes the measure point before the first pod arrives.
	corridorApproachLength = 4000.0
	corridorStreamPods     = 20
	// corridorQueueGap is the distance between two stopped pods of a queue.
	corridorQueueGap = 45.0
	// corridorSkip is the number of pods that pass the measure point before
	// the measured headways start. It excludes the start of the queue.
	corridorSkip = 5
	// corridorPlatoonGap is the distance between two stopped pods of one
	// platoon. It is more than the largest link clearance of the corridor,
	// 16.98 m at the 90 degree merge.
	corridorPlatoonGap = 18.0
)

// mergeCorridor returns a main lane, an exit lane and an approach lane in a
// straight line, with a destination station at the end of the approach lane.
// The lane lengths set the track cells, so a change of length changes the
// headways. With a side lane, a
// second feed lane of the same length meets the main lane at the merge node
// at sideDegrees. A small origin station, not connected to the corridor,
// gives the requests a passenger origin. All lanes have a 14 m/s limit.
func mergeCorridor(side bool, sideDegrees float64) Network {
	network := Network{
		Nodes: []Node{
			{ID: "main-start", Position: Point{X: -corridorFeedLength}},
			{ID: "merge"},
			{ID: "exit-end", Position: Point{X: corridorExitLength}},
			{ID: "dest-entry", Position: Point{X: corridorExitLength + corridorApproachLength}},
			{ID: "dest-exit", Position: Point{X: corridorExitLength + corridorApproachLength + 200}},
			{ID: "origin-entry", Position: Point{X: -corridorFeedLength, Y: 1000}},
			{ID: "origin-berth", Position: Point{X: -corridorFeedLength + 100, Y: 1060}},
			{ID: "origin-exit", Position: Point{X: -corridorFeedLength + 200, Y: 1000}},
		},
		Lanes: []Lane{
			{ID: "main", From: "main-start", To: "merge", SpeedLimit: 14},
			{ID: "exit", From: "merge", To: "exit-end", SpeedLimit: 14},
			{ID: "approach", From: "exit-end", To: "dest-entry", SpeedLimit: 14},
			{ID: "dest-through", From: "dest-entry", To: "dest-exit", SpeedLimit: 14},
			{ID: "origin-through", From: "origin-entry", To: "origin-exit", SpeedLimit: 14},
			{ID: "origin-in", From: "origin-entry", To: "origin-berth", SpeedLimit: 14},
			{ID: "origin-out", From: "origin-berth", To: "origin-exit", SpeedLimit: 14},
		},
		Stations: []Station{
			{ID: "origin", Name: "Origin", Entry: "origin-entry", Exit: "origin-exit", Berths: []Berth{{ID: "origin-1", Node: "origin-berth"}}},
			{ID: "dest", Name: "Destination", Entry: "dest-entry", Exit: "dest-exit"},
		},
	}
	// The destination berths hold the initial fleet. The restore moves each
	// pod to its queue, so no pod uses these berths during the test.
	for index := range 2 * corridorStreamPods {
		node := fmt.Sprintf("dest-berth-%02d", index+1)
		network.Nodes = append(network.Nodes, Node{ID: node, Position: Point{X: corridorExitLength + corridorApproachLength + 100, Y: 40 + 25*float64(index)}})
		network.Lanes = append(network.Lanes,
			Lane{ID: node + "-in", From: "dest-entry", To: node, SpeedLimit: 14},
			Lane{ID: node + "-out", From: node, To: "dest-exit", SpeedLimit: 14})
		network.Stations[1].Berths = append(network.Stations[1].Berths, Berth{ID: fmt.Sprintf("dest-%02d", index+1), Node: node})
	}
	if side {
		radians := sideDegrees * math.Pi / 180
		start := Point{X: -corridorFeedLength * math.Cos(radians), Y: -corridorFeedLength * math.Sin(radians)}
		network.Nodes = append(network.Nodes, Node{ID: "side-start", Position: start})
		network.Lanes = append(network.Lanes, Lane{ID: "side", From: "side-start", To: "merge", SpeedLimit: 14})
	}
	return network
}

type corridorCase struct {
	name        string
	side        bool
	sideDegrees float64
	// streams lists the feed lanes that hold a stopped queue.
	streams []string
	// queueHead is the position of the first pod of each queue on its feed
	// lane.
	queueHead float64
	// onMain measures the headway at a point 2,000 m along the main lane.
	// Otherwise the test measures it at the merge node, with the node pass
	// records that the compare command reads.
	onMain bool
	// want is the pinned mean headway in seconds.
	want float64
	// platoonLimit turns on virtual platoons with this limit. 0 keeps
	// platooning off.
	platoonLimit int
	// platoonQueue restores each queue as platoons of platoonLimit pods,
	// corridorPlatoonGap apart in a platoon and corridorQueueGap apart
	// between two platoons. Otherwise all pods are corridorQueueGap apart.
	platoonQueue bool
}

type corridorResult struct {
	// headway is the mean time in seconds between two pods at the measure
	// point, after corridorSkip pods.
	headway float64
	// order lists the feed lane of each pod in the order in which the pods
	// enter the exit lane.
	order []string
	// coupled counts the pods that were in a platoon at some tick, and
	// largest is the largest platoon.
	coupled, largest int
}

// runMergeCorridor restores a stopped queue of corridorStreamPods pods on
// each stream lane. Each pod carries a party to the destination station. It
// runs until each pod enters the exit lane, and checks separation and berth
// use at each tick.
func runMergeCorridor(t *testing.T, test corridorCase) corridorResult {
	t.Helper()
	network := mergeCorridor(test.side, test.sideDegrees)
	laneIndex := make(map[string]int, len(network.Lanes))
	for index, lane := range network.Lanes {
		laneIndex[lane.ID] = index
	}
	var fleet []Placement
	state := SavedState{SharedRidePartyLimit: 1}
	stream := make(map[string]string)
	for streamIndex, feed := range test.streams {
		distance := test.queueHead
		for position := range corridorStreamPods {
			id := fmt.Sprintf("p%02d", streamIndex*corridorStreamPods+position+1)
			fleet = append(fleet, Placement{ID: id, StationID: "dest", BerthID: fmt.Sprintf("dest-%02d", len(fleet)+1)})
			state.RequestID++
			state.Boarded++
			leader := ""
			if position > 0 {
				gap := corridorQueueGap
				if test.platoonQueue && position%test.platoonLimit != 0 {
					gap, leader = corridorPlatoonGap, state.Pods[len(state.Pods)-1].ID
				}
				distance -= gap
			}
			state.Pods = append(state.Pods, SavedPod{
				ID: id, Activity: "traveling", Occupied: true, Origin: "origin-1", DestinationStation: "dest",
				Riders: []SavedRequest{{SharingConsent: SharedConsent, Service: OnDemandService, ID: state.RequestID, From: "origin", To: "dest", PartySize: 1, PodID: id}},
				Stops:  []string{"dest"},
				Route:  []int{laneIndex[feed], laneIndex["exit"], laneIndex["approach"]}, LaneID: feed, LaneDistance: distance, Distance: distance,
			})
			if leader != "" {
				route := []string{feed, "exit", "approach"}
				state.Pods[len(state.Pods)-1].Platoon = savedRunLink(network, route, route, leader, 0)
			}
			stream[id] = feed
		}
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("the queue did not restore in place: %+v", result)
	}
	s.SetExperimentRecords(true)
	platoons := newPlatoonMonitor(s)
	platoons.ownerTicks = TicksPerSecond / 4
	if test.platoonLimit != 0 {
		if err := s.SetPlatoonLimit(test.platoonLimit); err != nil {
			t.Fatal(err)
		}
		if err := s.SetPlatooning(PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
	}
	measured, entered := make(map[string]bool), make(map[string]bool)
	var ticks []int64
	var order []string
	for range 900 * TicksPerSecond {
		s.Step()
		snapshot := s.Snapshot()
		checkTraffic(t, snapshot)
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatalf("tick %d: %v", snapshot.Tick, err)
		}
		if test.platoonLimit != 0 {
			platoons.check(t)
		}
		for _, v := range snapshot.Vehicles {
			if v.Pod.LaneID == "main" && v.Pod.LaneDistance >= 2000 && !measured[v.Pod.ID] {
				measured[v.Pod.ID] = true
				ticks = append(ticks, snapshot.Tick)
			}
			if v.Pod.LaneID == "exit" && !entered[v.Pod.ID] {
				entered[v.Pod.ID] = true
				order = append(order, stream[v.Pod.ID])
			}
		}
		if len(entered) == len(fleet) {
			break
		}
	}
	if len(entered) != len(fleet) {
		t.Fatalf("%d of %d pods entered the exit lane", len(entered), len(fleet))
	}
	if !test.onMain {
		ticks = ticks[:0]
		for _, pass := range s.NodePasses() {
			if pass.Node == "merge" {
				ticks = append(ticks, pass.Tick)
			}
		}
	}
	if len(ticks) != len(fleet) {
		t.Fatalf("%d pods passed the measure point, want %d", len(ticks), len(fleet))
	}
	return corridorResult{headway: meanHeadway(ticks), order: order, coupled: len(platoons.coupled), largest: platoons.largest}
}

// meanHeadway returns the mean time in seconds between two ticks after
// corridorSkip ticks, or 0 when there are too few ticks.
func meanHeadway(ticks []int64) float64 {
	last := len(ticks) - 1
	if last <= corridorSkip {
		return 0
	}
	return float64(ticks[last]-ticks[corridorSkip]) / float64(last-corridorSkip) / TicksPerSecond
}

// TestMergeCorridorHeadway pins the saturation headway of a stopped queue on
// a synthetic corridor with 30 m cells. A platoon or cell change must
// compare its headway with these values. See the platoon screening in
// docs/qualification.md.
func TestMergeCorridorHeadway(t *testing.T) {
	t.Parallel()
	skipLong(t)
	feedHead := corridorFeedLength - 100
	cases := []corridorCase{
		{name: "straight lane", streams: []string{"main"}, queueHead: 1000, onMain: true, want: 6.011},
		{name: "one stream through a lane boundary", streams: []string{"main"}, queueHead: feedHead, want: 7.217},
		{name: "one stream through a 30 degree merge", side: true, sideDegrees: 30, streams: []string{"main"}, queueHead: feedHead, want: 7.217},
		{name: "two streams, 90 degree merge", side: true, sideDegrees: 90, streams: []string{"main", "side"}, queueHead: feedHead, want: 8.650},
		{name: "two streams, 30 degree merge", side: true, sideDegrees: 30, streams: []string{"main", "side"}, queueHead: feedHead, want: 8.650},
		{name: "two streams, 15 degree merge", side: true, sideDegrees: 15, streams: []string{"main", "side"}, queueHead: feedHead, want: 10.783},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := runMergeCorridor(t, test)
			t.Logf("headway %.3f s (%.0f pods/h), order %v", got.headway, 3600/got.headway, got.order)
			if math.Abs(got.headway-test.want) > 0.01 {
				t.Errorf("headway %.3f s, want %.3f s", got.headway, test.want)
			}
		})
	}
}

// TestMergeCorridorPlatoonHeadway pins the saturation headway of the merge
// corridor with virtual platoons of 2 and 4 pods. Each queue starts as
// platoons that are coupled and close, as a queue with platoons is. The
// design estimates the gain of platoons of 4 as 2.4 times the flow of
// single pods on a lane and 2.7 times at a 30 degree merge. See the
// platoon screening in docs/qualification.md.
func TestMergeCorridorPlatoonHeadway(t *testing.T) {
	t.Parallel()
	skipLong(t)
	feedHead := corridorFeedLength - 100
	// Each case holds the pinned headways for platoons of 2 and of 4.
	cases := []struct {
		corridorCase
		want [2]float64
	}{
		{corridorCase{name: "straight lane", streams: []string{"main"}, queueHead: 1000, onMain: true}, [2]float64{3.758, 2.415}},
		{corridorCase{name: "one stream through a lane boundary", streams: []string{"main"}, queueHead: feedHead}, [2]float64{4.120, 2.517}},
		{corridorCase{name: "two streams, 90 degree merge", side: true, sideDegrees: 90, streams: []string{"main", "side"}, queueHead: feedHead}, [2]float64{5.014, 3.131}},
		{corridorCase{name: "two streams, 30 degree merge", side: true, sideDegrees: 30, streams: []string{"main", "side"}, queueHead: feedHead}, [2]float64{4.925, 2.991}},
		{corridorCase{name: "two streams, 15 degree merge", side: true, sideDegrees: 15, streams: []string{"main", "side"}, queueHead: feedHead}, [2]float64{6.017, 3.511}},
	}
	for _, platoonCase := range cases {
		for index, limit := range []int{2, 4} {
			test := platoonCase.corridorCase
			test.name = fmt.Sprintf("%s, platoons of %d", test.name, limit)
			test.platoonLimit, test.platoonQueue, test.want = limit, true, platoonCase.want[index]
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()
				got := runMergeCorridor(t, test)
				t.Logf("headway %.3f s (%.0f pods/h), largest platoon %d", got.headway, 3600/got.headway, got.largest)
				if got.largest != limit || math.Abs(got.headway-test.want) > 0.01 {
					t.Errorf("headway %.3f s with platoons of up to %d, want %.3f s with platoons of %d", got.headway, got.largest, test.want, limit)
				}
			})
		}
	}
}
