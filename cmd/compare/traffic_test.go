package main

import (
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestSampleWaits samples two snapshots of five pods. In each snapshot,
// three pods are stopped: one for junction traffic, one for a pod ahead,
// and one for a berth. The pod at 0.2 m/s is not stopped, and the pod with
// no wait reason does not wait.
func TestSampleWaits(t *testing.T) {
	t.Parallel()
	pod := func(reason sim.WaitReason, speed float64) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{WaitReason: reason, Speed: speed}}
	}
	vehicles := []sim.Vehicle{
		pod(sim.JunctionOccupied, 0),
		pod(sim.TrackOccupied, 0.05),
		pod(sim.TrackOccupied, 0.2),
		pod(sim.BerthOccupied, 0),
		pod(sim.NoWait, 0),
	}
	var waits trafficWaits
	waits.sampleWaits(vehicles)
	waits.sampleWaits(vehicles)
	if want := (trafficWaits{stopped: 6, junction: 2, track: 2}); waits != want {
		t.Fatalf("waits = %+v, want %+v", waits, want)
	}
}

// smallBurstInput gives a hub burst on the small qualification ring, where
// pods queue at the hub.
func smallBurstInput(t *testing.T) runInput {
	t.Helper()
	caseStudy, err := loadScenario(smallProject(t), "")
	if err != nil {
		t.Fatal(err)
	}
	schedule := demandSchedule(scheduleInput{
		seed: 1, durationTicks: durationTicks(2 * time.Minute), intervalTicks: durationTicks(5 * time.Second),
		pattern: "hub-burst", burstSize: 12, passengers: caseStudy.passengers, focus: caseStudy.focus,
	})
	return runInput{
		policy: "off", duration: 10 * time.Minute, arrivalsFor: 2 * time.Minute, requestEvery: 5 * time.Second, seed: 1,
		pattern: "hub-burst", queueLimit: len(schedule), burstSize: 12, sharingLimit: 1, routingPolicy: "free-flow",
		schedule: schedule, scenario: caseStudy,
	}
}

// replayEachTick makes the simulation calls of run for input, and it calls
// observe with the snapshot after each tick.
func replayEachTick(t *testing.T, input runInput, observe func(sim.Snapshot)) {
	t.Helper()
	simulation, err := sim.NewFleet(input.scenario.network, input.scenario.fleet)
	if err != nil {
		t.Fatal(err)
	}
	weights, err := demandWeights(input.pattern, input.band, input.scenario)
	if err != nil {
		t.Fatal(err)
	}
	if err := simulation.SetDemandWeights(weights); err != nil {
		t.Fatal(err)
	}
	next := 0
	for tick := range durationTicks(input.duration) {
		for next < len(input.schedule) && input.schedule[next].tick == tick {
			if err := simulation.RequestTrip(input.schedule[next].origin, input.schedule[next].destination); err != nil {
				t.Fatal(err)
			}
			next++
		}
		simulation.Step()
		observe(simulation.Snapshot())
	}
}

// TestWaitColumnsInRun checks the columns of run against the stopped pods
// of a replay at each whole second.
func TestWaitColumnsInRun(t *testing.T) {
	t.Parallel()
	input := smallBurstInput(t)
	outcome, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	var want trafficWaits
	replayEachTick(t, input, func(state sim.Snapshot) {
		if state.Tick%sim.TicksPerSecond != 0 {
			return
		}
		for _, vehicle := range state.Vehicles {
			if vehicle.Pod.WaitReason == sim.NoWait || vehicle.Pod.Speed >= 0.1 {
				continue
			}
			want.stopped++
			want.junction += boolCount(vehicle.Pod.WaitReason == sim.JunctionOccupied)
			want.track += boolCount(vehicle.Pod.WaitReason == sim.TrackOccupied)
		}
	})
	got := trafficWaits{
		stopped: int(outcome.StoppedPodSeconds), junction: int(outcome.JunctionWaitSeconds), track: int(outcome.TrackWaitSeconds),
	}
	if got != want || want.junction == 0 || want.track == 0 {
		t.Fatalf("wait columns = %+v, replay = %+v", got, want)
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

// TestNodeFlowWindow checks the 60 s window and the tie rule. Node x has
// passes at 1 s, 2 s and 61 s, so the most in one window is 2. Node z has
// passes at 0 s and 60 s, which are not in the same window. Node y and
// node w each have 3 passes in one window, and w has the lower ID.
func TestNodeFlowWindow(t *testing.T) {
	t.Parallel()
	pass := func(node string, second int64) sim.NodePass {
		return sim.NodePass{Tick: second * sim.TicksPerSecond, Node: node}
	}
	passes := []sim.NodePass{pass("z", 0), pass("x", 1), pass("x", 2), pass("z", 60), pass("x", 61)}
	if peak, node := peakNodeFlow(passes); peak != 2 || node != "x" {
		t.Fatalf("peak = %d at %q, want 2 at x", peak, node)
	}
	passes = append(passes, pass("y", 100), pass("y", 100), pass("y", 159), pass("w", 300), pass("w", 310), pass("w", 320))
	if peak, node := peakNodeFlow(passes); peak != 3 || node != "w" {
		t.Fatalf("peak = %d at %q, want 3 at w", peak, node)
	}
	if peak, node := peakNodeFlow(nil); peak != 0 || node != "" {
		t.Fatalf("peak without passes = %d at %q", peak, node)
	}
}

// TestNodeFlowInRun checks the columns of run against a replay that
// examines the lane of each pod at each tick. A lane is at least 24 m long,
// so at each tick a pod can enter at most one lane. The replay counts the
// passes in each window directly.
func TestNodeFlowInRun(t *testing.T) {
	t.Parallel()
	input := smallBurstInput(t)
	outcome, err := run(input)
	if err != nil {
		t.Fatal(err)
	}
	laneFrom := make(map[string]string)
	for _, lane := range input.scenario.network.Lanes {
		laneFrom[lane.ID] = lane.From
	}
	type pass struct {
		node string
		tick int64
	}
	var passes []pass
	last := make(map[string]string)
	replayEachTick(t, input, func(state sim.Snapshot) {
		for _, vehicle := range state.Vehicles {
			lane := vehicle.Pod.LaneID
			if lane != "" && lane != last[vehicle.Pod.ID] {
				passes = append(passes, pass{node: laneFrom[lane], tick: state.Tick})
			}
			last[vehicle.Pod.ID] = lane
		}
	})
	peak, peakNode := 0, ""
	for _, end := range passes {
		count := 0
		for _, other := range passes {
			if other.node == end.node && other.tick <= end.tick && other.tick > end.tick-60*sim.TicksPerSecond {
				count++
			}
		}
		if count > peak || (count == peak && end.node < peakNode) {
			peak, peakNode = count, end.node
		}
	}
	if outcome.PeakNodeThroughputPerMinute != peak || outcome.PeakNode != peakNode || peak < 2 {
		t.Fatalf("peak node = %d at %q, replay = %d at %q", outcome.PeakNodeThroughputPerMinute, outcome.PeakNode, peak, peakNode)
	}
	t.Logf("peak node = %d at %q", peak, peakNode)
}
