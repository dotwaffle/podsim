package session

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// codecTick saves the session of x through the state file, decodes the
// file, and starts a session from it, as a restart does. The decoded
// simulation must equal the exported one. The restore must keep every pod
// in the physical tier with the speed bits of the live pod, and export the
// saved state again, except a distance within the restore tolerance. It
// returns the largest distance change.
func (x incidentSave) codecTick(t *testing.T, name string) float64 {
	t.Helper()
	x.s.mu.Lock()
	want := x.s.simulation.ExportState()
	tick := x.s.simulation.Tick()
	x.s.revision++
	x.s.mu.Unlock()
	if err := x.s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatalf("%s, tick %d: save: %v", name, tick, err)
	}
	data := x.store.takeLastWrite()
	file, err := decodeIncidentSave(data)
	if err != nil {
		t.Fatalf("%s, tick %d: decode: %v", name, tick, err)
	}
	if !reflect.DeepEqual(file.Simulation, want) {
		t.Fatalf("%s, tick %d: the file gives\n%+v\nwant\n%+v", name, tick, file.Simulation, want)
	}
	restored, err := newFromStore(t.Context(), StoreInput{Store: &fakeStore{data: data}}, realRestoreSteps())
	if err != nil {
		t.Fatalf("%s, tick %d: restore: %v", name, tick, err)
	}
	defer restored.Close()
	if restored.restore.Tier != "physical" || restored.restore.Reason != "" || restored.restore.Demoted+restored.restore.Requeued+restored.restore.Dropped != 0 {
		t.Fatalf("%s, tick %d: restore: %+v", name, tick, restored.restore)
	}
	restored.mu.Lock()
	got := restored.simulation.ExportState()
	restored.mu.Unlock()
	got.Pods = slices.Clone(got.Pods)
	residual := 0.0
	for index := range got.Pods {
		pod, live := &got.Pods[index], want.Pods[index]
		if math.Float64bits(pod.Speed) != math.Float64bits(live.Speed) {
			t.Fatalf("%s, tick %d, pod %s: restored speed %v, want %v", name, tick, pod.ID, pod.Speed, live.Speed)
		}
		change := max(math.Abs(pod.Distance-live.Distance), math.Abs(pod.LaneDistance-live.LaneDistance))
		if change > 1e-6 {
			t.Fatalf("%s, tick %d, pod %s: restored at %v, want %v", name, tick, pod.ID, pod.Distance, live.Distance)
		}
		residual = max(residual, change)
		pod.Distance, pod.LaneDistance = live.Distance, live.LaneDistance
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s, tick %d: the physical restore exports\n%+v\nwant\n%+v", name, tick, got, want)
	}
	return residual
}

// TestSavedMotionCodecEveryTick saves and restores sessions of the example
// project through the state file at every tick (see codecTick): two pods
// with riders through a fault on a moving pod and its evacuation, and a
// pod through an emergency and its unload. TestRestoreMotionEveryTick in
// package sim checks the frontiers and the owners at every tick of more
// fixtures.
func TestSavedMotionCodecEveryTick(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the codec round trip at every tick runs without -short")
	}
	for _, test := range []struct {
		name  string
		start func(t *testing.T) incidentSave
		ticks int
	}{
		{"fault", func(t *testing.T) incidentSave {
			t.Helper()
			x := faultSave(t, 2, 0)
			x.boardTwo(t)
			x.command(t, Command{Action: "fault", PodID: "01"})
			return x
		}, 30 * sim.TicksPerSecond},
		{"emergency", func(t *testing.T) incidentSave {
			t.Helper()
			x := emergencySave(t, 1, true)
			x.boardTwo(t)
			x.emergency(t)
			return x
		}, 120 * sim.TicksPerSecond},
		{"demand", func(t *testing.T) incidentSave {
			t.Helper()
			config := policiesOffProject()
			config.Demand.Enabled = true
			x := newIncidentSave(t, config)
			x.s.mu.Lock()
			defer x.s.mu.Unlock()
			x.run(t)
			return x
		}, 120 * sim.TicksPerSecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			x := test.start(t)
			x.s.mu.Lock()
			x.run(t)
			x.s.mu.Unlock()
			residual, moving := 0.0, 0
			for range test.ticks {
				residual = max(residual, x.codecTick(t, test.name))
				x.s.mu.Lock()
				x.s.step()
				for _, v := range x.s.simulation.Snapshot().Vehicles {
					if v.Pod.Speed > 0 {
						moving++
					}
				}
				x.s.mu.Unlock()
			}
			if moving == 0 {
				t.Fatal("no pod moved")
			}
			t.Logf("%s: %d ticks, %d moving pod ticks, largest position residual %v m", test.name, test.ticks, moving, residual)
		})
	}
}
