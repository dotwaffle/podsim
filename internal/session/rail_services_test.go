package session

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

func railServicesProject() project.Config {
	config := project.Default()
	config.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: "rail-services", Seed: 7}
	config.RailArrivals = []project.RailArrival{{ID: "train", Station: "garden", Passengers: 1, Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
	config.RailDepartures = []project.RailDeparture{
		{ID: "early", Station: "harbor", AtSeconds: 10, Passengers: 2, Origins: []project.RailOrigin{{Station: "market", Weight: 1}}},
		{ID: "later", Station: "harbor", AtSeconds: 600, WalkingSeconds: 2, RequestFromSeconds: 1, RequestUntilSeconds: 2, Passengers: 3, Origins: []project.RailOrigin{{Station: "market", Weight: 1}, {Station: "garden", Weight: 1}}},
	}
	return config
}

func TestRailServicesLifecycle(t *testing.T) {
	t.Parallel()
	config := railServicesProject()
	s := newRailSession(t, config)
	s.step()
	if s.demand.state.Generated != 3 || s.demand.state.Connections != (rail.Counts{Unresolved: 2}) {
		t.Fatalf("first releases: %+v", s.demand.state)
	}
	records := s.demand.connectionRecords()
	if records[0].RequestID != 2 || records[1].RequestID != 3 {
		t.Fatal("tie did not issue arrivals first")
	}
	config.Demand.Seed++
	if err := s.applyDemand(config.Demand, nil); err != nil {
		t.Fatal(err)
	}
	if s.demand.state.Generated != 0 || !slices.Equal(records, s.demand.connectionRecords()) {
		t.Fatal("reseed erased ledger")
	}
	for s.simulation.Tick() < 120 {
		s.step()
	}
	if len(s.demand.connectionRecords()) != 5 || s.demand.state.Generated != 3 {
		t.Fatal("reseed replayed or lost identities")
	}
	config.Demand.Enabled = false
	if err := s.applyDemand(config.Demand, nil); err != nil {
		t.Fatal(err)
	}
	for s.simulation.Tick() < 600*sim.TicksPerSecond {
		s.step()
	}
	counts := s.demand.state.Connections
	if counts.Unresolved != 0 || counts.Made+counts.Missed != 5 || counts.Made == 0 || counts.Missed == 0 {
		t.Fatalf("disabled deadline outcomes: %+v", counts)
	}
	before := s.demand.connectionRecords()
	config.Demand.Enabled = true
	config.Demand.Pattern = "balanced"
	if err := s.applyDemand(config.Demand, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, s.demand.connectionRecords()) || s.demand.state.Connections != counts {
		t.Fatal("pattern switch erased ledger")
	}
	if err := s.simulation.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, s.demand.connectionRecords()) {
		t.Fatal("manual order acquired deadline")
	}
	client := newTestClient(s, "services")
	client.mustApply(t, Command{Action: "reset"})
	if len(s.demand.connectionRecords()) != 0 || s.demand.state.Connections != (rail.Counts{}) {
		t.Fatal("reset retained connections")
	}
}

func TestRailServicesQueueRejections(t *testing.T) {
	t.Parallel()
	s := newRailSession(t, railServicesProject())
	for s.simulation.PendingCount() < QueueLimit {
		if err := s.simulation.RequestTrip("garden", "market"); err != nil {
			t.Fatal(err)
		}
	}
	s.step()
	if s.demand.state.Connections != (rail.Counts{Unserved: 2}) || s.demand.state.Skipped != 3 {
		t.Fatalf("rejections: %+v", s.demand.state)
	}
	for _, record := range s.demand.connectionRecords() {
		if record.RequestID != 0 || record.Reason != "queue-limit" {
			t.Fatal(record)
		}
	}
}

