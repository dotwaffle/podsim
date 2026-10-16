package session

import (
	"bytes"
	"encoding/json/jsontext"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func motionFrames(t *testing.T) (StreamFrame, StreamFrame) {
	t.Helper()
	shared, previous := streamFixture(t)
	t.Cleanup(shared.Close)
	current := previous
	current.State.Revision++
	current.State.Simulation.Vehicles = slices.Clone(previous.State.Simulation.Vehicles)
	return previous, current
}

func motionEnvelope(t *testing.T, previous, current StreamFrame) StreamEnvelope {
	t.Helper()
	delta, err := makeDelta(previous, current)
	if err != nil {
		t.Fatal(err)
	}
	return StreamEnvelope{OrderContract: current.State.Simulation.OrderContract, Kind: "delta", Stream: "motion", Sequence: 2, Base: 1, Source: sourceOf(current), Build: current.State.Build, Delta: &delta}
}

func TestMotionDeltaRoundTrip(t *testing.T) {
	t.Parallel()
	for _, motion := range []podMotion{
		{Position: sim.Point{X: -123.5, Y: 456.25}, LaneDistance: 10, Speed: 2},
		{},
	} {
		previous, current := motionFrames(t)
		motion.replace(&current.State.Simulation.Vehicles[0].Pod)
		if current.State.Simulation.Vehicles[0].Pod == previous.State.Simulation.Vehicles[0].Pod {
			previous.State.Simulation.Vehicles[0].Pod.Speed = 1
		}
		before := string(mustJSON(t, previous))
		envelope := motionEnvelope(t, previous, current)
		if len(envelope.Delta.Vehicles) != 1 || envelope.Delta.Vehicles[0].Motion == nil || envelope.Delta.Vehicles[0].Pod != nil {
			t.Fatalf("movement did not use motion replacement: %+v", envelope.Delta.Vehicles)
		}
		raw, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeStreamJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ApplyStream(previous, "motion", 1, decoded)
		if err != nil || !reflect.DeepEqual(got, current) {
			t.Fatalf("motion round trip changed content: %v", err)
		}
		if string(mustJSON(t, previous)) != before {
			t.Fatal("motion mutated retained predecessor")
		}
	}
}

func TestMotionDeltaStableFieldFallback(t *testing.T) {
	t.Parallel()
	for field := range reflect.TypeFor[sim.Pod]().Fields() {
		if field.Name == "Position" || field.Name == "LaneDistance" || field.Name == "Speed" {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()
			previous, current := motionFrames(t)
			pod := &current.State.Simulation.Vehicles[0].Pod
			value := reflect.ValueOf(pod).Elem().FieldByIndex(field.Index)
			switch value.Kind() {
			case reflect.String:
				value.SetString(value.String() + "changed")
			case reflect.Bool:
				value.SetBool(!value.Bool())
			default:
				t.Fatalf("new stable pod field needs a test value: %s", field.Name)
			}
			pod.Position.X++
			envelope := motionEnvelope(t, previous, current)
			if len(envelope.Delta.Vehicles) != 1 || envelope.Delta.Vehicles[0].Motion != nil || envelope.Delta.Vehicles[0].Pod == nil {
				t.Fatal("stable-field transition was reduced to motion")
			}
		})
	}
}

func TestMotionDeltaStreamFamilies(t *testing.T) {
	t.Parallel()
	for name, frames := range streamFamilyFrames(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			previous := frames[0]
			current := previous
			current.State.Revision++
			current.State.Simulation.Vehicles = slices.Clone(previous.State.Simulation.Vehicles)
			current.State.Simulation.Vehicles[0].Pod.Position.X++
			envelope := motionEnvelope(t, previous, current)
			if len(envelope.Delta.Vehicles) != 1 || envelope.Delta.Vehicles[0].Motion == nil {
				t.Fatal("family did not use motion replacement")
			}
			raw, err := EncodeStreamJSON(envelope)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeStreamJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ApplyStream(previous, "motion", 1, decoded)
			if err != nil || !reflect.DeepEqual(got, current) {
				t.Fatalf("family motion round trip differs: %v", err)
			}
		})
	}
}

func TestMotionDeltaRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		motion   podMotion
		conflict bool
	}{
		{name: "negative speed", motion: podMotion{Speed: -1}},
		{name: "negative lane distance", motion: podMotion{LaneDistance: -1}},
		{name: "nonfinite position", motion: podMotion{Position: sim.Point{X: math.NaN()}}},
		{name: "nonfinite position y", motion: podMotion{Position: sim.Point{Y: math.Inf(-1)}}},
		{name: "nonfinite lane distance", motion: podMotion{LaneDistance: math.Inf(1)}},
		{name: "nonfinite speed", motion: podMotion{Speed: math.Inf(1)}},
		{name: "conflicting pod", conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			previous, current := motionFrames(t)
			motion := VehicleDelta{ID: current.State.Simulation.Vehicles[0].Pod.ID, Motion: &Replacement[podMotion]{Value: test.motion}}
			want := "invalid motion replacement"
			if test.conflict {
				motion.Pod = &Replacement[sim.Pod]{Value: current.State.Simulation.Vehicles[0].Pod}
				want = "conflicting pod and motion replacements"
			}
			envelope := StreamEnvelope{Kind: "delta", Stream: "motion", Sequence: 2, Base: 1, Source: sourceOf(current), Delta: &StreamDelta{Vehicles: []VehicleDelta{motion}}}
			if _, err := EncodeStreamJSON(envelope); err == nil || err.Error() != want {
				t.Fatalf("encode = %v, want %q", err, want)
			}
			if _, err := ApplyStream(previous, "motion", 1, envelope); err == nil || err.Error() != want {
				t.Fatalf("apply = %v, want %q", err, want)
			}
		})
	}
}

func TestMotionReplacementShape(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`null`, `{}`, `{"value":null}`, `{"value":{}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":0}}`,
		`{"value":{"position":{"x":0,"y":0},"speed":0}}`,
		`{"value":{"laneDistance":0,"speed":0}}`,
		`{"value":{"position":null,"laneDistance":0,"speed":0}}`,
		`{"value":{"position":{"x":0},"laneDistance":0,"speed":0}}`,
		`{"value":{"position":{"y":0},"laneDistance":0,"speed":0}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":0,"speed":null}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":0,"speed":"0"}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":0,"speed":-1}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":-1,"speed":0}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":0,"speed":0,"extra":0}}`,
		`{"value":{"position":{"x":0,"y":0},"laneDistance":0,"speed":0},"extra":0}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			data := []byte(`{"delta":{"vehicles":[{"id":"01","motion":` + raw + `}]}}`)
			if _, err := DecodeStreamJSON(data); err == nil {
				t.Fatal("accepted incomplete or invalid motion replacement")
			}
		})
	}
	valid := []byte(`{"delta":{"vehicles":[{"id":"01","motion":{"value":{"position":{"x":0,"y":0},"laneDistance":0,"speed":0}}}]}}`)
	if _, err := DecodeStreamJSON(valid); err != nil {
		t.Fatalf("explicit zero replacement: %v", err)
	}
}

func TestMotionScanKeepsDocumentOrder(t *testing.T) {
	t.Parallel()
	for _, motionFirst := range []bool{true, false} {
		members := `"motion":null,"boardings":null`
		if !motionFirst {
			members = `"boardings":null,"motion":null`
		}
		_, err := DecodeStreamJSON([]byte(`{"delta":{"vehicles":[{"id":"01",` + members + `}]}}`))
		if err == nil || strings.Contains(err.Error(), "motion replacement") != motionFirst {
			t.Fatalf("motion first %t: unexpected first refusal %v", motionFirst, err)
		}
	}
}

func TestStreamMotionGoldenBytes(t *testing.T) {
	t.Parallel()
	shared, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	if err = shared.simulation.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	for range 200 {
		shared.step()
	}
	previous, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		shared.step()
	}
	current, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	for _, frame := range []*StreamFrame{&previous, &current} {
		frame.State.Epoch, frame.State.ServerStart, frame.State.Build = "golden-epoch", "golden-start", "golden-build"
	}
	envelope := motionEnvelope(t, previous, current)
	if len(envelope.Delta.Vehicles) != 1 || envelope.Delta.Vehicles[0].Motion == nil {
		t.Fatal("golden fixture did not exercise motion")
	}
	raw, err := EncodeStreamJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/stream_motion_delta.json"
	if *update {
		value := jsontext.Value(bytes.Clone(raw))
		if err = value.Indent(jsontext.WithIndent("  ")); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(value, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := jsontext.Value(golden)
	if err := want.Compact(); err != nil || !bytes.Equal(raw, want) {
		t.Fatalf("motion golden changed: %v", err)
	}
}
