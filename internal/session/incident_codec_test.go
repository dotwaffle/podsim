package session

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentMemberFile returns the test state with the incident marker and
// each stage 1 save member: the counters and the serial, the holds and the
// operational destination of pod 01, the leg origin harbor, station index
// 0, of its rider, and the leg origin market and the excluded pod 02 of
// the waiting trip.
func incidentMemberFile(t *testing.T) stateFile {
	t.Helper()
	file := newTestStateFile(t)
	file.Project = project.Clone(file.Project)
	file.Project.IncidentContract = sim.IncidentV1Contract
	state := &file.Simulation
	state.Interrupted, state.InterruptedPassengers, state.IncidentSerial = 2, 3, 4
	pod := &state.Pods[0]
	pod.Riders = append([]sim.SavedRequest(nil), pod.Riders...)
	if pod.ID != "01" || len(pod.Riders) != 1 || pod.Riders[0].From != "harbor" || len(state.Waiting) != 1 || state.Waiting[0].Request.To != "harbor" {
		t.Fatalf("unexpected test state %+v", state)
	}
	pod.Withdrawn, pod.Purpose, pod.Owner, pod.Interrupt = 3, 1, 2, 1
	pod.Riders[0].LegFrom = "harbor"
	state.Waiting = append([]sim.SavedTrip(nil), state.Waiting...)
	state.Waiting[0].Request.LegFrom, state.Waiting[0].ExcludedPod = "market", "02"
	return file
}

// decodeIncidentSave decodes a compressed save and resolves its
// references, as a restore does before RestoreState.
func decodeIncidentSave(data []byte) (stateFile, error) {
	file, err := decodeStateFile(data)
	if err != nil {
		return stateFile{}, err
	}
	if err := file.resolveBoardings(); err != nil {
		return stateFile{}, err
	}
	return file, nil
}

// TestIncidentSaveMemberRoundTrip checks the save members of section 11.5
// of the incident contract: the encoder writes each index and tuple, and
// the decoder gives back the native state.
func TestIncidentSaveMemberRoundTrip(t *testing.T) {
	t.Parallel()
	file := incidentMemberFile(t)
	raw := decompressTestJSON(t, encodeTestState(t, file))
	for _, member := range []string{
		`"interrupted":2`, `"interruptedPassengers":3`, `"incidentSerial":4`, `"withdrawn":3`,
		`"operational":[1,2,1]`, `"legFrom":0`, `"legFrom":2`, `"excludedPod":1`,
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
		t.Fatalf("decoded\n%+v\nwant\n%+v", decoded.Simulation, file.Simulation)
	}
	// An operational tuple without interrupt set has two numbers.
	file.Simulation.Pods[0].Interrupt = 0
	if pair := decompressTestJSON(t, encodeTestState(t, file)); !bytes.Contains(pair, []byte(`"operational":[1,2]`)) {
		t.Error("the save writes an empty interrupt set")
	}
	// Pod index 0 is a present exclusion.
	file.Simulation.Waiting[0].ExcludedPod = "01"
	raw = decompressTestJSON(t, encodeTestState(t, file))
	if !bytes.Contains(raw, []byte(`"excludedPod":0`)) {
		t.Error("the save has no exclusion of pod index 0")
	}
	decoded, err = decodeIncidentSave(compressTestJSON(t, raw))
	if err != nil || decoded.Simulation.Waiting[0].ExcludedPod != "01" {
		t.Fatalf("decoded exclusion %q: %v", decoded.Simulation.Waiting[0].ExcludedPod, err)
	}
}

// TestOperationalTupleShapes checks the typed decode of an operational
// tuple by itself, also for the shapes that the prescan refuses first.
func TestOperationalTupleShapes(t *testing.T) {
	t.Parallel()
	for text, valid := range map[string]bool{
		`[1,1]`: true, `[3,2]`: true, `[1,2,5]`: true,
		`[]`: false, `[1]`: false, `[1,2,1,1]`: false, `[0,1]`: false, `[4,1]`: false,
		`[1,0]`: false, `[1,3]`: false, `[1,4]`: false, `[2,1,1]`: false, `[1,1,0]`: false, `[1,"1"]`: false,
	} {
		var tuple operationalTuple
		if err := jsonv2.Unmarshal([]byte(text), &tuple); (err == nil) != valid {
			t.Errorf("%s: error %v, want valid %v", text, err, valid)
		}
	}
}

