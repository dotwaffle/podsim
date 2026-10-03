package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// legacyTestState removes metadata that historical save formats did not write.
func legacyTestState(file stateFile, version int) stateFile {
	file.Version = version
	file.Project = project.Clone(file.Project)
	file.Project.ExpressServices = nil
	for i := range file.Project.Fleet {
		file.Project.Fleet[i].Class = ""
	}
	for i := range file.Project.Network.Lanes {
		file.Project.Network.Lanes[i].VehicleClasses = 0
	}
	for i := range file.Project.Network.Stations {
		station := &file.Project.Network.Stations[i]
		station.VehicleClasses = 0
		for j := range station.Berths {
			station.Berths[j].VehicleClasses = 0
		}
	}
	strip := func(request sim.SavedRequest) sim.SavedRequest {
		request.SharingConsent, request.Service, request.ServiceID = "", "", ""
		request.LegacyPartySize = false
		return request
	}
	file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
	for i := range file.Simulation.Pods {
		pod := &file.Simulation.Pods[i]
		pod.Class, pod.LegacyCohort = "", false
		pod.Riders = slices.Clone(pod.Riders)
		for j := range pod.Riders {
			pod.Riders[j] = strip(pod.Riders[j])
		}
	}
	file.Simulation.Waiting = slices.Clone(file.Simulation.Waiting)
	for i := range file.Simulation.Waiting {
		file.Simulation.Waiting[i].Request = strip(file.Simulation.Waiting[i].Request)
	}
	return file
}

func legacyStateMembersType(typ reflect.Type) reflect.Type {
	typ = withoutMember(typ, reflect.TypeFor[project.Config](), "expressServices", "stationQueueSpacing")
	typ = withoutMember(typ, reflect.TypeFor[sim.SavedPod](), "class", "legacyCohort", "stationBuffered", "compactQueue", "boardings")
	typ = withoutMember(typ, reflect.TypeFor[sim.SavedRequest](), "sharingConsent", "service", "serviceID", "legacyPartySize")
	typ = withoutMember(typ, reflect.TypeFor[sim.SavedPlatoonLink](), "kind", "terminalCell")
	typ = withoutMember(typ, reflect.TypeFor[sim.Placement](), "Class")
	typ = withoutMember(typ, reflect.TypeFor[sim.Lane](), "VehicleClasses")
	typ = withoutMember(typ, reflect.TypeFor[sim.Station](), "VehicleClasses", "Banks")
	return withoutMember(typ, reflect.TypeFor[sim.Berth](), "VehicleClasses")
}

