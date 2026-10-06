package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// faultStreamFrames returns the topology and two frames of a paused
// session of the example project. With marked, the session has the fault
// marker, and the second frame has a pod fault on 01 with an end and a
// debris record without one. Without it, the session has the incident
// marker only, and the frames differ in their revision.
func faultStreamFrames(t *testing.T, marked bool) (TopologySnapshot, StreamFrame, StreamFrame) {
	t.Helper()
	config := markedProject()
	if marked {
		config = faultSessionProject(project.FaultConfig{})
	}
	client := newFaultClient(t, config)
	base, err := client.session.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	if marked {
		client.mustApply(t, Command{Action: "fault", PodID: "01", DurationSeconds: new(int64(600))})
		client.mustApply(t, debrisCommand())
	} else {
		client.mustApply(t, Command{Action: "pause", Paused: false})
	}
	next, err := client.session.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	return client.session.Topology(), base, next
}

// faultEnvelopeJSON encodes the full envelope of frame, or the delta from
// base to frame.
func faultEnvelopeJSON(t *testing.T, base, frame StreamFrame, kind string) []byte {
	t.Helper()
	raw, err := EncodeStreamJSON(incidentEnvelope(t, base, frame, kind))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// applyFaultJSON decodes raw, applies it to base, and assembles the
// result with a new assembler of topology that has accepted base.
func applyFaultJSON(topology TopologySnapshot, base StreamFrame, raw []byte) (StreamFrame, error) {
	assembler, err := NewStreamAssembler(topology)
	if err != nil {
		return StreamFrame{}, err
	}
	if _, baseErr := assembler.State(base); baseErr != nil {
		return StreamFrame{}, fmt.Errorf("base: %w", baseErr)
	}
	applied, err := applyIncidentJSON(base, raw)
	if err != nil {
		return StreamFrame{}, err
	}
	_, err = assembler.State(applied)
	return applied, err
}

// TestFaultStreamMembers checks that the client gets the faults of a
// frame back from a full frame, a delta with the faults group, and the
// HTTP state (section 13.4 of the incident suspension contract). A record
// without endTick is accepted.
func TestFaultStreamMembers(t *testing.T) {
	t.Parallel()
	topology, base, next := faultStreamFrames(t, true)
	if topology.FaultContract != sim.FaultV1Contract || next.State.Simulation.FaultContract != sim.FaultV1Contract {
		t.Fatalf("markers %q %q", topology.FaultContract, next.State.Simulation.FaultContract)
	}
	want := next.State.Simulation.Faults
	if len(want.Active) != 2 || want.Active[0].EndTick == 0 || want.Active[1].EndTick != 0 || want.Counters.Started != 2 {
		t.Fatalf("faults %+v", want)
	}
	for _, kind := range []string{"full", "delta"} {
		raw := faultEnvelopeJSON(t, base, next, kind)
		if kind == "delta" && !bytes.Contains(raw, []byte(`"faults":{"active":[`)) {
			t.Fatalf("the delta has no faults group: %s", raw)
		}
		applied, err := applyFaultJSON(topology, base, raw)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := applied.State.Simulation.Faults; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s gives %+v, want %+v", kind, got, want)
		}
	}
	// The group is {} when no fault is active and each counter is 0.
	if raw := faultEnvelopeJSON(t, next, base, "delta"); !bytes.Contains(raw, []byte(`"faults":{}`)) {
		t.Fatalf("the delta to no fault has no empty group: %s", raw)
	}
	data, err := EncodeStateJSON(topology, next)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DecodeStateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateFrame(state).Simulation; got.FaultContract != sim.FaultV1Contract || !reflect.DeepEqual(got.Faults, want) {
		t.Fatalf("HTTP state gives %q %+v", got.FaultContract, got.Faults)
	}
}

