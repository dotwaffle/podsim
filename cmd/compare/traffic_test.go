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