// TestIncidentSaveRejections checks each rejection rule of section 11.6 of
// the incident contract for a save with the marker. Each failure refuses
// the whole save.
func TestIncidentSaveRejections(t *testing.T) {
	t.Parallel()
	native := map[string]func(*stateFile){
		"unknown hold bit":               func(file *stateFile) { file.Simulation.Pods[0].Withdrawn = 7 },
		"owner not held":                 func(file *stateFile) { file.Simulation.Pods[0].Withdrawn = 1 },
		"interrupt of no rider":          func(file *stateFile) { file.Simulation.Pods[0].Interrupt = 1 << 5 },
		"interrupt of a completed rider": func(file *stateFile) { file.Simulation.Pods[0].Riders[0].Completed = true },
		"leg origin is the destination":  func(file *stateFile) { file.Simulation.Waiting[0].Request.LegFrom = "harbor" },
		"leg origin at parking":          func(file *stateFile) { file.Simulation.Waiting[0].Request.LegFrom = "parking" },
		"rider leg origin at parking":    func(file *stateFile) { file.Simulation.Pods[0].Riders[0].LegFrom = "parking" },
		"exclusion of a boarded trip": func(file *stateFile) {
			file.Simulation.Waiting[0].Boarded = true
		},
		"exclusion of the pod":  func(file *stateFile) { file.Simulation.Waiting[0].Request.PodID = "02" },
		"exclusion of the hold": func(file *stateFile) { file.Simulation.Waiting[0].DeferPodID = "02" },
	}
	for name, change := range native {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			file := incidentMemberFile(t)
			change(&file)
			if _, err := decodeIncidentSave(encodeTestState(t, file)); err == nil {
				t.Fatal("the decoder accepts the save")
			}
		})
	}
	raw := string(decompressTestJSON(t, encodeTestState(t, incidentMemberFile(t))))
	edits := map[string][2]string{
		"operational of one number":       {`"operational":[1,2,1]`, `"operational":[1]`},
		"operational of four numbers":     {`"operational":[1,2,1]`, `"operational":[1,2,1,1]`},
		"operational of service":          {`"operational":[1,2,1]`, `"operational":[0,2]`},
		"unknown purpose":                 {`"operational":[1,2,1]`, `"operational":[4,2]`},
		"owner of two holds":              {`"operational":[1,2,1]`, `"operational":[1,3,1]`},
		"owner of no hold":                {`"operational":[1,2,1]`, `"operational":[1,0,1]`},
		"unknown owner":                   {`"operational":[1,2,1]`, `"operational":[1,4,1]`},
		"interrupt of a refuge":           {`"operational":[1,2,1]`, `"operational":[2,2,1]`},
		"empty interrupt set":             {`"operational":[1,2,1]`, `"operational":[1,2,0]`},
		"operational nonnumber":           {`"operational":[1,2,1]`, `"operational":[1,"2",1]`},
		"operational object":              {`"operational":[1,2,1]`, `"operational":{}`},
		"leg origin out of range":         {`"legFrom":2`, `"legFrom":4`},
		"negative leg origin":             {`"legFrom":2`, `"legFrom":-1`},
		"rider leg origin out of range":   {`"legFrom":0`, `"legFrom":300`},
		"excluded pod out of range":       {`"excludedPod":1`, `"excludedPod":2`},
		"negative excluded pod":           {`"excludedPod":1`, `"excludedPod":-1`},
		"leg origin text":                 {`"legFrom":2`, `"legFrom":"bWFya2V0"`},
		"null withdrawn":                  {`"withdrawn":3`, `"withdrawn":null`},
		"null operational":                {`"operational":[1,2,1]`, `"operational":null`},
		"null leg origin":                 {`"legFrom":2`, `"legFrom":null`},
		"null rider leg origin":           {`"legFrom":0`, `"legFrom":null`},
		"null excluded pod":               {`"excludedPod":1`, `"excludedPod":null`},
		"null interrupted":                {`"interrupted":2`, `"interrupted":null`},
		"null interrupted passengers":     {`"interruptedPassengers":3`, `"interruptedPassengers":null`},
		"null incident serial":            {`"incidentSerial":4`, `"incidentSerial":null`},
		"operational over the scan limit": {`"operational":[1,2,1]`, `"operational":[1,2,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1]`},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if strings.Count(raw, edit[0]) != 1 {
				t.Fatalf("the save has %d of %s", strings.Count(raw, edit[0]), edit[0])
			}
			edited := strings.Replace(raw, edit[0], edit[1], 1)
			if _, err := decodeIncidentSave(compressTestJSON(t, []byte(edited))); err == nil {
				t.Fatal("the decoder accepts the save")
			}
		})
	}
}