// TestCurrentStateGolden records the complete version 6 member contract.
func TestCurrentStateGolden(t *testing.T) {
	file := newTestStateFile(t)
	data := decompressTestJSON(t, encodeTestState(t, file))
	if *update {
		value := jsontext.Value(data)
		if err := value.Indent(jsontext.WithIndent("  ")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/state_v6.json", append(value, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile("testdata/state_v6.json")
	if err != nil {
		t.Fatal(err)
	}
	want := jsontext.Value(golden)
	if compactErr := want.Compact(); compactErr != nil {
		t.Fatal(compactErr)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("version 6 golden differs from the current capture")
	}
	decoded, err := decodeStateFile(compressTestJSON(t, want))
	if err != nil || decoded.Version != serviceStateVersion {
		t.Fatalf("version 6 golden decode: %v", err)
	}
	membersType := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[sim.SavedPod](), "boardings")
	lines := stateMembers(t, "", membersType, nil)
	// The session adapter writes tuples instead of native boarding objects.
	lines = append(lines, stateMembers(t, "simulation.pods[].boardings", reflect.TypeFor[[][2]float64](), nil)...)
	members := strings.Join(lines, "\n") + "\n"
	const path = "testdata/state_v6_members.txt"
	if *update {
		if writeErr := os.WriteFile(path, []byte(members), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	wantMembers, err := os.ReadFile(path)
	if err != nil || string(wantMembers) != members {
		t.Fatalf("version 6 member contract differs: %v", err)
	}
}

func TestSavedOrderFieldVersionAndNullGates(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		name, object, value string
	}{
		{"class", "pod", `"legacy"`},
		{"legacyCohort", "pod", "false"},
		{"sharingConsent", "rider", `"shared"`},
		{"service", "rider", `"on-demand"`},
		{"serviceID", "rider", `"hub-pair"`},
		{"legacyPartySize", "rider", "false"},
		{"sharingConsent", "waiting", `"private"`},
		{"service", "waiting", `"on-demand"`},
		{"serviceID", "waiting", `"hub-pair"`},
		{"legacyPartySize", "waiting", "false"},
	} {
		t.Run(field.object+"/"+field.name, func(t *testing.T) {
			t.Parallel()
			raw := func(value string) []byte {
				member := fmt.Sprintf(`%q:%s`, field.name, value)
				switch field.object {
				case "pod":
					return []byte(`{"simulation":{"pods":[{` + member + `}]}}`)
				case "rider":
					return []byte(`{"simulation":{"pods":[{"riders":[{` + member + `}]}]}}`)
				default:
					return []byte(`{"simulation":{"waiting":[{"request":{` + member + `}}]}}`)
				}
			}
			// Exercise the loader boundary as well as the field scanner.
			for _, value := range []string{field.value, "null"} {
				file := legacyTestState(newTestStateFile(t), stateVersion)
				var object map[string]any
				if err := json.Unmarshal(decompressTestJSON(t, encodeTestState(t, file)), &object); err != nil {
					t.Fatal(err)
				}
				simulation := savedTestObject(t, object["simulation"])
				var target map[string]any
				if field.object == "waiting" {
					target = savedTestObject(t, savedTestObject(t, savedTestArray(t, simulation["waiting"])[0])["request"])
				} else {
					target = savedTestObject(t, savedTestArray(t, simulation["pods"])[0])
					if field.object == "rider" {
						// The example capture can retain its journey outside the first pod.
						for _, pod := range savedTestArray(t, simulation["pods"]) {
							if riders, ok := savedTestObject(t, pod)["riders"].([]any); ok && len(riders) > 0 {
								target = savedTestObject(t, riders[0])
								break
							}
						}
					}
				}
				var decoded any
				if err := json.Unmarshal([]byte(value), &decoded); err != nil {
					t.Fatal(err)
				}
				target[field.name] = decoded
				data, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := decodeStateFile(compressTestJSON(t, data)); err == nil {
					t.Fatalf("legacy decoder ignored present %s", value)
				}
			}
			for version := stateVersion; version < serviceStateVersion; version++ {
				for _, value := range []string{field.value, "null"} {
					if err := scanStateOrderFields(raw(value), version); err == nil {
						t.Fatalf("version %d admitted %s", version, value)
					}
				}
			}
			if err := scanStateOrderFields(raw(field.value), serviceStateVersion); err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"null", `""`, "0", "{}", "[]"} {
				if err := scanStateOrderFields(raw(value), serviceStateVersion); err == nil {
					t.Fatalf("version 6 admitted %s", value)
				}
			}
		})
	}
}

func TestNativeRestoreRequiresEffectiveOrderOptions(t *testing.T) {
	t.Parallel()
	file := legacyTestState(newTestStateFile(t), stateVersion)
	if _, _, err := sim.RestoreState(sim.RestoreStateInput{Network: file.Project.Network, Fleet: file.Project.Fleet, State: file.Simulation}); err == nil {
		t.Fatal("direct native restore invented effective order options")
	}
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion, bankStateVersion} {
		file.Version = version
		if version == bankStateVersion {
			config := project.Default()
			config.Version = project.BankVersion
			config.Network = sim.BankExample()
			config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
			banked, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := banked.simulation.SubmitTrip("origin", "hub"); err != nil {
				t.Fatal(err)
			}
			file = legacyTestState(sessionStateFile(t, banked), version)
		}
		s := newTestSession(t)
		loaded, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
		if err != nil {
			t.Fatalf("explicit legacy migration, version %d: %v", version, err)
		}
		for _, trip := range loaded.simulation.ExportState().Waiting {
			if trip.Request.SharingConsent != sim.PrivateConsent || trip.Request.Service != sim.OnDemandService {
				t.Fatalf("legacy pending order gained sharing consent: %+v", trip.Request)
			}
		}
	}
}

