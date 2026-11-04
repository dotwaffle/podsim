package session

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// faultMemberFile returns the test state with the incident and fault
// markers and each stage 2 save member: a pod record of pod index 1, a
// debris record on lane index 0, and each counter.
func faultMemberFile(t *testing.T) stateFile {
	t.Helper()
	file := newTestStateFile(t)
	file.Project = project.Clone(file.Project)
	file.Project.Faults = &project.FaultConfig{}
	file.Simulation.IncidentSerial = 5
	file.Simulation.Faults = &sim.SavedFaults{
		Records: []sim.SavedFault{
			{Generation: 1, Serial: 2, Start: 3, Pod: 1},
			{Generation: 1, Serial: 4, Debris: true, Start: 4, End: 9, Lane: 0, From: 1.5, To: 2.25},
		},
		Counters: sim.FaultCounters{Started: 4, Cleared: 2, Evacuations: 1, Reroutes: 3, FaultWaitTicks: 7},
	}
	return file
}

// TestFaultSaveMemberRoundTrip checks the save members of section 13.3 of
// the incident suspension contract: the encoder writes each record as one
// tuple and the counters as one object, and the decoder gives back the
// native state.
func TestFaultSaveMemberRoundTrip(t *testing.T) {
	t.Parallel()
	file := faultMemberFile(t)
	raw := decompressTestJSON(t, encodeTestState(t, file))
	for _, member := range []string{
		`"faults":{"records":[[1,2,3,0,0,1],[1,4,4,9,1,0,1.5,2.25]],"counters":{"started":4,"cleared":2,"evacuations":1,"reroutes":3,"faultWaitTicks":7}}`,
	} {
		if !bytes.Contains(raw, []byte(member)) {
			t.Errorf("the save has no %s", member)
		}
	}
	decoded, err := decodeIncidentSave(compressTestJSON(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Simulation, file.Simulation) {
		t.Fatalf("decoded\n%+v\nwant\n%+v", decoded.Simulation.Faults, file.Simulation.Faults)
	}
	// Records and counters are omitted when empty.
	file.Simulation.Faults.Records = nil
	if raw := decompressTestJSON(t, encodeTestState(t, file)); bytes.Contains(raw, []byte(`"records"`)) {
		t.Error("the save writes an empty records member")
	}
	file.Simulation.Faults = &sim.SavedFaults{Records: faultMemberFile(t).Simulation.Faults.Records}
	if raw := decompressTestJSON(t, encodeTestState(t, file)); bytes.Contains(raw, []byte(`"counters"`)) {
		t.Error("the save writes zero counters")
	}
}

// TestSavedFaultTupleShapes checks the typed decode of a fault tuple by
// itself, also for the shapes that the prescan refuses first. The restore
// checks the values.
func TestSavedFaultTupleShapes(t *testing.T) {
	t.Parallel()
	options := jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(decodeSavedFault))
	for text, valid := range map[string]bool{
		`[1,2,3,0,0,1]`: true, `[1,2,3,4,1,0,0,2]`: true, `[18446744073709551615,18446744073709551615,-1,-1,0,-1]`: true,
		`[1,2,3,4,1,-1,-1.5,1e3]`: true,
		`[]`:                      false, `[1,2,3,0,0]`: false, `[1,2,3,0,0,1,0]`: false, `[1,2,3,0,1,0,0]`: false,
		`[1,2,3,0,1,0,0,2,3]`: false, `[1,2,3,0,0,1,0,2]`: false, `[1,2,3,0,2,1]`: false, `[1,2,3,0,-1,1]`: false,
		`[1,2,3,0,0.5,1]`: false, `[1,2,3,0,0,1.5]`: false, `[1,2,3,0,0,"1"]`: false, `[1,2,3,0,0,null]`: false,
		`[1,2,3,4,2,0,0,2]`: false, `[1,2,3,4,0,0,0,2]`: false, `[1,2,3,4,1,0]`: false,
		`[-1,2,3,0,0,1]`: false, `[1,2,1e19,0,0,1]`: false, `[1,2,3,0,1,0,0,"2"]`: false, `[[1],2,3,0,0,1]`: false,
		`{}`: false, `null`: false, `1`: false,
	} {
		var fault sim.SavedFault
		if err := jsonv2.Unmarshal([]byte(text), &fault, options); (err == nil) != valid {
			t.Errorf("%s: error %v, want valid %v", text, err, valid)
		}
	}
}

