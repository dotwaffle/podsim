package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentStreamFrames returns the topology and two frames of the example
// project with a pause change between them. marked selects the incident
// marker.
func incidentStreamFrames(t *testing.T, marked bool) (TopologySnapshot, [2]StreamFrame) {
	t.Helper()
	config := project.Default()
	if marked {
		config.IncidentContract = sim.IncidentV1Contract
	}
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return incidentFrames(t, s)
}

// withIncidentMembers returns frame with each stage 1 stream member set:
// the counters, the holds and the purpose of the first vehicle, a rider
// with a leg origin on that vehicle, and a pending order with a leg
// origin.
func withIncidentMembers(frame StreamFrame) StreamFrame {
	order := func(id int) sim.Request {
		return sim.Request{ID: id, From: "harbor", LegFrom: "garden", To: "market", PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}
	}
	simulation := &frame.State.Simulation
	simulation.Interrupted, simulation.InterruptedPassengers = 1, 2
	simulation.Vehicles = slices.Clone(simulation.Vehicles)
	vehicle := &simulation.Vehicles[0]
	vehicle.Withdrawn, vehicle.Operational = 2, sim.OperationalEmergencyUnload
	vehicle.Riders = []sim.Request{order(1)}
	simulation.Pending = []sim.Request{order(2)}
	return frame
}

// incidentEnvelope returns a full envelope of frame, or a delta envelope
// from base to frame.
func incidentEnvelope(t *testing.T, base, frame StreamFrame, kind string) StreamEnvelope {
	t.Helper()
	e := StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "incident", Sequence: 1, Build: frame.State.Build, Source: sourceOf(frame), Full: &frame}
	if kind == "full" {
		return e
	}
	delta, err := makeDelta(base, frame)
	if err != nil {
		t.Fatal(err)
	}
	e.Kind, e.Full, e.Delta, e.Sequence, e.Base = "delta", nil, &delta, 2, 1
	return e
}

// applyIncidentJSON decodes raw and applies it to base, as a stream client
// does.
func applyIncidentJSON(base StreamFrame, raw []byte) (StreamFrame, error) {
	envelope, err := DecodeStreamJSON(raw)
	if err != nil {
		return StreamFrame{}, err
	}
	return ApplyStream(base, "incident", 1, envelope)
}

