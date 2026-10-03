package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const compactShapeJSON = `{"kind":"compact-buffer-v1","phase":"compact","lane":"entry","members":["01"],"start":1,"frontier":100,"stopCells":[3],"speeds":[0],"targets":[20],"landingSpeeds":[1]}`

func TestCompactQueueVersionAndShape(t *testing.T) {
	t.Parallel()
	fields := []string{"kind", "phase", "lane", "members", "start", "frontier", "stopCells", "speeds", "targets", "landingSpeeds"}
	cases := []struct {
		name, value string
		valid       bool
	}{
		{"valid", compactShapeJSON, true},
		{"queue null", "null", false},
		{"queue array", "[]", false},
		{"unknown member", strings.TrimSuffix(compactShapeJSON, "}") + `,"other":1}`, false},
		{"duplicate member", strings.TrimSuffix(compactShapeJSON, "}") + `,"kind":"compact-buffer-v1"}`, false},
		{"unknown kind", strings.Replace(compactShapeJSON, "compact-buffer-v1", "buffer", 1), false},
		{"unknown phase", strings.Replace(compactShapeJSON, `"compact"`, `"draining"`, 1), false},
		{"empty lane", strings.Replace(compactShapeJSON, `"entry"`, `""`, 1), false},
		{"long lane", strings.Replace(compactShapeJSON, `"entry"`, `"`+strings.Repeat("x", 65)+`"`, 1), false},
		{"empty members", strings.Replace(compactShapeJSON, `["01"]`, `[]`, 1), false},
		{"duplicate members", strings.Replace(compactShapeJSON, `["01"]`, `["01","01"]`, 1), false},
		{"aligned duplicate members", `{"kind":"compact-buffer-v1","phase":"compact","lane":"entry","members":["01","01"],"start":1,"frontier":100,"stopCells":[3,3],"speeds":[0,0],"targets":[20,20],"landingSpeeds":[1,1]}`, false},
		{"empty member", strings.Replace(compactShapeJSON, `["01"]`, `[""]`, 1), false},
		{"long member", strings.Replace(compactShapeJSON, `["01"]`, `["`+strings.Repeat("x", 65)+`"]`, 1), false},
		{"fractional stop", strings.Replace(compactShapeJSON, `"stopCells":[3]`, `"stopCells":[1.5]`, 1), false},
		{"negative stop", strings.Replace(compactShapeJSON, `"stopCells":[3]`, `"stopCells":[-1]`, 1), false},
		{"negative speed", strings.Replace(compactShapeJSON, `"speeds":[0]`, `"speeds":[-1]`, 1), false},
		{"fast speed", strings.Replace(compactShapeJSON, `"speeds":[0]`, `"speeds":[2.50001]`, 1), false},
		{"infinite frontier", strings.Replace(compactShapeJSON, `"frontier":100`, `"frontier":1e999`, 1), false},
	}
	var object map[string]jsontext.Value
	if err := json.Unmarshal([]byte(compactShapeJSON), &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		missing := maps.Clone(object)
		delete(missing, field)
		null := maps.Clone(object)
		null[field] = jsontext.Value("null")
		for _, mutation := range []struct {
			name string
			obj  map[string]jsontext.Value
		}{{"missing/", missing}, {"null/", null}} {
			data, err := json.Marshal(mutation.obj)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, struct {
				name, value string
				valid       bool
			}{mutation.name + field, string(data), false})
		}
	}
	for _, field := range []string{"members", "stopCells", "speeds", "targets", "landingSpeeds"} {
		for _, value := range []string{"[]", "[null]", "[1,1]", "[1,1,1,1,1]"} {
			mutation := maps.Clone(object)
			mutation[field] = jsontext.Value(value)
			data, err := json.Marshal(mutation)
			if err != nil {
				t.Fatal(err)
			}
			cases = append(cases, struct {
				name, value string
				valid       bool
			}{field + "/" + value, string(data), false})
		}
	}
	for _, version := range []int{2, 3, 4, 5, 6} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				t.Parallel()
				file := legacyTestState(newTestStateFile(t), version)
				if version == serviceStateVersion {
					file = newTestStateFile(t)
				}
				if version == bankStateVersion {
					file.Project = withBankMetadata(file.Project)
				}
				raw := decompressTestJSON(t, encodeTestState(t, file))
				raw = bytes.Replace(raw, []byte(`"pods":[{`), []byte(`"pods":[{"compactQueue":`+tc.value+`,`), 1)
				_, err := decodeStateFile(compressTestJSON(t, raw))
				if accepted := err == nil; accepted != (version == serviceStateVersion && tc.valid) {
					t.Fatalf("compact shape accepted=%t: %v", accepted, err)
				}
			})
		}
	}
}