// TestFaultSaveValues checks the rules on the values of the stage 2 member
// of a save (sections 13.3 and 13.5 of the incident suspension contract).
// The encoder refuses an empty member. The decoder refuses null at each
// level and an empty member, and it accepts a member with a value in each
// project.
func TestFaultSaveValues(t *testing.T) {
	t.Parallel()
	const empty = "saved faults member is empty"
	file := faultMemberFile(t)
	file.Simulation.Faults = &sim.SavedFaults{}
	if _, err := new(stateEncoder).encode(file); err == nil || err.Error() != empty {
		t.Fatalf("encode empty: %v", err)
	}
	file.Project.Faults = nil
	file.Simulation.Faults = nil
	raw := string(decompressTestJSON(t, encodeTestState(t, file)))
	if _, err := decodeIncidentSave(compressTestJSON(t, []byte(raw))); err != nil {
		t.Fatal("control:", err)
	}
	place := `"simulation":{`
	for value, want := range map[string]string{
		"null":                       "saved member /simulation/faults is null",
		"{}":                         empty,
		"[]":                         "unmarshal JSON array",
		"0":                          "unmarshal JSON number",
		`{"records":[]}`:             empty,
		`{"counters":{}}`:            empty,
		`{"counters":{"started":1}}`: "",
	} {
		t.Run("member "+value, func(t *testing.T) {
			t.Parallel()
			edited := strings.Replace(raw, place, place+`"faults":`+value+",", 1)
			_, err := decodeIncidentSave(compressTestJSON(t, []byte(edited)))
			if want == "" && err != nil || want != "" && (err == nil || stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), want)) {
				t.Fatalf("decode: %v, want %q", err, want)
			}
		})
	}
	markedRaw := string(decompressTestJSON(t, encodeTestState(t, faultMemberFile(t))))
	for name, edit := range map[string][2]string{
		"null faults":   {`"faults":{"records"`, `"faults":null,"x":{"records"`},
		"null records":  {`"records":[[`, `"records":null,"x":[[`},
		"null counters": {`"counters":{"started"`, `"counters":null,"x":{"started"`},
		"null counter":  {`"started":4`, `"started":null`},
		"null tuple":    {`[1,2,3,0,0,1]`, `null`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if strings.Count(markedRaw, edit[0]) != 1 {
				t.Fatalf("the save has %d of %s", strings.Count(markedRaw, edit[0]), edit[0])
			}
			edited := strings.Replace(markedRaw, edit[0], edit[1], 1)
			if _, err := decodeIncidentSave(compressTestJSON(t, []byte(edited))); err == nil || stateReason(err) != reasonInvalidState {
				t.Fatalf("decode: %v", err)
			}
		})
	}
	// The scan alone.
	if err := checkFaultMembers([]byte(`{"simulation":{"faults":{"records":[[1,2,3,0,0,1]],"counters":{"started":1}}}}`)); err != nil {
		t.Errorf("scan: %v", err)
	}
	if err := checkFaultMembers([]byte(`{"simulation":{"faults":{"counters":{"started":null}}}}`)); err == nil || err.Error() != "saved member /simulation/faults/counters/started is null" {
		t.Errorf("the scan of a null counter: %v", err)
	}
}

