//go:build unix

package statestore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

func policyStateJSON(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

func encodePolicyState(t *testing.T, file map[string]json.RawMessage) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := gzip.NewWriter(&data)
	if err := json.NewEncoder(writer).Encode(file); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// policyRestartFixture combines two crossed pickup assignments and one
// buffered pickup. It requires a real swap before exporting the physical state.
func policyRestartFixture(t *testing.T) (project.Config, sim.SavedState) {
	t.Helper()
	config := project.Default()
	config.StationBuffers, config.PickupReassignment = true, true
	config.Fleet = append(config.Fleet, sim.Placement{ID: "03", StationID: "parking", BerthID: "parking-1"})
	for index := range config.Network.Nodes {
		config.Network.Nodes[index].Position.X *= 4
		config.Network.Nodes[index].Position.Y *= 4
	}
	for index := range config.Network.Lanes {
		if config.Network.Lanes[index].ID == "market-approach" {
			config.Network.Lanes[index].StationRole = sim.StationEntryRole
		}
	}
	initial, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	state := initial.ExportState()
	state.RequestID = 2
	indexes := make(map[string]int)
	for index, lane := range config.Network.Lanes {
		indexes[lane.ID] = index
	}
	for index, from := range []string{"garden", "harbor"} {
		own, _ := config.Network.Station(config.Fleet[index].StationID)
		target, _ := config.Network.Station(from)
		route, routeErr := config.Network.Route(own.Berths[0].Node, target.Berths[0].Node)
		if routeErr != nil {
			t.Fatal(routeErr)
		}
		path := make([]int, len(route))
		for lane, value := range route {
			path[lane] = indexes[value.ID]
		}
		state.Pods[index] = sim.SavedPod{
			ID: config.Fleet[index].ID, Activity: "departing", StationID: own.ID, BerthID: own.Berths[0].ID,
			Origin: own.Berths[0].ID, Destination: target.Berths[0].ID,
			DestinationStation: from, RelocatingTo: from, Route: path,
		}
		state.Waiting = append(state.Waiting, sim.SavedTrip{Request: sim.SavedRequest{
			ID: index + 1, From: from, To: "market", PartySize: 1, PodID: config.Fleet[index].ID,
			SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService,
		}})
	}
	simulation, result, err := sim.RestoreState(sim.RestoreStateInput{Network: config.Network, Fleet: config.Fleet, State: state})
	if err != nil || result.Tier != sim.RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
		t.Fatalf("crossed fixture restore: %+v %v", result, err)
	}
	project.ConfigureExperiments(simulation, config)
	if err := simulation.SetFinishingPodWait(sim.FinishingPodWaitNone); err != nil {
		t.Fatal(err)
	}
	simulation.SetPickupSwaps(false)
	if err := simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	simulation.SetPickupSwaps(true)
	for range 2 {
		simulation.Step()
	}
	if _, err := simulation.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	state = simulation.ExportState()
	if simulation.PickupSwapStats().Swaps != 1 || !state.Pods[2].StationBuffered || state.Pods[0].StationBuffered || state.Pods[1].StationBuffered {
		t.Fatalf("fixture lacks swapped and mixed buffered pods: %+v %+v", simulation.PickupSwapStats(), state.Pods)
	}
	return config, state
}

func startPolicySession(t *testing.T, store *Store, config *project.Config) *session.Session {
	t.Helper()
	shared, err := session.NewFromStore(t.Context(), session.StoreInput{Store: store, Project: config})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	return shared
}

func storedPolicyFixture(t *testing.T, store *Store, config project.Config, simulation sim.SavedState) {
	t.Helper()
	shared := startPolicySession(t, store, &config)
	shared.Close()
	if err := shared.SaveState(t.Context(), session.SaveFinal); err != nil {
		t.Fatal(err)
	}
	file := policyStateJSON(t, mustRead(t, store))
	raw, err := json.Marshal(simulation)
	if err != nil {
		t.Fatal(err)
	}
	file["simulation"], file["version"] = raw, json.RawMessage("6")
	data := encodePolicyState(t, file)
	mustWrite(t, store, data)
}

