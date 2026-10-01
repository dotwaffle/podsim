package session

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestBufferStateVersions(t *testing.T) {
	t.Parallel()
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion, bufferPlatoonStateVersion + 1} {
		file := newTestStateFile(t)
		file.Version = version
		got, err := decodeCheckedState(encodeTestState(t, file))
		if version > bufferPlatoonStateVersion {
			if stateReason(err) != reasonUnsupportedVersion {
				t.Fatalf("future version accepted: %v", err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, file) {
			t.Fatalf("version %d failed: %v", version, err)
		}
	}
}

func TestBufferStateCaptureKeepsOrdinaryV2(t *testing.T) {
	t.Parallel()
	shared, err := NewWithProject(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	shared.persist = &persistence{}
	for _, enabled := range []bool{false, true, false} {
		shared.simulation.SetStationBuffers(enabled)
		file, write, err := shared.captureState(SavePeriodic)
		if err != nil || !write {
			t.Fatalf("capture failed: %t %v", write, err)
		}
		want := stateVersion
		if enabled {
			want = bufferStateVersion
		}
		if file.Version != want {
			t.Fatalf("enabled=%t version=%d want=%d", enabled, file.Version, want)
		}
	}
}

func TestBufferStateSessionRoundTrip(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Fleet = config.Fleet[:1]
	for index := range config.Network.Nodes {
		config.Network.Nodes[index].Position.X *= 4
		config.Network.Nodes[index].Position.Y *= 4
	}
	for index := range config.Network.Lanes {
		if config.Network.Lanes[index].ID == "market-approach" {
			config.Network.Lanes[index].StationRole = sim.StationEntryRole
		}
	}
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store, Project: &config})
	shared.simulation.SetStationBuffers(true)
	if err := shared.simulation.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	found := false
	for range 300 * sim.TicksPerSecond {
		shared.advance()
		state := shared.simulation.ExportState()
		if slices.ContainsFunc(state.Pods, func(p sim.SavedPod) bool {
			return p.Activity == "traveling" && p.LaneID == "market-approach" && p.Destination == ""
		}) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no berthless entry state")
	}
	shared.Close()
	if err := shared.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	file := store.lastWrite(t)
	if file.Version != bufferStateVersion {
		t.Fatalf("version%d", file.Version)
	}
	restoredStore := &fakeStore{data: store.writeList()[len(store.writeList())-1]}
	restored := startFromStore(t, StoreInput{Store: restoredStore, Project: &config})
	defer restored.Close()
	info := restored.State().Restore
	if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped != 0 || !restored.simulation.NeedsBufferState() {
		t.Fatalf("session buffer restore: %+v", info)
	}
	for range 300 * sim.TicksPerSecond {
		restored.advance()
		if restored.State().Simulation.Completed == 1 {
			break
		}
	}
	if restored.State().Simulation.Completed != 1 || restored.simulation.NeedsBufferState() {
		t.Fatal("restored session did not drain")
	}
	restored.Close()
	if err := restored.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	if got := restoredStore.lastWrite(t).Version; got != stateVersion {
		t.Fatalf("drained state retained v%d", got)
	}
}

func TestPickupBufferSessionDepartureRoundTrip(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Fleet = config.Fleet[:1]
	for index := range config.Network.Nodes {
		config.Network.Nodes[index].Position.X *= 4
		config.Network.Nodes[index].Position.Y *= 4
	}
	for index := range config.Network.Lanes {
		if config.Network.Lanes[index].ID == "market-approach" {
			config.Network.Lanes[index].StationRole = sim.StationEntryRole
		}
	}
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store, Project: &config})
	shared.simulation.SetStationBuffers(true)
	if err := shared.simulation.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	pod := shared.simulation.ExportState().Pods[0]
	if pod.Activity != "departing" || !pod.StationBuffered || pod.Destination != "" {
		t.Fatal("fixture did not save a berthless pickup departure")
	}
	shared.Close()
	if err := shared.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	if store.lastWrite(t).Version != bufferStateVersion {
		t.Fatal("buffer pickup departure did not require version 3")
	}
	data := store.writeList()[len(store.writeList())-1]
	restored := startFromStore(t, StoreInput{Store: &fakeStore{data: data}, Project: &config})
	defer restored.Close()
	info := restored.State().Restore
	if info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped != 0 {
		t.Fatalf("pickup departure did not restore physically: %+v", info)
	}
	if !restored.simulation.ExportState().Pods[0].StationBuffered || restored.simulation.PendingCount() != 1 {
		t.Fatal("file restore lost pickup membership or its order")
	}
	for range 1200 * sim.TicksPerSecond {
		restored.advance()
		if _, err := restored.simulation.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if restored.simulation.Snapshot().Completed == 1 {
			return
		}
	}
	t.Fatal("restored pickup did not complete its passenger journey")
}

func TestBufferMemberRequiresV3(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"true", "false"} {
		file := newTestStateFile(t)
		raw := decompressTestJSON(t, encodeTestState(t, file))
		raw = bytes.Replace(raw, []byte(`"pods":[{`), []byte(`"pods":[{"stationBuffered":`+value+`,`), 1)
		if !bytes.Contains(raw, []byte(`"stationBuffered"`)) {
			t.Fatal("failed to insert member")
		}
		_, err := decodeStateFile(compressTestJSON(t, raw))
		if stateReason(err) != reasonInvalidState {
			t.Fatalf("v2 accepted stationBuffered=%s: %v", value, err)
		}
		file.Version = bufferStateVersion
		raw = bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":3`), 1)
		if _, err := decodeStateFile(compressTestJSON(t, raw)); err != nil {
			t.Fatalf("v3 decode rejected member: %v", err)
		}
	}
}

func TestBufferV3MemberList(t *testing.T) {
	t.Parallel()
	legacy := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[sim.SavedPod](), "stationBuffered")
	legacy = withoutMember(legacy, reflect.TypeFor[sim.SavedPlatoonLink](), "kind", "terminalCell")
	v3Type := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[sim.SavedPlatoonLink](), "kind", "terminalCell")
	v2 := stateMembers(t, "", legacy, nil)
	v3 := stateMembers(t, "", v3Type, nil)
	extra := []string{}
	for _, member := range v3 {
		if !slices.Contains(v2, member) {
			extra = append(extra, member)
		}
	}
	if !slices.Equal(extra, []string{"simulation.pods[].stationBuffered boolean"}) {
		t.Fatalf("unexpected v3 extension: %v", extra)
	}
}
