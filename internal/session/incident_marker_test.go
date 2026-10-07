package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentMarkerMember is the encoded incident marker of a topology or a
// frame.
const incidentMarkerMember = `"incidentContract":"incident-v1"`

// markedProject returns the example project with the incident marker.
func markedProject() project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	return config
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

// TestIncidentMarkerPropagation checks that the marker of the project
// reaches the topology, the full frame and the HTTP state, and that a
// decoded stream keeps it across a delta, which carries no marker.
func TestIncidentMarkerPropagation(t *testing.T) {
	t.Parallel()
	s, err := NewWithProject(markedProject())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	topology, frames := incidentFrames(t, s)
	if topology.IncidentContract != sim.IncidentV1Contract || frames[0].State.Simulation.IncidentContract != sim.IncidentV1Contract {
		t.Fatal("topology or frame lost the marker")
	}
	topologyJSON, err := jsonv2.Marshal(topology, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var decodedTopology TopologySnapshot
	if decodeErr := jsonv2.Unmarshal(topologyJSON, &decodedTopology, json.DefaultOptionsV1()); decodeErr != nil || decodedTopology.IncidentContract != sim.IncidentV1Contract {
		t.Fatalf("decoded topology marker %q: %v", decodedTopology.IncidentContract, decodeErr)
	}
	httpState, err := EncodeStateJSON(topology, frames[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(httpState, []byte(incidentMarkerMember)); got != 2 {
		t.Fatalf("HTTP state has %d markers, want one in the topology and one in the frame", got)
	}
	state, err := DecodeStateJSON(httpState)
	if err != nil || state.Simulation.IncidentContract != sim.IncidentV1Contract {
		t.Fatalf("HTTP state marker %q: %v", state.Simulation.IncidentContract, err)
	}
	assembler, err := NewStreamAssembler(decodedTopology)
	if err != nil {
		t.Fatal(err)
	}
	var previous StreamFrame
	for index, kind := range []string{"full", "delta"} {
		raw, err := EncodeStreamJSON(streamFamilyEnvelope(t, frames, kind))
		if err != nil {
			t.Fatal(err)
		}
		if got := bytes.Count(raw, []byte(incidentMarkerMember)); got != 1-index {
			t.Fatalf("%s envelope has %d markers, want %d", kind, got, 1-index)
		}
		envelope, err := DecodeStreamJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		previous, err = ApplyStream(previous, envelope.Stream, envelope.Base, envelope)
		if err != nil {
			t.Fatal(kind, err)
		}
		state, err := assembler.State(previous)
		if err != nil || state.Simulation.IncidentContract != sim.IncidentV1Contract {
			t.Fatalf("%s: assembled marker %q: %v", kind, state.Simulation.IncidentContract, err)
		}
	}
}

// TestIncidentMarkerSurvivesSessionChanges checks that the topology and the
// frame keep the same marker after the traffic demo, a reset, a rewind and
// a restart, so that the frame binding accepts each frame.
func TestIncidentMarkerSurvivesSessionChanges(t *testing.T) {
	t.Parallel()
	config := markedProject()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	client := newTestClient(s, "incident")
	check := func(stage string, s *Session) {
		t.Helper()
		frame, err := s.presentationFrame()
		if err != nil {
			t.Fatal(stage, err)
		}
		state, err := FrameState(s.Topology(), frame.State)
		if err != nil || state.Simulation.IncidentContract != sim.IncidentV1Contract {
			t.Fatalf("%s: marker %q: %v", stage, state.Simulation.IncidentContract, err)
		}
	}
	checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	client.mustApply(t, Command{Action: "demo"})
	check("demo", s)
	client.mustApply(t, Command{Action: "reset"})
	check("reset", s)
	client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
	check("rewind", s)
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	restored := startFromStore(t, StoreInput{Store: store})
	t.Cleanup(restored.Close)
	if restored.restore.Tier != string(sim.RestorePhysical) {
		t.Fatalf("restart tier %q", restored.restore.Tier)
	}
	check("restart", restored)
}

// TestIncidentFrameBinding checks that a frame whose marker differs from
// the marker of its topology is refused, in both directions, by FrameState,
// by the stream assembler and by the HTTP state encoder. The assembler
// thus refuses a marker change inside one stream.
func TestIncidentFrameBinding(t *testing.T) {
	t.Parallel()
	marked, err := NewWithProject(markedProject())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(marked.Close)
	topology, frames := incidentFrames(t, marked)
	for _, change := range []struct {
		name     string
		topology sim.IncidentContract
		frame    sim.IncidentContract
	}{{"unmarked topology", "", sim.IncidentV1Contract}, {"unmarked frame", sim.IncidentV1Contract, ""}} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			bound := topology
			bound.IncidentContract = change.topology
			frame := frames[1]
			frame.State.Simulation.IncidentContract = change.frame
			if _, err := FrameState(bound, frame.State); err == nil || !strings.Contains(err.Error(), "incident contract") {
				t.Fatalf("FrameState error %v, want an incident contract mismatch", err)
			}
			if _, err := EncodeStateJSON(bound, frame); err == nil {
				t.Fatal("HTTP state encoder accepted a marker mismatch")
			}
			assembler, err := NewStreamAssembler(bound)
			if err != nil {
				t.Fatal(err)
			}
			first := frames[0]
			first.State.Simulation.IncidentContract = change.topology
			if _, err := assembler.State(first); err != nil {
				t.Fatal("control frame refused", err)
			}
			if _, err := assembler.State(frame); err == nil {
				t.Fatal("assembler accepted a marker change inside one stream")
			}
		})
	}
}