// TestIncidentSaveMembersNeedMarker checks that a save without the incident
// marker has no stage 1 member: the encoder refuses each stage 1 value,
// and the decoder refuses each member, also as 0, null, or an empty array
// (incident contract, sections 11.2 and 14.3).
func TestIncidentSaveMembersNeedMarker(t *testing.T) {
	t.Parallel()
	unmarked := func(t *testing.T) stateFile {
		t.Helper()
		file := incidentMemberFile(t)
		file.Project.IncidentContract = ""
		file.Simulation = newTestStateFile(t).Simulation
		return file
	}
	for name, change := range map[string]func(*sim.SavedState){
		"interrupted":            func(state *sim.SavedState) { state.Interrupted = 1 },
		"interrupted passengers": func(state *sim.SavedState) { state.InterruptedPassengers = 1 },
		"incident serial":        func(state *sim.SavedState) { state.IncidentSerial = 1 },
		"withdrawn":              func(state *sim.SavedState) { state.Pods[0].Withdrawn = 1 },
		"purpose":                func(state *sim.SavedState) { state.Pods[0].Purpose = 3 },
		"owner":                  func(state *sim.SavedState) { state.Pods[0].Owner = 1 },
		"interrupt":              func(state *sim.SavedState) { state.Pods[0].Interrupt = 1 },
		"rider leg origin": func(state *sim.SavedState) {
			state.Pods[0].Riders = append([]sim.SavedRequest(nil), state.Pods[0].Riders...)
			state.Pods[0].Riders[0].LegFrom = "harbor"
		},
		"leg origin": func(state *sim.SavedState) {
			state.Waiting = append([]sim.SavedTrip(nil), state.Waiting...)
			state.Waiting[0].Request.LegFrom = "market"
		},
		"excluded pod": func(state *sim.SavedState) {
			state.Waiting = append([]sim.SavedTrip(nil), state.Waiting...)
			state.Waiting[0].ExcludedPod = "02"
		},
	} {
		t.Run("encode "+name, func(t *testing.T) {
			t.Parallel()
			file := unmarked(t)
			change(&file.Simulation)
			if _, err := new(stateEncoder).encode(file); !errors.Is(err, errIncidentMemberUnmarked) {
				t.Fatalf("encode: %v", err)
			}
		})
	}
	raw := string(decompressTestJSON(t, encodeTestState(t, unmarked(t))))
	if _, err := decodeIncidentSave(compressTestJSON(t, []byte(raw))); err != nil {
		t.Fatal("control:", err)
	}
	places := map[string][2]string{
		"interrupted":            {`"simulation":{`, `"interrupted":`},
		"interrupted passengers": {`"simulation":{`, `"interruptedPassengers":`},
		"incident serial":        {`"simulation":{`, `"incidentSerial":`},
		"withdrawn":              {`"pods":[{`, `"withdrawn":`},
		"operational":            {`"pods":[{`, `"operational":`},
		"rider leg origin":       {`"riders":[{`, `"legFrom":`},
		"leg origin":             {`"request":{`, `"legFrom":`},
		"excluded pod":           {`"waiting":[{`, `"excludedPod":`},
	}
	for name, place := range places {
		for _, value := range []string{"0", "null", "[]", "1", "[3,1]"} {
			t.Run(name+" "+value, func(t *testing.T) {
				t.Parallel()
				if !strings.Contains(raw, place[0]) {
					t.Fatalf("the save has no %s", place[0])
				}
				edited := strings.Replace(raw, place[0], place[0]+place[1]+value+",", 1)
				_, err := decodeIncidentSave(compressTestJSON(t, []byte(edited)))
				if err == nil || stateReason(err) != reasonInvalidState {
					t.Fatalf("decode: %v", err)
				}
			})
		}
	}
}

