package session

import (
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// soakExpressPreset is the name of the Express project of the soak. No preset
// can run an Express pod, so the test builds this project. It is not a product
// preset, and only an explicit PODSIM_SOAK_PRESETS selects it.
const soakExpressPreset = "rail-hub-express"

// soakExpressArms lists the arms of the Express project. A large class forms no
// platoon link, so the platoon arms would test nothing on an all-Express fleet.
var soakExpressArms = []string{"incidents", "emergencies", "reference"}

// soakExpressProject returns rail-hub with the express-v1 order contract, each
// class on each lane, station, and berth, and an Express pod for each place in
// the fleet. Each other setting stays as the preset has it.
func soakExpressProject(t *testing.T) project.Config {
	t.Helper()
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	config := scenarios.RailHub()
	config.OrderContract = sim.ExpressOrderContract
	for index := range config.Network.Lanes {
		config.Network.Lanes[index].VehicleClasses = classes
	}
	for index := range config.Network.Stations {
		station := &config.Network.Stations[index]
		station.VehicleClasses = classes
		for berth := range station.Berths {
			station.Berths[berth].VehicleClasses = classes
		}
	}
	for index := range config.Fleet {
		config.Fleet[index].Class = sim.ExpressClass
	}
	return config
}

// TestSoakExpressProject checks that the project of the Express soak has only
// Express pods, and that the soak presets of the other arms have none.
func TestSoakExpressProject(t *testing.T) {
	t.Parallel()
	config := soakProject(t, soakExpressPreset, soakArms["incidents"], 1)
	if config.OrderContract != sim.ExpressOrderContract {
		t.Errorf("the order contract is %q", config.OrderContract)
	}
	for _, pod := range config.Fleet {
		if pod.Class != sim.ExpressClass {
			t.Errorf("pod %s has class %q", pod.ID, pod.Class)
		}
	}
	plain := soakPreset(t, "rail-hub")
	if plain.OrderContract != "" {
		t.Errorf("rail-hub has the order contract %q", plain.OrderContract)
	}
	for _, pod := range plain.Fleet {
		if pod.Class == sim.ExpressClass {
			t.Errorf("rail-hub has the Express pod %s", pod.ID)
		}
	}
	if slices.Contains(strings.Split(soakDefaultPresets, ","), soakExpressPreset) {
		t.Errorf("the default presets %q hold the Express project", soakDefaultPresets)
	}
}

// TestSoakExpressDriver runs a short soak of the Express project in the ordinary
// test run: a pod fault on an Express pod, a command fault, a command emergency,
// a save that restores in the physical tier, a rewind, and the clear.
func TestSoakExpressDriver(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("one simulation on one goroutine, so the race detector finds nothing")
	}
	input := soakInput{preset: soakExpressPreset, arm: soakArms["incidents"], seed: 1, hours: 0.02, checks: true, settle: 60 * sim.TicksPerSecond}
	d := newSoakDriver(t, input)
	d.nextFault, d.nextEmergency, d.nextSave, d.nextRewind = 5*sim.TicksPerSecond/3, 100*sim.TicksPerSecond/3, 125*sim.TicksPerSecond/3, 50*sim.TicksPerSecond
	reply := d.client.mustApply(t, Command{Action: "fault", PodID: d.config.Fleet[0].ID})
	d.clears = append(d.clears, soakClear{reply.FaultID, 50 * sim.TicksPerSecond / 3})
	d.runHours()
	d.clearAndSettle()
	if r := d.result; r.commands == 0 || r.refused != 0 || r.saves != 1 || r.demoted != 0 || r.rewinds != 1 || r.emergencies == 0 || r.settleTicks == 0 || len(r.misses) != 0 {
		t.Fatalf("the driver result is %+v", r)
	}
	state := d.s.simulation.Snapshot()
	if state.Faults.Counters.Started == 0 || state.Emergencies.Counters.Started == 0 {
		t.Fatalf("the faults are %+v, and the emergencies are %+v", state.Faults, state.Emergencies)
	}
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.Class != sim.ExpressClass {
			t.Fatalf("pod %s has class %q", vehicle.Pod.ID, vehicle.Pod.Class)
		}
	}
}
