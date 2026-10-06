package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCurrentStateGolden records the complete version 9 member contract.
func TestCurrentStateGolden(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	data := decompressTestJSON(t, encodeTestState(t, file))
	if *update {
		value := jsontext.Value(data)
		if err := value.Indent(jsontext.WithIndent("  ")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("testdata/state_v9.json", append(value, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile("testdata/state_v9.json")
	if err != nil {
		t.Fatal(err)
	}
	want := jsontext.Value(golden)
	if compactErr := want.Compact(); compactErr != nil {
		t.Fatal(compactErr)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("version 9 golden differs from the current capture")
	}
	decoded, err := decodeStateFile(compressTestJSON(t, want))
	if err != nil || decoded.Version != stateVersion {
		t.Fatalf("version 9 golden decode: %v", err)
	}
	membersType := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[sim.SavedPod](), "boardings")
	membersType = withoutMember(membersType, reflect.TypeFor[sim.SavedFaults](), "records")
	lines := stateMembers(t, "", membersType, nil)
	// The session adapter writes tuples instead of native boarding objects.
	lines = append(lines, stateMembers(t, "simulation.pods[].boardings", reflect.TypeFor[[][2]float64](), nil)...)
	// It also writes the stage 1 references as indexes, and the operational
	// destination as one tuple (incident contract, section 11.5).
	lines = append(lines, stateMembers(t, "simulation.pods[].operational", reflect.TypeFor[[]uint32](), nil)...)
	lines = append(lines, stateMembers(t, "simulation.pods[].riders[].legFrom", reflect.TypeFor[int](), nil)...)
	lines = append(lines, stateMembers(t, "simulation.waiting[].request.legFrom", reflect.TypeFor[int](), nil)...)
	lines = append(lines, stateMembers(t, "simulation.waiting[].excludedPod", reflect.TypeFor[int](), nil)...)
	// Each fault record is one tuple of numbers (incident suspension
	// contract, section 13.3).
	lines = append(lines, stateMembers(t, "simulation.faults.records", reflect.TypeFor[[][]float64](), nil)...)
	members := strings.Join(lines, "\n") + "\n"
	const path = "testdata/state_v9_members.txt"
	if *update {
		if writeErr := os.WriteFile(path, []byte(members), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	wantMembers, err := os.ReadFile(path)
	if err != nil || string(wantMembers) != members {
		t.Fatalf("version 9 member contract differs: %v", err)
	}
}

func TestSavedOrderFieldNullGates(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		name, object, value string
	}{
		{"class", "pod", `"legacy"`},
		{"sharingConsent", "rider", `"shared"`},
		{"service", "rider", `"on-demand"`},
		{"serviceID", "rider", `"hub-pair"`},
		{"sharingConsent", "waiting", `"private"`},
		{"service", "waiting", `"on-demand"`},
		{"serviceID", "waiting", `"hub-pair"`},
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
			if err := scanStateOrderFields(raw(field.value), false); err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"null", `""`, "0", "{}", "[]"} {
				if err := scanStateOrderFields(raw(value), false); err == nil {
					t.Fatalf("the scan admitted %s", value)
				}
			}
		})
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
				t.Fatal("the restore invented a missing effective option")
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
		{"unknown pending consent", func(file *stateFile) { file.Simulation.Waiting[0].Request.SharingConsent = "legacy-unknown" }},
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
			if _, err := decodeStateFile(data); (err == nil) != (count == sim.MaxSavedWaitingTrips) {
				t.Fatalf("save 6 parser, count %d: %v", count, err)
			} else if err != nil {
				assertSavedArrayRefusal(t, err)
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
		_, decodeErr := decodeStateFile(data)
		assertSavedArrayRefusal(t, decodeErr)
		if _, err := s.loadState(loadInput{data: data, steps: realRestoreSteps()}); err == nil {
			t.Fatal("supported profile restored nine stored riders")
		}
	})
}

// assertSavedArrayRefusal checks that the bounded scan refused a saved
// array with the invalid_state reason.
func assertSavedArrayRefusal(t *testing.T, err error) {
	t.Helper()
	stateErr, ok := errors.AsType[*stateError](err)
	if !ok || stateErr.reason != reasonInvalidState || !errors.Is(err, errJSONArrayTooLong) {
		t.Fatalf("got %v, want an invalid_state array refusal", err)
	}
}

// TestSavedLegacyOrderMembersRejected checks that version 9 rejects the
// removed legacy order members. Earlier executables wrote them only as true.
func TestSavedLegacyOrderMembersRejected(t *testing.T) {
	t.Parallel()
	file := boardingTestFile(t)
	file.Simulation.Waiting = newTestStateFile(t).Simulation.Waiting
	raw := decompressTestJSON(t, encodeTestState(t, file))
	if _, err := decodeStateFile(compressTestJSON(t, raw)); err != nil {
		t.Fatal("control", err)
	}
	for _, test := range []struct{ name, anchor, member string }{
		{"pod", `"pods":[{`, `"legacyCohort":true,`},
		{"rider", `"riders":[{`, `"legacyPartySize":true,`},
		{"waiting", `"request":{`, `"legacyPartySize":true,`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !bytes.Contains(raw, []byte(test.anchor)) {
				t.Fatal("fixture lacks", test.anchor)
			}
			changed := bytes.Replace(raw, []byte(test.anchor), []byte(test.anchor+test.member), 1)
			name := strings.Split(test.member, `"`)[1]
			if _, err := decodeStateFile(compressTestJSON(t, changed)); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("removed member %s: %v", name, err)
			}
		})
	}
}

// A saved state member whose case differs from the declared name is
// unknown, so the decoder refuses the file. The cases cover the plain and
// the coupling state and a member of the saved project.
func TestStateFileRefusesCaseVariantMembers(t *testing.T) {
	t.Parallel()
	golden, err := os.ReadFile("testdata/state_v9.json")
	if err != nil {
		t.Fatal(err)
	}
	data := couplingPhaseFixtures(t)
	coupling := decompressTestJSON(t, encodeTestState(t, couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))))
	for _, test := range []struct {
		name     string
		raw      []byte
		from, to string
	}{
		{"plain tick", golden, `"tick":`, `"Tick":`},
		{"plain rider", golden, `"dispatchReason":`, `"DispatchReason":`},
		{"plain project", golden, `"nodes":`, `"Nodes":`},
		{"coupling tick", coupling, `"tick":`, `"Tick":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := decodeStateFile(compressTestJSON(t, test.raw)); err != nil {
				t.Fatal("exact file refused", err)
			}
			if !bytes.Contains(test.raw, []byte(test.from)) {
				t.Fatal("file has no", test.from)
			}
			changed := bytes.Replace(test.raw, []byte(test.from), []byte(test.to), 1)
			_, err := decodeStateFile(compressTestJSON(t, changed))
			if err == nil {
				t.Fatalf("accepted %s", test.to)
			}
			// Startup moves each refused file aside.
			_, preserved := errors.AsType[*preservedStateError](err)
			if preserved || stateReason(err) != reasonInvalidState {
				t.Fatalf("wrong refusal class: preserved=%t err=%v", preserved, err)
			}
		})
	}
}
