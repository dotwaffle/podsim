package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// removedMarkers are the removed members, each with its only old value.
var removedMarkers = map[string]string{
	"incidentContract":  "incident-v1",
	"faultContract":     "fault-v1",
	"emergencyContract": "emergency-v1",
}

// incidentFrames returns the topology of s and two frames with a pause
// change between them.
func incidentFrames(t *testing.T, s *Session) (TopologySnapshot, [2]StreamFrame) {
	t.Helper()
	first, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(Command{Client: "incident", Sequence: 1, Epoch: first.State.Epoch, Action: "pause", Paused: !first.State.Simulation.Paused})
	second, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	return s.Topology(), [2]StreamFrame{first, second}
}

// sessionFrame returns the presentation frame of s.
func sessionFrame(t *testing.T, s *Session) StreamFrame {
	t.Helper()
	frame, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

// TestOperationsOffSessionChanges checks operationsOff through each change
// of the fleet. A change of the value comes with a new generation, so a
// stream sends a full frame. The commands agree with the value: with the
// value false a fault passes, and with the value true each command gets its
// refusal.
func TestOperationsOffSessionChanges(t *testing.T) {
	t.Parallel()
	config := project.Default()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	t.Cleanup(s.Close)
	client := newTestClient(s, "operations")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	previous := sessionFrame(t, s)
	check := func(stage string, want bool) {
		t.Helper()
		frame := sessionFrame(t, s)
		simulation := frame.State.Simulation
		if simulation.OperationsOff != want || s.State().Simulation.OperationsOff != want {
			t.Fatalf("%s: operationsOff %t, want %t", stage, simulation.OperationsOff, want)
		}
		if previous.State.Simulation.OperationsOff != want && (frame.State.Generation == previous.State.Generation || sameChain(previous, frame)) {
			t.Fatalf("%s: operationsOff changed in generation %d", stage, frame.State.Generation)
		}
		previous = frame
		if want {
			client.mustReject(t, Command{Action: "fault", PodID: "01"}, "faults are not enabled")
			client.mustReject(t, Command{Action: "emergency", PodID: "01"}, "emergencies are not enabled")
		}
	}
	check("start", false)
	ordinary := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	client.mustApply(t, Command{Action: "demo"})
	client.mustApply(t, Command{Action: "pause", Paused: true})
	check("demo start", true)
	demo := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	client.mustApply(t, Command{Action: "reset"})
	check("reset after the demo", false)
	client.mustApply(t, Command{Action: "rewind", Checkpoint: demo})
	check("rewind to the demo", true)
	client.mustApply(t, Command{Action: "rewind", Checkpoint: ordinary})
	check("rewind to the ordinary fleet", false)
	client.mustApply(t, Command{Action: "demo"})
	client.mustApply(t, Command{Action: "pause", Paused: true})
	check("second demo", true)
	next := project.Default()
	next.Name = "Operations project apply"
	client.mustApply(t, Command{Action: "project", Project: &next, ProjectRevision: s.Project().Revision})
	check("project apply", false)
}

// TestOperationsOffDemoEnd runs the demo to its end. The fleet keeps its
// parked demo pods, so operationsOff stays true and the generation does not
// change. A reset removes the demo pods and gives operations back.
func TestOperationsOffDemoEnd(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	s := client.session
	client.mustApply(t, Command{Action: "demo"})
	generation := s.State().Generation
	for tick := 0; s.State().Simulation.Demo; tick++ {
		if tick > 3600*sim.TicksPerSecond {
			t.Fatal("the demo did not end in 1 simulated hour")
		}
		s.advance()
	}
	state := s.State()
	if !state.Simulation.OperationsOff || state.Generation != generation {
		t.Fatalf("after the demo: operationsOff %t, generation %d, want %d", state.Simulation.OperationsOff, state.Generation, generation)
	}
	client.mustReject(t, Command{Action: "fault", PodID: "01"}, "faults are not enabled")
	client.mustApply(t, Command{Action: "reset"})
	if s.State().Simulation.OperationsOff {
		t.Fatal("the reset did not end operationsOff")
	}
	client.mustApply(t, Command{Action: "fault", PodID: "01"})
}

// TestOperationsOffRestore checks operationsOff after the restore of a save
// of the demo: true in the physical tier of a running or an ended demo, and
// false in the logical tier, which removes the demo fleet.
func TestOperationsOffRestore(t *testing.T) {
	t.Parallel()
	run := newDemoRun(t)
	for _, test := range []struct {
		name string
		data []byte
		tier string
		want bool
	}{
		{"running demo", run.data, "physical", true},
		{"ended demo", run.edited(t, func(file *stateFile) { file.Simulation.Demo = nil }), "physical", true},
		{"logical tier", run.edited(t, logicalOnly), "logical", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			restored := startFromStore(t, StoreInput{Store: &fakeStore{data: test.data}})
			t.Cleanup(restored.Close)
			if restored.restore.Tier != test.tier {
				t.Fatalf("restore %+v", restored.restore)
			}
			if got := sessionFrame(t, restored).State.Simulation.OperationsOff; got != test.want {
				t.Fatalf("operationsOff %t, want %t", got, test.want)
			}
		})
	}
}

