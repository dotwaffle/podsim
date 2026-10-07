package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func boardingStreamFixture(t *testing.T) (TopologySnapshot, StreamFrame) {
	t.Helper()
	shared, frame := streamFixture(t)
	t.Cleanup(shared.Close)
	topology := shared.Topology()
	v := &frame.State.Simulation.Vehicles[0]
	v.Riders = []sim.Request{{ID: 1, From: topology.Network.Stations[0].ID, To: topology.Network.Stations[1].ID, PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}}
	v.Boardings = []sim.RiderBoarding{{BerthID: topology.Network.Stations[0].Berths[0].ID, MetersAtBoarding: 0}}
	v.RiddenMeters = 10
	return topology, frame
}

func TestStreamBoardingPresence(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"full":{"state":{"simulation":{"vehicles":[{"pod":{"berthID":"old"}}]}}}}`, `{"delta":{"vehicles":[{"pod":{"value":{"berthID":"old"}}}]}}`} {
		if _, err := DecodeStreamJSON([]byte(raw)); err != nil {
			t.Fatal("pod berth path rejected", err)
		}
	}
	for _, record := range []string{`null`, `[]`, `[null]`, `[{}]`, `[{"berthID":"b"}]`, `[{"berthID":null,"metersAtBoarding":0}]`, `[{"berthID":"b","metersAtBoarding":null}]`, `[{"berthID":"b","metersAtBoarding":-1}]`, `[{"berthID":"b","metersAtBoarding":0,"extra":0}]`, `[{"berthID":"b","berthid":"b","metersAtBoarding":0}]`, `[{"bErThId":"b","mEtErSaTbOaRdInG":0}]`} {
		raw := []byte(`{"full":{"state":{"simulation":{"vehicles":[{"boardings":` + record + `}]}}}}`)
		if _, err := DecodeStreamJSON(raw); err == nil {
			t.Errorf("accepted records %s", record)
		}
	}
	for _, wrapper := range []string{`null`, `{}`, `{"value":null}`, `{"value":[],"extra":0}`, `{"value":[],"Value":[]}`} {
		raw := []byte(`{"delta":{"vehicles":[{"boardings":` + wrapper + `}]}}`)
		if _, err := DecodeStreamJSON(raw); err == nil {
			t.Errorf("accepted wrapper %s", wrapper)
		}
	}
	valid := `[{"berthID":"b","metersAtBoarding":0}]`
	if _, err := DecodeStreamJSON([]byte(`{"full":{"state":{"simulation":{"vehicles":[{"boardings":` + valid + `}]}}}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStreamJSON([]byte(`{"full":{"state":{"simulation":{"vehicles":[{"bOaRdInGs":` + valid + `}]}}}}`)); err == nil {
		t.Error("accepted a folded boardings name")
	}
}

