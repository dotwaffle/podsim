package session

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"reflect"
	"slices"
	"strings"
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
	if decoded.Version != serviceStateVersion || decoded.Project.Fleet[0].Class != sim.GroupClass || decoded.Simulation.Pods[0].Class != sim.GroupClass {
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
	assembler, err := NewStreamAssemblerVersion(shared.Topology(), 3)
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

// testGroupWorstCaseSize measures independent typed fields, not reachable placement.
func testGroupWorstCaseSize(t *testing.T, base stateFile, pod sim.SavedPod, trip sim.SavedTrip) {
	t.Helper()
	const wide = 0.0000010000000000000002
	alphabet := []byte{1, 2, 3, 4, 5, 6, 7, 11, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	for _, representation := range []string{"historical", "modern", "mixed"} {
		t.Run("group-"+representation, func(t *testing.T) {
			file := base
			file.Project = project.Clone(base.Project)
			stationID := pod.Riders[0].From
			berths := make([]sim.Berth, project.MaxBerths)
			classes, classErr := sim.NewClassSet("group")
			if classErr != nil {
				t.Fatal(classErr)
			}
			for i := range berths {
				berths[i] = sim.Berth{ID: strings.Repeat("\x01", 62) + string([]byte{alphabet[i/len(alphabet)], alphabet[i%len(alphabet)]}), Node: file.Project.Network.Nodes[i].ID, VehicleClasses: classes}
			}
			file.Project.Network.Stations[0].ID, file.Project.Network.Stations[0].Berths = stationID, berths
			file.Project.Network.Stations[0].VehicleClasses, file.Project.Network.Stations[0].Banks = classes, nil
			file.Project.Fleet = make([]sim.Placement, project.MaxPods)
			file.Simulation.Pods = make([]sim.SavedPod, project.MaxPods)
			for i := range file.Simulation.Pods {
				saved := pod
				saved.ID = strings.Repeat("\x01", 62) + string([]byte{alphabet[i/len(alphabet)], alphabet[i%len(alphabet)]})
				saved.Class = sim.GroupClass
				saved.Platoon, saved.CompactQueue, saved.Boardings = nil, nil, nil
				saved.RiddenMeters = 0
				saved.Riders = slices.Clone(pod.Riders)
				for j := range saved.Riders {
					saved.Riders[j].PartySize = 8
					saved.Riders[j].SharingConsent = sim.PrivateConsent
				}
				if representation == "modern" || representation == "mixed" && i%2 != 0 {
					saved.RiddenMeters = wide
					saved.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: berths[len(berths)-1].ID, MetersAtBoarding: wide}}, sim.MaxSharedRideParties)
				}
				file.Simulation.Pods[i] = saved
				file.Project.Fleet[i] = sim.Placement{ID: saved.ID, Class: sim.GroupClass, StationID: stationID, BerthID: berths[i%len(berths)].ID}
			}
			file.Project.Name = ""
			file.Project.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, file.Project))
			file.Simulation.Waiting = make([]sim.SavedTrip, sim.MaxSavedWaitingTrips)
			for i := range file.Simulation.Waiting {
				file.Simulation.Waiting[i] = trip
				file.Simulation.Waiting[i].Request.PartySize = 8
				if i >= project.MaxPods {
					file.Simulation.Waiting[i].Route = nil
				}
			}
			data := encodeTestState(t, file)
			raw := decompressTestJSON(t, data)
			if len(raw) > MaxStateBytes || len(file.Simulation.Pods) != 300 || len(file.Simulation.Waiting) != 2600 {
				t.Fatal("group save maximum changed byte or operating limits")
			}
			if scanErr := prescanJSON(raw, boardingStateLimits(compactStateLimits(stateJSONLimits))); scanErr != nil {
				t.Fatal(scanErr)
			}
			decoded, decodeErr := decodeStateFile(data)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if resolveErr := decoded.resolveBoardings(); resolveErr != nil {
				t.Fatal(resolveErr)
			}
			for i := range file.Simulation.Pods {
				want, got := file.Simulation.Pods[i], decoded.Simulation.Pods[i]
				if got.Class != sim.GroupClass || len(got.Riders) != 8 || !slices.Equal(want.Boardings, got.Boardings) {
					t.Fatal("typed group save maximum lost class or aligned records")
				}
			}
			if !bytes.Equal(raw, decompressTestJSON(t, encodeTestState(t, decoded))) {
				t.Fatal("typed group maximum changed on source-bound reencode")
			}
			t.Logf("typed group %s save maximum: raw=%d gzip=%d cap=%d", representation, len(raw), len(data), MaxStateBytes)
		})
	}
}
