package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// emergencyStreamProject returns the example project with the incident,
// fault, and emergency markers.
func emergencyStreamProject() project.Config {
	config := emergencySessionProject(project.EmergencyConfig{})
	config.FaultContract, config.Faults = project.FaultV1Contract, &project.FaultConfig{}
	return config
}

// emergencyStreamFrames returns the topology and two frames of a paused
// session of the example project in which pod 01 carries an order. With
// marked, the session has the incident, fault, and emergency markers, and
// the second frame has an emergency i1.1 on pod 01, which unloads at its
// berth, and a fault i1.2 on pod 02. Without it, the session has the incident marker only, and
// the frames differ in their pause.
func emergencyStreamFrames(t *testing.T, marked bool) (TopologySnapshot, StreamFrame, StreamFrame) {
	t.Helper()
	config := markedProject()
	config.SharedRidePartyLimit = 4
	if marked {
		config = emergencyStreamProject()
	}
	client := newFaultClient(t, config)
	client.boardOrders(t, 1)
	base, err := client.session.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	if marked {
		client.mustApply(t, Command{Action: "emergency", PodID: "01"})
		client.mustApply(t, Command{Action: "fault", PodID: "02"})
	} else {
		client.mustApply(t, Command{Action: "pause", Paused: false})
	}
	next, err := client.session.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	return client.session.Topology(), base, next
}

