package parkride

import (
	"bytes"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCheckpointPreallocationBounds(t *testing.T) {
	t.Parallel()
	run, err := NewRun(continuationInput())
	if err != nil {
		t.Fatal(err)
	}
	original := encodeCheckpoint(t, run)
	for _, tc := range []struct {
		name, path, reason string
		count              int
		element            any
	}{
		{"banks", "payload/origin/project/network/stations/0/banks", "array exceeds bound", 9, map[string]any{}},
		{"station classes", "payload/origin/project/network/stations/0/vehicleClasses", "array exceeds bound", 5, "legacy"},
		{"lane classes", "payload/origin/project/network/lanes/0/vehicleClasses", "array exceeds bound", 5, "legacy"},
		{"berth classes", "payload/origin/project/network/stations/0/berths/0/vehicleClasses", "array exceeds bound", 5, "legacy"},
		{"rail events", "payload/origin/project/railArrivals", "array exceeds bound", 257, map[string]any{}},
		{"rail destinations", "payload/origin/project/railArrivals/0/destinations", "array exceeds bound", 17, map[string]any{}},
		{"rail origins", "payload/origin/project/railDepartures/0/origins", "array exceeds bound", 17, map[string]any{}},
		{"profile bands", "payload/origin/project/demandProfiles/0/bands", "array exceeds bound", 25, map[string]any{}},
		{"profile flows", "payload/origin/project/demandProfiles/0/flows", "array exceeds bound", project.MaxFlows + 1, map[string]any{}},
		{"profile stations", "payload/origin/project/demandProfiles/0/stations", "array exceeds bound", project.MaxStations + 1, "harbor"},
		{"flow values", "payload/origin/project/demandProfiles/0/flows/0", "array exceeds bound", 27, 0},
		{"native stops", "payload/native/pods/0/stops", "array exceeds bound", 9, "harbor"},
		{"native stop object", "payload/native/pods/0/stops", "element type", 1, map[string]any{}},
		{"native route object", "payload/native/pods/0/route", "element type", 1, map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var tree map[string]any
			if err := json.Unmarshal(original, &tree); err != nil {
				t.Fatal(err)
			}
			values := make([]any, tc.count)
			for i := range values {
				values[i] = tc.element
			}
			replaceTreePath(t, tree, strings.Split(tc.path, "/"), values)
			raw, err := json.Marshal(tree)
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := DecodeCheckpoint(t.Context(), bytes.NewReader(raw), ResumeInput{Implementation: testImplementation()})
			if candidate != nil || err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("preallocation guard: %v", err)
			}
		})
	}
}

// This helper adds a first entry when the authored fixture has no such array.
func replaceTreePath(t *testing.T, tree map[string]any, path []string, value any) {
	t.Helper()
	if len(path) == 1 {
		tree[path[0]] = value
		return
	}
	if path[1] == "0" {
		entries, _ := tree[path[0]].([]any)
		if len(entries) == 0 {
			entries = []any{map[string]any{}}
			tree[path[0]] = entries
		}
		if len(path) == 2 {
			entries[0] = value
			return
		}
		entry, ok := entries[0].(map[string]any)
		if !ok {
			t.Fatal("fixture path does not contain an object")
		}
		replaceTreePath(t, entry, path[2:], value)
		return
	}
	child, _ := tree[path[0]].(map[string]any)
	if child == nil {
		child = make(map[string]any)
		tree[path[0]] = child
	}
	replaceTreePath(t, child, path[1:], value)
}