// TestFaultRecordArrayLimits checks the scan limits of the records and of
// a tuple (section 13.5 of the incident suspension contract): the limit
// passes the scan of each contract table, the limit plus one fails it before
// the typed decode, a deeper nesting passes the scan and fails the typed
// decode, and a gzip body that expands past the save cap is too large.
func TestFaultRecordArrayLimits(t *testing.T) {
	t.Parallel()
	records := func(count int, tuple string) string {
		return "[" + strings.Repeat(tuple+",", count-1) + tuple + "]"
	}
	document := func(value string) []byte {
		return []byte(`{"simulation":{"faults":{"records":` + value + `}}}`)
	}
	for _, markers := range []contractMarkers{{}, {order: sim.ExpressOrderContract}} {
		limits := savedLimits(markers)
		for name, test := range map[string]struct {
			value string
			err   error
		}{
			"records at the limit":    {records(maxFaultRecords, "[1]"), nil},
			"records past the limit":  {records(maxFaultRecords+1, "[1]"), errJSONArrayTooLong},
			"tuple at the limit":      {"[[1,2,3,4,5,6,7,8]]", nil},
			"tuple past the limit":    {"[[1,2,3,4,5,6,7,8,9]]", errJSONArrayTooLong},
			"nested under the tuple":  {"[[[1,2,3,4,5,6,7,8,9]]]", nil},
			"records under the tuple": {"[" + records(maxFaultRecords+1, "1") + "]", errJSONArrayTooLong},
		} {
			if err := prescanJSON(document(test.value), limits); !errors.Is(err, test.err) && (err != nil || test.err != nil) {
				t.Errorf("%+v %s: scan: %v", markers, name, err)
			}
		}
	}
	raw := string(decompressTestJSON(t, encodeTestState(t, faultMemberFile(t))))
	const tuple = `[1,2,3,0,0,1]`
	for name, edit := range map[string]string{
		"records past the limit": records(maxFaultRecords+1, tuple)[1 : len(records(maxFaultRecords+1, tuple))-1],
		"tuple past the limit":   `[1,2,3,0,0,1,0,0,0]`,
		"nested":                 `[[1,2,3,0,0,1,0,0,0]]`,
	} {
		edited := strings.Replace(raw, tuple, edit, 1)
		_, err := decodeStateFile(compressTestJSON(t, []byte(edited)))
		if err == nil || name != "nested" && !errors.Is(err, errJSONArrayTooLong) {
			t.Errorf("%s: decode: %v", name, err)
		}
	}
	padded := strings.Replace(raw, tuple, `[1,2,3,0,0,1`+strings.Repeat(" ", MaxStateBytes)+`]`, 1)
	if _, err := decodeStateFile(compressTestJSON(t, []byte(padded))); stateReason(err) != reasonTooLarge {
		t.Errorf("expanded body: %v", err)
	}
}

// faultSave returns an incidentSave of the example project with the
// incident and fault markers, the first pods of its fleet, and the
// evacuation delay.
func faultSave(t *testing.T, pods, evacuation int) incidentSave {
	t.Helper()
	config := incidentSaveProject(pods)
	config.Faults = &project.FaultConfig{EvacuationSeconds: new(evacuation)}
	return newIncidentSave(t, config)
}

// command applies a command at a paused command boundary and returns the
// fault ID of its reply.
func (x incidentSave) command(t *testing.T, command Command) string {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	result, err := x.s.apply(command)
	if err != nil {
		t.Fatalf("%s: %v", command.Action, err)
	}
	if _, err := x.s.apply(Command{Action: "pause", Paused: true}); err != nil {
		t.Fatal(err)
	}
	return result.faultID
}

// TestFaultSessionSaves saves at the command boundaries and tick ends of
// section 16.5 of the incident suspension contract, through the session
// adapter: after a fault command on a moving pod, at the end of the tick
// in which it reaches rest, at the end of the tick of its evacuation,
// after a fault command on a pod at a berth, after a debris command, and
// after a clear during braking. See incidentSave.check.
func TestFaultSessionSaves(t *testing.T) {
	t.Parallel()
	t.Run("moving pod", func(t *testing.T) {
		t.Parallel()
		x := faultSave(t, 2, 0)
		x.boardTwo(t)
		x.command(t, Command{Action: "fault", PodID: "01"})
		if state := x.check(t, "fault on a moving pod"); state.Faults == nil || len(state.Faults.Records) != 1 {
			t.Fatalf("faults %+v", state.Faults)
		}
		x.stepUntil(t, "rest", func(sim.SavedState) bool { return x.s.simulation.Snapshot().Vehicles[0].Pod.Speed == 0 })
		x.check(t, "end of the tick of rest")
		x.stepUntil(t, "the evacuation", func(state sim.SavedState) bool { return activeRiders(savedPod(state, "01")) == 0 })
		if state := x.check(t, "end of the tick of the evacuation"); state.Faults.Counters.Evacuations != 1 {
			t.Fatalf("counters %+v", state.Faults.Counters)
		}
		x.command(t, Command{Action: "fault", PodID: "02"})
		x.check(t, "fault on a pod at a berth")
		x.command(t, Command{Action: "fault", LaneID: "bypass-in", FromMeters: new(80.0), ToMeters: new(82.0)})
		if state := x.check(t, "debris command"); len(state.Faults.Records) != 3 {
			t.Fatalf("faults %+v", state.Faults)
		}
	})
	t.Run("clear during braking", func(t *testing.T) {
		t.Parallel()
		x := faultSave(t, 1, 300)
		x.boardTwo(t)
		id := x.command(t, Command{Action: "fault", PodID: "01"})
		x.command(t, Command{Action: "clearFault", FaultID: id})
		if state := x.check(t, "clear during braking"); state.Faults == nil || len(state.Faults.Records) != 0 || state.Faults.Counters.Cleared != 1 {
			t.Fatalf("faults %+v", state.Faults)
		}
	})
}