// TestIncidentMarkerRawValues checks that each decoder refuses an incident
// marker that is null, empty or another value, and a marker in a place
// that has none: the envelope root, a delta group and the hello.
func TestIncidentMarkerRawValues(t *testing.T) {
	t.Parallel()
	marked, err := NewWithProject(markedProject())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(marked.Close)
	topology, frames := incidentFrames(t, marked)
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
	decoders := map[string]struct {
		raw    []byte
		decode func([]byte) error
	}{
		"topology": {topologyJSON, func(raw []byte) error { return jsonv2.Unmarshal(raw, new(TopologySnapshot), json.DefaultOptionsV1()) }},
		"full":     {full, func(raw []byte) error { _, err := DecodeStreamJSON(raw); return err }},
		"http":     {httpState, func(raw []byte) error { _, err := DecodeStateJSON(raw); return err }},
	}
	for name, decoder := range decoders {
		if err := decoder.decode(decoder.raw); err != nil {
			t.Fatal(name, "control refused", err)
		}
		for _, value := range []string{`null`, `""`, `"incident-v2"`, `1`} {
			raw := bytes.ReplaceAll(decoder.raw, []byte(incidentMarkerMember), []byte(`"incidentContract":`+value))
			if err := decoder.decode(raw); err == nil {
				t.Errorf("%s accepted the marker %s", name, value)
			}
		}
	}
	misplaced := []struct {
		name          string
		original, raw []byte
	}{
		{"stream root", full, bytes.Replace(full, []byte(`{"kind"`), []byte(`{`+incidentMarkerMember+`,"kind"`), 1)},
		{"http root", httpState, bytes.Replace(httpState, []byte(`{"topology"`), []byte(`{`+incidentMarkerMember+`,"topology"`), 1)},
		{"delta group", delta, bytes.Replace(delta, []byte(`"global":{`), []byte(`"global":{`+incidentMarkerMember+`,`), 1)},
	}
	decode := func(name string, raw []byte) error {
		if name == "http root" {
			_, err := DecodeStateJSON(raw)
			return err
		}
		envelope, err := DecodeStreamJSON(raw)
		if err == nil && name == "delta group" {
			_, err = ApplyStream(frames[0], envelope.Stream, envelope.Base, envelope)
		}
		return err
	}
	for _, item := range misplaced {
		if bytes.Equal(item.raw, item.original) {
			t.Fatal(item.name, "fixture did not add a marker")
		}
		if err := decode(item.name, item.original); err != nil {
			t.Fatal(item.name, "control refused", err)
		}
		if decode(item.name, item.raw) == nil {
			t.Errorf("%s accepted a marker", item.name)
		}
	}
	hello := []byte(`{"kind":"hello","version":6,"build":"","serverStart":"x",` + incidentMarkerMember + `}`)
	if _, err := DecodeStreamHello(hello); err == nil {
		t.Error("hello accepted a marker")
	}
}

