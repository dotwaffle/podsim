package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// expressMaximumBraking is the maximum braking rate of the Express class, in
// m/s^2.
const expressMaximumBraking = 1.15

// TestLondonExpressIncidentFixtures runs the directed fixtures of the London
// soak row on the London Express preset, with background demand: 20 pooled
// singletons in one Express pod, a fault on that pod at speed with its
// standees, and an emergency on a pod with standees. It saves and restores
// at each state, and checks the safety oracle at each tick of each incident.
func TestLondonExpressIncidentFixtures(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the London Express fixtures run without -short or -race")
	}
	f := newLondonExpressFixture(t)
	shared := f.shared
	dt := 1.0 / sim.TicksPerSecond
	aboard := func(v sim.Vehicle) (parties, passengers int) {
		for _, r := range v.Riders {
			if !r.Completed && r.BoardedTick > 0 {
				parties, passengers = parties+1, passengers+r.PartySize
			}
		}
		return parties, passengers
	}
	find := func(id string) sim.Vehicle {
		for _, v := range shared.simulation.Snapshot().Vehicles {
			if v.Pod.ID == id {
				return v
			}
		}
		t.Fatalf("no pod %s", id)
		return sim.Vehicle{}
	}
	oracle := func(name string) {
		if _, err := shared.simulation.SafetyObservation().Check(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// until advances the session one second at a time, for at most limit
	// seconds, until done reports true. each runs at every tick.
	until := func(name string, limit int, each func(), done func() bool) {
		t.Helper()
		for range limit {
			for range sim.TicksPerSecond {
				shared.advance()
				if each != nil {
					each()
				}
			}
			if done() {
				return
			}
		}
		t.Fatalf("%s: not reached in %d s", name, limit)
	}

	// Pooled singletons: 20 parties of one share one Express pod. With
	// more pods free, dispatch gives each order its own pod, so faults hold
	// all Express pods but one in parking until the pod departs. The
	// riders past the 12 seats stand.
	var held []string
	for _, v := range shared.simulation.Snapshot().Vehicles {
		if v.Pod.Class == sim.ExpressClass && v.Pod.ID != "express-01" {
			held = append(held, f.client.mustApply(t, Command{Action: "fault", PodID: v.Pod.ID}).FaultID)
		}
	}
	for range 20 {
		if refusal := f.trip(londonWaterloo, londonBank, 1); refusal != "" {
			t.Fatal(refusal)
		}
	}
	pooled := ""
	standing := false
	until("pooled singletons", 600, nil, func() bool {
		for _, v := range shared.simulation.Snapshot().Vehicles {
			parties, passengers := aboard(v)
			if v.Pod.Class != sim.ExpressClass || parties == 0 {
				continue
			}
			if !standing && passengers > 12 && v.Pod.Activity == sim.Boarding {
				standing = true
				f.checkpoint("standees boarding")
			}
			if parties == 20 && v.Pod.Activity == sim.Traveling && v.Pod.Speed >= 10 {
				pooled = v.Pod.ID
				return true
			}
		}
		return false
	})
	if !standing {
		t.Fatal("no Express pod boarded with standees")
	}
	f.checkpoint("pooled at speed")
	for _, id := range held {
		f.client.mustApply(t, Command{Action: "clearFault", FaultID: id})
	}

	// A fault at speed on the pooled pod. It brakes at the maximum rate and
	// stops within its braking distance.
	v := find(pooled)
	start := v.Pod.Speed
	f.client.mustApply(t, Command{Action: "fault", PodID: pooled, DurationSeconds: new(int64(60))})
	speed, largest, travel := start, 0.0, 0.0
	for range 30 * sim.TicksPerSecond {
		shared.advance()
		oracle("fault")
		v = find(pooled)
		largest, travel, speed = max(largest, speed-v.Pod.Speed), travel+v.Pod.Speed*dt, v.Pod.Speed
		if speed == 0 {
			break
		}
	}
	if speed != 0 || v.Withdrawn&1 == 0 {
		t.Fatalf("the faulted pod did not stop: %+v withdrawn %d", v.Pod, v.Withdrawn)
	}
	if largest > expressMaximumBraking*dt+1e-9 || largest < 0.95*expressMaximumBraking*dt {
		t.Fatalf("largest drop %g, want about %g", largest, expressMaximumBraking*dt)
	}
	if limit := start * start / (2 * expressMaximumBraking); travel > limit+start*dt {
		t.Fatalf("the pod moved %g m after the fault, want at most %g", travel, limit)
	}
	if _, passengers := aboard(v); passengers != 20 {
		t.Fatalf("the faulted pod holds %d passengers", passengers)
	}
	t.Logf("fault on %s at %.2f m/s: largest drop %.5f m/s per tick, %.2f m to rest", pooled, start, largest, travel)
	f.checkpoint("fault at rest with standees")
	until("fault end", 120, func() { oracle("fault hold") }, func() bool { return find(pooled).Withdrawn&1 == 0 })

	// An emergency on a pod with standees at speed. It unloads at a station
	// and leaves service until the emergency ends.
	if refusal := f.trip(londonWaterloo, londonBank, 18); refusal != "" {
		t.Fatal(refusal)
	}
	target := ""
	until("standees at speed", 600, nil, func() bool {
		for _, v := range shared.simulation.Snapshot().Vehicles {
			if _, passengers := aboard(v); passengers > 12 && v.Pod.Activity == sim.Traveling && v.Pod.Speed >= 10 {
				target = v.Pod.ID
				return true
			}
		}
		return false
	})
	f.client.mustApply(t, Command{Action: "emergency", PodID: target})
	t.Logf("emergency on %s at tick %d", target, shared.simulation.Snapshot().Tick)
	f.checkpoint("emergency journey")
	unloaded := false
	until("emergency unload", 600, func() { oracle("emergency") }, func() bool {
		v := find(target)
		if !unloaded && v.Pod.Activity == sim.Unloading && v.Operational == sim.OperationalEmergencyUnload {
			unloaded = true
			t.Logf("emergency unload of %s at %s", target, v.Pod.StationID)
			f.checkpoint("emergency unload")
		}
		_, passengers := aboard(v)
		return unloaded && passengers == 0
	})
}
