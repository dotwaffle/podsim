package scenarios

import (
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// depotRing returns the small ring with a depot of lanes lanes and slots
// slots, and pods pods in the depot. The passenger stations hold the other
// pods.
func depotRing(t *testing.T, lanes, slots, pods int) Parameters {
	t.Helper()
	parameters := smallParameters()
	// Two berths at each passenger station make the pods that leave the
	// depot return to it.
	parameters.PassengerBerths = 2
	parameters.ParkingLanes, parameters.ParkingBerths, parameters.InitialParkingPods = lanes, slots, pods
	return parameters
}

// TestDepotRingRuns checks that a ring with a depot validates and runs
// with demand. Pods leave the depot from the heads and park in it again,
// and each tick passes the separation check.
func TestDepotRingRuns(t *testing.T) {
	t.Parallel()
	config, err := PresetWith("small", func(parameters *Parameters) {
		*parameters = depotRing(t, 2, 3, 4)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.Name, customCapacitySuffix) || len(config.Network.Depots) != 1 || len(config.Network.Depots[0].Lanes) != 2 {
		t.Fatalf("project %q has depots %+v", config.Name, config.Network.Depots)
	}
	parked := make(map[string]string)
	for _, placement := range config.Fleet {
		if placement.StationID == "parking" {
			parked[placement.ID] = placement.BerthID
		}
	}
	if want := map[string]string{"pod-009": "parking-1-01", "pod-010": "parking-2-01", "pod-011": "parking-1-02", "pod-012": "parking-2-02"}; len(parked) != len(want) || !mapsEqual(parked, want) {
		t.Fatalf("depot pods %v, want %v", parked, want)
	}
	simulation := newSimulation(t, config)
	schedule := requestSchedule(config, scheduleParameters{seed: 5, count: 20, interval: 10 * sim.TicksPerSecond})
	left, arrived := false, false
	for tick := range 15 * 60 * sim.TicksPerSecond {
		for len(schedule) > 0 && schedule[0].tick == int64(tick) {
			if err := simulation.RequestTrip(schedule[0].origin, schedule[0].destination); err != nil {
				t.Fatal(err)
			}
			schedule = schedule[1:]
		}
		simulation.Step()
		observation := simulation.SafetyObservation()
		if _, err := observation.Check(); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		for _, vehicle := range simulation.Snapshot().Vehicles {
			pod := vehicle.Pod
			berth, wasParked := parked[pod.ID]
			switch {
			case wasParked && pod.BerthID != berth:
				left = true
			case !wasParked && pod.StationID == "parking" && pod.BerthID != "":
				arrived = true
			}
		}
	}
	if !left || !arrived || simulation.Snapshot().Completed == 0 {
		t.Fatalf("left %t, arrived %t, completed %d", left, arrived, simulation.Snapshot().Completed)
	}
}

// mapsEqual reports whether two string maps are equal.
func mapsEqual(a, b map[string]string) bool {
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return len(a) == len(b)
}

// TestDepotRingRefusals checks the refusals of the depot parameters.
func TestDepotRingRefusals(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		lanes, pods int
		slots       int
		want        string
	}{
		{"negative lanes", -1, 0, 3, "parking lanes must not be negative"},
		{"too many lanes", sim.MaxDepotLanes + 1, 0, 3, "parking lanes must be at most 8"},
		{"no slots", 2, 0, 0, "the depot has 2 lanes of 0 slots, want 1 to 200 slots in all"},
		{"too many slots", 2, 0, 101, "the depot has 2 lanes of 101 slots, want 1 to 200 slots in all"},
		// 8 lanes of 2^61+1 slots wrap to 8 slots on a 64-bit int.
		{"wrapping slots", 8, 4, 1<<61 + 1, "the depot has 8 lanes of 2305843009213693953 slots, want 1 to 200 slots in all"},
		{"too many pods", 2, 7, 3, "initial pods exceed berth capacity"},
	} {
		_, err := PresetWith("small", func(parameters *Parameters) {
			*parameters = depotRing(t, test.lanes, test.slots, test.pods)
			parameters.Pods = max(parameters.Pods, test.pods)
		})
		if err == nil || err.Error() != test.want {
			t.Errorf("%s: got %v, want %q", test.name, err, test.want)
		}
	}
	if _, err := PresetWith("scale100", func(parameters *Parameters) { parameters.ParkingLanes = 2 }); err == nil || err.Error() != "parking lanes apply only to a ring" {
		t.Errorf("mesh: got %v", err)
	}
}