func TestSavedPolicyFileRestart(t *testing.T) {
	t.Parallel()
	config, physical := policyRestartFixture(t)
	store := openStore(t, "file://"+t.TempDir())
	storedPolicyFixture(t, store, config, physical)
	restored := startPolicySession(t, store, nil)
	checkPolicyPhysicalRestore(t, restored)
	if got := restored.Project().Project; !got.StationBuffers || !got.PickupReassignment {
		t.Fatal("restart lost the policy controls saved on disk")
	}
	finishPolicyRun(t, restored, 3)
}

func checkPolicyPhysicalRestore(t *testing.T, shared *session.Session) {
	t.Helper()
	info := shared.State().Restore
	if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped+info.Unaccounted != 0 {
		t.Fatalf("policy state did not restore physically: %+v", info)
	}
}

func finishPolicyRun(t *testing.T, shared *session.Session, count int) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		reply := shared.Apply(session.Command{Client: "policy-restart", Sequence: 1, Epoch: shared.State().Epoch, Action: "speed", Speed: 60})
		if reply.Error != "" {
			t.Fatal(reply.Error)
		}
		go shared.Run(ctx)
		for range 30 {
			time.Sleep(time.Second)
			synctest.Wait()
			if shared.State().Simulation.Completed == count {
				break
			}
		}
		cancel()
		synctest.Wait()
	})
	if state := shared.State().Simulation; state.Completed != count || state.Submitted != count || len(state.Pending) != 0 {
		t.Fatalf("restarted clock lost or stalled requests: %+v", state)
	}
}

func TestCombinedPoliciesFileRestart(t *testing.T) {
	t.Parallel()
	config, physical := policyRestartFixture(t)
	for _, buffers := range []bool{false, true} {
		for _, reassignment := range []bool{false, true} {
			t.Run(fmt.Sprintf("buffers%t-reassignment%t", buffers, reassignment), func(t *testing.T) {
				t.Parallel()
				store := openStore(t, "file://"+t.TempDir())
				storedPolicyFixture(t, store, config, physical)
				selected := project.Clone(config)
				selected.StationBuffers, selected.PickupReassignment = project.PolicyFlag(buffers), project.PolicyFlag(reassignment)
				shared := startPolicySession(t, store, &selected)
				checkPolicyPhysicalRestore(t, shared)
				if got := shared.Project().Project; got.StationBuffers != selected.StationBuffers || got.PickupReassignment != selected.PickupReassignment {
					t.Fatal("restart lost the selected policy controls")
				}
				shared.Close()
				if err := shared.SaveState(t.Context(), session.SaveFinal); err != nil {
					t.Fatal(err)
				}
				var saved sim.SavedState
				if err := json.Unmarshal(policyStateJSON(t, mustRead(t, store))["simulation"], &saved); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(saved, physical) {
					t.Fatal("file restart changed physical assignments or mixed buffer membership")
				}
				restarted := startPolicySession(t, store, &selected)
				checkPolicyPhysicalRestore(t, restarted)
				finishPolicyRun(t, restarted, 3)
				restarted.Close()
				if err := restarted.SaveState(t.Context(), session.SaveFinal); err != nil {
					t.Fatal(err)
				}
				wantVersion := "6"
				if got := string(policyStateJSON(t, mustRead(t, store))["version"]); got != wantVersion {
					t.Fatalf("drained save version=%s, want %s", got, wantVersion)
				}
			})
		}
	}
}

