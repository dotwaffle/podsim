package session

import (
	"encoding/json/v2"
	"os"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func groupConsumerProject(t *testing.T) project.Config {
	t.Helper()
	raw, err := os.ReadFile("../project/testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if decodeErr := json.Unmarshal(raw, &config); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	return config
}

func TestGroupSaveSourceClassRoundTrip(t *testing.T) {
	t.Parallel()
	config := groupConsumerProject(t)
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	file := sessionStateFile(t, shared)
	file.RestoreAttempts = 0
	data := encodeTestState(t, file)
	decoded, err := decodeCheckedState(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != stateVersion || decoded.Project.Fleet[0].Class != sim.GroupClass || decoded.Simulation.Pods[0].Class != sim.GroupClass {
		t.Fatal("save lost source or operating group class")
	}
	loaded, err := shared.loadState(loadInput{data: data, project: &config, steps: realRestoreSteps()})
	if err != nil || loaded.result.Tier != sim.RestorePhysical {
		t.Fatalf("group physical restore: %v, %+v", err, loaded.result)
	}
	if got := loaded.simulation.ExportState(); !reflect.DeepEqual(got.Pods, file.Simulation.Pods) {
		t.Fatal("group idle state changed on restore")
	}
	for _, class := range []sim.VehicleClass{sim.LegacyClass, sim.ExpressClass} {
		bad := file
		bad.Project = project.Clone(file.Project)
		bad.Project.Fleet[0].Class = class
		if _, err := shared.loadState(loadInput{data: encodeTestState(t, bad), steps: realRestoreSteps()}); err == nil {
			t.Fatal("saved class mismatch or express accepted", class)
		}
	}
}

func TestGroupRecordedRideConsumerRoundTrip(t *testing.T) {
	t.Parallel()
	config := groupConsumerProject(t)
	config.OnboardPickups, config.SharedRidePartyLimit = true, 4
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	shared.advance()
	client := newTestClient(shared, "group-records")
	for _, stations := range [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"garden", "market"}} {
		client.mustApply(t, Command{Action: "trip", Origin: stations[0], Destination: stations[1], PartySize: 2, SharingConsent: sim.SharedConsent})
	}
	assembler, err := NewStreamAssembler(shared.Topology())
	if err != nil {
		t.Fatal(err)
	}
	for range 300 * sim.TicksPerSecond {
		shared.advance()
		frame, frameErr := shared.presentationFrame()
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		if _, stateErr := assembler.State(frame); stateErr != nil {
			t.Fatal("native group publication rejected", stateErr)
		}
		vehicle := frame.State.Simulation.Vehicles[0]
		if vehicle.Pod.Activity != sim.Boarding || vehicle.Pod.StationID != "garden" || len(vehicle.Boardings) != 3 {
			continue
		}
		if len(vehicle.Riders) != 3 || !vehicle.Riders[1].Completed || vehicle.Boardings[2].BerthID != "garden-1" {
			t.Fatal("native group occupied-pickup fixture lost history or source")
		}
		file := sessionStateFile(t, shared)
		file.RestoreAttempts = 0
		data := encodeTestState(t, file)
		decoded, decodeErr := decodeCheckedState(data)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if resolveErr := decoded.resolveBoardings(); resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if !reflect.DeepEqual(decoded.Simulation.Pods[0].Boardings, file.Simulation.Pods[0].Boardings) {
			t.Fatal("group saved tuple source changed")
		}
		loaded, loadErr := shared.loadState(loadInput{data: data, project: &config, steps: realRestoreSteps()})
		if loadErr != nil || loaded.result.Tier != sim.RestorePhysical {
			t.Fatalf("recorded group physical restore: %v, %+v", loadErr, loaded.result)
		}
		restored := loaded.simulation.ExportState().Pods[0]
		if restored.Class != sim.GroupClass || restored.RiddenMeters != file.Simulation.Pods[0].RiddenMeters || !reflect.DeepEqual(restored.Boardings, file.Simulation.Pods[0].Boardings) || !reflect.DeepEqual(restored.Riders, file.Simulation.Pods[0].Riders) {
			t.Fatal("native group restore changed recorded interval")
		}
		return
	}
	t.Fatal("native group never reached occupied pickup", shared.simulation.ExportState())
}