// TestIncidentStreamGroups checks that each delta group of section 11.2 of
// the incident contract carries its member, that the leg origin is packed,
// that the client gets each member back from a full frame, a delta, and
// the HTTP state, and that the global group does not change.
func TestIncidentStreamGroups(t *testing.T) {
	t.Parallel()
	topology, frames := incidentStreamFrames(t, true)
	base := frames[1]
	changed := withIncidentMembers(base)
	delta, err := makeDelta(base, changed)
	if err != nil {
		t.Fatal(err)
	}
	if keys := slices.Sorted(maps.Keys(delta.Groups)); !slices.Equal(keys, []string{"incident", "pending"}) {
		t.Fatalf("delta groups %v", keys)
	}
	if got := string(delta.Groups["incident"]); got != `{"interrupted":1,"interruptedPassengers":2}` {
		t.Fatalf("incident group %s", got)
	}
	if !bytes.Contains(delta.Groups["pending"], []byte(`"legFrom":"Z2FyZGVu"`)) {
		t.Fatalf("pending group %s has no packed leg origin", delta.Groups["pending"])
	}
	if len(delta.Vehicles) != 1 || delta.Vehicles[0].Metadata == nil || delta.Vehicles[0].Riders == nil {
		t.Fatalf("vehicle replacements %+v", delta.Vehicles)
	}
	if metadata := delta.Vehicles[0].Metadata.Value; metadata.Withdrawn != 2 || metadata.Operational != sim.OperationalEmergencyUnload {
		t.Fatalf("metadata %+v", metadata)
	}
	if riders := delta.Vehicles[0].Riders.Value; len(riders) != 1 || riders[0].LegFrom != "garden" {
		t.Fatalf("riders %+v", riders)
	}
	groups, err := frameGroups(base)
	if err != nil {
		t.Fatal(err)
	}
	changedGroups, err := frameGroups(changed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(groups["global"], changedGroups["global"]) {
		t.Fatalf("the counters change the global group: %s and %s", groups["global"], changedGroups["global"])
	}
	_, plainFrames := incidentStreamFrames(t, false)
	plainGroups, err := frameGroups(plainFrames[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, found := plainGroups["incident"]; found || !bytes.Equal(plainGroups["global"], groups["global"]) {
		t.Fatalf("unmarked groups %v, global %s", slices.Sorted(maps.Keys(plainGroups)), plainGroups["global"])
	}
	want := incidentView(changed.State.Simulation)
	for _, kind := range []string{"full", "delta"} {
		raw, encodeErr := EncodeStreamJSON(incidentEnvelope(t, base, changed, kind))
		if encodeErr != nil {
			t.Fatal(kind, encodeErr)
		}
		if got := bytes.Count(raw, []byte(`"legFrom":"Z2FyZGVu"`)); got != 2 || bytes.Contains(raw, []byte(`"legFrom":"garden"`)) {
			t.Fatalf("%s envelope has %d packed leg origins", kind, got)
		}
		applied, loopErr := applyIncidentJSON(base, raw)
		if loopErr != nil {
			t.Fatal(kind, loopErr)
		}
		if got := incidentView(applied.State.Simulation); !slices.Equal(got.Vehicles, want.Vehicles) || !slices.Equal(got.Pending, want.Pending) ||
			got.Interrupted != want.Interrupted || got.InterruptedPassengers != want.InterruptedPassengers {
			t.Fatalf("%s envelope gives %+v, want %+v", kind, got, want)
		}
		// The delta of the next frame clears each member.
		cleared, loopErr := EncodeStreamJSON(incidentEnvelope(t, changed, base, "delta"))
		if loopErr != nil {
			t.Fatal(loopErr)
		}
		applied, loopErr = ApplyStream(applied, "incident", 1, mustDecodeStream(t, cleared))
		if loopErr != nil {
			t.Fatal(kind, loopErr)
		}
		if got, wantBase := incidentView(applied.State.Simulation), incidentView(base.State.Simulation); !slices.Equal(got.Vehicles, wantBase.Vehicles) || got.Interrupted != 0 || len(got.Pending) != 0 {
			t.Fatalf("after %s, the clearing delta gives %+v", kind, got)
		}
	}
	httpState, err := EncodeStateJSON(topology, changed)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DecodeStateJSON(httpState)
	if err != nil {
		t.Fatal(err)
	}
	if got := incidentView(stateFrame(state).Simulation); !slices.Equal(got.Vehicles, want.Vehicles) || !slices.Equal(got.Pending, want.Pending) || got.Interrupted != 1 {
		t.Fatalf("HTTP state gives %+v", got)
	}
}

func mustDecodeStream(t *testing.T, raw []byte) StreamEnvelope {
	t.Helper()
	envelope, err := DecodeStreamJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

// incidentMemberEdit inserts a raw member into an encoded envelope.
type incidentMemberEdit struct {
	name, kind, at, insert string
	// marked is true when the edit is valid with the incident marker.
	marked bool
}

// incidentMemberEdits are a stage 1 member at each stream place, as an
// explicit zero, null, or empty value.
var incidentMemberEdits = []incidentMemberEdit{
	{"withdrawn zero", "delta", `"metadata":{"value":{`, `"withdrawn":0,`, true},
	{"operational empty", "delta", `"metadata":{"value":{`, `"operational":"",`, false},
	{"operational null", "delta", `"metadata":{"value":{`, `"operational":null,`, false},
	{"pending leg origin null", "delta", `"pending":[{`, `"legFrom":null,`, false},
	{"rider leg origin null", "delta", `"riders":{"value":[{`, `"legFrom":null,`, false},
	{"rider leg origin empty", "delta", `"riders":{"value":[{`, `"legFrom":"",`, false},
	{"empty incident group", "delta", `"groups":{`, `"incident":{},`, true},
	{"incident group null", "delta", `"groups":{`, `"incident":null,`, false},
	{"interrupted null", "delta", `"groups":{`, `"incident":{"interrupted":null},`, false},
	{"interrupted passengers null", "delta", `"groups":{`, `"incident":{"interruptedPassengers":null},`, false},
	{"full interrupted zero", "full", `"simulation":{`, `"interrupted":0,`, true},
	{"full interrupted passengers zero", "full", `"simulation":{`, `"interruptedPassengers":0,`, true},
	{"full withdrawn zero", "full", `"vehicles":[{`, `"withdrawn":0,`, true},
	{"full pending leg origin null", "full", `"pending":[{`, `"legFrom":null,`, false},
}

// incidentEditFrames returns a base frame and a next frame with a changed
// vehicle metadata group, a changed rider list, and a changed pending
// group, with no stage 1 member.
func incidentEditFrames(t *testing.T, marked bool) (StreamFrame, StreamFrame) {
	t.Helper()
	_, frames := incidentStreamFrames(t, marked)
	next := withIncidentMembers(frames[1])
	simulation := &next.State.Simulation
	simulation.Interrupted, simulation.InterruptedPassengers = 0, 0
	vehicle := &simulation.Vehicles[0]
	vehicle.Withdrawn, vehicle.Operational, vehicle.Rebalancing = 0, "", !vehicle.Rebalancing
	vehicle.Riders = []sim.Request{{ID: 1, From: "harbor", To: "market", PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}}
	simulation.Pending = []sim.Request{{ID: 2, From: "harbor", To: "market", PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}}
	return frames[1], next
}

// TestIncidentStreamRawPresence checks the raw presence rule of section
// 11.2 of the incident contract: under a frame without the marker, a
// stage 1 member with an explicit zero, null, or empty value is refused
// before the delta applies, although the typed decode reads it as no
// member. With the marker, an explicit zero is accepted, and null or an
// empty text is refused.
func TestIncidentStreamRawPresence(t *testing.T) {
	t.Parallel()
	for _, marked := range []bool{false, true} {
		base, next := incidentEditFrames(t, marked)
		raw := map[string][]byte{}
		for _, kind := range []string{"full", "delta"} {
			var err error
			if raw[kind], err = EncodeStreamJSON(incidentEnvelope(t, base, next, kind)); err != nil {
				t.Fatal(err)
			}
			if _, err := applyIncidentJSON(base, raw[kind]); err != nil {
				t.Fatalf("marked %v: control %s: %v", marked, kind, err)
			}
		}
		for _, edit := range incidentMemberEdits {
			data := raw[edit.kind]
			if bytes.Count(data, []byte(edit.at)) < 1 {
				t.Fatalf("%s: the %s envelope has no %s", edit.name, edit.kind, edit.at)
			}
			data = bytes.Replace(data, []byte(edit.at), []byte(edit.at+edit.insert), 1)
			_, err := applyIncidentJSON(base, data)
			if valid := marked && edit.marked; (err == nil) != valid {
				t.Errorf("marked %v, %s: error %v, want valid %v", marked, edit.name, err, valid)
			}
		}
	}
}

// TestIncidentStreamDirectGroups checks the raw presence rule of section
// 11.2 of the incident contract on a delta that a caller builds. Such an
// envelope has no decoder scan, so ApplyStream scans the raw incident and
// pending groups itself.
func TestIncidentStreamDirectGroups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, group, value string
		// marked is true when the group is valid with the incident marker.
		marked bool
	}{
		{"pending leg origin null", "pending", `[{"legFrom":null,`, false},
		{"pending leg origin empty", "pending", `[{"legFrom":"",`, false},
		{"pending leg origin", "pending", `[{"legFrom":"Z2FyZGVu",`, true},
		{"interrupted null", "incident", `{"interrupted":null,"interruptedPassengers":0}`, false},
		{"interrupted passengers null", "incident", `{"interrupted":0,"interruptedPassengers":null}`, false},
		{"incident counters zero", "incident", `{"interrupted":0,"interruptedPassengers":0}`, true},
	}
	for _, marked := range []bool{false, true} {
		base, next := incidentEditFrames(t, marked)
		if _, err := ApplyStream(base, "incident", 1, incidentEnvelope(t, base, next, "delta")); err != nil {
			t.Fatalf("marked %v: control: %v", marked, err)
		}
		for _, test := range tests {
			e := incidentEnvelope(t, base, next, "delta")
			value := []byte(test.value)
			if test.group == "pending" {
				pending := e.Delta.Groups["pending"]
				if !bytes.HasPrefix(pending, []byte(`[{`)) {
					t.Fatalf("pending group %s", pending)
				}
				value = append(value, pending[2:]...)
			}
			e.Delta.Groups[test.group] = value
			_, err := ApplyStream(base, "incident", 1, e)
			if valid := marked && test.marked; (err == nil) != valid {
				t.Errorf("marked %v, %s: error %v, want valid %v", marked, test.name, err, valid)
			}
		}
	}
}

// TestIncidentStreamValues checks the stream rejections of section 11.6 of
// the incident contract: an unknown hold bit, an unknown purpose name, a
// purpose without a hold, a negative counter, and an unknown leg origin
// station. Each decoder and each encoder refuses them.
func TestIncidentStreamValues(t *testing.T) {
	t.Parallel()
	topology, frames := incidentStreamFrames(t, true)
	tests := []struct {
		name   string
		change func(*SimulationFrame)
	}{
		{"valid", func(*SimulationFrame) {}},
		{"unknown hold bit", func(s *SimulationFrame) { s.Vehicles[0].Withdrawn = 4 }},
		{"unknown purpose", func(s *SimulationFrame) { s.Vehicles[0].Operational = "parked" }},
		{"purpose without a hold", func(s *SimulationFrame) { s.Vehicles[0].Withdrawn = 0 }},
		{"negative counter", func(s *SimulationFrame) { s.Interrupted = -1 }},
		{"unknown leg origin", func(s *SimulationFrame) { s.Pending[0].LegFrom = "nowhere" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			frame := withIncidentMembers(frames[1])
			test.change(&frame.State.Simulation)
			valid := test.name == "valid"
			// The leg origin needs the topology, so only the assembler and
			// the HTTP state encoder check its station.
			typed := valid || test.name == "unknown leg origin"
			e := incidentEnvelope(t, frames[0], frame, "full")
			if _, err := ApplyStream(StreamFrame{}, "", 0, e); (err == nil) != typed {
				t.Errorf("ApplyStream: %v", err)
			}
			if _, err := EncodeStreamJSON(e); (err == nil) != typed {
				t.Errorf("EncodeStreamJSON: %v", err)
			}
			if _, err := makeDelta(frames[0], frame); (err == nil) != typed {
				t.Errorf("makeDelta: %v", err)
			}
			assembler, err := NewStreamAssembler(topology)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := assembler.State(frame); (err == nil) != valid {
				t.Errorf("assembler: %v", err)
			}
			if _, err := EncodeStateJSON(topology, frame); (err == nil) != valid {
				t.Errorf("EncodeStateJSON: %v", err)
			}
		})
	}
}

// TestIncidentStreamNeedsMarker checks that without the marker no encoder
// writes a stage 1 stream member, and that the HTTP state decoder refuses
// one also as an explicit zero.
func TestIncidentStreamNeedsMarker(t *testing.T) {
	t.Parallel()
	topology, frames := incidentStreamFrames(t, false)
	changed := withIncidentMembers(frames[1])
	if _, err := EncodeStreamJSON(incidentEnvelope(t, frames[0], changed, "full")); err == nil {
		t.Error("the full encoder writes a stage 1 member without the marker")
	}
	if _, err := makeDelta(frames[0], changed); err == nil {
		t.Error("the delta encoder writes a stage 1 member without the marker")
	}
	if _, err := EncodeStateJSON(topology, changed); err == nil {
		t.Error("the HTTP state encoder writes a stage 1 member without the marker")
	}
	if _, err := ApplyStream(StreamFrame{}, "", 0, incidentEnvelope(t, frames[0], changed, "full")); err == nil {
		t.Error("ApplyStream accepts a typed stage 1 member without the marker")
	}
	// A delta that the process builds has no raw presence record, so the
	// group replacement refuses the incident group by itself.
	for _, group := range []string{`{"interrupted":0,"interruptedPassengers":0}`, `{"interrupted":1,"interruptedPassengers":1}`} {
		delta := incidentEnvelope(t, frames[0], frames[1], "delta")
		delta.Delta.Groups["incident"] = jsontext.Value(group)
		if _, err := ApplyStream(frames[0], "incident", 1, delta); err == nil {
			t.Errorf("ApplyStream accepts the unmarked incident group %s", group)
		}
	}
	httpState, err := EncodeStateJSON(topology, frames[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStateJSON(httpState); err != nil {
		t.Fatal("control", err)
	}
	for _, insert := range []string{`"interrupted":0,`, `"interruptedPassengers":0,`} {
		edited := bytes.Replace(httpState, []byte(`"simulation":{`), []byte(`"simulation":{`+insert), 1)
		if _, err := DecodeStateJSON(edited); err == nil {
			t.Errorf("the HTTP state decoder accepts %s without the marker", insert)
		}
	}
	edited := bytes.Replace(httpState, []byte(`"vehicles":[{`), []byte(`"vehicles":[{"withdrawn":0,`), 1)
	if _, err := DecodeStateJSON(edited); err == nil {
		t.Error("the HTTP state decoder accepts withdrawn 0 without the marker")
	}
}

// TestIncidentStreamPackedLegOrigin checks that scanPackedOrders refuses
// noncanonical text in a packed leg origin of a full and a delta frame
// (incident contract, section 14.3).
func TestIncidentStreamPackedLegOrigin(t *testing.T) {
	t.Parallel()
	_, frames := incidentStreamFrames(t, true)
	changed := withIncidentMembers(frames[1])
	packed := []byte(`"legFrom":"Z2FyZGVu"`)
	for _, kind := range []string{"full", "delta"} {
		raw, err := EncodeStreamJSON(incidentEnvelope(t, frames[1], changed, kind))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := applyIncidentJSON(frames[1], raw); err != nil {
			t.Fatal("control", kind, err)
		}
		for _, text := range []string{`"\u005a2FyZGVu"`, `"Z2FyZGV"`, `"Z2FyZGVu\n"`, `"` + strings.Repeat("A", 88) + `"`} {
			edited := bytes.Replace(raw, packed, []byte(`"legFrom":`+text), 1)
			if bytes.Equal(edited, raw) {
				t.Fatal("no packed leg origin", kind, text)
			}
			if err := scanPackedOrders(edited); err == nil {
				t.Errorf("%s: scanPackedOrders accepts %s", kind, text)
			}
			if _, err := DecodeStreamJSON(edited); err == nil {
				t.Errorf("%s: the decoder accepts %s", kind, text)
			}
		}
	}
	var request sim.Request
	if err := jsonv2.Unmarshal([]byte(`{"legFrom":"garden"}`), &request, json.DefaultOptionsV1()); err != nil || request.LegFrom != "garden" {
		t.Fatalf("the native member %q: %v", request.LegFrom, err)
	}
}

// TestIncidentPathsMatchTags checks that each path of the raw presence
// scans names a member with its exact case.
func TestIncidentPathsMatchTags(t *testing.T) {
	t.Parallel()
	paths, _ := wirePaths(t)
	for _, set := range []map[string]bool{streamIncidentPaths, savedIncidentPaths} {
		for path := range set {
			if !paths[path] {
				t.Errorf("incident path %q names no member", path)
			}
		}
	}
}
