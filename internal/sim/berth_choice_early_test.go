package sim

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestPlatoonEarlyBerthChoice checks when a pod on the first approach
// chooses its berth before the usual point. The entry lane starts 1500 m
// along its route. With platoons on, the pod chooses at its first
// admission when the entry lane starts within platoonHorizon, and not 1 m
// further back. With platoons off, it does not choose early.
func TestPlatoonEarlyBerthChoice(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		ahead float64
		mode  Platooning
		early bool
	}{
		{ahead: 299, mode: PlatooningVirtual, early: true},
		{ahead: 301, mode: PlatooningVirtual},
		{ahead: 299, mode: PlatooningOff},
	} {
		t.Run(fmt.Sprint(test.ahead, test.mode), func(t *testing.T) {
			t.Parallel()
			s := restoreEntry(t, entryNetwork(londonEntry, 1), []entryPod{{approach: 1, distance: 1500 - test.ahead}})
			if err := s.SetPlatooning(test.mode); err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[0]
			if start := v.blocks.lanes[2].start; start != 1500 {
				t.Fatalf("the entry lane starts at %v", start)
			}
			s.Step()
			if early := v.destination.ID != ""; early != test.early {
				t.Fatalf("the pod has the berth %q after the first step, want an early choice %v", v.destination.ID, test.early)
			}
		})
	}
}

// TestPlatoonEarlyBerthChoiceNeverRefuses puts debris on the first arrival
// link of the station, so the berth choice of the pod fails. The early
// tries do not refuse the pod: it travels on to the usual point of the
// berth choice, where its next request starts on the final lane of its
// route. There the choice fails, and the pod stops on the entry lane with
// no berth.
func TestPlatoonEarlyBerthChoiceNeverRefuses(t *testing.T) {
	t.Parallel()
	s := restoreEntry(t, entryNetwork(londonEntry, 1), []entryPod{{approach: 1, distance: 1250}})
	s.faultsOn = true
	from, to := 10.0, 11.0
	if _, err := s.Fault(FaultRequest{LaneID: "dest-01-arrival-link", FromMeters: &from, ToMeters: &to}); err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	for range 60 * TicksPerSecond {
		s.Step()
		if v.destination.ID != "" {
			t.Fatalf("tick %d: the pod has the berth %s", s.tick, v.destination.ID)
		}
	}
	if next := v.reservedThrough + 1; v.Pod.Speed != 0 || v.Pod.LaneID != "dest-access-in" || v.blocks.lane(next).ID != "dest-access-in" {
		t.Fatalf("the pod is on %s at %v m/s, and its next request starts on %s", v.Pod.LaneID, v.Pod.Speed, v.blocks.lane(next).ID)
	}
}

// largeEntryNetwork is entryNetwork with the stations, their berths, the
// upstream lanes, and the lanes of the berths open to the Group class.
func largeEntryNetwork(t *testing.T, fleet int) Network {
	t.Helper()
	network := entryNetwork(entryShape{join: 30, approach: 150, large: true}, fleet)
	classes := largeGeometryClasses(t, "legacy", "group")
	for index := range network.Lanes {
		if lane := &network.Lanes[index]; lane.StationID == "dest" || lane.ID == "u1" || lane.ID == "u2" || strings.HasPrefix(lane.ID, "origin-berth-") {
			lane.VehicleClasses = classes
		}
	}
	for index := range network.Stations {
		network.Stations[index].VehicleClasses = classes
		for berth := range network.Stations[index].Berths {
			network.Stations[index].Berths[berth].VehicleClasses = classes
		}
	}
	return network
}

// TestPlatoonEarlyBerthChoiceLargeClasses checks that pods of the Group
// class, with no small pods, choose their berths at the usual point with
// platoons on: at the same tick and the same berth as with platoons off.
// The Express class has the same rule, but its physical profile needs the
// Express order contract and Express services, so these tests use the
// Group class only.
func TestPlatoonEarlyBerthChoiceLargeClasses(t *testing.T) {
	t.Parallel()
	type choice struct {
		tick  int64
		berth string
	}
	run := func(mode Platooning) []choice {
		s := restoreEntry(t, largeEntryNetwork(t, 2), []entryPod{{approach: 1, distance: 1250, class: GroupClass}, {approach: 2, distance: 1150, class: GroupClass}})
		if err := s.SetPlatooning(mode); err != nil {
			t.Fatal(err)
		}
		choices := make([]choice, len(s.vehicles))
		for s.completed < len(s.vehicles) {
			s.Step()
			for i := range s.vehicles {
				if v := &s.vehicles[i]; choices[i].berth == "" && v.destination.ID != "" {
					choices[i] = choice{tick: s.tick, berth: v.destination.ID}
				}
			}
			if s.tick > 600*TicksPerSecond {
				t.Fatalf("%d trips complete", s.completed)
			}
		}
		return choices
	}
	on, off := run(PlatooningVirtual), run(PlatooningOff)
	for index := range on {
		if on[index] != off[index] || on[index].tick < 10*TicksPerSecond {
			t.Fatalf("pod %d chooses %+v with platoons on, %+v with platoons off", index+1, on[index], off[index])
		}
	}
}

// TestPlatoonEarlyBerthChoiceMixedFleet queues six pods on each approach
// with platoons on. The second pod of each queue is of the Group class.
// The small pods choose early, and the large pods do not. The Group pods
// arrive, and the monitors pass at each tick for 600 seconds. A Group pod
// has no lane back to the origin, so it stays at its berth, and a small
// pod that chose that berth waits for it, as with platoons off. Every pod
// arrives, and the monitors pass at each tick. The test does not compare
// the outcome with platoons off: an early choice of a small pod changes
// the station load that a large pod reads.
func TestPlatoonEarlyBerthChoiceMixedFleet(t *testing.T) {
	t.Parallel()
	pods := append(approachQueue(1, 6, 1250, 45), approachQueue(2, 6, 1250, 45)...)
	for index := range pods {
		if index%6 == 1 {
			pods[index].class = GroupClass
		}
	}
	s := restoreEntry(t, largeEntryNetwork(t, len(pods)), pods)
	watch := newEntryWatch(s)
	group := []*vehicle{&s.vehicles[1], &s.vehicles[7]}
	watch.until(t, s, "the arrival of the Group pods", func() bool {
		return !slices.ContainsFunc(group, func(v *vehicle) bool { return v.Pod.Activity == Traveling })
	})
	for s.tick < 600*TicksPerSecond {
		watch.step(t, s)
	}
	if watch.runs.runs == 0 || s.completed < len(group)+2 {
		t.Fatalf("entry runs %+v, %d trips complete", watch.runs, s.completed)
	}
}
