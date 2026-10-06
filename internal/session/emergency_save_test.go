package session

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// emergencyMemberFile returns the test state with the incident and
// emergency markers and each stage 3 save member: a record of pod index 1
// and each counter. The codec tests do not restore it.
func emergencyMemberFile(t *testing.T) stateFile {
	t.Helper()
	file := newTestStateFile(t)
	file.Project = project.Clone(file.Project)
	file.Project.IncidentContract = sim.IncidentV1Contract
	file.Project.EmergencyContract = project.EmergencyV1Contract
	file.Project.Emergencies = &project.EmergencyConfig{}
	file.Simulation.IncidentSerial = 5
	file.Simulation.Emergencies = &sim.SavedEmergencies{
		Records:  []sim.SavedEmergency{{Generation: 1, Serial: 2, Start: 3, Pod: 1, Order: 7}},
		Counters: sim.EmergencyCounters{Started: 4, Ended: 3, EmergencyTicks: 9},
	}
	return file
}

// TestEmergencySaveMemberRoundTrip checks the save members of section 11.3
// of the incident emergency contract: the encoder writes each record as one
// tuple and the counters as one object, and the decoder gives back the
// native state. Records and counters are omitted when empty.
func TestEmergencySaveMemberRoundTrip(t *testing.T) {
	t.Parallel()
	file := emergencyMemberFile(t)
	raw := decompressTestJSON(t, encodeTestState(t, file))
	const member = `"emergencies":{"records":[[1,2,3,1,7]],"counters":{"started":4,"ended":3,"emergencyTicks":9}}`
	if !bytes.Contains(raw, []byte(member)) {
		t.Fatalf("the save has no %s", member)
	}
	decoded, err := decodeIncidentSave(compressTestJSON(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Simulation, file.Simulation) {
		t.Fatalf("decoded\n%+v\nwant\n%+v", decoded.Simulation.Emergencies, file.Simulation.Emergencies)
	}
	file.Simulation.Emergencies = &sim.SavedEmergencies{Counters: sim.EmergencyCounters{Ended: 1}}
	if raw := decompressTestJSON(t, encodeTestState(t, file)); !bytes.Contains(raw, []byte(`"emergencies":{"counters":{"ended":1}}`)) {
		t.Errorf("the save writes an empty records member or a zero counter: %s", raw)
	}
	file.Simulation.Emergencies = &sim.SavedEmergencies{Records: emergencyMemberFile(t).Simulation.Emergencies.Records}
	if raw := decompressTestJSON(t, encodeTestState(t, file)); !bytes.Contains(raw, []byte(`"emergencies":{"records":[[1,2,3,1,7]]}`)) {
		t.Errorf("the save writes zero counters: %s", raw)
	}
	// With the marker and no member, the save has no stage 3 member.
	file.Simulation.Emergencies = nil
	if raw := decompressTestJSON(t, encodeTestState(t, file)); bytes.Contains(raw, []byte(`"emergencies":{"`)) || !bytes.Contains(raw, []byte(`"emergencyContract":"emergency-v1"`)) {
		t.Errorf("the save without records has an emergencies member: %s", raw)
	}
}

// TestSavedEmergencyTupleShapes checks the typed decode of an emergency
// tuple by itself, also for the shapes that the prescan refuses first. The
// restore checks the values.
func TestSavedEmergencyTupleShapes(t *testing.T) {
	t.Parallel()
	options := jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(decodeSavedEmergency))
	for text, valid := range map[string]bool{
		`[1,2,3,4,5]`: true, `[18446744073709551615,18446744073709551615,9223372036854775807,299,9223372036854775807]`: true,
		`[0,0,-1,-1,-1]`: true,
		`[]`:             false, `[1,2,3,4]`: false, `[1,2,3,4,5,6]`: false, `[1,2,3,4,"5"]`: false, `[1,2,3,4,null]`: false,
		`[-1,2,3,4,5]`: false, `[1,-2,3,4,5]`: false, `[1,2,1e19,4,5]`: false, `[1,2,3,4.5,5]`: false, `[1,2,3,4,5.5]`: false,
		`[18446744073709551616,2,3,4,5]`: false, `[[1],2,3,4,5]`: false, `[true,2,3,4,5]`: false,
		`{}`: false, `null`: false, `1`: false,
	} {
		var record sim.SavedEmergency
		if err := jsonv2.Unmarshal([]byte(text), &record, options); (err == nil) != valid {
			t.Errorf("%s: error %v, want valid %v", text, err, valid)
		}
	}
}