func TestCurrentRestoreRejectsMissingOrderOptions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"consent", "service"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file := newTestStateFile(t)
			request := &file.Simulation.Waiting[0].Request
			if name == "consent" {
				request.SharingConsent = ""
			} else {
				request.Service = ""
			}
			s := newTestSession(t)
			if _, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()}); err == nil {
				t.Fatal("version 6 restore invented a missing effective option")
			}
		})
	}
}

func TestOrderConsentAndClassSurviveTwoRestarts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		class   sim.VehicleClass
		consent sim.SharingConsent
		parties int
		size    int
	}{
		{"private compact group", sim.CompactClass, sim.PrivateConsent, 1, 4},
		{"explicit shared singleton", sim.LegacyClass, sim.SharedConsent, 2, 1},
		{"omitted consent stays private", sim.LegacyClass, "", 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := project.Default()
			config.Version = 3
			config.SharedRidePartyLimit = 4
			config.Fleet = config.Fleet[:1]
			config.Fleet[0].Class = test.class
			s, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			for range test.parties {
				if _, err := s.simulation.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", PartySize: test.size, SharingConsent: test.consent}); err != nil {
					t.Fatal(err)
				}
			}
			want := s.simulation.ExportState()
			if len(want.Pods[0].Riders) != test.parties {
				t.Fatalf("fixture did not board %d parties: %+v", test.parties, want)
			}
			consent := test.consent
			if consent == "" {
				consent = sim.PrivateConsent
			}
			for restart := range 2 {
				file := sessionStateFile(t, s)
				file.RestoreAttempts = 0
				loaded, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
				if err != nil || loaded.result.Tier != sim.RestorePhysical {
					t.Fatalf("restart %d: %v, tier %v", restart, err, loaded.result.Tier)
				}
				s.simulation = loaded.simulation
				got := s.simulation.ExportState()
				if got.RequestID != want.RequestID || got.Completed != want.Completed || got.Boarded != want.Boarded || len(got.Pods[0].Riders) != test.parties || got.Pods[0].Class != test.class {
					t.Fatalf("restart changed class or conservation counts: %+v", got)
				}
				for i, rider := range got.Pods[0].Riders {
					if rider.ID != want.Pods[0].Riders[i].ID || rider.PartySize != test.size || rider.SharingConsent != consent || rider.Service != sim.OnDemandService || rider.ServiceID != "" {
						t.Fatalf("restart changed effective options: %+v", rider)
					}
				}
			}
		})
	}
}