// TestFaultStreamNeedsMarker checks that each stage 2 member without the
// marker is refused, also as null, {}, and [], in a full frame, a delta
// group, and an HTTP state (sections 13.1 and 16.3 of the incident
// suspension contract). With the marker, an empty value is accepted where
// the shape allows it, and null is refused.
func TestFaultStreamNeedsMarker(t *testing.T) {
	t.Parallel()
	edits := []struct {
		name, kind, insert string
		// valid is true when the edit is valid with the marker.
		valid bool
	}{
		{"faults null", "full", `"faults":null,`, false},
		{"faults empty object", "full", `"faults":{},`, true},
		{"faults empty array", "full", `"faults":[],`, false},
		{"faults zero", "full", `"faults":0,`, false},
		{"active null", "full", `"faults":{"active":null},`, false},
		{"active empty", "full", `"faults":{"active":[]},`, true},
		{"counter null", "full", `"faults":{"counters":{"started":null}},`, false},
		{"marker null", "full", `"faultContract":null,`, false},
		{"marker empty", "full", `"faultContract":"",`, false},
		{"group null", "delta", `"faults":null,`, false},
		{"group empty", "delta", `"faults":{},`, true},
		{"group empty array", "delta", `"faults":[],`, false},
		{"group counters null", "delta", `"faults":{"counters":null},`, false},
	}
	at := map[string]string{"full": `"simulation":{`, "delta": `"groups":{`}
	for _, marked := range []bool{false, true} {
		config := markedProject()
		if marked {
			config = faultSessionProject(project.FaultConfig{})
		}
		s, err := NewWithProject(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		topology, frames := incidentFrames(t, s)
		raw := map[string][]byte{"full": faultEnvelopeJSON(t, frames[0], frames[1], "full"), "delta": faultEnvelopeJSON(t, frames[0], frames[1], "delta")}
		http, err := EncodeStateJSON(topology, frames[1])
		if err != nil {
			t.Fatal(err)
		}
		for _, edit := range edits {
			valid := marked && edit.valid
			data := bytes.Replace(raw[edit.kind], []byte(at[edit.kind]), []byte(at[edit.kind]+edit.insert), 1)
			if _, err := applyIncidentJSON(frames[0], data); (err == nil) != valid {
				t.Errorf("marked %v, %s: error %v, want valid %v", marked, edit.name, err, valid)
			}
			if edit.kind != "full" {
				continue
			}
			data = bytes.Replace(http, []byte(at["full"]), []byte(at["full"]+edit.insert), 1)
			if _, err := DecodeStateJSON(data); (err == nil) != valid {
				t.Errorf("marked %v, HTTP %s: error %v, want valid %v", marked, edit.name, err, valid)
			}
		}
	}
	// A caller builds a delta with the faults group and no decoder scan.
	_, base, next := faultStreamFrames(t, false)
	envelope := incidentEnvelope(t, base, next, "delta")
	envelope.Delta.Groups["faults"] = json.RawMessage(`{}`)
	if _, err := ApplyStream(base, "incident", 1, envelope); !errors.Is(err, errFaultStreamUnmarked) {
		t.Fatalf("direct faults group: %v", err)
	}
	// With the marker, ApplyStream scans a group that a caller builds for
	// null.
	_, base, next = faultStreamFrames(t, true)
	envelope = incidentEnvelope(t, base, next, "delta")
	envelope.Delta.Groups["faults"] = json.RawMessage(`{"counters":{"started":null}}`)
	if _, err := ApplyStream(base, "incident", 1, envelope); err == nil {
		t.Fatal("direct faults group with a null counter")
	}
	envelope.Delta.Groups["faults"] = json.RawMessage(`{"counters":{"started":1}}`)
	if _, err := ApplyStream(base, "incident", 1, envelope); err != nil {
		t.Fatal("direct faults group:", err)
	}
}

// TestFaultFrameTypedRules checks the rules of section 13.5 of the
// incident suspension contract on typed frames, at each place that checks
// a frame: the encoder of a full frame and of a delta, ApplyStream, the
// assembler, and the HTTP state encoder. A wait report names a pod or an
// active fault.
func TestFaultFrameTypedRules(t *testing.T) {
	t.Parallel()
	topology, base, next := faultStreamFrames(t, true)
	edits := map[string]func(*SimulationFrame){
		"unmarked faults": func(frame *SimulationFrame) { frame.FaultContract = "" },
		"end at the start": func(frame *SimulationFrame) {
			frame.Tick = 5
			fault := &frame.Faults.Active[0]
			fault.StartTick, fault.EndTick, fault.EvacuateTick = 5, 5, new(int64(5))
		},
		"pod record with a lane": func(frame *SimulationFrame) { frame.Faults.Active[0].LaneID = "bypass-in" },
		"debris without a lane":  func(frame *SimulationFrame) { frame.Faults.Active[1].LaneID = "" },
		"debris with a pod":      func(frame *SimulationFrame) { frame.Faults.Active[1].PodID = "01" },
		"debris with a negative start": func(frame *SimulationFrame) {
			frame.Faults.Active[1].FromMeters, frame.Faults.Active[1].ToMeters = new(-1.0), new(1.0)
		},
	}
	assemble := func(frame StreamFrame) error {
		assembler, err := NewStreamAssembler(topology)
		if err != nil {
			return err
		}
		_, err = assembler.State(frame)
		return err
	}
	for name, edit := range edits {
		frame := next
		frame.State.Simulation.Faults = cloneFaults(next.State.Simulation.Faults)
		edit(&frame.State.Simulation)
		full := StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "incident", Sequence: 1, Build: frame.State.Build, Source: sourceOf(frame), Full: &frame}
		if _, err := EncodeStreamJSON(full); err == nil {
			t.Errorf("%s: the full frame encodes", name)
		}
		if _, err := makeDelta(base, frame); err == nil {
			t.Errorf("%s: the delta encodes", name)
		}
		if _, err := ApplyStream(StreamFrame{}, "incident", 0, full); err == nil {
			t.Errorf("%s: ApplyStream accepts the frame", name)
		}
		if err := assemble(frame); err == nil {
			t.Errorf("%s: the assembler accepts the frame", name)
		}
	}
	for name, blocked := range map[string]string{"cleared fault": "i9.9", "unknown pod": "nobody"} {
		frame := ownStreamBoardings(next)
		frame.State.Simulation.Vehicles[1].Pod.BlockedBy = blocked
		if err := assemble(frame); err == nil {
			t.Errorf("%s: the assembler accepts the wait report", name)
		}
		if _, err := EncodeStateJSON(topology, frame); err == nil {
			t.Errorf("%s: the HTTP state encodes", name)
		}
	}
	frame := ownStreamBoardings(next)
	frame.State.Simulation.Vehicles[1].Pod.BlockedBy = next.State.Simulation.Faults.Active[1].ID
	if err := assemble(frame); err != nil {
		t.Fatal("a wait report of an active fault:", err)
	}
	// FrameState checks the markers of the topology itself.
	topology.IncidentContract = ""
	state := next.State
	state.Simulation.IncidentContract = ""
	if _, err := FrameState(topology, state); err == nil {
		t.Fatal("FrameState accepts the fault marker without the incident marker")
	}
}