// faultSessionData returns a save of a session with the fault marker, a
// fault on pod 01 at a berth, and a debris record, and the decoded file.
func faultSessionData(t *testing.T) (stateFile, []byte) {
	t.Helper()
	x := faultSave(t, 2, 300)
	// The faults start after tick 0, so that an end at the start is not 0.
	x.stepUntil(t, "tick 10", func(state sim.SavedState) bool { return state.Tick >= 10 })
	x.command(t, Command{Action: "fault", PodID: "01"})
	x.command(t, Command{Action: "fault", LaneID: "bypass-in", FromMeters: new(80.0), ToMeters: new(82.0)})
	x.s.mu.Lock()
	x.s.revision++
	x.s.mu.Unlock()
	if err := x.s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	writes := x.store.writeList()
	data := writes[len(writes)-1]
	file, err := decodeStateFile(data)
	if err != nil {
		t.Fatal(err)
	}
	return file, data
}

// TestFaultSaveRejections checks that each rejection rule of section 13.5
// of the incident suspension contract makes the whole save invalid_state
// (section 16.2): no restore tier recovers a part of it.
func TestFaultSaveRejections(t *testing.T) {
	t.Parallel()
	base, data := faultSessionData(t)
	if _, err := newTestSession(t).loadState(loadInput{data: data, steps: realRestoreSteps()}); err != nil {
		t.Fatal("control:", err)
	}
	if records := base.Simulation.Faults.Records; len(records) != 2 || records[0].Debris || !records[1].Debris {
		t.Fatalf("records %+v", records)
	}
	pod := base.Simulation.Faults.Records[0].Pod
	lane := base.Simulation.Faults.Records[1].Lane
	native := map[string]struct {
		edit    func(*sim.SavedState)
		message string
	}{
		"pod index out of range":     {func(state *sim.SavedState) { state.Faults.Records[0].Pod = len(state.Pods) }, "out of range"},
		"negative pod index":         {func(state *sim.SavedState) { state.Faults.Records[0].Pod = -1 }, "out of range"},
		"lane index out of range":    {func(state *sim.SavedState) { state.Faults.Records[1].Lane = 1 << 20 }, "unknown lane"},
		"pod without the fault hold": {func(state *sim.SavedState) { state.Pods[pod].Withdrawn = 0 }, "without the fault hold"},
		"two records of one pod": {func(state *sim.SavedState) {
			state.IncidentSerial++
			state.Faults.Records = append(state.Faults.Records, sim.SavedFault{Generation: 1, Serial: state.IncidentSerial, Pod: pod})
		}, "another record"},
		"debris segment too long": {func(state *sim.SavedState) { state.Faults.Records[1].To = state.Faults.Records[1].From + 50.5 }, "debris"},
		"debris footprints meet": {func(state *sim.SavedState) {
			state.IncidentSerial++
			state.Faults.Records = append(state.Faults.Records, sim.SavedFault{Generation: 1, Serial: state.IncidentSerial, Debris: true, Lane: lane, From: 83, To: 84})
		}, "meets the footprint"},
		"more than 64 debris": {func(state *sim.SavedState) {
			for range 64 {
				state.IncidentSerial++
				state.Faults.Records = append(state.Faults.Records, sim.SavedFault{Generation: 1, Serial: state.IncidentSerial, Debris: true, Lane: lane, From: 1, To: 2})
			}
		}, "more than 64"},
		"serials out of order":   {func(state *sim.SavedState) { state.Faults.Records[1].Serial = state.Faults.Records[0].Serial }, "serial order"},
		"serial above the saved": {func(state *sim.SavedState) { state.Faults.Records[1].Serial = state.IncidentSerial + 1 }, "above the saved serial"},
		"negative start":         {func(state *sim.SavedState) { state.Faults.Records[0].Start = -1 }, "starts at tick"},
		"start after the tick":   {func(state *sim.SavedState) { state.Faults.Records[0].Start = state.Tick + 1 }, "starts at tick"},
		"negative end":           {func(state *sim.SavedState) { state.Faults.Records[0].End = -1 }, "not after its start"},
		"end at the start":       {func(state *sim.SavedState) { state.Faults.Records[0].End = state.Faults.Records[0].Start }, "not after its start"},
		"negative counter":       {func(state *sim.SavedState) { state.Faults.Counters.Reroutes = -1 }, "negative"},
		"evacuation tick too far": {func(state *sim.SavedState) {
			state.Tick, state.Faults.Records[0].Start = sim.MaxCounter, sim.MaxCounter
		}, "evacuation tick"},
	}
	for name, test := range native {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file := base
			state := &file.Simulation
			state.Pods = slices.Clone(state.Pods)
			state.Faults = &sim.SavedFaults{Records: slices.Clone(state.Faults.Records), Counters: state.Faults.Counters}
			test.edit(state)
			for _, logical := range []bool{false, true} {
				file.RestoreAttempts = 0
				if logical {
					file.RestoreAttempts = restoreLoopAttempts - 1
				}
				_, err := newTestSession(t).loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
				if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), test.message) {
					t.Fatalf("logical %t: %v", logical, err)
				}
			}
		})
	}
	raw := string(decompressTestJSON(t, data))
	for name, edit := range map[string]func(string) string{
		"pod tuple of debris length": func(tuple string) string { return strings.TrimSuffix(tuple, "]") + ",0,1]" },
		"unknown kind": func(tuple string) string {
			numbers := strings.Split(tuple, ",")
			numbers[4] = "2"
			return strings.Join(numbers, ",")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := strings.Index(raw, `"records":[[`) + len(`"records":[`)
			end := start + strings.Index(raw[start:], "]") + 1
			edited := raw[:start] + edit(raw[start:end]) + raw[end:]
			if edited == raw {
				t.Fatal("no edit")
			}
			_, err := newTestSession(t).loadState(loadInput{data: compressTestJSON(t, []byte(edited)), steps: realRestoreSteps()})
			if stateReason(err) != reasonInvalidState {
				t.Fatalf("restore: %v", err)
			}
		})
	}
}