func TestLegacyClosedCohortSurvivesSessionRestarts(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Fleet = config.Fleet[:1]
	config.SharedRidePartyLimit = 4
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, submitErr := s.simulation.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", SharingConsent: sim.SharedConsent}); submitErr != nil {
			t.Fatal(submitErr)
		}
	}
	legacy := legacyTestState(sessionStateFile(t, s), stateVersion)
	legacy.RestoreAttempts = 0
	want := legacy.Simulation
	loaded, err := s.loadState(loadInput{data: encodeTestState(t, legacy), steps: realRestoreSteps()})
	if err != nil || loaded.result.Tier != sim.RestorePhysical {
		t.Fatalf("legacy cohort migration: %v, tier %v", err, loaded.result.Tier)
	}
	s.simulation = loaded.simulation
	if _, submitErr := s.simulation.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", SharingConsent: sim.SharedConsent}); submitErr != nil {
		t.Fatal(submitErr)
	}
	for restart := range 2 {
		file := sessionStateFile(t, s)
		file.RestoreAttempts = 0
		loaded, err = s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
		if err != nil || loaded.result.Tier != sim.RestorePhysical {
			t.Fatalf("restart %d: %v, tier %v", restart, err, loaded.result.Tier)
		}
		s.simulation = loaded.simulation
		got := s.simulation.ExportState()
		if !got.Pods[0].LegacyCohort || len(got.Pods[0].Riders) != 2 || len(got.Waiting) != 1 || got.Waiting[0].Request.PodID != "" || got.RequestID != want.RequestID+1 || got.Boarded != want.Boarded || got.Completed != want.Completed || !slices.Equal(got.Pods[0].Stops, want.Pods[0].Stops) || !slices.Equal(got.Pods[0].Route, want.Pods[0].Route) {
			t.Fatalf("restart opened or changed historical cohort: %+v", got)
		}
		for i, rider := range got.Pods[0].Riders {
			if rider.SharingConsent != sim.LegacyUnknownConsent || rider.Service != sim.OnDemandService || rider.ID != want.Pods[0].Riders[i].ID {
				t.Fatalf("restart invented historical consent: %+v", rider)
			}
		}
	}
	file := sessionStateFile(t, s)
	file.RestoreAttempts = 1
	logical, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
	if err != nil || len(logical.result.Requeued) != 2 {
		t.Fatalf("logical cohort restore: %v, result %+v", err, logical.result)
	}
	got := logical.simulation.ExportState()
	if got.Pods[0].LegacyCohort || len(got.Waiting) != 3 || got.RequestID != want.RequestID+1 || got.Completed != want.Completed {
		t.Fatalf("logical requeue changed conservation: %+v", got)
	}
	for _, trip := range got.Waiting {
		if trip.Request.SharingConsent == sim.LegacyUnknownConsent || trip.Request.Service != sim.OnDemandService {
			t.Fatalf("logical requeue retained historical consent: %+v", trip.Request)
		}
		if trip.Request.ID <= want.RequestID && trip.Request.SharingConsent != sim.PrivateConsent {
			t.Fatalf("historical party was not requeued privately: %+v", trip.Request)
		}
	}
}

func TestLegacyPendingPartiesRemainPrivateAcrossRestarts(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.SharedRidePartyLimit = 4
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	file := legacyTestState(sessionStateFile(t, s), stateVersion)
	file.RestoreAttempts = 0
	file.Simulation.RequestID = 2
	file.Simulation.Waiting = []sim.SavedTrip{
		{Request: sim.SavedRequest{ID: 1, From: "harbor", To: "market", PartySize: 2, RequestedTick: 0}},
		{Request: sim.SavedRequest{ID: 2, From: "harbor", To: "market", PartySize: 12, RequestedTick: 0}},
	}
	for restart := range 3 {
		loaded, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
		if err != nil || len(loaded.result.Dropped) != 0 {
			t.Fatalf("restart %d dropped a historical party: %v, %+v", restart, err, loaded.result)
		}
		s.simulation = loaded.simulation
		for range 2 * sim.TicksPerSecond {
			s.simulation.Step()
		}
		got := s.simulation.ExportState()
		if len(got.Waiting) != 2 || got.RequestID != 2 || got.Completed != 0 || got.Boarded != 0 {
			t.Fatalf("restart split or dropped historical parties: %+v", got)
		}
		for i, trip := range got.Waiting {
			want := []int{2, 12}[i]
			if trip.Request.ID != i+1 || trip.Request.PartySize != want || trip.Request.RequestedTick != 0 || trip.Request.SharingConsent != sim.PrivateConsent || trip.Request.Service != sim.OnDemandService || trip.Request.PodID != "" || trip.Request.DispatchReason == "" || trip.Request.LegacyPartySize != (want > sim.MaxNewPartySize) {
				t.Fatalf("restart changed historical party: %+v", trip.Request)
			}
		}
		file = sessionStateFile(t, s)
		file.RestoreAttempts = 0
	}
}