// TestFaultMarkerBinding checks that a frame whose fault marker differs
// from the topology marker is refused, and that the topology marker needs
// the incident marker (section 13.1 of the incident suspension contract).
func TestFaultMarkerBinding(t *testing.T) {
	t.Parallel()
	marked, _, next := faultStreamFrames(t, true)
	unmarked := marked
	unmarked.FaultContract = ""
	assembler, err := NewStreamAssembler(unmarked)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := assembler.State(next); stateErr == nil {
		t.Fatal("an unmarked topology accepts a marked frame")
	}
	_, _, plain := faultStreamFrames(t, false)
	plainTopology := marked
	assembler, err = NewStreamAssembler(plainTopology)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembler.State(plain); err == nil {
		t.Fatal("a marked topology accepts an unmarked frame")
	}
	for name, change := range map[string]func(*TopologySnapshot){
		"no incident marker": func(topology *TopologySnapshot) { topology.IncidentContract = "" },
		"unknown marker":     func(topology *TopologySnapshot) { topology.FaultContract = "fault-v2" },
	} {
		topology := marked
		change(&topology)
		if _, err := NewStreamAssembler(topology); err == nil {
			t.Errorf("%s: the assembler accepts the topology", name)
		}
	}
}

// faultRecords returns the faults of frame as generic JSON values.
func faultRecords(t *testing.T, faults sim.FaultsView) map[string]any {
	t.Helper()
	var value map[string]any
	data, err := jsonv2.Marshal(faults)
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// TestFaultStreamRules checks each stream rule of section 13.5 of the
// incident suspension contract, through a full frame and through the
// faults group of a delta. A refused delta leaves the base frame as it
// was.
func TestFaultStreamRules(t *testing.T) {
	t.Parallel()
	topology, base, next := faultStreamFrames(t, true)
	lane, _ := topologyLane(topology, "bypass-in")
	length := topology.Network.Length(lane)
	active := func(value map[string]any) []any {
		list, _ := value["active"].([]any)
		return list
	}
	record := func(value map[string]any, index int) map[string]any {
		fault, _ := active(value)[index].(map[string]any)
		return fault
	}
	counters := func(value map[string]any) map[string]any {
		counters, _ := value["counters"].(map[string]any)
		return counters
	}
	edits := map[string]func(map[string]any){
		"duplicate ID": func(v map[string]any) { record(v, 1)["id"] = record(v, 0)["id"] },
		"two records for one pod": func(v map[string]any) {
			extra := maps.Clone(record(v, 0))
			extra["id"] = "i1.9"
			v["active"] = append(active(v), extra)
		},
		"65 debris records": func(v map[string]any) {
			for serial := range 64 {
				extra := maps.Clone(record(v, 1))
				extra["id"] = fmt.Sprintf("i1.%d", serial+3)
				v["active"] = append(active(v), extra)
			}
		},
		"forbidden member":            func(v map[string]any) { record(v, 0)["laneID"] = "bypass-in" },
		"forbidden debris member":     func(v map[string]any) { record(v, 1)["phase"] = "stopped" },
		"missing required member":     func(v map[string]any) { delete(record(v, 0), "phase") },
		"missing start":               func(v map[string]any) { delete(record(v, 1), "startTick") },
		"missing id":                  func(v map[string]any) { delete(record(v, 1), "id") },
		"null required member":        func(v map[string]any) { record(v, 1)["toMeters"] = nil },
		"null end":                    func(v map[string]any) { record(v, 1)["endTick"] = nil },
		"null counter":                func(v map[string]any) { counters(v)["started"] = nil },
		"negative counter":            func(v map[string]any) { counters(v)["started"] = -1 },
		"negative tick":               func(v map[string]any) { record(v, 1)["startTick"] = -1 },
		"start after the frame tick":  func(v map[string]any) { record(v, 1)["startTick"] = next.State.Simulation.Tick + 1 },
		"evacuation before the start": func(v map[string]any) { record(v, 0)["evacuateTick"] = -1 },
		"end at the start":            func(v map[string]any) { record(v, 0)["endTick"] = record(v, 0)["startTick"] },
		"end zero":                    func(v map[string]any) { record(v, 1)["endTick"] = 0 },
		"past the lane": func(v map[string]any) {
			record(v, 1)["fromMeters"], record(v, 1)["toMeters"] = length-1, length+0.5
		},
		"segment too long":    func(v map[string]any) { record(v, 1)["fromMeters"], record(v, 1)["toMeters"] = 0, 50.5 },
		"negative start":      func(v map[string]any) { record(v, 1)["fromMeters"], record(v, 1)["toMeters"] = -1, 1 },
		"empty pod member":    func(v map[string]any) { record(v, 1)["podID"] = "" },
		"empty debris member": func(v map[string]any) { record(v, 0)["laneID"] = "" },
		"empty segment":       func(v map[string]any) { record(v, 1)["toMeters"] = record(v, 1)["fromMeters"] },
		"unknown lane":        func(v map[string]any) { record(v, 1)["laneID"] = "nowhere" },
		"unknown pod":         func(v map[string]any) { record(v, 0)["podID"] = "nobody" },
		"unknown kind":        func(v map[string]any) { record(v, 1)["kind"] = "flood" },
		"unknown phase":       func(v map[string]any) { record(v, 0)["phase"] = "towed" },
		"unknown member":      func(v map[string]any) { record(v, 0)["severity"] = 1 },
		"noncanonical ID":     func(v map[string]any) { record(v, 0)["id"] = "i1.01" },
		"ID without prefix":   func(v map[string]any) { record(v, 0)["id"] = "1.1" },
		"serials not sorted":  func(v map[string]any) { v["active"] = []any{record(v, 1), record(v, 0)} },
		"unknown group member": func(v map[string]any) {
			v["extra"] = 1
		},
	}
	encoded, err := jsonv2.Marshal(next.State.Simulation.Faults, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	full, delta := faultEnvelopeJSON(t, base, next, "full"), faultEnvelopeJSON(t, base, next, "delta")
	for _, data := range [][]byte{full, delta} {
		if bytes.Count(data, encoded) != 1 {
			t.Fatalf("the envelope has %d of %s", bytes.Count(data, encoded), encoded)
		}
		if _, err := applyFaultJSON(topology, base, data); err != nil {
			t.Fatal("control:", err)
		}
	}
	for name, edit := range edits {
		value := faultRecords(t, next.State.Simulation.Faults)
		edit(value)
		replacement, err := jsonv2.Marshal(value, jsonv2.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		for kind, data := range map[string][]byte{"full": full, "delta": delta} {
			before := ownStreamBoardings(base)
			before.State.Simulation.Faults = cloneFaults(base.State.Simulation.Faults)
			edited := bytes.Replace(data, encoded, replacement, 1)
			if _, err := applyFaultJSON(topology, base, edited); err == nil {
				t.Errorf("%s %s: the client accepts %s", kind, name, replacement)
			}
			// Only the assembler knows the lanes of the topology.
			if _, err := applyIncidentJSON(base, edited); err == nil && name != "past the lane" && name != "unknown lane" {
				t.Errorf("%s %s: ApplyStream accepts %s", kind, name, replacement)
			}
			if !reflect.DeepEqual(before, base) {
				t.Fatalf("%s %s: the refused envelope changed the base frame", kind, name)
			}
		}
	}
}

// topologyLane returns the lane id of topology.
func topologyLane(topology TopologySnapshot, id string) (sim.Lane, bool) {
	for _, lane := range topology.Network.Lanes {
		if lane.ID == id {
			return lane, true
		}
	}
	return sim.Lane{}, false
}

// TestFaultStreamArrayLimits checks the prescan limits of the active
// faults (section 13.5 of the incident suspension contract) on real
// envelopes: a full frame, an HTTP state, a delta group, and the group
// alone. The limit passes the scan, the limit plus one fails it before the
// typed decode, a deeper nesting passes the scan and fails the typed
// decode, and a gzip body that expands past the byte cap is refused.
func TestFaultStreamArrayLimits(t *testing.T) {
	t.Parallel()
	topology, base, next := faultStreamFrames(t, true)
	encoded, err := jsonv2.Marshal(next.State.Simulation.Faults, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	pod, err := jsonv2.Marshal(next.State.Simulation.Faults.Active[0])
	if err != nil {
		t.Fatal(err)
	}
	group := func(count int, element []byte) []byte {
		return []byte(`{"active":[` + strings.TrimSuffix(strings.Repeat(string(element)+",", count), ",") + `]}`)
	}
	http, err := EncodeStateJSON(topology, next)
	if err != nil {
		t.Fatal(err)
	}
	documents := map[string]struct {
		data   []byte
		decode func([]byte) error
	}{
		"full":  {faultEnvelopeJSON(t, base, next, "full"), func(data []byte) error { _, err := DecodeStreamJSON(data); return err }},
		"delta": {faultEnvelopeJSON(t, base, next, "delta"), func(data []byte) error { _, err := applyIncidentJSON(base, data); return err }},
		"HTTP":  {http, func(data []byte) error { _, err := DecodeStateJSON(data); return err }},
	}
	for name, document := range documents {
		for count, tooLong := range map[int]bool{maxFaultRecords: false, maxFaultRecords + 1: true} {
			edited := bytes.Replace(document.data, encoded, group(count, pod), 1)
			if err := document.decode(edited); errors.Is(err, errJSONArrayTooLong) != tooLong {
				t.Errorf("%s with %d records: %v", name, count, err)
			}
		}
		nested := bytes.Replace(document.data, encoded, []byte(`{"active":[[`+strings.TrimSuffix(strings.Repeat("1,", maxFaultRecords+1), ",")+`]]}`), 1)
		if err := document.decode(nested); err == nil || errors.Is(err, errJSONArrayTooLong) {
			t.Errorf("%s nested: %v", name, err)
		}
	}
	for count, tooLong := range map[int]bool{maxFaultRecords: false, maxFaultRecords + 1: true} {
		if _, err := decodeFaultsGroup(group(count, pod)); errors.Is(err, errJSONArrayTooLong) != tooLong {
			t.Errorf("group alone with %d records: %v", count, err)
		}
	}
	padded := bytes.Replace(documents["full"].data, encoded, []byte(`{"active":[`+strings.Repeat(" ", MaxStreamJSON)+`]}`), 1)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(padded); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := InflateStream(compressed.Bytes()); err == nil {
		t.Error("the client inflates a body past the byte cap")
	}
}