// TestFaultSaveLogicalRestore restores a save with fault records in the
// logical tier, as after a restore loop. Every record ends, the fault hold
// is released, and the counters stay (section 12.7 of the incident
// suspension contract).
func TestFaultSaveLogicalRestore(t *testing.T) {
	t.Parallel()
	file, _ := faultSessionData(t)
	file.RestoreAttempts = restoreLoopAttempts - 1
	loaded, err := newTestSession(t).loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
	if err != nil {
		t.Fatal(err)
	}
	state := loaded.simulation.ExportState()
	if loaded.result.Tier != sim.RestoreLogical || loaded.result.DroppedFaults != 2 || state.Faults == nil ||
		len(state.Faults.Records) != 0 || state.Faults.Counters != file.Simulation.Faults.Counters {
		t.Fatalf("result %+v, faults %+v", loaded.result, state.Faults)
	}
	for _, pod := range state.Pods {
		if pod.Withdrawn != 0 {
			t.Fatalf("pod %s keeps the hold %d", pod.ID, pod.Withdrawn)
		}
	}
}

// TestFaultSaveOfDemoFleet checks the restore of a save of a demo fleet. The
// demo fleet has the fault operations off and zero counters, so a save with
// a hold, a record, or a counter is invalid_state before either restore
// tier, also in a restore that has only the logical tier.
func TestFaultSaveOfDemoFleet(t *testing.T) {
	t.Parallel()
	file, _ := faultSessionData(t)
	file.Simulation.Demo = &sim.SavedDemo{}
	file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
	check := func(want string) {
		t.Helper()
		// The last attempt before the restore loop limit restores only in
		// the logical tier, which drops the demo and the records.
		for _, attempts := range []int{0, restoreLoopAttempts - 1} {
			file.RestoreAttempts = attempts
			_, err := newTestSession(t).loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
			if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), want) {
				t.Fatalf("restore attempts %d: %v, want %q", attempts, err, want)
			}
		}
	}
	check("saved demo pod 01 has a service hold or an operational purpose")
	for index := range file.Simulation.Pods {
		pod := &file.Simulation.Pods[index]
		pod.Withdrawn, pod.Purpose, pod.Owner, pod.Interrupt = 0, 0, 0, 0
	}
	check("saved demo fleet has fault or emergency state")
	file.Simulation.Faults = &sim.SavedFaults{Counters: file.Simulation.Faults.Counters}
	check("saved demo fleet has fault or emergency state")
}