// TestEmergencySaveMembersNeedMarker checks that a save without the
// emergency marker has no stage 3 member: the encoder refuses the records
// and the counters, and the decoder refuses the member, also as null, an
// empty object, or an empty array (sections 11.3 and 11.5 of the incident
// emergency contract). With the marker, the decoder refuses null.
func TestEmergencySaveMembersNeedMarker(t *testing.T) {
	t.Parallel()
	file := emergencyMemberFile(t)
	file.Project.EmergencyContract, file.Project.Emergencies = "", nil
	if _, err := new(stateEncoder).encode(file); !errors.Is(err, errEmergencyMemberUnmarked) {
		t.Fatalf("encode: %v", err)
	}
	file.Simulation.Emergencies = &sim.SavedEmergencies{Counters: sim.EmergencyCounters{EmergencyTicks: 1}}
	if _, err := new(stateEncoder).encode(file); !errors.Is(err, errEmergencyMemberUnmarked) {
		t.Fatalf("encode counters: %v", err)
	}
	marked := emergencyMemberFile(t)
	marked.Simulation.Emergencies = &sim.SavedEmergencies{}
	if _, err := new(stateEncoder).encode(marked); err == nil {
		t.Fatal("the encoder writes an empty emergencies member")
	}
	file.Simulation.Emergencies = nil
	raw := string(decompressTestJSON(t, encodeTestState(t, file)))
	if _, err := decodeIncidentSave(compressTestJSON(t, []byte(raw))); err != nil {
		t.Fatal("control:", err)
	}
	place := `"simulation":{`
	for _, value := range []string{"null", "{}", "[]", "0", `{"records":[]}`, `{"counters":{}}`, `{"counters":{"started":1}}`, `{"records":[[1,1,0,0,1]]}`} {
		t.Run("unmarked "+value, func(t *testing.T) {
			t.Parallel()
			edited := strings.Replace(raw, place, place+`"emergencies":`+value+",", 1)
			// The typed decode refuses a value that is not an object
			// first.
			_, err := decodeIncidentSave(compressTestJSON(t, []byte(edited)))
			if stateReason(err) != reasonInvalidState || (value == "null" || value[0] == '{') && !errors.Is(err, errEmergencyMemberUnmarked) {
				t.Fatalf("decode: %v", err)
			}
		})
	}
	markedRaw := string(decompressTestJSON(t, encodeTestState(t, emergencyMemberFile(t))))
	if _, err := decodeIncidentSave(compressTestJSON(t, []byte(markedRaw))); err != nil {
		t.Fatal("marked control:", err)
	}
	for name, edit := range map[string][2]string{
		"null emergencies": {`"emergencies":{"records"`, `"emergencies":null,"x":{"records"`},
		"null records":     {`"records":[[1,2,3,1,7]]`, `"records":null`},
		"null counters":    {`"counters":{"started":4`, `"counters":null,"x":{"started":4`},
		"null counter":     {`"started":4`, `"started":null`},
		"null tuple":       {`[1,2,3,1,7]`, `null`},
		"null element":     {`[1,2,3,1,7]`, `[1,2,3,1,null]`},
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
	// The scan alone: a null counter without the marker, and each member
	// with the marker.
	for _, document := range []string{`{"simulation":{"emergencies":{"counters":{"ended":null}}}}`, `{"simulation":{"emergencies":{"records":[[1,2,3,4,5]]}}}`} {
		if err := scanSavedEmergencyMembers([]byte(document), false); !errors.Is(err, errEmergencyMemberUnmarked) {
			t.Errorf("unmarked scan of %s: %v", document, err)
		}
	}
	if err := scanSavedEmergencyMembers([]byte(`{"simulation":{"emergencies":{"records":[[1,2,3,4,5]],"counters":{"started":1}}}}`), true); err != nil {
		t.Errorf("marked scan: %v", err)
	}
	if err := scanSavedEmergencyMembers([]byte(`{"simulation":{"emergencies":{"counters":{"ended":null}}}}`), true); err == nil {
		t.Error("the marked scan accepts a null counter")
	}
}

// TestEmergencyRecordArrayLimits checks the scan limits of the records and
// of a tuple (section 11.5 of the incident emergency contract): the limit
// passes the scan of each marker table, the limit plus one fails it before
// the typed decode, a deeper nesting passes the scan and fails the typed
// decode, and a gzip body that expands past the save cap is too large.
func TestEmergencyRecordArrayLimits(t *testing.T) {
	t.Parallel()
	records := func(count int, tuple string) string {
		return "[" + strings.Repeat(tuple+",", count-1) + tuple + "]"
	}
	document := func(value string) []byte {
		return []byte(`{"simulation":{"emergencies":{"records":` + value + `}}}`)
	}
	for _, markers := range []contractMarkers{
		{}, {order: sim.ExpressOrderContract}, {coupling: sim.CompactPairV1CouplingContract},
		{order: sim.ExpressOrderContract, coupling: sim.CompactPairV1CouplingContract},
	} {
		limits := savedLimits(markers)
		for name, test := range map[string]struct {
			value string
			err   error
		}{
			"records at the limit":    {records(sim.MaxEmergencies, "[1]"), nil},
			"records past the limit":  {records(sim.MaxEmergencies+1, "[1]"), errJSONArrayTooLong},
			"tuple at the limit":      {"[[1,2,3,4,5]]", nil},
			"tuple past the limit":    {"[[1,2,3,4,5,6]]", errJSONArrayTooLong},
			"nested under the tuple":  {"[[[1,2,3,4,5,6]]]", nil},
			"records under the tuple": {"[" + records(sim.MaxEmergencies+1, "1") + "]", nil},
		} {
			if err := prescanJSON(document(test.value), limits); !errors.Is(err, test.err) && (err != nil || test.err != nil) {
				t.Errorf("%+v %s: scan: %v", markers, name, err)
			}
		}
	}
	raw := string(decompressTestJSON(t, encodeTestState(t, emergencyMemberFile(t))))
	const tuple = `[1,2,3,1,7]`
	for name, edit := range map[string]string{
		"records at the limit":   strings.Repeat(tuple+",", sim.MaxEmergencies-1) + tuple,
		"records past the limit": strings.Repeat(tuple+",", sim.MaxEmergencies) + tuple,
		"tuple past the limit":   `[1,2,3,1,7,0]`,
		"nested":                 `[[1,2,3,1,7,0]]`,
	} {
		edited := strings.Replace(raw, tuple, edit, 1)
		_, err := decodeStateFile(compressTestJSON(t, []byte(edited)))
		switch name {
		case "records at the limit":
			if err != nil {
				t.Errorf("%s: decode: %v", name, err)
			}
		case "nested":
			if err == nil || errors.Is(err, errJSONArrayTooLong) {
				t.Errorf("%s: decode: %v", name, err)
			}
		default:
			if !errors.Is(err, errJSONArrayTooLong) {
				t.Errorf("%s: decode: %v", name, err)
			}
		}
	}
	padded := strings.Replace(raw, tuple, `[1,2,3,1,7`+strings.Repeat(" ", MaxStateBytes)+`]`, 1)
	if _, err := decodeStateFile(compressTestJSON(t, []byte(padded))); stateReason(err) != reasonTooLarge {
		t.Errorf("expanded body: %v", err)
	}
}

// emergencySave returns an incidentSave of the example project with the
// incident and emergency markers and the first pods of its fleet. With
// faults, the project also has the fault marker and an evacuation delay
// of one hour, so that no faulted pod evacuates in a test.
func emergencySave(t *testing.T, pods int, faults bool) incidentSave {
	t.Helper()
	config := incidentSaveProject(pods)
	config.EmergencyContract = project.EmergencyV1Contract
	config.Emergencies = &project.EmergencyConfig{}
	if faults {
		config.FaultContract = project.FaultV1Contract
		config.Faults = &project.FaultConfig{EvacuationSeconds: new(3600)}
	}
	return newIncidentSave(t, config)
}

// emergency applies the emergency command for pod 01 at a paused command
// boundary and returns the emergency ID of its reply.
func (x incidentSave) emergency(t *testing.T) string {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	result, err := x.s.apply(Command{Action: "emergency", PodID: "01"})
	if err != nil {
		t.Fatal("emergency:", err)
	}
	if _, err := x.s.apply(Command{Action: "pause", Paused: true}); err != nil {
		t.Fatal(err)
	}
	return result.emergencyID
}

// savedEmergencyPhase returns the phase of the only emergency record of
// state, derived from its saved pod as section 4.2 of the incident
// emergency contract derives it, or "" when state has no record.
func savedEmergencyPhase(t *testing.T, state sim.SavedState) string {
	t.Helper()
	if state.Emergencies == nil || len(state.Emergencies.Records) == 0 {
		return ""
	}
	if len(state.Emergencies.Records) != 1 {
		t.Fatalf("records %+v", state.Emergencies.Records)
	}
	pod := state.Pods[state.Emergencies.Records[0].Pod]
	switch {
	case pod.Purpose == 0:
		return sim.EmergencyPhaseDeferred
	case pod.Activity == "unloading":
		return sim.EmergencyPhaseUnloading
	default:
		return sim.EmergencyPhaseBound
	}
}

// checkFrameEmergencies checks that the frame of a session shows the
// emergency records and counters of its saved state, with the phase that
// savedEmergencyPhase derives. Without the emergency marker, the frame
// has no stage 3 member.
func checkFrameEmergencies(t *testing.T, name string, saved sim.SavedState, frame SimulationFrame) {
	t.Helper()
	if frame.EmergencyContract == "" {
		if !reflect.DeepEqual(frame.Emergencies, sim.EmergenciesView{}) {
			t.Fatalf("%s: the unmarked frame has emergencies %+v", name, frame.Emergencies)
		}
		return
	}
	var want sim.EmergenciesView
	if saved.Emergencies != nil {
		want.Counters = saved.Emergencies.Counters
		for _, record := range saved.Emergencies.Records {
			want.Active = append(want.Active, sim.EmergencyView{
				ID: fmt.Sprintf("i%d.%d", record.Generation, record.Serial), PodID: saved.Pods[record.Pod].ID,
				OrderID: record.Order, Phase: savedEmergencyPhase(t, saved), StartTick: record.Start,
			})
		}
	}
	if !reflect.DeepEqual(frame.Emergencies, want) {
		t.Fatalf("%s: the frame shows %+v, the save has %+v", name, frame.Emergencies, want)
	}
}

// TestEmergencySessionSaves saves at the command boundaries and tick ends
// of section 14.5 of the incident emergency contract, through the session
// adapter: after each start, at each phase change, and at each end. Each
// save restores in the physical tier and passes CheckContract. See
// incidentSave.check. The faulted pod is the deferred member of section
// 14.2.
func TestEmergencySessionSaves(t *testing.T) {
	t.Parallel()
	phase := func(t *testing.T, state sim.SavedState, want string) {
		t.Helper()
		if got := savedEmergencyPhase(t, state); got != want {
			t.Fatalf("phase %q, want %q", got, want)
		}
	}
	t.Run("traveling pod", func(t *testing.T) {
		t.Parallel()
		x := emergencySave(t, 1, false)
		x.boardTwo(t)
		if id := x.emergency(t); id != "i1.1" {
			t.Fatalf("emergency ID %q", id)
		}
		phase(t, x.check(t, "start of a traveling pod"), sim.EmergencyPhaseBound)
		x.stepUntil(t, "the unload", func(state sim.SavedState) bool {
			return savedEmergencyPhase(t, state) == sim.EmergencyPhaseUnloading
		})
		phase(t, x.check(t, "end of the tick of the unload start"), sim.EmergencyPhaseUnloading)
		x.stepUntil(t, "the end", func(state sim.SavedState) bool { return savedEmergencyPhase(t, state) == "" })
		state := x.check(t, "end of the tick of the end")
		if state.Emergencies == nil || state.Emergencies.Counters.Started != 1 || state.Emergencies.Counters.Ended != 1 || savedPod(state, "01").Withdrawn != 0 {
			t.Fatalf("emergencies %+v, pod %+v", state.Emergencies, savedPod(state, "01"))
		}
	})
	t.Run("paused start at a berth", func(t *testing.T) {
		t.Parallel()
		x := emergencySave(t, 1, false)
		x.submit(t, sim.TripOptions{From: "harbor", To: "market"})
		x.stepUntil(t, "a rider aboard at the berth", func(state sim.SavedState) bool {
			pod := savedPod(state, "01")
			return pod.BerthID != "" && pod.Activity != "idle" && activeRiders(pod) == 1
		})
		x.emergency(t)
		phase(t, x.check(t, "paused start at a berth"), sim.EmergencyPhaseUnloading)
		x.stepUntil(t, "the end", func(state sim.SavedState) bool { return savedEmergencyPhase(t, state) == "" })
		x.check(t, "end of the tick of the end")
	})
	t.Run("faulted pod", func(t *testing.T) {
		t.Parallel()
		x := emergencySave(t, 1, true)
		x.boardTwo(t)
		fault := x.command(t, Command{Action: "fault", PodID: "01"})
		x.emergency(t)
		phase(t, x.check(t, "start of a faulted pod"), sim.EmergencyPhaseDeferred)
		x.stepUntil(t, "rest", func(sim.SavedState) bool { return x.s.simulation.Snapshot().Vehicles[0].Pod.Speed == 0 })
		phase(t, x.check(t, "end of the tick of rest"), sim.EmergencyPhaseDeferred)
		x.command(t, Command{Action: "clearFault", FaultID: fault})
		phase(t, x.check(t, "fault clear"), sim.EmergencyPhaseDeferred)
		x.stepUntil(t, "the end of the deferral", func(state sim.SavedState) bool {
			return savedEmergencyPhase(t, state) != sim.EmergencyPhaseDeferred
		})
		x.check(t, "end of the tick of the advance")
	})
}

// emergencySessionData returns a save of a session with the fault and
// emergency markers, a bound emergency on pod 01 with two riders, and a
// fault on the empty pod 02, and the decoded file. The emergency has
// serial 1, and the fault serial 2.
func emergencySessionData(t *testing.T) (stateFile, []byte) {
	t.Helper()
	x := emergencySave(t, 2, true)
	x.boardTwo(t)
	x.emergency(t)
	x.command(t, Command{Action: "fault", PodID: "02"})
	x.s.mu.Lock()
	x.s.revision++
	x.s.mu.Unlock()
	if err := x.s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	writes := x.store.writeList()
	data := writes[len(writes)-1]
	file, err := decodeIncidentSave(data)
	if err != nil {
		t.Fatal(err)
	}
	if phase := savedEmergencyPhase(t, file.Simulation); phase != sim.EmergencyPhaseBound {
		t.Fatalf("phase %q", phase)
	}
	return file, data
}

// TestEmergencySaveRejections checks that each rejection rule of section
// 11.5 of the incident emergency contract makes the whole save
// invalid_state before either restore tier (section 14.2), also in a
// restore that has only the logical tier: no tier recovers a part of it.
func TestEmergencySaveRejections(t *testing.T) {
	t.Parallel()
	base, data := emergencySessionData(t)
	if _, err := newTestSession(t).loadState(loadInput{data: data, steps: realRestoreSteps()}); err != nil {
		t.Fatal("control:", err)
	}
	record := base.Simulation.Emergencies.Records[0]
	pod := record.Pod
	other := slices.IndexFunc(base.Simulation.Pods[pod].Riders, func(rider sim.SavedRequest) bool { return rider.ID != record.Order && !rider.Completed })
	if other < 0 || base.Simulation.Faults == nil || len(base.Simulation.Faults.Records) != 1 {
		t.Fatalf("pod %+v, faults %+v", base.Simulation.Pods[pod], base.Simulation.Faults)
	}
	appendRecord := func(state *sim.SavedState, record sim.SavedEmergency) {
		state.IncidentSerial++
		record.Serial = state.IncidentSerial
		state.Emergencies.Records = append(state.Emergencies.Records, record)
	}
	native := map[string]struct {
		edit    func(*sim.SavedState)
		message string
	}{
		"serial 0":               {func(state *sim.SavedState) { state.Emergencies.Records[0].Serial = 0 }, "serial outside"},
		"serial above the saved": {func(state *sim.SavedState) { state.Emergencies.Records[0].Serial = state.IncidentSerial + 1 }, "serial outside"},
		"serial of a fault": {func(state *sim.SavedState) {
			state.Emergencies.Records[0].Serial = state.Faults.Records[0].Serial
		}, "serial of a fault record"},
		"serials out of order": {func(state *sim.SavedState) {
			appendRecord(state, sim.SavedEmergency{Generation: 1, Pod: 1 - pod, Order: 1})
			state.Emergencies.Records[0], state.Emergencies.Records[1] = state.Emergencies.Records[1], state.Emergencies.Records[0]
		}, "serial order"},
		"two records for one pod": {func(state *sim.SavedState) { appendRecord(state, record) }, "another record"},
		"pod index out of range":  {func(state *sim.SavedState) { state.Emergencies.Records[0].Pod = len(state.Pods) }, "out of range"},
		"negative pod index":      {func(state *sim.SavedState) { state.Emergencies.Records[0].Pod = -1 }, "out of range"},
		"negative start":          {func(state *sim.SavedState) { state.Emergencies.Records[0].Start = -1 }, "starts at tick"},
		"start after the tick":    {func(state *sim.SavedState) { state.Emergencies.Records[0].Start = state.Tick + 1 }, "starts at tick"},
		"order 0":                 {func(state *sim.SavedState) { state.Emergencies.Records[0].Order = 0 }, "not positive"},
		"negative counter":        {func(state *sim.SavedState) { state.Emergencies.Counters.EmergencyTicks = -1 }, "negative"},
		"more than 4 records": {func(state *sim.SavedState) {
			for range sim.MaxEmergencies {
				appendRecord(state, sim.SavedEmergency{Generation: 1, Pod: 1 - pod, Order: 1})
			}
		}, "more than 4"},
		"hold without a record (E2)": {func(state *sim.SavedState) {
			state.Emergencies = &sim.SavedEmergencies{Counters: state.Emergencies.Counters}
		}, "E2"},
		"emergency unload with another owner (E4)": {func(state *sim.SavedState) { state.Pods[pod].Owner = 1; state.Pods[pod].Withdrawn = 3 }, "E4"},
		"wrong party (E3)": {func(state *sim.SavedState) {
			state.Emergencies.Records[0].Order = state.Pods[pod].Riders[other].ID
		}, "E3"},
		"interrupt of two riders (E3)": {func(state *sim.SavedState) { state.Pods[pod].Interrupt = 3 }, "E3"},
		"recovery with an active rider (E8)": {func(state *sim.SavedState) {
			state.Pods[pod].Purpose, state.Pods[pod].Interrupt = 3, 0
		}, "E8"},
	}
	for name, test := range native {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file := base
			state := &file.Simulation
			state.Pods = slices.Clone(state.Pods)
			for index := range state.Pods {
				state.Pods[index].Riders = slices.Clone(state.Pods[index].Riders)
			}
			state.Emergencies = &sim.SavedEmergencies{Records: slices.Clone(state.Emergencies.Records), Counters: state.Emergencies.Counters}
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
	// A save with a record and no marker, and tuples of a wrong length.
	raw := string(decompressTestJSON(t, data))
	start := strings.Index(raw, `"emergencies":{"records":[[`) + len(`"emergencies":{"records":[`)
	end := start + strings.Index(raw[start:], "]") + 1
	tuple := raw[start:end]
	for name, edited := range map[string]string{
		"record without the marker": strings.Replace(strings.Replace(raw, `"emergencyContract":"emergency-v1",`, "", 1), `"emergencies":{},`, "", 1),
		"tuple of 4 numbers":        raw[:start] + tuple[:strings.LastIndex(tuple, ",")] + "]" + raw[end:],
		"tuple of 6 numbers":        raw[:start] + strings.TrimSuffix(tuple, "]") + ",1]" + raw[end:],
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if edited == raw {
				t.Fatal("no edit")
			}
			_, err := newTestSession(t).loadState(loadInput{data: compressTestJSON(t, []byte(edited)), steps: realRestoreSteps()})
			if stateReason(err) != reasonInvalidState || name == "record without the marker" && !errors.Is(err, errEmergencyMemberUnmarked) {
				t.Fatalf("restore: %v", err)
			}
		})
	}
}

