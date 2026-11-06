package session

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The fixtures of this file reach the rare paths of the rule A classifier, on the
// example network, with scripted commands, and through the real session, the real
// simulation, and watchEmergencies. Pod 02 is idle at
// the only berth of market, and debris on market-out, the only lane out of that
// berth, keeps it there. Pod 01 has an emergency and needs that berth.
//
// docs/studies/incident-soak.md, section "Coverage of the rare paths", has the
// answer for each path: which paths a run of the soak can reach, and which it
// cannot.

// soakPathDriver returns a driver of the example network, with the driver events
// off and the checks on, with no demand, and with pod 02 at market.
func soakPathDriver(t *testing.T) *soakDriver {
	t.Helper()
	config := project.Default()
	config.Fleet[1] = sim.Placement{ID: "02", StationID: "market", BerthID: "market-1"}
	config.Faults = &project.FaultConfig{}
	config.Emergencies = &project.EmergencyConfig{}
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	d := &soakDriver{t: t, input: soakInput{preset: "example", arm: soakArms["incidents"], seed: 1, checks: true}, config: config, s: s, client: newTestClient(s, "paths"),
		rng: rand.New(rand.NewPCG(1, 2)), open: map[string]*soakOpenRecord{}, lastWatch: -1}
	d.startChecks()
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = math.MaxInt64, math.MaxInt64, math.MaxInt64, math.MaxInt64
	return d
}

// pathPod returns the pod with the ID.
func (d *soakDriver) pathPod(id string) sim.Pod {
	d.t.Helper()
	state := d.s.simulation.Snapshot()
	for index := range state.Vehicles {
		if state.Vehicles[index].Pod.ID == id {
			return state.Vehicles[index].Pod
		}
	}
	d.t.Fatalf("no pod %s", id)
	return sim.Pod{}
}

// pathTick runs one tick of the driver loop, with its events, checks, and
// readings.
func (d *soakDriver) pathTick() {
	d.t.Helper()
	d.loopTick++
	d.runLoopTick(d.loopTick, &soakWindow{})
}

// pathUntil runs the driver loop until the condition holds.
func (d *soakDriver) pathUntil(what string, done func() bool) {
	d.t.Helper()
	for range soakClearTicks {
		if done() {
			return
		}
		d.pathTick()
	}
	d.t.Fatalf("no %s in %d ticks: pod 01 %+v, pod 02 %+v", what, soakClearTicks, d.pathPod("01"), d.pathPod("02"))
}

// pathBetweenReadings runs the driver loop to a tick with no reading, so a read
// of the test is the first read of its tick.
func (d *soakDriver) pathBetweenReadings() {
	d.t.Helper()
	d.pathTick()
	if d.lastWatch == d.s.simulation.Tick() {
		d.pathTick()
	}
}

// pathDebris starts debris on the lane, and it returns the fault ID.
func (d *soakDriver) pathDebris(lane string) string {
	d.t.Helper()
	return d.mustApply(Command{Action: "fault", LaneID: lane, FromMeters: new(40.0), ToMeters: new(44.0)}).FaultID
}

// pathEmergency takes pod 01 from harbor to market, and it starts an
// emergency on the approach to market. It returns when pod 01 stands at the end
// of its reservation and reports that pod 02 has no place to go.
func (d *soakDriver) pathEmergency() string {
	d.t.Helper()
	d.mustApply(Command{Action: "trip", Origin: "harbor", Destination: "market"})
	d.pathUntil("pod 01 on the approach", func() bool { return d.pathPod("01").LaneID == "market-approach" })
	id := d.mustApply(Command{Action: "emergency", PodID: "01"}).EmergencyID
	d.pathUntil("stop at the berth of pod 02", func() bool {
		pod := d.pathPod("01")
		return pod.Speed == 0 && pod.WaitReason == sim.ParkingUnavailable && pod.BlockedBy == "02"
	})
	return id
}

// TestSoakPathLegFreeNoOrder reaches the path of an idle pod with no order in
// the driver loop: the entry returns false because no trip names the pod. The
// debris keeps the pod at its berth, but the rule of amendment A8 names only an
// idle pod with an order, so the time counts as not blocked, and the gate fires
// at the limit. A clear ends the wait.
func TestSoakPathLegFreeNoOrder(t *testing.T) {
	t.Parallel()
	d := soakPathDriver(t)
	debris := d.pathDebris("market-out")
	id := d.pathEmergency()
	if d.s.simulation.BoardingLegBlockedForTest("02") {
		t.Fatal("the entry names a blocked leg for a pod with no order")
	}
	before, record := d.result.paths, d.open[id]
	blocked, unblocked := record.blocked, record.unblocked
	const seconds = 5
	for range seconds * soakViewTicks {
		d.pathTick()
	}
	want := before
	want[soakPathLegFree] += seconds
	if d.result.paths != want {
		t.Fatalf("5 seconds of readings give %s, want %s", d.result.paths, want)
	}
	if got := d.open[id]; got.blocked != blocked || got.unblocked != unblocked+seconds*soakViewTicks {
		t.Fatalf("the record has %d blocked and %d unblocked ticks, from %d and %d", got.blocked, got.unblocked, blocked, unblocked)
	}
	d.pathUntil("late record", func() bool { return len(d.result.misses) > 0 })
	if miss := d.result.misses[0]; d.result.emergencyLate != 1 || !strings.Contains(miss, `chain end pod 02, Ready, wait ""`) {
		t.Fatalf("the gate has %d late records: %q", d.result.emergencyLate, d.result.misses)
	}
	d.mustApply(Command{Action: "clearFault", FaultID: debris})
	d.pathUntil("end of the record", func() bool { return d.open[id] == nil })
}

