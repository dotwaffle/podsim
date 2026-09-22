package session

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestStateFrameRoundTrip(t *testing.T) {
	t.Parallel()
	shared := newTestSession(t)
	command := commandFor(shared, "trip")
	command.Origin, command.Destination = "harbor", "market"
	if reply := shared.Apply(command); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	for range 120 {
		shared.advance()
	}
	want := shared.State()
	got, err := FrameState(shared.Topology(), stateFrame(want))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed state\n got: %#v\nwant: %#v", got, want)
	}
}

func TestStateFrameJSONOmitsTopologyAndLaneObjects(t *testing.T) {
	t.Parallel()
	shared := newTestSession(t)
	command := commandFor(shared, "trip")
	command.Origin, command.Destination = "harbor", "market"
	if reply := shared.Apply(command); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	encoded, err := json.Marshal(shared.Frame())
	if err != nil {
		t.Fatal(err)
	}
	for _, repeated := range [][]byte{[]byte(`"network"`), []byte(`"Route"`), []byte(`"SpeedLimit"`)} {
		if bytes.Contains(encoded, repeated) {
			t.Fatalf("state frame contains repeated topology field %s", repeated)
		}
	}
	if !bytes.Contains(encoded, []byte(`"RouteLaneIDs"`)) {
		t.Fatal("state frame omits route lane IDs")
	}
}

func TestCommandReplyIsCompactTypedAcknowledgement(t *testing.T) {
	t.Parallel()
	shared := newTestSession(t)
	reply := shared.Apply(commandFor(shared, "pause"))
	if reply.Error != "" || reply.Epoch == "" || reply.Revision != shared.State().Revision {
		t.Fatalf("invalid acknowledgement: %+v", reply)
	}
	encoded, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"state"`)) || bytes.Contains(encoded, []byte(`"simulation"`)) {
		t.Fatalf("command acknowledgement repeats state: %s", encoded)
	}
	invalid := commandFor(shared, "unknown")
	invalid.Sequence = 2
	if rejected := shared.Apply(invalid); rejected.Error == "" || rejected.ErrorCode != CommandRejected {
		t.Fatalf("untyped rejection: %+v", rejected)
	}
}

func TestStateFrameRejectsMismatchedTopology(t *testing.T) {
	t.Parallel()
	shared := newTestSession(t)
	topology, frame := shared.Topology(), shared.Frame()
	topology.ProjectRevision++
	if _, err := FrameState(topology, frame); err == nil {
		t.Fatal("accepted mismatched topology")
	}
	topology = shared.Topology()
	frame.Simulation.Vehicles[0].RouteLaneIDs = []string{"missing"}
	if _, err := FrameState(topology, frame); err == nil {
		t.Fatal("accepted unknown route lane")
	}
}