func TestStreamBoardingDeltaPairing(t *testing.T) {
	_, original := boardingStreamFixture(t)
	for _, change := range []string{"riders", "boardings", "enter", "leave", "leave-riders", "metadata", "ordinary"} {
		t.Run(change, func(t *testing.T) {
			a, b := ownStreamBoardings(original), ownStreamBoardings(original)
			switch change {
			case "riders":
				b.State.Simulation.Vehicles[0].Riders[0].Completed = true
			case "boardings":
				b.State.Simulation.Vehicles[0].Boardings[0].MetersAtBoarding = 1
			case "enter":
				a.State.Simulation.Vehicles[0].Boardings = nil
				a.State.Simulation.Vehicles[0].RiddenMeters = 0
			case "leave", "leave-riders":
				b.State.Simulation.Vehicles[0].Boardings = nil
				b.State.Simulation.Vehicles[0].RiddenMeters = 0
				if change == "leave-riders" {
					b.State.Simulation.Vehicles[0].Riders = nil
				}
			case "metadata":
				b.State.Simulation.Vehicles[0].RiddenMeters = 12
			case "ordinary":
				a.State.Simulation.Vehicles[0].Boardings = nil
				a.State.Simulation.Vehicles[0].RiddenMeters = 0
				b = ownStreamBoardings(a)
				b.State.Simulation.Vehicles[0].Riders[0].Completed = true
			}
			b.State.Revision++
			d, err := makeDelta(a, b)
			if err != nil {
				t.Fatal(err)
			}
			paired := change != "metadata" && change != "ordinary"
			item := d.Vehicles[0]
			if paired && (item.Boardings == nil || item.Riders == nil) {
				t.Fatal("aligned replacements missing")
			}
			if !paired && item.Boardings != nil {
				t.Fatal("unnecessary boarding replacement")
			}
			e := StreamEnvelope{Kind: "delta", Stream: "boarding", Sequence: 2, Base: 1, Build: b.State.Build, Source: sourceOf(b), Delta: &d}
			before := streamJSON(t, a)
			got, err := ApplyStream(a, "boarding", 1, e)
			if err != nil || !reflect.DeepEqual(got, b) {
				t.Fatalf("delta mismatch: %v", err)
			}
			if paired {
				saved := d.Vehicles[0].Boardings
				d.Vehicles[0].Boardings = nil
				if _, err := ApplyStream(a, "boarding", 1, e); err == nil {
					t.Fatal("accepted omitted paired boarding replacement")
				}
				d.Vehicles[0].Boardings = saved
				d.Vehicles[0].Riders = nil
				if _, err := ApplyStream(a, "boarding", 1, e); err == nil {
					t.Fatal("accepted omitted paired rider replacement")
				}
			}
			if !bytes.Equal(before, streamJSON(t, a)) {
				t.Fatal("candidate changed predecessor")
			}
			if len(got.State.Simulation.Vehicles[0].Boardings) > 0 {
				got.State.Simulation.Vehicles[0].Boardings[0].BerthID = "changed"
				if len(a.State.Simulation.Vehicles[0].Boardings) > 0 && a.State.Simulation.Vehicles[0].Boardings[0].BerthID == "changed" {
					t.Fatal("shared boarding slice")
				}
			}
		})
	}
	a := ownStreamBoardings(original)
	b := ownStreamBoardings(a)
	b.State.Revision++
	b.State.Simulation.Vehicles[0].Boardings = nil
	d, err := makeDelta(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyStream(a, "boarding", 1, StreamEnvelope{Kind: "delta", Stream: "boarding", Sequence: 2, Base: 1, Build: b.State.Build, Source: sourceOf(b), Delta: &d}); err == nil {
		t.Fatal("boarding clear retained stale distance")
	}
}

func TestStreamBoardingBindingAndRollback(t *testing.T) {
	topology, frame := boardingStreamFixture(t)
	for _, defect := range []string{"station", "unknown", "class", "unknown-consent", "alignment", "count", "baseline", "nan", "distance", "id"} {
		t.Run(defect, func(t *testing.T) {
			a, err := NewStreamAssembler(topology)
			if err != nil {
				t.Fatal(err)
			}
			bad := ownStreamBoardings(frame)
			v := &bad.State.Simulation.Vehicles[0]
			switch defect {
			case "station":
				v.Boardings[0].BerthID = topology.Network.Stations[1].Berths[0].ID
			case "unknown":
				v.Boardings[0].BerthID = "missing"
			case "class":
				v.Pod.Class = sim.CompactClass
				a.boardingBerths[v.Boardings[0].BerthID] = boardingBerth{station: v.Riders[0].From, classes: 1}
			case "unknown-consent":
				v.Riders[0].SharingConsent = "legacy-unknown"
			case "alignment":
				v.Boardings = append(v.Boardings, v.Boardings[0])
			case "count":
				v.Boardings = slices.Repeat(v.Boardings, 9)
				v.Riders = slices.Repeat(v.Riders, 9)
			case "baseline":
				v.Boardings[0].MetersAtBoarding = 11
			case "nan":
				v.Boardings[0].MetersAtBoarding = math.NaN()
			case "distance":
				v.RiddenMeters = math.Inf(1)
			case "id":
				v.Boardings[0].BerthID = strings.Repeat("x", 65)
			}
			if _, stateErr := a.State(bad); stateErr == nil {
				t.Fatal("accepted invalid binding")
			}
			if a.classes != nil || len(a.previous.State.Simulation.Vehicles) > 0 {
				t.Fatal("rejection remembered state")
			}
			if defect == "class" {
				a.boardingBerths[frame.State.Simulation.Vehicles[0].Boardings[0].BerthID] = boardingBerth{station: frame.State.Simulation.Vehicles[0].Riders[0].From}
			}
			state, err := a.State(frame)
			if err != nil {
				t.Fatal("rejection poisoned next candidate", err)
			}
			frameCopy := ownStreamBoardings(frame)
			frameCopy.State.Simulation.Vehicles[0].Boardings[0].BerthID = "changed"
			if state.Simulation.Vehicles[0].Boardings[0].BerthID == "changed" {
				t.Fatal("shared published records")
			}
		})
	}
	a, err := NewStreamAssembler(topology)
	if err != nil {
		t.Fatal(err)
	}
	v := &frame.State.Simulation.Vehicles[0]
	v.Riders = append(v.Riders, v.Riders[0])
	v.Riders[1].ID = 2
	v.Boardings = append(v.Boardings, v.Boardings[0])
	if _, stateErr := a.State(frame); stateErr != nil {
		t.Fatal("repeated berth rejected", stateErr)
	}
	raw, err := jsonv2.Marshal(frame, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	frame.State.Simulation.Vehicles[0].Boardings[0].BerthID = "changed"
	if bytes.Contains(raw, []byte("changed")) {
		t.Fatal("invalid fixture")
	}
}

func TestStreamBoardingFrameCopies(t *testing.T) {
	t.Parallel()
	topology, frame := boardingStreamFixture(t)
	state, err := FrameState(topology, frame.State)
	if err != nil {
		t.Fatal(err)
	}
	copied := stateFrame(state)
	frame.State.Simulation.Vehicles[0].Boardings[0].BerthID = "input changed"
	state.Simulation.Vehicles[0].Boardings[0].BerthID = "state changed"
	if copied.Simulation.Vehicles[0].Boardings[0].BerthID == "state changed" {
		t.Fatal("frame shares records")
	}
	e := StreamEnvelope{Kind: "full", Stream: "x", Sequence: 1, Build: copied.Build, Source: sourceOf(StreamFrame{State: copied}), Full: &StreamFrame{State: copied, Routes: slices.Clone(frame.Routes)}}
	got, err := ApplyStream(StreamFrame{}, "", 0, e)
	if err != nil {
		t.Fatal(err)
	}
	copied.Simulation.Vehicles[0].Boardings[0].BerthID = "frame changed"
	if got.State.Simulation.Vehicles[0].Boardings[0].BerthID == "frame changed" {
		t.Fatal("full shares records")
	}
}

func TestStreamOrdinaryBoardingBytes(t *testing.T) {
	t.Parallel()
	// Keep the pre-boarding wire shape to detect ordinary encoding changes.
	type oldVehicle struct {
		Pod          sim.Pod       `json:"pod"`
		Riders       []sim.Request `json:"riders,omitempty"`
		Stops        []string      `json:"stops,omitempty"`
		RouteLaneIDs []string      `json:"routeLaneIDs"`
		RelocatingTo string        `json:"relocatingTo"`
		Rebalancing  bool          `json:"rebalancing"`
		PlatoonID    string        `json:"platoonID,omitempty"`
		PlatoonIndex int           `json:"platoonIndex,omitzero"`
	}
	type oldMetadata struct {
		RelocatingTo string `json:"relocatingTo"`
		Rebalancing  bool   `json:"rebalancing"`
		PlatoonID    string `json:"platoonID"`
		PlatoonIndex int    `json:"platoonIndex"`
	}
	_, frame := streamFixture(t)
	historical := widestVehicle()
	for _, v := range []VehicleFrame{frame.State.Simulation.Vehicles[0], historical} {
		old := oldVehicle{v.Pod, v.Riders, v.Stops, v.RouteLaneIDs, v.RelocatingTo, v.Rebalancing, v.PlatoonID, v.PlatoonIndex}
		if !bytes.Equal(streamJSON(t, v), streamJSON(t, old)) {
			t.Fatal("ordinary vehicle bytes changed")
		}
		metadata := oldMetadata{v.RelocatingTo, v.Rebalancing, v.PlatoonID, v.PlatoonIndex}
		if !bytes.Equal(streamJSON(t, meta(v)), streamJSON(t, metadata)) {
			t.Fatal("ordinary metadata bytes changed")
		}
	}
}

func TestStreamBoardingZeroDistanceRoundTrip(t *testing.T) {
	t.Parallel()
	topology, frame := boardingStreamFixture(t)
	frame.State.Simulation.Vehicles[0].RiddenMeters = 0
	e := StreamEnvelope{Kind: "full", Stream: "zero", Sequence: 1, Build: frame.State.Build, Source: sourceOf(frame), Full: &frame}
	raw, err := EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"riddenMeters"`)) || !bytes.Contains(raw, []byte(`"metersAtBoarding":0`)) {
		t.Fatal("zero baseline or cumulative omission changed")
	}
	decoded, err := DecodeStreamJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ApplyStream(StreamFrame{}, "", 0, decoded)
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewStreamAssembler(topology)
	if err != nil {
		t.Fatal(err)
	}
	if _, stateErr := assembler.State(got); stateErr != nil {
		t.Fatal(stateErr)
	}
	for _, value := range []string{`null`, `-1`, `"0"`, `true`} {
		for _, wrapper := range []string{`{"full":{"state":{"simulation":{"vehicles":[{"riddenMeters":%s}]}}}}`, `{"delta":{"vehicles":[{"metadata":{"value":{"riddenMeters":%s}}}]}}`} {
			raw := []byte(strings.Replace(wrapper, "%s", value, 1))
			if _, decodeErr := DecodeStreamJSON(raw); decodeErr == nil {
				t.Errorf("accepted distance %s", raw)
			}
		}
	}
}

func TestStreamRecordedRiderConsent(t *testing.T) {
	topology, frame := boardingStreamFixture(t)
	for _, test := range []struct {
		name      string
		consent   sim.SharingConsent
		completed bool
		reject    bool
	}{
		{"active-private", sim.PrivateConsent, false, true},
		{"active-shared", sim.SharedConsent, false, false},
		{"completed-private", sim.PrivateConsent, true, false},
		{"completed-unknown", "legacy-unknown", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := ownStreamBoardings(frame)
			candidate.State.Revision++
			rider := &candidate.State.Simulation.Vehicles[0].Riders[0]
			rider.SharingConsent = test.consent
			rider.Completed = test.completed
			full := StreamEnvelope{Kind: "full", Stream: "consent", Sequence: 1, Build: candidate.State.Build, Source: sourceOf(candidate), Full: &candidate}
			delta, err := makeDelta(frame, candidate)
			if err != nil {
				t.Fatal(err)
			}
			for _, envelope := range []StreamEnvelope{full, {Kind: "delta", Stream: "consent", Sequence: 2, Base: 1, Build: candidate.State.Build, Source: sourceOf(candidate), Delta: &delta}} {
				before := streamJSON(t, frame)
				got, applyErr := ApplyStream(frame, "consent", 1, envelope)
				if (applyErr != nil) != test.reject {
					t.Fatalf("%s consent validation: %v", envelope.Kind, applyErr)
				}
				if !test.reject && got.State.Simulation.Vehicles[0].Riders[0].SharingConsent != test.consent {
					t.Fatal("candidate changed accepted consent")
				}
				if !bytes.Equal(before, streamJSON(t, frame)) {
					t.Fatal("candidate changed predecessor")
				}
			}
			assembler, err := NewStreamAssembler(topology)
			if err != nil {
				t.Fatal(err)
			}
			state, stateErr := assembler.State(candidate)
			if (stateErr != nil) != test.reject {
				t.Fatalf("assembler consent validation: %v", stateErr)
			}
			if test.reject && assembler.classes != nil {
				t.Fatal("rejected consent changed class binding")
			}
			if !test.reject && state.Simulation.Vehicles[0].Riders[0].SharingConsent != test.consent {
				t.Fatal("assembler changed accepted consent")
			}
		})
	}
}

// TestScanStreamBoardingMembersRefusalOrder pins the members that the
// boarding scan checks and the error that it gives. The scan reads the
// members in document order and stops at the first refusal. A delta
// boarding member needs one value member before its records are checked.
func TestScanStreamBoardingMembersRefusalOrder(t *testing.T) {
	t.Parallel()
	const (
		distance    = "invalid passenger chain distance"
		replacement = "boarding replacement needs exactly one value"
		count       = "boarding records need 1 to 8 entries"
		shape       = "boarding records need an array"
	)
	records := func(n int) string {
		return "[" + strings.TrimSuffix(strings.Repeat(`{"berthID":"b","metersAtBoarding":0},`, n), ",") + "]"
	}
	full := func(vehicle string) string {
		return `{"full":{"state":{"simulation":{"vehicles":[{},` + vehicle + `]}}}}`
	}
	delta := func(vehicle string) string { return `{"delta":{"vehicles":[{},` + vehicle + `]}}` }
	for _, test := range []struct {
		name  string
		order sim.OrderContract
		raw   string
		want  string
	}{
		{"distance_before_records", "", full(`{"riddenMeters":-1,"boardings":[]}`), distance},
		{"records_before_distance", "", full(`{"boardings":[],"riddenMeters":-1}`), count},
		{"frame_distance", "", `{"frame":{"state":{"simulation":{"vehicles":[{"riddenMeters":null}]}}}}`, distance},
		{"metadata_distance", "", delta(`{"metadata":{"value":{"riddenMeters":-1}}}`), distance},
		{"replacement_before_records", "", delta(`{"boardings":{"value":{},"x":1}}`), replacement},
		{"delta_records", "", delta(`{"boardings":{"value":{}}}`), shape},
		{"delta_empty_records", "", delta(`{"boardings":{"value":[]}}`), ""},
		{"refusal_before_syntax_error", "", `{"full":{"state":{"simulation":{"vehicles":[{"riddenMeters":-1}]!`, distance},
		{"ordinary_limit", "", full(`{"boardings":` + records(9) + `}`), count},
		{"express_limit", sim.ExpressOrderContract, full(`{"boardings":` + records(20) + `}`), ""},
		{"express_past_limit", sim.ExpressOrderContract, full(`{"boardings":` + records(21) + `}`), count},
		{"delta_distance_unchecked", "", delta(`{"riddenMeters":-1}`), ""},
		{"metadata_records_unchecked", "", delta(`{"metadata":{"value":{"boardings":-1}}}`), ""},
		{"other_member_unchecked", "", `{"full":{"state":{"simulation":{"riders":[{"riddenMeters":-1}]}}}}`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := scanStreamBoardingMembers([]byte(test.raw), contractMarkers{order: test.order})
			if test.want == "" && err != nil || test.want != "" && (err == nil || err.Error() != test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