// TestSoakPathLegFreeWithOrder reaches the path of an idle pod whose order can
// board: the entry returns false because the leg route succeeds with the faults.
// The path holds only at a command boundary, between the clear of the fault on
// the leg and the next dispatch, which boards the order in the next tick. The
// driver does not read at that boundary, so this test reads there itself. A
// second fault keeps the blocked set active.
func TestSoakPathLegFreeWithOrder(t *testing.T) {
	t.Parallel()
	d := soakPathDriver(t)
	onLeg := d.pathDebris("market-out")
	d.pathDebris("garden-merge")
	d.mustApply(Command{Action: "trip", Origin: "market", Destination: "harbor"})
	d.pathEmergency()
	if !d.s.simulation.BoardingLegBlockedForTest("02") || d.pathPod("02").Activity != sim.Idle {
		t.Fatalf("the order of pod 02 does not wait for its leg: %+v", d.pathPod("02"))
	}
	d.pathUntil("reading of the blocked leg", func() bool { return d.result.paths[soakPathLegBlocked] > 0 })
	if d.result.paths[soakPathLegFree] != 0 {
		t.Fatalf("the order has a free leg before the clear: %s", d.result.paths)
	}

	d.pathBetweenReadings()
	d.mustApply(Command{Action: "clearFault", FaultID: onLeg})
	before := d.result.paths
	d.watchEmergencies()
	want := before
	want[soakPathLegFree]++
	if d.result.paths != want {
		t.Fatalf("the read after the clear gives %s, want %s", d.result.paths, want)
	}

	d.pathTick()
	if pod := d.pathPod("02"); pod.Activity != sim.Boarding {
		t.Fatalf("the tick after the clear does not board the order: %+v", pod)
	}
	freeAtClear := d.result.paths[soakPathLegFree]
	for range 2 * soakViewTicks {
		d.pathTick()
	}
	if d.result.paths[soakPathLegFree] != freeAtClear {
		t.Fatalf("the driver loop reads a free leg: %s", d.result.paths)
	}
}

// TestSoakPathFaultAtChainEnd reaches a chain that ends at another pod with a
// fault. The path holds only at a command boundary, between a fault command and
// the next admission, which writes the incident report in place of the name of
// the pod. The driver sends each command before its step and reads after it, so
// the same fault in the order of the driver gives the incident path.
func TestSoakPathFaultAtChainEnd(t *testing.T) {
	t.Parallel()
	t.Run("read at the command boundary", func(t *testing.T) {
		t.Parallel()
		d := soakPathDriver(t)
		d.pathDebris("market-out")
		id := d.pathEmergency()
		d.pathBetweenReadings()
		fault := d.mustApply(Command{Action: "fault", PodID: "02"}).FaultID
		before, blocked := d.result.paths, d.open[id].blocked
		d.watchEmergencies()
		want := before
		want[soakPathFaultEnd]++
		if d.result.paths != want {
			t.Fatalf("the read after the fault gives %s, want %s", d.result.paths, want)
		}
		if record := d.open[id]; record.blocked <= blocked || record.end != "pod 02 with a fault" {
			t.Fatalf("the record is %+v, from %d blocked ticks", record, blocked)
		}
		d.pathTick()
		if pod := d.pathPod("01"); pod.WaitReason != sim.BlockedByIncident || pod.BlockedBy != fault {
			t.Fatalf("the next tick reports %q by %q", pod.WaitReason, pod.BlockedBy)
		}
	})
	t.Run("order of the driver", func(t *testing.T) {
		t.Parallel()
		d := soakPathDriver(t)
		d.pathDebris("market-out")
		d.pathEmergency()
		d.pathBetweenReadings()
		d.apply(Command{Action: "fault", PodID: "02"})
		d.pathUntil("incident reading", func() bool { return d.result.paths[soakPathIncident] > 0 })
		for range 2 * soakViewTicks {
			d.pathTick()
		}
		if d.result.paths[soakPathFaultEnd] != 0 {
			t.Fatalf("the driver loop reads a fault at the end of a chain: %s", d.result.paths)
		}
	})
}