func TestCheckpointCombinedPreallocationBounds(t *testing.T) {
	t.Parallel()
	run, err := NewRun(continuationInput())
	if err != nil {
		t.Fatal(err)
	}
	data := encodeCheckpoint(t, run)
	for _, tc := range []struct {
		name, reason string
		mutate       func(*checkpointFile)
	}{
		{"retained request total", "retained requests exceed", func(f *checkpointFile) {
			request := sim.SavedRequest{SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService, ID: 1, From: "harbor", To: "market", PartySize: 1}
			f.Payload.Native.Waiting = []sim.SavedTrip{{Request: request}, {Request: request}}
			f.Payload.Native.Pods[0].Riders = []sim.SavedRequest{request}
		}},
		{"completed display counts", "retained requests exceed", func(f *checkpointFile) {
			request := sim.SavedRequest{SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService, ID: 1, From: "harbor", To: "market", PartySize: 1, Completed: true}
			f.Payload.Native.Pods[0].Riders = []sim.SavedRequest{request, request, request}
		}},
		{"actual pod route bound", "route exceeds origin", func(f *checkpointFile) {
			f.Payload.Native.Pods[0].Route = make([]int, len(f.Payload.Origin.Project.Network.Nodes)+len(f.Payload.Origin.Project.Network.Lanes)+1)
		}},
		{"actual pending route bound", "route exceeds origin", func(f *checkpointFile) {
			f.Payload.Native.Waiting = []sim.SavedTrip{{Request: sim.SavedRequest{SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService, ID: 1, From: "harbor", To: "market", PartySize: 1}, Route: make([]int, len(f.Payload.Origin.Project.Network.Nodes)+1)}}
		}},
		{"combined rail events", "combined event bound", func(f *checkpointFile) {
			f.Payload.Origin.Project.RailArrivals = make([]project.RailArrival, 128)
			f.Payload.Origin.Project.RailDepartures = make([]project.RailDeparture, 129)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var file checkpointFile
			if err := json.Unmarshal(data, &file); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&file)
			candidate, err := DecodeCheckpoint(t.Context(), bytes.NewReader(rehashCheckpoint(t, file)), ResumeInput{Implementation: testImplementation()})
			if candidate != nil || err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("combined preallocation guard: %v", err)
			}
		})
	}
	for _, replacement := range []string{`"3"`, `null`, `false`, `{}`, `[]`} {
		raw := bytes.Replace(data, []byte(`"project":{"version":1`), []byte(`"project":{"version":`+replacement), 1)
		candidate, err := DecodeCheckpoint(t.Context(), bytes.NewReader(raw), ResumeInput{Implementation: testImplementation()})
		if candidate != nil || err == nil {
			t.Fatal("invalid project version accepted", replacement)
		}
	}
	unknown := bytes.Replace(data, []byte(`"native":{`), []byte(`"native":{"demo":{"futureFlag":true},`), 1)
	if _, err := DecodeCheckpoint(t.Context(), bytes.NewReader(unknown), ResumeInput{Implementation: testImplementation()}); err == nil || !strings.Contains(err.Error(), "unknown checkpoint member") {
		t.Fatal("future demo member accepted", err)
	}
	// The frozen scan rules no longer list the removed legacy order members.
	for _, insert := range []string{
		`"legacyCohort":true,`,
		`"riders":[{"legacyPartySize":true}],`,
	} {
		legacy := bytes.Replace(data, []byte(`"pods":[{`), []byte(`"pods":[{`+insert), 1)
		if bytes.Equal(legacy, data) {
			t.Fatal("fixture has no pods")
		}
		if _, err := DecodeCheckpoint(t.Context(), bytes.NewReader(legacy), ResumeInput{Implementation: testImplementation()}); err == nil || !strings.Contains(err.Error(), "unknown checkpoint member") {
			t.Fatal("removed legacy member accepted", insert, err)
		}
	}
}

func TestCheckpointCanonicalComponentWhitespace(t *testing.T) {
	t.Parallel()
	run, err := NewRun(continuationInput())
	if err != nil {
		t.Fatal(err)
	}
	data := encodeCheckpoint(t, run)
	for _, component := range []string{"project", "plan"} {
		t.Run(component, func(t *testing.T) {
			t.Parallel()
			marker := []byte(`"` + component + `":{`)
			replacement := append(append([]byte(nil), marker...), bytes.Repeat([]byte(" "), project.MaxFileBytes)...)
			transport := bytes.Replace(data, marker, replacement, 1)
			restored, err := DecodeCheckpoint(t.Context(), bytes.NewReader(transport), ResumeInput{Implementation: testImplementation()})
			if err != nil {
				t.Fatal("valid canonical component rejected because of transport whitespace", err)
			}
			assertJointEqual(t, run, restored)
			if !bytes.Equal(data, encodeCheckpoint(t, restored)) {
				t.Fatal("transport whitespace changed canonical identity")
			}
		})
	}
}

// TestCheckpointOriginComponentBounds gives the byte budget a canonical
// origin project and plan just over their input bounds: 32 MiB for the
// project and 10 MiB for the plan. A plan of 10 MiB and one byte is less
// than the project bound, so it shows that the plan has its own bound.
func TestCheckpointOriginComponentBounds(t *testing.T) {
	t.Parallel()
	overProject := checkpointPayload{Origin: origin{Project: project.Config{Name: strings.Repeat("n", 32<<20)}}}
	if err := checkByteBudget(t.Context(), overProject); err == nil || err.Error() != "canonical origin project exceeds 32 MiB" {
		t.Fatalf("project over its bound: %v", err)
	}
	overPlan := checkpointPayload{Origin: origin{Plan: Plan{Lots: []Lot{{ID: strings.Repeat("l", 10<<20)}}}}}
	if err := checkByteBudget(t.Context(), overPlan); err == nil || err.Error() != "canonical origin plan exceeds 10 MiB" {
		t.Fatalf("plan over its bound: %v", err)
	}
}