func TestCompactFollowerVersionGate(t *testing.T) {
	t.Parallel()
	for _, version := range []int{2, 3, 4, 5, 6} {
		for _, tc := range []struct {
			name, fields string
			valid        bool
		}{
			{"valid", `"kind":"compact-buffer-v1","terminalCell":4,`, true},
			{"missing terminal", `"kind":"compact-buffer-v1",`, false},
			{"null terminal", `"kind":"compact-buffer-v1","terminalCell":null,`, false},
			{"draining", `"kind":"compact-buffer-v1","terminalCell":4,"draining":true,`, false},
			{"turned", `"kind":"compact-buffer-v1","terminalCell":4,"turn":0.1,`, false},
			{"null turn", `"kind":"compact-buffer-v1","terminalCell":4,"turn":null,`, false},
			{"escaped kind null turn", `"kind":"compact-\u0062uffer-v1","terminalCell":4,"turn":null,`, false},
			{"null draining", `"kind":"compact-buffer-v1","terminalCell":4,"draining":null,`, false},
		} {
			t.Run(fmt.Sprintf("v%d/%s", version, tc.name), func(t *testing.T) {
				t.Parallel()
				file := legacyTestState(platoonStateFile(t), version)
				if version == 5 {
					file.Project = withBankMetadata(file.Project)
				}
				link := file.Simulation.Pods[1].Platoon
				link.Lanes, link.Turn, link.Draining = 1, 0, false
				raw := decompressTestJSON(t, encodeTestState(t, file))
				if strings.Contains(tc.fields, `"turn":`) {
					raw = bytes.Replace(raw, []byte(`,"turn":0`), nil, 1)
				}
				raw = bytes.Replace(raw, []byte(`"platoon":{`), []byte(`"platoon":{`+tc.fields), 1)
				_, err := decodeStateFile(compressTestJSON(t, raw))
				if (err == nil) != (version == serviceStateVersion && tc.valid) {
					t.Fatalf("compact link accepted=%t: %v", err == nil, err)
				}
			})
		}
	}
}

func TestCompactMemberTotal(t *testing.T) {
	t.Parallel()
	for _, count := range []int{300, 301} {
		state := sim.SavedState{Pods: make([]sim.SavedPod, 76)}
		for i := range count {
			pod := &state.Pods[i/4]
			if pod.CompactQueue == nil {
				pod.CompactQueue = &sim.SavedCompactQueue{}
			}
			pod.CompactQueue.Members = append(pod.CompactQueue.Members, strconv.Itoa(i))
		}
		if (validateSavedCompactMembers(state) == nil) != (count == 300) {
			t.Fatalf("compact member total=%d", count)
		}
	}
}

func compactSessionFixture(t *testing.T) *Session {
	t.Helper()
	config := project.Default()
	config.Version, config.StationBuffers, config.PlatoonLimit, config.StationQueueSpacing = 3, true, 4, sim.StationQueueCompactV1
	config.Fleet = []sim.Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "market"}}
	for i := range config.Network.Nodes {
		config.Network.Nodes[i].Position.X *= 8
		config.Network.Nodes[i].Position.Y *= 8
	}
	for i := range config.Network.Lanes {
		config.Network.Lanes[i].SpeedLimit = 2.5
		if config.Network.Lanes[i].ID == "market-approach" {
			config.Network.Lanes[i].StationRole = sim.StationEntryRole
		}
	}
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	for _, id := range []string{"01", "02"} {
		if requestErr := shared.simulation.RequestJourney(id, "market"); requestErr != nil {
			t.Fatal(requestErr)
		}
	}
	for range 60 * sim.TicksPerSecond {
		shared.advance()
	}
	state := shared.simulation.ExportState()
	for i := range 2 {
		pod := &state.Pods[i]
		pod.RouteIndex = len(pod.Route) - 1
		pod.LaneID = config.Network.Lanes[pod.Route[pod.RouteIndex]].ID
		pod.LaneDistance, pod.Distance = float64(300-i*30), float64(300-i*30)
		for _, index := range pod.Route[:pod.RouteIndex] {
			pod.Distance += config.Network.Length(config.Network.Lanes[index])
		}
		pod.Waiting, pod.WaitSince = false, 0
	}
	staged, result, err := sim.RestoreState(sim.RestoreStateInput{Network: config.Network, Fleet: config.Fleet, State: state, StationBuffers: true})
	if err != nil || result.Tier != sim.RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("staged compact restore: %+v %v", result, err)
	}
	if err := project.ConfigurePlatoons(staged, config); err != nil {
		t.Fatal(err)
	}
	if err := project.ConfigureExperiments(staged, config); err != nil {
		t.Fatal(err)
	}
	shared.simulation = staged
	for range 200 * sim.TicksPerSecond {
		shared.advance()
		if shared.simulation.CompactQueueError() != nil {
			t.Fatal(shared.simulation.CompactQueueError())
		}
		state := shared.simulation.ExportState()
		if slices.ContainsFunc(state.Pods, func(p sim.SavedPod) bool { return p.CompactQueue != nil && len(p.CompactQueue.Members) == 2 }) {
			shared.persist = &persistence{}
			return shared
		}
	}
	t.Fatal("no compact session certificate")
	return nil
}

