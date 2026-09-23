package session

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// frameFixture describes a session with an active journey and save points.
type frameFixture struct {
	name        string
	checkpoints int
	// restoring is the number of save points, oldest first, from before a
	// demand change. A rewind to one of them restores the project.
	restoring int
}

var frameFixtures = []frameFixture{
	{"no checkpoints", 0, 0},
	{"with checkpoints", 2, 0},
	{"with project-restoring checkpoints", 2, 1},
}

// newFrameFixture starts a journey and saves the checkpoints of fixture
// while the clock runs. Save point i has ID i+1 and tick i+1.
func newFrameFixture(t *testing.T, fixture frameFixture) *Session {
	t.Helper()
	shared := newTestSession(t)
	client := newTestClient(shared, "fixture")
	client.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "market"})
	save := func(count int) {
		for range count {
			shared.advance()
			client.mustApply(t, Command{Action: "checkpoint"})
		}
	}
	save(fixture.restoring)
	if fixture.restoring > 0 {
		changeTestDemand(t, client)
	}
	save(fixture.checkpoints - fixture.restoring)
	for range 120 {
		shared.advance()
	}
	return shared
}

// checkpointsJSON returns the encoded checkpoint list of fixture.
func checkpointsJSON(fixture frameFixture) string {
	items := make([]string, fixture.checkpoints)
	for i := range items {
		number := strconv.Itoa(i + 1)
		items[i] = `{"id":` + number + `,"tick":` + number
		if i < fixture.restoring {
			items[i] += `,"restoresProject":true`
		}
		items[i] += "}"
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestStateFrameRoundTrip(t *testing.T) {
	t.Parallel()
	for _, fixture := range frameFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			shared := newFrameFixture(t, fixture)
			want := shared.State()
			if len(want.Checkpoints) != fixture.checkpoints {
				t.Fatalf("state has %d checkpoints, want %d", len(want.Checkpoints), fixture.checkpoints)
			}
			for i, entry := range want.Checkpoints {
				if entry.RestoresProject != (i < fixture.restoring) {
					t.Fatalf("checkpoint %d restores the project = %t", entry.ID, entry.RestoresProject)
				}
			}
			frame := stateFrame(want)
			got, err := FrameState(shared.Topology(), frame)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round trip changed state\n got: %#v\nwant: %#v", got, want)
			}
			if fixture.checkpoints == 0 && (want.Checkpoints != nil || frame.Checkpoints != nil || got.Checkpoints != nil) {
				t.Fatal("an empty checkpoint list is not nil")
			}
			if fixture.checkpoints > 0 && (&frame.Checkpoints[0] == &want.Checkpoints[0] || &got.Checkpoints[0] == &frame.Checkpoints[0]) {
				t.Fatal("round trip shares the checkpoint list")
			}
		})
	}
}

func TestStateFrameJSONOmitsTopologyAndLaneObjects(t *testing.T) {
	t.Parallel()
	for _, fixture := range frameFixtures {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			encoded := mustJSON(t, newFrameFixture(t, fixture).Frame())
			for _, repeated := range [][]byte{[]byte(`"network"`), []byte(`"Route"`), []byte(`"SpeedLimit"`)} {
				if bytes.Contains(encoded, repeated) {
					t.Fatalf("state frame contains repeated topology field %s", repeated)
				}
			}
			if !bytes.Contains(encoded, []byte(`"RouteLaneIDs"`)) {
				t.Fatal("state frame omits route lane IDs")
			}
			// An empty list and a false restoresProject are omitted, so frames
			// without save points or project restores keep their size.
			list, listed := jsonKeys(t, encoded)["checkpoints"]
			if listed != (fixture.checkpoints > 0) {
				t.Fatalf("state frame checkpoints key present = %t, want %t", listed, fixture.checkpoints > 0)
			}
			if want := checkpointsJSON(fixture); listed && string(list) != want {
				t.Fatalf("state frame checkpoints = %s, want %s", list, want)
			}
		})
	}
}

func TestCommandReplyIsCompactTypedAcknowledgement(t *testing.T) {
	t.Parallel()
	shared := newTestSession(t)
	reply := shared.Apply(commandFor(shared, "pause"))
	if reply.Error != "" || reply.Epoch == "" || reply.Revision != shared.State().Revision {
		t.Fatalf("invalid acknowledgement: %+v", reply)
	}
	encoded := mustJSON(t, reply)
	if bytes.Contains(encoded, []byte(`"state"`)) || bytes.Contains(encoded, []byte(`"simulation"`)) {
		t.Fatalf("command acknowledgement repeats state: %s", encoded)
	}
	if _, ok := jsonKeys(t, encoded)["checkpoint"]; ok {
		t.Fatalf("pause acknowledgement contains a checkpoint ID: %s", encoded)
	}
	save := commandFor(shared, "checkpoint")
	save.Client = "saver"
	encoded = mustJSON(t, shared.Apply(save))
	if id := jsonKeys(t, encoded)["checkpoint"]; string(id) != "1" {
		t.Fatalf("checkpoint acknowledgement has checkpoint ID %s, want 1: %s", id, encoded)
	}
	invalid := commandFor(shared, "unknown")
	invalid.Sequence = 2
	if rejected := shared.Apply(invalid); rejected.Error == "" || rejected.ErrorCode != CommandRejected {
		t.Fatalf("untyped rejection: %+v", rejected)
	}
}

// jsonKeys returns the top-level members of a JSON object.
func jsonKeys(t *testing.T, encoded []byte) map[string]json.RawMessage {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatal(err)
	}
	return keys
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