// TestOperationsCommands checks a fault, a clear, and an emergency in an
// ordinary project with no settings.
func TestOperationsCommands(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.SharedRidePartyLimit = 4
	client := newFaultClient(t, config)
	client.boardOrders(t, 1)
	fault := client.mustApply(t, Command{Action: "fault", PodID: "02"})
	if fault.FaultID == "" {
		t.Fatal("the fault command gave no ID")
	}
	client.mustApply(t, Command{Action: "clearFault", FaultID: fault.FaultID})
	if reply := client.mustApply(t, Command{Action: "emergency", PodID: "01"}); reply.EmergencyID == "" {
		t.Fatal("the emergency command gave no ID")
	}
}

// offFrames returns the topology and a frame of the demo fleet, and a frame
// of the same fleet with its records and counters from edit.
func offFrames(t *testing.T, edit func(*SimulationFrame)) (TopologySnapshot, StreamFrame, StreamFrame) {
	t.Helper()
	client := newFaultClient(t, project.Default())
	client.mustApply(t, Command{Action: "demo"})
	base := sessionFrame(t, client.session)
	frame := base
	edit(&frame.State.Simulation)
	return client.session.Topology(), base, frame
}

// TestOperationsOffFrameGuards checks the pinned text of each refusal of a
// frame with operationsOff and a typed fault or emergency value, at each
// place that checks a frame. With both, the fault refusal comes first.
func TestOperationsOffFrameGuards(t *testing.T) {
	t.Parallel()
	const faultText = "stream state with operations off contains a fault member"
	const emergencyText = "stream state with operations off contains an emergency member"
	for _, test := range []struct {
		name string
		edit func(*SimulationFrame)
		want string
	}{
		{"fault record", func(f *SimulationFrame) {
			f.Faults = sim.FaultsView{Active: []sim.FaultView{{ID: "i1.1", Kind: sim.FaultKindPod, PodID: f.Vehicles[0].Pod.ID, Phase: sim.FaultPhaseStopped}}}
		}, faultText},
		{"empty fault records", func(f *SimulationFrame) { f.Faults = sim.FaultsView{Active: []sim.FaultView{}} }, faultText},
		{"fault counter", func(f *SimulationFrame) { f.Faults.Counters.Started = 1 }, faultText},
		{"negative fault counter", func(f *SimulationFrame) { f.Faults.Counters.Started = -1 }, faultText},
		{"emergency record", func(f *SimulationFrame) {
			f.Emergencies = sim.EmergenciesView{Active: []sim.EmergencyView{{ID: "i1.1", PodID: f.Vehicles[0].Pod.ID, OrderID: 1, Phase: sim.EmergencyPhaseUnloading}}}
		}, emergencyText},
		{"empty emergency records", func(f *SimulationFrame) { f.Emergencies = sim.EmergenciesView{Active: []sim.EmergencyView{}} }, emergencyText},
		{"emergency counter", func(f *SimulationFrame) { f.Emergencies.Counters.Ended = 1 }, emergencyText},
		{"fault before emergency", func(f *SimulationFrame) {
			f.Faults.Counters.Cleared = 1
			f.Emergencies.Counters.Started = 1
		}, faultText},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			topology, base, frame := offFrames(t, test.edit)
			full := StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "off", Sequence: 1, Build: frame.State.Build, Source: sourceOf(frame), Full: &frame}
			assembler, err := NewStreamAssembler(topology)
			if err != nil {
				t.Fatal(err)
			}
			_, assembleErr := assembler.State(frame)
			_, fullErr := EncodeStreamJSON(full)
			_, applyErr := ApplyStream(StreamFrame{}, "off", 0, full)
			_, deltaErr := makeDelta(base, frame)
			_, stateErr := EncodeStateJSON(topology, frame)
			for name, err := range map[string]error{"assembler": assembleErr, "full encode": fullErr, "apply": applyErr, "delta": deltaErr, "HTTP encode": stateErr} {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Errorf("%s: %v, want %q", name, err, test.want)
				}
			}
		})
	}
}