func TestRailServicesCheckpointAndRestart(t *testing.T) {
	t.Parallel()
	s := newRailSession(t, railServicesProject())
	for range 120 {
		s.step()
	}
	cloneSim, cloneDemand := s.simulation.Clone(), s.demand.clone()
	if cloneDemand.connections == s.demand.connections || &cloneDemand.serviceOffers[0] != &s.demand.serviceOffers[0] {
		t.Fatal("incorrect clone ownership")
	}
	client := newTestClient(s, "services")
	checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	saved := sessionStateFile(t, s)
	saved.RestoreAttempts = 0
	restarted := startFromStore(t, StoreInput{Store: &fakeStore{data: encodeTestState(t, saved)}})
	t.Cleanup(restarted.Close)
	if restarted.restore.Tier != "physical" {
		t.Fatal(restarted.restore)
	}
	for range 600 {
		s.step()
		restarted.step()
		cloneSim.Step()
		cloneDemand.step(cloneSim)
	}
	want := s.demand.connectionRecords()
	if !slices.Equal(want, restarted.demand.connectionRecords()) || !slices.Equal(want, cloneDemand.connectionRecords()) || s.demand.state != cloneDemand.state || s.demand.state != restarted.demand.state {
		t.Fatal("clone or physical restore lost connections")
	}
	client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
	client.mustApply(t, Command{Action: "pause", Paused: false})
	for range 600 {
		s.step()
	}
	if !slices.Equal(want, s.demand.connectionRecords()) {
		t.Fatal("checkpoint replay changed connections")
	}
	saved = sessionStateFile(t, s)
	saved.RestoreAttempts = 0
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion} {
		saved.Version = version
		loaded, err := s.loadState(loadInput{data: encodeTestState(t, saved), steps: realRestoreSteps()})
		if err != nil || !slices.Equal(want, loaded.demand.connectionRecords()) {
			t.Fatalf("version %d: %v", version, err)
		}
	}
}

func TestRailServicesSavedLedgerValidation(t *testing.T) {
	t.Parallel()
	s := newRailSession(t, railServicesProject())
	s.step()
	for _, tc := range []struct {
		name string
		edit func(*stateFile)
	}{
		{"wrong counts", func(f *stateFile) { f.Demand.State.Connections.Unserved++ }},
		{"missing ledger", func(f *stateFile) { f.RailConnections = nil }},
		{"missing plan", func(f *stateFile) { f.Project.RailDepartures = nil }},
		{"changed binding", func(f *stateFile) { f.RailConnections[0].From = "garden" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := sessionStateFile(t, s)
			tc.edit(&f)
			if _, err := decodeCheckedState(encodeTestState(t, f)); err == nil {
				t.Fatal("accepted malformed connection state")
			}
		})
	}
	state := DemandState{Config: project.Default().Demand}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["connections"]; ok {
		t.Fatal("zero connections changed legacy JSON")
	}
	state.Connections = rail.Counts{Unresolved: 1}
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip DemandState
	if err := json.Unmarshal(data, &roundtrip); err != nil || !reflect.DeepEqual(state, roundtrip) {
		t.Fatalf("count roundtrip: %v", err)
	}
}