// TestIncidentMarkerOnlyBytes checks section 12 of the incident contract
// for the stream: with the marker and no feature, the full frame and the
// HTTP state differ from an unmarked session only by the marker, and a
// delta does not differ.
func TestIncidentMarkerOnlyBytes(t *testing.T) {
	t.Parallel()
	encode := func(config project.Config) (full, delta, httpState []byte) {
		t.Helper()
		s, err := NewWithProject(config)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		s.Apply(Command{Client: "bytes", Sequence: 1, Epoch: s.State().Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
		advanceTicks(s, 90)
		topology, frames := incidentFrames(t, s)
		topology.ServerStart, topology.Epoch = "fixed-start", "fixed-epoch"
		for i := range frames {
			frames[i].State.ServerStart, frames[i].State.Epoch, frames[i].State.Build = "fixed-start", "fixed-epoch", "fixed-build"
		}
		var encoded [3][]byte
		for i, kind := range []string{"full", "delta"} {
			if encoded[i], err = EncodeStreamJSON(streamFamilyEnvelope(t, frames, kind)); err != nil {
				t.Fatal(err)
			}
		}
		if encoded[2], err = EncodeStateJSON(topology, frames[0]); err != nil {
			t.Fatal(err)
		}
		return encoded[0], encoded[1], encoded[2]
	}
	plainFull, plainDelta, plainHTTP := encode(project.Default())
	markedFull, markedDelta, markedHTTP := encode(markedProject())
	for _, pair := range []struct {
		name          string
		plain, marked []byte
		markers       int
	}{{"full", plainFull, markedFull, 1}, {"delta", plainDelta, markedDelta, 0}, {"http", plainHTTP, markedHTTP, 2}} {
		if bytes.Contains(pair.plain, []byte("incidentContract")) {
			t.Fatalf("unmarked %s has an incident member", pair.name)
		}
		if got := bytes.Count(pair.marked, []byte(incidentMarkerMember)); got != pair.markers {
			t.Fatalf("marked %s has %d markers, want %d", pair.name, got, pair.markers)
		}
		if !bytes.Equal(bytes.ReplaceAll(pair.marked, []byte(incidentMarkerMember+","), nil), pair.plain) {
			t.Fatalf("marked %s differs from the unmarked bytes by more than the marker", pair.name)
		}
	}
}

// incidentMatchedSeedTicks is the length of each matched-seed run.
const incidentMatchedSeedTicks = 36000

// TestIncidentMarkerMatchedSeedRuns checks the off-state gate of section
// 12 of the incident contract: with the marker and no feature, London
// Central and the rail hub with the same demand seed give the same saved
// simulation and demand state at every 600th tick as without the marker.
func TestIncidentMarkerMatchedSeedRuns(t *testing.T) {
	t.Parallel()
	for name, preset := range map[string]func() project.Config{"london-central": scenarios.LondonCentral, "rail-hub": scenarios.RailHub} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var runs [2]*Session
			for index, marker := range []sim.IncidentContract{"", sim.IncidentV1Contract} {
				config := preset()
				config.Demand.Enabled = true
				config.IncidentContract = marker
				s, err := NewWithProject(config)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(s.Close)
				s.simulation.SetPaused(false)
				runs[index] = s
			}
			for tick := 1; tick <= incidentMatchedSeedTicks; tick++ {
				for _, s := range runs {
					s.advance()
				}
				if tick%600 != 0 {
					continue
				}
				plain, marked := runs[0].simulation.ExportState(), runs[1].simulation.ExportState()
				if !reflect.DeepEqual(plain, marked) || !reflect.DeepEqual(runs[0].demand.state, runs[1].demand.state) {
					t.Fatalf("tick %d: the marked run differs from the unmarked run", tick)
				}
			}
			if final := runs[0].simulation.ExportState(); final.Tick != incidentMatchedSeedTicks || final.RequestID == 0 {
				t.Fatalf("the run did not advance with demand: tick %d, orders %d", final.Tick, final.RequestID)
			}
		})
	}
}

// TestIncidentMarkerUnknownTyped checks that each typed boundary refuses an
// unknown incident marker, also when the topology and the frame agree on
// it, as it refuses an unknown order marker. Without this, an
// encoder would write a document that its own decoder refuses.
func TestIncidentMarkerUnknownTyped(t *testing.T) {
	t.Parallel()
	marked, err := NewWithProject(markedProject())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(marked.Close)
	topology, frames := incidentFrames(t, marked)
	full := streamFamilyEnvelope(t, frames, "full")
	delta := streamFamilyEnvelope(t, frames, "delta")
	// Each check returns the error of one boundary for a topology and two
	// frames with the given marker.
	checks := map[string]func(topology TopologySnapshot, frames [2]StreamFrame) error{
		"FrameState": func(topology TopologySnapshot, frames [2]StreamFrame) error {
			_, err := FrameState(topology, frames[0].State)
			return err
		},
		"NewStreamAssembler": func(topology TopologySnapshot, _ [2]StreamFrame) error {
			_, err := NewStreamAssembler(topology)
			return err
		},
		"EncodeStateJSON": func(topology TopologySnapshot, frames [2]StreamFrame) error {
			_, err := EncodeStateJSON(topology, frames[0])
			return err
		},
		"EncodeStreamJSON": func(_ TopologySnapshot, frames [2]StreamFrame) error {
			envelope := full
			envelope.Full = &frames[0]
			_, err := EncodeStreamJSON(envelope)
			return err
		},
		"ApplyStream full": func(_ TopologySnapshot, frames [2]StreamFrame) error {
			envelope := full
			envelope.Full = &frames[0]
			_, err := ApplyStream(StreamFrame{}, "", 0, envelope)
			return err
		},
		"ApplyStream delta": func(_ TopologySnapshot, frames [2]StreamFrame) error {
			_, err := ApplyStream(frames[0], delta.Stream, delta.Base, delta)
			return err
		},
	}
	for name, check := range checks {
		if err := check(topology, frames); err != nil {
			t.Fatal(name, "control refused", err)
		}
		unknown := topology
		unknown.IncidentContract = "incident-v2"
		var changed [2]StreamFrame
		for i := range frames {
			changed[i] = frames[i]
			changed[i].State.Simulation.IncidentContract = "incident-v2"
		}
		if err := check(unknown, changed); !errors.Is(err, sim.ErrUnknownIncidentContract) {
			t.Errorf("%s: error %v, want %v", name, err, sim.ErrUnknownIncidentContract)
		}
	}
}