// TestEmergencySaveLogicalRestore restores a save with an emergency record
// in the logical tier, as after a restore loop (section 10.7 of the
// incident emergency contract). Every record ends, no pod keeps the
// emergency hold, the counters stay, and the restore log reports the
// number of records that ended.
func TestEmergencySaveLogicalRestore(t *testing.T) {
	t.Parallel()
	file, _ := emergencySessionData(t)
	file.RestoreAttempts = restoreLoopAttempts - 1
	handler := &recordHandler{}
	restored := startFromStore(t, StoreInput{Store: &fakeStore{data: encodeTestState(t, file)}, Options: []Option{WithLogger(slog.New(handler))}})
	t.Cleanup(restored.Close)
	restored.mu.Lock()
	state := restored.simulation.ExportState()
	restored.mu.Unlock()
	if restored.restore.Tier != "logical" || state.Emergencies == nil || len(state.Emergencies.Records) != 0 ||
		state.Emergencies.Counters != file.Simulation.Emergencies.Counters {
		t.Fatalf("restore %+v, emergencies %+v", restored.restore, state.Emergencies)
	}
	for _, pod := range state.Pods {
		if pod.Withdrawn&2 != 0 || pod.Owner == 2 {
			t.Fatalf("pod %s keeps the emergency hold: %+v", pod.ID, pod)
		}
	}
	// The restore loop backs up the saved state before the logical tier.
	checkRecords(t, handler, []wantRecord{
		{slog.LevelInfo, "Backed up saved session state", nil}, startupSaved,
		{slog.LevelInfo, "Restored session", map[string]any{"tier": "logical", "droppedEmergencies": int64(1), "droppedFaults": int64(1)}},
	})
}