func TestRailServicesMaximumValidLedger(t *testing.T) {
	t.Parallel()
	config := project.Default()
	renamed := map[string]string{}
	for i := range config.Network.Stations {
		station := &config.Network.Stations[i]
		renamed[station.ID] = strings.Repeat("\x01", 61) + fmt.Sprintf("%03d", i)
		station.ID = renamed[station.ID]
	}
	for i := range config.Network.Lanes {
		if id := config.Network.Lanes[i].StationID; id != "" {
			config.Network.Lanes[i].StationID = renamed[id]
		}
	}
	for i := range config.Fleet {
		config.Fleet[i].StationID = renamed[config.Fleet[i].StationID]
	}
	config.Demand = DemandConfig{Pattern: "rail-services", PerMinute: 12, Seed: 7}
	passenger := project.PassengerStations(config.Network)
	for i := range project.MaxRailDeparturePassengers / 200 {
		config.RailDepartures = append(config.RailDepartures, project.RailDeparture{ID: strings.Repeat("\x01", 61) + fmt.Sprintf("%03d", i), Station: passenger[0].ID, AtSeconds: 600, RequestFromSeconds: i, RequestUntilSeconds: i, Passengers: 200, Origins: []project.RailOrigin{{Station: passenger[1].ID, Weight: 1}}})
	}
	s := newRailSession(t, config)
	for range 15 * sim.TicksPerSecond {
		s.step()
	}
	for _, offer := range project.RailServicesSchedule(nil, config.RailDepartures, 7) {
		if err := s.demand.connections.Add(offer, 0, "queue-limit"); err != nil {
			t.Fatal(err)
		}
	}
	s.demand.state.Connections = s.demand.connections.Counts()
	file := sessionStateFile(t, s)
	file.RestoreAttempts = 0
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion} {
		file.Version = version
		data := encodeTestState(t, file)
		decoded, err := decodeCheckedState(data)
		if err != nil || len(decoded.RailConnections) != project.MaxRailDeparturePassengers {
			t.Fatalf("max valid version%d: %v", version, err)
		}
		loaded, err := s.loadState(loadInput{data: data, steps: realRestoreSteps()})
		if err != nil || loaded.result.Tier != sim.RestorePhysical || !slices.Equal(s.demand.connectionRecords(), loaded.demand.connectionRecords()) {
			t.Fatalf("max physical version%d: %v", version, err)
		}
	}
}

func TestRailServicesLogicalRestoreReceipts(t *testing.T) {
	t.Parallel()
	t.Run("completed queue drop", func(t *testing.T) {
		s := newRailSession(t, railServicesProject())
		s.step()
		file := sessionStateFile(t, s)
		id := file.RailConnections[0].RequestID
		found := false
		for i := range file.Simulation.Waiting {
			if file.Simulation.Waiting[i].Request.ID == id {
				file.Simulation.Waiting[i].Request.Completed = true
				found = true
			}
		}
		if !found {
			t.Fatal("fixture connection is not queued")
		}
		file.RestoreAttempts = restoreLoopAttempts - 1
		loaded, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
		if err != nil || !slices.Contains(loaded.result.Dropped, id) || loaded.demand.connectionRecords()[0].Reason != "restore-drop" {
			t.Fatalf("drop receipt: %v %+v", err, loaded.result)
		}
	})
	t.Run("untimed unloading", func(t *testing.T) {
		s := newRailSession(t, railServicesProject())
		var file stateFile
		id := 0
		for s.simulation.Tick() < 590*sim.TicksPerSecond && id == 0 {
			s.step()
			saved := s.simulation.ExportState()
			for _, pod := range saved.Pods {
				if pod.Activity == "unloading" {
					for _, rider := range pod.Riders {
						if !rider.Completed {
							for _, record := range s.demand.connectionRecords() {
								if record.RequestID == rider.ID && record.Outcome == "pending" && record.AlightedTick == -1 {
									id = rider.ID
								}
							}
						}
					}
				}
			}
			if id > 0 {
				file = sessionStateFile(t, s)
			}
		}
		if id == 0 {
			t.Fatal("no pending outbound unloading fixture")
		}
		file.RestoreAttempts = restoreLoopAttempts - 1
		loaded, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
		if err != nil || !slices.Contains(loaded.result.LogicalCompleted, id) {
			t.Fatalf("unloading receipt: %v %+v", err, loaded.result)
		}
		found := false
		for _, record := range loaded.demand.connectionRecords() {
			if record.RequestID == id {
				found = record.Reason == "restore-degraded" && record.Outcome == "unserved" && record.AlightedTick == -1
			}
		}
		if !found {
			t.Fatal("logical completion acquired a timed connection outcome")
		}
	})
}