func TestCurrentSavedClassAndServiceRejections(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*stateFile)
	}{
		{"group class differs from saved fleet", func(file *stateFile) { file.Simulation.Pods[0].Class = sim.GroupClass }},
		{"unsupported express", func(file *stateFile) { file.Simulation.Pods[0].Class = sim.ExpressClass }},
		{"unknown class", func(file *stateFile) { file.Simulation.Pods[0].Class = "unknown" }},
		{"immutable class mismatch", func(file *stateFile) { file.Simulation.Pods[0].Class = sim.CompactClass }},
		{"on-demand service ID", func(file *stateFile) { file.Simulation.Waiting[0].Request.ServiceID = "hub-pair" }},
		{"private express", func(file *stateFile) {
			file.Simulation.Waiting[0].Request.SharingConsent = sim.PrivateConsent
			file.Simulation.Waiting[0].Request.Service = sim.ExpressServiceChoice
			file.Simulation.Waiting[0].Request.ServiceID = "hub-pair"
		}},
		{"unknown pending consent", func(file *stateFile) { file.Simulation.Waiting[0].Request.SharingConsent = sim.LegacyUnknownConsent }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, attempts := range []int{0, 1} {
				file := newTestStateFile(t)
				file.RestoreAttempts = attempts
				test.change(&file)
				s := newTestSession(t)
				if _, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()}); err == nil {
					t.Fatalf("restore attempts %d admitted invalid class or order", attempts)
				}
			}
		})
	}
}

func TestCurrentSupportedSavedCounts(t *testing.T) {
	t.Parallel()
	if sim.MaxSavedWaitingTrips != 2600 || sim.MaxSharedRideParties != 8 {
		t.Fatal("supported-profile counts changed without byte qualification")
	}
	t.Run("waiting", func(t *testing.T) {
		t.Parallel()
		s := newTestSession(t)
		for _, count := range []int{sim.MaxSavedWaitingTrips, sim.MaxSavedWaitingTrips + 1} {
			file := sessionStateFile(t, s)
			file.RestoreAttempts = 0
			file.Simulation.RequestID = count
			file.Simulation.Waiting = make([]sim.SavedTrip, count)
			for i := range file.Simulation.Waiting {
				file.Simulation.Waiting[i].Request = sim.SavedRequest{ID: i + 1, From: "harbor", To: "market", PartySize: 2, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
			}
			data := encodeTestState(t, file)
			if _, err := decodeStateFile(data); err != nil {
				t.Fatalf("save 6 parser rejected recognized count %d: %v", count, err)
			}
			for _, attempts := range []int{0, 1} {
				file.RestoreAttempts = attempts
				_, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
				if (err == nil) != (count == sim.MaxSavedWaitingTrips) {
					t.Fatalf("count %d, attempts %d: %v", count, attempts, err)
				}
			}
		}
	})
	t.Run("stored riders", func(t *testing.T) {
		t.Parallel()
		config := project.Default()
		config.Fleet = config.Fleet[:1]
		config.SharedRidePartyLimit = sim.MaxSharedRideParties
		s, err := NewWithProject(config)
		if err != nil {
			t.Fatal(err)
		}
		for range sim.MaxSharedRideParties {
			if _, err := s.simulation.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", SharingConsent: sim.SharedConsent}); err != nil {
				t.Fatal(err)
			}
		}
		file := sessionStateFile(t, s)
		file.RestoreAttempts = 0
		if len(file.Simulation.Pods[0].Riders) != sim.MaxSharedRideParties {
			t.Fatal("fixture did not reach eight stored riders")
		}
		if _, err := s.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()}); err != nil {
			t.Fatal(err)
		}
		extra := file.Simulation.Pods[0].Riders[0]
		file.Simulation.RequestID++
		extra.ID = file.Simulation.RequestID
		file.Simulation.Pods[0].Riders = append(file.Simulation.Pods[0].Riders, extra)
		data := encodeTestState(t, file)
		if _, err := decodeStateFile(data); err != nil {
			t.Fatalf("save 6 parser rejected recognized rider shape: %v", err)
		}
		if _, err := s.loadState(loadInput{data: data, steps: realRestoreSteps()}); err == nil {
			t.Fatal("supported profile restored nine stored riders")
		}
	})
}

func savedTestObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("saved fixture needs an object, got %T", value)
	}
	return object
}

func savedTestArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok || len(array) == 0 {
		t.Fatalf("saved fixture needs a nonempty array, got %T", value)
	}
	return array
}