// TestIncidentSaveScanRefusesMember checks the raw scan alone: without the
// marker, a stage 1 member with an explicit zero is refused before the
// typed decode loses it.
func TestIncidentSaveScanRefusesMember(t *testing.T) {
	t.Parallel()
	for path := range savedIncidentPaths {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		var document strings.Builder
		for _, part := range parts[:len(parts)-1] {
			if part == "*" {
				document.WriteString("[")
				continue
			}
			document.WriteString(`{"` + part + `":`)
		}
		document.WriteString(`{"` + parts[len(parts)-1] + `":0}`)
		for i := len(parts) - 2; i >= 0; i-- {
			if parts[i] == "*" {
				document.WriteString("]")
			} else {
				document.WriteString("}")
			}
		}
		data := []byte(document.String())
		if err := scanSavedIncidentMembers(data, false); !errors.Is(err, errIncidentMemberUnmarked) {
			t.Errorf("%s: scan of %s: %v", path, data, err)
		}
		if err := scanSavedIncidentMembers(data, true); err != nil {
			t.Errorf("%s: marked scan of %s: %v", path, data, err)
		}
	}
}

// TestOperationalArrayLimit checks the scan limit of the operational tuple
// (incident contract, section 11.6): 3 numbers pass the scan of each marker
// table, 4 fail it before the typed decode, a nested array under the tuple
// passes the scan and fails the typed decode, and a gzip body that expands
// past the save cap is too large.
func TestOperationalArrayLimit(t *testing.T) {
	t.Parallel()
	document := func(tuple string) []byte {
		return []byte(`{"simulation":{"pods":[{"operational":` + tuple + `}]}}`)
	}
	for _, markers := range []contractMarkers{
		{}, {order: sim.ExpressOrderContract}, {coupling: sim.CompactPairV1CouplingContract},
		{order: sim.ExpressOrderContract, coupling: sim.CompactPairV1CouplingContract},
	} {
		limits := savedLimits(markers)
		if err := prescanJSON(document("[1,2,1]"), limits); err != nil {
			t.Errorf("%+v: the scan refuses the limit: %v", markers, err)
		}
		if err := prescanJSON(document("[1,2,1,1]"), limits); !errors.Is(err, errJSONArrayTooLong) {
			t.Errorf("%+v: the scan of the limit plus one: %v", markers, err)
		}
	}
	raw := string(decompressTestJSON(t, encodeTestState(t, incidentMemberFile(t))))
	for name, tuple := range map[string]string{"limit plus one": "[1,2,1,1]", "nested": "[[1,2,1,1]]"} {
		edited := strings.Replace(raw, `"operational":[1,2,1]`, `"operational":`+tuple, 1)
		_, err := decodeStateFile(compressTestJSON(t, []byte(edited)))
		if err == nil || name == "limit plus one" && !errors.Is(err, errJSONArrayTooLong) {
			t.Errorf("%s: decode: %v", name, err)
		}
	}
	padded := strings.Replace(raw, `"operational":[1,2,1]`, `"operational":[1,2,1`+strings.Repeat(" ", MaxStateBytes)+`]`, 1)
	if _, err := decodeStateFile(compressTestJSON(t, []byte(padded))); stateReason(err) != reasonTooLarge {
		t.Errorf("expanded body: %v", err)
	}
}
