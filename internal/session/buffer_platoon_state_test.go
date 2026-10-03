package session

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"encoding/json/v2"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestBufferPlatoonFieldPresence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, fields string
		validV4      bool
	}{
		{"omitted", "", true},
		{"empty kind", `"kind":"",`, true},
		{"buffer", `"kind":"buffer","terminalCell":4,`, true},
		{"null kind", `"kind":null,`, false},
		{"numeric kind", `"kind":1,`, false},
		{"unknown kind", `"kind":"other",`, false},
		{"missing endpoint", `"kind":"buffer",`, false},
		{"null endpoint", `"kind":"buffer","terminalCell":null,`, false},
		{"fractional endpoint", `"kind":"buffer","terminalCell":1.5,`, false},
		{"negative endpoint", `"kind":"buffer","terminalCell":-1,`, false},
		{"string endpoint", `"kind":"buffer","terminalCell":"4",`, false},
		{"legacy endpoint", `"terminalCell":4,`, false},
		{"legacy null endpoint", `"terminalCell":null,`, false},
		{"overflow endpoint", `"kind":"buffer","terminalCell":18446744073709551616,`, false},
		{"duplicate kind", `"kind":"buffer","kind":"buffer","terminalCell":4,`, false},
		{"duplicate endpoint", `"kind":"buffer","terminalCell":4,"terminalCell":4,`, false},
	}
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				t.Parallel()
				file := legacyTestState(platoonStateFile(t), version)
				file.Simulation.Pods[1].Platoon.Lanes = 1
				raw, err := json.Marshal(file)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte(`"platoon":{`), []byte(`"platoon":{`+tc.fields), 1)
				got, err := decodeStateFile(compressTestJSON(t, raw))
				valid := tc.fields == "" || version == bufferPlatoonStateVersion && tc.validV4
				if (err == nil) != valid {
					t.Fatalf("decode accepted=%t want=%t: %v", err == nil, valid, err)
				}
				if valid && tc.name == "buffer" {
					link := got.Simulation.Pods[1].Platoon
					if link.Kind != "buffer" || link.TerminalCell == nil || *link.TerminalCell != 4 {
						t.Fatal("decoded certificate fields differ")
					}
				}
			})
		}
	}
}

func TestBufferV4MemberList(t *testing.T) {
	t.Parallel()
	currentType := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[sim.SavedPod](), "compactQueue")
	v3Type := withoutMember(currentType, reflect.TypeFor[sim.SavedPlatoonLink](), "kind", "terminalCell")
	v3 := stateMembers(t, "", v3Type, nil)
	v4 := stateMembers(t, "", currentType, nil)
	var extra []string
	for _, member := range v4 {
		if !slices.Contains(v3, member) {
			extra = append(extra, member)
		}
	}
	want := []string{"simulation.pods[].platoon.kind string", "simulation.pods[].platoon.terminalCell number"}
	if !slices.Equal(extra, want) {
		t.Fatalf("unexpected v4 extension: %v", extra)
	}
}

func bufferPlatoonSessionFixture(t *testing.T) *Session {
	t.Helper()
	config := project.Default()
	config.Fleet = []sim.Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}}
	config.StationBuffers, config.PlatoonLimit = true, 4
	for i := range config.Network.Nodes {
		config.Network.Nodes[i].Position.X *= 8
		config.Network.Nodes[i].Position.Y *= 8
	}
	for i := range config.Network.Lanes {
		if config.Network.Lanes[i].ID == "market-approach" {
			config.Network.Lanes[i].StationRole = sim.StationEntryRole
		}
	}
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	for _, placement := range config.Fleet {
		if requestErr := shared.simulation.RequestJourney(placement.ID, "market"); requestErr != nil {
			t.Fatal(requestErr)
		}
	}
	for range 60 * sim.TicksPerSecond {
		shared.advance()
	}
	state := shared.simulation.ExportState()
	for i := range state.Pods {
		pod := &state.Pods[i]
		position := float64(120 + (len(state.Pods)-i)*45)
		pod.RouteIndex = len(pod.Route) - 1
		pod.LaneID = config.Network.Lanes[pod.Route[pod.RouteIndex]].ID
		pod.LaneDistance, pod.Distance, pod.Waiting, pod.WaitSince = position, position, false, 0
		for _, index := range pod.Route[:pod.RouteIndex] {
			pod.Distance += config.Network.Length(config.Network.Lanes[index])
		}
	}
	restored, result, err := sim.RestoreState(sim.RestoreStateInput{Network: config.Network, Fleet: config.Fleet, State: state, StationBuffers: true})
	if err != nil || result.Tier != sim.RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("staged session restore: %+v %v", result, err)
	}
	restored.SetStationBuffers(true)
	if err := project.ConfigurePlatoons(restored, config); err != nil {
		t.Fatal(err)
	}
	shared.simulation = restored
	shared.advance()
	if !restored.NeedsBufferPlatoonState() {
		t.Fatal("staged session did not form an entry platoon")
	}
	shared.persist = &persistence{}
	return shared
}

func TestBufferPlatoonSessionRoundTrip(t *testing.T) {
	t.Parallel()
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint("disabled=", disabled), func(t *testing.T) {
			t.Parallel()
			shared := bufferPlatoonSessionFixture(t)
			if disabled {
				shared.project.StationBuffers, shared.project.PlatoonLimit = false, 0
				shared.simulation.SetStationBuffers(false)
				if err := shared.simulation.SetPlatooning(sim.PlatooningOff); err != nil {
					t.Fatal(err)
				}
			}
			shared.Close()
			file, write, err := shared.captureState(SaveFinal)
			if err != nil || !write || file.Version != serviceStateVersion {
				t.Fatalf("capture version=%d write=%t: %v", file.Version, write, err)
			}
			file.SavedAt = time.Date(2026, time.October, 1, 3, 0, 0, 0, time.UTC)
			data := encodeTestState(t, file)
			if got, err := decodeCheckedState(data); err != nil || !reflect.DeepEqual(got, file) {
				t.Fatalf("version4 codec changed state: %v", err)
			}
			store := &fakeStore{data: data}
			restored := startFromStore(t, StoreInput{Store: store, Project: &file.Project})
			t.Cleanup(restored.Close)
			info := restored.State().Restore
			if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped != 0 || !restored.simulation.NeedsBufferPlatoonState() {
				t.Fatalf("version4 session restore: %+v", info)
			}
			if got := restored.simulation.ExportState(); !reflect.DeepEqual(got, file.Simulation) {
				t.Fatal("session restore changed saved pose, membership, requests or certificate")
			}
			for range 600 * sim.TicksPerSecond {
				restored.advance()
				if _, err := restored.simulation.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				if restored.simulation.Snapshot().Completed == 2 {
					break
				}
			}
			if restored.simulation.Snapshot().Completed != 2 || restored.simulation.NeedsBufferPlatoonState() {
				t.Fatal("restored version4 members did not drain")
			}
			restored.Close()
			if err := restored.SaveState(t.Context(), SaveFinal); err != nil {
				t.Fatal(err)
			}
			want := serviceStateVersion
			if got := store.lastWrite(t).Version; got != want {
				t.Fatalf("drained save version=%d want=%d", got, want)
			}
		})
	}
}