// TestOperationsOffDecodedValues checks the decoder with the pinned text: a
// wire full frame or HTTP state with operationsOff and a value is refused by
// the typed check, after the decode. A frame with no such value passes.
func TestOperationsOffDecodedValues(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	client.mustApply(t, Command{Action: "demo"})
	topology, frames := incidentFrames(t, client.session)
	http, err := EncodeStateJSON(topology, frames[1])
	if err != nil {
		t.Fatal(err)
	}
	full := faultEnvelopeJSON(t, frames[0], frames[1], "full")
	if !bytes.Contains(full, []byte(`"operationsOff":true`)) || !bytes.Contains(http, []byte(`"operationsOff":true`)) {
		t.Fatal("a document of the demo has no operationsOff")
	}
	if delta := faultEnvelopeJSON(t, frames[0], frames[1], "delta"); bytes.Contains(delta, []byte("operationsOff")) {
		t.Fatalf("a delta has operationsOff: %s", delta)
	}
	const place = `"operationsOff":true,`
	for _, test := range []struct{ insert, want string }{
		{`"faults":{"counters":{"started":1}},`, "stream state with operations off contains a fault member"},
		{`"emergencies":{"counters":{"started":1}},`, "stream state with operations off contains an emergency member"},
	} {
		edit := func(raw []byte) []byte {
			edited := bytes.Replace(raw, []byte(place), []byte(place+test.insert), 1)
			if bytes.Equal(edited, raw) {
				t.Fatal("no edit")
			}
			return edited
		}
		if _, err := applyIncidentJSON(StreamFrame{}, edit(full)); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("full frame: %v, want %q", err, test.want)
		}
		if _, err := DecodeStateJSON(edit(http)); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("HTTP state: %v, want %q", err, test.want)
		}
	}
}

// TestSameChainComparesOperationsOff checks that a change of operationsOff
// alone ends a chain, so that it cannot pass as a delta.
func TestSameChainComparesOperationsOff(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	frame := sessionFrame(t, client.session)
	if !sameChain(frame, frame) {
		t.Fatal("a frame is not on the chain of itself")
	}
	changed := frame
	changed.State.Simulation.OperationsOff = true
	if sameChain(frame, changed) || sameChain(changed, frame) {
		t.Fatal("a change of operationsOff stays on the chain")
	}
}