// TestEmergencyStreamMembers checks that the client gets the emergencies of
// a frame back from a full frame, a delta with the emergencies group, and
// the HTTP state (section 11.4 of the incident emergency contract). The
// group is {} with the marker and no emergency, and the delta has it only
// when it changes.
func TestEmergencyStreamMembers(t *testing.T) {
	t.Parallel()
	topology, base, next := emergencyStreamFrames(t, true)
	if topology.EmergencyContract != sim.EmergencyV1Contract || base.State.Simulation.EmergencyContract != sim.EmergencyV1Contract {
		t.Fatalf("markers %q %q", topology.EmergencyContract, base.State.Simulation.EmergencyContract)
	}
	want := next.State.Simulation.Emergencies
	if len(want.Active) != 1 || want.Active[0] != (sim.EmergencyView{ID: "i1.1", PodID: "01", OrderID: want.Active[0].OrderID, Phase: sim.EmergencyPhaseUnloading, StartTick: next.State.Simulation.Tick}) ||
		want.Active[0].OrderID <= 0 || want.Counters != (sim.EmergencyCounters{Started: 1}) {
		t.Fatalf("emergencies %+v", want)
	}
	for _, kind := range []string{"full", "delta"} {
		raw := faultEnvelopeJSON(t, base, next, kind)
		if kind == "delta" && !bytes.Contains(raw, []byte(`"emergencies":{"active":[{"id":"i1.1","podID":"01","orderID":`)) {
			t.Fatalf("the delta has no emergencies group: %s", raw)
		}
		applied, err := applyFaultJSON(topology, base, raw)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := applied.State.Simulation.Emergencies; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s gives %+v, want %+v", kind, got, want)
		}
	}
	if raw := faultEnvelopeJSON(t, next, base, "delta"); !bytes.Contains(raw, []byte(`"emergencies":{}`)) {
		t.Fatalf("the delta to no emergency has no empty group: %s", raw)
	}
	// A frame without a change of the emergencies has no group.
	unchanged := next
	unchanged.State.Revision++
	delta, err := makeDelta(next, unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := delta.Groups["emergencies"]; found {
		t.Fatal("the delta has an unchanged emergencies group")
	}
	data, err := EncodeStateJSON(topology, next)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"emergencyContract":"emergency-v1"`)) {
		t.Fatalf("the HTTP state has no marker: %s", data)
	}
	state, err := DecodeStateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateFrame(state).Simulation; got.EmergencyContract != sim.EmergencyV1Contract || !reflect.DeepEqual(got.Emergencies, want) {
		t.Fatalf("HTTP state gives %q %+v", got.EmergencyContract, got.Emergencies)
	}
	// Without the marker, no document has a stage 3 member.
	topology, base, next = emergencyStreamFrames(t, false)
	for _, raw := range [][]byte{faultEnvelopeJSON(t, base, next, "full"), faultEnvelopeJSON(t, base, next, "delta")} {
		if bytes.Contains(raw, []byte("emergenc")) {
			t.Fatalf("an unmarked document has an emergency member: %s", raw)
		}
	}
	if data, err = EncodeStateJSON(topology, next); err != nil || bytes.Contains(data, []byte("emergenc")) {
		t.Fatalf("the unmarked HTTP state has an emergency member: %v", err)
	}
}

// TestEmergencyStreamNeedsMarker checks that each stage 3 member without
// the marker is refused, also as null, {}, and [], in a full frame, a
// delta group, and an HTTP state (sections 11.1, 11.5, and 14.3 of the
// incident emergency contract). With the marker, an empty value is
// accepted where the shape allows it, and null is refused.
func TestEmergencyStreamNeedsMarker(t *testing.T) {
	t.Parallel()
	edits := []struct {
		name, kind, insert string
		// valid is true when the edit is valid with the marker.
		valid bool
	}{
		{"emergencies null", "full", `"emergencies":null,`, false},
		{"emergencies empty object", "full", `"emergencies":{},`, true},
		{"emergencies empty array", "full", `"emergencies":[],`, false},
		{"emergencies zero", "full", `"emergencies":0,`, false},
		{"active null", "full", `"emergencies":{"active":null},`, false},
		{"active empty", "full", `"emergencies":{"active":[]},`, true},
		{"counters null", "full", `"emergencies":{"counters":null},`, false},
		{"counter null", "full", `"emergencies":{"counters":{"ended":null}},`, false},
		{"marker null", "full", `"emergencyContract":null,`, false},
		{"marker empty", "full", `"emergencyContract":"",`, false},
		{"marker unknown", "full", `"emergencyContract":"emergency-v2",`, false},
		{"group null", "delta", `"emergencies":null,`, false},
		{"group empty", "delta", `"emergencies":{},`, true},
		{"group empty array", "delta", `"emergencies":[],`, false},
		{"group counters null", "delta", `"emergencies":{"counters":null},`, false},
		{"group counters", "delta", `"emergencies":{"counters":{"started":1}},`, true},
	}
	at := map[string]string{"full": `"simulation":{`, "delta": `"groups":{`}
	for _, marked := range []bool{false, true} {
		config := markedProject()
		if marked {
			config = emergencySessionProject(project.EmergencyConfig{})
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
			// The marked frame has its marker, so a second one is a
			// duplicate member.
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
		// An emergency marker in the topology of an HTTP state, or in a
		// topology document, has the one value that the server writes.
		topologyJSON, marshalErr := jsonv2.Marshal(topology, json.DefaultOptionsV1())
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		for _, marker := range []string{`null`, `""`, `"emergency-v2"`} {
			insert := []byte(`"emergencyContract":` + marker + `,`)
			data := bytes.Replace(http, []byte(`"topology":{`), append([]byte(`"topology":{`), insert...), 1)
			if _, err := DecodeStateJSON(data); err == nil {
				t.Errorf("marked %v, HTTP topology marker %s: accepted", marked, marker)
			}
			var decoded TopologySnapshot
			if err := jsonv2.Unmarshal(append(append([]byte(`{`), insert...), topologyJSON[1:]...), &decoded, json.DefaultOptionsV1()); err == nil {
				t.Errorf("marked %v, topology marker %s: accepted", marked, marker)
			}
		}
		var decoded TopologySnapshot
		if err := jsonv2.Unmarshal(topologyJSON, &decoded, json.DefaultOptionsV1()); err != nil || decoded.EmergencyContract != topology.EmergencyContract {
			t.Errorf("marked %v, topology control: %v", marked, err)
		}
	}
	// A caller builds a delta with the emergencies group and no decoder
	// scan.
	_, base, next := emergencyStreamFrames(t, false)
	envelope := incidentEnvelope(t, base, next, "delta")
	envelope.Delta.Groups["emergencies"] = jsontext.Value(`{}`)
	if _, err := ApplyStream(base, "incident", 1, envelope); !errors.Is(err, errEmergencyStreamUnmarked) {
		t.Fatalf("direct emergencies group: %v", err)
	}
	// With the marker, ApplyStream scans a group that a caller builds for
	// null.
	_, base, next = emergencyStreamFrames(t, true)
	envelope = incidentEnvelope(t, base, next, "delta")
	envelope.Delta.Groups["emergencies"] = jsontext.Value(`{"counters":{"started":null}}`)
	if _, err := ApplyStream(base, "incident", 1, envelope); err == nil {
		t.Fatal("direct emergencies group with a null counter")
	}
	envelope.Delta.Groups["emergencies"] = jsontext.Value(`{"counters":{"started":1}}`)
	if _, err := ApplyStream(base, "incident", 1, envelope); err != nil {
		t.Fatal("direct emergencies group:", err)
	}
}

// TestEmergencyFrameTypedRules checks the rules of section 11.5 of the
// incident emergency contract on typed frames, at each place that checks a
// frame: the encoder of a full frame and of a delta, ApplyStream, the
// assembler, and the HTTP state encoder.
func TestEmergencyFrameTypedRules(t *testing.T) {
	t.Parallel()
	topology, base, next := emergencyStreamFrames(t, true)
	edits := map[string]func(*SimulationFrame){
		"unmarked emergencies": func(frame *SimulationFrame) { frame.EmergencyContract = "" },
		"unknown marker":       func(frame *SimulationFrame) { frame.EmergencyContract = "emergency-v2" },
		"serial 0":             func(frame *SimulationFrame) { frame.Emergencies.Active[0].ID = "i1.0" },
		"serial of a fault":    func(frame *SimulationFrame) { frame.Emergencies.Active[0].ID = frame.Faults.Active[0].ID },
		"unknown pod":          func(frame *SimulationFrame) { frame.Emergencies.Active[0].PodID = "nobody" },
		"order 0":              func(frame *SimulationFrame) { frame.Emergencies.Active[0].OrderID = 0 },
		"unknown phase":        func(frame *SimulationFrame) { frame.Emergencies.Active[0].Phase = "waiting" },
		"start after the tick": func(frame *SimulationFrame) { frame.Emergencies.Active[0].StartTick = frame.Tick + 1 },
		"negative counter":     func(frame *SimulationFrame) { frame.Emergencies.Counters.Ended = -1 },
		"5 records": func(frame *SimulationFrame) {
			for serial := range sim.MaxEmergencies {
				record := frame.Emergencies.Active[0]
				record.ID = fmt.Sprintf("i1.%d", serial+3)
				frame.Emergencies.Active = append(frame.Emergencies.Active, record)
			}
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
		frame.State.Simulation.Emergencies = cloneEmergencies(next.State.Simulation.Emergencies)
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
		if _, err := EncodeStateJSON(topology, frame); err == nil {
			t.Errorf("%s: the HTTP state encodes", name)
		}
	}
	// The rule bounds the records, also for records of distinct pods,
	// which the prescan of a typed frame does not bound: 4 encode, and 5
	// do not.
	for _, records := range []int{sim.MaxEmergencies, sim.MaxEmergencies + 1} {
		frame := next
		simulation := &frame.State.Simulation
		simulation.Vehicles = slices.Clone(next.State.Simulation.Vehicles)
		simulation.Emergencies = sim.EmergenciesView{}
		for i := range records {
			if i >= len(simulation.Vehicles) {
				vehicle := next.State.Simulation.Vehicles[1]
				vehicle.Pod.ID = fmt.Sprintf("v%d", i)
				simulation.Vehicles = append(simulation.Vehicles, vehicle)
			}
			record := next.State.Simulation.Emergencies.Active[0]
			record.ID, record.PodID = fmt.Sprintf("i1.%d", 10+i), simulation.Vehicles[i].Pod.ID
			simulation.Emergencies.Active = append(simulation.Emergencies.Active, record)
		}
		full := StreamEnvelope{OrderContract: simulation.OrderContract, Kind: "full", Stream: "incident", Sequence: 1, Build: frame.State.Build, Source: sourceOf(frame), Full: &frame}
		if _, err := EncodeStreamJSON(full); (err == nil) != (records <= sim.MaxEmergencies) {
			t.Errorf("%d records: %v", records, err)
		}
	}
	// FrameState checks the markers of the topology itself.
	topology.IncidentContract = ""
	state := next.State
	state.Simulation.IncidentContract = ""
	state.Simulation.FaultContract, state.Simulation.Faults = "", sim.FaultsView{}
	topology.FaultContract = ""
	if _, err := FrameState(topology, state); err == nil {
		t.Fatal("FrameState accepts the emergency marker without the incident marker")
	}
}

// TestEmergencyMarkerBinding checks that a frame whose emergency marker
// differs from the topology marker is refused, also as a change inside one
// stream, and that the topology marker needs the incident marker (section
// 11.1 of the incident emergency contract). A traffic demo keeps the
// marker of the topology.
func TestEmergencyMarkerBinding(t *testing.T) {
	t.Parallel()
	marked, _, next := emergencyStreamFrames(t, true)
	unmarked := marked
	unmarked.EmergencyContract = ""
	assembler, err := NewStreamAssembler(unmarked)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := assembler.State(next); stateErr == nil {
		t.Fatal("an unmarked topology accepts a marked frame")
	}
	if _, encodeErr := EncodeStateJSON(unmarked, next); encodeErr == nil {
		t.Fatal("the HTTP state of an unmarked topology and a marked frame encodes")
	}
	// The assembler of a stream accepts the marked frame, and then refuses
	// the same frame without the marker.
	assembler, err = NewStreamAssembler(marked)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := assembler.State(next); stateErr != nil {
		t.Fatal(stateErr)
	}
	changed := next
	changed.State.Simulation.EmergencyContract, changed.State.Simulation.Emergencies = "", sim.EmergenciesView{}
	if _, stateErr := assembler.State(changed); stateErr == nil {
		t.Fatal("the assembler accepts a change of the emergency marker")
	}
	for name, change := range map[string]func(*TopologySnapshot){
		"no incident marker": func(topology *TopologySnapshot) { topology.IncidentContract, topology.FaultContract = "", "" },
		"unknown marker":     func(topology *TopologySnapshot) { topology.EmergencyContract = "emergency-v2" },
	} {
		topology := marked
		change(&topology)
		if _, assemblerErr := NewStreamAssembler(topology); assemblerErr == nil {
			t.Errorf("%s: the assembler accepts the topology", name)
		}
	}
	// The demo fleet keeps the emergency marker, so its frames bind to the
	// topology of the project.
	client := newFaultClient(t, emergencySessionProject(project.EmergencyConfig{}))
	client.mustApply(t, Command{Action: "demo"})
	client.mustApply(t, Command{Action: "pause", Paused: true})
	topology := client.session.Topology()
	frame, err := client.session.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	if !frame.State.Simulation.Demo || frame.State.Simulation.EmergencyContract != sim.EmergencyV1Contract || topology.EmergencyContract != sim.EmergencyV1Contract {
		t.Fatalf("demo frame markers %q %q", frame.State.Simulation.EmergencyContract, topology.EmergencyContract)
	}
	data, err := EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStateJSON(data); err != nil {
		t.Fatal("demo HTTP state:", err)
	}
}

// emergencyValues returns the emergencies of a frame as generic JSON
// values.
func emergencyValues(t *testing.T, emergencies sim.EmergenciesView) map[string]any {
	t.Helper()
	var value map[string]any
	data, err := jsonv2.Marshal(emergencies)
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// TestEmergencyStreamRules checks each stream rule of section 11.5 of the
// incident emergency contract, through a full frame and through the
// emergencies group of a delta. A refused delta leaves the base frame as
// it was.
func TestEmergencyStreamRules(t *testing.T) {
	t.Parallel()
	topology, base, next := emergencyStreamFrames(t, true)
	active := func(value map[string]any) []any {
		list, _ := value["active"].([]any)
		return list
	}
	record := func(value map[string]any) map[string]any {
		emergency, _ := active(value)[0].(map[string]any)
		return emergency
	}
	counters := func(value map[string]any) map[string]any {
		counters, _ := value["counters"].(map[string]any)
		return counters
	}
	extra := func(v map[string]any, id string) map[string]any {
		copied := maps.Clone(record(v))
		copied["id"] = id
		v["active"] = append(active(v), copied)
		return copied
	}
	edits := map[string]func(map[string]any){
		"duplicate ID": func(v map[string]any) { extra(v, "i1.1")["podID"] = "02" },
		"serials not sorted": func(v map[string]any) {
			extra(v, "i1.5")["podID"] = "02"
			v["active"] = []any{active(v)[1], active(v)[0]}
		},
		"serial 0":          func(v map[string]any) { record(v)["id"] = "i1.0" },
		"serial of a fault": func(v map[string]any) { record(v)["id"] = next.State.Simulation.Faults.Active[0].ID },
		"two records for one pod": func(v map[string]any) {
			extra(v, "i1.9")
		},
		"5 records": func(v map[string]any) {
			for serial := range sim.MaxEmergencies {
				extra(v, fmt.Sprintf("i1.%d", serial+3))["podID"] = []string{"02", "01"}[serial%2]
			}
		},
		"noncanonical ID":       func(v map[string]any) { record(v)["id"] = "i1.01" },
		"ID without prefix":     func(v map[string]any) { record(v)["id"] = "1.1" },
		"ID without generation": func(v map[string]any) { record(v)["id"] = "i.1" },
		"unknown pod":           func(v map[string]any) { record(v)["podID"] = "nobody" },
		"order 0":               func(v map[string]any) { record(v)["orderID"] = 0 },
		"negative order":        func(v map[string]any) { record(v)["orderID"] = -1 },
		"fractional order":      func(v map[string]any) { record(v)["orderID"] = 1.5 },
		"unknown phase":         func(v map[string]any) { record(v)["phase"] = "waiting" },
		"negative start":        func(v map[string]any) { record(v)["startTick"] = -1 },
		"start after the frame tick": func(v map[string]any) {
			record(v)["startTick"] = next.State.Simulation.Tick + 1
		},
		"null counter":         func(v map[string]any) { counters(v)["started"] = nil },
		"negative counter":     func(v map[string]any) { counters(v)["started"] = -1 },
		"unknown member":       func(v map[string]any) { record(v)["severity"] = 1 },
		"unknown group member": func(v map[string]any) { v["extra"] = 1 },
		"string start":         func(v map[string]any) { record(v)["startTick"] = "1" },
	}
	for _, member := range emergencyViewMembers {
		edits["missing "+member] = func(v map[string]any) { delete(record(v), member) }
		edits["null "+member] = func(v map[string]any) { record(v)[member] = nil }
	}
	encoded, err := jsonv2.Marshal(next.State.Simulation.Emergencies, json.DefaultOptionsV1())
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
		value := emergencyValues(t, next.State.Simulation.Emergencies)
		edit(value)
		replacement, err := jsonv2.Marshal(value, jsonv2.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		for kind, data := range map[string][]byte{"full": full, "delta": delta} {
			before := ownStreamBoardings(base)
			before.State.Simulation.Emergencies = cloneEmergencies(base.State.Simulation.Emergencies)
			edited := bytes.Replace(data, encoded, replacement, 1)
			if _, applyErr := applyIncidentJSON(base, edited); applyErr == nil {
				t.Errorf("%s %s: ApplyStream accepts %s", kind, name, replacement)
			}
			if !reflect.DeepEqual(before, base) {
				t.Fatalf("%s %s: the refused envelope changed the base frame", kind, name)
			}
		}
		http, err := EncodeStateJSON(topology, next)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeStateJSON(bytes.Replace(http, encoded, replacement, 1)); err == nil {
			t.Errorf("HTTP %s: DecodeStateJSON accepts %s", name, replacement)
		}
	}
}

// TestEmergencyStreamArrayLimits checks the prescan limits of the active
// emergencies (section 11.5 of the incident emergency contract) on real
// envelopes: a full frame, an HTTP state, a delta group, and the group
// alone. The limit passes the scan, the limit plus one fails it before the
// typed decode, a deeper nesting passes the scan and fails the typed
// decode, and a gzip body that expands past the byte cap is refused. The
// audit of the real envelopes finds no array without an explicit limit.
func TestEmergencyStreamArrayLimits(t *testing.T) {
	t.Parallel()
	topology, base, next := emergencyStreamFrames(t, true)
	encoded, err := jsonv2.Marshal(next.State.Simulation.Emergencies, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	element, err := jsonv2.Marshal(next.State.Simulation.Emergencies.Active[0])
	if err != nil {
		t.Fatal(err)
	}
	group := func(count int) []byte {
		return []byte(`{"active":[` + strings.TrimSuffix(strings.Repeat(string(element)+",", count), ",") + `]}`)
	}
	http, err := EncodeStateJSON(topology, next)
	if err != nil {
		t.Fatal(err)
	}
	limits := streamLimits(contractMarkers{})
	documents := map[string]struct {
		data   []byte
		decode func([]byte) error
	}{
		"full":  {faultEnvelopeJSON(t, base, next, "full"), func(data []byte) error { _, err := DecodeStreamJSON(data); return err }},
		"delta": {faultEnvelopeJSON(t, base, next, "delta"), func(data []byte) error { _, err := applyIncidentJSON(base, data); return err }},
		"HTTP":  {http, func(data []byte) error { _, err := DecodeStateJSON(data); return err }},
	}
	for name, document := range documents {
		assertExplicitArrayBounds(t, name, document.data, limits)
		for count, tooLong := range map[int]bool{sim.MaxEmergencies: false, sim.MaxEmergencies + 1: true} {
			edited := bytes.Replace(document.data, encoded, group(count), 1)
			if err := document.decode(edited); errors.Is(err, errJSONArrayTooLong) != tooLong {
				t.Errorf("%s with %d records: %v", name, count, err)
			}
		}
		nested := bytes.Replace(document.data, encoded, []byte(`{"active":[[`+strings.TrimSuffix(strings.Repeat("1,", sim.MaxEmergencies+1), ",")+`]]}`), 1)
		if err := document.decode(nested); err == nil || errors.Is(err, errJSONArrayTooLong) {
			t.Errorf("%s nested: %v", name, err)
		}
	}
	assertExplicitArrayBounds(t, "group alone", group(1), emergencyGroupLimits())
	for count, tooLong := range map[int]bool{sim.MaxEmergencies: false, sim.MaxEmergencies + 1: true} {
		if _, err := decodeEmergenciesGroup(group(count)); errors.Is(err, errJSONArrayTooLong) != tooLong {
			t.Errorf("group alone with %d records: %v", count, err)
		}
	}
	// The faults group alone keeps its own bound.
	if limits.arrays["/active"] != maxFaultRecords || emergencyGroupLimits().arrays["/active"] != sim.MaxEmergencies {
		t.Fatal("the group bounds of /active differ from the record caps")
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