// A version 2 save comes from a server before version 6. Startup moves it
// aside as a rejected state and starts a new session that saves version 6.
func TestLegacyFileArchivedAtStartup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := openStore(t, "file://"+dir)
	config := project.Default()
	shared := startPolicySession(t, store, &config)
	reply := shared.Apply(session.Command{Client: "legacy", Sequence: 1, Epoch: shared.State().Epoch, Action: "trip", Origin: "garden", Destination: "market"})
	if reply.Error != "" {
		t.Fatal(reply.Error)
	}
	shared.Close()
	if err := shared.SaveState(t.Context(), session.SaveFinal); err != nil {
		t.Fatal(err)
	}

	// Construct a historical version-2 file from the current capture.
	// Historical files did not contain consent or vehicle-class metadata.
	file := policyStateJSON(t, mustRead(t, store))
	var saved sim.SavedState
	if err := json.Unmarshal(file["simulation"], &saved); err != nil {
		t.Fatal(err)
	}
	strip := func(request *sim.SavedRequest) {
		request.SharingConsent, request.Service, request.ServiceID = "", "", ""
		request.LegacyPartySize = false
	}
	for index := range saved.Pods {
		pod := &saved.Pods[index]
		pod.Class, pod.LegacyCohort, pod.StationBuffered, pod.Platoon = "", false, false, nil
		for rider := range pod.Riders {
			strip(&pod.Riders[rider])
		}
	}
	for index := range saved.Waiting {
		strip(&saved.Waiting[index].Request)
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	file["simulation"], file["version"] = raw, json.RawMessage("2")
	legacy := encodePolicyState(t, file)
	mustWrite(t, store, legacy)

	restarted := startPolicySession(t, store, &config)
	if info := restarted.State().Restore; info.Tier != "empty" || info.Reason != "unsupported_version" {
		t.Fatalf("legacy restore = %+v, want an empty session for an unsupported version", info)
	}
	var archived []string
	for _, name := range files(t, dir) {
		if strings.HasPrefix(name, rejectedPrefix) {
			archived = append(archived, name)
		}
	}
	if len(archived) != 1 {
		t.Fatalf("rejected files = %q, want one", archived)
	}
	if data, err := os.ReadFile(filepath.Join(dir, archived[0])); err != nil || !bytes.Equal(data, legacy) {
		t.Fatal("the rejected file is not the version 2 save", err)
	}
	if got := string(policyStateJSON(t, mustRead(t, store))["version"]); got != "6" {
		t.Fatalf("startup save version=%s, want 6", got)
	}
}

func TestCombinedPolicyFailedFileSave(t *testing.T) {
	t.Parallel()
	config, physical := policyRestartFixture(t)
	for _, failure := range []string{"canceled write", "directory sync"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			store := openStore(t, "file://"+t.TempDir())
			storedPolicyFixture(t, store, config, physical)
			shared := startPolicySession(t, store, &config)
			checkPolicyPhysicalRestore(t, shared)
			// A successful periodic save resets the crash-loop counter.
			if err := shared.SaveState(t.Context(), session.SavePeriodic); err != nil {
				t.Fatal(err)
			}
			old := mustRead(t, store)
			reply := shared.Apply(session.Command{Client: "failed-save", Sequence: 1, Epoch: shared.State().Epoch, Action: "speed", Speed: 15})
			if reply.Error != "" {
				t.Fatal(reply.Error)
			}
			shared.Close()
			ctx := t.Context()
			want := errSync
			if failure == "canceled write" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			} else {
				store.syncDir = func(string) error { return errSync }
			}
			if err := shared.SaveState(ctx, session.SaveFinal); !errors.Is(err, want) {
				t.Fatalf("failed save = %v, want %v", err, want)
			}
			if shared.Metrics().StateSaveErrors != 1 {
				t.Fatal("failed save did not update the error metric")
			}
			data := mustRead(t, store)
			var saved sim.SavedState
			if err := json.Unmarshal(policyStateJSON(t, data)["simulation"], &saved); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved, physical) {
				t.Fatal("failed save changed physical assignments or buffer membership")
			}
			if failure == "canceled write" && !bytes.Equal(data, old) {
				t.Fatal("canceled save replaced the previous state")
			}
			if failure == "directory sync" && bytes.Equal(data, old) {
				t.Fatal("sync failure fixture did not reach the completed rename")
			}
			store.syncDir = syncDirectory
			restored := startPolicySession(t, store, &config)
			checkPolicyPhysicalRestore(t, restored)
			if got, want := restored.State().Speed, 15; failure == "canceled write" {
				if got != 1 {
					t.Fatal("canceled save changed the restored speed")
				}
			} else if got != want {
				t.Fatal("completed rename did not retain the new valid state")
			}
			if err := shared.SaveState(t.Context(), session.SaveFinal); err != nil {
				t.Fatal(err)
			}
			if recovered := startPolicySession(t, store, &config); recovered.State().Speed != 15 {
				t.Fatal("successful retry did not recover the new state")
			}
		})
	}
}