func TestCompactSessionRoundTrip(t *testing.T) {
	t.Parallel()
	for _, disabled := range []int{0, 1, 2} {
		t.Run(fmt.Sprint("disabled=", disabled), func(t *testing.T) {
			t.Parallel()
			shared := compactSessionFixture(t)
			if disabled > 0 {
				shared.project.StationQueueSpacing = sim.StationQueueOrdinary
				if disabled == 2 {
					shared.project.StationBuffers, shared.project.PlatoonLimit = false, 0
					if err := project.ConfigurePlatoons(shared.simulation, shared.project); err != nil {
						t.Fatal(err)
					}
				}
				if err := project.ConfigureExperiments(shared.simulation, shared.project); err != nil {
					t.Fatal(err)
				}
			}
			shared.Close()
			file, write, err := shared.captureState(SaveFinal)
			if err != nil || !write || file.Version != serviceStateVersion || !shared.simulation.NeedsCompactQueueState() {
				t.Fatalf("compact capture write=%t version=%d: %v", write, file.Version, err)
			}
			decoded, err := decodeStateFile(encodeTestState(t, file))
			if err != nil || !reflect.DeepEqual(decoded.Simulation, file.Simulation) {
				t.Fatalf("compact decode changed physical certificate: %v", err)
			}
			store := &fakeStore{data: encodeTestState(t, file)}
			restored := startFromStore(t, StoreInput{Store: store, Project: &file.Project})
			t.Cleanup(restored.Close)
			if restored.State().Restore.Tier != "physical" || !restored.simulation.NeedsCompactQueueState() {
				t.Fatalf("compact cold restore: %+v", restored.State().Restore)
			}
			if disabled > 0 {
				for i := range file.Simulation.Pods {
					if q := file.Simulation.Pods[i].CompactQueue; q != nil {
						q.Phase = "recovering"
					}
				}
			}
			if !reflect.DeepEqual(restored.simulation.ExportState(), file.Simulation) {
				t.Fatal("compact restore changed exact speeds, destinations, or certificate")
			}
		})
	}
}

func TestCompactLogicalOnlyArchive(t *testing.T) {
	t.Parallel()
	shared := compactSessionFixture(t)
	base := sessionStateFile(t, shared)
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint("invalid=", invalid), func(t *testing.T) {
			t.Parallel()
			file := decodeTestState(t, encodeTestState(t, base))
			file.RestoreAttempts = restoreLoopAttempts - 1
			if invalid {
				for i := range file.Simulation.Pods {
					if q := file.Simulation.Pods[i].CompactQueue; q != nil {
						q.Targets[0] += 1
					}
				}
			}
			store, handler := &fakeStore{data: encodeTestState(t, file)}, &recordHandler{}
			restored := startFromStore(t, StoreInput{Store: store, Project: &file.Project, Options: []Option{WithLogger(slog.New(handler))}})
			t.Cleanup(restored.Close)
			if got := restored.State(); got.Restore.Tier != "empty" || got.Restore.Reason != reasonInvalidState || got.Epoch == file.Epoch {
				t.Fatalf("compact logical-only conversion did not reject: %+v", got.Restore)
			}
			if !slices.Equal(store.callList(), []string{"read", "reject", "write"}) {
				t.Fatalf("compact state was not archived: %v", store.callList())
			}
			checkRecords(t, handler, []wantRecord{
				{slog.LevelWarn, "Rejected saved session state", map[string]any{"reason": reasonInvalidState, "error": nonEmpty}}, startupSaved,
			})
			if _, err := shared.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()}); err == nil {
				t.Fatal("logical-only compact state converted in the same epoch")
			}
		})
	}
}