// TestRemovedMarkersUnknown checks that each document refuses each removed
// marker name as an unknown member, with each value, in each place.
func TestRemovedMarkersUnknown(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	topology, frames := incidentFrames(t, client.session)
	topologyJSON, err := jsonv2.Marshal(topology, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	full, err := EncodeStreamJSON(streamFamilyEnvelope(t, frames, "full"))
	if err != nil {
		t.Fatal(err)
	}
	delta, err := EncodeStreamJSON(streamFamilyEnvelope(t, frames, "delta"))
	if err != nil {
		t.Fatal(err)
	}
	httpState, err := EncodeStateJSON(topology, frames[0])
	if err != nil {
		t.Fatal(err)
	}
	decodeTopology := func(raw []byte) error {
		return jsonv2.Unmarshal(raw, new(TopologySnapshot), json.DefaultOptionsV1())
	}
	decodeStream := func(raw []byte) error {
		_, err := DecodeStreamJSON(raw)
		return err
	}
	decodeHTTP := func(raw []byte) error {
		_, err := DecodeStateJSON(raw)
		return err
	}
	decodeDelta := func(raw []byte) error {
		envelope, err := DecodeStreamJSON(raw)
		if err != nil {
			return err
		}
		_, err = ApplyStream(frames[0], envelope.Stream, envelope.Base, envelope)
		return err
	}
	hello := []byte(fmt.Sprintf(`{"kind":"hello","version":%d,"build":"","serverStart":"x"}`, StreamVersion))
	decodeHello := func(raw []byte) error {
		_, err := DecodeStreamHello(raw)
		return err
	}
	for _, place := range []struct {
		name   string
		raw    []byte
		at     string
		decode func([]byte) error
	}{
		{"topology", topologyJSON, `{`, decodeTopology},
		{"full frame", full, `"simulation":{`, decodeStream},
		{"stream root", full, `{`, decodeStream},
		{"HTTP topology", httpState, `"topology":{`, decodeHTTP},
		{"HTTP frame", httpState, `"simulation":{`, decodeHTTP},
		{"HTTP root", httpState, `{`, decodeHTTP},
		{"delta group", delta, `"global":{`, decodeDelta},
		{"hello", hello, `{`, decodeHello},
	} {
		if err := place.decode(place.raw); err != nil {
			t.Fatal(place.name, "control refused", err)
		}
		for name, old := range removedMarkers {
			for _, value := range []string{`null`, `""`, `"` + old + `"`, `"other"`, `1`} {
				raw := bytes.Replace(place.raw, []byte(place.at), []byte(place.at+`"`+name+`":`+value+`,`), 1)
				if bytes.Equal(raw, place.raw) {
					t.Fatal(place.name, "no edit")
				}
				if err := place.decode(raw); err == nil || !strings.Contains(err.Error(), name) {
					t.Errorf("%s: %s %s: %v, want a refusal that names the member", place.name, name, value, err)
				}
			}
		}
	}
}

// TestStreamDecodeRefusalOrder pins the order of the stream and HTTP decode
// checks with inputs that have more than one error. A removed marker is
// refused by the strict decode, so each earlier scan reports first.
func TestStreamDecodeRefusalOrder(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	topology, frames := incidentFrames(t, client.session)
	full := faultEnvelopeJSON(t, frames[0], frames[1], "full")
	httpState, err := EncodeStateJSON(topology, frames[0])
	if err != nil {
		t.Fatal(err)
	}
	const at = `"simulation":{`
	const marker = `"incidentContract":"incident-v1",`
	for _, test := range []struct {
		name, insert, want string
	}{
		{"marker alone", marker, "incidentContract"},
		{"null incident member before the marker", marker + `"interrupted":null,`, "/interrupted"},
		{"null fault member before the marker", marker + `"faults":null,`, "/faults"},
		{"null emergency member before the marker", marker + `"emergencies":null,`, "/emergencies"},
		{"null incident before null fault", `"faults":null,"interrupted":null,`, "/interrupted"},
		{"null fault before null emergency", `"emergencies":null,"faults":null,`, "/faults"},
	} {
		for name, data := range map[string][]byte{"full": full, "http": httpState} {
			edited := bytes.Replace(data, []byte(at), []byte(at+test.insert), 1)
			var err error
			if name == "full" {
				_, err = DecodeStreamJSON(edited)
			} else {
				_, err = DecodeStateJSON(edited)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("%s, %s: %v, want %q", name, test.name, err, test.want)
			}
		}
	}
}

// TestStreamPackedScanBeforeRemovedMarker checks that a marker name comes
// to the strict decode, so a refusal of the packed scan comes before the
// unknown member.
func TestStreamPackedScanBeforeRemovedMarker(t *testing.T) {
	t.Parallel()
	_, frames := incidentStreamFrames(t)
	changed := withIncidentMembers(frames[1])
	packed := []byte(`"legFrom":"Z2FyZGVu"`)
	raw, err := EncodeStreamJSON(incidentEnvelope(t, frames[1], changed, "full"))
	if err != nil {
		t.Fatal(err)
	}
	const at = `"simulation":{`
	edited := bytes.Replace(raw, []byte(at), []byte(at+`"incidentContract":"incident-v1",`), 1)
	if _, decodeErr := DecodeStreamJSON(edited); decodeErr == nil || !strings.Contains(decodeErr.Error(), "incidentContract") {
		t.Fatalf("marker alone: %v", decodeErr)
	}
	both := bytes.Replace(edited, packed, []byte(`"legFrom":"Z2FyZGV"`), 1)
	if bytes.Equal(both, edited) {
		t.Fatal("no packed leg origin")
	}
	want := scanPackedOrders(both)
	if want == nil {
		t.Fatal("the packed scan accepts the leg origin")
	}
	if _, decodeErr := DecodeStreamJSON(both); decodeErr == nil || decodeErr.Error() != want.Error() {
		t.Errorf("packed and marker: %v, want %v", decodeErr, want)
	}
}

// TestRemovedMarkerDeltaGroups checks a removed marker as a group name of a
// delta. The groups are a map, so the strict decode cannot refuse the name,
// and the apply refuses it as each other unknown group.
func TestRemovedMarkerDeltaGroups(t *testing.T) {
	t.Parallel()
	_, frames := incidentStreamFrames(t)
	raw, err := EncodeStreamJSON(incidentEnvelope(t, frames[0], withIncidentMembers(frames[1]), "delta"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyIncidentJSON(frames[0], raw); err != nil {
		t.Fatal("control:", err)
	}
	const at = `"groups":{`
	for _, name := range []string{"incidentContract", "faultContract", "emergencyContract", "other"} {
		edited := bytes.Replace(raw, []byte(at), []byte(at+`"`+name+`":"incident-v1",`), 1)
		if bytes.Equal(edited, raw) {
			t.Fatal("no groups")
		}
		_, applyErr := applyIncidentJSON(frames[0], edited)
		want := `unknown stream group "` + name + `"`
		if applyErr == nil || applyErr.Error() != want {
			t.Errorf("%s: %v, want %q", name, applyErr, want)
		}
	}
}

// TestReplacementGroupsNeedOperations checks the replacement groups on a
// base frame with operationsOff. The incident group has no such rule, and
// its value rules stay. The fault refusal does not depend on the value, so
// it comes before the value rules.
func TestReplacementGroupsNeedOperations(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	client.mustApply(t, Command{Action: "demo"})
	_, frames := incidentFrames(t, client.session)
	envelope := incidentEnvelope(t, frames[0], frames[1], "delta")
	for _, test := range []struct {
		group, value, want string
	}{
		{"faults", `{}`, "stream state with operations off contains a fault member"},
		{"faults", `{"counters":{"started":null}}`, "stream state with operations off contains a fault member"},
		{"emergencies", `{}`, "stream state with operations off contains an emergency member"},
		{"emergencies", `{"counters":{"started":null}}`, "stream state with operations off contains an emergency member"},
		{"incident", `{"interrupted":1}`, ""},
		{"incident", `{"interrupted":null}`, "is null"},
	} {
		groups := map[string]jsontext.Value{test.group: jsontext.Value(test.value)}
		edited := envelope
		delta := *envelope.Delta
		delta.Groups = groups
		edited.Delta = &delta
		_, err := ApplyStream(frames[0], envelope.Stream, envelope.Base, edited)
		if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Errorf("%s %s: %v, want %q", test.group, test.value, err, test.want)
		}
	}
}

// operationsIdentityTicks is the length of each matched-seed run.
const operationsIdentityTicks = 200 * sim.TicksPerSecond

// TestOperationsOffIdentity checks the off-state gate of the marker removal:
// London Central and the rail hub with the same demand seed and no command
// give the same saved state at every 600th tick with the operations on and
// off, and the same saved-state bytes at the last tick.
func TestOperationsOffIdentity(t *testing.T) {
	t.Parallel()
	for name, preset := range map[string]func() project.Config{"london-central": scenarios.LondonCentral, "rail-hub": scenarios.RailHub} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var runs [2]*Session
			for index, operations := range []bool{true, false} {
				config := preset()
				config.Demand.Enabled = true
				s, err := NewWithProject(config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(s.Close)
				s.simulation.SetPaused(false)
				if err := project.ConfigureExperiments(s.simulation, s.project, operations); err != nil {
					t.Fatal(err)
				}
				runs[index] = s
			}
			if !runs[1].State().Simulation.OperationsOff || runs[0].State().Simulation.OperationsOff {
				t.Fatal("the switches of the runs are wrong")
			}
			for tick := 1; tick <= operationsIdentityTicks; tick++ {
				for _, s := range runs {
					s.advance()
				}
				if tick%(10*sim.TicksPerSecond) != 0 {
					continue
				}
				on, off := runs[0].simulation.ExportState(), runs[1].simulation.ExportState()
				if !reflect.DeepEqual(on, off) || !reflect.DeepEqual(runs[0].demand.state, runs[1].demand.state) {
					t.Fatalf("tick %d: the run with the operations off differs", tick)
				}
			}
			final := runs[0].simulation.ExportState()
			if final.Tick != operationsIdentityTicks || final.RequestID == 0 {
				t.Fatalf("the run did not advance with demand: tick %d, orders %d", final.Tick, final.RequestID)
			}
			if !bytes.Equal(encodeTestState(t, identityFile(t, runs[0])), encodeTestState(t, identityFile(t, runs[1]))) {
				t.Fatal("the saved-state bytes differ")
			}
		})
	}
}

// identityFile returns the state file of s with the session identity that
// two sessions do not share cleared.
func identityFile(t *testing.T, s *Session) stateFile {
	t.Helper()
	file := sessionStateFile(t, s)
	file.Build = ""
	return file
}

// TestPlainDocumentsHaveNoOperationsMembers checks that a plain session with
// no fault and no emergency writes no operations member in a full frame, a
// delta, or an HTTP state, and no removed marker.
func TestPlainDocumentsHaveNoOperationsMembers(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, project.Default())
	topology, frames := incidentFrames(t, client.session)
	httpState, err := EncodeStateJSON(topology, frames[0])
	if err != nil {
		t.Fatal(err)
	}
	documents := map[string][]byte{
		"full":  faultEnvelopeJSON(t, frames[0], frames[1], "full"),
		"delta": faultEnvelopeJSON(t, frames[0], frames[1], "delta"),
		"http":  httpState,
	}
	names := slices.Concat([]string{"operationsOff", `"interrupted"`, `"faults"`, `"emergencies"`}, slices.Collect(maps.Keys(removedMarkers)))
	for document, raw := range documents {
		for _, name := range names {
			if bytes.Contains(raw, []byte(name)) {
				t.Errorf("the %s document has %s", document, name)
			}
		}
	}
}