func TestCompactRestoreSelectedPolicy(t *testing.T) {
	t.Parallel()
	shared := compactSessionFixture(t)
	file := sessionStateFile(t, shared)
	file.RestoreAttempts = 0
	selected := project.Clone(file.Project)
	selected.StationQueueSpacing = sim.StationQueueOrdinary
	steps := realRestoreSteps()
	steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
		if !input.CompactQueues || !input.StationBuffers || !input.BufferPlatoons || input.StationQueueSpacing != sim.StationQueueOrdinary || input.PlatoonLimit != selected.PlatoonLimit {
			t.Fatalf("compact restore context differs: %+v", input)
		}
		return sim.RestoreState(input)
	}
	loaded, err := shared.loadState(loadInput{data: encodeTestState(t, file), project: &selected, steps: steps})
	if err != nil || loaded.result.Tier != sim.RestorePhysical || loaded.config.StationQueueSpacing != sim.StationQueueOrdinary {
		t.Fatalf("selected compact policy restore: %+v %v", loaded.result, err)
	}
	for _, pod := range loaded.simulation.ExportState().Pods {
		if pod.CompactQueue != nil && pod.CompactQueue.Phase != "recovering" {
			t.Fatal("selected policy did not retain the recovering certificate")
		}
	}
}

func TestCompactMalformedPhysicalRestore(t *testing.T) {
	t.Parallel()
	shared := compactSessionFixture(t)
	base := sessionStateFile(t, shared)
	base.RestoreAttempts = 0
	for _, tc := range []struct {
		name string
		edit func(*stateFile)
	}{
		{"target proof", func(file *stateFile) { file.Simulation.Pods[0].CompactQueue.Targets[0] += 1 }},
		{"frontier proof", func(file *stateFile) { file.Simulation.Pods[0].CompactQueue.Frontier += 1 }},
		{"owned stop", func(file *stateFile) { file.Simulation.Pods[0].CompactQueue.StopCells[0]++ }},
		{"missing follower", func(file *stateFile) { file.Simulation.Pods[1].Platoon = nil }},
		{"ordinary follower", func(file *stateFile) { file.Simulation.Pods[1].Platoon.Kind = "buffer" }},
		{"pose", func(file *stateFile) { file.Simulation.Pods[1].Distance += 1 }},
		{"cross-group duplicate", func(file *stateFile) { file.Simulation.Pods[2].CompactQueue = file.Simulation.Pods[0].CompactQueue }},
	} {
		for _, logical := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/logical=%t", tc.name, logical), func(t *testing.T) {
				t.Parallel()
				file := decodeTestState(t, encodeTestState(t, base))
				if logical {
					file.RestoreAttempts = restoreLoopAttempts - 1
				}
				tc.edit(&file)
				loaded, err := shared.loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
				if err == nil || loaded.simulation != nil {
					t.Fatal("malformed compact state fell back or requeued")
				}
			})
		}
	}
}

func TestCompactArrayShapeBounds(t *testing.T) {
	t.Parallel()
	for count := 1; count <= 5; count++ {
		queue := sim.SavedCompactQueue{Kind: "compact-buffer-v1", Phase: "compact", Lane: "entry", Start: 1, Frontier: 100,
			Members: make([]string, count), StopCells: make([]int, count), Speeds: make([]float64, count),
			Targets: make([]float64, count), LandingSpeeds: make([]float64, count)}
		for i := range count {
			queue.Members[i] = strconv.Itoa(i)
		}
		raw, err := json.Marshal(queue)
		if err != nil {
			t.Fatal(err)
		}
		var decoded sim.SavedCompactQueue
		err = decodeCompactQueue(jsontext.NewDecoder(bytes.NewReader(raw)), &decoded)
		if (err == nil) != (count <= 4) {
			t.Fatalf("compact decoder member bound accepted=%d", count)
		}
		file := []byte(`{"simulation":{"pods":[{"compactQueue":` + string(raw) + `}]}}`)
		err = prescanJSON(file, compactStateLimits(serviceStateLimits()))
		if (err == nil) != (count <= 4) {
			t.Fatalf("compact scanner member bound accepted=%d", count)
		}
	}
	duplicate := sim.SavedState{Pods: []sim.SavedPod{{CompactQueue: &sim.SavedCompactQueue{Members: []string{"01", "01"}}}}}
	if validateSavedCompactMembers(duplicate) == nil {
		t.Fatal("compact duplicate members accepted")
	}
}

func TestCompactRestoreConfigurationError(t *testing.T) {
	t.Parallel()
	shared := compactSessionFixture(t)
	file := sessionStateFile(t, shared)
	file.RestoreAttempts = 0
	selected := project.Clone(file.Project)
	selected.StationQueueSpacing = "unknown"
	steps := realRestoreSteps()
	steps.validateProject = func(project.Config) error { return nil }
	steps.restoreSimulation = func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
		return shared.simulation.Clone(), sim.RestoreResult{Tier: sim.RestorePhysical}, nil
	}
	if _, err := shared.loadState(loadInput{data: encodeTestState(t, file), project: &selected, steps: steps}); err == nil {
		t.Fatal("compact restore ignored configuration failure")
	}
}
